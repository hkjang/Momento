import assert from "node:assert/strict";
import test from "node:test";
import {
  ROLE_ORDER,
  assignableRoles,
  canAdministerRole,
  roleRank,
} from "../src/pages/roleScope.ts";

// internal/auth/auth.go:239 그대로 옮긴 것. 화면이 서버와 어긋나면 이 복제본과
// canAdministerRole 이 갈라지므로 아래 5×5 순회가 잡는다.
const SERVER_RANK = {
  viewer: 1,
  analyst: 2,
  workspace_admin: 3,
  organization_admin: 4,
  super_admin: 5,
};
const serverRoleAbove = (role, callerRole) =>
  (SERVER_RANK[role] ?? 0) > (SERVER_RANK[callerRole] ?? 0);

test("서열은 서버 roleRank 와 같은 순서다", () => {
  assert.deepEqual(ROLE_ORDER, [
    "viewer",
    "analyst",
    "workspace_admin",
    "organization_admin",
    "super_admin",
  ]);
  for (const role of ROLE_ORDER) {
    assert.equal(roleRank(role), SERVER_RANK[role]);
  }
});

test("모르는 역할의 등급은 서버와 같이 0 이다", () => {
  assert.equal(roleRank(""), 0);
  assert.equal(roleRank("root"), 0);
  assert.equal(roleRank("SUPER_ADMIN"), 0);
});

test("역할 5개 × 호출자 5개에서 서버 RoleAbove 의 정확한 반대다", () => {
  for (const caller of ROLE_ORDER) {
    for (const target of ROLE_ORDER) {
      assert.equal(
        canAdministerRole(caller, target),
        !serverRoleAbove(target, caller),
        `caller=${caller} target=${target}`,
      );
    }
  }
});

test("모르는 호출자 역할은 아무 계정도 편집하지 못한다", () => {
  for (const target of [...ROLE_ORDER, "", "root"]) {
    assert.equal(canAdministerRole("", target), false, `target=${target}`);
    assert.equal(canAdministerRole("root", target), false, `target=${target}`);
  }
});

test("모르는 대상 역할은 서버처럼 등급 0 으로 다룬다", () => {
  // 서버 RoleAbove("root", "viewer") 는 0 > 1 = false 라 막지 않는다.
  assert.equal(canAdministerRole("viewer", "root"), true);
  assert.equal(canAdministerRole("super_admin", ""), true);
});

test("권한 목록은 오름차순이고 호출자 역할까지만 담는다", () => {
  for (const caller of ROLE_ORDER) {
    const list = assignableRoles(caller);
    assert.deepEqual(
      list,
      [...list].sort((a, b) => roleRank(a) - roleRank(b)),
      `오름차순이 아님: caller=${caller}`,
    );
    assert.ok(list.includes(caller), `호출자 역할 누락: caller=${caller}`);
    for (const role of list) {
      assert.ok(
        roleRank(role) <= roleRank(caller),
        `caller=${caller} 보다 높은 ${role} 이 들어 있다`,
      );
      assert.equal(canAdministerRole(caller, role), true);
    }
    for (const role of ROLE_ORDER) {
      if (roleRank(role) > roleRank(caller)) {
        assert.ok(!list.includes(role), `caller=${caller} 에 ${role} 이 보인다`);
      }
    }
  }
});

test("super_admin 에게는 다섯 역할이 모두 남는다", () => {
  assert.deepEqual(assignableRoles("super_admin"), ROLE_ORDER);
  assert.deepEqual(assignableRoles("organization_admin"), [
    "viewer",
    "analyst",
    "workspace_admin",
    "organization_admin",
  ]);
  assert.deepEqual(assignableRoles("viewer"), ["viewer"]);
});

test("모르는 호출자 역할에는 고를 권한이 하나도 없다", () => {
  assert.deepEqual(assignableRoles(""), []);
  assert.deepEqual(assignableRoles("root"), []);
});

test("목록을 고쳐도 ROLE_ORDER 원본은 그대로다", () => {
  const list = assignableRoles("super_admin");
  list.pop();
  assert.equal(ROLE_ORDER.length, 5);
  assert.equal(assignableRoles("super_admin").length, 5);
});
