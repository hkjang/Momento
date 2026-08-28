import assert from "node:assert/strict";
import test from "node:test";

/**
 * The frustration and search detectors react to real DOM events, so this file
 * runs them against a DOM small enough to reason about: the element tree, the
 * selectors the tracker actually uses, and a mutation observer the test drives
 * by hand.
 */

class MemoryStorage {
  values = new Map();
  getItem(key) {
    return this.values.get(key) ?? null;
  }
  setItem(key, value) {
    this.values.set(key, String(value));
  }
  removeItem(key) {
    this.values.delete(key);
  }
}

function setGlobal(name, value) {
  Object.defineProperty(globalThis, name, {
    value,
    writable: true,
    configurable: true,
  });
}

function matchOne(node, selector) {
  if (!node.tagName) return false;
  if (selector.startsWith("[")) {
    const body = selector.slice(1, -1);
    const equals = body.indexOf("=");
    const name = equals === -1 ? body : body.slice(0, equals);
    const attribute = node.getAttribute(name);
    if (attribute === null || attribute === undefined) return false;
    if (equals === -1) return true;
    return attribute === body.slice(equals + 1).replace(/["']/g, "");
  }
  if (selector.startsWith("#")) return node.id === selector.slice(1);
  return node.tagName.toLowerCase() === selector.toLowerCase();
}

function matches(node, selector) {
  return selector.split(",").some((part) => matchOne(node, part.trim()));
}

const registry = [];

function el(tag, options = {}) {
  const node = {
    tagName: tag.toUpperCase(),
    id: options.id || "",
    className: options.className || "",
    name: options.name || "",
    type: options.type,
    href: options.href,
    origin: options.href ? new URL(options.href).origin : undefined,
    pathname: options.href ? new URL(options.href).pathname : undefined,
    innerText: options.text || "",
    dataset: options.dataset || {},
    attributes: options.attributes || {},
    children: [],
    parentElement: null,
    cursor: options.cursor || "auto",
    getAttribute(attribute) {
      if (attribute in this.attributes) return this.attributes[attribute];
      if (attribute === "role") return this.attributes.role ?? null;
      if (attribute === "name") return this.name || null;
      if (attribute === "action") return this.attributes.action ?? null;
      return null;
    },
    setAttribute(attribute, value) {
      this.attributes[attribute] = String(value);
    },
    closest(selector) {
      let current = this;
      while (current) {
        if (matches(current, selector)) return current;
        current = current.parentElement;
      }
      return null;
    },
    querySelectorAll(selector) {
      const found = [];
      const walk = (parent) => {
        for (const child of parent.children) {
          if (matches(child, selector.replace(/\[href\]$/, "")))
            found.push(child);
          walk(child);
        }
      };
      walk(this);
      return found;
    },
    append(...nodes) {
      for (const child of nodes) {
        child.parentElement = this;
        this.children.push(child);
      }
      return this;
    },
  };
  registry.push(node);
  return node;
}

const listeners = new Map();
const observers = [];

// Auto-tracking installs a heartbeat interval it never clears, which is correct
// in a browser and would keep this process alive forever. Unreferencing the
// timer keeps the behaviour and lets the test file exit.
const realSetInterval = globalThis.setInterval;

function harness(url = "https://service.internal/page/a") {
  setGlobal("setInterval", (handler, ms) => {
    const timer = realSetInterval(handler, ms);
    timer.unref?.();
    return timer;
  });
  registry.length = 0;
  listeners.clear();
  observers.length = 0;
  setGlobal("window", globalThis);
  setGlobal("location", new URL(url));
  setGlobal("localStorage", new MemoryStorage());
  setGlobal("navigator", {
    userAgent: "Mozilla/5.0 Chrome/140.0",
    language: "ko-KR",
    doNotTrack: "0",
    sendBeacon: () => true,
  });
  setGlobal("screen", { width: 1920, height: 1080 });
  setGlobal("scrollY", 0);
  setGlobal("innerHeight", 800);
  const documentElement = el("html");
  setGlobal("document", {
    title: "Page A",
    referrer: "",
    currentScript: null,
    visibilityState: "visible",
    readyState: "complete",
    documentElement,
    activeElement: null,
    body: documentElement,
    querySelector(selector) {
      return registry.find((node) => matches(node, selector)) || null;
    },
  });
  setGlobal("history", {
    pushState(_state, _title, url) {
      if (url) setGlobal("location", new URL(url, location.href));
    },
    replaceState() {},
  });
  setGlobal("addEventListener", (name, handler) => {
    if (!listeners.has(name)) listeners.set(name, []);
    listeners.get(name).push(handler);
  });
  setGlobal("getComputedStyle", (node) => ({ cursor: node.cursor || "auto" }));
  setGlobal(
    "MutationObserver",
    class {
      constructor(callback) {
        this.callback = callback;
        observers.push(this);
      }
      observe() {
        this.observing = true;
      }
      disconnect() {
        this.observing = false;
      }
      trigger() {
        if (this.observing) this.callback([]);
      }
    },
  );
  setGlobal("PerformanceObserver", undefined);
  // now() matters as much as the entry list: every browser has it, the tracker
  // measures its in-page windows with it, and a stub without it sent the tracker
  // back to the wall clock — where a two second backward jump on this machine made
  // a search two seconds old look 120ms old and swallowed the signal.
  setGlobal("performance", {
    getEntriesByType: () => [],
    now: () => Number(process.hrtime.bigint() / 1000000n),
  });
  document.getSelection = undefined;
  window.getSelection = () => ({ toString: () => "" });
  const deliveries = [];
  setGlobal("fetch", async (_url, init) => {
    deliveries.push(JSON.parse(init.body));
    return { ok: true, status: 202 };
  });
  return { deliveries, documentElement };
}

function dispatch(name, event) {
  for (const handler of listeners.get(name) || []) handler(event);
}

const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// The module installs itself on window at import time, so the globals have to
// exist before the import, not only inside each test.
harness();
const { MomentoTracker } = await import("../src/index.ts");

function start(overrides = {}) {
  const tracker = new MomentoTracker();
  tracker.init({
    siteId: "SITE_TEST",
    endpoint: "https://analytics.internal",
    mode: "cookieless",
    batchSize: 500,
    ...overrides,
  });
  return tracker;
}

async function events(tracker, deliveries) {
  await tracker.flush();
  return deliveries.flatMap((delivery) => delivery.events);
}

test("three clicks on one element within a second report a rage click", async () => {
  const { deliveries } = harness();
  const tracker = start();
  const button = el("button", { id: "save" });
  harnessAttach(button);
  for (let i = 0; i < 4; i += 1) dispatch("click", { target: button });
  await wait(1300);
  const found = (await events(tracker, deliveries)).filter(
    (event) => event.name === "rage_click",
  );
  assert.equal(found.length, 1, "one event per burst, not one per click");
  assert.equal(found[0].properties.clicks, 4, "reports the real click count");
  assert.equal(found[0].properties.element_id, "save");
});

test("a click that changes nothing reports a dead click, a click that mutates does not", async () => {
  const { deliveries, documentElement } = harness();
  const tracker = start();
  const inert = el("div", { id: "inert", cursor: "pointer" });
  const alive = el("div", { id: "alive", cursor: "pointer" });
  documentElement.append(inert, alive);

  dispatch("click", { target: inert });
  await wait(1400);
  dispatch("click", { target: alive });
  observers[observers.length - 1].trigger();
  await wait(1400);

  const dead = (await events(tracker, deliveries)).filter(
    (event) => event.name === "dead_click",
  );
  assert.deepEqual(
    dead.map((event) => event.properties.element_id),
    ["inert"],
  );
});

test("plain text and real links are never reported as dead clicks", async () => {
  const { deliveries, documentElement } = harness();
  const tracker = start();
  const paragraph = el("p", { id: "copy" });
  const link = el("a", { id: "go", href: "https://service.internal/next" });
  documentElement.append(paragraph, link);
  dispatch("click", { target: paragraph });
  await wait(1400);
  dispatch("click", { target: link });
  await wait(1400);
  const dead = (await events(tracker, deliveries)).filter(
    (event) => event.name === "dead_click",
  );
  assert.deepEqual(dead, []);
});

test("an error right after a click is attributed to that click", async () => {
  const { deliveries } = harness();
  const tracker = start();
  const button = el("button", { id: "submit-approval" });
  harnessAttach(button);
  dispatch("click", { target: button });
  dispatch("error", {
    message: "approval failed",
    filename: "app.js",
    lineno: 4,
  });
  const found = await events(tracker, deliveries);
  const linked = found.find((event) => event.name === "error_after_click");
  assert.ok(linked, "the error is linked to the click that preceded it");
  assert.equal(linked.properties.element_id, "submit-approval");
  assert.equal(linked.properties.error_kind, "error");
  assert.ok(linked.properties.since_click_ms <= 2000);
});

test("submitting the same form twice reports a retry, the first submit does not", async () => {
  const { deliveries } = harness();
  const tracker = start();
  const form = el("form", { id: "leave-request" });
  dispatch("submit", { target: form });
  dispatch("submit", { target: form });
  const retries = (await events(tracker, deliveries)).filter(
    (event) => event.name === "form_retry",
  );
  assert.equal(retries.length, 1);
  assert.equal(retries[0].properties.attempt, 2);
  assert.equal(retries[0].properties.reason, "resubmit");
});

test("a failed field validation reports one retry however many fields fail", async () => {
  const { deliveries } = harness();
  const tracker = start();
  const form = el("form", { id: "leave-request" });
  const first = el("input", { name: "start_date" });
  const second = el("input", { name: "reason" });
  form.append(first, second);
  dispatch("invalid", { target: first });
  dispatch("invalid", { target: second });
  const retries = (await events(tracker, deliveries)).filter(
    (event) => event.name === "form_retry",
  );
  assert.equal(retries.length, 1, "one submit attempt is one retry signal");
  assert.equal(retries[0].properties.reason, "validation");
  assert.equal(retries[0].properties.field, "start_date");
});

test("leaving a page immediately after arriving reports a rapid back", async () => {
  const { deliveries } = harness();
  const tracker = start();
  history.pushState({}, "", "https://service.internal/page/b");
  dispatch("popstate", {});
  const found = (await events(tracker, deliveries)).filter(
    (event) => event.name === "rapid_back",
  );
  assert.equal(found.length, 1);
  assert.equal(found[0].properties.from_path, "/page/b");
  assert.ok(found[0].properties.dwell_ms <= 3000);
});

test("a search is detected from the query string without the term by default", async () => {
  const { deliveries } = harness(
    "https://service.internal/search?q=%EC%97%B0%EC%B0%A8%20%EC%8B%A0%EC%B2%AD",
  );
  const tracker = start();
  await wait(1800);
  const search = (await events(tracker, deliveries)).find(
    (event) => event.name === "search",
  );
  assert.ok(search, "the results page reports a search");
  assert.equal(search.properties.source, "url");
  assert.equal(search.properties.param, "q");
  assert.equal(search.properties.query_words, 2);
  assert.equal(search.properties.query_length, 5);
  assert.equal(
    search.properties.query,
    undefined,
    "the term is not collected unless the site opts in",
  );
});

test("an opted-in search term is collected, normalised and stripped of identifiers", async () => {
  const { deliveries } = harness(
    "https://service.internal/search?query=A%20%20B",
  );
  const tracker = start({ collectSearchTerms: true });
  await wait(1800);
  const searches = (await events(tracker, deliveries)).filter(
    (event) => event.name === "search",
  );
  assert.equal(searches[0].properties.query, "a b");
  tracker.trackSearch("who is hong@corp.example", 3);
  const collected = (await events(tracker, deliveries)).filter(
    (event) => event.name === "search",
  );
  const manual = collected[collected.length - 1];
  assert.equal(manual.properties.query, "who is [REDACTED_EMAIL]");
  assert.equal(manual.properties.result_count, 3);
  assert.equal(manual.properties.source, "manual");
});

test("the result count is read from the page so zero-result searches are visible", async () => {
  const { deliveries, documentElement } = harness(
    "https://service.internal/search?q=nothing",
  );
  const results = el("div", {
    attributes: { "data-momento-search-results": "0" },
  });
  documentElement.append(results);
  const tracker = start();
  await wait(400);
  const search = (await events(tracker, deliveries)).find(
    (event) => event.name === "search",
  );
  assert.equal(search.properties.result_count, 0);
});

test("repeating a search is separated from refining one", async () => {
  const { deliveries } = harness();
  const tracker = start({ collectSearchTerms: true });
  tracker.trackSearch("연차");
  await wait(2100);
  tracker.trackSearch("연차");
  tracker.trackSearch("연차 신청");
  tracker.trackSearch("완전히 다른 말");
  const found = await events(tracker, deliveries);
  const repeated = found.filter((event) => event.name === "repeated_search");
  const refined = found.filter((event) => event.name === "search_refine");
  assert.equal(
    repeated.length,
    1,
    "the same words again means the results failed",
  );
  assert.equal(refined.length, 1, "narrowing the words is a different problem");
  assert.equal(refined[0].properties.direction, "narrowed");
  assert.equal(refined[0].properties.previous_query, "연차");
  assert.equal(
    found.filter((event) => event.name === "search").length,
    4,
    "every search is still counted",
  );
});

test("opening a result reports its position in the list", async () => {
  const { deliveries, documentElement } = harness(
    "https://service.internal/search?q=vacation",
  );
  const results = el("div", {
    attributes: { "data-momento-search-results": "2" },
  });
  const first = el("a", { href: "https://service.internal/doc/1" });
  const second = el("a", { href: "https://service.internal/doc/2" });
  results.append(first, second);
  documentElement.append(results);
  const tracker = start();
  await wait(400);
  dispatch("click", { target: second });
  const click = (await events(tracker, deliveries)).find(
    (event) => event.name === "search_click",
  );
  assert.ok(click);
  assert.equal(click.properties.position, 2);
  assert.equal(click.properties.url, "https://service.internal/doc/2");
});

test("signals stay switched off when the site disables them", async () => {
  const { deliveries } = harness("https://service.internal/search?q=vacation");
  const tracker = start({ frustrationSignals: false, searchTracking: false });
  const button = el("button", { id: "save" });
  harnessAttach(button);
  for (let i = 0; i < 4; i += 1) dispatch("click", { target: button });
  await wait(1800);
  const names = new Set(
    (await events(tracker, deliveries)).map((event) => event.name),
  );
  assert.equal(names.has("rage_click"), false);
  assert.equal(names.has("search"), false);
  assert.equal(names.has("click"), true, "ordinary tracking is unaffected");
});

function harnessAttach(node) {
  document.documentElement.append(node);
}

// The tracker compares its in-page windows against a clock. Date.now() is not a
// clock that only moves forward: an NTP correction, a laptop resuming, or someone
// setting the system time moves it, and this was observed here — a wait of 2099ms
// by the monotonic clock read as 120ms of wall time, which put two searches two
// seconds apart inside the 2000ms window that suppresses a repeated keystroke and
// dropped the repeated_search signal.
//
// So the windows are measured with performance.now(). This forces the jump that
// was seen by accident.
test("in-page windows survive the system clock moving backwards", async () => {
  const { deliveries } = harness();
  const tracker = start({ collectSearchTerms: true });
  const realNow = Date.now;
  let skew = 0;
  Date.now = () => realNow() + skew;
  try {
    tracker.trackSearch("연차");
    await wait(2100);
    // The system clock jumps back two seconds, so by wall time the two searches
    // are 100ms apart and the repeat looks like a keystroke.
    skew = -2000;
    tracker.trackSearch("연차");
  } finally {
    Date.now = realNow;
  }
  const found = await events(tracker, deliveries);
  const repeated = found.filter((event) => event.name === "repeated_search");
  assert.equal(
    repeated.length,
    1,
    "a backward jump in the system clock swallowed the repeated search",
  );
  assert.equal(
    found.filter((event) => event.name === "search").length,
    2,
    "the second search was dropped as a duplicate keystroke",
  );
});

test("한 페이지가 보고하는 신호에는 상한이 있고, 그 상한은 페이지마다 다시 열린다", async () => {
  // 신호 보고에 상한을 둔 이유는 코드에 적혀 있습니다: "렌더 루프에 빠진
  // 페이지가 수천 개의 이벤트가 되어서는 안 된다." 그런데 그 상한도, 그것이
  // 라우트 변경마다 다시 열린다는 것도 검사된 적이 없었습니다.
  //
  // 둘 다 필요합니다. 상한이 없으면 망가진 페이지 하나가 그 사람의 하루치
  // 데이터를 덮어씁니다. 다시 열리지 않으면 상한은 페이지당이 아니라 세션당이
  // 되고, 긴 단일 페이지 세션은 스무 번째 신호 이후 남은 방문 내내 조용해집니다
  // — 그리고 그 침묵은 "막힘이 없었다"로 읽힙니다.
  const { deliveries } = harness();
  const tracker = start();

  // 서로 다른 요소를 계속 눌러 rage click을 상한보다 많이 만듭니다.
  const rage = async (id) => {
    const button = el("button", { id });
    harnessAttach(button);
    for (let i = 0; i < 4; i += 1) dispatch("click", { target: button });
    await wait(1300);
  };
  for (let i = 0; i < 25; i += 1) await rage(`btn-${i}`);

  // 상한은 신호 종류별이 아니라 페이지 전체에 걸립니다. 클릭 한 번이 rage와
  // dead 양쪽으로 보고될 수 있으므로, 세는 것은 신호의 총수입니다.
  const signalNames = [
    "rage_click",
    "dead_click",
    "rapid_back",
    "form_retry",
    "slow_interaction",
    "error_after_click",
    "repeated_search",
  ];
  const first = (await events(tracker, deliveries)).filter((event) =>
    signalNames.includes(event.name),
  );
  assert.equal(
    first.length,
    20,
    `한 페이지에서 신호 ${first.length}개가 나갔습니다: 상한이 상한이 아닙니다`,
  );

  // 라우트가 바뀌면 다음 페이지는 다시 보고할 수 있어야 합니다.
  deliveries.length = 0;
  history.pushState({}, "", "https://service.internal/page/capped");
  await wait(50);
  await rage("btn-after-route");

  const second = (await events(tracker, deliveries)).filter((event) =>
    signalNames.includes(event.name),
  );
  assert.ok(
    second.length > 0,
    "라우트가 바뀐 뒤에도 신호가 나가지 않습니다: 상한이 페이지당이 아니라 세션당이 되어, 긴 방문의 나머지가 통째로 조용해집니다",
  );
});
