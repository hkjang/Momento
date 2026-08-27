import assert from "node:assert/strict";
import test from "node:test";
import {
  describeSignal,
  frustrationSetupHint,
  searchSetupHint,
  zeroResultReadiness,
  ecommerceSetupHint,
  featureSetupHint,
  aiSetupHint,
} from "../src/pages/signalGuide.ts";

test("every automatic signal explains what it means and what to do", () => {
  for (const name of [
    "rage_click",
    "dead_click",
    "rapid_back",
    "form_retry",
    "repeated_search",
    "error_after_click",
    "slow_interaction",
  ]) {
    const described = describeSignal(name);
    assert.equal(described.origin, "automatic", `${name} is detected by the tracker`);
    assert.ok(described.label !== name, `${name} has a readable label`);
    assert.ok(described.meaning.length > 10);
    assert.ok(described.action.length > 10);
  }
});

test("a signal the tracker does not send is marked as the site's own", () => {
  const described = describeSignal("approval_timeout");
  assert.equal(described.origin, "manual");
  assert.equal(described.label, "approval_timeout");
});

test("an empty frustration report says whether anything is being measured", () => {
  assert.match(frustrationSetupHint({ total_sessions: 0, affected_sessions: 0 }), /스니펫이 설치/);
  assert.match(
    frustrationSetupHint({ total_sessions: 4000, affected_sessions: 0 }),
    /0\.22\.0부터 자동 감지/,
  );
  assert.equal(frustrationSetupHint({ total_sessions: 4000, affected_sessions: 12 }), null);
  assert.equal(frustrationSetupHint(undefined), null);
});

test("search guidance separates no searches from uncollected terms", () => {
  assert.match(searchSetupHint({ searches: 0 }, []), /trackSearch/);
  assert.match(
    searchSetupHint({ searches: 120 }, [{ query: "(not set)" }, { query: "(not set)" }]),
    /data-collect-search-terms/,
  );
  assert.equal(searchSetupHint({ searches: 120 }, [{ query: "연차" }]), null);
});

test("a missing result-count hook is reported instead of read as perfect search", () => {
  assert.match(zeroResultReadiness({ searches: 300, zero_results: 0 }), /data-momento-search-results/);
  assert.equal(zeroResultReadiness({ searches: 300, zero_results: 4 }), null);
  assert.equal(zeroResultReadiness({ searches: 0 }), null);
});

test("영향 분석 헤드라인은 먼저 고칠 신호를 지목한다", async () => {
  const { frictionHeadline } = await import("../src/pages/signalGuide.ts");
  const headline = frictionHeadline([
    {
      signal: "error_after_click",
      affected_people: 400,
      unaffected_people: 600,
      affected_conversion_rate: 10,
      unaffected_conversion_rate: 26.7,
      gap_points: -16.7,
      verdict: "worse",
      reliable: true,
      estimated_lost_conversions: 67,
      evidence: "",
    },
  ]);
  assert.match(headline, /Error After Click/);
  assert.match(headline, /16\.7%p/);
  assert.match(headline, /67건/);
});

test("영향 분석은 손실이 없을 때와 판단할 수 없을 때를 구분한다", async () => {
  const { frictionHeadline } = await import("../src/pages/signalGuide.ts");
  const base = {
    signal: "slow_interaction",
    affected_people: 200,
    unaffected_people: 800,
    affected_conversion_rate: 19.5,
    unaffected_conversion_rate: 20.1,
    gap_points: -0.6,
    estimated_lost_conversions: 0,
    evidence: "",
  };
  assert.match(
    frictionHeadline([{ ...base, verdict: "similar", reliable: true }]),
    /뚜렷하게 떨어뜨리는 것은 없습니다/,
  );
  assert.match(
    frictionHeadline([{ ...base, verdict: "insufficient", reliable: false }]),
    /인원이 모이지 않아/,
  );
  assert.equal(frictionHeadline([]), null);
  assert.equal(frictionHeadline(undefined), null);
});

// The ecommerce screen renders a complete-looking report of zeroes whether a
// site sold nothing or never instrumented selling at all, and a purchase can
// arrive without its money or without its items while everything else works.
test("이커머스 빈 화면은 안 팔린 것과 계측되지 않은 것을 구분한다", () => {
  const none = ecommerceSetupHint({ transactions: 0, revenue: 0 }, [{ event: "purchase", users: 0 }], []);
  assert.match(String(none), /이커머스 이벤트가 하나도 없습니다/);

  const noMoney = ecommerceSetupHint({ transactions: 12, revenue: 0 }, [{ event: "purchase", users: 9 }], [{}]);
  assert.match(String(noMoney), /금액이 비어 있습니다/);

  const noItems = ecommerceSetupHint({ transactions: 12, revenue: 4500 }, [{ event: "purchase", users: 9 }], []);
  assert.match(String(noItems), /상품 정보가 없습니다/);

  // A site that measures everything and simply had a quiet month gets nothing:
  // an advisory that never goes away stops being read.
  assert.equal(
    ecommerceSetupHint({ transactions: 12, revenue: 4500 }, [{ event: "purchase", users: 9 }], [{}]),
    null,
  );
  // And activity without a sale is a real answer, not a setup problem.
  assert.equal(
    ecommerceSetupHint({ transactions: 0, revenue: 0 }, [{ event: "view_item", users: 40 }], []),
    null,
  );
});

// The feature and AI screens read data a site has to send itself, so an empty
// table is two different situations wearing the same face.
test("기능 화면은 사용이 없는 것과 태깅되지 않은 것을 구분한다", () => {
  assert.match(String(featureSetupHint({ population: 0, features: [] })), /이벤트가 없습니다/);
  assert.match(
    String(featureSetupHint({ population: 400, features: [] })),
    /`feature` 속성이 없습니다/,
  );
  assert.equal(featureSetupHint({ population: 400, features: [{}] }), null);
});

test("AI 화면은 이벤트가 없는 것과 구분 속성이 빈 것을 나눈다", () => {
  assert.match(String(aiSetupHint([], "model")), /AI 이벤트가 없습니다/);
  assert.match(
    String(aiSetupHint([{ label: "(not set)" }, { label: "(not set)" }], "model")),
    /`model` 속성이 비어 있어/,
  );
  assert.equal(aiSetupHint([{ label: "claude" }], "model"), null);
  // The advisory names the dimension the reader actually chose.
  assert.match(String(aiSetupHint([{ label: "(not set)" }], "provider")), /provider/);
});
