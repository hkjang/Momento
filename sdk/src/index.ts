export type TrackingMode =
  "consent-required" | "cookie-consent" | "cookieless" | "full" | "disabled";
type ConsentState = "unknown" | "granted" | "denied";
type RouteKind = "push" | "replace" | "pop";
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
  frustrationSignals?: boolean;
  searchTracking?: boolean;
  collectSearchTerms?: boolean;
  searchParams?: string[];
  appVersion?: string;
  releaseVersion?: string;
  gitSha?: string;
  deploymentId?: string;
  sessionProperties?: Properties;
}

interface QueuedEvent {
  id: string;
  name: string;
  timestamp: number;
  properties: Properties;
  session_properties: Properties;
  context?: EventContext;
  debug?: boolean;
  contract_version: number;
  // The session and visitor this event happened in. The collector reads them per
  // payload rather than per event, and a queue survives page loads, so without
  // these an event queued while the network was down was delivered under whatever
  // session happened to be current when it finally went out. They are stripped
  // before the payload is sent, so the wire format is unchanged.
  session_id?: string;
  visitor_id?: string;
}

/**
 * Frustration detection thresholds.
 *
 * These are the numbers the signal definitions in the console are written
 * against, so they live here as named constants rather than inline literals.
 * Rage clicks follow the industry convention of three clicks on one element
 * inside a second; a slow interaction is the "poor" end of the INP scale.
 */
/**
 * The identifier identify() refuses. It becomes the user id on every event, in
 * the visitor and identified-user tables, on the identities screen and in every
 * export — and the collector's privacy filter does not look at it. It walks user
 * properties, session properties, event properties, items and the page URL,
 * title and referrer; the user id is not among them, so this is the only place
 * that decides.
 *
 * It used to be `@` or eight consecutive digits, which let through the way a
 * Korean phone number and a resident registration number are actually written:
 * 010-1234-5678 and 860101-1234567 have no run of eight digits in them. These
 * are the shapes the collector already masks everywhere else, so the two halves
 * now refuse the same things. The long-digit rule is kept alongside them, so
 * this only ever refuses more than it did.
 */
const PII_IDENTIFIER = [
  /@/,
  /\+?\d{8,}/,
  /(?:\+?82[- ]?)?0?1[016789][- ]?\d{3,4}[- ]?\d{4}/,
  /\b\d{6}[- ]?[1-8]\d{6}\b/,
];

function looksLikeAPerson(value: string): boolean {
  return PII_IDENTIFIER.some((pattern) => pattern.test(value));
}

const RAGE_CLICK_WINDOW_MS = 1000;
const RAGE_CLICK_THRESHOLD = 3;
const DEAD_CLICK_WINDOW_MS = 1200;
const RAPID_BACK_MS = 3000;
const ERROR_AFTER_CLICK_MS = 2000;
const SLOW_INTERACTION_MS = 500;
const REPEATED_SEARCH_WINDOW_MS = 120000;
const SEARCH_RESULT_WAIT_MS = 1500;
const SEARCH_RESULT_POLL_MS = 300;

/** A page cannot report an unbounded number of signals. */
const SIGNALS_PER_PAGE = 20;

const DEFAULT_SEARCH_PARAMS = [
  "q",
  "query",
  "search",
  "searchword",
  "keyword",
  "kwd",
  "term",
  "s",
];

const INTERACTIVE_SELECTOR =
  "a,button,input,select,textarea,summary,label,[role=button],[role=link],[role=tab],[role=menuitem],[onclick],[data-analytics-button]";

const RESULTS_ATTRIBUTE = "data-momento-search-results";
const POSITION_ATTRIBUTE = "data-momento-search-position";

const VISITOR_KEY = "momento_visitor_id";
const SESSION_KEY = "momento_session";
const CONSENT_KEY = "momento_consent";
const OFFLINE_KEY = "momento_offline_queue";
// Events the queue could not keep. The cap dropped the oldest without a trace:
// 260 tracked became 200 persisted and nothing recorded the 60. A number an
// operator can see beats a gap they cannot.
const DROPPED_KEY = "momento_dropped_events";
// How many queued events survive a page load. At the measured 472 bytes each that
// is about 94KB, well inside a localStorage origin budget.
const OFFLINE_LIMIT = 200;

function id(): string {
  if (typeof crypto !== "undefined" && crypto.randomUUID)
    return crypto.randomUUID();
  return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    return (c === "x" ? r : (r & 3) | 8).toString(16);
  });
}

/**
 * elapsed reads a clock that cannot move backwards, for the windows the tracker
 * compares against within one page: the rage-click burst, the retry on a form,
 * an error after a click, how long a page was open, and the gap between two
 * searches.
 *
 * Date.now() is not that clock. It follows the system clock, and an NTP
 * correction, a laptop resuming, or someone setting the time moves it. Measured
 * here: a wait of 2099ms by the monotonic clock read as 120ms of wall time, which
 * put two searches two seconds apart inside the window that suppresses a repeated
 * keystroke and swallowed the signal. A jump the other way would split a live
 * visit into two sessions.
 *
 * Event timestamps stay on the wall clock, because the server needs the real time
 * an event happened. So does the session timeout, because it has to survive a page
 * load and nothing else does.
 */
function elapsed(): number {
  return typeof performance !== "undefined" &&
    typeof performance.now === "function"
    ? performance.now()
    : Date.now();
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

/**
 * redactPII removes the identifiers that most often leak into free text before
 * it leaves the browser. Error messages and search terms are both free text
 * typed or produced by a person, so they share one redactor.
 */
function redactPII(value: unknown, limit: number): string {
  return String(value ?? "")
    .replace(/[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}/gi, "[REDACTED_EMAIL]")
    .replace(/\b\d{6}-?[1-4]\d{6}\b/g, "[REDACTED_ID]")
    .replace(/\b01[016789]-?\d{3,4}-?\d{4}\b/g, "[REDACTED_PHONE]")
    .slice(0, limit);
}

function sanitizedError(value: unknown): string {
  return redactPII(value, 500);
}

/** elementSignature identifies an element well enough to group repeat clicks. */
function elementSignature(element: Element): string {
  const tag = element.tagName?.toLowerCase() || "unknown";
  if (element.id) return `${tag}#${element.id}`;
  const className =
    typeof element.className === "string"
      ? element.className.trim().split(/\s+/)[0]
      : "";
  const parent = element.parentElement;
  const index = parent
    ? Array.prototype.indexOf.call(parent.children, element)
    : -1;
  return `${tag}${className ? "." + className : ""}${index >= 0 ? ":" + index : ""}`;
}

/**
 * looksClickable decides whether nothing happening after a click is worth
 * reporting. A paragraph that does not react is not a dead click; a styled div
 * that the application treats as a button is.
 */
function looksClickable(element: Element): boolean {
  if (element.closest?.(INTERACTIVE_SELECTOR)) return true;
  try {
    return getComputedStyle(element).cursor === "pointer";
  } catch {
    return false;
  }
}

/**
 * resolveEndpoint decides where events are sent.
 *
 * `data-endpoint` accepts an absolute collector URL or a same-origin path. A path
 * is the escape hatch for applications whose Content-Security-Policy only allows
 * `connect-src 'self'`: proxy the collector under, for example, `/momento` and the
 * request stays first party instead of being blocked.
 */
export function resolveEndpoint(
  configured: string | undefined,
  scriptSrc: string,
  fallbackOrigin: string,
): string {
  const value = (configured || "").trim();
  if (!value) {
    try {
      return scriptSrc
        ? new URL(scriptSrc, fallbackOrigin).origin
        : fallbackOrigin;
    } catch {
      return fallbackOrigin;
    }
  }
  const trimmed = value.replace(/\/+$/, "");
  if (/^https?:\/\//i.test(trimmed)) return trimmed;
  if (!trimmed || trimmed === "/") return fallbackOrigin;
  return trimmed.startsWith("/") ? trimmed : `/${trimmed}`;
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
  private sessionProperties: Properties = {};
  private defaultSessionProperties: Properties = {};
  private queue: QueuedEvent[] = [];
  private timer?: number;
  private debugEnabled = false;
  private initialized = false;
  private trackedScroll = new Set<number>();
  private consentState: ConsentState = "unknown";
  private acquisition: TrafficContext = {};
  private cspReported = false;
  private routeHooks: Array<
    (kind: RouteKind, previousAt: number, previousPath: string) => void
  > = [];
  private routeInstalled = false;
  private lastRouteAt = 0;
  private lastRoutePath = "";
  private rageBurst: {
    signature: string;
    at: number;
    count: number;
    timer?: number;
  } | null = null;
  private deadClickPending = false;
  private lastClick: { at: number; properties: Properties } | null = null;
  private formAttempts = new Map<string, number>();
  private lastInvalidForm = { key: "", at: 0 };
  private slowInteractionSeen = 0;
  private searchState: { query: string; at: number } | null = null;
  private signalsThisPage = 0;

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
      frustrationSignals: true,
      searchTracking: true,
      collectSearchTerms: false,
      ...options,
      environment,
      contractVersion,
      endpoint: options.endpoint.replace(/\/$/, ""),
    };
    this.debugEnabled = !!this.options.debug;
    this.defaultSessionProperties = { ...(options.sessionProperties || {}) };
    this.sessionProperties = { ...this.defaultSessionProperties };
    this.migrateLegacyStorage();
    this.consentState = this.readConsentState();
    this.loadIdentity();
    this.restoreOffline();
    this.reportDropped();
    if (!this.initialized) this.installCSPDiagnostics();
    if (this.options.autoTrack && !this.initialized) this.installAutoTracking();
    if (this.options.autoRUM && !this.initialized) this.installRUM();
    if (this.options.frustrationSignals && !this.initialized)
      this.installFrustrationSignals();
    const firstRun = !this.initialized;
    this.initialized = true;
    if (this.options.autoTrack) this.track("page_view");
    if (this.options.searchTracking && firstRun) this.installSearchTracking();
    this.log("initialized", {
      siteId: options.siteId,
      mode: this.options.mode,
      environment: this.options.environment,
    });
    return this;
  }

  identify(userId: string, properties: Properties = {}) {
    if (!userId || looksLikeAPerson(userId)) {
      console.error(
        "[Momento] userId must be a non-PII internal or pseudonymous identifier",
      );
      return;
    }
    this.userId = userId;
    this.userProperties = { ...this.userProperties, ...properties };
  }

  setSessionProperties(properties: Properties = {}) {
    this.sessionProperties = { ...this.sessionProperties, ...properties };
    this.persistSession();
  }

  /**
   * trackSearch reports a site search the tracker cannot see by itself, for
   * applications that search without changing the URL. Pass resultCount when
   * it is known — the zero-result rate is the most actionable search metric and
   * it cannot be derived from the query alone.
   */
  trackSearch(query: string, resultCount?: number) {
    if (!query || !query.trim()) return;
    this.recordSearch(query, {
      source: "manual",
      ...(Number.isFinite(resultCount as number)
        ? { result_count: Math.max(0, Math.trunc(resultCount as number)) }
        : {}),
    });
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
      session_properties: { ...this.sessionProperties },
      context: this.context(),
      debug: this.debugEnabled,
      contract_version: this.options?.contractVersion || 1,
      session_id: this.sessionId,
      visitor_id: this.visitorId,
    });
    this.lastEventAt = timestamp;
    this.persistSession();
    this.log("track", name, properties);
    if (this.queue.length >= (this.options?.batchSize || 10)) void this.flush();
    else this.scheduleFlush();
  }

  /** collectorURL keeps the absolute and proxied forms in one place. */
  private collectorURL(): string {
    return `${this.options?.endpoint || ""}/collect/v1/events`;
  }

  /**
   * installCSPDiagnostics turns a silent browser block into an actionable message.
   * A tracked page that ships `connect-src 'self'` rejects the collector request,
   * and the console error names the exact policy to add.
   */
  private installCSPDiagnostics() {
    if (typeof addEventListener !== "function") return;
    addEventListener("securitypolicyviolation", (event: Event) => {
      if (this.cspReported) return;
      const violation = event as SecurityPolicyViolationEvent;
      const blocked = String(violation.blockedURI || "");
      const endpoint = this.options?.endpoint || "";
      if (!blocked || !endpoint) return;
      let origin = "";
      try {
        origin = new URL(
          endpoint.startsWith("http") ? endpoint : location.origin + endpoint,
        ).origin;
      } catch {
        return;
      }
      if (!blocked.startsWith(origin)) return;
      this.cspReported = true;
      const directive = String(
        violation.effectiveDirective ||
          violation.violatedDirective ||
          "connect-src",
      );
      console.error(
        `[Momento] ${directive} 위반으로 수집이 차단되었습니다. 측정 대상 애플리케이션의 CSP에 "script-src 'self' ${origin}; connect-src 'self' ${origin}"을 추가하거나, ${origin} 을 같은 Origin 경로로 프록시하고 tracker script에 data-endpoint="/momento"를 지정하십시오.`,
      );
    });
  }

  async flush(useBeacon = false) {
    if (!this.options || !this.queue.length || !this.canTrack()) return;
    const events = this.queue.splice(0, this.options.batchSize || 10);
    try {
      // One payload per session, because the collector reads the session from the
      // payload and applies it to every event in it. A batch restored from the
      // offline queue can hold yesterday's events, and sending those under today's
      // session pulled its start time back to yesterday: a 26 hour session, a
      // landing page from the previous visit, and an average session duration that
      // rose with every reconnection.
      for (const group of this.groupBySession(events)) {
        await this.deliver(group, useBeacon);
      }
      if (this.queue.length) this.scheduleFlush();
    } catch (error) {
      // The whole batch goes back, including groups already sent. Redelivery is
      // safe: every event carries an id and the collector ignores one it has.
      this.queue.unshift(...events);
      this.saveOffline();
      this.log("delivery failed; queued offline", error);
    }
  }

  /** groupBySession splits a batch into runs that share a session and visitor. */
  private groupBySession(events: QueuedEvent[]): QueuedEvent[][] {
    const groups = new Map<string, QueuedEvent[]>();
    for (const event of events) {
      // An event queued by a version that did not record them belongs to whoever
      // is current, which is the old behaviour and the best available guess.
      const key = `${event.visitor_id || this.visitorId}\u0000${event.session_id || this.sessionId}`;
      const group = groups.get(key);
      if (group) group.push(event);
      else groups.set(key, [event]);
    }
    return [...groups.values()];
  }

  private async deliver(events: QueuedEvent[], useBeacon: boolean) {
    const first = events[0];
    const payload = JSON.stringify({
      site_id: this.options!.siteId,
      environment: this.options!.environment,
      visitor_id: first?.visitor_id || this.visitorId,
      session_id: first?.session_id || this.sessionId,
      user_id: this.userId || undefined,
      user_properties: this.userProperties,
      session_properties: this.sessionProperties,
      context: first?.context || this.context(),
      // The session and visitor are payload-level on the wire, so they are not
      // repeated on each event.
      events: events.map(({ session_id, visitor_id, ...event }) => event),
    });
    if (useBeacon && navigator.sendBeacon) {
      // sendBeacon reports whether the browser accepted the payload, never
      // whether it arrived, and there is no callback that would say. Measured:
      // after an accepted beacon nothing was left in storage, so a beacon that
      // never went out took the batch with it — and the batch at page exit holds
      // the last page view, the exit page and a completed purchase.
      //
      // Being offline is the one case where the outcome is known in advance, and
      // it is the case the offline queue exists for. Treating it as the failure it
      // is keeps the queue, and the next page load sends it. A browser killed hard
      // while online can still lose an accepted beacon; nothing the page can
      // observe would tell it so.
      if (navigator.onLine === false)
        throw new Error("offline when the page was hidden");
      const ok = navigator.sendBeacon(
        this.collectorURL(),
        new Blob([payload], { type: "text/plain;charset=UTF-8" }),
      );
      if (!ok) throw new Error("sendBeacon rejected payload");
      return;
    }
    const response = await fetch(this.collectorURL(), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: payload,
      keepalive: true,
    });
    if (!response.ok) throw new Error(`collector returned ${response.status}`);
  }

  consent = {
    grant: () => {
      const wasTracking = this.canTrack();
      this.consentState = "granted";
      if (storageAvailable())
        localStorage.setItem(this.storageKey(CONSENT_KEY), "granted");
      // Keep the acquisition captured when the SDK first loaded. A visitor can
      // move to another SPA route while the consent banner is still open.
      this.loadIdentity(true);
      if (!wasTracking && this.options?.autoTrack) this.track("page_view");
    },
    deny: () => {
      this.consentState = "denied";
      if (storageAvailable())
        localStorage.setItem(this.storageKey(CONSENT_KEY), "denied");
      this.clearTrackingState();
    },
    revoke: () => {
      this.consentState = "unknown";
      if (storageAvailable())
        localStorage.removeItem(this.storageKey(CONSENT_KEY));
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
    localStorage.removeItem(this.storageKey(DROPPED_KEY));
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
        this.sessionProperties =
          s.properties && typeof s.properties === "object"
            ? { ...this.defaultSessionProperties, ...s.properties }
            : { ...this.defaultSessionProperties };
      } catch {
        this.sessionId = id();
        this.lastEventAt = 0;
        this.acquisition = acquisitionFallback();
        this.sessionProperties = { ...this.defaultSessionProperties };
      }
    } else {
      this.sessionId = id();
      this.lastEventAt = 0;
      this.acquisition = acquisitionFallback();
      this.sessionProperties = { ...this.defaultSessionProperties };
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
      this.sessionProperties = { ...this.defaultSessionProperties };
      if (this.canTrack())
        this.queue.push({
          id: id(),
          name: "session_start",
          timestamp,
          properties: this.releaseContext(),
          session_properties: { ...this.sessionProperties },
          context: this.context(),
          debug: this.debugEnabled,
          contract_version: this.options?.contractVersion || 1,
          session_id: this.sessionId,
          visitor_id: this.visitorId,
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
          properties: this.sessionProperties,
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
      const kept = this.queue.slice(-OFFLINE_LIMIT);
      this.recordDropped(this.queue.length - kept.length);
      localStorage.setItem(this.storageKey(OFFLINE_KEY), JSON.stringify(kept));
    } catch {
      // A quota failure loses the whole queue, which is the same silent gap as
      // the cap, so it is counted the same way.
      this.recordDropped(this.queue.length);
    }
  }

  /** recordDropped remembers events that were lost so a later delivery can say so. */
  private recordDropped(count: number) {
    if (count <= 0 || !storageAvailable() || !this.isPersistent()) return;
    try {
      const key = this.storageKey(DROPPED_KEY);
      const previous = Number(localStorage.getItem(key) || 0);
      localStorage.setItem(key, String((previous || 0) + count));
    } catch {
      /* nothing left to do if the counter itself cannot be stored */
    }
  }

  /**
   * reportDropped turns the silent gap into an event, once per page load and after
   * the queue is restored. Without it the loss is invisible: the numbers are
   * simply lower than they should be, and nothing says by how much or that it
   * happened at all.
   */
  private reportDropped() {
    if (!storageAvailable() || !this.isPersistent()) return;
    try {
      const key = this.storageKey(DROPPED_KEY);
      const dropped = Number(localStorage.getItem(key) || 0);
      localStorage.removeItem(key);
      if (dropped > 0)
        this.track("collection_dropped", { events_dropped: dropped });
    } catch {
      /* corrupt counter */
    }
  }
  private restoreOffline() {
    if (!storageAvailable() || !this.isPersistent()) return;
    try {
      const saved = JSON.parse(
        localStorage.getItem(this.storageKey(OFFLINE_KEY)) || "[]",
      );
      if (Array.isArray(saved)) this.queue.push(...saved.slice(-OFFLINE_LIMIT));
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
      const fcp = entries.find(
        (entry) => entry.name === "first-contentful-paint",
      );
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
      const entry = performance.getEntriesByType?.("navigation")[0] as
        PerformanceNavigationTiming | undefined;
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

  /**
   * installRouteTracking patches history once and lets every feature register a
   * hook. Page views, search detection and back-navigation timing all need to
   * know that the route changed, and each of them patching history separately
   * would report a route change several times.
   */
  private installRouteTracking() {
    if (this.routeInstalled) return;
    this.routeInstalled = true;
    this.lastRouteAt = elapsed();
    this.lastRoutePath =
      typeof location !== "undefined" ? location.pathname : "";
    const fire = (kind: RouteKind) => {
      const previousAt = this.lastRouteAt;
      const previousPath = this.lastRoutePath;
      this.lastRouteAt = elapsed();
      this.lastRoutePath = location.pathname;
      this.signalsThisPage = 0;
      for (const hook of this.routeHooks) {
        try {
          hook(kind, previousAt, previousPath);
        } catch {
          /* one hook must not stop the others */
        }
      }
    };
    for (const method of ["pushState", "replaceState"] as const) {
      const original = history[method];
      history[method] = function (
        this: History,
        ...args: Parameters<History[typeof method]>
      ) {
        const value = original.apply(this, args);
        fire(method === "pushState" ? "push" : "replace");
        return value;
      } as History[typeof method];
    }
    addEventListener("popstate", () => fire("pop"));
  }

  private onRoute(
    hook: (kind: RouteKind, previousAt: number, previousPath: string) => void,
  ) {
    this.installRouteTracking();
    this.routeHooks.push(hook);
  }

  /** elementProperties describes a clicked element the same way for every signal. */
  private elementProperties(target: HTMLElement): Properties {
    const props: Properties = {
      element_tag: target.tagName?.toLowerCase(),
      element_id: target.id || undefined,
      button:
        target.dataset?.analyticsButton ||
        target.getAttribute?.("aria-label") ||
        undefined,
      feature: target.dataset?.analyticsFeature || undefined,
    };
    if (this.options?.collectElementText)
      props.element_text = target.innerText?.trim().slice(0, 100) || undefined;
    return props;
  }

  /**
   * signal reports a frustration or search signal under a per-page cap. A page
   * stuck in a render loop must not turn into thousands of events.
   */
  private signal(name: string, properties: Properties = {}) {
    if (this.signalsThisPage >= SIGNALS_PER_PAGE) return;
    this.signalsThisPage += 1;
    this.track(name, properties);
  }

  private installAutoTracking() {
    this.onRoute(() => {
      this.trackedScroll.clear();
      window.setTimeout(() => this.track("page_view"), 0);
    });
    addEventListener(
      "click",
      (event) => {
        const element = event.target as HTMLElement | null;
        if (!element) return;
        const target = (element.closest?.("a,button,[role=button]") ||
          null) as HTMLElement | null;
        if (target) {
          const anchor = target.closest("a") as HTMLAnchorElement | null;
          const props = this.elementProperties(target);
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
          if (this.options?.searchTracking)
            this.observeSearchClick(target, anchor);
        }
        if (this.options?.frustrationSignals)
          this.observeClickFrustration(target || element);
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
          this.reportErrorAfterClick("error");
          return;
        }
        const target = event.target as
          (HTMLElement & { src?: string; href?: string }) | null;
        if (target)
          this.track("resource_error", {
            resource: sanitizedURL(target.src || target.href || ""),
            resource_type: target.tagName?.toLowerCase(),
          });
        this.reportErrorAfterClick("resource_error");
      },
      true,
    );
    addEventListener("unhandledrejection", (event) => {
      this.track("error", {
        message: this.options?.sanitizeErrorMessages
          ? sanitizedError(event.reason)
          : String(event.reason),
        type: "unhandledrejection",
      });
      this.reportErrorAfterClick("unhandledrejection");
    });
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

  /**
   * installFrustrationSignals turns the signals the Frustration report scores
   * into something the tracker produces on its own. Before this the report
   * weighed seven signals that only hand-written instrumentation ever sent, so
   * the page was empty for everyone using the tracker as shipped.
   *
   * Click-driven signals are detected inside the single click listener that
   * auto-tracking already installs; the rest are wired here.
   */
  private installFrustrationSignals() {
    this.onRoute((kind, previousAt, previousPath) => {
      if (kind !== "pop") return;
      const dwell = elapsed() - previousAt;
      if (dwell > RAPID_BACK_MS) return;
      this.signal("rapid_back", {
        dwell_ms: dwell,
        from_path: previousPath || undefined,
        to_path: location.pathname,
      });
    });

    addEventListener("submit", (event) => {
      const form = event.target as HTMLFormElement | null;
      if (!form) return;
      const key = this.formKey(form);
      const attempt = (this.formAttempts.get(key) || 0) + 1;
      this.formAttempts.set(key, attempt);
      if (attempt < 2) return;
      this.signal("form_retry", {
        form_id: form.id || form.name || undefined,
        attempt,
        reason: "resubmit",
      });
    });

    addEventListener(
      "invalid",
      (event) => {
        const field = event.target as HTMLElement | null;
        const form = field?.closest?.("form") as HTMLFormElement | null;
        const key = form ? this.formKey(form) : "detached";
        const now = elapsed();
        if (
          this.lastInvalidForm.key === key &&
          now - this.lastInvalidForm.at < 1000
        )
          return;
        this.lastInvalidForm = { key, at: now };
        this.signal("form_retry", {
          form_id: form?.id || form?.name || undefined,
          field:
            (field as HTMLInputElement | null)?.name || field?.id || undefined,
          reason: "validation",
        });
      },
      true,
    );

    if (typeof PerformanceObserver === "undefined") return;
    try {
      const observer = new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) {
          const timing = entry as PerformanceEntry & {
            duration: number;
            interactionId?: number;
          };
          if ((timing.interactionId || 0) <= 0) continue;
          if (timing.duration < SLOW_INTERACTION_MS) continue;
          if (this.slowInteractionSeen >= SIGNALS_PER_PAGE) break;
          this.slowInteractionSeen += 1;
          this.signal("slow_interaction", {
            duration_ms: Math.round(timing.duration),
            interaction: entry.name,
          });
        }
      });
      observer.observe({
        type: "event",
        durationThreshold: SLOW_INTERACTION_MS,
      } as PerformanceObserverInit);
    } catch {
      /* the browser does not report event timing */
    }
  }

  private formKey(form: HTMLFormElement): string {
    return form.id || form.name || form.getAttribute("action") || "form";
  }

  /**
   * observeClickFrustration counts repeat clicks on one element and checks
   * whether a click that looked actionable actually did anything.
   */
  private observeClickFrustration(target: HTMLElement) {
    this.lastClick = {
      at: elapsed(),
      properties: this.elementProperties(target),
    };
    const signature = elementSignature(target);
    const now = elapsed();
    const burst = this.rageBurst;
    if (
      burst &&
      burst.signature === signature &&
      now - burst.at <= RAGE_CLICK_WINDOW_MS
    ) {
      burst.at = now;
      burst.count += 1;
      if (burst.count === RAGE_CLICK_THRESHOLD) {
        // Report after the burst settles so the count is the real one, not three.
        burst.timer = window.setTimeout(() => {
          this.signal("rage_click", {
            ...this.elementProperties(target),
            clicks: this.rageBurst?.count || RAGE_CLICK_THRESHOLD,
            window_ms: RAGE_CLICK_WINDOW_MS,
          });
          this.rageBurst = null;
        }, RAGE_CLICK_WINDOW_MS);
      }
      return;
    }
    if (burst?.timer) window.clearTimeout(burst.timer);
    this.rageBurst = { signature, at: now, count: 1 };
    this.checkDeadClick(target);
  }

  /**
   * checkDeadClick reports a click that changed nothing. Navigation, a DOM
   * mutation, a scroll, a focus move or a text selection all count as something
   * happening, so only a genuinely inert click is reported. One check runs at a
   * time: the observer is attached on demand and disconnected immediately.
   */
  private checkDeadClick(target: HTMLElement) {
    if (this.deadClickPending) return;
    if (typeof MutationObserver === "undefined") return;
    if (!looksClickable(target)) return;
    const anchor = target.closest?.("a") as HTMLAnchorElement | null;
    if (anchor?.href) return;
    const type = (target as HTMLInputElement).type;
    if (type === "submit" || type === "reset" || type === "file") return;
    if (target.closest?.("[data-momento-ignore-dead-click]")) return;
    this.deadClickPending = true;
    let mutated = false;
    const observer = new MutationObserver(() => {
      mutated = true;
    });
    try {
      observer.observe(document.documentElement, {
        childList: true,
        subtree: true,
        attributes: true,
        characterData: true,
      });
    } catch {
      this.deadClickPending = false;
      return;
    }
    const before = {
      url: location.href,
      scroll: typeof scrollY === "number" ? scrollY : 0,
      focus: document.activeElement,
    };
    window.setTimeout(() => {
      observer.disconnect();
      this.deadClickPending = false;
      if (mutated) return;
      if (location.href !== before.url) return;
      if ((typeof scrollY === "number" ? scrollY : 0) !== before.scroll) return;
      if (document.activeElement !== before.focus) return;
      if (window.getSelection?.()?.toString()) return;
      this.signal("dead_click", {
        ...this.elementProperties(target),
        waited_ms: DEAD_CLICK_WINDOW_MS,
      });
    }, DEAD_CLICK_WINDOW_MS);
  }

  /** reportErrorAfterClick links a failure to the action that preceded it. */
  private reportErrorAfterClick(kind: string) {
    if (!this.options?.frustrationSignals) return;
    const click = this.lastClick;
    if (!click) return;
    const since = elapsed() - click.at;
    if (since > ERROR_AFTER_CLICK_MS) return;
    this.signal("error_after_click", {
      ...click.properties,
      error_kind: kind,
      since_click_ms: since,
    });
  }

  /**
   * installSearchTracking reports site search without asking the application to
   * instrument it. A search is recognised from the query string of the results
   * page, which is how search works in nearly every application; anything that
   * searches without changing the URL can call trackSearch instead.
   */
  private installSearchTracking() {
    this.onRoute(() => {
      this.searchState = null;
      void this.detectSearchFromURL();
    });
    void this.detectSearchFromURL();
  }

  private searchParamNames(): string[] {
    const extra = this.options?.searchParams || [];
    return [...extra, ...DEFAULT_SEARCH_PARAMS];
  }

  private searchTermFromURL(): { query: string; param: string } | null {
    let params: URLSearchParams;
    try {
      params = new URLSearchParams(location.search || "");
    } catch {
      return null;
    }
    for (const name of this.searchParamNames()) {
      const value = params.get(name);
      if (value && value.trim()) return { query: value, param: name };
    }
    return null;
  }

  private async detectSearchFromURL() {
    const found = this.searchTermFromURL();
    if (!found) return;
    const count = await this.awaitResultCount();
    this.recordSearch(found.query, {
      source: "url",
      param: found.param,
      ...(count === undefined ? {} : { result_count: count }),
    });
  }

  /**
   * awaitResultCount waits briefly for the application to publish how many
   * results it rendered. The zero-result rate is the metric that makes search
   * analytics actionable and it cannot be inferred from the query, so the
   * tracker gives the page a moment to set the attribute before reporting.
   */
  private async awaitResultCount(): Promise<number | undefined> {
    const read = () => {
      const holder = document.querySelector?.(`[${RESULTS_ATTRIBUTE}]`);
      if (!holder) return undefined;
      const raw = holder.getAttribute(RESULTS_ATTRIBUTE);
      const value = Number(raw);
      return raw !== null && raw !== "" && Number.isFinite(value)
        ? Math.max(0, Math.trunc(value))
        : undefined;
    };
    const immediate = read();
    if (immediate !== undefined) return immediate;
    const attempts = Math.ceil(SEARCH_RESULT_WAIT_MS / SEARCH_RESULT_POLL_MS);
    for (let attempt = 0; attempt < attempts; attempt += 1) {
      await new Promise((resolve) =>
        window.setTimeout(resolve, SEARCH_RESULT_POLL_MS),
      );
      const value = read();
      if (value !== undefined) return value;
    }
    return undefined;
  }

  /**
   * recordSearch reports one search and, when it follows another closely, says
   * whether the person repeated the same words or refined them. Those are
   * different problems: a repeat means the results were not usable, a refinement
   * means the first wording was not specific enough.
   */
  private recordSearch(rawQuery: string, extra: Properties) {
    const normalized = rawQuery
      .replace(/\s+/g, " ")
      .trim()
      .toLowerCase()
      .slice(0, 100);
    if (!normalized) return;
    const now = elapsed();
    const previous = this.searchState;
    if (previous && previous.query === normalized && now - previous.at < 2000)
      return;
    this.searchState = { query: normalized, at: now };
    const words = normalized.split(" ").filter(Boolean);
    const props: Properties = {
      ...extra,
      query_length: normalized.length,
      query_words: words.length,
    };
    if (this.options?.collectSearchTerms)
      props.query = redactPII(normalized, 100);
    this.track("search", props);
    if (!previous || now - previous.at > REPEATED_SEARCH_WINDOW_MS) return;
    const refined =
      previous.query !== normalized &&
      (normalized.startsWith(previous.query) ||
        previous.query.startsWith(normalized));
    if (previous.query === normalized)
      this.signal("repeated_search", {
        seconds_since: Math.round((now - previous.at) / 1000),
        ...(this.options?.collectSearchTerms
          ? { query: redactPII(normalized, 100) }
          : {}),
      });
    else if (refined)
      this.track("search_refine", {
        seconds_since: Math.round((now - previous.at) / 1000),
        direction:
          normalized.length > previous.query.length ? "narrowed" : "widened",
        ...(this.options?.collectSearchTerms
          ? {
              query: redactPII(normalized, 100),
              previous_query: redactPII(previous.query, 100),
            }
          : {}),
      });
  }

  /**
   * observeSearchClick reports which result a person opened. Position matters
   * more than the count: clicks that land far down the list say the ranking is
   * wrong even when the search looks successful.
   */
  private observeSearchClick(
    target: HTMLElement,
    anchor: HTMLAnchorElement | null,
  ) {
    if (!anchor?.href) return;
    const container = target.closest?.(
      `[${RESULTS_ATTRIBUTE}]`,
    ) as HTMLElement | null;
    if (!container && !this.searchState) return;
    const marked = target.closest?.(`[${POSITION_ATTRIBUTE}]`);
    let position: number | undefined;
    if (marked) {
      const value = Number(marked.getAttribute(POSITION_ATTRIBUTE));
      if (Number.isFinite(value)) position = Math.trunc(value);
    } else if (container) {
      const links = Array.prototype.slice.call(
        container.querySelectorAll("a[href]"),
      );
      const index = links.indexOf(anchor);
      if (index >= 0) position = index + 1;
    }
    this.track("search_click", {
      url: sanitizedURL(anchor.href),
      ...(position === undefined ? {} : { position }),
    });
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
  const endpoint = resolveEndpoint(
    script.dataset.endpoint,
    script.src,
    location.origin,
  );
  tracker.init({
    siteId: script.dataset.siteId,
    endpoint,
    mode: (script.dataset.mode as TrackingMode) || "full",
    environment: script.dataset.environment || "prd",
    contractVersion: Number(script.dataset.contractVersion || 1),
    debug: script.dataset.debug === "true",
    collectElementText: script.dataset.collectElementText === "true",
    // The site's session timeout is configured in the console; sessionization
    // happens here, so the snippet is how that setting reaches the tracker.
    sessionTimeoutMinutes: Number(script.dataset.sessionTimeout) || undefined,
    autoRUM: script.dataset.autoRum !== "false",
    frustrationSignals: script.dataset.frustrationSignals !== "false",
    searchTracking: script.dataset.searchTracking !== "false",
    collectSearchTerms: script.dataset.collectSearchTerms === "true",
    searchParams: script.dataset.searchParams
      ? script.dataset.searchParams
          .split(",")
          .map((name) => name.trim())
          .filter(Boolean)
      : undefined,
    appVersion: script.dataset.appVersion,
    releaseVersion: script.dataset.releaseVersion,
    gitSha: script.dataset.gitSha,
    deploymentId: script.dataset.deploymentId,
  });
}
