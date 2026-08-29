import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// Every control on the privacy form is a protection, and the form built its
// value as `local || q.data?.privacy.value || {}`. An empty object draws all of
// them switched off — which is exactly the picture an administrator who turned
// them all off would see — and `set` spreads that same object, so the first
// click wrote it: masked parameters, blocked properties, the PII mode and the
// retention windows, gone, because the settings read had failed.
//
// The component had no error branch at all. It is the only form on this page
// that did.
function source(path) {
  return readFileSync(new URL(`../src/${path}`, import.meta.url), "utf8");
}

function component(source, name) {
  const start = source.indexOf(`function ${name}(`);
  assert.notEqual(start, -1, `${name}을 찾을 수 없다`);
  const next = source.indexOf("\nfunction ", start + 1);
  return source.slice(start, next === -1 ? source.length : next);
}

test("Privacy 설정 폼은 읽지 못한 정책을 '전부 꺼짐'으로 그리지 않는다", () => {
  const privacy = component(source("pages/AdminPage.tsx"), "PrivacyAdmin");

  // 실패를 로딩 중과 구분해서 처리해야 한다.
  assert.match(
    privacy,
    /if \(q\.error\) return <ErrorState/,
    "설정 읽기가 실패해도 폼을 그린다: 모든 보호 장치가 꺼진 화면이 되고, 저장하면 그대로 기록된다",
  );

  // 그리고 응답에 그룹 자체가 없을 때도 마찬가지다 — `|| {}`로 떨어지면
  // 실패와 구별되지 않는다.
  assert.match(
    privacy,
    /if \(!q\.data\?\.privacy\)/,
    "응답에 privacy 그룹이 없을 때 빈 객체로 떨어진다: 저장 한 번이면 정책 전체가 지워진다",
  );

  // 실패 분기는 값을 만들기 **전에** 와야 한다. 뒤에 있으면 아무것도 막지 못한다.
  const errorAt = privacy.indexOf("if (q.error)");
  const valueAt = privacy.indexOf("const v = local ||");
  assert.ok(
    errorAt !== -1 && valueAt !== -1 && errorAt < valueAt,
    "실패 분기가 폼 값보다 뒤에 있다: 폼은 이미 그려진 뒤다",
  );
});
