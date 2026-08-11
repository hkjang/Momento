export type InsightFormat = "number" | "percent" | "decimal" | "duration";
export type FindingSeverity = "critical" | "warning" | "info" | "positive";

export interface InsightKPI {
  key: string;
  label: string;
  format: InsightFormat;
  current: number;
  previous: number;
  change_percent: number;
  /** "higher" and "lower" state which direction is progress; "neutral" is ambiguous. */
  goal: "higher" | "lower" | "neutral";
}

export interface InsightFinding {
  id: string;
  title: string;
  severity: FindingSeverity;
  evidence: string;
  cause: string;
  action: string;
  impact: number;
}

export interface InsightLifecycle {
  kind: "new" | "returning";
  users: number;
  sessions: number;
  sessions_per_user: number;
  conversion_rate: number;
  share_percent: number;
}

export interface InsightChannel {
  channel: string;
  users: number;
  sessions: number;
  converted_users: number;
  conversion_rate: number;
  previous_users: number;
  change_percent: number;
  user_share_percent: number;
}

export interface InsightLanding {
  page: string;
  sessions: number;
  bounce_rate: number;
  engagement_rate: number;
  conversion_rate: number;
  average_seconds: number;
  session_share_percent: number;
}

export interface InsightBucket {
  bucket: string;
  label: string;
  users: number;
  share_percent: number;
  conversion_rate: number;
}

export interface InsightDevice {
  device: string;
  users: number;
  sessions: number;
  conversion_rate: number;
  share_percent: number;
}

export interface SegmentRule {
  combinator?: string;
  rules?: SegmentRule[];
  field?: string;
  operator?: string;
  value?: unknown;
}

export interface InsightAudience {
  key: string;
  label: string;
  users: number;
  action: string;
  /** Ready-to-save segment definition for this audience. */
  segment?: SegmentRule;
  segment_note?: string;
}

export interface Anomaly {
  metric: string;
  label: string;
  date: string;
  value: number;
  baseline: number;
  change_percent: number;
  robust_z: number;
  severity: "critical" | "warning" | "positive" | "normal" | "insufficient_history" | "unknown";
  direction: "above" | "below" | "flat";
  samples: number;
  weekday: string;
  evidence: string;
  action?: string;
}

export interface AnomalyReport {
  environment: string;
  evaluated_date: string;
  timezone: string;
  baseline_weeks: number;
  detected: Anomaly[];
  checked: Anomaly[];
  note: string;
}

export interface AttributionChannel {
  channel: string;
  credited_conversions: number;
  credited_users: number;
  assisted_conversions: number;
  credit_share_percent: number;
  assist_only_conversions: number;
}

export interface AttributionReport {
  model: string;
  label: string;
  description: string;
  lookback_days: number;
  total_conversions: number;
  attributed_conversions: number;
  unattributed_conversions: number;
  channels: AttributionChannel[];
  note: string;
}

export interface AttributionModel {
  key: string;
  label: string;
  description: string;
}

export const anomalySeverityLabel: Record<Anomaly["severity"], string> = {
  critical: "심각",
  warning: "경고",
  positive: "긍정 변화",
  normal: "정상",
  insufficient_history: "데이터 부족",
  unknown: "집계 없음",
};

export interface VisitorInsightReport {
  environment: string;
  timezone?: string;
  from: string;
  to: string;
  previous_from: string;
  previous_to: string;
  days: number;
  headline: string;
  kpis: InsightKPI[];
  findings: InsightFinding[];
  lifecycle: InsightLifecycle[];
  channels: InsightChannel[];
  landing_pages: InsightLanding[];
  frequency: InsightBucket[];
  recency: InsightBucket[];
  devices: InsightDevice[];
  audiences: InsightAudience[];
  notes: string[];
}

const numberFormat = new Intl.NumberFormat("ko-KR", { maximumFractionDigits: 1 });

export function formatInsightValue(value: number, format: InsightFormat) {
  if (format === "percent") return `${value.toFixed(1)}%`;
  if (format === "decimal") return value.toFixed(2);
  if (format === "duration") {
    const minutes = Math.floor(value / 60);
    const seconds = Math.round(value % 60);
    return `${minutes}분 ${seconds}초`;
  }
  return numberFormat.format(value);
}

export function formatChange(change: number) {
  if (Math.abs(change) < 0.05) return "변화 없음";
  return `${change > 0 ? "+" : ""}${change.toFixed(1)}%`;
}

/**
 * changeTone answers "is this movement good?" instead of "is this movement up?".
 * A rising share of first-time visitors, for example, is not automatically good,
 * so a neutral goal is never coloured as progress or regression.
 */
export function changeTone(kpi: InsightKPI): "good" | "bad" | "flat" {
  if (kpi.goal === "neutral" || Math.abs(kpi.change_percent) < 1) return "flat";
  const rising = kpi.change_percent > 0;
  return rising === (kpi.goal === "higher") ? "good" : "bad";
}

export const severityLabel: Record<FindingSeverity, string> = {
  critical: "심각",
  warning: "경고",
  info: "참고",
  positive: "양호",
};

const lifecycleLabel: Record<string, string> = {
  new: "신규 방문자",
  returning: "재방문자",
};

export function lifecycleName(kind: string) {
  return lifecycleLabel[kind] || kind;
}

function table(header: string[], rows: string[][]) {
  return [
    `| ${header.join(" | ")} |`,
    `| ${header.map(() => "---").join(" | ")} |`,
    ...rows.map((row) => `| ${row.join(" | ")} |`),
  ].join("\n");
}

function localDate(value: string) {
  return value.slice(0, 10);
}

/**
 * buildInsightMarkdown renders the whole report as a digest that can be pasted
 * into a ticket, a wiki page or a chat message without any rework. Findings come
 * first because they carry the conclusion; the tables below them are the evidence.
 */
export function buildInsightMarkdown(
  report: VisitorInsightReport,
  siteName: string,
  anomalies?: AnomalyReport,
): string {
  const sections: string[] = [];
  sections.push(
    `# 방문자 인사이트 · ${siteName} · ${report.environment.toUpperCase()}`,
  );
  sections.push(report.headline);
  sections.push(
    `분석 기간 ${localDate(report.from)} ~ ${localDate(report.to)} (비교 기간 ${localDate(report.previous_from)} ~ ${localDate(report.previous_to)}${report.timezone ? `, ${report.timezone} 기준` : ""})`,
  );

  if (anomalies?.detected.length) {
    sections.push(
      `## 이상 감지 (${anomalies.evaluated_date.slice(0, 10)} 기준, 같은 요일 최근 ${anomalies.baseline_weeks}주 비교)`,
    );
    sections.push(
      anomalies.detected
        .map(
          (anomaly) =>
            `- [${anomalySeverityLabel[anomaly.severity] || anomaly.severity}] ${anomaly.label}: ${anomaly.evidence}${anomaly.action ? ` → ${anomaly.action}` : ""}`,
        )
        .join("\n"),
    );
  }

  if (report.findings.length) {
    sections.push("## 핵심 인사이트");
    sections.push(
      report.findings
        .map((finding, index) =>
          [
            `${index + 1}. [${severityLabel[finding.severity] || finding.severity}] ${finding.title}`,
            `   - 근거: ${finding.evidence}`,
            `   - 원인 후보: ${finding.cause}`,
            `   - 다음 행동: ${finding.action}`,
          ].join("\n"),
        )
        .join("\n"),
    );
  }

  if (report.kpis.length) {
    sections.push("## 지표 (이전 동일 기간 대비)");
    sections.push(
      table(
        ["지표", "현재", "이전", "변화"],
        report.kpis.map((kpi) => [
          kpi.label,
          formatInsightValue(kpi.current, kpi.format),
          formatInsightValue(kpi.previous, kpi.format),
          formatChange(kpi.change_percent),
        ]),
      ),
    );
  }

  if (report.lifecycle.length) {
    sections.push("## 신규 · 재방문");
    sections.push(
      table(
        ["구분", "방문자", "비중", "1인당 방문", "전환율"],
        report.lifecycle.map((row) => [
          lifecycleName(row.kind),
          numberFormat.format(row.users),
          `${row.share_percent.toFixed(1)}%`,
          row.sessions_per_user.toFixed(2),
          `${row.conversion_rate.toFixed(1)}%`,
        ]),
      ),
    );
  }

  if (report.channels.length) {
    sections.push("## 유입 채널");
    sections.push(
      table(
        ["채널", "방문자", "비중", "전환율", "전기간 대비"],
        report.channels.map((row) => [
          row.channel,
          numberFormat.format(row.users),
          `${row.user_share_percent.toFixed(1)}%`,
          `${row.conversion_rate.toFixed(1)}%`,
          formatChange(row.change_percent),
        ]),
      ),
    );
  }

  if (report.landing_pages.length) {
    sections.push("## 진입 페이지");
    sections.push(
      table(
        ["페이지", "세션", "비중", "이탈률", "전환율"],
        report.landing_pages.map((row) => [
          row.page,
          numberFormat.format(row.sessions),
          `${row.session_share_percent.toFixed(1)}%`,
          `${row.bounce_rate.toFixed(1)}%`,
          `${row.conversion_rate.toFixed(1)}%`,
        ]),
      ),
    );
  }

  const actionable = report.audiences.filter((audience) => audience.users > 0);
  if (actionable.length) {
    sections.push("## 실행 대상");
    sections.push(
      actionable
        .map(
          (audience) =>
            `- ${audience.label}: ${numberFormat.format(audience.users)}명 → ${audience.action}`,
        )
        .join("\n"),
    );
  }

  if (report.notes.length) {
    sections.push("## 해석 주의");
    sections.push(report.notes.map((note) => `- ${note}`).join("\n"));
  }
  return sections.join("\n\n");
}
