package insight

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

// Attribution answers which channel deserves the credit for a conversion. The SDK
// captures acquisition per session, so a session is the touchpoint: a person can
// arrive from a notice on Monday and convert after a direct visit on Wednesday, and
// the model decides how much of that conversion each visit earns.
//
// Single touch models give one visit the whole conversion. Multi touch models split
// it, which is why credit is fractional: a conversion with three visits under the
// linear model contributes 0.333 to each channel rather than one to a winner.

// AttributionModel describes one crediting rule.
type AttributionModel struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	MultiTouch  bool   `json:"multi_touch"`
}

// AttributionModels lists the supported models in the order they should be offered.
func AttributionModels() []AttributionModel {
	return []AttributionModel{
		{Key: "last_non_direct", Label: "마지막 비직접 터치", Description: "전환 직전 채널 정보가 있는 마지막 방문에 전부 배분합니다. 직접 방문만 있으면 직접에 배분합니다."},
		{Key: "first_touch", Label: "최초 터치", Description: "Lookback 안의 첫 방문에 전부 배분합니다. 신규 발견 기여를 봅니다."},
		{Key: "last_touch", Label: "마지막 터치", Description: "전환 직전 방문에 전부 배분합니다."},
		{Key: "linear", Label: "선형 분배", Description: "경로의 모든 방문에 같은 비중으로 나눕니다. 전환까지의 모든 접점을 동등하게 인정합니다.", MultiTouch: true},
		{Key: "time_decay", Label: "시간 감쇠", Description: "전환에 가까운 방문에 더 많이 배분합니다. 기본 반감기는 7일입니다.", MultiTouch: true},
		{Key: "position_based", Label: "위치 기반", Description: "첫 방문과 마지막 방문에 각 40%, 중간 방문들이 나머지 20%를 나눕니다.", MultiTouch: true},
	}
}

// attributionWeights maps a model to the SQL weight it gives one touch of a path.
// The fragments are fixed strings chosen from this map, never built from request
// input, and every model's weights sum to exactly one per conversion.
var attributionWeights = map[string]string{
	"first_touch":     `CASE WHEN p.position=1 THEN 1.0 ELSE 0.0 END`,
	"last_touch":      `CASE WHEN p.position=p.touches THEN 1.0 ELSE 0.0 END`,
	"last_non_direct": `CASE WHEN p.non_direct_touches>0 THEN CASE WHEN p.non_direct_rank=1 THEN 1.0 ELSE 0.0 END WHEN p.position=p.touches THEN 1.0 ELSE 0.0 END`,
	"linear":          `1.0/p.touches`,
	"position_based":  `CASE WHEN p.touches=1 THEN 1.0 WHEN p.touches=2 THEN 0.5 WHEN p.position=1 OR p.position=p.touches THEN 0.4 ELSE 0.2/(p.touches-2) END`,
	// time_decay needs the path total to normalise, so it carries its own window sum.
	"time_decay": `pow(0.5,p.days_before/%[1]d.0)/nullif(sum(pow(0.5,p.days_before/%[1]d.0)) OVER (PARTITION BY p.event_id),0)`,
}

// defaultHalfLifeDays is the time decay half life used when none is given.
const defaultHalfLifeDays = 7

// AttributionWeight returns the weight expression for a model. The half life only
// applies to time decay and is clamped so it can never reach the query as free text.
func AttributionWeight(model string, halfLifeDays int) (string, bool) {
	expression, ok := attributionWeights[model]
	if !ok {
		return "", false
	}
	if model != "time_decay" {
		return expression, true
	}
	if halfLifeDays < 1 || halfLifeDays > 90 {
		halfLifeDays = defaultHalfLifeDays
	}
	return fmt.Sprintf(expression, halfLifeDays), true
}

// AttributionOrder reports whether a model is supported. It keeps the name used by
// the v0.11 handler so existing callers stay valid.
func AttributionOrder(model string) (string, bool) {
	expression, ok := AttributionWeight(model, defaultHalfLifeDays)
	return expression, ok
}

// AttributionChannel is the credit given to one channel group.
type AttributionChannel struct {
	Channel              string  `json:"channel"`
	CreditedConversions  float64 `json:"credited_conversions"`
	CreditedUsers        int64   `json:"credited_users"`
	TouchedConversions   int64   `json:"touched_conversions"`
	AssistedConversions  int64   `json:"assisted_conversions"`
	CreditSharePercent   float64 `json:"credit_share_percent"`
	AssistOnlyConversion int64   `json:"assist_only_conversions"`
	TouchSharePercent    float64 `json:"touch_share_percent"`
}

// AttributionReport is the full response for one model.
type AttributionReport struct {
	Model            string               `json:"model"`
	Label            string               `json:"label"`
	Description      string               `json:"description"`
	MultiTouch       bool                 `json:"multi_touch"`
	LookbackDays     int                  `json:"lookback_days"`
	HalfLifeDays     int                  `json:"half_life_days,omitempty"`
	Environment      string               `json:"environment"`
	From             time.Time            `json:"from"`
	To               time.Time            `json:"to"`
	TotalConversions int64                `json:"total_conversions"`
	Attributed       float64              `json:"attributed_conversions"`
	Unattributed     float64              `json:"unattributed_conversions"`
	AveragePathTouch float64              `json:"average_path_touches"`
	Channels         []AttributionChannel `json:"channels"`
	Note             string               `json:"note"`
}

// attributionPathCTE resolves conversions and their candidate touch sessions, and
// numbers each touch so any model can be expressed as a weight over that numbering.
const attributionPathCTE = `WITH conv AS (
		SELECT event_id,entity_id,event_timestamp converted_at FROM analytics_events
		WHERE site_id=$1 AND environment=$2 AND event_timestamp >= $3 AND event_timestamp < $4 AND is_conversion
	), touch AS (
		SELECT CASE WHEN coalesce(i.user_id,s.user_id) IS NOT NULL THEN 'u:'||coalesce(i.user_id,s.user_id) ELSE 'v:'||s.visitor_id END entity_id,
			s.session_id,s.started_at,coalesce(s.source,'') source,coalesce(s.medium,'') medium
		FROM sessions s LEFT JOIN visitor_identities i ON i.site_id=s.site_id AND i.visitor_id=s.visitor_id
		WHERE s.site_id=$1 AND s.environment=$2 AND s.started_at >= $3 - make_interval(days=>$5) AND s.started_at < $4
	), path AS (
		SELECT c.event_id,c.entity_id,t.source,t.medium,
			row_number() OVER (PARTITION BY c.event_id ORDER BY t.started_at,t.session_id) position,
			count(*) OVER (PARTITION BY c.event_id) touches,
			count(*) FILTER(WHERE t.source<>'' OR t.medium<>'') OVER (PARTITION BY c.event_id) non_direct_touches,
			CASE WHEN t.source<>'' OR t.medium<>'' THEN row_number() OVER (PARTITION BY c.event_id,(t.source<>'' OR t.medium<>'') ORDER BY t.started_at DESC,t.session_id) END non_direct_rank,
			extract(epoch FROM (c.converted_at-t.started_at))/86400 days_before
		FROM conv c JOIN touch t ON t.entity_id=c.entity_id AND t.started_at <= c.converted_at AND t.started_at >= c.converted_at - make_interval(days=>$5)
	)`

// Attribution credits conversions in the period to channel groups.
func (rep Reporter) Attribution(ctx context.Context, siteID uuid.UUID, environment string, from, to time.Time, lookbackDays int, model string, halfLifeDays int) (AttributionReport, error) {
	if lookbackDays < 1 || lookbackDays > 180 {
		lookbackDays = 30
	}
	if halfLifeDays < 1 || halfLifeDays > 90 {
		halfLifeDays = defaultHalfLifeDays
	}
	weight, ok := AttributionWeight(model, halfLifeDays)
	if !ok {
		return AttributionReport{}, fmt.Errorf("unsupported attribution model %q", model)
	}
	report := AttributionReport{
		Model: model, LookbackDays: lookbackDays, Environment: environment, From: from, To: to,
		Channels: []AttributionChannel{},
		Note:     "Touchpoint는 세션 단위 유입 정보입니다. Lookback 안에 방문 기록이 없는 전환은 미배분으로 표시하고, 다중 터치 모델의 배분 전환은 소수로 나뉩니다.",
	}
	for _, item := range AttributionModels() {
		if item.Key == model {
			report.Label, report.Description, report.MultiTouch = item.Label, item.Description, item.MultiTouch
		}
	}
	if model == "time_decay" {
		report.HalfLifeDays = halfLifeDays
	}
	if err := rep.DB.QueryRow(ctx, `SELECT count(*) FROM analytics_events WHERE site_id=$1 AND environment=$2 AND event_timestamp >= $3 AND event_timestamp < $4 AND is_conversion`,
		siteID, environment, from, to).Scan(&report.TotalConversions); err != nil {
		return report, err
	}
	if report.TotalConversions == 0 {
		return report, nil
	}

	credited := map[string]*AttributionChannel{}
	entry := func(channel string) *AttributionChannel {
		if existing, ok := credited[channel]; ok {
			return existing
		}
		created := &AttributionChannel{Channel: channel}
		credited[channel] = created
		return created
	}
	rows, err := rep.DB.Query(ctx, attributionPathCTE+`
		SELECT source,medium,sum(weight),count(DISTINCT event_id) FILTER(WHERE weight>0),count(DISTINCT entity_id) FILTER(WHERE weight>0),count(DISTINCT event_id)
		FROM (SELECT p.event_id,p.entity_id,p.source,p.medium,coalesce(`+weight+`,0) weight FROM path p) weighted
		GROUP BY 1,2`, siteID, environment, from, to, lookbackDays)
	if err != nil {
		return report, err
	}
	for rows.Next() {
		var source, medium string
		var creditedConversions float64
		var creditedEvents, creditedUsers, touchedConversions int64
		if rows.Scan(&source, &medium, &creditedConversions, &creditedEvents, &creditedUsers, &touchedConversions) != nil {
			continue
		}
		target := entry(ClassifyChannel(source, medium, false, false))
		target.CreditedConversions += creditedConversions
		target.CreditedUsers += creditedUsers
		target.TouchedConversions += touchedConversions
		target.AssistedConversions += touchedConversions
		report.Attributed += creditedConversions
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return report, err
	}

	var attributedConversions int64
	var averageTouches *float64
	if err := rep.DB.QueryRow(ctx, attributionPathCTE+`
		SELECT count(DISTINCT event_id),avg(touches) FROM (SELECT DISTINCT event_id,touches FROM path) distinct_paths`,
		siteID, environment, from, to, lookbackDays).Scan(&attributedConversions, &averageTouches); err != nil {
		return report, err
	}
	if averageTouches != nil {
		report.AveragePathTouch = *averageTouches
	}
	report.Unattributed = float64(report.TotalConversions - attributedConversions)
	if report.Unattributed < 0 {
		report.Unattributed = 0
	}
	for _, item := range credited {
		item.CreditSharePercent = ratio(item.CreditedConversions, report.Attributed) * 100
		item.TouchSharePercent = percent(item.TouchedConversions, attributedConversions)
		if float64(item.TouchedConversions) > item.CreditedConversions {
			item.AssistOnlyConversion = item.TouchedConversions - int64(item.CreditedConversions)
		}
		report.Channels = append(report.Channels, *item)
	}
	sort.Slice(report.Channels, func(i, j int) bool {
		if report.Channels[i].CreditedConversions == report.Channels[j].CreditedConversions {
			return report.Channels[i].Channel < report.Channels[j].Channel
		}
		return report.Channels[i].CreditedConversions > report.Channels[j].CreditedConversions
	})
	return report, nil
}
