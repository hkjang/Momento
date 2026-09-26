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
