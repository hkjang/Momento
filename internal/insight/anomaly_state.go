package insight

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Alert state turns detections into transitions. An anomaly that is still open on
// the next run is not news, and an anomaly that disappeared is news of its own, so
// the same drop is announced once and its recovery is announced once.

// AnomalyState is what Momento already knows about one metric.
type AnomalyState struct {
	Metric          string     `json:"metric"`
	Severity        string     `json:"severity"`
	FirstDetectedOn time.Time  `json:"first_detected_on"`
	LastDetectedOn  time.Time  `json:"last_detected_on"`
	NotifiedOn      *time.Time `json:"notified_on"`
	ResolvedOn      *time.Time `json:"resolved_on"`
	DaysOpen        int        `json:"days_open"`
}

// AnomalyTransition is what changed since the previous evaluation.
type AnomalyTransition struct {
	Metric     string  `json:"metric"`
	Label      string  `json:"label"`
	State      string  `json:"state"` // new, ongoing, recovered
	Severity   string  `json:"severity"`
	DaysOpen   int     `json:"days_open"`
	RobustZ    float64 `json:"robust_z"`
	Evidence   string  `json:"evidence"`
	Action     string  `json:"action,omitempty"`
	Notifiable bool    `json:"notifiable"`
}

// AnomalyStates reads the open and recently closed alert rows without changing them,
// so a dashboard refresh never rewrites alert history.
func (rep Reporter) AnomalyStates(ctx context.Context, siteID uuid.UUID, environment string) (map[string]AnomalyState, error) {
	out := map[string]AnomalyState{}
	rows, err := rep.DB.Query(ctx, `SELECT metric,severity,first_detected_on,last_detected_on,notified_on,resolved_on
		FROM anomaly_alerts WHERE site_id=$1 AND environment=$2`, siteID, environment)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var state AnomalyState
		if rows.Scan(&state.Metric, &state.Severity, &state.FirstDetectedOn, &state.LastDetectedOn, &state.NotifiedOn, &state.ResolvedOn) != nil {
			continue
		}
		state.DaysOpen = int(state.LastDetectedOn.Sub(state.FirstDetectedOn).Hours()/24) + 1
		out[state.Metric] = state
	}
	return out, rows.Err()
}

// NotifiableStates is the default set of transitions worth sending: a problem that
// just appeared and a problem that just went away.
func NotifiableStates() []string { return []string{"new", "recovered"} }

func containsState(states []string, state string) bool {
	for _, item := range states {
		if item == state {
			return true
		}
	}
	return false
}

// AnnotateStates labels a freshly detected report with what is already known,
// without writing anything. The console uses it to show "new" against "open for
// three days".
func AnnotateStates(report AnomalyReport, states map[string]AnomalyState) []AnomalyTransition {
	transitions := []AnomalyTransition{}
	detected := map[string]Anomaly{}
	for _, anomaly := range report.Detected {
		detected[anomaly.Metric] = anomaly
		if anomaly.Severity == "positive" {
			continue
		}
		state := "new"
		daysOpen := 1
		if known, ok := states[anomaly.Metric]; ok && known.ResolvedOn == nil {
			state = "ongoing"
			daysOpen = known.DaysOpen
			if known.LastDetectedOn.Before(report.EvaluatedDate) {
				daysOpen++
			}
		}
		transitions = append(transitions, AnomalyTransition{
			Metric: anomaly.Metric, Label: anomaly.Label, State: state, Severity: anomaly.Severity,
			DaysOpen: daysOpen, RobustZ: anomaly.RobustZ, Evidence: anomaly.Evidence, Action: anomaly.Action,
			Notifiable: containsState(NotifiableStates(), state),
		})
	}
	for metric, known := range states {
		if _, stillDetected := detected[metric]; stillDetected || known.ResolvedOn != nil {
			continue
		}
		label := metric
		for _, watched := range WatchedMetrics() {
			if watched.Key == metric {
				label = watched.Label
			}
		}
		transitions = append(transitions, AnomalyTransition{
			Metric: metric, Label: label, State: "recovered", Severity: known.Severity,
			DaysOpen:   known.DaysOpen,
			Evidence:   "기준선 범위로 돌아왔습니다. " + known.FirstDetectedOn.Format("2006-01-02") + " 부터 " + known.LastDetectedOn.Format("2006-01-02") + " 까지 이어졌습니다.",
			Notifiable: containsState(NotifiableStates(), "recovered"),
		})
	}
	return transitions
}

// ApplyAnomalyState persists the evaluation and returns the transitions that should
// be announced. Only the delivery path calls it, so reading a report never changes
// what has been announced.
func (rep Reporter) ApplyAnomalyState(ctx context.Context, siteID uuid.UUID, environment string, report AnomalyReport, notifyOn []string) ([]AnomalyTransition, error) {
	if len(notifyOn) == 0 {
		notifyOn = NotifiableStates()
	}
	states, err := rep.AnomalyStates(ctx, siteID, environment)
	if err != nil {
		return nil, err
	}
	transitions := AnnotateStates(report, states)
	evaluated := report.EvaluatedDate
	announce := []AnomalyTransition{}
	for index := range transitions {
		transition := &transitions[index]
		transition.Notifiable = containsState(notifyOn, transition.State)
		switch transition.State {
		case "new", "ongoing":
			// Even when the operator opts into ongoing alerts, the same open anomaly
			// is announced at most once per evaluated day.
			if known, ok := states[transition.Metric]; ok && known.NotifiedOn != nil && !known.NotifiedOn.Before(evaluated) {
				transition.Notifiable = false
			}
			if _, err := rep.DB.Exec(ctx, `INSERT INTO anomaly_alerts(site_id,environment,metric,severity,robust_z,value,baseline,first_detected_on,last_detected_on,notified_on,resolved_on,updated_at)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8,CASE WHEN $9 THEN $8::date END,NULL,now())
				ON CONFLICT(site_id,environment,metric) DO UPDATE SET
					severity=excluded.severity,robust_z=excluded.robust_z,value=excluded.value,baseline=excluded.baseline,
					first_detected_on=CASE WHEN anomaly_alerts.resolved_on IS NULL THEN anomaly_alerts.first_detected_on ELSE excluded.first_detected_on END,
					last_detected_on=excluded.last_detected_on,
					notified_on=CASE WHEN $9 THEN excluded.last_detected_on ELSE anomaly_alerts.notified_on END,
					resolved_on=NULL,updated_at=now()`,
				siteID, environment, transition.Metric, transition.Severity, transition.RobustZ,
				anomalyValue(report, transition.Metric), anomalyBaseline(report, transition.Metric), evaluated, transition.Notifiable); err != nil {
				return nil, err
			}
		case "recovered":
			if _, err := rep.DB.Exec(ctx, `UPDATE anomaly_alerts SET resolved_on=$4,updated_at=now() WHERE site_id=$1 AND environment=$2 AND metric=$3 AND resolved_on IS NULL`,
				siteID, environment, transition.Metric, evaluated); err != nil {
				return nil, err
			}
		}
		if transition.Notifiable {
			announce = append(announce, *transition)
		}
	}
	return announce, nil
}

func anomalyValue(report AnomalyReport, metric string) float64 {
	for _, anomaly := range report.Detected {
		if anomaly.Metric == metric {
			return anomaly.Value
		}
	}
	return 0
}

func anomalyBaseline(report AnomalyReport, metric string) float64 {
	for _, anomaly := range report.Detected {
		if anomaly.Metric == metric {
			return anomaly.Baseline
		}
	}
	return 0
}
