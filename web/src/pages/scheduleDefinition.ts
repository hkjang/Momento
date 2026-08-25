// Turning a finding into a recurring delivery used to mean typing a definition
// document by hand in another screen. The report kinds each need a handful of
// values, so the form asks for those and builds the definition.

export type ScheduleKind =
  | "overview"
  | "insights"
  | "visitor_insight"
  | "anomaly"
  | "adoption"
  | "experience"
  | "ai"
  | "segment";

export interface ScheduleForm {
  environment: string;
  days: number;
  notifyOn: string[];
  alwaysSend: boolean;
  eventName: string;
  feature: string;
  department: string;
}

export const scheduleKindLabel: Record<ScheduleKind, string> = {
  overview: "개요 요약",
  insights: "인사이트 요약",
  visitor_insight: "방문자 인사이트 전체",
  anomaly: "이상 감지 알림",
  adoption: "기능 도입 현황",
  experience: "경험 · 오류",
  ai: "AI 사용량",
  segment: "Segment 집계",
};

export const defaultScheduleForm: ScheduleForm = {
  environment: "prd",
  days: 7,
  notifyOn: ["new", "recovered"],
  alwaysSend: false,
  eventName: "",
  feature: "",
  department: "",
};

/** Report kinds that measure a period and therefore need a range. */
export function kindUsesRange(kind: ScheduleKind): boolean {
  return kind !== "anomaly";
}

/** Only the anomaly alert has notification state to choose. */
export function kindUsesAlertState(kind: ScheduleKind): boolean {
  return kind === "anomaly";
}

/** Only the segment aggregate needs matching conditions. */
export function kindUsesSegmentFilters(kind: ScheduleKind): boolean {
  return kind === "segment";
}

/**
 * buildScheduleDefinition produces exactly the keys a kind uses. Sending the whole
 * form would store fields the report ignores, which later reads as configuration
 * that does something.
 */
export function buildScheduleDefinition(
  kind: ScheduleKind,
  form: ScheduleForm,
): Record<string, unknown> {
  const definition: Record<string, unknown> = { environment: form.environment };
  if (kindUsesRange(kind)) definition.days = form.days;
  if (kindUsesAlertState(kind)) {
    definition.notify_on = form.notifyOn.length ? form.notifyOn : ["new", "recovered"];
    if (form.alwaysSend) definition.always_send = true;
  }
  if (kindUsesSegmentFilters(kind)) {
    if (form.eventName.trim()) definition.event_name = form.eventName.trim();
    if (form.feature.trim()) definition.feature = form.feature.trim();
    if (form.department.trim()) definition.department = form.department.trim();
  }
  return definition;
}

export const intervalPresets = [
  { label: "1시간마다", minutes: 60 },
  { label: "6시간마다", minutes: 360 },
  { label: "매일", minutes: 1440 },
  { label: "매주", minutes: 10080 },
] as const;

/** describeSchedule states in one line what will be delivered and how often. */
export function describeSchedule(
  kind: ScheduleKind,
  form: ScheduleForm,
  intervalMinutes: number,
): string {
  const cadence =
    intervalPresets.find((preset) => preset.minutes === intervalMinutes)?.label ||
    `${intervalMinutes}분마다`;
  const parts = [`${cadence} ${scheduleKindLabel[kind]}`, form.environment.toUpperCase()];
  if (kindUsesRange(kind)) parts.push(`최근 ${form.days}일`);
  if (kindUsesAlertState(kind)) {
    parts.push(
      form.alwaysSend
        ? "감지 여부와 무관하게 매번 전송"
        : `${form.notifyOn.map(alertStateLabel).join("·")} 상태만 전송`,
    );
  }
  if (kindUsesSegmentFilters(kind)) {
    const filters = [form.eventName, form.feature, form.department].filter(Boolean);
    parts.push(filters.length ? `조건 ${filters.join(" / ")}` : "조건 없음");
  }
  return parts.join(" · ");
}

export function alertStateLabel(state: string): string {
  if (state === "new") return "신규";
  if (state === "ongoing") return "지속";
  if (state === "recovered") return "회복";
  return state;
}

/** parseScheduleKind keeps a deep link from selecting a kind the server rejects. */
export function parseScheduleKind(value: string | null): ScheduleKind | null {
  if (!value) return null;
  return value in scheduleKindLabel ? (value as ScheduleKind) : null;
}
