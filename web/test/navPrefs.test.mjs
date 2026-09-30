import assert from "node:assert/strict";
import test from "node:test";
import { FAVORITE_LIMIT, parseExpanded, parseFavorites, toggleFavorite } from "../src/components/navPrefs.ts";

test("펼침 상태는 참거짓만 남긴다", () => {
  assert.deepEqual(parseExpanded('{"분석":true,"모니터링":false,"x":1}'), { 분석: true, 모니터링: false });
  assert.deepEqual(parseExpanded("[1]"), {});
  assert.deepEqual(parseExpanded("{"), {});
});

test("즐겨찾기는 메뉴에 있는 화면만, 개수 안에서 기억한다", () => {
  assert.deepEqual(parseFavorites('["/events","/gone",3]', ["/events", "/pages"]), ["/events"]);
  let favorites = [];
  favorites = toggleFavorite(favorites, "/events");
  favorites = toggleFavorite(favorites, "/pages");
  assert.deepEqual(favorites, ["/events", "/pages"]);
  assert.deepEqual(toggleFavorite(favorites, "/events"), ["/pages"]);
  let full = Array.from({ length: FAVORITE_LIMIT }, (_, i) => `/p${i}`);
  full = toggleFavorite(full, "/new");
  assert.equal(full.length, FAVORITE_LIMIT);
  assert.equal(full.at(-1), "/new");
  assert.equal(full[0], "/p1");
});
