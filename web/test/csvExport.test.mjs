import assert from "node:assert/strict";
import test from "node:test";
import { buildCSV, csvValue } from "../src/components/csvExport.ts";

// 표의 값은 대부분 방문자의 브라우저가 보낸 것이다. 스프레드시트는 "="로 시작하는
// 셀을 글자가 아니라 수식으로 읽으므로, 그대로 내보내면 수집기에 닿을 수 있는
// 누구나 분석가의 Excel 이 무엇을 실행할지 정할 수 있다.
test("수식으로 읽힐 셀은 글자로 표시된다", () => {
  for (const hostile of [
    '=HYPERLINK("https://evil.example","open")',
    "+1+1",
    "-2+3",
    "@SUM(A1:A9)",
    " =cmd",
    "\t=cmd",
  ]) {
    const cell = csvValue(hostile);
    assert.ok(cell.startsWith(`"'`), `${JSON.stringify(hostile)} → ${cell}`);
    // 값 자체는 바뀌지 않는다 — 앞에 아포스트로피 하나가 붙을 뿐이다.
    assert.equal(cell, `"'${hostile.replaceAll('"', '""')}"`);
  }
});

test("숫자·JSON·URL·빈 값·보통 글자는 그대로 나간다", () => {
  assert.equal(csvValue(-12.5), '"-12.5"');
  assert.equal(csvValue(0), '"0"');
  assert.equal(csvValue(null), '""');
  assert.equal(csvValue(undefined), '""');
  assert.equal(csvValue("page_view"), '"page_view"');
  assert.equal(
    csvValue("https://portal.internal/home?q=a"),
    '"https://portal.internal/home?q=a"',
  );
  assert.equal(csvValue({ feature: "=search" }), '"{""feature"":""=search""}"');
  assert.equal(csvValue('say "hi"'), '"say ""hi"""');
});

test("표 전체를 만들 때 머리글과 각 행에 같은 규칙이 적용된다", () => {
  const csv = buildCSV(
    [
      { key: "page", label: "페이지" },
      { key: "views", label: "조회" },
    ],
    [
      { page: "=cmd|' /C calc'!A0", views: -3 },
      { page: "/home", views: 12 },
    ],
  );
  assert.equal(
    csv,
    ['"페이지","조회"', `"'=cmd|' /C calc'!A0","-3"`, '"/home","12"'].join(
      "\n",
    ),
  );
});
