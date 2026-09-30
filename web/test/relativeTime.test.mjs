import assert from "node:assert/strict";
import test from "node:test";
import { absoluteTime, relativeTime } from "../src/components/relativeTime.ts";

const now = Date.parse("2026-10-01T12:00:00Z");
const ago = (seconds) => new Date(now - seconds * 1000).toISOString();

test("최근 시각은 상대 시각이고 일주일이 넘으면 날짜로 돌아간다", () => {
  assert.equal(relativeTime(ago(5), now), "방금");
  assert.equal(relativeTime(ago(125), now), "2분 전");
  assert.equal(relativeTime(ago(3 * 3600 + 5), now), "3시간 전");
  assert.equal(relativeTime(ago(2 * 86400), now), "2일 전");
  assert.equal(relativeTime(ago(8 * 86400), now), null);
});

test("시계가 조금 어긋난 미래는 방금이고, 알 수 없는 값은 상대 시각이 없다", () => {
  assert.equal(relativeTime(ago(-20), now), "방금");
  assert.equal(relativeTime(ago(-3600), now), null);
  for (const value of [null, undefined, "", "not a date"]) assert.equal(relativeTime(value, now), null);
  assert.equal(absoluteTime(null), "—");
  assert.equal(absoluteTime("not a date"), "not a date");
});
