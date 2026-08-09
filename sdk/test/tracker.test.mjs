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
  tracker.setSessionProperties({ login_status: "authenticated", workflow: "approval" });
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
    JSON.stringify({ id: "legacy-session", last: Date.now(), traffic: { source: "legacy" } }),
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
  assert.equal(deliveries[0].events[0].properties.release_version, "event-supplied");
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
