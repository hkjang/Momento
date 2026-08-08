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
  Typography,
} from "@mui/material";
import PlayArrowRounded from "@mui/icons-material/PlayArrowRounded";
import { useMutation } from "@tanstack/react-query";
import { post } from "../api/client";
import { useSite } from "../contexts/SiteContext";
import DataTable from "../components/DataTable";
import { NoSite } from "../components/States";
const dimensions = [
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
  "revenue",
];
export default function ExplorerPage() {
  const { site } = useSite();
  const [dims, setDims] = useState<string[]>(["event.name"]);
  const [mets, setMets] = useState<string[]>(["events", "users"]);
  const mutation = useMutation({
    mutationFn: () =>
      post<{ rows: Record<string, unknown>[]; columns: string[] }>(
        "/api/v1/query",
        {
          site_id: site!.site_id,
          date_range: {
            from: new Date(Date.now() - 29 * 86400000)
              .toISOString()
              .slice(0, 10),
            to: new Date().toISOString().slice(0, 10),
          },
          dimensions: dims,
          metrics: mets,
          filters: [],
          limit: 200,
        },
      ),
  });
  if (!site) return <NoSite />;
  return (
    <Stack spacing={2}>
      <Card sx={{ p: 2.5 }}>
        <Typography fontWeight={700} mb={2}>
          Query Builder
        </Typography>
        <Box
          sx={{
            display: "grid",
            gridTemplateColumns: { xs: "1fr", md: "1fr 1fr auto" },
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
      {mutation.error && (
        <Alert severity="error">{mutation.error.message}</Alert>
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
