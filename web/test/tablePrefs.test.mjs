import assert from "node:assert/strict";
import test from "node:test";
import {
  exportName,
  highlightParts,
  isNumericColumn,
  parseHidden,
  tableStorageKey,
  toggleHidden,
} from "../src/components/tablePrefs.ts";

test("표의 저장 키는 내보내기 이름, 제목, 열 순으로 정한다", () => {
  assert.equal(tableStorageKey("제목", "momento-pages", ["a"]), "momento:table-columns:momento-pages");
  assert.equal(tableStorageKey("제목", undefined, ["a"]), "momento:table-columns:제목");
  assert.equal(tableStorageKey(undefined, undefined, ["a", "b"]), "momento:table-columns:a,b");
});

test("숨긴 열 목록은 모르는 열과 망가진 값을 버린다", () => {
  assert.deepEqual(parseHidden('["a","zzz",3]', ["a", "b"]), ["a"]);
  assert.deepEqual(parseHidden("{", ["a"]), []);
  assert.deepEqual(parseHidden(null, ["a"]), []);
});

test("마지막 남은 열은 숨길 수 없다", () => {
  const keys = ["a", "b"];
  assert.deepEqual(toggleHidden([], "a", keys), ["a"]);
  assert.deepEqual(toggleHidden(["a"], "b", keys), ["a"]);
  assert.deepEqual(toggleHidden(["a"], "a", keys), []);
});

test("값이 있는 칸이 모두 숫자인 열만 숫자 열이다", () => {
  assert.equal(isNumericColumn([1, null, 2.5, ""]), true);
  assert.equal(isNumericColumn([1, "2"]), false);
  assert.equal(isNumericColumn([null, undefined]), false);
});

test("내보내기 이름이 없으면 제목에서 파일 이름을 만든다", () => {
  assert.equal(exportName("x.csv", "무시"), "x.csv");
  assert.equal(exportName(undefined, "감사 로그: 최근/전체"), "momento-감사-로그-최근전체");
  assert.equal(exportName(undefined, undefined), "momento-table");
});

test("검색어와 일치한 부분을 대소문자 없이 모두 찾는다", () => {
  assert.deepEqual(highlightParts("Page_view page", "PAGE"), [
    { text: "Page", match: true },
    { text: "_view ", match: false },
    { text: "page", match: true },
  ]);
  assert.deepEqual(highlightParts("abc", ""), [{ text: "abc", match: false }]);
  assert.deepEqual(highlightParts("abc", "x"), [{ text: "abc", match: false }]);
  // 정규식 문자는 글자 그대로다.
  assert.deepEqual(highlightParts("a.b", "."), [
    { text: "a", match: false },
    { text: ".", match: true },
    { text: "b", match: false },
  ]);
});
