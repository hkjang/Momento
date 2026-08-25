package httpapi

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// Performance and errors are where cohorts differ most: the same page is fast on a
// desktop inside the office network and slow on a phone over VPN. A single site wide
// p75 averages those together and hides both.

type experienceVital struct {
	Metric    string  `json:"metric"`
	Samples   int64   `json:"samples"`
	P75       float64 `json:"p75"`
	GoodRate  float64 `json:"good_rate"`
	Threshold float64 `json:"good_threshold"`
}

type experienceCohort struct {
	Key           string            `json:"key"`
	Label         string            `json:"label"`
	Users         int64             `json:"users"`
	ErrorUsers    int64             `json:"error_users"`
	ErrorUserRate float64           `json:"error_user_rate"`
	Vitals        []experienceVital `json:"vitals"`
}

// coreWebVitalThresholds are the published "good" boundaries, kept here so the
// report can say whether a cohort is merely slower or actually failing the bar.
var coreWebVitalThresholds = map[string]float64{
	"LCP":  2500,
	"INP":  200,
	"CLS":  0.1,
	"FCP":  1800,
	"TTFB": 800,
}

func vitalThreshold(metric string) float64 {
	return coreWebVitalThresholds[strings.ToUpper(strings.TrimSpace(metric))]
}

// runExperienceCohort measures one population's vitals and error exposure.
func (s *Server) runExperienceCohort(ctx context.Context, siteID uuid.UUID, environment string, from, to any, resolver dimensionResolver, segment *segmentNode) (experienceCohort, error) {
	cohort := experienceCohort{Vitals: []experienceVital{}}
	args := []any{siteID, from, to, environment, []string{"error", "resource_error"}}
	predicate := ""
	if segment != nil {
		part, err := compileSegment(*segment, resolver, "e", &args, 0)
		if err != nil {
			return cohort, err
		}
		predicate = " AND (" + part + ")"
	}
	query := `WITH base AS (
		SELECT e.* FROM analytics_events e
		WHERE e.site_id=$1 AND e.environment=$4 AND e.event_timestamp >= $2 AND e.event_timestamp < $3` + predicate + `
	), summary AS (
		SELECT count(DISTINCT entity_id) users,count(DISTINCT entity_id) FILTER(WHERE event_name = ANY($5)) error_users FROM base
	), vitals AS (
		SELECT coalesce(properties->>'metric','unknown') metric,count(*) samples,
			percentile_disc(.75) WITHIN GROUP(ORDER BY (properties->>'value')::numeric)::double precision p75,
			count(*) FILTER(WHERE properties->>'rating'='good') good
		FROM base WHERE event_name='web_vital' AND coalesce(properties->>'value','') ~ '^[0-9]+(\.[0-9]+)?$'
		GROUP BY 1
	)
	SELECT 'summary' kind,'' metric,(SELECT users FROM summary),(SELECT error_users FROM summary),0::double precision,0::bigint
	UNION ALL
	SELECT 'vital',metric,samples,0::bigint,p75,good FROM vitals`
	rows, err := s.DB.Query(ctx, query, args...)
	if err != nil {
		return cohort, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, metric string
		var first, second, good int64
		var p75 float64
		if rows.Scan(&kind, &metric, &first, &second, &p75, &good) != nil {
			continue
		}
		if kind == "summary" {
			cohort.Users, cohort.ErrorUsers = first, second
			cohort.ErrorUserRate = percent(second, first)
			continue
		}
		cohort.Vitals = append(cohort.Vitals, experienceVital{
			Metric: metric, Samples: first, P75: p75,
			GoodRate: percent(good, first), Threshold: vitalThreshold(metric),
		})
	}
	sort.Slice(cohort.Vitals, func(i, j int) bool { return cohort.Vitals[i].Metric < cohort.Vitals[j].Metric })
	return cohort, rows.Err()
}

// experienceGap is one difference worth acting on.
type experienceGap struct {
	Key      string  `json:"key"`
	Label    string  `json:"label"`
	Kind     string  `json:"kind"` // vital, error
	Metric   string  `json:"metric,omitempty"`
	Severity string  `json:"severity"`
	Impact   float64 `json:"impact"`
	Evidence string  `json:"evidence"`
	Action   string  `json:"action"`
}

const (
	// minimumExperienceSamples keeps a handful of measurements from becoming a claim.
	minimumExperienceSamples = 20
	// vitalRegressionRatio is how much slower a cohort must be before it is reported.
	vitalRegressionRatio = 1.3
	// errorRateGapPoints is the error exposure difference worth naming.
	errorRateGapPoints = 5
)

func findVital(vitals []experienceVital, metric string) (experienceVital, bool) {
	for _, vital := range vitals {
		if vital.Metric == metric {
			return vital, true
		}
	}
	return experienceVital{}, false
}

// compareExperience reports where a cohort's experience is materially worse than the
// baseline. A cohort that is merely a little slower is not a finding.
func compareExperience(baseline experienceCohort, cohorts []experienceCohort) []experienceGap {
	gaps := []experienceGap{}
	for _, cohort := range cohorts {
		if cohort.Users >= minimumExperienceSamples && baseline.ErrorUserRate >= 0 {
			gap := cohort.ErrorUserRate - baseline.ErrorUserRate
			if gap >= errorRateGapPoints {
				severity := "warning"
				if gap >= 15 {
					severity = "critical"
				}
				gaps = append(gaps, experienceGap{
					Key: cohort.Key, Label: cohort.Label, Kind: "error", Severity: severity, Impact: gap,
					Evidence: fmt.Sprintf("사용자 %d명 중 %.1f%%가 오류를 경험했습니다. 전체는 %.1f%%로 %.1fpp 차이입니다.",
						cohort.Users, cohort.ErrorUserRate, baseline.ErrorUserRate, gap),
					Action: "Experience의 오류 목록을 이 조건으로 좁혀 어떤 메시지와 페이지에서 발생하는지 확인하십시오.",
				})
			}
		}
		for _, vital := range cohort.Vitals {
			base, ok := findVital(baseline.Vitals, vital.Metric)
			if !ok || vital.Samples < minimumExperienceSamples || base.P75 <= 0 {
				continue
			}
			ratio := vital.P75 / base.P75
			if ratio < vitalRegressionRatio {
				continue
			}
			severity := "warning"
			if threshold := vital.Threshold; threshold > 0 && vital.P75 > threshold && base.P75 <= threshold {
				// Crossing the published bar while the baseline stays inside it is the
				// difference between "slower" and "no longer acceptable".
				severity = "critical"
			}
			gaps = append(gaps, experienceGap{
				Key: cohort.Key, Label: cohort.Label, Kind: "vital", Metric: vital.Metric, Severity: severity,
				Impact: (ratio - 1) * 100,
				Evidence: fmt.Sprintf("%s p75가 %.0f로 전체(%.0f)보다 %.0f%% 느립니다. 표본 %d건%s",
					vital.Metric, vital.P75, base.P75, (ratio-1)*100, vital.Samples, thresholdNote(vital)),
				Action: "이 조건의 사용자가 보는 페이지와 네트워크를 확인하고, 해당 구간의 자원 크기와 응답 시간을 점검하십시오.",
			})
		}
	}
	sort.SliceStable(gaps, func(i, j int) bool {
		if gaps[i].Severity != gaps[j].Severity {
			return gaps[i].Severity == "critical"
		}
		return math.Abs(gaps[i].Impact) > math.Abs(gaps[j].Impact)
	})
	return gaps
}

func thresholdNote(vital experienceVital) string {
	if vital.Threshold <= 0 {
		return "."
	}
	if vital.P75 > vital.Threshold {
		return fmt.Sprintf(", 권장 기준 %.0f를 초과합니다.", vital.Threshold)
	}
	return fmt.Sprintf(", 권장 기준 %.0f 이내입니다.", vital.Threshold)
}
