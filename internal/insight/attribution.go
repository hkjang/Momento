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
// the model decides which of those two visits gets the credit.

// AttributionModel describes one crediting rule.
type AttributionModel struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// AttributionModels lists the supported models in the order they should be offered.
func AttributionModels() []AttributionModel {
	return []AttributionModel{
		{Key: "last_non_direct", Label: "마지막 비직접 터치", Description: "전환 직전 채널 정보가 있는 마지막 방문에 배분합니다. 직접 방문만 있으면 직접에 배분합니다."},
		{Key: "first_touch", Label: "최초 터치", Description: "조회 기간 이전 lookback 내 첫 방문 채널에 배분합니다. 신규 발견 기여를 봅니다."},
		{Key: "last_touch", Label: "마지막 터치", Description: "전환 직전 방문 채널에 그대로 배분합니다."},
	}
}

// attributionOrder maps a model to its touch ranking. The fragments are fixed
// strings chosen from this map, never built from request input.
var attributionOrder = map[string]string{
	"first_touch":     "t.started_at ASC,t.session_id",
	"last_touch":      "t.started_at DESC,t.session_id",
	"last_non_direct": "(t.source<>'' OR t.medium<>'') DESC,t.started_at DESC,t.session_id",
}

// AttributionOrder returns the ranking for a model and whether the model is known.
func AttributionOrder(model string) (string, bool) {
	order, ok := attributionOrder[model]
	return order, ok
}

// AttributionChannel is the credit given to one channel group.
type AttributionChannel struct {
	Channel              string  `json:"channel"`
	CreditedConversions  int64   `json:"credited_conversions"`
	CreditedUsers        int64   `json:"credited_users"`
	AssistedConversions  int64   `json:"assisted_conversions"`
	CreditSharePercent   float64 `json:"credit_share_percent"`
	AssistOnlyConversion int64   `json:"assist_only_conversions"`
}

// AttributionReport is the full response for one model.
type AttributionReport struct {
	Model            string               `json:"model"`
	Label            string               `json:"label"`
	Description      string               `json:"description"`
	LookbackDays     int                  `json:"lookback_days"`
	Environment      string               `json:"environment"`
	From             time.Time            `json:"from"`
	To               time.Time            `json:"to"`
	TotalConversions int64                `json:"total_conversions"`
	Attributed       int64                `json:"attributed_conversions"`
	Unattributed     int64                `json:"unattributed_conversions"`
	Channels         []AttributionChannel `json:"channels"`
	Note             string               `json:"note"`
}

// Attribution credits conversions in the period to channel groups.
func (rep Reporter) Attribution(ctx context.Context, siteID uuid.UUID, environment string, from, to time.Time, lookbackDays int, model string) (AttributionReport, error) {
	order, ok := AttributionOrder(model)
	if !ok {
		return AttributionReport{}, fmt.Errorf("unsupported attribution model %q", model)
	}
	if lookbackDays < 1 || lookbackDays > 180 {
		lookbackDays = 30
	}
	report := AttributionReport{
		Model: model, LookbackDays: lookbackDays, Environment: environment, From: from, To: to,
		Channels: []AttributionChannel{},
		Note:     "Touchpoint는 세션 단위 유입 정보입니다. Lookback 안에 방문 기록이 없는 전환은 미배분으로 표시합니다.",
	}
	for _, item := range AttributionModels() {
		if item.Key == model {
			report.Label, report.Description = item.Label, item.Description
		}
	}
	if err := rep.DB.QueryRow(ctx, `SELECT count(*) FROM analytics_events WHERE site_id=$1 AND environment=$2 AND event_timestamp >= $3 AND event_timestamp < $4 AND is_conversion`,
		siteID, environment, from, to).Scan(&report.TotalConversions); err != nil {
		return report, err
	}
	if report.TotalConversions == 0 {
		return report, nil
	}

	// touchSessions maps every session to the same entity identity the analytics
	// view uses, so a person's desktop and phone visits credit one path.
	const touchCTE = `WITH conv AS (
		SELECT event_id,entity_id,event_timestamp converted_at FROM analytics_events
		WHERE site_id=$1 AND environment=$2 AND event_timestamp >= $3 AND event_timestamp < $4 AND is_conversion
	), touch AS (
		SELECT CASE WHEN coalesce(i.user_id,s.user_id) IS NOT NULL THEN 'u:'||coalesce(i.user_id,s.user_id) ELSE 'v:'||s.visitor_id END entity_id,
			s.session_id,s.started_at,coalesce(s.source,'') source,coalesce(s.medium,'') medium
		FROM sessions s LEFT JOIN visitor_identities i ON i.site_id=s.site_id AND i.visitor_id=s.visitor_id
		WHERE s.site_id=$1 AND s.environment=$2 AND s.started_at >= $3 - make_interval(days=>$5) AND s.started_at < $4
	)`

	credited := map[string]*AttributionChannel{}
	channelOf := func(source, medium string) string { return ClassifyChannel(source, medium, false, false) }
	rows, err := rep.DB.Query(ctx, touchCTE+`
		SELECT source,medium,count(*),count(DISTINCT entity_id) FROM (
			SELECT DISTINCT ON (c.event_id) c.event_id,c.entity_id,t.source,t.medium
			FROM conv c JOIN touch t ON t.entity_id=c.entity_id AND t.started_at <= c.converted_at AND t.started_at >= c.converted_at - make_interval(days=>$5)
			ORDER BY c.event_id,`+order+`
		) chosen GROUP BY 1,2`, siteID, environment, from, to, lookbackDays)
	if err != nil {
		return report, err
	}
	for rows.Next() {
		var source, medium string
		var conversions, users int64
		if rows.Scan(&source, &medium, &conversions, &users) != nil {
			continue
		}
		channel := channelOf(source, medium)
		entry, ok := credited[channel]
		if !ok {
			entry = &AttributionChannel{Channel: channel}
			credited[channel] = entry
		}
		entry.CreditedConversions += conversions
		entry.CreditedUsers += users
		report.Attributed += conversions
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return report, err
	}

	// Assisted credit shows every channel that appeared on the path, which is what
	// makes the difference between models visible instead of implied.
	assistRows, err := rep.DB.Query(ctx, touchCTE+`
		SELECT t.source,t.medium,count(DISTINCT c.event_id)
		FROM conv c JOIN touch t ON t.entity_id=c.entity_id AND t.started_at <= c.converted_at AND t.started_at >= c.converted_at - make_interval(days=>$5)
		GROUP BY 1,2`, siteID, environment, from, to, lookbackDays)
	if err != nil {
		return report, err
	}
	for assistRows.Next() {
		var source, medium string
		var conversions int64
		if assistRows.Scan(&source, &medium, &conversions) != nil {
			continue
		}
		channel := channelOf(source, medium)
		entry, ok := credited[channel]
		if !ok {
			entry = &AttributionChannel{Channel: channel}
			credited[channel] = entry
		}
		entry.AssistedConversions += conversions
	}
	assistRows.Close()
	if err := assistRows.Err(); err != nil {
		return report, err
	}

	report.Unattributed = report.TotalConversions - report.Attributed
	if report.Unattributed < 0 {
		report.Unattributed = 0
	}
	for _, entry := range credited {
		entry.CreditSharePercent = percent(entry.CreditedConversions, report.Attributed)
		if entry.AssistedConversions > entry.CreditedConversions {
			entry.AssistOnlyConversion = entry.AssistedConversions - entry.CreditedConversions
		}
		report.Channels = append(report.Channels, *entry)
	}
	sort.Slice(report.Channels, func(i, j int) bool {
		if report.Channels[i].CreditedConversions == report.Channels[j].CreditedConversions {
			return report.Channels[i].Channel < report.Channels[j].Channel
		}
		return report.Channels[i].CreditedConversions > report.Channels[j].CreditedConversions
	})
	return report, nil
}
