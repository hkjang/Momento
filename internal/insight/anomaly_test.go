package insight

import (
	"math"
	"testing"
	"time"
)

func day(value string) time.Time {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		panic(err)
	}
	return parsed
}

// weekdaySeries builds eight Mondays plus noisy other weekdays, which is the shape
// an internal service actually produces.
func weekdaySeries(mondayValue float64, others float64) []AnomalyPoint {
	series := []AnomalyPoint{}
	start := day("2026-06-01") // a Monday
	for week := 0; week < 8; week++ {
		monday := start.AddDate(0, 0, week*7)
		series = append(series, AnomalyPoint{Date: monday, Value: mondayValue})
		for offset := 1; offset <= 6; offset++ {
			series = append(series, AnomalyPoint{Date: monday.AddDate(0, 0, offset), Value: others})
		}
	}
	return series
}

var users = AnomalyMetric{Key: "users", Label: "방문자", Action: "확인"}
var errorsMetric = AnomalyMetric{Key: "errors", Label: "오류", BadWhenHigher: true, Action: "확인"}

func TestWeekdayBaselineIgnoresOtherWeekdays(t *testing.T) {
	t.Parallel()

	// Mondays sit at 1000 while the rest of the week sits at 100. A Monday at 950
	// is normal even though it is far from the weekly average.
	series := weekdaySeries(1000, 100)
	target := day("2026-07-27") // Monday
	series = append(series, AnomalyPoint{Date: target, Value: 950})

	got := DetectAnomaly(users, series, target)
	if got.Severity != "normal" {
		t.Fatalf("severity = %q (z=%.2f, baseline=%.0f), want normal", got.Severity, got.RobustZ, got.Baseline)
	}
	if got.Baseline != 1000 {
		t.Fatalf("baseline = %.0f, want the Monday median 1000", got.Baseline)
	}
	if got.Weekday != "월" {
		t.Fatalf("weekday = %q, want 월", got.Weekday)
	}
}

func TestVisitorCollapseIsCritical(t *testing.T) {
	t.Parallel()

	series := weekdaySeries(1000, 900)
	target := day("2026-07-27")
	series = append(series, AnomalyPoint{Date: target, Value: 120})

	got := DetectAnomaly(users, series, target)
	if got.Severity != "critical" {
		t.Fatalf("severity = %q (z=%.2f), want critical", got.Severity, got.RobustZ)
	}
	if got.Direction != "below" {
		t.Fatalf("direction = %q, want below", got.Direction)
	}
	if got.Action == "" {
		t.Fatal("a detected problem must carry the next action")
	}
	if got.ChangePercent > -80 {
		t.Fatalf("change = %.1f%%, want a large drop", got.ChangePercent)
	}
}

func TestGrowthIsReportedButNotAsAProblem(t *testing.T) {
	t.Parallel()

	series := weekdaySeries(500, 500)
	target := day("2026-07-27")
	series = append(series, AnomalyPoint{Date: target, Value: 1400})

	got := DetectAnomaly(users, series, target)
	if got.Severity != "positive" {
		t.Fatalf("severity = %q (z=%.2f), want positive", got.Severity, got.RobustZ)
	}
	if !got.Detected() {
		t.Fatal("a large positive move should still surface")
	}
	if got.Action != "" {
		t.Fatalf("action = %q, want none for a positive move", got.Action)
	}
}

func TestErrorSpikeIsBadWhenHigher(t *testing.T) {
	t.Parallel()

	series := weekdaySeries(4, 5)
	target := day("2026-07-27")
	series = append(series, AnomalyPoint{Date: target, Value: 90})

	got := DetectAnomaly(errorsMetric, series, target)
	if got.Severity != "critical" || got.Direction != "above" {
		t.Fatalf("severity = %q direction = %q (z=%.2f), want critical above", got.Severity, got.Direction, got.RobustZ)
	}
}

func TestFlatSeriesDoesNotExplodeOnASmallChange(t *testing.T) {
	t.Parallel()

	// Eight identical Mondays give a zero deviation; the floor keeps a one unit
	// change from reading as an infinite anomaly.
	series := weekdaySeries(10, 10)
	target := day("2026-07-27")
	series = append(series, AnomalyPoint{Date: target, Value: 11})

	got := DetectAnomaly(users, series, target)
	if got.Severity != "normal" {
		t.Fatalf("severity = %q (z=%.2f), want normal", got.Severity, got.RobustZ)
	}
	if math.IsInf(got.RobustZ, 0) || math.IsNaN(got.RobustZ) {
		t.Fatalf("robust z = %v, want a finite value", got.RobustZ)
	}
}

func TestSingleOutlierDoesNotMoveTheBaseline(t *testing.T) {
	t.Parallel()

	// One outage Monday must not make the next low Monday look normal.
	series := weekdaySeries(1000, 1000)
	series = append(series, AnomalyPoint{Date: day("2026-07-20"), Value: 0})
	target := day("2026-07-27")
	series = append(series, AnomalyPoint{Date: target, Value: 150})

	got := DetectAnomaly(users, series, target)
	if got.Baseline != 1000 {
		t.Fatalf("baseline = %.0f, want the median to resist the outlier", got.Baseline)
	}
	if got.Severity != "critical" {
		t.Fatalf("severity = %q (z=%.2f), want critical", got.Severity, got.RobustZ)
	}
}

func TestThinHistoryIsNotJudged(t *testing.T) {
	t.Parallel()

	target := day("2026-07-27")
	series := []AnomalyPoint{
		{Date: day("2026-07-25"), Value: 10},
		{Date: day("2026-07-26"), Value: 12},
		{Date: target, Value: 400},
	}
	got := DetectAnomaly(users, series, target)
	if got.Severity != "insufficient_history" {
		t.Fatalf("severity = %q, want insufficient_history", got.Severity)
	}
	if got.Detected() {
		t.Fatal("an unjudged metric must not be reported as an anomaly")
	}
}

func TestNewWeekdayFallsBackToRecentDays(t *testing.T) {
	t.Parallel()

	// Only two Mondays exist, so the baseline falls back to the last four weeks.
	series := []AnomalyPoint{}
	start := day("2026-07-13")
	for offset := 0; offset < 10; offset++ {
		series = append(series, AnomalyPoint{Date: start.AddDate(0, 0, offset), Value: 100})
	}
	target := day("2026-07-27")
	series = append(series, AnomalyPoint{Date: target, Value: 10})

	got := DetectAnomaly(users, series, target)
	if got.Severity != "critical" {
		t.Fatalf("severity = %q (z=%.2f, samples=%d), want critical", got.Severity, got.RobustZ, got.Samples)
	}
	if got.Samples < 10 {
		t.Fatalf("samples = %d, want the recent-day fallback", got.Samples)
	}
}

func TestMissingDayIsReportedAsUnknown(t *testing.T) {
	t.Parallel()

	got := DetectAnomaly(users, weekdaySeries(100, 100), day("2026-08-03"))
	if got.Severity != "unknown" || got.Detected() {
		t.Fatalf("severity = %q, detected = %v, want unknown and not detected", got.Severity, got.Detected())
	}
}

func TestWatchedMetricsCoverTheBasics(t *testing.T) {
	t.Parallel()

	keys := map[string]bool{}
	for _, metric := range WatchedMetrics() {
		if metric.Label == "" || metric.Action == "" {
			t.Fatalf("metric %q is missing a label or an action", metric.Key)
		}
		keys[metric.Key] = true
	}
	for _, want := range []string{"users", "sessions", "events", "conversions", "errors"} {
		if !keys[want] {
			t.Fatalf("watch list is missing %q", want)
		}
	}
}
