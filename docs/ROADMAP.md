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

## Next schema-compatible increments

- Scheduled aggregate rebuild jobs and Parquet export
- Cohort/retention and first/last/last-non-direct attribution
- ClickHouse sink selected from administrator storage settings once event volume requires it
- Optional Kafka/Redpanda transport and separate collector/worker deployments for 10k+ EPS validation
- Mobile/server SDK packages, anomaly alerts and AI-assisted diagnosis

Raw Event remains the source of truth so these increments do not require changing the tracking protocol.
