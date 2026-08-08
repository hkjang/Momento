import { useEffect, useState } from "react";
import {
  Alert,
  Box,
  Button,
  Card,
  IconButton,
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
type Step = { name: string; event: string };
export default function FunnelPage({ mode }: { mode: "funnel" | "path" }) {
  const { site } = useSite();
  const [steps, setSteps] = useState<Step[]>([
    { name: "페이지 조회", event: "page_view" },
    { name: "클릭", event: "click" },
    { name: "전환", event: "conversion" },
  ]);
  const funnel = useMutation({
    mutationFn: () =>
      post<{ steps: Record<string, unknown>[] }>("/api/v1/funnel", {
        site_id: site!.site_id,
        from: new Date(Date.now() - 29 * 86400000).toISOString().slice(0, 10),
        to: new Date().toISOString().slice(0, 10),
        steps,
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
  useEffect(() => {
    if (site && mode === "funnel") funnel.mutate();
  }, [site?.site_id]);
  if (!site) return <NoSite />;
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
        <Stack spacing={1.2}>
          {steps.map((step, i) => (
            <Stack direction="row" spacing={1} alignItems="center" key={i}>
              <Box
                sx={{
                  width: 30,
                  height: 30,
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
                label="단계 이름"
                value={step.name}
                onChange={(e) =>
                  setSteps((v) =>
                    v.map((x, j) =>
                      j === i ? { ...x, name: e.target.value } : x,
                    ),
                  )
                }
              />
              <TextField
                label="Event name"
                value={step.event}
                onChange={(e) =>
                  setSteps((v) =>
                    v.map((x, j) =>
                      j === i ? { ...x, event: e.target.value } : x,
                    ),
                  )
                }
                sx={{ flex: 1 }}
              />
              <IconButton
                disabled={steps.length <= 2}
                onClick={() => setSteps((v) => v.filter((_, j) => j !== i))}
              >
                <DeleteOutlineRounded />
              </IconButton>
            </Stack>
          ))}
        </Stack>
        <Button
          sx={{ mt: 2 }}
          startIcon={<AddRounded />}
          disabled={steps.length >= 10}
          onClick={() =>
            setSteps((v) => [...v, { name: `단계 ${v.length + 1}`, event: "" }])
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
