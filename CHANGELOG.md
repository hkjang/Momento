# Changelog

## v0.5.0

- Added first-class DEV/STG/PRD and custom environment isolation across the SDK, Collector, Raw Events, daily aggregates, core reports, Funnel, Path, Export, Query API and new analytics workspaces.
- Added immutable Event Contract versions with draft/active/deprecated lifecycle and environment-specific `allow`, `warn`, or `reject` enforcement.
- Added a safe AST-based Semantic Metric Registry with definition versioning, built-in metrics, REST evaluation, Query API support and MCP access.
- Added the Data Quality Center with Tracking Health Score, inbox lag, duplicate/late/rejected events, contract warnings, PII-block counts, missing dimensions, dead letters and daily cardinality guards.
- Added weekly/daily/monthly Cohort and Retention analysis with configurable cohort and return events.
- Added reusable 2-12 step Business Journeys with sequential matching, conversion windows, overall conversion and average elapsed time.
- Added Organization/Department Feature Adoption with eligible-user targets, repeat usage, active and dormant users.
- Added automatic SDK RUM for LCP, INP, CLS, FCP, TTFB, page load and resource errors plus Error Conversion Impact and Release Impact reports.
- Added offline rule-based Insight/Root Cause detection and an offline Korean/English natural-language analytics parser with no external data transfer.
- Added standardized AI/Model/Agent/MCP/Tool analytics for calls, users, success rate, latency, tokens, cost and fallback behavior.
- Added allowlisted Scheduled Reports and Segment actions for Webhook, Confluence, Mail gateway, internal messaging and AI Agent endpoints, with write-only header values and delivery audit history.
- Expanded Analytics MCP from 5 to 13 tools for Semantic Metrics, Retention, Adoption, Experience, AI operations, Data Quality and offline questions.
- Added SDK environment-scoped Visitor/Session/Offline storage and release context fields (`app_version`, `release_version`, `git_sha`, `deployment_id`).
- Preserved v0.4 browser identity continuity by migrating legacy production Visitor, Session, Consent and Offline Queue storage into site-scoped keys.
- Kept the runtime contract at exactly three required environment variables and retained PostgreSQL Raw Events as the immutable source of truth.

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
