import assert from "node:assert/strict";
import test from "node:test";
import { clampPage } from "../src/components/tablePaging.ts";

test("범위 안의 page 는 그대로 쓰인다", () => {
  assert.equal(clampPage(0, 25, 100), 0);
  assert.equal(clampPage(1, 25, 100), 1);
  assert.equal(clampPage(3, 25, 100), 3);
  assert.equal(clampPage(2, 10, 21), 2);
});

test("마지막 페이지를 넘는 page 는 마지막 유효 페이지로 내려간다", () => {
  // 100행 25개씩이면 마지막은 3페이지. 결과가 5건으로 줄면 0페이지뿐이다.
  assert.equal(clampPage(3, 25, 100), 3);
  assert.equal(clampPage(3, 25, 5), 0);
  assert.equal(clampPage(9, 10, 35), 3);
  assert.equal(clampPage(4, 10, 21), 2);
});

test("행이 없으면 0, 한 페이지에 다 들어가면 0", () => {
  assert.equal(clampPage(0, 25, 0), 0);
  assert.equal(clampPage(7, 25, 0), 0);
  assert.equal(clampPage(7, 25, 24), 0);
  assert.equal(clampPage(7, 25, 25), 0);
  assert.equal(clampPage(1, 25, 26), 1);
});

test("음수·NaN·나눌 수 없는 pageSize 는 첫 페이지로 읽는다", () => {
  assert.equal(clampPage(-1, 25, 100), 0);
  assert.equal(clampPage(Number.NaN, 25, 100), 0);
  assert.equal(clampPage(2, 0, 100), 0);
  assert.equal(clampPage(2, -10, 100), 0);
});

// DataTable 이 이 값으로 slice 하므로, total 이 0 이 아닌 한 잘라낸 결과가 비어서는
// 안 된다. 비면 행도 Empty 안내문도 없는 빈 표가 나온다(Empty 는 total===0 에서만
// 나온다). clampPage 를 raw page 로 되돌리면 이 순회가 실패한다.
test("clamp 한 page 로 slice 하면 total>0 인 어떤 조합에서도 결과가 비지 않는다", () => {
  for (const pageSize of [1, 3, 10, 25, 50]) {
    for (let total = 1; total <= 60; total += 1) {
      const rows = Array.from({ length: total }, (_, index) => index);
      for (let page = 0; page <= 12; page += 1) {
        const safe = clampPage(page, pageSize, total);
        const paged = rows.slice(safe * pageSize, safe * pageSize + pageSize);
        assert.ok(
          paged.length > 0,
          `page=${page} pageSize=${pageSize} total=${total} → ${paged.length}행`,
        );
        // 잘라낸 행은 실제 데이터 안에 있어야 한다 — 범위를 넘겨 자르지 않는다.
        assert.ok(paged.length <= pageSize);
        assert.equal(paged[0], safe * pageSize);
      }
    }
  }
});

// TablePagination 과 slice 가 같은 값을 읽어야 "4페이지" 라고 표시하면서 0페이지
// 행을 보여주거나 그 반대가 되지 않는다.
test("표시용 페이지 번호와 slice 가 같은 값을 쓴다", () => {
  for (const [page, pageSize, total] of [
    [3, 25, 5],
    [9, 10, 35],
    [0, 25, 100],
  ]) {
    const safe = clampPage(page, pageSize, total);
    assert.ok(safe * pageSize < total);
    assert.equal(safe, Math.min(page, Math.ceil(total / pageSize) - 1));
  }
});
