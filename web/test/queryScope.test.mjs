import test from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";

// keepPrevious.ts says why a query key has to carry the site and the environment:
// "The one thing worse than a blank screen is one site's numbers displayed under
// another site's name, so a change of site or environment still clears. Every
// analytical query key carries both."
//
// Four did not. rangeQuery puts the selected environment into the URL, so the
// request was right — but React Query decides whether to fetch from the key, and
// a key that does not change when the environment does means no fetch at all.
// Ecommerce, 사내 사용 현황, visitor search and visitor trace answered from the
// cache of the environment the reader had just left, under the new one's name,
// and stayed that way.
//
// The rule reads the source rather than keeping a list: a query whose URL is
// built with rangeQuery is a query whose answer depends on the environment, so
// its key has to say so.
function pageSources() {
  const out = [];
  for (const dir of ["../src/pages/", "../src/components/"]) {
    const base = new URL(dir, import.meta.url);
    for (const name of readdirSync(base)) {
      if (!name.endsWith(".tsx")) continue;
      out.push([dir + name, readFileSync(new URL(name, base), "utf8")]);
    }
  }
  return out;
}

// Each useQuery({...}) block in a file, with its line number.
function queries(source) {
  const found = [];
  let from = source.indexOf("useQuery({");
  while (from !== -1) {
    let depth = 0;
    let end = from;
    for (let i = source.indexOf("{", from); i < source.length; i += 1) {
      if (source[i] === "{") depth += 1;
      if (source[i] === "}") {
        depth -= 1;
        if (depth === 0) {
          end = i;
          break;
        }
      }
    }
    found.push({
      body: source.slice(from, end + 1),
      line: source.slice(0, from).split("\n").length,
    });
    from = source.indexOf("useQuery({", end);
  }
  return found;
}

test("환경에 따라 답이 달라지는 조회는 키에도 환경이 있다", () => {
  let checked = 0;
  for (const [name, source] of pageSources()) {
    for (const { body, line } of queries(source)) {
      if (!body.includes("rangeQuery(")) continue;
      checked += 1;
      const key = /queryKey:\s*\[([^\]]*)\]/s.exec(body);
      assert.ok(key, `${name}:${line} 의 useQuery에 queryKey가 없다`);
      assert.ok(
        key[1].includes("environment"),
        `${name}:${line} 의 조회는 URL에 환경을 실으면서 키에는 넣지 않는다: 환경을 바꿔도 다시 가져오지 않고, 방금 떠난 환경의 숫자를 새 환경 이름 아래 계속 보여 준다\n  키: [${key[1].replace(/\s+/g, " ").trim()}]`,
      );
    }
  }
  // 이 검사가 아무 조회도 못 찾았다면 통과는 아무 뜻이 없다.
  assert.ok(checked >= 20, `환경에 의존하는 조회를 ${checked}개만 찾았다`);
});
