// Shared guard and HTTP helpers for the guide screenshot scripts.
//
// These scripts write data into a deployment. They must never be pointed at a
// real one by accident, so the target comes from variables nobody else uses and
// the host has to be local unless the operator says otherwise.

export const SITE_NAME = "데모 포털";
export const SEED_MARKER = "guide-seed";

export function readTarget() {
  const baseURL = (process.env.MOMENTO_GUIDE_BASE_URL || "").replace(/\/+$/, "");
  const email = process.env.MOMENTO_GUIDE_ADMIN_EMAIL || "";
  const password = process.env.MOMENTO_GUIDE_ADMIN_PASSWORD || "";
  if (!baseURL || !email || !password) {
    fail(
      "MOMENTO_GUIDE_BASE_URL, MOMENTO_GUIDE_ADMIN_EMAIL, MOMENTO_GUIDE_ADMIN_PASSWORD 를 설정하세요. " +
        "이 스크립트는 대상 배포에 데이터를 씁니다 — 버려도 되는 로컬 배포만 가리키세요.",
    );
  }
  const host = new URL(baseURL).hostname;
  const local = host === "localhost" || host === "127.0.0.1" || host === "::1";
  if (!local && process.env.MOMENTO_GUIDE_ALLOW_REMOTE !== "1") {
    fail(`${host} 는 로컬 호스트가 아닙니다. 정말 버려도 되는 배포라면 MOMENTO_GUIDE_ALLOW_REMOTE=1 을 함께 설정하세요.`);
  }
  return { baseURL, email, password };
}

export function fail(message) {
  console.error(message);
  process.exit(1);
}

export async function login({ baseURL, email, password }) {
  const res = await fetch(`${baseURL}/api/v1/auth/login`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ email, password }),
  });
  if (!res.ok) fail(`로그인 실패 (${res.status}): ${await res.text()}`);
  const cookie = (res.headers.get("set-cookie") || "").split(";")[0];
  if (!cookie.startsWith("momento_session=")) fail("세션 쿠키를 받지 못했습니다");
  return cookie;
}

export function api(baseURL, cookie) {
  return async (method, path, body) => {
    const res = await fetch(`${baseURL}${path}`, {
      method,
      headers: { "content-type": "application/json", cookie },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await res.text();
    let json;
    try {
      json = text ? JSON.parse(text) : null;
    } catch {
      json = text;
    }
    if (!res.ok) throw new Error(`${method} ${path} -> ${res.status}: ${text.slice(0, 300)}`);
    return json;
  };
}
