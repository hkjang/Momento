import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

import { metricMeaning } from "../src/pages/signalGuide.ts";

// metricMeaning says what it is for: "the metric names that could be mistaken for
// each other. Two session counts answer two different questions and used to share
// one name, so the reader has to be told which is which."
//
// The query builder offered user_conversion_rate and session_conversion_rate side
// by side and explained neither — two names differing by one word, over different
// denominators, which is the case this module exists for.
//
// So the rule is derived from the offered list rather than kept as a second list
// to remember: if a metric shares a word with another metric on offer, a reader
// choosing between them has to be told which is which.
function offeredMetrics() {
  const source = readFileSync(new URL("../src/pages/ExplorerPage.tsx", import.meta.url), "utf8");
  const list = source.slice(source.indexOf("const metrics = ["));
  const body = list.slice(0, list.indexOf("]"));
  return [...body.matchAll(/"([a-z_]+)"/g)].map((m) => m[1]);
}

// A metric's words, singular, so "sessions" and "conversion_sessions" share one.
function words(metric) {
  return metric.split("_").map((word) => word.replace(/s$/, ""));
}

test("서로 헷갈릴 수 있는 지표는 모두 설명이 있다", () => {
  const metrics = offeredMetrics();
  assert.ok(metrics.length >= 10, `쿼리 빌더의 지표 목록을 ${metrics.length}개만 읽었다`);

  const shared = metrics.filter((metric) =>
    metrics.some(
      (other) => other !== metric && words(other).some((word) => words(metric).includes(word)),
    ),
  );
  assert.ok(shared.length >= 6, `이름이 겹치는 지표를 ${shared.length}개만 찾았다`);

  for (const metric of shared) {
    assert.ok(
      metricMeaning[metric],
      `${metric}은(는) 다른 지표와 이름을 나눠 쓰는데 설명이 없다: 읽는 사람은 이름 하나만 보고 둘 중 무엇을 고를지 정해야 한다`,
    );
  }
});

test("설명은 무엇으로 나누는지를 말한다", () => {
  // 비율은 분모가 다르다는 것이 요점이므로, 그 말이 없으면 설명이 아니다.
  for (const rate of ["user_conversion_rate", "session_conversion_rate"]) {
    assert.match(
      metricMeaning[rate],
      /÷/,
      `${rate}의 설명이 무엇을 무엇으로 나누는지 말하지 않는다`,
    );
  }
});
