<p align="center">
  <img src="docs/favicon.svg" alt="Momento Logo" width="90"><br><br>
  <h1 align="center">Momento</h1>
</p>

<p align="center">
  <strong>사내 Web/App을 위한 온프레미스 이벤트 애널리틱스 플랫폼</strong><br>
  누가 어느 조직·부서·네트워크 망에서 어느 서비스·기능·버튼을 사용하는지를 Raw Event 수준에서 직접 소유하고 분석합니다.
</p>

<p align="center">
  <a href="https://hkjang.github.io/Momento/">🇰🇷 홍보 페이지</a> · <a href="https://hkjang.github.io/Momento/index_en.html">🇺🇸 English Page</a> · <a href="https://github.com/sponsors/hkjang">💖 Sponsor</a>
</p>

---

## 현재 제공 범위

- JavaScript SDK: Page View, SPA route, custom event, identify, session/visitor, UTM, device, click/scroll/form/download/outbound/error/heartbeat, consent, cookieless, batch, Beacon, offline queue
- Durable Collector: `POST /collect/v1/events`, domain/key 검증, 중복 제거, 개인정보 필터, PostgreSQL inbox 기반 비동기 적재
- 분석: Overview, Realtime, Acquisition, Pages, Events, Visitors, 사내 사용 현황, 저장형 Query Builder, 조건형 Open/Closed Funnel, Path, Ecommerce, User Timeline
- 관리: Site/Tracking Key, Keycloak OIDC(PKCE), RBAC, 사용자, 개인정보, 사이트별 Retention, C 클래스/CIDR 망 이름, Event Schema/Conversion, Custom Dimension, Audit
- 고급 분석: 중첩 AND/OR Segment Registry, Segment 기반 Query/Funnel, 물리화 Session 요약, 저장된 Exploration
- 개인화: Profile, password, 개인 API key 발급·회전·폐기
- 연동: REST/OpenAPI, Raw CSV/NDJSON export, Analytics MCP
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
<script async src="https://analytics.example.com/tracker.js" data-site-id="SITE_XXXXXXXX"></script>
```

```javascript
analytics.identify("INTERNAL_USER_001", {
  department: "Digital Platform",
  organization: "Technology"
});

analytics.track("feature_use", {
  service: "intranet",
  feature: "document_search",
  button: "advanced_filter"
});
```

이메일·전화번호·주민번호를 `user_id` 또는 property로 전달하지 마십시오. 기본 차단 property는 관리자 → 개인정보에서 변경할 수 있습니다.

## 설정 원칙

프로세스가 받는 환경변수는 정확히 세 개입니다.

- `MOMENTO_POSTGRES_DSN`
- `MOMENTO_BOOTSTRAP_ADMIN`
- `MOMENTO_BOOTSTRAP_ADMIN_PASSWORD`

그 밖의 공개 URL, Keycloak client, claim mapping, 개인정보, 저장소, 보안 및 망 대역은 DB에 저장되는 관리자 설정입니다. Bootstrap 비밀번호는 최초 관리자 생성에만 쓰며 기존 비밀번호를 덮어쓰지 않습니다.

자세한 내용은 [오프라인 설치](docs/OFFLINE.md), [아키텍처](docs/ARCHITECTURE.md), [제공 범위와 로드맵](docs/ROADMAP.md), [MCP](docs/MCP.md), [OpenAPI](docs/openapi.yaml)를 참고하십시오.

## 개발

```bash
go test ./...
cd sdk && npm install && npm run typecheck && npm run build
cd ../web && npm install && npm run build
docker build -t momento:dev .
```

릴리스는 `v*` tag push 시 GitHub Actions가 `momento-v<version>` 이미지를 `momento-v<version>.tar.gz`로 내보내 Release에 첨부합니다. 예를 들어 `v0.2.0` 태그는 `momento-v0.2.0.tar.gz`를 생성합니다. 소스 번들 또는 온라인 설치 스크립트는 별도 릴리스 자산에 포함하지 않습니다.

## License

Apache-2.0
