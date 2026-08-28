import { Suspense, lazy } from "react";
import { Box, Button, Card, CircularProgress, Typography } from "@mui/material";
import { Navigate, Route, Routes } from "react-router-dom";
import { useAuth } from "./contexts/AuthContext";
import { SiteProvider } from "./contexts/SiteContext";
import LoginPage from "./pages/LoginPage";
import AppShell from "./components/AppShell";
import { ErrorState } from "./components/States";

// Every screen used to be part of the first download, so signing in cost the
// whole console — the charting library included — before the landing screen
// could be drawn, and a reader who only ever opens two screens paid for all
// twenty-two. Each is fetched when it is first opened instead, and the browser
// keeps it from then on.
const OverviewPage = lazy(() => import("./pages/OverviewPage"));
const RealtimePage = lazy(() => import("./pages/RealtimePage"));
const VisitorInsightsPage = lazy(() => import("./pages/VisitorInsightsPage"));
const UsagePage = lazy(() => import("./pages/UsagePage"));
const ReportPage = lazy(() => import("./pages/ReportPage"));
const ExplorerPage = lazy(() => import("./pages/ExplorerPage"));
const AdminPage = lazy(() => import("./pages/AdminPage"));
const ProfilePage = lazy(() => import("./pages/ProfilePage"));
const FunnelPage = lazy(() => import("./pages/FunnelPage"));
const SegmentsPage = lazy(() => import("./pages/SegmentsPage"));
const EcommercePage = lazy(() => import("./pages/EcommercePage"));
const UserExplorerPage = lazy(() => import("./pages/UserExplorerPage"));
const PlatformAnalyticsPage = lazy(
  () => import("./pages/PlatformAnalyticsPage"),
);
const PlatformAdminPage = lazy(() => import("./pages/PlatformAdminPage"));
const EnterpriseAnalyticsPage = lazy(
  () => import("./pages/EnterpriseAnalyticsPage"),
);
const EnterpriseAdminPage = lazy(() => import("./pages/EnterpriseAdminPage"));

export default function App() {
  const { user, loading, sessionError, refresh } = useAuth();
  if (loading)
    return (
      <Box sx={{ minHeight: "100vh", display: "grid", placeItems: "center" }}>
        <CircularProgress />
      </Box>
    );
  // Not signed in and could not tell are different answers. Showing the login
  // form for the second sends somebody with a valid session to a form that will
  // fail the same way, and says nothing about why.
  if (!user && sessionError)
    return (
      <Box
        sx={{
          minHeight: "100vh",
          display: "grid",
          placeItems: "center",
          p: 3,
        }}
      >
        <Card sx={{ p: 4, maxWidth: 520, textAlign: "center" }}>
          <Typography variant="h6" gutterBottom>
            로그인 상태를 확인하지 못했습니다
          </Typography>
          <Typography color="text.secondary" mb={2}>
            로그아웃된 것이 아니라 서버에 닿지 못한 것입니다. 연결을 확인한 뒤
            다시 시도하세요.
          </Typography>
          <ErrorState error={sessionError} />
          <Button
            variant="contained"
            sx={{ mt: 2 }}
            onClick={() => void refresh()}
          >
            다시 확인
          </Button>
        </Card>
      </Box>
    );
  if (!user) return <LoginPage />;
  return (
    <SiteProvider>
      <AppShell>
        {/* The shell, the navigation and the site picker are already on screen, so
            a screen still arriving shows a spinner in the content area rather than
            replacing the page. */}
        <Suspense
          fallback={
            <Box sx={{ display: "grid", placeItems: "center", py: 10 }}>
              <CircularProgress />
            </Box>
          }
        >
          <Routes>
            <Route path="/" element={<OverviewPage />} />
            <Route path="/realtime" element={<RealtimePage />} />
            <Route path="/visitor-insights" element={<VisitorInsightsPage />} />
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
            <Route
              path="/adoption"
              element={<PlatformAnalyticsPage mode="adoption" />}
            />
            <Route
              path="/cohort"
              element={<PlatformAnalyticsPage mode="cohort" />}
            />
            <Route
              path="/journey"
              element={<PlatformAnalyticsPage mode="journey" />}
            />
            <Route
              path="/experience"
              element={<PlatformAnalyticsPage mode="experience" />}
            />
            <Route
              path="/insights"
              element={<PlatformAnalyticsPage mode="insights" />}
            />
            <Route
              path="/ai-analytics"
              element={<PlatformAnalyticsPage mode="ai" />}
            />
            <Route
              path="/data-quality"
              element={<PlatformAnalyticsPage mode="quality" />}
            />
            <Route
              path="/workspace"
              element={<EnterpriseAnalyticsPage mode="workspace" />}
            />
            <Route
              path="/features"
              element={<EnterpriseAnalyticsPage mode="features" />}
            />
            <Route
              path="/search-analytics"
              element={<EnterpriseAnalyticsPage mode="search" />}
            />
            <Route
              path="/frustration"
              element={<EnterpriseAnalyticsPage mode="frustration" />}
            />
            <Route
              path="/experiments"
              element={<EnterpriseAnalyticsPage mode="experiments" />}
            />
            <Route
              path="/goals"
              element={<EnterpriseAnalyticsPage mode="goals" />}
            />
            <Route
              path="/change-calendar"
              element={<EnterpriseAnalyticsPage mode="calendar" />}
            />
            <Route
              path="/admin/governance"
              element={<PlatformAdminPage mode="governance" />}
            />
            <Route
              path="/admin/automation"
              element={<PlatformAdminPage mode="automation" />}
            />
            <Route
              path="/admin/analytics-engineering"
              element={<EnterpriseAdminPage mode="engineering" />}
            />
            <Route
              path="/admin/privacy-requests"
              element={<EnterpriseAdminPage mode="privacy" />}
            />
            <Route
              path="/admin/product-lab"
              element={<EnterpriseAdminPage mode="product" />}
            />
            <Route path="/admin" element={<AdminPage />} />
            <Route path="/profile" element={<ProfilePage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </Suspense>
      </AppShell>
    </SiteProvider>
  );
}
