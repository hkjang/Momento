package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/auth"
	"github.com/hkjang/Momento/internal/service"
)

func (s *Server) listMetricGoals(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT g.id,g.metric_name,g.name,g.description,g.target_value,g.comparator,g.period,g.environment,g.organization,g.department,g.starts_on,g.ends_on,g.owner,g.active,g.created_at,g.updated_at,m.label,m.format
		FROM metric_goals g LEFT JOIN semantic_metrics m ON m.site_id=g.site_id AND m.name=g.metric_name WHERE g.site_id=$1 ORDER BY g.active DESC,g.updated_at DESC`, siteID)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var metric, name, description, comparator, period, environment, organization, department, owner string
		var label, format *string
		var target float64
		var starts, ends *time.Time
		var active bool
		var created, updated time.Time
		if rows.Scan(&id, &metric, &name, &description, &target, &comparator, &period, &environment, &organization, &department, &starts, &ends, &owner, &active, &created, &updated, &label, &format) == nil {
			out = append(out, map[string]any{"id": id, "metric_name": metric, "metric_label": label, "format": format, "name": name, "description": description, "target_value": target, "comparator": comparator, "period": period, "environment": environment, "organization": organization, "department": department, "starts_on": starts, "ends_on": ends, "owner": owner, "active": active, "created_at": created, "updated_at": updated})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) saveMetricGoal(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	var in struct {
		ID           string  `json:"id"`
		MetricName   string  `json:"metric_name"`
		Name         string  `json:"name"`
		Description  string  `json:"description"`
		TargetValue  float64 `json:"target_value"`
		Comparator   string  `json:"comparator"`
		Period       string  `json:"period"`
		Environment  string  `json:"environment"`
		Organization string  `json:"organization"`
		Department   string  `json:"department"`
		StartsOn     *string `json:"starts_on"`
		EndsOn       *string `json:"ends_on"`
		Owner        string  `json:"owner"`
		Active       *bool   `json:"active"`
	}
	if err := decodeJSON(r, &in, 64<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if !registryNamePattern.MatchString(in.MetricName) || strings.TrimSpace(in.Name) == "" {
		writeError(w, 400, "INVALID_GOAL", "metric_name and name are required")
		return
	}
	var metricExists bool
	if err := s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM semantic_metrics WHERE site_id=$1 AND name=$2 AND status='active')`, siteID, in.MetricName).Scan(&metricExists); err != nil || !metricExists {
		writeError(w, 400, "INVALID_GOAL", "active semantic metric not found")
		return
	}
	if in.Comparator == "" {
		in.Comparator = "gte"
	}
	if in.Comparator != "gte" && in.Comparator != "lte" {
		writeError(w, 400, "INVALID_GOAL", "comparator must be gte or lte")
		return
	}
	if !map[string]bool{"day": true, "week": true, "month": true, "quarter": true}[in.Period] {
		writeError(w, 400, "INVALID_GOAL", "period must be day, week, month or quarter")
		return
	}
	if !environmentNamePattern.MatchString(in.Environment) {
		in.Environment = "prd"
	}
	starts, parseErr := optionalISODate(in.StartsOn)
	if parseErr != nil {
		writeError(w, 400, "INVALID_DATE", "starts_on: "+parseErr.Error())
		return
	}
	ends, parseErr := optionalISODate(in.EndsOn)
	if parseErr != nil {
		writeError(w, 400, "INVALID_DATE", "ends_on: "+parseErr.Error())
		return
	}
	if starts != nil && ends != nil && ends.Before(*starts) {
		writeError(w, 400, "INVALID_DATE", "ends_on must not precede starts_on")
		return
	}
	active := true
	if in.Active != nil {
		active = *in.Active
	}
	var id uuid.UUID
	err = s.DB.QueryRow(r.Context(), `INSERT INTO metric_goals(id,site_id,metric_name,name,description,target_value,comparator,period,environment,organization,department,starts_on,ends_on,owner,active,created_by)
		VALUES(coalesce(nullif($1,'')::uuid,gen_random_uuid()),$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT(site_id,name) DO UPDATE SET metric_name=excluded.metric_name,description=excluded.description,target_value=excluded.target_value,comparator=excluded.comparator,period=excluded.period,environment=excluded.environment,organization=excluded.organization,department=excluded.department,starts_on=excluded.starts_on,ends_on=excluded.ends_on,owner=excluded.owner,active=excluded.active,updated_at=now() RETURNING id`, in.ID, siteID, in.MetricName, strings.TrimSpace(in.Name), in.Description, in.TargetValue, in.Comparator, in.Period, in.Environment, in.Organization, in.Department, starts, ends, in.Owner, active, p.ID).Scan(&id)
	if err != nil {
		writeError(w, 500, "GOAL_SAVE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "metric_goal.save", "metric_goal", id.String(), map[string]any{"name": in.Name, "metric": in.MetricName}, clientIP(r))
	writeJSON(w, 200, map[string]any{"id": id})
}

func optionalISODate(value *string) (*time.Time, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(*value))
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func (s *Server) evaluateMetricGoals(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	_, location, err := s.siteTimezone(r.Context(), siteID)
	if err != nil {
		writeError(w, 500, "TIMEZONE_FAILED", err.Error())
		return
	}
	now := time.Now().In(location)
	rows, err := s.DB.Query(r.Context(), `SELECT g.id,g.name,g.metric_name,g.target_value,g.comparator,g.period,g.environment,g.organization,g.department,m.definition,m.format
		FROM metric_goals g JOIN semantic_metrics m ON m.site_id=g.site_id AND m.name=g.metric_name AND m.status='active'
		WHERE g.site_id=$1 AND g.active AND (g.starts_on IS NULL OR g.starts_on<=$2::date) AND (g.ends_on IS NULL OR g.ends_on>=$2::date) ORDER BY g.name`, siteID, now.Format("2006-01-02"))
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var name, metric, comparator, period, environment, organization, department, format string
		var target float64
		var raw []byte
		if rows.Scan(&id, &name, &metric, &target, &comparator, &period, &environment, &organization, &department, &raw, &format) != nil {
			continue
		}
		var definition semanticDefinition
		if json.Unmarshal(raw, &definition) != nil {
			continue
		}
		if organization != "" {
			definition.Filters = append(definition.Filters, semanticFilter{Property: "organization", Operator: "eq", Value: organization, Scope: "user"})
		}
		if department != "" {
			definition.Filters = append(definition.Filters, semanticFilter{Property: "department", Operator: "eq", Value: department, Scope: "user"})
		}
		fromLocal := goalPeriodStart(now, period)
		from := fromLocal.UTC()
		to := now.Add(time.Nanosecond).UTC()
		value, evalErr := s.evaluateSemanticMetric(r, siteID, environment, from, to, definition, 1)
		if evalErr != nil {
			out = append(out, map[string]any{"id": id, "name": name, "error": evalErr.Error()})
			continue
		}
		achieved := (comparator == "gte" && value >= target) || (comparator == "lte" && value <= target)
		progress := 0.0
		if target != 0 {
			if comparator == "gte" && format != "percent" {
				progress = value / target * 100
			} else if value != 0 {
				progress = target / value * 100
			}
		}
		forecast := goalForecast(now, fromLocal, period, format, value, target, comparator)
		row := map[string]any{"id": id, "name": name, "metric_name": metric, "value": value, "target_value": target, "comparator": comparator, "period": period, "format": format, "progress_percent": progress, "achieved": achieved, "from": from, "to": to}
		for key, item := range forecast {
			row[key] = item
		}
		out = append(out, row)
	}
	writeJSON(w, 200, out)
}

// goalPeriodEnd is the exclusive end of the goal period in local time.
func goalPeriodEnd(start time.Time, period string) time.Time {
	switch period {
	case "day":
		return start.AddDate(0, 0, 1)
	case "week":
		return start.AddDate(0, 0, 7)
	case "quarter":
		return start.AddDate(0, 3, 0)
	default:
		return start.AddDate(0, 1, 0)
	}
}

// goalForecast projects where a cumulative goal lands if the current pace holds.
// Reporting only "45% of target" halfway through a month hides whether that is on
// track, so the projection and the required remaining pace are stated explicitly.
func goalForecast(now, start time.Time, period, format string, value, target float64, comparator string) map[string]any {
	end := goalPeriodEnd(start, period)
	total := end.Sub(start).Seconds()
	elapsed := now.Sub(start).Seconds()
	if total <= 0 || elapsed <= 0 {
		return map[string]any{"forecast_available": false, "forecast_reason": "기간이 아직 시작되지 않았습니다."}
	}
	if elapsed > total {
		elapsed = total
	}
	fraction := elapsed / total
	out := map[string]any{
		"period_start":       start,
		"period_end":         end,
		"elapsed_percent":    fraction * 100,
		"remaining_seconds":  end.Sub(now).Seconds(),
		"forecast_available": true,
		"forecast_basis":     "현재 기간의 진행률과 누적 값을 그대로 연장한 선형 추정입니다.",
	}
	if fraction < 0.1 {
		out["forecast_available"] = false
		out["forecast_reason"] = "기간 진행률이 10% 미만이어서 추정이 불안정합니다."
		return out
	}
	// A rate such as a conversion percentage does not accumulate, so extending it
	// linearly would be wrong. The value measured so far is the best estimate.
	projected := value / fraction
	if format == "percent" {
		projected = value
		out["forecast_basis"] = "비율 지표는 누적되지 않으므로 현재 관측값을 그대로 착지 추정으로 사용합니다."
	}
	out["projected_value"] = projected
	onTrack := (comparator == "gte" && projected >= target) || (comparator == "lte" && projected <= target)
	out["projected_achieved"] = onTrack
	if comparator == "gte" && format != "percent" {
		remaining := target - value
		if remaining < 0 {
			remaining = 0
		}
		out["remaining_to_target"] = remaining
		days := end.Sub(now).Hours() / 24
		if days > 0 && remaining > 0 {
			out["required_daily_pace"] = remaining / days
		}
		achievedPace := 0.0
		if elapsed > 0 {
			achievedPace = value / (elapsed / 86400)
		}
		out["current_daily_pace"] = achievedPace
	}
	if !onTrack {
		out["forecast_status"] = "behind"
	} else {
		out["forecast_status"] = "on_track"
	}
	return out
}

func goalPeriodStart(now time.Time, period string) time.Time {
	switch period {
	case "day":
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	case "week":
		days := (int(now.Weekday()) + 6) % 7
		return time.Date(now.Year(), now.Month(), now.Day()-days, 0, 0, 0, 0, now.Location())
	case "quarter":
		month := time.Month(((int(now.Month())-1)/3)*3 + 1)
		return time.Date(now.Year(), month, 1, 0, 0, 0, 0, now.Location())
	default:
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	}
}

func (s *Server) getQueryPolicy(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	var maxDays, maxScore, background int
	var fast, preview float64
	err = s.DB.QueryRow(r.Context(), `SELECT max_exact_days,max_complexity_score,background_threshold,fast_sample_percent,preview_sample_percent FROM query_policies WHERE site_id=$1`, siteID).Scan(&maxDays, &maxScore, &background, &fast, &preview)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"max_exact_days": maxDays, "max_complexity_score": maxScore, "background_threshold": background, "fast_sample_percent": fast, "preview_sample_percent": preview})
}

func (s *Server) putQueryPolicy(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	var in struct {
		MaxExactDays         int     `json:"max_exact_days"`
		MaxComplexityScore   int     `json:"max_complexity_score"`
		BackgroundThreshold  int     `json:"background_threshold"`
		FastSamplePercent    float64 `json:"fast_sample_percent"`
		PreviewSamplePercent float64 `json:"preview_sample_percent"`
	}
	if err := decodeJSON(r, &in, 16<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if in.MaxExactDays < 1 || in.MaxExactDays > 3650 || in.MaxComplexityScore < 10 || in.MaxComplexityScore > 500 || in.BackgroundThreshold < 10 || in.BackgroundThreshold > in.MaxComplexityScore || in.FastSamplePercent < .1 || in.FastSamplePercent > 100 || in.PreviewSamplePercent < .1 || in.PreviewSamplePercent > in.FastSamplePercent {
		writeError(w, 400, "INVALID_POLICY", "query policy values are outside allowed ranges")
		return
	}
	_, err = s.DB.Exec(r.Context(), `INSERT INTO query_policies(site_id,max_exact_days,max_complexity_score,background_threshold,fast_sample_percent,preview_sample_percent,updated_by) VALUES($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT(site_id) DO UPDATE SET max_exact_days=excluded.max_exact_days,max_complexity_score=excluded.max_complexity_score,background_threshold=excluded.background_threshold,fast_sample_percent=excluded.fast_sample_percent,preview_sample_percent=excluded.preview_sample_percent,updated_by=excluded.updated_by,updated_at=now()`, siteID, in.MaxExactDays, in.MaxComplexityScore, in.BackgroundThreshold, in.FastSamplePercent, in.PreviewSamplePercent, p.ID)
	if err != nil {
		writeError(w, 500, "POLICY_SAVE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "query_policy.update", "site", siteID.String(), in, clientIP(r))
	writeJSON(w, 200, map[string]bool{"updated": true})
}

func (s *Server) listQueryAudit(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT q.id,q.environment,q.query_mode,q.complexity_score,q.sample_percent,q.date_from,q.date_to,q.dimensions,q.metrics,q.duration_ms,q.result_rows,q.status,q.error,q.created_at,coalesce(u.display_name,'API') FROM query_audit q LEFT JOIN users u ON u.id=q.actor_id WHERE q.site_id=$1 ORDER BY q.created_at DESC LIMIT 200`, siteID)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var environment, mode, status, actor string
		var complexity int
		var sample float64
		var from, to, created time.Time
		var dimensions, metrics []string
		var duration, resultRows *int
		var queryError *string
		if rows.Scan(&id, &environment, &mode, &complexity, &sample, &from, &to, &dimensions, &metrics, &duration, &resultRows, &status, &queryError, &created, &actor) == nil {
			out = append(out, map[string]any{"id": id, "environment": environment, "mode": mode, "complexity_score": complexity, "sample_percent": sample, "from": from, "to": to, "dimensions": dimensions, "metrics": metrics, "duration_ms": duration, "result_rows": resultRows, "status": status, "error": queryError, "created_at": created, "actor": actor})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) listAggregateJobs(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT id,environment,job_type,date_from,date_to,status,reason,attempts,started_at,finished_at,error,created_at FROM aggregate_jobs WHERE site_id=$1 ORDER BY created_at DESC LIMIT 200`, siteID)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var environment, jobType, status, reason string
		var from, to *time.Time
		var attempts int
		var started, finished *time.Time
		var jobError *string
		var created time.Time
		if rows.Scan(&id, &environment, &jobType, &from, &to, &status, &reason, &attempts, &started, &finished, &jobError, &created) == nil {
			out = append(out, map[string]any{"id": id, "environment": environment, "job_type": jobType, "date_from": from, "date_to": to, "status": status, "reason": reason, "attempts": attempts, "started_at": started, "finished_at": finished, "error": jobError, "created_at": created})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) createAggregateJob(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	var in struct {
		Environment string `json:"environment"`
		JobType     string `json:"job_type"`
		DateFrom    string `json:"date_from"`
		DateTo      string `json:"date_to"`
		Reason      string `json:"reason"`
	}
	if err := decodeJSON(r, &in, 16<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if in.JobType != "date_range" && in.JobType != "full_rebuild" {
		writeError(w, 400, "INVALID_JOB", "job_type must be date_range or full_rebuild")
		return
	}
	if !environmentNamePattern.MatchString(in.Environment) {
		in.Environment = "prd"
	}
	var from, to any
	if in.JobType == "date_range" {
		parsedFrom, fromErr := time.Parse("2006-01-02", in.DateFrom)
		parsedTo, toErr := time.Parse("2006-01-02", in.DateTo)
		if fromErr != nil || toErr != nil || parsedTo.Before(parsedFrom) || parsedTo.Sub(parsedFrom) > 366*24*time.Hour {
			writeError(w, 400, "INVALID_RANGE", "a valid range of at most 367 days is required")
			return
		}
		from, to = parsedFrom, parsedTo
	}
	var id uuid.UUID
	err = s.DB.QueryRow(r.Context(), `INSERT INTO aggregate_jobs(site_id,environment,job_type,date_from,date_to,reason,requested_by) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, siteID, in.Environment, in.JobType, from, to, in.Reason, p.ID).Scan(&id)
	if err != nil {
		writeError(w, 500, "JOB_CREATE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "aggregate.rebuild", "aggregate_job", id.String(), in, clientIP(r))
	writeJSON(w, 202, map[string]any{"id": id, "status": "pending"})
}

func (s *Server) listAnnotations(w http.ResponseWriter, r *http.Request) {
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
	rows, err := s.DB.Query(r.Context(), `SELECT a.id,a.site_id,a.environment,a.occurred_at,a.ended_at,a.kind,a.title,a.description,a.source,a.metadata,a.created_at
		FROM analytics_annotations a JOIN sites s ON s.workspace_id=a.workspace_id WHERE s.id=$1 AND (a.site_id IS NULL OR a.site_id=$1) AND a.occurred_at >= $2 AND a.occurred_at < $3 ORDER BY a.occurred_at DESC`, siteID, from, to)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var annotationSite *uuid.UUID
		var environment, kind, title, description, source string
		var occurred, created time.Time
		var ended *time.Time
		var raw []byte
		if rows.Scan(&id, &annotationSite, &environment, &occurred, &ended, &kind, &title, &description, &source, &raw, &created) == nil {
			var metadata any
			_ = json.Unmarshal(raw, &metadata)
			out = append(out, map[string]any{"id": id, "site_id": annotationSite, "environment": environment, "occurred_at": occurred, "ended_at": ended, "kind": kind, "title": title, "description": description, "source": source, "metadata": metadata, "created_at": created})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) saveAnnotation(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	var in struct {
		ID            string         `json:"id"`
		Environment   string         `json:"environment"`
		OccurredAt    time.Time      `json:"occurred_at"`
		EndedAt       *time.Time     `json:"ended_at"`
		Kind          string         `json:"kind"`
		Title         string         `json:"title"`
		Description   string         `json:"description"`
		Source        string         `json:"source"`
		Metadata      map[string]any `json:"metadata"`
		WorkspaceWide bool           `json:"workspace_wide"`
	}
	if err := decodeJSON(r, &in, 64<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if in.OccurredAt.IsZero() || strings.TrimSpace(in.Title) == "" || !map[string]bool{"deployment": true, "release": true, "incident": true, "campaign": true, "training": true, "feature_flag": true, "organization": true, "manual": true}[in.Kind] {
		writeError(w, 400, "INVALID_ANNOTATION", "occurred_at, title, and a valid kind are required")
		return
	}
	if !environmentNamePattern.MatchString(in.Environment) {
		in.Environment = "prd"
	}
	if in.Source == "" {
		in.Source = "manual"
	}
	metadata, _ := json.Marshal(in.Metadata)
	var targetSite any = siteID
	if in.WorkspaceWide {
		targetSite = nil
	}
	var id uuid.UUID
	err = s.DB.QueryRow(r.Context(), `INSERT INTO analytics_annotations(id,site_id,workspace_id,environment,occurred_at,ended_at,kind,title,description,source,metadata,created_by)
		SELECT coalesce(nullif($1,'')::uuid,gen_random_uuid()),$2,s.workspace_id,$3,$4,$5,$6,$7,$8,$9,$10,$11 FROM sites s WHERE s.id=$12
		ON CONFLICT(id) DO UPDATE SET site_id=excluded.site_id,environment=excluded.environment,occurred_at=excluded.occurred_at,ended_at=excluded.ended_at,kind=excluded.kind,title=excluded.title,description=excluded.description,source=excluded.source,metadata=excluded.metadata,updated_at=now() RETURNING id`, in.ID, targetSite, in.Environment, in.OccurredAt, in.EndedAt, in.Kind, strings.TrimSpace(in.Title), in.Description, in.Source, metadata, p.ID, siteID).Scan(&id)
	if err != nil {
		writeError(w, 500, "ANNOTATION_SAVE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "annotation.save", "analytics_annotation", id.String(), map[string]any{"kind": in.Kind, "title": in.Title}, clientIP(r))
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) deleteAnnotation(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "INVALID_ID", "invalid annotation id")
		return
	}
	tag, err := s.DB.Exec(r.Context(), `DELETE FROM analytics_annotations a USING sites s WHERE a.id=$1 AND s.id=$2 AND a.workspace_id=s.workspace_id`, id, siteID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, 404, "NOT_FOUND", "annotation not found")
		return
	}
	s.audit(r.Context(), &p, "annotation.delete", "analytics_annotation", id.String(), nil, clientIP(r))
	w.WriteHeader(204)
}

func (s *Server) validateEventContractCI(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	var in struct {
		Environment string `json:"environment"`
		Events      []struct {
			Name            string         `json:"name"`
			ContractVersion int            `json:"contract_version"`
			Properties      map[string]any `json:"properties"`
		} `json:"events"`
	}
	if err := decodeJSON(r, &in, 512<<10); err != nil || len(in.Events) == 0 || len(in.Events) > 1000 {
		writeError(w, 400, "INVALID_PAYLOAD", "1 to 1000 events are required")
		return
	}
	if !environmentNamePattern.MatchString(in.Environment) {
		in.Environment = "prd"
	}
	var environmentMode string
	if s.DB.QueryRow(r.Context(), `SELECT contract_mode FROM site_environments WHERE site_id=$1 AND name=$2 AND active`, siteID, in.Environment).Scan(&environmentMode) != nil {
		writeError(w, 400, "INVALID_ENVIRONMENT", "unknown or inactive environment")
		return
	}
	results := []map[string]any{}
	errorsCount, warningsCount := 0, 0
	for _, event := range in.Events {
		version := event.ContractVersion
		var current int
		var deprecated bool
		if err := s.DB.QueryRow(r.Context(), `SELECT current_version,deprecated FROM event_definitions WHERE site_id=$1 AND name=$2`, siteID, event.Name).Scan(&current, &deprecated); err != nil {
			errorsCount++
			results = append(results, map[string]any{"event": event.Name, "status": "error", "messages": []string{"event is not registered"}})
			continue
		}
		if version == 0 {
			version = current
		}
		var schema []byte
		var contractMode, status string
		if err := s.DB.QueryRow(r.Context(), `SELECT schema,validation_mode,status FROM event_contract_versions WHERE site_id=$1 AND event_name=$2 AND version=$3`, siteID, event.Name, version).Scan(&schema, &contractMode, &status); err != nil {
			errorsCount++
			results = append(results, map[string]any{"event": event.Name, "version": version, "status": "error", "messages": []string{"contract version is not registered"}})
			continue
		}
		messages := validateCIProperties(event.Properties, schema)
		if deprecated || status == "deprecated" {
			messages = append(messages, "event or contract is deprecated")
		}
		mode := strictestCIMode(environmentMode, contractMode)
		resultStatus := "valid"
		if len(messages) > 0 && mode == "reject" {
			resultStatus = "error"
			errorsCount++
		} else if len(messages) > 0 {
			resultStatus = "warning"
			warningsCount++
		}
		results = append(results, map[string]any{"event": event.Name, "version": version, "status": resultStatus, "messages": messages})
	}
	writeJSON(w, 200, map[string]any{"valid": errorsCount == 0, "environment": in.Environment, "errors": errorsCount, "warnings": warningsCount, "results": results})
}

func strictestCIMode(a, b string) string {
	rank := map[string]int{"allow": 0, "warn": 1, "reject": 2}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func validateCIProperties(properties map[string]any, schemaRaw []byte) []string {
	var schema struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if json.Unmarshal(schemaRaw, &schema) != nil {
		return []string{"invalid registry schema"}
	}
	messages := []string{}
	for _, key := range schema.Required {
		value, ok := properties[key]
		if !ok || value == nil || fmt.Sprint(value) == "" {
			messages = append(messages, key+" is required")
		}
	}
	for key, definition := range schema.Properties {
		value, ok := properties[key]
		if !ok || value == nil {
			continue
		}
		valid := true
		switch definition.Type {
		case "string":
			_, valid = value.(string)
		case "number", "integer":
			switch value.(type) {
			case float64, float32, int, int32, int64, json.Number:
			default:
				valid = false
			}
		case "boolean":
			_, valid = value.(bool)
		case "array":
			_, valid = value.([]any)
		case "object":
			_, valid = value.(map[string]any)
		}
		if !valid {
			messages = append(messages, key+" must be "+definition.Type)
		}
	}
	return messages
}

func (s *Server) eventCatalog(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	environment := requestEnvironment(r)
	rows, err := s.DB.Query(r.Context(), `SELECT d.name,d.description,d.owner,d.current_version,d.deprecated,v.schema,min(e.event_timestamp),max(e.event_timestamp),count(e.event_id),count(*) FILTER(WHERE e.event_timestamp>=now()-interval '30 days'),
		(SELECT count(*) FROM semantic_metrics m WHERE m.site_id=d.site_id AND m.definition::text LIKE '%'||d.name||'%'),
		(SELECT count(*) FROM metric_goals g WHERE g.site_id=d.site_id AND g.metric_name IN (SELECT name FROM semantic_metrics m WHERE m.site_id=d.site_id AND m.definition::text LIKE '%'||d.name||'%'))
		FROM event_definitions d JOIN event_contract_versions v ON v.site_id=d.site_id AND v.event_name=d.name AND v.version=d.current_version
		LEFT JOIN raw_events e ON e.site_id=d.site_id AND e.event_name=d.name AND e.environment=$2 WHERE d.site_id=$1
		GROUP BY d.site_id,d.name,d.description,d.owner,d.current_version,d.deprecated,v.schema ORDER BY d.name`, siteID, environment)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var name, description, owner string
		var version int
		var deprecated bool
		var schema []byte
		var firstSeen, lastSeen *time.Time
		var volume, volume30, metricUses, goalUses int64
		if rows.Scan(&name, &description, &owner, &version, &deprecated, &schema, &firstSeen, &lastSeen, &volume, &volume30, &metricUses, &goalUses) == nil {
			var schemaValue any
			_ = json.Unmarshal(schema, &schemaValue)
			status := "healthy"
			if deprecated {
				status = "deprecated"
			} else if volume30 == 0 {
				status = "inactive"
			}
			out = append(out, map[string]any{"name": name, "description": description, "owner": owner, "current_version": version, "status": status, "schema": schemaValue, "first_seen": firstSeen, "last_seen": lastSeen, "volume": volume, "volume_30d": volume30, "used_by": map[string]any{"metrics": metricUses, "goals": goalUses}})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) dataLineage(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	metricRows, err := s.DB.Query(r.Context(), `SELECT name,label,definition FROM semantic_metrics WHERE site_id=$1 ORDER BY name`, siteID)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer metricRows.Close()
	nodes := []map[string]any{}
	edges := []map[string]any{}
	for metricRows.Next() {
		var name, label string
		var raw []byte
		if metricRows.Scan(&name, &label, &raw) != nil {
			continue
		}
		nodes = append(nodes, map[string]any{"id": "metric:" + name, "kind": "metric", "label": label})
		var def semanticDefinition
		if json.Unmarshal(raw, &def) == nil {
			if def.EventName != "" {
				nodes = append(nodes, map[string]any{"id": "event:" + def.EventName, "kind": "event", "label": def.EventName})
				edges = append(edges, map[string]any{"from": "event:" + def.EventName, "to": "metric:" + name, "relation": "aggregates"})
			}
			if def.Metric != "" {
				edges = append(edges, map[string]any{"from": "metric:" + def.Metric, "to": "metric:" + name, "relation": "formula"})
			}
		}
	}
	goalRows, _ := s.DB.Query(r.Context(), `SELECT id,name,metric_name FROM metric_goals WHERE site_id=$1`, siteID)
	if goalRows != nil {
		defer goalRows.Close()
		for goalRows.Next() {
			var id uuid.UUID
			var name, metric string
			if goalRows.Scan(&id, &name, &metric) == nil {
				nodes = append(nodes, map[string]any{"id": "goal:" + id.String(), "kind": "goal", "label": name})
				edges = append(edges, map[string]any{"from": "metric:" + metric, "to": "goal:" + id.String(), "relation": "measures"})
			}
		}
	}
	writeJSON(w, 200, map[string]any{"nodes": dedupeLineageNodes(nodes), "edges": edges})
}

func dedupeLineageNodes(in []map[string]any) []map[string]any {
	seen := map[string]bool{}
	out := []map[string]any{}
	for _, node := range in {
		id := fmt.Sprint(node["id"])
		if !seen[id] {
			seen[id] = true
			out = append(out, node)
		}
	}
	return out
}

func (s *Server) listPrivacyRequests(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT id,request_type,identity_type,identity_value,date_from,date_to,status,reason,result,requested_at,approved_at,completed_at FROM privacy_requests WHERE site_id=$1 ORDER BY requested_at DESC LIMIT 200`, siteID)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var requestType, identityType, identityValue, status, reason string
		var from, to, approved, completed *time.Time
		var requested time.Time
		var raw []byte
		if rows.Scan(&id, &requestType, &identityType, &identityValue, &from, &to, &status, &reason, &raw, &requested, &approved, &completed) == nil {
			var result any
			_ = json.Unmarshal(raw, &result)
			out = append(out, map[string]any{"id": id, "request_type": requestType, "identity_type": identityType, "identity_value": identityValue, "date_from": from, "date_to": to, "status": status, "reason": reason, "result": result, "requested_at": requested, "approved_at": approved, "completed_at": completed})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) createPrivacyRequest(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	var in struct {
		RequestType   string `json:"request_type"`
		IdentityType  string `json:"identity_type"`
		IdentityValue string `json:"identity_value"`
		DateFrom      string `json:"date_from"`
		DateTo        string `json:"date_to"`
		Reason        string `json:"reason"`
	}
	if err := decodeJSON(r, &in, 32<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if !map[string]bool{"delete": true, "export": true}[in.RequestType] || !map[string]bool{"user_id": true, "visitor_id": true, "period": true}[in.IdentityType] {
		writeError(w, 400, "INVALID_REQUEST", "request_type or identity_type is invalid")
		return
	}
	var from, to any
	if in.IdentityType == "period" {
		parsedFrom, e1 := time.Parse("2006-01-02", in.DateFrom)
		parsedTo, e2 := time.Parse("2006-01-02", in.DateTo)
		if e1 != nil || e2 != nil || parsedTo.Before(parsedFrom) {
			writeError(w, 400, "INVALID_RANGE", "valid date_from and date_to are required")
			return
		}
		from, to = parsedFrom, parsedTo
	} else if strings.TrimSpace(in.IdentityValue) == "" {
		writeError(w, 400, "INVALID_IDENTITY", "identity_value is required")
		return
	}
	var id uuid.UUID
	err = s.DB.QueryRow(r.Context(), `INSERT INTO privacy_requests(site_id,request_type,identity_type,identity_value,date_from,date_to,reason,requested_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, siteID, in.RequestType, in.IdentityType, in.IdentityValue, from, to, in.Reason, p.ID).Scan(&id)
	if err != nil {
		writeError(w, 500, "REQUEST_CREATE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "privacy_request.create", "privacy_request", id.String(), map[string]any{"request_type": in.RequestType, "identity_type": in.IdentityType}, clientIP(r))
	writeJSON(w, 201, map[string]any{"id": id, "status": "pending"})
}

func (s *Server) decidePrivacyRequest(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "INVALID_ID", "invalid privacy request id")
		return
	}
	var in struct {
		Decision string `json:"decision"`
	}
	if err := decodeJSON(r, &in, 8<<10); err != nil || (in.Decision != "approve" && in.Decision != "reject") {
		writeError(w, 400, "INVALID_DECISION", "decision must be approve or reject")
		return
	}
	if in.Decision == "reject" {
		tag, err := s.DB.Exec(r.Context(), `UPDATE privacy_requests SET status='rejected',approved_by=$3,approved_at=now(),updated_at=now() WHERE id=$1 AND site_id=$2 AND status='pending'`, id, siteID, p.ID)
		if err != nil || tag.RowsAffected() == 0 {
			writeError(w, 409, "REQUEST_NOT_PENDING", "privacy request is not pending")
			return
		}
		s.audit(r.Context(), &p, "privacy_request.reject", "privacy_request", id.String(), nil, clientIP(r))
		writeJSON(w, 200, map[string]string{"status": "rejected"})
		return
	}
	result, err := s.executePrivacyRequest(r.Context(), siteID, id, p.ID)
	if err != nil {
		writeError(w, 500, "PRIVACY_REQUEST_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "privacy_request.complete", "privacy_request", id.String(), result, clientIP(r))
	writeJSON(w, 200, map[string]any{"status": "completed", "result": result})
}

func (s *Server) exportPrivacyRequest(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "INVALID_ID", "invalid privacy request id")
		return
	}
	var identityType, value string
	var fromDate, toDate *time.Time
	if err := s.DB.QueryRow(r.Context(), `SELECT identity_type,identity_value,date_from,date_to FROM privacy_requests WHERE id=$1 AND site_id=$2 AND request_type='export' AND status='completed'`, id, siteID).Scan(&identityType, &value, &fromDate, &toDate); err != nil {
		writeError(w, 404, "EXPORT_NOT_AUTHORIZED", "completed export request not found")
		return
	}
	linked := []string{}
	if identityType == "user_id" {
		rows, queryErr := s.DB.Query(r.Context(), `SELECT visitor_id FROM visitor_identities WHERE site_id=$1 AND user_id=$2`, siteID, value)
		if queryErr != nil {
			writeError(w, 500, "EXPORT_FAILED", queryErr.Error())
			return
		}
		for rows.Next() {
			var visitor string
			if rows.Scan(&visitor) == nil {
				linked = append(linked, visitor)
			}
		}
		rows.Close()
	}
	var from, to time.Time
	if fromDate != nil && toDate != nil {
		from, to, err = s.privacyRequestDateRange(r.Context(), siteID, *fromDate, *toDate)
		if err != nil {
			writeError(w, 500, "TIMEZONE_FAILED", err.Error())
			return
		}
	}
	where, args := privacyRequestWhere(siteID, identityType, value, linked, from, to)
	rows, err := s.DB.Query(r.Context(), `SELECT event_id,event_timestamp,event_name,visitor_id,session_id,user_id,page_url,page_title,referrer,source,medium,campaign,device_type,browser,os,language,screen,network_name,properties,user_properties,session_properties,environment,contract_version FROM raw_events WHERE `+where+` ORDER BY event_timestamp,id`, args...)
	if err != nil {
		writeError(w, 500, "EXPORT_FAILED", err.Error())
		return
	}
	defer rows.Close()
	s.audit(r.Context(), &p, "privacy_request.export", "privacy_request", id.String(), map[string]any{"format": "ndjson", "complete": true}, clientIP(r))
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="momento-privacy-%s.ndjson"`, id.String()))
	w.Header().Set("X-Momento-Export-Completeness", "complete")
	encoder := json.NewEncoder(w)
	for rows.Next() {
		values, scanErr := rows.Values()
		if scanErr != nil {
			continue
		}
		for _, index := range []int{18, 19, 20} {
			if raw, ok := values[index].([]byte); ok {
				values[index] = json.RawMessage(raw)
			}
		}
		_ = encoder.Encode(map[string]any{"event_id": exportUUID(values[0]), "timestamp": values[1], "event_name": values[2], "visitor_id": values[3], "session_id": values[4], "user_id": values[5], "page_url": values[6], "page_title": values[7], "referrer": values[8], "source": values[9], "medium": values[10], "campaign": values[11], "device_type": values[12], "browser": values[13], "os": values[14], "language": values[15], "screen": values[16], "network": values[17], "properties": values[18], "user_properties": values[19], "session_properties": values[20], "environment": values[21], "contract_version": values[22]})
	}
}

func (s *Server) executePrivacyRequest(ctx context.Context, siteID, requestID, approver uuid.UUID) (map[string]any, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var requestType, identityType, value string
	var fromDate, toDate *time.Time
	if err := tx.QueryRow(ctx, `UPDATE privacy_requests SET status='running',approved_by=$3,approved_at=now(),updated_at=now() WHERE id=$1 AND site_id=$2 AND status='pending' RETURNING request_type,identity_type,identity_value,date_from,date_to`, requestID, siteID, approver).Scan(&requestType, &identityType, &value, &fromDate, &toDate); err != nil {
		return nil, fmt.Errorf("request is not pending: %w", err)
	}
	linked := []string{}
	if identityType == "user_id" {
		rows, err := tx.Query(ctx, `SELECT visitor_id FROM visitor_identities WHERE site_id=$1 AND user_id=$2`, siteID, value)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var visitor string
			if rows.Scan(&visitor) == nil {
				linked = append(linked, visitor)
			}
		}
		rows.Close()
	}
	mode := map[string]string{"visitor_id": "visitor", "user_id": "user_id", "period": "period"}[identityType]
	var from, to time.Time
	if fromDate != nil && toDate != nil {
		from, to, err = s.privacyRequestDateRange(ctx, siteID, *fromDate, *toDate)
		if err != nil {
			return nil, err
		}
	}
	result := map[string]any{}
	if requestType == "export" {
		var count int64
		where, args := privacyRequestWhere(siteID, identityType, value, linked, from, to)
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM raw_events WHERE `+where, args...).Scan(&count); err != nil {
			return nil, err
		}
		result["matched_events"] = count
		result["message"] = "The authorized NDJSON export is available from this Privacy Request."
	} else if requestType == "property_delete" {
		if !eventNamePatternForProperty.MatchString(value) {
			return nil, fmt.Errorf("property_delete requires a safe property name in identity_value")
		}
		tag, err := tx.Exec(ctx, `UPDATE raw_events SET properties=properties-$2 WHERE site_id=$1 AND properties ? $2`, siteID, value)
		if err != nil {
			return nil, err
		}
		result["updated_events"] = tag.RowsAffected()
	} else {
		if err := scrubQueuedAnalyticsData(ctx, tx, siteID, mode, value, linked, from, to); err != nil {
			return nil, err
		}
		where, args := privacyRequestWhere(siteID, identityType, value, linked, from, to)
		tag, err := tx.Exec(ctx, `DELETE FROM raw_events WHERE `+where, args...)
		if err != nil {
			return nil, err
		}
		result["deleted_events"] = tag.RowsAffected()
		if err := service.RebuildSiteDerivedData(ctx, tx, siteID); err != nil {
			return nil, err
		}
	}
	body, _ := json.Marshal(result)
	if _, err := tx.Exec(ctx, `UPDATE privacy_requests SET status='completed',result=$2,completed_at=now(),updated_at=now() WHERE id=$1`, requestID, body); err != nil {
		return nil, err
	}
	return result, tx.Commit(ctx)
}

func (s *Server) privacyRequestDateRange(ctx context.Context, siteID uuid.UUID, fromDate, toDate time.Time) (time.Time, time.Time, error) {
	_, location, err := s.siteTimezone(ctx, siteID)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	from := time.Date(fromDate.Year(), fromDate.Month(), fromDate.Day(), 0, 0, 0, 0, location).UTC()
	to := time.Date(toDate.Year(), toDate.Month(), toDate.Day()+1, 0, 0, 0, 0, location).UTC()
	return from, to, nil
}

func privacyRequestWhere(siteID uuid.UUID, identityType, value string, linked []string, from, to time.Time) (string, []any) {
	switch identityType {
	case "visitor_id":
		return "site_id=$1 AND visitor_id=$2", []any{siteID, value}
	case "user_id":
		return "site_id=$1 AND (user_id=$2 OR visitor_id=ANY($3))", []any{siteID, value, linked}
	default:
		return "site_id=$1 AND event_timestamp >= $2 AND event_timestamp < $3", []any{siteID, from, to}
	}
}
