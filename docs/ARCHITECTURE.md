# Architecture

초기 배포는 PostgreSQL 하나로 운영되지만 수집과 분석 책임은 코드와 durable queue에서 분리됩니다.

```text
Browser SDK / HTTP API
  → stateless Go Collector (Environment + Contract + privacy validation)
  → PostgreSQL event_inbox (durable acceptance boundary)
  → Go Worker (privacy re-check, normalization, network classification)
  → PostgreSQL raw_events (single source of truth)
  → PostgreSQL sessions (deduplicated event-driven session summary)
  → Visitor Identity Graph + canonical User profile
  → Visitor/Session/Site-local Daily aggregates
  → Late Event / Administrator Aggregate rebuild queue
  → Semantic Metric + Goal + Query Cost Guard
  → Workspace/Product/Search/Experiment/RUM/AI Query APIs / MCP
  → React Console

Scheduled Report / Segment Aggregate
  → Host Allowlist + HTTP delivery adapter
  → Webhook / Confluence / Mail Gateway / Internal Message / AI Agent
```

Collector는 개인정보 필터가 적용된 Inbox commit 이후 `202 Accepted`를 반환합니다. 여러 Momento 컨테이너를 실행하면 `FOR UPDATE SKIP LOCKED`로 inbox 작업을 나누므로 Collector와 Worker를 함께 수평 확장할 수 있습니다. Worker는 작업마다 PostgreSQL savepoint를 두어 한 작업의 SQL 오류가 같은 batch의 정상 작업과 retry/dead-letter 기록을 중단하지 않게 합니다. 중복 `event_id`는 `(site_id, event_id)` unique key로 제거합니다.

`raw_events`는 불변 원본이고 `analytics_events` read model이 `(site, visitor_id) → user_id`의 결정적 연결을 적용합니다. 식별된 사용자는 `u:<user_id>`, 익명 사용자는 `v:<visitor_id>`로 namespace를 분리해 ID 충돌을 막습니다. 같은 SSO User ID를 가진 여러 브라우저와 로그인 전 Event는 하나의 사용자로 분석되며 fingerprint는 사용하지 않습니다. 최신 canonical User Property는 과거 익명 Event의 User-scope 부서·조직 차원에도 적용하지만 Raw Event 자체는 수정하지 않습니다.

`raw_events`에서 Segment, Funnel, Ecommerce, Visitor Timeline을 계산하며, 허용된 Dimension Registry만 SQL 표현식으로 변환합니다. Segment 정의는 중첩 JSON AST로 저장되어 SQL 원문을 입력받지 않습니다. `sessions`, `visitors`, `visitor_sessions`와 Site-local 일별 집계는 Raw Event insert가 실제로 성공한 경우에만 동일 transaction에서 upsert되므로 SDK 재시도에 의해 합계가 중복되지 않습니다. Overview의 Calendar-day Trend는 일별 집계를 사용하고 partial-day 요청은 Raw Event로 fallback합니다. Timestamp는 UTC로 저장하고 모든 Calendar Query는 Site의 IANA Timezone 경계로 변환합니다. 개인정보 삭제는 Inbox/Dead Letter, Raw Event, Identity/Visitor/Session/Daily 파생 데이터 재생성을 하나의 transaction으로 묶습니다.

v0.5부터 `environment`와 `contract_version`이 Raw Event의 거버넌스 경계입니다. 기존 Event는 PRD/Version 1로 승격되며 Daily Aggregate의 Key도 Environment를 포함합니다. Event Contract는 Draft/Active/Deprecated Version으로 보관되고 Collector는 환경 정책과 Contract 정책 중 더 엄격한 규칙을 적용합니다.

v0.6의 Semantic Metric은 제한된 JSON AST 안에서 Ratio, Metric Reference, Event/User/Session Property Filter와 최소 사용 횟수를 조합합니다. Metric Goal, Explorer, REST와 MCP가 같은 정의를 사용합니다. Query Guard는 실행 전에 기간·Dimension·Filter·Segment의 Complexity를 평가하고 Exact/Fast/Preview 정책을 적용하며 모든 실행을 `query_audit`에 기록합니다.

Collector는 durable Inbox 이전에 값 기반 PII Detector를 실행합니다. `mask`는 Email·전화·주민번호·Luhn 유효 카드번호·Credential을 치환하고, `reject`는 원문 Sample 없이 Detector 종류만 Data Quality Issue에 남깁니다. 한 시간 이상 늦게 도착한 Event는 Site-local 날짜의 재집계 Job을 중복 없이 생성하며 Maintenance Worker가 Raw Event에서 해당 범위를 다시 계산합니다.

Workspace Analytics는 SSO가 확인된 사용자를 `u:<user_id>`로 Site 간 결합하고 익명 사용자는 `s:<site_uuid>:v:<visitor_id>`로 Site 범위에 고정합니다. 따라서 Cross-Site Journey가 익명 ID 충돌로 잘못 연결되지 않습니다.

Data Quality 카운터는 Collector의 Received/Rejected와 Worker의 Accepted/Duplicate/Late/PII/Cardinality 상태를 분리해 기록합니다. Cardinality Registry에는 원문 Value 대신 SHA-256 digest만 저장합니다. 자동화 Worker는 관리자 설정이 활성화되고 Endpoint Host가 Allowlist에 정확히 일치할 때만 HTTP 요청을 수행하며 Redirect를 따르지 않습니다.

수십억 건 고도화 단계에서는 `settings.storage.engine`을 전환점으로 사용해 Worker의 sink를 ClickHouse로 추가합니다. Metadata와 audit, user, settings, durable inbox는 PostgreSQL에 유지하고 Raw Event 및 aggregate만 ClickHouse에 둡니다.
