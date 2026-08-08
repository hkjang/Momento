type TrackingMode = "consent-required" | "cookie-consent" | "cookieless" | "full" | "disabled";
type Properties = Record<string, unknown>;

export interface MomentoOptions {
  siteId: string;
  endpoint: string;
  mode?: TrackingMode;
  sessionTimeoutMinutes?: number;
  debug?: boolean;
  autoTrack?: boolean;
  heartbeatSeconds?: number;
  batchSize?: number;
}

interface QueuedEvent {
  id: string;
  name: string;
  timestamp: number;
  properties: Properties;
  debug?: boolean;
}

const VISITOR_KEY = "momento_visitor_id";
const SESSION_KEY = "momento_session";
const CONSENT_KEY = "momento_consent";
const OFFLINE_KEY = "momento_offline_queue";

function id(): string {
  if (typeof crypto !== "undefined" && crypto.randomUUID) return crypto.randomUUID();
  return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, c => {
    const r = Math.random() * 16 | 0;
    return (c === "x" ? r : (r & 3 | 8)).toString(16);
  });
}

function storageAvailable(): boolean {
  try { localStorage.setItem("__momento_test", "1"); localStorage.removeItem("__momento_test"); return true; }
  catch { return false; }
}

function parseUA() {
  const ua = navigator.userAgent;
  const browser = /Edg\//.test(ua) ? "Edge" : /Chrome\//.test(ua) ? "Chrome" : /Firefox\//.test(ua) ? "Firefox" : /Safari\//.test(ua) ? "Safari" : "Other";
  const os = /Windows/.test(ua) ? "Windows" : /Android/.test(ua) ? "Android" : /iPhone|iPad/.test(ua) ? "iOS" : /Mac OS/.test(ua) ? "macOS" : /Linux/.test(ua) ? "Linux" : "Other";
  const type = /Mobi|Android|iPhone/.test(ua) ? "mobile" : /iPad|Tablet/.test(ua) ? "tablet" : "desktop";
  return { browser, os, type, language: navigator.language, screen: `${screen.width}x${screen.height}` };
}

class MomentoTracker {
  private options: MomentoOptions | null = null;
  private visitorId = "";
  private sessionId = "";
  private lastEventAt = 0;
  private userId = "";
  private userProperties: Properties = {};
  private queue: QueuedEvent[] = [];
  private timer?: number;
  private debugEnabled = false;
  private initialized = false;
  private trackedScroll = new Set<number>();

  init(options: MomentoOptions) {
    if (!options.siteId || !options.endpoint) throw new Error("Momento: siteId and endpoint are required");
    this.options = { mode: "full", sessionTimeoutMinutes: 30, autoTrack: true, heartbeatSeconds: 15, batchSize: 10, ...options, endpoint: options.endpoint.replace(/\/$/, "") };
    this.debugEnabled = !!this.options.debug;
    this.loadIdentity();
    this.restoreOffline();
    if (this.options.autoTrack && !this.initialized) this.installAutoTracking();
    this.initialized = true;
    if (this.options.autoTrack) this.track("page_view");
    this.log("initialized", { siteId: options.siteId, mode: this.options.mode });
    return this;
  }

  identify(userId: string, properties: Properties = {}) {
    if (!userId || /@|\+?\d{8,}/.test(userId)) { console.error("[Momento] userId must be a non-PII internal or pseudonymous identifier"); return; }
    this.userId = userId;
    this.userProperties = { ...this.userProperties, ...properties };
  }

  track(name: string, properties: Properties = {}) {
    if (!this.canTrack()) return;
    this.ensureSession();
    this.queue.push({ id: id(), name, timestamp: Date.now(), properties, debug: this.debugEnabled });
    this.lastEventAt = Date.now();
    this.persistSession();
    this.log("track", name, properties);
    if (this.queue.length >= (this.options?.batchSize || 10)) void this.flush();
    else this.scheduleFlush();
  }

  async flush(useBeacon = false) {
    if (!this.options || !this.queue.length || !this.canTrack()) return;
    const events = this.queue.splice(0, this.options.batchSize || 10);
    const payload = JSON.stringify({
      site_id: this.options.siteId,
      visitor_id: this.visitorId,
      session_id: this.sessionId,
      user_id: this.userId || undefined,
      user_properties: this.userProperties,
      context: this.context(),
      events
    });
    try {
      if (useBeacon && navigator.sendBeacon) {
        const ok = navigator.sendBeacon(`${this.options.endpoint}/collect/v1/events`, new Blob([payload], { type: "text/plain;charset=UTF-8" }));
        if (!ok) throw new Error("sendBeacon rejected payload");
      } else {
        const response = await fetch(`${this.options.endpoint}/collect/v1/events`, { method: "POST", headers: { "Content-Type": "application/json" }, body: payload, keepalive: true });
        if (!response.ok) throw new Error(`collector returned ${response.status}`);
      }
      if (this.queue.length) this.scheduleFlush();
    } catch (error) {
      this.queue.unshift(...events);
      this.saveOffline();
      this.log("delivery failed; queued offline", error);
    }
  }

  consent = {
    grant: () => { if (storageAvailable()) localStorage.setItem(CONSENT_KEY, "granted"); this.loadIdentity(); },
    deny: () => { if (storageAvailable()) localStorage.setItem(CONSENT_KEY, "denied"); this.queue = []; },
    revoke: () => { if (storageAvailable()) { localStorage.removeItem(CONSENT_KEY); localStorage.removeItem(VISITOR_KEY); localStorage.removeItem(SESSION_KEY); } this.queue = []; this.visitorId = id(); }
  };

  cookie = {
    enable: () => { if (this.options) { this.options.mode = "full"; this.loadIdentity(); } },
    disable: () => { if (this.options) { this.options.mode = "cookieless"; if (storageAvailable()) { localStorage.removeItem(VISITOR_KEY); localStorage.removeItem(SESSION_KEY); } } }
  };

  debug(enabled = true) { this.debugEnabled = enabled; }

  private canTrack() {
    if (!this.options || this.options.mode === "disabled") return false;
    if (navigator.doNotTrack === "1" && this.options.mode !== "full") return false;
    if (this.options.mode === "consent-required" && storageAvailable() && localStorage.getItem(CONSENT_KEY) !== "granted") return false;
    return !(storageAvailable() && localStorage.getItem(CONSENT_KEY) === "denied");
  }

  private isPersistent() { return this.options?.mode === "full" || (this.options?.mode === "consent-required" && localStorage.getItem(CONSENT_KEY) === "granted"); }
  private loadIdentity() {
    const persistent = storageAvailable() && this.isPersistent();
    this.visitorId = persistent ? localStorage.getItem(VISITOR_KEY) || id() : id();
    if (persistent) localStorage.setItem(VISITOR_KEY, this.visitorId);
    if (persistent) {
      try { const s = JSON.parse(localStorage.getItem(SESSION_KEY) || "{}"); this.sessionId = s.id || id(); this.lastEventAt = s.last || 0; }
      catch { this.sessionId = id(); }
    } else this.sessionId = id();
    this.ensureSession();
  }

  private ensureSession() {
    const timeout = (this.options?.sessionTimeoutMinutes || 30) * 60_000;
    if (!this.sessionId || Date.now() - this.lastEventAt > timeout) { this.sessionId = id(); this.lastEventAt = Date.now(); if (this.canTrack()) this.queue.push({ id: id(), name: "session_start", timestamp: Date.now(), properties: {}, debug: this.debugEnabled }); }
  }
  private persistSession() { if (storageAvailable() && this.isPersistent()) localStorage.setItem(SESSION_KEY, JSON.stringify({ id: this.sessionId, last: this.lastEventAt })); }
  private context() {
    const params = new URLSearchParams(location.search);
    return { page: { url: location.href, title: document.title, referrer: document.referrer }, device: parseUA(), traffic: { source: params.get("utm_source") || undefined, medium: params.get("utm_medium") || undefined, campaign: params.get("utm_campaign") || undefined, term: params.get("utm_term") || undefined, content: params.get("utm_content") || undefined } };
  }
  private scheduleFlush() { if (this.timer) return; this.timer = window.setTimeout(() => { this.timer = undefined; void this.flush(); }, 1000); }
  private saveOffline() { if (!storageAvailable()) return; try { localStorage.setItem(OFFLINE_KEY, JSON.stringify(this.queue.slice(-200))); } catch { /* quota */ } }
  private restoreOffline() { if (!storageAvailable()) return; try { const saved = JSON.parse(localStorage.getItem(OFFLINE_KEY) || "[]"); if (Array.isArray(saved)) this.queue.push(...saved.slice(-200)); localStorage.removeItem(OFFLINE_KEY); } catch { /* corrupt queue */ } }
  private log(...args: unknown[]) { if (this.debugEnabled) console.debug("[Momento]", ...args); }

  private installAutoTracking() {
    const route = () => { this.trackedScroll.clear(); window.setTimeout(() => this.track("page_view"), 0); };
    for (const method of ["pushState", "replaceState"] as const) { const original = history[method]; history[method] = function (this: History, ...args: Parameters<History[typeof method]>) { const value = original.apply(this, args); route(); return value; } as History[typeof method]; }
    addEventListener("popstate", route);
    addEventListener("click", event => {
      const target = (event.target as Element | null)?.closest("a,button,[role=button]") as HTMLElement | null;
      if (!target) return;
      const anchor = target.closest("a") as HTMLAnchorElement | null;
      const props: Properties = { element_tag: target.tagName.toLowerCase(), element_id: target.id || undefined, element_text: target.innerText?.trim().slice(0, 100) || undefined, button: target.dataset.analyticsButton || target.getAttribute("aria-label") || undefined, feature: target.dataset.analyticsFeature || undefined };
      if (anchor?.href && anchor.origin !== location.origin) { props.url = anchor.href; this.track("outbound_click", props); }
      else if (anchor?.download || /\.(pdf|zip|docx?|xlsx?|pptx?|csv)$/i.test(anchor?.pathname || "")) { props.url = anchor?.href; this.track("file_download", props); }
      else this.track("click", props);
    }, { capture: true });
    addEventListener("scroll", () => { const max = document.documentElement.scrollHeight - innerHeight; if (max <= 0) return; const depth = Math.round(scrollY / max * 100); for (const threshold of [25, 50, 75, 90]) if (depth >= threshold && !this.trackedScroll.has(threshold)) { this.trackedScroll.add(threshold); this.track("scroll", { percent: threshold }); } }, { passive: true });
    addEventListener("focusin", event => { const form = (event.target as Element | null)?.closest("form"); if (form && !form.dataset.momentoStarted) { form.dataset.momentoStarted = "1"; this.track("form_start", { form_id: form.id || form.getAttribute("name") }); } });
    addEventListener("submit", event => { const form = event.target as HTMLFormElement; this.track("form_submit", { form_id: form.id || form.name }); });
    addEventListener("error", event => this.track("error", { message: event.message, filename: event.filename, line: event.lineno, column: event.colno }));
    addEventListener("unhandledrejection", event => this.track("error", { message: String(event.reason), type: "unhandledrejection" }));
    addEventListener("visibilitychange", () => { if (document.visibilityState === "hidden") void this.flush(true); });
    addEventListener("online", () => void this.flush());
    window.setInterval(() => { if (document.visibilityState === "visible") this.track("user_engagement", { active_seconds: this.options?.heartbeatSeconds }); }, (this.options?.heartbeatSeconds || 15) * 1000);
  }
}

const tracker = new MomentoTracker();
declare global { interface Window { analytics: MomentoTracker; } }
window.analytics = tracker;

const script = document.currentScript as HTMLScriptElement | null;
if (script?.dataset.siteId) {
  const endpoint = script.src ? new URL(script.src).origin : location.origin;
  tracker.init({ siteId: script.dataset.siteId, endpoint, mode: (script.dataset.mode as TrackingMode) || "full", debug: script.dataset.debug === "true" });
}
