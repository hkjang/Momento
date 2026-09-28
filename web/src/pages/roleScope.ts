// The role hierarchy the server applies to user administration, so the console
// can stop showing operations it already knows will be refused. The authority is
// auth.RoleAbove (internal/auth/auth.go:239) over roleRank (auth.go:229), which
// is what createUser and updateUser (internal/httpapi/admin.go) answer
// 403 ROLE_ABOVE_CALLER from. This is a mirror of that rule, never a substitute:
// the server check stays the boundary, and must not be loosened because the
// console now hides the operation.

// Same order as the server's roleRank, so rank is the index plus one and an
// unknown role gets 0 exactly as the Go map's zero value does.
export const ROLE_ORDER = [
  "viewer",
  "analyst",
  "workspace_admin",
  "organization_admin",
  "super_admin",
] as const;

export function roleRank(role: string): number {
  return ROLE_ORDER.indexOf(role as (typeof ROLE_ORDER)[number]) + 1;
}

/**
 * Whether `callerRole` may administer an account currently holding
 * `targetRole` — the inverse of the server's `RoleAbove(targetRole, callerRole)`.
 *
 * A caller whose own role is unknown gets false rather than the server's answer:
 * the console would be guessing about the person holding it, and refusing to
 * offer an edit is the harmless direction. An unknown *target* role is ranked 0
 * like the server ranks it, so it stays administrable.
 */
export function canAdministerRole(
  callerRole: string,
  targetRole: string,
): boolean {
  const caller = roleRank(callerRole);
  if (caller === 0) return false;
  return roleRank(targetRole) <= caller;
}

/** The roles a caller may hand out, lowest first. Never more than their own. */
export function assignableRoles(callerRole: string): string[] {
  return ROLE_ORDER.filter((role) => canAdministerRole(callerRole, role));
}

/**
 * The three things `updateUser` (internal/httpapi/admin.go) refuses to do to
 * the account making the request, each answered 400:
 *   SELF_DISABLE  — `id == p.ID && in.Active != nil && !*in.Active`
 *   SELF_ROLE     — `id == p.ID && current != in.Role`
 *   SELF_PASSWORD — `id == p.ID && in.Password != ""`
 * All three turn on the same question, so the console asks it once here rather
 * than comparing ids at each control, where they had already drifted apart.
 *
 * Note what the server does *not* refuse, and this must not either: sending an
 * unchanged role, turning one's own account back on, or leaving the password
 * field empty. A mirror wider than the rule is its own bug.
 */
export interface SelfAccountLimits {
  canChangeRole: boolean;
  canDeactivate: boolean;
  canResetPassword: boolean;
}

export function selfAccountLimits(
  callerId: string | undefined,
  targetId: string,
): SelfAccountLimits {
  // No caller id means the session is not settled yet (`useAuth().user` is
  // null), and nothing is limited: the server never reaches this route without
  // a subject, so guessing here would only mislock somebody else's account.
  const self = callerId !== undefined && callerId === targetId;
  return {
    canChangeRole: !self,
    canDeactivate: !self,
    canResetPassword: !self,
  };
}
