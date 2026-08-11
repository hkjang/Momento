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
