package insight

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AdoptionRow is one feature's uptake in one part of the organisation.
//
// It lives here rather than in the report handler because two callers need the
// same numbers: the adoption screen and the scheduled digest named after it. The
// digest used to run its own query and answered with a feature event list — the
// feature intelligence report's content under the adoption report's name, with
// no adoption rate in it at all.
type AdoptionRow struct {
	Organization    string    `json:"organization"`
	Department      string    `json:"department"`
	Feature         string    `json:"feature"`
	Users           int64     `json:"users"`
	Events          int64     `json:"events"`
	EligibleUsers   int64     `json:"eligible_users"`
	AdoptionRate    float64   `json:"adoption_rate"`
	RepeatUsers     int64     `json:"repeat_users"`
	RepeatUsageRate float64   `json:"repeat_usage_rate"`
	Active7d        int64     `json:"active_7d"`
	DormantUsers    int64     `json:"dormant_users"`
	FirstUsed       time.Time `json:"first_used"`
	LastUsed        time.Time `json:"last_used"`
}

// adoptionSQL measures uptake against the population it should have reached: an
// administrator's declared target when there is one, and the people observed in
// that part of the organisation when there is not. A rate against the wrong
// denominator is worse than no rate.
const adoptionSQL = `WITH usage AS (
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
GROUP BY u.organization,u.department,u.feature,t.eligible_users,p.observed_population ORDER BY users DESC`

// Adoption returns the feature uptake rows for a period. A limit of zero returns
// every row, which is what the screen wants; a digest passes a small number.
func (rep Reporter) Adoption(ctx context.Context, siteID uuid.UUID, environment string, from, to time.Time, limit int) ([]AdoptionRow, error) {
	rows, err := rep.DB.Query(ctx, adoptionSQL, siteID, from, to, environment)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdoptionRow{}
	for rows.Next() {
		var row AdoptionRow
		if rows.Scan(&row.Organization, &row.Department, &row.Feature, &row.Users, &row.Events,
			&row.RepeatUsers, &row.Active7d, &row.DormantUsers, &row.EligibleUsers,
			&row.FirstUsed, &row.LastUsed) != nil {
			continue
		}
		row.AdoptionRate = percentFloat(row.Users, row.EligibleUsers)
		row.RepeatUsageRate = percentFloat(row.RepeatUsers, row.Users)
		out = append(out, row)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, rows.Err()
}

func percentFloat(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}
