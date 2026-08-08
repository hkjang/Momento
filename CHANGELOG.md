# Changelog

## v0.4.0

- Added a deterministic Visitor Identity Graph that links anonymous and authenticated Visitor IDs through pseudonymous `user_id` values without fingerprinting.
- Added canonical User profiles so historical anonymous events inherit the identified user's latest User-scope department, organization, and custom dimensions while Raw Events remain immutable.
- Applied canonical identity to Overview, Realtime, Events, Pages, internal usage, Query Builder, Segment event-existence rules, Funnel, Ecommerce, Session reports, and Analytics MCP.
- Added `visitors`, `identified_users`, `visitor_identities`, `visitor_sessions`, `daily_site_metrics`, `daily_site_visitors`, and `daily_site_sessions` PostgreSQL summaries with automatic v0.3 Raw Event backfill.
- Added an `analytics_events` read model with collision-safe `u:`/`v:` entity identifiers and canonical user properties.
- Added the privacy-controlled Identity Graph REST report, User Explorer graph UI, linked-Visitor navigation, and `query_identity_graph` MCP tool.
- Accelerated Site-local Overview trends with daily aggregates while retaining exact Raw Event fallback for partial-day ranges.
- Rebuilt daily aggregates atomically when a Site timezone changes.
- Expanded User ID privacy deletion to every linked anonymous/device Visitor and atomically rebuilt all affected derived data.

## v0.3.0

- Captured page, device, and first-touch acquisition context at event occurrence time so SPA route changes cannot rewrite queued event context.
- Made `consent-required` fail closed even when browser storage is unavailable, while preserving an in-memory consent grant and the original acquisition context.
- Changed automatic DOM text collection to opt-in, sanitized common PII patterns in browser error messages, and removed query strings and fragments from SDK URLs by default.
- Changed the server privacy baseline to strip URL query strings before the durable inbox write.
- Added user and session conversion counts/rates; retained `conversion_rate` as a compatibility alias for user conversion rate.
- Defined engaged sessions as threshold duration, a conversion, two page views, or sufficient active engagement, and materialized active milliseconds, heartbeat count, and interaction count.
- Added per-site IANA timezone and engagement-threshold settings with an administrator editor and immediate Session summary reconciliation.
- Applied site-local calendar boundaries to dashboard, reports, Query, Funnel, exports, privacy deletion, and MCP date ranges.
- Isolated Worker inbox jobs with PostgreSQL savepoints so a failed event cannot abort retry/dead-letter bookkeeping or block valid jobs in the same batch.
- Made privacy deletion atomically scrub Inbox/Dead Letter payloads, update Raw Events, and rebuild Session summaries.

## v0.2.0

- Added nested AND/OR Segment Registry with personal and shared ownership.
- Applied saved or inline segments to Query Builder and Funnel analysis.
- Added open/closed Funnel modes, per-step property conditions, and completion windows.
- Added saved Exploration definitions and Custom Dimension Registry.
- Added Ecommerce summary, product performance, and commerce funnel reports.
- Added privacy-controlled Visitor Timeline and materialized Session reports.
- Added per-site retention policies and migration-time Session backfill.
- Extended Analytics MCP with Ecommerce and Segment tools.

## v0.1.0

- Initial on-premise event analytics MVP.
- PostgreSQL durable inbox and Raw Event storage, JavaScript SDK, React console, Keycloak OIDC, RBAC, privacy, REST API, and MCP.
- Offline Docker image release workflow.
