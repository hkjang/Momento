import { Box, Card, Chip, Stack, Typography } from "@mui/material";
import NorthEastRounded from "@mui/icons-material/NorthEastRounded";
import SouthEastRounded from "@mui/icons-material/SouthEastRounded";
import RemoveRounded from "@mui/icons-material/RemoveRounded";
import type { ReactNode } from "react";

export function formatMetric(
  value: number,
  type?: "percent" | "duration" | "currency",
) {
  if (type === "percent") return `${value.toFixed(1)}%`;
  if (type === "duration") {
    const m = Math.floor(value / 60),
      s = Math.round(value % 60);
    return `${m}m ${s}s`;
  }
  if (type === "currency")
    return new Intl.NumberFormat("ko-KR", {
      style: "currency",
      currency: "KRW",
      maximumFractionDigits: 0,
    }).format(value);
  return Intl.NumberFormat("ko-KR", {
    notation: value >= 100000 ? "compact" : "standard",
    maximumFractionDigits: 1,
  }).format(value);
}
export default function MetricCard({
  label,
  value,
  previous,
  type,
  icon,
}: {
  label: string;
  value: number;
  previous?: number;
  type?: "percent" | "duration" | "currency";
  icon?: ReactNode;
}) {
  const change =
    previous && previous !== 0
      ? ((value - previous) / Math.abs(previous)) * 100
      : 0;
  const positive = change > 0.05;
  const negative = change < -0.05;
  return (
    <Card sx={{ p: 2.3, minWidth: 0 }}>
      <Stack
        direction="row"
        justifyContent="space-between"
        alignItems="flex-start"
      >
        <Box>
          <Typography variant="body2" color="text.secondary" fontWeight={580}>
            {label}
          </Typography>
          <Typography
            sx={{
              fontSize: 26,
              fontWeight: 750,
              mt: 0.8,
              letterSpacing: "-.035em",
            }}
          >
            {formatMetric(value, type)}
          </Typography>
        </Box>
        {icon && (
          <Box
            sx={{
              p: 1,
              borderRadius: 2.5,
              bgcolor: "#F0F0FF",
              color: "primary.main",
              display: "flex",
            }}
          >
            {icon}
          </Box>
        )}
      </Stack>
      {previous !== undefined && (
        <Stack direction="row" alignItems="center" gap={0.7} mt={1.5}>
          <Chip
            size="small"
            icon={
              positive ? (
                <NorthEastRounded />
              ) : negative ? (
                <SouthEastRounded />
              ) : (
                <RemoveRounded />
              )
            }
            label={`${Math.abs(change).toFixed(1)}%`}
            sx={{
              height: 24,
              color: positive
                ? "#07875E"
                : negative
                  ? "#C43E44"
                  : "text.secondary",
              bgcolor: positive ? "#E9F9F3" : negative ? "#FEF0F0" : "#F1F3F6",
              "& .MuiChip-icon": { color: "inherit", fontSize: 14 },
            }}
          />
          <Typography variant="caption" color="text.secondary">
            이전 기간 대비
          </Typography>
        </Stack>
      )}
    </Card>
  );
}
