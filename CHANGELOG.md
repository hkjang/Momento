# Changelog

## v0.34.1

- **거부된 식별자가 "없는 식별자"로 집계되고 있었습니다.** 데이터 품질 화면은 사용자 ID 없이 도착한 이벤트를 세어 담당자가 "식별을 시작하라"고 말할 수 있게 합니다. v0.34.0부터 개인정보 필터가 사용자 ID를 검사해 보관할 수 없는 값을 비우는데, 그 이벤트가 **같은 카운터에 들어갔습니다.** 정반대의 문제입니다 — 한쪽은 `identify()`를 아예 안 부르고, 다른 쪽은 전화번호로 부르고 있습니다. 앞엣것을 보고 담당자는 **이미 하고 있는 일을 하라고** 팀에 말하러 가고, 진짜 문제는 계속 도착합니다. 이제 따로 세고, 화면이 조치로 이어지는 말로 알려주며, 둘 중 하나만 표시합니다.
- **업그레이드 테스트가 이번 마이그레이션이 건드리는 테이블을 채우지 않고 있었습니다.** 그 테스트의 존재 이유는 "빈 스키마에서는 멀쩡하고 데이터가 있는 스키마에서 실패하는 마이그레이션"인데, `data_quality_daily`는 채워지는 테이블 목록에 없어서 **빈 테이블에만 적용되고 있었습니다.** 이제 카운터 행을 심고, 업그레이드 후 그 값이 그대로이고 새 컬럼이 기본값에 도달했는지 확인합니다. 마이그레이션에서 `DEFAULT`를 빼면 **15개 과거 스키마 지점 전부에서** `contains null values`로 실패합니다 — 그게 아니었다면 담당자가 폐쇄망에서, 이전 이미지는 이미 멈춘 채로 마주쳤을 실패입니다.

## v0.34.0

- **사용자 ID를 아무 탐지기도 보지 않고 있었습니다.** 요청이 싣는 다른 모든 문자열은 내구성 있는 저장 전에 개인정보 탐지기를 통과합니다 — 사용자 속성, 세션 속성, 이벤트 속성, 항목, 페이지 URL·제목·리퍼러. `user_id`만 아니었습니다. 전화번호를 식별자로 보내면 그대로 저장되어 인덱싱되고, 신원 그래프에 연결되고, 신원 화면에 표시되고, 모든 Export에 들어갔습니다. v0.33.9가 브라우저 쪽을 막았지만 **서버 대 서버 수집은 브라우저를 거치지 않습니다** — 서버 API 키를 가진 통합은 그대로 보낼 수 있었습니다.
- **`mask` 모드에서 사용자 ID는 마스킹하지 않고 비웁니다.** 식별자를 상수로 치환하면 거부된 식별자를 가진 사람들이 **전부 한 사람으로 합쳐지고** 그들의 모든 1인당 지표가 조용히 망가집니다 — 신원 화면에는 기기 수천 개를 가진 방문자 하나가 뜹니다. "보관하지 않는다"는 정책 아래 정직한 기록은 "이 사람이 누구인지 모른다"이므로 이벤트를 익명으로 만듭니다. 브라우저에서 `identify()`가 거부했을 때와 같은 결과입니다.
- **URL 옆에 앉아 있던 필드들도 검사합니다.** 유입 필드는 페이지의 쿼리 문자열에서 뽑아내는데 그 문자열은 이미 검사됩니다 — 그래서 **같은 값이 URL에서는 마스킹되고 바로 옆 `traffic.term`에서는 원문 그대로 남아 있었습니다.** 채널·캠페인 리포트가 그 필드로 묶고, `utm_term`은 유료 검색이 사용자가 입력한 것을 넣는 자리입니다. 이벤트 이름도 같은 요청에서 같은 테이블에 도달하므로 함께 검사합니다.
- 방문자 ID와 세션 ID는 **다시 쓰지 않고 보고합니다.** 이벤트는 그것들 없이 귀속될 수 없고, 상수로 치환하면 영향받은 모든 방문이 하나로 합쳐집니다. `reject` 모드에서는 배치를 거부하고, 다른 모드에서는 어떤 탐지기가 이름을 댔는지와 함께 데이터 품질 화면에 나타납니다. **`mask` 모드가 사람인 식별자를 어떻게 다뤄야 하는가는 의도적으로 열어둔 결정입니다** — 가명화가 답일 수 있지만, 지나가는 김에 정할 일이 아닙니다.

## v0.33.9

- **전화번호를 전화번호처럼 쓰면 사용자 ID로 저장되었습니다.** `identify()`는 `@`가 있거나 숫자 8자리가 연속되면 거부했는데, 한국 휴대폰 번호는 `010-1234-5678`이고 주민등록번호는 `860101-1234567`입니다 — **둘 다 연속 8자리가 없어서 그대로 통과했습니다.** 그리고 다른 무엇도 잡지 못합니다: 수집기의 개인정보 필터는 사용자 속성·세션 속성·이벤트 속성·항목·페이지 URL/제목/리퍼러를 훑지만 **사용자 ID는 보지 않습니다.** 이 가드가 유일하게 판단하는 곳인데 **테스트가 하나도 없었습니다** — `identify()`는 SDK 테스트에서 한 번도 호출된 적이 없습니다. 이제 수집기가 이미 다른 모든 곳에서 마스킹하는 형태를 트래커도 거부합니다. 기존 8자리 규칙은 함께 남겨서 **어제 통과하던 사내 식별자는 오늘도 통과합니다.**
- **새 세션이 이전 세션의 캠페인에 귀속되지 않는지 확인합니다.** 세션의 유입 정보는 세션 시작 시 한 번 정해지고 그 세션의 모든 이벤트가 같은 답을 싣습니다 — 채널 표와 기여도 리포트가 의미를 갖는 이유입니다. 방문 안에서의 규칙은 이미 검사되고 있었지만 **경계는 아니었습니다.** 만료된 세션이 이전 캠페인을 물려받으면 그 사람의 이후 모든 방문이 처음 데려온 캠페인에 영영 귀속되고, **이벤트 자체가 그렇게 말하므로 하류의 무엇도 반박할 수 없습니다.**
- 사내 사용 현황 화면을 사용자 가이드에 설명합니다. 콘솔의 이동 가능한 화면 40개를 두 가이드와 대조한 결과 어떤 이름으로도 언급되지 않은 유일한 화면이었습니다. 여섯 기준(망·부서·조직·서비스·기능·버튼)이 각각 어디서 오는지와, `(미지정)`이 대부분인 화면이 데이터 없음이 아니라 **속성을 안 보내는 것**이라는 점을 씁니다.
- **두 운영 가이드가 자기 자신에 대해 거짓말을 하고 있었습니다.** 헤더가 `문서 버전: v0.21.0`인데 본문은 v0.33.1에 도입된 동작을 설명합니다. 온프레미스 제품에서 가이드는 지원 채널이고, v0.33을 돌리는 담당자가 v0.21로 찍힌 문서를 열면 내용을 못 믿거나 필요한 부분이 빠졌다고 넘겨짚습니다. 가드는 신선함이 아니라 **자기 일관성**을 검사합니다 — 어떤 버전을 인용하는 문서는 최소한 그만큼 최신이라고 인정해야 합니다. 로드맵은 아직 없는 버전을 의도적으로 이름 붙이므로 제외했습니다.
- 사용자 가이드 3.5절 번호가 `3.5.1 → 3.5.0 → 3.5.1`로 나가서 두 절이 번호를 공유하고 앵커가 충돌했습니다.

## v0.33.8

- **화면이 대응할 수 없는 실패 세 가지를 설명합니다.** `VISITOR_PROFILES_DISABLED`는 실패가 아니라 관리자가 개인정보 정책에서 방문자·신원 화면을 끈 것인데, 읽는 사람에게는 일반 오류와 **한국어 콘솔에 영어 문장**과 **설정을 고치라는 "다시 시도" 버튼**이 나왔습니다. `INVALID_TIMEZONE`은 그 사이트의 모든 리포트가 실패한다는 뜻이므로 사이트 설정으로 보냅니다. `RESPONSE_NOT_ENCODABLE`은 같은 요청이 같은 값을 만들어내므로 재시도를 제안하지 않습니다.
- **이커머스·기능·AI 화면이 "안 일어난 것"과 "측정되지 않는 것"을 구분합니다.** 셋 다 사이트가 직접 보내야 하는 데이터를 읽으므로 0으로 가득한 표는 어느 쪽이든 똑같아 보입니다. 특히 **거래 12건에 매출 0원**은 제품 버그처럼 보이지 `purchase` 이벤트에 `value` 속성이 없는 것처럼 보이지 않습니다. 상품 정보(`items`) 누락, `feature` 속성 미태깅, AI 이벤트의 구분 속성이 전부 `(not set)`인 경우도 각각 자기가 무엇인지 말합니다. **전부 계측된 화면은 아무 말도 하지 않습니다** — 사라지지 않는 안내는 읽히지 않게 됩니다.
- 그 안내들이 읽는 응답 필드 이름을 서버가 직접 확인합니다. 필드 이름이 바뀌면 아무것도 눈에 띄게 깨지지 않고 **안내만 조용히 사라져서**, 화면은 설명 없는 0의 표로 되돌아갑니다 — 원래 그 상태였으니 아무도 돌아온 것을 모릅니다. 콘솔 자신의 테스트는 객체를 손으로 만들어 쓰므로 이걸 잡을 수 없습니다(이번 주에 제가 정확히 그 실수를 했고 실제 응답만이 그것을 보여줬습니다).
- **릴리스 복구 장치가 스스로를 반복할 수 없었습니다.** 조정 워크플로는 릴리스가 없거나 아티팩트가 둘 중 하나뿐인 태그에 릴리스 워크플로를 디스패치하는데, 릴리스 워크플로는 `gh release create`로 발행하고 **이건 릴리스가 이미 있으면 그냥 실패합니다.** 즉 "반쯤 발행된 릴리스"라는 고치라고 만들어진 바로 그 경우를 고칠 수 없었습니다. 같은 에러가 정상 릴리스를 빨갛게 만들기도 했습니다 — v0.33.7이 19:03에 정상 발행됐는데 19:02에 들여다본 조정이 중복 실행을 띄웠습니다.
- **아티팩트가 자기 것이 아닌 커밋을 달고 있었습니다.** `GITHUB_SHA`는 태그 푸시에서는 태그의 커밋이지만 **디스패치에서는 워크플로를 던진 브랜치**입니다 — 체크아웃은 태그를 하는데도요. 같은 v0.33.7을 두 번 빌드해 `2a90aee80a9e`와 `b1216bf9052b`가 나왔고 후자는 그 태그에 없는 커밋입니다. 복구 경로에서만 나타나므로, 이미 뭔가 잘못됐을 때만 조사하는 사람을 이미지에 없는 코드로 보냅니다.

## v0.33.7

- **구매 금액 계산이 두 패키지 일곱 곳에 각각 적혀 있었습니다.** 금액이 `value`로도 `revenue`로도 오고, 캐스팅 전에 검사해야 하고, 환불은 음수 — 이 세 결정이 개요·쿼리 빌더·이커머스·플랫폼 롤업·MCP·예약 다이제스트·일별 롤업에서 따로 내려지고 있었습니다. 일곱 개가 일치했지만 **일치하는 것과 어긋날 수 없는 것은 다릅니다** — 이 저장소는 그중 하나가 `value`만 읽어 매출 0을 답한 버전을 이미 배포한 적이 있습니다. 이제 정의는 `insight.RevenueAmountSQL` 하나이고, 그 밖에서 이 합계를 직접 쓰면 파일과 줄 번호를 대며 실패합니다.
- **예약 다이제스트의 숫자를 화면과 비교한 적이 없었습니다.** 다이제스트는 자기 이름이 가리키는 화면과 **다른 패키지에서 자기 쿼리로** 만들어집니다. 확인해 온 것은 모양뿐이었습니다 — 도착했는가, 종류가 맞는가. 화면이 3천 명이라 할 때 다이제스트가 4천 명이라 해도 **읽는 사람 앞에는 대조할 화면이 없습니다.** 이제 방문자·세션·이벤트·전환·매출 다섯이 일치해야 하고, 기간을 하루 옮기면 넷에서 두 숫자를 모두 대며 실패합니다.
- **engagement 규칙이 네 곳에 각각 적혀 있는데 서로 같은 답을 하는지 확인한 적이 없었습니다.** 수집기·재구축·폴백·마이그레이션이 각자 다른 컬럼을 다른 별칭으로 읽어서 문자열 하나로 합칠 수 없습니다. 대신 같은 답을 하도록 붙잡았습니다 — 네 경로마다 방문 하나씩과 어느 경로도 안 타는 방문 하나. **재구축은 개인정보 삭제 요청 안에서 실행되므로**, 수집기와 어긋나면 삭제 요청 하나가 사이트의 과거를 조용히 바꿉니다.
- **방문자별 집계에 들어간 이벤트를 세어본 적이 없었습니다.** 신원 화면과 방문자 검색이 읽는 카운터인데, 어긋나도 **그럴듯합니다** — 9와 8은 봐서 구분이 안 되고 두 화면이 같은 행을 읽으니 서로 일치합니다. 익명으로 2건 뒤 로그인해 3건을 만든 기기와 처음부터 식별된 기기를 보내, 5+4=9건·전환 2·기기 2가 나오는지 확인합니다.
- **사이트의 Session Timeout 설정이 실제로 무언가를 하는지 확인한 적이 없었습니다.** 이 값은 콘솔에서 스니펫의 `data-session-timeout`으로, 다시 트래커 옵션으로 전달되고 **세션 구분은 트래커에서 일어납니다** — 수집기는 받은 session_id를 그대로 씁니다. 기존 트래커 테스트는 전부 기본값 30을 쓰고 시계를 움직이지 않아서, 트래커가 이 값을 초로 읽거나 무시해도 스위트 전체가 통과했을 것입니다. 5분 설정에 4분·6분·6초 간격을 넣어 단위까지 고정합니다.

## v0.33.6

- **표현할 수 없는 값이 들어간 응답이 200으로 나가고 있었습니다.** `writeJSON`이 상태 헤더를 먼저 쓰고 인코딩하면서 인코더가 뭐라 하든 버렸습니다. NaN이나 무한대 — Go가 0으로 나눈 결과이자, 분모가 0인 비율이 되는 값 — 이 하나라도 있으면 **200과 잘린 본문**이 나가고 어디에도 기록이 남지 않았습니다. 콘솔은 파싱 실패를 보여주는데 서버 로그는 성공한 요청으로 남습니다. 이제 인코딩을 먼저 하고, 실패하면 자기 이름을 대는 500입니다.
- **아무것도 없는 기간을 아무도 물어본 적이 없었습니다.** 모든 화면의 모든 비율은 나눗셈이고 분모는 데이터베이스가 돌려준 개수인데, 빈 기간은 그 분모를 전부 한꺼번에 0으로 만듭니다 — 활동이 가득한 픽스처는 절대 도달하지 못하는 경우입니다. 리포트 18개에게 400일 전 하루를 물어보고 모든 숫자가 자기 이름이 주장하는 바를 지키는지 확인합니다. 살아있는 결함은 없었고, 가드를 하나 빼면 화면 이름을 대며 실패합니다.
- **개요의 차트와 그 위의 총계를 아무도 비교한 적이 없었습니다.** 총계는 이벤트에서 세고 일별 시계열은 롤업에서 읽습니다 — 두 소스, 한 숫자. 수집기로 3일치를 실제로 보내 "보낸 것"이라는 제3의 근거에 맞추고, 70일 이력이 있는 사이트에서 30일 범위로 다시 확인합니다. 후자는 원래 불가능했습니다: 픽스처가 롤업에 이벤트와 무관하게 하루 60/12/3을 써넣고 있어서 그 사이트에서는 두 소스가 구조적으로 어긋나 있었습니다.
- **세션 테이블이 측정된 적이 없었습니다.** 화면의 모든 세션 숫자를 여기서 읽는데, 이 테이블은 수집기가 이벤트 하나씩 유도합니다. 알려진 모양의 방문(6분·이벤트 4개·페이지뷰 2개·구매 1개)을 보내고 그대로 읽히는지, 페이지뷰 하나뿐인 방문이 이탈로 남는지, 화면이 테이블이 가진 것을 보고하는지 확인합니다. **오프라인 큐가 만드는 역순 도착** — 세션의 마지막 이벤트가 첫 이벤트보다 먼저 도착하는 경우 — 도 같은 방문으로 읽혀야 합니다.
- **모든 테스트가 UTC보다 9시간 앞선 사이트에서만 돌고 있었습니다.** UTC 서쪽 사이트는 반대로 변환하고, 제품 전체가 그 변환 위에 서 있습니다. 뉴욕 사이트의 현지 자정 30분 전후 페이지뷰 두 개가 **하나의 UTC 날짜가 아니라 실제로 일어난 두 날짜에** 기록되는지 확인합니다. 살아있는 결함은 없었고, UTC 날짜로 분류하게 만들면 `0 on 2026-08-06 and 2 on 2026-08-07`로 실패합니다.
- 마찰 리포트가 한 줄짜리 관계를 CROSS JOIN하고 `max()`로 사이트 전체 총계를 가져오느라 리포트의 5분의 4를 썼습니다 — 스칼라 서브쿼리로 바꿔 **470ms → 91ms**, 값 하나까지 동일. 워크스페이스 롤업은 `SELECT e.*`로 모든 컬럼(집계가 읽지 않는 jsonb blob 둘 포함)을 머티리얼라이즈하고 두 번째 읽기가 첫 번째를 기다렸습니다 — **400ms → 211ms**.
- **규모 테스트가 자기가 만든 데이터의 뒷정리를 재고 있었습니다.** 5만 이벤트를 넣고 파생 테이블을 재구축한 직후 측정을 시작하면 autovacuum이 그 사이 테이블을 훑고, 매번 다른 프로브와 경쟁합니다. 이론이 아니라 실제로 저를 없는 결함을 찾게 만들었습니다 — 안정된 테이블에서 134ms인 화면이 시드 직후엔 423ms였습니다. 이제 `VACUUM (ANALYZE)`로 그 작업을 먼저 끝냅니다.

## v0.33.5

- **방문자 인사이트가 필요 없는 조인 하나에 묶여 있었습니다.** 방문 빈도·최근성 집계가 리포트 전체 시간이었습니다 — 445ms 응답 중 452ms. 네 갈래 중 셋은 기간만 읽는데, "첫 방문 시점"이 네 갈래 전부에 조인되어 있었습니다. 같은 20만 이벤트에서 **조인 없이 97ms, 조인 있이 454ms** — 플래너는 한 기간에 사람이 몇 명인지 알 수 없어 400명인 곳을 10만으로 추정하고 600만 행짜리 조인을 계획합니다. 필요한 한 갈래로 옮겨 답은 그대로, 단계는 109ms. **리포트 전체 445ms → 198ms.**
- 라이프사이클 집계는 기간의 모든 이벤트를 정렬해 그룹별 distinct를 셌습니다. 사람 단위로 먼저 접으면 사람 수만큼의 해시 집계입니다 — **485ms → 135ms**, 같은 네 숫자. 채널 표는 기간과 비교 기간을 한 번에 스캔하면서 카운트를 전부 타임스탬프 FILTER로 써서, 채널당 숫자 하나만 기여하는 비교 기간 때문에 대상 기간의 두 배를 읽었습니다 — 경계 있는 두 읽기로 나눠 동시 실행, **528ms → 192ms + 158ms**.
- **콘솔이 한 덩어리로 배포되고 있었습니다.** 로그인 화면조차 차트 라이브러리 570KB를 포함한 전체 콘솔을 받은 뒤에야 비밀번호 입력란을 그렸고, 두 화면만 쓰는 사람이 스물두 화면 값을 냈습니다. 이제 화면을 열 때 받습니다 — **첫 로드 1.49MB → 676KB (gzip 474KB → 211KB).** 라우트를 일반 import로 추가하면 그 화면이 다시 첫 로드에 들어가는데 콘솔에서는 아무 이상도 안 보이므로, 그렇게 한 라우트의 이름을 대는 테스트를 넣었습니다.
- 이커머스는 요약·상품 표·퍼널을 순서대로 읽었고(**290ms → 223ms**), 실시간은 같은 30분을 네 번 순서대로 읽었습니다(**20ms → 10ms**) — 타이머로 갱신되는 화면이라 매 틱마다 그 대기를 냈습니다. 손대지 않은 개요를 대조군으로 함께 재서(256ms vs 254ms) 호스트 부하와 구분했습니다.
- **CI가 race detector를 한 번도 돌린 적이 없었습니다.** 이번 릴리스에서 여섯 개 핸들러가 goroutine을 쓰고 호출자가 나중에 읽는 변수에 씁니다. 테스트가 CPU가 아니라 데이터베이스를 기다리기 때문에 스위트가 3분에서 4분 30초가 되는 정도입니다. 현재 트리에서는 아무것도 보고하지 않는데, 할 말이 생기기 전에 넣는 것이 요점입니다.
- **실시간 화면은 CI가 돌리지 않는 opt-in 부하 테스트에서만 호출되고 있었습니다** — 저장된 범위가 아니라 최근 30분을 읽는 유일한 화면인데, 그 네 쿼리를 동시 실행으로 바꾸는 시점에 커버리지가 0이었습니다. 이제 수집기로 이벤트를 넣고 화면이 그것을 보는지 요구합니다.
- **규모 테스트 픽스처가 자기가 검사한다는 범위 밖에 있었습니다.** 시드가 `i % 5184000`(60일치 초)로 이벤트를 뿌렸는데 `i`는 5만까지만 가서 그 나머지 연산에 도달하지 못합니다 — 모든 이벤트가 최근 14시간 안에 있었습니다. 30일·60일 필터가 아무것도 걸러내지 않았고, 주간 코호트는 6개가 아니라 1개 버킷이었고, 모든 이전 기간 비교가 빈 기간을 읽었습니다. 이제 세션이 하루에 들어가고 방문자는 여러 날에 걸쳐 재방문합니다.

## v0.33.4

- **한 번의 잘못된 날짜가 서비스를 죽였습니다.** 기간을 파싱하는 모든 리포트 엔드포인트가 거치는 `writeRangeError`가, 정책 위반이 아닌 모든 경우에 자기 자신을 다시 호출했습니다. `?from=not-a-date` 요청 하나가 `fatal error: stack overflow`로 끝나고 프로세스가 죽습니다. Go의 스택 오버플로는 recoverer가 잡을 수 있는 panic이 아니라서, 진행 중이던 다른 요청까지 전부 함께 사라집니다. 로그인한 사용자가 날짜를 잘못 타이핑하는 것만으로 재현됩니다.
- **프로필 메뉴가 서비스 버전 자리에 "dev"를 보여주고 있었습니다.** 버전이 바이너리에 들어가는 경로는 `-ldflags` 하나뿐이었고, 그 값을 넘기는 것은 릴리스 워크플로뿐이었습니다 — `Dockerfile`, `Makefile`, `compose.yml`, README의 빌드 명령 모두 `dev`였습니다. 이제 버전 선언은 `internal/version` 한 곳이고, 어떤 빌드 경로도 그 자리에 자리표시자를 넣지 않습니다. 버전을 선언하는 네 파일이 서로 어긋나면 실패하는 테스트와, 세 빌드 파일에 자리표시자가 돌아오면 실패하는 테스트가 함께 들어갑니다.
- **모든 응답이 압축 없이 나가고 있었습니다.** 콘솔 JavaScript 약 1.5MB와 리포트 JSON 전부입니다. 브라우저는 첫 요청부터 gzip을 요청하고 있었습니다. 세션 목록은 **42,755 bytes → 1,359 bytes**로, 콘솔 첫 로딩은 **1.49MB → 474KB**로 줄었습니다 — 빌드한 이미지를 실제로 띄워 잰 값입니다.
- **리포트가 서로 무관한 조회를 하나씩 순서대로 실행했습니다.** 사용 현황은 같은 기간을 차원마다 한 번씩, 여섯 번 스캔하고 그 합을 기다렸습니다 — 20만 건 기준 **2.09s → 447ms**. 기여도 분석은 세 번 중 두 번이 각각 터치포인트 경로를 처음부터 다시 만들었습니다 — **809ms → 546ms**. 한 리포트의 동시 조회 한도는 4에서 8로, 커넥션 풀은 20에서 32로 올렸습니다 — 방문자 인사이트 **904ms → 679ms**. 집계 정렬이 4MB 기본값에서 매번 디스크로 넘어가던 것도 커넥션마다 32MB로 정리했습니다.
- **보존 기간이 지나면 재방문자가 다시 신규 방문자가 되었습니다.** 신규 여부를 원본 이벤트 전체를 그룹핑해 판단했는데, 원본 이벤트는 사이트 보존 정책에 따라 만료되고 일별 롤업은 별도 설정이 없으면 남습니다. 1년치를 보관하는 사이트에서는 모든 재방문자가 1년마다 한 번씩 신규가 됩니다. 이제 롤업에서 읽고, 정의는 `insight.FirstSeenCTE` 한 곳입니다. 개요와 방문자 인사이트가 같은 정의를 호출합니다.
- **기간을 바꿀 때마다 화면이 깜빡였습니다.** 새 쿼리 키에는 데이터가 없어서 화면 전체가 스켈레톤으로 교체되었다가 다시 그려졌습니다. 이제 다음 답이 올 때까지 이전 답을 유지하되, 사이트나 환경이 바뀌면 유지하지 않습니다 — 다른 사이트의 숫자를 이 사이트 이름 아래 보여주는 것은 빈 화면보다 나쁩니다. 로딩 표시는 헤더와 분석 툴바 두 곳에 있고, 잠깐 뒤에 나타나므로 빨리 끝나는 갱신에서는 아무것도 깜빡이지 않습니다.
- 세그먼트와 그룹핑이 합계를 보존하는지 확인했습니다. 전원이 매칭되는 세그먼트는 세그먼트 없는 결과와 정확히 같아야 하고, 인구를 둘로 나누는 두 세그먼트는 다시 합쳐서 전체가 되어야 합니다. 픽스처의 모든 사람이 전환하기 때문에 전환/미전환 분할은 "전체 + 0"이 되어 무엇이든 통과했을 것이라, 전환하지 않는 방문자를 먼저 수집시킨 뒤 비교합니다.

## v0.33.3

- Asked one question through every screen that can answer it and compared: sessions, page views, events, conversions, revenue, transactions, buyers, the ecommerce and analysis funnels, the query builder and the workspace rollup. **21 numbers across seven screens, all agreeing.** Every report builds its own SQL, and when two disagree there is no error to find — an operator with both screens open simply stops trusting each.
- The fixture alone could not have shown that: it reports 90 sessions, 90 page views and 90 conversions, so a report returning the wrong one of those would have looked like agreement. The test ingests a batch first to pull the numbers apart, and refuses to run if any two of them are still equal.
- Confirmed the comparison catches a real drift by making the pages report count every event instead of page views: `page views: the overview says 97, the pages report says 667`.
- `/api/v1/funnel` had no request body in the OpenAPI document, and its `description` key was written twice so the file silently kept one. Its date range is also flat `from` and `to` while `/api/v1/query` nests them under `date_range` — the difference cost a request to discover. The body is now specified, the duplicate is gone, and the difference is stated where a reader will meet it.

## v0.33.2

- Swept what a personal API key can reach, with real requests against every mutating route rather than by reading the middleware. **No defect: 51 of 57 refuse a key outright, and the six that reach a handler are reads that use POST only because the question does not fit in a query string** — the query builder, the funnel, natural-language query, two journey analyses and contract validation. None of them writes.
- That confinement rests entirely on each route being wrapped, and a route added without the wrapper would hand a key whatever its owner's role allows with nothing to say so. A test now walks the router and fails, naming the route, when a mutating one answers a key. Confirmed by adding one. The key it uses belongs to a super administrator, so a leak would leak at full authority.
- The RBAC table in the admin guide still said a Workspace Admin can change PII rules, which stopped being true in v0.33.1. Corrected, along with what a workspace administrator can now reach and the rule that no one may change their own role or grant one above it.
- Documented what an API key can do, which was nowhere: never administration regardless of the owner's role, no writes, every site the owner can see and no way to narrow that, and `scopes` carries the same value on every key — the limits come from the authentication layer, not from that field.

## v0.33.1

- **A workspace_admin could promote itself to super_admin.** `PATCH /api/v1/users/{id}` sat behind the admin middleware, which means "at least workspace_admin", and nothing bounded the role being granted. Measured: as a workspace_admin of one workspace, `{"role":"super_admin"}` on its own account answered 200 and the next request satisfied every workspace check in the service. `POST /api/v1/users` was the same door — it accepted any role, so a super_admin account could be created outright with a known password.
- The same middleware guarded the rest of instance administration: `PUT /api/v1/settings/{key}`, which sets the PII policy, security and automation for every site in every workspace, and the network ranges that decide what counts as internal traffic — a workspace_admin deleted one and it was gone.
- Those five routes now require an organization administrator, and no caller may grant a role above their own or change their own role at all — the second bound so an organization_admin cannot make itself a super_admin either. Each guard is verified independently: removing the middleware and removing the role bound each fail the test on their own.
- Checked what still has to work, at every level: an organization_admin grants roles below its own and edits instance settings; a super_admin grants super_admin; neither can raise its own role, and a super_admin cannot demote itself, which would leave a deployment with no administrator.
- The console no longer offers a workspace administrator the sections it can no longer write. Reaching one by link explains that the setting applies to the whole deployment rather than failing on the request.
- Swept the other 18 uuid-addressed routes the same way, with real requests from outside the owning workspace or user: personal API keys, segments, saved reports, dimensions, delivery channels, scheduled reports, annotations, journeys, adoption targets and privacy requests all refuse correctly, including a child id from another site addressed through a site the caller does own.

## v0.33.0

- **A workspace_admin could rename, rotate the keys of, and delete a site in another workspace.** The administrative routes on `/api/v1/sites/{id}` take the site's uuid from the path; their middleware checks the caller is at least a workspace_admin and never checks which workspace, and four of the six handlers parsed the uuid and acted on it without checking either. Measured as an admin whose only membership was one workspace: PATCH renamed a neighbouring site, both key rotations succeeded, and DELETE returned 204 and took every event, session and aggregate with it. Rotating a key stops that site collecting until someone redeploys its tracker; the deletion cannot be undone. All six now resolve the site through one helper that applies the same workspace rule the report paths use, and the owner keeps full control of their own site — checked in both directions, because a fix that locks the owner out is not a fix.
- Two of the six handlers had the membership predicate written inline and were correct, which is how this survived: the routes looked guarded because some of them were. There is one place to call now, so it is no longer a per-handler decision.
- Every table with a `site_id` is checked to cascade from `sites`. Deleting a site is one statement and a cascade, and nothing verified what it reaches — a table added later without the foreign key would keep that site's rows forever, and since every read path resolves a site first, nobody could ever see them. All 37 cascade today; the guard names the table when one does not.
- Site deletion is now covered end to end: it refuses anything but an exact confirmation and does not delete on a refusal, removes every trace across the 13 tables the fixture fills, leaves a neighbouring site's data untouched, and still records the deletion in the audit log — the only thing left afterwards.

## v0.32.9

- Applied last release's lesson to the other guards that read the source: each one was measured for what it actually inspects, not what it appears to. All three were covering a fraction of their subject.
- **`DELETE /api/v1/sites/{id}` was absent from the OpenAPI contract.** It erases a site and every event, session and aggregate belonging to it, irreversibly. The contract guard compared paths in both directions and passed, because the path was listed for its PATCH — methods were never compared. Documented now, including the confirm parameter that must match the Site ID exactly, and the guard compares operations rather than paths. 130 operations served, 130 documented.
- The revenue-property guard inspected **3 of the 28** reads of `properties->>'value'` in the tree, and never opened `internal/service`, where the digest and the daily rollups compute revenue. It matched only what sat within 400 characters after `event_name='purchase'`. It now classifies every read — 17 paired with `revenue`, 11 web vitals where the property has no second name, 0 unclassified — and catches a defect introduced in `internal/service/derived.go`, which the old one could not see. No live defect: every revenue expression already read both names.
- The query-policy guard checked 7 of the 22 reports that take a date range, described as a representative spread. It covers all 22 now, and a new check fails when a handler that reads a range is added without coverage, naming it. Confirmed by adding one. Verified separately that all 22 refuse a 300-day range under a 14-day policy, and that the four ranged-looking endpoints which answer it — realtime, catalog, identities, anomalies — take no date range at all.

## v0.32.8

- **A site with a strict contract lost the first batch of every session.** `session_start` was missing from the collector's list of automatic events, so a reject-mode environment answered 422 to the batch carrying it — and one event refuses the whole batch, so the first page view and the web vital sent with it went too. The guard that exists to prevent this had been passing: it matched `track()` and `signal()` calls, and `session_start` is queued directly because `track()` starts a session and would recurse. The guard now matches both paths and fails on the omission, and the integration test sends a real first batch instead of single events.
- A page hidden while the browser is offline no longer hands its batch to `sendBeacon`. Beacon reports whether the browser accepted the payload, never whether it arrived, and measured: after an accepted beacon nothing was left in storage, so the batch was gone — and the batch at page exit holds the last page view, the exit page and a completed purchase. Offline is the one case knowable in advance and the one the offline queue exists for, so it is treated as the failure it is and the next page load delivers it. A browser killed hard while online can still lose an accepted beacon; nothing the page can observe would say so, and this does not claim to fix it.
- The queue's 200-event cap dropped the oldest silently — measured, 260 tracked became 200 persisted with nothing recording the 60. It now counts what it could not keep, including a quota failure, and reports it as `collection_dropped` with `events_dropped` on the next page load, once. A gap an operator can measure beats numbers that are quietly low.
- Re-verified before relying on it: delivering the same batch three times leaves raw events, session counters, visitor totals and the daily rollups at their first-delivery values.

## v0.32.7

- The SDK delivers queued events under the session they happened in. A queue entry carried no session of its own and the collector reads the session from the payload, so a batch queued while the network was down went out under whatever session was current when it reconnected. Measured end to end on the server: three events from 26 hours earlier delivered under a fresh session produced a session **26 hours long**, took its landing page from the previous visit, and raised `avg_session_duration` on the overview. A flush now sends one payload per session and visitor; the ids are stripped before sending, so the wire format is unchanged.
- Re-sending is safe, which is what makes that fix simple: measured that delivering the same batch three times leaves raw events, session counters, visitor totals and the daily rollups at their first-delivery values. Every event carries an id and the insert ignores one it has seen.
- The tracker measures its in-page windows with `performance.now()` instead of `Date.now()`. The wall clock is not a clock that only moves forward, and this was caught in the act: a wait of 2099ms by the monotonic clock read as **120ms** of wall time, which put two searches two seconds apart inside the 2000ms window that suppresses a repeated keystroke and dropped the `repeated_search` signal. Event timestamps and the session timeout stay on the wall clock, the first because the server needs real times and the second because it has to survive a page load.
- That was a CI flake, failing about one run in ten before this change and none in fourteen after. Diagnosed rather than retried: the fix is the product's, not the test's.
- The test harness stubbed `performance` without `now()`, which sent the tracker back to the wall clock and hid the monotonic path from every test. Both new guards fail against the code they replace.

## v0.32.6

- The retention screen now reports the last unattended pass: when it ran, how long it took, how many rows it removed per table, and the error if it failed. Retention runs hourly with nobody watching and left no evidence anywhere — the screen showed the policy and when someone last edited it, and a failing pass produced one line on stderr. In a closed network without a log pipeline, a job that had been failing for a month looked exactly like one with nothing to do, and the operator found out when the disk filled.
- The three states an operator has to tell apart are now distinguishable: never ran, ran and removed nothing, ran and failed. A pass with nothing to do is reported as a completed pass with zero rows, so a quiet screen is not the same as a stopped job.
- The failure case is induced in the test rather than written by hand — a table the pass deletes from is renamed away — so this checks the pass records its own failure and names what broke, not merely that a failed row would display.
- The record is bounded to the last 200 passes. The table that accounts for the trimming would otherwise be the next one nothing ever trims.
- Migration 015 adds `retention_runs`. The upgrade test covers it from all fourteen earlier schema points.

## v0.32.5

- Retention deletes in committed batches instead of one statement per table. Measured the old shape on the load harness: 2,001,497 expired events removed in a single 20.7s statement, leaving 2.9GB of table bloat, with nothing committed until it finished. That is the problem — an operator lowering a thirteen month policy to three on a large site, or a restart or statement timeout mid-pass, made no progress at all, and the hourly job then started over from the beginning forever. Batches of 20,000 rows commit as they go, so an interrupted pass keeps what it did and the next one continues. The same 2M events now take 30.9s, 49% slower for continuous progress, and 95ms at the 50,000-event scale the CI guard measures.
- Found a bug in the first version of that loop by running the whole suite rather than the one test: it stopped when a batch removed fewer rows than it selected, which is not the same as being finished. A ctid chosen by the inner select can already be gone when the delete runs it — a second instance running its own hourly pass, or the event worker trimming the inbox — so under any concurrency the loop exited early and left rows behind. It now stops only on a statement that removed nothing.
- The new test tells the two shapes apart rather than merely passing: a cancelled pass must leave part of the work done and the rest recoverable, and one statement per table can only ever leave every row or none. Confirmed it fails on the unbatched code — `a cancelled pass left 0 of 678 events, so the deletion is not split into committed batches`.
- The admin guide now states what retention actually does per pass, and that the daily visitor and session rollups hold a row per visitor per day with the visitor and user id on it.

## v0.32.4

- Retention now expires the identity tables with the events they describe. `visitor_identities`, `identified_users` and `visitors` had no policy and no expiry: once retention deleted a person's events and sessions, the visitor-to-user mapping and the per-visitor aggregate stayed behind forever. Measured it — after a site fully expired, 0 events and 0 sessions remained while 2 mappings, 1 identified user and 3 visitor aggregates survived, and the identities screen still named the employee with 100 events and both of their device ids, reading the count off the aggregate. An operator who set a window to satisfy a retention obligation had not met it, and the console said the opposite. A row is now kept exactly as long as an event or session it describes still exists, which needs no new setting and cannot remove anything still referenced.
- The new test checks both directions, because an over-eager prune is the worse failure: identities must survive a partial expiry untouched and disappear entirely once nothing is left. Confirmed it fails without the fix — `2 visitor_id to user_id mappings outlived every event and session they describe`.
- The scale guard now times a retention pass: it runs unattended, so a missing index would turn a nightly job into one that stops finishing with nobody watching. 50ms over 50,000 events, against a 30s budget.
- The Aggregation retention field said it trimmed "일별 집계 테이블". Two of those tables hold one row per visitor per day carrying the visitor and user id, so leaving the field blank keeps person-level records after the raw events are gone. The field now says that.

## v0.32.3

- The in-place upgrade path is now verified in CI. Every release note has promised it and it had only ever been checked by hand. The test rebuilds the schema each past release shipped, fills it with data — a site, events, sessions, an API key, a delivery channel, a scheduled report, an anomaly alert — and applies the current migration set on top, then asserts the upgrade completes, no row was lost, `analytics_events` still resolves over rows written before the columns it reads existed, and a restart re-runs nothing. All thirteen historical points, in 6 seconds.
- The failure this exists for is a migration that is fine on an empty schema and fails on a populated one: a CHECK or NOT NULL that stored rows violate. The service exits when a migration fails, so that upgrade leaves an operator with nothing running. Confirmed the guard catches all three shapes — a violated CHECK, a NOT NULL column with no default, and a migration that quietly deletes rows — by introducing each one and watching it fail.
- `database.MigrateThrough` and `database.Versions` make a historical schema reachable; `Migrate` is unchanged for callers.
- Verified the real upgrade first, v0.21.2 → v0.32.2 on one database: migration 014 applied, all 72 events and 12 sessions intact, the tracking key, server key and personal API key issued by v0.21.2 still authenticated, secrets encrypted by the old release decrypted, and all 43 read endpoints answered 200 over the old data.

## v0.32.2

- Every release now verifies its own offline artifact before publishing. The tarball is the product for an air-gapped deployment and nothing had ever loaded one and started it: the workflow published whatever `docker save` produced. It now loads the archive it is about to publish — not the image still in the local daemon — starts it with the environment variables the release notes give, waits for `/health/ready` so the migrations are known to have run, checks the reported version matches the tag, fetches the console, and logs in as the bootstrapped administrator. A tarball that cannot start is no longer published.
- Verified the current artifact by hand first, following the documented install exactly: checksum, load, run, ready, version, console, login. It worked, so the automation encodes a passing path rather than a fix.

## v0.32.1

- A missed release now repairs itself. A tag push is a one-shot trigger and a platform incident dropped it twice, leaving a tag whose version was announced and whose offline install artifact did not exist — a state nobody notices without looking for it. An hourly job compares recent tags against their releases and dispatches the release workflow for any that is missing, or that has only half of its two assets.
- Only tags from the last seven days are considered: an older tag without a release was a decision by then, and rebuilding it would be surprising.
- The repair path was executed rather than assumed. A reconciler that has only ever reported "nothing to do" has not demonstrated that it can act, so the dispatch was forced once against a cheap target to prove the token, the permission and the call, then reverted.
- No product code changed in this release.

## v0.32.0

- Documented that reports include bot, monitoring and internal traffic. The collector classifies every event by user agent and network and stores the result, and nothing filters on it, so an uptime monitor hitting a page every minute contributes fourteen hundred page views a day to the numbers a reader takes at face value. Nothing in the documentation said so.
- `traffic.internal` is a segment field now. The collector has always recorded whether an event came from a network an administrator marked internal, and nothing could read it — so excluding one's own staff meant naming every internal network by hand.
- The batch size limit and the traffic classification are tested. A batch at the configured limit is accepted, one over it is refused with the limit named, and the four user agent classes come out as expected.
- That reports include every class is now asserted rather than merely true, so changing it later is a deliberate act with a failing test to acknowledge.
- Two workflow fixes from after the v0.31.6 tag: CI and the release workflow both accept a manual trigger, because a platform incident dropped a push and a tag and neither could be recovered. The release trigger needed a second attempt — the first shadowed `GITHUB_REF_NAME`, which the runner overrides, so it tried to release a tag named "main".

## v0.31.6

- Server-side ingestion is tested. A request with no Origin is server to server and must present the site's server API key; the tracking key is not enough there, because it is published in the HTML of every page the site serves. No test had ever sent a request without an Origin, so that rule had never been exercised.
- The fixture stored a literal string in place of the server key hash, so no request could present a valid server key. It now stores a real hash, which is what made the path testable.
- Login rate limiting is tested at the wiring rather than the limiter. The limiter had unit tests; nothing had checked that the login endpoint consults it, which is the only thing between a reachable console and an unlimited password guessing loop.
- Added a manual trigger to the CI workflow. A push to main went unscheduled during a platform incident and, with only push and pull_request triggers, that commit could not be verified afterwards. The trigger worked on its first use.

## v0.31.5

- The personal API key path is tested. Every other test arrives with a session cookie, so the way a BI job or another service actually connects had never been exercised — including the refusals that stop a key being used as an administrator.
- A key reads analytics and the raw export, is refused on administrator endpoints even when its owner is a super administrator, is refused on interactive writes with `SESSION_REQUIRED`, and stops working the moment it is revoked, expires, or its owner is deactivated.
- Removed the Scope column from the API key list. Keys carry a scopes field that nothing reads, and the console never sets it, so the column was always empty while implying the key was restricted. No scope model was invented to fill it; what a key can and cannot do is documented instead.

## v0.31.4

- Access control is tested. Every test had signed in as a super administrator, which short-circuits the workspace membership check entirely, so neither that branch nor any administrator refusal had ever executed. On a shared internal deployment those two rules are what keep one team's analytics out of another team's console.
- An analyst reaches sites in their own workspace and receives 404 for a site in another organisation's — not 403, which would confirm the site exists. That site is also absent from their site list.
- Administrator endpoints refuse an analyst: users, settings, the audit log, the tracking debugger and the query policy.
- Turning visitor profiles off blocks the visitor list, the identity list and the person timeline even for a super administrator, because it is a privacy policy rather than a permission level, while the overview still answers because it names nobody.
- The membership rule is shown to be load-bearing rather than incidentally correct: granting the analyst a role in the other workspace makes the same refused request succeed, and revoking it refuses again.

## v0.31.3

- Environment isolation is tested. Every test had run against a site with only a production environment, so nothing had ever checked that staging traffic stays out of production reports. Most of these queries assemble their environment predicate by string concatenation, which is where one gets dropped; a leak would have appeared as production numbers that were quietly too high. Removing one filter to check makes the test fail, so it is doing its job.
- Adoption's declared target population is tested. Adoption is a rate, and its denominator is an administrator's declared eligible population when one exists and the observed population when it does not. No fixture ever declared a target, so only the fallback had run — and a rate against the wrong denominator is worse than no rate.
- A static sweep for analytical queries missing an environment filter found none; all four candidates build the predicate dynamically. Worth recording as checked rather than assumed.
- Both behaviours were already correct.

## v0.31.2

- The ecommerce funnel is exercised for the first time. It measures four steps and the fixture created only the last one, so the funnel and the cart and checkout user counts had never produced a number.
- The product table is exercised for the first time. It reads an `items` array from the purchase payload and no seeded purchase carried one, so that whole table was empty in every test run. Purchases now carry a realistic array whose price times quantity equals the purchase value, and the assertions check that relationship rather than a fixed total.
- Search refinements, exits and successes are exercised for the first time. Those three figures were zero for want of the event names, not for want of the behaviour.
- All of these were already correct. The point is that the suite could not have told the difference between correct and broken for any of them.

## v0.31.1

- Four reports are verified against known inputs for the first time. The shared fixture created no refund, no engaged-time event, no resource error and no AI call, so the ecommerce refund arithmetic, the engagement path, the resource-error half of the experience report and the whole AI operations report ran, answered zero and passed. All four were correct; that could not be known before.
- Refunds now check that net revenue is revenue minus refunds; engagement checks that an unparseable `active_seconds` is ignored rather than counted or fatal; the experience report checks the resource error count; the AI report checks calls, success rate, token totals, average latency and cost against the seeded values.
- The added fixture events belong to the sessions that already existed for that visitor and day. Writing events without a session row is something the collector never does, and it made the event-derived and table-derived session counts disagree for a reason no deployment would produce — which an earlier test caught.

## v0.31.0

- Completed the OpenAPI document. Thirty-five paths were missing from it, including the page, event and visitor reports, the raw event export, the personal API key surface, user and settings administration, the audit log and every delete and rotate operation. For an on-premise deployment that document is how a BI team or another service learns what the server offers, and nearly a third of it was undescribed.
- Added a test that walks the real router and compares it with the document in both directions: a path the server serves but the document omits, and a path the document describes but the server does not serve. The second matters more, because a reader will build against it.
- The test needs no database, so it runs on every push. The document drifted because it was maintained by hand next to the router with nothing comparing the two.

## v0.30.4

- Verified the search report's numbers against known inputs for the first time. The fixture carried no search events, so nothing in the suite had ever checked a search figure; six searches by five people with one returning nothing and one result clicked now pin the count, the distinct user count, the zero-result rate and the click-through rate.
- Pinned the agreement between the screens and the MCP tools for search and retention. Both pairs own separate copies of their query — the arrangement that let one defect ship three times in the adoption report — and they agree today, so a test is what keeps them agreeing.
- No new disagreement was found in this pass. Search, retention and experience each answer the same from the screen and the tool, which is worth stating rather than implying by silence.

## v0.30.3

- `analyze_feature_adoption` returns the adoption report. It ran its own query and answered with feature events and users, so an agent asked about adoption received no adoption rate, no eligible population and no dormant users — the same defect fixed in the digest last release, in a third place. All three now call one implementation.
- The site's query period limit applies to the funnel and to every MCP tool. It was added to the helper the reports use, and these callers had their own, so a limit lowered to protect the database still left an agent free to ask for five years. Enforcement is the default in both helpers now, and the one caller that must exceed it — privacy deletion, which has to reach as far back as the data goes — says so explicitly.
- Compared the rest of the MCP surface against the screens: `query_metrics` shares the overview's implementation and matches it including session duration, `analyze_experience` agrees with the experience report's impact figures, and `analyze_frustration` carries the same per-signal impact the screen shows.

## v0.30.2

- The adoption digest carries the adoption report. It ran its own query and answered with feature events and users — the feature intelligence report's content under the adoption report's name — so a schedule called Adoption 요약 delivered no adoption rate, no eligible population and no dormant users.
- The adoption computation now lives in one place that the screen and the digest both call, which is the fix for the cause rather than the instance: the digest drifted because it had its own copy of the query.
- Checked the other delivery kinds against their screens: the experience digest's error count and affected users agree with the experience report's impact figures, the AI digest reports what the AI screen reports, and the segment digest is its own definition rather than a screen's.
- The test compares every field of a delivered row against the screen's row rather than checking that the payload is non-empty.

## v0.30.1

- A scheduled report now covers the period the screen it is named after covers. Every delivery measured from the moment the schedule happened to fire, while every screen reads the site's calendar and ends at local midnight, so a seven day digest and the seven day screen described different spans — and the digest's span moved every time the send time drifted. Both windows now come from the same rule.
- The window travels with the payload, so a reader can see what was measured instead of reconstructing it from the send time.
- The overview digest reports sessions and engaged sessions, counted from the sessions table by when they started, which is the definition the screens settled on in v0.29.2. It previously omitted sessions altogether.
- The test asserts the window directly rather than inferring it from counts: whether the counts differ depends on whether any event falls in the band where the two windows disagree, which for the fixture depends on the hour the test runs. Restoring the previous window fails it with a six hour discrepancy.

## v0.30.0

- Separated the two session counts that shared one name. A dimensional breakdown needs the sessions active in a range — the ones that saw a page or arrived from a channel — while the overview reports the ones that began in it, and the difference is every session open at the boundary. On a two day window with one session carried over from before, the overview said six and the query builder said seven.
- `sessions` in the query builder keeps the active meaning that a breakdown requires, and `sessions_started` is the overview's number, so the same question can be asked there and get the same answer.
- The metric picker shows what each metric counts. Two session counts, a conversion count that is events rather than people, and a revenue that reads either property name are not distinguishable from a list of identifiers.
- Checked the report tables while looking for this: the page and event tables count users the same way as the overview, and their view, event and conversion columns sum to the overview totals.

## v0.29.2

- The overview and the insight report agree about sessions. Both answer how many sessions a period had, how many were engaged and how long the average one lasted; the overview measured the span of events inside the query window while the insight report read the sessions table, so the same period was a sixteen minute average session on one screen and twelve on the other.
- Everything about a session now comes from the sessions table, which the collector maintains and which is the only place that knows a session's real span. Sessions are counted by when they started, so consecutive periods add up instead of both claiming a session that spanned midnight.
- The events answer only when no session row exists for the period, which means the derived data is behind rather than that nothing happened. That replaces taking the larger of the two counts, which mixed definitions to avoid showing a zero.
- Session conversion rate now shares that denominator, so the rate and the session count on the same card refer to the same set of sessions.
- Bounded the insight report's first-seen scan, which was missed when the others were bounded in v0.24.2: it read the site's whole history to count new people in a period.
- A test asserts the two screens agree on all three numbers, and fails with 960 against 720 on the previous definitions.

## v0.29.1

- Revenue means the same thing on every screen. A purchase may carry its amount as `value` or `revenue`, and every report read both — except the query builder, which read only the first and answered zero for a site that sends the other. Nothing failed; one screen was simply wrong, which is harder to notice than an error.
- The amount now comes from one definition shared by the overview and the query builder, and a test reads the source to check that no purchase-amount expression accepts only one of the two names, so the next report cannot drift the same way.
- The visitor list says what it counts. It is a per-browser list, so one person using a desktop and a phone appears twice and the row count exceeds the user count on the first screen; the description said neither.
- Documented what the shared metric names mean: user is a person, Visitor is a browser, revenue reads either property, and a rate called 전환율 is user-based while the session-based one is named as such.
- Fixed a fixture that made the biggest integration test fail depending on the hour it ran. Event timestamps were built as now() minus N days plus an hour offset, so after 15:00 UTC two offsets landed on the same Asia/Seoul date, the daily series lost a day and the anomaly baseline came up one sample short. Timestamps are anchored to site-local calendar dates now, and the test passes under UTC, Asia/Seoul and America/Los_Angeles.

## v0.29.0

- Audited every administrative setting for whether anything reads it, after finding last release that the query period limit was enforced in one handler out of twenty-nine. Three settings were accepted, validated, stored, and then read by nothing.
- The aggregate retention limit now deletes. The retention screen offers a period for the daily rollups and the sweep never consulted it, so a site that asked to keep one year of aggregates kept all of them forever. An empty value still means keep indefinitely, so only a site that set a number is affected.
- The site's session timeout now reaches the tracker. Sessions are decided in the browser, and the tracker used its own thirty minute default while the configured value went nowhere; the installed snippet now carries it as `data-session-timeout`. Because the decision is made in the browser, changing the setting requires updating the installed snippet, and the console says so rather than implying it takes effect on its own.
- The realtime retention field is labelled as not applied. Momento keeps no separate realtime store, so there is nothing for the value to trim; it is still accepted for API compatibility, and the screen says what it does instead of leaving a control that silently does nothing.
- Cardinality limits, PII detection mode, retention of raw events and sessions, blocked properties, query string stripping, allowed domains and contract mode were checked and are enforced.

## v0.28.0

- The site's maximum exact query period now applies to every analytical report. It was consulted by one handler — the query builder — while twenty-eight others read whatever range they were asked for, so an administrator who lowered the limit to protect the database still had every heavy report reading without one. The administration screen presented a limit that was not in force.
- A period the policy forbids is refused as `RANGE_EXCEEDS_POLICY` rather than folded into a malformed-range error, and the refusal names the current limit so a reader can pick a period that will work.
- The period control offers only what the policy allows: the limit travels to the console with the site. If even the shortest period is over the limit the control keeps it, so the reader sees the refusal and its reason instead of an empty control.
- Measured the wider periods added in v0.27.0 against two million events. They are not slower — the ninety day overview is faster than the thirty day one, because the dominant cost is whole-history work rather than the window. Nothing exceeded the budget, so the ranges shipped last release are safe.

## v0.27.0

- Nine analytical screens gained a period control. The range was written into the request and could not be changed, so asking what happened this week was impossible on the frustration screen and the retention grid was fixed at six months. Experience comparison, retention, adoption, frustration, search analytics, feature intelligence, workspace roll-up, AI analytics and ecommerce now choose their own period.
- That also completes the recovery added in v0.26.0. The advice for a query that runs out of time starts with narrowing the range, and the slowest screen in the product was one where the range could not be narrowed; the button now appears there because there is something for it to do.
- Each screen offers the periods that suit it rather than one list everywhere: retention over 90, 180 and 365 days because cohorts are measured in months, feature intelligence over 30, 60 and 90, the rest over 7, 30 and 90. Insights and data quality stay at seven days — they report the current state rather than a period.
- The period control stays visible while the query runs and while an error is shown, so the range can be changed without leaving the screen.
- Renamed the retention grid's "기간" field to "표시 주차": it selects how many weeks the grid shows, which is a different thing from the period being analysed, and having both on screen made the old label ambiguous.

## v0.26.0

- A failed query now offers the recovery instead of describing it. The server already answered a timeout with advice — narrow the range, use a segment, run it in Fast mode, have it delivered on a schedule — and the console printed that as text under a generic "요청을 완료하지 못했습니다", leaving the reader to find where any of those live. Each step is now a button that goes there.
- The advice is only offered where it can be taken. A screen with a fixed range does not get a "narrow the range" button, and neither does a reader already on the shortest one; a permission error gets no buttons at all, because nothing on that screen would fix it.
- Cancelled, failed and rejected queries are told apart. A cancelled query says that leaving the screen cancels it, a failed one keeps the server's message for reporting, and being over the segment comparison limit says why the limit exists.
- Waiting says what is happening. After eight seconds the loading state reports that the query is taking longer than usual, and after twenty that it is approaching the limit — while there is still time to act rather than after the failure.
- `APIError` no longer uses constructor parameter properties, which the test runner cannot parse, so error-handling logic can be covered by tests.

## v0.25.1

- Added a scale guard that CI runs on every push. Two severe defects shipped because every test ran against a few hundred rows, where a query that scans the site per person and one that reads the table in visitor order both finish instantly. Fifty thousand events seed in two seconds and make the first class unmissable: with the aggregate that shipped before v0.24.1 restored, every segment-carrying report answers 504 and the query builder runs for nearly eight minutes on that data.
- Measured what the guard does not catch rather than assuming it catches everything. The rebuild defect fixed in v0.25.0 was a plan choice, not a query shape: with the old statement restored the rebuild finishes in three seconds at fifty thousand events and five at a quarter of a million, because the table still fits in cache. That class needs the two million event harness, and the test file says so.
- Guarded the other half of that fix directly: a test asserts the rebuild refreshes the statistics of the tables it fills, since a later step joins them and a latency budget cannot see the difference at any size CI can afford.

## v0.25.0

- Fixed a rebuild query that did not finish. Deriving visitor identities joined raw events back to a per-visitor subquery, and the planner answered that by reading the whole table in visitor order through an index — millions of random heap fetches. On a two million event site it was still running after thirty-three minutes; three grouped scans and two hash joins produce the same rows in under two seconds. An integration test runs both forms against the same data and compares every link, timestamp included.
- That rebuild runs inside the privacy deletion request, in the same transaction as the delete. On a site of any size the request could not complete, so the deletion it was part of was rolled back: deleting a person's data was not merely slow, it did not finish. The same query backs the full rebuild job, the timezone change and the retention sweep, and it holds a transaction open while it runs — a run left blocked an unrelated statement for ten minutes.
- The rebuild refreshes the planner's statistics before it starts and after each step a later step reads. It empties a table, fills it with hundreds of thousands of rows, and the next step joins it while the planner still believes it holds three — which turned a join of two small tables into a nested loop that ran for five minutes. A database user that may not analyse a table still rebuilds, on whatever statistics exist.
- The load harness now rebuilds the daily rollups the way the aggregation worker does before measuring. Reporting latency against empty rollups measured a path a running deployment rarely takes.

## v0.24.3

- The funnel and retention comparisons run their cohorts together rather than one after another. Comparing three segments used to mean four full evaluations in sequence; the funnel comparison went from 9.1 to 7.3 seconds at the median on a two million event site.
- The load harness now reports the median of three runs with the fastest and slowest alongside it, and checks the budget against the median. A single timing is not evidence: the same probe varied by three seconds between runs on an idle machine, which was enough to make one endpoint look improved and another look regressed when neither had changed.
- Removed the unused request parameter from the funnel evaluator, which never read it.

## v0.24.2

- Added a load harness that seeds two million events and times every analytical endpoint through the real router, failing any report that exceeds a 15 second budget. Correctness tests run against a few hundred rows, where a query that scans the site per person still finishes; this answers the question they cannot.
- Visitor insights was returning a timeout instead of a report. Its visitor bucket query read each person's first-ever activity with a scalar subquery evaluated once per person, against a scan of the site's entire history — the same shape that made behavioural segments unusable. Grouping once and joining is the same answer in one pass: the endpoint went from exceeding the 25 second deadline to 9.6 seconds.
- Every first-seen scan now stops at the end of the period being measured. A row after it cannot move anybody's first event into the window, so reading the rest made these queries grow with the site's whole history rather than with the period.
- The overview no longer waits for the sum of its three reads, and its period aggregate selects the columns it uses rather than every column including both jsonb blobs: 11.9 to 8.1 seconds.
- The experience report runs its four base reads concurrently, and the baseline and per-segment cohorts concurrently rather than one after another: 17.7 to 10.8 seconds with a segment, 6.9 to 3.9 without.

## v0.24.1

- Behavioural segments now run at scale. Every `entity.*` field compiled to an aggregate evaluated once per candidate row, and because `analytics_events.entity_id` is derived from a join with the identity table it cannot be indexed, so each evaluation scanned the site. On a two million event site a thirty day query did not finish inside a minute — past the analytical deadline, which means these segments returned a timeout rather than an answer on any site with real history. The same condition now compiles to a semi-join against one grouped subquery: 2.3 seconds on the same data, and an integration test runs both forms against the same rows to show they select the same people.
- The behavioural aggregate is scoped by the request's site and environment instead of by the outer row. A resolver built without them refuses to compile the aggregate rather than silently measuring the wrong population.
- The Frustration report runs its four independent reads concurrently. Measured serially on the same two million event site they came to roughly 9.6 seconds; the endpoint now waits for the slowest rather than the sum.
- `RunParallel` is exported from the insight package rather than reimplemented, so the connection-pool ceiling stays in one place.

## v0.24.0

- The Frustration report now says whether a signal costs anything. For every signal it compares the people who hit it against the people who did not and returns a verdict — conversion loss, no difference, occurs alongside conversion, or withheld — with the gap in points and an estimate of the conversions that gap accounts for.
- Signals are ranked by estimated lost conversions rather than by the size of the gap or the number of events. A modest gap that most people hit can be worth more than a severe gap almost nobody hits, and the ranking is the part that tells a reader where to start.
- Judgement is withheld unless both sides have at least twenty people. A signal almost everyone hits is withheld too: the handful who avoided it cannot be a baseline.
- A signal that fires on the way to converting — a retried form on the last step of a purchase — is reported as occurring alongside conversion instead of as harm, so nobody is sent to fix it.
- The comparison states that it is an association, not a cause, on the response itself rather than leaving the reader to assume.
- `analyze_frustration` returns the same impact analysis and caveat, so an agent asking about friction gets the ranking rather than raw counts, and no longer carries its own copy of the signal list.

## v0.23.0

- Friction and search are now expressible as audiences. Five behavioural segment fields join the existing ones: `entity.frustration_signals`, `entity.frustration_sessions`, `entity.searches`, `entity.zero_result_searches` and `entity.search_clicks`. "Hit friction twice and never converted" and "searched and found nothing" are now segment definitions, which means they can go straight into the funnel, retention and experience comparisons that already accept segments.
- The Frustration and Search reports hand over the audience instead of naming it. Each offers ready-to-save definitions — people the product blocked, people repeatedly blocked, people whose search returned nothing, people who searched repeatedly and opened nothing — with the count that the report itself measured, so saving a segment cannot disagree with the number the reader just saw.
- The Frustration table links each signal to the people who hit it. `?q=` on the user explorer accepts a search from any report, so a signal leads to real visitors and their timelines rather than to a count.
- The signal list behind the friction fields lives in one place shared by the report, the audiences and the segment aggregates, so they cannot drift apart.
- The audience list is one component now, used by visitor insights and both new reports instead of three copies of the same block.

- Fixed a test that asked the reports for "today" in the runner's timezone while every analytical endpoint answers in the site's. The fixture site is on Asia/Seoul, so a UTC afternoon is already the next day there and an event ingested during the test fell outside the window it was queried with — the v0.22.0 signal test failed in CI for that reason and passed locally. Every integration test now derives its dates from the site calendar.

## v0.22.0

- The tracker now detects the frustration signals the Frustration report has always scored. Rage clicks (three clicks on one element inside a second), dead clicks (a clickable-looking element that changed nothing), rapid backs, form retries from a resubmit or a failed validation, errors within two seconds of a click, and interactions slower than 500ms are all reported without any instrumentation. Seven of the report's nine signals previously arrived only from hand-written code, so the page was empty for every deployment using the tracker as shipped.
- Site search is detected from the query string of the results page, so search counts, click-through and refinements appear without instrumentation. `analytics.trackSearch(query, resultCount)` covers applications that search without changing the URL, and `data-momento-search-results` lets a page publish how many results it rendered, which is what makes the zero-result rate trustworthy. `data-momento-search-position` records which result was opened.
- Search terms stay out of the payload unless the site asks for them with `data-collect-search-terms="true"`, matching how button text is treated. A collected term is normalised, truncated to 100 characters, and stripped of email addresses, phone numbers and resident registration numbers in the browser before the server's own PII policy sees it.
- The events the tracker emits are no longer treated as unregistered by a strict event contract. Turning on reject mode previously required transcribing every built-in event, and shipping a new automatic signal would have dropped whole batches for the sites that had. A site that does register one keeps its own schema and validation mode.
- The Frustration report explains each signal and what to check, and both it and Search Analytics now distinguish "nothing is wrong" from "nothing is measuring": an empty report says which setting or snippet version to look at, and a search table with no terms says the term collection is deliberately off.
- Per-page signal reporting is capped at twenty so a page stuck in a render loop cannot turn into thousands of events.
- `tracker.js` grew from 15.2KB to 23.0KB minified. The detection code ships whether or not the signals are enabled.

## v0.21.4

- Verified privacy deletion end to end against a real database. Deleting by SSO user removes every row across raw events, sessions, visitors, visitor sessions, identity links, identified users and both daily rollups, while another person's data on the same site is untouched. Visitor, period and property deletion keep what is outside their boundary, and property deletion strips the key rather than the event.
- Verified that an export request cannot be downloaded before it is approved, that retention honours the per-site policy in both directions, and that a full aggregate rebuild finishes with no failed job.
- Separated a malformed request body from an invalid value in the privacy decision endpoint, which previously answered "decision must be approve or reject" for a body that carried an extra field.
- Added `Worker.ApplyRetention` and `Maintenance.RunPending` so retention and the aggregate queue can be driven once, by a test or by an operator applying a policy change now.

## v0.21.3

- Fixed the scheduled report kinds `visitor_insight` and `anomaly`, which the API accepted and the database rejected. The check constraint had never learned the kinds added in v0.10 and v0.11, so neither delivery could be created at all: the anomaly alert introduced in v0.11 and the form built for it in v0.19 both targeted a value the database refused.
- Added a regression test that reads the live check constraints and fails when a value the service considers valid would be rejected, covering report kinds, channel types and delivery statuses.
- Added write-path integration coverage: anomaly alert state through new, unchanged, opted-in, recovered and reopened transitions; secret issue, reveal, rotate, reveal again and re-seal; and scheduled delivery producing success, skipped and failed outcomes with the sealed credential actually sent and never listed back.
- Verified that a behavioural segment compiles and evaluates against real data, matching nobody when nobody qualifies rather than silently matching everyone.

## v0.21.2

- Fixed the query cost policy screen, which answered 500 for a site with no stored policy while the query guard silently applied defaults for the same site. Both now read one definition of the defaults, and the response says whether the values are stored or default.
- Extended the integration suite to the two largest unverified surfaces: the collector and worker ingestion path, and every advertised MCP tool. The collector test asserts that a blocked property, a blocked user property and a URL query string never reach storage, and that a wrong tracking key is refused.
- Added coverage for the remaining reports and governance endpoints: path, catalog, lineage, query audit, aggregate jobs, annotations, environments, contracts, semantic metrics and their evaluation, goals, journeys and workspace journeys with analysis, adoption targets, flags, experiments, privacy requests, delivery channels and runs, retention, the tracking debugger, audit, settings, users, networks, encryption status, contract CI validation, and the CSV and NDJSON exports.

## v0.21.1

- Fixed anomaly detection, which had been failing since v0.11: the daily error series aliased a column as `day`, a keyword PostgreSQL rejects in that position, so the endpoint answered 500 and the `anomaly` scheduled report failed every run. The v0.14 rollup change avoided the same mistake for the other four metrics but still reached this query for errors.
- Added an integration suite that runs the analytical endpoints against a real PostgreSQL instance: visitor insights, anomalies, all six attribution models in both scopes, cohort and experience comparison, visitor trace and search, funnel comparison, diagnostics, goals and sixteen reports. It asserts the reports actually compute rather than only that the SQL parses.
- Added a PostgreSQL service to CI so hand-written SQL is executed on every push. The suite skips when `MOMENTO_TEST_POSTGRES_DSN` is unset, so local unit runs are unchanged.

## v0.21.0

- Replaced the delivery channel headers JSON field with name and value rows, finishing what the scheduled report form started: a channel usually needs one credential, and asking for a JSON document turned a two field task into a syntax exercise.
- Reported the input the collector would reject before the request is sent: a duplicated header name, a Host override, a line break, or a name with no value.
- Restructured chapter 3 of the user guide, which eight feature releases had left with a duplicated 2.4, sections nested four levels deep, and the three comparison features scattered across unrelated numbers. Sections now follow the order a reader needs and the document has a table of contents.
- Corrected the user guide version, which had been left at v0.8.0 while the rest of the documentation moved.

## v0.20.0

- Ran the eight independent reads behind the visitor insight report concurrently with a ceiling of four, so the page waits for the slowest few queries instead of the sum of all of them, while one request still cannot exhaust the twenty connection pool.
- Moved the derived arithmetic out of the queries: channel and device shares are computed once every read has returned, rather than being threaded through a query that needed the visitor total.
- Returned the first failure and cancelled the remaining reads, because a partial report presented as a complete one is worse than an error.
- Treated an already cancelled request as cancelled rather than as an empty successful report.

## v0.19.0

- Replaced the hand-written definition document in the scheduled report form with kind-aware inputs: choosing what to send now shows only the values that kind actually uses, and the definition is built from them.
- Gave the anomaly alert its own inputs, which are notification states and an always-send switch rather than an aggregation range, and left the range to the reports that measure a period.
- Added a one-line summary of what will be delivered and how often before the schedule is saved.
- Added deep links from the anomaly card and the visitor insight header into the schedule form with the kind preselected, so a finding turns into a recurring delivery without hunting for the right screen.
- Kept a raw JSON escape hatch for unusual definitions, and stopped writing keys a report kind ignores, which previously read as configuration that did something.

## v0.18.0

- Added cohort comparison to the experience report: Core Web Vitals p75 and error exposure per segment, because a site-wide p75 averages a fast desktop on the office network together with a phone over VPN and hides both.
- Separated "slower" from "no longer acceptable" by checking the published Core Web Vitals thresholds: a cohort that crosses the bar while the baseline stays inside it is reported as critical rather than as a warning.
- Reported only differences worth acting on: at least 30 percent slower, or at least five points more error exposure, and never from fewer than twenty measurements.
- Stated plainly when no cohort is materially worse, instead of leaving an empty panel.

## v0.17.0

- Added cross-service conversion credit: with the workspace scope, a visit on a sibling service in the same workspace can earn credit for a conversion on this one, which is how a notice on one internal system leads to a submission on another.
- Reported credit per originating service alongside the channel breakdown, marking the service where the conversion happened.
- Restricted cross-service credit to people the identity graph knows, because an anonymous visitor is deliberately site scoped and matching them across services would be a guess.
- Widened the scope only to services the reader can already open, using the same access rule as the site list.
- Replaced the attribution parameter list with a query struct so the scope, lookback, model and half life travel together.

## v0.16.0

- Added an attention band to the Overview landing screen that merges detected anomalies and metric goals forecast to miss into one severity-ranked list, each with its evidence, next action and a link to the detail screen.
- Ranked an anomaly above a goal at the same severity, because an anomaly is a change that just happened while a goal is a standing target.
- Left achieved, on-track and not-yet-judged goals out, along with normal and insufficient-history anomalies, so the list itself carries a signal; an empty list states plainly that nothing is wrong.
- Kept the landing screen fast by reading only the rollup-based anomaly report and the metric registry, never the heavy insight report.
- Surfaced the alert state on the landing screen, so a problem reads as newly detected or open for a number of days.

## v0.15.0

- Added segment comparison to retention: up to three segments produce size-weighted average retention curves beside the baseline, with the same cohort definition applied to both who enters a cohort and what counts as a return.
- Excluded cohorts that are not yet old enough to have reached a period from that period's denominator, so a cohort started last week no longer drags a week-four number toward zero.
- Weighted the pooled curve by cohort size rather than averaging rates, which stops a three person cohort from outvoting a thousand person one.
- Named the first-return gap and the period where a segment falls furthest behind, in day, week or month units to match the selected granularity.
- Withheld a verdict for cohorts under twenty people and labelled them as an insufficient sample.

## v0.14.0

- Bounded every interactive analytical read at 25 seconds and cancelled the running statement with it, so one very wide range can no longer hold a database connection until the browser gives up.
- Reported a timeout as a 504 with the reason and the alternatives (narrow the range, apply a segment, schedule the report) instead of an internal error, and separated a client disconnect and a server-side cancel from a genuine failure.
- Built the anomaly baseline from the daily rollups the worker already maintains, falling back to the event table only for a day that has not been aggregated yet; this removes an eight week event scan from every insights page load and makes the numbers match the Overview screen.
- Added the `sessions(site_id, environment, started_at)` index that attribution touch lookups and the landing page report depend on, and pg_trgm indexes for visitor search that degrade to a sequential scan when the extension cannot be created.
- Left the event table without a new index on purpose: a non-concurrent build there would block ingestion during the startup migration.

## v0.13.0

- Added segment comparison to the funnel: up to three segments run beside the baseline with identical steps, mode and conversion window, so a flat overall completion rate becomes a per-cohort comparison.
- Named the step where each cohort loses the most ground against the baseline, and left it empty rather than inventing one when a cohort never falls behind.
- Charted the comparison on completion rate instead of user counts, because putting a small department next to a large one hides the shape of the funnel.
- Withheld a verdict for cohorts with fewer than twenty entrants and labelled them as an insufficient sample instead of reporting noise as a finding.
- Preserved the single-cohort funnel response, so existing callers of `POST /api/v1/funnel` are unaffected; `series` and `comparison` appear only when `compare_segment_ids` is sent.

## v0.12.1

- Bumped the pinned Go toolchain and the builder image to 1.26.7, clearing six standard library advisories (net/http, encoding/xml, encoding/asn1, golang.org/x/net idna) that were fixed in 1.26.6.

## v0.12.0

- Added multi-touch attribution with linear, time-decay (configurable half life) and position-based 40/20/40 models, expressed as weights over one shared path numbering so single-touch and multi-touch models never diverge in definition.
- Made attribution credit fractional: a conversion reached through three visits now contributes a third to each channel instead of naming one winner, and every model's weights sum to exactly one per conversion.
- Added average path length, touched conversions and touch share so the difference between models is visible rather than implied.
- Added anomaly alert state: a detection is reported as new, ongoing for a stated number of days, or recovered, so an hourly schedule stops re-announcing the same drop and announces its recovery once.
- Restricted anomaly delivery to new and recovered transitions by default, with `notify_on` to opt into ongoing alerts and same-day duplicate suppression; reading the report never rewrites alert history because only the delivery path persists state.
- Preserved the v0.11.0 SDK, collector, tracking protocol and privacy contracts. Migration `012_anomaly_state.sql` only adds the alert state table.
- Changed `credited_conversions` and the attribution totals from integers to numbers so fractional credit is representable; single-touch models still return whole numbers.

## v0.11.0

- Rebuilt the visitor timeline as a person-level trace: every visitor ID the deterministic identity graph links to one SSO user is merged into a single chronology, grouped into sessions with entry and exit pages, channel, device, engagement and the gap between consecutive events.
- Added the moment an anonymous visit became a known person as an explicit marker, plus a per-device identity link list showing when each browser profile joined the person.
- Added visitor search by SSO user id, department, organization, visitor id fragment, page URL, event name or feature, with an activity summary per candidate and trace deep links from the session and visitor reports.
- Added cursor paging so a long history can be walked backwards instead of silently stopping at the newest events, cross-service activity for the same SSO user, Markdown export of the trace, and an audit record for every individual-level lookup.
- Added anomaly detection that compares the last complete day against the same weekday median of the previous eight weeks using a median absolute deviation, so weekday seasonality and one-off outages no longer produce false alarms; thin history is reported as unjudged rather than guessed.
- Added the `anomaly` scheduled report kind that delivers only when something is detected, with `skipped` recorded as a normal delivery outcome instead of a failure.
- Added behavioural segment fields (`entity.sessions`, `entity.events`, `entity.conversions`, `entity.days_since_last_seen`, `entity.days_since_first_seen`) and one-click segment creation from every actionable audience in Visitor Insights.
- Added conversion attribution over session-level touchpoints with first-touch, last-touch and last-non-direct models, assisted conversions, assist-only credit and explicit unattributed conversions.
- Added metric goal landing forecasts with elapsed period share, projected value, required daily pace and an on-track verdict; rate metrics are not extrapolated and forecasts are withheld below ten percent of the period.
- Added the `detect_anomalies` and `analyze_attribution` MCP tools, bringing the analytics MCP surface to 22 tools.
- Preserved the v0.10.0 SDK, collector, tracking protocol and privacy contracts. Migration `011_anomaly_alerting.sql` only widens the delivery outcome constraint.

## v0.10.0

- Added a Visitor Insights report that pairs every visitor metric with the previous equivalent period and states the conclusion first: ranked findings that each carry their evidence, a likely cause, and the next action.
- Added default channel grouping over source and medium, including the internal portal, notice and messenger channels an on-premise employee deployment needs, plus a distinct `Direct (사내망)` group for corporate-network visits without acquisition data.
- Added new-versus-returning lifecycle structure, visit frequency and recency distributions, landing page bounce and conversion analysis, and device conversion gap detection.
- Added actionable audiences with counts and recommended next steps: repeat visitors who never convert, first-time visitors who never return, users active only in the previous period, and users returning from dormancy.
- Added one-click takeaway of the whole report as Markdown through clipboard copy and file download, with per-table CSV export retained.
- Added the `visitor_insight` scheduled report kind so the same report is delivered to webhook, mail, Confluence, internal messaging and AI agent channels, and the `get_visitor_insights` MCP tool so an agent can pull it directly.
- Added a goal-aware comparison so metrics whose direction is ambiguous, such as the share of first-time visitors, are no longer coloured as progress or regression.
- Extracted the report into `internal/insight` so the console, MCP surface and scheduled delivery share one narrative, and covered the classification, thresholds and digest rendering with unit tests.
- Preserved the v0.9.0 database, SDK, collector, tracking protocol, REST/OpenAPI and privacy contracts; no migration and no new environment variable.

## v0.9.0

- Added `MOMENTO_ENCRYPTION_KEY` (with the shared `ENCRYPTION_KEY` alias) so personal API keys, site tracking keys, server API keys, OIDC client secrets, and delivery channel headers are stored with AES-256-GCM and survive a restart instead of being lost or re-entered.
- Added key re-display for sites and personal API keys with audit logging, replacing rotation as the only way to recover a lost key.
- Added `MOMENTO_ENCRYPTION_KEY_PREVIOUS` rotation support, an encryption status endpoint, and an administrator re-seal action that finishes a key rotation without a redeploy.
- Fixed collector requests being blocked by the measured application's Content-Security-Policy by shipping the exact `script-src`/`connect-src` policy, a meta tag, and a reverse proxy snippet in the console, the tracking-code API, and the documentation.
- Added SDK `data-endpoint` support for a first-party collector proxy and a CSP violation listener that names the required policy in the browser console.
- Made the console Content-Security-Policy configurable through the public URL origin and a new `additional_connect_origins` security setting, and stopped sending a document policy on collector responses.
- Added a server-side install diagnostics report and console tab covering site state, ingestion volume, CSP guidance, allowed domains, environment match, pipeline backlog, and key recoverability.
- Added console access to previously server-only capabilities: session report, raw event export, delivery run history, delivery channel and schedule deletion, event contract activation and CI validation, semantic metric evaluation, and workspace business journeys.
- Added regression tests for the encryption keyring, environment configuration, CSP construction and guidance, origin allowlist matching, and SDK endpoint resolution.
- Preserved the v0.8.0 database, SDK, collector, tracking protocol, REST/OpenAPI, and privacy contracts; the three required environment variables are unchanged and encryption stays optional.

## v0.8.0

- Fixed Path Analysis rendering failures when real journeys contained bidirectional or cyclic transitions by projecting origins and destinations into separate acyclic graph layers.
- Kept Sankey nodes and links under the same top-transition limit, preventing links from referencing omitted nodes, and added a contextual empty state plus movement summaries.
- Added an operational briefing to Administration with a readiness score, seven-day collection and quality metrics, pending workflow counts, and manual refresh.
- Added a severity-ordered action queue for collector failures, dead letters, failed aggregate jobs, pending privacy requests, data quality degradation, unrestricted origins, administrator redundancy, SSO, and inactive SDK collection.
- Added readiness checks for collection boundaries, URL privacy, value-based PII policy, administrator redundancy, and Enterprise SSO with direct remediation links.
- Added recent administrator activity to the control plane and shareable deep links for Analytics Engineering and Product Lab panels.
- Added a first-class PII value-detection policy editor with server-side validation for `detect`, `warn`, `mask`, and `reject`.
- Added browser-console regression tests for cyclic Path data and graph node/link consistency, and included them in local and CI verification.
- Preserved the v0.7.0 database, SDK, collector, tracking protocol, REST/OpenAPI, privacy, and three-environment-variable runtime contracts.

## v0.7.0

- Reorganized the console into role-aware, collapsible navigation groups for monitoring, web, product, exploration, experience, and AI analytics.
- Added a global `Ctrl/Cmd+K` command palette for pages, analytics functions, and permission-aware administration shortcuts.
- Added persistent breadcrumbs, site/environment context, responsive mobile navigation, and clearer active-location cues.
- Rebuilt Administration as a task-oriented control center with a summary home, grouped sticky navigation, mobile selector, and shareable `?section=` deep links.
- Split Analytics Engineering into Metric/Goal, Query Cost, Aggregate, Change Calendar, and Catalog/Lineage workspaces; split Product Lab into Feature Flag and Experiment workspaces.
- Upgraded shared data tables with search, pagination, result counts, responsive overflow, and UTF-8 CSV export.
- Replaced generic loading and empty states with contextual skeletons, retry guidance, and setup actions.
- Added explicit confirmation UX for full aggregate rebuilds and privacy request decisions, plus clearer required-field and mutation feedback.
- Refined the MUI theme, focus visibility, cards, dialogs, tooltips, reduced-motion behavior, and responsive spacing for an accessible enterprise console.
- Preserved all v0.6.0 API, database, tracking protocol, SDK, privacy, and three-environment-variable runtime contracts.

## v0.6.0

- Added Formula-capable Semantic Metrics with metric references, user/session/event property filters, minimum occurrence rules, owners, scopes, tags, and shared evaluation across REST, Explorer, Goals, and MCP.
- Added occurrence-time Session Property snapshots across SDK, Collector, Raw Events, materialized Sessions, PII filtering, Semantic Metric session scope, and deterministic Raw Event rebuilds.
- Added Metric Goals with target/comparator/period/environment/organization/department ownership and live achievement evaluation.
- Added deterministic Exact/Fast/Preview query modes, complexity scoring, sampling policy, cost rejection, execution metadata, and Query Audit history.
- Added Event Contract CI validation, Event Catalog usage health, Data Lineage from Event to Metric to Goal, and explicit data ownership metadata.
- Added value-based PII detection before the durable inbox with `detect`, `warn`, `mask`, and `reject` policies. Detector samples never retain matching secrets.
- Added Late Event detection and deduplicated automatic date-range aggregate rebuild jobs, plus administrator-requested date-range and full rebuilds.
- Added Cardinality health levels (`low`, `medium`, `high`, `extreme`) and Query Builder eligibility guidance.
- Added Workspace Roll-Up and cross-site Business Journeys using deterministic SSO identity while keeping anonymous Visitors site-scoped.
- Added Service Score, Feature Score, adoption/repeat/conversion/error signals, trend comparison, and Dead Feature candidates.
- Added first-class Search Analytics for Zero Result, CTR, refinement, exit and success; and privacy-preserving Frustration Analytics for rage/dead clicks, retries, errors, and slow interactions.
- Added Feature Flag and Experiment registries with Variant population, Semantic primary metrics, Lift, and two-proportion confidence estimates.
- Added Change Calendar annotations for deployment, release, incident, campaign, training, feature flag and organization changes.
- Added audited Privacy Request workflow for delete/export authorization with separate request and approval steps and complete NDJSON downloads.
- Expanded Analytics MCP from 13 to 19 tools with Workspace Roll-Up, Feature Score, Search, Frustration, Metric Goals, and Event Catalog access.
- Added dedicated React workspaces for Enterprise Analytics, Analytics Engineering, Product Lab, and Privacy Requests.
- Verified both clean PostgreSQL 17 installation and in-place v0.5.0 to v0.6.0 migration while retaining exactly three required runtime environment variables.

## v0.5.0

- Added first-class DEV/STG/PRD and custom environment isolation across the SDK, Collector, Raw Events, daily aggregates, core reports, Funnel, Path, Export, Query API and new analytics workspaces.
- Added immutable Event Contract versions with draft/active/deprecated lifecycle and environment-specific `allow`, `warn`, or `reject` enforcement.
- Added a safe AST-based Semantic Metric Registry with definition versioning, built-in metrics, REST evaluation, Query API support and MCP access.
- Added the Data Quality Center with Tracking Health Score, inbox lag, duplicate/late/rejected events, contract warnings, PII-block counts, missing dimensions, dead letters and daily cardinality guards.
- Added weekly/daily/monthly Cohort and Retention analysis with configurable cohort and return events.
- Added reusable 2-12 step Business Journeys with sequential matching, conversion windows, overall conversion and average elapsed time.
- Added Organization/Department Feature Adoption with eligible-user targets, repeat usage, active and dormant users.
- Added automatic SDK RUM for LCP, INP, CLS, FCP, TTFB, page load and resource errors plus Error Conversion Impact and Release Impact reports.
- Added offline rule-based Insight/Root Cause detection and an offline Korean/English natural-language analytics parser with no external data transfer.
- Added standardized AI/Model/Agent/MCP/Tool analytics for calls, users, success rate, latency, tokens, cost and fallback behavior.
- Added allowlisted Scheduled Reports and Segment actions for Webhook, Confluence, Mail gateway, internal messaging and AI Agent endpoints, with write-only header values and delivery audit history.
- Expanded Analytics MCP from 5 to 13 tools for Semantic Metrics, Retention, Adoption, Experience, AI operations, Data Quality and offline questions.
- Added SDK environment-scoped Visitor/Session/Offline storage and release context fields (`app_version`, `release_version`, `git_sha`, `deployment_id`).
- Preserved v0.4 browser identity continuity by migrating legacy production Visitor, Session, Consent and Offline Queue storage into site-scoped keys.
- Kept the runtime contract at exactly three required environment variables and retained PostgreSQL Raw Events as the immutable source of truth.

## v0.4.0

- Added a deterministic Visitor Identity Graph that links anonymous and authenticated Visitor IDs through pseudonymous `user_id` values without fingerprinting.
- Added canonical User profiles so historical anonymous events inherit the identified user's latest User-scope department, organization, and custom dimensions while Raw Events remain immutable.
- Applied canonical identity to Overview, Realtime, Events, Pages, internal usage, Query Builder, Segment event-existence rules, Funnel, Ecommerce, Session reports, and Analytics MCP.
- Added `visitors`, `identified_users`, `visitor_identities`, `visitor_sessions`, `daily_site_metrics`, `daily_site_visitors`, and `daily_site_sessions` PostgreSQL summaries with automatic v0.3 Raw Event backfill.
- Added an `analytics_events` read model with collision-safe `u:`/`v:` entity identifiers and canonical user properties.
- Added the privacy-controlled Identity Graph REST report, User Explorer graph UI, linked-Visitor navigation, and `query_identity_graph` MCP tool.
- Accelerated Site-local Overview trends with daily aggregates while retaining exact Raw Event fallback for partial-day ranges.
- Rebuilt daily aggregates atomically when a Site timezone changes.
- Expanded User ID privacy deletion to every linked anonymous/device Visitor and atomically rebuilt all affected derived data.

## v0.3.0

- Captured page, device, and first-touch acquisition context at event occurrence time so SPA route changes cannot rewrite queued event context.
- Made `consent-required` fail closed even when browser storage is unavailable, while preserving an in-memory consent grant and the original acquisition context.
- Changed automatic DOM text collection to opt-in, sanitized common PII patterns in browser error messages, and removed query strings and fragments from SDK URLs by default.
- Changed the server privacy baseline to strip URL query strings before the durable inbox write.
- Added user and session conversion counts/rates; retained `conversion_rate` as a compatibility alias for user conversion rate.
- Defined engaged sessions as threshold duration, a conversion, two page views, or sufficient active engagement, and materialized active milliseconds, heartbeat count, and interaction count.
- Added per-site IANA timezone and engagement-threshold settings with an administrator editor and immediate Session summary reconciliation.
- Applied site-local calendar boundaries to dashboard, reports, Query, Funnel, exports, privacy deletion, and MCP date ranges.
- Isolated Worker inbox jobs with PostgreSQL savepoints so a failed event cannot abort retry/dead-letter bookkeeping or block valid jobs in the same batch.
- Made privacy deletion atomically scrub Inbox/Dead Letter payloads, update Raw Events, and rebuild Session summaries.

## v0.2.0

- Added nested AND/OR Segment Registry with personal and shared ownership.
- Applied saved or inline segments to Query Builder and Funnel analysis.
- Added open/closed Funnel modes, per-step property conditions, and completion windows.
- Added saved Exploration definitions and Custom Dimension Registry.
- Added Ecommerce summary, product performance, and commerce funnel reports.
- Added privacy-controlled Visitor Timeline and materialized Session reports.
- Added per-site retention policies and migration-time Session backfill.
- Extended Analytics MCP with Ecommerce and Segment tools.

## v0.1.0

- Initial on-premise event analytics MVP.
- PostgreSQL durable inbox and Raw Event storage, JavaScript SDK, React console, Keycloak OIDC, RBAC, privacy, REST API, and MCP.
- Offline Docker image release workflow.
