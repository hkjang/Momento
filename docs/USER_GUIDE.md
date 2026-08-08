# Momento 엔터프라이즈 사용자 가이드 (User Guide & Developer Manual)

- **문서 버전**: v0.1.0-ENTERPRISE  
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
  data-endpoint="https://momento.internal/collect/v1/events"
  data-auto-track="pageview,click,error,download"
></script>
```

| 속성 (Attribute) | 타입 | 필수 여부 | 설명 |
| :--- | :--- | :--- | :--- |
| `data-site-id` | String | **필수** | 관리자 콘솔에서 생성한 사이트 고유 식별 키 |
| `data-endpoint` | String | 선택 | 수집기 URL (기본값: 현재 오리진의 `/collect/v1/events`) |
| `data-auto-track` | String | 선택 | 자동 트래킹 요소 (`pageview`, `click`, `error`, `form`, `download`, `outbound`) |

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
> 이메일 주소, 전화번호, 주민등록번호, 카드번호 등 개인식별정보(PII)는 `user_id` 또는 속성으로 전달하지 마십시오. 수집기 레벨에서 자동 마스킹 및 차단 처리됩니다.

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
router.on('routeChangeComplete', (url) => {
  analytics.pageview({
    title: document.title,
    path: url,
    referrer: document.referrer
  });
});
```

---

### 2.5 오프라인 큐 & Beacon 재전송 메커니즘

- **Beacon API**: 브라우저 닫힘 또는 페이지 이동 시 이벤트 손실 방지를 위해 `navigator.sendBeacon`을 이용합니다.
- **Offline Queue**: 사내 Wi-Fi/네트워크 유실 시 `localStorage` / `IndexedDB` 큐에 이벤트를 보관하며, 네트워크 복구 시 자동으로 배치 재전송을 수행합니다.

---

## 3. 고급 분석 기능 사용법

### 3.1 쿼리 빌더 (Query Builder)
1. **필터링 조건**: 날짜 범위, 부서, 특정 이벤트명, 커스텀 속성(Key-Value) 조건 설정.
2. **그룹핑 (GroupBy)**: `department`별 또는 `browser`별 시계열 집계 그래프 생성.

### 3.2 2~10단계 퍼널 분석 (Funnel Analysis)
- 서비스 진입 ➔ 주요 기능 탐색 ➔ 최종 결재/제출까지 단계별 전환율 및 이탈율(Drop-off) 시각화.

### 3.3 사용자 경로 분석 (Path Analysis)
- 진입 페이지부터 이탈 페이지까지의 이동 경로를 Sankey 다이어그램으로 분석.

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
