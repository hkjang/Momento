# Momento 엔터프라이즈 관리자 가이드 (Admin & Security Guide)

- **문서 버전**: v0.5.0
- **대상**: 시스템 관리자, Security/DevOps 엔지니어, 데이터 보안 담당자, CISO  
- **문서 개요**: Momento 온프레미스 시스템 배포, Keycloak OIDC SSO 연동, RBAC 권한 관리, 개인정보 필터, CIDR 서브넷 매핑 및 Audit Trail 감사 운영

---

## 1. 시스템 아키텍처 및 부트스트랩 (Bootstrap)

Momento 컨테이너 프로세스는 오직 **3개의 필수 환경변수**만을 통해 최소 인프라로 구동됩니다.

```bash
# .env 환경 설정
MOMENTO_POSTGRES_DSN=postgres://momento:Secr3tPass@10.10.20.5:5432/momento?sslmode=disable
MOMENTO_BOOTSTRAP_ADMIN=admin@corporate.internal
MOMENTO_BOOTSTRAP_ADMIN_PASSWORD=SuperSecretAdminPassword123!
```

> **설정 원칙 (Design Principles)**:  
> 그 밖의 모든 공개 URL, Keycloak OIDC Client 정보, Claim Mapping, PII 차단 필터, CIDR 망 대역은 DB에 저장되는 동적 관리자 설정입니다. 부트스트랩 비밀번호는 최초 관리자 계정 생성 시에만 사용되며 기존 계정을 덮어쓰지 않습니다.

---

## 2. Keycloak OIDC SSO 연동 및 RBAC 매핑

Momento는 PKCE(S256)가 적용된 표준 OIDC(OpenID Connect) SSO 통합을 지원합니다.

### 2.1 Keycloak Client 구성
1. Keycloak Admin Console에서 `momento-web` Client ID 생성.
2. Valid Redirect URIs 설정: `https://momento.internal/api/v1/auth/oidc/callback`
3. Access Token Claim Mappers에 `groups` 및 `roles` 파싱 규칙 추가.

### 2.2 RBAC (Role-Based Access Control) 권한 매트릭스

| 역할 (Role) | 개요 대시보드 | 쿼리 빌더 | 퍼널/경로 분석 | PII 룰 변경 | API 키 관리 | 감사 로그 |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: |
| **Super Admin / Organization Admin** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **Workspace Admin** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **Analyst** | ✅ | ✅ | ✅ | ❌ | 개인 키 | ❌ |
| **Viewer** | ✅ | 조회 | 조회 | ❌ | 개인 키 | ❌ |

---

## 3. 개인정보 (PII) 필터 & URL 마스킹

수집기(Durable Collector)는 Inbox에 저장하기 전 개인정보 정책을 적용합니다. 기본 정책은 개인정보로 지정된 Property key를 중첩 객체와 Item 배열까지 제거하며, URL Query String과 Fragment를 제거합니다. Query String 수집을 명시적으로 활성화한 경우에만 관리자 목록의 Parameter를 마스킹합니다.

- **기본 차단 Property**: `email`, `phone`, `resident_number`
- **기본 URL 정책**: Query String 및 Fragment 제거
- **Query 수집 활성화 시 기본 마스킹 Parameter**: `token`, `password`, `email`
- **IP 익명화**: IPv4 `/24`, IPv6 `/64`
- **선택 정책**: User ID·User Agent 수집, Query String 제거, DNT, Visitor Profile

> **관리자 제어**: 관리자 콘솔 `관리 ➔ 개인정보` 메뉴에서 차단 key와 URL Parameter를 변경할 수 있습니다. SDK는 자동 DOM text 수집을 기본 비활성화하고 흔한 이메일·전화번호·주민번호 형태의 Error Message를 치환하지만, Custom Property 값까지 판별하지는 않으므로 연동 단계에서도 PII를 보내지 않아야 합니다.

---

## 4. C클래스 / CIDR 서브넷 망대역 맵핑

사내 C-Class 및 CIDR IP 서브넷 대역을 특정 물리적 오피스 또는 사업장 이름으로 매핑합니다.

```json
[
  { "cidr": "10.10.0.0/16", "name": "본사 판교 R&D 센터" },
  { "cidr": "10.20.0.0/16", "name": "서초 디지털 오피스" },
  { "cidr": "192.168.100.0/24", "name": "사내 SSL-VPN 접속망" }
]
```

---

## 5. API 키 관리 (Lifecycle) & 감사 로그 (Audit Trail)

### 5.1 API 키 발급 및 회전 (Rotation)
- API 키는 발급 시 단 1회만 원문이 표시되며, DB에는 SHA-256 해시값으로만 보관됩니다.
- 개인 키 회전 시 기존 키는 즉시 폐기되고 새 키가 1회 표시됩니다. 무중단 교체가 필요하면 새 키를 별도로 발급한 후 클라이언트를 전환하고 기존 키를 폐기하십시오.

### 5.2 감사 로그 (Audit Trail)
- 사이트 생성, 개인정보 설정 변경, Keycloak 설정 수정, API 키 발급/폐기 등의 작업은 애플리케이션 API를 통해 추가 전용 감사 로그로 기록됩니다. DB 운영자는 별도의 DB 권한 통제·백업·보존 정책으로 로그 무결성을 보호해야 합니다.

---

## 6. 사이트별 보존정책과 Dimension Registry

- `관리 ➔ 보존 정책`에서 Raw Event, Session 요약, Aggregation, Realtime, Debug/Dead Letter 보존기간을 사이트별로 지정합니다.
- Raw Event와 Session 정리는 매시간 실행되며, Session은 Raw Event보다 오래 보존할 수 있도록 별도 요약 테이블에 저장됩니다.
- `관리 ➔ 사용자 정의 차원`에서 User, Session, Event, Item Scope와 데이터 타입을 등록합니다. 활성 User/Session/Event 차원은 `custom.<name>`으로 Query와 Segment에서 사용할 수 있습니다.

## 7. 사이트 Timezone과 참여 세션 기준

- `관리 ➔ 사이트 ➔ 설정`에서 IANA Timezone(예: `Asia/Seoul`)과 참여 기준 시간(기본 10초)을 지정합니다.
- Event Timestamp는 UTC로 저장되지만 날짜 범위, 일별 Trend, Query, Funnel, Export와 MCP는 Site Timezone의 자정 경계를 사용합니다.
- 참여 세션은 `지속시간 ≥ 기준`, `Conversion 1회 이상`, `Page View 2회 이상`, `Active Engagement ≥ 기준` 중 하나를 충족하면 됩니다.
- 기준 시간을 바꾸면 기존 Session 요약의 `engaged` 값도 같은 트랜잭션에서 즉시 다시 계산됩니다.

## 8. 개인정보 삭제 일관성

Visitor, User ID, 기간 또는 Site 삭제는 PostgreSQL Inbox와 Dead Letter 원본 payload를 먼저 정리하고 Raw Event를 삭제한 뒤 남은 Raw Event에서 Session, Visitor, Identity Graph와 일별 집계를 재생성합니다. User ID 삭제 시 Identity Graph에 연결된 로그인 전 익명 Visitor와 다른 기기의 Raw Event도 함께 삭제합니다. Event Property 삭제도 처리 대기/Debug payload와 Raw Event에 함께 적용됩니다. 따라서 삭제된 데이터가 Worker 재시도로 복원되거나 파생 보고서에 잔존하지 않습니다.

## 9. Identity Graph와 파생 집계 운영

- `visitor_identities`는 `(site_id, visitor_id) → user_id`의 결정적 연결을 보관합니다.
- `identified_users`는 연결된 모든 Visitor에서 가장 이른 최초 활동과 최신 User Property를 유지합니다.
- `visitors`, `visitor_sessions`, `daily_site_metrics`, `daily_site_visitors`, `daily_site_sessions`는 Worker가 Raw Event와 같은 transaction에서 증분 갱신합니다.
- Overview의 Site-local 일별 Trend는 일별 집계를 사용하고, 임의 시각 범위는 Raw Event로 정확히 fallback합니다.
- Site Timezone을 바꾸면 Raw Timestamp는 그대로 두고 일별 집계만 새 Calendar 경계로 즉시 재생성합니다.
- 장애 복구와 개인정보 삭제에서는 Raw Event를 Single Source of Truth로 파생 데이터를 재생성합니다.

## 10. Environment와 Event Contract

- `Analytics Governance`에서 Site별 DEV/STG/PRD와 사용자 정의 Environment를 관리합니다.
- 각 Environment는 Event Contract 정책 `allow`, `warn`, `reject`와 일별 Cardinality Limit를 가집니다.
- Event Contract는 Version마다 JSON Schema, Validation Mode, Changelog, 작성자와 활성시각을 보관합니다.
- Draft는 수집에 사용할 수 없습니다. Active Version을 바꾸면 이전 Active는 Deprecated가 되지만 Retry 호환을 위해 계속 검증할 수 있습니다.
- PRD를 비활성화할 수 없으며 SDK와 Server API가 Environment를 생략하면 PRD가 적용됩니다.

## 11. Semantic Metric과 Data Quality

- Semantic Metric은 관리자 정의 SQL을 받지 않고 허용된 JSON AST만 저장합니다.
- 수정 시 Version이 증가하고 REST/Query/MCP가 동일한 정의를 사용합니다.
- Session Scope Filter는 SDK/HTTP Event 발생 시점의 `session_properties`를 사용하며 Raw Event 전체 재빌드에서도 최신 Event 값 우선으로 복원됩니다.
- Data Quality는 Received, Accepted, Duplicate, Late, Reject, Contract Warning, Missing User/Feature, Unknown Network, PII Blocked, Dead Letter, Cardinality를 표시합니다.
- Cardinality 원문은 저장하지 않으며 SHA-256 digest로 Daily Distinct만 계산합니다.

## 12. Report / Action 보안

Scheduled Report를 사용하려면 먼저 관리자 설정 `automation`에서 기능을 활성화하고 `allowed_webhook_hosts`를 지정해야 합니다. 빈 Allowlist에서는 어떤 Endpoint도 호출하지 않습니다.

- 지원 Channel: Webhook, Confluence, Mail HTTP Gateway, Internal Message HTTP Gateway, AI Agent
- HTTP(S)만 허용하며 URL 내 Credential과 Redirect는 허용하지 않습니다.
- Channel Header 값은 저장 후 API에서 다시 노출하지 않습니다.
- Segment Delivery는 기본적으로 Aggregate만 전송하고 `max_entity_ids=0`입니다.
- 실행 결과는 Delivery Run과 Audit Log에서 확인합니다.

## 13. Analytics Engineering

- Formula Metric Builder는 Numerator/Denominator, 집계, Event, 최소 사용 횟수를 허용된 AST로 저장합니다. Metric마다 Owner, Entity Scope와 Tag를 지정하십시오.
- Goal Framework는 Metric, 목표값, `gte/lte`, 일/주/월/분기, Environment, 조직·부서 범위를 관리합니다.
- Query Policy는 Exact 최대 기간, Complexity 상한, Guarded 실행 기준과 Fast/Preview 표본 비율을 Site별로 제한합니다.
- Event Contract CI endpoint는 배포 전 미등록 Event, Version, Required Property, Deprecated 계약을 검사합니다.
- Event Catalog와 Lineage는 Event → Metric → Goal 사용 관계, Owner, First/Last Seen과 Volume을 표시합니다.

## 14. Aggregate와 Late Event 운영

Event가 수신 시각보다 한 시간 이상 과거이면 Momento는 Site Timezone의 해당 날짜에 `late_event` 재집계 Job을 한 건만 생성합니다. Maintenance Worker는 Raw Event를 기준으로 Site/Visitor/Session 일별 집계를 다시 계산합니다. 관리자는 Analytics Engineering에서 367일 이하 Date Range 또는 Full Rebuild를 요청할 수 있습니다.

## 15. 값 기반 PII와 Privacy Request

- `privacy.pii_detection_mode`는 `detect`, `warn`, `mask`, `reject` 중 하나입니다. 기본값은 `mask`입니다.
- Email, 한국 전화번호, 주민번호 형태, Luhn 검증 카드번호, Bearer/JWT Credential을 Inbox commit 전에 검사합니다.
- Data Quality Issue에는 Detector 종류만 기록하고 일치한 원문은 저장하지 않습니다.
- Privacy Request는 요청과 승인을 분리합니다. 승인 실행과 영향 건수는 Audit Log에 남습니다.
- 승인된 Export는 임의 행 제한 없이 NDJSON을 streaming하며 사용자·세션 속성도 포함합니다.

## 16. Workspace와 Experiment 운영

- Workspace Roll-Up과 Cross-Site Journey는 SSO User ID만 Site 간 결합합니다. 익명 ID는 Site 범위를 벗어나 결합하지 않습니다.
- Feature Flag는 2~20개 Variant를 등록할 수 있습니다.
- Experiment에는 `experiment_id`, `variant`, Primary Semantic Metric을 지정합니다. 첫 Variant가 Control이며 Lift와 두 비율 정규 근사 Confidence를 제공합니다.
- Change Calendar에 Release, Deployment, Incident, Campaign, Training, Feature Flag와 조직 변경을 기록하면 분석 시점의 원인 후보를 보존할 수 있습니다.
