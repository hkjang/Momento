import { Box, CircularProgress } from "@mui/material";
import { Navigate, Route, Routes } from "react-router-dom";
import { useAuth } from "./contexts/AuthContext";
import { SiteProvider } from "./contexts/SiteContext";
import LoginPage from "./pages/LoginPage";
import AppShell from "./components/AppShell";
import OverviewPage from "./pages/OverviewPage";
import RealtimePage from "./pages/RealtimePage";
import UsagePage from "./pages/UsagePage";
import ReportPage from "./pages/ReportPage";
import ExplorerPage from "./pages/ExplorerPage";
import AdminPage from "./pages/AdminPage";
import ProfilePage from "./pages/ProfilePage";
import FunnelPage from "./pages/FunnelPage";
import SegmentsPage from "./pages/SegmentsPage";
import EcommercePage from "./pages/EcommercePage";
import UserExplorerPage from "./pages/UserExplorerPage";

export default function App() {
  const { user, loading } = useAuth();
  if (loading)
    return (
      <Box sx={{ minHeight: "100vh", display: "grid", placeItems: "center" }}>
        <CircularProgress />
      </Box>
    );
  if (!user) return <LoginPage />;
  return (
    <SiteProvider>
      <AppShell>
        <Routes>
          <Route path="/" element={<OverviewPage />} />
          <Route path="/realtime" element={<RealtimePage />} />
          <Route path="/usage" element={<UsagePage />} />
          <Route
            path="/acquisition"
            element={<ReportPage kind="acquisition" />}
          />
          <Route path="/pages" element={<ReportPage kind="pages" />} />
          <Route path="/events" element={<ReportPage kind="events" />} />
          <Route path="/visitors" element={<ReportPage kind="visitors" />} />
          <Route path="/user-explorer" element={<UserExplorerPage />} />
          <Route path="/ecommerce" element={<EcommercePage />} />
          <Route path="/explorer" element={<ExplorerPage />} />
          <Route path="/segments" element={<SegmentsPage />} />
          <Route path="/funnel" element={<FunnelPage mode="funnel" />} />
          <Route path="/path" element={<FunnelPage mode="path" />} />
          <Route path="/admin" element={<AdminPage />} />
          <Route path="/profile" element={<ProfilePage />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </AppShell>
    </SiteProvider>
  );
}
