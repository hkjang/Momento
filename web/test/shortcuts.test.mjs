import assert from "node:assert/strict";
import test from "node:test";
import { isTypingTarget, readShortcut, SEQUENCE_WINDOW_MS } from "../src/components/shortcuts.ts";

const idle = { pendingSince: null };

test("g 다음 글자는 이동이고, 늦게 온 글자는 아무것도 아니다", () => {
  let step = readShortcut(idle, "g", 1000, false);
  assert.deepEqual(step.action, { kind: "none" });
  assert.deepEqual(readShortcut(step.state, "e", 1500, false).action, { kind: "go", to: "/events" });
  assert.deepEqual(readShortcut(step.state, "E", 1500, false).action, { kind: "go", to: "/events" });
  assert.deepEqual(readShortcut(step.state, "e", 1000 + SEQUENCE_WINDOW_MS + 1, false).action, { kind: "none" });
  // 모르는 글자는 대기를 끝낸다.
  step = readShortcut(step.state, "q", 1100, false);
  assert.deepEqual(step.action, { kind: "none" });
  assert.equal(step.state.pendingSince, null);
});

test("관리 센터로 가는 단축키는 관리자에게만 있다", () => {
  const pending = readShortcut(idle, "g", 0, false).state;
  assert.deepEqual(readShortcut(pending, "a", 10, false).action, { kind: "none" });
  assert.deepEqual(readShortcut(pending, "a", 10, true).action, { kind: "go", to: "/admin" });
});

test("/ 는 검색, ? 는 도움말이다", () => {
  assert.deepEqual(readShortcut(idle, "/", 0, false).action, { kind: "search" });
  assert.deepEqual(readShortcut(idle, "?", 0, false).action, { kind: "help" });
  assert.deepEqual(readShortcut(idle, "k", 0, false).action, { kind: "none" });
});

test("글자를 쓰는 곳에서는 단축키가 동작하지 않는다", () => {
  const el = (tagName, attrs = {}, editable = false) => ({ tagName, isContentEditable: editable, getAttribute: (n) => attrs[n] ?? null });
  assert.equal(isTypingTarget(el("input")), true);
  assert.equal(isTypingTarget(el("TEXTAREA")), true);
  assert.equal(isTypingTarget(el("div", {}, true)), true);
  assert.equal(isTypingTarget(el("div", { role: "combobox" })), true);
  assert.equal(isTypingTarget(el("button")), false);
  assert.equal(isTypingTarget(null), false);
});
