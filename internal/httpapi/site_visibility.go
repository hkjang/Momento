package httpapi

// The membership rule: which sites a caller may see, and which they may
// administer.
//
// resolveSite and resolveSiteByID apply it for a request that names one site in
// its path, and resolveSiteByID's own comment records what it cost to have that
// decision made per handler — a workspace_admin whose only membership was one
// workspace could rename, rotate the keys of and delete a site in another,
// because four of six handlers had decided for themselves.
//
// Eight more queries could not call those helpers. They list across every site a
// caller can see, where the site filter is optional, or they delete a row
// addressed by its own id and reach the site through a join. So they wrote the
// predicate out by hand, and it is the same authorization decision written nine
// times. They agree today. Nothing makes them agree tomorrow, and a copy that
// drifts does not fail — it answers, with somebody else's data in it.
//
// alias is how the sites table is named in the query. roleArg and userArg are
// the placeholders carrying the caller's role and id.

// siteVisibleTo is true for a site the caller may read: an instance-wide
// administrator, or a member of the workspace that owns it.
func siteVisibleTo(alias, roleArg, userArg string) string {
	return "(" + roleArg + " IN ('super_admin','organization_admin') OR EXISTS(SELECT 1 FROM user_workspace_roles uwr WHERE uwr.workspace_id=" +
		alias + ".workspace_id AND uwr.user_id=" + userArg + "))"
}

// siteAdministeredBy is true for a site the caller may change: an instance-wide
// administrator, or a workspace_admin of the workspace that owns it.
//
// Deleting somebody else's saved segment or report takes this one. Ownership is
// the resource's own business, so the caller ORs that in.
func siteAdministeredBy(alias, roleArg, userArg string) string {
	return "(" + roleArg + " IN ('super_admin','organization_admin') OR EXISTS(SELECT 1 FROM user_workspace_roles uwr WHERE uwr.workspace_id=" +
		alias + ".workspace_id AND uwr.user_id=" + userArg + " AND uwr.role='workspace_admin'))"
}
