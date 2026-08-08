import { createTheme } from "@mui/material/styles";

export const theme = createTheme({
  palette: {
    mode: "light",
    primary: { main: "#5B5CE2", dark: "#4143B9", light: "#8A8BF1" },
    secondary: { main: "#14B8A6" },
    background: { default: "#F5F7FB", paper: "#FFFFFF" },
    text: { primary: "#172033", secondary: "#667085" },
    success: { main: "#12A875" },
    warning: { main: "#F59E0B" },
    error: { main: "#E5484D" },
  },
  typography: {
    fontFamily: '"Inter Variable", "Pretendard", system-ui, sans-serif',
    h4: { fontWeight: 750, letterSpacing: "-0.035em" },
    h5: { fontWeight: 720, letterSpacing: "-0.025em" },
    h6: { fontWeight: 700 },
    button: { textTransform: "none", fontWeight: 650 },
  },
  shape: { borderRadius: 12 },
  components: {
    MuiCard: {
      styleOverrides: {
        root: {
          border: "1px solid #E8ECF3",
          boxShadow:
            "0 1px 2px rgba(16,24,40,.03), 0 8px 24px rgba(16,24,40,.035)",
        },
      },
    },
    MuiButton: {
      defaultProps: { disableElevation: true },
      styleOverrides: { root: { borderRadius: 9 } },
    },
    MuiTextField: { defaultProps: { size: "small" } },
    MuiTableCell: {
      styleOverrides: {
        head: {
          color: "#667085",
          fontWeight: 650,
          background: "#F9FAFC",
          borderBottom: "1px solid #E8ECF3",
        },
        root: { borderBottom: "1px solid #EEF1F6" },
      },
    },
    MuiChip: { styleOverrides: { root: { fontWeight: 600 } } },
  },
});
