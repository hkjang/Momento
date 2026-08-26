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

## v0.17 — implemented

- 교차 서비스 전환 기여도: 같은 Workspace의 다른 서비스 방문도 Touchpoint로 인정하고 서비스별 배분을 제공
- SSO로 식별된 사용자에게만 적용하고 익명 방문자는 서비스별 격리를 유지
- 조회자가 접근할 수 있는 서비스로만 범위를 확장

## v0.18 — implemented

- 집단별 경험 비교: Segment별 Core Web Vitals p75와 오류 경험 사용자 비율
- 권장 기준(LCP 2500·INP 200·CLS 0.1·FCP 1800·TTFB 800) 통과 여부로 "느림"과 "기준 초과"를 구분
- 표본 20건 미만과 30% 미만 지연은 보고하지 않아 실행 가능한 격차만 남김

## v0.19 — implemented

- 종류별 Scheduled Report 입력 폼: 보낼 내용·주기·환경·기간·알림 상태를 선택하면 정의를 자동 생성
- 이상 감지와 방문자 인사이트 화면에서 정기 배달 설정으로 직접 이동하는 딥링크
- 종류가 쓰지 않는 값은 정의에 넣지 않고, 필요할 때만 JSON 직접 입력으로 우회

## v0.20 — implemented

- 방문자 인사이트 보고서의 8개 독립 조회를 동시 실행 상한 4로 병렬화하고 파생 계산을 조회 이후로 분리
- 첫 실패를 반환하며 남은 조회를 취소해 부분 보고서를 완성된 보고서로 제시하지 않음
- 상위 context 취소를 성공으로 오인하지 않는 종료 처리

## v0.21 — implemented

- Delivery Channel 인증 Header를 이름·값 행으로 입력하고 서버가 거부할 입력(중복·Host 재정의·줄바꿈·값 누락)을 저장 전에 표시
- 여덟 번의 기능 릴리스로 어긋난 사용자 가이드 3장 구조를 목적 순서로 재정렬하고 목차 추가

## v0.21.1 — implemented

- 이상 감지 SQL의 예약어 alias 오류 수정. v0.11부터 이상 감지 API와 `anomaly` 배달이 실패하고 있었음
- 실제 PostgreSQL을 사용하는 통합 테스트 추가: 인사이트·이상 감지·기여도 6모델×2범위·Cohort·경험·추적·검색·Funnel 비교·진단·리포트 16종
- CI에 PostgreSQL service를 추가해 손으로 만든 SQL이 매 푸시마다 실행되도록 함

## v0.21.2 — implemented

- Query Cost Policy 조회가 저장된 정책이 없을 때 500이 아니라 Guard와 동일한 기본값을 반환하도록 수정
- 통합 검증 범위 확장: Collector 수집·Worker 적재·PII 필터, MCP 22개 도구 전체, 거버넌스·리포트 엔드포인트 30여 개, Export, Journey 분석, Contract CI 검증

## v0.21.3 — implemented

- `visitor_insight`·`anomaly` Scheduled Report를 데이터베이스 제약이 거부하던 문제 수정. v0.10·v0.11 이후 두 종류를 아예 만들 수 없었음
- 서비스가 허용하는 값이 데이터베이스 제약을 통과하는지 확인하는 회귀 테스트 추가
- 쓰기 경로 통합 검증: 이상 감지 알림 상태 전이, 비밀값 발급·재조회·회전·재암호화, Scheduled Report 전송(성공·skipped·실패)과 봉인된 Header 사용

## v0.31.1 — implemented

- Fixture에 `refund`·`user_engagement`·`resource_error`·`ai_model_call` 추가
- Ecommerce 환불·순매출, 참여 시간 숫자 가드, Resource Error, AI 리포트(호출·성공률·토큰·지연·비용)를 알려진 입력으로 검증
- 추가 이벤트를 기존 Session에 귀속시켜 Fixture 일관성 유지

## v0.31.0 — implemented

- OpenAPI 문서 누락 경로 35건 추가(리포트·Export·개인 API Key·사용자·설정·감사 로그·삭제/회전 작업)
- 라우터와 문서를 양방향 비교하는 테스트 추가(DB 불필요, 매 push 실행)

## v0.30.4 — implemented

- 검색 리포트 숫자를 알려진 입력으로 최초 검증(Fixture에 검색 이벤트가 없어 미검증 상태였음)
- 검색·Retention의 화면 ↔ MCP 도구 일치를 테스트로 고정
- 이번 점검에서 새로운 불일치는 발견되지 않음

## v0.30.3 — implemented

- MCP `analyze_feature_adoption`이 Adoption 리포트 내용을 반환하도록 수정(화면·배달과 동일 구현)
- 조회 기간 정책을 Funnel과 모든 MCP 도구에 적용. 개인정보 삭제만 명시적 예외
- 나머지 MCP 도구를 화면과 비교 확인

## v0.30.2 — implemented

- Adoption 배달이 Adoption 리포트 내용을 담도록 수정(이전에는 Feature Intelligence 내용)
- Adoption 계산을 공용 패키지로 추출해 화면과 배달이 같은 구현 사용
- 나머지 배달 kind를 대응 화면과 비교 확인

## v0.30.1 — implemented

- 정기 배달의 조회 기간을 사이트 로컬 날짜 기준으로 변경해 화면과 동일한 기간 사용
- payload에 조회 기간(from/to) 포함
- Overview 배달에 Session·참여 Session 추가(v0.29.2 정의와 동일)
- 기간 자체를 단정하는 테스트 추가(이전 구현에서 6시간 차이로 실패)

## v0.30.0 — implemented

- Session 지표를 두 정의로 분리: `sessions`(기간 내 활동), `sessions_started`(기간 내 시작, 첫 화면과 동일)
- 쿼리 빌더 지표 선택 목록에 각 지표의 정의 표시
- 페이지·이벤트 리포트의 사용자 정의 일치 및 합계 일치 확인

## v0.29.2 — implemented

- Session 관련 지표(개수·참여·평균 시간·Session 전환율)를 sessions 테이블 한 곳에서 계산해 첫 화면과 인사이트 리포트 불일치 해소
- Session은 시작 시각 기준으로 계산. 파생 데이터가 없을 때만 이벤트로 대체
- v0.24.2에서 놓친 인사이트 리포트의 무제한 first_seen 스캔 상한 적용
- 두 화면 일치를 단정하는 테스트 추가(이전 정의에서 960 대 720으로 실패)

## v0.29.1 — implemented

- 매출 정의 통일: 쿼리 빌더가 `revenue` Property를 읽지 않아 0으로 표시되던 문제 수정
- 소스를 읽어 매출 표현식이 두 Property를 모두 받는지 확인하는 테스트 추가
- `방문자 상세`가 브라우저 단위임을 설명에 명시, 공용 지표 정의를 문서화
- Fixture 타임스탬프를 사이트 로컬 날짜에 고정해 실행 시각에 따른 통합 테스트 실패 제거

## v0.29.0 — implemented

- 관리 설정 전수 점검: 저장만 되고 읽히지 않던 설정 3건 확인
- 집계 보존 기간을 Retention 정리에 실제 적용(비우면 무기한 유지)
- 사이트 Session Timeout이 스니펫의 `data-session-timeout`으로 tracker에 전달
- Realtime 보존 항목은 적용 대상이 없음을 화면과 문서에 명시

## v0.28.0 — implemented

- `최대 정확 조회 기간` 정책을 전체 리포트 엔드포인트에 적용(기존에는 쿼리 빌더 1곳)
- 정책 초과를 `RANGE_EXCEEDS_POLICY`로 구분하고 현재 한도를 함께 반환
- 콘솔 기간 선택지가 사이트 정책 한도를 반영
- v0.27.0에서 추가한 넓은 기간을 200만 건으로 실측: 예산 초과 없음

## v0.27.0 — implemented

- 분석 기간 선택을 9개 화면에 추가(경험 비교·Retention·Adoption·Frustration·검색·Feature·Workspace·AI·Ecommerce)
- 화면 성격에 맞는 기간 목록 제공(Retention 90/180/365, Feature 30/60/90, 기본 7/30/90)
- 로딩·오류 상태에서도 기간 선택 유지
- Retention의 `기간` 필드를 `표시 주차`로 명확화

## v0.26.0 — implemented

- 조회 실패 화면이 원인별 복구 경로를 버튼으로 제공(기간 줄이기·Segment·Fast 모드·정기 배달)
- 실행할 수 없는 제안은 표시하지 않음(고정 기간 화면, 최단 기간, 권한 문제)
- 대기 8초·20초 시점에 상황 안내
- `APIError`를 테스트 가능한 형태로 정리

## v0.25.1 — implemented

- CI가 매 push마다 실행하는 규모 가드 추가(5만 이벤트, 약 13초). Segment 상관 서브쿼리류를 확실히 검출
- 가드가 잡지 못하는 것도 실측해 명시: 플래너 선택에 의한 문제는 200만 건 하네스가 필요
- 재집계의 통계 갱신을 직접 검증하는 테스트 추가

## v0.25.0 — implemented

- Identity 재집계 쿼리 재작성: raw_events 자기 조인(merge join, 전체 테이블 visitor 순 인덱스 스캔) → 그룹 스캔 3회 + hash join 2회. 33분 미완료 → 1.8초
- 재집계는 개인정보 삭제 요청 안에서 동기 실행되므로, 이 문제로 삭제 자체가 완료되지 못했음
- 재집계 각 단계 전후로 통계 갱신. 방금 채운 테이블을 다음 단계가 조인하며 nested loop를 선택하는 문제 해소
- 부하 하네스가 측정 전 일별 rollup을 운영과 동일한 경로로 생성

## v0.24.3 — implemented

- Funnel·Retention 비교의 코호트 병렬 실행. Funnel+Segment 9.1초 → 7.3초(중앙값)
- 부하 하네스가 3회 실행의 중앙값과 최소·최대를 함께 보고, 예산은 중앙값으로 판정
- 측정 결과 view의 `identified_users` join은 병목이 아님을 확인(초기 관측은 캐시 상태 차이)

## v0.24.2 — implemented

- 부하 하네스 추가: 200만 이벤트 시드 후 전체 분석 엔드포인트 실측, 15초 예산 초과 시 실패
- 방문자 인사이트 25초 기한 초과(504) 해소: 사람당 스칼라 서브쿼리 → 그룹 후 조인. 9.6초
- first_seen 스캔을 조회 기간 종료 시점으로 상한
- Overview 3개 조회 병렬화 및 컬럼 프로젝션: 11.9초 → 8.1초
- 경험 리포트 기본 조회 4건과 Segment별 코호트 병렬화: 17.7초 → 10.8초

## v0.24.1 — implemented

- `entity.*` Segment 필드를 상관 서브쿼리에서 semi-join으로 재작성. 200만 이벤트 30일 조회 60초 초과 → 2.3초
- 행동 집계의 범위를 요청의 Site·Environment로 고정, 범위 없는 컴파일은 거부
- Frustration 리포트의 독립 조회 4건 병렬 실행
- `insight.RunParallel` 공개로 연결 풀 상한을 한 곳에서 관리

## v0.24.0 — implemented

- 신호별 전환 영향 분석: 겪은 집단 대 겪지 않은 집단 전환율 비교, 판정 4종, 전환 손실 추정
- 차이 크기가 아니라 전환 손실 추정치 순 정렬
- 양쪽 20명 표본 하한, 거의 전원이 겪는 신호도 보류
- 전환과 함께 발생하는 신호를 손실과 구분
- 연관성 주의 문구를 응답에 포함, MCP `analyze_frustration`에도 동일 분석 제공

## v0.23.0 — implemented

- 행동 Segment 필드 5종 추가: `entity.frustration_signals`, `entity.frustration_sessions`, `entity.searches`, `entity.zero_result_searches`, `entity.search_clicks`
- Frustration·검색 리포트가 저장 가능한 `실행 대상` 집단을 함께 제공
- Frustration 신호 → 사용자 탐색기 `?q=` 딥링크로 실제 방문자 추적 연결
- 실행 대상 UI를 공용 컴포넌트로 통합

## v0.22.0 — implemented

- Frustration 신호 7종 자동 감지: Rage Click, Dead Click, Rapid Back, Form Retry, Repeated Search, Error After Click, Slow Interaction
- 사이트 검색 자동 인식: 질의 문자열 기반 `search`, 결과 순위 포함 `search_click`, 재검색 구분 `search_refine`, `trackSearch()` API
- 검색어 수집은 opt-in, 브라우저 단계 PII 제거, 페이지당 신호 20건 상한
- 내장 Event는 strict Contract에서 미등록 거부 대상에서 제외
- 두 리포트의 빈 표를 "문제 없음"과 "측정 안 됨"으로 구분해 안내

## v0.21.4 — implemented

- 개인정보 삭제·Retention·Aggregate 재집계 통합 검증. 삭제 모드 4종, 파생 테이블 8종 잔존 0건, 타인 데이터 보존, 승인 전 Export 차단 확인
- 승인 결정 API가 잘못된 본문과 잘못된 값을 구분해 보고하도록 수정
- 운영·테스트용 진입점 추가: `Worker.ApplyRetention`, `Maintenance.RunPending`

## Next schema-compatible increments

- Cross-service conversion credit and data-driven attribution weights
- PostgreSQL 월별 Partition 전환과 Parquet export
- ClickHouse sink selected from administrator storage settings once event volume requires it
- Optional Kafka/Redpanda transport and separate collector/worker deployments for 10k+ EPS validation
- Mobile/server SDK packages, forecast-based anomaly bands and governed external LLM diagnosis

Raw Event remains the source of truth so these increments do not require changing the tracking protocol.
