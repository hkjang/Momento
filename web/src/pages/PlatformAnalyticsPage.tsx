import { useMemo, useState } from "react";
import {
  Alert,
  Box,
  Button,
  Card,
  Chip,
  LinearProgress,
  MenuItem,
  Stack,
  TextField,
  Typography,
} from "@mui/material";
import AutoAwesomeRounded from "@mui/icons-material/AutoAwesomeRounded";
import ReactECharts from "../components/Chart";
import { useMutation, useQuery } from "@tanstack/react-query";
import { get, post, rangeQuery } from "../api/client";
import { keepWithinScope } from "../api/keepPrevious";
import { aiSetupHint } from "./signalGuide";
import { useSite } from "../contexts/SiteContext";
import DataTable from "../components/DataTable";
import MetricCard from "../components/MetricCard";
import { ErrorState, Loading, NoSite } from "../components/States";
import RangeSelect from "../components/RangeSelect";
import { narrowerRange } from "../components/queryError";

export type PlatformMode =
  | "cohort"
  | "journey"
  | "adoption"
  | "experience"
  | "insights"
  | "ai"
  | "quality";

export default function PlatformAnalyticsPage({
  mode,
}: {
  mode: PlatformMode;
}) {
  if (mode === "cohort") return <Cohort />;
  if (mode === "journey") return <Journey />;
  if (mode === "adoption") return <Adoption />;
  if (mode === "experience") return <Experience />;
  if (mode === "insights") return <Insights />;
  if (mode === "ai") return <AIAnalytics />;
  return <Quality />;
}

type RetentionPeriod = {
  period: number;
  users: number;
  retention_rate: number;
  cohort_users?: number;
};
type RetentionCurve = {
  key: string;
  label: string;
  cohort_users: number;
  periods: RetentionPeriod[];
};
type RetentionComparison = {
  key: string;
  label: string;
  cohort_users: number;
  first_return_rate: number;
  baseline_first_return_rate: number;
  first_return_gap_points: number;
  worst_period: number;
  worst_period_gap_points: number;
  verdict: "better" | "worse" | "similar" | "insufficient";
  evidence: string;
  reliable: boolean;
};

const retentionVerdictLabel: Record<RetentionComparison["verdict"], string> = {
  better: "더 높음",
  worse: "더 낮음",
  similar: "비슷함",
  insufficient: "표본 부족",
};

const retentionVerdictColor: Record<
  RetentionComparison["verdict"],
  "success" | "error" | "default" | "warning"
> = {
  better: "success",
  worse: "error",
  similar: "default",
  insufficient: "warning",
};

const curveColors = ["#5B5CE2", "#12A875", "#E2A03F", "#C43E44"];

function Cohort() {
  const { site, environment } = useSite();
  // Retention is measured over months, so this screen offers longer periods than
  // the others rather than the shared 7/30/90.
  const [days, setDays] = useState(180);
  const [cohortEvent, setCohortEvent] = useState("");
  const [returnEvent, setReturnEvent] = useState("");
  const [periods, setPeriods] = useState(12);
  const [compareIds, setCompareIds] = useState<string[]>([]);
  const segments = useQuery({
    queryKey: ["segments", site?.site_id],
    placeholderData: keepWithinScope(site?.site_id, environment),
    enabled: !!site,
    queryFn: () =>
      get<{ id: string; name: string }[]>(
        `/api/v1/segments?site_id=${site!.site_id}`,
      ),
  });
  const q = useQuery({
    queryKey: [
      "cohort",
      site?.site_id,
      environment,
      days,
      cohortEvent,
      returnEvent,
      periods,
      compareIds.join(","),
    ],
    placeholderData: keepWithinScope(site?.site_id, environment),
    enabled: !!site,
    queryFn: () =>
      get<{
        cohorts: { cohort: string; size: number; periods: RetentionPeriod[] }[];
        curves?: RetentionCurve[];
        comparison?: RetentionComparison[];
      }>(
        `/api/v1/sites/${site!.site_id}/cohort?${rangeQuery(days, site!.timezone)}&granularity=week&periods=${periods}&cohort_event=${encodeURIComponent(cohortEvent)}&return_event=${encodeURIComponent(returnEvent)}${compareIds.length ? `&segment_ids=${compareIds.map(encodeURIComponent).join(",")}` : ""}`,
      ),
  });
  if (!site) return <NoSite />;
  if (q.isLoading) return <Loading />;
  if (q.error) return <ErrorState error={q.error} />;
  return (
    <Stack spacing={2}>
      <Card sx={{ p: 2.5 }}>
        <Stack direction={{ xs: "column", md: "row" }} spacing={2}>
          <TextField
            label="Cohort Event"
            placeholder="비우면 최초 Event"
            value={cohortEvent}
            onChange={(e) => setCohortEvent(e.target.value)}
          />
          <TextField
            label="Return Event"
            placeholder="비우면 모든 활동"
            value={returnEvent}
            onChange={(e) => setReturnEvent(e.target.value)}
          />
          <TextField
            select
            label="표시 주차"
            value={periods}
            onChange={(e) => setPeriods(Number(e.target.value))}
          >
            {[8, 12, 16, 24].map((value) => (
              <MenuItem key={value} value={value}>
                {value}주
              </MenuItem>
            ))}
          </TextField>
          <RangeSelect
            days={days}
            setDays={setDays}
            maxExactDays={site.max_exact_days}
            options={[90, 180, 365]}
          />
          <TextField
            select
            label="비교 Segment"
            value={compareIds}
            onChange={(e) =>
              setCompareIds(
                (typeof e.target.value === "string"
                  ? e.target.value.split(",")
                  : (e.target.value as unknown as string[])
                ).slice(0, 3),
              )
            }
            slotProps={{ select: { multiple: true } }}
            helperText="최대 3개. 전체와 Retention 곡선을 비교합니다."
            sx={{ minWidth: 220 }}
          >
            {(segments.data || []).map((segment) => (
              <MenuItem key={segment.id} value={segment.id}>
                {segment.name}
              </MenuItem>
            ))}
          </TextField>
        </Stack>
      </Card>
      {q.data?.curves && (
        <Card sx={{ p: 2.5 }}>
          <Typography variant="h6">Retention 곡선 비교</Typography>
          <Typography variant="body2" color="text.secondary" mb={2}>
            Cohort 크기로 가중한 평균 곡선입니다. 아직 해당 주차에 도달하지 못한
            Cohort는 분모에서 제외하므로 최근 Cohort가 곡선을 끌어내리지
            않습니다.
          </Typography>
          <ReactECharts
            style={{ height: 300 }}
            option={{
              tooltip: {
                trigger: "axis",
                valueFormatter: (value: number) =>
                  `${Number(value).toFixed(1)}%`,
              },
              legend: {
                data: q.data.curves.map((curve) => curve.label),
                bottom: 0,
              },
              grid: { left: 20, right: 20, bottom: 40, containLabel: true },
              xAxis: {
                type: "category",
                data: Array.from({ length: periods }, (_, i) => `W${i}`),
              },
              yAxis: { type: "value", axisLabel: { formatter: "{value}%" } },
              series: q.data.curves.map((curve, index) => ({
                name: curve.label,
                type: "line",
                smooth: true,
                symbolSize: 6,
                lineStyle: {
                  width: index === 0 ? 3 : 2,
                  type: index === 0 ? "solid" : "dashed",
                },
                itemStyle: { color: curveColors[index % curveColors.length] },
                data: curve.periods.map((period) =>
                  Number(period.retention_rate.toFixed(1)),
                ),
              })),
            }}
          />
          {!!q.data.comparison?.length && (
            <Stack spacing={1.2} mt={1}>
              {q.data.comparison.map((item) => (
                <Card key={item.key} variant="outlined" sx={{ p: 1.8 }}>
                  <Stack
                    direction="row"
                    gap={1}
                    alignItems="center"
                    flexWrap="wrap"
                  >
                    <Chip
                      size="small"
                      color={retentionVerdictColor[item.verdict]}
                      label={retentionVerdictLabel[item.verdict]}
                    />
                    <Typography fontWeight={700}>{item.label}</Typography>
                    <Chip
                      size="small"
                      variant="outlined"
                      label={`1주차 ${item.first_return_gap_points >= 0 ? "+" : ""}${item.first_return_gap_points.toFixed(1)}pp`}
                    />
                    {item.worst_period > 0 && (
                      <Chip
                        size="small"
                        variant="outlined"
                        color="warning"
                        label={`${item.worst_period}주차 격차`}
                      />
                    )}
                  </Stack>
                  <Typography variant="body2" color="text.secondary" mt={0.6}>
                    {item.evidence}
                  </Typography>
                </Card>
              ))}
            </Stack>
          )}
        </Card>
      )}
      <Card sx={{ overflowX: "auto" }}>
        <Box
          component="table"
          sx={{ width: "100%", borderCollapse: "collapse", minWidth: 900 }}
        >
          <Box component="thead">
            <Box component="tr">
              <Box component="th" sx={cellSx}>
                Cohort
              </Box>
              <Box component="th" sx={cellSx}>
                Users
              </Box>
              {Array.from({ length: periods }, (_, i) => (
                <Box component="th" key={i} sx={cellSx}>
                  W{i}
                </Box>
              ))}
            </Box>
          </Box>
          <Box component="tbody">
            {(q.data?.cohorts || []).map((row) => (
              <Box component="tr" key={row.cohort}>
                <Box component="td" sx={cellSx}>
                  {row.cohort}
                </Box>
                <Box component="td" sx={cellSx}>
                  {row.size.toLocaleString()}
                </Box>
                {row.periods.map((period) => (
                  <Box
                    component="td"
                    key={period.period}
                    sx={{
                      ...cellSx,
                      bgcolor: `rgba(86,88,220,${Math.max(0.04, period.retention_rate / 110)})`,
                      color: period.retention_rate > 55 ? "white" : "inherit",
                      fontWeight: 700,
                    }}
                  >
                    {period.retention_rate.toFixed(1)}%
                  </Box>
                ))}
              </Box>
            ))}
          </Box>
        </Box>
      </Card>
    </Stack>
  );
}

const cellSx = {
  p: 1.4,
  borderBottom: "1px solid #E8ECF3",
  textAlign: "center",
  fontSize: 13,
  whiteSpace: "nowrap",
};

function Journey() {
  const { site, environment } = useSite();
  const [events, setEvents] = useState("login\nsearch\npurchase");
  const steps = useMemo(
    () =>
      events
        .split("\n")
        .map((event) => event.trim())
        .filter(Boolean)
        .map((event) => ({ name: event, event })),
    [events],
  );
  const analyze = useMutation({
    mutationFn: () =>
      post<{ steps: Record<string, unknown>[] }>(
        `/api/v1/sites/${site!.site_id}/journeys/analyze?${rangeQuery(30, site!.timezone)}`,
        { steps, conversion_window_days: 30 },
      ),
  });
  const save = useMutation({
    mutationFn: () =>
      post(`/api/v1/sites/${site!.site_id}/journeys`, {
        name: `Journey ${new Date().toLocaleDateString("ko-KR")}`,
        steps,
        conversion_window_days: 30,
        shared: true,
      }),
  });
  if (!site) return <NoSite />;
  return (
    <Stack spacing={2}>
      <Card sx={{ p: 2.5 }}>
        <Typography fontWeight={720}>Business Journey</Typography>
        <Typography variant="body2" color="text.secondary" mb={2}>
          업무 결과까지 이어지는 Event를 줄마다 순서대로 입력합니다. 동일
          Canonical User가 30일 이내 순차 도달해야 합니다.
        </Typography>
        <TextField
          fullWidth
          multiline
          minRows={5}
          label="Journey Steps"
          value={events}
          onChange={(e) => setEvents(e.target.value)}
          helperText={`${steps.length} steps · ${environment.toUpperCase()}`}
        />
        <Stack direction="row" spacing={1} mt={2}>
          <Button
            variant="contained"
            disabled={steps.length < 2 || analyze.isPending}
            onClick={() => analyze.mutate()}
          >
            분석 실행
          </Button>
          <Button
            variant="outlined"
            disabled={steps.length < 2 || save.isPending}
            onClick={() => save.mutate()}
          >
            공유 Journey 저장
          </Button>
        </Stack>
        {save.isSuccess && (
          <Alert severity="success" sx={{ mt: 2 }}>
            Journey가 저장되었습니다.
          </Alert>
        )}
      </Card>
      {analyze.error && <ErrorState error={analyze.error} />}
      {analyze.data && (
        <DataTable
          rows={analyze.data.steps}
          columns={[
            { key: "step", label: "단계" },
            { key: "name", label: "업무 단계" },
            { key: "users", label: "사용자", align: "right" },
            {
              key: "conversion_rate",
              label: "최초 대비",
              align: "right",
              format: (v) => `${Number(v).toFixed(1)}%`,
            },
            {
              key: "average_elapsed_seconds",
              label: "평균 소요",
              align: "right",
              format: (v) => `${(Number(v) / 60).toFixed(1)}분`,
            },
          ]}
        />
      )}
    </Stack>
  );
}

function Adoption() {
  const { site, environment } = useSite();
  const [days, setDays] = useState(30);
  const q = useQuery({
    queryKey: ["adoption", site?.site_id, environment, days],
    placeholderData: keepWithinScope(site?.site_id, environment),
    enabled: !!site,
    queryFn: () =>
      get<{ rows: Record<string, unknown>[] }>(
        `/api/v1/sites/${site!.site_id}/adoption?${rangeQuery(days, site!.timezone)}`,
      ),
  });
  const narrower = narrowerRange(days);
  if (!site) return <NoSite />;
  return (
    <Stack spacing={2}>
      <RangeSelect
        days={days}
        setDays={setDays}
        maxExactDays={site.max_exact_days}
        timezone={site.timezone}
      />
      {q.isLoading ? (
        <Loading />
      ) : q.error ? (
        <ErrorState
          error={q.error}
          retry={() => q.refetch()}
          narrowRange={narrower === null ? undefined : () => setDays(narrower)}
        />
      ) : (
        <DataTable
          rows={q.data?.rows || []}
          columns={[
            { key: "organization", label: "조직" },
            { key: "department", label: "부서" },
            { key: "feature", label: "기능" },
            { key: "eligible_users", label: "대상자", align: "right" },
            { key: "users", label: "사용자", align: "right" },
            {
              key: "adoption_rate",
              label: "Adoption",
              align: "right",
              format: (v) => (
                <Stack minWidth={110}>
                  <Typography variant="body2">
                    {Number(v).toFixed(1)}%
                  </Typography>
                  <LinearProgress
                    variant="determinate"
                    value={Math.min(100, Number(v))}
                  />
                </Stack>
              ),
            },
            {
              key: "repeat_usage_rate",
              label: "재사용률",
              align: "right",
              format: (v) => `${Number(v).toFixed(1)}%`,
            },
            { key: "dormant_users", label: "비활성", align: "right" },
          ]}
        />
      )}
    </Stack>
  );
}

type ExperienceCohort = {
  key: string;
  label: string;
  users: number;
  error_users: number;
  error_user_rate: number;
  vitals: {
    metric: string;
    samples: number;
    p75: number;
    good_rate: number;
    good_threshold: number;
  }[];
};

type ExperienceGap = {
  key: string;
  label: string;
  kind: "vital" | "error";
  metric?: string;
  severity: "critical" | "warning";
  impact: number;
  evidence: string;
  action: string;
};

function Experience() {
  const { site, environment } = useSite();
  const [compareIds, setCompareIds] = useState<string[]>([]);
  const segments = useQuery({
    queryKey: ["segments", site?.site_id],
    placeholderData: keepWithinScope(site?.site_id, environment),
    enabled: !!site,
    queryFn: () =>
      get<{ id: string; name: string }[]>(
        `/api/v1/segments?site_id=${site!.site_id}`,
      ),
  });
  const [days, setDays] = useState(30);
  const q = useQuery({
    queryKey: [
      "experience",
      site?.site_id,
      environment,
      days,
      compareIds.join(","),
    ],
    placeholderData: keepWithinScope(site?.site_id, environment),
    enabled: !!site,
    queryFn: () =>
      get<{
        vitals: Record<string, unknown>[];
        errors: Record<string, unknown>[];
        releases: Record<string, unknown>[];
        impact: Record<string, number>;
        cohorts?: ExperienceCohort[];
        gaps?: ExperienceGap[];
      }>(
        `/api/v1/sites/${site!.site_id}/experience?${rangeQuery(days, site!.timezone)}${compareIds.length ? `&segment_ids=${compareIds.map(encodeURIComponent).join(",")}` : ""}`,
      ),
  });
  const narrower = narrowerRange(days);
  if (!site) return <NoSite />;
  if (q.isLoading) return <Loading />;
  if (q.error)
    return (
      <Stack spacing={2}>
        <RangeSelect
          days={days}
          setDays={setDays}
          maxExactDays={site.max_exact_days}
          timezone={site.timezone}
        />
        <ErrorState
          error={q.error}
          retry={() => q.refetch()}
          narrowRange={narrower === null ? undefined : () => setDays(narrower)}
        />
      </Stack>
    );
  return (
    <Stack spacing={2}>
      <RangeSelect
        days={days}
        setDays={setDays}
        maxExactDays={site.max_exact_days}
        timezone={site.timezone}
        note="Core Web Vitals와 오류는 이 기간의 이벤트 기준"
      />
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: { xs: "1fr", md: "repeat(3,1fr)" },
          gap: 2,
        }}
      >
        <MetricCard
          label="오류 영향 사용자"
          value={q.data?.impact.error_users || 0}
        />
        <MetricCard
          label="오류 사용자 전환율"
          value={q.data?.impact.error_user_conversion_rate || 0}
          type="percent"
        />
        <MetricCard
          label="정상 사용자 전환율"
          value={q.data?.impact.clean_user_conversion_rate || 0}
          type="percent"
        />
      </Box>
      <Card sx={{ p: 2.5 }}>
        <Stack
          direction={{ xs: "column", md: "row" }}
          justifyContent="space-between"
          alignItems={{ md: "center" }}
          gap={1.5}
        >
          <Box>
            <Typography variant="h6">집단별 경험 비교</Typography>
            <Typography variant="body2" color="text.secondary">
              사이트 전체 p75는 빠른 환경과 느린 환경을 평균해 둘 다 가립니다.
              Segment를 선택하면 같은 측정을 집단별로 나눠 봅니다.
            </Typography>
          </Box>
          <TextField
            select
            size="small"
            label="비교 Segment"
            value={compareIds}
            onChange={(e) =>
              setCompareIds(
                (typeof e.target.value === "string"
                  ? e.target.value.split(",")
                  : (e.target.value as unknown as string[])
                ).slice(0, 3),
              )
            }
            slotProps={{ select: { multiple: true } }}
            sx={{ minWidth: 220 }}
            helperText="최대 3개"
          >
            {(segments.data || []).map((segment) => (
              <MenuItem key={segment.id} value={segment.id}>
                {segment.name}
              </MenuItem>
            ))}
          </TextField>
        </Stack>
        {!!q.data?.gaps?.length && (
          <Stack spacing={1.2} mt={2}>
            {q.data.gaps.map((gap, index) => (
              <Card
                key={`${gap.key}-${gap.kind}-${gap.metric || index}`}
                variant="outlined"
                sx={{ p: 1.8 }}
              >
                <Stack
                  direction="row"
                  gap={1}
                  alignItems="center"
                  flexWrap="wrap"
                >
                  <Chip
                    size="small"
                    color={gap.severity === "critical" ? "error" : "warning"}
                    label={gap.severity === "critical" ? "심각" : "주의"}
                  />
                  <Typography fontWeight={700}>{gap.label}</Typography>
                  <Chip
                    size="small"
                    variant="outlined"
                    label={
                      gap.kind === "vital" ? `${gap.metric} 지연` : "오류 노출"
                    }
                  />
                </Stack>
                <Typography variant="body2" color="text.secondary" mt={0.6}>
                  {gap.evidence}
                </Typography>
                <Typography variant="body2" color="primary.main" mt={0.3}>
                  다음 행동 · {gap.action}
                </Typography>
              </Card>
            ))}
          </Stack>
        )}
        {!!q.data?.cohorts?.length && (
          <Box mt={2}>
            <DataTable
              rows={q.data.cohorts.flatMap((cohort) =>
                cohort.vitals.map((vital) => ({
                  label: cohort.label,
                  users: cohort.users,
                  error_user_rate: cohort.error_user_rate,
                  metric: vital.metric,
                  samples: vital.samples,
                  p75: vital.p75,
                  good_threshold: vital.good_threshold,
                })),
              )}
              exportFilename="momento-experience-cohorts"
              columns={[
                { key: "label", label: "집단" },
                { key: "metric", label: "Metric" },
                { key: "samples", label: "표본", align: "right" },
                {
                  key: "p75",
                  label: "P75",
                  align: "right",
                  format: (v, row) => (
                    <Typography
                      variant="body2"
                      color={
                        Number(row.good_threshold) > 0 &&
                        Number(v) > Number(row.good_threshold)
                          ? "error.main"
                          : "inherit"
                      }
                    >
                      {Number(v).toFixed(Number(v) < 10 ? 2 : 0)}
                    </Typography>
                  ),
                },
                {
                  key: "good_threshold",
                  label: "권장 기준",
                  align: "right",
                  format: (v) => (Number(v) > 0 ? Number(v).toString() : "—"),
                },
                { key: "users", label: "사용자", align: "right" },
                {
                  key: "error_user_rate",
                  label: "오류 경험",
                  align: "right",
                  format: (v) => `${Number(v).toFixed(1)}%`,
                },
              ]}
            />
          </Box>
        )}
        {!!compareIds.length && !q.data?.gaps?.length && !q.isFetching && (
          <Alert severity="success" sx={{ mt: 2 }}>
            선택한 집단에서 전체보다 뚜렷하게 나쁜 경험 지표를 찾지 못했습니다.
          </Alert>
        )}
      </Card>
      <Typography variant="h6">Core Web Vitals / RUM</Typography>
      <DataTable
        rows={q.data?.vitals || []}
        columns={[
          { key: "metric", label: "Metric" },
          { key: "page", label: "Page" },
          { key: "samples", label: "Samples", align: "right" },
          { key: "p75", label: "P75", align: "right" },
          {
            key: "good_rate",
            label: "Good",
            align: "right",
            format: (v) => `${Number(v).toFixed(1)}%`,
          },
        ]}
      />
      <Typography variant="h6">오류와 사용자 영향</Typography>
      <DataTable
        rows={q.data?.errors || []}
        columns={[
          { key: "event", label: "유형" },
          { key: "message", label: "오류 / Resource" },
          { key: "page", label: "Page" },
          { key: "count", label: "건수", align: "right" },
          { key: "affected_users", label: "영향 사용자", align: "right" },
        ]}
      />
      <Typography variant="h6">Release Impact</Typography>
      <DataTable
        rows={q.data?.releases || []}
        columns={[
          { key: "release", label: "Release" },
          { key: "events", label: "Events", align: "right" },
          { key: "users", label: "Users", align: "right" },
          { key: "errors", label: "Errors", align: "right" },
          {
            key: "user_conversion_rate",
            label: "전환율",
            align: "right",
            format: (v) => `${Number(v).toFixed(1)}%`,
          },
          { key: "last_seen", label: "Last Seen" },
        ]}
      />
    </Stack>
  );
}

function Insights() {
  const { site, environment } = useSite();
  const [question, setQuestion] = useState("지난주 사용 현황을 요약해줘");
  const q = useQuery({
    queryKey: ["insights", site?.site_id, environment],
    placeholderData: keepWithinScope(site?.site_id, environment),
    enabled: !!site,
    queryFn: () =>
      get<{ insights: Record<string, unknown>[]; engine: string }>(
        `/api/v1/sites/${site!.site_id}/insights?${rangeQuery(7, site!.timezone)}`,
      ),
  });
  const ask = useMutation({
    mutationFn: () =>
      post<{
        answer: string;
        engine: string;
        confidence: number;
        data: unknown;
      }>(`/api/v1/sites/${site!.site_id}/natural-query`, {
        question,
        environment,
      }),
  });
  if (!site) return <NoSite />;
  return (
    <Stack spacing={2}>
      <Card
        sx={{ p: 2.5, background: "linear-gradient(135deg,#F0F0FF,#F7FAFF)" }}
      >
        <Stack direction="row" spacing={1} alignItems="center">
          <AutoAwesomeRounded color="primary" />
          <Typography fontWeight={750}>
            Offline Natural Language Analytics
          </Typography>
          <Chip size="small" label="외부 전송 없음" color="success" />
        </Stack>
        <Stack direction={{ xs: "column", md: "row" }} spacing={1.5} mt={2}>
          <TextField
            fullWidth
            value={question}
            onChange={(e) => setQuestion(e.target.value)}
            placeholder="어떤 부서가 AI 검색을 가장 많이 사용했어?"
          />
          <Button
            variant="contained"
            disabled={!question.trim() || ask.isPending}
            onClick={() => ask.mutate()}
          >
            질문
          </Button>
        </Stack>
        {ask.data && (
          <Alert severity="info" sx={{ mt: 2 }}>
            <Typography fontWeight={700}>{ask.data.answer}</Typography>
            <Typography variant="caption">
              {ask.data.engine} · confidence{" "}
              {(ask.data.confidence * 100).toFixed(0)}%
            </Typography>
          </Alert>
        )}
        {ask.error && (
          <Box mt={2}>
            <ErrorState error={ask.error} />
          </Box>
        )}
      </Card>
      {q.isLoading ? (
        <Loading />
      ) : q.error ? (
        <ErrorState error={q.error} />
      ) : (
        <DataTable
          rows={q.data?.insights || []}
          columns={[
            {
              key: "severity",
              label: "심각도",
              format: (v) => (
                <Chip
                  size="small"
                  color={
                    v === "critical"
                      ? "error"
                      : v === "warning"
                        ? "warning"
                        : "default"
                  }
                  label={String(v)}
                />
              ),
            },
            { key: "title", label: "Insight" },
            {
              key: "change_percent",
              label: "변화",
              align: "right",
              format: (v) =>
                `${Number(v) >= 0 ? "+" : ""}${Number(v).toFixed(1)}%`,
            },
            { key: "evidence", label: "근거" },
            { key: "recommendation", label: "권장 조치" },
          ]}
        />
      )}
    </Stack>
  );
}

function AIAnalytics() {
  const { site, environment } = useSite();
  const [days, setDays] = useState(30);
  const [group, setGroup] = useState("model");
  const q = useQuery({
    queryKey: ["ai-analytics", site?.site_id, environment, days, group],
    placeholderData: keepWithinScope(site?.site_id, environment),
    enabled: !!site,
    queryFn: () =>
      get<{ rows: Record<string, unknown>[] }>(
        `/api/v1/sites/${site!.site_id}/ai-analytics?${rangeQuery(days, site!.timezone)}&group_by=${group}`,
      ),
  });
  if (!site) return <NoSite />;
  return (
    <Stack spacing={2}>
      <Card sx={{ p: 2 }}>
        <Stack
          direction={{ xs: "column", sm: "row" }}
          gap={2}
          alignItems={{ sm: "center" }}
        >
          <TextField
            select
            label="분석 차원"
            value={group}
            onChange={(e) => setGroup(e.target.value)}
            sx={{ minWidth: 220 }}
          >
            {["model", "provider", "agent", "mcp_server", "tool"].map(
              (item) => (
                <MenuItem value={item} key={item}>
                  {item}
                </MenuItem>
              ),
            )}
          </TextField>
          <RangeSelect
            days={days}
            setDays={setDays}
            maxExactDays={site.max_exact_days}
            timezone={site.timezone}
          />
        </Stack>
      </Card>
      {q.isLoading ? (
        <Loading />
      ) : q.error ? (
        <ErrorState error={q.error} />
      ) : (
        <Stack spacing={2}>
          {/* Every one of these events is sent by the application; an empty
              table and a table of "(not set)" are different problems that look
              the same. */}
          {aiSetupHint(
            (q.data?.rows || []) as { label?: unknown }[],
            group,
          ) && (
            <Alert severity="warning" role="status">
              {aiSetupHint(
                (q.data?.rows || []) as { label?: unknown }[],
                group,
              )}
            </Alert>
          )}
          <DataTable
            rows={q.data?.rows || []}
            columns={[
              { key: "label", label: group },
              { key: "calls", label: "호출", align: "right" },
              { key: "users", label: "사용자", align: "right" },
              {
                key: "success_rate",
                label: "성공률",
                align: "right",
                format: (v) => `${Number(v).toFixed(1)}%`,
              },
              {
                key: "average_latency_ms",
                label: "평균 지연(ms)",
                align: "right",
              },
              { key: "input_tokens", label: "Input Token", align: "right" },
              { key: "output_tokens", label: "Output Token", align: "right" },
              { key: "cost", label: "Cost", align: "right" },
              { key: "fallbacks", label: "Fallback", align: "right" },
            ]}
          />
        </Stack>
      )}
    </Stack>
  );
}

function Quality() {
  const { site, environment } = useSite();
  const q = useQuery({
    queryKey: ["data-quality", site?.site_id, environment],
    placeholderData: keepWithinScope(site?.site_id, environment),
    enabled: !!site,
    refetchInterval: 30000,
    queryFn: () =>
      get<{
        health_score: number;
        collector: Record<string, number>;
        quality: Record<string, number>;
        cardinalities: Record<string, unknown>[];
        issues: Record<string, unknown>[];
      }>(
        `/api/v1/sites/${site!.site_id}/data-quality?${rangeQuery(7, site!.timezone)}`,
      ),
  });
  if (!site) return <NoSite />;
  if (q.isLoading) return <Loading />;
  if (q.error) return <ErrorState error={q.error} />;
  const data = q.data!;
  return (
    <Stack spacing={2}>
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: { xs: "1fr 1fr", lg: "repeat(5,1fr)" },
          gap: 2,
        }}
      >
        <MetricCard label="Health Score" value={data.health_score} />
        <MetricCard label="Accepted" value={data.collector.accepted || 0} />
        <MetricCard
          label="Inbox Lag(s)"
          value={data.collector.inbox_lag_seconds || 0}
        />
        <MetricCard label="Rejected" value={data.quality.rejected || 0} />
        <MetricCard label="PII Blocked" value={data.quality.pii_blocked || 0} />
      </Box>
      <Alert
        severity={
          data.health_score >= 90
            ? "success"
            : data.health_score >= 70
              ? "warning"
              : "error"
        }
      >
        수집 계약·중복·지연·Cardinality를 반영한 {environment.toUpperCase()}{" "}
        환경 점수입니다.
      </Alert>
      {/* A refused identifier and a missing one leave the same empty field, and
          they need opposite actions: one team has to start identifying people,
          the other has to stop sending something we will not keep. */}
      {(data.quality.refused_user_id || 0) > 0 && (
        <Alert severity="warning" role="status">
          사용자 ID가 개인정보로 판정되어 <b>{data.quality.refused_user_id}</b>
          건의 이벤트를 익명으로 저장했습니다. 이 통합은 사용자를 식별하고
          있지만 이메일·전화번호·주민등록번호처럼 보관할 수 없는 값을 보내고
          있습니다. 사내 식별자나 가명 식별자로 바꿔야 합니다.
        </Alert>
      )}
      {(data.quality.missing_user_id || 0) > 0 &&
        !(data.quality.refused_user_id || 0) && (
          <Alert severity="info" role="status">
            <b>{data.quality.missing_user_id}</b>건의 이벤트에 사용자 ID가
            없습니다. 사람 단위 분석이 필요하면 로그인 시점에{" "}
            <code>analytics.identify()</code>를 호출하세요.
          </Alert>
        )}
      <Typography variant="h6">Cardinality Guard</Typography>
      <DataTable
        rows={data.cardinalities}
        columns={[
          { key: "dimension", label: "Dimension" },
          { key: "distinct_values", label: "Distinct Values", align: "right" },
        ]}
      />
      <Typography variant="h6">최근 품질 이슈</Typography>
      <DataTable
        rows={data.issues}
        columns={[
          { key: "severity", label: "심각도" },
          { key: "code", label: "Code" },
          { key: "event_name", label: "Event" },
          { key: "message", label: "내용" },
          { key: "occurred_at", label: "시각" },
        ]}
      />
    </Stack>
  );
}
