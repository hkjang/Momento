package httpapi

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/auth"
)

func (s *Server) workspaceForSite(r *http.Request) (uuid.UUID, uuid.UUID, error) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	var workspaceID uuid.UUID
	err = s.DB.QueryRow(r.Context(), `SELECT workspace_id FROM sites WHERE id=$1`, siteID).Scan(&workspaceID)
	return siteID, workspaceID, err
}

func (s *Server) workspaceRollup(w http.ResponseWriter, r *http.Request) {
	siteID, workspaceID, err := s.workspaceForSite(r)
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	environment := requestEnvironment(r)
	rows, err := s.DB.Query(r.Context(), `WITH base AS (
		SELECT e.*,CASE WHEN e.canonical_user_id IS NOT NULL THEN 'u:'||e.canonical_user_id ELSE 's:'||e.site_id::text||':v:'||e.visitor_id END workspace_entity
		FROM analytics_events e JOIN sites s ON s.id=e.site_id WHERE s.workspace_id=$1 AND e.environment=$4 AND e.event_timestamp >= $2 AND e.event_timestamp < $3
	), repeat_users AS (SELECT site_id,workspace_entity FROM base GROUP BY site_id,workspace_entity HAVING count(*)>=2), site_stats AS (
		SELECT s.id,s.site_key,s.name,s.service_name,count(b.event_id) events,count(DISTINCT b.workspace_entity) users,count(DISTINCT b.session_id) sessions,
		count(DISTINCT b.workspace_entity) FILTER(WHERE b.is_conversion) conversion_users,count(*) FILTER(WHERE b.event_name=ANY($5)) errors,
		(SELECT count(*) FROM repeat_users r WHERE r.site_id=s.id) repeat_users
		FROM sites s LEFT JOIN base b ON b.site_id=s.id WHERE s.workspace_id=$1 AND s.active GROUP BY s.id
	) SELECT *,sum(events) OVER() total_events,count(*) OVER() site_count FROM site_stats ORDER BY users DESC`, workspaceID, from, to, environment, []string{"error", "resource_error"})
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	services := []map[string]any{}
	var totalEvents, totalUsers, totalSessions, siteCount int64
	for rows.Next() {
		var id uuid.UUID
		var key, name, serviceName string
		var events, users, sessions, conversions, errorCount, repeatUsers, windowEvents, windowSites int64
		if rows.Scan(&id, &key, &name, &serviceName, &events, &users, &sessions, &conversions, &errorCount, &repeatUsers, &windowEvents, &windowSites) != nil {
			continue
		}
		repeatRate, conversionRate, errorRate := percent(repeatUsers, users), percent(conversions, users), percent(errorCount, events)
		score := clampScore(.45*repeatRate + .25*math.Min(100, conversionRate*5) + .30*(100-errorRate))
		services = append(services, map[string]any{"site_uuid": id, "site_id": key, "site_name": name, "service": serviceName, "events": events, "users": users, "sessions": sessions, "conversion_users": conversions, "repeat_users": repeatUsers, "repeat_rate": repeatRate, "conversion_rate": conversionRate, "error_rate": errorRate, "service_score": score})
		totalEvents, siteCount = windowEvents, windowSites
		totalUsers += users
		totalSessions += sessions
	}
	var uniqueUsers int64
	_ = s.DB.QueryRow(r.Context(), `SELECT count(DISTINCT CASE WHEN e.canonical_user_id IS NOT NULL THEN 'u:'||e.canonical_user_id ELSE 's:'||e.site_id::text||':v:'||e.visitor_id END)
		FROM analytics_events e JOIN sites s ON s.id=e.site_id WHERE s.workspace_id=$1 AND e.environment=$4 AND e.event_timestamp >= $2 AND e.event_timestamp < $3`, workspaceID, from, to, environment).Scan(&uniqueUsers)
	writeJSON(w, 200, map[string]any{"workspace_id": workspaceID, "environment": environment, "summary": map[string]any{"registered_services": siteCount, "users": uniqueUsers, "site_user_sum": totalUsers, "sessions": totalSessions, "events": totalEvents}, "services": services})
}

func percent(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}

func clampScore(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return math.Round(value*10) / 10
}

func (s *Server) listWorkspaceJourneys(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, err := s.workspaceForSite(r)
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	p, _ := auth.FromContext(r.Context())
	rows, err := s.DB.Query(r.Context(), `SELECT id,name,description,steps,conversion_window_days,shared,active,owner_id,created_at,updated_at FROM workspace_journeys WHERE workspace_id=$1 AND (shared OR owner_id=$2 OR $3 IN ('super_admin','organization_admin','workspace_admin')) ORDER BY updated_at DESC`, workspaceID, p.ID, p.Role)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var name, description string
		var raw []byte
		var days int
		var shared, active bool
		var owner *uuid.UUID
		var created, updated time.Time
		if rows.Scan(&id, &name, &description, &raw, &days, &shared, &active, &owner, &created, &updated) == nil {
			var steps any
			_ = json.Unmarshal(raw, &steps)
			out = append(out, map[string]any{"id": id, "name": name, "description": description, "steps": steps, "conversion_window_days": days, "shared": shared, "active": active, "owner_id": owner, "created_at": created, "updated_at": updated})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) saveWorkspaceJourney(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, err := s.workspaceForSite(r)
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	p, _ := auth.FromContext(r.Context())
	var in struct {
		ID                   string        `json:"id"`
		Name                 string        `json:"name"`
		Description          string        `json:"description"`
		Steps                []journeyStep `json:"steps"`
		ConversionWindowDays int           `json:"conversion_window_days"`
		Shared               bool          `json:"shared"`
		Active               *bool         `json:"active"`
	}
	if err := decodeJSON(r, &in, 128<<10); err != nil || strings.TrimSpace(in.Name) == "" {
		writeError(w, 400, "INVALID_PAYLOAD", "name and steps are required")
		return
	}
	if err := validateJourneySteps(in.Steps); err != nil {
		writeError(w, 400, "INVALID_JOURNEY", err.Error())
		return
	}
	if in.ConversionWindowDays == 0 {
		in.ConversionWindowDays = 30
	}
	if in.ConversionWindowDays < 1 || in.ConversionWindowDays > 365 {
		writeError(w, 400, "INVALID_WINDOW", "conversion window must be 1 to 365 days")
		return
	}
	active := true
	if in.Active != nil {
		active = *in.Active
	}
	raw, _ := json.Marshal(in.Steps)
	var id uuid.UUID
	if in.ID == "" {
		err = s.DB.QueryRow(r.Context(), `INSERT INTO workspace_journeys(workspace_id,owner_id,name,description,steps,conversion_window_days,shared,active) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, workspaceID, p.ID, in.Name, in.Description, raw, in.ConversionWindowDays, in.Shared, active).Scan(&id)
	} else {
		id, err = uuid.Parse(in.ID)
		if err == nil {
			var tag interface{ RowsAffected() int64 }
			tag, err = s.DB.Exec(r.Context(), `UPDATE workspace_journeys SET name=$3,description=$4,steps=$5,conversion_window_days=$6,shared=$7,active=$8,updated_at=now() WHERE id=$1 AND workspace_id=$2 AND (owner_id=$9 OR $10 IN ('super_admin','organization_admin','workspace_admin'))`, id, workspaceID, in.Name, in.Description, raw, in.ConversionWindowDays, in.Shared, active, p.ID, p.Role)
			if err == nil && tag.RowsAffected() == 0 {
				err = fmt.Errorf("journey not found")
			}
		}
	}
	if err != nil {
		writeError(w, 500, "JOURNEY_SAVE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "workspace_journey.save", "workspace_journey", id.String(), map[string]any{"name": in.Name}, clientIP(r))
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) analyzeWorkspaceJourney(w http.ResponseWriter, r *http.Request) {
	siteID, workspaceID, err := s.workspaceForSite(r)
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	var in struct {
		JourneyID            string        `json:"journey_id"`
		Steps                []journeyStep `json:"steps"`
		ConversionWindowDays int           `json:"conversion_window_days"`
	}
	if err := decodeJSON(r, &in, 128<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if in.JourneyID != "" {
		id, parseErr := uuid.Parse(in.JourneyID)
		var raw []byte
		if parseErr != nil || s.DB.QueryRow(r.Context(), `SELECT steps,conversion_window_days FROM workspace_journeys WHERE id=$1 AND workspace_id=$2 AND active`, id, workspaceID).Scan(&raw, &in.ConversionWindowDays) != nil {
			writeError(w, 404, "JOURNEY_NOT_FOUND", "workspace journey not found")
			return
		}
		_ = json.Unmarshal(raw, &in.Steps)
	}
	if err := validateJourneySteps(in.Steps); err != nil {
		writeError(w, 400, "INVALID_JOURNEY", err.Error())
		return
	}
	if in.ConversionWindowDays < 1 || in.ConversionWindowDays > 365 {
		in.ConversionWindowDays = 30
	}
	args := []any{workspaceID, from, to, requestEnvironment(r), in.ConversionWindowDays}
	ctes := []string{}
	entity := `CASE WHEN e.canonical_user_id IS NOT NULL THEN 'u:'||e.canonical_user_id ELSE 's:'||e.site_id::text||':v:'||e.visitor_id END`
	for index, step := range in.Steps {
		args = append(args, step.Event, step.SiteID, step.Service, step.Feature)
		eventArg, siteArg, serviceArg, featureArg := len(args)-3, len(args)-2, len(args)-1, len(args)
		condition := fmt.Sprintf("e.event_name=$%d AND ($%d='' OR site.site_key=$%d) AND ($%d='' OR e.properties->>'service'=$%d) AND ($%d='' OR e.properties->>'feature'=$%d)", eventArg, siteArg, siteArg, serviceArg, serviceArg, featureArg, featureArg)
		name := fmt.Sprintf("step%d", index+1)
		if index == 0 {
			ctes = append(ctes, fmt.Sprintf("%s AS (SELECT %s entity_id,min(e.event_timestamp) reached_at FROM analytics_events e JOIN sites site ON site.id=e.site_id WHERE site.workspace_id=$1 AND e.environment=$4 AND e.event_timestamp >= $2 AND e.event_timestamp < $3 AND %s GROUP BY 1)", name, entity, condition))
		} else {
			previous := fmt.Sprintf("step%d", index)
			ctes = append(ctes, fmt.Sprintf("%s AS (SELECT p.entity_id,min(e.event_timestamp) reached_at FROM %s p JOIN analytics_events e ON %s=p.entity_id AND e.environment=$4 AND e.event_timestamp>p.reached_at AND e.event_timestamp<=p.reached_at+make_interval(days=>$5) JOIN sites site ON site.id=e.site_id AND site.workspace_id=$1 WHERE e.event_timestamp<$3 AND %s GROUP BY p.entity_id)", name, previous, entity, condition))
		}
	}
	selects := []string{}
	for index := range in.Steps {
		name := fmt.Sprintf("step%d", index+1)
		elapsed := "0::double precision"
		if index > 0 {
			elapsed = fmt.Sprintf("coalesce((SELECT avg(extract(epoch FROM(s.reached_at-f.reached_at))) FROM %s s JOIN step1 f USING(entity_id)),0)", name)
		}
		selects = append(selects, fmt.Sprintf("SELECT %d,(SELECT count(*) FROM %s),%s", index+1, name, elapsed))
	}
	rows, err := s.DB.Query(r.Context(), "WITH "+strings.Join(ctes, ",")+" "+strings.Join(selects, " UNION ALL ")+" ORDER BY 1", args...)
	if err != nil {
		writeError(w, 500, "JOURNEY_QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	var first int64
	for rows.Next() {
		var step int
		var users int64
		var elapsed float64
		if rows.Scan(&step, &users, &elapsed) == nil {
			if step == 1 {
				first = users
			}
			out = append(out, map[string]any{"step": step, "name": in.Steps[step-1].Name, "event": in.Steps[step-1].Event, "site_id": in.Steps[step-1].SiteID, "users": users, "conversion_rate": percent(users, first), "average_elapsed_seconds": elapsed})
		}
	}
	writeJSON(w, 200, map[string]any{"workspace_id": workspaceID, "environment": requestEnvironment(r), "steps": out, "identity_policy": "SSO user_id across sites; anonymous visitor is site-scoped"})
}

func (s *Server) featureIntelligence(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	environment := requestEnvironment(r)
	var population int64
	_ = s.DB.QueryRow(r.Context(), `SELECT count(DISTINCT entity_id) FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3`, siteID, from, to, environment).Scan(&population)
	midpoint := from.Add(to.Sub(from) / 2)
	rows, err := s.DB.Query(r.Context(), `WITH user_feature AS (
		SELECT properties->>'feature' feature,entity_id,count(*) events,bool_or(is_conversion) converted,bool_or(event_name=ANY($6)) errored,min(event_timestamp) first_seen,max(event_timestamp) last_seen
		FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND coalesce(properties->>'feature','')<>'' GROUP BY 1,2
	), trend AS (
		SELECT properties->>'feature' feature,count(*) FILTER(WHERE event_timestamp<$5) previous_events,count(*) FILTER(WHERE event_timestamp>=$5) current_events
		FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND coalesce(properties->>'feature','')<>'' GROUP BY 1
	) SELECT u.feature,count(*) users,sum(u.events) events,count(*) FILTER(WHERE u.events>=2) repeat_users,count(*) FILTER(WHERE u.converted) converted_users,count(*) FILTER(WHERE u.errored) error_users,min(u.first_seen),max(u.last_seen),t.previous_events,t.current_events,coalesce(a.eligible_users,$7)
	FROM user_feature u JOIN trend t USING(feature) LEFT JOIN adoption_targets a ON a.site_id=$1 AND a.organization='' AND a.department='' AND a.feature=u.feature GROUP BY u.feature,t.previous_events,t.current_events,a.eligible_users ORDER BY users DESC`, siteID, from, to, environment, midpoint, []string{"error", "resource_error"}, population)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var feature string
		var users, events, repeats, converted, errors, previous, current, eligible int64
		var first, last time.Time
		if rows.Scan(&feature, &users, &events, &repeats, &converted, &errors, &first, &last, &previous, &current, &eligible) != nil {
			continue
		}
		adoption, repeatRate, conversionRate, errorRate := percent(users, eligible), percent(repeats, users), percent(converted, users), percent(errors, users)
		trend := float64(0)
		if previous > 0 {
			trend = float64(current-previous) * 100 / float64(previous)
		}
		score := clampScore(.4*math.Min(100, adoption) + .3*repeatRate + .2*conversionRate + .1*(100-errorRate))
		dead := users < 10 && trend < 0 && repeatRate < 10
		out = append(out, map[string]any{"feature": feature, "users": users, "eligible_users": eligible, "events": events, "adoption_rate": adoption, "repeat_rate": repeatRate, "conversion_rate": conversionRate, "error_rate": errorRate, "trend_percent": trend, "feature_score": score, "dead_feature": dead, "first_seen": first, "last_seen": last})
	}
	writeJSON(w, 200, map[string]any{"population": population, "environment": environment, "features": out})
}

func (s *Server) searchAnalytics(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	environment := requestEnvironment(r)
	var searches, users, noResults, clicks, refinements, exits, successes int64
	_ = s.DB.QueryRow(r.Context(), `SELECT count(*) FILTER(WHERE event_name='search'),count(DISTINCT entity_id) FILTER(WHERE event_name='search'),count(*) FILTER(WHERE event_name='search_no_result' OR (event_name='search' AND properties->>'result_count'='0')),count(*) FILTER(WHERE event_name='search_click'),count(*) FILTER(WHERE event_name='search_refine'),count(*) FILTER(WHERE event_name='search_exit'),count(*) FILTER(WHERE event_name='search_success')
		FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3`, siteID, from, to, environment).Scan(&searches, &users, &noResults, &clicks, &refinements, &exits, &successes)
	rows, err := s.DB.Query(r.Context(), `SELECT coalesce(properties->>'query',properties->>'search_term','(not set)') query,count(*) FILTER(WHERE event_name='search') searches,count(DISTINCT entity_id) users,count(*) FILTER(WHERE event_name='search_no_result' OR (event_name='search' AND properties->>'result_count'='0')) no_results,count(*) FILTER(WHERE event_name='search_click') clicks,max(event_timestamp) last_seen
		FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND event_name=ANY($5) GROUP BY 1 ORDER BY searches DESC LIMIT 200`, siteID, from, to, environment, []string{"search", "search_result", "search_click", "search_no_result", "search_refine", "search_exit", "search_success"})
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	queries := []map[string]any{}
	for rows.Next() {
		var query string
		var count, queryUsers, zero, queryClicks int64
		var last time.Time
		if rows.Scan(&query, &count, &queryUsers, &zero, &queryClicks, &last) == nil {
			queries = append(queries, map[string]any{"query": query, "searches": count, "users": queryUsers, "zero_results": zero, "clicks": queryClicks, "ctr": math.Min(100, percent(queryClicks, count)), "last_seen": last})
		}
	}
	writeJSON(w, 200, map[string]any{"environment": environment, "summary": map[string]any{"searches": searches, "users": users, "zero_results": noResults, "zero_result_rate": math.Min(100, percent(noResults, searches)), "clicks": clicks, "search_ctr": math.Min(100, percent(clicks, searches)), "refinements": refinements, "exits": exits, "successes": successes, "success_rate": math.Min(100, percent(successes, searches))}, "queries": queries})
}

func (s *Server) frustrationAnalytics(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	environment := requestEnvironment(r)
	weights := map[string]int{"rage_click": 20, "dead_click": 15, "rapid_back": 10, "form_retry": 10, "repeated_search": 10, "error_after_click": 25, "slow_interaction": 10, "error": 20, "resource_error": 20}
	events := make([]string, 0, len(weights))
	for event := range weights {
		events = append(events, event)
	}
	rows, err := s.DB.Query(r.Context(), `SELECT event_name,count(*),count(DISTINCT entity_id),count(DISTINCT session_id),max(event_timestamp) FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND event_name=ANY($5) GROUP BY event_name ORDER BY count(*) DESC`, siteID, from, to, environment, events)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	signals := []map[string]any{}
	var weightedCount int64
	for rows.Next() {
		var event string
		var count, users, sessions int64
		var last time.Time
		if rows.Scan(&event, &count, &users, &sessions, &last) == nil {
			weightedCount += count * int64(weights[event])
			signals = append(signals, map[string]any{"signal": event, "count": count, "users": users, "sessions": sessions, "weight": weights[event], "last_seen": last})
		}
	}
	var totalSessions, affectedSessions int64
	_ = s.DB.QueryRow(r.Context(), `SELECT count(DISTINCT session_id),count(DISTINCT session_id) FILTER(WHERE event_name=ANY($5)) FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3`, siteID, from, to, environment, events).Scan(&totalSessions, &affectedSessions)
	averageScore := float64(0)
	if affectedSessions > 0 {
		averageScore = float64(weightedCount) / float64(affectedSessions)
	}
	writeJSON(w, 200, map[string]any{"environment": environment, "summary": map[string]any{"total_sessions": totalSessions, "affected_sessions": affectedSessions, "affected_session_rate": percent(affectedSessions, totalSessions), "average_frustration_score": averageScore}, "signals": signals})
}

func (s *Server) listFeatureFlags(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT id,flag_key,name,description,variants,status,starts_at,ends_at,owner,created_at,updated_at FROM feature_flags WHERE site_id=$1 ORDER BY updated_at DESC`, siteID)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var key, name, description, status, owner string
		var raw []byte
		var starts, ends *time.Time
		var created, updated time.Time
		if rows.Scan(&id, &key, &name, &description, &raw, &status, &starts, &ends, &owner, &created, &updated) == nil {
			var variants any
			_ = json.Unmarshal(raw, &variants)
			out = append(out, map[string]any{"id": id, "flag_key": key, "name": name, "description": description, "variants": variants, "status": status, "starts_at": starts, "ends_at": ends, "owner": owner, "created_at": created, "updated_at": updated})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) saveFeatureFlag(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	var in struct {
		FlagKey     string     `json:"flag_key"`
		Name        string     `json:"name"`
		Description string     `json:"description"`
		Variants    []string   `json:"variants"`
		Status      string     `json:"status"`
		StartsAt    *time.Time `json:"starts_at"`
		EndsAt      *time.Time `json:"ends_at"`
		Owner       string     `json:"owner"`
	}
	if err := decodeJSON(r, &in, 64<<10); err != nil || strings.TrimSpace(in.FlagKey) == "" || strings.TrimSpace(in.Name) == "" || len(in.Variants) < 2 || len(in.Variants) > 20 {
		writeError(w, 400, "INVALID_FLAG", "flag_key, name and 2 to 20 variants are required")
		return
	}
	if !map[string]bool{"draft": true, "active": true, "paused": true, "archived": true}[in.Status] {
		in.Status = "active"
	}
	raw, _ := json.Marshal(in.Variants)
	var id uuid.UUID
	err = s.DB.QueryRow(r.Context(), `INSERT INTO feature_flags(site_id,flag_key,name,description,variants,status,starts_at,ends_at,owner,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT(site_id,flag_key) DO UPDATE SET name=excluded.name,description=excluded.description,variants=excluded.variants,status=excluded.status,starts_at=excluded.starts_at,ends_at=excluded.ends_at,owner=excluded.owner,updated_at=now() RETURNING id`, siteID, in.FlagKey, in.Name, in.Description, raw, in.Status, in.StartsAt, in.EndsAt, in.Owner, p.ID).Scan(&id)
	if err != nil {
		writeError(w, 500, "FLAG_SAVE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "feature_flag.save", "feature_flag", id.String(), map[string]any{"flag_key": in.FlagKey}, clientIP(r))
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) listExperiments(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT id,feature_flag_id,experiment_key,name,hypothesis,primary_metric,variants,audience,environment,status,starts_at,ends_at,owner,created_at,updated_at FROM experiments WHERE site_id=$1 ORDER BY updated_at DESC`, siteID)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var flagID *uuid.UUID
		var key, name, hypothesis, metric, environment, status, owner string
		var variantsRaw, audienceRaw []byte
		var starts, ends *time.Time
		var created, updated time.Time
		if rows.Scan(&id, &flagID, &key, &name, &hypothesis, &metric, &variantsRaw, &audienceRaw, &environment, &status, &starts, &ends, &owner, &created, &updated) == nil {
			var variants, audience any
			_ = json.Unmarshal(variantsRaw, &variants)
			_ = json.Unmarshal(audienceRaw, &audience)
			out = append(out, map[string]any{"id": id, "feature_flag_id": flagID, "experiment_key": key, "name": name, "hypothesis": hypothesis, "primary_metric": metric, "variants": variants, "audience": audience, "environment": environment, "status": status, "starts_at": starts, "ends_at": ends, "owner": owner, "created_at": created, "updated_at": updated})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) saveExperiment(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	var in struct {
		FeatureFlagID string         `json:"feature_flag_id"`
		ExperimentKey string         `json:"experiment_key"`
		Name          string         `json:"name"`
		Hypothesis    string         `json:"hypothesis"`
		PrimaryMetric string         `json:"primary_metric"`
		Variants      []string       `json:"variants"`
		Audience      map[string]any `json:"audience"`
		Environment   string         `json:"environment"`
		Status        string         `json:"status"`
		StartsAt      *time.Time     `json:"starts_at"`
		EndsAt        *time.Time     `json:"ends_at"`
		Owner         string         `json:"owner"`
	}
	if err := decodeJSON(r, &in, 128<<10); err != nil || strings.TrimSpace(in.ExperimentKey) == "" || strings.TrimSpace(in.Name) == "" || !registryNamePattern.MatchString(in.PrimaryMetric) || len(in.Variants) < 2 || len(in.Variants) > 20 {
		writeError(w, 400, "INVALID_EXPERIMENT", "key, name, semantic metric and 2 to 20 variants are required")
		return
	}
	if !environmentNamePattern.MatchString(in.Environment) {
		in.Environment = "prd"
	}
	if !map[string]bool{"draft": true, "running": true, "completed": true, "archived": true}[in.Status] {
		in.Status = "draft"
	}
	var flagID any
	if in.FeatureFlagID != "" {
		parsed, parseErr := uuid.Parse(in.FeatureFlagID)
		if parseErr != nil {
			writeError(w, 400, "INVALID_FLAG", "invalid feature flag id")
			return
		}
		flagID = parsed
	}
	variants, _ := json.Marshal(in.Variants)
	audience, _ := json.Marshal(in.Audience)
	var id uuid.UUID
	err = s.DB.QueryRow(r.Context(), `INSERT INTO experiments(site_id,feature_flag_id,experiment_key,name,hypothesis,primary_metric,variants,audience,environment,status,starts_at,ends_at,owner,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT(site_id,experiment_key) DO UPDATE SET feature_flag_id=excluded.feature_flag_id,name=excluded.name,hypothesis=excluded.hypothesis,primary_metric=excluded.primary_metric,variants=excluded.variants,audience=excluded.audience,environment=excluded.environment,status=excluded.status,starts_at=excluded.starts_at,ends_at=excluded.ends_at,owner=excluded.owner,updated_at=now() RETURNING id`, siteID, flagID, in.ExperimentKey, in.Name, in.Hypothesis, in.PrimaryMetric, variants, audience, in.Environment, in.Status, in.StartsAt, in.EndsAt, in.Owner, p.ID).Scan(&id)
	if err != nil {
		writeError(w, 500, "EXPERIMENT_SAVE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "experiment.save", "experiment", id.String(), map[string]any{"experiment_key": in.ExperimentKey}, clientIP(r))
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) analyzeExperiment(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "INVALID_ID", "invalid experiment id")
		return
	}
	var key, metric, environment string
	var variantsRaw, definitionRaw []byte
	var starts, ends *time.Time
	if err := s.DB.QueryRow(r.Context(), `SELECT e.experiment_key,e.primary_metric,e.environment,e.variants,e.starts_at,e.ends_at,m.definition FROM experiments e JOIN semantic_metrics m ON m.site_id=e.site_id AND m.name=e.primary_metric WHERE e.id=$1 AND e.site_id=$2`, id, siteID).Scan(&key, &metric, &environment, &variantsRaw, &starts, &ends, &definitionRaw); err != nil {
		writeError(w, 404, "EXPERIMENT_NOT_FOUND", "experiment or primary metric not found")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	if starts != nil && starts.After(from) {
		from = *starts
	}
	if ends != nil && ends.Before(to) {
		to = *ends
	}
	var variants []string
	var baseDefinition semanticDefinition
	_ = json.Unmarshal(variantsRaw, &variants)
	_ = json.Unmarshal(definitionRaw, &baseDefinition)
	out := []map[string]any{}
	var controlRate, controlValue float64
	var controlUsers, controlConversions int64
	for index, variant := range variants {
		definition := baseDefinition
		definition.Filters = append(definition.Filters, semanticFilter{Property: "experiment_id", Operator: "eq", Value: key}, semanticFilter{Property: "variant", Operator: "eq", Value: variant})
		value, evalErr := s.evaluateSemanticMetric(r, siteID, environment, from, to, definition, 1)
		if evalErr != nil {
			writeError(w, 500, "EXPERIMENT_QUERY_FAILED", evalErr.Error())
			return
		}
		var users, converted int64
		_ = s.DB.QueryRow(r.Context(), `SELECT count(DISTINCT entity_id),count(DISTINCT entity_id) FILTER(WHERE is_conversion) FROM analytics_events WHERE site_id=$1 AND environment=$2 AND event_timestamp >= $3 AND event_timestamp < $4 AND properties->>'experiment_id'=$5 AND properties->>'variant'=$6`, siteID, environment, from, to, key, variant).Scan(&users, &converted)
		rate := percent(converted, users)
		lift, confidence := float64(0), float64(0)
		if index == 0 {
			controlValue, controlRate, controlUsers, controlConversions = value, rate, users, converted
		} else {
			if controlValue != 0 {
				lift = (value - controlValue) * 100 / math.Abs(controlValue)
			}
			confidence = twoProportionConfidence(controlConversions, controlUsers, converted, users)
		}
		out = append(out, map[string]any{"variant": variant, "users": users, "conversion_users": converted, "conversion_rate": rate, "metric": metric, "metric_value": value, "lift_percent": lift, "confidence_percent": confidence, "is_control": index == 0, "control_conversion_rate": controlRate})
	}
	writeJSON(w, 200, map[string]any{"experiment_id": id, "experiment_key": key, "environment": environment, "from": from, "to": to, "variants": out, "method": "two-proportion normal approximation; first variant is control"})
}

func twoProportionConfidence(successA, totalA, successB, totalB int64) float64 {
	if totalA == 0 || totalB == 0 {
		return 0
	}
	pA, pB := float64(successA)/float64(totalA), float64(successB)/float64(totalB)
	pooled := float64(successA+successB) / float64(totalA+totalB)
	se := math.Sqrt(pooled * (1 - pooled) * (1/float64(totalA) + 1/float64(totalB)))
	if se == 0 {
		return 0
	}
	z := math.Abs(pB-pA) / se
	pValue := math.Erfc(z / math.Sqrt2)
	return clampScore((1 - pValue) * 100)
}
