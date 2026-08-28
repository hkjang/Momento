import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// The session check turned every failure into "not signed in". A network blip, a
// 500, a timeout — all of them cleared the user and sent the reader to the login
// form, where signing in again would fail for the same reason and say nothing
// about it. Somebody with a perfectly valid session was signed out of the
// console by a moment of bad network.
//
// Only the server refusing the session — 401 or 403 — is an answer about whether
// anybody is signed in. Everything else is a failure to find out, and the two
// have to reach the screen as different things.
function source(path) {
  return readFileSync(new URL(`../src/${path}`, import.meta.url), "utf8");
}

test("세션 확인 실패와 로그아웃 상태를 구분한다", () => {
  const auth = source("contexts/AuthContext.tsx");

  // 거부(401/403)일 때만 사용자를 비운다.
  assert.match(
    auth,
    /error\.status === 401/,
    "AuthContext가 거부와 그 밖의 실패를 구분하지 않는다: 네트워크가 잠깐 끊기면 멀쩡한 세션이 로그아웃된다",
  );
  assert.match(auth, /sessionError/, "확인 실패를 기록하지 않는다");

  // 로그아웃은 서버 응답과 무관하게 로컬 상태를 비운다.
  const logout = auth.slice(auth.indexOf("logout:"));
  assert.match(
    logout.slice(0, 400),
    /finally\s*\{/,
    "로그아웃 요청이 실패하면 로그인된 것처럼 남는다: 나가겠다고 한 사람은 나간 것이다",
  );
});

test("확인하지 못했을 때 로그인 폼을 보여 주지 않는다", () => {
  const app = source("App.tsx");
  assert.match(
    app,
    /!user && sessionError/,
    "App이 확인 실패와 로그아웃을 같은 화면으로 처리한다: 멀쩡한 세션을 가진 사람을 같은 이유로 실패할 폼으로 보낸다",
  );
  assert.match(
    app,
    /로그인 상태를 확인하지 못했습니다/,
    "확인하지 못했을 때 할 말이 없다",
  );
  // 그리고 진짜 로그아웃 경로는 남아 있어야 한다.
  assert.match(app, /if \(!user\) return <LoginPage \/>;/);
});
