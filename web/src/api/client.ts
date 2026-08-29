export interface User {
  id: string;
  email: string;
  display_name: string;
  department: string;
  organization_name: string;
  role: string;
}
export interface Site {
  id: string;
  site_id: string;
  name: string;
  service_name: string;
  allowed_domains: string[];
  session_timeout_minutes: number;
  timezone: string;
  engagement_threshold_seconds: number;
  active: boolean;
  tracking_key_prefix: string;
  server_api_key_prefix: string;
  workspace: string;
  organization: string;
  created_at: string;
  /** Longest period this site's query policy allows an interactive read to cover. */
  max_exact_days?: number;
}

export interface SiteEnvironment {
  name: string;
  label: string;
  contract_mode: "allow" | "warn" | "reject";
  cardinality_limit: number;
  active: boolean;
}

export class APIError extends Error {
  // Declared and assigned rather than written as constructor parameter
  // properties: those are a TypeScript-only construct, and the test runner
  // strips types without compiling them, so a test that imports this module
  // fails to parse it.
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

export async function api<T>(url: string, init?: RequestInit): Promise<T> {
  let target = url;
  const method = init?.method || "GET";
  if (method === "GET" && url.includes("/api/v1/sites/")) {
    const environment =
      localStorage.getItem("momento:selected-environment") || "prd";
    const parsed = new URL(url, location.origin);
    if (!parsed.searchParams.has("environment"))
      parsed.searchParams.set("environment", environment);
    target = `${parsed.pathname}${parsed.search}${parsed.hash}`;
  }
  const response = await fetch(target, {
    credentials: "same-origin",
    ...init,
    headers: {
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...init?.headers,
    },
  });
  if (response.status === 204) return undefined as T;
  const text = await response.text();
  let body: { error?: { code?: string; message?: string } } | undefined;
  if (text) {
    try {
      body = JSON.parse(text);
    } catch {
      body = undefined;
    }
  }
  if (!response.ok)
    throw new APIError(
      response.status,
      body?.error?.code || "REQUEST_FAILED",
      body?.error?.message || `HTTP ${response.status}`,
    );
  // A success whose body is not JSON is not a success.
  //
  // This used to fall back to {} for anything unparseable, and return it — so a
  // 200 carrying an HTML page, which is what a proxy or a sign-in redirect in
  // front of the service answers with, reached the screen as an object with no
  // fields in it. Every number read off it was undefined, and the screen drew
  // the same empty state it draws for a site that has not sent any events.
  // On-premise is exactly where something sits in front of the service.
  if (body === undefined)
    throw new APIError(
      response.status,
      "RESPONSE_NOT_JSON",
      `서버가 JSON이 아닌 응답을 보냈습니다 (${response.status}).`,
    );
  return body as T;
}

export const get = <T>(url: string) => api<T>(url);
export const post = <T>(url: string, body?: unknown) =>
  api<T>(url, {
    method: "POST",
    body: body === undefined ? undefined : JSON.stringify(body),
  });
export const put = <T>(url: string, body: unknown) =>
  api<T>(url, { method: "PUT", body: JSON.stringify(body) });
export const patch = <T>(url: string, body: unknown) =>
  api<T>(url, { method: "PATCH", body: JSON.stringify(body) });
export const del = <T>(url: string) => api<T>(url, { method: "DELETE" });

export function dateRangeValues(days = 30, timezone = "UTC") {
  let parts: Intl.DateTimeFormatPart[];
  try {
    parts = new Intl.DateTimeFormat("en-US", {
      timeZone: timezone,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    }).formatToParts(new Date());
  } catch {
    parts = new Intl.DateTimeFormat("en-US", {
      timeZone: "UTC",
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    }).formatToParts(new Date());
  }
  const value = (type: Intl.DateTimeFormatPartTypes) =>
    Number(parts.find((part) => part.type === type)?.value || 0);
  const year = value("year");
  const month = value("month");
  const day = value("day");
  const fromDate = new Date(Date.UTC(year, month - 1, day - (days - 1)));
  const date = (dateValue: Date) =>
    `${dateValue.getUTCFullYear()}-${String(dateValue.getUTCMonth() + 1).padStart(2, "0")}-${String(dateValue.getUTCDate()).padStart(2, "0")}`;
  return {
    from: date(fromDate),
    to: `${year}-${String(month).padStart(2, "0")}-${String(day).padStart(2, "0")}`,
  };
}

export function rangeQuery(days = 30, timezone = "UTC") {
  const { from, to } = dateRangeValues(days, timezone);
  const environment =
    localStorage.getItem("momento:selected-environment") || "prd";
  return `from=${from}&to=${to}&environment=${encodeURIComponent(environment)}`;
}
