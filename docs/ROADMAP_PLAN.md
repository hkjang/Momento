# Momento 중장기 로드맵 및 발전 계획 (Roadmap Plan)

- **작성일**: 2026년 8월 8일  
- **버전**: v0.1.0 ~ v2.0 Vision  
- **문서 분류**: 로드맵 및 중장기 기술 비전 문서 (Strategic Product Roadmap)  

---

## 1. 단계별 로드맵 개요

```
+-------------------------------------------------------------------------------+
|                        Momento 중장기 성장 단계 (Phases)                      |
+-------------------------------------------------------------------------------+
| [Phase 1: v0.1.0] (완료) ➔ 온프레미스 인프라 구축 및 Raw Event 수집기          |
| [Phase 2: v0.5.0] (진행중) ➔ 실시간 퍼널/경로 엔진 & Analytics MCP 확장        |
| [Phase 3: v1.0.0] (Q4 2026) ➔ Enterprise Multi-Cluster & AI Anomaly Detection|
| [Phase 4: v2.0.0] (2027) ➔ 사내 자율형 데이터 인사이트 Copilot 생태계        |
+-------------------------------------------------------------------------------+
```

---

## 2. 세부 마일스톤 (Detailed Milestones)

### 2.1 Phase 1: v0.1.0 온프레미스 기반 구축 (현재 완료)
- Durable Collector 개발 (PostgreSQL Inbox 기반 async ingest)
- Rich JavaScript SDK (PageView, SPA, Custom Events, Offline Queue)
- Keycloak OIDC (PKCE) 인증 및 PII 마스킹 필터 엔진
- Analytics MCP (Model Context Protocol) 기본 지원
- Single Non-root Docker Image 및 에어갭 `.tar.gz` 배포 번들

### 2.2 Phase 2: v0.5.0 고도화 & 분석 분석 엔진 강화 (2026년 Q3)
- 2~10단계 유연한 퍼널(Funnel) 및 Sankey 기반 경로 분석(Path Analysis) 시각화
- 사내 C클래스/CIDR 서브넷 매핑 자동화 인터페이스
- 쿼리 빌더 성능 개선 및 Raw CSV / NDJSON 대용량 스트리밍 내보내기

### 2.3 Phase 3: v1.0.0 엔터프라이즈 확산 및 AI 이상 징후 감지 (2026년 Q4)
- PostgreSQL 파티셔닝 기반 사내 억 단위 대용량 이벤트 보존 수집 엔진
- 사내 서비스 장애 및 이탈률 급증 자동 알림 (Slack/Teams/Webhook)
- AI 에이전트 전용 자연어 SQL 쿼리 생성 지원 (MCP 2.0 연동)

---

## 3. 리소스 및 품질 관리 전략

- **100% 테스트 자동화**: Go 백엔드 유닛 테스트 및 SDK 타입 검사 자동화
- **무중단 마이그레이션**: PostgreSQL Migration 자동 스키마 적용 및 백워드 호환 보장
- **보안 감사 및 PII 컴플라이언스**: 분기별 PII 필터링 룰셋 및 OIDC 감사 수행
