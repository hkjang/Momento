import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// Fifteen screens render NoSite when there is no site to analyse, and it said the
// same thing whether the reader had no sites or the console could not reach the
// server. A failed request told somebody with sites to go and create their first
// one — which invites them to fix the wrong thing, and to fix it by adding a
// duplicate.
//
// The site list is read outside React Query, in the shell's own bootstrap, so
// nothing else was going to notice. The rule is that a fetch there records what
// went wrong and the screen tells the two apart.
function source(path) {
  return readFileSync(new URL(`../src/${path}`, import.meta.url), "utf8");
}

test("사이트 목록 읽기 실패는 기록되고, 목록이 비어 있는 것과 구분된다", () => {
  const context = source("contexts/SiteContext.tsx");
  assert.match(
    context,
    /loadError/,
    "SiteContext가 목록 읽기 실패를 기록하지 않는다: 실패가 빈 목록과 구분되지 않는다",
  );
  // 두 읽기 모두 실패를 잡아야 한다 — 사이트 목록과 환경 목록.
  assert.equal(
    (context.match(/setLoadError/g) || []).length >= 2,
    true,
    "실패를 기록하는 자리가 둘보다 적다: 사이트 목록과 환경 목록 둘 다 조용히 실패할 수 있다",
  );
  // 아무도 기다리지 않는 호출이 unhandled rejection이 되면 안 된다.
  assert.match(
    context,
    /refresh\(\)\.catch\(/,
    "부팅 시 호출이 거부되면 unhandled rejection이 된다",
  );

  const states = source("components/States.tsx");
  // 문자열이 있는지가 아니라 **컨텍스트에서 온 값인지**를 봅니다. 첫 판본은
  // `loadError`가 파일 어딘가에 있기만 하면 통과했고, `const loadError = null`로
  // 바꿔도 그대로 통과했습니다 — 화면은 다시 구분하지 못하는데도.
  assert.match(
    states,
    /const\s*\{[^}]*loadError[^}]*\}\s*=\s*useSite\(\)/,
    "NoSite가 읽기 실패를 컨텍스트에서 가져오지 않는다: 사이트가 있는 사람에게 첫 사이트를 만들라고 말하게 된다",
  );
  assert.match(
    states,
    /사이트 목록을 불러오지 못했습니다/,
    "실패했을 때 보여 줄 말이 없다",
  );
  // 그리고 진짜로 비어 있을 때의 안내는 남아 있어야 한다.
  assert.match(states, /분석할 사이트가 없습니다/);
  assert.match(states, /사이트 만들기/);
});
