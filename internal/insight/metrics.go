package insight

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
)

type MetricSet struct {
	Users, NewUsers, Sessions, PageViews, Events, Conversions int64
	ConversionUsers, ConversionSessions                       int64
	EngagementRate, AvgSessionDuration, UserConversionRate    float64
	SessionConversionRate, Revenue                            float64
}

// Metrics is the period's headline figures: who came, how much they did, how
// much of it converted, and what it was worth.
func (r Reporter) Metrics(ctx context.Context, siteID uuid.UUID, environment string, from, to time.Time) (MetricSet, error) {
	var m MetricSet
	err := r.DB.QueryRow(ctx, `WITH
		-- Only the columns the aggregates read. Selecting every column made the
		-- planner carry both jsonb blobs through a two million row materialisation.
		period AS (SELECT entity_id entity,event_name,is_conversion,properties FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3),
		-- New people are those whose first ever visit falls inside the period.
		-- That needs history, but only history before the period ends, and it is
		-- read from the daily visitor rollup rather than from every event the site
		-- has ever collected. insight.FirstSeenCTE says why, and holds the one
		-- definition this and the insight report both use.
		new_users AS (
			SELECT count(*) value FROM (`+FirstSeenCTE("$1", "$4", "$3")+`) firsts
			WHERE firsts.first_at >= $2
		)
		SELECT
			count(DISTINCT p.entity),
			(SELECT value FROM new_users),
			count(*) FILTER(WHERE p.event_name='page_view'),
			count(*),
			count(*) FILTER(WHERE p.is_conversion),
			count(DISTINCT p.entity) FILTER(WHERE p.is_conversion),
			coalesce(100.0*count(DISTINCT p.entity) FILTER(WHERE p.is_conversion)/nullif(count(DISTINCT p.entity),0),0),
			`+RevenueAmountSQL("p")+`
		FROM period p`, siteID, from, to, environment).
		Scan(&m.Users, &m.NewUsers, &m.PageViews, &m.Events, &m.Conversions, &m.ConversionUsers, &m.UserConversionRate, &m.Revenue)
	if err != nil {
		return m, err
	}
	// Everything about sessions comes from one place, so the overview and the
	// insight report cannot disagree about how long a session lasted.
	sessions, err := r.SessionMetrics(ctx, siteID, environment, from, to)
	if err != nil {
		return m, err
	}
	m.Sessions = sessions.Sessions
	m.ConversionSessions = sessions.Converting
	m.EngagementRate = sessions.EngagementRate()
	m.AvgSessionDuration = sessions.AverageSeconds
	m.SessionConversionRate = sessions.ConversionRate()
	return m, nil
}

// MetricMap is the wire form the console and the digest both read.
func MetricMap(m MetricSet) map[string]any {
	return map[string]any{"users": m.Users, "new_users": m.NewUsers, "sessions": m.Sessions, "page_views": m.PageViews, "events": m.Events, "engagement_rate": m.EngagementRate, "avg_session_duration": m.AvgSessionDuration, "conversions": m.Conversions, "conversion_users": m.ConversionUsers, "conversion_sessions": m.ConversionSessions, "conversion_rate": m.UserConversionRate, "user_conversion_rate": m.UserConversionRate, "session_conversion_rate": m.SessionConversionRate, "revenue": m.Revenue}
}

// PreviousRange is the period immediately before this one, of the same length.
// Whole local days are shifted by whole days so a week compares with a week
// rather than with 168 hours that start mid-morning after a clock change.
func PreviousRange(from, to time.Time, location *time.Location) (time.Time, time.Time) {
	localFrom, localTo := from.In(location), to.In(location)
	if localFrom.Hour() == 0 && localFrom.Minute() == 0 && localFrom.Second() == 0 && localFrom.Nanosecond() == 0 &&
		localTo.Hour() == 0 && localTo.Minute() == 0 && localTo.Second() == 0 && localTo.Nanosecond() == 0 {
		fromDate := time.Date(localFrom.Year(), localFrom.Month(), localFrom.Day(), 0, 0, 0, 0, time.UTC)
		toDate := time.Date(localTo.Year(), localTo.Month(), localTo.Day(), 0, 0, 0, 0, time.UTC)
		days := int(toDate.Sub(fromDate) / (24 * time.Hour))
		return localFrom.AddDate(0, 0, -days).UTC(), from
	}
	duration := to.Sub(from)
	return from.Add(-duration), from
}

// PlatformMetrics is what the insight ranking compares between two periods.
type PlatformMetrics struct {
	Users, Events, Conversions, Errors int64
	Revenue                            float64
}

// Platform reads the five figures the insight ranking compares across periods.
func (r Reporter) Platform(ctx context.Context, siteID uuid.UUID, environment string, from, to time.Time) (PlatformMetrics, error) {
	var value PlatformMetrics
	err := r.DB.QueryRow(ctx, `SELECT count(DISTINCT entity_id),count(*),count(*) FILTER(WHERE is_conversion),count(*) FILTER(WHERE event_name=ANY($5)),`+RevenueAmountSQL("")+`::double precision FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3`, siteID, from, to, environment, []string{"error", "resource_error"}).Scan(&value.Users, &value.Events, &value.Conversions, &value.Errors, &value.Revenue)
	return value, err
}

// Insight is one finding: a measure that moved enough to be worth saying, with
// the evidence and what to do about it.
type Insight struct {
	Metric         string  `json:"metric"`
	Title          string  `json:"title"`
	Severity       string  `json:"severity"`
	ChangePercent  float64 `json:"change_percent"`
	Current        float64 `json:"current"`
	Previous       float64 `json:"previous"`
	Evidence       string  `json:"evidence"`
	Recommendation string  `json:"recommendation"`
}

// RankInsights is the insights screen's own ranking: the measures that moved by
// more than a tenth, ordered by how far they moved, with a severity that knows
// which direction is the bad one.
//
// It lives here because the scheduled report named "인사이트 요약" delivered the
// period's absolute totals and no insights at all — the same five numbers the
// overview digest sends, under a name that promises the findings.
func RankInsights(current, previous PlatformMetrics) []Insight {
	type candidate struct {
		metric, title, recommendation string
		current, previous             float64
		badWhenHigher                 bool
	}
	candidates := []candidate{
		{"users", "활성 사용자 변화", "조직·부서별 Adoption을 확인해 변화가 큰 대상을 점검하세요.", float64(current.Users), float64(previous.Users), false},
		{"events", "이벤트 사용량 변화", "배포와 주요 기능별 이벤트 추세를 비교하세요.", float64(current.Events), float64(previous.Events), false},
		{"conversions", "전환 변화", "Business Journey에서 이탈이 커진 단계를 확인하세요.", float64(current.Conversions), float64(previous.Conversions), false},
		{"errors", "사용자 오류 변화", "Experience에서 오류 메시지·페이지·릴리즈 영향을 확인하세요.", float64(current.Errors), float64(previous.Errors), true},
		{"revenue", "매출 변화", "구매 퍼널과 유입 채널의 전환율을 함께 확인하세요.", current.Revenue, previous.Revenue, false},
	}
	insights := []Insight{}
	for _, item := range candidates {
		change := percentChange(item.current, item.previous)
		if math.Abs(change) < 10 && item.current != 0 {
			continue
		}
		severity := "info"
		bad := (item.badWhenHigher && change > 20) || (!item.badWhenHigher && change < -20)
		if bad {
			severity = "critical"
		} else if math.Abs(change) >= 20 {
			severity = "warning"
		}
		insights = append(insights, Insight{Metric: item.metric, Title: item.title, Severity: severity,
			ChangePercent: change, Current: item.current, Previous: item.previous,
			Evidence:       fmt.Sprintf("현재 %.2f, 이전 기간 %.2f", item.current, item.previous),
			Recommendation: item.recommendation})
	}
	sort.SliceStable(insights, func(i, j int) bool {
		return math.Abs(insights[i].ChangePercent) > math.Abs(insights[j].ChangePercent)
	})
	return insights
}

// MetricChange is each figure's movement against the previous period, in
// percent. A summary that states a number without it leaves the reader to
// remember what last week's was.
func MetricChange(current, previous MetricSet) map[string]any {
	now, before := MetricMap(current), MetricMap(previous)
	change := map[string]any{}
	for key, value := range now {
		change[key] = percentChange(toFloat(value), toFloat(before[key]))
	}
	return change
}

func toFloat(value any) float64 {
	switch number := value.(type) {
	case int64:
		return float64(number)
	case float64:
		return number
	default:
		return 0
	}
}
