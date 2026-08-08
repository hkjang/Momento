import { Box, Card, Stack, Tab, Tabs, Typography } from "@mui/material";
import ReactECharts from "../components/Chart";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { get, rangeQuery } from "../api/client";
import { useSite } from "../contexts/SiteContext";
import DataTable from "../components/DataTable";
import { ErrorState, Loading, NoSite } from "../components/States";
type Row = { label: string; events: number; users: number; sessions: number };
type Usage = Record<
  | "networks"
  | "departments"
  | "organizations"
  | "services"
  | "features"
  | "buttons",
  Row[]
>;
const tabs = [
  { k: "departments", l: "부서" },
  { k: "organizations", l: "조직" },
  { k: "services", l: "서비스" },
  { k: "features", l: "기능" },
  { k: "buttons", l: "버튼" },
  { k: "networks", l: "네트워크 망" },
] as const;
export default function UsagePage() {
  const { site } = useSite();
  const [tab, setTab] = useState(0);
  const q = useQuery({
    queryKey: ["usage", site?.site_id, site?.timezone],
    queryFn: () =>
      get<Usage>(
        `/api/v1/sites/${site!.site_id}/usage?${rangeQuery(30, site!.timezone)}`,
      ),
    enabled: !!site,
  });
  if (!site) return <NoSite />;
  if (q.isLoading) return <Loading />;
  if (q.error) return <ErrorState error={q.error} />;
  const key = tabs[tab].k;
  const rows = q.data![key];
  const total = rows.reduce((n, r) => n + r.events, 0);
  return (
    <Stack spacing={2}>
      <Card sx={{ px: 1 }}>
        <Tabs
          value={tab}
          onChange={(_, v) => setTab(v)}
          variant="scrollable"
          scrollButtons="auto"
        >
          {tabs.map((t) => (
            <Tab key={t.k} label={t.l} />
          ))}
        </Tabs>
      </Card>
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: { xs: "1fr", lg: "1.2fr 1fr" },
          gap: 2,
        }}
      >
        <Card sx={{ p: 2.5 }}>
          <Typography fontWeight={700}>{tabs[tab].l}별 이벤트 분포</Typography>
          <Typography variant="caption" color="text.secondary">
            최근 30일 · 총 {Intl.NumberFormat("ko-KR").format(total)}건
          </Typography>
          <ReactECharts
            style={{ height: 420 }}
            option={{
              tooltip: { trigger: "axis", axisPointer: { type: "shadow" } },
              grid: {
                left: 15,
                right: 25,
                top: 25,
                bottom: 10,
                containLabel: true,
              },
              xAxis: {
                type: "value",
                splitLine: { lineStyle: { color: "#EEF1F5" } },
              },
              yAxis: {
                type: "category",
                inverse: true,
                data: rows.slice(0, 12).map((x) => x.label),
                axisLabel: { width: 150, overflow: "truncate" },
              },
              series: [
                {
                  type: "bar",
                  data: rows.slice(0, 12).map((x, i) => ({
                    value: x.events,
                    itemStyle: {
                      color: i === 0 ? "#5B5CE2" : "#9A9BF2",
                      borderRadius: [0, 5, 5, 0],
                    },
                  })),
                  barWidth: 18,
                },
              ],
            }}
          />
        </Card>
        <Box>
          <DataTable
            columns={[
              { key: "label", label: tabs[tab].l },
              { key: "events", label: "이벤트", align: "right" },
              { key: "users", label: "사용자", align: "right" },
              { key: "sessions", label: "세션", align: "right" },
            ]}
            rows={rows}
          />
        </Box>
      </Box>
      <Card sx={{ p: 2.2, bgcolor: "#F0F5FF", borderColor: "#D8E3FF" }}>
        <Typography fontWeight={700} color="#304A80">
          분석 팁
        </Typography>
        <Typography variant="body2" color="#536B99" mt={0.5}>
          SDK의 identify 속성에 <span className="mono">department</span>와{" "}
          <span className="mono">organization</span>을, 이벤트 속성에{" "}
          <span className="mono">service</span>·
          <span className="mono">feature</span>·
          <span className="mono">button</span>을 지정하면 사내 사용 현황이
          자동으로 분류됩니다.
        </Typography>
      </Card>
    </Stack>
  );
}
