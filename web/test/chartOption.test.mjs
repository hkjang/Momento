import assert from "node:assert/strict";
import test from "node:test";
import { optionSignature } from "../src/components/chartOption.ts";

const option = (data, formatter = (v) => `${v}명`) => ({
  tooltip: { trigger: "axis", valueFormatter: formatter },
  series: [{ type: "bar", data }],
});

test("같은 내용의 새 객체는 같은 서명이다", () => {
  assert.equal(optionSignature(option([1, 2, 3])), optionSignature(option([1, 2, 3])));
});

test("값·형식 함수가 바뀌면 서명도 바뀐다", () => {
  assert.notEqual(optionSignature(option([1, 2, 3])), optionSignature(option([1, 2, 4])));
  assert.notEqual(
    optionSignature(option([1], (v) => `${v}명`)),
    optionSignature(option([1], (v) => `${v}건`)),
  );
  assert.notEqual(optionSignature({ v: Number.NaN }), optionSignature({ v: null }));
});

test("순환 참조가 있어도 멈추지 않는다", () => {
  const cyclic = { a: 1 };
  cyclic.self = cyclic;
  assert.equal(typeof optionSignature(cyclic), "string");
});
