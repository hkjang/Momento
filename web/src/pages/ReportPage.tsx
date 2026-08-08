import { Box, Card, Chip, Stack, Typography } from "@mui/material";
import ReactECharts from "../components/Chart";
import { useQuery } from "@tanstack/react-query";
import { get, post, rangeQuery } from "../api/client";
import { useSite } from "../contexts/SiteContext";
import DataTable, { type Column } from "../components/DataTable";
import { ErrorState, Loading, NoSite } from "../components/States";
type Kind = "acquisition" | "pages" | "events" | "visitors";
const columns: Record<Exclude<Kind, "acquisition">, Column[]> = {
  pages: [
    { key: "page", label: "페이지" },
    { key: "title", label: "제목" },
    { key: "views", label: "조회", align: "right" },
    { key: "users", label: "사용자", align: "right" },
    { key: "sessions", label: "세션", align: "right" },
    { key: "conversions", label: "전환", align: "right" },
  ],
  events: [
    {
      key: "event",
      label: "이벤트",
      format: (v) => <Chip size="small" label={String(v)} variant="outlined" />,
    },
    { key: "count", label: "발생", align: "right" },
    { key: "users", label: "사용자", align: "right" },
    { key: "conversions", label: "전환", align: "right" },
    {
      key: "last_seen",
      label: "마지막 수집",
      format: (v) => new Date(String(v)).toLocaleString("ko-KR"),
    },
  ],
  visitors: [
    {
      key: "visitor_id",
      label: "Visitor ID",
      format: (v) => (
        <Typography variant="body2" className="mono">
          {String(v)}
        </Typography>
      ),
    },
    { key: "user_id", label: "User ID" },
    { key: "events", label: "이벤트", align: "right" },
    { key: "sessions", label: "세션", align: "right" },
    { key: "conversions", label: "전환", align: "right" },
    {
      key: "last_seen",
      label: "마지막 활동",
      format: (v) => new Date(String(v)).toLocaleString("ko-KR"),
    },
  ],
};
export default function ReportPage({ kind }: { kind: Kind }) {
  const { site } = useSite();
  const q = useQuery({
    queryKey: ["report", kind, site?.site_id],
    queryFn: async () => {
      if (kind === "acquisition")
        return (
          await post<{ rows: Record<string, unknown>[] }>("/api/v1/query", {
            site_id: site!.site_id,
            date_range: {
              from: new Date(Date.now() - 29 * 86400000)
                .toISOString()
                .slice(0, 10),
              to: new Date().toISOString().slice(0, 10),
            },
            dimensions: [
              "traffic.source",
              "traffic.medium",
              "traffic.campaign",
            ],
            metrics: ["users", "sessions", "page_views", "conversions"],
            filters: [],
            limit: 200,
          })
        ).rows;
      return get<Record<string, unknown>[]>(
        `/api/v1/sites/${site!.site_id}/${kind}?${rangeQuery()}`,
      );
    },
    enabled: !!site,
  });
  if (!site) return <NoSite />;
  if (q.isLoading) return <Loading />;
  if (q.error) return <ErrorState error={q.error} />;
  const rows = q.data || [];
  if (kind === "acquisition") {
    const cols: Column[] = [
      { key: "traffic.source", label: "소스" },
      { key: "traffic.medium", label: "매체" },
      { key: "traffic.campaign", label: "캠페인" },
      { key: "users", label: "사용자", align: "right" },
      { key: "sessions", label: "세션", align: "right" },
      { key: "page_views", label: "페이지뷰", align: "right" },
      { key: "conversions", label: "전환", align: "right" },
    ];
    return (
      <Stack spacing={2}>
        <Card sx={{ p: 2.5 }}>
          <Typography fontWeight={700}>상위 유입 소스</Typography>
          <ReactECharts
            style={{ height: 280 }}
            option={{
              tooltip: { trigger: "axis" },
              grid: { left: 15, right: 15, bottom: 20, containLabel: true },
              xAxis: {
                type: "category",
                data: rows
                  .slice(0, 10)
                  .map((x) => String(x["traffic.source"] || "(direct)")),
              },
              yAxis: {
                type: "value",
                splitLine: { lineStyle: { color: "#EEF1F5" } },
              },
              series: [
                {
                  type: "bar",
                  data: rows.slice(0, 10).map((x) => x.users),
                  itemStyle: { color: "#5B5CE2", borderRadius: [5, 5, 0, 0] },
                },
              ],
            }}
          />
        </Card>
        <DataTable columns={cols} rows={rows} />
      </Stack>
    );
  }
  return (
    <Stack spacing={2}>
      <Box sx={{ display: "flex", justifyContent: "flex-end" }}>
        <Chip label="최근 30일" variant="outlined" />
      </Box>
      <DataTable columns={columns[kind]} rows={rows} />
    </Stack>
  );
}
