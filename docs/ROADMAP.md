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

## v0.10 — implemented

- 방문자 인사이트 보고서: 이전 동일 기간 자동 비교 KPI, 신규·재방문 구조, 방문 빈도·최근 활동 분포, 기기 격차
- Source·Medium을 Direct/Organic/Paid/Email/Social/Referral/Internal Portal·Notice·Message/Display로 분류하는 채널 그룹
- 진입 페이지 이탈률·참여율·전환율과 세션 비중
- 근거·원인 후보·다음 행동을 함께 제시하고 영향 순으로 정렬되는 자동 인사이트
- 실행 대상 Audience: 반복 방문 미전환, 1회 방문 신규, 휴면, 복귀
- Markdown 요약 즉시 복사·다운로드, `visitor_insight` Scheduled Report 배달, `get_visitor_insights` MCP 도구

## v0.11 — implemented

- 사람 단위 방문자 추적: 연결된 모든 Visitor를 합친 세션 그룹 타임라인, 이벤트 간격, 식별 시점 표시, 커서 기반 과거 탐색
- User ID·부서·조직·Visitor ID·페이지·이벤트·기능으로 실제 방문자를 찾는 검색과 리포트에서의 추적 딥링크
- 교차 서비스 활동 확인과 추적 기록 Markdown 내보내기, 개인 단위 조회 Audit
- 같은 요일 계절성을 반영한 중위수·MAD 기반 이상 감지와 이상이 있을 때만 전송하는 알림 배달
- 행동 기반 Segment 필드(entity.sessions/events/conversions/days_since_last_seen/days_since_first_seen)와 인사이트 대상의 Segment 자동 생성
- first-touch·last-touch·last-non-direct 전환 기여도와 관여 전환 비교
- Metric Goal 기간 진행률 기반 착지 예측과 필요 일일 속도

## v0.12 — implemented

- 다중 터치 기여도: 선형, 시간 감쇠(반감기 조정), 위치 기반 40/20/40 모델과 소수 배분, 평균 경로 길이, 관여 비중
- 모든 모델을 하나의 경로 numbering 위에서 가중치로 표현해 단일·다중 터치가 같은 정의를 공유
- 이상 감지 알림 상태 관리: 신규·지속·회복 전이 판정, 같은 이상의 반복 통보 제거, 회복 알림, 상태 저장 테이블
- 알림 전송 기준(`notify_on`) 설정과 읽기 경로에서 상태를 변경하지 않는 분리

## v0.13 — implemented

- Segment 비교 Funnel: 전체와 최대 3개 Segment를 같은 단계·모드·전환 시간으로 나란히 평가
- 완주율 기준 비교 차트, 전체 대비 pp/% 격차, 격차가 가장 크게 벌어지는 단계 지목
- 진입 20명 미만 Segment는 판정하지 않고 표본 부족으로 표시

## v0.14 — implemented

- 대화형 분석 읽기의 25초 상한과 취소 전파, 시간 초과 시 원인과 대안을 설명하는 504 응답
- 이상 감지 기준선을 일별 Rollup에서 계산하고 Rollup이 없는 날에만 Raw Event로 회귀
- Attribution·진입 페이지가 사용하는 `sessions(site_id, environment, started_at)` 인덱스
- 방문자 검색용 pg_trgm 인덱스와 확장 사용 불가 시 안전한 성능 저하

## v0.15 — implemented

- Retention Segment 비교: 전체와 최대 3개 Segment의 Cohort 크기 가중 평균 곡선과 격차 판정
- 해당 주차에 도달하지 못한 Cohort를 분모에서 제외해 최근 Cohort가 곡선을 끌어내리지 않도록 보정
- 첫 재방문 격차와 격차가 가장 큰 주차 지목, granularity에 맞는 단위 표기, 표본 20명 미만 판정 보류

## v0.16 — implemented

- Overview 첫 화면의 「지금 봐야 할 것」: 이상 감지와 Goal 미달 전망을 심각도 순으로 통합하고 상세 화면으로 연결
- 같은 심각도에서는 방금 발생한 변화(이상)를 표준 목표(Goal)보다 앞에 배치
- 달성·순항·판정 보류 Goal과 정상·데이터 부족 이상은 목록에서 제외해 목록 자체가 신호가 되도록 유지
- 일별 Rollup 기반 이상 감지와 Metric Registry만 사용해 랜딩 화면 응답 시간 유지

## Next schema-compatible increments

- Cross-service conversion credit and data-driven attribution weights
- PostgreSQL 월별 Partition 전환과 Parquet export
- ClickHouse sink selected from administrator storage settings once event volume requires it
- Optional Kafka/Redpanda transport and separate collector/worker deployments for 10k+ EPS validation
- Mobile/server SDK packages, forecast-based anomaly bands and governed external LLM diagnosis

Raw Event remains the source of truth so these increments do not require changing the tracking protocol.
