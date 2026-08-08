import { useEffect, useState } from "react";
import {
  Alert,
  Box,
  Button,
  Card,
  Checkbox,
  Chip,
  FormControlLabel,
  IconButton,
  Stack,
  TextField,
  Typography,
} from "@mui/material";
import AddRounded from "@mui/icons-material/AddRounded";
import DeleteOutlineRounded from "@mui/icons-material/DeleteOutlineRounded";
import PlayArrowRounded from "@mui/icons-material/PlayArrowRounded";
import SaveRounded from "@mui/icons-material/SaveRounded";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import SegmentBuilder, {
  emptySegment,
  type SegmentNode,
} from "../components/SegmentBuilder";
import DataTable from "../components/DataTable";
import { dateRangeValues, del, get, post, put } from "../api/client";
import { useSite } from "../contexts/SiteContext";
import { useAuth } from "../contexts/AuthContext";
import { ErrorState, Loading, NoSite } from "../components/States";

interface Segment {
  id: string;
  name: string;
  description: string;
  definition: SegmentNode;
  shared: boolean;
  owner?: string;
  updated_at: string;
}

interface Dimension {
  query_name: string;
  active: boolean;
  scope: string;
}

const initialForm = () => ({
  name: "",
  description: "",
  shared: false,
  definition: emptySegment(),
});

export default function SegmentsPage() {
  const { site } = useSite();
  const { user } = useAuth();
  const qc = useQueryClient();
  const [selected, setSelected] = useState<string | null>(null);
  const [form, setForm] = useState(initialForm);
  const query = useQuery({
    queryKey: ["segments", site?.site_id],
    queryFn: () => get<Segment[]>(`/api/v1/segments?site_id=${site!.site_id}`),
    enabled: !!site,
  });
  const dimensions = useQuery({
    queryKey: ["dimensions", site?.site_id],
    queryFn: () =>
      get<Dimension[]>(`/api/v1/dimensions?site_id=${site!.site_id}`),
    enabled: !!site,
  });
  useEffect(() => {
    setSelected(null);
    setForm(initialForm());
  }, [site?.site_id]);
  const save = useMutation({
    mutationFn: () => {
      const body = { site_id: site!.site_id, ...form };
      return selected
        ? put(`/api/v1/segments/${selected}`, body)
        : post("/api/v1/segments", body);
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["segments", site?.site_id] });
      setSelected(null);
      setForm(initialForm());
    },
  });
  const remove = useMutation({
    mutationFn: (id: string) => del(`/api/v1/segments/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["segments", site?.site_id] });
      setSelected(null);
      setForm(initialForm());
    },
  });
  const preview = useMutation({
    mutationFn: () =>
      post<{ columns: string[]; rows: Record<string, unknown>[] }>(
        "/api/v1/query",
        {
          site_id: site!.site_id,
          date_range: dateRangeValues(30, site!.timezone),
          dimensions: ["event.name"],
          metrics: ["events", "users", "sessions"],
          filters: [],
          segment: form.definition,
          limit: 20,
        },
      ),
  });
  if (!site) return <NoSite />;
  if (query.isLoading) return <Loading />;
  if (query.error) return <ErrorState error={query.error} />;
  const canShare = !user || !["analyst", "viewer"].includes(user.role);
  const customFields = (dimensions.data || [])
    .filter((item) => item.active && item.scope !== "item")
    .map((item) => item.query_name);
  return (
    <Stack spacing={2}>
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: { xs: "1fr", xl: "340px 1fr" },
          gap: 2,
        }}
      >
        <Card sx={{ p: 2.5, height: "fit-content" }}>
          <Stack
            direction="row"
            justifyContent="space-between"
            alignItems="center"
            mb={2}
          >
            <Box>
              <Typography fontWeight={720}>저장된 Segment</Typography>
              <Typography variant="caption" color="text.secondary">
                개인 또는 Workspace 공유 조건
              </Typography>
            </Box>
            <IconButton
              color="primary"
              onClick={() => {
                setSelected(null);
                setForm(initialForm());
              }}
            >
              <AddRounded />
            </IconButton>
          </Stack>
          <Stack spacing={1}>
            {(query.data || []).map((segment) => (
              <Box
                key={segment.id}
                onClick={() => {
                  setSelected(segment.id);
                  setForm({
                    name: segment.name,
                    description: segment.description,
                    shared: segment.shared,
                    definition: segment.definition,
                  });
                }}
                sx={{
                  p: 1.5,
                  borderRadius: 2,
                  border: "1px solid",
                  borderColor:
                    selected === segment.id ? "primary.main" : "#E5E9F1",
                  bgcolor: selected === segment.id ? "#F4F4FF" : "white",
                  cursor: "pointer",
                }}
              >
                <Stack direction="row" alignItems="center" gap={1}>
                  <Typography variant="body2" fontWeight={680}>
                    {segment.name}
                  </Typography>
                  {segment.shared && (
                    <Chip
                      size="small"
                      label="공유"
                      color="primary"
                      variant="outlined"
                    />
                  )}
                </Stack>
                <Typography variant="caption" color="text.secondary">
                  {segment.description || segment.owner || "내 Segment"}
                </Typography>
              </Box>
            ))}
            {!query.data?.length && (
              <Typography variant="body2" color="text.secondary">
                저장된 Segment가 없습니다.
              </Typography>
            )}
          </Stack>
        </Card>
        <Card sx={{ p: 2.5 }}>
          <Stack direction={{ xs: "column", md: "row" }} spacing={1.5} mb={2}>
            <TextField
              size="small"
              label="Segment 이름"
              value={form.name}
              onChange={(event) =>
                setForm({ ...form, name: event.target.value })
              }
              sx={{ minWidth: 220 }}
            />
            <TextField
              size="small"
              label="설명"
              value={form.description}
              onChange={(event) =>
                setForm({ ...form, description: event.target.value })
              }
              sx={{ flex: 1 }}
            />
            {canShare && (
              <FormControlLabel
                control={
                  <Checkbox
                    checked={form.shared}
                    onChange={(event) =>
                      setForm({ ...form, shared: event.target.checked })
                    }
                  />
                }
                label="Workspace 공유"
              />
            )}
          </Stack>
          <SegmentBuilder
            value={form.definition}
            onChange={(definition) => setForm({ ...form, definition })}
            customFields={customFields}
          />
          {(save.error || preview.error || remove.error) && (
            <Alert severity="error" sx={{ mt: 2 }}>
              {(save.error || preview.error || remove.error)?.message}
            </Alert>
          )}
          <Stack direction="row" gap={1} mt={2}>
            <Button
              variant="contained"
              startIcon={<SaveRounded />}
              disabled={!form.name.trim() || save.isPending}
              onClick={() => save.mutate()}
            >
              {selected ? "변경 저장" : "Segment 저장"}
            </Button>
            <Button
              startIcon={<PlayArrowRounded />}
              disabled={preview.isPending}
              onClick={() => preview.mutate()}
            >
              30일 미리보기
            </Button>
            {selected && (
              <Button
                color="error"
                startIcon={<DeleteOutlineRounded />}
                onClick={() => remove.mutate(selected)}
              >
                삭제
              </Button>
            )}
          </Stack>
        </Card>
      </Box>
      {preview.data && (
        <DataTable
          columns={preview.data.columns.map((key) => ({
            key,
            label: key,
            align: key === "event.name" ? "left" : "right",
          }))}
          rows={preview.data.rows}
        />
      )}
    </Stack>
  );
}
