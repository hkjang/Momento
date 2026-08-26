import assert from "node:assert/strict";
import test from "node:test";

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

setGlobal("window", globalThis);
setGlobal(
  "location",
  new URL(
    "https://service.internal/page/a?utm_source=intranet&utm_medium=link",
  ),
);
setGlobal("navigator", {
  userAgent: "Mozilla/5.0 Chrome/140.0",
  language: "ko-KR",
  doNotTrack: "0",
  sendBeacon: () => true,
});
setGlobal("screen", { width: 1920, height: 1080 });
setGlobal("document", {
  title: "Page A",
  referrer: "https://portal.internal/home?employee=secret",
  currentScript: null,
  visibilityState: "visible",
  documentElement: { scrollHeight: 1000 },
});
setGlobal("history", {
  pushState() {},
  replaceState() {},
});
setGlobal("addEventListener", () => {});

const { MomentoTracker } = await import("../src/index.ts");

test("captures page and acquisition context when each event occurs", async () => {
  setGlobal("localStorage", new MemoryStorage());
  setGlobal(
    "location",
    new URL(
      "https://service.internal/page/a?utm_source=intranet&utm_medium=link",
    ),
  );
  document.title = "Page A";
  const deliveries = [];
  setGlobal("fetch", async (_url, init) => {
    deliveries.push(JSON.parse(init.body));
    return { ok: true, status: 202 };
  });

  const tracker = new MomentoTracker();
  tracker.init({
    siteId: "SITE_TEST",
    endpoint: "https://analytics.internal",
    mode: "cookieless",
    autoTrack: false,
    environment: "stg",
    contractVersion: 3,
    releaseVersion: "v2.4.1",
    gitSha: "abc123",
    sessionProperties: { login_status: "anonymous" },
  });
  tracker.setSessionProperties({
    login_status: "authenticated",
    workflow: "approval",
  });
  tracker.track("click", { button: "from-a" });

  setGlobal(
    "location",
    new URL("https://service.internal/page/b?utm_source=changed"),
  );
  document.title = "Page B";
  tracker.track("click", { button: "from-b" });
  await tracker.flush();

  assert.equal(deliveries.length, 1);
  const clicks = deliveries[0].events.filter((event) => event.name === "click");
  assert.equal(clicks[0].context.page.url, "https://service.internal/page/a");
  assert.equal(clicks[0].context.page.title, "Page A");
  assert.equal(clicks[1].context.page.url, "https://service.internal/page/b");
  assert.equal(clicks[1].context.page.title, "Page B");
  assert.equal(clicks[0].context.traffic.source, "intranet");
  assert.equal(clicks[1].context.traffic.source, "intranet");
  assert.equal(clicks[0].context.page.referrer, "https://portal.internal/home");
  assert.equal(deliveries[0].environment, "stg");
  assert.equal(clicks[0].contract_version, 3);
  assert.equal(clicks[0].properties.release_version, "v2.4.1");
  assert.equal(clicks[0].properties.git_sha, "abc123");
  assert.deepEqual(deliveries[0].session_properties, {
    login_status: "authenticated",
    workflow: "approval",
  });
  tracker.consent.deny();
});

test("migrates v0.4 identity into the scoped production storage", async () => {
  const storage = new MemoryStorage();
  storage.setItem("momento_visitor_id", "legacy-visitor");
  storage.setItem(
    "momento_session",
    JSON.stringify({
      id: "legacy-session",
      last: Date.now(),
      traffic: { source: "legacy" },
    }),
  );
  setGlobal("localStorage", storage);
  const deliveries = [];
  setGlobal("fetch", async (_url, init) => {
    deliveries.push(JSON.parse(init.body));
    return { ok: true, status: 202 };
  });

  const tracker = new MomentoTracker();
  tracker.init({
    siteId: "SITE_UPGRADE",
    endpoint: "https://analytics.internal",
    mode: "full",
    autoTrack: false,
  });
  tracker.track("click", { release_version: "event-supplied" });
  await tracker.flush();

  assert.equal(deliveries[0].visitor_id, "legacy-visitor");
  assert.equal(deliveries[0].session_id, "legacy-session");
  assert.equal(
    deliveries[0].events[0].properties.release_version,
    "event-supplied",
  );
  assert.equal(
    storage.getItem("momento_visitor_id:SITE_UPGRADE:prd"),
    "legacy-visitor",
  );
  tracker.consent.deny();
});

test("consent-required remains blocked when localStorage is unavailable", async () => {
  setGlobal(
    "location",
    new URL(
      "https://service.internal/landing?utm_source=intranet&utm_medium=banner",
    ),
  );
  const unavailableStorage = {
    getItem() {
      throw new Error("blocked");
    },
    setItem() {
      throw new Error("blocked");
    },
    removeItem() {
      throw new Error("blocked");
    },
  };
  setGlobal("localStorage", unavailableStorage);
  const deliveries = [];
  setGlobal("fetch", async (_url, init) => {
    deliveries.push(JSON.parse(init.body));
    return { ok: true, status: 202 };
  });

  const tracker = new MomentoTracker();
  tracker.init({
    siteId: "SITE_TEST",
    endpoint: "https://analytics.internal",
    mode: "consent-required",
    autoTrack: false,
  });
  tracker.track("click", { button: "blocked" });
  await tracker.flush();
  assert.equal(deliveries.length, 0);

  setGlobal(
    "location",
    new URL("https://service.internal/page/after-consent?utm_source=changed"),
  );
  tracker.consent.grant();
  tracker.track("click", { button: "allowed-in-memory" });
  await tracker.flush();
  assert.equal(deliveries.length, 1);
  assert.deepEqual(
    deliveries[0].events.map((event) => event.name),
    ["session_start", "click"],
  );
  assert.equal(
    deliveries[0].events[1].context.traffic.source,
    "intranet",
    "consent delay must not replace the first-touch acquisition",
  );
  tracker.consent.deny();
});

test("data-endpoint keeps the collector first party for a strict CSP", async () => {
  const { resolveEndpoint } = await import("../src/index.ts");

  assert.equal(
    resolveEndpoint(
      undefined,
      "https://momento.internal/tracker.js",
      "https://service.internal",
    ),
    "https://momento.internal",
    "the script origin stays the default collector",
  );
  assert.equal(
    resolveEndpoint(
      "https://momento.internal/",
      "",
      "https://service.internal",
    ),
    "https://momento.internal",
  );
  assert.equal(
    resolveEndpoint(
      "/momento/",
      "https://momento.internal/tracker.js",
      "https://service.internal",
    ),
    "/momento",
    "a path proxies the collector on the tracked origin",
  );
  assert.equal(
    resolveEndpoint("momento", "", "https://service.internal"),
    "/momento",
  );
  assert.equal(
    resolveEndpoint("/", "", "https://service.internal"),
    "https://service.internal",
  );
});

test("a proxied endpoint posts to the same origin", async () => {
  setGlobal("localStorage", new MemoryStorage());
  const requests = [];
  setGlobal("fetch", async (url, init) => {
    requests.push({ url, body: JSON.parse(init.body) });
    return { ok: true, status: 202 };
  });

  const tracker = new MomentoTracker();
  tracker.init({
    siteId: "SITE_PROXY",
    endpoint: "/momento",
    autoTrack: false,
  });
  tracker.track("click", { button: "proxied" });
  await tracker.flush();

  assert.equal(requests.length, 1);
  assert.equal(requests[0].url, "/momento/collect/v1/events");
  assert.equal(requests[0].body.site_id, "SITE_PROXY");
});

// The offline queue survives page loads, and a queue entry carried no session of
// its own: the collector reads the session from the payload and applies it to
// every event in it. So a batch queued while the network was down went out under
// whatever session was current when it finally reconnected. Measured on the
// server, a batch of yesterday's events delivered under today's session produced
// a session 26 hours long, took its landing page from the previous visit, and
// raised the average session duration on the overview.
test("delivers queued events under the session they happened in", async () => {
  const storage = new MemoryStorage();
  setGlobal("localStorage", storage);
  setGlobal("location", new URL("https://service.internal/yesterday"));

  // Day one: every delivery fails, so the events land in the offline queue.
  setGlobal("fetch", async () => {
    throw new Error("network unreachable");
  });
  const yesterday = new MomentoTracker();
  yesterday.init({
    siteId: "SITE_OFFLINE",
    endpoint: "https://analytics.internal",
    autoTrack: false,
    sessionTimeoutMinutes: 30,
  });
  yesterday.track("page_view", {});
  yesterday.track("purchase", { value: 1000 });
  await yesterday.flush();

  const keys = [...storage.values.keys()];
  const offlineKey = keys.find((key) => key.includes("offline_queue"));
  const sessionKey = keys.find((key) => key.includes("momento_session"));
  const stored = JSON.parse(storage.getItem(offlineKey) || "[]");
  assert.ok(stored.length >= 2, "the failed delivery was not queued offline");
  const firstSession = JSON.parse(storage.getItem(sessionKey)).id;
  for (const event of stored) {
    assert.equal(
      event.session_id,
      firstSession,
      `queued ${event.name} does not record the session it happened in`,
    );
  }

  // Day one's tracker still holds a pending flush timer. Denying consent makes
  // any timer that fires a no-op, so it cannot deliver into day two's recording
  // and make this test depend on which fired first.
  yesterday.consent.deny();

  // Day two loads the browser fresh: its own storage, holding what day one left —
  // the same visitor, a session long past its timeout, and yesterday's queue.
  const nextDay = new MemoryStorage();
  nextDay.setItem(
    keys.find((key) => key.includes("visitor_id")),
    storage.getItem(keys.find((key) => key.includes("visitor_id"))),
  );
  nextDay.setItem(
    sessionKey,
    JSON.stringify({
      ...JSON.parse(storage.getItem(sessionKey)),
      last: Date.now() - 26 * 60 * 60 * 1000,
    }),
  );
  nextDay.setItem(
    offlineKey,
    JSON.stringify(
      stored.map((event) => ({
        ...event,
        timestamp: event.timestamp - 26 * 60 * 60 * 1000,
      })),
    ),
  );
  setGlobal("localStorage", nextDay);
  const deliveries = [];
  setGlobal("fetch", async (_url, init) => {
    deliveries.push(JSON.parse(init.body));
    return { ok: true, status: 202 };
  });
  setGlobal("location", new URL("https://service.internal/today"));
  const today = new MomentoTracker();
  today.init({
    siteId: "SITE_OFFLINE",
    endpoint: "https://analytics.internal",
    autoTrack: false,
    sessionTimeoutMinutes: 30,
  });
  today.track("page_view", {});
  await today.flush();

  assert.ok(
    deliveries.length >= 2,
    "yesterday's events were sent in the same payload as today's",
  );
  const cutoff = Date.now() - 60 * 60 * 1000;
  for (const delivery of deliveries) {
    const old = delivery.events.filter((event) => event.timestamp < cutoff);
    const fresh = delivery.events.filter((event) => event.timestamp >= cutoff);
    assert.ok(
      !(old.length && fresh.length),
      "one payload mixes yesterday's events with today's, so they share a session id",
    );
    assert.equal(
      delivery.session_id,
      old.length ? firstSession : delivery.session_id,
      "yesterday's events were delivered under a different session than the one they happened in",
    );
    // The session belongs on the payload, not repeated on every event.
    for (const event of delivery.events) {
      assert.equal(
        event.session_id,
        undefined,
        "session_id leaked into the wire format",
      );
      assert.equal(
        event.visitor_id,
        undefined,
        "visitor_id leaked into the wire format",
      );
    }
  }
  const oldPayload = deliveries.find((d) =>
    d.events.some((e) => e.timestamp < cutoff),
  );
  assert.ok(oldPayload, "yesterday's events were never delivered");
  assert.equal(
    oldPayload.session_id,
    firstSession,
    "yesterday's events were attributed to today's session",
  );
  const newPayload = deliveries.find((d) =>
    d.events.every((e) => e.timestamp >= cutoff),
  );
  assert.ok(newPayload, "today's event was never delivered");
  assert.notEqual(
    newPayload.session_id,
    firstSession,
    "today's events were attributed to yesterday's session",
  );
});

// A page hidden while the browser is offline still went out as a beacon.
// sendBeacon reports whether the browser accepted the payload, never whether it
// arrived, so the tracker dropped the batch from its queue and the events were
// gone — and the batch at page exit is the one holding the last page view, the
// exit page and a completed purchase.
test("keeps the exit batch when the browser is offline", async () => {
  const storage = new MemoryStorage();
  setGlobal("localStorage", storage);
  const accepted = [];
  setGlobal("navigator", {
    ...navigator,
    onLine: false,
    sendBeacon: (_url, blob) => {
      accepted.push(blob);
      return true;
    },
  });
  setGlobal("fetch", async () => {
    throw new Error("fetch must not be used for an exit flush");
  });
  const leaving = new MomentoTracker();
  leaving.init({
    siteId: "SITE_EXIT",
    endpoint: "https://analytics.internal",
    autoTrack: false,
  });
  leaving.track("page_view", {});
  leaving.track("purchase", { value: 900 });
  await leaving.flush(true);

  assert.equal(
    accepted.length,
    0,
    "the payload was handed to a beacon that could not deliver it",
  );
  const offlineKey = [...storage.values.keys()].find((key) =>
    key.includes("offline_queue"),
  );
  const held = JSON.parse(storage.getItem(offlineKey) || "[]");
  assert.ok(
    held.some((event) => event.name === "purchase"),
    "the exit batch was not kept for the next page load",
  );

  // Back online on the next page load, the batch goes out.
  setGlobal("navigator", { ...navigator, onLine: true });
  const deliveries = [];
  setGlobal("fetch", async (_url, init) => {
    deliveries.push(JSON.parse(init.body));
    return { ok: true, status: 202 };
  });
  const returning = new MomentoTracker();
  returning.init({
    siteId: "SITE_EXIT",
    endpoint: "https://analytics.internal",
    autoTrack: false,
  });
  await returning.flush();
  const names = deliveries.flatMap((delivery) =>
    delivery.events.map((event) => event.name),
  );
  assert.ok(
    names.includes("purchase"),
    "the events kept at exit were never delivered",
  );
});

// The queue keeps 200 events across a page load and drops the oldest beyond that.
// It did so silently: 260 tracked became 200 persisted with nothing recording the
// 60, so the numbers were simply lower than they should be and nothing said by how
// much. A gap an operator can measure is worth more than one they cannot see.
test("reports events the queue could not keep", async () => {
  const storage = new MemoryStorage();
  setGlobal("localStorage", storage);
  setGlobal("navigator", { ...navigator, onLine: true });
  setGlobal("fetch", async () => {
    throw new Error("offline");
  });
  const overflowing = new MomentoTracker();
  overflowing.init({
    siteId: "SITE_CAP",
    endpoint: "https://analytics.internal",
    autoTrack: false,
    batchSize: 1000,
  });
  for (let index = 0; index < 260; index += 1)
    overflowing.track("page_view", { index });
  await overflowing.flush();

  const offlineKey = [...storage.values.keys()].find((key) =>
    key.includes("offline_queue"),
  );
  const kept = JSON.parse(storage.getItem(offlineKey));
  assert.equal(kept.length, 200, "the cap is not what it claims to be");

  const deliveries = [];
  setGlobal("fetch", async (_url, init) => {
    deliveries.push(JSON.parse(init.body));
    return { ok: true, status: 202 };
  });
  const next = new MomentoTracker();
  next.init({
    siteId: "SITE_CAP",
    endpoint: "https://analytics.internal",
    autoTrack: false,
    batchSize: 1000,
  });
  await next.flush();
  const reports = deliveries
    .flatMap((delivery) => delivery.events)
    .filter((event) => event.name === "collection_dropped");
  assert.equal(reports.length, 1, "the dropped events were never reported");
  assert.ok(
    reports[0].properties.events_dropped >= 60,
    `reported ${reports[0].properties.events_dropped} dropped, expected at least the 60 the cap discarded`,
  );

  // Reported once, not on every page load afterwards.
  const after = [];
  setGlobal("fetch", async (_url, init) => {
    after.push(JSON.parse(init.body));
    return { ok: true, status: 202 };
  });
  const third = new MomentoTracker();
  third.init({
    siteId: "SITE_CAP",
    endpoint: "https://analytics.internal",
    autoTrack: false,
  });
  await third.flush();
  assert.equal(
    after
      .flatMap((delivery) => delivery.events)
      .filter((event) => event.name === "collection_dropped").length,
    0,
    "the same loss is reported again on every page load",
  );
});
