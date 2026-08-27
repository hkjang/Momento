import { useState } from "react";
import {
  Alert,
  Box,
  Button,
  Card,
  Chip,
  Stack,
  Typography,
} from "@mui/material";
import { Link as RouterLink } from "react-router-dom";
import InsightsRounded from "@mui/icons-material/InsightsRounded";
import PeopleAltOutlined from "@mui/icons-material/PeopleAltOutlined";
import LayersOutlined from "@mui/icons-material/LayersOutlined";
import VisibilityOutlined from "@mui/icons-material/VisibilityOutlined";
import MouseOutlined from "@mui/icons-material/MouseOutlined";
import AdsClickOutlined from "@mui/icons-material/AdsClickOutlined";
import ReactECharts from "../components/Chart";
import { useQuery } from "@tanstack/react-query";
import { get, rangeQuery } from "../api/client";
import { keepWithinScope } from "../api/keepPrevious";
import { useSite } from "../contexts/SiteContext";
import MetricCard from "../components/MetricCard";
import { ErrorState, Loading, NoSite } from "../components/States";
import AnalysisToolbar from "../components/AnalysisToolbar";
import {
  buildAttentionItems,
  type AttentionSeverity,
  type GoalEvaluation,
} from "./attention";
import type { AnomalyReport } from "./visitorInsights";

const attentionColor: Record<AttentionSeverity, "error" | "warning" | "info"> =
  {
    critical: "error",
    warning: "warning",
    info: "info",
  };

const attentionLabel: Record<AttentionSeverity, string> = {
  critical: "심각",
  warning: "주의",
  info: "참고",
};

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
    placeholderData: keepWithinScope(site?.site_id, environment),
    queryFn: () =>
      get<Overview>(
        `/api/v1/sites/${site!.site_id}/overview?${rangeQuery(days, site!.timezone)}`,
      ),
    enabled: !!site,
  });
  // Both reads are cheap: anomalies come from the daily rollups and goals from the
  // metric registry, so the landing screen stays fast.
  const anomalies = useQuery({
    queryKey: ["overview-anomalies", site?.site_id, environment],
    placeholderData: keepWithinScope(site?.site_id, environment),
    enabled: !!site,
    queryFn: () =>
      get<AnomalyReport>(
        `/api/v1/sites/${site!.site_id}/anomalies?environment=${environment}`,
      ),
  });
  const goals = useQuery({
    queryKey: ["overview-goals", site?.site_id, environment],
    placeholderData: keepWithinScope(site?.site_id, environment),
    enabled: !!site,
    queryFn: () =>
      get<GoalEvaluation[]>(
        `/api/v1/sites/${site!.site_id}/metric-goals/evaluate`,
      ),
  });
  if (!site) return <NoSite />;
  const attention = buildAttentionItems(
    anomalies.data?.detected,
    anomalies.data?.transitions,
    goals.data,
  );
  const attentionReady = anomalies.isSuccess || goals.isSuccess;
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
      {attentionReady && (
        <Card sx={{ p: 2.5 }}>
          <Stack
            direction="row"
            justifyContent="space-between"
            alignItems="center"
            gap={1}
            mb={attention.items.length ? 1.5 : 0}
          >
            <Stack direction="row" gap={1.2} alignItems="center">
              <InsightsRounded color="primary" />
              <Typography fontWeight={720}>지금 봐야 할 것</Typography>
            </Stack>
            <Button
              size="small"
              component={RouterLink}
              to="/visitor-insights"
              endIcon={<InsightsRounded />}
            >
              방문자 인사이트
            </Button>
          </Stack>
          {attention.items.length ? (
            <Stack spacing={1.2}>
              {attention.items.map((item) => (
                <Card key={item.id} variant="outlined" sx={{ p: 1.8 }}>
                  <Stack
                    direction="row"
                    gap={1}
                    alignItems="center"
                    flexWrap="wrap"
                  >
                    <Chip
                      size="small"
                      color={attentionColor[item.severity]}
                      label={attentionLabel[item.severity]}
                    />
                    <Typography fontWeight={700}>{item.title}</Typography>
                    <Box flexGrow={1} />
                    <Button size="small" component={RouterLink} to={item.to}>
                      확인
                    </Button>
                  </Stack>
                  <Typography variant="body2" color="text.secondary" mt={0.5}>
                    {item.detail}
                  </Typography>
                  {item.action && (
                    <Typography variant="body2" color="primary.main" mt={0.3}>
                      다음 행동 · {item.action}
                    </Typography>
                  )}
                </Card>
              ))}
              {attention.hidden > 0 && (
                <Typography variant="caption" color="text.secondary">
                  그 외 {attention.hidden}건은 방문자 인사이트와 Goal 화면에서
                  확인하십시오.
                </Typography>
              )}
            </Stack>
          ) : (
            <Alert severity="success">
              감시 지표가 기준선 안에 있고 미달 전망 Goal도 없습니다.
            </Alert>
          )}
        </Card>
      )}
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
