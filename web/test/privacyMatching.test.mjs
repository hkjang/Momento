import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// Two lists on this form matched differently — the URL parameters case
// sensitively, the event properties case insensitively — and the form said
// nothing about either. An operator types the name they see in their own URLs;
// whether the rule then works depends on a difference no label mentioned.
//
// The browser SDK never transmits a query string at all: it reads utm_* and the
// configured search parameters and sends origin + pathname. So the two URL
// settings on this form govern events delivered with a Server API Key, and an
// administrator reading the form had no way to know that.
function source(path) {
  return readFileSync(new URL(`../src/${path}`, import.meta.url), "utf8");
}

test("URL·Property 필터 설정은 무엇에 적용되는지 화면에서 말한다", () => {
  const admin = source("pages/AdminPage.tsx");
  const masked = admin.slice(admin.indexOf('label="마스킹할 URL Parameter"'));
  const maskedField = masked.slice(0, masked.indexOf("</TextField>"));
  assert.match(
    maskedField,
    /helperText=/,
    "마스킹할 URL Parameter에 설명이 없다: 대소문자 규칙도, 브라우저 SDK에는 적용되지 않는다는 것도 화면에 없다",
  );
  assert.match(
    maskedField,
    /Server API Key/,
    "브라우저 SDK가 Query String을 보내지 않는다는 사실이 화면에 없다: 관리자는 이 목록이 자기 트래픽에 적용된다고 읽는다",
  );

  const blocked = admin.slice(admin.indexOf('label="차단할 Event Property"'));
  assert.match(
    blocked.slice(0, blocked.indexOf("</TextField>")),
    /helperText=/,
    "차단할 Event Property에 설명이 없다",
  );
});
