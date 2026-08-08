import { Box, Card, Stack, Typography } from "@mui/material";
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
  const { site } = useSite();
  const q = useQuery({
    queryKey: ["realtime", site?.site_id],
    queryFn: () => get<Realtime>(`/api/v1/sites/${site!.site_id}/realtime`),
    enabled: !!site,
    refetchInterval: 5000,
  });
  if (!site) return <NoSite />;
  if (q.isLoading) return <Loading />;
  if (q.error) return <ErrorState error={q.error} />;
  const d = q.data!;
  return (
    <Stack spacing={2.5}>
      <Stack direction="row" alignItems="center" gap={1}>
        <Box
          sx={{
            width: 9,
            height: 9,
            borderRadius: "50%",
            bgcolor: "success.main",
            boxShadow: "0 0 0 5px rgba(18,168,117,.12)",
          }}
        />
        <Typography variant="body2" fontWeight={650}>
          Live · 5초마다 갱신
        </Typography>
      </Stack>
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
          columns={[
            { key: "name", label: "상위 이벤트" },
            { key: "count", label: "이벤트", align: "right" },
            { key: "users", label: "사용자", align: "right" },
          ]}
          rows={d.top_events}
        />
        <DataTable
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
