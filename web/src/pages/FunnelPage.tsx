import { useEffect, useState } from "react";
import {
  Alert,
  Box,
  Button,
  Card,
  Chip,
  FormControlLabel,
  IconButton,
  MenuItem,
  Stack,
  Switch,
  TextField,
  Typography,
} from "@mui/material";
import AddRounded from "@mui/icons-material/AddRounded";
import DeleteOutlineRounded from "@mui/icons-material/DeleteOutlineRounded";
import PlayArrowRounded from "@mui/icons-material/PlayArrowRounded";
import RefreshRounded from "@mui/icons-material/RefreshRounded";
import ReactECharts from "../components/Chart";
import { useMutation, useQuery } from "@tanstack/react-query";
import { dateRangeValues, get, post, rangeQuery } from "../api/client";
import { useSite } from "../contexts/SiteContext";
import DataTable from "../components/DataTable";
import { Empty, ErrorState, Loading, NoSite } from "../components/States";
import { builtInSegmentFields } from "../components/SegmentBuilder";

type FunnelStep = Record<string, unknown> & {
  name: string;
  users: number;
  overall_conversion_rate: number;
};

type FunnelComparison = {
  key: string;
  label: string;
  entered: number;
  completion_rate: number;
  baseline_completion_rate: number;
  lift_points: number;
  lift_percent: number;
  worst_step: number;
  worst_step_name: string;
  verdict: "better" | "worse" | "similar" | "insufficient";
  evidence: string;
  reliable: boolean;
};

type FunnelResult = {
  steps: FunnelStep[];
  series?: { key: string; label: string; steps: FunnelStep[] }[];
  comparison?: FunnelComparison[];
};

const seriesColors = ["#5B5CE2", "#12A875", "#E2A03F", "#C43E44"];

const verdictLabel: Record<FunnelComparison["verdict"], string> = {
  better: "더 높음",
  worse: "더 낮음",
  similar: "비슷함",
  insufficient: "표본 부족",
};

const verdictColor: Record<FunnelComparison["verdict"], "success" | "error" | "default" | "warning"> = {
  better: "success",
  worse: "error",
  similar: "default",
  insufficient: "warning",
};
import {
  buildPathFlow,
  type PathNode,
  type PathTransition,
} from "./pathFlow";

type Step = {
  name: string;
  event: string;
  filterField: string;
  filterOperator: string;
  filterValue: string;
};

export default function FunnelPage({ mode }: { mode: "funnel" | "path" }) {
  const { site, environment } = useSite();
  const [steps, setSteps] = useState<Step[]>([
    {
      name: "페이지 조회",
      event: "page_view",
      filterField: "",
      filterOperator: "=",
      filterValue: "",
    },
    {
      name: "클릭",
      event: "click",
      filterField: "",
      filterOperator: "=",
      filterValue: "",
    },
    {
      name: "전환",
      event: "conversion",
      filterField: "",
      filterOperator: "=",
      filterValue: "",
    },
  ]);
  const [funnelMode, setFunnelMode] = useState("closed");
  const [withinMinutes, setWithinMinutes] = useState(0);
  const [segmentId, setSegmentId] = useState("");
  const [compareIds, setCompareIds] = useState<string[]>([]);
  const [pathDays, setPathDays] = useState(30);
  const [pathView, setPathView] = useState<"pages" | "events" | "all">(
    "pages",
  );
  const [includeSystemEvents, setIncludeSystemEvents] = useState(false);
  const segments = useQuery({
    queryKey: ["segments", site?.site_id],
    queryFn: () =>
      get<{ id: string; name: string }[]>(
        `/api/v1/segments?site_id=${site!.site_id}`,
      ),
    enabled: !!site && mode === "funnel",
  });
  const customDimensions = useQuery({
    queryKey: ["dimensions", site?.site_id],
    queryFn: () =>
      get<{ query_name: string; active: boolean; scope: string }[]>(
        `/api/v1/dimensions?site_id=${site!.site_id}`,
      ),
    enabled: !!site && mode === "funnel",
  });
  const funnel = useMutation({
    mutationFn: () =>
      post<FunnelResult>("/api/v1/funnel", {
        site_id: site!.site_id,
        environment,
        ...dateRangeValues(30, site!.timezone),
        mode: funnelMode,
        within_minutes: withinMinutes,
        segment_id: segmentId || undefined,
        compare_segment_ids: compareIds.length ? compareIds : undefined,
        steps: steps.map((step) => ({
          name: step.name,
          event: step.event,
          filters: step.filterField
            ? [
                {
                  field: step.filterField,
                  operator: step.filterOperator,
                  value: step.filterValue,
                },
              ]
            : [],
        })),
      }),
  });
  const paths = useQuery({
    queryKey: [
      "paths",
      site?.site_id,
      site?.timezone,
      environment,
      pathDays,
      pathView,
      includeSystemEvents,
    ],
    queryFn: () =>
      get<PathTransition[]>(
        `/api/v1/sites/${site!.site_id}/path?${rangeQuery(pathDays, site!.timezone)}&view=${pathView}&include_system=${includeSystemEvents}`,
      ),
    enabled: !!site && mode === "path",
  });
  const runFunnel = funnel.mutate;
  const siteID = site?.site_id;
  const siteTimezone = site?.timezone;
  useEffect(() => {
    if (siteID && mode === "funnel") runFunnel();
  }, [siteID, siteTimezone, mode, runFunnel]);
  if (!site) return <NoSite />;
  const funnelFields = [
    ...builtInSegmentFields,
    ...(customDimensions.data || [])
      .filter((item) => item.active && item.scope !== "item")
      .map((item) => item.query_name),
  ];
  if (mode === "path") {
    const rows = paths.data || [];
    const flow = buildPathFlow(rows);
    const totalMoves = rows.reduce((sum, row) => sum + row.count, 0);
    const pathLabels = {
      pages: {
        title: "페이지 이동 경로",
        description:
          "업무 이벤트 사이를 건너뛰고 같은 세션의 다음 Page View를 연결합니다.",
      },
      events: {
        title: "이벤트 행동 경로",
        description: "Page View를 제외한 업무 이벤트의 연속 흐름입니다.",
      },
      all: {
        title: "페이지 · 이벤트 통합 경로",
        description: "같은 세션에서 연속으로 발생한 페이지와 이벤트입니다.",
      },
    }[pathView];
    const tableRows = rows.map((row) => ({
      ...row,
      share: totalMoves ? (row.count * 100) / totalMoves : 0,
    }));
    return (
      <Stack spacing={2}>
        <Card sx={{ p: 2.5 }}>
          <Stack
            direction={{ xs: "column", sm: "row" }}
            justifyContent="space-between"
            alignItems={{ sm: "flex-start" }}
            gap={1.5}
            mb={2}
          >
            <Box>
              <Typography fontWeight={700}>{pathLabels.title}</Typography>
              <Typography variant="body2" color="text.secondary">
                {pathLabels.description}
              </Typography>
            </Box>
            <Stack direction="row" gap={0.75} flexWrap="wrap">
              <Chip size="small" label={`경로 ${rows.length}개`} />
              <Chip
                size="small"
                variant="outlined"
                label={`이동 ${totalMoves.toLocaleString()}회`}
              />
              <Chip
                size="small"
                variant="outlined"
                label={`${environment.toUpperCase()} · 최근 ${pathDays}일`}
              />
            </Stack>
          </Stack>
          <Stack
            direction={{ xs: "column", md: "row" }}
            spacing={1.5}
            alignItems={{ md: "center" }}
            mb={2}
          >
            <TextField
              select
              size="small"
              label="분석 기간"
              value={pathDays}
              onChange={(event) => setPathDays(Number(event.target.value))}
              sx={{ minWidth: 140 }}
            >
              <MenuItem value={7}>최근 7일</MenuItem>
              <MenuItem value={30}>최근 30일</MenuItem>
              <MenuItem value={90}>최근 90일</MenuItem>
            </TextField>
            <TextField
              select
              size="small"
              label="경로 기준"
              value={pathView}
              onChange={(event) =>
                setPathView(event.target.value as "pages" | "events" | "all")
              }
              sx={{ minWidth: 190 }}
            >
              <MenuItem value="pages">페이지 이동</MenuItem>
              <MenuItem value="events">업무 이벤트</MenuItem>
              <MenuItem value="all">페이지 + 이벤트</MenuItem>
            </TextField>
            <FormControlLabel
              control={
                <Switch
                  checked={includeSystemEvents}
                  onChange={(event) =>
                    setIncludeSystemEvents(event.target.checked)
                  }
                />
              }
              label="시스템 이벤트 포함"
              sx={{ mr: "auto" }}
            />
            {paths.dataUpdatedAt > 0 && (
              <Typography variant="caption" color="text.secondary" noWrap>
                {new Date(paths.dataUpdatedAt).toLocaleTimeString("ko-KR", {
                  hour: "2-digit",
                  minute: "2-digit",
                })}
                에 갱신
              </Typography>
            )}
            <Button
              variant="outlined"
              startIcon={<RefreshRounded />}
              disabled={paths.isFetching}
              onClick={() => void paths.refetch()}
            >
              새로고침
            </Button>
          </Stack>
          {paths.isLoading ? (
            <Loading label="이동 경로를 계산하는 중" />
          ) : paths.error ? (
            <ErrorState error={paths.error} retry={() => paths.refetch()} />
          ) : flow.links.length ? (
            <ReactECharts
              style={{ height: 480 }}
              option={{
                tooltip: {
                  formatter: (params: {
                    dataType: string;
                    data: {
                      displayName?: string;
                      sourceName?: string;
                      targetName?: string;
                      value?: number;
                    };
                  }) =>
                    params.dataType === "edge"
                      ? `${params.data.sourceName} → ${params.data.targetName}<br/>${Number(params.data.value || 0).toLocaleString()}회`
                      : params.data.displayName || "",
                },
                series: [
                  {
                    type: "sankey",
                    layout: "none",
                    nodeAlign: "justify",
                    emphasis: { focus: "adjacency" },
                    label: {
                      formatter: (params: { data: PathNode }) =>
                        params.data.shortName,
                    },
                    lineStyle: {
                      color: "gradient",
                      curveness: 0.5,
                      opacity: 0.25,
                    },
                    data: flow.nodes,
                    links: flow.links,
                  },
                ],
              }}
            />
          ) : (
            <Empty
              title="표시할 이동 경로가 없습니다"
              description={`선택한 기간과 환경에서 한 세션에 두 개 이상의 ${pathView === "pages" ? "Page View가" : "이벤트가"} 수집되면 경로가 표시됩니다.`}
            />
          )}
        </Card>
        <DataTable
          title="상위 이동 상세"
          description="자기 자신으로 반복된 이동은 제외하며 비중은 조회된 상위 경로 기준입니다."
          exportFilename={`momento-path-${pathView}-${pathDays}d.csv`}
          searchable
          columns={[
            { key: "source", label: "시작" },
            { key: "target", label: "다음" },
            {
              key: "count",
              label: "이동",
              align: "right",
              format: (value) => Number(value).toLocaleString(),
            },
            {
              key: "share",
              label: "비중",
              align: "right",
              format: (value) => `${Number(value).toFixed(1)}%`,
            },
          ]}
          rows={tableRows}
        />
      </Stack>
    );
  }
  return (
    <Stack spacing={2}>
      <Card sx={{ p: 2.5 }}>
        <Stack
          direction="row"
          justifyContent="space-between"
          alignItems="center"
          mb={2}
        >
          <Box>
            <Typography fontWeight={700}>퍼널 단계</Typography>
            <Typography variant="caption" color="text.secondary">
              순서대로 발생한 이벤트를 사용자 기준으로 계산합니다.
            </Typography>
          </Box>
          <Button
            variant="contained"
            startIcon={<PlayArrowRounded />}
            onClick={() => funnel.mutate()}
            disabled={funnel.isPending}
          >
            분석
          </Button>
        </Stack>
        <Box
          sx={{
            display: "grid",
            gridTemplateColumns: { xs: "1fr", md: "180px 1fr 220px" },
            gap: 1.5,
            mb: 2,
          }}
        >
          <TextField
            select
            size="small"
            label="Funnel 유형"
            value={funnelMode}
            onChange={(event) => setFunnelMode(event.target.value)}
          >
            <MenuItem value="closed">Closed Funnel</MenuItem>
            <MenuItem value="open">Open Funnel</MenuItem>
          </TextField>
          <TextField
            select
            size="small"
            label="Segment"
            value={segmentId}
            onChange={(event) => setSegmentId(event.target.value)}
          >
            <MenuItem value="">전체 사용자</MenuItem>
            {(segments.data || []).map((segment) => (
              <MenuItem key={segment.id} value={segment.id}>
                {segment.name}
              </MenuItem>
            ))}
          </TextField>
          <TextField
            select
            size="small"
            label="비교 Segment"
            value={compareIds}
            onChange={(event) =>
              setCompareIds(
                (typeof event.target.value === "string"
                  ? event.target.value.split(",")
                  : (event.target.value as unknown as string[])
                ).slice(0, 3),
              )
            }
            slotProps={{ select: { multiple: true } }}
            helperText="최대 3개. 전체와 나란히 비교합니다."
          >
            {(segments.data || []).map((segment) => (
              <MenuItem key={segment.id} value={segment.id}>
                {segment.name}
              </MenuItem>
            ))}
          </TextField>
          <TextField
            size="small"
            type="number"
            label="최대 전환 시간 (분)"
            value={withinMinutes || ""}
            onChange={(event) => setWithinMinutes(Number(event.target.value))}
            helperText="비워 두면 제한 없음"
          />
        </Box>
        <Stack spacing={1.2}>
          {steps.map((step, i) => (
            <Card
              key={i}
              variant="outlined"
              sx={{ p: 1.5, bgcolor: "#FAFBFD" }}
            >
              <Stack
                direction={{ xs: "column", lg: "row" }}
                spacing={1}
                alignItems={{ lg: "center" }}
              >
                <Box
                  sx={{
                    width: 30,
                    height: 30,
                    flex: "0 0 auto",
                    borderRadius: "50%",
                    bgcolor: "#EEEEFF",
                    color: "primary.main",
                    display: "grid",
                    placeItems: "center",
                    fontWeight: 750,
                  }}
                >
                  {i + 1}
                </Box>
                <TextField
                  size="small"
                  label="단계 이름"
                  value={step.name}
                  onChange={(event) =>
                    setSteps((value) =>
                      value.map((item, index) =>
                        index === i
                          ? { ...item, name: event.target.value }
                          : item,
                      ),
                    )
                  }
                />
                <TextField
                  size="small"
                  label="Event name"
                  value={step.event}
                  onChange={(event) =>
                    setSteps((value) =>
                      value.map((item, index) =>
                        index === i
                          ? { ...item, event: event.target.value }
                          : item,
                      ),
                    )
                  }
                  sx={{ minWidth: 180 }}
                />
                <TextField
                  select
                  size="small"
                  label="Property 조건"
                  value={step.filterField}
                  onChange={(event) =>
                    setSteps((value) =>
                      value.map((item, index) =>
                        index === i
                          ? { ...item, filterField: event.target.value }
                          : item,
                      ),
                    )
                  }
                  sx={{ minWidth: 190 }}
                >
                  <MenuItem value="">조건 없음</MenuItem>
                  {funnelFields.map((field) => (
                    <MenuItem key={field} value={field}>
                      {field}
                    </MenuItem>
                  ))}
                </TextField>
                <TextField
                  select
                  size="small"
                  label="연산자"
                  value={step.filterOperator}
                  disabled={!step.filterField}
                  onChange={(event) =>
                    setSteps((value) =>
                      value.map((item, index) =>
                        index === i
                          ? { ...item, filterOperator: event.target.value }
                          : item,
                      ),
                    )
                  }
                  sx={{ minWidth: 105 }}
                >
                  {["=", "!=", "contains", ">", ">=", "<", "<=", "exists"].map(
                    (operator) => (
                      <MenuItem key={operator} value={operator}>
                        {operator}
                      </MenuItem>
                    ),
                  )}
                </TextField>
                <TextField
                  size="small"
                  label="조건 값"
                  value={step.filterValue}
                  disabled={
                    !step.filterField || step.filterOperator === "exists"
                  }
                  onChange={(event) =>
                    setSteps((value) =>
                      value.map((item, index) =>
                        index === i
                          ? { ...item, filterValue: event.target.value }
                          : item,
                      ),
                    )
                  }
                  sx={{ flex: 1 }}
                />
                <IconButton
                  disabled={steps.length <= 2}
                  onClick={() =>
                    setSteps((value) => value.filter((_, index) => index !== i))
                  }
                >
                  <DeleteOutlineRounded />
                </IconButton>
              </Stack>
            </Card>
          ))}
        </Stack>
        <Button
          sx={{ mt: 2 }}
          startIcon={<AddRounded />}
          disabled={steps.length >= 10}
          onClick={() =>
            setSteps((v) => [
              ...v,
              {
                name: `단계 ${v.length + 1}`,
                event: "",
                filterField: "",
                filterOperator: "=",
                filterValue: "",
              },
            ])
          }
        >
          단계 추가
        </Button>
      </Card>
      {funnel.error && <Alert severity="error">{funnel.error.message}</Alert>}
      {funnel.data && (
        <>
          <Card sx={{ p: 2.5 }}>
            <ReactECharts
              style={{ height: 340 }}
              option={{
                tooltip: { trigger: "axis" },
                grid: { left: 20, right: 20, bottom: 20, containLabel: true },
                xAxis: {
                  type: "category",
                  data: funnel.data.steps.map((x) => x.name),
                },
                yAxis: {
                  type: "value",
                  axisLabel: funnel.data.series ? { formatter: "{value}%" } : undefined,
                },
                legend: funnel.data.series
                  ? { data: funnel.data.series.map((item) => item.label), bottom: 0 }
                  : undefined,
                series: funnel.data.series
                  ? // Comparing cohorts uses the completion rate, because raw counts
                    // put a small department next to a large one and hide the shape.
                    funnel.data.series.map((item, index) => ({
                      name: item.label,
                      type: "bar",
                      data: item.steps.map((step) => Number(step.overall_conversion_rate).toFixed(1)),
                      itemStyle: {
                        color: seriesColors[index % seriesColors.length],
                        borderRadius: [7, 7, 0, 0],
                      },
                      barMaxWidth: 60,
                    }))
                  : [
                      {
                        type: "bar",
                        data: funnel.data.steps.map((x, i) => ({
                          value: x.users,
                          itemStyle: {
                            color: ["#5B5CE2", "#7779EA", "#999AF1", "#B8B9F5"][
                              Math.min(i, 3)
                            ],
                            borderRadius: [7, 7, 0, 0],
                          },
                        })),
                        barMaxWidth: 100,
                        label: { show: true, position: "top", formatter: "{c}명" },
                      },
                    ],
              }}
            />
          </Card>
          <DataTable
            columns={[
              { key: "step", label: "단계" },
              { key: "name", label: "이름" },
              { key: "event", label: "이벤트" },
              { key: "users", label: "사용자", align: "right" },
              {
                key: "step_conversion_rate",
                label: "단계 전환율",
                align: "right",
                format: (v) => `${Number(v).toFixed(1)}%`,
              },
              {
                key: "overall_conversion_rate",
                label: "전체 전환율",
                align: "right",
                format: (v) => `${Number(v).toFixed(1)}%`,
              },
            ]}
            rows={funnel.data.steps}
          />
          {funnel.data.comparison && funnel.data.comparison.length > 0 && (
            <Card sx={{ p: 2.5 }}>
              <Typography variant="h6">Segment 비교</Typography>
              <Typography variant="body2" color="text.secondary" mb={2}>
                전체 대비 완주율 차이가 큰 순서입니다. 격차가 가장 크게 벌어지는
                단계가 먼저 확인할 지점입니다.
              </Typography>
              <Stack spacing={1.2}>
                {funnel.data.comparison.map((item) => (
                  <Card key={item.key} variant="outlined" sx={{ p: 1.8 }}>
                    <Stack direction="row" gap={1} alignItems="center" flexWrap="wrap">
                      <Chip
                        size="small"
                        color={verdictColor[item.verdict]}
                        label={verdictLabel[item.verdict]}
                      />
                      <Typography fontWeight={700}>{item.label}</Typography>
                      <Chip
                        size="small"
                        variant="outlined"
                        label={`${item.lift_points >= 0 ? "+" : ""}${item.lift_points.toFixed(1)}pp`}
                      />
                      {item.worst_step > 0 && (
                        <Chip
                          size="small"
                          variant="outlined"
                          color="warning"
                          label={`${item.worst_step}단계 ${item.worst_step_name}`}
                        />
                      )}
                    </Stack>
                    <Typography variant="body2" color="text.secondary" mt={0.6}>
                      {item.evidence}
                    </Typography>
                  </Card>
                ))}
              </Stack>
            </Card>
          )}
        </>
      )}
    </Stack>
  );
}
