import { useState } from "react";
import { Button, Card, Chip, Stack, Typography } from "@mui/material";
import { Link as RouterLink } from "react-router-dom";
import ReactECharts from "../components/Chart";
import { useQuery } from "@tanstack/react-query";
import { dateRangeValues, get, post, rangeQuery } from "../api/client";
import { useSite } from "../contexts/SiteContext";
import DataTable, { type Column } from "../components/DataTable";
import { ErrorState, Loading, NoSite } from "../components/States";
import AnalysisToolbar from "../components/AnalysisToolbar";
type Kind = "acquisition" | "pages" | "events" | "visitors" | "sessions";
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
  sessions: [
    {
      key: "session_id",
      label: "Session",
      format: (v) => (
        <Typography variant="body2" className="mono">
          {String(v).slice(0, 12)}
        </Typography>
      ),
    },
    { key: "user_id", label: "User ID", format: (v) => String(v || "익명") },
    {
      key: "started_at",
      label: "시작",
      format: (v) => new Date(String(v)).toLocaleString("ko-KR"),
    },
    {
      key: "duration_seconds",
      label: "체류(초)",
      align: "right",
      format: (v) => Number(v).toFixed(0),
    },
    { key: "page_views", label: "페이지뷰", align: "right" },
    { key: "events", label: "이벤트", align: "right" },
    { key: "interaction_count", label: "상호작용", align: "right" },
    {
      key: "engaged",
      label: "참여",
      format: (v) => (
        <Chip
          size="small"
          color={v ? "success" : "default"}
          label={v ? "참여" : "이탈"}
        />
      ),
    },
    { key: "conversions", label: "전환", align: "right" },
    { key: "landing_page", label: "시작 페이지", format: (v) => String(v || "—") },
    { key: "exit_page", label: "종료 페이지", format: (v) => String(v || "—") },
    { key: "source", label: "소스", format: (v) => String(v || "direct") },
    { key: "device_type", label: "기기", format: (v) => String(v || "—") },
    {
      key: "visitor_id",
      label: "",
      align: "right",
      format: (v) => (
        <Button
          size="small"
          component={RouterLink}
          to={`/user-explorer?visitor=${encodeURIComponent(String(v))}`}
        >
          추적
        </Button>
      ),
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
    {
      key: "visitor_id",
      label: "",
      align: "right",
      format: (v) => (
        <Button
          size="small"
          component={RouterLink}
          to={`/user-explorer?visitor=${encodeURIComponent(String(v))}`}
        >
          추적
        </Button>
      ),
    },
  ],
};
export default function ReportPage({ kind }: { kind: Kind }) {
  const { site, environment } = useSite();
  const [days, setDays] = useState(30);
  const q = useQuery({
    queryKey: [
      "report",
      kind,
      site?.site_id,
      site?.timezone,
      environment,
      days,
    ],
    queryFn: async () => {
      if (kind === "acquisition")
        return (
          await post<{ rows: Record<string, unknown>[] }>("/api/v1/query", {
            site_id: site!.site_id,
            environment,
            date_range: dateRangeValues(days, site!.timezone),
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
        `/api/v1/sites/${site!.site_id}/${kind}?${rangeQuery(days, site!.timezone)}`,
      );
    },
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
        {toolbar}
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
        <DataTable
          title="유입 소스 상세"
          description={`${days}일 동안 사용자를 유입시킨 Source · Medium · Campaign입니다.`}
          columns={cols}
          rows={rows}
          searchable
          exportFilename={`momento-acquisition-${days}d.csv`}
        />
      </Stack>
    );
  }
  const metadata = {
    pages: {
      title: "페이지 성과 상세",
      description: "조회·사용자·세션과 전환을 페이지별로 비교합니다.",
    },
    events: {
      title: "이벤트 상세",
      description: "업무 행동별 발생량, 사용자와 마지막 수집 시각입니다.",
    },
    visitors: {
      title: "사용자 활동 상세",
      description:
        "브라우저(Visitor) 단위 목록입니다. 한 사람이 데스크톱과 모바일을 쓰면 두 줄로 나타나므로, 첫 화면의 사용자 수보다 줄 수가 많을 수 있습니다. 사람 단위로 보려면 사용자 탐색기를 사용하세요.",
    },
    sessions: {
      title: "세션 상세",
      description:
        "최근 500개 세션의 체류·참여·전환과 시작·종료 페이지입니다. 참여 기준은 사이트 설정을 따릅니다.",
    },
  }[kind];
  return (
    <Stack spacing={2}>
      {toolbar}
      <DataTable
        title={metadata.title}
        description={metadata.description}
        columns={columns[kind]}
        rows={rows}
        searchable
        exportFilename={`momento-${kind}-${days}d.csv`}
      />
    </Stack>
  );
}
