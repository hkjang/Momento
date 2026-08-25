import assert from "node:assert/strict";
import test from "node:test";
import { buildSDKSnippet } from "../src/pages/sdkGuide.ts";

test("선택한 사이트·환경·동의 모드로 SDK 설치 코드를 만든다", () => {
  const snippet = buildSDKSnippet({
    endpoint: "https://momento.example/",
    siteId: "SITE_123",
    environment: "stg",
    mode: "consent-required",
  });

  assert.match(snippet, /src="https:\/\/momento\.example\/tracker\.js"/);
  assert.match(snippet, /data-site-id="SITE_123"/);
  assert.match(snippet, /data-environment="stg"/);
  assert.match(snippet, /data-mode="consent-required"/);
  assert.match(snippet, /data-auto-rum="true"/);
});

test("SDK 설치 코드의 HTML 속성 값을 이스케이프한다", () => {
  const snippet = buildSDKSnippet({
    endpoint: 'https://example.com/\"><script>',
    siteId: 'SITE_\" onload=\"bad',
    environment: "prd",
    mode: "full",
  });

  assert.doesNotMatch(snippet, /<script><script>/);
  assert.doesNotMatch(snippet, /onload="bad/);
  assert.match(snippet, /&quot;/);
});

test("CSP 안내는 collector Origin과 프록시 대안을 함께 제공한다", async () => {
  const { buildCSPGuidance } = await import("../src/pages/sdkGuide.ts");
  const guidance = buildCSPGuidance("https://momento.kubagents-ofc.koreacb.com/");

  assert.equal(guidance.origin, "https://momento.kubagents-ofc.koreacb.com");
  assert.match(
    guidance.header,
    /connect-src 'self' https:\/\/momento\.kubagents-ofc\.koreacb\.com/,
  );
  assert.match(
    guidance.header,
    /script-src 'self' https:\/\/momento\.kubagents-ofc\.koreacb\.com/,
  );
  assert.match(guidance.meta, /http-equiv="Content-Security-Policy"/);
  assert.match(
    guidance.proxy,
    /proxy_pass https:\/\/momento\.kubagents-ofc\.koreacb\.com\//,
  );
});

test("프록시 경로를 지정하면 설치 코드에 data-endpoint가 붙는다", async () => {
  const { buildSDKSnippet } = await import("../src/pages/sdkGuide.ts");

  const proxied = buildSDKSnippet({
    endpoint: "https://momento.example",
    siteId: "SITE_123",
    environment: "prd",
    mode: "full",
    proxyPath: "momento/",
  });
  assert.match(proxied, /data-endpoint="\/momento"/);

  const direct = buildSDKSnippet({
    endpoint: "https://momento.example",
    siteId: "SITE_123",
    environment: "prd",
    mode: "full",
  });
  assert.doesNotMatch(direct, /data-endpoint/);
});

test("설치 코드는 자동 감지를 켜고 검색어 수집은 요청할 때만 켠다", async () => {
  const { buildSDKSnippet } = await import("../src/pages/sdkGuide.ts");
  const standard = buildSDKSnippet({
    endpoint: "https://momento.example",
    siteId: "SITE_123",
    environment: "prd",
    mode: "full",
  });
  assert.match(standard, /data-frustration-signals="true"/);
  assert.match(standard, /data-search-tracking="true"/);
  assert.doesNotMatch(
    standard,
    /data-collect-search-terms/,
    "검색어는 개인정보가 섞일 수 있으므로 기본값이 아니다",
  );

  const withTerms = buildSDKSnippet({
    endpoint: "https://momento.example",
    siteId: "SITE_123",
    environment: "prd",
    mode: "full",
    collectSearchTerms: true,
  });
  assert.match(withTerms, /data-collect-search-terms="true"/);
});

test("계측 힌트는 결과 수·순위·Dead Click 제외를 모두 안내한다", async () => {
  const { signalInstrumentation } = await import("../src/pages/sdkGuide.ts");
  assert.match(signalInstrumentation, /data-momento-search-results/);
  assert.match(signalInstrumentation, /data-momento-search-position/);
  assert.match(signalInstrumentation, /data-momento-ignore-dead-click/);
  assert.match(signalInstrumentation, /analytics\.trackSearch/);
});
