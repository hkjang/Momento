# 오프라인망 설치

> 아래 설치 절차는 릴리스마다 자동으로 실행됩니다. 릴리스 워크플로가 게시하려는 tarball을 그대로 `docker load`해 컨테이너를 띄우고, 마이그레이션이 끝나 `/health/ready`가 응답하는지, 보고하는 버전이 태그와 같은지, 콘솔이 서비스되는지, 부트스트랩 관리자 로그인이 되는지 확인합니다. 시작되지 않는 아티팩트는 게시되지 않습니다.

릴리스 자산에는 실행에 필요한 Momento 서비스 이미지 레이어가 모두 포함됩니다. 대상 서버에 Docker Engine과 접근 가능한 PostgreSQL 15 이상이 있어야 합니다.

1. 인터넷 연결이 가능한 구간에서 GitHub Release의 `momento-v<version>.tar.gz`와 `.sha256`을 내려받습니다. 예: `momento-v0.34.14.tar.gz`.

   아카이브 이름과 그 안의 이미지 이름은 형태가 다릅니다. 파일은 한 단어라 `momento-v<version>.tar.gz`이고, 이미지는 저장소와 태그라 `momento:v<version>`입니다. `docker load`가 출력하는 이름이 아래 `docker run`에 쓰는 이름입니다.
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
  momento:v<version>
```

`MOMENTO_ENCRYPTION_KEY`는 발급한 API key와 Tracking Key를 암호화해 저장하므로 컨테이너를 교체하거나 재기동해도 키를 다시 발급할 필요가 없습니다. 값은 비밀 관리 체계에 보관하고 컨테이너 교체 시 동일하게 주입하십시오. 값을 잃으면 암호화 저장된 키는 복구할 수 없고 회전해야 합니다.

최초 기동 시 DB migration과 bootstrap super admin 생성이 자동 수행됩니다. 같은 이메일이 이미 있으면 bootstrap 비밀번호로 덮어쓰지 않습니다. 이후 OIDC, 공개 URL, 개인정보, 수집 보안, 보존, 망 대역 등의 설정은 관리자 UI에서 변경합니다.

## PostgreSQL을 Docker로 운영하는 경우

Docker는 컨테이너에 **`/dev/shm` 64MB**를 줍니다. PostgreSQL은 병렬 질의의 공유 해시 테이블과 tuple queue를 거기에 둡니다.

수백만 행을 묶는 리포트는 그보다 많이 요구하는데, 이때 질의는 **느려지는 것이 아니라 실패합니다**:

```
ERROR: could not resize shared memory segment ... No space left on device (SQLSTATE 53100)
```

읽는 사람에게는 **지난달까지 되던 화면의 500**으로 도착합니다. 그리고 이건 **사이트가 플래너가 병렬로 갈 만큼 커진 뒤에야** 나타납니다 — 아무도 배포 설정을 건드리지 않고 있을 바로 그때입니다.

동봉한 `compose.yml`은 `shm_size: 1gb`를 설정합니다. **직접 운영하는 PostgreSQL 컨테이너에도 같은 값을 주십시오:**

```
docker run -d --name momento-postgres --shm-size=1g \
  -e POSTGRES_DB=momento -e POSTGRES_PASSWORD=… postgres:17-alpine
```

관리형 PostgreSQL이나 물리 서버에 직접 설치한 경우에는 해당하지 않습니다.

백업 대상은 PostgreSQL뿐입니다. 업그레이드 전 `pg_dump`로 백업하고 새 이미지로 컨테이너를 교체하십시오. migration은 전진 적용되며 Raw Event는 삭제하지 않습니다.

이 in-place 업그레이드 경로는 CI에서 검증됩니다. 과거 모든 릴리스가 남긴 스키마를 재현해 데이터를 채운 뒤 현재 migration 전체를 적용하고, 적용이 끝나는지·행이 그대로인지·`analytics_events` 뷰가 기존 행에서도 해석되는지·재기동 시 아무것도 다시 실행되지 않는지 확인합니다. 이미 저장된 행이 위반하는 제약을 추가하는 migration은 서비스 기동을 막기 때문에, 그런 변경은 폐쇄망이 아니라 CI에서 드러나야 합니다.
