import test from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";

import { allowedRanges } from "../src/components/queryError.ts";

// The server tells the console each site's max_exact_days and says of it: "The
// console builds its period options from this, so it never offers a range the
// site's policy will refuse."
//
// That was true of RangeSelect and false of AnalysisToolbar, which hard-coded 7,
// 30 and 90 — and AnalysisToolbar is the control on the overview and the visitor
// insight screens. On a site limited to 14 days the dropdown offered two periods
// that answer RANGE_EXCEEDS_POLICY, on the two screens people open first.
//
// A control that offers a period cannot decide for itself whether the limit
// applies, so this reads the sources: every period control must run its options
// through allowedRanges, and every screen that mounts one must hand it the
// site's limit.
test("모든 기간 선택 컨트롤은 사이트 정책을 적용한다", () => {
  const controls = ["AnalysisToolbar.tsx", "RangeSelect.tsx"];
  for (const file of controls) {
    const source = readFileSync(new URL(`../src/components/${file}`, import.meta.url), "utf8");
    assert.ok(
      source.includes("allowedRanges("),
      `${file}은 기간 목록을 allowedRanges로 거르지 않는다: 정책이 거부할 기간을 제시하게 된다`,
    );
    assert.ok(
      source.includes("maxExactDays"),
      `${file}은 사이트 한도를 받지 않는다`,
    );
  }
});

test("기간 컨트롤을 붙이는 화면은 사이트 한도를 넘겨준다", () => {
  const dir = new URL("../src/pages/", import.meta.url);
  let mounted = 0;
  for (const name of readdirSync(dir)) {
    if (!name.endsWith(".tsx")) continue;
    const source = readFileSync(new URL(name, dir), "utf8");
    for (const control of ["<AnalysisToolbar", "<RangeSelect"]) {
      let from = source.indexOf(control);
      while (from !== -1) {
        const end = source.indexOf("/>", from);
        const element = source.slice(from, end === -1 ? source.length : end);
        mounted += 1;
        assert.ok(
          element.includes("maxExactDays"),
          `${name}의 ${control}에 maxExactDays가 없다: 이 화면은 정책이 거부할 기간을 제시한다\n${element.slice(0, 240)}`,
        );
        from = source.indexOf(control, from + 1);
      }
    }
  }
  // 컨트롤을 하나도 못 찾았다면 이 검사는 아무것도 확인하지 않은 것이다.
  assert.ok(mounted >= 8, `기간 컨트롤을 ${mounted}개만 찾았다`);
});

test("한도가 없으면 모든 기간이 남고, 한도보다 짧은 것만 제시한다", () => {
  assert.deepEqual(allowedRanges([7, 30, 90], undefined), [7, 30, 90]);
  assert.deepEqual(allowedRanges([7, 30, 90], 14), [7]);
  // 가장 짧은 기간마저 한도를 넘으면 컨트롤을 비우지 않는다: 읽는 사람이
  // 거부와 그 설명을 보는 편이 빈 선택지보다 낫다.
  assert.deepEqual(allowedRanges([7, 30, 90], 3), [7]);
});
