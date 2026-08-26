import assert from "node:assert/strict";
import test from "node:test";
import { describeQueryError, slowQueryNotice } from "../src/components/queryError.ts";

// The recovery reads the code off the error by shape, so the test builds the
// same shape the API client throws without importing it.
class APIError extends Error {
  constructor(status, code, message) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

test("시간 초과는 같은 답을 얻는 경로를 목적지와 함께 제시한다", () => {
  const recovery = describeQueryError(
    new APIError(504, "QUERY_TIMEOUT", "분석 쿼리가 25초 제한을 초과했습니다."),
    { canNarrowRange: true, canRetry: true },
  );
  assert.match(recovery.title, /25초/);
  const kinds = recovery.actions.map((action) => action.kind);
  assert.ok(kinds.includes("narrow"), "기간을 줄일 수 있는 화면에서는 줄이기를 제공한다");
  assert.ok(kinds.includes("retry"));
  const destinations = recovery.actions
    .filter((action) => action.kind === "link")
    .map((action) => action.to);
  assert.deepEqual(destinations, ["/segments", "/explorer", "/admin/automation"]);
});

test("기간을 바꿀 수 없는 화면에서는 기간 줄이기를 제안하지 않는다", () => {
  const recovery = describeQueryError(new APIError(504, "QUERY_TIMEOUT", "초과"), {});
  assert.equal(
    recovery.actions.some((action) => action.kind === "narrow"),
    false,
    "누를 수 없는 버튼을 제시하면 안내가 아니라 막다른 길이 된다",
  );
});

test("중단과 실패는 서로 다른 원인을 말한다", () => {
  const canceled = describeQueryError(new APIError(499, "QUERY_CANCELED", "취소"));
  assert.match(canceled.explanation, /새로고침/);
  assert.equal(canceled.actions[0].kind, "retry");

  const failed = describeQueryError(new APIError(500, "QUERY_FAILED", "ERROR: syntax"));
  assert.match(failed.explanation, /관리자/);
  assert.equal(failed.detail, "ERROR: syntax", "원문 메시지는 신고에 필요하므로 남긴다");
});

test("요청이 잘못된 경우는 고칠 위치를 가리킨다", () => {
  assert.deepEqual(
    describeQueryError(new APIError(400, "TOO_MANY_SEGMENTS", "3개")).actions,
    [{ kind: "link", label: "Segment 관리", to: "/segments" }],
  );
  assert.match(
    describeQueryError(new APIError(404, "UNKNOWN_SITE", "없음")).title,
    /사이트를 찾을 수 없습니다/,
  );
  assert.deepEqual(
    describeQueryError(new APIError(403, "FORBIDDEN", "권한")).actions,
    [],
    "권한 문제는 사용자가 화면에서 해결할 수 없으므로 버튼을 만들지 않는다",
  );
});

test("알 수 없는 오류는 기존 동작을 유지한다", () => {
  const recovery = describeQueryError(new Error("네트워크 오류"), { canRetry: true });
  assert.equal(recovery.title, "요청을 완료하지 못했습니다");
  assert.equal(recovery.explanation, "네트워크 오류");
  assert.deepEqual(recovery.actions, [{ kind: "retry", label: "다시 시도" }]);
});

test("느린 조회는 제한에 닿기 전에 상황을 알린다", () => {
  assert.equal(slowQueryNotice(3000), null, "짧은 대기는 알리지 않는다");
  assert.match(slowQueryNotice(9000), /오래 걸리고/);
  assert.match(slowQueryNotice(21000), /25초 제한에 가까워/);
});

test("기간 줄이기는 더 짧은 선택지가 있을 때만 제안한다", async () => {
  const { narrowerRange } = await import("../src/components/queryError.ts");
  assert.equal(narrowerRange(90), 30);
  assert.equal(narrowerRange(30), 7);
  assert.equal(narrowerRange(7), null, "가장 짧은 기간에서는 줄일 곳이 없다");
  assert.equal(narrowerRange(14), 7, "선택지에 없는 값에서도 더 짧은 쪽으로 내려간다");
});

test("정책이 허용하지 않는 기간은 선택지에 넣지 않는다", async () => {
  const { allowedRanges } = await import("../src/components/queryError.ts");
  assert.deepEqual(allowedRanges([7, 30, 90], 180), [7, 30, 90], "제한이 넉넉하면 그대로");
  assert.deepEqual(allowedRanges([90, 180, 365], 180), [90, 180], "365일은 제한을 넘으므로 제외");
  assert.deepEqual(allowedRanges([7, 30, 90], undefined), [7, 30, 90], "제한을 모르면 줄이지 않는다");
  assert.deepEqual(
    allowedRanges([7, 30, 90], 3),
    [7],
    "가장 짧은 기간마저 제한을 넘으면 빈 선택 대신 그 기간을 남겨 거절 사유를 보게 한다",
  );
});

test("정책 초과는 기간 줄이기와 정기 배달을 제시한다", async () => {
  const { describeQueryError } = await import("../src/components/queryError.ts");
  const recovery = describeQueryError(
    Object.assign(new Error("180일을 넘습니다"), { code: "RANGE_EXCEEDS_POLICY" }),
    { canNarrowRange: true },
  );
  assert.match(recovery.title, /정책이 허용하지 않습니다/);
  assert.deepEqual(
    recovery.actions.map((action) => action.kind),
    ["narrow", "link"],
  );
  assert.equal(recovery.detail, "180일을 넘습니다");
});
