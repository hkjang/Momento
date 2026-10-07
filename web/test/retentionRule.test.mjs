import test from "node:test";
import assert from "node:assert/strict";

import {
  RETENTION_LIMITS,
  judgeRetention,
  retentionFieldProps,
  retentionHelperText,
  retentionNotice,
  retentionRangeText,
} from "../src/pages/retentionRule.ts";

// 보존 정책 다섯 칸의 범위 판정 — retentionRule.ts
//
// 다섯 칸이 전부 type="number" 자유 입력이고, 범위의 정본은 서버의
// validateRetention(internal/httpapi/advanced_analytics.go:80-97)이다. 그런데 그
// 숫자가 세 곳에 따로 적혀 있었다 — 서버의 Go, adminErrors.ts 의 안내 문장,
// AdminPage.tsx 의 helperText 문자열. 그래서 두 칸(「Session 요약 (개월)」·
// 「Aggregation (개월)」)은 helperText 가 범위를 **아예 말하지 않았고**, 사용자는
// 저장을 눌러 400 을 받은 뒤에야 범위를 읽었다. 이 모듈이 그 숫자의 단일 출처다.
// ---------------------------------------------------------------------------

// 서버 validateRetention 의 숫자를 글자 그대로 옮긴 것. 이 표가 서버와 모듈의
// 대응을 묶는다 — 서버가 범위를 바꾸면 이 테스트가 먼저 깨져야 한다.
const SERVER_RANGES = [
  {
    column: "raw_event_months",
    label: "Raw Event (개월)",
    min: 1,
    max: 120,
    unit: "개월",
    nullable: false,
  },
  {
    column: "session_months",
    label: "Session 요약 (개월)",
    min: 1,
    max: 120,
    unit: "개월",
    nullable: false,
  },
  {
    column: "aggregation_months",
    label: "Aggregation (개월)",
    min: 1,
    max: 1200,
    unit: "개월",
    nullable: true,
  },
  {
    column: "realtime_hours",
    label: "Realtime (시간)",
    min: 1,
    max: 168,
    unit: "시간",
    nullable: false,
  },
  {
    column: "debug_days",
    label: "Debugger / Dead Letter (일)",
    min: 1,
    max: 90,
    unit: "일",
    nullable: false,
  },
];

test("다섯 칸의 범위가 서버 validateRetention 의 숫자와 같다", () => {
  assert.equal(RETENTION_LIMITS.length, SERVER_RANGES.length);
  for (const expected of SERVER_RANGES) {
    const limit = RETENTION_LIMITS.find(
      (entry) => entry.column === expected.column,
    );
    assert.ok(limit, `${expected.column} 칸이 없다`);
    assert.equal(limit.label, expected.label, `${expected.column} 라벨`);
    assert.equal(limit.min, expected.min, `${expected.column} 하한`);
    assert.equal(limit.max, expected.max, `${expected.column} 상한`);
    assert.equal(limit.unit, expected.unit, `${expected.column} 단위`);
    assert.equal(
      !!limit.nullable,
      expected.nullable,
      `${expected.column} 비워 둘 수 있는지`,
    );
  }
});

// 이 결함을 짚는 단언. 지금 화면은 「Session 요약 (개월)」 과
// 「Aggregation (개월)」 의 helperText 에 범위를 적지 않는다.
test("다섯 칸의 기본 helperText 가 모두 자기 범위의 두 끝을 말한다", () => {
  for (const expected of SERVER_RANGES) {
    const helper = retentionHelperText(expected.column);
    assert.ok(
      helper.includes(String(expected.min)),
      `${expected.column}: 하한 ${expected.min} 이 helperText 에 없다: ${helper}`,
    );
    assert.ok(
      helper.includes(String(expected.max)),
      `${expected.column}: 상한 ${expected.max} 이 helperText 에 없다: ${helper}`,
    );
    assert.ok(
      helper.includes(expected.unit),
      `${expected.column}: 단위 "${expected.unit}" 가 helperText 에 없다: ${helper}`,
    );
  }
});

// 범위를 덧붙이면서 기존 문장을 지우지 않았는지. 세 칸의 설명은 범위가 아닌
// 것을 말하고 있고(특히 Realtime 의 '적용되지 않습니다' 와 Aggregation 의
// 사람 단위 기록 경고), 그것을 통째로 바꾸면 의도를 지운다.
test("범위를 더해도 기존 helperText 의 설명 문장은 남는다", () => {
  const kept = [
    ["session_months", "Raw Event 삭제 후에도 유지되는 요약"],
    ["aggregation_months", "비워 두면 무기한"],
    [
      "aggregation_months",
      "Visitor ID와 User ID를 행마다 가지므로, 비워 두면 Raw Event가 삭제된 뒤에도 사람 단위 기록이 남습니다",
    ],
    ["realtime_hours", "현재 적용되지 않습니다"],
    ["realtime_hours", "별도의 Realtime 저장소가 없어 삭제할 대상이 없습니다"],
  ];
  for (const [column, sentence] of kept) {
    const helper = retentionHelperText(column);
    assert.ok(
      helper.includes(sentence),
      `${column}: 기존 설명 "${sentence}" 가 사라졌다: ${helper}`,
    );
  }
});

// adminErrors.ts 의 INVALID_RETENTION 안내가 쓰는 문장. 지금 adminErrors 가
// 글자 그대로 품고 있는 다섯 문장과 **완전히 같아야** 한다 — 기존
// adminErrors.test.mjs 가 그 문장의 조각을 단언하고 있으므로, 여기서 어긋나면
// 두 테스트가 함께 깨진다.
test("INVALID_RETENTION 안내 문장이 칸 이름과 범위를 한국어 단위로 말한다", () => {
  const expected = new Map([
    [
      "raw_event_months",
      "「Raw Event (개월)」 은 1개월에서 120개월 사이로 적으세요.",
    ],
    [
      "session_months",
      "「Session 요약 (개월)」 은 1개월에서 120개월 사이로 적으세요.",
    ],
    [
      "aggregation_months",
      "「Aggregation (개월)」 은 비워 두거나 1개월에서 1200개월 사이로 적으세요.",
    ],
    [
      "realtime_hours",
      "「Realtime (시간)」 은 1시간에서 168시간 사이로 적으세요.",
    ],
    [
      "debug_days",
      "「Debugger / Dead Letter (일)」 은 1일에서 90일 사이로 적으세요.",
    ],
  ]);
  for (const [column, sentence] of expected) {
    assert.equal(retentionNotice(column), sentence, column);
  }
});

test("범위 문구는 두 끝을 물결로 잇고 단위를 뒤에 붙인다", () => {
  assert.equal(retentionRangeText("raw_event_months"), "1~120개월");
  assert.equal(retentionRangeText("realtime_hours"), "1~168시간");
  assert.equal(retentionRangeText("debug_days"), "1~90일");
  // 비워 둘 수 있는 칸은 그것을 먼저 말한다.
  assert.equal(
    retentionRangeText("aggregation_months"),
    "비워 두거나 1~1200개월",
  );
});

test("범위 안의 값과 두 경계값은 막지 않는다", () => {
  for (const { column, min, max } of SERVER_RANGES) {
    for (const value of [min, max, Math.floor((min + max) / 2)]) {
      const judged = judgeRetention(column, value);
      assert.equal(
        judged.blocking,
        undefined,
        `${column}: 범위 안의 ${value} 를 막았다: ${judged.blocking}`,
      );
      assert.equal(judged.error, undefined, `${column}: ${value} 에 오류 색`);
    }
  }
});

test("경계 바로 밖의 값은 막고 그 칸의 범위를 말한다", () => {
  for (const { column, min, max } of SERVER_RANGES) {
    for (const value of [min - 1, max + 1]) {
      const judged = judgeRetention(column, value);
      assert.equal(
        judged.blocking,
        retentionNotice(column),
        `${column}: 범위 밖의 ${value} 를 막지 않았거나 다른 문장을 썼다`,
      );
      assert.equal(judged.error, true, `${column}: ${value} 에 오류 색이 없다`);
    }
  }
});

// 칸을 비우면 Number("") 가 0 이 되어 그대로 실려 간다(AdminPage 의 onChange).
// 서버는 0 을 거절하므로 화면도 거절해야 한다 — 비워 둘 수 있는 한 칸만 빼고.
test("비울 수 없는 네 칸의 0 은 막고 Aggregation 의 빈 칸은 통과시킨다", () => {
  for (const { column, nullable } of SERVER_RANGES) {
    if (nullable) continue;
    const judged = judgeRetention(column, 0);
    assert.equal(judged.error, true, `${column}: 0 을 막지 않았다`);
  }
  const blank = judgeRetention("aggregation_months", null);
  assert.equal(
    blank.blocking,
    undefined,
    `Aggregation 의 빈 칸을 막았다: ${blank.blocking}`,
  );
});

test("비울 수 없는 칸의 null 은 막는다", () => {
  for (const { column, nullable } of SERVER_RANGES) {
    if (nullable) continue;
    const judged = judgeRetention(column, null);
    assert.equal(judged.error, true, `${column}: 빈 칸을 막지 않았다`);
  }
});

// 서버의 다섯 필드는 int·*int 라서 1.5 는 범위 판정에 닿기도 전에 JSON
// 디코딩에서 400 INVALID_PAYLOAD 로 떨어진다. 화면이 서버보다 좁아지지 않는다.
test("정수가 아닌 값은 막고 범위와 다른 이유를 말한다", () => {
  const judged = judgeRetention("raw_event_months", 1.5);
  assert.equal(judged.error, true, "1.5 를 막지 않았다");
  assert.ok(
    judged.blocking?.includes("정수"),
    `정수가 아니라는 것을 말하지 않는다: ${judged.blocking}`,
  );
  assert.notEqual(
    judged.blocking,
    retentionNotice("raw_event_months"),
    "범위 안내와 같은 문장을 쓴다 — 이유가 다른데 같은 말을 한다",
  );
});

// TextField 가 그대로 받는 모양. 막을 것이 없으면 기본 안내를, 있으면 그 문장을
// helperText 로 쓴다 — 한 칸이 범위를 두 번 말하지 않게.
test("TextField 는 막을 것이 없으면 기본 안내를 helperText 로 받는다", () => {
  const props = retentionFieldProps("raw_event_months", 13);
  assert.equal(props.helperText, retentionHelperText("raw_event_months"));
  assert.equal(props.error, false);
});

// 안내가 「」 로 부르는 이름과 화면에 찍히는 이름이 같은 상수여야 한다 — 둘을
// 따로 적으면 한쪽만 고쳐져 Alert 이 화면에 없는 칸을 가리킨다.
test("TextField 가 받는 라벨은 안내 문장이 부르는 이름과 같은 상수다", () => {
  for (const { column, label } of SERVER_RANGES) {
    const props = retentionFieldProps(column, 1);
    assert.equal(props.label, label, column);
    assert.ok(
      retentionNotice(column).includes(`「${props.label}」`),
      `${column}: 안내가 화면 라벨을 부르지 않는다`,
    );
  }
});

test("TextField 는 막을 것이 있으면 그 문장을 helperText 로 받는다", () => {
  const props = retentionFieldProps("raw_event_months", 999);
  assert.equal(props.helperText, retentionNotice("raw_event_months"));
  assert.equal(props.error, true);
});

// 모르는 칸 이름으로 부르는 것은 배선 실수다 — 조용히 빈 문자열을 돌려주면
// helperText 가 사라진 것을 아무도 못 본다.
test("모르는 칸 이름은 조용히 넘기지 않고 던진다", () => {
  assert.throws(() => retentionHelperText("sample_rate_percent"));
  assert.throws(() => retentionNotice("sample_rate_percent"));
});
