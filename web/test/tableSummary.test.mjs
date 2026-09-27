import assert from "node:assert/strict";
import test from "node:test";
import { tableCaption } from "../src/components/tableSummary.ts";

test("검색하지 않을 때는 description 을 그대로 보여준다", () => {
  assert.equal(
    tableCaption({
      description: "첫 페이지별 세션과 이탈률입니다.",
      total: 40,
      matched: 40,
      searching: false,
    }),
    "첫 페이지별 세션과 이탈률입니다.",
  );
});

test("검색하지 않고 description 도 없으면 항목 수만 보여준다", () => {
  assert.equal(
    tableCaption({ description: undefined, total: 40, matched: 40, searching: false }),
    "40개 항목",
  );
  // 빈 문자열 description 은 지금 코드의 `description || …` 과 같게 취급한다.
  assert.equal(
    tableCaption({ description: "", total: 7, matched: 7, searching: false }),
    "7개 항목",
  );
});

test("검색 중에 description 이 있으면 뒤에 일치 건수를 붙인다", () => {
  assert.equal(
    tableCaption({
      description: "자기 자신으로 반복된 이동은 제외합니다.",
      total: 1240,
      matched: 3,
      searching: true,
    }),
    "자기 자신으로 반복된 이동은 제외합니다. · 전체 1,240개 중 3개 일치",
  );
});

test("검색 중에 description 이 없으면 일치 건수만 보여준다", () => {
  assert.equal(
    tableCaption({ description: undefined, total: 1240, matched: 3, searching: true }),
    "전체 1,240개 중 3개 일치",
  );
});

test("일치가 0건이어도 전체 규모와 0을 보여준다", () => {
  assert.equal(
    tableCaption({ description: undefined, total: 52, matched: 0, searching: true }),
    "전체 52개 중 0개 일치",
  );
  assert.equal(
    tableCaption({ description: "진입 페이지", total: 52, matched: 0, searching: true }),
    "진입 페이지 · 전체 52개 중 0개 일치",
  );
});

test("전체가 다 일치해도 같은 문구를 쓴다", () => {
  assert.equal(
    tableCaption({ description: undefined, total: 12, matched: 12, searching: true }),
    "전체 12개 중 12개 일치",
  );
});

// 1,000 이상에서 구분자가 빠지면 규모를 읽기 어렵다. 검색 중 문구와 검색 전 문구가
// 같은 Intl.NumberFormat("ko-KR") 을 쓰는지 양쪽 다 확인한다.
test("1,000 이상은 한국어 천 단위 구분자가 붙는다", () => {
  const fmt = Intl.NumberFormat("ko-KR");
  assert.equal(
    tableCaption({ description: undefined, total: 1240, matched: 1200, searching: true }),
    "전체 1,240개 중 1,200개 일치",
  );
  assert.equal(
    tableCaption({
      description: undefined,
      total: 1234567,
      matched: 1234567,
      searching: false,
    }),
    `${fmt.format(1234567)}개 항목`,
  );
});

// 검색어가 없으면 DataTable 의 filtered 는 rows 를 그대로 돌려주므로 matched===total
// 이 된다. 그 상태에서 캡션이 지금과 한 글자도 달라지지 않는 것이 이 변경의 조건이다.
// tableCaption 을 상수 반환이나 검색 분기 없는 구현으로 되돌리면 이 순회가 실패한다.
test("검색 전 캡션은 기존 `description || N개 항목` 과 완전히 같다", () => {
  const fmt = Intl.NumberFormat("ko-KR");
  for (const total of [0, 1, 8, 25, 999, 1000, 1240]) {
    for (const description of [undefined, "", "표 설명"]) {
      assert.equal(
        tableCaption({ description, total, matched: total, searching: false }),
        description || `${fmt.format(total)}개 항목`,
      );
    }
  }
});

test("검색 중 캡션은 항상 일치 건수와 전체 건수를 둘 다 담는다", () => {
  const fmt = Intl.NumberFormat("ko-KR");
  for (const [total, matched] of [
    [1240, 0],
    [1240, 3],
    [1240, 1240],
    [10, 10],
    [1, 0],
  ]) {
    for (const description of [undefined, "", "표 설명"]) {
      const caption = tableCaption({ description, total, matched, searching: true });
      assert.ok(
        caption.includes(`${fmt.format(matched)}개 일치`),
        `일치 건수 누락: ${caption}`,
      );
      assert.ok(caption.includes(`전체 ${fmt.format(total)}개`), `전체 건수 누락: ${caption}`);
      if (description) assert.ok(caption.startsWith(description));
    }
  }
});
