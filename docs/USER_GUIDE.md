# Momento 사용자 가이드 (User Guide)

- **버전**: v0.1.0  
- **대상**: 사내 웹/앱 서비스 개발자, 데이터 분석가, 서비스 기획자  
- **문서 목적**: Momento JavaScript SDK 연동 방법, 이벤트 트래킹 규칙, 쿼리 빌더 활용 및 퍼널/경로 분석 가이드  

---

## 1. 플랫폼 개요

Momento는 사내 웹 및 애플리케이션의 사용자 행동 데이터를 수집하고 분석하는 **온프레미스 이벤트 애널리틱스 플랫폼**입니다. 기존 외부 SaaS와 달리 사내 데이터가 외부에 전송되지 않으며, 부서 및 인프라 네트워크 단위의 사용자 행동을 원시 이벤트(Raw Event) 수준에서 조회할 수 있습니다.

---

## 2. JavaScript SDK 연동 가이드

### 2.1 스크립트 태그 삽입

사내 웹 서비스의 `<head>` 영역에 아래 코드 비동기로 추가합니다. `data-site-id`에는 관리자 콘솔에서 발급받은 Site Tracking Key를 입력합니다.

```html
<script async src="https://momento.example.com/tracker.js" data-site-id="SITE_CORPORATE_001"></script>
```

### 2.2 사용자 식별 (`identify`)

로그인한 사용자 및 사내 부서/조직 정보를 식별할 때 호출합니다.

```javascript
// 사용자 식별 예시
analytics.identify("EMP_2026_1042", {
  department: "Digital Platform Team",
  organization: "R&D Center",
  position: "Senior Engineer"
});
```

> **주의사항**: 주민등록번호, 이메일 주소, 전화번호 등 개인 식별 정보(PII)는 `user_id` 또는 속성값으로 전달하지 마십시오.

### 2.3 커스텀 이벤트 트래킹 (`track`)

특정 버튼 클릭, 문서 다운로드, 서식 제출 등 비즈니스 기능을 추적합니다.

```javascript
// 이벤트 추적 예시
analytics.track("document_exported", {
  format: "PDF",
  document_type: "Monthly_Report",
  button_id: "export_btn_top"
});
```

---

## 3. 주요 분석 기능 활용

### 3.1 실시간(Realtime) & 개요(Overview)
- 현재 접속 중인 사내 활성 사용자 수와 분당 이벤트 발생량 추이를 실시간으로 모니터링합니다.
- 인기 페이지, 상위 이벤트, 브라우저 및 디바이스 환경 통계를 제공합니다.

### 3.2 쿼리 빌더 (Query Builder)
- 커스텀 이벤트 속성, 부서별 필터, 날짜 범위를 조합하여 원하는 맞춤 데이터를 즉시 필터링 및 조회할 수 있습니다.

### 3.3 퍼널 분석 (Funnel Analysis)
- 2단계부터 최대 10단계까지의 목표 전환 퍼널을 설정하고, 단계별 전환율 및 이탈 지점을 시각적으로 파악합니다.

### 3.4 경로 분석 (Path Analysis)
- 사용자가 진입 후 어떤 화면과 기능을 거쳐 이탈하거나 최종 목표에 도달했는지 노드 그래프로 시각화합니다.

---

## 4. 데이터 내보내기 (Export)

- 분석 결과를 Raw CSV 및 NDJSON 형식으로 다운로드하여 사내 BI 도구(Tableau, PowerBI 등)와 연동할 수 있습니다.
