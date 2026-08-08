import { Box, Typography } from "@mui/material";

export function Logo({
  compact = false,
  light = false,
}: {
  compact?: boolean;
  light?: boolean;
}) {
  return (
    <Box sx={{ display: "flex", alignItems: "center", gap: 1.2 }}>
      <Box
        sx={{
          width: 34,
          height: 34,
          borderRadius: "11px",
          position: "relative",
          overflow: "hidden",
          background: "linear-gradient(145deg,#8B8CF6,#5B5CE2)",
          boxShadow: "0 8px 20px rgba(91,92,226,.28)",
          "&:before": {
            content: '""',
            position: "absolute",
            width: 13,
            height: 19,
            border: "3px solid white",
            borderRight: 0,
            borderRadius: "8px 0 0 8px",
            left: 7,
            top: 7,
          },
          "&:after": {
            content: '""',
            position: "absolute",
            width: 8,
            height: 8,
            borderRadius: "50%",
            background: "#5EEAD4",
            right: 5,
            top: 5,
          },
        }}
      />
      {!compact && (
        <Typography
          sx={{
            color: light ? "white" : "#172033",
            fontWeight: 780,
            fontSize: 20,
            letterSpacing: "-.04em",
          }}
        >
          momento
        </Typography>
      )}
    </Box>
  );
}
