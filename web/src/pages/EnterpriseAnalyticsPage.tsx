import { useState } from "react";
import { Alert, Box, Card, Chip, MenuItem, Stack, TextField, Typography } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import { get, rangeQuery } from "../api/client";
import { useSite } from "../contexts/SiteContext";
import DataTable from "../components/DataTable";
import MetricCard from "../components/MetricCard";
import { ErrorState, Loading, NoSite } from "../components/States";

export type EnterpriseAnalyticsMode = "workspace" | "features" | "search" | "frustration" | "experiments" | "goals" | "calendar";

export default function EnterpriseAnalyticsPage({ mode }: { mode: EnterpriseAnalyticsMode }) {
  if (mode === "workspace") return <WorkspaceRollup />;
  if (mode === "features") return <FeatureIntelligence />;
  if (mode === "search") return <SearchAnalytics />;
  if (mode === "frustration") return <Frustration />;
  if (mode === "experiments") return <Experiments />;
  if (mode === "goals") return <Goals />;
  return <ChangeCalendar />;
}

function WorkspaceRollup() {
  const { site, environment } = useSite();
  const q = useQuery({ queryKey: ["workspace-rollup", site?.site_id, environment], enabled: !!site, queryFn: () => get<{ summary: Record<string, number>; services: Record<string, unknown>[] }>(`/api/v1/sites/${site!.site_id}/workspace-rollup?${rangeQuery(30, site!.timezone)}`) });
  if (!site) return <NoSite />;
  if (q.isLoading) return <Loading />;
  if (q.error) return <ErrorState error={q.error} />;
  return <Stack spacing={2}>
    <Alert severity="info">동일 SSO User ID는 사이트를 넘어 한 사람으로 계산하고, 익명 Visitor는 사이트별로 격리합니다.</Alert>
    <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr 1fr", lg: "repeat(4,1fr)" }, gap: 2 }}>
      <MetricCard label="등록 서비스" value={q.data?.summary.registered_services || 0} />
      <MetricCard label="Workspace Users" value={q.data?.summary.users || 0} />
      <MetricCard label="Sessions" value={q.data?.summary.sessions || 0} />
      <MetricCard label="Events" value={q.data?.summary.events || 0} />
    </Box>
    <DataTable rows={q.data?.services || []} columns={[{ key: "site_name", label: "서비스" }, { key: "site_id", label: "Site ID" }, { key: "users", label: "Users", align: "right" }, { key: "events", label: "Events", align: "right" }, { key: "repeat_rate", label: "재사용률", align: "right", format: (v) => `${Number(v).toFixed(1)}%` }, { key: "conversion_rate", label: "전환율", align: "right", format: (v) => `${Number(v).toFixed(1)}%` }, { key: "error_rate", label: "오류율", align: "right", format: (v) => `${Number(v).toFixed(2)}%` }, { key: "service_score", label: "Service Score", align: "right", format: (v) => <Chip size="small" color={Number(v) >= 80 ? "success" : Number(v) >= 60 ? "warning" : "error"} label={Number(v).toFixed(1)} /> }]} />
  </Stack>;
}

function FeatureIntelligence() {
  const { site, environment } = useSite();
  const q = useQuery({ queryKey: ["feature-intelligence", site?.site_id, environment], enabled: !!site, queryFn: () => get<{ population: number; features: Record<string, unknown>[] }>(`/api/v1/sites/${site!.site_id}/feature-intelligence?${rangeQuery(60, site!.timezone)}`) });
  if (!site) return <NoSite />;
  if (q.isLoading) return <Loading />;
  if (q.error) return <ErrorState error={q.error} />;
  return <Stack spacing={2}>
    <Alert severity="info">Feature Score = Adoption 40% + 재사용 30% + 전환 20% + 오류 없음 10%. 대상자 설정이 없으면 관측 활성 사용자를 분모로 사용합니다.</Alert>
    <DataTable rows={q.data?.features || []} columns={[{ key: "feature", label: "Feature" }, { key: "users", label: "Users", align: "right" }, { key: "adoption_rate", label: "Adoption", align: "right", format: percentCell }, { key: "repeat_rate", label: "Repeat", align: "right", format: percentCell }, { key: "conversion_rate", label: "Conversion", align: "right", format: percentCell }, { key: "trend_percent", label: "기간 추세", align: "right", format: (v) => `${Number(v) >= 0 ? "+" : ""}${Number(v).toFixed(1)}%` }, { key: "feature_score", label: "Score", align: "right" }, { key: "dead_feature", label: "판정", format: (v) => <Chip size="small" color={v ? "warning" : "success"} label={v ? "Dead 후보" : "활성"} /> }]} />
  </Stack>;
}

const percentCell = (value: unknown) => `${Number(value).toFixed(1)}%`;

function SearchAnalytics() {
  const { site, environment } = useSite();
  const q = useQuery({ queryKey: ["search-analytics", site?.site_id, environment], enabled: !!site, queryFn: () => get<{ summary: Record<string, number>; queries: Record<string, unknown>[] }>(`/api/v1/sites/${site!.site_id}/search-analytics?${rangeQuery(30, site!.timezone)}`) });
  if (!site) return <NoSite />;
  if (q.isLoading) return <Loading />;
  if (q.error) return <ErrorState error={q.error} />;
  return <Stack spacing={2}>
    <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr 1fr", lg: "repeat(4,1fr)" }, gap: 2 }}><MetricCard label="Searches" value={q.data?.summary.searches || 0} /><MetricCard label="Search Users" value={q.data?.summary.users || 0} /><MetricCard label="Zero Result" value={q.data?.summary.zero_result_rate || 0} type="percent" /><MetricCard label="Search CTR" value={q.data?.summary.search_ctr || 0} type="percent" /></Box>
    <DataTable rows={q.data?.queries || []} columns={[{ key: "query", label: "Query" }, { key: "searches", label: "검색", align: "right" }, { key: "users", label: "Users", align: "right" }, { key: "zero_results", label: "Zero Result", align: "right" }, { key: "clicks", label: "Clicks", align: "right" }, { key: "ctr", label: "CTR", align: "right", format: percentCell }, { key: "last_seen", label: "Last Seen" }]} />
  </Stack>;
}

function Frustration() {
  const { site, environment } = useSite();
  const q = useQuery({ queryKey: ["frustration", site?.site_id, environment], enabled: !!site, queryFn: () => get<{ summary: Record<string, number>; signals: Record<string, unknown>[] }>(`/api/v1/sites/${site!.site_id}/frustration?${rangeQuery(30, site!.timezone)}`) });
  if (!site) return <NoSite />;
  if (q.isLoading) return <Loading />;
  if (q.error) return <ErrorState error={q.error} />;
  return <Stack spacing={2}>
    <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "repeat(3,1fr)" }, gap: 2 }}><MetricCard label="영향 Session" value={q.data?.summary.affected_sessions || 0} /><MetricCard label="영향률" value={q.data?.summary.affected_session_rate || 0} type="percent" /><MetricCard label="평균 Frustration Score" value={q.data?.summary.average_frustration_score || 0} /></Box>
    <Alert severity="info">Session Replay 없이 행동 신호만 사용해 개인정보 노출을 줄입니다.</Alert>
    <DataTable rows={q.data?.signals || []} columns={[{ key: "signal", label: "Signal" }, { key: "count", label: "Count", align: "right" }, { key: "users", label: "Users", align: "right" }, { key: "sessions", label: "Sessions", align: "right" }, { key: "weight", label: "Weight", align: "right" }, { key: "last_seen", label: "Last Seen" }]} />
  </Stack>;
}

function Experiments() {
  const { site, environment } = useSite();
  const [selected, setSelected] = useState("");
  const list = useQuery({ queryKey: ["experiments", site?.site_id], enabled: !!site, queryFn: () => get<Record<string, unknown>[]>(`/api/v1/sites/${site!.site_id}/experiments`) });
  const analysis = useQuery({ queryKey: ["experiment-analysis", selected, environment], enabled: !!site && !!selected, queryFn: () => get<{ method: string; variants: Record<string, unknown>[] }>(`/api/v1/sites/${site!.site_id}/experiments/${selected}/analysis?${rangeQuery(90, site!.timezone)}`) });
  if (!site) return <NoSite />;
  if (list.isLoading) return <Loading />;
  if (list.error) return <ErrorState error={list.error} />;
  return <Stack spacing={2}>
    <Card sx={{ p: 2.5 }}><TextField select fullWidth label="Experiment" value={selected} onChange={(e) => setSelected(e.target.value)}><MenuItem value="">선택</MenuItem>{(list.data || []).map((item) => <MenuItem key={String(item.id)} value={String(item.id)}>{String(item.name)} · {String(item.status)}</MenuItem>)}</TextField></Card>
    {analysis.isLoading ? <Loading /> : analysis.error ? <ErrorState error={analysis.error} /> : analysis.data ? <><Alert severity="info">{analysis.data.method}</Alert><DataTable rows={analysis.data.variants} columns={[{ key: "variant", label: "Variant" }, { key: "users", label: "Users", align: "right" }, { key: "conversion_rate", label: "Conversion", align: "right", format: percentCell }, { key: "metric_value", label: "Primary Metric", align: "right" }, { key: "lift_percent", label: "Lift", align: "right", format: percentCell }, { key: "confidence_percent", label: "Confidence", align: "right", format: percentCell }, { key: "is_control", label: "Role", format: (v) => v ? "Control" : "Variant" }]} /></> : <Card sx={{ p: 6, textAlign: "center" }}><Typography color="text.secondary">관리자가 등록한 Experiment를 선택하세요.</Typography></Card>}
  </Stack>;
}

function Goals() {
  const { site, environment } = useSite();
  const q = useQuery({ queryKey: ["metric-goal-evaluation", site?.site_id, environment], enabled: !!site, queryFn: () => get<Record<string, unknown>[]>(`/api/v1/sites/${site!.site_id}/metric-goals/evaluate`) });
  if (!site) return <NoSite />;
  if (q.isLoading) return <Loading />;
  if (q.error) return <ErrorState error={q.error} />;
  return <DataTable rows={q.data || []} columns={[{ key: "name", label: "Goal" }, { key: "metric_name", label: "Metric" }, { key: "value", label: "현재", align: "right" }, { key: "target_value", label: "목표", align: "right" }, { key: "period", label: "Period" }, { key: "progress_percent", label: "Progress", align: "right", format: percentCell }, { key: "achieved", label: "상태", format: (v) => <Chip size="small" color={v ? "success" : "warning"} label={v ? "달성" : "진행 중"} /> }]} />;
}

function ChangeCalendar() {
  const { site, environment } = useSite();
  const q = useQuery({ queryKey: ["annotations", site?.site_id, environment], enabled: !!site, queryFn: () => get<Record<string, unknown>[]>(`/api/v1/sites/${site!.site_id}/annotations?${rangeQuery(90, site!.timezone)}`) });
  if (!site) return <NoSite />;
  if (q.isLoading) return <Loading />;
  if (q.error) return <ErrorState error={q.error} />;
  return <Stack spacing={2}><Alert severity="info">배포·릴리즈·장애·교육·캠페인을 분석 Timeline에 함께 기록합니다.</Alert><DataTable rows={q.data || []} columns={[{ key: "occurred_at", label: "시각" }, { key: "kind", label: "종류", format: (v) => <Chip size="small" label={String(v)} /> }, { key: "title", label: "변경" }, { key: "description", label: "설명" }, { key: "source", label: "Source" }, { key: "environment", label: "Env" }]} /></Stack>;
}
