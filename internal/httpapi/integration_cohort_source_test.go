package httpapi

import (
	"fmt"
	"testing"
)

// A person's cohort is the bucket their first activity falls in. That needs
// their whole history, and reading it from every event they ever sent means
// grouping the entire site to answer a question about one quarter.
//
// The unnarrowed grid now reads the daily visitor rollup instead — the same
// insight.FirstSeenCTE the overview's new user count uses. Two definitions of
// "first seen" that agree today is not the same as two that cannot disagree, so
// this runs the grid both ways over the same period and requires the same
// answer, cell by cell.
//
// The events version is reached by asking for a cohort event, which is the
// branch condition; naming an event every seeded visit carries makes the two
// populations the same, so a difference is a difference in definition.
func TestTheCohortGridAgreesWhicheverSourceItReads(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	from, today := f.siteDates(t, 90)
	site := "/api/v1/sites/" + f.siteKey + "/cohort?from=" + from + "&to=" + today + "&granularity=week&periods=12"

	rollup, _ := f.get(t, site)["cohorts"].([]any)
	// page_view is on every seeded visit, so narrowing the cohort to it selects
	// the same people the unnarrowed grid does — and takes the events path.
	events, _ := f.get(t, site+"&cohort_event=page_view")["cohorts"].([]any)

	if len(rollup) == 0 {
		t.Fatal("the grid read from the rollup has no cohorts, so agreement proves nothing")
	}
	if len(events) != len(rollup) {
		t.Fatalf("the rollup gives %d cohorts and the events give %d", len(rollup), len(events))
	}
	compared := 0
	for index := range rollup {
		fromRollup, _ := rollup[index].(map[string]any)
		fromEvents, _ := events[index].(map[string]any)
		if fromRollup == nil || fromEvents == nil {
			continue
		}
		for _, field := range []string{"cohort", "size"} {
			if fmt.Sprint(fromRollup[field]) != fmt.Sprint(fromEvents[field]) {
				t.Errorf("cohort %d %s: the rollup says %v and the events say %v",
					index, field, fromRollup[field], fromEvents[field])
			}
		}
		rollupPeriods, _ := fromRollup["periods"].([]any)
		eventPeriods, _ := fromEvents["periods"].([]any)
		if len(rollupPeriods) != len(eventPeriods) {
			t.Errorf("cohort %v has %d periods from the rollup and %d from the events",
				fromRollup["cohort"], len(rollupPeriods), len(eventPeriods))
			continue
		}
		for period := range rollupPeriods {
			a, _ := rollupPeriods[period].(map[string]any)
			b, _ := eventPeriods[period].(map[string]any)
			if a == nil || b == nil {
				continue
			}
			if fmt.Sprint(a["users"]) != fmt.Sprint(b["users"]) {
				t.Errorf("cohort %v week %d: the rollup says %v people and the events say %v",
					fromRollup["cohort"], period, a["users"], b["users"])
			}
			compared++
		}
	}
	if compared == 0 {
		t.Fatal("no cell was comparable, so this proves nothing about the two sources agreeing")
	}
	t.Logf("compared %d cells across %d cohorts", compared, len(rollup))
}
