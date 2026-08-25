package httpapi

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/auth"
)

type queryPlan struct {
	Mode               string  `json:"mode"`
	ComplexityScore    int     `json:"complexity_score"`
	SamplePercent      float64 `json:"sample_percent"`
	Execution          string  `json:"execution"`
	Exact              bool    `json:"exact"`
	EstimatedErrorRate float64 `json:"estimated_error_percent,omitempty"`
}

// queryPolicy is the cost guard a site runs under.
type queryPolicy struct {
	MaxExactDays         int     `json:"max_exact_days"`
	MaxComplexityScore   int     `json:"max_complexity_score"`
	BackgroundThreshold  int     `json:"background_threshold"`
	FastSamplePercent    float64 `json:"fast_sample_percent"`
	PreviewSamplePercent float64 `json:"preview_sample_percent"`
	Defaults             bool    `json:"defaults"`
}

// defaultQueryPolicy is what a site runs under before an administrator sets one.
// The guard and the administration screen read it from here so they can never
// disagree about what limits are actually in force.
func defaultQueryPolicy() queryPolicy {
	return queryPolicy{MaxExactDays: 180, MaxComplexityScore: 90, BackgroundThreshold: 60, FastSamplePercent: 10, PreviewSamplePercent: 1, Defaults: true}
}

// loadQueryPolicy reads a site's policy, falling back to the defaults when none has
// been stored yet.
func (s *Server) loadQueryPolicy(ctx context.Context, siteID uuid.UUID) queryPolicy {
	policy := defaultQueryPolicy()
	var stored queryPolicy
	if err := s.DB.QueryRow(ctx, `SELECT max_exact_days,max_complexity_score,background_threshold,fast_sample_percent,preview_sample_percent FROM query_policies WHERE site_id=$1`, siteID).
		Scan(&stored.MaxExactDays, &stored.MaxComplexityScore, &stored.BackgroundThreshold, &stored.FastSamplePercent, &stored.PreviewSamplePercent); err != nil {
		return policy
	}
	stored.Defaults = false
	return stored
}

func (s *Server) planAnalyticsQuery(ctx context.Context, siteID uuid.UUID, in queryRequest, from, to time.Time) (queryPlan, string) {
	policy := s.loadQueryPolicy(ctx, siteID)
	maxExactDays, maxScore, backgroundAt := policy.MaxExactDays, policy.MaxComplexityScore, policy.BackgroundThreshold
	fastPercent, previewPercent := policy.FastSamplePercent, policy.PreviewSamplePercent
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = "exact"
	}
	if mode != "exact" && mode != "fast" && mode != "preview" {
		return queryPlan{}, "query mode must be exact, fast, or preview"
	}
	days := int(math.Ceil(to.Sub(from).Hours() / 24))
	score := 5 + len(in.Dimensions)*5 + len(in.Filters)*4
	if days > 31 {
		score += 10
	}
	if days > 180 {
		score += 20
	}
	if days > 365 {
		score += 20
	}
	if in.SegmentID != "" || in.Segment != nil {
		score += 15
	}
	for _, dimension := range in.Dimensions {
		if strings.HasPrefix(dimension, "event.") || strings.HasPrefix(dimension, "user.") || strings.HasPrefix(dimension, "session.") {
			score += 5
		}
	}
	if mode == "exact" {
		score += 10
	}
	plan := queryPlan{Mode: mode, ComplexityScore: score, SamplePercent: 100, Execution: "interactive", Exact: mode == "exact"}
	if mode == "fast" {
		plan.SamplePercent = fastPercent
	}
	if mode == "preview" {
		plan.SamplePercent = previewPercent
	}
	if plan.SamplePercent < 100 {
		p := plan.SamplePercent / 100
		plan.EstimatedErrorRate = math.Round(1.96*math.Sqrt((1-p)/(10000*p))*10000) / 100
	}
	if mode == "exact" && days > maxExactDays {
		return plan, "exact mode exceeds the site's maximum exact date range; use fast mode or reduce the range"
	}
	if score > maxScore {
		return plan, "query complexity exceeds the site policy; reduce dimensions, filters, or date range"
	}
	if score >= backgroundAt {
		plan.Execution = "guarded_interactive"
	}
	return plan, ""
}

func (s *Server) createQueryAudit(ctx context.Context, siteID uuid.UUID, environment string, in queryRequest, from, to time.Time, plan queryPlan, status, message string) int64 {
	p, _ := auth.FromContext(ctx)
	var actor any
	if p.ID != uuid.Nil {
		actor = p.ID
	}
	var id int64
	_ = s.DB.QueryRow(ctx, `INSERT INTO query_audit(site_id,actor_id,environment,query_mode,complexity_score,sample_percent,date_from,date_to,dimensions,metrics,status,error)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,nullif($12,'')) RETURNING id`, siteID, actor, environment, plan.Mode, plan.ComplexityScore, plan.SamplePercent, from, to, in.Dimensions, in.Metrics, status, message).Scan(&id)
	return id
}

func (s *Server) finishQueryAudit(ctx context.Context, id int64, started time.Time, rows int, status, message string) {
	if id == 0 {
		return
	}
	duration := int(time.Since(started).Milliseconds())
	_, _ = s.DB.Exec(ctx, `UPDATE query_audit SET duration_ms=$2,result_rows=$3,status=$4,error=nullif($5,'') WHERE id=$1`, id, duration, rows, status, message)
}
