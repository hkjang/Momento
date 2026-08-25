package httpapi

import (
	"strings"
	"testing"
)

func TestJudgeFrictionImpactRanksBySizeOfTheLossNotTheGap(t *testing.T) {
	t.Parallel()

	// A wide gap almost nobody hits should rank below a modest gap everybody hits:
	// fixing the second recovers more conversions.
	rows := []impactRow{
		{Signal: "rare_but_severe", Affected: 25, AffectedConverters: 0, TotalPeople: 1000, TotalConverters: 200},
		{Signal: "common_and_costly", Affected: 400, AffectedConverters: 40, TotalPeople: 1000, TotalConverters: 200},
	}
	judged := judgeFrictionImpact(rows)
	if judged[0].Signal != "common_and_costly" {
		t.Fatalf("ranked %q first, want the signal accounting for more lost conversions: %+v", judged[0].Signal, judged)
	}
	if judged[0].Verdict != "worse" || judged[1].Verdict != "worse" {
		t.Fatalf("both signals hurt conversion but the verdicts are %q and %q", judged[0].Verdict, judged[1].Verdict)
	}
	if judged[0].LostConversions <= judged[1].LostConversions {
		t.Fatalf("lost conversions %v should exceed %v", judged[0].LostConversions, judged[1].LostConversions)
	}
	// 400 affected converting at 10% against 600 unaffected converting at 26.7%
	// is a 16.7 point gap, so roughly 67 conversions.
	if judged[0].GapPoints > -16 || judged[0].GapPoints < -18 {
		t.Fatalf("gap = %v, want about -16.7 points", judged[0].GapPoints)
	}
	if judged[0].LostConversions < 60 || judged[0].LostConversions > 75 {
		t.Fatalf("estimated lost conversions = %v, want about 67", judged[0].LostConversions)
	}
}

func TestJudgeFrictionImpactHoldsBackWhenEitherSideIsTooSmall(t *testing.T) {
	t.Parallel()

	rows := []impactRow{
		// Too few people hit it.
		{Signal: "barely_seen", Affected: 4, AffectedConverters: 0, TotalPeople: 500, TotalConverters: 100},
		// Nearly everyone hit it, so the unaffected side cannot be a baseline.
		{Signal: "almost_everyone", Affected: 495, AffectedConverters: 90, TotalPeople: 500, TotalConverters: 100},
	}
	for _, impact := range judgeFrictionImpact(rows) {
		if impact.Verdict != "insufficient" {
			t.Errorf("%s returned verdict %q, want insufficient", impact.Signal, impact.Verdict)
		}
		if impact.Reliable {
			t.Errorf("%s was marked reliable", impact.Signal)
		}
		if impact.LostConversions != 0 {
			t.Errorf("%s estimated a loss from an unreliable comparison", impact.Signal)
		}
		if !strings.Contains(impact.Evidence, "보류") {
			t.Errorf("%s does not say why it is withheld: %s", impact.Signal, impact.Evidence)
		}
	}
}

func TestJudgeFrictionImpactCallsASmallDifferenceSimilar(t *testing.T) {
	t.Parallel()

	// 200 affected at 19.5% against 800 unaffected at 20.1% is well inside the
	// noise band and must not be reported as harm.
	judged := judgeFrictionImpact([]impactRow{
		{Signal: "slow_interaction", Affected: 200, AffectedConverters: 39, TotalPeople: 1000, TotalConverters: 200},
	})
	if judged[0].Verdict != "similar" {
		t.Fatalf("verdict = %q with a %.2f point gap, want similar", judged[0].Verdict, judged[0].GapPoints)
	}
	if judged[0].LostConversions != 0 {
		t.Fatalf("a similar verdict estimated %v lost conversions", judged[0].LostConversions)
	}
}

func TestJudgeFrictionImpactReportsAPositiveAssociationInstead(t *testing.T) {
	t.Parallel()

	// Some signals fire on the way to converting — a form retry on the last step
	// of a purchase, for instance. Reporting that as harm would send someone to
	// fix the wrong thing, so it is stated as what it is.
	judged := judgeFrictionImpact([]impactRow{
		{Signal: "form_retry", Affected: 100, AffectedConverters: 60, TotalPeople: 1000, TotalConverters: 200},
	})
	if judged[0].Verdict != "better" {
		t.Fatalf("verdict = %q, want better", judged[0].Verdict)
	}
	if !strings.Contains(judged[0].Evidence, "오히려") {
		t.Fatalf("evidence does not flag the reversal: %s", judged[0].Evidence)
	}
	if judged[0].LostConversions != 0 {
		t.Fatalf("a positive association estimated %v lost conversions", judged[0].LostConversions)
	}
}
