import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// Every screen used to be part of the first download. Signing in fetched the
// whole console — the 570KB charting library included — before the landing
// screen could be drawn, and a reader who only opens two screens paid for all
// twenty-two of them.
//
// A route added with a plain import puts that screen back into the first load,
// and nothing about the console would look wrong; it would just be slower for
// everyone, permanently. This names the route that did it.
//
// The login page is deliberately not lazy: it is the screen an unauthenticated
// visitor lands on, so deferring it would only add a round trip.
test("every screen behind the sign-in is loaded on demand", () => {
  const source = readFileSync(new URL("../src/App.tsx", import.meta.url), "utf8");
  const eager = [...source.matchAll(/^import\s+(\w+)\s+from\s+"\.\/pages\/(\w+)";$/gm)]
    .map((match) => match[2])
    .filter((name) => name !== "LoginPage");
  assert.deepEqual(
    eager,
    [],
    `these screens are in the first download instead of being fetched when opened: ${eager.join(", ")}`,
  );

  // And the routes have to actually be wired to something lazy, not merely
  // declared that way: a lazy() that no Suspense boundary encloses throws.
  assert.match(source, /<Suspense/, "the lazy screens need a Suspense boundary to render into");
});
