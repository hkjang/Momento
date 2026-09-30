import { Stack, Typography } from "@mui/material";
import PeriodField from "./PeriodField";
import type { DateRange } from "./periodParam";
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
  range,
  setRange,
}: {
  days: number;
  setDays(days: number): void;
  /** A custom period, when the screen offers one (usePeriodParam). */
  range?: DateRange | null;
  setRange?(range: DateRange | null): void;
  options?: number[];
  timezone?: string;
  note?: string;
  /** The site's policy limit, so a refused period is never offered. */
  maxExactDays?: number;
}) {
  const available = allowedRanges(options, maxExactDays);
  return (
    <Stack direction="row" alignItems="center" gap={1.5} flexWrap="wrap">
      <PeriodField
        days={days}
        setDays={setDays}
        options={available}
        range={range}
        setRange={setRange}
        timezone={timezone}
        maxExactDays={maxExactDays}
      />
      {(timezone || note) && (
        <Typography variant="caption" color="text.secondary">
          {[timezone ? `${timezone} 기준` : "", note].filter(Boolean).join(" · ")}
        </Typography>
      )}
    </Stack>
  );
}
