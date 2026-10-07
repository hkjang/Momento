import assert from "node:assert/strict";
import test from "node:test";
import {
  JSON_SCHEMA_RULE,
  judgeSchemaText,
} from "../src/pages/schemaTextRule.ts";

// 「JSON Schema」 칸은 다섯 줄 자유 입력인데 「저장」 이 `!site || !form.name` 만
// 보았다(AdminPage.tsx). 그래서 깨진 JSON 으로 저장을 누르면 뮤테이션 안의
// `JSON.parse(form.schemaText)` 가 던져 **요청이 서버에 가지도 않는데**, 사용자는
// 그 헛클릭 왕복을 한 번 치른 뒤에야 안내를 읽었다.
//
// 이 칸의 판정자는 서버가 아니라 화면 자신의 `JSON.parse` 다 — cidrRule·
// retentionRule 과 달리 '화면이 서버보다 좁아질' 여지가 구조적으로 없고, 그래서
// 「저장」 을 닫는 것이 이 칸에서만 정당하다. 아래 첫 단언이 그 등가성을 묶는다.

// 왼쪽은 입력, 오른쪽은 `JSON.parse` 가 통과시키는지. 추측이 아니라 아래
// PARSES 단언이 같은 엔진에 실제로 넣어 확인한다.
const CASES = [
  ['{"properties": {}}', true],
  ["{}", true],
  ["null", true],
  ["[1,2]", true],
  ['"hi"', true],
  ["5", true],
  ["true", true],
  ['{"properties":{"a":{"type":"string"}}}', true],
  ['  {"a": 1}  ', true],
  ['{"properties": }', false],
  ["{", false],
  ['{"a":1,}', false],
  ["{'a':1}", false],
  ["[1,2", false],
  ['{"a": 1} {"b": 2}', false],
  ["{properties: {}}", false],
  ["", false],
  ["   ", false],
  ["undefined", false],
];

/** 표의 오른쪽 칸이 추측이 아니라는 것부터 확인한다. */
function parses(text) {
  try {
    JSON.parse(text);
    return true;
  } catch {
    return false;
  }
}

test("표의 통과 여부는 실제 JSON.parse 의 결과다", () => {
  for (const [text, expected] of CASES) {
    assert.equal(
      parses(text),
      expected,
      `${JSON.stringify(text)}: 표가 실제 JSON.parse 와 어긋난다`,
    );
  }
});

// 이 모듈의 계약 전부. 판정자가 뮤테이션과 **같은** JSON.parse 이므로 등가여야
// 한다 — 좁으면 서버가 받아 줄 값을 넣을 수 없게 되고(되돌릴 수 없는 결함),
// 넓으면 막아야 할 헛클릭이 그대로 남는다.
test("막는 값은 JSON.parse 가 거절하는 값과 정확히 같다", () => {
  for (const [text, ok] of CASES) {
    assert.equal(
      Boolean(judgeSchemaText(text).blocking),
      !ok,
      `${JSON.stringify(text)} 의 판정이 JSON.parse 와 어긋난다`,
    );
  }
});

// 기준 2. 저장 실패 Alert(describeEventDefinitionError)과 helperText 가 한
// 상수에서 나오므로 갈라질 수 없다. 반대쪽 단언은 adminErrors.test.mjs 에 있다.
test("깨진 JSON 의 안내는 JSON_SCHEMA_RULE 그대로다", () => {
  assert.equal(judgeSchemaText('{"properties": }').blocking, JSON_SCHEMA_RULE);
  assert.equal(judgeSchemaText("{").blocking, JSON_SCHEMA_RULE);
});

// 기준 3. V8 의 영문은 안내 본문에 들어오지 않는다 — 이 칸을 읽는 사람이
// 고쳐야 하는 것은 중괄호 짝이고, `Unexpected token '}' … position 17` 은 그
// 말을 하지 않는다.
const ENGINE_MARKS = [
  "Unexpected token",
  "is not valid JSON",
  "position",
  "JSON.parse",
  "Unexpected end of",
];

test("안내 본문에 V8 의 영문 표지가 남지 않는다", () => {
  for (const [text, ok] of CASES) {
    if (ok) continue;
    const blocking = judgeSchemaText(text).blocking ?? "";
    for (const mark of ENGINE_MARKS) {
      assert.ok(
        !blocking.includes(mark),
        `${JSON.stringify(text)}: 엔진 표지 "${mark}" 가 안내 본문에 섞였다: ${blocking}`,
      );
    }
  }
});

// 올바른 값은 오류 색도 쓰지 않는다 — 특히 `null`·`[1,2]` 처럼 객체가 아닌
// 값까지. 서버는 Schema map[string]any 로 받으므로 배열·문자열·숫자는 400 이지만
// `null` 은 통과한다(nil map → json.Marshal 이 null). 그래서 이번 판정은 **구문
// 만** 본다 — '객체가 아니면 막는다' 는 규칙은 null 에서 화면을 서버보다 좁게
// 만든다.
test("올바른 JSON 은 객체가 아니어도 막지 않고 칠하지 않는다", () => {
  for (const text of ["{}", "null", "[1,2]", '"hi"', "5", "true"]) {
    const judged = judgeSchemaText(text);
    assert.equal(judged.blocking, undefined, `${text}: 막았다`);
    assert.equal(judged.error, undefined, `${text}: 오류로 칠했다`);
  }
});

test("깨진 JSON 은 오류로 칠한다", () => {
  for (const text of ['{"properties": }', "{", '{"a":1,}', "[1,2"]) {
    assert.equal(judgeSchemaText(text).error, true, `${text}: 칠하지 않았다`);
  }
});

// cidrRule.ts 의 빈 칸 처리와 같은 이유. 빈 칸은 `JSON.parse("")` 가 던지므로
// 저장이 되지 않는 값이고 「저장」 은 닫아야 하지만, 폼을 열자마자 빨간 글씨가
// 보이면 안 되므로 오류 색은 쓰지 않는다.
test("빈 칸은 저장을 닫되 오류로 칠하지 않는다", () => {
  for (const text of ["", "   "]) {
    const judged = judgeSchemaText(text);
    assert.ok(judged.blocking, `${JSON.stringify(text)}: 저장을 닫지 않았다`);
    assert.equal(judged.error, undefined, `${JSON.stringify(text)}: 칠했다`);
  }
});

// 엔진 원문은 **어디가** 깨졌는지에 대한 유일한 단서다. 본문에서는 빼지만
// 버리지도 않는다 — Alert 이 캡션으로 쓰는 것과 같은 자리.
test("엔진 원문은 버리지 않고 detail 로 남는다", () => {
  const detail = judgeSchemaText('{"properties": }').detail ?? "";
  assert.ok(detail.length > 0, "엔진 원문이 사라졌다");
  assert.ok(
    ENGINE_MARKS.some((mark) => detail.includes(mark)),
    `엔진 원문이 아닌 것이 detail 에 들어 있다: ${detail}`,
  );
});

test("올바른 값에는 detail 을 남기지 않는다", () => {
  assert.equal(judgeSchemaText("{}").detail, undefined);
});

// 기준 2 의 반대쪽. adminErrors.ts 의 JSON 구문 안내가 이 상수를 읽으므로,
// 상수가 그 문장과 글자 단위로 같아야 그 테스트가 손대지 않고 통과한다.
test("JSON_SCHEMA_RULE 은 저장 실패 안내가 쓰는 문장 그대로다", () => {
  assert.equal(
    JSON_SCHEMA_RULE,
    "「JSON Schema」 칸의 내용이 올바른 JSON 이 아닙니다. 중괄호·대괄호의 짝과 쉼표 위치를 확인하고, 키와 문자열 값은 겹따옴표로 감싸세요. 규격을 비워 둘 때는 {} 로 적습니다.",
  );
});
