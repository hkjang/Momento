import {
  Alert,
  Box,
  Button,
  Card,
  CircularProgress,
  Stack,
  Typography,
} from "@mui/material";
import AddRounded from "@mui/icons-material/AddRounded";
import InboxOutlined from "@mui/icons-material/InboxOutlined";
import { useNavigate } from "react-router-dom";

export function Loading() {
  return (
    <Box sx={{ minHeight: 300, display: "grid", placeItems: "center" }}>
      <CircularProgress size={30} />
    </Box>
  );
}
export function ErrorState({ error }: { error: unknown }) {
  return (
    <Alert severity="error">
      {error instanceof Error ? error.message : "데이터를 불러오지 못했습니다."}
    </Alert>
  );
}
export function NoSite() {
  const navigate = useNavigate();
  return (
    <Card sx={{ p: 7, textAlign: "center" }}>
      <InboxOutlined sx={{ fontSize: 48, color: "#AAB3C2" }} />
      <Typography variant="h6" mt={2}>
        분석할 사이트가 없습니다
      </Typography>
      <Typography color="text.secondary" mb={3}>
        첫 사이트를 만들고 Tracking SDK를 설치해 보세요.
      </Typography>
      <Button
        variant="contained"
        startIcon={<AddRounded />}
        onClick={() => navigate("/admin")}
      >
        사이트 만들기
      </Button>
    </Card>
  );
}
export function Empty({
  title = "아직 데이터가 없습니다",
  description = "SDK에서 이벤트가 수집되면 이곳에 표시됩니다.",
}) {
  return (
    <Stack alignItems="center" sx={{ py: 7 }}>
      <InboxOutlined sx={{ fontSize: 42, color: "#B7BFCC" }} />
      <Typography fontWeight={700} mt={1.5}>
        {title}
      </Typography>
      <Typography color="text.secondary" variant="body2">
        {description}
      </Typography>
    </Stack>
  );
}
