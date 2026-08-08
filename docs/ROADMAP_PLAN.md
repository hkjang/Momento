# Momento 엔터프라이즈 중장기 기술 로드맵 (Product Roadmap Plan)

- **문서 버전**: v0.1.0 ~ v2.0-VISION  
- **작성일자**: 2026년 8월 8일  
- **문서 분류**: 비즈니스 및 아키텍처 중장기 로드맵 (Strategic Product Roadmap)  

---

## 1. 비전 및 발전 마일스톤 개요

Momento 플랫폼은 사내 데이터 주권을 보장하는 온프레미스 원시 이벤트 수집을 시작으로, 사내 AI 데이터 에이전트와 자율 협업하는 차세대 Enterprise Data Platform으로 진화합니다.

```
========================================================================================
                          [Momento 단계별 마일스톤 아키텍처]
========================================================================================
 [Phase 1: v0.1.0] (완료) ➔ Durable Ingestion Engine & Keycloak OIDC / PII Masking
 [Phase 2: v0.5.0] (진행) ➔ Realtime Stream Engine, Funnel/Path Analytics & MCP 1.0
 [Phase 3: v1.0.0] (2026 Q4) ➔ PostgreSQL Partitioning, Anomaly Alert & NL-to-SQL MCP 2.0
 [Phase 4: v2.0.0] (2027)    ➔ Autonomous Data Copilot & Predictive Churn Analytics
========================================================================================
```

---

## 2. Phase별 세부 기술 명세

### 2.1 Phase 1: v0.1.0 온프레미스 기반 수집 구축 (완료)
- **Durable Ingestion Engine**: PostgreSQL Inbox 패턴 기반 유실 없는 비동기 이벤트 수집기 구현.
- **Rich JS Tracker SDK**: PageView, SPA Routing, Click, Form, Scroll, Error, Heartbeat, Offline Queue.
- **보안 & SSO**: Keycloak OIDC PKCE 인증, RBAC 권한 매핑, PII 수집기 레벨 자동 정규식 마스킹.
- **패키징**: 단일 non-root Docker 이미지 및 air-gapped 폐쇄망 `.tar.gz` 오프라인 번들.

### 2.2 Phase 2: v0.5.0 고도화 & 분석 시각화 엔진 (2026년 Q3)
- **고급 시각화**: 2~10단계 유연한 퍼널(Funnel) 및 Sankey 기반 경로 분석(Path Analysis) 시각화.
- **망대역 관리**: CIDR / C-Class 서브넷 IP의 사내 오피스 이름 맵핑 자동화 인터페이스.
- **Analytics MCP 1.0**: Claude, Cursor 연동을 위한 Streamable HTTP MCP JSON-RPC 표준 엔드포인트.

### 2.3 Phase 3: v1.0.0 대용량 확산 & AI 이상 징후 감지 (2026년 Q4)
- **대용량 파티셔닝**: PostgreSQL Table Partitioning 기반 월간 억 단위 데이터 고성능 보존 및 인덱싱.
- **실시간 이상 감지 (Anomaly Alerting)**: 특정 사내 서비스 장애 또는 기능 이탈률 급증 시 Slack/Teams/Webhook 자동 알림.
- **Analytics MCP 2.0 (NL-to-SQL)**: AI 에이전트가 "지난달 서초 오피스에서 가장 많이 사용된 기능 TOP 5는?"과 같은 자연어 질의 시 SQL 자동 변환 처리.

---

## 3. 리소스 및 품질 관리 전략

- **100% 테스트 자동화**: Go 백엔드 유닛 테스트 및 SDK 타입 검사 자동화.
- **무중단 마이그레이션**: PostgreSQL Migration 자동 스키마 적용 및 백워드 호환성 보장.
