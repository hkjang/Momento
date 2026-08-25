package insight

import (
	"testing"
	"time"
)

func detectedReport(evaluated time.Time, metrics ...string) AnomalyReport {
	report := AnomalyReport{EvaluatedDate: evaluated}
	for _, metric := range metrics {
		label := metric
		for _, watched := range WatchedMetrics() {
			if watched.Key == metric {
				label = watched.Label
			}
		}
		report.Detected = append(report.Detected, Anomaly{
			Metric: metric, Label: label, Severity: "critical", RobustZ: -5,
			Value: 100, Baseline: 900, Evidence: "근거", Action: "확인", Date: evaluated,
		})
	}
	return report
}

func transitionFor(transitions []AnomalyTransition, metric string) (AnomalyTransition, bool) {
	for _, transition := range transitions {
		if transition.Metric == metric {
			return transition, true
		}
	}
	return AnomalyTransition{}, false
}

func TestFirstDetectionIsNewAndAnnounced(t *testing.T) {
	t.Parallel()

	evaluated := day("2026-08-10")
	transitions := AnnotateStates(detectedReport(evaluated, "users"), map[string]AnomalyState{})

	got, ok := transitionFor(transitions, "users")
	if !ok {
		t.Fatalf("no transition for users in %+v", transitions)
	}
	if got.State != "new" || !got.Notifiable {
		t.Fatalf("state = %q notifiable = %v, want a new announced anomaly", got.State, got.Notifiable)
	}
	if got.DaysOpen != 1 {
		t.Fatalf("days open = %d, want 1", got.DaysOpen)
	}
}

func TestStillOpenAnomalyIsNotAnnouncedAgain(t *testing.T) {
	t.Parallel()

	evaluated := day("2026-08-10")
	yesterday := day("2026-08-09")
	states := map[string]AnomalyState{
		"users": {Metric: "users", Severity: "critical", FirstDetectedOn: day("2026-08-07"), LastDetectedOn: yesterday, DaysOpen: 3},
	}
	transitions := AnnotateStates(detectedReport(evaluated, "users"), states)

	got, _ := transitionFor(transitions, "users")
	if got.State != "ongoing" {
		t.Fatalf("state = %q, want ongoing", got.State)
	}
	if got.Notifiable {
		t.Fatal("an anomaly that was already announced must not be announced again by default")
	}
	if got.DaysOpen != 4 {
		t.Fatalf("days open = %d, want 4 after another day of the same problem", got.DaysOpen)
	}
}

func TestRecoveryIsAnnouncedOnce(t *testing.T) {
	t.Parallel()

	evaluated := day("2026-08-10")
	states := map[string]AnomalyState{
		"users": {Metric: "users", Severity: "critical", FirstDetectedOn: day("2026-08-07"), LastDetectedOn: day("2026-08-09"), DaysOpen: 3},
	}
	// Nothing is detected today, so the open alert has recovered.
	transitions := AnnotateStates(AnomalyReport{EvaluatedDate: evaluated}, states)

	got, ok := transitionFor(transitions, "users")
	if !ok {
		t.Fatalf("no recovery transition in %+v", transitions)
	}
	if got.State != "recovered" || !got.Notifiable {
		t.Fatalf("state = %q notifiable = %v, want an announced recovery", got.State, got.Notifiable)
	}
	if got.Label != "방문자" {
		t.Fatalf("label = %q, want the watched metric label", got.Label)
	}
	if got.Evidence == "" {
		t.Fatal("a recovery must state the period it covered")
	}
}

func TestAlreadyResolvedAlertDoesNotRecoverTwice(t *testing.T) {
	t.Parallel()

	resolved := day("2026-08-09")
	states := map[string]AnomalyState{
		"users": {Metric: "users", FirstDetectedOn: day("2026-08-07"), LastDetectedOn: resolved, ResolvedOn: &resolved},
	}
	transitions := AnnotateStates(AnomalyReport{EvaluatedDate: day("2026-08-10")}, states)
	if len(transitions) != 0 {
		t.Fatalf("transitions = %+v, want none for an already resolved alert", transitions)
	}
}

func TestReopenedAlertIsNewAgain(t *testing.T) {
	t.Parallel()

	resolved := day("2026-08-08")
	states := map[string]AnomalyState{
		"users": {Metric: "users", FirstDetectedOn: day("2026-08-05"), LastDetectedOn: resolved, ResolvedOn: &resolved},
	}
	transitions := AnnotateStates(detectedReport(day("2026-08-10"), "users"), states)

	got, _ := transitionFor(transitions, "users")
	if got.State != "new" || !got.Notifiable {
		t.Fatalf("state = %q, want a resolved alert to be new when it returns", got.State)
	}
}

func TestPositiveMovesAreNotAlerts(t *testing.T) {
	t.Parallel()

	report := AnomalyReport{EvaluatedDate: day("2026-08-10"), Detected: []Anomaly{
		{Metric: "users", Label: "방문자", Severity: "positive", RobustZ: 4},
	}}
	if transitions := AnnotateStates(report, map[string]AnomalyState{}); len(transitions) != 0 {
		t.Fatalf("transitions = %+v, want none for a positive move", transitions)
	}
}

func TestNotifiableStatesAreNewAndRecovered(t *testing.T) {
	t.Parallel()

	states := NotifiableStates()
	if len(states) != 2 || !containsState(states, "new") || !containsState(states, "recovered") {
		t.Fatalf("notifiable states = %v, want new and recovered", states)
	}
	if containsState(states, "ongoing") {
		t.Fatal("ongoing anomalies must not be announced by default")
	}
}
