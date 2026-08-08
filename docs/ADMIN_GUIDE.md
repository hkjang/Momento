# Momento 엔터프라이즈 관리자 가이드 (Admin & Security Guide)

- **문서 버전**: v0.2.0
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

수집기(Durable Collector)는 저장 전 개인정보 정책을 적용합니다. 기본 정책은 개인정보로 지정된 Property key를 중첩 객체와 Item 배열까지 제거하며, URL Parameter는 관리자 목록에 따라 마스킹합니다.

- **기본 차단 Property**: `email`, `phone`, `resident_number`
- **기본 URL 마스킹 Parameter**: `token`, `password`, `email`
- **IP 익명화**: IPv4 `/24`, IPv6 `/64`
- **선택 정책**: User ID·User Agent 수집, Query String 제거, DNT, Visitor Profile

> **관리자 제어**: 관리자 콘솔 `관리 ➔ 개인정보` 메뉴에서 차단 key와 URL Parameter를 변경할 수 있습니다. 값의 정규식 탐지는 제공하지 않으므로 SDK 연동 단계에서도 PII를 보내지 않아야 합니다.

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
