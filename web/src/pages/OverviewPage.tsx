import { useState } from "react";
import { Box, Card, Stack, Typography } from "@mui/material";
import PeopleAltOutlined from "@mui/icons-material/PeopleAltOutlined";
import LayersOutlined from "@mui/icons-material/LayersOutlined";
import VisibilityOutlined from "@mui/icons-material/VisibilityOutlined";
import MouseOutlined from "@mui/icons-material/MouseOutlined";
import AdsClickOutlined from "@mui/icons-material/AdsClickOutlined";
import ReactECharts from "../components/Chart";
import { useQuery } from "@tanstack/react-query";
import { get, rangeQuery } from "../api/client";
import { useSite } from "../contexts/SiteContext";
import MetricCard from "../components/MetricCard";
import { ErrorState, Loading, NoSite } from "../components/States";
import AnalysisToolbar from "../components/AnalysisToolbar";

interface Metrics {
  users: number;
  new_users: number;
  sessions: number;
  page_views: number;
  events: number;
  engagement_rate: number;
  avg_session_duration: number;
  conversions: number;
  conversion_users: number;
  conversion_sessions: number;
  conversion_rate: number;
  user_conversion_rate: number;
  session_conversion_rate: number;
  revenue: number;
}
interface Overview {
  timezone: string;
  current: Metrics;
  previous: Metrics;
  trend: {
    date: string;
    users: number;
    sessions: number;
    page_views: number;
    events: number;
    conversions: number;
  }[];
}
export default function OverviewPage() {
  const { site, environment } = useSite();
  const [days, setDays] = useState(30);
  const q = useQuery({
    queryKey: ["overview", site?.site_id, site?.timezone, environment, days],
    queryFn: () =>
      get<Overview>(
        `/api/v1/sites/${site!.site_id}/overview?${rangeQuery(days, site!.timezone)}`,
      ),
    enabled: !!site,
  });
  if (!site) return <NoSite />;
  const toolbar = (
    <AnalysisToolbar
      days={days}
      setDays={setDays}
      environment={environment}
      timezone={site.timezone}
      updatedAt={q.dataUpdatedAt}
      refreshing={q.isFetching}
      refresh={() => void q.refetch()}
      comparePrevious
    />
  );
  if (q.isLoading)
    return (
      <Stack spacing={2}>
        {toolbar}
        <Loading />
      </Stack>
    );
  if (q.error)
    return (
      <Stack spacing={2}>
        {toolbar}
        <ErrorState error={q.error} retry={() => q.refetch()} />
      </Stack>
    );
  const d = q.data!;
  const cards = [
    { k: "users", l: "사용자", icon: <PeopleAltOutlined /> },
    { k: "sessions", l: "세션", icon: <LayersOutlined /> },
    { k: "page_views", l: "페이지뷰", icon: <VisibilityOutlined /> },
    { k: "events", l: "이벤트", icon: <MouseOutlined /> },
    { k: "conversions", l: "전환", icon: <AdsClickOutlined /> },
  ] as const;
  return (
    <Stack spacing={2.5}>
      {toolbar}
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: {
            xs: "1fr",
            sm: "repeat(2,1fr)",
            xl: "repeat(5,1fr)",
          },
          gap: 2,
        }}
      >
        {cards.map((x) => (
          <MetricCard
            key={x.k}
            label={x.l}
            value={d.current[x.k]}
            previous={d.previous[x.k]}
            icon={x.icon}
          />
        ))}
      </Box>
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: { xs: "1fr", xl: "2fr 1fr" },
          gap: 2,
        }}
      >
        <Card sx={{ p: 2.5 }}>
          <Stack direction="row" justifyContent="space-between">
            <Box>
              <Typography fontWeight={700}>사용 추이</Typography>
              <Typography variant="caption" color="text.secondary">
                사용자·세션·페이지뷰
              </Typography>
            </Box>
          </Stack>
          <ReactECharts
            style={{ height: 340 }}
            option={{
              tooltip: { trigger: "axis" },
              legend: { bottom: 0, data: ["사용자", "세션", "페이지뷰"] },
              grid: {
                left: 20,
                right: 20,
                top: 35,
                bottom: 45,
                containLabel: true,
              },
              xAxis: {
                type: "category",
                boundaryGap: false,
                data: d.trend.map((x) => x.date.slice(5)),
                axisLine: { lineStyle: { color: "#DDE2EA" } },
              },
              yAxis: {
                type: "value",
                splitLine: { lineStyle: { color: "#EEF1F5" } },
              },
              series: [
                {
                  name: "사용자",
                  type: "line",
                  smooth: true,
                  symbol: "none",
                  lineStyle: { width: 3, color: "#5B5CE2" },
                  areaStyle: {
                    color: {
                      type: "linear",
                      x: 0,
                      y: 0,
                      x2: 0,
                      y2: 1,
                      colorStops: [
                        { offset: 0, color: "rgba(91,92,226,.2)" },
                        { offset: 1, color: "rgba(91,92,226,0)" },
                      ],
                    },
                  },
                  data: d.trend.map((x) => x.users),
                },
                {
                  name: "세션",
                  type: "line",
                  smooth: true,
                  symbol: "none",
                  lineStyle: { width: 2, color: "#14B8A6" },
                  data: d.trend.map((x) => x.sessions),
                },
                {
                  name: "페이지뷰",
                  type: "line",
                  smooth: true,
                  symbol: "none",
                  lineStyle: { width: 2, color: "#F59E0B" },
                  data: d.trend.map((x) => x.page_views),
                },
              ],
            }}
          />
        </Card>
        <Stack spacing={2}>
          <MetricCard
            label="참여율"
            value={d.current.engagement_rate}
            previous={d.previous.engagement_rate}
            type="percent"
          />
          <MetricCard
            label="평균 세션 시간"
            value={d.current.avg_session_duration}
            previous={d.previous.avg_session_duration}
            type="duration"
          />
          <MetricCard
            label="사용자 전환율"
            value={d.current.user_conversion_rate}
            previous={d.previous.user_conversion_rate}
            type="percent"
          />
          <MetricCard
            label="세션 전환율"
            value={d.current.session_conversion_rate}
            previous={d.previous.session_conversion_rate}
            type="percent"
          />
          <MetricCard
            label="매출"
            value={d.current.revenue}
            previous={d.previous.revenue}
            type="currency"
          />
        </Stack>
      </Box>
    </Stack>
  );
}
