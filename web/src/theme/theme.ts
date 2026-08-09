import { createTheme } from "@mui/material/styles";

export const theme = createTheme({
  palette: {
    mode: "light",
    primary: { main: "#5B5CE2", dark: "#4143B9", light: "#8A8BF1" },
    secondary: { main: "#14B8A6" },
    background: { default: "#F4F6FA", paper: "#FFFFFF" },
    divider: "#E6EAF1",
    text: { primary: "#172033", secondary: "#667085" },
    success: { main: "#12A875" },
    warning: { main: "#F59E0B" },
    error: { main: "#E5484D" },
  },
  typography: {
    fontFamily: '"Inter Variable", "Pretendard", system-ui, sans-serif',
    h4: { fontWeight: 750, letterSpacing: "-0.035em" },
    h5: { fontWeight: 720, letterSpacing: "-0.025em" },
    h6: { fontWeight: 720, letterSpacing: "-.015em" },
    body2: { lineHeight: 1.55 },
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
          backgroundImage: "none",
        },
      },
    },
    MuiButton: {
      defaultProps: { disableElevation: true },
      styleOverrides: {
        root: { borderRadius: 9, minHeight: 36 },
      },
    },
    MuiTextField: { defaultProps: { size: "small", variant: "outlined" } },
    MuiOutlinedInput: {
      styleOverrides: {
        root: {
          background: "#FFFFFF",
          transition: "box-shadow .16s ease, border-color .16s ease",
          "&.Mui-focused": { boxShadow: "0 0 0 3px rgba(91,92,226,.11)" },
        },
      },
    },
    MuiTableCell: {
      styleOverrides: {
        head: {
          color: "#667085",
          fontWeight: 650,
          background: "#F9FAFC",
          borderBottom: "1px solid #E8ECF3",
        },
        root: {
          borderBottom: "1px solid #EEF1F6",
          paddingTop: 11,
          paddingBottom: 11,
        },
      },
    },
    MuiChip: { styleOverrides: { root: { fontWeight: 650 } } },
    MuiDialog: {
      styleOverrides: {
        paper: {
          border: "1px solid #E6EAF1",
          boxShadow: "0 24px 70px rgba(17,24,39,.22)",
        },
      },
    },
    MuiMenu: {
      styleOverrides: {
        paper: {
          border: "1px solid #E6EAF1",
          boxShadow: "0 16px 44px rgba(17,24,39,.15)",
        },
      },
    },
    MuiTooltip: { defaultProps: { arrow: true, enterDelay: 450 } },
  },
});
