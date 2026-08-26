import { useState } from "react";
import {
  Alert,
  Box,
  Button,
  Card,
  Chip,
  FormControl,
  InputLabel,
  MenuItem,
  OutlinedInput,
  Select,
  Stack,
  TextField,
  Typography,
} from "@mui/material";
import PlayArrowRounded from "@mui/icons-material/PlayArrowRounded";
import SaveRounded from "@mui/icons-material/SaveRounded";
import DeleteOutlineRounded from "@mui/icons-material/DeleteOutlineRounded";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { dateRangeValues, del, get, post, put } from "../api/client";
import { useSite } from "../contexts/SiteContext";
import { metricMeaning } from "./signalGuide";
import DataTable from "../components/DataTable";
import { NoSite } from "../components/States";
const baseDimensions = [
  "event.name",
  "page.url",
  "device.type",
  "browser",
  "os",
  "country",
  "traffic.source",
  "traffic.medium",
  "traffic.campaign",
  "network",
  "user.department",
  "user.organization",
  "feature",
  "button",
];
const metrics = [
  "events",
  "users",
  // sessions counts the sessions active in the range; sessions_started counts the
  // ones that began in it, which is the number the first screen reports.
  "sessions",
  "sessions_started",
  "page_views",
  "conversions",
  "conversion_users",
  "conversion_sessions",
  "user_conversion_rate",
  "session_conversion_rate",
  "revenue",
];
/**
 * RawEventExport exposes the raw event export endpoint. Owning the raw data is a
 * core promise of an on-premise deployment, so the console needs a way out that
 * is not limited to the aggregated table above.
 */
function RawEventExport() {
  const { site, environment } = useSite();
  const [days, setDays] = useState(7);
  const [format, setFormat] = useState<"csv" | "json">("csv");
  if (!site) return null;
  const { from, to } = dateRangeValues(days, site.timezone);
  const href = `/api/v1/sites/${site.site_id}/export?from=${from}&to=${to}&environment=${encodeURIComponent(environment)}&format=${format}`;
  return (
    <Card sx={{ p: 2.5 }}>
      <Typography variant="h6">Raw Event Export</Typography>
      <Typography variant="body2" color="text.secondary" mb={2}>
        선택한 기간과 환경의 Raw Event를 최대 100,000건까지 내려받습니다. 내보낸
        파일에는 원본 Property가 포함되므로 사내 보안 정책에 따라 취급하십시오.
      </Typography>
      <Stack direction={{ xs: "column", md: "row" }} spacing={1.5} alignItems={{ md: "center" }}>
        <TextField
          select
          label="기간"
          value={days}
          onChange={(event) => setDays(Number(event.target.value))}
          sx={{ minWidth: 140 }}
        >
          {[1, 7, 30, 90].map((value) => (
            <MenuItem key={value} value={value}>
              최근 {value}일
            </MenuItem>
          ))}
        </TextField>
        <TextField
          select
          label="형식"
          value={format}
          onChange={(event) => setFormat(event.target.value as "csv" | "json")}
          sx={{ minWidth: 140 }}
        >
          <MenuItem value="csv">CSV</MenuItem>
          <MenuItem value="json">NDJSON</MenuItem>
        </TextField>
        <Typography variant="body2" color="text.secondary">
          {from} ~ {to} · {environment.toUpperCase()}
        </Typography>
        <Box flexGrow={1} />
        <Button variant="outlined" component="a" href={href} download>
          내보내기
        </Button>
      </Stack>
    </Card>
  );
}

export default function ExplorerPage() {
  const { site, environment } = useSite();
  const qc = useQueryClient();
  const [dims, setDims] = useState<string[]>(["event.name"]);
  const [mets, setMets] = useState<string[]>(["events", "users"]);
  const [segmentId, setSegmentId] = useState("");
	const [queryMode, setQueryMode] = useState<"exact" | "fast" | "preview">("exact");
  const [reportId, setReportId] = useState("");
  const [reportName, setReportName] = useState("");
  const segments = useQuery({
    queryKey: ["segments", site?.site_id],
    queryFn: () =>
      get<{ id: string; name: string }[]>(
        `/api/v1/segments?site_id=${site!.site_id}`,
      ),
    enabled: !!site,
  });
  const customDimensions = useQuery({
    queryKey: ["dimensions", site?.site_id],
    queryFn: () =>
      get<{ query_name: string; active: boolean; scope: string }[]>(
        `/api/v1/dimensions?site_id=${site!.site_id}`,
      ),
    enabled: !!site,
  });
  const reports = useQuery({
    queryKey: ["saved-reports", site?.site_id, "exploration"],
    queryFn: () =>
      get<
        {
          id: string;
          name: string;
          definition: {
            dimensions?: string[];
            metrics?: string[];
            segment_id?: string;
          };
        }[]
      >(`/api/v1/reports?site_id=${site!.site_id}&kind=exploration`),
    enabled: !!site,
  });
  const mutation = useMutation({
    mutationFn: () =>
		post<{ rows: Record<string, unknown>[]; columns: string[]; query: { mode: string; complexity_score: number; sample_percent: number; execution: string; exact: boolean; estimated_error_percent?: number } }>(
        "/api/v1/query",
        {
          site_id: site!.site_id,
          environment,
          date_range: dateRangeValues(30, site!.timezone),
          dimensions: dims,
          metrics: mets,
          filters: [],
          segment_id: segmentId || undefined,
					mode: queryMode,
          limit: 200,
        },
      ),
  });
  const saveReport = useMutation({
    mutationFn: () => {
      const body = {
        site_id: site!.site_id,
        kind: "exploration",
        name: reportName,
        description: "Query Builder에서 저장",
        definition: {
          dimensions: dims,
          metrics: mets,
          segment_id: segmentId || undefined,
        },
        shared: false,
      };
      return reportId
        ? put(`/api/v1/reports/${reportId}`, body)
        : post("/api/v1/reports", body);
    },
    onSuccess: async () => {
      setReportName("");
      setReportId("");
      await qc.invalidateQueries({
        queryKey: ["saved-reports", site?.site_id, "exploration"],
      });
    },
  });
  const deleteReport = useMutation({
    mutationFn: () => del(`/api/v1/reports/${reportId}`),
    onSuccess: async () => {
      setReportId("");
      await qc.invalidateQueries({
        queryKey: ["saved-reports", site?.site_id, "exploration"],
      });
    },
  });
  if (!site) return <NoSite />;
  const dimensions = [
    ...baseDimensions,
    ...(customDimensions.data || [])
      .filter((item) => item.active && item.scope !== "item")
      .map((item) => item.query_name),
  ];
  return (
    <Stack spacing={2}>
      <Card sx={{ p: 2.5 }}>
        <Typography fontWeight={700} mb={2}>
          저장된 Exploration
        </Typography>
        <Box
          sx={{
            display: "grid",
            gridTemplateColumns: { xs: "1fr", md: "1fr 1fr auto auto" },
            gap: 1.5,
          }}
        >
          <TextField
            select
            size="small"
            label="불러오기"
            value={reportId}
            onChange={(event) => {
              const id = event.target.value;
              setReportId(id);
              const report = reports.data?.find((item) => item.id === id);
              if (report) {
                setReportName(report.name);
                setDims(report.definition.dimensions || ["event.name"]);
                setMets(report.definition.metrics || ["events"]);
                setSegmentId(report.definition.segment_id || "");
              }
            }}
          >
            <MenuItem value="">선택 안 함</MenuItem>
            {(reports.data || []).map((report) => (
              <MenuItem key={report.id} value={report.id}>
                {report.name}
              </MenuItem>
            ))}
          </TextField>
          <TextField
            size="small"
            label={reportId ? "Exploration 이름" : "새 Exploration 이름"}
            value={reportName}
            onChange={(event) => setReportName(event.target.value)}
          />
          <Button
            startIcon={<SaveRounded />}
            disabled={!reportName.trim() || saveReport.isPending}
            onClick={() => saveReport.mutate()}
          >
            {reportId ? "변경 저장" : "저장"}
          </Button>
          <Button
            color="error"
            startIcon={<DeleteOutlineRounded />}
            disabled={!reportId || deleteReport.isPending}
            onClick={() => deleteReport.mutate()}
          >
            삭제
          </Button>
        </Box>
      </Card>
      <Card sx={{ p: 2.5 }}>
        <Typography fontWeight={700} mb={2}>
          Query Builder
        </Typography>
        <Box
          sx={{
            display: "grid",
			gridTemplateColumns: { xs: "1fr", md: "1fr 1fr 220px 140px auto" },
            gap: 2,
            alignItems: "center",
          }}
        >
          <FormControl size="small">
            <InputLabel>Dimensions</InputLabel>
            <Select
              multiple
              value={dims}
              onChange={(e) =>
                setDims(
                  typeof e.target.value === "string"
                    ? e.target.value.split(",")
                    : e.target.value,
                )
              }
              input={<OutlinedInput label="Dimensions" />}
              renderValue={(v) => (
                <Stack direction="row" gap={0.5} flexWrap="wrap">
                  {v.map((x) => (
                    <Chip key={x} size="small" label={x} />
                  ))}
                </Stack>
              )}
            >
              {dimensions.map((x) => (
                <MenuItem key={x} value={x}>
                  {x}
                </MenuItem>
              ))}
            </Select>
          </FormControl>
			<TextField select size="small" label="Query Mode" value={queryMode} onChange={(event) => setQueryMode(event.target.value as typeof queryMode)}>
				<MenuItem value="exact">Exact · 100%</MenuItem>
				<MenuItem value="fast">Fast · 10%</MenuItem>
				<MenuItem value="preview">Preview · 1%</MenuItem>
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
          <FormControl size="small">
            <InputLabel>Metrics</InputLabel>
            <Select
              multiple
              value={mets}
              onChange={(e) =>
                setMets(
                  typeof e.target.value === "string"
                    ? e.target.value.split(",")
                    : e.target.value,
                )
              }
              input={<OutlinedInput label="Metrics" />}
              renderValue={(v) => (
                <Stack direction="row" gap={0.5} flexWrap="wrap">
                  {v.map((x) => (
                    <Chip key={x} size="small" label={x} />
                  ))}
                </Stack>
              )}
            >
              {metrics.map((x) => (
                <MenuItem key={x} value={x}>
                  <Stack>
                    <Typography variant="body2">{x}</Typography>
                    {metricMeaning[x] && (
                      <Typography variant="caption" color="text.secondary" sx={{ whiteSpace: "normal" }}>
                        {metricMeaning[x]}
                      </Typography>
                    )}
                  </Stack>
                </MenuItem>
              ))}
            </Select>
          </FormControl>
          <Button
            variant="contained"
            startIcon={<PlayArrowRounded />}
            disabled={!mets.length || mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            실행
          </Button>
        </Box>
      </Card>
      <RawEventExport />
      {(mutation.error || saveReport.error || deleteReport.error) && (
        <Alert severity="error">
          {(mutation.error || saveReport.error || deleteReport.error)?.message}
        </Alert>
      )}
      {mutation.data ? (
		<Stack spacing={1.5}>
			<Alert severity={mutation.data.query.exact ? "success" : "warning"}>
				{mutation.data.query.exact ? "전체 Raw Event를 정확 계산했습니다." : `${mutation.data.query.sample_percent}% 결정적 표본 결과입니다.`}
				{" · "}Complexity {mutation.data.query.complexity_score} · {mutation.data.query.execution}
				{mutation.data.query.estimated_error_percent ? ` · 추정 오차 ±${mutation.data.query.estimated_error_percent}%` : ""}
			</Alert>
			<DataTable
          columns={mutation.data.columns.map((key) => ({
            key,
            label: key,
            align: mets.includes(key) ? ("right" as const) : ("left" as const),
          }))}
          rows={mutation.data.rows}
        />
		</Stack>
      ) : (
        <Card sx={{ p: 7, textAlign: "center" }}>
          <Typography color="text.secondary">
            Dimension과 Metric을 선택한 후 실행하세요.
          </Typography>
        </Card>
      )}
    </Stack>
  );
}
