import { useEffect, useState } from "react";
import {
  Alert,
  Box,
  Button,
  Card,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  MenuItem,
  Stack,
  Tab,
  Tabs,
  TextField,
  Typography,
} from "@mui/material";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { get, post, put } from "../api/client";
import { useAuth } from "../contexts/AuthContext";
import { useSite } from "../contexts/SiteContext";
import DataTable from "../components/DataTable";
import { ErrorState, Loading, NoSite } from "../components/States";

export default function EnterpriseAdminPage({
  mode,
}: {
  mode: "engineering" | "privacy" | "product";
}) {
  const { user } = useAuth();
  if (user?.role === "analyst" || user?.role === "viewer")
    return <Alert severity="warning">관리자 권한이 필요합니다.</Alert>;
  if (mode === "privacy") return <PrivacyRequests />;
  if (mode === "product") return <ProductLab />;
  return <AnalyticsEngineering />;
}

function AnalyticsEngineering() {
  const { site, environment } = useSite();
  const qc = useQueryClient();
  const [params, setParams] = useSearchParams();
  const panelNames = ["metrics", "query-cost", "aggregate", "calendar", "catalog"];
  const requestedPanel = panelNames.indexOf(params.get("panel") || "metrics");
  const panel = requestedPanel < 0 ? 0 : requestedPanel;
  const selectPanel = (value: number) => {
    if (value === 0) setParams({});
    else setParams({ panel: panelNames[value] });
  };
  const [confirmRebuild, setConfirmRebuild] = useState(false);
  const metrics = useQuery({
    queryKey: ["semantic-metrics", site?.site_id],
    enabled: !!site,
    queryFn: () =>
      get<Record<string, unknown>[]>(
        `/api/v1/sites/${site!.site_id}/semantic-metrics`,
      ),
  });
  const goals = useQuery({
    queryKey: ["metric-goals", site?.site_id],
    enabled: !!site,
    queryFn: () =>
      get<Record<string, unknown>[]>(
        `/api/v1/sites/${site!.site_id}/metric-goals`,
      ),
  });
  const policy = useQuery({
    queryKey: ["query-policy", site?.site_id],
    enabled: !!site,
    queryFn: () =>
      get<Record<string, number>>(
        `/api/v1/sites/${site!.site_id}/query-policy`,
      ),
  });
  const queryAudit = useQuery({
    queryKey: ["query-audit", site?.site_id],
    enabled: !!site,
    queryFn: () =>
      get<Record<string, unknown>[]>(
        `/api/v1/sites/${site!.site_id}/query-audit`,
      ),
  });
  const jobs = useQuery({
    queryKey: ["aggregate-jobs", site?.site_id],
    enabled: !!site,
    refetchInterval: 5000,
    queryFn: () =>
      get<Record<string, unknown>[]>(
        `/api/v1/sites/${site!.site_id}/aggregate-jobs`,
      ),
  });
  const catalog = useQuery({
    queryKey: ["catalog", site?.site_id, environment],
    enabled: !!site,
    queryFn: () =>
      get<Record<string, unknown>[]>(`/api/v1/sites/${site!.site_id}/catalog`),
  });
  const lineage = useQuery({
    queryKey: ["lineage", site?.site_id],
    enabled: !!site,
    queryFn: () =>
      get<{
        nodes: Record<string, unknown>[];
        edges: Record<string, unknown>[];
      }>(`/api/v1/sites/${site!.site_id}/lineage`),
  });
  const [formula, setFormula] = useState({
    name: "",
    label: "",
    numerator_type: "unique_users",
    numerator_event: "",
    denominator_type: "unique_users",
    denominator_event: "",
    owner: "",
    format: "percent",
    min_occurrences: 0,
  });
  const [goal, setGoal] = useState({
    name: "",
    metric_name: "",
    target_value: 0,
    comparator: "gte",
    period: "month",
    owner: "",
    environment,
  });
  const [policyForm, setPolicyForm] = useState({
    max_exact_days: 180,
    max_complexity_score: 90,
    background_threshold: 60,
    fast_sample_percent: 10,
    preview_sample_percent: 1,
  });
  const [job, setJob] = useState({
    job_type: "date_range",
    date_from: "",
    date_to: "",
    reason: "관리자 재집계",
    environment,
  });
  const [annotation, setAnnotation] = useState({
    kind: "deployment",
    title: "",
    description: "",
    occurred_at: new Date().toISOString().slice(0, 16),
    environment,
    workspace_wide: false,
  });
  useEffect(() => {
    if (policy.data) setPolicyForm(policy.data as typeof policyForm);
  }, [policy.data]);
  useEffect(() => {
    setGoal((v) => ({ ...v, environment }));
    setJob((v) => ({ ...v, environment }));
    setAnnotation((v) => ({ ...v, environment }));
  }, [environment]);
  const saveFormula = useMutation({
    mutationFn: () =>
      post(`/api/v1/sites/${site!.site_id}/semantic-metrics`, {
        name: formula.name,
        label: formula.label || formula.name,
        description: "Metric Builder에서 생성",
        format: formula.format,
        status: "active",
        owner: formula.owner,
        entity_scope: "user",
        tags: ["formula"],
        definition: {
          type: "ratio",
          numerator: {
            type: formula.numerator_type,
            ...(formula.numerator_event
              ? { event_name: formula.numerator_event }
              : {}),
            ...(formula.min_occurrences > 1
              ? { min_occurrences: formula.min_occurrences }
              : {}),
          },
          denominator: {
            type: formula.denominator_type,
            ...(formula.denominator_event
              ? { event_name: formula.denominator_event }
              : {}),
          },
        },
      }),
    onSuccess: () => {
      setFormula({ ...formula, name: "", label: "" });
      void qc.invalidateQueries({ queryKey: ["semantic-metrics"] });
    },
  });
  const saveGoal = useMutation({
    mutationFn: () => post(`/api/v1/sites/${site!.site_id}/metric-goals`, goal),
    onSuccess: () => {
      setGoal({ ...goal, name: "" });
      void qc.invalidateQueries({ queryKey: ["metric-goals"] });
    },
  });
  const savePolicy = useMutation({
    mutationFn: () =>
      put(`/api/v1/sites/${site!.site_id}/query-policy`, policyForm),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["query-policy"] }),
  });
  const createJob = useMutation({
    mutationFn: () =>
      post(`/api/v1/sites/${site!.site_id}/aggregate-jobs`, job),
    onSuccess: () => {
      setConfirmRebuild(false);
      return qc.invalidateQueries({ queryKey: ["aggregate-jobs"] });
    },
  });
  const saveAnnotation = useMutation({
    mutationFn: () =>
      post(`/api/v1/sites/${site!.site_id}/annotations`, {
        ...annotation,
        occurred_at: new Date(annotation.occurred_at).toISOString(),
        source: "manual",
        metadata: {},
      }),
    onSuccess: () => {
      setAnnotation({ ...annotation, title: "", description: "" });
      void qc.invalidateQueries({ queryKey: ["annotations"] });
    },
  });
  if (!site) return <NoSite />;
  if (metrics.isLoading || policy.isLoading) return <Loading />;
  const error =
    metrics.error ||
    goals.error ||
    policy.error ||
    queryAudit.error ||
    jobs.error ||
    catalog.error ||
    lineage.error;
  if (error) return <ErrorState error={error} />;
  return (
    <Stack spacing={3}>
      <Alert severity="info">
        Semantic Metric → Goal → Dashboard/API/MCP가 하나의 정의를 사용합니다.
        SQL은 노출하지 않고 허용된 Metric AST만 저장합니다.
      </Alert>
      <Card sx={{ px: 1, overflow: "hidden" }}>
        <Tabs
          value={panel}
          onChange={(_, value) => selectPanel(value)}
          variant="scrollable"
          scrollButtons="auto"
        >
          <Tab label="Metric · Goal" />
          <Tab label="Query Cost" />
          <Tab label="Aggregate" />
          <Tab label="Change Calendar" />
          <Tab label="Catalog · Lineage" />
        </Tabs>
      </Card>
      {panel === 0 && (
        <>
          <Card sx={{ p: 2.5 }}>
            <Typography variant="h6">Formula Metric Builder</Typography>
            <Typography variant="body2" color="text.secondary" mb={2}>
              예: AI 활용률 = ai_prompt 사용자 / 전체 활성 사용자 × 100
            </Typography>
            <Box
              sx={{
                display: "grid",
                gridTemplateColumns: { xs: "1fr", md: "repeat(3,1fr)" },
                gap: 1.5,
              }}
            >
              <TextField
                label="Metric Key"
                value={formula.name}
                onChange={(e) =>
                  setFormula({ ...formula, name: e.target.value })
                }
              />
              <TextField
                label="표시 이름"
                value={formula.label}
                onChange={(e) =>
                  setFormula({ ...formula, label: e.target.value })
                }
              />
              <TextField
                label="Owner"
                value={formula.owner}
                onChange={(e) =>
                  setFormula({ ...formula, owner: e.target.value })
                }
              />
              <TextField
                select
                label="Numerator"
                value={formula.numerator_type}
                onChange={(e) =>
                  setFormula({ ...formula, numerator_type: e.target.value })
                }
              >
                <MenuItem value="unique_users">Unique Users</MenuItem>
                <MenuItem value="unique_sessions">Unique Sessions</MenuItem>
                <MenuItem value="count">Events</MenuItem>
              </TextField>
              <TextField
                label="Numerator Event"
                placeholder="ai_prompt"
                value={formula.numerator_event}
                onChange={(e) =>
                  setFormula({ ...formula, numerator_event: e.target.value })
                }
              />
              <TextField
                type="number"
                label="최소 사용 횟수"
                value={formula.min_occurrences}
                onChange={(e) =>
                  setFormula({
                    ...formula,
                    min_occurrences: Number(e.target.value),
                  })
                }
              />
              <TextField
                select
                label="Denominator"
                value={formula.denominator_type}
                onChange={(e) =>
                  setFormula({ ...formula, denominator_type: e.target.value })
                }
              >
                <MenuItem value="unique_users">All Users</MenuItem>
                <MenuItem value="unique_sessions">All Sessions</MenuItem>
                <MenuItem value="count">All Events</MenuItem>
              </TextField>
              <TextField
                label="Denominator Event (선택)"
                value={formula.denominator_event}
                onChange={(e) =>
                  setFormula({ ...formula, denominator_event: e.target.value })
                }
              />
              <Button
                variant="contained"
                disabled={!formula.name || saveFormula.isPending}
                onClick={() => saveFormula.mutate()}
              >
                Formula 저장
              </Button>
            </Box>
            {saveFormula.error && (
              <Box mt={2}>
                <ErrorState error={saveFormula.error} />
              </Box>
            )}
            <Box mt={2}>
              <DataTable
                title="Semantic Metric Registry"
                description="모든 Dashboard, API, MCP가 공유하는 중앙 KPI 정의입니다."
                exportFilename="momento-semantic-metrics"
                rows={metrics.data || []}
                columns={[
                  { key: "name", label: "Metric" },
                  { key: "label", label: "Label" },
                  { key: "owner", label: "Owner" },
                  { key: "entity_scope", label: "Scope" },
                  {
                    key: "definition_version",
                    label: "Version",
                    align: "right",
                  },
                  { key: "format", label: "Format" },
                  {
                    key: "tags",
                    label: "Tags",
                    format: (v) => ((v as string[]) || []).join(", "),
                  },
                ]}
              />
            </Box>
          </Card>
          <Card sx={{ p: 2.5 }}>
            <Typography variant="h6">Goal Framework</Typography>
            <Stack direction={{ xs: "column", lg: "row" }} spacing={1.5} mt={2}>
              <TextField
                label="Goal 이름"
                value={goal.name}
                onChange={(e) => setGoal({ ...goal, name: e.target.value })}
              />
              <TextField
                select
                label="Metric"
                value={goal.metric_name}
                onChange={(e) =>
                  setGoal({ ...goal, metric_name: e.target.value })
                }
                sx={{ minWidth: 180 }}
              >
                {(metrics.data || []).map((m) => (
                  <MenuItem key={String(m.name)} value={String(m.name)}>
                    {String(m.label)}
                  </MenuItem>
                ))}
              </TextField>
              <TextField
                type="number"
                label="목표값"
                value={goal.target_value}
                onChange={(e) =>
                  setGoal({ ...goal, target_value: Number(e.target.value) })
                }
              />
              <TextField
                select
                label="기준"
                value={goal.comparator}
                onChange={(e) =>
                  setGoal({ ...goal, comparator: e.target.value })
                }
              >
                <MenuItem value="gte">이상</MenuItem>
                <MenuItem value="lte">이하</MenuItem>
              </TextField>
              <TextField
                select
                label="기간"
                value={goal.period}
                onChange={(e) => setGoal({ ...goal, period: e.target.value })}
              >
                {["day", "week", "month", "quarter"].map((x) => (
                  <MenuItem key={x} value={x}>
                    {x}
                  </MenuItem>
                ))}
              </TextField>
              <Button
                variant="contained"
                disabled={!goal.name || !goal.metric_name || saveGoal.isPending}
                onClick={() => saveGoal.mutate()}
              >
                목표 저장
              </Button>
            </Stack>
            <Box mt={2}>
              <DataTable
                title="Goal Registry"
                description="환경과 기간별 목표 기준을 관리합니다."
                exportFilename="momento-metric-goals"
                rows={goals.data || []}
                columns={[
                  { key: "name", label: "Goal" },
                  { key: "metric_label", label: "Metric" },
                  { key: "target_value", label: "Target", align: "right" },
                  { key: "comparator", label: "기준" },
                  { key: "period", label: "기간" },
                  { key: "environment", label: "Env" },
                  {
                    key: "active",
                    label: "상태",
                    format: (v) => (
                      <Chip size="small" label={v ? "Active" : "Disabled"} />
                    ),
                  },
                ]}
              />
            </Box>
          </Card>
        </>
      )}
      {panel === 1 && (
        <Card sx={{ p: 2.5 }}>
          <Typography variant="h6">Query Cost Guard & Sampling</Typography>
          <Stack direction={{ xs: "column", lg: "row" }} spacing={1.5} mt={2}>
            {Object.entries(policyForm).map(([key, value]) => (
              <TextField
                key={key}
                type="number"
                label={
                  {
                    max_exact_days: "Exact 최대 기간(일)",
                    max_complexity_score: "최대 복잡도 점수",
                    background_threshold: "백그라운드 기준",
                    fast_sample_percent: "Fast 표본(%)",
                    preview_sample_percent: "Preview 표본(%)",
                  }[key] || key
                }
                value={value}
                onChange={(e) =>
                  setPolicyForm({
                    ...policyForm,
                    [key]: Number(e.target.value),
                  })
                }
              />
            ))}
            <Button
              variant="contained"
              disabled={savePolicy.isPending}
              onClick={() => savePolicy.mutate()}
            >
              정책 저장
            </Button>
          </Stack>
          <Box mt={2}>
            <DataTable
              title="Query Audit"
              description="비용 정책 적용 결과와 실행 시간을 확인합니다."
              exportFilename="momento-query-audit"
              rows={queryAudit.data || []}
              columns={[
                { key: "created_at", label: "시각" },
                { key: "actor", label: "Actor" },
                { key: "mode", label: "Mode" },
                {
                  key: "complexity_score",
                  label: "Complexity",
                  align: "right",
                },
                { key: "sample_percent", label: "Sample %", align: "right" },
                { key: "duration_ms", label: "Duration(ms)", align: "right" },
                { key: "result_rows", label: "Rows", align: "right" },
                {
                  key: "status",
                  label: "Status",
                  format: (v) => (
                    <Chip
                      size="small"
                      color={
                        v === "success"
                          ? "success"
                          : v === "rejected" || v === "failed"
                            ? "error"
                            : "default"
                      }
                      label={String(v)}
                    />
                  ),
                },
              ]}
            />
          </Box>
        </Card>
      )}
      {panel === 2 && (
        <Card sx={{ p: 2.5 }}>
          <Typography variant="h6">
            Aggregate Manager / Late Event Rebuild
          </Typography>
          <Stack direction={{ xs: "column", lg: "row" }} spacing={1.5} mt={2}>
            <TextField
              select
              label="Job"
              value={job.job_type}
              onChange={(e) => setJob({ ...job, job_type: e.target.value })}
            >
              <MenuItem value="date_range">Date Range</MenuItem>
              <MenuItem value="full_rebuild">Full Rebuild</MenuItem>
            </TextField>
            <TextField
              type="date"
              label="From"
              InputLabelProps={{ shrink: true }}
              value={job.date_from}
              onChange={(e) => setJob({ ...job, date_from: e.target.value })}
              disabled={job.job_type === "full_rebuild"}
            />
            <TextField
              type="date"
              label="To"
              InputLabelProps={{ shrink: true }}
              value={job.date_to}
              onChange={(e) => setJob({ ...job, date_to: e.target.value })}
              disabled={job.job_type === "full_rebuild"}
            />
            <TextField
              label="Reason"
              value={job.reason}
              onChange={(e) => setJob({ ...job, reason: e.target.value })}
            />
            <Button
              variant="contained"
              onClick={() =>
                job.job_type === "full_rebuild"
                  ? setConfirmRebuild(true)
                  : createJob.mutate()
              }
              disabled={
                createJob.isPending ||
                (job.job_type === "date_range" &&
                  (!job.date_from || !job.date_to))
              }
            >
              재집계 요청
            </Button>
          </Stack>
          <Box mt={2}>
            <DataTable
              title="Aggregate Job"
              description="재집계·백필 작업 상태를 5초마다 갱신합니다."
              rows={jobs.data || []}
              columns={[
                { key: "job_type", label: "Job" },
                { key: "environment", label: "Env" },
                { key: "date_from", label: "From" },
                { key: "date_to", label: "To" },
                {
                  key: "status",
                  label: "Status",
                  format: (v) => (
                    <Chip
                      size="small"
                      label={String(v)}
                      color={
                        v === "success"
                          ? "success"
                          : v === "failed"
                            ? "error"
                            : "default"
                      }
                    />
                  ),
                },
                { key: "reason", label: "Reason" },
                { key: "error", label: "Error" },
              ]}
            />
          </Box>
        </Card>
      )}
      {panel === 3 && (
        <Card sx={{ p: 2.5 }}>
          <Typography variant="h6">Change Calendar Annotation</Typography>
          <Stack direction={{ xs: "column", lg: "row" }} spacing={1.5} mt={2}>
            <TextField
              select
              label="종류"
              value={annotation.kind}
              onChange={(e) =>
                setAnnotation({ ...annotation, kind: e.target.value })
              }
            >
              {[
                "deployment",
                "release",
                "incident",
                "campaign",
                "training",
                "feature_flag",
                "organization",
                "manual",
              ].map((x) => (
                <MenuItem key={x} value={x}>
                  {x}
                </MenuItem>
              ))}
            </TextField>
            <TextField
              type="datetime-local"
              label="시각"
              InputLabelProps={{ shrink: true }}
              value={annotation.occurred_at}
              onChange={(e) =>
                setAnnotation({ ...annotation, occurred_at: e.target.value })
              }
            />
            <TextField
              label="제목"
              value={annotation.title}
              onChange={(e) =>
                setAnnotation({ ...annotation, title: e.target.value })
              }
            />
            <TextField
              fullWidth
              label="설명"
              value={annotation.description}
              onChange={(e) =>
                setAnnotation({ ...annotation, description: e.target.value })
              }
            />
            <Button
              variant="contained"
              disabled={!annotation.title || saveAnnotation.isPending}
              onClick={() => saveAnnotation.mutate()}
            >
              기록
            </Button>
          </Stack>
        </Card>
      )}
      {panel === 4 && (
        <Card sx={{ p: 2.5 }}>
          <Typography variant="h6">Event Catalog</Typography>
          <DataTable
            title="Event Catalog"
            description="소유자, 버전, 최근 수집량과 사용 상태를 함께 봅니다."
            exportFilename="momento-event-catalog"
            rows={catalog.data || []}
            columns={[
              { key: "name", label: "Event" },
              { key: "owner", label: "Owner" },
              { key: "current_version", label: "Version", align: "right" },
              { key: "status", label: "Status" },
              { key: "volume_30d", label: "30d Volume", align: "right" },
              { key: "first_seen", label: "First Seen" },
              { key: "last_seen", label: "Last Seen" },
            ]}
          />
          <Typography variant="h6" mt={3}>
            Data Lineage
          </Typography>
          <DataTable
            title="Data Lineage"
            description="원천 Event가 Metric과 분석 소비자로 연결되는 경로입니다."
            rows={lineage.data?.edges || []}
            columns={[
              { key: "from", label: "Source" },
              { key: "relation", label: "Relation" },
              { key: "to", label: "Consumer" },
            ]}
          />
        </Card>
      )}
      <Dialog
        open={confirmRebuild}
        onClose={() => setConfirmRebuild(false)}
        maxWidth="xs"
        fullWidth
      >
        <DialogTitle>전체 Aggregate 재생성</DialogTitle>
        <DialogContent>
          <Alert severity="warning">
            전체 기간 재집계는 데이터 양에 따라 오래 걸리고 Query 부하를 높일 수
            있습니다. 현재 환경은 {environment.toUpperCase()}입니다.
          </Alert>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setConfirmRebuild(false)}>취소</Button>
          <Button
            variant="contained"
            color="warning"
            disabled={createJob.isPending}
            onClick={() => createJob.mutate()}
          >
            전체 재집계 실행
          </Button>
        </DialogActions>
      </Dialog>
    </Stack>
  );
}

function PrivacyRequests() {
  const { site, environment } = useSite();
  const qc = useQueryClient();
  const requests = useQuery({
    queryKey: ["privacy-requests", site?.site_id],
    enabled: !!site,
    queryFn: () =>
      get<Record<string, unknown>[]>(
        `/api/v1/sites/${site!.site_id}/privacy-requests`,
      ),
  });
  const [form, setForm] = useState({
    request_type: "delete",
    identity_type: "user_id",
    identity_value: "",
    date_from: "",
    date_to: "",
    reason: "개인정보 요청",
  });
  const [decision, setDecision] = useState<{
    id: string;
    requestType: string;
    action: "approve" | "reject";
  } | null>(null);
  const create = useMutation({
    mutationFn: () =>
      post(`/api/v1/sites/${site!.site_id}/privacy-requests`, form),
    onSuccess: () => {
      setForm({ ...form, identity_value: "" });
      void qc.invalidateQueries({ queryKey: ["privacy-requests"] });
    },
  });
  const decide = useMutation({
    mutationFn: ({ id, decision }: { id: string; decision: string }) =>
      post(`/api/v1/sites/${site!.site_id}/privacy-requests/${id}/decision`, {
        decision,
      }),
    onSuccess: () => {
      setDecision(null);
      return qc.invalidateQueries({ queryKey: ["privacy-requests"] });
    },
  });
  if (!site) return <NoSite />;
  if (requests.isLoading) return <Loading />;
  if (requests.error) return <ErrorState error={requests.error} />;
  return (
    <Stack spacing={2}>
      <Alert severity="warning">
        요청 생성과 실행을 분리하며, 승인·삭제 건수와 승인자는 Audit Log에
        남습니다. 현재 환경: {environment.toUpperCase()}
      </Alert>
      <Card sx={{ p: 2.5 }}>
        <Stack direction={{ xs: "column", lg: "row" }} spacing={1.5}>
          <TextField
            select
            label="요청"
            value={form.request_type}
            onChange={(e) => setForm({ ...form, request_type: e.target.value })}
          >
            {["delete", "export"].map((x) => (
              <MenuItem key={x} value={x}>
                {x}
              </MenuItem>
            ))}
          </TextField>
          <TextField
            select
            label="Identity"
            value={form.identity_type}
            onChange={(e) =>
              setForm({ ...form, identity_type: e.target.value })
            }
          >
            {["user_id", "visitor_id", "period"].map((x) => (
              <MenuItem key={x} value={x}>
                {x}
              </MenuItem>
            ))}
          </TextField>
          {form.identity_type === "period" ? (
            <>
              <TextField
                type="date"
                label="From"
                InputLabelProps={{ shrink: true }}
                value={form.date_from}
                onChange={(e) =>
                  setForm({ ...form, date_from: e.target.value })
                }
              />
              <TextField
                type="date"
                label="To"
                InputLabelProps={{ shrink: true }}
                value={form.date_to}
                onChange={(e) => setForm({ ...form, date_to: e.target.value })}
              />
            </>
          ) : (
            <TextField
              label="Identity Value"
              value={form.identity_value}
              onChange={(e) =>
                setForm({ ...form, identity_value: e.target.value })
              }
            />
          )}
          <TextField
            label="Reason"
            value={form.reason}
            onChange={(e) => setForm({ ...form, reason: e.target.value })}
          />
          <Button
            variant="contained"
            onClick={() => create.mutate()}
            disabled={
              create.isPending ||
              !form.reason.trim() ||
              (form.identity_type === "period"
                ? !form.date_from || !form.date_to
                : !form.identity_value.trim())
            }
          >
            요청 생성
          </Button>
        </Stack>
        {create.error && (
          <Box mt={2}>
            <ErrorState error={create.error} />
          </Box>
        )}
      </Card>
      <DataTable
        rows={requests.data || []}
        title="Privacy Request 이력"
        description="요청과 승인 상태, 실행 결과를 추적합니다."
        exportFilename="momento-privacy-requests"
        columns={[
          { key: "request_type", label: "요청" },
          { key: "identity_type", label: "Identity" },
          { key: "identity_value", label: "Value" },
          {
            key: "status",
            label: "Status",
            format: (v) => (
              <Chip
                size="small"
                label={String(v)}
                color={
                  v === "completed"
                    ? "success"
                    : v === "failed"
                      ? "error"
                      : "default"
                }
              />
            ),
          },
          { key: "reason", label: "Reason" },
          { key: "requested_at", label: "Requested" },
          {
            key: "id",
            label: "Decision",
            format: (v, row) =>
              row.status === "pending" ? (
                <Stack direction="row">
                  <Button
                    size="small"
                    onClick={() =>
                      setDecision({
                        id: String(v),
                        requestType: String(row.request_type),
                        action: "approve",
                      })
                    }
                  >
                    승인
                  </Button>
                  <Button
                    size="small"
                    color="error"
                    onClick={() =>
                      setDecision({
                        id: String(v),
                        requestType: String(row.request_type),
                        action: "reject",
                      })
                    }
                  >
                    거절
                  </Button>
                </Stack>
              ) : row.status === "completed" &&
                row.request_type === "export" ? (
                <Button
                  component="a"
                  href={`/api/v1/sites/${site.site_id}/privacy-requests/${String(v)}/export`}
                  size="small"
                >
                  NDJSON
                </Button>
              ) : (
                "—"
              ),
          },
        ]}
      />
      <Dialog
        open={!!decision}
        onClose={() => setDecision(null)}
        maxWidth="xs"
        fullWidth
      >
        <DialogTitle>
          {decision?.action === "approve"
            ? "Privacy Request 승인"
            : "Privacy Request 거절"}
        </DialogTitle>
        <DialogContent>
          {decide.error && (
            <Alert severity="error" sx={{ mb: 2 }}>
              요청 처리에 실패했습니다. 권한과 요청 상태를 확인한 뒤 다시
              시도하세요.
            </Alert>
          )}
          {decision?.action === "approve" &&
          decision.requestType === "delete" ? (
            <Alert severity="error">
              삭제 승인은 즉시 Raw Event와 관련 집계를 변경합니다. 대상
              Identity와 요청 사유를 다시 확인하세요.
            </Alert>
          ) : (
            <Alert
              severity={decision?.action === "approve" ? "warning" : "info"}
            >
              이 결정은 요청 이력과 Audit Log에 승인자 정보와 함께 기록됩니다.
            </Alert>
          )}
          <Stack mt={2} spacing={1}>
            <Typography variant="caption" color="text.secondary">
              Request ID
            </Typography>
            <Typography
              className="mono"
              variant="body2"
              sx={{ overflowWrap: "anywhere" }}
            >
              {decision?.id}
            </Typography>
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDecision(null)}>취소</Button>
          <Button
            variant="contained"
            color={
              decision?.action === "approve" &&
              decision.requestType === "delete"
                ? "error"
                : "primary"
            }
            disabled={!decision || decide.isPending}
            onClick={() =>
              decision &&
              decide.mutate({ id: decision.id, decision: decision.action })
            }
          >
            {decision?.action === "approve" ? "확인 후 승인" : "요청 거절"}
          </Button>
        </DialogActions>
      </Dialog>
    </Stack>
  );
}

function ProductLab() {
  const { site, environment } = useSite();
  const qc = useQueryClient();
  const [params, setParams] = useSearchParams();
  const panelNames = ["flags", "experiments"];
  const requestedPanel = panelNames.indexOf(params.get("panel") || "flags");
  const panel = requestedPanel < 0 ? 0 : requestedPanel;
  const selectPanel = (value: number) => {
    if (value === 0) setParams({});
    else setParams({ panel: panelNames[value] });
  };
  const flags = useQuery({
    queryKey: ["feature-flags", site?.site_id],
    enabled: !!site,
    queryFn: () =>
      get<Record<string, unknown>[]>(
        `/api/v1/sites/${site!.site_id}/feature-flags`,
      ),
  });
  const experiments = useQuery({
    queryKey: ["experiments", site?.site_id],
    enabled: !!site,
    queryFn: () =>
      get<Record<string, unknown>[]>(
        `/api/v1/sites/${site!.site_id}/experiments`,
      ),
  });
  const metrics = useQuery({
    queryKey: ["semantic-metrics", site?.site_id],
    enabled: !!site,
    queryFn: () =>
      get<Record<string, unknown>[]>(
        `/api/v1/sites/${site!.site_id}/semantic-metrics`,
      ),
  });
  const [flag, setFlag] = useState({
    flag_key: "",
    name: "",
    description: "",
    variants: "control,variant",
    status: "active",
    owner: "",
  });
  const [experiment, setExperiment] = useState({
    experiment_key: "",
    name: "",
    hypothesis: "",
    primary_metric: "",
    variants: "control,variant",
    status: "draft",
    owner: "",
    environment,
    feature_flag_id: "",
    audience: {},
  });
  useEffect(() => setExperiment((v) => ({ ...v, environment })), [environment]);
  const saveFlag = useMutation({
    mutationFn: () =>
      post(`/api/v1/sites/${site!.site_id}/feature-flags`, {
        ...flag,
        variants: flag.variants
          .split(",")
          .map((x) => x.trim())
          .filter(Boolean),
      }),
    onSuccess: () => {
      setFlag({ ...flag, flag_key: "", name: "" });
      void qc.invalidateQueries({ queryKey: ["feature-flags"] });
    },
  });
  const saveExperiment = useMutation({
    mutationFn: () =>
      post(`/api/v1/sites/${site!.site_id}/experiments`, {
        ...experiment,
        variants: experiment.variants
          .split(",")
          .map((x) => x.trim())
          .filter(Boolean),
        feature_flag_id: experiment.feature_flag_id || undefined,
      }),
    onSuccess: () => {
      setExperiment({ ...experiment, experiment_key: "", name: "" });
      void qc.invalidateQueries({ queryKey: ["experiments"] });
    },
  });
  if (!site) return <NoSite />;
  if (flags.isLoading || experiments.isLoading || metrics.isLoading)
    return <Loading />;
  const error = flags.error || experiments.error || metrics.error;
  if (error) return <ErrorState error={error} />;
  return (
    <Stack spacing={3}>
      <Alert severity="info">
        이벤트에 experiment_id와 variant를 전송하면 Variant별 Semantic Metric,
        Lift, Confidence를 계산합니다. 첫 Variant가 Control입니다.
      </Alert>
      <Card sx={{ px: 1, overflow: "hidden" }}>
        <Tabs
          value={panel}
          onChange={(_, value) => selectPanel(value)}
          variant="scrollable"
          scrollButtons="auto"
        >
          <Tab label="Feature Flags" />
          <Tab label="Experiments" />
        </Tabs>
      </Card>
      {panel === 0 && (
        <Card sx={{ p: 2.5 }}>
          <Typography variant="h6">Feature Flag Registry</Typography>
          <Stack direction={{ xs: "column", lg: "row" }} spacing={1.5} mt={2}>
            <TextField
              label="Flag Key"
              value={flag.flag_key}
              onChange={(e) => setFlag({ ...flag, flag_key: e.target.value })}
            />
            <TextField
              label="Name"
              value={flag.name}
              onChange={(e) => setFlag({ ...flag, name: e.target.value })}
            />
            <TextField
              label="Variants"
              value={flag.variants}
              onChange={(e) => setFlag({ ...flag, variants: e.target.value })}
            />
            <TextField
              label="Owner"
              value={flag.owner}
              onChange={(e) => setFlag({ ...flag, owner: e.target.value })}
            />
            <Button
              variant="contained"
              disabled={!flag.flag_key || !flag.name || saveFlag.isPending}
              onClick={() => saveFlag.mutate()}
            >
              Flag 저장
            </Button>
          </Stack>
          {saveFlag.error && (
            <Box mt={2}>
              <ErrorState error={saveFlag.error} />
            </Box>
          )}
          <Box mt={2}>
            <DataTable
              title="Feature Flag Registry"
              description="실험과 Release Impact 분석에 연결할 Flag·Variant입니다."
              exportFilename="momento-feature-flags"
              rows={flags.data || []}
              columns={[
                { key: "flag_key", label: "Flag" },
                { key: "name", label: "Name" },
                {
                  key: "variants",
                  label: "Variants",
                  format: (v) => (v as string[]).join(", "),
                },
                { key: "status", label: "Status" },
                { key: "owner", label: "Owner" },
              ]}
            />
          </Box>
        </Card>
      )}
      {panel === 1 && (
        <Card sx={{ p: 2.5 }}>
          <Typography variant="h6">Experiment</Typography>
          <Stack spacing={1.5} mt={2}>
            <Stack direction={{ xs: "column", lg: "row" }} spacing={1.5}>
              <TextField
                label="Experiment Key"
                value={experiment.experiment_key}
                onChange={(e) =>
                  setExperiment({
                    ...experiment,
                    experiment_key: e.target.value,
                  })
                }
              />
              <TextField
                label="Name"
                value={experiment.name}
                onChange={(e) =>
                  setExperiment({ ...experiment, name: e.target.value })
                }
              />
              <TextField
                select
                label="Feature Flag"
                value={experiment.feature_flag_id}
                onChange={(e) =>
                  setExperiment({
                    ...experiment,
                    feature_flag_id: e.target.value,
                  })
                }
              >
                <MenuItem value="">선택 안 함</MenuItem>
                {(flags.data || []).map((f) => (
                  <MenuItem key={String(f.id)} value={String(f.id)}>
                    {String(f.name)}
                  </MenuItem>
                ))}
              </TextField>
              <TextField
                select
                label="Primary Metric"
                value={experiment.primary_metric}
                onChange={(e) =>
                  setExperiment({
                    ...experiment,
                    primary_metric: e.target.value,
                  })
                }
              >
                {(metrics.data || []).map((m) => (
                  <MenuItem key={String(m.name)} value={String(m.name)}>
                    {String(m.label)}
                  </MenuItem>
                ))}
              </TextField>
            </Stack>
            <Stack direction={{ xs: "column", lg: "row" }} spacing={1.5}>
              <TextField
                fullWidth
                label="Hypothesis"
                value={experiment.hypothesis}
                onChange={(e) =>
                  setExperiment({ ...experiment, hypothesis: e.target.value })
                }
              />
              <TextField
                label="Variants"
                value={experiment.variants}
                onChange={(e) =>
                  setExperiment({ ...experiment, variants: e.target.value })
                }
              />
              <TextField
                select
                label="Status"
                value={experiment.status}
                onChange={(e) =>
                  setExperiment({ ...experiment, status: e.target.value })
                }
              >
                {["draft", "running", "completed", "archived"].map((x) => (
                  <MenuItem key={x} value={x}>
                    {x}
                  </MenuItem>
                ))}
              </TextField>
              <Button
                variant="contained"
                disabled={
                  !experiment.experiment_key ||
                  !experiment.name ||
                  !experiment.primary_metric ||
                  saveExperiment.isPending
                }
                onClick={() => saveExperiment.mutate()}
              >
                Experiment 저장
              </Button>
            </Stack>
          </Stack>
          {saveExperiment.error && (
            <Box mt={2}>
              <ErrorState error={saveExperiment.error} />
            </Box>
          )}
          <Box mt={2}>
            <DataTable
              title="Experiment Registry"
              description="환경별 가설, Primary Metric, Variant 구성을 관리합니다."
              exportFilename="momento-experiments"
              rows={experiments.data || []}
              columns={[
                { key: "experiment_key", label: "Experiment" },
                { key: "name", label: "Name" },
                { key: "primary_metric", label: "Metric" },
                {
                  key: "variants",
                  label: "Variants",
                  format: (v) => (v as string[]).join(", "),
                },
                { key: "status", label: "Status" },
                { key: "environment", label: "Env" },
              ]}
            />
          </Box>
        </Card>
      )}
    </Stack>
  );
}
