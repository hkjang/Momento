import test from "node:test";
import assert from "node:assert/strict";

import {
  defaultQueryPolicyForm,
  queryPolicyFields,
  queryPolicyLabel,
  readQueryPolicy,
} from "../src/pages/queryPolicyForm.ts";

// The query policy endpoint answers the five limits and a sixth field —
// `defaults`, a boolean saying whether the site has a stored policy or is
// running under the built-in one. The screen cast the whole response into its
// form, TypeScript accepted the cast, and the form renders one control per key
// it holds: an administrator saw a sixth number field labelled `defaults`
// holding a boolean, beside the five real limits.
//
// This is the screen that tells somebody what limits are in force, and it was
// inventing one.
test("정책 폼은 서버 응답에서 다섯 한도만 가져온다", () => {
  const answered = {
    max_exact_days: 14,
    max_complexity_score: 70,
    background_threshold: 30,
    fast_sample_percent: 5,
    preview_sample_percent: 2,
    defaults: false,
  };
  const form = readQueryPolicy(answered);
  assert.deepEqual(Object.keys(form).sort(), [...queryPolicyFields].sort());
  assert.equal(form.max_exact_days, 14);
  assert.ok(!("defaults" in form), "서버의 defaults 플래그가 폼에 들어왔다");
});

test("응답에 없는 값은 지금 보고 있는 값을 유지한다", () => {
  const current = { ...defaultQueryPolicyForm, max_exact_days: 30 };
  // 부분 응답이 관리자가 보고 있는 컨트롤을 비우면 안 된다.
  const form = readQueryPolicy({ max_complexity_score: 55 }, current);
  assert.equal(form.max_exact_days, 30);
  assert.equal(form.max_complexity_score, 55);
});

test("숫자가 아닌 값은 한도로 받아들이지 않는다", () => {
  // `defaults`가 그랬듯, 숫자 아닌 것이 숫자 입력란에 앉으면 안 된다.
  const form = readQueryPolicy({
    max_exact_days: true,
    max_complexity_score: "90",
    background_threshold: null,
    fast_sample_percent: Number.NaN,
  });
  assert.deepEqual(form, defaultQueryPolicyForm);
});

test("모든 한도에 사람이 읽을 이름이 있다", () => {
  // 라벨이 없으면 화면은 원본 키를 보여 준다 — `defaults`가 그렇게 나타났다.
  for (const field of queryPolicyFields) {
    assert.ok(queryPolicyLabel[field], `${field}에 라벨이 없다`);
  }
});
