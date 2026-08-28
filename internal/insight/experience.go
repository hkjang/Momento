package insight

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ErrorEvents are the two events that count as something going wrong in front of
// a person.
var ErrorEvents = []string{"error", "resource_error"}

// ExperienceSummary is the experience report reduced to the figures that answer
// "is the site working for people?" — the three Core Web Vitals at the 75th
// percentile, how many errors happened and to how many people, and what those
// errors did to conversion.
//
// It exists because three places asked the question and two of them answered
// less. The MCP tool computed the vitals and the error counts but not the
// conversion impact; the scheduled digest, which is named after the experience
// screen, carried an error count and an affected-user count and nothing else —
// no Web Vitals at all, on a screen whose subject is Web Vitals.
//
// The impact figures are the reason to send the digest: an error count alone
// does not say whether it mattered, and a mailed report has no screen beside it
// to be checked against.
type ExperienceSummary struct {
	P75                     map[string]float64 `json:"p75"`
	Errors                  int64              `json:"errors"`
	AffectedUsers           int64              `json:"affected_users"`
	Users                   int64              `json:"users"`
	ErrorUsers              int64              `json:"error_users"`
	ErrorUserConversionRate float64            `json:"error_user_conversion_rate"`
	CleanUserConversionRate float64            `json:"clean_user_conversion_rate"`
	ConversionRateDelta     float64            `json:"conversion_rate_delta"`
}

// Experience reads the summary. The two halves are independent, so they run
// together.
func (r Reporter) Experience(ctx context.Context, siteID uuid.UUID, environment string, from, to time.Time) (ExperienceSummary, error) {
	var summary ExperienceSummary
	var impact ExperienceSummary
	var lcp, inp, cls float64
	err := RunParallel(ctx, QueryConcurrency,
		func(stepCtx context.Context) error {
			return r.DB.QueryRow(stepCtx, `SELECT count(*) FILTER(WHERE event_name=ANY($5)),count(DISTINCT entity_id) FILTER(WHERE event_name=ANY($5)),`+
				vitalP75("LCP")+`,`+vitalP75("INP")+`,`+vitalP75("CLS")+`
				FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3`,
				siteID, from, to, environment, ErrorEvents).Scan(&summary.Errors, &summary.AffectedUsers, &lcp, &inp, &cls)
		},
		func(stepCtx context.Context) error {
			value, err := r.ExperienceImpact(stepCtx, siteID, environment, from, to)
			impact = value
			return err
		})
	if err != nil {
		return ExperienceSummary{P75: map[string]float64{}}, err
	}
	impact.Errors, impact.AffectedUsers = summary.Errors, summary.AffectedUsers
	impact.P75 = map[string]float64{"LCP": lcp, "INP": inp, "CLS": cls}
	return impact, nil
}

// ExperienceImpact is what the errors did to conversion, and nothing else. The
// experience screen reads only this half: it draws its own per-page vitals table
// and its own grouped error list, and asking for the site-wide figures beside
// them would be a query whose answer the screen never prints.
func (r Reporter) ExperienceImpact(ctx context.Context, siteID uuid.UUID, environment string, from, to time.Time) (ExperienceSummary, error) {
	summary := ExperienceSummary{P75: map[string]float64{}}
	var conversionsWithError, conversionsWithoutError int64
	if err := r.DB.QueryRow(ctx, `WITH entity AS (SELECT entity_id,bool_or(event_name=ANY($5)) has_error,bool_or(is_conversion) converted
		FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 GROUP BY entity_id)
		SELECT count(*),count(*) FILTER(WHERE has_error),count(*) FILTER(WHERE has_error AND converted),count(*) FILTER(WHERE NOT has_error AND converted) FROM entity`,
		siteID, from, to, environment, ErrorEvents).
		Scan(&summary.Users, &summary.ErrorUsers, &conversionsWithError, &conversionsWithoutError); err != nil {
		return ExperienceSummary{P75: map[string]float64{}}, err
	}
	if summary.ErrorUsers > 0 {
		summary.ErrorUserConversionRate = percent(conversionsWithError, summary.ErrorUsers)
	}
	if clean := summary.Users - summary.ErrorUsers; clean > 0 {
		summary.CleanUserConversionRate = percent(conversionsWithoutError, clean)
	}
	// The difference is stated only when both populations exist. With nobody on
	// one side of it, a delta is the other side's rate wearing a comparison.
	if summary.ErrorUsers > 0 && summary.Users-summary.ErrorUsers > 0 {
		summary.ConversionRateDelta = summary.ErrorUserConversionRate - summary.CleanUserConversionRate
	}
	return summary, nil
}

// vitalP75 is the 75th percentile of one Web Vital, ignoring values that are not
// numbers: the field is whatever the page put there.
func vitalP75(metric string) string {
	return `coalesce(percentile_disc(.75) WITHIN GROUP(ORDER BY (properties->>'value')::numeric) FILTER(WHERE event_name='web_vital' AND properties->>'metric'='` + metric +
		`' AND coalesce(properties->>'value','')~'^[0-9]+(\.[0-9]+)?$'),0)::double precision`
}
