# Momento 관리자 가이드 (Admin Guide)

- **버전**: v0.1.0  
- **대상**: 시스템 관리자, Security/DevOps 엔지니어, 데이터 보안 담당자  
- **문서 목적**: Momento 설치·배포, Keycloak OIDC SSO 연동, RBAC 권한, PII 차단 필터, CIDR 네트워크 대역 맵핑 및 감사 로그 운영  

---

## 1. 초기 환경 설정 & 부트스트랩

Momento 프로세스는 오직 다음 **3개의 표준 환경변수**만 받습니다.

- `MOMENTO_POSTGRES_DSN`: PostgreSQL 데이터베이스 연결 문자열
- `MOMENTO_BOOTSTRAP_ADMIN`: 최초 관리자 이메일 계정
- `MOMENTO_BOOTSTRAP_ADMIN_PASSWORD`: 최초 관리자 12자리 이상 비밀번호

```bash
# .env 설정 예시
MOMENTO_POSTGRES_DSN=postgres://momento:password@localhost:5432/momento?sslmode=disable
MOMENTO_BOOTSTRAP_ADMIN=admin@corporate.internal
MOMENTO_BOOTSTRAP_ADMIN_PASSWORD=SuperSecretPassword123!
```

> **참고**: 그 밖의 모든 공개 URL, Keycloak 클라이언트 정보, 개인정보 차단 규칙, CIDR 대역은 DB에 보관되는 동적 관리자 설정입니다.

---

## 2. Keycloak OIDC SSO 및 RBAC 연동

Momento는 PKCE(S256)를 적용한 표준 Keycloak OIDC 통합을 지원합니다.

1. **Keycloak Client 등록**: `momento-web` Client ID 생성 및 Redirect URI 설정 (`https://momento.internal/auth/callback`)
2. **역할(Role) 매핑**:
   - `Momento_Admin`: 전체 사이트 설정, PII 필터, API 키 및 감사 로그 접근 권한
   - `Momento_Analyst`: 쿼리 빌더, 퍼널 및 경로 분석 조회 권한
   - `Momento_Viewer`: 개요 및 실시간 대시보드 전용 조회 권한

---

## 3. 개인정보(PII) 자동 차단 & 보안 관리

- **PII 차단 엔진**: 수집기(Collector)에 유입되는 이벤트의 속성 및 `user_id`에서 이메일, 주민번호, 전화번호 패턴을 자동 차단합니다.
- **API 키 관리**: 무중단 회전을 위한 유예 기간(Grace Period)을 제공합니다. API 키 원문은 발급 시 단 1회만 표시되며 DB에는 SHA-256 해시로 저장됩니다.

---

## 4. CIDR 수집망 대역 이름 맵핑

사내 물리적 네트워크 대역을 이름으로 매핑하여 사내 네트워크별 사용 현황을 분석할 수 있습니다.

- `10.10.0.0/16` ➔ **본사 R&D 센터**
- `10.20.0.0/16` ➔ **서초 오피스**
- `192.168.100.0/24` ➔ **사내 VPN 접속망**

---

## 5. 감사 로그 (Audit Trail) 운영

관리자 콘솔에서 수행된 모든 관리자 행위(사이트 생성, PII 규칙 변경, API 키 생성 및 사용자 권한 변경)는 수정 불가능한 감사 로그(Audit Logs)로 영구 기록됩니다.
