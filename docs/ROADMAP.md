# Delivery scope

## v0.1 — implemented

- Phase 1 event protocol, browser SDK, durable collector/worker, PostgreSQL Raw Event
- Visitor/session IDs, realtime, overview, acquisition, page, event, device and traffic dimensions
- Conversion registry, privacy/retention baseline, delete API, User/RBAC, audit and tracking debugger
- Organization/workspace/site schema and workspace-scoped report access
- Internal usage dimensions: organization, department, service, feature, button and CIDR network
- Generic query API, funnel, path, CSV/NDJSON export, personal API keys and MCP
- Local login and Keycloak-compatible OIDC Discovery + Authorization Code/PKCE
- Offline single-image release workflow

## v0.2 — implemented

- Site-specific Raw Event, Session, Realtime and Debug retention policy editor
- Materialized Session summaries with migration-time Raw Event backfill
- Segment registry with nested AND/OR groups, 14 operators, personal/shared ownership and preview
- Segment-aware Query Builder and open/closed Funnel with per-step property conditions and completion window
- Saved Exploration registry
- Custom Dimension Registry for User, Session, Event and Item scopes
- Ecommerce revenue, refund, transaction, product and commerce funnel reports
- Privacy-controlled Visitor Timeline and Session reporting APIs

## v0.3 — implemented

- Event occurrence-time Context snapshot and first-touch acquisition preservation for SPA journeys
- Fail-closed consent-required state even without browser storage; privacy-first DOM text and Error defaults
- User/Session conversion rates and configurable engaged-session semantics
- Site IANA Timezone applied consistently to API, UI, exports, deletion and MCP calendar ranges
- Active engagement milliseconds, heartbeat count and interaction count in materialized Sessions
- Worker savepoint isolation with durable retry/dead-letter bookkeeping
- Queue-aware privacy deletion and Raw Event-driven Session reconciliation

## v0.4 — implemented

- Deterministic SSO/identify Visitor Identity Graph without fingerprinting
- Collision-safe canonical user/entity read model across Query, Segment, Funnel, Ecommerce and MCP
- Canonical User Profile propagation for department, organization and User-scope custom dimensions
- Materialized Visitor, identified User, Visitor-Session and Site-local Daily summaries
- Automatic Raw Event backfill for upgrades from v0.3 and fresh installations
- Daily-aggregate Overview trends with partial-day Raw Event fallback
- Privacy-controlled Identity Graph REST/MCP/User Explorer experience
- Linked anonymous/device Visitor deletion and complete derived-data reconciliation
- Atomic daily aggregate rebuild on Site Timezone changes

## Next schema-compatible increments

- Cohort/retention and first/last/last-non-direct attribution
- Scheduled aggregate validation/rebuild jobs and Parquet export
- ClickHouse sink selected from administrator storage settings once event volume requires it
- Optional Kafka/Redpanda transport and separate collector/worker deployments for 10k+ EPS validation
- Mobile/server SDK packages, anomaly alerts and AI-assisted diagnosis

Raw Event remains the source of truth so these increments do not require changing the tracking protocol.
