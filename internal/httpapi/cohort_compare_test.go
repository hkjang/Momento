package httpapi

import (
	"strings"
	"testing"
	"time"
)

func cohortRow(date string, size int64, retained []int64) map[string]any {
	values := make([]map[string]any, len(retained))
	for index, users := range retained {
		values[index] = map[string]any{"period": index, "users": users, "retention_rate": percent(users, size)}
	}
	return map[string]any{"cohort": date, "size": size, "periods": values}
}

func rateAt(curve []map[string]any, period int) float64 { return curveRate(curve, period) }

func TestPooledCurveWeightsCohortsBySize(t *testing.T) {
	t.Parallel()

	end := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC) // exclusive window end
	grid := []map[string]any{
		// Both cohorts are old enough for every period in the window.
		cohortRow("2026-06-01", 1000, []int64{1000, 500, 300}),
		cohortRow("2026-06-08", 10, []int64{10, 10, 10}),
	}
	total, curve := pooledRetentionCurve(grid, 3, end, "week")
	if total != 1010 {
		t.Fatalf("cohort users = %d, want 1010", total)
	}
	// 510/1010 rather than the arithmetic mean of 50% and 100%.
	if got := rateAt(curve, 1); got < 50.4 || got > 50.6 {
		t.Fatalf("period 1 = %.2f, want the size weighted 50.5", got)
	}
}

func TestYoungCohortsAreNotCountedAsZero(t *testing.T) {
	t.Parallel()

	end := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	grid := []map[string]any{
		// Mature cohort: 12 weeks old, retains half in week one.
		cohortRow("2026-06-01", 100, []int64{100, 50, 50, 50}),
		// Started three days before the window ended: only period 0 is possible.
		cohortRow("2026-08-20", 900, []int64{900, 0, 0, 0}),
	}
	_, curve := pooledRetentionCurve(grid, 4, end, "week")
	if got := rateAt(curve, 1); got < 49.9 || got > 50.1 {
		t.Fatalf("period 1 = %.2f, want 50 because the young cohort is excluded", got)
	}
	exposed, _ := curve[1]["cohort_users"].(int64)
	if exposed != 100 {
		t.Fatalf("period 1 denominator = %d, want only the mature cohort", exposed)
	}
	if exposedZero, _ := curve[0]["cohort_users"].(int64); exposedZero != 1000 {
		t.Fatalf("period 0 denominator = %d, want every cohort", exposedZero)
	}
}

func TestCohortMaturityPerGranularity(t *testing.T) {
	t.Parallel()

	end := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC) // last included day is 08-23
	cohort := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	if got := cohortMaturity(cohort, end, "week"); got != 3 {
		t.Fatalf("week maturity = %d, want 3", got)
	}
	if got := cohortMaturity(cohort, end, "day"); got != 21 {
		t.Fatalf("day maturity = %d, want 21", got)
	}
	if got := cohortMaturity(time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC), end, "month"); got != 2 {
		t.Fatalf("month maturity = %d, want 2 because 08-23 has not reached the 30th", got)
	}
	// A cohort from the final day has lived through nothing yet.
	if got := cohortMaturity(time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC), end, "week"); got != 0 {
		t.Fatalf("same day maturity = %d, want 0", got)
	}
}

func curveOf(key, label string, users int64, rates []float64) map[string]any {
	periods := make([]map[string]any, len(rates))
	for index, rate := range rates {
		periods[index] = map[string]any{"period": index, "retention_rate": rate}
	}
	return map[string]any{"key": key, "label": label, "cohort_users": users, "periods": periods}
}

func TestRetentionComparisonNamesTheWorstPeriod(t *testing.T) {
	t.Parallel()

	baseline, _ := curveOf("baseline", "전체", 1000, []float64{100, 40, 30, 25})["periods"].([]map[string]any)
	weak := curveOf("seg-1", "영업부", 300, []float64{100, 20, 8, 6})

	got := compareRetentionCurves(baseline, []map[string]any{weak}, "week")
	if len(got) != 1 {
		t.Fatalf("comparisons = %d, want 1", len(got))
	}
	item := got[0]
	if item.Verdict != "worse" {
		t.Fatalf("verdict = %q (gap %.1f), want worse", item.Verdict, item.FirstReturnGap)
	}
	if item.FirstReturnGap != -20 {
		t.Fatalf("first return gap = %.1f, want -20", item.FirstReturnGap)
	}
	if item.WorstPeriod != 2 || item.WorstPeriodGap != -22 {
		t.Fatalf("worst period = %d (%.1f), want period 2 at -22", item.WorstPeriod, item.WorstPeriodGap)
	}
	if !strings.Contains(item.Evidence, "2주차") || !strings.Contains(item.Evidence, "낮습니다") {
		t.Fatalf("evidence = %q, want the period in weeks and the direction", item.Evidence)
	}
}

func TestRetentionEvidenceFollowsGranularity(t *testing.T) {
	t.Parallel()

	baseline, _ := curveOf("baseline", "전체", 500, []float64{100, 50, 40})["periods"].([]map[string]any)
	segment := curveOf("seg-2", "지원", 200, []float64{100, 30, 10})

	monthly := compareRetentionCurves(baseline, []map[string]any{segment}, "month")[0]
	if !strings.Contains(monthly.Evidence, "1개월") || !strings.Contains(monthly.Evidence, "2개월차") {
		t.Fatalf("monthly evidence = %q, want month units", monthly.Evidence)
	}
	daily := compareRetentionCurves(baseline, []map[string]any{segment}, "day")[0]
	if !strings.Contains(daily.Evidence, "1일") {
		t.Fatalf("daily evidence = %q, want day units", daily.Evidence)
	}
}

func TestSmallRetentionCohortIsNotJudged(t *testing.T) {
	t.Parallel()

	baseline, _ := curveOf("baseline", "전체", 1000, []float64{100, 40, 30})["periods"].([]map[string]any)
	tiny := curveOf("seg-3", "신규 팀", 7, []float64{100, 0, 0})

	got := compareRetentionCurves(baseline, []map[string]any{tiny}, "week")[0]
	if got.Verdict != "insufficient" || got.Reliable {
		t.Fatalf("verdict = %q reliable = %v, want an unreliable verdict", got.Verdict, got.Reliable)
	}
	if !strings.Contains(got.Evidence, "표본이 적어") {
		t.Fatalf("evidence = %q, want the sample caveat", got.Evidence)
	}
}

func TestBetterRetentionLeadsAndSimilarIsFlat(t *testing.T) {
	t.Parallel()

	baseline, _ := curveOf("baseline", "전체", 1000, []float64{100, 40, 30})["periods"].([]map[string]any)
	better := curveOf("seg-4", "플랫폼", 400, []float64{100, 62, 55})
	similar := curveOf("seg-5", "운영", 400, []float64{100, 42, 31})

	got := compareRetentionCurves(baseline, []map[string]any{similar, better}, "week")
	if got[0].Key != "seg-4" || got[0].Verdict != "better" {
		t.Fatalf("first = %q %q, want the biggest mover first", got[0].Key, got[0].Verdict)
	}
	if got[1].Verdict != "similar" {
		t.Fatalf("second verdict = %q, want similar", got[1].Verdict)
	}
	if got[1].WorstPeriod != 0 {
		t.Fatalf("worst period = %d, want none for a cohort that never falls behind", got[1].WorstPeriod)
	}
}

func TestComparisonNeedsABaselineCurve(t *testing.T) {
	t.Parallel()

	segment := curveOf("seg-6", "영업", 100, []float64{100, 20})
	if got := compareRetentionCurves(nil, []map[string]any{segment}, "week"); len(got) != 0 {
		t.Fatalf("comparisons = %+v, want none without a baseline", got)
	}
}
