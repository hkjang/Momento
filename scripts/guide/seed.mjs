// Fills a throwaway Momento deployment with fake activity so the guide's
// screenshots show populated screens. Everything it creates is fictional:
// people are EMP0001…, departments are generic, the site is "데모 포털".
//
//   MOMENTO_GUIDE_BASE_URL=http://127.0.0.1:8080 \
//   MOMENTO_GUIDE_ADMIN_EMAIL=... MOMENTO_GUIDE_ADMIN_PASSWORD=... node seed.mjs
//   node seed.mjs --cleanup     # removes the site and network ranges it made
//
// It only adds: one site, its events, a few segments/goals/targets, two network
// ranges and two demo users. No global setting is touched, so there is nothing
// to restore — cleanup deletes what was added and nothing else.

import { SITE_NAME, SEED_MARKER, api, login, readTarget } from "./common.mjs";

const target = readTarget();
const cookie = await login(target);
const call = api(target.baseURL, cookie);
const cleanup = process.argv.includes("--cleanup");

const NETWORKS = [
  { name: "본사 사무실", cidr: "127.0.0.0/8", description: SEED_MARKER, internal: true },
  { name: "지사 VPN", cidr: "10.99.0.0/16", description: SEED_MARKER, internal: true },
];

if (cleanup) {
  const sites = await call("GET", "/api/v1/sites");
  for (const site of sites.filter((s) => s.name === SITE_NAME)) {
    await call("DELETE", `/api/v1/sites/${site.id}`);
    console.log("사이트 삭제", site.site_id);
  }
  const networks = await call("GET", "/api/v1/networks");
  for (const network of networks.filter((n) => n.description === SEED_MARKER)) {
    await call("DELETE", `/api/v1/networks/${network.id}`);
    console.log("네트워크 삭제", network.name);
  }
  process.exit(0);
}

// ---------------------------------------------------------------- site & keys
const existing = (await call("GET", "/api/v1/sites")).find((s) => s.name === SITE_NAME);
if (existing) {
  console.error(`${SITE_NAME} 사이트가 이미 있습니다. 먼저 --cleanup 을 실행하세요.`);
  process.exit(1);
}
const site = await call("POST", "/api/v1/sites", {
  name: SITE_NAME,
  service_name: "사내 포털",
  allowed_domains: ["portal.example.com"],
  timezone: "Asia/Seoul",
});
const siteKey = site.site_id;
const serverKey = site.server_api_key;
console.log("사이트 생성", siteKey);

for (const network of NETWORKS) {
  const known = (await call("GET", "/api/v1/networks")).find((n) => n.cidr === network.cidr);
  if (!known) await call("POST", "/api/v1/networks", network);
}

// User Explorer asks for a 365-day timeline; the shipped policy allows 180, so
// the demo site's own policy (deleted with the site) is widened for the capture.
await call("PUT", `/api/v1/sites/${siteKey}/query-policy`, {
  max_exact_days: 365, max_complexity_score: 90, background_threshold: 60, fast_sample_percent: 10, preview_sample_percent: 1,
});

await call("POST", "/api/v1/event-definitions", {
  site_id: siteKey,
  name: "document_submitted",
  description: "결재 문서 제출",
  schema: { type: "object", properties: { category: { type: "string" } } },
  validation_mode: "warn",
  conversion: true,
});
await call("POST", "/api/v1/event-definitions", {
  site_id: siteKey,
  name: "feature_used",
  description: "기능 사용",
  schema: { type: "object", properties: { feature: { type: "string" } } },
  validation_mode: "warn",
  conversion: false,
});

// ---------------------------------------------------------------- fake people
let seed = 20260911;
const rand = () => {
  // Deterministic, so two runs draw the same screens.
  seed = (seed * 1103515245 + 12345) % 2147483648;
  return seed / 2147483648;
};
const pick = (list) => list[Math.floor(rand() * list.length)];

const DEPARTMENTS = ["인사팀", "재무팀", "플랫폼개발팀", "영업1팀", "마케팅팀", "총무팀"];
const ORGANIZATIONS = ["본사", "연구소", "지사"];
const people = Array.from({ length: 48 }, (_, i) => ({
  id: `EMP${String(i + 1).padStart(4, "0")}`,
  department: DEPARTMENTS[i % DEPARTMENTS.length],
  organization: ORGANIZATIONS[i % ORGANIZATIONS.length],
  desktop: `v-d-${i + 1}`,
  mobile: i % 3 === 0 ? `v-m-${i + 1}` : null,
}));
const anonymous = Array.from({ length: 14 }, (_, i) => `v-anon-${i + 1}`);

const PAGES = [
  ["/home", "홈"],
  ["/notice", "공지사항"],
  ["/approval", "전자결재"],
  ["/approval/new", "결재 작성"],
  ["/hr/leave", "휴가 신청"],
  ["/docs", "문서함"],
  ["/search", "통합 검색"],
  ["/help", "도움말"],
];
const FEATURES = ["전자결재", "휴가신청", "문서검색", "공지열람", "근태조회"];
const SEARCHES = [
  ["연차 신청", 7],
  ["출장 정산", 3],
  ["재택 근무 규정", 5],
  ["법인카드", 4],
  ["사원증 재발급", 0],
  ["주차 등록", 0],
];
const TRAFFIC = [
  { source: "", medium: "" },
  { source: "", medium: "" },
  { source: "intranet", medium: "portal" },
  { source: "notice", medium: "internal_notice" },
  { source: "mail", medium: "email", campaign: "9월 안전교육" },
  { source: "messenger", medium: "internal_message" },
];
const DEVICES = {
  desktop: { type: "desktop", browser: "Chrome", os: "Windows", language: "ko-KR", screen: "1920x1080" },
  mobile: { type: "mobile", browser: "Safari", os: "iOS", language: "ko-KR", screen: "390x844" },
};

// ---------------------------------------------------------------- activity
const DAY = 24 * 60 * 60 * 1000;
const now = Date.now();
const batches = [];
let sessionCounter = 0;

function session(day, person, visitorID, device, options = {}) {
  const startHour = 8 + Math.floor(rand() * 10);
  const start = now - day * DAY - (now % DAY) + startHour * 3600 * 1000 + Math.floor(rand() * 3600 * 1000);
  const id = `s-${++sessionCounter}`;
  const traffic = options.traffic || pick(TRAFFIC);
  const events = [];
  let at = start;
  let page = PAGES[0];
  const event = (name, properties = {}, extra = {}) => {
    at += 1000 + Math.floor(rand() * 40000);
    events.push({
      name,
      timestamp: at,
      properties,
      context: {
        page: { url: `https://portal.example.com${page[0]}`, title: page[1], referrer: "" },
        device: DEVICES[device],
        traffic,
      },
      ...extra,
    });
  };
  event("page_view");
  event("web_vital", { metric: "LCP", value: device === "mobile" ? 2400 + Math.floor(rand() * 2600) : 900 + Math.floor(rand() * 1200), rating: "good" });
  event("web_vital", { metric: "INP", value: device === "mobile" ? 180 + Math.floor(rand() * 200) : 60 + Math.floor(rand() * 120), rating: "good" });
  const hops = 1 + Math.floor(rand() * 4);
  for (let i = 0; i < hops; i++) {
    page = pick(PAGES.slice(1));
    event("page_view");
    if (rand() < 0.5) event("click", { element_text: pick(["열기", "저장", "다음", "신청"]), feature: pick(FEATURES) });
    if (rand() < 0.45) event("feature_used", { feature: pick(FEATURES) });
    if (page[0] === "/search") {
      const [query, count] = pick(SEARCHES);
      event("search", { query, result_count: count, query_length: query.length, query_words: query.split(" ").length });
      if (count > 0 && rand() < 0.6) event("search_click", { query, position: 1 + Math.floor(rand() * 3) });
      else if (count === 0 && rand() < 0.5) event("search", { query: query.split(" ")[0], result_count: 0, query_words: 1 });
    }
    if (page[0] === "/help" && rand() < 0.6) event("dead_click", { element_text: "도움말" });
    if (page[0] === "/approval/new" && rand() < 0.25) event("rage_click", { element_text: "제출", clicks: 4 });
    if (page[0] === "/hr/leave" && rand() < 0.2) event("form_retry", { form_id: "leave", reason: "validation" });
  }
  if (rand() < 0.08) event("error", { message: "TypeError: Cannot read properties of undefined" });
  if (options.convert) {
    page = PAGES[3];
    event("document_submitted", { category: pick(["휴가", "지출", "구매", "출장"]), amount: 100000 + Math.floor(rand() * 900000) });
  }
  if (options.purchase) {
    page = PAGES[5];
    const item = pick([["sku-1", "연차 신청서", 500], ["sku-2", "출장 신청서", 800], ["sku-3", "명함 신청", 1200]]);
    event("view_item", { item_id: item[0], item_name: item[1] });
    event("add_to_cart", { item_id: item[0], item_name: item[1] });
    if (rand() < 0.7) {
      event("begin_checkout", { item_id: item[0], item_name: item[1] });
      if (rand() < 0.7) event("purchase", { value: item[2] * 2, transaction_id: `tx-${id}`, items: [{ item_id: item[0], item_name: item[1], category: "서식", quantity: 2, price: item[2] }] });
    }
  }
  if (options.ai) {
    page = PAGES[6];
    event("ai_model_call", { model: "demo-assistant", provider: "internal", success: rand() < 0.9, latency_ms: 500 + Math.floor(rand() * 1500), input_tokens: 800 + Math.floor(rand() * 600), output_tokens: 200 + Math.floor(rand() * 300), cost: 0.01 });
  }
  event("user_engagement", { active_seconds: 30 + Math.floor(rand() * 240) });
  batches.push({
    site_id: siteKey,
    environment: "prd",
    tracking_key: serverKey,
    visitor_id: visitorID,
    session_id: id,
    user_id: person ? person.id : undefined,
    user_properties: person ? { department: person.department, organization: person.organization } : undefined,
    events,
  });
}

for (let day = 70; day >= 1; day--) {
  const weekday = new Date(now - day * DAY).getDay();
  const weekend = weekday === 0 || weekday === 6;
  const headcount = weekend ? 4 : 16 + Math.floor(rand() * 8);
  const active = new Set();
  while (active.size < headcount) active.add(pick(people));
  for (const person of active) {
    const mobile = person.mobile && rand() < 0.3;
    session(day, person, mobile ? person.mobile : person.desktop, mobile ? "mobile" : "desktop", {
      convert: rand() < 0.35,
      purchase: rand() < 0.15,
      ai: rand() < 0.2,
    });
  }
  const strangers = weekend ? 1 : 2 + Math.floor(rand() * 3);
  for (let i = 0; i < strangers; i++) {
    session(day, null, pick(anonymous), "desktop", { convert: rand() < 0.1, traffic: pick(TRAFFIC.slice(2)) });
  }
}

let accepted = 0;
for (const batch of batches) {
  const res = await fetch(`${target.baseURL}/collect/v1/events`, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      "user-agent": batch.events[0].context.device.type === "mobile"
        ? "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
        : "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36",
    },
    body: JSON.stringify(batch),
  });
  if (!res.ok) throw new Error(`collect -> ${res.status}: ${await res.text()}`);
  accepted += batch.events.length;
}
console.log(`세션 ${batches.length}개, 이벤트 ${accepted}건 전송`);

// A couple of monitoring pings so the traffic class column has something to show.
for (let day = 3; day >= 1; day--) {
  const res = await fetch(`${target.baseURL}/collect/v1/events`, {
    method: "POST",
    headers: { "content-type": "application/json", "user-agent": "UptimeRobot/2.0 healthcheck" },
    body: JSON.stringify({
      site_id: siteKey, environment: "prd", tracking_key: serverKey, visitor_id: "v-monitor", session_id: `s-monitor-${day}`,
      events: [{ name: "page_view", timestamp: now - day * DAY, context: { page: { url: "https://portal.example.com/home", title: "홈" } } }],
    }),
  });
  if (!res.ok) throw new Error(`collect -> ${res.status}: ${await res.text()}`);
}

// ---------------------------------------------------------------- saved objects
await call("POST", "/api/v1/segments", {
  site_id: siteKey, name: "모바일 방문", description: "모바일 기기에서 접속한 방문", shared: true,
  definition: { combinator: "and", rules: [{ field: "device.type", operator: "=", value: "mobile" }] },
});
await call("POST", "/api/v1/segments", {
  site_id: siteKey, name: "반복 방문 미전환", description: "3회 이상 방문했지만 전환하지 않은 사람", shared: true,
  definition: { combinator: "and", rules: [{ field: "entity.sessions", operator: ">=", value: 3 }, { field: "entity.conversions", operator: "=", value: 0 }] },
});
await call("POST", "/api/v1/segments", {
  site_id: siteKey, name: "사람 트래픽만", description: "봇·모니터링 제외", shared: true,
  definition: { combinator: "and", rules: [{ field: "traffic.class", operator: "=", value: "normal" }] },
});
await call("POST", `/api/v1/sites/${siteKey}/metric-goals`, {
  name: "월간 활성 사용자", metric_name: "users", target_value: 60, comparator: "gte", period: "month", environment: "prd",
});
await call("POST", `/api/v1/sites/${siteKey}/metric-goals`, {
  name: "주간 결재 제출", metric_name: "conversions", target_value: 40, comparator: "gte", period: "week", environment: "prd",
});
for (const [organization, department, feature, eligible] of [
  ["본사", "인사팀", "휴가신청", 8],
  ["본사", "재무팀", "전자결재", 8],
  ["연구소", "플랫폼개발팀", "문서검색", 8],
]) {
  await call("POST", `/api/v1/sites/${siteKey}/adoption-targets`, { organization, department, feature, eligible_users: eligible });
}
await call("POST", `/api/v1/sites/${siteKey}/annotations`, {
  kind: "release", title: "포털 v3.2 배포", description: "결재 작성 화면 개편", environment: "prd", occurred_at: new Date(now - 12 * DAY).toISOString(),
});

// Two demo accounts, so the user list shows more than the bootstrap admin.
const demoPassword = process.env.MOMENTO_GUIDE_DEMO_PASSWORD;
if (demoPassword) {
  const users = await call("GET", "/api/v1/users");
  for (const [email, name, department, role] of [
    ["hong@example.com", "홍길동", "데이터분석팀", "analyst"],
    ["kim@example.com", "김영희", "플랫폼개발팀", "viewer"],
  ]) {
    if (!users.some((u) => u.email === email)) {
      await call("POST", "/api/v1/users", { email, display_name: name, department, organization_name: "데모 회사", role, password: demoPassword });
    }
  }
} else {
  console.log("MOMENTO_GUIDE_DEMO_PASSWORD 가 없어 데모 계정은 만들지 않습니다.");
}

console.log("완료:", siteKey);
