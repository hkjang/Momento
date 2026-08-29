package insight

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
)

// Anomaly detection compares a day against the same weekday in recent weeks.
// Internal services have a strong weekly rhythm — Monday never looks like Sunday —
// so a plain week-over-week or 7-day average produces false alarms. The baseline is
// a median with a median absolute deviation, which a single outage day cannot drag.

// AnomalyPoint is one day of one metric.
type AnomalyPoint struct {
	Date  time.Time `json:"date"`
	Value float64   `json:"value"`
}

// AnomalyMetric describes what is being watched and how to read a deviation.
type AnomalyMetric struct {
	Key           string
	Label         string
	BadWhenHigher bool
	Action        string
}

// Anomaly is one evaluated metric with its verdict.
type Anomaly struct {
	Metric        string    `json:"metric"`
	Label         string    `json:"label"`
	Date          time.Time `json:"date"`
	Value         float64   `json:"value"`
	Baseline      float64   `json:"baseline"`
	Deviation     float64   `json:"deviation"`
	ChangePercent float64   `json:"change_percent"`
	RobustZ       float64   `json:"robust_z"`
	Severity      string    `json:"severity"`
	Direction     string    `json:"direction"`
	Samples       int       `json:"samples"`
	// BaselineKind says what the value was actually compared against:
	// same_weekday, or recent_days when there were not enough of the same weekday
	// to judge. The evidence sentence used to say "same weekday" either way, so a
	// reader was told the comparison had been made the way this detector says it
	// must be made, on the days when it had not.
	BaselineKind string `json:"baseline_kind"`
	Weekday      string `json:"weekday"`
	Evidence     string `json:"evidence"`
	Action       string `json:"action,omitempty"`
}

// Detected reports whether the verdict is worth showing to a person.
func (a Anomaly) Detected() bool {
	return a.Severity == "critical" || a.Severity == "warning" || a.Severity == "positive"
}

const (
	minimumBaselineSamples = 3
	weekdayLookbackWeeks   = 8
	fallbackLookbackDays   = 28
)

var weekdayNames = map[time.Weekday]string{
	time.Sunday: "일", time.Monday: "월", time.Tuesday: "화", time.Wednesday: "수",
	time.Thursday: "목", time.Friday: "금", time.Saturday: "토",
}

// Which history a verdict was reached against. A service with a weekly rhythm is
// the reason this detector compares like weekday with like, so the day it cannot
// is a day its own premise does not hold — and the reader has to be told, because
// the number looks identical either way.
const (
	baselineSameWeekday = "same_weekday"
	baselineRecentDays  = "recent_days"
)

// baselineFor collects comparable history for a day: the same weekday first, and
// recent days only when a weekday baseline is too thin to judge.
func baselineFor(series []AnomalyPoint, target time.Time) ([]float64, string) {
	sameWeekday := []float64{}
	recent := []float64{}
	for _, point := range series {
		if !point.Date.Before(target) {
			continue
		}
		age := target.Sub(point.Date)
		if point.Date.Weekday() == target.Weekday() && age <= time.Duration(weekdayLookbackWeeks)*7*24*time.Hour {
			sameWeekday = append(sameWeekday, point.Value)
		}
		if age <= time.Duration(fallbackLookbackDays)*24*time.Hour {
			recent = append(recent, point.Value)
		}
	}
	if len(sameWeekday) >= minimumBaselineSamples {
		return sameWeekday, baselineSameWeekday
	}
	return recent, baselineRecentDays
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

// medianAbsoluteDeviation scaled to be comparable with a standard deviation.
func medianAbsoluteDeviation(values []float64, center float64) float64 {
	if len(values) == 0 {
		return 0
	}
	deviations := make([]float64, 0, len(values))
	for _, value := range values {
		deviations = append(deviations, math.Abs(value-center))
	}
	return median(deviations) * 1.4826
}

// DetectAnomaly evaluates one metric on one day against its own history.
func DetectAnomaly(metric AnomalyMetric, series []AnomalyPoint, target time.Time) Anomaly {
	target = target.UTC().Truncate(24 * time.Hour)
	result := Anomaly{Metric: metric.Key, Label: metric.Label, Date: target, Severity: "unknown", Direction: "flat", Weekday: weekdayNames[target.Weekday()]}
	value, ok := 0.0, false
	for _, point := range series {
		if point.Date.UTC().Truncate(24 * time.Hour).Equal(target) {
			value, ok = point.Value, true
			break
		}
	}
	if !ok {
		result.Evidence = "평가할 날짜의 집계가 없습니다."
		return result
	}
	result.Value = value
	baseline, kind := baselineFor(series, target)
	result.BaselineKind = kind
	result.Samples = len(baseline)
	if len(baseline) < minimumBaselineSamples {
		result.Severity = "insufficient_history"
		result.Evidence = fmt.Sprintf("비교 가능한 과거 데이터가 %d일뿐이어서 판정하지 않았습니다.", len(baseline))
		return result
	}
	center := median(baseline)
	spread := medianAbsoluteDeviation(baseline, center)
	// A metric that barely moves still needs a floor, otherwise a one unit change
	// looks infinitely significant.
	floor := math.Max(1, center*0.1)
	if spread < floor {
		spread = floor
	}
	result.Baseline = center
	result.Deviation = spread
	result.RobustZ = (value - center) / spread
	result.ChangePercent = percentChange(value, center)
	if result.RobustZ > 0 {
		result.Direction = "above"
	} else if result.RobustZ < 0 {
		result.Direction = "below"
	}
	magnitude := math.Abs(result.RobustZ)
	bad := (metric.BadWhenHigher && result.RobustZ > 0) || (!metric.BadWhenHigher && result.RobustZ < 0)
	switch {
	case magnitude >= 3.5 && bad:
		result.Severity = "critical"
	case magnitude >= 2.5 && bad:
		result.Severity = "warning"
	case magnitude >= 2.5 && !bad:
		result.Severity = "positive"
	default:
		result.Severity = "normal"
	}
	basis := fmt.Sprintf("같은 요일 %d주 기준선", len(baseline))
	if kind == baselineRecentDays {
		// Named for what it is. On an internal service a weekday compared against a
		// window holding weekends is the comparison this detector exists to avoid,
		// and the verdict reads the same as any other.
		basis = fmt.Sprintf("최근 %d일 기준선(같은 요일 표본 부족, 요일 혼합)", len(baseline))
	}
	result.Evidence = fmt.Sprintf("%s(%s) %.0f · %s %.0f · 편차 %.1fσ · %s",
		target.Format("2006-01-02"), result.Weekday, value, basis, center, result.RobustZ, formatSignedPercent(result.ChangePercent))
	if result.Detected() && result.Severity != "positive" {
		result.Action = metric.Action
	}
	return result
}

func formatSignedPercent(value float64) string {
	if value > 0 {
		return fmt.Sprintf("+%.1f%%", value)
	}
	return fmt.Sprintf("%.1f%%", value)
}

// WatchedMetrics is the default anomaly watch list.
func WatchedMetrics() []AnomalyMetric {
	return []AnomalyMetric{
		{Key: "users", Label: "방문자", Action: "채널·진입 페이지 변화와 배포·공지 일정을 대조하십시오."},
		{Key: "sessions", Label: "세션", Action: "수집 중단 가능성을 설치 진단에서 먼저 확인하십시오."},
		{Key: "events", Label: "이벤트", Action: "SDK 배포 변경이나 계약 위반으로 인한 누락을 확인하십시오."},
		{Key: "conversions", Label: "전환", Action: "Funnel에서 이탈이 커진 단계를 확인하십시오."},
		{Key: "errors", Label: "오류", BadWhenHigher: true, Action: "Experience에서 오류 메시지·페이지·릴리즈 영향을 확인하십시오."},
	}
}

// AnomalyReport is the response shape shared by the API, the console and delivery.
type AnomalyReport struct {
	Environment   string    `json:"environment"`
	EvaluatedDate time.Time `json:"evaluated_date"`
	Timezone      string    `json:"timezone"`
	BaselineWeeks int       `json:"baseline_weeks"`
	Detected      []Anomaly `json:"detected"`
	Checked       []Anomaly `json:"checked"`
	Note          string    `json:"note"`
}

// DetectSiteAnomalies evaluates the watch list for the last complete local day.
// Evaluating today would compare a partial day against full days and report a drop
// every morning.
func (rep Reporter) DetectSiteAnomalies(ctx context.Context, siteID uuid.UUID, environment string, location *time.Location) (AnomalyReport, error) {
	now := time.Now().In(location)
	evaluated := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	windowStart := evaluated.AddDate(0, 0, -weekdayLookbackWeeks*7)
	series, err := rep.dailySeries(ctx, siteID, environment, location, windowStart, evaluated.AddDate(0, 0, 1))
	if err != nil {
		return AnomalyReport{}, err
	}
	report := AnomalyReport{
		Environment:   environment,
		EvaluatedDate: evaluated,
		Timezone:      location.String(),
		BaselineWeeks: weekdayLookbackWeeks,
		Detected:      []Anomaly{},
		Checked:       []Anomaly{},
		Note:          "직전 완료된 하루를 같은 요일 최근 8주 중위수와 비교합니다. 부분 집계된 오늘은 평가하지 않습니다.",
	}
	for _, metric := range WatchedMetrics() {
		anomaly := DetectAnomaly(metric, series[metric.Key], evaluated)
		report.Checked = append(report.Checked, anomaly)
		if anomaly.Detected() {
			report.Detected = append(report.Detected, anomaly)
		}
	}
	sort.SliceStable(report.Detected, func(i, j int) bool {
		return math.Abs(report.Detected[i].RobustZ) > math.Abs(report.Detected[j].RobustZ)
	})
	return report, nil
}

// dailySeries reads one daily value per watched metric. It prefers the daily
// rollups the worker already maintains, because scanning eight weeks of raw events
// on every dashboard load is the most expensive query in the product and produces
// the same numbers the Overview screen shows.
func (rep Reporter) dailySeries(ctx context.Context, siteID uuid.UUID, environment string, location *time.Location, from, to time.Time) (map[string][]AnomalyPoint, error) {
	series, err := rep.dailySeriesFromRollups(ctx, siteID, environment, from, to)
	if err != nil {
		return nil, err
	}
	// A day missing from the rollups would look like a collapse, so fall back to the
	// event table when the day being judged has not been aggregated yet.
	evaluated := to.AddDate(0, 0, -1)
	if !hasDay(series["events"], evaluated) {
		return rep.dailySeriesFromEvents(ctx, siteID, environment, location, from, to)
	}
	errorSeries, err := rep.dailyErrorSeries(ctx, siteID, environment, location, from, to)
	if err != nil {
		return nil, err
	}
	series["errors"] = errorSeries
	return series, nil
}

func hasDay(points []AnomalyPoint, day time.Time) bool {
	target := day.UTC().Truncate(24 * time.Hour)
	for _, point := range points {
		if point.Date.UTC().Truncate(24 * time.Hour).Equal(target) {
			return true
		}
	}
	return false
}

// dailySeriesFromRollups reads users, sessions, events and conversions from the
// daily aggregates. Users follow the same identity rule as every other report: one
// SSO user is one person, an anonymous visitor is site scoped.
func (rep Reporter) dailySeriesFromRollups(ctx context.Context, siteID uuid.UUID, environment string, from, to time.Time) (map[string][]AnomalyPoint, error) {
	series := map[string][]AnomalyPoint{}
	rows, err := rep.DB.Query(ctx, `WITH days AS (
			SELECT event_date FROM daily_site_metrics WHERE site_id=$1 AND environment=$2 AND event_date >= $3::date AND event_date < $4::date
		), visitors AS (
			SELECT d.event_date,count(DISTINCT coalesce('u:'||i.user_id,'v:'||d.visitor_id)) users
			FROM daily_site_visitors d LEFT JOIN visitor_identities i ON i.site_id=d.site_id AND i.visitor_id=d.visitor_id
			WHERE d.site_id=$1 AND d.environment=$2 AND d.event_date >= $3::date AND d.event_date < $4::date GROUP BY 1
		), sessions AS (
			SELECT event_date,count(*) sessions FROM daily_site_sessions
			WHERE site_id=$1 AND environment=$2 AND event_date >= $3::date AND event_date < $4::date GROUP BY 1
		)
		SELECT m.event_date,coalesce(v.users,0),coalesce(s.sessions,0),m.events,m.conversions
		FROM daily_site_metrics m
		LEFT JOIN visitors v ON v.event_date=m.event_date
		LEFT JOIN sessions s ON s.event_date=m.event_date
		WHERE m.site_id=$1 AND m.environment=$2 AND m.event_date >= $3::date AND m.event_date < $4::date
		ORDER BY 1`, siteID, environment, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var day time.Time
		var users, sessions, events, conversions int64
		if err := rows.Scan(&day, &users, &sessions, &events, &conversions); err != nil {
			return nil, err
		}
		date := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
		series["users"] = append(series["users"], AnomalyPoint{Date: date, Value: float64(users)})
		series["sessions"] = append(series["sessions"], AnomalyPoint{Date: date, Value: float64(sessions)})
		series["events"] = append(series["events"], AnomalyPoint{Date: date, Value: float64(events)})
		series["conversions"] = append(series["conversions"], AnomalyPoint{Date: date, Value: float64(conversions)})
	}
	return series, rows.Err()
}

// dailyErrorSeries counts error events per day. Errors are not in the rollups, but
// the event name index keeps this read narrow.
func (rep Reporter) dailyErrorSeries(ctx context.Context, siteID uuid.UUID, environment string, location *time.Location, from, to time.Time) ([]AnomalyPoint, error) {
	rows, err := rep.DB.Query(ctx, `SELECT (event_timestamp AT TIME ZONE $5)::date AS bucket_date,count(*)
		FROM raw_events
		WHERE site_id=$1 AND environment=$2 AND event_name = ANY($6) AND event_timestamp >= $3 AND event_timestamp < $4
		GROUP BY 1 ORDER BY 1`, siteID, environment, from, to, location.String(), []string{"error", "resource_error"})
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AnomalyPoint{}
	for rows.Next() {
		var day time.Time
		var count int64
		if err := rows.Scan(&day, &count); err != nil {
			return nil, err
		}
		out = append(out, AnomalyPoint{Date: time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC), Value: float64(count)})
	}
	return out, rows.Err()
}

// dailySeriesFromEvents is the fallback when the rollups have not caught up. It is
// correct but reads the event table, so it only runs when it has to.
func (rep Reporter) dailySeriesFromEvents(ctx context.Context, siteID uuid.UUID, environment string, location *time.Location, from, to time.Time) (map[string][]AnomalyPoint, error) {
	series := map[string][]AnomalyPoint{}
	zone := location.String()
	rows, err := rep.DB.Query(ctx, `SELECT (event_timestamp AT TIME ZONE $5)::date AS bucket_date,
			count(DISTINCT entity_id) users,
			count(DISTINCT session_id) sessions,
			count(*) events,
			count(*) FILTER(WHERE is_conversion) conversions,
			count(*) FILTER(WHERE event_name = ANY($6)) errors
		FROM analytics_events
		WHERE site_id=$1 AND environment=$2 AND event_timestamp >= $3 AND event_timestamp < $4
		GROUP BY 1 ORDER BY 1`, siteID, environment, from, to, zone, []string{"error", "resource_error"})
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var day time.Time
		var users, sessions, events, conversions, errorCount int64
		if err := rows.Scan(&day, &users, &sessions, &events, &conversions, &errorCount); err != nil {
			return nil, err
		}
		date := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
		series["users"] = append(series["users"], AnomalyPoint{Date: date, Value: float64(users)})
		series["sessions"] = append(series["sessions"], AnomalyPoint{Date: date, Value: float64(sessions)})
		series["events"] = append(series["events"], AnomalyPoint{Date: date, Value: float64(events)})
		series["conversions"] = append(series["conversions"], AnomalyPoint{Date: date, Value: float64(conversions)})
		series["errors"] = append(series["errors"], AnomalyPoint{Date: date, Value: float64(errorCount)})
	}
	return series, rows.Err()
}
