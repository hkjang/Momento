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
    ...(proxyPath ? [`  data-endpoint="${escapeAttribute(proxyPath)}"`] : []),
    '  data-auto-rum="true"',
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
