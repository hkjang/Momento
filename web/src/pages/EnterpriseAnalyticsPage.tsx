import { useState } from "react";
import { Alert, Box, Button, Card, Chip, Divider, MenuItem, Stack, TextField, Tooltip, Typography } from "@mui/material";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { get, post, rangeQuery } from "../api/client";
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
    <WorkspaceJourneys />
    <DataTable rows={q.data?.services || []} columns={[{ key: "site_name", label: "서비스" }, { key: "site_id", label: "Site ID" }, { key: "users", label: "Users", align: "right" }, { key: "events", label: "Events", align: "right" }, { key: "repeat_rate", label: "재사용률", align: "right", format: (v) => `${Number(v).toFixed(1)}%` }, { key: "conversion_rate", label: "전환율", align: "right", format: (v) => `${Number(v).toFixed(1)}%` }, { key: "error_rate", label: "오류율", align: "right", format: (v) => `${Number(v).toFixed(2)}%` }, { key: "service_score", label: "Service Score", align: "right", format: (v) => <Chip size="small" color={Number(v) >= 80 ? "success" : Number(v) >= 60 ? "warning" : "error"} label={Number(v).toFixed(1)} /> }]} />
  </Stack>;
}

type WorkspaceJourney = { id: string; name: string; description: string; steps: { name: string; event: string; site_id?: string }[]; conversion_window_days: number; shared: boolean };
type JourneyAnalysis = { steps: Record<string, unknown>[]; identity_policy: string };

/**
 * WorkspaceJourneys analyses a business flow that crosses services. The backend
 * has always supported it; this is the console entry point for defining, saving
 * and running one.
 */
function WorkspaceJourneys() {
  const { site, environment } = useSite();
  const qc = useQueryClient();
  const [selected, setSelected] = useState("");
  const [name, setName] = useState("");
  const [steps, setSteps] = useState('[\n  { "name": "포털 진입", "event": "page_view" },\n  { "name": "업무 완료", "event": "feature_used" }\n]');
  const [windowDays, setWindowDays] = useState(30);
  const journeys = useQuery({ queryKey: ["workspace-journeys", site?.site_id], enabled: !!site, queryFn: () => get<WorkspaceJourney[]>(`/api/v1/sites/${site!.site_id}/workspace-journeys`) });
  const save = useMutation({
    mutationFn: () => post(`/api/v1/sites/${site!.site_id}/workspace-journeys`, { name, steps: JSON.parse(steps), conversion_window_days: windowDays, shared: true }),
    onSuccess: () => { setName(""); void qc.invalidateQueries({ queryKey: ["workspace-journeys"] }); },
  });
  const analysis = useMutation({
    mutationFn: (journeyID: string) => post<JourneyAnalysis>(`/api/v1/sites/${site!.site_id}/workspace-journeys/analyze?${rangeQuery(30, site!.timezone)}`, journeyID ? { journey_id: journeyID } : { steps: JSON.parse(steps), conversion_window_days: windowDays }),
  });
  if (!site) return null;
  return <Card sx={{ p: 2.5 }}>
    <Typography variant="h6">Workspace Business Journey</Typography>
    <Typography variant="body2" color="text.secondary" mb={2}>서비스 경계를 넘는 업무 흐름의 단계별 전환을 SSO User 기준으로 분석합니다. 현재 환경은 {environment.toUpperCase()}입니다.</Typography>
    <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "1fr 1fr" }, gap: 2 }}>
      <Stack spacing={1.5}>
        <TextField label="Journey 이름" value={name} onChange={(e) => setName(e.target.value)} />
        <TextField type="number" label="전환 기간(일)" value={windowDays} onChange={(e) => setWindowDays(Number(e.target.value))} />
        <Button variant="contained" disabled={!name || save.isPending} onClick={() => save.mutate()}>Journey 저장</Button>
        {save.error && <ErrorState error={save.error} />}
      </Stack>
      <TextField multiline minRows={6} label="Steps JSON" value={steps} onChange={(e) => setSteps(e.target.value)} helperText="2~12단계. name과 event는 필수이고 site_id·service·feature로 좁힐 수 있습니다." />
    </Box>
    <Divider sx={{ my: 2 }} />
    <Stack direction={{ xs: "column", md: "row" }} spacing={1.5} alignItems={{ md: "center" }}>
      <TextField select fullWidth label="저장된 Journey" value={selected} onChange={(e) => setSelected(e.target.value)}>
        <MenuItem value="">위 Steps JSON으로 즉시 분석</MenuItem>
        {(journeys.data || []).map((item) => <MenuItem key={item.id} value={item.id}>{item.name}</MenuItem>)}
      </TextField>
      <Button variant="outlined" disabled={analysis.isPending} onClick={() => analysis.mutate(selected)}>분석 실행</Button>
    </Stack>
    {analysis.error && <Box mt={2}><ErrorState error={analysis.error} /></Box>}
    {analysis.data && <Box mt={2}>
      <Alert severity="info" sx={{ mb: 2 }}>{analysis.data.identity_policy}</Alert>
      <DataTable rows={analysis.data.steps} exportFilename="momento-workspace-journey" columns={[{ key: "step", label: "단계", align: "right" }, { key: "name", label: "이름" }, { key: "event", label: "Event" }, { key: "site_id", label: "Site", format: (v) => String(v || "전체") }, { key: "users", label: "Users", align: "right" }, { key: "conversion_rate", label: "전환율", align: "right", format: percentCell }, { key: "average_elapsed_seconds", label: "평균 소요(초)", align: "right", format: (v) => Number(v).toFixed(1) }]} />
    </Box>}
  </Card>;
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
  return <Stack spacing={2}>
    <Alert severity="info">착지 예상치는 기간 진행률과 현재 누적 값을 연장한 추정입니다. 비율 지표는 누적되지 않으므로 현재 관측값을 그대로 사용합니다.</Alert>
    <DataTable rows={q.data || []} exportFilename="momento-metric-goals" columns={[
      { key: "name", label: "Goal" },
      { key: "metric_name", label: "Metric" },
      { key: "value", label: "현재", align: "right" },
      { key: "target_value", label: "목표", align: "right" },
      { key: "period", label: "Period" },
      { key: "elapsed_percent", label: "기간 진행", align: "right", format: (v) => v === undefined || v === null ? "—" : `${Number(v).toFixed(0)}%` },
      { key: "progress_percent", label: "Progress", align: "right", format: percentCell },
      { key: "projected_value", label: "착지 예상", align: "right", format: (v, row) => row.forecast_available === false || v === undefined || v === null ? <Tooltip title={String(row.forecast_reason || "추정할 수 없습니다.")}><span>—</span></Tooltip> : Number(v).toLocaleString("ko-KR", { maximumFractionDigits: 1 }) },
      { key: "required_daily_pace", label: "필요 일일 속도", align: "right", format: (v) => v === undefined || v === null ? "—" : Number(v).toLocaleString("ko-KR", { maximumFractionDigits: 1 }) },
      { key: "forecast_status", label: "전망", format: (v, row) => row.forecast_available === false ? <Chip size="small" label="추정 보류" /> : <Chip size="small" color={v === "on_track" ? "success" : "error"} label={v === "on_track" ? "달성 전망" : "미달 전망"} /> },
      { key: "achieved", label: "현재 상태", format: (v) => <Chip size="small" color={v ? "success" : "warning"} label={v ? "달성" : "진행 중"} /> },
    ]} />
  </Stack>;
}

function ChangeCalendar() {
  const { site, environment } = useSite();
  const q = useQuery({ queryKey: ["annotations", site?.site_id, environment], enabled: !!site, queryFn: () => get<Record<string, unknown>[]>(`/api/v1/sites/${site!.site_id}/annotations?${rangeQuery(90, site!.timezone)}`) });
  if (!site) return <NoSite />;
  if (q.isLoading) return <Loading />;
  if (q.error) return <ErrorState error={q.error} />;
  return <Stack spacing={2}><Alert severity="info">배포·릴리즈·장애·교육·캠페인을 분석 Timeline에 함께 기록합니다.</Alert><DataTable rows={q.data || []} columns={[{ key: "occurred_at", label: "시각" }, { key: "kind", label: "종류", format: (v) => <Chip size="small" label={String(v)} /> }, { key: "title", label: "변경" }, { key: "description", label: "설명" }, { key: "source", label: "Source" }, { key: "environment", label: "Env" }]} /></Stack>;
}
