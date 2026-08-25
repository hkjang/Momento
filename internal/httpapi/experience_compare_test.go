package httpapi

import (
	"strings"
	"testing"
)

func vitals(pairs map[string]struct {
	p75     float64
	samples int64
}) []experienceVital {
	out := []experienceVital{}
	for metric, value := range pairs {
		out = append(out, experienceVital{Metric: metric, P75: value.p75, Samples: value.samples, Threshold: vitalThreshold(metric)})
	}
	return out
}

type vital = struct {
	p75     float64
	samples int64
}

func TestSlowCohortIsReportedWithItsRatio(t *testing.T) {
	t.Parallel()

	baseline := experienceCohort{Key: "baseline", Label: "전체", Users: 1000, Vitals: vitals(map[string]vital{"LCP": {2000, 5000}})}
	mobile := experienceCohort{Key: "seg", Label: "모바일", Users: 300, Vitals: vitals(map[string]vital{"LCP": {4200, 900}})}

	gaps := compareExperience(baseline, []experienceCohort{mobile})
	if len(gaps) != 1 {
		t.Fatalf("gaps = %d, want 1", len(gaps))
	}
	got := gaps[0]
	if got.Kind != "vital" || got.Metric != "LCP" {
		t.Fatalf("gap = %+v, want an LCP vital gap", got)
	}
	// Baseline is inside the 2500 bar and the cohort is outside it, which is worse
	// than merely being slower.
	if got.Severity != "critical" {
		t.Fatalf("severity = %q, want critical when the cohort crosses the good threshold", got.Severity)
	}
	if got.Impact < 109 || got.Impact > 111 {
		t.Fatalf("impact = %.1f, want about 110 percent slower", got.Impact)
	}
	if !strings.Contains(got.Evidence, "권장 기준 2500를 초과") {
		t.Fatalf("evidence = %q, want the threshold note", got.Evidence)
	}
}

func TestSlowerButStillWithinTheBarIsOnlyAWarning(t *testing.T) {
	t.Parallel()

	baseline := experienceCohort{Users: 1000, Vitals: vitals(map[string]vital{"LCP": {1200, 5000}})}
	cohort := experienceCohort{Key: "seg", Label: "지사", Users: 200, Vitals: vitals(map[string]vital{"LCP": {2000, 400}})}

	gaps := compareExperience(baseline, []experienceCohort{cohort})
	if len(gaps) != 1 || gaps[0].Severity != "warning" {
		t.Fatalf("gaps = %+v, want a single warning", gaps)
	}
	if !strings.Contains(gaps[0].Evidence, "이내입니다") {
		t.Fatalf("evidence = %q, want the within-threshold note", gaps[0].Evidence)
	}
}

func TestSmallDifferencesAndThinSamplesAreNotFindings(t *testing.T) {
	t.Parallel()

	baseline := experienceCohort{Users: 1000, Vitals: vitals(map[string]vital{"LCP": {2000, 5000}})}
	// 20% slower is below the reporting ratio.
	mild := experienceCohort{Key: "a", Label: "A", Users: 500, Vitals: vitals(map[string]vital{"LCP": {2400, 900}})}
	// Twice as slow but only 5 measurements.
	thin := experienceCohort{Key: "b", Label: "B", Users: 500, Vitals: vitals(map[string]vital{"LCP": {4000, 5}})}

	if gaps := compareExperience(baseline, []experienceCohort{mild, thin}); len(gaps) != 0 {
		t.Fatalf("gaps = %+v, want none", gaps)
	}
}

func TestErrorExposureGapIsReported(t *testing.T) {
	t.Parallel()

	baseline := experienceCohort{Users: 2000, ErrorUsers: 100, ErrorUserRate: 5}
	cohort := experienceCohort{Key: "seg", Label: "영업부", Users: 400, ErrorUsers: 120, ErrorUserRate: 30}

	gaps := compareExperience(baseline, []experienceCohort{cohort})
	if len(gaps) != 1 || gaps[0].Kind != "error" {
		t.Fatalf("gaps = %+v, want one error gap", gaps)
	}
	if gaps[0].Severity != "critical" {
		t.Fatalf("severity = %q, want critical for a 25pp gap", gaps[0].Severity)
	}
	if !strings.Contains(gaps[0].Evidence, "25.0pp") {
		t.Fatalf("evidence = %q, want the point difference", gaps[0].Evidence)
	}
	// A cohort with too few users is not judged at all.
	tiny := experienceCohort{Key: "t", Label: "T", Users: 9, ErrorUsers: 9, ErrorUserRate: 100}
	if gaps := compareExperience(baseline, []experienceCohort{tiny}); len(gaps) != 0 {
		t.Fatalf("gaps = %+v, want none for a nine person cohort", gaps)
	}
}

func TestCriticalGapsLeadAndRatiosOrderTheRest(t *testing.T) {
	t.Parallel()

	baseline := experienceCohort{Users: 1000, ErrorUserRate: 2, Vitals: vitals(map[string]vital{"LCP": {1000, 5000}, "INP": {100, 5000}})}
	cohorts := []experienceCohort{
		{Key: "mild", Label: "mild", Users: 500, ErrorUserRate: 2, Vitals: vitals(map[string]vital{"INP": {140, 500}})},
		{Key: "severe", Label: "severe", Users: 500, ErrorUserRate: 2, Vitals: vitals(map[string]vital{"LCP": {4000, 500}})},
	}
	gaps := compareExperience(baseline, cohorts)
	if len(gaps) != 2 {
		t.Fatalf("gaps = %d, want 2", len(gaps))
	}
	if gaps[0].Key != "severe" || gaps[0].Severity != "critical" {
		t.Fatalf("first gap = %+v, want the critical one first", gaps[0])
	}
}

func TestThresholdsCoverTheCoreVitals(t *testing.T) {
	t.Parallel()

	for metric, want := range map[string]float64{"LCP": 2500, "INP": 200, "CLS": 0.1, "FCP": 1800, "TTFB": 800} {
		if got := vitalThreshold(metric); got != want {
			t.Fatalf("threshold(%q) = %v, want %v", metric, got, want)
		}
		if got := vitalThreshold(strings.ToLower(metric)); got != want {
			t.Fatalf("threshold is case sensitive for %q", metric)
		}
	}
	if vitalThreshold("custom_metric") != 0 {
		t.Fatal("an unknown metric must have no threshold rather than a made up one")
	}
}
