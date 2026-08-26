import { MenuItem, Stack, TextField, Typography } from "@mui/material";
import { allowedRanges } from "./queryError";

/**
 * RangeSelect gives a screen a period without the full analysis toolbar, which
 * also wants refresh plumbing and a last-updated stamp.
 *
 * Most analytical screens had no period control at all: the range was written
 * into the request and could not be changed. That is a gap on its own — asking
 * "what happened this week" was impossible on the frustration screen — and it
 * was worse on the heaviest screens, because the advice given when a query runs
 * out of time starts with narrowing the range.
 */
export default function RangeSelect({
  days,
  setDays,
  options = [7, 30, 90],
  timezone,
  note,
  maxExactDays,
}: {
  days: number;
  setDays(days: number): void;
  options?: number[];
  timezone?: string;
  note?: string;
  /** The site's policy limit, so a refused period is never offered. */
  maxExactDays?: number;
}) {
  const available = allowedRanges(options, maxExactDays);
  return (
    <Stack direction="row" alignItems="center" gap={1.5} flexWrap="wrap">
      <TextField
        select
        size="small"
        label="분석 기간"
        value={days}
        onChange={(event) => setDays(Number(event.target.value))}
        sx={{ minWidth: 140 }}
      >
        {available.map((option) => (
          <MenuItem key={option} value={option}>
            {`최근 ${option}일`}
          </MenuItem>
        ))}
      </TextField>
      {(timezone || note) && (
        <Typography variant="caption" color="text.secondary">
          {[timezone ? `${timezone} 기준` : "", note].filter(Boolean).join(" · ")}
        </Typography>
      )}
    </Stack>
  );
}
