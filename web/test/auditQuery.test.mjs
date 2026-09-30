import assert from "node:assert/strict";
import test from "node:test";
import {
  auditDetailText,
  auditPath,
  emptyAuditFilters,
  nextAuditCursor,
} from "../src/pages/auditQuery.ts";

test("비어 있는 필터는 주소에 싣지 않고 값은 인코딩한다", () => {
  assert.equal(auditPath(emptyAuditFilters), "/api/v1/audit?limit=200");
  assert.equal(
    auditPath({ action: " site.key ", actor: "", resource: "a&b" }, 42, 50),
    "/api/v1/audit?action=site.key&resource=a%26b&limit=50&before_id=42",
  );
});

test("꽉 찬 페이지 뒤에만 다음 페이지가 있고, 가장 작은 id 에서 이어진다", () => {
  const full = [{ id: 9 }, { id: 8 }, { id: 7 }];
  assert.equal(nextAuditCursor(full, 3), 7);
  assert.equal(nextAuditCursor(full.slice(0, 2), 3), undefined);
  assert.equal(nextAuditCursor([{ id: "x" }, { id: null }, { id: 0 }], 3), undefined);
});

test("상세는 짧은 JSON 이고 빈 객체는 비어 있다", () => {
  assert.equal(auditDetailText(null), "");
  assert.equal(auditDetailText({}), "");
  assert.equal(auditDetailText({ role: "viewer" }), '{"role":"viewer"}');
  assert.equal(auditDetailText("text"), "text");
});
