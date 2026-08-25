import assert from "node:assert/strict";
import test from "node:test";
import { buildAttentionItems, formatGoalNumber } from "../src/pages/attention.ts";

const anomaly = (metric, label, severity, evidence = "근거", action = "확인") => ({
  metric,
  label,
  date: "2026-08-24T00:00:00Z",
  value: 100,
  baseline: 900,
  change_percent: -88,
  robust_z: -5,
  severity,
  direction: "below",
  samples: 8,
  weekday: "월",
  evidence,
  action,
});

const goal = (id, name, overrides = {}) => ({
  id,
  name,
  metric_name: "active_users",
  value: 400,
  target_value: 1000,
  comparator: "gte",
  period: "month",
  achieved: false,
  progress_percent: 40,
  forecast_available: true,
  forecast_status: "behind",
  projected_value: 800,
  elapsed_percent: 50,
  ...overrides,
});

test("심각한 이상이 먼저, 같은 등급에서는 이상이 Goal보다 먼저 온다", () => {
  const { items } = buildAttentionItems(
    [anomaly("errors", "오류", "warning"), anomaly("users", "방문자", "critical")],
    [],
    [goal("g1", "월간 활성 사용자")],
  );

  assert.deepEqual(
    items.map((item) => item.id),
    ["anomaly:users", "anomaly:errors", "goal:g1"],
  );
  assert.equal(items[0].severity, "critical");
  assert.equal(items[2].severity, "warning");
  assert.equal(items[2].to, "/goals");
  assert.equal(items[0].to, "/visitor-insights");
});

test("달성했거나 순항하는 Goal은 목록에 넣지 않는다", () => {
  const { items } = buildAttentionItems(
    [],
    [],
    [
      goal("done", "달성", { achieved: true }),
      goal("ontrack", "순항", { forecast_status: "on_track" }),
      goal("early", "판정 보류", { forecast_available: false }),
      goal("behind", "미달 전망"),
    ],
  );

  assert.deepEqual(
    items.map((item) => item.id),
    ["goal:behind"],
  );
  assert.match(items[0].detail, /착지 예상 800/);
  assert.match(items[0].detail, /기간 진행 50%/);
});

test("알림 상태가 있으면 신규와 지속을 제목에 붙인다", () => {
  const ongoing = buildAttentionItems(
    [anomaly("users", "방문자", "critical")],
    [
      {
        metric: "users",
        label: "방문자",
        state: "ongoing",
        severity: "critical",
        days_open: 4,
        robust_z: -5,
        evidence: "근거",
        notifiable: false,
      },
    ],
    [],
  );
  assert.match(ongoing.items[0].title, /지속 4일/);

  const fresh = buildAttentionItems(
    [anomaly("users", "방문자", "critical")],
    [
      {
        metric: "users",
        label: "방문자",
        state: "new",
        severity: "critical",
        days_open: 1,
        robust_z: -5,
        evidence: "근거",
        notifiable: true,
      },
    ],
    [],
  );
  assert.match(fresh.items[0].title, /신규/);
});

test("긍정 변화는 참고로만 표시하고 다른 문구를 쓴다", () => {
  const { items } = buildAttentionItems([anomaly("users", "방문자", "positive")], [], []);
  assert.equal(items[0].severity, "info");
  assert.match(items[0].title, /크게 늘었습니다/);
});

test("정상·데이터 부족 판정은 목록에 넣지 않는다", () => {
  const { items } = buildAttentionItems(
    [
      anomaly("users", "방문자", "normal"),
      anomaly("events", "이벤트", "insufficient_history"),
      anomaly("sessions", "세션", "unknown"),
    ],
    [],
    [],
  );
  assert.equal(items.length, 0);
});

test("목록은 상한을 넘기지 않고 남은 건수를 보고한다", () => {
  const many = ["a", "b", "c", "d", "e", "f", "g"].map((key) =>
    anomaly(key, key.toUpperCase(), "warning"),
  );
  const { items, hidden } = buildAttentionItems(many, [], [], 5);
  assert.equal(items.length, 5);
  assert.equal(hidden, 2);
});

test("아무 입력이 없으면 빈 목록이다", () => {
  const { items, hidden } = buildAttentionItems(undefined, undefined, undefined);
  assert.equal(items.length, 0);
  assert.equal(hidden, 0);
});

test("Goal 숫자는 정수는 그대로, 소수는 한 자리로 표기한다", () => {
  assert.equal(formatGoalNumber(1000), "1,000");
  assert.equal(formatGoalNumber(999.96), "1,000");
  assert.equal(formatGoalNumber(12.34), "12.3");
});
