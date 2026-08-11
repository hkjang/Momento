<p align="center">
  <img src="docs/favicon.svg" alt="Momento Logo" width="90"><br><br>
  <h1 align="center">Momento</h1>
</p>

<p align="center">
  <strong>온프레미스 Product · Employee · Experience · AI Analytics 플랫폼</strong><br>
  누가 어느 조직·부서·네트워크 망에서 어느 서비스·기능·버튼·AI Agent를 사용하는지부터 실제 업무 결과와 사용자 경험까지 Raw Event 수준에서 직접 소유하고 분석합니다.
</p>

<p align="center">
  <a href="https://hkjang.github.io/Momento/">🇰🇷 홍보 페이지</a> · <a href="https://hkjang.github.io/Momento/index_en.html">🇺🇸 English Page</a> · <a href="https://github.com/sponsors/hkjang">💖 Sponsor</a>
</p>

---

## 현재 제공 범위

- JavaScript SDK: Event 발생 시점 Context snapshot, DEV/STG/PRD, Event Contract version, Release context, Page View/SPA/custom event, identify, session/visitor, first-touch UTM, click/form/error, Core Web Vitals·Resource Error RUM, fail-closed consent, cookieless, batch, Beacon, offline queue
- Durable Collector: `POST /collect/v1/events`, 환경별 계약 `allow/warn/reject`, domain/key 검증, 중복 제거, 수집 전 값 기반 PII Detect/Mask/Reject, Cardinality Guard, PostgreSQL inbox 기반 비동기 적재 및 작업별 savepoint 재시도
- 방문자 추적: SSO로 연결된 모든 기기를 한 사람으로 합친 세션 단위 타임라인, User·부서·페이지·이벤트 검색, 식별 시점, 커서 페이징, 교차 서비스 활동, 추적 기록 Markdown
- 이상 감지: 같은 요일 최근 8주 중위수·MAD 기반 robust z-score로 방문자·세션·이벤트·전환·오류 감시, 이상이 있을 때만 배달되는 알림
- 기여도: first-touch·last-touch·last-non-direct 채널 배분과 관여 전환, Metric Goal 착지 예측
- 방문자 인사이트: 전기간 자동 비교 KPI, 신규·재방문 구조, 채널 그룹 분류, 진입 페이지 이탈, 방문 빈도·최근성, 기기 격차, 우선순위 인사이트와 실행 대상 Segment, Markdown 즉시 복사·다운로드·정기 배달
- 분석: Overview, Realtime, Acquisition, Pages, Events, Visitors, Sessions, Cohort/Retention, Site·Cross-Site Business Journey, Workspace Roll-Up, Feature/Search/Frustration, Experiment, Web Vitals/Error/Release Impact, Insight/Root Cause, Ecommerce, User Timeline
- Identity/집계: fingerprint 없는 SSO/identify 기반 Deterministic Identity Graph, canonical User Property, Visitor/Session 요약, Site-local 일별 집계와 기존 Raw Event 자동 backfill
- 거버넌스: 버전형 Event Contract와 CI 검증, Formula 지원 Semantic Metric Registry, Metric Goal, Event Catalog·Lineage, DEV/STG/PRD 정책, Tracking Health Score, PII·Cardinality 이슈, Adoption 대상자 분모
- 비밀값 관리: `MOMENTO_ENCRYPTION_KEY` 기반 AES-256-GCM 저장, 재기동 후 키 재조회, 키 교체용 이전 키 병행과 재암호화, 설치 진단(CSP·허용 도메인·환경·적재 파이프라인)
- 관리: 운영 준비도·품질 지표·우선순위 조치함을 갖춘 역할 기반 관리 센터, `Ctrl/Cmd+K` 명령 팔레트, Site/Tracking Key, Keycloak OIDC(PKCE), RBAC, 사용자, 개인정보, 사이트별 Retention, CIDR 망 이름, Event Schema/Conversion, Custom Dimension, Audit
- 고급 분석: Exact/Fast/Preview Query Cost Guard, 중첩 AND/OR Segment Registry, Segment 기반 Query/Funnel, 사용자·세션 전환율, 참여 기준과 활동량이 포함된 Session 요약, 저장된 Exploration
- 개인화: Profile, password, 개인 API key 발급·회전·폐기
- AI/연동: Model·Agent·MCP·Tool 사용량/성공률/지연/토큰/비용, 완전 오프라인 자연어 분석, 22개 Analytics MCP 도구, REST/OpenAPI, Raw CSV/NDJSON export
- Action: 보안 Host Allowlist 기반 Scheduled Report와 Segment 집계를 Webhook, Confluence, Mail Gateway, 사내 메시지, AI Agent로 전달
- 운영: Late Event 자동 재집계, Aggregate Manager, Change Calendar, Feature Flag/Experiment Registry, 승인형 개인정보 요청 Workflow
- 배포: 단일 non-root Docker image, PostgreSQL migration 자동 적용, tag 기반 offline `.tar.gz` GitHub Release

## 빠른 시작

```bash
cp .env.example .env
# .env의 관리자 이메일과 12자 이상 비밀번호를 변경
docker compose up --build
```

`http://localhost:8080`에 접속한 뒤 관리 → 사이트에서 첫 사이트를 생성합니다.

SDK 설치:

```html
<script
  async
  src="https://analytics.example.com/tracker.js"
  data-site-id="SITE_XXXXXXXX"
  data-environment="prd"
  data-contract-version="1">
</script>
```

측정 대상 애플리케이션이 CSP를 사용하면 `script-src`와 `connect-src`에 Momento Origin을 허용해야 합니다. CSP를 바꿀 수 없으면 Collector를 같은 Origin으로 프록시하고 `data-endpoint="/momento"`를 지정하십시오. 관리 → 사이트 → SDK 설치의 **CSP 허용**과 **설치 진단**에서 정책 스니펫과 수집 상태를 확인할 수 있습니다.

```javascript
analytics.identify("INTERNAL_USER_001", {
  department: "Digital Platform",
  organization: "Technology"
});

analytics.setSessionProperties({
  login_status: "authenticated",
  workflow: "approval"
});

analytics.track("feature_use", {
  service: "intranet",
  feature: "document_search",
  button: "advanced_filter"
});
```

이메일·전화번호·주민번호를 `user_id` 또는 property로 전달하지 마십시오. SDK는 URL Query/Fragment를 전송하지 않고 자동 `element_text` 수집을 기본 비활성화하며, 서버도 기본적으로 Query String을 제거하고 값 기반 PII 정책을 Inbox 저장 전에 적용합니다. 차단 property와 정책은 관리자 → 개인정보에서 변경할 수 있습니다.

## 설정 원칙

프로세스가 받는 필수 환경변수는 세 개입니다.

- `MOMENTO_POSTGRES_DSN`
- `MOMENTO_BOOTSTRAP_ADMIN`
- `MOMENTO_BOOTSTRAP_ADMIN_PASSWORD`

여기에 권장 환경변수 `MOMENTO_ENCRYPTION_KEY`(플랫폼 공용 `ENCRYPTION_KEY`도 alias로 인식)를 더하면 발급한 개인 API key, Tracking Key, Server API Key, OIDC Client Secret, Delivery Header를 AES-256-GCM으로 암호화해 저장합니다. 같은 값을 유지하는 한 **서비스를 재기동해도 키가 사라지지 않고 다시 입력하거나 회전할 필요가 없으며**, 관리 → 사이트의 `키 보기`와 프로필 → API 키의 `저장된 키 보기`로 재조회할 수 있습니다(조회는 Audit Log에 기록). 키 교체 시에는 `MOMENTO_ENCRYPTION_KEY_PREVIOUS`에 이전 키를 두고 관리 → 설정 → 비밀값 암호화에서 재암호화를 실행합니다.

그 밖의 공개 URL, Keycloak client, claim mapping, 개인정보, 저장소, 보안 및 망 대역은 DB에 저장되는 관리자 설정입니다. Bootstrap 비밀번호는 최초 관리자 생성에만 쓰며 기존 비밀번호를 덮어쓰지 않습니다.

자세한 내용은 [오프라인 설치](docs/OFFLINE.md), [아키텍처](docs/ARCHITECTURE.md), [제공 범위와 로드맵](docs/ROADMAP.md), [MCP](docs/MCP.md), [OpenAPI](docs/openapi.yaml)를 참고하십시오.

## 개발

```bash
go test ./... && go vet ./...
cd sdk && npm install && npm run typecheck && npm run build
cd ../web && npm install && npm run lint && npm test && npm run build
docker build -t momento:dev .
```

릴리스는 `v*` tag push 시 GitHub Actions가 `momento-v<version>` 이미지를 `momento-v<version>.tar.gz`로 내보내 Release에 첨부합니다. 예를 들어 `v0.11.0` 태그는 `momento-v0.11.0.tar.gz`를 생성합니다. 소스 번들 또는 온라인 설치 스크립트는 별도 릴리스 자산에 포함하지 않습니다.

## License

Apache-2.0
