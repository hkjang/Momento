# Architecture

초기 배포는 PostgreSQL 하나로 운영되지만 수집과 분석 책임은 코드와 durable queue에서 분리됩니다.

```text
Browser SDK / HTTP API
  → stateless Go Collector (validation + pre-queue privacy filter)
  → PostgreSQL event_inbox (durable acceptance boundary)
  → Go Worker (privacy re-check, normalization, network classification)
  → PostgreSQL raw_events (single source of truth)
  → PostgreSQL sessions (deduplicated event-driven session summary)
  → Visitor Identity Graph + canonical User profile
  → Visitor/Session/Site-local Daily aggregates
  → Go Query API / MCP
  → React Console
```

Collector는 개인정보 필터가 적용된 Inbox commit 이후 `202 Accepted`를 반환합니다. 여러 Momento 컨테이너를 실행하면 `FOR UPDATE SKIP LOCKED`로 inbox 작업을 나누므로 Collector와 Worker를 함께 수평 확장할 수 있습니다. Worker는 작업마다 PostgreSQL savepoint를 두어 한 작업의 SQL 오류가 같은 batch의 정상 작업과 retry/dead-letter 기록을 중단하지 않게 합니다. 중복 `event_id`는 `(site_id, event_id)` unique key로 제거합니다.

`raw_events`는 불변 원본이고 `analytics_events` read model이 `(site, visitor_id) → user_id`의 결정적 연결을 적용합니다. 식별된 사용자는 `u:<user_id>`, 익명 사용자는 `v:<visitor_id>`로 namespace를 분리해 ID 충돌을 막습니다. 같은 SSO User ID를 가진 여러 브라우저와 로그인 전 Event는 하나의 사용자로 분석되며 fingerprint는 사용하지 않습니다. 최신 canonical User Property는 과거 익명 Event의 User-scope 부서·조직 차원에도 적용하지만 Raw Event 자체는 수정하지 않습니다.

`raw_events`에서 Segment, Funnel, Ecommerce, Visitor Timeline을 계산하며, 허용된 Dimension Registry만 SQL 표현식으로 변환합니다. Segment 정의는 중첩 JSON AST로 저장되어 SQL 원문을 입력받지 않습니다. `sessions`, `visitors`, `visitor_sessions`와 Site-local 일별 집계는 Raw Event insert가 실제로 성공한 경우에만 동일 transaction에서 upsert되므로 SDK 재시도에 의해 합계가 중복되지 않습니다. Overview의 Calendar-day Trend는 일별 집계를 사용하고 partial-day 요청은 Raw Event로 fallback합니다. Timestamp는 UTC로 저장하고 모든 Calendar Query는 Site의 IANA Timezone 경계로 변환합니다. 개인정보 삭제는 Inbox/Dead Letter, Raw Event, Identity/Visitor/Session/Daily 파생 데이터 재생성을 하나의 transaction으로 묶습니다.

수십억 건 고도화 단계에서는 `settings.storage.engine`을 전환점으로 사용해 Worker의 sink를 ClickHouse로 추가합니다. Metadata와 audit, user, settings, durable inbox는 PostgreSQL에 유지하고 Raw Event 및 aggregate만 ClickHouse에 둡니다.
