import { useEffect, useState } from "react";
import {
  Alert,
  Box,
  Button,
  Card,
  Chip,
  Divider,
  MenuItem,
  Stack,
  TextField,
  Typography,
} from "@mui/material";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { del, get, post, put, rangeQuery, type SiteEnvironment } from "../api/client";
import { useAuth } from "../contexts/AuthContext";
import { useSite } from "../contexts/SiteContext";
import DataTable from "../components/DataTable";
import { ErrorState, Loading, NoSite } from "../components/States";

export default function PlatformAdminPage({ mode }: { mode: "governance" | "automation" }) {
  const { user } = useAuth();
  if (user?.role === "analyst" || user?.role === "viewer") return <Alert severity="warning">관리자 권한이 필요합니다.</Alert>;
  return mode === "governance" ? <Governance /> : <Automation />;
}

function Governance() {
  const { site, environment } = useSite();
  const qc = useQueryClient();
  const envs = useQuery({ queryKey: ["platform-environments", site?.site_id], enabled: !!site, queryFn: () => get<SiteEnvironment[]>(`/api/v1/sites/${site!.site_id}/environments`) });
  const contracts = useQuery({ queryKey: ["event-contracts", site?.site_id], enabled: !!site, queryFn: () => get<Record<string, unknown>[]>(`/api/v1/sites/${site!.site_id}/event-contracts`) });
  const metrics = useQuery({ queryKey: ["semantic-metrics", site?.site_id], enabled: !!site, queryFn: () => get<Record<string, unknown>[]>(`/api/v1/sites/${site!.site_id}/semantic-metrics`) });
  const targets = useQuery({ queryKey: ["adoption-targets", site?.site_id], enabled: !!site, queryFn: () => get<Record<string, unknown>[]>(`/api/v1/sites/${site!.site_id}/adoption-targets`) });
  const [eventName, setEventName] = useState("");
  const [eventOwner, setEventOwner] = useState("");
  const [eventSchema, setEventSchema] = useState('{\n  "required": [],\n  "properties": {}\n}');
  const [metricName, setMetricName] = useState("");
  const [metricDefinition, setMetricDefinition] = useState('{"type":"count"}');
  const [target, setTarget] = useState({ organization: "", department: "", feature: "", eligible_users: 0 });
  const contractSave = useMutation({
    mutationFn: () => post(`/api/v1/sites/${site!.site_id}/event-contracts`, { event_name: eventName, owner: eventOwner, schema: JSON.parse(eventSchema), validation_mode: "warn", changelog: "Admin Console", activate: true }),
    onSuccess: () => { setEventName(""); void qc.invalidateQueries({ queryKey: ["event-contracts"] }); },
  });
  const metricSave = useMutation({
    mutationFn: () => post(`/api/v1/sites/${site!.site_id}/semantic-metrics`, { name: metricName, label: metricName, description: "Admin Console metric", definition: JSON.parse(metricDefinition), format: "number", status: "active" }),
    onSuccess: () => { setMetricName(""); void qc.invalidateQueries({ queryKey: ["semantic-metrics"] }); },
  });
  const targetSave = useMutation({ mutationFn: () => post(`/api/v1/sites/${site!.site_id}/adoption-targets`, target), onSuccess: () => { setTarget({ organization: "", department: "", feature: "", eligible_users: 0 }); void qc.invalidateQueries({ queryKey: ["adoption-targets"] }); } });
  // Activating a stored version is what makes it the collection contract, so the
  // registry needs the action next to every row instead of only on creation.
  const activate = useMutation({
    mutationFn: (row: { event: string; version: number }) => post(`/api/v1/sites/${site!.site_id}/event-contracts/${encodeURIComponent(row.event)}/${row.version}/activate`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["event-contracts"] }),
  });
  const [ciPayload, setCIPayload] = useState('{\n  "events": [\n    { "name": "feature_used", "contract_version": 1, "properties": { "feature": "document_search" } }\n  ]\n}');
  const ciCheck = useMutation({
    mutationFn: () => post<CIValidation>(`/api/v1/sites/${site!.site_id}/event-contracts/validate`, { environment, ...(JSON.parse(ciPayload) as Record<string, unknown>) }),
  });
  const [metricName2, setMetricName2] = useState("");
  const metricQuery = useQuery({
    queryKey: ["semantic-metric-value", site?.site_id, metricName2, environment],
    enabled: !!site && !!metricName2,
    queryFn: () => get<{ label: string; value: number; format: string; unit: string; definition_version: number }>(`/api/v1/sites/${site!.site_id}/semantic-metrics/${encodeURIComponent(metricName2)}/query?${rangeQuery(30, site!.timezone)}`),
  });
  if (!site) return <NoSite />;
  if (envs.isLoading || contracts.isLoading || metrics.isLoading) return <Loading />;
  const error = envs.error || contracts.error || metrics.error || targets.error;
  if (error) return <ErrorState error={error} />;
  return <Stack spacing={3}>
    <Card sx={{ p: 2.5 }}>
      <Typography variant="h6">Environment Policy</Typography>
      <Typography variant="body2" color="text.secondary" mb={2}>SDK/Server API의 환경을 물리적으로 분리하고 계약 강도와 일별 Cardinality 상한을 관리합니다.</Typography>
      <Stack spacing={1.5}>{envs.data?.map((item) => <EnvironmentEditor key={item.name} siteID={site.site_id} item={item} />)}</Stack>
    </Card>
    <Card sx={{ p: 2.5 }}>
      <Typography variant="h6">Event Contract Versioning</Typography>
      <Typography variant="body2" color="text.secondary" mb={2}>Draft를 만들고 활성화한 버전만 수집 계약의 기준이 됩니다. 현재 선택 환경은 {environment.toUpperCase()}입니다.</Typography>
      <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "1fr 1fr" }, gap: 2 }}>
        <Stack spacing={1.5}><TextField label="Event Name" value={eventName} onChange={(e) => setEventName(e.target.value)} /><TextField label="Owner" value={eventOwner} onChange={(e) => setEventOwner(e.target.value)} /><Button variant="contained" disabled={!eventName || contractSave.isPending} onClick={() => contractSave.mutate()}>Version 생성 및 활성화</Button>{contractSave.error && <ErrorState error={contractSave.error} />}</Stack>
        <TextField multiline minRows={7} label="JSON Schema" value={eventSchema} onChange={(e) => setEventSchema(e.target.value)} />
      </Box>
      <Divider sx={{ my: 2 }} />
      <DataTable rows={contracts.data || []} columns={[{ key: "event_name", label: "Event" }, { key: "owner", label: "Owner" }, { key: "version", label: "Version", align: "right" }, { key: "status", label: "Status", format: (v) => <Chip size="small" label={String(v)} color={v === "active" ? "success" : "default"} /> }, { key: "validation_mode", label: "Validation" }, { key: "changelog", label: "Changelog" }, { key: "event_name", label: "", align: "right", format: (v, row) => row.status === "active" ? <Chip size="small" color="success" label="사용 중" /> : <Button size="small" disabled={activate.isPending} onClick={() => activate.mutate({ event: String(v), version: Number(row.version) })}>활성화</Button> }]} />
      {activate.error && <Box mt={2}><ErrorState error={activate.error} /></Box>}
      <Divider sx={{ my: 2 }} />
      <Typography fontWeight={700}>배포 전 CI 검증</Typography>
      <Typography variant="body2" color="text.secondary" mb={1.5}>배포 파이프라인이 호출하는 것과 같은 검증을 콘솔에서 실행합니다. 활성 Contract와 환경 정책을 함께 적용합니다.</Typography>
      <Stack spacing={1.5}>
        <TextField multiline minRows={5} label="검증할 이벤트 JSON" value={ciPayload} onChange={(e) => setCIPayload(e.target.value)} />
        <Box><Button variant="outlined" disabled={ciCheck.isPending} onClick={() => ciCheck.mutate()}>{environment.toUpperCase()} 기준 검증</Button></Box>
        {ciCheck.error && <ErrorState error={ciCheck.error} />}
        {ciCheck.data && <>
          <Alert severity={ciCheck.data.valid ? "success" : "error"}>오류 {ciCheck.data.errors}건, 경고 {ciCheck.data.warnings}건입니다.</Alert>
          <DataTable rows={ciCheck.data.results as unknown as Record<string, unknown>[]} columns={[{ key: "event", label: "Event" }, { key: "version", label: "Version", align: "right" }, { key: "status", label: "결과", format: (v) => <Chip size="small" label={String(v)} color={v === "ok" ? "success" : v === "warning" ? "warning" : "error"} /> }, { key: "messages", label: "메시지", format: (v) => (v as string[]).join(", ") || "—" }]} />
        </>}
      </Stack>
    </Card>
    <Card sx={{ p: 2.5 }}>
      <Typography variant="h6">Semantic Metric Registry</Typography>
      <Typography variant="body2" color="text.secondary" mb={2}>허용된 AST만 SQL로 컴파일해 모든 Dashboard/API/MCP가 같은 지표 정의를 사용합니다.</Typography>
      <Stack direction={{ xs: "column", md: "row" }} spacing={1.5}><TextField label="Metric Name" value={metricName} onChange={(e) => setMetricName(e.target.value)} /><TextField fullWidth label="Definition JSON" value={metricDefinition} onChange={(e) => setMetricDefinition(e.target.value)} /><Button variant="contained" disabled={!metricName || metricSave.isPending} onClick={() => metricSave.mutate()}>저장</Button></Stack>
      {metricSave.error && <Box mt={2}><ErrorState error={metricSave.error} /></Box>}
      <Box mt={2}><DataTable rows={metrics.data || []} columns={[{ key: "name", label: "Metric" }, { key: "label", label: "Label" }, { key: "definition_version", label: "Version", align: "right" }, { key: "format", label: "Format" }, { key: "status", label: "Status" }, { key: "description", label: "Description" }, { key: "name", label: "", align: "right", format: (v) => <Button size="small" onClick={() => setMetricName2(String(v))}>값 조회</Button> }]} /></Box>
      {metricName2 && <Box mt={2}>
        <Divider sx={{ mb: 2 }} />
        <Typography fontWeight={700} mb={1}>{metricName2} · 최근 30일 · {environment.toUpperCase()}</Typography>
        {metricQuery.isLoading ? <Loading /> : metricQuery.error ? <ErrorState error={metricQuery.error} /> : metricQuery.data ? <Alert severity="info">{metricQuery.data.label}: {Number(metricQuery.data.value).toLocaleString("ko-KR")} {metricQuery.data.unit} (정의 v{metricQuery.data.definition_version}, {metricQuery.data.format})</Alert> : null}
      </Box>}
    </Card>
    <Card sx={{ p: 2.5 }}>
      <Typography variant="h6">Organization Adoption Target</Typography>
      <Typography variant="body2" color="text.secondary" mb={2}>조직·부서별 실제 대상자 수를 입력하면 Feature Adoption Rate의 분모로 사용합니다.</Typography>
      <Stack direction={{ xs: "column", lg: "row" }} spacing={1.5}>
        <TextField label="Organization" value={target.organization} onChange={(e) => setTarget({ ...target, organization: e.target.value })} />
        <TextField label="Department" value={target.department} onChange={(e) => setTarget({ ...target, department: e.target.value })} />
        <TextField label="Feature" value={target.feature} onChange={(e) => setTarget({ ...target, feature: e.target.value })} />
        <TextField label="Eligible Users" type="number" value={target.eligible_users} onChange={(e) => setTarget({ ...target, eligible_users: Number(e.target.value) })} />
        <Button variant="contained" disabled={!target.feature || targetSave.isPending} onClick={() => targetSave.mutate()}>저장</Button>
      </Stack>
      <Box mt={2}><DataTable rows={targets.data || []} columns={[{ key: "organization", label: "조직" }, { key: "department", label: "부서" }, { key: "feature", label: "기능" }, { key: "eligible_users", label: "대상자", align: "right" }, { key: "id", label: "", format: (v) => <Button color="error" size="small" onClick={() => void del(`/api/v1/sites/${site.site_id}/adoption-targets/${String(v)}`).then(() => qc.invalidateQueries({ queryKey: ["adoption-targets"] }))}>삭제</Button> }]} /></Box>
    </Card>
  </Stack>;
}

function EnvironmentEditor({ siteID, item }: { siteID: string; item: SiteEnvironment }) {
  const qc = useQueryClient();
  const [mode, setMode] = useState(item.contract_mode);
  const [limit, setLimit] = useState(item.cardinality_limit);
  const save = useMutation({ mutationFn: () => put(`/api/v1/sites/${siteID}/environments/${item.name}`, { label: item.label, contract_mode: mode, cardinality_limit: limit, active: item.active }), onSuccess: () => qc.invalidateQueries({ queryKey: ["platform-environments"] }) });
  return <Stack direction={{ xs: "column", md: "row" }} spacing={1.5} alignItems={{ md: "center" }}><Chip label={item.name.toUpperCase()} color={item.name === "prd" ? "primary" : "default"} /><Typography sx={{ minWidth: 130 }}>{item.label}</Typography><TextField select size="small" label="Contract" value={mode} onChange={(e) => setMode(e.target.value as SiteEnvironment["contract_mode"])} sx={{ minWidth: 130 }}>{["allow", "warn", "reject"].map((value) => <MenuItem key={value} value={value}>{value}</MenuItem>)}</TextField><TextField size="small" type="number" label="Cardinality / day" value={limit} onChange={(e) => setLimit(Number(e.target.value))} /><Button size="small" variant="outlined" disabled={save.isPending} onClick={() => save.mutate()}>저장</Button></Stack>;
}

type CIValidation = { valid: boolean; errors: number; warnings: number; results: Record<string, unknown>[] };
type SettingsResponse = Record<string, { value: Record<string, unknown> }>;
type Channel = { id: string; name: string; channel_type: string; endpoint_url: string; header_names: string[]; active: boolean };

function Automation() {
  const { site, environment } = useSite();
  const qc = useQueryClient();
  const settings = useQuery({ queryKey: ["settings"], queryFn: () => get<SettingsResponse>("/api/v1/settings") });
  const channels = useQuery({ queryKey: ["delivery-channels", site?.site_id], enabled: !!site, queryFn: () => get<Channel[]>(`/api/v1/sites/${site!.site_id}/delivery-channels`) });
  const schedules = useQuery({ queryKey: ["scheduled-reports", site?.site_id], enabled: !!site, queryFn: () => get<Record<string, unknown>[]>(`/api/v1/sites/${site!.site_id}/scheduled-reports`) });
  const [config, setConfig] = useState<Record<string, unknown> | null>(null);
  const current = config || settings.data?.automation?.value || { enabled: false, allowed_webhook_hosts: [], delivery_timeout_seconds: 10, max_entity_ids: 0 };
  const [channel, setChannel] = useState({ name: "", channel_type: "webhook", endpoint_url: "", headers: "{}" });
  const [schedule, setSchedule] = useState({ name: "", channel_id: "", report_kind: "overview", interval_minutes: 1440, definition: `{"environment":"${environment}","days":7}` });
  useEffect(() => setSchedule((value) => ({ ...value, definition: `{"environment":"${environment}","days":7}` })), [environment]);
  const configSave = useMutation({ mutationFn: () => put("/api/v1/settings/automation", current), onSuccess: () => { setConfig(null); void qc.invalidateQueries({ queryKey: ["settings"] }); } });
  const channelSave = useMutation({ mutationFn: () => post(`/api/v1/sites/${site!.site_id}/delivery-channels`, { ...channel, headers: JSON.parse(channel.headers), active: true }), onSuccess: () => { setChannel({ name: "", channel_type: "webhook", endpoint_url: "", headers: "{}" }); void qc.invalidateQueries({ queryKey: ["delivery-channels"] }); } });
  const scheduleSave = useMutation({ mutationFn: () => post(`/api/v1/sites/${site!.site_id}/scheduled-reports`, { ...schedule, definition: JSON.parse(schedule.definition), enabled: true }), onSuccess: () => { setSchedule({ ...schedule, name: "" }); void qc.invalidateQueries({ queryKey: ["scheduled-reports"] }); } });
  // Delivery history is the only place where a failing webhook explains itself.
  const runs = useQuery({ queryKey: ["delivery-runs", site?.site_id], enabled: !!site, refetchInterval: 30000, queryFn: () => get<Record<string, unknown>[]>(`/api/v1/sites/${site!.site_id}/delivery-runs`) });
  const channelDelete = useMutation({ mutationFn: (id: string) => del(`/api/v1/sites/${site!.site_id}/delivery-channels/${id}`), onSuccess: () => qc.invalidateQueries({ queryKey: ["delivery-channels"] }) });
  const scheduleDelete = useMutation({ mutationFn: (id: string) => del(`/api/v1/sites/${site!.site_id}/scheduled-reports/${id}`), onSuccess: () => qc.invalidateQueries({ queryKey: ["scheduled-reports"] }) });
  const runNow = useMutation({ mutationFn: (id: string) => post(`/api/v1/sites/${site!.site_id}/scheduled-reports/${id}/run`), onSuccess: () => { void qc.invalidateQueries({ queryKey: ["scheduled-reports"] }); void qc.invalidateQueries({ queryKey: ["delivery-runs"] }); } });
  if (!site) return <NoSite />;
  if (settings.isLoading || channels.isLoading || schedules.isLoading) return <Loading />;
  const error = settings.error || channels.error || schedules.error;
  if (error) return <ErrorState error={error} />;
  const allowedHosts = ((current.allowed_webhook_hosts as string[]) || []).join(", ");
  return <Stack spacing={3}>
    <Alert severity="info">모든 전송은 관리자 Allowlist와 Audit Log를 거칩니다. 기본값은 비활성이고, Entity ID 전달은 기본 0건입니다.</Alert>
    <Card sx={{ p: 2.5 }}><Typography variant="h6">Automation Security</Typography><Stack spacing={1.5} mt={2}><TextField select label="Scheduler" value={String(Boolean(current.enabled))} onChange={(e) => setConfig({ ...current, enabled: e.target.value === "true" })}><MenuItem value="false">Disabled</MenuItem><MenuItem value="true">Enabled</MenuItem></TextField><TextField label="Allowed Webhook Hosts" value={allowedHosts} onChange={(e) => setConfig({ ...current, allowed_webhook_hosts: e.target.value.split(",").map((x) => x.trim()).filter(Boolean) })} helperText="예: hooks.internal, *.corp.local" /><TextField type="number" label="Delivery Timeout (seconds)" value={Number(current.delivery_timeout_seconds || 10)} onChange={(e) => setConfig({ ...current, delivery_timeout_seconds: Number(e.target.value) })} /><TextField type="number" label="Segment Entity ID 최대 전달 수" value={Number(current.max_entity_ids || 0)} onChange={(e) => setConfig({ ...current, max_entity_ids: Number(e.target.value) })} helperText="0이면 집계만 전달합니다." /><Button variant="contained" disabled={!config || configSave.isPending} onClick={() => configSave.mutate()}>보안 설정 저장</Button></Stack></Card>
    <Card sx={{ p: 2.5 }}><Typography variant="h6">Delivery Channel</Typography><Typography variant="body2" color="text.secondary" mb={2}>Webhook, Confluence, Mail Gateway, 사내 메시지, AI Agent endpoint를 등록합니다. Header 값은 다시 표시되지 않습니다.</Typography><Stack spacing={1.5}><TextField label="이름" value={channel.name} onChange={(e) => setChannel({ ...channel, name: e.target.value })} /><TextField select label="유형" value={channel.channel_type} onChange={(e) => setChannel({ ...channel, channel_type: e.target.value })}>{["webhook", "confluence", "mail", "internal_message", "ai_agent"].map((value) => <MenuItem key={value} value={value}>{value}</MenuItem>)}</TextField><TextField label="Endpoint URL" value={channel.endpoint_url} onChange={(e) => setChannel({ ...channel, endpoint_url: e.target.value })} /><TextField label="Headers JSON" value={channel.headers} onChange={(e) => setChannel({ ...channel, headers: e.target.value })} /><Button variant="contained" disabled={!channel.name || !channel.endpoint_url || channelSave.isPending} onClick={() => channelSave.mutate()}>Channel 등록</Button>{channelSave.error && <ErrorState error={channelSave.error} />}</Stack><Box mt={2}><DataTable rows={(channels.data || []) as unknown as Record<string, unknown>[]} columns={[{ key: "name", label: "이름" }, { key: "channel_type", label: "유형" }, { key: "endpoint_url", label: "Endpoint" }, { key: "header_names", label: "Headers", format: (v) => (v as string[]).join(", ") || "—" }, { key: "active", label: "상태", format: (v) => <Chip size="small" label={v ? "Active" : "Disabled"} /> }, { key: "id", label: "", align: "right", format: (v) => <Button size="small" color="error" disabled={channelDelete.isPending} onClick={() => channelDelete.mutate(String(v))}>삭제</Button> }]} />{channelDelete.error && <Box mt={1}><ErrorState error={channelDelete.error} /></Box>}</Box></Card>
    <Card sx={{ p: 2.5 }}><Typography variant="h6">Scheduled Report / Segment Action</Typography><Typography variant="body2" color="text.secondary" mb={2}>분석에서 Action까지 연결합니다: Segment → Webhook → CRM/Mail/AI Agent/사내 메시지.</Typography><Stack spacing={1.5}><TextField label="이름" value={schedule.name} onChange={(e) => setSchedule({ ...schedule, name: e.target.value })} /><TextField select label="Channel" value={schedule.channel_id} onChange={(e) => setSchedule({ ...schedule, channel_id: e.target.value })}>{channels.data?.map((item) => <MenuItem key={item.id} value={item.id}>{item.name}</MenuItem>)}</TextField><TextField select label="Report" value={schedule.report_kind} onChange={(e) => setSchedule({ ...schedule, report_kind: e.target.value })}>{["overview", "insights", "adoption", "experience", "ai", "segment"].map((value) => <MenuItem key={value} value={value}>{value}</MenuItem>)}</TextField><TextField type="number" label="Interval Minutes" value={schedule.interval_minutes} onChange={(e) => setSchedule({ ...schedule, interval_minutes: Number(e.target.value) })} /><TextField multiline label="Definition JSON" value={schedule.definition} onChange={(e) => setSchedule({ ...schedule, definition: e.target.value })} /><Button variant="contained" disabled={!schedule.name || !schedule.channel_id || scheduleSave.isPending} onClick={() => scheduleSave.mutate()}>Schedule 저장</Button>{scheduleSave.error && <ErrorState error={scheduleSave.error} />}</Stack><Box mt={2}><DataTable rows={schedules.data || []} columns={[{ key: "name", label: "이름" }, { key: "report_kind", label: "Report" }, { key: "channel_name", label: "Channel" }, { key: "interval_minutes", label: "Interval", align: "right" }, { key: "last_status", label: "Last Status" }, { key: "next_run_at", label: "Next Run" }, { key: "last_error", label: "Last Error", format: (v) => v ? <Typography variant="caption" color="error.main">{String(v)}</Typography> : "—" }, { key: "id", label: "", align: "right", format: (v) => <Stack direction="row" justifyContent="flex-end" spacing={0.5}><Button size="small" disabled={runNow.isPending} onClick={() => runNow.mutate(String(v))}>지금 실행</Button><Button size="small" color="error" disabled={scheduleDelete.isPending} onClick={() => scheduleDelete.mutate(String(v))}>삭제</Button></Stack> }]} />{runNow.error && <Box mt={1}><ErrorState error={runNow.error} /></Box>}</Box></Card>
    <Card sx={{ p: 2.5 }}>
      <Stack direction="row" justifyContent="space-between" alignItems="center" mb={2}>
        <Box>
          <Typography variant="h6">Delivery 이력</Typography>
          <Typography variant="body2" color="text.secondary">최근 전송 결과와 응답 코드입니다. 실패는 Allowlist, 인증 Header, 대상 시스템 순으로 확인하십시오.</Typography>
        </Box>
        <Button size="small" onClick={() => void runs.refetch()} disabled={runs.isFetching}>새로 고침</Button>
      </Stack>
      {runs.isLoading ? <Loading /> : runs.error ? <ErrorState error={runs.error} /> : <DataTable rows={runs.data || []} exportFilename="momento-delivery-runs" columns={[{ key: "started_at", label: "시작", format: (v) => new Date(String(v)).toLocaleString("ko-KR") }, { key: "status", label: "상태", format: (v) => <Chip size="small" label={String(v)} color={v === "success" ? "success" : v === "running" ? "info" : "error"} /> }, { key: "response_status", label: "HTTP", align: "right", format: (v) => v ? String(v) : "—" }, { key: "error", label: "오류", format: (v) => v ? String(v) : "—" }, { key: "finished_at", label: "종료", format: (v) => v ? new Date(String(v)).toLocaleString("ko-KR") : "—" }]} />}
    </Card>
  </Stack>;
}
