import assert from "node:assert/strict";
import test from "node:test";
import {
  buildScheduleDefinition,
  defaultScheduleForm,
  describeSchedule,
  kindUsesAlertState,
  kindUsesRange,
  kindUsesSegmentFilters,
  parseScheduleKind,
  scheduleKindLabel,
} from "../src/pages/scheduleDefinition.ts";

const form = { ...defaultScheduleForm, environment: "stg", days: 30 };

test("정의에는 해당 종류가 쓰는 값만 담는다", () => {
  assert.deepEqual(buildScheduleDefinition("overview", form), {
    environment: "stg",
    days: 30,
  });

  // 이상 감지는 기간이 아니라 알림 상태를 쓴다.
  assert.deepEqual(buildScheduleDefinition("anomaly", form), {
    environment: "stg",
    notify_on: ["new", "recovered"],
  });

  assert.deepEqual(
    buildScheduleDefinition("anomaly", { ...form, notifyOn: ["new", "ongoing"], alwaysSend: true }),
    { environment: "stg", notify_on: ["new", "ongoing"], always_send: true },
  );
});

test("빈 알림 상태는 기본값으로 되돌린다", () => {
  const definition = buildScheduleDefinition("anomaly", { ...form, notifyOn: [] });
  assert.deepEqual(definition.notify_on, ["new", "recovered"]);
});

test("Segment 조건은 입력된 것만 넣는다", () => {
  assert.deepEqual(
    buildScheduleDefinition("segment", { ...form, eventName: " feature_used ", department: "플랫폼" }),
    { environment: "stg", days: 30, event_name: "feature_used", department: "플랫폼" },
  );
  assert.deepEqual(buildScheduleDefinition("segment", form), {
    environment: "stg",
    days: 30,
  });
});

test("종류별로 필요한 입력이 무엇인지 구분한다", () => {
  assert.equal(kindUsesRange("anomaly"), false);
  assert.equal(kindUsesRange("visitor_insight"), true);
  assert.equal(kindUsesAlertState("anomaly"), true);
  assert.equal(kindUsesAlertState("overview"), false);
  assert.equal(kindUsesSegmentFilters("segment"), true);
  assert.equal(kindUsesSegmentFilters("ai"), false);
});

test("설정 요약은 무엇이 언제 전송되는지 한 줄로 말한다", () => {
  assert.equal(
    describeSchedule("visitor_insight", form, 10080),
    "매주 방문자 인사이트 전체 · STG · 최근 30일",
  );
  assert.match(describeSchedule("anomaly", form, 60), /^1시간마다 이상 감지 알림 · STG · 신규·회복 상태만 전송$/);
  assert.match(
    describeSchedule("anomaly", { ...form, alwaysSend: true }, 60),
    /감지 여부와 무관하게 매번 전송/,
  );
  assert.match(describeSchedule("segment", form, 1440), /조건 없음$/);
  // 사전 정의에 없는 주기도 그대로 표현한다.
  assert.match(describeSchedule("overview", form, 45), /^45분마다/);
});

test("딥링크의 종류는 서버가 아는 값만 받는다", () => {
  assert.equal(parseScheduleKind("anomaly"), "anomaly");
  assert.equal(parseScheduleKind("visitor_insight"), "visitor_insight");
  assert.equal(parseScheduleKind("dashboard"), null);
  assert.equal(parseScheduleKind(null), null);
  for (const kind of Object.keys(scheduleKindLabel)) {
    assert.equal(parseScheduleKind(kind), kind);
  }
});
