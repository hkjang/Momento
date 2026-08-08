import { useEffect, useState } from "react";
import {
  Alert,
  Box,
  Button,
  Card,
  IconButton,
  MenuItem,
  Stack,
  TextField,
  Typography,
} from "@mui/material";
import AddRounded from "@mui/icons-material/AddRounded";
import DeleteOutlineRounded from "@mui/icons-material/DeleteOutlineRounded";
import PlayArrowRounded from "@mui/icons-material/PlayArrowRounded";
import ReactECharts from "../components/Chart";
import { useMutation, useQuery } from "@tanstack/react-query";
import { get, post, rangeQuery } from "../api/client";
import { useSite } from "../contexts/SiteContext";
import DataTable from "../components/DataTable";
import { ErrorState, Loading, NoSite } from "../components/States";
import { builtInSegmentFields } from "../components/SegmentBuilder";
type Step = {
  name: string;
  event: string;
  filterField: string;
  filterOperator: string;
  filterValue: string;
};
export default function FunnelPage({ mode }: { mode: "funnel" | "path" }) {
  const { site } = useSite();
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
      post<{ steps: Record<string, unknown>[] }>("/api/v1/funnel", {
        site_id: site!.site_id,
        from: new Date(Date.now() - 29 * 86400000).toISOString().slice(0, 10),
        to: new Date().toISOString().slice(0, 10),
        mode: funnelMode,
        within_minutes: withinMinutes,
        segment_id: segmentId || undefined,
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
    queryKey: ["paths", site?.site_id],
    queryFn: () =>
      get<Record<string, unknown>[]>(
        `/api/v1/sites/${site!.site_id}/path?${rangeQuery()}`,
      ),
    enabled: !!site && mode === "path",
  });
  const runFunnel = funnel.mutate;
  const siteID = site?.site_id;
  useEffect(() => {
    if (siteID && mode === "funnel") runFunnel();
  }, [siteID, mode, runFunnel]);
  if (!site) return <NoSite />;
  const funnelFields = [
    ...builtInSegmentFields,
    ...(customDimensions.data || [])
      .filter((item) => item.active && item.scope !== "item")
      .map((item) => item.query_name),
  ];
  if (mode === "path") {
    if (paths.isLoading) return <Loading />;
    if (paths.error) return <ErrorState error={paths.error} />;
    const rows = paths.data || [];
    return (
      <Stack spacing={2}>
        <Card sx={{ p: 2.5 }}>
          <Typography fontWeight={700}>상위 사용자 이동</Typography>
          <Typography variant="body2" color="text.secondary" mb={2}>
            동일 세션에서 연속으로 발생한 페이지·이벤트 전환입니다.
          </Typography>
          <ReactECharts
            style={{ height: 480 }}
            option={{
              tooltip: {},
              series: [
                {
                  type: "sankey",
                  layout: "none",
                  emphasis: { focus: "adjacency" },
                  lineStyle: {
                    color: "gradient",
                    curveness: 0.5,
                    opacity: 0.25,
                  },
                  data: Array.from(
                    new Set(
                      rows.flatMap((r) => [String(r.source), String(r.target)]),
                    ),
                  )
                    .slice(0, 60)
                    .map((name) => ({ name })),
                  links: rows
                    .map((r) => ({
                      source: r.source,
                      target: r.target,
                      value: r.count,
                    }))
                    .filter((x) => x.source !== x.target),
                },
              ],
            }}
          />
        </Card>
        <DataTable
          columns={[
            { key: "source", label: "시작" },
            { key: "target", label: "다음" },
            { key: "count", label: "이동", align: "right" },
          ]}
          rows={rows}
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
                yAxis: { type: "value" },
                series: [
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
        </>
      )}
    </Stack>
  );
}
