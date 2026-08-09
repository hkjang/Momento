export type TrackingMode =
  "consent-required" | "cookie-consent" | "cookieless" | "full" | "disabled";
type ConsentState = "unknown" | "granted" | "denied";
type Properties = Record<string, unknown>;

interface TrafficContext {
  source?: string;
  medium?: string;
  campaign?: string;
  term?: string;
  content?: string;
}

interface EventContext {
  page: { url: string; title: string; referrer: string };
  device: ReturnType<typeof parseUA>;
  traffic: TrafficContext;
}

export interface MomentoOptions {
  siteId: string;
  endpoint: string;
  environment?: string;
  contractVersion?: number;
  mode?: TrackingMode;
  sessionTimeoutMinutes?: number;
  debug?: boolean;
  autoTrack?: boolean;
  heartbeatSeconds?: number;
  batchSize?: number;
  collectElementText?: boolean;
  sanitizeErrorMessages?: boolean;
  autoRUM?: boolean;
  appVersion?: string;
  releaseVersion?: string;
  gitSha?: string;
  deploymentId?: string;
}

interface QueuedEvent {
  id: string;
  name: string;
  timestamp: number;
  properties: Properties;
  context?: EventContext;
  debug?: boolean;
  contract_version: number;
}

const VISITOR_KEY = "momento_visitor_id";
const SESSION_KEY = "momento_session";
const CONSENT_KEY = "momento_consent";
const OFFLINE_KEY = "momento_offline_queue";

function id(): string {
  if (typeof crypto !== "undefined" && crypto.randomUUID)
    return crypto.randomUUID();
  return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    return (c === "x" ? r : (r & 3) | 8).toString(16);
  });
}

function storageAvailable(): boolean {
  try {
    localStorage.setItem("__momento_test", "1");
    localStorage.removeItem("__momento_test");
    return true;
  } catch {
    return false;
  }
}

function sanitizedURL(raw: string): string {
  if (!raw) return "";
  try {
    const value = new URL(raw, location.href);
    return `${value.origin}${value.pathname}`;
  } catch {
    return raw.split(/[?#]/, 1)[0];
  }
}

function sanitizedError(value: unknown): string {
  return String(value ?? "")
    .replace(/[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}/gi, "[REDACTED_EMAIL]")
    .replace(/\b\d{6}-?[1-4]\d{6}\b/g, "[REDACTED_ID]")
    .replace(/\b01[016789]-?\d{3,4}-?\d{4}\b/g, "[REDACTED_PHONE]")
    .slice(0, 500);
}

function hasTraffic(context: TrafficContext): boolean {
  return Object.values(context).some(Boolean);
}

function parseUA() {
  const ua = navigator.userAgent;
  const browser = /Edg\//.test(ua)
    ? "Edge"
    : /Chrome\//.test(ua)
      ? "Chrome"
      : /Firefox\//.test(ua)
        ? "Firefox"
        : /Safari\//.test(ua)
          ? "Safari"
          : "Other";
  const os = /Windows/.test(ua)
    ? "Windows"
    : /Android/.test(ua)
      ? "Android"
      : /iPhone|iPad/.test(ua)
        ? "iOS"
        : /Mac OS/.test(ua)
          ? "macOS"
          : /Linux/.test(ua)
            ? "Linux"
            : "Other";
  const type = /Mobi|Android|iPhone/.test(ua)
    ? "mobile"
    : /iPad|Tablet/.test(ua)
      ? "tablet"
      : "desktop";
  return {
    browser,
    os,
    type,
    language: navigator.language,
    screen: `${screen.width}x${screen.height}`,
  };
}

export class MomentoTracker {
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
  private consentState: ConsentState = "unknown";
  private acquisition: TrafficContext = {};

  init(options: MomentoOptions) {
    if (!options.siteId || !options.endpoint)
      throw new Error("Momento: siteId and endpoint are required");
    const environment = options.environment || "prd";
    const contractVersion = options.contractVersion ?? 1;
    if (!/^[a-z][a-z0-9_-]{0,31}$/.test(environment))
      throw new Error("Momento: environment is invalid");
    if (!Number.isInteger(contractVersion) || contractVersion < 1)
      throw new Error("Momento: contractVersion must be a positive integer");
    this.options = {
      mode: "full",
      sessionTimeoutMinutes: 30,
      autoTrack: true,
      heartbeatSeconds: 15,
      batchSize: 10,
      collectElementText: false,
      sanitizeErrorMessages: true,
      autoRUM: true,
      ...options,
      environment,
      contractVersion,
      endpoint: options.endpoint.replace(/\/$/, ""),
    };
    this.debugEnabled = !!this.options.debug;
    this.migrateLegacyStorage();
    this.consentState = this.readConsentState();
    this.loadIdentity();
    this.restoreOffline();
    if (this.options.autoTrack && !this.initialized) this.installAutoTracking();
    if (this.options.autoRUM && !this.initialized) this.installRUM();
    this.initialized = true;
    if (this.options.autoTrack) this.track("page_view");
    this.log("initialized", {
      siteId: options.siteId,
      mode: this.options.mode,
      environment: this.options.environment,
    });
    return this;
  }

  identify(userId: string, properties: Properties = {}) {
    if (!userId || /@|\+?\d{8,}/.test(userId)) {
      console.error(
        "[Momento] userId must be a non-PII internal or pseudonymous identifier",
      );
      return;
    }
    this.userId = userId;
    this.userProperties = { ...this.userProperties, ...properties };
  }

  track(name: string, properties: Properties = {}) {
    if (!this.canTrack()) return;
    this.ensureSession();
    const timestamp = Date.now();
    this.queue.push({
      id: id(),
      name,
      timestamp,
      properties: { ...properties, ...this.releaseContext() },
      context: this.context(),
      debug: this.debugEnabled,
      contract_version: this.options?.contractVersion || 1,
    });
    this.lastEventAt = timestamp;
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
      environment: this.options.environment,
      visitor_id: this.visitorId,
      session_id: this.sessionId,
      user_id: this.userId || undefined,
      user_properties: this.userProperties,
      context: events[0]?.context || this.context(),
      events,
    });
    try {
      if (useBeacon && navigator.sendBeacon) {
        const ok = navigator.sendBeacon(
          `${this.options.endpoint}/collect/v1/events`,
          new Blob([payload], { type: "text/plain;charset=UTF-8" }),
        );
        if (!ok) throw new Error("sendBeacon rejected payload");
      } else {
        const response = await fetch(
          `${this.options.endpoint}/collect/v1/events`,
          {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: payload,
            keepalive: true,
          },
        );
        if (!response.ok)
          throw new Error(`collector returned ${response.status}`);
      }
      if (this.queue.length) this.scheduleFlush();
    } catch (error) {
      this.queue.unshift(...events);
      this.saveOffline();
      this.log("delivery failed; queued offline", error);
    }
  }

  consent = {
    grant: () => {
      const wasTracking = this.canTrack();
      this.consentState = "granted";
      if (storageAvailable()) localStorage.setItem(this.storageKey(CONSENT_KEY), "granted");
      // Keep the acquisition captured when the SDK first loaded. A visitor can
      // move to another SPA route while the consent banner is still open.
      this.loadIdentity(true);
      if (!wasTracking && this.options?.autoTrack) this.track("page_view");
    },
    deny: () => {
      this.consentState = "denied";
      if (storageAvailable()) localStorage.setItem(this.storageKey(CONSENT_KEY), "denied");
      this.clearTrackingState();
    },
    revoke: () => {
      this.consentState = "unknown";
      if (storageAvailable()) localStorage.removeItem(this.storageKey(CONSENT_KEY));
      this.clearTrackingState();
      this.loadIdentity();
    },
  };

  cookie = {
    enable: () => {
      if (this.options) {
        this.options.mode = "full";
        this.loadIdentity(true);
      }
    },
    disable: () => {
      if (!this.options) return;
      this.options.mode = "cookieless";
      this.clearIdentityStorage();
      this.visitorId = id();
      this.sessionId = id();
      this.lastEventAt = Date.now();
      this.acquisition = this.detectTraffic();
    },
  };

  debug(enabled = true) {
    this.debugEnabled = enabled;
  }

  private canTrack() {
    if (!this.options || this.options.mode === "disabled") return false;
    if (this.consentState === "denied") return false;
    if (navigator.doNotTrack === "1" && this.consentState !== "granted")
      return false;
    if (this.options.mode === "consent-required")
      return this.consentState === "granted";
    return true;
  }

  private isPersistent() {
    if (this.consentState === "denied") return false;
    if (navigator.doNotTrack === "1" && this.consentState !== "granted")
      return false;
    return (
      this.options?.mode === "full" ||
      ((this.options?.mode === "consent-required" ||
        this.options?.mode === "cookie-consent") &&
        this.consentState === "granted")
    );
  }

  private readConsentState(): ConsentState {
    if (!storageAvailable()) return "unknown";
    const stored = localStorage.getItem(this.storageKey(CONSENT_KEY));
    return stored === "granted" || stored === "denied" ? stored : "unknown";
  }

  private storageKey(base: string) {
    return `${base}:${this.options?.siteId || "unknown"}:${this.options?.environment || "prd"}`;
  }

  private migrateLegacyStorage() {
    if (!storageAvailable() || this.options?.environment !== "prd") return;
    for (const key of [VISITOR_KEY, SESSION_KEY, CONSENT_KEY, OFFLINE_KEY]) {
      const scoped = this.storageKey(key);
      if (localStorage.getItem(scoped) !== null) continue;
      const legacy = localStorage.getItem(key);
      if (legacy !== null) localStorage.setItem(scoped, legacy);
    }
  }

  private clearIdentityStorage() {
    if (!storageAvailable()) return;
    localStorage.removeItem(this.storageKey(VISITOR_KEY));
    localStorage.removeItem(this.storageKey(SESSION_KEY));
    localStorage.removeItem(this.storageKey(OFFLINE_KEY));
  }

  private clearTrackingState() {
    this.queue = [];
    if (this.timer) window.clearTimeout(this.timer);
    this.timer = undefined;
    this.clearIdentityStorage();
    this.visitorId = id();
    this.sessionId = id();
    this.lastEventAt = Date.now();
    this.acquisition = this.detectTraffic();
  }

  private loadIdentity(preserveAcquisition = false) {
    const initialAcquisition = this.acquisition;
    const acquisitionFallback = () =>
      preserveAcquisition && hasTraffic(initialAcquisition)
        ? initialAcquisition
        : this.detectTraffic();
    const persistent = storageAvailable() && this.isPersistent();
    this.visitorId = persistent
      ? localStorage.getItem(this.storageKey(VISITOR_KEY)) || id()
      : id();
    if (persistent)
      localStorage.setItem(this.storageKey(VISITOR_KEY), this.visitorId);
    if (persistent) {
      try {
        const s = JSON.parse(
          localStorage.getItem(this.storageKey(SESSION_KEY)) || "{}",
        );
        this.sessionId = s.id || id();
        this.lastEventAt = s.last || 0;
        this.acquisition =
          s.traffic && typeof s.traffic === "object"
            ? s.traffic
            : acquisitionFallback();
      } catch {
        this.sessionId = id();
        this.lastEventAt = 0;
        this.acquisition = acquisitionFallback();
      }
    } else {
      this.sessionId = id();
      this.lastEventAt = 0;
      this.acquisition = acquisitionFallback();
    }
    this.ensureSession(preserveAcquisition ? acquisitionFallback() : undefined);
  }

  private ensureSession(acquisition?: TrafficContext) {
    const timeout = (this.options?.sessionTimeoutMinutes || 30) * 60_000;
    if (!this.sessionId || Date.now() - this.lastEventAt > timeout) {
      const timestamp = Date.now();
      this.sessionId = id();
      this.lastEventAt = timestamp;
      this.acquisition = acquisition || this.detectTraffic();
      if (this.canTrack())
        this.queue.push({
          id: id(),
          name: "session_start",
          timestamp,
          properties: this.releaseContext(),
          context: this.context(),
          debug: this.debugEnabled,
          contract_version: this.options?.contractVersion || 1,
        });
    }
  }
  private persistSession() {
    if (storageAvailable() && this.isPersistent()) {
      localStorage.setItem(
        this.storageKey(SESSION_KEY),
        JSON.stringify({
          id: this.sessionId,
          last: this.lastEventAt,
          traffic: this.acquisition,
        }),
      );
    }
  }

  private detectTraffic(): TrafficContext {
    const params = new URLSearchParams(location.search);
    return {
      source: params.get("utm_source") || undefined,
      medium: params.get("utm_medium") || undefined,
      campaign: params.get("utm_campaign") || undefined,
      term: params.get("utm_term") || undefined,
      content: params.get("utm_content") || undefined,
    };
  }

  private context() {
    return {
      page: {
        url: sanitizedURL(location.href),
        title: document.title,
        referrer: sanitizedURL(document.referrer),
      },
      device: parseUA(),
      traffic: { ...this.acquisition },
    };
  }
  private scheduleFlush() {
    if (this.timer) return;
    this.timer = window.setTimeout(() => {
      this.timer = undefined;
      void this.flush();
    }, 1000);
  }
  private saveOffline() {
    if (!storageAvailable() || !this.isPersistent()) return;
    try {
      localStorage.setItem(
        this.storageKey(OFFLINE_KEY),
        JSON.stringify(this.queue.slice(-200)),
      );
    } catch {
      /* quota */
    }
  }
  private restoreOffline() {
    if (!storageAvailable() || !this.isPersistent()) return;
    try {
      const saved = JSON.parse(
        localStorage.getItem(this.storageKey(OFFLINE_KEY)) || "[]",
      );
      if (Array.isArray(saved)) this.queue.push(...saved.slice(-200));
      localStorage.removeItem(this.storageKey(OFFLINE_KEY));
    } catch {
      /* corrupt queue */
    }
  }
  private log(...args: unknown[]) {
    if (this.debugEnabled) console.debug("[Momento]", ...args);
  }

  private releaseContext(): Properties {
    if (!this.options) return {};
    const context: Properties = {};
    if (this.options.appVersion) context.app_version = this.options.appVersion;
    if (this.options.releaseVersion)
      context.release_version = this.options.releaseVersion;
    if (this.options.gitSha) context.git_sha = this.options.gitSha;
    if (this.options.deploymentId)
      context.deployment_id = this.options.deploymentId;
    return context;
  }

  private vitalRating(metric: string, value: number) {
    const thresholds: Record<string, [number, number]> = {
      LCP: [2500, 4000],
      INP: [200, 500],
      CLS: [0.1, 0.25],
      FCP: [1800, 3000],
      TTFB: [800, 1800],
    };
    const limits = thresholds[metric];
    if (!limits) return "unknown";
    return value <= limits[0]
      ? "good"
      : value <= limits[1]
        ? "needs-improvement"
        : "poor";
  }

  private trackVital(metric: string, value: number, extra: Properties = {}) {
    if (!Number.isFinite(value) || value < 0) return;
    this.track("web_vital", {
      metric,
      value: Math.round(value * 1000) / 1000,
      rating: this.vitalRating(metric, value),
      ...extra,
    });
  }

  private installRUM() {
    const observe = (
      type: string,
      callback: (entries: PerformanceEntry[]) => void,
    ) => {
      if (typeof PerformanceObserver === "undefined") return;
      try {
        const observer = new PerformanceObserver((list) =>
          callback(list.getEntries()),
        );
        observer.observe({ type, buffered: true });
      } catch {
        /* browser does not support this entry type */
      }
    };

    let lastLCP = 0;
    let cls = 0;
    let maxINP = 0;
    let emitted = false;
    observe("largest-contentful-paint", (entries) => {
      const last = entries[entries.length - 1];
      if (last) lastLCP = last.startTime;
    });
    observe("layout-shift", (entries) => {
      for (const entry of entries) {
        const shift = entry as PerformanceEntry & {
          value?: number;
          hadRecentInput?: boolean;
        };
        if (!shift.hadRecentInput) cls += shift.value || 0;
      }
    });
    observe("event", (entries) => {
      for (const entry of entries) {
        const timing = entry as PerformanceEntry & {
          duration: number;
          interactionId?: number;
        };
        if ((timing.interactionId || 0) > 0)
          maxINP = Math.max(maxINP, timing.duration);
      }
    });
    observe("paint", (entries) => {
      const fcp = entries.find((entry) => entry.name === "first-contentful-paint");
      if (fcp) this.trackVital("FCP", fcp.startTime);
    });

    const emitFinalVitals = () => {
      if (emitted) return;
      emitted = true;
      if (lastLCP) this.trackVital("LCP", lastLCP);
      this.trackVital("CLS", cls);
      if (maxINP) this.trackVital("INP", maxINP);
    };
    addEventListener("visibilitychange", () => {
      if (document.visibilityState === "hidden") emitFinalVitals();
    });

    const navigation = () => {
      const entry = performance.getEntriesByType?.(
        "navigation",
      )[0] as PerformanceNavigationTiming | undefined;
      if (!entry) return;
      this.trackVital("TTFB", entry.responseStart, {
        navigation_type: entry.type,
      });
      this.trackVital("DOMContentLoaded", entry.domContentLoadedEventEnd);
      this.trackVital("page_load", entry.loadEventEnd || entry.duration);
    };
    if (document.readyState === "complete") window.setTimeout(navigation, 0);
    else addEventListener("load", navigation, { once: true });
  }

  private installAutoTracking() {
    const route = () => {
      this.trackedScroll.clear();
      window.setTimeout(() => this.track("page_view"), 0);
    };
    for (const method of ["pushState", "replaceState"] as const) {
      const original = history[method];
      history[method] = function (
        this: History,
        ...args: Parameters<History[typeof method]>
      ) {
        const value = original.apply(this, args);
        route();
        return value;
      } as History[typeof method];
    }
    addEventListener("popstate", route);
    addEventListener(
      "click",
      (event) => {
        const target = (event.target as Element | null)?.closest(
          "a,button,[role=button]",
        ) as HTMLElement | null;
        if (!target) return;
        const anchor = target.closest("a") as HTMLAnchorElement | null;
        const props: Properties = {
          element_tag: target.tagName.toLowerCase(),
          element_id: target.id || undefined,
          button:
            target.dataset.analyticsButton ||
            target.getAttribute("aria-label") ||
            undefined,
          feature: target.dataset.analyticsFeature || undefined,
        };
        if (this.options?.collectElementText)
          props.element_text =
            target.innerText?.trim().slice(0, 100) || undefined;
        if (anchor?.href && anchor.origin !== location.origin) {
          props.url = sanitizedURL(anchor.href);
          this.track("outbound_click", props);
        } else if (
          anchor?.download ||
          /\.(pdf|zip|docx?|xlsx?|pptx?|csv)$/i.test(anchor?.pathname || "")
        ) {
          props.url = sanitizedURL(anchor?.href || "");
          this.track("file_download", props);
        } else this.track("click", props);
      },
      { capture: true },
    );
    addEventListener(
      "scroll",
      () => {
        const max = document.documentElement.scrollHeight - innerHeight;
        if (max <= 0) return;
        const depth = Math.round((scrollY / max) * 100);
        for (const threshold of [25, 50, 75, 90])
          if (depth >= threshold && !this.trackedScroll.has(threshold)) {
            this.trackedScroll.add(threshold);
            this.track("scroll", { percent: threshold });
          }
      },
      { passive: true },
    );
    addEventListener("focusin", (event) => {
      const form = (event.target as Element | null)?.closest("form");
      if (form && !form.dataset.momentoStarted) {
        form.dataset.momentoStarted = "1";
        this.track("form_start", {
          form_id: form.id || form.getAttribute("name"),
        });
      }
    });
    addEventListener("submit", (event) => {
      const form = event.target as HTMLFormElement;
      this.track("form_submit", { form_id: form.id || form.name });
    });
    addEventListener(
      "error",
      (event) => {
        const error = event as ErrorEvent;
        if (error.message) {
          this.track("error", {
            message: this.options?.sanitizeErrorMessages
              ? sanitizedError(error.message)
              : error.message,
            filename: sanitizedURL(error.filename),
            line: error.lineno,
            column: error.colno,
          });
          return;
        }
        const target = event.target as
          | (HTMLElement & { src?: string; href?: string })
          | null;
        if (target)
          this.track("resource_error", {
            resource: sanitizedURL(target.src || target.href || ""),
            resource_type: target.tagName?.toLowerCase(),
          });
      },
      true,
    );
    addEventListener("unhandledrejection", (event) =>
      this.track("error", {
        message: this.options?.sanitizeErrorMessages
          ? sanitizedError(event.reason)
          : String(event.reason),
        type: "unhandledrejection",
      }),
    );
    addEventListener("visibilitychange", () => {
      if (document.visibilityState === "hidden") void this.flush(true);
    });
    addEventListener("online", () => void this.flush());
    window.setInterval(
      () => {
        if (document.visibilityState === "visible")
          this.track("user_engagement", {
            active_seconds: this.options?.heartbeatSeconds,
          });
      },
      (this.options?.heartbeatSeconds || 15) * 1000,
    );
  }
}

const tracker = new MomentoTracker();
declare global {
  interface Window {
    analytics: MomentoTracker;
  }
}
window.analytics = tracker;

const script = document.currentScript as HTMLScriptElement | null;
if (script?.dataset.siteId) {
  const endpoint = script.src ? new URL(script.src).origin : location.origin;
  tracker.init({
    siteId: script.dataset.siteId,
    endpoint,
    mode: (script.dataset.mode as TrackingMode) || "full",
    environment: script.dataset.environment || "prd",
    contractVersion: Number(script.dataset.contractVersion || 1),
    debug: script.dataset.debug === "true",
    collectElementText: script.dataset.collectElementText === "true",
    autoRUM: script.dataset.autoRum !== "false",
    appVersion: script.dataset.appVersion,
    releaseVersion: script.dataset.releaseVersion,
    gitSha: script.dataset.gitSha,
    deploymentId: script.dataset.deploymentId,
  });
}
