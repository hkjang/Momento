import assert from "node:assert/strict";
import test from "node:test";
import {
  buildTraceMarkdown,
  entryExit,
  formatDuration,
  formatGap,
  sessionTitle,
} from "../src/pages/visitorTrace.ts";

const session = {
  session_id: "sess-1",
  visitor_id: "visitor-desktop",
  started_at: "2026-08-11T01:00:00.000Z",
  ended_at: "2026-08-11T01:12:30.000Z",
  duration_seconds: 750,
  event_count: 3,
  page_views: 2,
  conversions: 1,
  engaged: true,
  partial: false,
  device_type: "desktop",
  browser: "Chrome",
  os: "Windows",
  source: "intranet",
  medium: "portal",
  campaign: "",
  network: "본사 3층",
  landing_page: "https://portal.internal/home",
  exit_page: "https://portal.internal/done",
  interaction_count: 7,
  active_engagement_ms: 240000,
  events: [
    {
      event_id: "e1",
      event_name: "page_view",
      timestamp: "2026-08-11T01:00:00.000Z",
      visitor_id: "visitor-desktop",
      session_id: "sess-1",
      user_id: null,
      page_url: "https://portal.internal/home",
      page_title: "홈",
      is_conversion: false,
      properties: {},
      seconds_since_previous: 0,
      environment: "prd",
      contract_version: 1,
      traffic_class: "normal",
    },
    {
      event_id: "e2",
      event_name: "login",
      timestamp: "2026-08-11T01:02:15.000Z",
      visitor_id: "visitor-desktop",
      session_id: "sess-1",
      user_id: "EMP001",
      page_url: "https://portal.internal/home",
      is_conversion: false,
      properties: { method: "sso" },
      seconds_since_previous: 135,
      marker: "identified",
      environment: "prd",
      contract_version: 1,
      traffic_class: "normal",
    },
    {
      event_id: "e3",
      event_name: "approval_submit",
      timestamp: "2026-08-11T01:12:30.000Z",
      visitor_id: "visitor-desktop",
      session_id: "sess-1",
      user_id: "EMP001",
      page_url: "https://portal.internal/done",
      is_conversion: true,
      properties: { feature: "approval" },
      seconds_since_previous: 615,
      environment: "prd",
      contract_version: 1,
      traffic_class: "normal",
    },
  ],
};

const trace = {
  visitor_id: "visitor-desktop",
  user_id: "EMP001",
  scope: "person",
  visitor_ids: ["visitor-desktop", "visitor-mobile"],
  user_properties: { department: "디지털플랫폼" },
  summary: {
    first_seen: "2026-05-02T00:10:00.000Z",
    last_seen: "2026-08-11T01:12:30.000Z",
    events: 412,
    sessions: 37,
    conversions: 9,
    page_views: 210,
    active_days: 24,
    top_pages: [],
    top_features: [],
    devices: [],
    networks: [],
  },
  identity_links: [
    {
      visitor_id: "visitor-desktop",
      first_seen: "2026-05-02T00:10:00.000Z",
      linked_at: "2026-05-02T00:12:00.000Z",
      last_seen: "2026-08-11T01:12:30.000Z",
      source: "identify",
      confidence: 1,
    },
  ],
  other_sites: [
    {
      site_id: "SITE_HR",
      name: "인사 시스템",
      first_seen: "2026-06-01T00:00:00.000Z",
      last_seen: "2026-08-01T00:00:00.000Z",
    },
  ],
  sessions: [session],
  window: { from: "2025-08-11T15:00:00Z", to: "2026-08-11T15:00:00Z", environment: "prd" },
  paging: { limit: 200, has_more: true, next_before: "2026-08-01T00:00:00Z" },
};

test("체류·간격을 사람이 읽는 단위로 표기한다", () => {
  assert.equal(formatDuration(45), "45초");
  assert.equal(formatDuration(90), "1분 30초");
  assert.equal(formatDuration(600), "10분");
  assert.equal(formatDuration(3600), "1시간");
  assert.equal(formatDuration(3900), "1시간 5분");
  assert.equal(formatDuration(90000), "1일 1시간");
  assert.equal(formatGap(0), "", "세션 첫 이벤트에는 간격을 표시하지 않는다");
  assert.equal(formatGap(135), "+2분 15초");
});

test("세션 한 줄 요약에 시각·체류·채널·기기가 들어간다", () => {
  const title = sessionTitle(session);
  assert.match(title, /12분 30초/);
  assert.match(title, /intranet \/ portal/);
  assert.match(title, /desktop · Chrome/);
});

test("진입과 종료가 다르면 경로로 표시한다", () => {
  assert.equal(
    entryExit(session),
    "https://portal.internal/home → https://portal.internal/done",
  );
  assert.equal(
    entryExit({ ...session, exit_page: session.landing_page }),
    "https://portal.internal/home",
  );
  assert.equal(entryExit({ ...session, landing_page: "", exit_page: "" }), "");
});

test("추적 기록 Markdown은 사람 단위 맥락과 세션 흐름을 담는다", () => {
  const markdown = buildTraceMarkdown(trace, "사내 포털");

  assert.match(markdown, /^# 방문자 추적 · 사내 포털 · User EMP001/);
  assert.match(markdown, /사람 단위\(연결 Visitor 2개\)/);
  assert.match(markdown, /활동일 24일 · 세션 37회 · 이벤트 412건 · 전환 9건/);
  assert.match(markdown, /다른 서비스 활동: 인사 시스템\(SITE_HR\)/);
  assert.match(markdown, /## 식별 연결/);
  assert.match(markdown, /## 세션 타임라인/);
  assert.match(markdown, /- 이벤트 3건 · 페이지뷰 2 · 전환 1 · 참여 세션 · 본사 3층/);
  assert.match(markdown, /- 경로: .*home → .*done/);
  assert.match(markdown, /login — .*이 시점에 사용자 식별/);
  assert.match(markdown, /approval_submit — .*전환/);
  assert.match(markdown, /\(\+2분 15초\)/);
  assert.match(markdown, /이전 기록이 더 있습니다/);
});

test("활동이 없으면 빈 타임라인임을 명시한다", () => {
  const markdown = buildTraceMarkdown(
    { ...trace, sessions: [], identity_links: [], other_sites: [], paging: { ...trace.paging, has_more: false } },
    "사내 포털",
  );
  assert.match(markdown, /선택한 기간에 활동이 없습니다/);
  assert.doesNotMatch(markdown, /## 식별 연결/);
  assert.doesNotMatch(markdown, /이전 기록이 더 있습니다/);
});

test("익명 방문자는 Visitor ID를 제목으로 쓴다", () => {
  const markdown = buildTraceMarkdown(
    { ...trace, user_id: null, scope: "device", visitor_ids: ["visitor-desktop"] },
    "사내 포털",
  );
  assert.match(markdown, /^# 방문자 추적 · 사내 포털 · Visitor visitor-desktop/);
  assert.match(markdown, /범위: 단일 Visitor/);
});
