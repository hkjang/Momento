import {
  Button,
  Card,
  Chip,
  MenuItem,
  Stack,
  TextField,
  Typography,
} from "@mui/material";
import RefreshRounded from "@mui/icons-material/RefreshRounded";

export default function AnalysisToolbar({
  days,
  setDays,
  environment,
  timezone,
  updatedAt,
  refreshing,
  refresh,
  comparePrevious = false,
}: {
  days: number;
  setDays(days: number): void;
  environment: string;
  timezone: string;
  updatedAt: number;
  refreshing: boolean;
  refresh(): void;
  comparePrevious?: boolean;
}) {
  return (
    <Card variant="outlined" sx={{ px: 2, py: 1.4 }}>
      <Stack
        direction={{ xs: "column", sm: "row" }}
        alignItems={{ sm: "center" }}
        spacing={1.25}
      >
        <TextField
          select
          size="small"
          label="분석 기간"
          value={days}
          onChange={(event) => setDays(Number(event.target.value))}
          sx={{ minWidth: 140 }}
        >
          <MenuItem value={7}>최근 7일</MenuItem>
          <MenuItem value={30}>최근 30일</MenuItem>
          <MenuItem value={90}>최근 90일</MenuItem>
        </TextField>
        <Chip
          size="small"
          label={environment.toUpperCase()}
          color={environment === "prd" ? "primary" : "default"}
          variant="outlined"
        />
        <Typography variant="caption" color="text.secondary">
          {timezone} 기준
          {comparePrevious ? " · 이전 동일 기간과 비교" : " · Raw Event 기준"}
        </Typography>
        <Stack
          direction="row"
          alignItems="center"
          spacing={1}
          sx={{ ml: { sm: "auto!important" } }}
        >
          {updatedAt > 0 && (
            <Typography variant="caption" color="text.secondary" noWrap>
              {new Date(updatedAt).toLocaleTimeString("ko-KR", {
                hour: "2-digit",
                minute: "2-digit",
              })}
              에 갱신
            </Typography>
          )}
          <Button
            variant="outlined"
            size="small"
            startIcon={<RefreshRounded />}
            disabled={refreshing}
            onClick={refresh}
          >
            새로고침
          </Button>
        </Stack>
      </Stack>
    </Card>
  );
}
