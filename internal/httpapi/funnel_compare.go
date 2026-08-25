package httpapi

import (
	"fmt"
	"math"
	"sort"
)

// Comparing cohorts is what makes a funnel actionable. An overall 12% completion
// rate hides that one department converts at 22% and another at 4%; the comparison
// names the step where the gap opens so the next question is already answered.

func funnelStepUsers(step map[string]any) int64 {
	if value, ok := step["users"].(int64); ok {
		return value
	}
	return 0
}

func funnelStepOverall(step map[string]any) float64 {
	if value, ok := step["overall_conversion_rate"].(float64); ok {
		return value
	}
	return 0
}

// funnelEntered is how many people reached the first step of a cohort.
func funnelEntered(steps []map[string]any) int64 {
	if len(steps) == 0 {
		return 0
	}
	return funnelStepUsers(steps[0])
}

// funnelCompletion is the share of entrants that reached the final step.
func funnelCompletion(steps []map[string]any) float64 {
	if len(steps) == 0 {
		return 0
	}
	return funnelStepOverall(steps[len(steps)-1])
}

// funnelComparison states how one cohort differs from the baseline.
type funnelComparison struct {
	Key                string  `json:"key"`
	Label              string  `json:"label"`
	Entered            int64   `json:"entered"`
	CompletionRate     float64 `json:"completion_rate"`
	BaselineCompletion float64 `json:"baseline_completion_rate"`
	LiftPoints         float64 `json:"lift_points"`
	LiftPercent        float64 `json:"lift_percent"`
	WorstStep          int     `json:"worst_step"`
	WorstStepName      string  `json:"worst_step_name"`
	WorstStepGap       float64 `json:"worst_step_gap_points"`
	Verdict            string  `json:"verdict"`
	Evidence           string  `json:"evidence"`
	Reliable           bool    `json:"reliable"`
}

// minimumCohortEntrants keeps a three person cohort from producing a headline.
const minimumCohortEntrants = 20

// compareFunnelSeries measures every segment against the baseline series.
func compareFunnelSeries(series []map[string]any) []funnelComparison {
	out := []funnelComparison{}
	var baseline []map[string]any
	for _, item := range series {
		if item["key"] == "baseline" {
			baseline, _ = item["steps"].([]map[string]any)
		}
	}
	if len(baseline) == 0 {
		return out
	}
	baselineCompletion := funnelCompletion(baseline)
	for _, item := range series {
		key, _ := item["key"].(string)
		if key == "baseline" {
			continue
		}
		steps, _ := item["steps"].([]map[string]any)
		if len(steps) == 0 {
			continue
		}
		label, _ := item["label"].(string)
		comparison := funnelComparison{
			Key: key, Label: label,
			Entered:            funnelEntered(steps),
			CompletionRate:     funnelCompletion(steps),
			BaselineCompletion: baselineCompletion,
		}
		comparison.LiftPoints = comparison.CompletionRate - baselineCompletion
		if baselineCompletion > 0 {
			comparison.LiftPercent = comparison.LiftPoints / baselineCompletion * 100
		}
		comparison.Reliable = comparison.Entered >= minimumCohortEntrants
		// The worst step is where this cohort loses the most ground relative to the
		// baseline, which is the step worth investigating first.
		worstGap := 0.0
		for index := range steps {
			if index >= len(baseline) {
				break
			}
			gap := funnelStepOverall(steps[index]) - funnelStepOverall(baseline[index])
			if gap < worstGap {
				worstGap = gap
				comparison.WorstStep = index + 1
				if name, ok := steps[index]["name"].(string); ok && name != "" {
					comparison.WorstStepName = name
				} else if event, ok := steps[index]["event"].(string); ok {
					comparison.WorstStepName = event
				}
				comparison.WorstStepGap = gap
			}
		}
		switch {
		case !comparison.Reliable:
			comparison.Verdict = "insufficient"
		case comparison.LiftPoints >= 5:
			comparison.Verdict = "better"
		case comparison.LiftPoints <= -5:
			comparison.Verdict = "worse"
		default:
			comparison.Verdict = "similar"
		}
		comparison.Evidence = funnelEvidence(comparison)
		out = append(out, comparison)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return math.Abs(out[i].LiftPoints) > math.Abs(out[j].LiftPoints)
	})
	return out
}

func funnelEvidence(comparison funnelComparison) string {
	if !comparison.Reliable {
		return fmt.Sprintf("진입 %d명으로 표본이 적어 비교를 신뢰할 수 없습니다. 기간을 넓히거나 조건을 완화하십시오.", comparison.Entered)
	}
	direction := "높습니다"
	if comparison.LiftPoints < 0 {
		direction = "낮습니다"
	}
	evidence := fmt.Sprintf("진입 %d명 · 완주율 %.1f%%로 전체(%.1f%%)보다 %.1fpp %s",
		comparison.Entered, comparison.CompletionRate, comparison.BaselineCompletion, math.Abs(comparison.LiftPoints), direction)
	if comparison.WorstStep > 0 {
		evidence += fmt.Sprintf(". 격차가 가장 크게 벌어지는 지점은 %d단계 %s(%.1fpp)입니다",
			comparison.WorstStep, comparison.WorstStepName, comparison.WorstStepGap)
	}
	return evidence
}
