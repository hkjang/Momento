import {
  Alert,
  Box,
  Button,
  Card,
  Skeleton,
  Stack,
  Typography,
} from "@mui/material";
import AddRounded from "@mui/icons-material/AddRounded";
import InboxOutlined from "@mui/icons-material/InboxOutlined";
import RefreshRounded from "@mui/icons-material/RefreshRounded";
import { useNavigate } from "react-router-dom";
import type { ReactNode } from "react";

export function Loading({
  label = "데이터를 불러오는 중",
}: {
  label?: string;
}) {
  return (
    <Stack spacing={2} aria-label={label} aria-busy="true">
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: {
            xs: "1fr",
            sm: "repeat(2,1fr)",
            xl: "repeat(4,1fr)",
          },
          gap: 2,
        }}
      >
        {[0, 1, 2, 3].map((item) => (
          <Card key={item} sx={{ p: 2.5 }}>
            <Skeleton width="42%" />
            <Skeleton width="68%" height={44} sx={{ mt: 0.5 }} />
            <Skeleton width="52%" />
          </Card>
        ))}
      </Box>
      <Card sx={{ p: 2.5 }}>
        <Skeleton width="22%" />
        <Skeleton variant="rounded" height={230} sx={{ mt: 2 }} />
      </Card>
      <Typography variant="caption" color="text.secondary" textAlign="center">
        {label}
      </Typography>
    </Stack>
  );
}

export function ErrorState({
  error,
  retry,
}: {
  error: unknown;
  retry?: () => void;
}) {
  return (
    <Alert
      severity="error"
      action={
        retry ? (
          <Button
            color="inherit"
            size="small"
            startIcon={<RefreshRounded />}
            onClick={retry}
          >
            다시 시도
          </Button>
        ) : undefined
      }
    >
      <Typography fontWeight={700}>요청을 완료하지 못했습니다</Typography>
      <Typography variant="body2">
        {error instanceof Error
          ? error.message
          : "데이터를 불러오지 못했습니다."}
      </Typography>
    </Alert>
  );
}

export function NoSite() {
  const navigate = useNavigate();
  return (
    <Card
      sx={{ p: { xs: 4, md: 7 }, textAlign: "center", borderStyle: "dashed" }}
    >
      <Box className="empty-state-icon">
        <InboxOutlined />
      </Box>
      <Typography variant="h6" mt={2}>
        분석할 사이트가 없습니다
      </Typography>
      <Typography color="text.secondary" mb={3}>
        첫 사이트를 만들고 Tracking SDK를 설치해 보세요.
      </Typography>
      <Button
        variant="contained"
        startIcon={<AddRounded />}
        onClick={() => navigate("/admin?section=sites")}
      >
        사이트 만들기
      </Button>
    </Card>
  );
}

export function Empty({
  title = "아직 데이터가 없습니다",
  description = "SDK에서 이벤트가 수집되면 이곳에 표시됩니다.",
  action,
}: {
  title?: string;
  description?: string;
  action?: ReactNode;
}) {
  return (
    <Stack alignItems="center" textAlign="center" sx={{ px: 2, py: 7 }}>
      <Box className="empty-state-icon">
        <InboxOutlined />
      </Box>
      <Typography fontWeight={700} mt={1.5}>
        {title}
      </Typography>
      <Typography color="text.secondary" variant="body2" mt={0.5}>
        {description}
      </Typography>
      {action && <Box mt={2}>{action}</Box>}
    </Stack>
  );
}
