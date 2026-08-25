import assert from "node:assert/strict";
import test from "node:test";
import {
  buildInsightMarkdown,
  changeTone,
  formatChange,
  formatInsightValue,
} from "../src/pages/visitorInsights.ts";

const report = {
  environment: "prd",
  timezone: "Asia/Seoul",
  from: "2026-07-12T15:00:00Z",
  to: "2026-08-11T15:00:00Z",
  previous_from: "2026-06-12T15:00:00Z",
  previous_to: "2026-07-12T15:00:00Z",
  days: 30,
  headline: "최근 30일 방문자 1234명(전기간 대비 +12.3%)",
  kpis: [
    {
      key: "users",
      label: "방문자",
      format: "number",
      current: 1234,
      previous: 1099,
      change_percent: 12.3,
      goal: "higher",
    },
    {
      key: "new_user_share",
      label: "신규 비중",
      format: "percent",
      current: 45.6,
      previous: 40,
      change_percent: 14,
      goal: "neutral",
    },
  ],
  findings: [
    {
      id: "onboarding_gap",
      title: "신규 방문자의 전환율이 재방문자보다 낮습니다",
      severity: "warning",
      evidence: "신규 4.0%, 재방문 18.0%",
      cause: "첫 방문 경험의 안내가 부족할 수 있습니다.",
      action: "신규 Segment로 Funnel을 비교하십시오.",
      impact: 28,
    },
  ],
  lifecycle: [
    {
      kind: "new",
      users: 560,
      sessions: 600,
      sessions_per_user: 1.07,
      conversion_rate: 4,
      share_percent: 45.6,
    },
    {
      kind: "returning",
      users: 674,
      sessions: 1500,
      sessions_per_user: 2.23,
      conversion_rate: 18,
      share_percent: 54.4,
    },
  ],
  channels: [
    {
      channel: "Internal Portal",
      users: 700,
      sessions: 900,
      converted_users: 90,
      conversion_rate: 12.9,
      previous_users: 500,
      change_percent: 40,
      user_share_percent: 56.7,
    },
  ],
  landing_pages: [
    {
      page: "/search",
      sessions: 400,
      bounce_rate: 82,
      engagement_rate: 18,
      conversion_rate: 2,
      average_seconds: 95,
      session_share_percent: 45,
    },
  ],
  frequency: [],
  recency: [],
  devices: [],
  audiences: [
    {
      key: "loyal_not_converted",
      label: "3회 이상 방문했지만 미전환",
      users: 42,
      action: "Funnel과 Frustration으로 장애 요인 확인",
    },
    { key: "returned", label: "휴면 후 복귀", users: 0, action: "복귀 계기 확인" },
  ],
  notes: ["채널 합계는 중복 사용자를 포함할 수 있습니다."],
};

test("인사이트 요약 Markdown은 결론과 근거를 함께 담는다", () => {
  const markdown = buildInsightMarkdown(report, "사내 포털");

  assert.match(markdown, /^# 방문자 인사이트 · 사내 포털 · PRD/);
  assert.ok(markdown.includes(report.headline));
  assert.match(markdown, /분석 기간 2026-07-12 ~ 2026-08-11/);
  assert.match(markdown, /비교 기간 2026-06-12 ~ 2026-07-12, Asia\/Seoul 기준/);

  const findingIndex = markdown.indexOf("## 핵심 인사이트");
  const kpiIndex = markdown.indexOf("## 지표");
  assert.ok(findingIndex > 0 && findingIndex < kpiIndex, "결론이 지표보다 앞에 온다");
  assert.match(markdown, /1\. \[경고\] 신규 방문자의 전환율이 재방문자보다 낮습니다/);
  assert.match(markdown, /- 근거: 신규 4\.0%, 재방문 18\.0%/);
  assert.match(markdown, /- 다음 행동: 신규 Segment로 Funnel을 비교하십시오\./);

  assert.match(markdown, /\| 방문자 \| 1,234 \| 1,099 \| \+12\.3% \|/);
  assert.match(markdown, /\| 신규 방문자 \| 560 \| 45\.6% \| 1\.07 \| 4\.0% \|/);
  assert.match(markdown, /\| Internal Portal \| 700 \| 56\.7% \| 12\.9% \| \+40\.0% \|/);
  assert.match(markdown, /\| \/search \| 400 \| 45\.0% \| 82\.0% \| 2\.0% \|/);
});

test("실행 대상은 인원이 있는 항목만 요약에 넣는다", () => {
  const markdown = buildInsightMarkdown(report, "사내 포털");

  assert.match(markdown, /- 3회 이상 방문했지만 미전환: 42명 →/);
  assert.doesNotMatch(markdown, /휴면 후 복귀/);
});

test("비어 있는 구간은 표를 만들지 않는다", () => {
  const markdown = buildInsightMarkdown(
    { ...report, findings: [], channels: [], landing_pages: [], notes: [] },
    "사내 포털",
  );

  assert.doesNotMatch(markdown, /## 핵심 인사이트/);
  assert.doesNotMatch(markdown, /## 유입 채널/);
  assert.doesNotMatch(markdown, /## 해석 주의/);
  assert.match(markdown, /## 지표/);
});

test("변화 방향은 지표의 목표를 따른다", () => {
  assert.equal(changeTone({ ...report.kpis[0], change_percent: 12 }), "good");
  assert.equal(changeTone({ ...report.kpis[0], change_percent: -12 }), "bad");
  assert.equal(
    changeTone({ ...report.kpis[0], goal: "lower", change_percent: -12 }),
    "good",
    "낮을수록 좋은 지표는 하락이 개선이다",
  );
  assert.equal(
    changeTone({ ...report.kpis[1], change_percent: 30 }),
    "flat",
    "방향이 모호한 지표는 성과로 표시하지 않는다",
  );
  assert.equal(changeTone({ ...report.kpis[0], change_percent: 0.3 }), "flat");
});

test("값과 변화 표기를 형식에 맞춘다", () => {
  assert.equal(formatInsightValue(1234, "number"), "1,234");
  assert.equal(formatInsightValue(45.67, "percent"), "45.7%");
  assert.equal(formatInsightValue(1.234, "decimal"), "1.23");
  assert.equal(formatInsightValue(95, "duration"), "1분 35초");
  assert.equal(formatChange(0), "변화 없음");
  assert.equal(formatChange(-3.14), "-3.1%");
  assert.equal(formatChange(3.14), "+3.1%");
});

test("이상 감지 결과는 요약 앞부분에 근거와 함께 들어간다", async () => {
  const { buildInsightMarkdown } = await import("../src/pages/visitorInsights.ts");
  const anomalies = {
    environment: "prd",
    evaluated_date: "2026-08-10T00:00:00Z",
    timezone: "Asia/Seoul",
    baseline_weeks: 8,
    note: "직전 완료된 하루를 비교합니다.",
    detected: [
      {
        metric: "users",
        label: "방문자",
        date: "2026-08-10T00:00:00Z",
        value: 120,
        baseline: 1000,
        change_percent: -88,
        robust_z: -6.2,
        severity: "critical",
        direction: "below",
        samples: 8,
        weekday: "월",
        evidence: "2026-08-10(월) 120 · 같은 요일 기준선 1000 · 편차 -6.2σ · -88.0%",
        action: "채널 변화를 확인하십시오.",
      },
    ],
    checked: [],
  };

  const markdown = buildInsightMarkdown(report, "사내 포털", anomalies);
  const anomalyIndex = markdown.indexOf("## 이상 감지");
  const findingIndex = markdown.indexOf("## 핵심 인사이트");
  assert.ok(anomalyIndex > 0, "이상 감지 구간이 있어야 한다");
  assert.ok(anomalyIndex < findingIndex, "이상 감지는 인사이트보다 앞에 온다");
  assert.match(markdown, /2026-08-10 기준, 같은 요일 최근 8주 비교/);
  assert.match(markdown, /\[심각\] 방문자: .*편차 -6\.2σ.* → 채널 변화를 확인하십시오\./);
});

test("이상이 없으면 요약에 이상 감지 구간을 만들지 않는다", async () => {
  const { buildInsightMarkdown } = await import("../src/pages/visitorInsights.ts");
  const markdown = buildInsightMarkdown(report, "사내 포털", {
    environment: "prd",
    evaluated_date: "2026-08-10T00:00:00Z",
    timezone: "Asia/Seoul",
    baseline_weeks: 8,
    note: "",
    detected: [],
    checked: [],
  });
  assert.doesNotMatch(markdown, /## 이상 감지/);
});

test("이상 감지 요약은 신규·지속 상태와 회복을 함께 적는다", async () => {
  const { buildInsightMarkdown } = await import("../src/pages/visitorInsights.ts");
  const anomalies = {
    environment: "prd",
    evaluated_date: "2026-08-10T00:00:00Z",
    timezone: "Asia/Seoul",
    baseline_weeks: 8,
    note: "",
    detected: [
      {
        metric: "users",
        label: "방문자",
        date: "2026-08-10T00:00:00Z",
        value: 120,
        baseline: 900,
        change_percent: -86.7,
        robust_z: -5.1,
        severity: "critical",
        direction: "below",
        samples: 8,
        weekday: "월",
        evidence: "근거 문장",
        action: "채널 변화를 확인하십시오.",
      },
    ],
    checked: [],
    notify_on: ["new", "recovered"],
    transitions: [
      {
        metric: "users",
        label: "방문자",
        state: "ongoing",
        severity: "critical",
        days_open: 3,
        robust_z: -5.1,
        evidence: "근거 문장",
        notifiable: false,
      },
      {
        metric: "errors",
        label: "오류",
        state: "recovered",
        severity: "warning",
        days_open: 2,
        robust_z: 0,
        evidence: "기준선 범위로 돌아왔습니다.",
        notifiable: true,
      },
    ],
  };

  const markdown = buildInsightMarkdown(report, "사내 포털", anomalies);
  assert.match(markdown, /\[심각\] \(지속 3일\) 방문자: 근거 문장 → 채널 변화를 확인하십시오\./);
  assert.match(markdown, /\[회복\] 오류: 기준선 범위로 돌아왔습니다\./);
});

test("전환 배분은 소수 기여만 소수로 표기한다", async () => {
  const { formatCredit, stateSummary } = await import("../src/pages/visitorInsights.ts");

  assert.equal(formatCredit(12), "12");
  assert.equal(formatCredit(12.04), "12");
  assert.equal(formatCredit(12.35), "12.4");
  assert.equal(formatCredit(0.333), "0.3");

  assert.equal(stateSummary({ state: "new", days_open: 1 }), "신규");
  assert.equal(stateSummary({ state: "ongoing", days_open: 4 }), "지속 4일");
  assert.equal(stateSummary({ state: "recovered", days_open: 3 }), "회복");
});
