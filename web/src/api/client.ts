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
  active: boolean;
  tracking_key_prefix: string;
  server_api_key_prefix: string;
  workspace: string;
  organization: string;
  created_at: string;
}

export class APIError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
  }
}

export async function api<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, {
    credentials: "same-origin",
    ...init,
    headers: {
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...init?.headers,
    },
  });
  if (response.status === 204) return undefined as T;
  const body = await response.json().catch(() => ({}));
  if (!response.ok)
    throw new APIError(
      response.status,
      body?.error?.code || "REQUEST_FAILED",
      body?.error?.message || `HTTP ${response.status}`,
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

export function rangeQuery(days = 30) {
  const to = new Date();
  const from = new Date(to.getTime() - (days - 1) * 86400000);
  const date = (d: Date) => d.toISOString().slice(0, 10);
  return `from=${date(from)}&to=${date(to)}`;
}
