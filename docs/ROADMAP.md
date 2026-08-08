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

## Next schema-compatible increments

- Site-specific retention policy editor and scheduled aggregate rebuild jobs
- Segment registry with nested AND/OR UI, property-aware funnel conditions and saved explorations
- Ecommerce-specific reports, User Timeline, custom dimensions and Parquet export
- Cohort/retention and first/last/last-non-direct attribution
- ClickHouse sink selected from administrator storage settings once event volume requires it
- Optional Kafka/Redpanda transport and separate collector/worker deployments for 10k+ EPS validation
- Mobile/server SDK packages, anomaly alerts and AI-assisted diagnosis

Raw Event remains the source of truth so these increments do not require changing the tracking protocol.
