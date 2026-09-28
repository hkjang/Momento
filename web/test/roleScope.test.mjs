import assert from "node:assert/strict";
import test from "node:test";
import {
  ROLE_ORDER,
  assignableRoles,
  canAdministerRole,
  roleRank,
  selfAccountLimits,
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

// ---------------------------------------------------------------------------
// selfAccountLimits — updateUser(internal/httpapi/admin.go) 의 자기 계정 분기
// (SELF_DISABLE·SELF_ROLE·SELF_PASSWORD) 를 화면이 미리 거울로 보여주는지.
// ---------------------------------------------------------------------------

/**
 * updateUser 의 자기 계정 분기 세 개를 그대로 옮긴 복제본.
 *   SELF_DISABLE: id == p.ID && in.Active != nil && !*in.Active
 *   SELF_ROLE:    id == p.ID && current != in.Role
 *   SELF_PASSWORD:id == p.ID && in.Password != ""
 * 화면이 서버와 갈라지면 아래 순회가 잡는다.
 */
const serverRefuses = (kind, callerId, targetId, req) => {
  const isSelf = callerId === targetId;
  if (!isSelf) return false;
  if (kind === "active") return req.wantActive === false;
  if (kind === "role") return req.newRole !== req.currentRole;
  if (kind === "password") return req.password !== "";
  throw new Error(`알 수 없는 분기: ${kind}`);
};

const ME = "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d";
const OTHER = "0fedcba9-8765-4321-0fed-cba987654321";

test("자기 계정을 편집할 때는 역할·중지·비밀번호 셋 다 막힌다", () => {
  assert.deepEqual(selfAccountLimits(ME, ME), {
    canChangeRole: false,
    canDeactivate: false,
    canResetPassword: false,
  });
});

test("남의 계정을 편집할 때는 셋 다 열려 있다", () => {
  assert.deepEqual(selfAccountLimits(ME, OTHER), {
    canChangeRole: true,
    canDeactivate: true,
    canResetPassword: true,
  });
});

test("자기/남 두 경우에 세 제약이 서버 분기와 일치한다", () => {
  for (const target of [ME, OTHER]) {
    const limits = selfAccountLimits(ME, target);
    // 역할: 지금과 다른 역할을 고르는 것이 서버에서 거절되면 select 를 열지 않는다.
    for (const currentRole of ROLE_ORDER) {
      for (const newRole of ROLE_ORDER) {
        if (newRole === currentRole) continue;
        assert.equal(
          limits.canChangeRole,
          !serverRefuses("role", ME, target, { currentRole, newRole }),
          `target=${target} ${currentRole}→${newRole}`,
        );
      }
      // 같은 역할을 그대로 보내는 것은 자기 계정이라도 서버가 받는다 —
      // 그래서 select 를 disabled 로 두어도 저장 자체는 막히지 않는다.
      assert.equal(
        serverRefuses("role", ME, target, {
          currentRole,
          newRole: currentRole,
        }),
        false,
        `target=${target} ${currentRole} 유지`,
      );
    }
    // 활성: 서버는 끄는 것만 막는다. 켜는 것은 자기 계정이라도 거절하지 않으므로
    // 체크박스를 통째로 잠그면 서버보다 좁아진다.
    assert.equal(
      limits.canDeactivate,
      !serverRefuses("active", ME, target, { wantActive: false }),
      `target=${target} 중지`,
    );
    assert.equal(
      serverRefuses("active", ME, target, { wantActive: true }),
      false,
      `target=${target} 활성화`,
    );
    // 비밀번호: 값이 있을 때만 서버가 본다.
    assert.equal(
      limits.canResetPassword,
      !serverRefuses("password", ME, target, { password: "s3cret-passphrase" }),
      `target=${target} 비밀번호`,
    );
    assert.equal(
      serverRefuses("password", ME, target, { password: "" }),
      false,
      `target=${target} 빈 비밀번호`,
    );
  }
});

test("호출자를 모르면 제약을 걸지 않는다", () => {
  // 세션이 아직 확정되지 않은 구간에서 useAuth().user 는 null 이라 undefined 가
  // 들어온다. 주체 없이는 서버가 이 라우트에 닿지 않으므로 화면이 먼저 잠글
  // 이유가 없고, 잠그면 남의 계정까지 잘못 막는다.
  assert.deepEqual(selfAccountLimits(undefined, ME), {
    canChangeRole: true,
    canDeactivate: true,
    canResetPassword: true,
  });
  assert.deepEqual(selfAccountLimits(undefined, ""), {
    canChangeRole: true,
    canDeactivate: true,
    canResetPassword: true,
  });
});

test("자기 판정은 id 만 보고 대소문자·공백을 봐주지 않는다", () => {
  // 서버는 id == p.ID 한 번뿐이라 화면도 같은 엄밀함이어야 한다.
  assert.equal(selfAccountLimits(ME, ME.toUpperCase()).canChangeRole, true);
  assert.equal(selfAccountLimits(ME, ` ${ME}`).canChangeRole, true);
});
