import {
  Button,
  Card,
  Chip,
  LinearProgress,
  Stack,
  Typography,
} from "@mui/material";
import RefreshRounded from "@mui/icons-material/RefreshRounded";
import PeriodField from "./PeriodField";
import type { DateRange } from "./periodParam";
import { allowedRanges } from "./queryError";
import { useDelayedBusy } from "./useDelayedBusy";

export default function AnalysisToolbar({
  days,
  setDays,
  environment,
  timezone,
  updatedAt,
  refreshing,
  refresh,
  comparePrevious = false,
  maxExactDays,
  range,
  setRange,
}: {
  days: number;
  setDays(days: number): void;
  /** A custom period, when the screen offers one (usePeriodParam). */
  range?: DateRange | null;
  setRange?(range: DateRange | null): void;
  environment: string;
  timezone: string;
  updatedAt: number;
  refreshing: boolean;
  refresh(): void;
  comparePrevious?: boolean;
  /**
   * The site's policy limit. This control offered 7, 30 and 90 days whatever the
   * site allowed, and the server's own comment said the console "builds its
   * period options from this, so it never offers a range the site's policy will
   * refuse" — which was true of RangeSelect and not of this, the control the
   * overview and the visitor insight screens use. On a site limited to 14 days
   * the dropdown offered two periods that answer RANGE_EXCEEDS_POLICY.
   */
  maxExactDays?: number;
}) {
  // The screen keeps the previous numbers while the next ones load, so without
  // this the only sign anything was happening was a disabled button.
  const busy = useDelayedBusy(refreshing);
  const available = allowedRanges([7, 30, 90], maxExactDays);
  return (
    <Card variant="outlined" sx={{ px: 2, py: 1.4, position: "relative" }}>
      {busy && (
        <LinearProgress
          aria-label="새 기간을 불러오는 중"
          sx={{ position: "absolute", top: 0, left: 0, right: 0, height: 2 }}
        />
      )}
      <Stack
        direction={{ xs: "column", sm: "row" }}
        alignItems={{ sm: "center" }}
        spacing={1.25}
      >
        <PeriodField
          days={days}
          setDays={setDays}
          options={available}
          range={range}
          setRange={setRange}
          timezone={timezone}
          maxExactDays={maxExactDays}
        />
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
          {busy ? (
            <Typography variant="caption" color="text.secondary" noWrap>
              불러오는 중
            </Typography>
          ) : (
            updatedAt > 0 && (
              <Typography variant="caption" color="text.secondary" noWrap>
                {new Date(updatedAt).toLocaleTimeString("ko-KR", {
                  hour: "2-digit",
                  minute: "2-digit",
                })}
                에 갱신
              </Typography>
            )
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
