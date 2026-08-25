package httpapi

import (
	"strings"
	"testing"
)

// steps builds a funnel result the way the query scanner does.
func steps(names []string, users []int64) []map[string]any {
	out := []map[string]any{}
	first := users[0]
	for index, name := range names {
		overall := 0.0
		if first > 0 {
			overall = float64(users[index]) * 100 / float64(first)
		}
		out = append(out, map[string]any{
			"step": index + 1, "name": name, "event": name,
			"users": users[index], "overall_conversion_rate": overall,
		})
	}
	return out
}

func series(key, label string, items []map[string]any) map[string]any {
	return map[string]any{"key": key, "label": label, "steps": items, "entered": funnelEntered(items), "completion_rate": funnelCompletion(items)}
}

var funnelSteps = []string{"진입", "검색", "상세", "제출"}

func TestComparisonNamesTheStepWhereTheGapOpens(t *testing.T) {
	t.Parallel()

	baseline := series("baseline", "전체", steps(funnelSteps, []int64{1000, 800, 600, 400}))
	// The weak cohort keeps up until the detail step and then collapses.
	weak := series("seg-1", "영업부", steps(funnelSteps, []int64{200, 160, 40, 20}))

	comparisons := compareFunnelSeries([]map[string]any{baseline, weak})
	if len(comparisons) != 1 {
		t.Fatalf("comparisons = %d, want 1", len(comparisons))
	}
	got := comparisons[0]
	if got.Verdict != "worse" {
		t.Fatalf("verdict = %q (lift %.1f), want worse", got.Verdict, got.LiftPoints)
	}
	if got.WorstStep != 3 || !strings.Contains(got.WorstStepName, "상세") {
		t.Fatalf("worst step = %d %q, want step 3 상세", got.WorstStep, got.WorstStepName)
	}
	if got.CompletionRate < 9.9 || got.CompletionRate > 10.1 {
		t.Fatalf("completion = %.2f, want 10", got.CompletionRate)
	}
	if got.BaselineCompletion != 40 {
		t.Fatalf("baseline completion = %.2f, want 40", got.BaselineCompletion)
	}
	if got.LiftPoints > -29.9 || got.LiftPoints < -30.1 {
		t.Fatalf("lift = %.2f points, want -30", got.LiftPoints)
	}
	if got.LiftPercent > -74 || got.LiftPercent < -76 {
		t.Fatalf("lift percent = %.2f, want about -75", got.LiftPercent)
	}
	if !strings.Contains(got.Evidence, "3단계") || !strings.Contains(got.Evidence, "낮습니다") {
		t.Fatalf("evidence = %q, want the step and the direction", got.Evidence)
	}
}

func TestSmallCohortIsNotAHeadline(t *testing.T) {
	t.Parallel()

	baseline := series("baseline", "전체", steps(funnelSteps, []int64{1000, 800, 600, 400}))
	tiny := series("seg-2", "신규 팀", steps(funnelSteps, []int64{6, 5, 1, 0}))

	got := compareFunnelSeries([]map[string]any{baseline, tiny})[0]
	if got.Verdict != "insufficient" || got.Reliable {
		t.Fatalf("verdict = %q reliable = %v, want an unreliable verdict", got.Verdict, got.Reliable)
	}
	if !strings.Contains(got.Evidence, "표본이 적어") {
		t.Fatalf("evidence = %q, want the sample size caveat", got.Evidence)
	}
}

func TestBetterAndSimilarCohortsAreDistinguished(t *testing.T) {
	t.Parallel()

	baseline := series("baseline", "전체", steps(funnelSteps, []int64{1000, 800, 600, 400}))
	better := series("seg-3", "플랫폼", steps(funnelSteps, []int64{200, 190, 180, 160}))
	similar := series("seg-4", "지원", steps(funnelSteps, []int64{200, 160, 120, 82}))

	comparisons := compareFunnelSeries([]map[string]any{baseline, better, similar})
	if len(comparisons) != 2 {
		t.Fatalf("comparisons = %d, want 2", len(comparisons))
	}
	// The largest movement is reported first so the biggest difference leads.
	if comparisons[0].Key != "seg-3" || comparisons[0].Verdict != "better" {
		t.Fatalf("first = %q %q, want the better cohort first", comparisons[0].Key, comparisons[0].Verdict)
	}
	if comparisons[1].Verdict != "similar" {
		t.Fatalf("second verdict = %q, want similar", comparisons[1].Verdict)
	}
	// This cohort is at or above the baseline at every step, so there is no step
	// where it loses ground and none is invented.
	if comparisons[1].WorstStep != 0 || comparisons[1].WorstStepName != "" {
		t.Fatalf("worst step = %d %q, want none for a cohort that never falls behind", comparisons[1].WorstStep, comparisons[1].WorstStepName)
	}
	if strings.Contains(comparisons[1].Evidence, "단계") {
		t.Fatalf("evidence = %q, must not name a gap step that does not exist", comparisons[1].Evidence)
	}
}

func TestComparisonNeedsABaseline(t *testing.T) {
	t.Parallel()

	only := series("seg-5", "영업", steps(funnelSteps, []int64{100, 50, 25, 10}))
	if got := compareFunnelSeries([]map[string]any{only}); len(got) != 0 {
		t.Fatalf("comparisons = %+v, want none without a baseline", got)
	}
	if got := compareFunnelSeries([]map[string]any{series("baseline", "전체", nil), only}); len(got) != 0 {
		t.Fatalf("comparisons = %+v, want none when the baseline is empty", got)
	}
}

func TestEnteredAndCompletionReadTheEnds(t *testing.T) {
	t.Parallel()

	items := steps(funnelSteps, []int64{500, 400, 300, 250})
	if funnelEntered(items) != 500 {
		t.Fatalf("entered = %d, want 500", funnelEntered(items))
	}
	if funnelCompletion(items) != 50 {
		t.Fatalf("completion = %.1f, want 50", funnelCompletion(items))
	}
	if funnelEntered(nil) != 0 || funnelCompletion(nil) != 0 {
		t.Fatal("an empty funnel has no entrants and no completion")
	}
}
