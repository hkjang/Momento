# Momento 엔터프라이즈 사용자 가이드 (User Guide & Developer Manual)

- **문서 버전**: v0.2.0
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
  data-mode="full"
  data-debug="false"
></script>
```

| 속성 (Attribute) | 타입 | 필수 여부 | 설명 |
| :--- | :--- | :--- | :--- |
| `data-site-id` | String | **필수** | 관리자 콘솔에서 생성한 사이트 고유 식별 키 |
| `data-mode` | String | 선택 | `full`, `consent-required`, `cookieless`, `disabled` 중 선택 |
| `data-debug` | Boolean | 선택 | `true`이면 브라우저 콘솔에 SDK 진단 로그 출력 |

Collector endpoint는 `tracker.js`를 제공한 Origin의 `/collect/v1/events`로 자동 설정됩니다. Page View, SPA History 변경, 클릭, 스크롤, Form, Download, Outbound Link, Error와 Heartbeat는 기본 자동 수집됩니다.

---

### 2.2 사용자 및 부서 식별 (`analytics.identify`)

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
> 이메일 주소, 전화번호, 주민등록번호, 카드번호 등 개인식별정보(PII)는 `user_id` 또는 속성으로 전달하지 마십시오. 수집기는 관리자가 지정한 Property key를 제거하고 URL Parameter를 마스킹하지만, 값 자체를 정규식으로 판별하지는 않습니다.

---

### 2.3 커스텀 이벤트 트래킹 (`analytics.track`)

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

---

### 2.4 SPA (Single Page Application) 라우트 변경 추적

React, Vue, Next.js 등 SPA 라우터 전환 시 페이지뷰를 수동 또는 자동 수집합니다.

```javascript
// History API 또는 라우터 변경 감지 시
router.on('routeChangeComplete', () => {
  analytics.track("page_view");
});
```

---

### 2.5 오프라인 큐 & Beacon 재전송 메커니즘

- **Beacon API**: 브라우저 닫힘 또는 페이지 이동 시 이벤트 손실 방지를 위해 `navigator.sendBeacon`을 이용합니다.
- **Offline Queue**: 사내 Wi-Fi/네트워크 유실 시 최근 이벤트를 `localStorage`에 보관하며, 네트워크 복구 시 자동으로 배치 재전송합니다.

---

## 3. 고급 분석 기능 사용법

### 3.1 쿼리 빌더 (Query Builder)
1. **필터링 조건**: 날짜 범위, 부서, 특정 이벤트명, 커스텀 속성(Key-Value) 조건 설정.
2. **그룹핑 (GroupBy)**: `department`별 또는 `browser`별 시계열 집계 그래프 생성.

### 3.2 2~10단계 퍼널 분석 (Funnel Analysis)
- 서비스 진입 ➔ 주요 기능 탐색 ➔ 최종 결재/제출까지 단계별 전환율 및 이탈율(Drop-off) 시각화.

### 3.3 사용자 경로 분석 (Path Analysis)
- 진입 페이지부터 이탈 페이지까지의 이동 경로를 Sankey 다이어그램으로 분석.

### 3.4 Segment와 저장된 Exploration
- Segment 화면에서 최대 5단계의 중첩 `AND`/`OR` 조건과 14개 연산자를 조합합니다.
- `event.has = purchase`를 사용하면 조회 행의 Event 종류와 무관하게 구매 경험이 있는 사용자를 선택합니다.
- 저장된 Segment는 Query Builder와 Funnel에서 재사용할 수 있으며 Query 조합 자체도 Exploration으로 저장합니다.

### 3.5 Ecommerce와 User Explorer
- Ecommerce는 `view_item`, `add_to_cart`, `begin_checkout`, `purchase`, `refund`와 `items` 배열을 기준으로 매출·거래·상품 성과를 계산합니다.
- User Explorer는 Visitor별 Event Timeline을 제공하며 관리자가 Visitor Profile을 비활성화하면 API와 화면이 모두 차단됩니다.

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
