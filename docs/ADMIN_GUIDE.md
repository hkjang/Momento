# Momento 엔터프라이즈 관리자 가이드 (Admin & Security Guide)

- **문서 버전**: v0.1.0-ENTERPRISE  
- **대상**: 시스템 관리자, Security/DevOps 엔지니어, 데이터 보안 담당자, CISO  
- **문서 개요**: Momento 온프레미스 시스템 배포, Keycloak OIDC SSO 연동, RBAC 권한 관리, PII 자동 마스킹 엔진, CIDR 서브넷 매핑 및 Audit Trail 감사 운영  

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
2. Valid Redirect URIs 설정: `https://momento.internal/auth/callback`
3. Access Token Claim Mappers에 `groups` 및 `roles` 파싱 규칙 추가.

### 2.2 RBAC (Role-Based Access Control) 권한 매트릭스

| 역할 (Role) | 개요 대시보드 | 쿼리 빌더 | 퍼널/경로 분석 | PII 룰 변경 | API 키 관리 | 감사 로그 |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: |
| **Momento_Admin** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **Momento_Analyst** | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ |
| **Momento_Viewer** | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **ServiceAccount** | ❌ | ❌ | ❌ | ❌ | ✅ (MCP 전용) | ❌ |

---

## 3. 개인정보 (PII) 자동 필터 & 마스킹 엔진

수집기(Durable Collector)는 유입되는 모든 이벤트의 속성과 `user_id`를 검증하고 PII 패턴을 자동 차단합니다.

- **주민등록번호 정규식**: `\d{6}-[1-4]\d{6}` ➔ `[REDACTED_SSN]`
- **이메일 정규식**: `[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}` ➔ `[REDACTED_EMAIL]`
- **전화번호 정규식**: `01[016789]-\d{3,4}-\d{4}` ➔ `[REDACTED_PHONE]`

> **관리자 제어**: 관리자 콘솔 `관리 ➔ 개인정보` 메뉴에서 추가 차단 속성명을 동적으로 추가/삭제할 수 있습니다.

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

### 5.1 API 키 발급 및 유예기간 회전 (Rotation)
- API 키는 발급 시 단 1회만 원문이 표시되며, DB에는 SHA-256 해시값으로만 보관됩니다.
- 기존 키를 폐기하지 않고 신규 키로 전환할 수 있도록 **7일간의 무중단 유예기간(Grace Period)**을 설정할 수 있습니다.

### 5.2 감사 로그 (Audit Trail)
- 사이트 생성, PII 규칙 변경, Keycloak 맵핑 수정, API 키 발급/폐기 등 관리자의 모든 조작 행위는 수정 불가능한 감사 로그 테이블에 무결성 보장 상태로 영구 기록됩니다.
