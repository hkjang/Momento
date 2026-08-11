export interface TraceEvent {
  event_id: string;
  event_name: string;
  timestamp: string;
  visitor_id: string;
  session_id: string;
  user_id?: string | null;
  page_url?: string | null;
  page_title?: string | null;
  referrer?: string | null;
  is_conversion: boolean;
  properties?: Record<string, unknown> | null;
  seconds_since_previous: number;
  marker?: string;
  environment: string;
  contract_version: number;
  traffic_class: string;
}

export interface TraceSession {
  session_id: string;
  visitor_id: string;
  started_at: string;
  ended_at: string;
  duration_seconds: number;
  events: TraceEvent[];
  event_count: number;
  page_views: number;
  conversions: number;
  engaged: boolean;
  partial: boolean;
  device_type: string;
  browser: string;
  os: string;
  source: string;
  medium: string;
  campaign: string;
  network: string;
  landing_page: string;
  exit_page: string;
  interaction_count: number;
  active_engagement_ms: number;
}

export interface TraceTop {
  value: string;
  events: number;
  last_seen: string;
}

export interface TraceSummary {
  first_seen?: string | null;
  last_seen?: string | null;
  events: number;
  sessions: number;
  conversions: number;
  page_views: number;
  active_days: number;
  top_pages: TraceTop[];
  top_features: TraceTop[];
  devices: TraceTop[];
  networks: TraceTop[];
}

export interface TraceIdentityLink {
  visitor_id: string;
  first_seen: string;
  linked_at: string;
  last_seen: string;
  source: string;
  confidence: number;
}

export interface TraceOtherSite {
  site_id: string;
  name: string;
  first_seen: string;
  last_seen: string;
}

export interface VisitorTrace {
  visitor_id: string;
  user_id?: string | null;
  scope: "person" | "device";
  visitor_ids: string[];
  user_properties?: Record<string, unknown> | null;
  summary: TraceSummary;
  identity_links: TraceIdentityLink[];
  other_sites: TraceOtherSite[];
  sessions: TraceSession[];
  window: { from: string; to: string; environment: string };
  paging: { limit: number; has_more: boolean; next_before: string };
}

export interface VisitorSearchResult {
  visitor_id: string;
  user_id?: string | null;
  matched_by: "user_id" | "department" | "organization" | "visitor_id" | "page" | "event";
  matched_value: string;
  first_seen?: string;
  last_seen?: string;
  events?: number;
  sessions?: number;
  conversions?: number;
}

export const matchedByLabel: Record<VisitorSearchResult["matched_by"], string> = {
  user_id: "User ID",
  department: "부서",
  organization: "조직",
  visitor_id: "Visitor ID",
  page: "페이지",
  event: "이벤트",
};

/** formatDuration reads a span the way an analyst says it out loud. */
export function formatDuration(seconds: number): string {
  const total = Math.max(0, Math.round(seconds));
  if (total < 60) return `${total}초`;
  const minutes = Math.floor(total / 60);
  if (minutes < 60) {
    const rest = total % 60;
    return rest ? `${minutes}분 ${rest}초` : `${minutes}분`;
  }
  const hours = Math.floor(minutes / 60);
  const restMinutes = minutes % 60;
  if (hours < 24) return restMinutes ? `${hours}시간 ${restMinutes}분` : `${hours}시간`;
  const days = Math.floor(hours / 24);
  return `${days}일 ${hours % 24}시간`;
}

/**
 * formatGap labels the wait between two events. The first event of a visit has no
 * previous event, so it gets no gap rather than a misleading "0초".
 */
export function formatGap(seconds: number): string {
  if (!seconds || seconds <= 0) return "";
  return `+${formatDuration(seconds)}`;
}

/** sessionTitle summarises one visit in a single line. */
export function sessionTitle(session: TraceSession): string {
  const start = new Date(session.started_at).toLocaleString("ko-KR");
  const channel = session.source
    ? `${session.source}${session.medium ? ` / ${session.medium}` : ""}`
    : "direct";
  const device = [session.device_type, session.browser].filter(Boolean).join(" · ");
  return [start, formatDuration(session.duration_seconds), channel, device]
    .filter(Boolean)
    .join(" · ");
}

/** entryExit shows where a visit began and ended, the shape of a real journey. */
export function entryExit(session: TraceSession): string {
  if (!session.landing_page && !session.exit_page) return "";
  if (session.landing_page === session.exit_page) return session.landing_page;
  return `${session.landing_page || "?"} → ${session.exit_page || "?"}`;
}

function propertySummary(properties?: Record<string, unknown> | null) {
  if (!properties) return "";
  const entries = Object.entries(properties).filter(
    ([, value]) => value !== null && value !== undefined && value !== "",
  );
  if (!entries.length) return "";
  return entries
    .slice(0, 6)
    .map(([key, value]) => `${key}=${typeof value === "object" ? JSON.stringify(value) : String(value)}`)
    .join(", ");
}

/**
 * buildTraceMarkdown renders the trace so it can be attached to an incident ticket
 * or a privacy request answer without rebuilding it by hand.
 */
export function buildTraceMarkdown(trace: VisitorTrace, siteName: string): string {
  const subject = trace.user_id
    ? `User ${trace.user_id}`
    : `Visitor ${trace.visitor_id}`;
  const lines: string[] = [
    `# 방문자 추적 · ${siteName} · ${subject}`,
    "",
    `- 범위: ${trace.scope === "person" ? `사람 단위(연결 Visitor ${trace.visitor_ids.length}개)` : "단일 Visitor"}`,
    `- 환경: ${trace.window.environment.toUpperCase()}`,
    `- 최초 활동: ${trace.summary.first_seen ? new Date(trace.summary.first_seen).toLocaleString("ko-KR") : "—"}`,
    `- 최근 활동: ${trace.summary.last_seen ? new Date(trace.summary.last_seen).toLocaleString("ko-KR") : "—"}`,
    `- 활동일 ${trace.summary.active_days}일 · 세션 ${trace.summary.sessions}회 · 이벤트 ${trace.summary.events}건 · 전환 ${trace.summary.conversions}건`,
  ];
  if (trace.other_sites.length) {
    lines.push(
      `- 다른 서비스 활동: ${trace.other_sites.map((site) => `${site.name}(${site.site_id})`).join(", ")}`,
    );
  }
  if (trace.identity_links.length) {
    lines.push("", "## 식별 연결");
    for (const link of trace.identity_links) {
      lines.push(
        `- ${link.visitor_id}: 최초 ${new Date(link.first_seen).toLocaleString("ko-KR")} · 연결 ${new Date(link.linked_at).toLocaleString("ko-KR")} (${link.source})`,
      );
    }
  }
  lines.push("", "## 세션 타임라인");
  if (!trace.sessions.length) {
    lines.push("- 선택한 기간에 활동이 없습니다.");
  }
  for (const session of trace.sessions) {
    lines.push("", `### ${sessionTitle(session)}`);
    const meta = [
      `이벤트 ${session.event_count}건`,
      `페이지뷰 ${session.page_views}`,
      session.conversions ? `전환 ${session.conversions}` : "",
      session.engaged ? "참여 세션" : "비참여",
      session.network || "",
      session.partial ? "이 페이지에 일부 이벤트만 표시" : "",
    ].filter(Boolean);
    lines.push(`- ${meta.join(" · ")}`);
    const path = entryExit(session);
    if (path) lines.push(`- 경로: ${path}`);
    for (const event of session.events) {
      const time = new Date(event.timestamp).toLocaleTimeString("ko-KR");
      const gap = formatGap(event.seconds_since_previous);
      const detail = [
        event.page_url || "",
        propertySummary(event.properties),
        event.marker === "identified" ? "이 시점에 사용자 식별" : "",
        event.is_conversion ? "전환" : "",
      ]
        .filter(Boolean)
        .join(" · ");
      lines.push(
        `  - ${time}${gap ? ` (${gap})` : ""} ${event.event_name}${detail ? ` — ${detail}` : ""}`,
      );
    }
  }
  if (trace.paging.has_more) {
    lines.push("", "> 이전 기록이 더 있습니다. 콘솔에서 계속 불러오세요.");
  }
  return lines.join("\n");
}
