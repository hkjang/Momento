package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/auth"
	"github.com/hkjang/Momento/internal/insight"
)

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
		writeRangeError(w, err)
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
		writeRangeError(w, err)
		return
	}
	// The same numbers the scheduled digest sends, from one implementation: the
	// digest used to run its own query and answered with feature events under the
	// adoption report's name.
	rows, err := insight.New(s.DB).Adoption(r.Context(), siteID, requestEnvironment(r), from, to, 0)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{"organization": row.Organization, "department": row.Department, "feature": row.Feature,
			"users": row.Users, "events": row.Events, "eligible_users": row.EligibleUsers, "adoption_rate": row.AdoptionRate,
			"repeat_users": row.RepeatUsers, "repeat_usage_rate": row.RepeatUsageRate, "active_7d": row.Active7d,
			"dormant_users": row.DormantUsers, "first_used": row.FirstUsed, "last_used": row.LastUsed})
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
		writeRangeError(w, err)
		return
	}
	environment := requestEnvironment(r)
	// Four independent reads over the same window. Serially they were most of the
	// endpoint's cost before a single segment was compared.
	baseCtx, cancelBase := s.analyticalContext(r)
	defer cancelBase()
	var vitals, errorsOut, releases []map[string]any
	var summary insight.ExperienceSummary
	errorEvents := insight.ErrorEvents
	if err := insight.RunParallel(baseCtx, insight.QueryConcurrency,
		func(ctx context.Context) error {
			rows, err := s.DB.Query(ctx, `SELECT coalesce(properties->>'metric','unknown'),coalesce(page_url,'(unknown)'),count(*),avg((properties->>'value')::numeric)::double precision,percentile_disc(.75) WITHIN GROUP(ORDER BY (properties->>'value')::numeric)::double precision,count(*) FILTER(WHERE properties->>'rating'='good')
				FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND event_name='web_vital' AND coalesce(properties->>'value','') ~ '^[0-9]+(\.[0-9]+)?$' GROUP BY 1,2 ORDER BY 1,5 DESC LIMIT 200`, siteID, from, to, environment)
			if err != nil {
				return err
			}
			vitals = rowsToList(rows, func() (map[string]any, error) {
				var metric, page string
				var samples, good int64
				var average, p75 float64
				err := rows.Scan(&metric, &page, &samples, &average, &p75, &good)
				goodRate := float64(0)
				if samples > 0 {
					goodRate = float64(good) * 100 / float64(samples)
				}
				return map[string]any{"metric": metric, "page": page, "samples": samples, "average": average, "p75": p75, "good_rate": goodRate}, err
			})
			return nil
		},
		func(ctx context.Context) error {
			rows, err := s.DB.Query(ctx, `SELECT event_name,coalesce(properties->>'message',properties->>'resource','(unknown)'),coalesce(page_url,'(unknown)'),count(*),count(DISTINCT entity_id),max(event_timestamp)
				FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND event_name=ANY($5) GROUP BY 1,2,3 ORDER BY 4 DESC LIMIT 100`, siteID, from, to, environment, errorEvents)
			if err != nil {
				return err
			}
			errorsOut = rowsToList(rows, func() (map[string]any, error) {
				var eventName, message, page string
				var count, users int64
				var lastSeen time.Time
				err := rows.Scan(&eventName, &message, &page, &count, &users, &lastSeen)
				return map[string]any{"event": eventName, "message": message, "page": page, "count": count, "affected_users": users, "last_seen": lastSeen}, err
			})
			return nil
		},
		func(ctx context.Context) error {
			// The impact block, the MCP tool and the scheduled digest all state
			// what errors did to conversion; insight.Experience is where that is
			// worked out.
			value, err := insight.New(s.DB).ExperienceImpact(ctx, siteID, environment, from, to)
			summary = value
			return err
		},
		func(ctx context.Context) error {
			rows, err := s.DB.Query(ctx, `SELECT coalesce(properties->>'release_version',properties->>'app_version','(not set)'),count(*),count(DISTINCT entity_id),count(*) FILTER(WHERE event_name=ANY($5)),100.0*count(DISTINCT entity_id) FILTER(WHERE is_conversion)/nullif(count(DISTINCT entity_id),0),max(event_timestamp)
				FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 GROUP BY 1 ORDER BY 6 DESC LIMIT 50`, siteID, from, to, environment, errorEvents)
			if err != nil {
				return err
			}
			releases = rowsToList(rows, func() (map[string]any, error) {
				var release string
				var events, users, errors int64
				var conversionRate float64
				var lastSeen time.Time
				err := rows.Scan(&release, &events, &users, &errors, &conversionRate, &lastSeen)
				return map[string]any{"release": release, "events": events, "users": users, "errors": errors, "user_conversion_rate": conversionRate, "last_seen": lastSeen}, err
			})
			return nil
		}); err != nil {
		writeQueryError(w, err)
		return
	}
	response := map[string]any{"environment": environment, "vitals": vitals, "errors": errorsOut, "releases": releases,
		"impact": map[string]any{"users": summary.Users, "error_users": summary.ErrorUsers,
			"error_user_conversion_rate": summary.ErrorUserConversionRate, "clean_user_conversion_rate": summary.CleanUserConversionRate,
			"conversion_rate_delta": summary.ConversionRateDelta}}
	// Optional cohort comparison: the same measurements per segment, so a site wide
	// p75 stops hiding the group that is actually slow.
	compare := []string{}
	for _, id := range strings.Split(r.URL.Query().Get("segment_ids"), ",") {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			compare = append(compare, trimmed)
		}
	}
	if len(compare) > 3 {
		writeError(w, 400, "TOO_MANY_SEGMENTS", "segment_ids accepts at most 3 segments")
		return
	}
	if len(compare) > 0 {
		ctx, cancel := s.analyticalContext(r)
		defer cancel()
		resolver, resolverErr := s.newDimensionResolver(ctx, siteID, environment)
		if resolverErr != nil {
			writeQueryError(w, resolverErr)
			return
		}
		// The definitions are read first because an unknown segment is a request
		// error, not a query failure, and the two answer differently.
		definitions := make([]segmentNode, len(compare))
		names := make([]string, len(compare))
		for index, id := range compare {
			definition, segmentErr := s.loadSegment(ctx, siteID, id)
			if segmentErr != nil {
				writeError(w, 400, "INVALID_SEGMENT", segmentErr.Error())
				return
			}
			definitions[index] = definition
			names[index], _ = s.segmentName(ctx, siteID, id)
		}
		// The baseline and each cohort are the same measurements over different
		// people, so they are independent reads. Serially, three segments made this
		// the slowest endpoint in the product.
		var baseline experienceCohort
		cohorts := make([]experienceCohort, len(compare))
		steps := []func(context.Context) error{
			func(stepCtx context.Context) error {
				cohort, err := s.runExperienceCohort(stepCtx, siteID, environment, from, to, resolver, nil)
				if err != nil {
					return err
				}
				cohort.Key, cohort.Label = "baseline", "전체"
				baseline = cohort
				return nil
			},
		}
		for index := range compare {
			index := index
			steps = append(steps, func(stepCtx context.Context) error {
				cohort, err := s.runExperienceCohort(stepCtx, siteID, environment, from, to, resolver, &definitions[index])
				if err != nil {
					return err
				}
				cohort.Key, cohort.Label = compare[index], names[index]
				cohorts[index] = cohort
				return nil
			})
		}
		if err := insight.RunParallel(ctx, insight.QueryConcurrency, steps...); err != nil {
			writeQueryError(w, err)
			return
		}
		response["cohorts"] = append([]experienceCohort{baseline}, cohorts...)
		response["gaps"] = compareExperience(baseline, cohorts)
	}
	writeJSON(w, 200, response)
}

// aiOperationRows answers the AI operations report: one row per value of the
// chosen dimension, with what it was called, by how many people, how often it
// worked, how long it took, what it consumed and what it cost.
//
// The MCP tool that answers the same question had its own query, and that query
// stopped at the token counts — an agent asked what the AI usage cost got a
// report with no cost in it, and no success rate either, while the screen beside
// it had both. One definition now, so the two cannot answer differently or
// answer different amounts.
// aiOperationRows is the AI operations report. The query lives in insight
// because the screen, the MCP tool and the scheduled digest all ask for it.
func (s *Server) aiOperationRows(ctx context.Context, siteID uuid.UUID, environment, group string, from, to time.Time) ([]map[string]any, error) {
	return insight.New(s.DB).AIOperations(ctx, siteID, environment, group, from, to)
}

// aiOperationDimension is the cut the caller asked for, or the default.
func aiOperationDimension(group string) string { return insight.AIOperationDimension(group) }

func (s *Server) aiAnalyticsReport(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeRangeError(w, err)
		return
	}
	group := aiOperationDimension(r.URL.Query().Get("group_by"))
	environment := requestEnvironment(r)
	out, err := s.aiOperationRows(r.Context(), siteID, environment, group, from, to)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"environment": environment, "group_by": group, "rows": out})
}

type platformMetricValues struct {
	Users, Events, Conversions, Errors int64
	Revenue                            float64
}

func (s *Server) platformMetrics(r *http.Request, siteID uuid.UUID, environment string, from, to time.Time) (platformMetricValues, error) {
	var value platformMetricValues
	err := s.DB.QueryRow(r.Context(), `SELECT count(DISTINCT entity_id),count(*),count(*) FILTER(WHERE is_conversion),count(*) FILTER(WHERE event_name=ANY($5)),`+insight.RevenueAmountSQL("")+`::double precision FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3`, siteID, from, to, environment, []string{"error", "resource_error"}).Scan(&value.Users, &value.Events, &value.Conversions, &value.Errors, &value.Revenue)
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
		writeRangeError(w, err)
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
