package httpapi

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/insight"
)

// Retention answers whether people come back. A single pooled curve says "38% return
// in week one", which is only actionable once you can see that one department returns
// at 62% and another at 9%. The comparison therefore runs the same cohort definition
// per segment and names the period where a group falls behind.

type cohortParams struct {
	From, To    time.Time
	Environment string
	Timezone    string
	Granularity string
	Periods     int
	CohortEvent string
	ReturnEvent string
}

// cohortBuckets returns the date bucket and the period offset for a granularity.
func cohortBuckets(granularity string) (string, string, string) {
	switch granularity {
	case "day":
		return "day", "(e.event_timestamp AT TIME ZONE $5)::date", "(a.activity_date-c.cohort_date)"
	case "month":
		return "month", "date_trunc('month',e.event_timestamp AT TIME ZONE $5)::date",
			"((extract(year FROM a.activity_date)::int-extract(year FROM c.cohort_date)::int)*12+extract(month FROM a.activity_date)::int-extract(month FROM c.cohort_date)::int)"
	default:
		return "week", "date_trunc('week',e.event_timestamp AT TIME ZONE $5)::date", "((a.activity_date-c.cohort_date)/7)"
	}
}

// runCohortGrid evaluates the cohort grid for one population.
func (s *Server) runCohortGrid(ctx context.Context, siteID uuid.UUID, params cohortParams, resolver dimensionResolver, segment *segmentNode) ([]map[string]any, error) {
	_, bucket, offset := cohortBuckets(params.Granularity)
	args := []any{siteID, params.From, params.To, params.Environment, params.Timezone, params.CohortEvent, params.ReturnEvent, params.Periods - 1}
	predicate := ""
	if segment != nil {
		part, err := compileSegment(*segment, resolver, "e", &args, 0)
		if err != nil {
			return nil, err
		}
		// The same predicate scopes both the cohort definition and the return
		// activity, so a segment never retains people it never counted.
		predicate = " AND (" + part + ")"
	}
	query := `WITH cohort_candidates AS (
		SELECT e.entity_id,min(` + bucket + `) cohort_date FROM analytics_events e
		WHERE e.site_id=$1 AND e.environment=$4 AND ($6='' OR e.event_name=$6)` + predicate + ` GROUP BY e.entity_id
	), cohorts AS (
		SELECT entity_id,cohort_date FROM cohort_candidates WHERE cohort_date >= ($2 AT TIME ZONE $5)::date AND cohort_date < ($3 AT TIME ZONE $5)::date
	), activity AS (
		SELECT DISTINCT e.entity_id,` + bucket + ` activity_date FROM analytics_events e
		WHERE e.site_id=$1 AND e.environment=$4 AND e.event_timestamp >= $2 AND e.event_timestamp < $3 AND ($7='' OR e.event_name=$7)` + predicate + `
	), retained AS (
		SELECT c.cohort_date,` + offset + ` period_no,count(DISTINCT c.entity_id) retained
		FROM cohorts c JOIN activity a ON a.entity_id=c.entity_id AND a.activity_date>=c.cohort_date
		GROUP BY c.cohort_date,period_no
	), sizes AS (SELECT cohort_date,count(*) cohort_size FROM cohorts GROUP BY cohort_date)
	SELECT s.cohort_date,s.cohort_size,r.period_no,r.retained FROM sizes s JOIN retained r USING(cohort_date)
	WHERE r.period_no BETWEEN 0 AND $8 ORDER BY s.cohort_date,r.period_no`
	rows, err := s.DB.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type cohort struct {
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
			cohorts[key] = &cohort{size: size, ret: map[int]int64{}}
			order = append(order, key)
		}
		cohorts[key].ret[period] = retained
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, key := range order {
		item := cohorts[key]
		values := make([]map[string]any, params.Periods)
		for period := 0; period < params.Periods; period++ {
			count := item.ret[period]
			values[period] = map[string]any{"period": period, "users": count, "retention_rate": percent(count, item.size)}
		}
		out = append(out, map[string]any{"cohort": key, "size": item.size, "periods": values})
	}
	return out, nil
}

// cohortMaturity is how many complete periods a cohort has had the chance to live
// through by the end of the window. A cohort that started last week cannot have a
// week four number, and counting it as zero there would understate retention.
func cohortMaturity(cohortDate, windowEnd time.Time, granularity string) int {
	last := windowEnd.AddDate(0, 0, -1)
	switch granularity {
	case "day":
		return int(last.Sub(cohortDate).Hours() / 24)
	case "month":
		months := (last.Year()-cohortDate.Year())*12 + int(last.Month()) - int(cohortDate.Month())
		if last.Day() < cohortDate.Day() {
			months--
		}
		if months < 0 {
			return 0
		}
		return months
	default:
		return int(last.Sub(cohortDate).Hours() / 24 / 7)
	}
}

// pooledRetentionCurve averages the grid into one curve, weighting each cohort by
// its size. A simple mean of rates would let a three person cohort outvote a
// thousand person one, and a cohort only enters a period it is old enough to have
// reached.
func pooledRetentionCurve(grid []map[string]any, periods int, windowEnd time.Time, granularity string) (int64, []map[string]any) {
	totalSize := int64(0)
	retained := make([]int64, periods)
	exposed := make([]int64, periods)
	for _, cohort := range grid {
		size, _ := cohort["size"].(int64)
		totalSize += size
		maturity := periods - 1
		if date, ok := cohort["cohort"].(string); ok {
			if parsed, err := time.Parse("2006-01-02", date); err == nil {
				maturity = cohortMaturity(parsed, windowEnd, granularity)
			}
		}
		values, _ := cohort["periods"].([]map[string]any)
		for index, value := range values {
			if index >= periods || index > maturity {
				break
			}
			users, _ := value["users"].(int64)
			exposed[index] += size
			retained[index] += users
		}
	}
	curve := make([]map[string]any, periods)
	for period := 0; period < periods; period++ {
		curve[period] = map[string]any{
			"period": period, "users": retained[period],
			"retention_rate": percent(retained[period], exposed[period]),
			"cohort_users":   exposed[period],
		}
	}
	return totalSize, curve
}

// retentionComparison states how one segment's retention differs from the baseline.
type retentionComparison struct {
	Key               string  `json:"key"`
	Label             string  `json:"label"`
	CohortUsers       int64   `json:"cohort_users"`
	FirstReturnRate   float64 `json:"first_return_rate"`
	BaselineFirstRate float64 `json:"baseline_first_return_rate"`
	FinalRate         float64 `json:"final_retention_rate"`
	BaselineFinalRate float64 `json:"baseline_final_retention_rate"`
	FirstReturnGap    float64 `json:"first_return_gap_points"`
	WorstPeriod       int     `json:"worst_period"`
	WorstPeriodGap    float64 `json:"worst_period_gap_points"`
	Verdict           string  `json:"verdict"`
	Evidence          string  `json:"evidence"`
	Reliable          bool    `json:"reliable"`
}

// minimumCohortUsers keeps a handful of people from producing a retention verdict.
const minimumCohortUsers = 20

func curveRate(curve []map[string]any, period int) float64 {
	if period < 0 || period >= len(curve) {
		return 0
	}
	value, _ := curve[period]["retention_rate"].(float64)
	return value
}

// compareRetentionCurves measures each segment curve against the baseline curve.
func compareRetentionCurves(baseline []map[string]any, segments []map[string]any, granularity string) []retentionComparison {
	out := []retentionComparison{}
	if len(baseline) == 0 {
		return out
	}
	finalPeriod := len(baseline) - 1
	for _, item := range segments {
		curve, _ := item["periods"].([]map[string]any)
		if len(curve) == 0 {
			continue
		}
		key, _ := item["key"].(string)
		label, _ := item["label"].(string)
		users, _ := item["cohort_users"].(int64)
		comparison := retentionComparison{
			Key: key, Label: label, CohortUsers: users,
			FirstReturnRate:   curveRate(curve, 1),
			BaselineFirstRate: curveRate(baseline, 1),
			FinalRate:         curveRate(curve, finalPeriod),
			BaselineFinalRate: curveRate(baseline, finalPeriod),
		}
		comparison.FirstReturnGap = comparison.FirstReturnRate - comparison.BaselineFirstRate
		comparison.Reliable = users >= minimumCohortUsers
		worst := 0.0
		for period := 1; period < len(curve) && period < len(baseline); period++ {
			gap := curveRate(curve, period) - curveRate(baseline, period)
			if gap < worst {
				worst, comparison.WorstPeriod, comparison.WorstPeriodGap = gap, period, gap
			}
		}
		switch {
		case !comparison.Reliable:
			comparison.Verdict = "insufficient"
		case comparison.FirstReturnGap >= 5:
			comparison.Verdict = "better"
		case comparison.FirstReturnGap <= -5:
			comparison.Verdict = "worse"
		default:
			comparison.Verdict = "similar"
		}
		comparison.Evidence = retentionEvidence(comparison, granularity)
		out = append(out, comparison)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return math.Abs(out[i].FirstReturnGap) > math.Abs(out[j].FirstReturnGap)
	})
	return out
}

func periodUnit(granularity string) string {
	switch granularity {
	case "day":
		return "일"
	case "month":
		return "개월"
	default:
		return "주"
	}
}

func retentionEvidence(comparison retentionComparison, granularity string) string {
	unit := periodUnit(granularity)
	if !comparison.Reliable {
		return fmt.Sprintf("Cohort 인원 %d명으로 표본이 적어 판정하지 않았습니다. 기간을 넓히거나 조건을 완화하십시오.", comparison.CohortUsers)
	}
	direction := "높습니다"
	if comparison.FirstReturnGap < 0 {
		direction = "낮습니다"
	}
	evidence := fmt.Sprintf("Cohort %d명 · 첫 재방문(%d%s) %.1f%%로 전체(%.1f%%)보다 %.1fpp %s",
		comparison.CohortUsers, 1, unit, comparison.FirstReturnRate, comparison.BaselineFirstRate, math.Abs(comparison.FirstReturnGap), direction)
	if comparison.WorstPeriod > 0 {
		evidence += fmt.Sprintf(". 격차가 가장 큰 시점은 %d%s차(%.1fpp)입니다", comparison.WorstPeriod, unit, comparison.WorstPeriodGap)
	}
	return evidence
}

// cohortReport returns the retention grid, and when segments are given, one pooled
// curve per segment beside the baseline with a comparison.
func (s *Server) cohortReport(w http.ResponseWriter, r *http.Request) {
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
	timezone, _, err := s.siteTimezone(r.Context(), siteID)
	if err != nil {
		writeError(w, 500, "INVALID_TIMEZONE", err.Error())
		return
	}
	periods, _ := strconv.Atoi(r.URL.Query().Get("periods"))
	if periods < 1 || periods > 52 {
		periods = 12
	}
	granularity, _, _ := cohortBuckets(r.URL.Query().Get("granularity"))
	params := cohortParams{
		From: from, To: to, Environment: requestEnvironment(r), Timezone: timezone,
		Granularity: granularity, Periods: periods,
		CohortEvent: strings.TrimSpace(r.URL.Query().Get("cohort_event")),
		ReturnEvent: strings.TrimSpace(r.URL.Query().Get("return_event")),
	}
	resolver, err := s.newDimensionResolver(r.Context(), siteID, requestEnvironment(r))
	if err != nil {
		writeQueryError(w, err)
		return
	}
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
	ctx, cancel := s.analyticalContext(r)
	defer cancel()
	// The definitions are read before anything runs: an unknown segment is a
	// request error, and it must not be reported as a query failure.
	definitions := make([]segmentNode, len(compare))
	names := make([]string, len(compare))
	for index, id := range compare {
		definition, err := s.loadSegment(ctx, siteID, id)
		if err != nil {
			writeError(w, 400, "INVALID_SEGMENT", err.Error())
			return
		}
		definitions[index] = definition
		names[index], _ = s.segmentName(ctx, siteID, id)
	}
	// The baseline grid and each segment's grid are the same measurement over
	// different people, so they run together rather than one after another.
	var baselineGrid []map[string]any
	grids := make([][]map[string]any, len(compare))
	steps := []func(context.Context) error{
		func(stepCtx context.Context) error {
			grid, err := s.runCohortGrid(stepCtx, siteID, params, resolver, nil)
			baselineGrid = grid
			return err
		},
	}
	for index := range compare {
		index := index
		steps = append(steps, func(stepCtx context.Context) error {
			grid, err := s.runCohortGrid(stepCtx, siteID, params, resolver, &definitions[index])
			grids[index] = grid
			return err
		})
	}
	if err := insight.RunParallel(ctx, insight.QueryConcurrency, steps...); err != nil {
		writeQueryError(w, err)
		return
	}
	response := map[string]any{
		"granularity": params.Granularity, "cohort_event": params.CohortEvent, "return_event": params.ReturnEvent,
		"environment": params.Environment, "cohorts": baselineGrid,
	}
	if len(compare) == 0 {
		writeJSON(w, 200, response)
		return
	}
	baselineUsers, baselineCurve := pooledRetentionCurve(baselineGrid, periods, params.To, params.Granularity)
	curves := []map[string]any{{"key": "baseline", "label": "전체", "cohort_users": baselineUsers, "periods": baselineCurve}}
	segments := []map[string]any{}
	for index, id := range compare {
		users, curve := pooledRetentionCurve(grids[index], periods, params.To, params.Granularity)
		entry := map[string]any{"key": id, "label": names[index], "cohort_users": users, "periods": curve}
		curves = append(curves, entry)
		segments = append(segments, entry)
	}
	response["curves"] = curves
	response["comparison"] = compareRetentionCurves(baselineCurve, segments, params.Granularity)
	writeJSON(w, 200, response)
}
