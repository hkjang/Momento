import assert from "node:assert/strict";
import test from "node:test";
import {
  mcpMetadataUrl,
  mcpOauthReadiness,
  mcpResource,
} from "../src/pages/mcpOauth.ts";

test("리소스 식별자는 명시 값이 우선이고, 없으면 Public URL + /mcp 다", () => {
  assert.equal(
    mcpResource(
      { resource: " https://mcp.example/mcp " },
      { public_url: "https://web.example" },
    ),
    "https://mcp.example/mcp",
  );
  assert.equal(
    mcpResource({ resource: "" }, { public_url: "https://web.example/" }),
    "https://web.example/mcp",
  );
  assert.equal(mcpResource({}, {}), "");
});

test("메타데이터 주소는 origin 아래 경로별 well-known 이다", () => {
  assert.equal(
    mcpMetadataUrl("https://web.example:8443/mcp"),
    "https://web.example:8443/.well-known/oauth-protected-resource/mcp",
  );
  assert.equal(mcpMetadataUrl(""), "");
  assert.equal(mcpMetadataUrl("not a url"), "");
});

test("켜도 동작하지 않을 이유를 발급자·주소 순으로 말한다", () => {
  const noIssuer = mcpOauthReadiness({ enabled: true }, {}, {});
  assert.equal(noIssuer.ready, false);
  assert.match(noIssuer.reason, /Issuer URL/);
  const noAddress = mcpOauthReadiness(
    { enabled: true },
    { issuer_url: "https://kc.example/realms/x" },
    {},
  );
  assert.equal(noAddress.ready, false);
  assert.match(noAddress.reason, /Public URL/);
  assert.deepEqual(
    mcpOauthReadiness(
      { enabled: true },
      { issuer_url: "https://kc.example/realms/x" },
      { public_url: "https://web.example" },
    ),
    { ready: true, reason: "" },
  );
});
