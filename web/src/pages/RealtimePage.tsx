import { useState } from "react";
import { Box, Button, Card, Chip, Stack, Typography } from "@mui/material";
import PauseRounded from "@mui/icons-material/PauseRounded";
import PlayArrowRounded from "@mui/icons-material/PlayArrowRounded";
import RefreshRounded from "@mui/icons-material/RefreshRounded";
import ReactECharts from "../components/Chart";
import { useQuery } from "@tanstack/react-query";
import { get } from "../api/client";
import { useSite } from "../contexts/SiteContext";
import MetricCard from "../components/MetricCard";
import DataTable from "../components/DataTable";
import { ErrorState, Loading, NoSite } from "../components/States";
interface Realtime {
  active_users_1m: number;
  active_users_5m: number;
  active_users_30m: number;
  events_30m: number;
  events_per_second: number;
  top_events: Record<string, unknown>[];
  top_pages: Record<string, unknown>[];
  timeline: { time: string; events: number; page_views: number }[];
}
export default function RealtimePage() {
  const { site, environment } = useSite();
  const [live, setLive] = useState(true);
  const q = useQuery({
    queryKey: ["realtime", site?.site_id, environment],
    queryFn: () => get<Realtime>(`/api/v1/sites/${site!.site_id}/realtime`),
    enabled: !!site,
    refetchInterval: live ? 5000 : false,
  });
  if (!site) return <NoSite />;
  const controls = (
    <Card variant="outlined" sx={{ px: 2, py: 1.4 }}>
      <Stack
        direction={{ xs: "column", sm: "row" }}
        alignItems={{ sm: "center" }}
        spacing={1.25}
      >
        <Stack direction="row" alignItems="center" gap={1}>
          <Box
            sx={{
              width: 9,
              height: 9,
              borderRadius: "50%",
              bgcolor: live ? "success.main" : "text.disabled",
              boxShadow: live ? "0 0 0 5px rgba(18,168,117,.12)" : "none",
            }}
          />
          <Typography variant="body2" fontWeight={650}>
            {live ? "Live · 5초마다 갱신" : "자동 갱신 일시정지"}
          </Typography>
        </Stack>
        <Chip size="small" variant="outlined" label={environment.toUpperCase()} />
        {q.dataUpdatedAt > 0 && (
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{ ml: { sm: "auto!important" } }}
          >
            마지막 갱신 {new Date(q.dataUpdatedAt).toLocaleTimeString("ko-KR")}
          </Typography>
        )}
        <Stack direction="row" spacing={1}>
          <Button
            size="small"
            variant="outlined"
            startIcon={live ? <PauseRounded /> : <PlayArrowRounded />}
            onClick={() => setLive((value) => !value)}
          >
            {live ? "일시정지" : "Live 재개"}
          </Button>
          <Button
            size="small"
            variant="outlined"
            startIcon={<RefreshRounded />}
            disabled={q.isFetching}
            onClick={() => void q.refetch()}
          >
            지금 갱신
          </Button>
        </Stack>
      </Stack>
    </Card>
  );
  if (q.isLoading)
    return (
      <Stack spacing={2}>
        {controls}
        <Loading />
      </Stack>
    );
  if (q.error)
    return (
      <Stack spacing={2}>
        {controls}
        <ErrorState error={q.error} retry={() => q.refetch()} />
      </Stack>
    );
  const d = q.data!;
  return (
    <Stack spacing={2.5}>
      {controls}
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: {
            xs: "1fr",
            sm: "repeat(2,1fr)",
            lg: "repeat(4,1fr)",
          },
          gap: 2,
        }}
      >
        <MetricCard label="활성 사용자 · 1분" value={d.active_users_1m} />
        <MetricCard label="활성 사용자 · 5분" value={d.active_users_5m} />
        <MetricCard label="활성 사용자 · 30분" value={d.active_users_30m} />
        <MetricCard label="초당 이벤트" value={d.events_per_second} />
      </Box>
      <Card sx={{ p: 2.5 }}>
        <Typography fontWeight={700}>최근 30분 이벤트</Typography>
        <ReactECharts
          style={{ height: 260 }}
          option={{
            tooltip: { trigger: "axis" },
            grid: {
              left: 20,
              right: 20,
              top: 30,
              bottom: 20,
              containLabel: true,
            },
            xAxis: {
              type: "category",
              data: d.timeline.map((x) =>
                new Date(x.time).toLocaleTimeString("ko-KR", {
                  hour: "2-digit",
                  minute: "2-digit",
                }),
              ),
            },
            yAxis: {
              type: "value",
              splitLine: { lineStyle: { color: "#EEF1F5" } },
            },
            series: [
              {
                type: "bar",
                name: "이벤트",
                data: d.timeline.map((x) => x.events),
                itemStyle: { color: "#6D6FE8", borderRadius: [4, 4, 0, 0] },
              },
              {
                type: "line",
                name: "페이지뷰",
                data: d.timeline.map((x) => x.page_views),
                smooth: true,
                lineStyle: { color: "#14B8A6", width: 2 },
              },
            ],
          }}
        />
      </Card>
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: { xs: "1fr", lg: "1fr 1fr" },
          gap: 2,
        }}
      >
        <DataTable
          title="상위 이벤트"
          description="최근 30분 이벤트와 활성 사용자"
          exportFilename="momento-realtime-events.csv"
          columns={[
            { key: "name", label: "상위 이벤트" },
            { key: "count", label: "이벤트", align: "right" },
            { key: "users", label: "사용자", align: "right" },
          ]}
          rows={d.top_events}
        />
        <DataTable
          title="상위 페이지"
          description="최근 30분 Page View와 활성 사용자"
          exportFilename="momento-realtime-pages.csv"
          columns={[
            { key: "name", label: "상위 페이지" },
            { key: "count", label: "조회", align: "right" },
            { key: "users", label: "사용자", align: "right" },
          ]}
          rows={d.top_pages}
        />
      </Box>
    </Stack>
  );
}
