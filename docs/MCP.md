# Momento Analytics MCP

Momento는 Streamable HTTP 형태의 MCP endpoint를 제공합니다.

```text
POST https://momento.example.com/mcp
Authorization: Bearer mom_key_...
Content-Type: application/json
```

개인화 화면의 **API 키 · MCP**에서 키를 발급하고 회전/폐기할 수 있습니다. 키 원문은 발급 직후 한 번만 표시되고 서버에는 SHA-256 digest만 저장됩니다.

지원 도구:

- `query_metrics`: 기간별 users, new_users, sessions, page_views, events, engagement_rate, conversions, revenue
- `analyze_internal_usage`: department, organization, service, feature, button, network 차원별 사용량
- `query_ecommerce`: 매출, 환불, 순매출, 거래, 구매자, 평균 주문 금액, 구매 전환율
- `query_identity_graph`: 내부 User ID에 결정적으로 연결된 Visitor 목록, 최초/연결/최근 활동, Event와 Conversion 수. Visitor Profile 정책이 활성화된 경우에만 사용 가능
- `list_segments`: MCP 분석에서 재사용할 수 있는 저장 Segment와 중첩 AND/OR 정의
- `list_semantic_metrics`: Metric Registry의 정의·Format·Version 조회
- `query_semantic_metric`: 등록된 Semantic Metric을 Environment와 기간 기준으로 계산
- `analyze_retention`: Event 기반 주차별 Cohort/Retention 분석
- `analyze_feature_adoption`: 조직·부서·기능별 도입률, 대상 인원, 재사용률, 미사용자(Adoption 화면과 동일한 계산)
- `analyze_experience`: Web Vitals P75, Error와 영향 사용자 분석
- `analyze_ai_operations`: Model·Provider·Agent·MCP Server·Tool별 호출, 지연, Token 분석
- `inspect_data_quality`: 수집·중복·계약·PII·Cardinality 품질 분석
- `get_workspace_rollup`: Workspace 전체 서비스와 교차 사이트 SSO 사용자 Roll-Up
- `get_feature_scores`: Feature Adoption·재사용·전환과 Dead Feature 후보
- `analyze_search`: 검색량·Zero Result·CTR·성공률
- `analyze_frustration`: Rage/Dead Click·재시도·오류·느린 상호작용 신호와 신호별 전환 영향(판정·전환 손실 추정·연관성 주의)
- `get_metric_goals`: Semantic Metric 목표와 관리 범위
- `get_event_catalog`: Event Owner·Contract Version·Volume·Last Seen
- `get_visitor_insights`: 방문자 인사이트 전체 보고서. 전기간 대비 KPI, 신규·재방문, 채널 그룹, 진입 페이지, 방문 빈도·최근성, 기기, 실행 대상 Segment와 우선순위 인사이트를 한 번에 반환
- `detect_anomalies`: 직전 완료된 하루를 같은 요일 최근 8주 중위수와 비교한 이상 감지 결과와 신규·지속·회복 알림 상태
- `analyze_attribution`: 단일 터치(`last_non_direct`, `first_touch`, `last_touch`)와 다중 터치(`linear`, `time_decay`, `position_based`) 모델별 채널 전환 기여도, 관여 전환, 평균 경로 길이. `half_life_days`로 시간 감쇠 반감기를, `scope=workspace`로 교차 서비스 배분을 조정합니다
- `ask_analytics`: 외부 LLM 호출 없는 오프라인 한국어/영어 Analytics 질의

초기화 예제:

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"client","version":"1"}}}
```

도구 호출 예제:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": {
    "name": "analyze_internal_usage",
    "arguments": {
      "site_id": "SITE_12345678",
      "dimension": "department",
      "environment": "prd",
      "from": "2026-08-01",
      "to": "2026-08-08"
    }
  }
}
```

모든 기간형 도구는 선택적인 `environment`를 받으며 생략 시 `prd`를 조회합니다. `ask_analytics`는 Momento DB 밖으로 질문이나 Event를 전송하지 않는 결정적 Parser입니다.

Identity Graph 조회 예제:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "query_identity_graph",
    "arguments": {
      "site_id": "SITE_12345678",
      "user_id": "EMP_2026_9012"
    }
  }
}
```

## 조회 기간 제한

MCP 도구도 사이트의 `최대 정확 조회 기간` 정책을 따릅니다. 한도를 넘는 `from`/`to`를 전달하면 현재 한도를 알려주는 오류를 반환합니다. v0.30.3 이전에는 도구가 제한 없이 조회할 수 있었습니다.
