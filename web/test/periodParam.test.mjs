import assert from "node:assert/strict";
import test from "node:test";
import { parsePeriod, withPeriod } from "../src/components/periodParam.ts";

const options = [7, 30, 90];

test("주소의 기간은 화면이 내주는 선택지일 때만 받아들인다", () => {
  assert.equal(parsePeriod("90", 30, options), 90);
  assert.equal(parsePeriod(" 7 ", 30, options), 7);
  for (const hostile of [null, undefined, "", "45", "-7", "7.5", "1e2", "90abc", "99999"]) {
    assert.equal(parsePeriod(hostile, 30, options), 30, String(hostile));
  }
});

test("기본 기간은 주소에서 빠지고 다른 조건은 그대로 남는다", () => {
  const params = new URLSearchParams("visitor=abc&days=90");
  assert.equal(withPeriod(params, 30, 30).toString(), "visitor=abc");
  assert.equal(withPeriod(params, 7, 30).toString(), "visitor=abc&days=7");
  // 입력은 바뀌지 않는다.
  assert.equal(params.toString(), "visitor=abc&days=90");
});
