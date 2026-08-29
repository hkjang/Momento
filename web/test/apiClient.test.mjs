import test from "node:test";
import assert from "node:assert/strict";

import { APIError, api } from "../src/api/client.ts";

// Every request the console makes goes through api(). It read the body with
// `response.json().catch(() => ({}))` and returned that — so a 200 carrying
// anything but JSON became an object with no fields in it, and every number read
// off it was undefined. The screen then drew the empty state it draws for a site
// that has not sent any events.
//
// A proxy's error page, a gateway's maintenance page, a sign-in redirect in
// front of the service: all of them answer 200 with HTML, and on-premise is
// exactly where something sits in front of the service.
function withFetch(reply, run) {
  const real = globalThis.fetch;
  const realStorage = globalThis.localStorage;
  const realLocation = globalThis.location;
  globalThis.localStorage = {
    getItem: () => null,
    setItem() {},
    removeItem() {},
  };
  // api() resolves a relative url against the page origin to add the selected
  // environment, so the harness needs one.
  globalThis.location = new URL("https://console.internal/");
  globalThis.fetch = async () => reply;
  return run().finally(() => {
    globalThis.fetch = real;
    globalThis.localStorage = realStorage;
    globalThis.location = realLocation;
  });
}

function reply(status, body, ok = status < 400) {
  return { status, ok, text: async () => body };
}

test("JSON이 아닌 200은 성공이 아니다", async () => {
  await withFetch(reply(200, "<html><body>502 Bad Gateway</body></html>"), async () => {
    await assert.rejects(
      () => api("/api/v1/sites/SITE/overview"),
      (error) => {
        assert.ok(error instanceof APIError, "APIError가 아니다");
        assert.equal(
          error.code,
          "RESPONSE_NOT_JSON",
          "JSON이 아닌 본문이 빈 객체로 화면에 도착한다: 데이터가 없는 사이트와 구분되지 않는다",
        );
        return true;
      },
    );
  });
});

test("정상 응답과 빈 응답은 그대로 통과한다", async () => {
  await withFetch(reply(200, '{"users":12}'), async () => {
    assert.deepEqual(await api("/api/v1/sites/SITE/overview"), { users: 12 });
  });
  // 204는 본문이 없는 성공이므로 undefined로 답해야 한다.
  await withFetch({ status: 204, ok: true, text: async () => "" }, async () => {
    assert.equal(await api("/api/v1/sites/SITE/thing"), undefined);
  });
});

test("서버가 준 오류 코드와 메시지를 그대로 전한다", async () => {
  const payload = JSON.stringify({
    error: { code: "QUERY_TIMEOUT", message: "너무 넓습니다" },
  });
  await withFetch(reply(504, payload), async () => {
    await assert.rejects(
      () => api("/api/v1/sites/SITE/overview"),
      (error) => {
        assert.equal(error.code, "QUERY_TIMEOUT");
        assert.equal(error.message, "너무 넓습니다");
        return true;
      },
    );
  });
});

test("오류 응답이 JSON이 아니면 상태 코드로 말한다", async () => {
  // 앞단이 502 HTML을 돌려줄 때, 상태 코드가 유일한 정보다.
  await withFetch(reply(502, "<html>bad gateway</html>"), async () => {
    await assert.rejects(
      () => api("/api/v1/sites/SITE/overview"),
      (error) => {
        assert.equal(error.code, "REQUEST_FAILED");
        assert.match(error.message, /502/);
        return true;
      },
    );
  });
});
