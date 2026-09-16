import assert from "node:assert/strict";
import test from "node:test";
import {
  MAX_PASSWORD_BYTES,
  MIN_PASSWORD_LENGTH,
  PASSWORD_RULE,
  passwordWithinBounds,
} from "../src/pages/passwordRule.ts";

test("최소 길이는 글자 수로 센다", () => {
  assert.equal(
    passwordWithinBounds("a".repeat(MIN_PASSWORD_LENGTH - 1)),
    false,
  );
  assert.equal(passwordWithinBounds("a".repeat(MIN_PASSWORD_LENGTH)), true);
  // 한글 4자는 12바이트지만 4글자이므로 부족하다.
  assert.equal(passwordWithinBounds("가".repeat(4)), false);
  assert.equal(passwordWithinBounds("가".repeat(MIN_PASSWORD_LENGTH)), true);
});

test("최대 길이는 bcrypt 의 72바이트로 센다", () => {
  assert.equal(passwordWithinBounds("a".repeat(MAX_PASSWORD_BYTES)), true);
  assert.equal(passwordWithinBounds("a".repeat(MAX_PASSWORD_BYTES + 1)), false);
  // 한글 24자는 72바이트라 통과, 25자는 75바이트라 거부.
  assert.equal(passwordWithinBounds("가".repeat(24)), true);
  assert.equal(passwordWithinBounds("가".repeat(25)), false);
});

test("도움말은 두 한계를 모두 말한다", () => {
  assert.match(PASSWORD_RULE, /12자 이상/);
  assert.match(PASSWORD_RULE, /72바이트\(한글 24자\) 이하/);
});
