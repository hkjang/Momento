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

## v0.5 — implemented

- DEV/STG/PRD Environment isolation for SDK, Collector, Raw Events, aggregates and analytics
- Immutable Event Contract versions with environment-specific allow/warn/reject enforcement
- Safe AST-based Semantic Metric Registry shared by REST, Query API and MCP
- Data Quality Center with Health Score, Collector lag, Contract/PII/Cardinality diagnostics
- Cohort/Retention and reusable sequential Business Journey analysis
- Organization/Department Feature Adoption with eligible-user targets and repeat/dormant usage
- Automatic Web Vitals/Resource Error RUM, Error Conversion Impact and Release Impact
- Offline Insight/Root Cause and Korean/English Natural Language Analytics
- AI Model/Agent/MCP/Tool calls, success, latency, token, cost and fallback analytics
- Allowlisted Scheduled Report and Segment Action delivery to HTTP-based enterprise channels
- 13-tool Analytics MCP surface

## v0.6 — implemented

- Formula/Reference/Scoped Filter 기반 Semantic Metric과 Metric Goal Framework
- Exact/Fast/Preview Sampling, Query Complexity/Cost Policy와 Query Audit
- 수집 전 값 기반 PII Detect/Warn/Mask/Reject 및 Privacy Request 승인 Workflow
- Late Event 자동 재집계와 Aggregate Manager
- Event Contract CI 검사, Event Catalog, Owner와 Data Lineage
- Workspace Roll-Up과 SSO 기반 Cross-Site Business Journey
- Service/Feature Score, Dead Feature, Search와 Frustration Analytics
- Feature Flag/Experiment Registry, Variant Lift와 Confidence
- Change Calendar Annotation과 19-tool Analytics MCP surface

## v0.9 — implemented

- `MOMENTO_ENCRYPTION_KEY` 기반 AES-256-GCM 비밀값 저장으로 재기동 후에도 API key·Tracking Key·Server API Key·OIDC Secret·Delivery Header 유지
- 감사 기록이 남는 사이트·개인 키 재조회, `MOMENTO_ENCRYPTION_KEY_PREVIOUS` 병행 복호화와 관리자 재암호화
- 측정 대상 애플리케이션 CSP 차단 해소: 정책·meta·Reverse Proxy 스니펫 제공, SDK `data-endpoint` first-party 프록시와 CSP 위반 진단
- Public URL Origin과 `additional_connect_origins` 기반 콘솔 CSP 구성, Collector 응답의 불필요한 Document CSP 제거
- 수집 수신·CSP·허용 도메인·환경 일치·적재 파이프라인·키 저장 상태를 함께 보고하는 설치 진단 API와 콘솔
- 콘솔에서 사용 가능해진 Session 리포트, Raw Event Export, Delivery 이력·삭제, Event Contract 활성화·CI 검증, Semantic Metric 조회, Workspace Business Journey

## Next schema-compatible increments

- First/last/last-non-direct attribution and enterprise channel classification
- PostgreSQL 월별 Partition 전환과 Parquet export
- ClickHouse sink selected from administrator storage settings once event volume requires it
- Optional Kafka/Redpanda transport and separate collector/worker deployments for 10k+ EPS validation
- Mobile/server SDK packages, statistical anomaly detection and governed external LLM diagnosis

Raw Event remains the source of truth so these increments do not require changing the tracking protocol.
