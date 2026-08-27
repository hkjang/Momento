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

// AttributionQuery is one attribution question. TouchSites decides whether a visit
// on a sibling service can earn credit for a conversion on this one.
type AttributionQuery struct {
	SiteID       uuid.UUID
	TouchSites   []uuid.UUID
	Environment  string
	From, To     time.Time
	LookbackDays int
	Model        string
	HalfLifeDays int
	Scope        string
}

// AttributionSite is the credit earned by visits that happened on one service.
type AttributionSite struct {
	SiteID              string  `json:"site_id"`
	Name                string  `json:"name"`
	CreditedConversions float64 `json:"credited_conversions"`
	CreditSharePercent  float64 `json:"credit_share_percent"`
	IsConversionSite    bool    `json:"is_conversion_site"`
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
	Scope            string               `json:"scope"`
	Sites            []AttributionSite    `json:"sites,omitempty"`
	CrossSiteCredit  float64              `json:"cross_site_credit"`
	Note             string               `json:"note"`
}

// attributionPathCTE resolves conversions and their candidate touch sessions, and
// numbers each touch so any model can be expressed as a weight over that numbering.
const attributionPathCTE = `WITH conv AS (
		SELECT event_id,entity_id,event_timestamp converted_at FROM analytics_events
		WHERE site_id=$1 AND environment=$2 AND event_timestamp >= $3 AND event_timestamp < $4 AND is_conversion
	), touch AS (
		SELECT CASE WHEN coalesce(i.user_id,s.user_id) IS NOT NULL THEN 'u:'||coalesce(i.user_id,s.user_id) ELSE 'v:'||s.visitor_id END entity_id,
			s.session_id,s.started_at,s.site_id,coalesce(s.source,'') source,coalesce(s.medium,'') medium
		FROM sessions s LEFT JOIN visitor_identities i ON i.site_id=s.site_id AND i.visitor_id=s.visitor_id
		WHERE s.site_id = ANY($6) AND s.environment=$2 AND s.started_at >= $3 - make_interval(days=>$5) AND s.started_at < $4
	), path AS (
		SELECT c.event_id,c.entity_id,t.source,t.medium,t.site_id touch_site,
			row_number() OVER (PARTITION BY c.event_id ORDER BY t.started_at,t.session_id) position,
			count(*) OVER (PARTITION BY c.event_id) touches,
			count(*) FILTER(WHERE t.source<>'' OR t.medium<>'') OVER (PARTITION BY c.event_id) non_direct_touches,
			CASE WHEN t.source<>'' OR t.medium<>'' THEN row_number() OVER (PARTITION BY c.event_id,(t.source<>'' OR t.medium<>'') ORDER BY t.started_at DESC,t.session_id) END non_direct_rank,
			extract(epoch FROM (c.converted_at-t.started_at))/86400 days_before
		FROM conv c JOIN touch t ON t.entity_id=c.entity_id AND t.started_at <= c.converted_at AND t.started_at >= c.converted_at - make_interval(days=>$5)
	)`

// Attribution credits conversions in the period to channel groups, and when the
// scope spans a workspace, also to the service the visit happened on.
//
// Cross-service credit only works for people the identity graph knows: an anonymous
// visitor is deliberately site scoped, so a visit on another service can only earn
// credit once the same SSO user is identified on both.
func (rep Reporter) Attribution(ctx context.Context, query AttributionQuery) (AttributionReport, error) {
	if query.LookbackDays < 1 || query.LookbackDays > 180 {
		query.LookbackDays = 30
	}
	if query.HalfLifeDays < 1 || query.HalfLifeDays > 90 {
		query.HalfLifeDays = defaultHalfLifeDays
	}
	if len(query.TouchSites) == 0 {
		query.TouchSites = []uuid.UUID{query.SiteID}
	}
	if query.Scope == "" {
		query.Scope = "site"
	}
	weight, ok := AttributionWeight(query.Model, query.HalfLifeDays)
	if !ok {
		return AttributionReport{}, fmt.Errorf("unsupported attribution model %q", query.Model)
	}
	report := AttributionReport{
		Model: query.Model, LookbackDays: query.LookbackDays, Environment: query.Environment,
		From: query.From, To: query.To, Scope: query.Scope,
		Channels: []AttributionChannel{},
		Note:     "Touchpoint는 세션 단위 유입 정보입니다. Lookback 안에 방문 기록이 없는 전환은 미배분으로 표시하고, 다중 터치 모델의 배분 전환은 소수로 나뉩니다.",
	}
	if query.Scope == "workspace" {
		report.Note += " 교차 서비스 배분은 SSO로 식별된 사용자에게만 적용됩니다. 익명 방문자는 서비스별로 격리됩니다."
	}
	for _, item := range AttributionModels() {
		if item.Key == query.Model {
			report.Label, report.Description, report.MultiTouch = item.Label, item.Description, item.MultiTouch
		}
	}
	if query.Model == "time_decay" {
		report.HalfLifeDays = query.HalfLifeDays
	}
	args := []any{query.SiteID, query.Environment, query.From, query.To, query.LookbackDays, query.TouchSites}

	// Three reads of the same period, and they used to run one after another: the
	// conversion total, the credited channels, and the path summary — the last two
	// each building the whole touchpoint path from scratch. The total was read
	// first only so a site with no conversions could skip the rest, which every
	// site that anyone opens this screen for pays for. They now run together and
	// the early return is decided from the results; an empty site answers no
	// slower than before, and a real one waits for the longest read instead of the
	// sum of all three.
	type creditRow struct {
		source, medium                              string
		touchSite                                   uuid.UUID
		creditedConversions                         float64
		creditedEvents, creditedUsers, touchedConvs int64
	}
	credited := map[string]*AttributionChannel{}
	sites := map[uuid.UUID]*AttributionSite{}
	var creditRows []creditRow
	var attributedConversions int64
	var averageTouches *float64
	err := RunParallel(ctx, 3,
		func(stepCtx context.Context) error {
			return rep.DB.QueryRow(stepCtx, `SELECT count(*) FROM analytics_events WHERE site_id=$1 AND environment=$2 AND event_timestamp >= $3 AND event_timestamp < $4 AND is_conversion`,
				query.SiteID, query.Environment, query.From, query.To).Scan(&report.TotalConversions)
		},
		func(stepCtx context.Context) error {
			rows, err := rep.DB.Query(stepCtx, attributionPathCTE+`
				SELECT source,medium,touch_site,sum(weight),count(DISTINCT event_id) FILTER(WHERE weight>0),count(DISTINCT entity_id) FILTER(WHERE weight>0),count(DISTINCT event_id)
				FROM (SELECT p.event_id,p.entity_id,p.source,p.medium,p.touch_site,coalesce(`+weight+`,0) weight FROM path p) weighted
				GROUP BY 1,2,3`, args...)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var row creditRow
				if rows.Scan(&row.source, &row.medium, &row.touchSite, &row.creditedConversions, &row.creditedEvents, &row.creditedUsers, &row.touchedConvs) != nil {
					continue
				}
				creditRows = append(creditRows, row)
			}
			return rows.Err()
		},
		func(stepCtx context.Context) error {
			return rep.DB.QueryRow(stepCtx, attributionPathCTE+`
				SELECT count(DISTINCT event_id),avg(touches) FROM (SELECT DISTINCT event_id,touches FROM path) distinct_paths`,
				args...).Scan(&attributedConversions, &averageTouches)
		})
	if err != nil {
		return report, err
	}
	if report.TotalConversions == 0 {
		return report, nil
	}
	for _, row := range creditRows {
		channel := ClassifyChannel(row.source, row.medium, false, false)
		target, ok := credited[channel]
		if !ok {
			target = &AttributionChannel{Channel: channel}
			credited[channel] = target
		}
		target.CreditedConversions += row.creditedConversions
		target.CreditedUsers += row.creditedUsers
		target.TouchedConversions += row.touchedConvs
		target.AssistedConversions += row.touchedConvs
		report.Attributed += row.creditedConversions
		site, ok := sites[row.touchSite]
		if !ok {
			site = &AttributionSite{IsConversionSite: row.touchSite == query.SiteID}
			sites[row.touchSite] = site
		}
		site.CreditedConversions += row.creditedConversions
		if row.touchSite != query.SiteID {
			report.CrossSiteCredit += row.creditedConversions
		}
	}
	if averageTouches != nil {
		report.AveragePathTouch = *averageTouches
	}
	report.Unattributed = float64(report.TotalConversions - attributedConversions)
	if report.Unattributed < 0 {
		report.Unattributed = 0
	}
	for _, entry := range credited {
		entry.CreditSharePercent = ratio(entry.CreditedConversions, report.Attributed) * 100
		entry.TouchSharePercent = percent(entry.TouchedConversions, attributedConversions)
		if float64(entry.TouchedConversions) > entry.CreditedConversions {
			entry.AssistOnlyConversion = entry.TouchedConversions - int64(entry.CreditedConversions)
		}
		report.Channels = append(report.Channels, *entry)
	}
	sort.Slice(report.Channels, func(i, j int) bool {
		if report.Channels[i].CreditedConversions == report.Channels[j].CreditedConversions {
			return report.Channels[i].Channel < report.Channels[j].Channel
		}
		return report.Channels[i].CreditedConversions > report.Channels[j].CreditedConversions
	})
	if query.Scope == "workspace" {
		names, err := rep.siteNames(ctx, query.TouchSites)
		if err != nil {
			return report, err
		}
		for id, site := range sites {
			site.SiteID = names[id].key
			site.Name = names[id].name
			site.CreditSharePercent = ratio(site.CreditedConversions, report.Attributed) * 100
			report.Sites = append(report.Sites, *site)
		}
		sort.Slice(report.Sites, func(i, j int) bool {
			if report.Sites[i].CreditedConversions == report.Sites[j].CreditedConversions {
				return report.Sites[i].Name < report.Sites[j].Name
			}
			return report.Sites[i].CreditedConversions > report.Sites[j].CreditedConversions
		})
	}
	return report, nil
}

type siteLabel struct{ key, name string }

// siteNames resolves the public site key and name for the credited services.
func (rep Reporter) siteNames(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]siteLabel, error) {
	out := map[uuid.UUID]siteLabel{}
	rows, err := rep.DB.Query(ctx, `SELECT id,site_key,name FROM sites WHERE id = ANY($1)`, ids)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var key, name string
		if rows.Scan(&id, &key, &name) == nil {
			out[id] = siteLabel{key: key, name: name}
		}
	}
	return out, rows.Err()
}
