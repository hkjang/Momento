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
import PlatformAnalyticsPage from "./pages/PlatformAnalyticsPage";
import PlatformAdminPage from "./pages/PlatformAdminPage";
import EnterpriseAnalyticsPage from "./pages/EnterpriseAnalyticsPage";
import EnterpriseAdminPage from "./pages/EnterpriseAdminPage";

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
          <Route path="/sessions" element={<ReportPage kind="sessions" />} />
          <Route path="/user-explorer" element={<UserExplorerPage />} />
          <Route path="/ecommerce" element={<EcommercePage />} />
          <Route path="/explorer" element={<ExplorerPage />} />
          <Route path="/segments" element={<SegmentsPage />} />
          <Route path="/funnel" element={<FunnelPage mode="funnel" />} />
          <Route path="/path" element={<FunnelPage mode="path" />} />
          <Route path="/adoption" element={<PlatformAnalyticsPage mode="adoption" />} />
          <Route path="/cohort" element={<PlatformAnalyticsPage mode="cohort" />} />
          <Route path="/journey" element={<PlatformAnalyticsPage mode="journey" />} />
          <Route path="/experience" element={<PlatformAnalyticsPage mode="experience" />} />
          <Route path="/insights" element={<PlatformAnalyticsPage mode="insights" />} />
          <Route path="/ai-analytics" element={<PlatformAnalyticsPage mode="ai" />} />
          <Route path="/data-quality" element={<PlatformAnalyticsPage mode="quality" />} />
		  <Route path="/workspace" element={<EnterpriseAnalyticsPage mode="workspace" />} />
		  <Route path="/features" element={<EnterpriseAnalyticsPage mode="features" />} />
		  <Route path="/search-analytics" element={<EnterpriseAnalyticsPage mode="search" />} />
		  <Route path="/frustration" element={<EnterpriseAnalyticsPage mode="frustration" />} />
		  <Route path="/experiments" element={<EnterpriseAnalyticsPage mode="experiments" />} />
		  <Route path="/goals" element={<EnterpriseAnalyticsPage mode="goals" />} />
		  <Route path="/change-calendar" element={<EnterpriseAnalyticsPage mode="calendar" />} />
          <Route path="/admin/governance" element={<PlatformAdminPage mode="governance" />} />
          <Route path="/admin/automation" element={<PlatformAdminPage mode="automation" />} />
		  <Route path="/admin/analytics-engineering" element={<EnterpriseAdminPage mode="engineering" />} />
		  <Route path="/admin/privacy-requests" element={<EnterpriseAdminPage mode="privacy" />} />
		  <Route path="/admin/product-lab" element={<EnterpriseAdminPage mode="product" />} />
          <Route path="/admin" element={<AdminPage />} />
          <Route path="/profile" element={<ProfilePage />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </AppShell>
    </SiteProvider>
  );
}
