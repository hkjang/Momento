# 오프라인망 설치

> 아래 설치 절차는 릴리스마다 자동으로 실행됩니다. 릴리스 워크플로가 게시하려는 tarball을 그대로 `docker load`해 컨테이너를 띄우고, 마이그레이션이 끝나 `/health/ready`가 응답하는지, 보고하는 버전이 태그와 같은지, 콘솔이 서비스되는지, 부트스트랩 관리자 로그인이 되는지 확인합니다. 시작되지 않는 아티팩트는 게시되지 않습니다.

릴리스 자산에는 실행에 필요한 Momento 서비스 이미지 레이어가 모두 포함됩니다. 대상 서버에 Docker Engine과 접근 가능한 PostgreSQL 15 이상이 있어야 합니다.

1. 인터넷 연결이 가능한 구간에서 GitHub Release의 `momento-v<version>.tar.gz`와 `.sha256`을 내려받습니다. 예: `momento-v0.21.0.tar.gz`.
2. 보안 반입 절차 후 checksum을 확인합니다.

```bash
sha256sum -c momento-v<version>.tar.gz.sha256
gzip -dc momento-v<version>.tar.gz | docker load
```

3. 실행합니다. 애플리케이션이 받는 환경변수는 필수 세 개와 권장 하나입니다.

```bash
docker run -d --name momento --restart unless-stopped \
  --read-only --tmpfs /tmp:size=64m,noexec,nosuid \
  --security-opt no-new-privileges --cap-drop ALL \
  -p 8080:8080 \
  -e MOMENTO_POSTGRES_DSN='postgres://user:password@db.internal:5432/momento?sslmode=require' \
  -e MOMENTO_BOOTSTRAP_ADMIN='admin@example.com' \
  -e MOMENTO_BOOTSTRAP_ADMIN_PASSWORD='a-long-random-bootstrap-password' \
  -e MOMENTO_ENCRYPTION_KEY='a-long-random-encryption-key' \
  momento-v<version>
```

`MOMENTO_ENCRYPTION_KEY`는 발급한 API key와 Tracking Key를 암호화해 저장하므로 컨테이너를 교체하거나 재기동해도 키를 다시 발급할 필요가 없습니다. 값은 비밀 관리 체계에 보관하고 컨테이너 교체 시 동일하게 주입하십시오. 값을 잃으면 암호화 저장된 키는 복구할 수 없고 회전해야 합니다.

최초 기동 시 DB migration과 bootstrap super admin 생성이 자동 수행됩니다. 같은 이메일이 이미 있으면 bootstrap 비밀번호로 덮어쓰지 않습니다. 이후 OIDC, 공개 URL, 개인정보, 수집 보안, 보존, 망 대역 등의 설정은 관리자 UI에서 변경합니다.

백업 대상은 PostgreSQL뿐입니다. 업그레이드 전 `pg_dump`로 백업하고 새 이미지로 컨테이너를 교체하십시오. migration은 전진 적용되며 Raw Event는 삭제하지 않습니다.
