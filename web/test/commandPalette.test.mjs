import assert from "node:assert/strict";
import test from "node:test";
import {
  moveActive,
  orderWithRecent,
  parseRecent,
  rememberRoute,
  routeFor,
} from "../src/components/commandPalette.ts";

test("방향키는 목록 끝에서 반대쪽 끝으로 돈다", () => {
  assert.equal(moveActive(0, 1, 3), 1);
  assert.equal(moveActive(2, 1, 3), 0);
  assert.equal(moveActive(0, -1, 3), 2);
  assert.equal(moveActive(5, 1, 0), 0);
});

test("지금 주소의 메뉴는 조건이 맞는 것 중 가장 구체적인 것이다", () => {
  const routes = ["/", "/events", "/admin", "/admin?section=users", "/admin?section=sites"];
  assert.equal(routeFor("/admin", "?section=users", routes), "/admin?section=users");
  assert.equal(routeFor("/admin", "", routes), "/admin");
  assert.equal(routeFor("/admin", "?section=unknown", routes), "/admin");
  // 메뉴가 모르는 조건은 무시한다.
  assert.equal(routeFor("/events", "?days=90", routes), "/events");
  assert.equal(routeFor("/nowhere", "", routes), null);
});

test("최근 화면은 중복 없이 새것이 앞이고 개수가 제한된다", () => {
  let recent = [];
  for (const to of ["/a", "/b", "/a", "/c", "/d", "/e", "/f"]) recent = rememberRoute(recent, to);
  assert.deepEqual(recent, ["/f", "/e", "/d", "/c", "/a"]);
});

test("저장소의 값이 망가져 있어도 빈 목록이 된다", () => {
  assert.deepEqual(parseRecent(null), []);
  assert.deepEqual(parseRecent("{"), []);
  assert.deepEqual(parseRecent('{"a":1}'), []);
  assert.deepEqual(parseRecent('["/a", 3, null, "/b"]'), ["/a", "/b"]);
});

test("검색어가 없으면 최근 화면이 먼저 나오고 사라진 메뉴는 빠진다", () => {
  const routes = [{ to: "/" }, { to: "/events" }, { to: "/pages" }, { to: "/sessions" }];
  const ordered = orderWithRecent(routes, ["/sessions", "/admin?section=users", "/events"], 3);
  assert.deepEqual(ordered.map((r) => [r.to, r.recent]), [
    ["/sessions", true],
    ["/events", true],
    ["/", false],
  ]);
});
