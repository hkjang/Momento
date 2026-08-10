# Momento 엔터프라이즈 사용자 가이드 (User Guide & Developer Manual)

- **문서 버전**: v0.8.0
- **대상**: 웹/앱 개발자, 데이터 분석가, 서비스 기획자(PO), BI 엔지니어  
- **문서 개요**: Momento JavaScript SDK 상세 연동법, 이벤트 트래킹 규칙, 쿼리 빌더, 퍼널 및 경로 분석, BI 데이터 내보내기 실전 매뉴얼  

---

## 1. 플랫폼 아키텍처 개요

Momento는 사내 애플리케이션 및 인트라넷 환경에서 발생하는 모든 행동 이벤트를 외부 SaaS로 전송하지 않고 사내 DB에 원시 이벤트(Raw Event) 수준으로 직접 저장·분석하는 온프레미스 플랫폼입니다.

---

## 2. JavaScript SDK 연동 및 설정 가이드

### 2.1 스크립트 비동기 설치

웹 애플리케이션의 `<head>` 영역에 아래 스크립트를 비동기로 삽입합니다.

```html
<!-- Momento JavaScript Tracker SDK -->
<script 
  async 
  src="https://momento.internal/tracker.js" 
  data-site-id="SITE_CORPORATE_001"
  data-environment="prd"
  data-contract-version="1"
  data-mode="full"
  data-debug="false"
></script>
```

| 속성 (Attribute) | 타입 | 필수 여부 | 설명 |
| :--- | :--- | :--- | :--- |
| `data-site-id` | String | **필수** | 관리자 콘솔에서 생성한 사이트 고유 식별 키 |
| `data-environment` | String | 선택 | `dev`, `stg`, `prd` 등 관리자에게 등록한 환경. 기본 `prd` |
| `data-contract-version` | Number | 선택 | 전송 Event Contract version. 기본 `1` |
| `data-mode` | String | 선택 | `full`, `consent-required`, `cookieless`, `disabled` 중 선택 |
| `data-debug` | Boolean | 선택 | `true`이면 브라우저 콘솔에 SDK 진단 로그 출력 |
| `data-collect-element-text` | Boolean | 선택 | 버튼 문구 수집. 개인정보 최소화를 위해 기본값은 `false` |
| `data-auto-rum` | Boolean | 선택 | Core Web Vitals와 Resource Error 자동 수집. 기본 `true` |
| `data-release-version` | String | 선택 | Release Impact 비교용 애플리케이션 릴리스 |
| `data-git-sha` | String | 선택 | 배포 소스 revision |

Collector endpoint는 `tracker.js`를 제공한 Origin의 `/collect/v1/events`로 자동 설정됩니다. Page View, SPA History 변경, 클릭, 스크롤, Form, Download, Outbound Link, Error, Heartbeat, LCP/INP/CLS/FCP/TTFB와 Resource Error는 기본 자동 수집됩니다.

각 이벤트에는 `track()` 호출 순간의 URL, 제목, Referrer, Device와 최초 UTM Context가 snapshot으로 저장됩니다. 따라서 1초 배치 전송을 기다리는 동안 SPA Route가 바뀌어도 이전 페이지의 Click이 새 페이지로 잘못 분류되지 않습니다. SDK가 자동 수집하는 URL에서는 Query String과 Fragment를 제거합니다.

### 2.2 Consent와 Cookieless

| 모드 | 동의 전 | Visitor 저장 | 용도 |
| :--- | :--- | :--- | :--- |
| `disabled` | Event 없음 | 없음 | 추적 중지 |
| `consent-required` | Event 없음 | 동의 후에만 저장 | 분석 동의가 필수인 환경 |
| `cookieless` | Event 수집 | 영속 저장 없음 | 익명 분석 |
| `full` | Event 수집 | 영속 Visitor/Session | 허용된 내부 분석 |

`consent-required`는 `localStorage`가 차단된 브라우저에서도 안전하게 수집을 중지합니다. `analytics.consent.grant()`는 저장소가 없어도 현재 Page에서 유효하며, 동의를 기다리는 동안에도 최초 UTM은 유지합니다. `deny()`와 `revoke()`는 대기열·영속 식별자·Offline Queue를 정리합니다.

### 2.3 사용자 및 부서 식별 (`analytics.identify`)

로그인한 사용자의 사내 식별자와 부서/조직 체계 정보를 수집기에 전달합니다.

```javascript
// 사용자 로그인 시 호출
analytics.identify("EMP_2026_9012", {
  department: "Digital Platform Team",
  organization: "R&D Center",
  role: "Senior Architect",
  location: "HQ_Seoul"
});
```

> ⚠️ **보안 수칙 (Security Policy)**:  
> 이메일 주소, 전화번호, 주민등록번호, 카드번호 등 개인식별정보(PII)는 `user_id` 또는 속성으로 전달하지 마십시오. 수집기는 관리자가 지정한 Property key 제거와 URL 정책에 더해 값 기반 PII 탐지·마스킹을 Inbox 저장 전에 수행하지만, 애플리케이션의 최소 수집 책임을 대신하지는 않습니다.

---

### 2.4 커스텀 이벤트 트래킹 (`analytics.track`)

사용자의 특정 비즈니스 액션을 세밀하게 기록합니다.

```javascript
// 서식 제출 이벤트
analytics.track("document_submitted", {
  document_id: "DOC_2026_0808",
  category: "Approval",
  amount: 1500000,
  approval_step: "Final"
});

// 파일 다운로드 이벤트
analytics.track("file_downloaded", {
  file_name: "Q2_Financial_Report.pdf",
  file_size_mb: 14.2,
  download_source: "Intranet_Notice"
});
```

세션 동안 유지할 로그인 상태나 업무 흐름은 Event Property와 구분해 설정할 수 있습니다. 값은 각 Event 발생 시점에 snapshot되므로 배치 전송이나 Raw Event 재집계 후에도 Session Scope Dimension이 동일합니다.

```javascript
analytics.setSessionProperties({
  login_status: "authenticated",
  workflow: "approval"
});
```

---

### 2.5 SPA (Single Page Application) 라우트 변경 추적

React, Vue, Next.js 등 SPA 라우터 전환 시 페이지뷰를 수동 또는 자동 수집합니다.

```javascript
// History API 또는 라우터 변경 감지 시
router.on('routeChangeComplete', () => {
  analytics.track("page_view");
});
```

---

### 2.6 오프라인 큐 & Beacon 재전송 메커니즘

- **Beacon API**: 브라우저 닫힘 또는 페이지 이동 시 이벤트 손실 방지를 위해 `navigator.sendBeacon`을 이용합니다.
- **Offline Queue**: 사내 Wi-Fi/네트워크 유실 시 최근 이벤트를 `localStorage`에 보관하며, 네트워크 복구 시 자동으로 배치 재전송합니다.

---

## 3. 고급 분석 기능 사용법

Overview의 `conversion_rate`는 호환성을 위해 User Conversion Rate를 의미합니다. API는 `conversion_users`, `conversion_sessions`, `user_conversion_rate`, `session_conversion_rate`를 모두 제공합니다. 날짜는 관리자에게 설정된 Site Timezone 기준이며, 저장 Timestamp 자체는 UTC입니다.

### 3.1 쿼리 빌더 (Query Builder)
1. **필터링 조건**: 날짜 범위, 부서, 특정 이벤트명, 커스텀 속성(Key-Value) 조건 설정.
2. **그룹핑 (GroupBy)**: `department`별 또는 `browser`별 시계열 집계 그래프 생성.

### 3.2 2~10단계 퍼널 분석 (Funnel Analysis)
- 서비스 진입 ➔ 주요 기능 탐색 ➔ 최종 결재/제출까지 단계별 전환율 및 이탈율(Drop-off) 시각화.

### 3.3 사용자 경로 분석 (Path Analysis)
- 동일 Session에서 연속으로 발생한 Page·Event 이동을 시작/도착 두 계층 Sankey 다이어그램으로 분석합니다. 왕복 이동도 안전하게 표시하며 현재 Environment의 최근 30일 전환 수와 이동 횟수를 함께 제공합니다.

### 3.4 Segment와 저장된 Exploration
- Segment 화면에서 최대 5단계의 중첩 `AND`/`OR` 조건과 14개 연산자를 조합합니다.
- `event.has = purchase`를 사용하면 조회 행의 Event 종류와 무관하게 구매 경험이 있는 사용자를 선택합니다.
- 저장된 Segment는 Query Builder와 Funnel에서 재사용할 수 있으며 Query 조합 자체도 Exploration으로 저장합니다.

### 3.5 Ecommerce와 User Explorer
- Ecommerce는 `view_item`, `add_to_cart`, `begin_checkout`, `purchase`, `refund`와 `items` 배열을 기준으로 매출·거래·상품 성과를 계산합니다.
- User Explorer는 Visitor별 Event Timeline과 Deterministic Identity Graph를 제공합니다. 같은 `user_id`로 식별된 브라우저·기기의 Visitor ID는 하나의 canonical user로 연결되며, 로그인 전 익명 Event도 해당 사용자의 Funnel·Segment·전환 및 부서/조직 분석에 포함됩니다.
- Momento는 fingerprint나 확률 기반 결합을 사용하지 않습니다. `analytics.identify()` 또는 SSO에서 받은 내부 pseudonymous ID만 신뢰하며, 관리자가 Visitor Profile을 비활성화하면 Identity Graph API와 화면도 함께 차단됩니다.

### 3.6 Cohort, Business Journey와 Feature Adoption

- Cohort는 최초 Event/가입/구매 등 Cohort Event와 Return Event를 분리해 Day/Week/Month Retention을 계산합니다.
- Business Journey는 2~12개의 Event·Service·Feature 조건을 실제 도달 순서와 Conversion Window로 연결합니다.
- Feature Adoption은 Canonical User의 Organization/Department와 Event의 `feature`를 연결하며 대상자, 사용률, 재사용률, 최근 활성 및 비활성 사용자를 제공합니다.

### 3.7 Experience와 Release Impact

SDK의 자동 RUM은 LCP, INP, CLS, FCP, TTFB, Load와 Resource Error를 `web_vital`, `resource_error` Event로 전송합니다. Release 비교가 필요하면 초기화 시 `releaseVersion`, `gitSha`, `deploymentId`를 지정하십시오. Experience 화면은 오류가 발생한 사용자와 정상 사용자의 전환율을 비교합니다.

### 3.8 AI / Agent / MCP Event 표준

`ai_prompt`, `ai_response`, `ai_model_call`, `ai_tool_call`, `ai_agent_run`, `ai_mcp_call`을 사용하고 `model`, `provider`, `agent`, `mcp_server`, `tool`, `success`, `latency_ms`, `input_tokens`, `output_tokens`, `cost`, `fallback_model`을 Property로 전달합니다. 실제 Prompt/Response 원문은 개인정보와 기밀정보 위험 때문에 기본 분석 규격에 포함하지 않는 것을 권장합니다.

### 3.9 Workspace, Feature, Search와 Frustration

- Workspace Roll-Up은 같은 Workspace의 Site를 합쳐 서비스별 사용자·Event·Session·Service Score를 비교합니다. 같은 SSO `user_id`는 Site를 넘어 한 명으로 계산하고 익명 Visitor는 Site별로 격리합니다.
- Feature Intelligence는 `feature` Property를 기준으로 Adoption, Repeat, Conversion, Error, 기간 추세와 Dead Feature 후보를 계산합니다.
- Search Analytics는 `search`, `search_result`, `search_click`, `search_no_result`, `search_refine`, `search_exit`, `search_success` 표준 Event를 사용합니다.
- Frustration Analytics는 Replay를 저장하지 않고 `rage_click`, `dead_click`, `rapid_back`, `form_retry`, `repeated_search`, `error_after_click`, `slow_interaction`과 오류 Event만으로 막힘을 추정합니다.

### 3.10 Experiment와 Goal

- Experiment Event에는 `experiment_id`와 `variant` Property를 함께 전송합니다. 등록한 Semantic Metric을 Variant별로 계산하고 첫 Variant를 Control로 사용해 Lift와 Confidence를 제공합니다.
- Goal 화면은 일/주/월/분기 Metric 목표의 현재값, 진행률과 달성 여부를 Site Timezone 기준으로 표시합니다.
- Change Calendar는 배포, Release, 장애, 캠페인, 교육, Feature Flag와 조직 변경을 분석 기간에 함께 표시합니다.

---

## 4. BI 연동 & 데이터 내보내기 (Export)

Raw CSV / NDJSON 형식으로 원클릭 내보내기가 가능합니다.

```python
# Python Pandas 연동 예시
import pandas as pd

# Momento Export NDJSON 읽기
df = pd.read_json('momento_events_20260808.ndjson', lines=True)

# 부서별 이벤트 건수 분석
dept_stats = df.groupby('department')['event_name'].value_counts()
print(dept_stats)
```

## 5. Query Mode와 비용 보호

- `Exact`는 Raw Event 100%를 계산합니다. 관리자가 정한 최대 기간과 Complexity를 넘으면 실행 전에 거부됩니다.
- `Fast`와 `Preview`는 Event ID의 결정적 Hash로 관리자 설정 비율을 표본화합니다. 같은 입력은 같은 표본을 사용합니다.
- 결과에는 Query Mode, Complexity Score, Sample Percent, 실행 분류와 보수적인 예상 오차 안내가 포함됩니다.

## 6. 개인정보 요청 Workflow

관리자는 Privacy Requests에서 삭제 또는 Export 요청을 먼저 생성하고 별도의 승인 동작으로 실행합니다. User ID 삭제는 Identity Graph에 연결된 Visitor까지 포함하며, 기간 삭제는 Site Timezone 경계를 사용합니다. 요청자·승인자·결과 건수·상태는 Audit와 요청 이력에 남습니다. 승인 완료된 Export는 이벤트·사용자·세션 속성을 포함한 전체 NDJSON으로 내려받을 수 있습니다.

## 7. Console 탐색과 표 활용

- 좌측 메뉴는 모니터링, 웹 분석, 제품 분석, 탐색·실험, 경험·AI로 묶여 있으며 현재 화면이 속한 그룹만 자동으로 펼쳐집니다.
- `Ctrl+K` 또는 macOS의 `Cmd+K`를 누르면 화면, 분석 기능, 관리자 설정을 이름으로 검색해 바로 이동할 수 있습니다. 일반 사용자는 권한이 없는 관리자 명령을 볼 수 없습니다.
- 상단 Breadcrumb와 Site·Environment Context를 확인하면 현재 분석 범위를 놓치지 않을 수 있습니다.
- 데이터 표는 행 검색, 페이지당 행 수 변경과 CSV 내보내기를 공통으로 지원합니다. CSV는 현재 검색 결과를 UTF-8 BOM 형식으로 저장합니다.
- 모바일에서는 Sidebar 대신 Drawer와 축약된 Site Selector를 사용하며, 넓은 표는 가로 스크롤로 열 손실 없이 확인합니다.
