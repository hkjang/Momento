import assert from "node:assert/strict";
import test from "node:test";
import { describeAggregateJobStatus } from "../src/pages/aggregateJobStatus.ts";

test("사유가 남은 pending 작업은 재시도 대기로 구분한다", () => {
  const chip = describeAggregateJobStatus({
    status: "pending",
    error: "recovered after interrupted worker",
  });
  assert.equal(chip.label, "재시도 대기");
  assert.equal(chip.color, "warning");
});

test("아직 시작하지 않은 pending 작업은 그대로 pending 으로 보인다", () => {
  for (const error of [null, undefined, "", "   "]) {
    const chip = describeAggregateJobStatus({ status: "pending", error });
    assert.equal(chip.label, "pending", `error=${JSON.stringify(error)}`);
    assert.equal(chip.color, "default");
  }
});

test("성공·실패·실행 중은 기존 색을 유지한다", () => {
  assert.deepEqual(describeAggregateJobStatus({ status: "success" }), {
    label: "success",
    color: "success",
  });
  assert.deepEqual(
    describeAggregateJobStatus({ status: "failed", error: "boom" }),
    { label: "failed", color: "error" },
  );
  assert.deepEqual(describeAggregateJobStatus({ status: "running" }), {
    label: "running",
    color: "default",
  });
});
