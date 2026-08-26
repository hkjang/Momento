export type SDKTrackingMode = "full" | "consent-required" | "cookieless";

export interface SDKSnippetOptions {
  endpoint: string;
  siteId: string;
  environment: string;
  mode: SDKTrackingMode;
  /**
   * Same-origin path that proxies the collector. Set it when the measured
   * application ships a Content-Security-Policy it cannot change: the request
   * then stays first party and `connect-src 'self'` is enough.
   */
  proxyPath?: string;
  /**
   * Include the search term itself. Off by default: a search box receives text
   * a person typed, so the tracker reports the shape of the query (length, word
   * count, whether it was repeated) unless the site opts into the words.
   */
  collectSearchTerms?: boolean;
  /**
   * The site's session timeout. Sessions are decided in the browser, so this is
   * the only way the console setting reaches the tracker; a site that changes it
   * has to update the installed snippet for the change to take effect.
   */
  sessionTimeoutMinutes?: number;
}

function escapeAttribute(value: string) {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll('"', "&quot;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}

export function buildSDKSnippet(options: SDKSnippetOptions) {
  const endpoint = options.endpoint.replace(/\/+$/, "");
  const proxyPath = normalizeProxyPath(options.proxyPath);
  return [
    "<!-- Momento JavaScript SDK: <head> 안에 추가 -->",
    "<script",
    "  async",
    `  src="${escapeAttribute(endpoint)}/tracker.js"`,
    `  data-site-id="${escapeAttribute(options.siteId)}"`,
    `  data-environment="${escapeAttribute(options.environment)}"`,
    '  data-contract-version="1"',
    `  data-mode="${escapeAttribute(options.mode)}"`,
    ...(options.sessionTimeoutMinutes && options.sessionTimeoutMinutes !== 30
      ? [`  data-session-timeout="${options.sessionTimeoutMinutes}"`]
      : []),
    ...(proxyPath ? [`  data-endpoint="${escapeAttribute(proxyPath)}"`] : []),
    '  data-auto-rum="true"',
    '  data-frustration-signals="true"',
    '  data-search-tracking="true"',
    ...(options.collectSearchTerms ? ['  data-collect-search-terms="true"'] : []),
    '  data-debug="false"',
    "></script>",
  ].join("\n");
}

function normalizeProxyPath(value?: string) {
  const trimmed = (value || "").trim().replace(/\/+$/, "");
  if (!trimmed) return "";
  return trimmed.startsWith("/") ? trimmed : `/${trimmed}`;
}

export interface CSPGuidance {
  origin: string;
  header: string;
  meta: string;
  proxy: string;
}

/**
 * buildCSPGuidance produces what a measured application has to allow. Without it
 * a strict policy such as `connect-src 'self' ws: wss:` silently blocks every
 * collector request, which is the most common "no data" report.
 */
export function buildCSPGuidance(endpoint: string, proxyPath = "/momento"): CSPGuidance {
  const origin = collectorOrigin(endpoint);
  const path = normalizeProxyPath(proxyPath) || "/momento";
  return {
    origin,
    header: `Content-Security-Policy: script-src 'self' ${origin}; connect-src 'self' ${origin}`,
    meta: `<meta http-equiv="Content-Security-Policy" content="script-src 'self' ${origin}; connect-src 'self' ${origin}">`,
    proxy: [
      `location ${path}/ {`,
      `  proxy_pass ${origin}/;`,
      "  proxy_set_header Host $host;",
      "  proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;",
      "  proxy_set_header X-Forwarded-Proto $scheme;",
      "}",
    ].join("\n"),
  };
}

function collectorOrigin(endpoint: string) {
  const trimmed = endpoint.trim().replace(/\/+$/, "");
  try {
    const parsed = new URL(trimmed);
    return `${parsed.protocol}//${parsed.host}`;
  } catch {
    return trimmed;
  }
}

export const identifyAndEventExample = `// 로그인 완료 시: 이메일 대신 사내 비식별 ID를 사용하세요.
analytics.identify("INTERNAL_USER_001", {
  department: "Digital Platform",
  organization: "Technology"
});

// 같은 세션의 모든 이벤트에 붙일 업무 Context
analytics.setSessionProperties({
  login_status: "authenticated",
  workflow: "approval"
});

// 분석할 업무 행동. Event Contract와 이름을 맞추세요.
analytics.track("feature_used", {
  feature: "document_search",
  button: "advanced_filter"
});`;

export const consentExample = `// 사용자가 분석 수집에 동의했을 때
analytics.consent.grant();

// 동의를 거부하거나 철회했을 때
analytics.consent.deny();
analytics.consent.revoke();`;

/**
 * signalInstrumentation documents the two hooks that make automatic detection
 * more precise. Neither is required: without them searches are still counted
 * and frustration is still detected, but the zero-result rate and the clicked
 * position cannot be known from the outside.
 */
export const signalInstrumentation = `<!-- 검색 결과 수를 알려주면 Zero Result 비율이 정확해집니다 -->
<div data-momento-search-results="\${results.length}">
  <!-- 결과 순위를 알려주면 몇 번째 결과를 열었는지 기록됩니다 -->
  <a href="/doc/1" data-momento-search-position="1">첫 번째 결과</a>
</div>

<!-- URL이 바뀌지 않는 검색이라면 직접 알려주세요 -->
<script>
  analytics.trackSearch(query, results.length);
</script>

<!-- 정상 동작하는 커스텀 위젯이 Dead Click으로 잡히면 제외하세요 -->
<div data-momento-ignore-dead-click>...</div>`;
