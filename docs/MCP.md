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
- `list_segments`: MCP 분석에서 재사용할 수 있는 저장 Segment와 중첩 AND/OR 정의

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
      "from": "2026-08-01",
      "to": "2026-08-08"
    }
  }
}
```
