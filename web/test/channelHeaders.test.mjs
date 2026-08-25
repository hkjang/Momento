import assert from "node:assert/strict";
import test from "node:test";
import {
  buildHeaders,
  headerIssues,
  parseHeaderRows,
} from "../src/pages/channelHeaders.ts";

test("행에서 Header 객체를 만들고 빈 행은 버린다", () => {
  assert.deepEqual(
    buildHeaders([
      { name: " Authorization ", value: " Bearer token " },
      { name: "", value: "" },
      { name: "X-Api-Key", value: "abc" },
    ]),
    { Authorization: "Bearer token", "X-Api-Key": "abc" },
  );
  assert.deepEqual(buildHeaders([{ name: "", value: "" }]), {});
});

test("저장된 정의를 다시 행으로 읽는다", () => {
  assert.deepEqual(parseHeaderRows('{"Authorization":"Bearer x"}'), [
    { name: "Authorization", value: "Bearer x" },
  ]);
  // 비어 있거나 깨진 값에서도 편집 가능한 빈 행을 준다.
  for (const input of ["", "   ", "{", "not json"]) {
    assert.deepEqual(parseHeaderRows(input), [{ name: "", value: "" }]);
  }
  assert.deepEqual(parseHeaderRows("{}"), [{ name: "", value: "" }]);
  assert.deepEqual(parseHeaderRows('{"X-Count":3}'), [{ name: "X-Count", value: "3" }]);
});

test("서버가 거부할 입력을 미리 알려준다", () => {
  assert.deepEqual(headerIssues([{ name: "Host", value: "evil.internal" }]), [
    "Host Header는 재정의할 수 없습니다.",
  ]);
  assert.deepEqual(headerIssues([{ name: "host", value: "x" }]), [
    "Host Header는 재정의할 수 없습니다.",
  ]);
  assert.match(
    headerIssues([
      { name: "X-Api-Key", value: "a" },
      { name: "x-api-key", value: "b" },
    ])[0],
    /중복/,
  );
  assert.match(headerIssues([{ name: "X-Token", value: "a\nb" }])[0], /줄바꿈/);
  assert.match(headerIssues([{ name: "X-Token", value: "  " }])[0], /값이 비어/);
  assert.match(headerIssues([{ name: "", value: "orphan" }])[0], /값만 입력된/);
});

test("정상 입력에는 문제를 보고하지 않고 중복 메시지를 합친다", () => {
  assert.deepEqual(
    headerIssues([
      { name: "Authorization", value: "Bearer token" },
      { name: "", value: "" },
    ]),
    [],
  );
  const duplicated = headerIssues([
    { name: "Host", value: "a" },
    { name: "Host", value: "b" },
  ]);
  assert.equal(duplicated.filter((issue) => issue.includes("Host Header")).length, 1);
});
