import type { Anomaly, AnomalyTransition } from "./visitorInsights";

// The landing screen should answer "what needs me today" before it shows totals.
// Anomalies and goals already know that; this merges them into one ranked list so
// the operator does not have to visit three screens to find out nothing is wrong.

export type AttentionSeverity = "critical" | "warning" | "info";

export interface AttentionItem {
  id: string;
  severity: AttentionSeverity;
  title: string;
  detail: string;
  action?: string;
  to: string;
  source: "anomaly" | "goal";
}

export interface GoalEvaluation {
  id: string;
  name: string;
  metric_name: string;
  value: number;
  target_value: number;
  comparator: string;
  period: string;
  achieved: boolean;
  progress_percent: number;
  forecast_available?: boolean;
  forecast_status?: "on_track" | "behind";
  projected_value?: number;
  elapsed_percent?: number;
}

const severityRank: Record<AttentionSeverity, number> = {
  critical: 0,
  warning: 1,
  info: 2,
};

function anomalySeverity(anomaly: Anomaly): AttentionSeverity | null {
  if (anomaly.severity === "critical") return "critical";
  if (anomaly.severity === "warning") return "warning";
  if (anomaly.severity === "positive") return "info";
  return null;
}

function stateSuffix(transition?: AnomalyTransition): string {
  if (!transition) return "";
  if (transition.state === "ongoing") return ` · 지속 ${transition.days_open}일`;
  if (transition.state === "new") return " · 신규";
  return "";
}

/**
 * buildAttentionItems ranks what deserves attention. An anomaly outranks a goal at
 * the same severity because it is a change that just happened, while a goal is a
 * standing target. Achieved and on-track goals are left out entirely: a list that
 * includes everything tells the reader nothing.
 */
export function buildAttentionItems(
  anomalies: Anomaly[] | undefined,
  transitions: AnomalyTransition[] | undefined,
  goals: GoalEvaluation[] | undefined,
  limit = 5,
): { items: AttentionItem[]; hidden: number } {
  const items: AttentionItem[] = [];
  for (const anomaly of anomalies || []) {
    const severity = anomalySeverity(anomaly);
    if (!severity) continue;
    const transition = (transitions || []).find((item) => item.metric === anomaly.metric);
    items.push({
      id: `anomaly:${anomaly.metric}`,
      severity,
      title:
        severity === "info"
          ? `${anomaly.label}이(가) 기준선보다 크게 늘었습니다`
          : `${anomaly.label}이(가) 기준선을 벗어났습니다${stateSuffix(transition)}`,
      detail: anomaly.evidence,
      action: anomaly.action,
      to: "/visitor-insights",
      source: "anomaly",
    });
  }
  for (const goal of goals || []) {
    if (goal.achieved || goal.forecast_available === false) continue;
    if (goal.forecast_status !== "behind") continue;
    const projected = goal.projected_value ?? 0;
    items.push({
      id: `goal:${goal.id}`,
      severity: "warning",
      title: `${goal.name} 목표 미달 전망`,
      detail: `현재 ${formatGoalNumber(goal.value)} · 착지 예상 ${formatGoalNumber(projected)} · 목표 ${formatGoalNumber(goal.target_value)} (기간 진행 ${Math.round(goal.elapsed_percent ?? 0)}%)`,
      to: "/goals",
      source: "goal",
    });
  }
  items.sort((left, right) => {
    if (severityRank[left.severity] !== severityRank[right.severity]) {
      return severityRank[left.severity] - severityRank[right.severity];
    }
    if (left.source !== right.source) return left.source === "anomaly" ? -1 : 1;
    return left.title.localeCompare(right.title);
  });
  return { items: items.slice(0, limit), hidden: Math.max(0, items.length - limit) };
}

export function formatGoalNumber(value: number): string {
  const rounded = Math.round(value * 10) / 10;
  return Number.isInteger(rounded)
    ? rounded.toLocaleString("ko-KR")
    : rounded.toLocaleString("ko-KR", { minimumFractionDigits: 1, maximumFractionDigits: 1 });
}
