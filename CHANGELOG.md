# Changelog

## v0.7.0

- Reorganized the console into role-aware, collapsible navigation groups for monitoring, web, product, exploration, experience, and AI analytics.
- Added a global `Ctrl/Cmd+K` command palette for pages, analytics functions, and permission-aware administration shortcuts.
- Added persistent breadcrumbs, site/environment context, responsive mobile navigation, and clearer active-location cues.
- Rebuilt Administration as a task-oriented control center with a summary home, grouped sticky navigation, mobile selector, and shareable `?section=` deep links.
- Split Analytics Engineering into Metric/Goal, Query Cost, Aggregate, Change Calendar, and Catalog/Lineage workspaces; split Product Lab into Feature Flag and Experiment workspaces.
- Upgraded shared data tables with search, pagination, result counts, responsive overflow, and UTF-8 CSV export.
- Replaced generic loading and empty states with contextual skeletons, retry guidance, and setup actions.
- Added explicit confirmation UX for full aggregate rebuilds and privacy request decisions, plus clearer required-field and mutation feedback.
- Refined the MUI theme, focus visibility, cards, dialogs, tooltips, reduced-motion behavior, and responsive spacing for an accessible enterprise console.
- Preserved all v0.6.0 API, database, tracking protocol, SDK, privacy, and three-environment-variable runtime contracts.

## v0.6.0

- Added Formula-capable Semantic Metrics with metric references, user/session/event property filters, minimum occurrence rules, owners, scopes, tags, and shared evaluation across REST, Explorer, Goals, and MCP.
- Added occurrence-time Session Property snapshots across SDK, Collector, Raw Events, materialized Sessions, PII filtering, Semantic Metric session scope, and deterministic Raw Event rebuilds.
- Added Metric Goals with target/comparator/period/environment/organization/department ownership and live achievement evaluation.
- Added deterministic Exact/Fast/Preview query modes, complexity scoring, sampling policy, cost rejection, execution metadata, and Query Audit history.
- Added Event Contract CI validation, Event Catalog usage health, Data Lineage from Event to Metric to Goal, and explicit data ownership metadata.
- Added value-based PII detection before the durable inbox with `detect`, `warn`, `mask`, and `reject` policies. Detector samples never retain matching secrets.
- Added Late Event detection and deduplicated automatic date-range aggregate rebuild jobs, plus administrator-requested date-range and full rebuilds.
- Added Cardinality health levels (`low`, `medium`, `high`, `extreme`) and Query Builder eligibility guidance.
- Added Workspace Roll-Up and cross-site Business Journeys using deterministic SSO identity while keeping anonymous Visitors site-scoped.
- Added Service Score, Feature Score, adoption/repeat/conversion/error signals, trend comparison, and Dead Feature candidates.
- Added first-class Search Analytics for Zero Result, CTR, refinement, exit and success; and privacy-preserving Frustration Analytics for rage/dead clicks, retries, errors, and slow interactions.
- Added Feature Flag and Experiment registries with Variant population, Semantic primary metrics, Lift, and two-proportion confidence estimates.
- Added Change Calendar annotations for deployment, release, incident, campaign, training, feature flag and organization changes.
- Added audited Privacy Request workflow for delete/export authorization with separate request and approval steps and complete NDJSON downloads.
- Expanded Analytics MCP from 13 to 19 tools with Workspace Roll-Up, Feature Score, Search, Frustration, Metric Goals, and Event Catalog access.
- Added dedicated React workspaces for Enterprise Analytics, Analytics Engineering, Product Lab, and Privacy Requests.
- Verified both clean PostgreSQL 17 installation and in-place v0.5.0 to v0.6.0 migration while retaining exactly three required runtime environment variables.

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
