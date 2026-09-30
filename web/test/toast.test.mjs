import assert from "node:assert/strict";
import test from "node:test";
import { showToast, subscribeToasts, successMessage } from "../src/components/toast.ts";

test("알림은 구독자에게 가고 구독을 끊으면 더는 오지 않는다", () => {
  const got = [];
  const stop = subscribeToasts((toast) => got.push(toast));
  showToast("저장했습니다.");
  showToast("   ");
  stop();
  showToast("삭제했습니다.");
  assert.deepEqual(got.map((t) => t.message), ["저장했습니다."]);
});

test("meta 의 문자열만 알림 문구가 된다", () => {
  assert.equal(successMessage({ successMessage: " 저장했습니다. " }), "저장했습니다.");
  for (const meta of [undefined, null, {}, { successMessage: 1 }, { successMessage: "" }, "x"]) {
    assert.equal(successMessage(meta), null);
  }
});
