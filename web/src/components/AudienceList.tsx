import { useState } from "react";
import { Box, Button, Chip, Snackbar, Stack, Tooltip, Typography } from "@mui/material";
import { useMutation } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { post } from "../api/client";
import type { InsightAudience } from "../pages/visitorInsights";

/**
 * AudienceList turns a finding into a saved audience. A report that names a
 * group but makes the reader retype its definition into the segment builder
 * loses the group somewhere between the two screens, so the definition the
 * server counted is the definition that gets saved.
 */
export default function AudienceList({
  audiences,
  siteId,
  source,
  title = "실행 대상",
}: {
  audiences: InsightAudience[];
  siteId: string;
  source: string;
  title?: string;
}) {
  const navigate = useNavigate();
  const [toast, setToast] = useState("");
  const save = useMutation({
    mutationFn: (audience: InsightAudience) =>
      post<{ id: string }>("/api/v1/segments", {
        site_id: siteId,
        name: `${audience.label} (자동 생성)`,
        description: `${source} · ${audience.action}`,
        definition: audience.segment,
        shared: false,
      }),
    onSuccess: () => {
      setToast("Segment를 저장했습니다. Segment 화면으로 이동합니다.");
      window.setTimeout(() => navigate("/segments"), 900);
    },
    onError: (error: Error) => setToast(`Segment 저장 실패: ${error.message}`),
  });
  if (!audiences.length) return null;
  return (
    <Box>
      <Typography fontWeight={700} mb={1}>
        {title}
      </Typography>
      <Stack spacing={1}>
        {audiences.map((audience) => (
          <Stack
            key={audience.key}
            direction="row"
            justifyContent="space-between"
            alignItems="center"
            gap={1}
          >
            <Box>
              <Typography variant="body2" fontWeight={600}>
                {audience.label}
              </Typography>
              <Typography variant="caption" color="text.secondary">
                {audience.action}
              </Typography>
            </Box>
            <Stack direction="row" gap={0.5} alignItems="center">
              <Chip
                size="small"
                color={audience.users > 0 ? "primary" : "default"}
                label={`${audience.users.toLocaleString("ko-KR")}명`}
              />
              {audience.segment && audience.users > 0 && (
                <Tooltip
                  title={
                    audience.segment_note ||
                    "이 조건으로 Segment를 저장해 Query·Funnel·Action에서 재사용합니다."
                  }
                >
                  <Button
                    size="small"
                    disabled={save.isPending}
                    onClick={() => save.mutate(audience)}
                  >
                    Segment 만들기
                  </Button>
                </Tooltip>
              )}
            </Stack>
          </Stack>
        ))}
      </Stack>
      <Snackbar
        open={!!toast}
        message={toast}
        autoHideDuration={4000}
        onClose={() => setToast("")}
      />
    </Box>
  );
}
