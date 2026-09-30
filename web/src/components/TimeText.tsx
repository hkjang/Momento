import { Tooltip, Typography } from "@mui/material";
import { absoluteTime, relativeTime } from "./relativeTime";

// 최근 시각은 「3분 전」, 오래된 시각은 날짜로 보이고, 올리면 정확한 시각이 보인다.
export default function TimeText({
  value,
  empty = "—",
}: {
  value: unknown;
  empty?: string;
}) {
  if (value == null || value === "") return <>{empty}</>;
  const absolute = absoluteTime(value);
  const relative = relativeTime(value, Date.now());
  return (
    <Tooltip title={absolute}>
      <Typography component="time" variant="body2" noWrap dateTime={String(value)}>
        {relative ?? absolute}
      </Typography>
    </Tooltip>
  );
}
