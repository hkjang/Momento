import assert from "node:assert/strict";
import test from "node:test";
import {
  compareValues,
  firstDirection,
  nextSort,
  sortRows,
} from "../src/components/tableSort.ts";

const byValue = (row) => row.v;

test("숫자는 글자가 아니라 크기로 정렬된다", () => {
  const rows = [{ v: 9 }, { v: 100 }, { v: 25 }, { v: -3 }];
  assert.deepEqual(sortRows(rows, byValue, "asc").map(byValue), [-3, 9, 25, 100]);
  assert.deepEqual(sortRows(rows, byValue, "desc").map(byValue), [100, 25, 9, -3]);
});

test("글자는 한국어 순서이고 안에 든 숫자는 크기로 읽는다", () => {
  const rows = [{ v: "하나" }, { v: "가방" }, { v: "나무" }];
  assert.deepEqual(sortRows(rows, byValue, "asc").map(byValue), ["가방", "나무", "하나"]);
  const pages = [{ v: "/p/10" }, { v: "/p/2" }, { v: "/p/1" }];
  assert.deepEqual(sortRows(pages, byValue, "asc").map(byValue), ["/p/1", "/p/2", "/p/10"]);
  // 대소문자는 순서를 가르지 않는다.
  assert.equal(compareValues("Alpha", "alpha"), 0);
});

test("빈 값은 방향과 무관하게 언제나 맨 뒤에 남는다", () => {
  const rows = [{ v: null }, { v: 3 }, { v: "" }, { v: 7 }, {}];
  for (const direction of ["asc", "desc"]) {
    const values = sortRows(rows, byValue, direction).map(byValue);
    assert.deepEqual(values.slice(2), [null, "", undefined], direction);
  }
});

test("값이 같으면 서버가 준 순서를 지키고 입력은 바뀌지 않는다", () => {
  const rows = [
    { id: "a", v: 1 },
    { id: "b", v: 2 },
    { id: "c", v: 1 },
    { id: "d", v: 2 },
  ];
  const snapshot = JSON.stringify(rows);
  assert.deepEqual(sortRows(rows, byValue, "asc").map((r) => r.id), ["a", "c", "b", "d"]);
  assert.deepEqual(sortRows(rows, byValue, "desc").map((r) => r.id), ["b", "d", "a", "c"]);
  assert.equal(JSON.stringify(rows), snapshot);
});

test("숫자와 글자가 섞인 열도 순서가 매번 같다", () => {
  const rows = [{ v: "n/a" }, { v: 5 }, { v: 1 }];
  assert.deepEqual(sortRows(rows, byValue, "asc").map(byValue), [1, 5, "n/a"]);
  assert.ok(compareValues(Number.NaN, 1) > 0);
});

test("숫자 열은 내림차순, 글자 열은 가나다순에서 시작한다", () => {
  assert.equal(firstDirection([null, "", 12]), "desc");
  assert.equal(firstDirection([undefined, "page_view"]), "asc");
  assert.equal(firstDirection([]), "asc");
});

test("같은 열은 첫 방향 → 반대 → 원래 순서로 돌고, 다른 열은 새로 시작한다", () => {
  let state = nextSort(null, "users", "desc");
  assert.deepEqual(state, { key: "users", direction: "desc" });
  state = nextSort(state, "users", "desc");
  assert.deepEqual(state, { key: "users", direction: "asc" });
  assert.equal(nextSort(state, "users", "desc"), null);
  assert.deepEqual(nextSort({ key: "users", direction: "asc" }, "page", "asc"), {
    key: "page",
    direction: "asc",
  });
});
