package httpapi

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/auth"
)

func (s *Server) cohortReport(w http.ResponseWriter, r *http.Request) {
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
	granularity := r.URL.Query().Get("granularity")
	if granularity == "" {
		granularity = "week"
	}
	periods, _ := strconv.Atoi(r.URL.Query().Get("periods"))
	if periods < 1 || periods > 52 {
		periods = 12
	}
	var bucket, offset string
	switch granularity {
	case "day":
		bucket = "(event_timestamp AT TIME ZONE $5)::date"
		offset = "(a.activity_date-c.cohort_date)"
	case "month":
		bucket = "date_trunc('month',event_timestamp AT TIME ZONE $5)::date"
		offset = "((extract(year FROM a.activity_date)::int-extract(year FROM c.cohort_date)::int)*12+extract(month FROM a.activity_date)::int-extract(month FROM c.cohort_date)::int)"
	default:
		granularity = "week"
		bucket = "date_trunc('week',event_timestamp AT TIME ZONE $5)::date"
		offset = "((a.activity_date-c.cohort_date)/7)"
	}
	timezone, _, _ := s.siteTimezone(r.Context(), siteID)
	cohortEvent := strings.TrimSpace(r.URL.Query().Get("cohort_event"))
	returnEvent := strings.TrimSpace(r.URL.Query().Get("return_event"))
	query := `WITH cohort_candidates AS (
		SELECT entity_id,min(` + bucket + `) cohort_date FROM analytics_events
		WHERE site_id=$1 AND environment=$4 AND ($6='' OR event_name=$6) GROUP BY entity_id
	), cohorts AS (
		SELECT entity_id,cohort_date FROM cohort_candidates WHERE cohort_date >= ($2 AT TIME ZONE $5)::date AND cohort_date < ($3 AT TIME ZONE $5)::date
	), activity AS (
		SELECT DISTINCT entity_id,` + bucket + ` activity_date FROM analytics_events
		WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND ($7='' OR event_name=$7)
	), retained AS (
		SELECT c.cohort_date,` + offset + ` period_no,count(DISTINCT c.entity_id) retained
		FROM cohorts c JOIN activity a ON a.entity_id=c.entity_id AND a.activity_date>=c.cohort_date
		GROUP BY c.cohort_date,period_no
	), sizes AS (SELECT cohort_date,count(*) cohort_size FROM cohorts GROUP BY cohort_date)
	SELECT s.cohort_date,s.cohort_size,r.period_no,r.retained FROM sizes s JOIN retained r USING(cohort_date)
	WHERE r.period_no BETWEEN 0 AND $8 ORDER BY s.cohort_date,r.period_no`
	rows, err := s.DB.Query(r.Context(), query, siteID, from, to, requestEnvironment(r), timezone, cohortEvent, returnEvent, periods-1)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	type cohort struct {
		date string
		size int64
		ret  map[int]int64
	}
	cohorts := map[string]*cohort{}
	order := []string{}
	for rows.Next() {
		var date time.Time
		var size, retained int64
		var period int
		if rows.Scan(&date, &size, &period, &retained) != nil {
			continue
		}
		key := date.Format("2006-01-02")
		if cohorts[key] == nil {
			cohorts[key] = &cohort{date: key, size: size, ret: map[int]int64{}}
			order = append(order, key)
		}
		cohorts[key].ret[period] = retained
	}
	out := []map[string]any{}
	for _, key := range order {
		item := cohorts[key]
		values := make([]map[string]any, periods)
		for period := 0; period < periods; period++ {
			count := item.ret[period]
			rate := float64(0)
			if item.size > 0 {
				rate = float64(count) * 100 / float64(item.size)
			}
			values[period] = map[string]any{"period": period, "users": count, "retention_rate": rate}
		}
		out = append(out, map[string]any{"cohort": item.date, "size": item.size, "periods": values})
	}
	writeJSON(w, 200, map[string]any{"granularity": granularity, "cohort_event": cohortEvent, "return_event": returnEvent, "environment": requestEnvironment(r), "cohorts": out})
}

type journeyStep struct {
	Name    string `json:"name"`
	Event   string `json:"event"`
	SiteID  string `json:"site_id,omitempty"`
	Service string `json:"service,omitempty"`
	Feature string `json:"feature,omitempty"`
}

func validateJourneySteps(steps []journeyStep) error {
	if len(steps) < 2 || len(steps) > 12 {
		return fmt.Errorf("journey requires 2 to 12 steps")
	}
	for index, step := range steps {
		if strings.TrimSpace(step.Name) == "" || !eventNamePatternForProperty.MatchString(step.Event) {
			return fmt.Errorf("step %d requires a name and valid event", index+1)
		}
	}
	return nil
}

func (s *Server) listBusinessJourneys(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	p, _ := auth.FromContext(r.Context())
	rows, err := s.DB.Query(r.Context(), `SELECT id,name,description,steps,conversion_window_days,shared,active,owner_id,created_at,updated_at FROM business_journeys WHERE site_id=$1 AND (shared OR owner_id=$2 OR $3 IN ('super_admin','organization_admin','workspace_admin')) ORDER BY updated_at DESC`, siteID, p.ID, p.Role)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var name, description string
		var steps []byte
		var window int
		var shared, active bool
		var owner *uuid.UUID
		var created, updated time.Time
		if rows.Scan(&id, &name, &description, &steps, &window, &shared, &active, &owner, &created, &updated) == nil {
			var value any
			_ = json.Unmarshal(steps, &value)
			out = append(out, map[string]any{"id": id, "name": name, "description": description, "steps": value, "conversion_window_days": window, "shared": shared, "active": active, "owner_id": owner, "created_at": created, "updated_at": updated})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) saveBusinessJourney(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
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
	if err := decodeJSON(r, &in, 128<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		writeError(w, 400, "INVALID_JOURNEY", "name is required")
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
		writeError(w, 400, "INVALID_WINDOW", "conversion window must be between 1 and 365 days")
		return
	}
	active := true
	if in.Active != nil {
		active = *in.Active
	}
	steps, _ := json.Marshal(in.Steps)
	var id uuid.UUID
	if in.ID == "" {
		err = s.DB.QueryRow(r.Context(), `INSERT INTO business_journeys(site_id,owner_id,name,description,steps,conversion_window_days,shared,active) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, siteID, p.ID, in.Name, in.Description, steps, in.ConversionWindowDays, in.Shared, active).Scan(&id)
	} else {
		id, err = uuid.Parse(in.ID)
		if err == nil {
			var tag interface{ RowsAffected() int64 }
			tag, err = s.DB.Exec(r.Context(), `UPDATE business_journeys SET name=$3,description=$4,steps=$5,conversion_window_days=$6,shared=$7,active=$8,updated_at=now() WHERE id=$1 AND site_id=$2 AND (owner_id=$9 OR $10 IN ('super_admin','organization_admin','workspace_admin'))`, id, siteID, in.Name, in.Description, steps, in.ConversionWindowDays, in.Shared, active, p.ID, p.Role)
			if err == nil && tag.RowsAffected() == 0 {
				err = fmt.Errorf("journey not found")
			}
		}
	}
	if err != nil {
		writeError(w, 500, "JOURNEY_SAVE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "business_journey.save", "business_journey", id.String(), map[string]any{"name": in.Name}, clientIP(r))
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) deleteBusinessJourney(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	p, _ := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "INVALID_ID", "invalid journey id")
		return
	}
	tag, err := s.DB.Exec(r.Context(), `DELETE FROM business_journeys WHERE id=$1 AND site_id=$2 AND (owner_id=$3 OR $4 IN ('super_admin','organization_admin','workspace_admin'))`, id, siteID, p.ID, p.Role)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, 404, "NOT_FOUND", "journey not found")
		return
	}
	s.audit(r.Context(), &p, "business_journey.delete", "business_journey", id.String(), nil, clientIP(r))
	w.WriteHeader(204)
}

func (s *Server) analyzeBusinessJourney(w http.ResponseWriter, r *http.Request) {
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
		if parseErr != nil || s.DB.QueryRow(r.Context(), `SELECT steps,conversion_window_days FROM business_journeys WHERE id=$1 AND site_id=$2 AND active`, id, siteID).Scan(&raw, &in.ConversionWindowDays) != nil {
			writeError(w, 404, "JOURNEY_NOT_FOUND", "journey not found")
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
	args := []any{siteID, from, to, requestEnvironment(r), in.ConversionWindowDays}
	ctes := []string{}
	for index, step := range in.Steps {
		args = append(args, step.Event)
		eventArg := len(args)
		args = append(args, step.Service)
		serviceArg := len(args)
		args = append(args, step.Feature)
		featureArg := len(args)
		condition := fmt.Sprintf("e.event_name=$%d AND ($%d='' OR e.properties->>'service'=$%d) AND ($%d='' OR e.properties->>'feature'=$%d)", eventArg, serviceArg, serviceArg, featureArg, featureArg)
		name := fmt.Sprintf("step%d", index+1)
		if index == 0 {
			ctes = append(ctes, fmt.Sprintf("%s AS (SELECT e.entity_id,min(e.event_timestamp) reached_at FROM analytics_events e WHERE e.site_id=$1 AND e.environment=$4 AND e.event_timestamp >= $2 AND e.event_timestamp < $3 AND %s GROUP BY e.entity_id)", name, condition))
		} else {
			previous := fmt.Sprintf("step%d", index)
			ctes = append(ctes, fmt.Sprintf("%s AS (SELECT p.entity_id,min(e.event_timestamp) reached_at FROM %s p JOIN analytics_events e ON e.site_id=$1 AND e.environment=$4 AND e.entity_id=p.entity_id AND e.event_timestamp>p.reached_at AND e.event_timestamp<=p.reached_at+make_interval(days=>$5) WHERE e.event_timestamp<$3 AND %s GROUP BY p.entity_id)", name, previous, condition))
		}
	}
	selects := []string{}
	for index := range in.Steps {
		name := fmt.Sprintf("step%d", index+1)
		elapsed := "0::double precision"
		if index > 0 {
			elapsed = fmt.Sprintf("coalesce((SELECT avg(extract(epoch FROM(s.reached_at-f.reached_at))) FROM %s s JOIN step1 f USING(entity_id)),0)", name)
		}
		selects = append(selects, fmt.Sprintf("SELECT %d step_no,(SELECT count(*) FROM %s) users,%s avg_seconds", index+1, name, elapsed))
	}
	query := "WITH " + strings.Join(ctes, ",") + " " + strings.Join(selects, " UNION ALL ") + " ORDER BY step_no"
	rows, err := s.DB.Query(r.Context(), query, args...)
	if err != nil {
		writeError(w, 500, "JOURNEY_QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	result := []map[string]any{}
	var first int64
	for rows.Next() {
		var stepNo int
		var users int64
		var avgSeconds float64
		if rows.Scan(&stepNo, &users, &avgSeconds) != nil {
			continue
		}
		if stepNo == 1 {
			first = users
		}
		rate := float64(0)
		if first > 0 {
			rate = float64(users) * 100 / float64(first)
		}
		result = append(result, map[string]any{"step": stepNo, "name": in.Steps[stepNo-1].Name, "event": in.Steps[stepNo-1].Event, "users": users, "conversion_rate": rate, "average_elapsed_seconds": avgSeconds})
	}
	writeJSON(w, 200, map[string]any{"environment": requestEnvironment(r), "steps": result})
}

func (s *Server) listAdoptionTargets(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT id,organization,department,feature,eligible_users,updated_at FROM adoption_targets WHERE site_id=$1 ORDER BY organization,department,feature`, siteID)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var organization, department, feature string
		var eligible int
		var updated time.Time
		if rows.Scan(&id, &organization, &department, &feature, &eligible, &updated) == nil {
			out = append(out, map[string]any{"id": id, "organization": organization, "department": department, "feature": feature, "eligible_users": eligible, "updated_at": updated})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) saveAdoptionTarget(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	p, _ := auth.FromContext(r.Context())
	var in struct {
		Organization string `json:"organization"`
		Department   string `json:"department"`
		Feature      string `json:"feature"`
		Eligible     int    `json:"eligible_users"`
	}
	if err := decodeJSON(r, &in, 32<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if strings.TrimSpace(in.Feature) == "" || in.Eligible < 0 || len(in.Feature) > 128 {
		writeError(w, 400, "INVALID_TARGET", "feature and non-negative eligible_users are required")
		return
	}
	var id uuid.UUID
	err = s.DB.QueryRow(r.Context(), `INSERT INTO adoption_targets(site_id,organization,department,feature,eligible_users,updated_by) VALUES($1,$2,$3,$4,$5,$6)
		ON CONFLICT(site_id,organization,department,feature) DO UPDATE SET eligible_users=excluded.eligible_users,updated_by=excluded.updated_by,updated_at=now() RETURNING id`, siteID, strings.TrimSpace(in.Organization), strings.TrimSpace(in.Department), strings.TrimSpace(in.Feature), in.Eligible, p.ID).Scan(&id)
	if err != nil {
		writeError(w, 500, "TARGET_SAVE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "adoption_target.save", "adoption_target", id.String(), map[string]any{"feature": in.Feature, "eligible_users": in.Eligible}, clientIP(r))
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) deleteAdoptionTarget(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	siteID, err := s.resolveSite(r, "siteID")
	id, parseErr := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil || parseErr != nil {
		writeError(w, 404, "NOT_FOUND", "target not found")
		return
	}
	tag, err := s.DB.Exec(r.Context(), `DELETE FROM adoption_targets WHERE id=$1 AND site_id=$2`, id, siteID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, 404, "NOT_FOUND", "target not found")
		return
	}
	s.audit(r.Context(), &p, "adoption_target.delete", "adoption_target", id.String(), nil, clientIP(r))
	w.WriteHeader(204)
}

func (s *Server) adoptionReport(w http.ResponseWriter, r *http.Request) {
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
	rows, err := s.DB.Query(r.Context(), `WITH usage AS (
		SELECT entity_id,coalesce(canonical_user_properties->>'organization','(미지정)') organization,coalesce(canonical_user_properties->>'department','(미지정)') department,properties->>'feature' feature,count(*) events,min(event_timestamp) first_used,max(event_timestamp) last_used
		FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND coalesce(properties->>'feature','')<>'' GROUP BY 1,2,3,4
	), population AS (
		SELECT coalesce(canonical_user_properties->>'organization','(미지정)') organization,coalesce(canonical_user_properties->>'department','(미지정)') department,count(DISTINCT entity_id) observed_population
		FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 GROUP BY 1,2
	)
	SELECT u.organization,u.department,u.feature,count(*) users,sum(u.events),count(*) FILTER(WHERE u.events>=2),count(*) FILTER(WHERE u.last_used >= $3-interval '7 days'),count(*) FILTER(WHERE u.last_used < $3-interval '30 days'),coalesce(t.eligible_users,p.observed_population),min(u.first_used),max(u.last_used)
	FROM usage u JOIN population p USING(organization,department) LEFT JOIN LATERAL (
		SELECT target.eligible_users FROM adoption_targets target
		WHERE target.site_id=$1 AND (target.organization='' OR target.organization=u.organization) AND (target.department='' OR target.department=u.department) AND target.feature=u.feature
		ORDER BY ((target.organization<>'')::int+(target.department<>'')::int) DESC LIMIT 1
	) t ON true
	GROUP BY u.organization,u.department,u.feature,t.eligible_users,p.observed_population ORDER BY users DESC`, siteID, from, to, requestEnvironment(r))
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var organization, department, feature string
		var users, events, repeatUsers, active7, dormant, eligible int64
		var first, last time.Time
		if rows.Scan(&organization, &department, &feature, &users, &events, &repeatUsers, &active7, &dormant, &eligible, &first, &last) != nil {
			continue
		}
		adoptionRate, repeatRate := float64(0), float64(0)
		if eligible > 0 {
			adoptionRate = float64(users) * 100 / float64(eligible)
		}
		if users > 0 {
			repeatRate = float64(repeatUsers) * 100 / float64(users)
		}
		out = append(out, map[string]any{"organization": organization, "department": department, "feature": feature, "users": users, "events": events, "eligible_users": eligible, "adoption_rate": adoptionRate, "repeat_users": repeatUsers, "repeat_usage_rate": repeatRate, "active_7d": active7, "dormant_users": dormant, "first_used": first, "last_used": last})
	}
	writeJSON(w, 200, map[string]any{"environment": requestEnvironment(r), "rows": out})
}

func (s *Server) experienceReport(w http.ResponseWriter, r *http.Request) {
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
	vitalRows, err := s.DB.Query(r.Context(), `SELECT coalesce(properties->>'metric','unknown'),coalesce(page_url,'(unknown)'),count(*),avg((properties->>'value')::numeric)::double precision,percentile_disc(.75) WITHIN GROUP(ORDER BY (properties->>'value')::numeric)::double precision,count(*) FILTER(WHERE properties->>'rating'='good')
		FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND event_name='web_vital' AND coalesce(properties->>'value','') ~ '^[0-9]+(\.[0-9]+)?$' GROUP BY 1,2 ORDER BY 1,5 DESC LIMIT 200`, siteID, from, to, environment)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	vitals := rowsToList(vitalRows, func() (map[string]any, error) {
		var metric, page string
		var samples, good int64
		var average, p75 float64
		err := vitalRows.Scan(&metric, &page, &samples, &average, &p75, &good)
		goodRate := float64(0)
		if samples > 0 {
			goodRate = float64(good) * 100 / float64(samples)
		}
		return map[string]any{"metric": metric, "page": page, "samples": samples, "average": average, "p75": p75, "good_rate": goodRate}, err
	})
	errorRows, err := s.DB.Query(r.Context(), `SELECT event_name,coalesce(properties->>'message',properties->>'resource','(unknown)'),coalesce(page_url,'(unknown)'),count(*),count(DISTINCT entity_id),max(event_timestamp)
		FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND event_name=ANY($5) GROUP BY 1,2,3 ORDER BY 4 DESC LIMIT 100`, siteID, from, to, environment, []string{"error", "resource_error"})
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	errorsOut := rowsToList(errorRows, func() (map[string]any, error) {
		var eventName, message, page string
		var count, users int64
		var lastSeen time.Time
		err := errorRows.Scan(&eventName, &message, &page, &count, &users, &lastSeen)
		return map[string]any{"event": eventName, "message": message, "page": page, "count": count, "affected_users": users, "last_seen": lastSeen}, err
	})
	var allUsers, errorUsers, conversionsWithError, conversionsWithoutError int64
	_ = s.DB.QueryRow(r.Context(), `WITH entity AS (SELECT entity_id,bool_or(event_name=ANY($5)) has_error,bool_or(is_conversion) converted FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 GROUP BY entity_id)
		SELECT count(*),count(*) FILTER(WHERE has_error),count(*) FILTER(WHERE has_error AND converted),count(*) FILTER(WHERE NOT has_error AND converted) FROM entity`, siteID, from, to, environment, []string{"error", "resource_error"}).Scan(&allUsers, &errorUsers, &conversionsWithError, &conversionsWithoutError)
	errorRate, cleanRate := float64(0), float64(0)
	if errorUsers > 0 {
		errorRate = float64(conversionsWithError) * 100 / float64(errorUsers)
	}
	if allUsers-errorUsers > 0 {
		cleanRate = float64(conversionsWithoutError) * 100 / float64(allUsers-errorUsers)
	}
	conversionDelta := float64(0)
	if errorUsers > 0 && allUsers-errorUsers > 0 {
		conversionDelta = errorRate - cleanRate
	}
	releaseRows, _ := s.DB.Query(r.Context(), `SELECT coalesce(properties->>'release_version',properties->>'app_version','(not set)'),count(*),count(DISTINCT entity_id),count(*) FILTER(WHERE event_name=ANY($5)),100.0*count(DISTINCT entity_id) FILTER(WHERE is_conversion)/nullif(count(DISTINCT entity_id),0),max(event_timestamp)
		FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 GROUP BY 1 ORDER BY 6 DESC LIMIT 50`, siteID, from, to, environment, []string{"error", "resource_error"})
	releases := []map[string]any{}
	if releaseRows != nil {
		defer releaseRows.Close()
		for releaseRows.Next() {
			var release string
			var events, users, errors int64
			var conversionRate float64
			var lastSeen time.Time
			if releaseRows.Scan(&release, &events, &users, &errors, &conversionRate, &lastSeen) == nil {
				releases = append(releases, map[string]any{"release": release, "events": events, "users": users, "errors": errors, "user_conversion_rate": conversionRate, "last_seen": lastSeen})
			}
		}
	}
	writeJSON(w, 200, map[string]any{"environment": environment, "vitals": vitals, "errors": errorsOut, "releases": releases, "impact": map[string]any{"users": allUsers, "error_users": errorUsers, "error_user_conversion_rate": errorRate, "clean_user_conversion_rate": cleanRate, "conversion_rate_delta": conversionDelta}})
}

func (s *Server) aiAnalyticsReport(w http.ResponseWriter, r *http.Request) {
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
	group := r.URL.Query().Get("group_by")
	allowed := map[string]bool{"model": true, "provider": true, "agent": true, "mcp_server": true, "tool": true}
	if !allowed[group] {
		group = "model"
	}
	rows, err := s.DB.Query(r.Context(), `SELECT coalesce(properties->>$5,'(not set)'),count(*),count(DISTINCT entity_id),100.0*count(*) FILTER(WHERE lower(coalesce(properties->>'success','true')) IN ('true','1'))/nullif(count(*),0),coalesce(avg(CASE WHEN coalesce(properties->>'latency_ms','') ~ '^[0-9]+(\.[0-9]+)?$' THEN (properties->>'latency_ms')::numeric END),0)::double precision,coalesce(sum(CASE WHEN coalesce(properties->>'input_tokens','') ~ '^[0-9]+$' THEN (properties->>'input_tokens')::bigint ELSE 0 END),0),coalesce(sum(CASE WHEN coalesce(properties->>'output_tokens','') ~ '^[0-9]+$' THEN (properties->>'output_tokens')::bigint ELSE 0 END),0),coalesce(sum(CASE WHEN coalesce(properties->>'cost','') ~ '^[0-9]+(\.[0-9]+)?$' THEN (properties->>'cost')::numeric ELSE 0 END),0)::double precision,count(*) FILTER(WHERE lower(coalesce(properties->>'fallback_model','')) NOT IN ('','false'))
		FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND event_name=ANY($6) GROUP BY 1 ORDER BY 2 DESC LIMIT 200`, siteID, from, to, requestEnvironment(r), group, []string{"ai_prompt", "ai_response", "ai_tool_call", "ai_agent_run", "ai_mcp_call", "ai_model_call"})
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var label string
		var calls, users, inputTokens, outputTokens, fallbacks int64
		var successRate, latency, cost float64
		if rows.Scan(&label, &calls, &users, &successRate, &latency, &inputTokens, &outputTokens, &cost, &fallbacks) == nil {
			out = append(out, map[string]any{"label": label, "calls": calls, "users": users, "success_rate": successRate, "average_latency_ms": latency, "input_tokens": inputTokens, "output_tokens": outputTokens, "cost": cost, "fallbacks": fallbacks})
		}
	}
	writeJSON(w, 200, map[string]any{"environment": requestEnvironment(r), "group_by": group, "rows": out})
}

type platformMetricValues struct {
	Users, Events, Conversions, Errors int64
	Revenue                            float64
}

func (s *Server) platformMetrics(r *http.Request, siteID uuid.UUID, environment string, from, to time.Time) (platformMetricValues, error) {
	var value platformMetricValues
	err := s.DB.QueryRow(r.Context(), `SELECT count(DISTINCT entity_id),count(*),count(*) FILTER(WHERE is_conversion),count(*) FILTER(WHERE event_name=ANY($5)),coalesce(sum(CASE WHEN event_name='purchase' AND coalesce(properties->>'value',properties->>'revenue','') ~ '^-?[0-9]+(\.[0-9]+)?$' THEN coalesce(properties->>'value',properties->>'revenue')::numeric ELSE 0 END),0)::double precision FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3`, siteID, from, to, environment, []string{"error", "resource_error"}).Scan(&value.Users, &value.Events, &value.Conversions, &value.Errors, &value.Revenue)
	return value, err
}

func percentChange(current, previous float64) float64 {
	if previous == 0 {
		if current == 0 {
			return 0
		}
		return 100
	}
	return (current - previous) * 100 / previous
}

func (s *Server) insightsReport(w http.ResponseWriter, r *http.Request) {
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
	_, location, _ := s.siteTimezone(r.Context(), siteID)
	previousFrom, previousTo := previousDateRange(from, to, location)
	environment := requestEnvironment(r)
	current, err := s.platformMetrics(r, siteID, environment, from, to)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	previous, _ := s.platformMetrics(r, siteID, environment, previousFrom, previousTo)
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
	insights := []map[string]any{}
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
		insights = append(insights, map[string]any{"metric": item.metric, "title": item.title, "severity": severity, "change_percent": change, "current": item.current, "previous": item.previous, "evidence": fmt.Sprintf("현재 %.2f, 이전 기간 %.2f", item.current, item.previous), "recommendation": item.recommendation})
	}
	sort.SliceStable(insights, func(i, j int) bool {
		return math.Abs(insights[i]["change_percent"].(float64)) > math.Abs(insights[j]["change_percent"].(float64))
	})
	writeJSON(w, 200, map[string]any{"engine": "offline-rule-v1", "environment": environment, "current": current, "previous": previous, "insights": insights})
}

func (s *Server) naturalLanguageAnalytics(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	var in struct {
		Question    string `json:"question"`
		Environment string `json:"environment"`
	}
	if err := decodeJSON(r, &in, 32<<10); err != nil || strings.TrimSpace(in.Question) == "" {
		writeError(w, 400, "INVALID_QUESTION", "question is required")
		return
	}
	if !environmentNamePattern.MatchString(in.Environment) {
		in.Environment = "prd"
	}
	_, location, _ := s.siteTimezone(r.Context(), siteID)
	now := time.Now().In(location)
	endLocal := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location).AddDate(0, 0, 1)
	startLocal := endLocal.AddDate(0, 0, -7)
	lower := strings.ToLower(in.Question)
	if strings.Contains(lower, "오늘") || strings.Contains(lower, "today") {
		startLocal = endLocal.AddDate(0, 0, -1)
	} else if strings.Contains(lower, "지난달") || strings.Contains(lower, "last month") {
		endLocal = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, location)
		startLocal = endLocal.AddDate(0, -1, 0)
	} else if strings.Contains(lower, "이번달") || strings.Contains(lower, "이번 달") || strings.Contains(lower, "this month") {
		startLocal = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, location)
	} else if strings.Contains(lower, "30일") || strings.Contains(lower, "30 days") {
		startLocal = endLocal.AddDate(0, 0, -30)
	}
	from, to := startLocal.UTC(), endLocal.UTC()
	intent, dimension := "summary", ""
	dimensions := []struct {
		key   string
		words []string
	}{
		{"feature", []string{"기능", "feature"}}, {"department", []string{"부서", "department"}}, {"organization", []string{"조직", "organization"}},
		{"page", []string{"페이지", "page"}}, {"model", []string{"모델", "model"}}, {"agent", []string{"에이전트", "agent"}}, {"tool", []string{"도구", "tool", "mcp"}},
	}
	for _, candidate := range dimensions {
		for _, word := range candidate.words {
			if strings.Contains(lower, word) {
				intent, dimension = "top_dimension", candidate.key
			}
		}
	}
	result := any(nil)
	answer := ""
	if intent == "summary" {
		metrics, queryErr := s.platformMetrics(r, siteID, in.Environment, from, to)
		if queryErr != nil {
			writeError(w, 500, "QUERY_FAILED", queryErr.Error())
			return
		}
		result = metrics
		answer = fmt.Sprintf("선택 기간의 사용자는 %d명, 이벤트는 %d건, 전환은 %d건, 오류는 %d건입니다.", metrics.Users, metrics.Events, metrics.Conversions, metrics.Errors)
	} else {
		expressions := map[string]string{"feature": "coalesce(properties->>'feature','(미지정)')", "department": "coalesce(canonical_user_properties->>'department','(미지정)')", "organization": "coalesce(canonical_user_properties->>'organization','(미지정)')", "page": "coalesce(page_url,'(미지정)')", "model": "coalesce(properties->>'model','(미지정)')", "agent": "coalesce(properties->>'agent','(미지정)')", "tool": "coalesce(properties->>'tool',properties->>'mcp_server','(미지정)')"}
		expression := expressions[dimension]
		rows, queryErr := s.DB.Query(r.Context(), `SELECT `+expression+`,count(*),count(DISTINCT entity_id) FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND `+expression+`<>'(미지정)' GROUP BY 1 ORDER BY 2 DESC LIMIT 10`, siteID, from, to, in.Environment)
		if queryErr != nil {
			writeError(w, 500, "QUERY_FAILED", queryErr.Error())
			return
		}
		items := rowsToList(rows, func() (map[string]any, error) {
			var label string
			var events, users int64
			err := rows.Scan(&label, &events, &users)
			return map[string]any{"label": label, "events": events, "users": users}, err
		})
		result = items
		if len(items) > 0 {
			answer = fmt.Sprintf("가장 많이 사용된 %s은(는) %s이며 %d건입니다.", dimension, items[0]["label"], items[0]["events"])
		} else {
			answer = "선택 기간에 조건과 일치하는 데이터가 없습니다."
		}
	}
	writeJSON(w, 200, map[string]any{"engine": "offline-semantic-v1", "question": in.Question, "intent": intent, "dimension": dimension, "environment": in.Environment, "from": from, "to": to, "answer": answer, "confidence": 0.85, "data": result})
}
