import assert from "node:assert/strict";
import test from "node:test";
import {
  parsePeriod,
  parseRange,
  periodKey,
  rangeLength,
  rangeProblem,
  withPeriod,
  withRange,
} from "../src/components/periodParam.ts";

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
  // 최근 N일을 고르면 직접 기간은 빠진다.
  assert.equal(withPeriod(new URLSearchParams("from=2026-09-01&to=2026-09-10"), 7, 30).toString(), "days=7");
  assert.equal(withPeriod(params, 7, 30).toString(), "visitor=abc&days=7");
  // 입력은 바뀌지 않는다.
  assert.equal(params.toString(), "visitor=abc&days=90");
});

test("직접 기간은 달력에 있는 날짜이고 시작이 끝보다 늦지 않다", () => {
  assert.deepEqual(parseRange("2026-09-01", "2026-09-10"), { from: "2026-09-01", to: "2026-09-10" });
  assert.deepEqual(parseRange("2026-09-05", "2026-09-05"), { from: "2026-09-05", to: "2026-09-05" });
  for (const [from, to] of [
    ["2026-09-10", "2026-09-01"],
    ["2026-02-30", "2026-03-01"],
    ["2026-9-1", "2026-09-10"],
    ["", "2026-09-10"],
    [null, null],
    ["0001-01-01", "2026-09-10"],
  ]) {
    assert.equal(parseRange(from, to), null, `${from}..${to}`);
  }
});

test("길이는 양 끝을 포함하고 윤년과 월말을 넘는다", () => {
  assert.equal(rangeLength({ from: "2026-09-01", to: "2026-09-30" }), 30);
  assert.equal(rangeLength({ from: "2028-02-28", to: "2028-03-01" }), 3);
  assert.equal(rangeLength({ from: "2026-12-31", to: "2027-01-01" }), 2);
});

test("적용 전에 막아야 할 기간은 이유를 말한다", () => {
  const today = "2026-09-30";
  assert.equal(rangeProblem({ from: "2026-09-01", to: "2026-09-30" }, today, 90), null);
  assert.match(rangeProblem({ from: "", to: "2026-09-30" }, today), /모두/);
  assert.match(rangeProblem({ from: "2026-09-20", to: "2026-09-10" }, today), /늦습니다/);
  assert.match(rangeProblem({ from: "2026-09-01", to: "2026-10-01" }, today), /오늘 이후/);
  assert.match(rangeProblem({ from: "2026-01-01", to: "2026-09-30" }, today, 90), /최대 90일.*273일/);
});

test("직접 기간과 최근 N일은 주소에서 서로를 지운다", () => {
  const params = new URLSearchParams("days=90&visitor=abc");
  assert.equal(
    withRange(params, { from: "2026-09-01", to: "2026-09-10" }).toString(),
    "visitor=abc&from=2026-09-01&to=2026-09-10",
  );
  assert.equal(withRange(new URLSearchParams("from=a&to=b&x=1"), null).toString(), "x=1");
  // 같은 길이라도 캐시 키가 다르다.
  assert.notEqual(periodKey(10, { from: "2026-09-01", to: "2026-09-10" }), periodKey(10, null));
});
