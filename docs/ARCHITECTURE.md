# Architecture

초기 배포는 PostgreSQL 하나로 운영되지만 수집과 분석 책임은 코드와 durable queue에서 분리됩니다.

```text
Browser SDK / HTTP API
  → stateless Go Collector (validation + pre-queue privacy filter)
  → PostgreSQL event_inbox (durable acceptance boundary)
  → Go Worker (privacy re-check, normalization, network classification)
  → PostgreSQL raw_events (single source of truth)
  → Go Query API / MCP
  → React Console
```

Collector는 inbox commit 이후 `202 Accepted`를 반환합니다. 여러 Momento 컨테이너를 실행하면 `FOR UPDATE SKIP LOCKED`로 inbox 작업을 나누므로 Collector와 Worker를 함께 수평 확장할 수 있습니다. 중복 `event_id`는 `(site_id, event_id)` unique key로 제거합니다.

수십억 건 고도화 단계에서는 `settings.storage.engine`을 전환점으로 사용해 Worker의 sink를 ClickHouse로 추가합니다. Metadata와 audit, user, settings, durable inbox는 PostgreSQL에 유지하고 Raw Event 및 aggregate만 ClickHouse에 둡니다.
