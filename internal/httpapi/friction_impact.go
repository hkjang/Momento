package httpapi

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

// frictionImpact states what one signal costs. A count of rage clicks says
// something happened; it does not say whether it mattered. Comparing the people
// who hit a signal against the people who did not turns "240 rage clicks" into
// "the people who hit it converted 6 points lower", which is what decides
// whether to spend a sprint on it.
type frictionImpact struct {
	Signal               string  `json:"signal"`
	Affected             int64   `json:"affected_people"`
	Unaffected           int64   `json:"unaffected_people"`
	AffectedConversion   float64 `json:"affected_conversion_rate"`
	UnaffectedConversion float64 `json:"unaffected_conversion_rate"`
	GapPoints            float64 `json:"gap_points"`
	Verdict              string  `json:"verdict"`
	Reliable             bool    `json:"reliable"`
	// LostConversions estimates how many conversions the gap accounts for, which
	// is what makes two signals comparable: a small gap everybody hits can matter
	// more than a large gap almost nobody hits.
	LostConversions float64 `json:"estimated_lost_conversions"`
	Evidence        string  `json:"evidence"`
}

// frictionImpactCaveat is stated once, on the response, rather than implied.
const frictionImpactCaveat = "이 비교는 연관성입니다. 신호를 겪은 집단이 원래 더 어려운 업무를 하고 있었을 수도 있으므로, 인과를 확인하려면 해당 화면을 고친 뒤 같은 집단을 다시 비교하세요."

// minimumImpactPeople keeps a three person group from producing a headline. It
// matches the entrant floor the funnel and cohort comparisons already use.
const minimumImpactPeople = minimumCohortEntrants

// impactRow is what the database returns before any judgement is applied.
type impactRow struct {
	Signal             string
	Affected           int64
	AffectedConverters int64
	TotalPeople        int64
	TotalConverters    int64
}

// judgeFrictionImpact turns counts into a verdict. It is separate from the query
// so the thresholds can be tested without a database.
func judgeFrictionImpact(rows []impactRow) []frictionImpact {
	out := make([]frictionImpact, 0, len(rows))
	for _, row := range rows {
		unaffected := row.TotalPeople - row.Affected
		unaffectedConverters := row.TotalConverters - row.AffectedConverters
		impact := frictionImpact{
			Signal:               row.Signal,
			Affected:             row.Affected,
			Unaffected:           unaffected,
			AffectedConversion:   percent(row.AffectedConverters, row.Affected),
			UnaffectedConversion: percent(unaffectedConverters, unaffected),
		}
		impact.GapPoints = impact.AffectedConversion - impact.UnaffectedConversion
		// Both sides need enough people: a reliable baseline cannot be built from
		// the handful of people who happened not to hit a common signal.
		impact.Reliable = row.Affected >= minimumImpactPeople && unaffected >= minimumImpactPeople
		switch {
		case !impact.Reliable:
			impact.Verdict = "insufficient"
		case impact.GapPoints <= -5:
			impact.Verdict = "worse"
		case impact.GapPoints >= 5:
			impact.Verdict = "better"
		default:
			impact.Verdict = "similar"
		}
		if impact.Verdict == "worse" {
			impact.LostConversions = math.Round(-impact.GapPoints / 100 * float64(row.Affected))
		}
		impact.Evidence = frictionEvidence(impact)
		out = append(out, impact)
	}
	// The signal worth fixing first is the one accounting for the most lost
	// conversions, not the one with the widest gap or the highest count.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].LostConversions != out[j].LostConversions {
			return out[i].LostConversions > out[j].LostConversions
		}
		return out[i].Affected > out[j].Affected
	})
	return out
}

func frictionEvidence(impact frictionImpact) string {
	if !impact.Reliable {
		return fmt.Sprintf("겪은 사람 %d명, 겪지 않은 사람 %d명으로 한쪽이 %d명 미만이라 판단을 보류합니다.",
			impact.Affected, impact.Unaffected, minimumImpactPeople)
	}
	switch impact.Verdict {
	case "worse":
		return fmt.Sprintf("겪은 %d명의 전환율 %.1f%%, 겪지 않은 %d명은 %.1f%%로 %.1f%%p 낮습니다. 이 차이는 전환 약 %.0f건에 해당합니다.",
			impact.Affected, impact.AffectedConversion, impact.Unaffected, impact.UnaffectedConversion,
			-impact.GapPoints, impact.LostConversions)
	case "better":
		return fmt.Sprintf("겪은 %d명의 전환율이 오히려 %.1f%%p 높습니다. 전환 과정에서 자연히 발생하는 신호인지 확인하세요.", impact.Affected, impact.GapPoints)
	default:
		return fmt.Sprintf("겪은 %d명과 겪지 않은 %d명의 전환율 차이가 %.1f%%p로 크지 않습니다.",
			impact.Affected, impact.Unaffected, math.Abs(impact.GapPoints))
	}
}

// frictionImpactReport compares, for every signal, the people who hit it against
// the people who did not. The person aggregate is built once and both the
// totals and the per-signal counts are read from it, so the two sides always
// add up to the same population.
func (s *Server) frictionImpactReport(ctx context.Context, siteID any, from, to time.Time, environment string) ([]frictionImpact, error) {
	// The two site-wide totals are the same on every row, and they used to be
	// carried there by cross joining a one row relation and taking max() of it.
	// That one row cost four fifths of the report: 470ms with the cross join and
	// 91ms with the totals read as scalars, on 200,000 events, for output that is
	// identical value for value. Joining a relation, however small, puts it in the
	// planner's hands; a scalar subquery is evaluated once and read.
	rows, err := s.DB.Query(ctx, `WITH person AS (
			SELECT entity_id, bool_or(is_conversion) converted
			FROM analytics_events
			WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3
			GROUP BY entity_id),
		totals AS (SELECT count(*) people, count(*) FILTER(WHERE converted) converters FROM person),
		touched AS (
			SELECT DISTINCT event_name signal, entity_id
			FROM analytics_events
			WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3
				AND event_name=ANY($5))
		SELECT t.signal, count(*), count(*) FILTER(WHERE p.converted),
			(SELECT people FROM totals), (SELECT converters FROM totals)
		FROM touched t JOIN person p ON p.entity_id=t.entity_id
		GROUP BY t.signal`, siteID, from, to, environment, frictionSignalNames)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	collected := []impactRow{}
	for rows.Next() {
		var row impactRow
		if err := rows.Scan(&row.Signal, &row.Affected, &row.AffectedConverters, &row.TotalPeople, &row.TotalConverters); err != nil {
			return nil, err
		}
		collected = append(collected, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return judgeFrictionImpact(collected), nil
}
