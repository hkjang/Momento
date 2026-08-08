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
  "sessions",
  "page_views",
  "conversions",
  "conversion_users",
  "conversion_sessions",
  "user_conversion_rate",
  "session_conversion_rate",
  "revenue",
];
export default function ExplorerPage() {
  const { site } = useSite();
  const qc = useQueryClient();
  const [dims, setDims] = useState<string[]>(["event.name"]);
  const [mets, setMets] = useState<string[]>(["events", "users"]);
  const [segmentId, setSegmentId] = useState("");
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
      post<{ rows: Record<string, unknown>[]; columns: string[] }>(
        "/api/v1/query",
        {
          site_id: site!.site_id,
          date_range: dateRangeValues(30, site!.timezone),
          dimensions: dims,
          metrics: mets,
          filters: [],
          segment_id: segmentId || undefined,
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
            gridTemplateColumns: { xs: "1fr", md: "1fr 1fr 220px auto" },
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
                  {x}
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
      {(mutation.error || saveReport.error || deleteReport.error) && (
        <Alert severity="error">
          {(mutation.error || saveReport.error || deleteReport.error)?.message}
        </Alert>
      )}
      {mutation.data ? (
        <DataTable
          columns={mutation.data.columns.map((key) => ({
            key,
            label: key,
            align: mets.includes(key) ? ("right" as const) : ("left" as const),
          }))}
          rows={mutation.data.rows}
        />
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
