import { useState, type ReactNode } from "react";
import {
  Alert,
  Box,
  Button,
  Card,
  Checkbox,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  FormControlLabel,
  IconButton,
  InputAdornment,
  LinearProgress,
  List,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  MenuItem,
  Stack,
  Tab,
  Tabs,
  TextField,
  Typography,
} from "@mui/material";
import AddRounded from "@mui/icons-material/AddRounded";
import AccountTreeRounded from "@mui/icons-material/AccountTreeRounded";
import AdminPanelSettingsRounded from "@mui/icons-material/AdminPanelSettingsRounded";
import BugReportRounded from "@mui/icons-material/BugReportRounded";
import ChevronRightRounded from "@mui/icons-material/ChevronRightRounded";
import CheckCircleRounded from "@mui/icons-material/CheckCircleRounded";
import ContentCopyRounded from "@mui/icons-material/ContentCopyRounded";
import DeleteOutlineRounded from "@mui/icons-material/DeleteOutlineRounded";
import ErrorOutlineRounded from "@mui/icons-material/ErrorOutlineRounded";
import FactCheckRounded from "@mui/icons-material/FactCheckRounded";
import HomeRounded from "@mui/icons-material/HomeRounded";
import IntegrationInstructionsRounded from "@mui/icons-material/IntegrationInstructionsRounded";
import KeyRounded from "@mui/icons-material/KeyRounded";
import LanRounded from "@mui/icons-material/LanRounded";
import PeopleAltRounded from "@mui/icons-material/PeopleAltRounded";
import RefreshRounded from "@mui/icons-material/RefreshRounded";
import SaveRounded from "@mui/icons-material/SaveRounded";
import SchemaRounded from "@mui/icons-material/SchemaRounded";
import SecurityRounded from "@mui/icons-material/SecurityRounded";
import SettingsRounded from "@mui/icons-material/SettingsRounded";
import StorageRounded from "@mui/icons-material/StorageRounded";
import WarningAmberRounded from "@mui/icons-material/WarningAmberRounded";
import EditOutlined from "@mui/icons-material/EditOutlined";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearchParams } from "react-router-dom";
import {
  del,
  get,
  patch,
  post,
  put,
  rangeQuery,
  type Site,
} from "../api/client";
import { useAuth } from "../contexts/AuthContext";
import { useSite } from "../contexts/SiteContext";
import DataTable from "../components/DataTable";
import { Empty, ErrorState, Loading, NoSite } from "../components/States";
import {
  buildCSPGuidance,
  buildSDKSnippet,
  consentExample,
  identifyAndEventExample,
  signalInstrumentation,
  type SDKTrackingMode,
} from "./sdkGuide";

const adminSections = [
  {
    id: "overview",
    label: "관리 홈",
    description: "운영 상태와 빠른 작업",
    group: "관리 센터",
    icon: <HomeRounded />,
  },
  {
    id: "sites",
    label: "사이트",
    description: "수집 경계와 Tracking Key",
    group: "서비스 설정",
    icon: <StorageRounded />,
  },
  {
    id: "settings",
    // Instance-wide: it applies to every workspace, so it belongs to an
    // organisation administrator. A workspace_admin used to see these and have
    // its writes refused by the server.
    orgOnly: true,
    label: "SSO · 일반",
    description: "Keycloak과 공통 보안 설정",
    group: "서비스 설정",
    icon: <SettingsRounded />,
  },
  {
    id: "privacy",
    // Instance-wide: it applies to every workspace, so it belongs to an
    // organisation administrator. A workspace_admin used to see these and have
    // its writes refused by the server.
    orgOnly: true,
    label: "개인정보",
    description: "PII와 최소 수집 정책",
    group: "보안 · 데이터",
    icon: <SecurityRounded />,
  },
  {
    id: "retention",
    label: "보존 정책",
    description: "Raw Event와 집계 보존",
    group: "보안 · 데이터",
    icon: <FactCheckRounded />,
  },
  {
    id: "networks",
    // Instance-wide: it applies to every workspace, so it belongs to an
    // organisation administrator. A workspace_admin used to see these and have
    // its writes refused by the server.
    orgOnly: true,
    label: "네트워크 망",
    description: "CIDR 기반 사내망 분류",
    group: "보안 · 데이터",
    icon: <LanRounded />,
  },
  {
    id: "users",
    // Instance-wide: it applies to every workspace, so it belongs to an
    // organisation administrator. A workspace_admin used to see these and have
    // its writes refused by the server.
    orgOnly: true,
    label: "사용자 · 권한",
    description: "RBAC와 Workspace 권한",
    group: "접근 제어",
    icon: <PeopleAltRounded />,
  },
  {
    id: "schemas",
    label: "이벤트 스키마",
    description: "Event Contract와 전환",
    group: "Tracking 설계",
    icon: <SchemaRounded />,
  },
  {
    id: "dimensions",
    label: "사용자 정의 차원",
    description: "User·Session·Event Scope",
    group: "Tracking 설계",
    icon: <AccountTreeRounded />,
  },
  {
    id: "debugger",
    label: "Tracking Debugger",
    description: "수집 이벤트와 오류 확인",
    group: "운영 도구",
    icon: <BugReportRounded />,
  },
  {
    id: "audit",
    label: "감사 로그",
    description: "관리자 작업 추적",
    group: "운영 도구",
    icon: <AdminPanelSettingsRounded />,
  },
] as const;

export default function AdminPage() {
  const { user } = useAuth();
  const [params, setParams] = useSearchParams();
  const requested = params.get("section") || "overview";
  const orgAdmin =
    user?.role === "organization_admin" || user?.role === "super_admin";
  const visibleSections = adminSections.filter(
    (item) => orgAdmin || !("orgOnly" in item && item.orgOnly),
  );
  const section = visibleSections.some((item) => item.id === requested)
    ? requested
    : "overview";
  const active = visibleSections.find((item) => item.id === section)!;
  if (user?.role === "analyst" || user?.role === "viewer")
    return <Alert severity="warning">관리자 권한이 필요합니다.</Alert>;
  // Reached by a link or a bookmark rather than the navigation.
  if (
    requested !== section &&
    adminSections.some((item) => item.id === requested)
  )
    return (
      <Alert severity="warning">
        이 설정은 배포 전체에 적용되므로 조직 관리자(organization_admin) 이상만
        변경할 수 있습니다. Workspace 관리자는 사이트와 보존 정책을 관리합니다.
      </Alert>
    );
  const selectSection = (id: string) => {
    if (id === "overview") setParams({});
    else setParams({ section: id });
  };
  const content =
    section === "sites" ? (
      <SitesAdmin />
    ) : section === "settings" ? (
      <SettingsAdmin groups={["general", "oidc", "storage", "security"]} />
    ) : section === "privacy" ? (
      <PrivacyAdmin />
    ) : section === "retention" ? (
      <RetentionAdmin />
    ) : section === "networks" ? (
      <NetworksAdmin />
    ) : section === "users" ? (
      <UsersAdmin />
    ) : section === "schemas" ? (
      <SchemasAdmin />
    ) : section === "dimensions" ? (
      <DimensionsAdmin />
    ) : section === "debugger" ? (
      <DebuggerAdmin />
    ) : section === "audit" ? (
      <AuditAdmin />
    ) : (
      <AdminOverview />
    );

  return (
    <Stack spacing={2.5}>
      <TextField
        select
        label="관리 메뉴"
        value={section}
        onChange={(event) => selectSection(event.target.value)}
        sx={{ display: { xs: "flex", md: "none" } }}
      >
        {visibleSections.map((item) => (
          <MenuItem key={item.id} value={item.id}>
            {item.label} · {item.description}
          </MenuItem>
        ))}
      </TextField>
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: {
            xs: "minmax(0,1fr)",
            md: "248px minmax(0,1fr)",
          },
          gap: 2.5,
          alignItems: "start",
        }}
      >
        <Card
          component="aside"
          sx={{
            display: { xs: "none", md: "block" },
            p: 1.25,
            position: "sticky",
            top: 92,
            maxHeight: "calc(100vh - 116px)",
            overflowY: "auto",
          }}
        >
          {[...new Set(visibleSections.map((item) => item.group))].map(
            (group) => (
              <Box key={group} sx={{ mb: 1.2 }}>
                <Typography
                  variant="overline"
                  color="text.secondary"
                  sx={{
                    px: 1.25,
                    fontSize: 10,
                    fontWeight: 800,
                    letterSpacing: ".08em",
                  }}
                >
                  {group}
                </Typography>
                <List disablePadding>
                  {visibleSections
                    .filter((item) => item.group === group)
                    .map((item) => (
                      <ListItemButton
                        key={item.id}
                        selected={section === item.id}
                        onClick={() => selectSection(item.id)}
                        sx={{
                          borderRadius: 2,
                          mb: 0.25,
                          alignItems: "flex-start",
                          "&.Mui-selected": {
                            bgcolor: "#EEEEFF",
                            color: "primary.dark",
                          },
                        }}
                      >
                        <ListItemIcon
                          sx={{
                            minWidth: 36,
                            color: "inherit",
                            mt: 0.25,
                            "& svg": { fontSize: 19 },
                          }}
                        >
                          {item.icon}
                        </ListItemIcon>
                        <ListItemText
                          primary={item.label}
                          secondary={item.description}
                          primaryTypographyProps={{
                            fontSize: 13.5,
                            fontWeight: 700,
                          }}
                          secondaryTypographyProps={{
                            fontSize: 10.5,
                            lineHeight: 1.35,
                          }}
                        />
                      </ListItemButton>
                    ))}
                </List>
              </Box>
            ),
          )}
        </Card>
        <Box minWidth={0}>
          {section !== "overview" && (
            <Stack
              direction={{ xs: "column", sm: "row" }}
              justifyContent="space-between"
              alignItems={{ xs: "flex-start", sm: "center" }}
              gap={1}
              sx={{ mb: 2 }}
            >
              <Box>
                <Stack direction="row" gap={1} alignItems="center">
                  <Box sx={{ color: "primary.main", display: "flex" }}>
                    {active.icon}
                  </Box>
                  <Typography variant="h6">{active.label}</Typography>
                </Stack>
                <Typography variant="body2" color="text.secondary" mt={0.3}>
                  {active.description}
                </Typography>
              </Box>
              <Chip
                size="small"
                label="변경 사항은 감사 로그에 기록됩니다"
                variant="outlined"
              />
            </Stack>
          )}
          {content}
        </Box>
      </Box>
    </Stack>
  );
}

type AdminUserSummary = {
  role: string;
  active: boolean;
};

type DataQualitySummary = {
  health_score: number;
  collector: {
    received: number;
    accepted: number;
    pending: number;
    inbox_lag_seconds: number;
    dead_letters: number;
  };
};

type TrackingSummary = {
  events: Record<string, unknown>[];
  errors: Record<string, unknown>[];
};

type WorkflowSummary = {
  status: string;
  environment?: string;
  created_at?: string;
};

type AuditSummary = {
  id: number;
  action: string;
  resource_type: string;
  actor: string;
  created_at: string;
};

type EncryptionStatus = {
  enabled: boolean;
  algorithm: string;
  key_id: string;
  previous_key_ids: string[];
  recoverable_keys: number;
  unrecoverable_keys: number;
  pending_reseal: number;
};

function relativeTime(value: string) {
  const timestamp = new Date(value).getTime();
  if (!Number.isFinite(timestamp)) return "—";
  const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000));
  if (seconds < 60) return "방금 전";
  if (seconds < 3600) return `${Math.floor(seconds / 60)}분 전`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}시간 전`;
  return `${Math.floor(seconds / 86400)}일 전`;
}

function AdminOverview() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { user } = useAuth();
  const { sites, site, environment } = useSite();
  const settings = useQuery({
    queryKey: ["settings"],
    queryFn: () => get<Settings>("/api/v1/settings"),
  });
  const users = useQuery({
    queryKey: ["users"],
    queryFn: () => get<AdminUserSummary[]>("/api/v1/users"),
  });
  const debuggerQuery = useQuery({
    queryKey: ["tracking-debugger", site?.site_id],
    queryFn: () =>
      get<TrackingSummary>(
        `/api/v1/tracking-debugger?site_id=${site?.site_id || ""}`,
      ),
    enabled: !!site,
    refetchInterval: 30000,
  });
  const quality = useQuery({
    queryKey: ["data-quality", site?.site_id, environment],
    queryFn: () =>
      get<DataQualitySummary>(
        `/api/v1/sites/${site!.site_id}/data-quality?${rangeQuery(7, site!.timezone)}`,
      ),
    enabled: !!site,
    refetchInterval: 30000,
  });
  const privacyRequests = useQuery({
    queryKey: ["privacy-requests", site?.site_id],
    queryFn: () =>
      get<WorkflowSummary[]>(`/api/v1/sites/${site!.site_id}/privacy-requests`),
    enabled: !!site,
  });
  const aggregateJobs = useQuery({
    queryKey: ["aggregate-jobs", site?.site_id],
    queryFn: () =>
      get<WorkflowSummary[]>(`/api/v1/sites/${site!.site_id}/aggregate-jobs`),
    enabled: !!site,
    refetchInterval: 30000,
  });
  const audit = useQuery({
    queryKey: ["audit"],
    queryFn: () => get<AuditSummary[]>("/api/v1/audit"),
  });
  const encryption = useQuery({
    queryKey: ["encryption-status"],
    queryFn: () => get<EncryptionStatus>("/api/v1/system/encryption"),
  });
  const privacy = settings.data?.privacy?.value || {};
  const oidc = settings.data?.oidc?.value || {};
  const activeSites = sites.filter((item) => item.active);
  const administratorCount = (users.data || []).filter(
    (item) => item.active && item.role.endsWith("_admin"),
  ).length;
  const unrestrictedSites = activeSites.filter(
    (item) => item.allowed_domains.length === 0,
  );
  const readiness = [
    {
      label: "수집 사이트",
      detail: activeSites.length
        ? `${activeSites.length}개 사이트가 활성 상태입니다.`
        : "활성 사이트를 등록해야 합니다.",
      ready: activeSites.length > 0,
      to: "/admin?section=sites",
    },
    {
      label: "Origin 제한",
      detail: unrestrictedSites.length
        ? `${unrestrictedSites.length}개 사이트가 모든 Origin을 허용합니다.`
        : "활성 사이트의 허용 도메인이 제한되어 있습니다.",
      ready: activeSites.length > 0 && unrestrictedSites.length === 0,
      to: "/admin?section=sites",
    },
    {
      label: "URL 개인정보 보호",
      detail: privacy.strip_query_string
        ? "Query String을 Raw Event 저장 전에 제거합니다."
        : "Query String 제거 정책이 꺼져 있습니다.",
      ready: privacy.strip_query_string === true,
      to: "/admin?section=privacy",
    },
    {
      label: "PII 값 탐지",
      detail: ["mask", "reject"].includes(String(privacy.pii_detection_mode))
        ? `${String(privacy.pii_detection_mode).toUpperCase()} 정책이 적용됩니다.`
        : "PII 탐지를 Mask 또는 Reject로 강화하세요.",
      ready: ["mask", "reject"].includes(String(privacy.pii_detection_mode)),
      to: "/admin?section=privacy",
    },
    {
      label: "관리자 이중화",
      detail:
        administratorCount >= 2
          ? `${administratorCount}개의 활성 관리자 계정이 있습니다.`
          : "비상 운영을 위해 관리자를 한 명 더 지정하세요.",
      ready: administratorCount >= 2,
      to: "/admin?section=users",
    },
    {
      label: "키 영구 저장",
      detail: encryption.data?.enabled
        ? `MOMENTO_ENCRYPTION_KEY(${encryption.data.key_id})로 ${encryption.data.recoverable_keys}개 키를 암호화 저장했습니다.`
        : "MOMENTO_ENCRYPTION_KEY가 없어 발급한 키를 재기동 후 다시 조회할 수 없습니다.",
      ready: encryption.data?.enabled === true,
      to: "/admin?section=settings",
    },
    {
      label: "Enterprise SSO",
      detail: oidc.enabled
        ? "OIDC 로그인이 활성화되어 있습니다."
        : "OIDC를 연결하면 계정 수명주기를 중앙 관리할 수 있습니다.",
      ready: oidc.enabled === true,
      to: "/admin?section=settings",
    },
  ];
  const readinessLoading =
    settings.isLoading || users.isLoading || encryption.isLoading;
  const readinessScore = Math.round(
    (readiness.filter((item) => item.ready).length / readiness.length) * 100,
  );
  const pendingPrivacy = (privacyRequests.data || []).filter(
    (item) => item.status === "pending",
  ).length;
  const activeJobs = (aggregateJobs.data || []).filter(
    (item) =>
      item.environment === environment &&
      ["pending", "running"].includes(item.status),
  ).length;
  const recentFailedJobs = (aggregateJobs.data || []).filter((item) => {
    if (item.environment !== environment || item.status !== "failed")
      return false;
    const created = new Date(item.created_at || 0).getTime();
    return created > Date.now() - 7 * 86400000;
  }).length;
  const collectorErrors = debuggerQuery.data?.errors.length || 0;
  const deadLetters = quality.data?.collector.dead_letters || 0;
  const actions: {
    severity: "critical" | "warning" | "info";
    title: string;
    detail: string;
    to: string;
  }[] = [];
  if (collectorErrors || deadLetters)
    actions.push({
      severity: "critical",
      title: "수집 처리 오류 확인",
      detail: `처리 오류 ${collectorErrors}건 · 최근 Dead Letter ${deadLetters}건`,
      to: "/admin?section=debugger",
    });
  if (recentFailedJobs)
    actions.push({
      severity: "critical",
      title: "실패한 재집계 작업 확인",
      detail: `현재 환경에서 최근 7일간 ${recentFailedJobs}개 작업이 실패했습니다.`,
      to: "/admin/analytics-engineering?panel=aggregate",
    });
  if (pendingPrivacy)
    actions.push({
      severity: "warning",
      title: "개인정보 요청 검토",
      detail: `${pendingPrivacy}개 요청이 관리자 결정을 기다립니다.`,
      to: "/admin/privacy-requests",
    });
  if (quality.data && quality.data.health_score < 95)
    actions.push({
      severity: "warning",
      title: "데이터 품질 저하 분석",
      detail: `최근 7일 품질 점수가 ${quality.data.health_score.toFixed(1)}점입니다.`,
      to: "/data-quality",
    });
  if (unrestrictedSites.length)
    actions.push({
      severity: "warning",
      title: "수집 Origin 제한",
      detail: `${unrestrictedSites.map((item) => item.name).join(", ")} 사이트가 모든 Origin을 허용합니다.`,
      to: "/admin?section=sites",
    });
  if (users.data && administratorCount < 2)
    actions.push({
      severity: "warning",
      title: "비상 관리자 지정",
      detail: "단일 관리자 잠금에 대비해 활성 관리자 계정을 추가하세요.",
      to: "/admin?section=users",
    });
  if (settings.data && !oidc.enabled)
    actions.push({
      severity: "info",
      title: "Enterprise SSO 연결",
      detail: "Keycloak OIDC로 입·퇴사자 계정 관리를 중앙화할 수 있습니다.",
      to: "/admin?section=settings",
    });
  if (quality.data && quality.data.collector.received === 0)
    actions.push({
      severity: "info",
      title: "SDK 수집 상태 점검",
      detail: `최근 7일간 ${environment.toUpperCase()} 환경에서 수신된 이벤트가 없습니다.`,
      to: "/admin?section=debugger",
    });
  const summary = [
    {
      label: "데이터 품질",
      value: quality.data ? quality.data.health_score.toFixed(1) : "—",
      suffix: quality.data ? "점" : "",
      detail: "최근 7일 수집 정확도",
      tone:
        quality.data && quality.data.health_score < 95 ? "warning" : "success",
    },
    {
      label: "수신 이벤트",
      value: (quality.data?.collector.received || 0).toLocaleString(),
      detail: `${environment.toUpperCase()} · 최근 7일`,
      tone: "default",
    },
    {
      label: "수집 대기",
      value: (quality.data?.collector.pending || 0).toLocaleString(),
      detail: quality.data?.collector.inbox_lag_seconds
        ? `최대 ${Math.round(quality.data.collector.inbox_lag_seconds)}초 지연`
        : "Inbox 지연 없음",
      tone: quality.data?.collector.pending ? "warning" : "success",
    },
    {
      label: "처리 오류",
      value: collectorErrors.toLocaleString(),
      detail: `Dead Letter ${deadLetters}건`,
      tone: collectorErrors || deadLetters ? "error" : "success",
    },
    {
      label: "개인정보 요청",
      value: pendingPrivacy.toLocaleString(),
      detail: "결정 대기",
      tone: pendingPrivacy ? "warning" : "success",
    },
    {
      label: "재집계 작업",
      value: activeJobs.toLocaleString(),
      detail: `진행 중 · 실패 ${recentFailedJobs}건`,
      tone: recentFailedJobs ? "error" : activeJobs ? "warning" : "success",
    },
  ];
  const quickActions = [
    {
      title: "새 사이트와 SDK 연결",
      description: "수집 경계, 허용 도메인과 Tracking Key를 구성합니다.",
      to: "/admin?section=sites",
      color: "#5B5CE2",
    },
    {
      title: "Keycloak SSO 구성",
      description: "Issuer, Client ID와 Claim Mapping을 설정합니다.",
      to: "/admin?section=settings",
      color: "#0F9F8F",
    },
    {
      title: "Tracking 계약 설계",
      description: "Event Contract와 Custom Dimension을 등록합니다.",
      to: "/admin/governance",
      color: "#C47A0A",
    },
    {
      title: "데이터 품질 확인",
      description: "PII, 지연, 중복과 Cardinality 위험을 확인합니다.",
      to: "/data-quality",
      color: "#D14A50",
    },
  ];
  const platform = [
    {
      label: "Analytics Engineering",
      description: "Metric · Goal · Query Cost · Aggregate",
      to: "/admin/analytics-engineering",
    },
    {
      label: "Feature Flag · Lab",
      description: "실험 계약과 Variant 관리",
      to: "/admin/product-lab",
    },
    {
      label: "Privacy Requests",
      description: "삭제와 Export 승인 Workflow",
      to: "/admin/privacy-requests",
    },
    {
      label: "Report · Action",
      description: "Scheduled Report와 Webhook",
      to: "/admin/automation",
    },
  ];
  const overviewError =
    settings.error ||
    users.error ||
    debuggerQuery.error ||
    quality.error ||
    privacyRequests.error ||
    aggregateJobs.error ||
    audit.error;
  const refreshing =
    settings.isFetching ||
    users.isFetching ||
    debuggerQuery.isFetching ||
    quality.isFetching ||
    privacyRequests.isFetching ||
    aggregateJobs.isFetching ||
    audit.isFetching;
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ["settings"] });
    void qc.invalidateQueries({ queryKey: ["users"] });
    void qc.invalidateQueries({ queryKey: ["tracking-debugger"] });
    void qc.invalidateQueries({ queryKey: ["data-quality"] });
    void qc.invalidateQueries({ queryKey: ["privacy-requests"] });
    void qc.invalidateQueries({ queryKey: ["aggregate-jobs"] });
    void qc.invalidateQueries({ queryKey: ["audit"] });
  };
  return (
    <Stack spacing={2.5}>
      <Card className="admin-hero" sx={{ p: { xs: 2.5, md: 3.25 } }}>
        <Stack
          direction={{ xs: "column", md: "row" }}
          justifyContent="space-between"
          gap={2}
        >
          <Box maxWidth={700}>
            <Chip
              size="small"
              label="ADMIN CONTROL PLANE"
              sx={{ mb: 1.5, bgcolor: "rgba(255,255,255,.14)", color: "white" }}
            />
            <Typography variant="h5" color="white">
              {site?.name || "Momento"} 운영 브리핑
            </Typography>
            <Typography color="rgba(255,255,255,.72)" variant="body2" mt={1}>
              {environment.toUpperCase()} 환경의 수집 상태와 미처리 작업을
              한곳에서 확인하고, 우선순위가 높은 조치부터 처리하세요.
            </Typography>
          </Box>
          <Stack alignItems={{ md: "flex-end" }} gap={1}>
            <Typography
              color="white"
              fontSize={{ xs: 32, md: 40 }}
              fontWeight={800}
              lineHeight={1}
            >
              {readinessLoading ? "—" : readinessScore}
              <Typography component="span" color="rgba(255,255,255,.7)">
                /100
              </Typography>
            </Typography>
            <Typography variant="caption" color="rgba(255,255,255,.72)">
              운영 준비도 · {user?.role.replaceAll("_", " ") || "admin"}
            </Typography>
            <Button
              variant="contained"
              color="inherit"
              size="small"
              startIcon={<RefreshRounded />}
              onClick={refresh}
              disabled={refreshing}
              sx={{ color: "primary.dark", bgcolor: "white" }}
            >
              {refreshing ? "갱신 중" : "상태 새로고침"}
            </Button>
          </Stack>
        </Stack>
      </Card>
      {overviewError && (
        <Alert severity="warning">
          일부 운영 지표를 불러오지 못했습니다. 새로고침 후에도 계속되면 Audit
          Log와 서버 상태를 확인하세요.
        </Alert>
      )}
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: {
            xs: "repeat(2,minmax(0,1fr))",
            md: "repeat(3,minmax(0,1fr))",
            xl: "repeat(6,minmax(0,1fr))",
          },
          gap: 1.5,
        }}
      >
        {summary.map((item) => (
          <Card key={item.label} sx={{ p: 2.2 }}>
            <Typography
              variant="caption"
              color="text.secondary"
              fontWeight={700}
            >
              {item.label}
            </Typography>
            <Stack direction="row" alignItems="baseline" gap={0.4} mt={0.6}>
              <Typography fontSize={{ xs: 20, md: 25 }} fontWeight={760} noWrap>
                {item.value}
              </Typography>
              {"suffix" in item && item.suffix && (
                <Typography variant="caption" color="text.secondary">
                  {item.suffix}
                </Typography>
              )}
            </Stack>
            <Typography variant="caption" color="text.secondary">
              {item.detail}
            </Typography>
            <Box
              sx={{
                width: 7,
                height: 7,
                borderRadius: "50%",
                mt: 1.2,
                bgcolor:
                  item.tone === "error"
                    ? "error.main"
                    : item.tone === "warning"
                      ? "warning.main"
                      : item.tone === "success"
                        ? "success.main"
                        : "text.disabled",
              }}
            />
          </Card>
        ))}
      </Box>
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: { xs: "1fr", xl: "minmax(0,1fr) minmax(0,1fr)" },
          gap: 2,
        }}
      >
        <Card sx={{ p: { xs: 2.2, md: 2.75 } }}>
          <Stack direction="row" justifyContent="space-between" gap={2}>
            <Box>
              <Typography variant="h6">운영 준비도</Typography>
              <Typography variant="body2" color="text.secondary">
                보안·접근·수집 기본 설정의 권장 상태입니다.
              </Typography>
            </Box>
            <Chip
              size="small"
              color={
                readinessScore >= 90
                  ? "success"
                  : readinessScore >= 60
                    ? "warning"
                    : "error"
              }
              label={readinessLoading ? "계산 중" : `${readinessScore}%`}
            />
          </Stack>
          <LinearProgress
            variant={readinessLoading ? "indeterminate" : "determinate"}
            value={readinessScore}
            color={readinessScore >= 90 ? "success" : "primary"}
            sx={{ mt: 2, mb: 1.5, height: 7, borderRadius: 999 }}
          />
          <Stack spacing={0.4}>
            {readiness.map((item) => (
              <ListItemButton
                key={item.label}
                onClick={() => navigate(item.to)}
                sx={{ borderRadius: 2, px: 1, py: 0.8 }}
              >
                <ListItemIcon sx={{ minWidth: 34 }}>
                  {item.ready ? (
                    <CheckCircleRounded color="success" fontSize="small" />
                  ) : (
                    <WarningAmberRounded color="warning" fontSize="small" />
                  )}
                </ListItemIcon>
                <ListItemText
                  primary={item.label}
                  secondary={item.detail}
                  primaryTypographyProps={{ fontWeight: 700, fontSize: 13.5 }}
                  secondaryTypographyProps={{ fontSize: 11.5 }}
                />
                <ChevronRightRounded color="action" fontSize="small" />
              </ListItemButton>
            ))}
          </Stack>
        </Card>
        <Card sx={{ p: { xs: 2.2, md: 2.75 } }}>
          <Stack
            direction="row"
            justifyContent="space-between"
            gap={2}
            mb={1.5}
          >
            <Box>
              <Typography variant="h6">조치 필요</Typography>
              <Typography variant="body2" color="text.secondary">
                위험도와 처리 시급성을 기준으로 정렬했습니다.
              </Typography>
            </Box>
            <Chip
              size="small"
              variant="outlined"
              label={`${actions.length}개`}
            />
          </Stack>
          {actions.length ? (
            <Stack spacing={1}>
              {actions.slice(0, 6).map((item) => (
                <ListItemButton
                  key={`${item.severity}-${item.title}`}
                  onClick={() => navigate(item.to)}
                  sx={{
                    border: "1px solid",
                    borderColor: "divider",
                    borderRadius: 2,
                    alignItems: "flex-start",
                  }}
                >
                  <ListItemIcon sx={{ minWidth: 36, mt: 0.25 }}>
                    {item.severity === "critical" ? (
                      <ErrorOutlineRounded color="error" fontSize="small" />
                    ) : item.severity === "warning" ? (
                      <WarningAmberRounded color="warning" fontSize="small" />
                    ) : (
                      <FactCheckRounded color="info" fontSize="small" />
                    )}
                  </ListItemIcon>
                  <ListItemText
                    primary={item.title}
                    secondary={item.detail}
                    primaryTypographyProps={{ fontWeight: 700, fontSize: 13.5 }}
                    secondaryTypographyProps={{ fontSize: 11.5 }}
                  />
                  <ChevronRightRounded color="action" fontSize="small" />
                </ListItemButton>
              ))}
            </Stack>
          ) : (
            <Stack alignItems="center" textAlign="center" py={5}>
              <CheckCircleRounded color="success" sx={{ fontSize: 42 }} />
              <Typography fontWeight={750} mt={1.2}>
                즉시 처리할 운영 항목이 없습니다
              </Typography>
              <Typography variant="body2" color="text.secondary" mt={0.4}>
                수집과 관리 Workflow가 정상 범위에 있습니다.
              </Typography>
            </Stack>
          )}
        </Card>
      </Box>
      <Box>
        <Typography variant="h6">빠른 작업</Typography>
        <Typography variant="body2" color="text.secondary" mb={1.5}>
          운영 빈도가 높은 설정과 점검을 바로 시작합니다.
        </Typography>
        <Box
          sx={{
            display: "grid",
            gridTemplateColumns: { xs: "1fr", lg: "repeat(2,minmax(0,1fr))" },
            gap: 1.5,
          }}
        >
          {quickActions.map((item) => (
            <Card
              key={item.title}
              component="button"
              className="action-card"
              onClick={() => navigate(item.to)}
              sx={{ p: 2.2, textAlign: "left", cursor: "pointer" }}
            >
              <Stack direction="row" gap={1.5} alignItems="center">
                <Box
                  sx={{
                    width: 10,
                    height: 44,
                    borderRadius: 2,
                    bgcolor: item.color,
                  }}
                />
                <Box flex={1}>
                  <Typography fontWeight={720}>{item.title}</Typography>
                  <Typography variant="body2" color="text.secondary" mt={0.25}>
                    {item.description}
                  </Typography>
                </Box>
                <ChevronRightRounded color="action" />
              </Stack>
            </Card>
          ))}
        </Box>
      </Box>
      <Card sx={{ p: { xs: 2.2, md: 2.75 } }}>
        <Stack
          direction={{ xs: "column", sm: "row" }}
          justifyContent="space-between"
          gap={1}
          mb={1.5}
        >
          <Box>
            <Typography variant="h6">최근 관리자 활동</Typography>
            <Typography variant="body2" color="text.secondary">
              설정 변경과 운영 작업의 최신 Audit 기록입니다.
            </Typography>
          </Box>
          <Button
            size="small"
            endIcon={<ChevronRightRounded />}
            onClick={() => navigate("/admin?section=audit")}
          >
            전체 감사 로그
          </Button>
        </Stack>
        {(audit.data || []).length ? (
          <Stack divider={<Divider flexItem />}>
            {(audit.data || []).slice(0, 5).map((item) => (
              <Stack
                key={item.id}
                direction={{ xs: "column", sm: "row" }}
                justifyContent="space-between"
                gap={0.5}
                py={1.2}
              >
                <Stack direction="row" gap={1} alignItems="center" minWidth={0}>
                  <Chip size="small" variant="outlined" label={item.action} />
                  <Typography variant="body2" noWrap>
                    {item.resource_type} · {item.actor}
                  </Typography>
                </Stack>
                <Typography variant="caption" color="text.secondary" noWrap>
                  {relativeTime(item.created_at)}
                </Typography>
              </Stack>
            ))}
          </Stack>
        ) : (
          <Typography variant="body2" color="text.secondary" py={3}>
            아직 기록된 관리자 활동이 없습니다.
          </Typography>
        )}
      </Card>
      <Box>
        <Typography variant="h6">고급 운영</Typography>
        <Typography variant="body2" color="text.secondary" mb={1.5}>
          분석 엔지니어링과 개인정보 Workflow를 독립된 작업 공간에서 관리합니다.
        </Typography>
        <Box
          sx={{
            display: "grid",
            gridTemplateColumns: { xs: "1fr", md: "repeat(2,minmax(0,1fr))" },
            gap: 1.5,
          }}
        >
          {platform.map((item) => (
            <ListItemButton
              key={item.to}
              onClick={() => navigate(item.to)}
              sx={{
                bgcolor: "background.paper",
                border: "1px solid",
                borderColor: "divider",
                borderRadius: 2.5,
                py: 1.6,
              }}
            >
              <ListItemText
                primary={item.label}
                secondary={item.description}
                primaryTypographyProps={{ fontWeight: 700 }}
              />
              <ChevronRightRounded color="action" />
            </ListItemButton>
          ))}
        </Box>
      </Box>
    </Stack>
  );
}

function CopyField({ value, label }: { value: string; label?: string }) {
  return (
    <TextField
      fullWidth
      label={label}
      multiline={value.includes("\n")}
      minRows={value.includes("\n") ? 3 : undefined}
      value={value}
      slotProps={{
        input: {
          readOnly: true,
          endAdornment: (
            <InputAdornment position="end">
              <IconButton
                onClick={() => void navigator.clipboard.writeText(value)}
              >
                <ContentCopyRounded />
              </IconButton>
            </InputAdornment>
          ),
        },
      }}
    />
  );
}

interface SiteGuide {
  id: string;
  siteId: string;
  name: string;
  endpoint?: string;
  trackingKey?: string;
  serverAPIKey?: string;
  /** The site's configured session timeout, so the snippet can carry it. */
  sessionTimeoutMinutes?: number;
}

interface TrackingCode {
  site_id: string;
  environment: string;
  collector_endpoint: string;
  tracking_code: string;
}

interface DiagnosticCheck {
  id: string;
  title: string;
  status: "ok" | "warn" | "fail" | "info";
  detail: string;
  action?: string;
}

interface InstallDiagnostics {
  site_id: string;
  environment: string;
  collector_endpoint: string;
  status: "ok" | "warn" | "fail";
  checks: DiagnosticCheck[];
  metrics: Record<string, number | string | null>;
  observed_origins: string[];
  unlisted_origins: string[];
}

const diagnosticColor = {
  ok: "success",
  warn: "warning",
  fail: "error",
  info: "info",
} as const;

function SiteSDKGuideDialog({
  guide,
  close,
}: {
  guide: SiteGuide;
  close(): void;
}) {
  const navigate = useNavigate();
  const [tab, setTab] = useState(0);
  const [environment, setEnvironment] = useState("prd");
  const [mode, setMode] = useState<SDKTrackingMode>("full");
  const [useProxy, setUseProxy] = useState(false);
  const [proxyPath, setProxyPath] = useState("/momento");
  const tracking = useQuery({
    queryKey: ["tracking-code", guide.id, environment],
    queryFn: () =>
      get<TrackingCode>(
        `/api/v1/sites/${guide.id}/tracking-code?environment=${environment}`,
      ),
    enabled: !guide.endpoint,
  });
  const endpoint = guide.endpoint || tracking.data?.collector_endpoint || "";
  const snippet = endpoint
    ? buildSDKSnippet({
        endpoint,
        siteId: guide.siteId,
        environment,
        mode,
        proxyPath: useProxy ? proxyPath : undefined,
        sessionTimeoutMinutes: guide.sessionTimeoutMinutes,
      })
    : "Collector 주소를 확인하고 있습니다…";
  const csp = buildCSPGuidance(endpoint || location.origin, proxyPath);
  const diagnostics = useQuery({
    queryKey: ["install-diagnostics", guide.siteId, environment],
    queryFn: () =>
      get<InstallDiagnostics>(
        `/api/v1/sites/${guide.siteId}/install-diagnostics?environment=${environment}`,
      ),
    enabled: tab === 2,
  });
  const modes: Array<{
    value: SDKTrackingMode;
    label: string;
    description: string;
  }> = [
    {
      value: "full",
      label: "Full",
      description: "허용된 내부 서비스에서 Visitor와 Session을 유지합니다.",
    },
    {
      value: "consent-required",
      label: "동의 필수",
      description: "동의 전에는 아무 이벤트도 수집하지 않습니다.",
    },
    {
      value: "cookieless",
      label: "Cookieless",
      description: "이벤트는 수집하지만 브라우저에 식별자를 저장하지 않습니다.",
    },
  ];

  return (
    <Dialog open maxWidth="md" fullWidth>
      <DialogTitle>
        <Stack direction="row" gap={1.2} alignItems="center">
          <IntegrationInstructionsRounded color="primary" />
          <Box>
            <Typography variant="h6">JavaScript SDK 연동</Typography>
            <Typography variant="caption" color="text.secondary">
              {guide.name} · {guide.siteId}
            </Typography>
          </Box>
        </Stack>
      </DialogTitle>
      <DialogContent dividers>
        {(guide.trackingKey || guide.serverAPIKey) && (
          <Alert severity="warning" sx={{ mb: 2.5 }}>
            <Typography fontWeight={700} mb={1}>
              생성된 키는 이 창을 닫으면 다시 볼 수 없습니다.
            </Typography>
            <Stack spacing={2}>
              {guide.trackingKey && (
                <Box>
                  <Typography variant="caption" color="text.secondary">
                    Tracking Key · 직접 Collector 연동용, 기본 JS SDK는 사용하지
                    않음
                  </Typography>
                  <CopyField value={guide.trackingKey} />
                </Box>
              )}
              {guide.serverAPIKey && (
                <Box>
                  <Typography variant="caption" color="text.secondary">
                    Server API Key · Origin이 없는 서버 전송의 X-Momento-Key
                  </Typography>
                  <CopyField value={guide.serverAPIKey} />
                </Box>
              )}
            </Stack>
          </Alert>
        )}

        <Tabs
          value={tab}
          onChange={(_, value) => setTab(value)}
          variant="scrollable"
          allowScrollButtonsMobile
          sx={{ mb: 2.5 }}
        >
          <Tab label="1. SDK 설치" />
          <Tab label="2. 사용자 · 이벤트" />
          <Tab label="3. 설치 진단" />
          <Tab label="4. 수집 확인" />
        </Tabs>

        {tab === 0 && (
          <Stack spacing={2.2}>
            <Alert severity="info">
              아래 코드를 서비스의 <strong>&lt;head&gt;</strong> 안에 넣으세요.
              Page View, SPA 이동, 클릭, Form, 오류와 Web Vitals가 자동
              수집됩니다. 브라우저 SDK에는 비밀 키를 넣지 않습니다.
            </Alert>
            <Stack direction={{ xs: "column", sm: "row" }} spacing={2}>
              <TextField
                select
                fullWidth
                label="수집 환경"
                value={environment}
                onChange={(event) => setEnvironment(event.target.value)}
                helperText="배포 환경과 Momento 환경 이름을 맞추세요."
              >
                <MenuItem value="dev">dev · Development</MenuItem>
                <MenuItem value="stg">stg · Staging</MenuItem>
                <MenuItem value="prd">prd · Production</MenuItem>
              </TextField>
              <TextField
                select
                fullWidth
                label="개인정보 수집 모드"
                value={mode}
                onChange={(event) =>
                  setMode(event.target.value as SDKTrackingMode)
                }
                helperText={
                  modes.find((item) => item.value === mode)?.description
                }
              >
                {modes.map((item) => (
                  <MenuItem key={item.value} value={item.value}>
                    {item.label}
                  </MenuItem>
                ))}
              </TextField>
            </Stack>
            {tracking.error && (
              <Alert severity="error">
                설치 코드 주소를 가져오지 못했습니다. {tracking.error.message}
              </Alert>
            )}
            <CopyField label="설치 코드" value={snippet} />
            <Card variant="outlined" sx={{ p: 2 }}>
              <Stack
                direction={{ xs: "column", sm: "row" }}
                justifyContent="space-between"
                alignItems={{ sm: "center" }}
                gap={1}
                mb={1}
              >
                <Typography fontWeight={700}>
                  Content-Security-Policy 허용
                </Typography>
                <FormControlLabel
                  control={
                    <Checkbox
                      size="small"
                      checked={useProxy}
                      onChange={(event) => setUseProxy(event.target.checked)}
                    />
                  }
                  label="CSP를 바꿀 수 없어 같은 Origin으로 프록시"
                />
              </Stack>
              <Typography variant="body2" color="text.secondary" mb={1.5}>
                측정 대상 애플리케이션의 CSP가{" "}
                <code>connect-src &apos;self&apos;</code> 수준이면 수집 요청이
                차단됩니다. 아래 정책을 추가하거나, 프록시 방식을 선택해{" "}
                <code>data-endpoint</code>로 first-party 경로를 사용하세요.
              </Typography>
              {useProxy ? (
                <Stack spacing={1.5}>
                  <TextField
                    size="small"
                    label="프록시 경로"
                    value={proxyPath}
                    onChange={(event) => setProxyPath(event.target.value)}
                    helperText="애플리케이션 도메인 아래에서 Collector로 전달할 경로"
                  />
                  <CopyField
                    label="Reverse Proxy 설정 (nginx)"
                    value={csp.proxy}
                  />
                </Stack>
              ) : (
                <Stack spacing={1.5}>
                  <CopyField label="응답 헤더" value={csp.header} />
                  <CopyField label="meta 태그" value={csp.meta} />
                </Stack>
              )}
            </Card>
            <Card variant="outlined" sx={{ p: 2 }}>
              <Typography fontWeight={700} mb={1}>
                운영 배포 시 권장 속성
              </Typography>
              <Typography variant="body2" color="text.secondary">
                배포 영향 분석이 필요하면 script에{" "}
                <code>data-release-version</code>, <code>data-git-sha</code>,{" "}
                <code>data-deployment-id</code>를 추가하세요. 버튼 문구는
                개인정보 위험 때문에 기본 수집하지 않으며, 필요할 때만
                <code> data-collect-element-text=&quot;true&quot;</code>를
                사용하세요.
              </Typography>
              <Typography variant="body2" color="text.secondary" mt={1.5}>
                Session은 브라우저에서 구분하므로, 사이트의 Session Timeout
                설정은 이 스니펫의 <code>data-session-timeout</code>으로
                전달됩니다. 설정을 바꾸면 설치된 스니펫도 갱신해야 적용됩니다.
              </Typography>
              <Typography variant="body2" color="text.secondary" mt={1.5}>
                Rage Click, Dead Click, Rapid Back, Form Retry, Repeated Search,
                Error After Click, Slow Interaction과 사이트 검색은 별도 계측
                없이 자동 감지됩니다. 끄려면{" "}
                <code>data-frustration-signals=&quot;false&quot;</code> 또는{" "}
                <code>data-search-tracking=&quot;false&quot;</code>를
                사용하세요. 검색어 자체는 개인정보가 섞일 수 있어 기본 수집하지
                않으며, 필요할 때만
                <code> data-collect-search-terms=&quot;true&quot;</code>를
                사용하세요. 질의 문자열 이름이 다르면{" "}
                <code>data-search-params=&quot;kw,keyword&quot;</code>로
                지정합니다.
              </Typography>
            </Card>
          </Stack>
        )}

        {tab === 1 && (
          <Stack spacing={2.2}>
            <Alert severity="warning">
              이메일·전화번호·주민번호를 user ID나 속성으로 보내지 마세요. SSO의
              사번을 그대로 쓰기보다 내부 비식별 ID를 권장합니다.
            </Alert>
            <CopyField
              label="사용자 식별과 업무 이벤트 예시"
              value={identifyAndEventExample}
            />
            <Typography variant="body2" color="text.secondary">
              Event 이름과 Property는 관리자 → 이벤트 스키마의 Contract와
              맞추세요. 세션 전체에 필요한 값은 Event Property 대신
              <code> setSessionProperties()</code>에 둡니다.
            </Typography>
            <CopyField
              label="검색·Dead Click 정확도를 높이는 계측 힌트"
              value={signalInstrumentation}
            />
            <Typography variant="body2" color="text.secondary">
              세 속성은 모두 선택 사항입니다. 없어도 검색 횟수와 Frustration
              신호는 수집되지만, 결과 0건 비율과 클릭된 결과 순위는 페이지가
              알려줘야만 알 수 있습니다.
            </Typography>
            {mode === "consent-required" && (
              <CopyField label="동의 배너 연결 예시" value={consentExample} />
            )}
          </Stack>
        )}

        {tab === 2 && (
          <Stack spacing={2}>
            <Stack
              direction="row"
              justifyContent="space-between"
              alignItems="center"
            >
              <Typography variant="body2" color="text.secondary">
                선택한 {environment.toUpperCase()} 환경의 수집 상태를 서버에서
                직접 점검합니다.
              </Typography>
              <Button
                size="small"
                startIcon={<RefreshRounded />}
                onClick={() => void diagnostics.refetch()}
                disabled={diagnostics.isFetching}
              >
                다시 점검
              </Button>
            </Stack>
            {diagnostics.isLoading ? (
              <Loading />
            ) : diagnostics.error ? (
              <ErrorState error={diagnostics.error} />
            ) : diagnostics.data ? (
              <Stack spacing={1.4}>
                <Alert
                  severity={
                    diagnostics.data.status === "ok"
                      ? "success"
                      : diagnostics.data.status === "warn"
                        ? "warning"
                        : "error"
                  }
                >
                  최근 1시간{" "}
                  {String(diagnostics.data.metrics.events_last_hour ?? 0)}건,
                  24시간 {String(diagnostics.data.metrics.events_last_24h ?? 0)}
                  건 수신했습니다.
                </Alert>
                {diagnostics.data.checks.map((check) => (
                  <Card key={check.id} variant="outlined" sx={{ p: 1.8 }}>
                    <Stack
                      direction="row"
                      gap={1.2}
                      alignItems="center"
                      mb={0.5}
                    >
                      <Chip
                        size="small"
                        color={diagnosticColor[check.status]}
                        label={check.status.toUpperCase()}
                      />
                      <Typography fontWeight={700}>{check.title}</Typography>
                    </Stack>
                    <Typography variant="body2" color="text.secondary">
                      {check.detail}
                    </Typography>
                    {check.action && (
                      <Box mt={1}>
                        <CopyField value={check.action} />
                      </Box>
                    )}
                  </Card>
                ))}
                {diagnostics.data.observed_origins.length > 0 && (
                  <Card variant="outlined" sx={{ p: 1.8 }}>
                    <Typography variant="caption" color="text.secondary">
                      최근 24시간 관측된 도메인
                    </Typography>
                    <Stack direction="row" gap={0.5} mt={0.8} flexWrap="wrap">
                      {diagnostics.data.observed_origins.map((origin) => (
                        <Chip
                          key={origin}
                          size="small"
                          label={origin}
                          color={
                            diagnostics.data!.unlisted_origins.includes(origin)
                              ? "warning"
                              : "default"
                          }
                        />
                      ))}
                    </Stack>
                  </Card>
                )}
              </Stack>
            ) : null}
          </Stack>
        )}

        {tab === 3 && (
          <Stack spacing={1.4}>
            {[
              "허용 도메인에 실제 서비스 Host 또는 와일드카드가 등록되어 있는지 확인",
              "측정 대상 애플리케이션의 CSP가 collector Origin을 허용하는지 확인",
              "브라우저 Network에서 tracker.js가 200으로 로드되는지 확인",
              "collect/v1/events 요청이 202 Accepted를 반환하는지 확인",
              "Tracking Debugger에서 선택한 환경의 page_view와 업무 이벤트 확인",
              "운영 전 이벤트 스키마와 PII 정책을 등록하고 Data Quality 경고 확인",
            ].map((item, index) => (
              <Stack
                key={item}
                direction="row"
                spacing={1.2}
                alignItems="flex-start"
              >
                <CheckCircleRounded color="success" fontSize="small" />
                <Typography variant="body2">
                  {index + 1}. {item}
                </Typography>
              </Stack>
            ))}
            <Alert severity="info" sx={{ mt: 1 }}>
              수집이 보이지 않으면 환경 이름, Origin 허용 목록, 브라우저 DNT,
              동의 상태를 먼저 확인하세요. <strong>동의 필수</strong> 모드는
              <code> analytics.consent.grant()</code> 전까지 정상적으로 아무
              이벤트도 보내지 않습니다.
            </Alert>
          </Stack>
        )}
      </DialogContent>
      <DialogActions>
        <Button
          onClick={() => {
            close();
            navigate("/admin?section=debugger");
          }}
          startIcon={<BugReportRounded />}
        >
          Tracking Debugger
        </Button>
        <Button variant="contained" onClick={close}>
          완료
        </Button>
      </DialogActions>
    </Dialog>
  );
}
function SecretDialog({
  title,
  secret,
  recoverable,
  note,
  close,
}: {
  title: string;
  secret: string;
  recoverable?: boolean;
  note?: string;
  close(): void;
}) {
  return (
    <Dialog open onClose={close} maxWidth="sm" fullWidth>
      <DialogTitle>{title}</DialogTitle>
      <DialogContent>
        <Alert severity={recoverable ? "info" : "warning"} sx={{ mb: 2 }}>
          {note ||
            (recoverable
              ? "이 키는 MOMENTO_ENCRYPTION_KEY로 암호화 저장되어 재기동 후에도 다시 조회할 수 있습니다."
              : "이 값은 지금 한 번만 표시됩니다. 안전한 곳에 복사하세요.")}
        </Alert>
        <CopyField value={secret} />
      </DialogContent>
      <DialogActions>
        <Button onClick={close}>확인</Button>
      </DialogActions>
    </Dialog>
  );
}

function SitesAdmin() {
  const qc = useQueryClient();
  const { refresh } = useSite();
  const q = useQuery({
    queryKey: ["admin-sites"],
    queryFn: () => get<Site[]>("/api/v1/sites"),
  });
  const [open, setOpen] = useState(false);
  const [secret, setSecret] = useState<{
    title: string;
    value: string;
    recoverable?: boolean;
    note?: string;
  } | null>(null);
  const [sdkGuide, setSDKGuide] = useState<SiteGuide | null>(null);
  const [name, setName] = useState("");
  const [service, setService] = useState("");
  const [domains, setDomains] = useState("");
  const [timezone, setTimezone] = useState("Asia/Seoul");
  const [engagementThreshold, setEngagementThreshold] = useState(10);
  const [editing, setEditing] = useState<Site | null>(null);
  const create = useMutation({
    mutationFn: () =>
      post<{
        id: string;
        site_id: string;
        name: string;
        collector_endpoint: string;
        tracking_key: string;
        server_api_key: string;
      }>("/api/v1/sites", {
        name,
        service_name: service,
        allowed_domains: domains
          .split(/[\n,]/)
          .map((x) => x.trim())
          .filter(Boolean),
        session_timeout_minutes: 30,
        timezone,
        engagement_threshold_seconds: engagementThreshold,
      }),
    onSuccess: async (d) => {
      setSDKGuide({
        id: d.id,
        siteId: d.site_id,
        name: d.name,
        endpoint: d.collector_endpoint,
        trackingKey: d.tracking_key,
        serverAPIKey: d.server_api_key,
        // A site created here always starts at the default timeout, so the
        // snippet carries no override until an administrator changes it.
      });
      setOpen(false);
      setName("");
      setService("");
      setDomains("");
      await qc.invalidateQueries({ queryKey: ["admin-sites"] });
      await refresh();
    },
  });
  const rotate = useMutation({
    mutationFn: (id: string) =>
      post<{ tracking_key: string; recoverable: boolean }>(
        `/api/v1/sites/${id}/rotate-key`,
      ),
    onSuccess: (d) =>
      setSecret({
        title: "새 Tracking Key",
        value: d.tracking_key,
        recoverable: d.recoverable,
      }),
  });
  const rotateServer = useMutation({
    mutationFn: (id: string) =>
      post<{ server_api_key: string; recoverable: boolean }>(
        `/api/v1/sites/${id}/rotate-server-key`,
      ),
    onSuccess: (d) =>
      setSecret({
        title: "새 Server API Key",
        value: d.server_api_key,
        recoverable: d.recoverable,
      }),
  });
  // Keys are sealed with MOMENTO_ENCRYPTION_KEY, so an administrator can read them
  // again after a restart instead of rotating a key just to see it.
  const reveal = useMutation({
    mutationFn: (id: string) =>
      post<{
        tracking_key?: string;
        server_api_key?: string;
        tracking_key_reason?: string;
        server_api_key_reason?: string;
      }>(`/api/v1/sites/${id}/reveal-keys`),
    onSuccess: (d) => {
      const lines = [
        d.tracking_key
          ? `Tracking Key: ${d.tracking_key}`
          : `Tracking Key: 조회 불가 · ${d.tracking_key_reason || ""}`,
        d.server_api_key
          ? `Server API Key: ${d.server_api_key}`
          : `Server API Key: 조회 불가 · ${d.server_api_key_reason || ""}`,
      ];
      setSecret({
        title: "저장된 사이트 키",
        value: lines.join("\n"),
        recoverable: Boolean(d.tracking_key || d.server_api_key),
        note:
          d.tracking_key || d.server_api_key
            ? "암호화 저장된 키를 복호화해 표시했습니다. 조회 사실은 Audit Log에 기록됩니다."
            : "저장된 키가 없습니다. MOMENTO_ENCRYPTION_KEY를 설정한 뒤 키를 한 번 회전하세요.",
      });
    },
  });
  if (q.isLoading) return <Loading />;
  if (q.error) return <ErrorState error={q.error} retry={() => q.refetch()} />;
  return (
    <>
      <Stack direction="row" justifyContent="space-between" alignItems="center">
        <Box>
          <Typography variant="h6">분석 사이트</Typography>
          <Typography variant="body2" color="text.secondary">
            서비스별 수집 경계와 허용 도메인을 관리합니다.
          </Typography>
        </Box>
        <Button
          variant="contained"
          startIcon={<AddRounded />}
          onClick={() => setOpen(true)}
        >
          사이트 추가
        </Button>
      </Stack>
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: { xs: "1fr", lg: "repeat(2,1fr)" },
          gap: 2,
        }}
      >
        {q.data!.length ? (
          q.data!.map((site) => (
            <Card key={site.id} sx={{ p: 2.5 }}>
              <Stack
                direction={{ xs: "column", sm: "row" }}
                justifyContent="space-between"
                gap={1.5}
              >
                <Box>
                  <Stack direction="row" gap={1} alignItems="center">
                    <Typography fontWeight={720}>{site.name}</Typography>
                    <Chip
                      size="small"
                      color={site.active ? "success" : "default"}
                      label={site.active ? "수집 중" : "중지"}
                    />
                  </Stack>
                  <Typography
                    className="mono"
                    variant="caption"
                    color="primary.main"
                  >
                    {site.site_id}
                  </Typography>
                </Box>
                <Stack
                  direction="row"
                  flexWrap="wrap"
                  justifyContent="flex-end"
                >
                  <Button
                    size="small"
                    startIcon={<IntegrationInstructionsRounded />}
                    onClick={() =>
                      setSDKGuide({
                        id: site.id,
                        siteId: site.site_id,
                        name: site.name,
                        sessionTimeoutMinutes: site.session_timeout_minutes,
                      })
                    }
                  >
                    SDK 설치
                  </Button>
                  <Button
                    size="small"
                    startIcon={<EditOutlined />}
                    onClick={() => setEditing(site)}
                  >
                    설정
                  </Button>
                  <Button
                    size="small"
                    startIcon={<KeyRounded />}
                    onClick={() => reveal.mutate(site.id)}
                    disabled={reveal.isPending}
                  >
                    키 보기
                  </Button>
                  <Button size="small" onClick={() => rotate.mutate(site.id)}>
                    Tracking 키 회전
                  </Button>
                  <Button
                    size="small"
                    startIcon={<KeyRounded />}
                    onClick={() => rotateServer.mutate(site.id)}
                  >
                    Server 키 회전
                  </Button>
                </Stack>
              </Stack>
              <Divider sx={{ my: 2 }} />
              <Stack spacing={1}>
                <Info label="서비스" value={site.service_name || "—"} />
                <Info
                  label="세션 만료"
                  value={`${site.session_timeout_minutes}분`}
                />
                <Info label="기준 시간대" value={site.timezone} />
                <Info
                  label="참여 세션 기준"
                  value={`${site.engagement_threshold_seconds}초 또는 전환 또는 2 Page View`}
                />
                <Info
                  label="Tracking Key"
                  value={`${site.tracking_key_prefix}••••••`}
                />
                <Info
                  label="Server API Key"
                  value={`${site.server_api_key_prefix || "미발급"}••••••`}
                />
                <Box>
                  <Typography variant="caption" color="text.secondary">
                    허용 도메인
                  </Typography>
                  <Stack direction="row" gap={0.5} mt={0.5} flexWrap="wrap">
                    {site.allowed_domains.length ? (
                      site.allowed_domains.map((x) => (
                        <Chip size="small" key={x} label={x} />
                      ))
                    ) : (
                      <Chip
                        size="small"
                        color="warning"
                        label="모든 Origin 허용"
                      />
                    )}
                  </Stack>
                </Box>
              </Stack>
            </Card>
          ))
        ) : (
          <Card sx={{ gridColumn: "1 / -1" }}>
            <Empty
              title="등록된 분석 사이트가 없습니다"
              description="첫 사이트를 만든 뒤 안내되는 SDK 설치 절차를 완료하세요."
              action={
                <Button
                  variant="contained"
                  startIcon={<AddRounded />}
                  onClick={() => setOpen(true)}
                >
                  첫 사이트 만들기
                </Button>
              }
            />
          </Card>
        )}
      </Box>
      <Dialog
        open={open}
        onClose={() => setOpen(false)}
        fullWidth
        maxWidth="sm"
      >
        <DialogTitle>새 분석 사이트</DialogTitle>
        <DialogContent>
          <Stack spacing={2} pt={1}>
            <TextField
              label="사이트 이름"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
            <TextField
              label="서비스 이름"
              value={service}
              onChange={(e) => setService(e.target.value)}
              helperText="사내 사용 현황의 서비스 차원으로 사용합니다."
            />
            <TextField
              label="허용 도메인"
              multiline
              minRows={3}
              value={domains}
              onChange={(e) => setDomains(e.target.value)}
              placeholder={"service.example.com\n*.intranet.example.com"}
              helperText="한 줄에 하나씩 입력하세요. 비워 두면 모든 Origin을 허용합니다."
            />
            {!domains.trim() && (
              <Alert severity="warning">
                도메인을 비우면 모든 Origin에서 이벤트를 보낼 수 있습니다. 운영
                서비스는 실제 Host를 등록하세요.
              </Alert>
            )}
            <TextField
              label="IANA 시간대"
              value={timezone}
              onChange={(e) => setTimezone(e.target.value)}
              placeholder="Asia/Seoul"
              helperText="일별 집계와 날짜 범위의 기준입니다."
            />
            <TextField
              label="참여 기준 시간(초)"
              type="number"
              value={engagementThreshold}
              onChange={(e) => setEngagementThreshold(Number(e.target.value))}
              slotProps={{ htmlInput: { min: 1, max: 300 } }}
            />
            {create.error && (
              <Alert severity="error">{create.error.message}</Alert>
            )}
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setOpen(false)}>취소</Button>
          <Button
            variant="contained"
            onClick={() => create.mutate()}
            disabled={!name || create.isPending}
          >
            생성
          </Button>
        </DialogActions>
      </Dialog>
      {secret && (
        <SecretDialog
          title={secret.title}
          secret={secret.value}
          recoverable={secret.recoverable}
          note={secret.note}
          close={() => setSecret(null)}
        />
      )}
      {sdkGuide && (
        <SiteSDKGuideDialog guide={sdkGuide} close={() => setSDKGuide(null)} />
      )}
      {editing && (
        <SiteSettingsDialog
          site={editing}
          close={() => setEditing(null)}
          saved={async () => {
            setEditing(null);
            await qc.invalidateQueries({ queryKey: ["admin-sites"] });
            await refresh();
          }}
        />
      )}
    </>
  );
}

function SiteSettingsDialog({
  site,
  close,
  saved,
}: {
  site: Site;
  close(): void;
  saved(): Promise<void>;
}) {
  const [name, setName] = useState(site.name);
  const [service, setService] = useState(site.service_name);
  const [domains, setDomains] = useState(site.allowed_domains.join("\n"));
  const [sessionTimeout, setSessionTimeout] = useState(
    site.session_timeout_minutes,
  );
  const [timezone, setTimezone] = useState(site.timezone);
  const [engagementThreshold, setEngagementThreshold] = useState(
    site.engagement_threshold_seconds,
  );
  const update = useMutation({
    mutationFn: () =>
      patch(`/api/v1/sites/${site.id}`, {
        name,
        service_name: service,
        allowed_domains: domains
          .split(/[\n,]/)
          .map((value) => value.trim())
          .filter(Boolean),
        session_timeout_minutes: sessionTimeout,
        timezone,
        engagement_threshold_seconds: engagementThreshold,
        active: site.active,
      }),
    onSuccess: saved,
  });
  return (
    <Dialog open onClose={close} fullWidth maxWidth="sm">
      <DialogTitle>사이트 분석 설정</DialogTitle>
      <DialogContent>
        <Stack spacing={2} pt={1}>
          <TextField
            label="사이트 이름"
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
          <TextField
            label="서비스 이름"
            value={service}
            onChange={(event) => setService(event.target.value)}
          />
          <TextField
            label="허용 도메인"
            multiline
            minRows={3}
            value={domains}
            onChange={(event) => setDomains(event.target.value)}
          />
          <TextField
            label="세션 만료(분)"
            type="number"
            value={sessionTimeout}
            onChange={(event) => setSessionTimeout(Number(event.target.value))}
            slotProps={{ htmlInput: { min: 1, max: 1440 } }}
          />
          <TextField
            label="IANA 시간대"
            value={timezone}
            onChange={(event) => setTimezone(event.target.value)}
            helperText="예: Asia/Seoul, America/New_York"
          />
          <TextField
            label="참여 기준 시간(초)"
            type="number"
            value={engagementThreshold}
            onChange={(event) =>
              setEngagementThreshold(Number(event.target.value))
            }
            slotProps={{ htmlInput: { min: 1, max: 300 } }}
            helperText="이 시간 이상이거나 전환이 있거나 Page View가 2회 이상이면 참여 세션입니다."
          />
          {update.error && (
            <Alert severity="error">{update.error.message}</Alert>
          )}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={close}>취소</Button>
        <Button
          variant="contained"
          onClick={() => update.mutate()}
          disabled={!name || !timezone || update.isPending}
        >
          저장
        </Button>
      </DialogActions>
    </Dialog>
  );
}
function Info({ label, value }: { label: string; value: string }) {
  return (
    <Stack direction="row" justifyContent="space-between">
      <Typography variant="body2" color="text.secondary">
        {label}
      </Typography>
      <Typography variant="body2" fontWeight={620}>
        {value}
      </Typography>
    </Stack>
  );
}

type Settings = Record<
  string,
  { value: Record<string, unknown>; updated_at: string }
>;
function SettingsAdmin({ groups }: { groups: string[] }) {
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["settings"],
    queryFn: () => get<Settings>("/api/v1/settings"),
  });
  const [edits, setEdits] = useState<Record<string, Record<string, unknown>>>(
    {},
  );
  const save = useMutation({
    mutationFn: async () => {
      for (const key of groups) {
        if (edits[key]) await put(`/api/v1/settings/${key}`, edits[key]);
      }
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["settings"] }),
  });
  if (q.isLoading) return <Loading />;
  if (q.error) return <ErrorState error={q.error} />;
  const value = (group: string) => edits[group] || q.data![group]?.value || {};
  const change = (group: string, key: string, next: unknown) =>
    setEdits((v) => ({ ...v, [group]: { ...value(group), [key]: next } }));
  const general = value("general"),
    oidc = value("oidc"),
    storage = value("storage"),
    security = value("security");
  return (
    <>
      <Card sx={{ p: 3 }}>
        <Stack spacing={3}>
          <Section
            title="일반"
            desc="외부에서 접근하는 공개 URL은 OIDC callback과 Tracking Code에 사용합니다."
          >
            <TextField
              label="제품 이름"
              value={String(general.product_name || "")}
              onChange={(e) =>
                change("general", "product_name", e.target.value)
              }
            />
            <TextField
              label="Public URL"
              value={String(general.public_url || "")}
              onChange={(e) => change("general", "public_url", e.target.value)}
              placeholder="https://analytics.company.local"
            />
            <TextField
              label="Timezone"
              value={String(general.timezone || "")}
              onChange={(e) => change("general", "timezone", e.target.value)}
            />
          </Section>
          <Divider />
          <Section
            title="Keycloak / OIDC"
            desc="Issuer URL에서 Discovery 문서를 읽어 자동 연동합니다."
          >
            <FormControlLabel
              control={
                <Checkbox
                  checked={Boolean(oidc.enabled)}
                  onChange={(e) => change("oidc", "enabled", e.target.checked)}
                />
              }
              label="OIDC 로그인 활성화"
            />
            <TextField
              label="Issuer URL"
              value={String(oidc.issuer_url || "")}
              onChange={(e) => change("oidc", "issuer_url", e.target.value)}
              placeholder="https://keycloak.example/realms/company"
            />
            <TextField
              label="Client ID"
              value={String(oidc.client_id || "")}
              onChange={(e) => change("oidc", "client_id", e.target.value)}
            />
            <TextField
              label="Client Secret"
              type="password"
              value={String(oidc.client_secret || "")}
              onChange={(e) => change("oidc", "client_secret", e.target.value)}
            />
            <Box
              sx={{
                display: "grid",
                gridTemplateColumns: { xs: "1fr", sm: "1fr 1fr" },
                gap: 2,
              }}
            >
              {[
                ["claim_email", "Email claim"],
                ["claim_name", "Name claim"],
                ["claim_department", "Department claim"],
                ["claim_organization", "Organization claim"],
              ].map(([k, l]) => (
                <TextField
                  key={k}
                  label={l}
                  value={String(oidc[k] || "")}
                  onChange={(e) => change("oidc", k, e.target.value)}
                />
              ))}
            </Box>
          </Section>
          <Divider />
          <Section
            title="데이터 저장소"
            desc="현재 PostgreSQL이 Raw Event 원본 저장소입니다. ClickHouse는 고도화 시 관리자 전환 항목입니다."
          >
            <TextField
              select
              label="Event storage"
              value={String(storage.engine || "postgres")}
              onChange={(e) => change("storage", "engine", e.target.value)}
            >
              <MenuItem value="postgres">PostgreSQL</MenuItem>
              <MenuItem value="clickhouse" disabled>
                ClickHouse (향후 전환)
              </MenuItem>
            </TextField>
          </Section>
          <Divider />
          <Section
            title="수집 보안"
            desc="Collector가 허용할 요청 크기와 건수를 제한합니다."
          >
            <TextField
              label="분당 요청 한도"
              type="number"
              value={Number(security.collector_rate_limit_per_minute || 6000)}
              onChange={(e) =>
                change(
                  "security",
                  "collector_rate_limit_per_minute",
                  Number(e.target.value),
                )
              }
            />
            <TextField
              label="요청당 최대 이벤트"
              type="number"
              value={Number(security.max_events_per_request || 100)}
              onChange={(e) =>
                change(
                  "security",
                  "max_events_per_request",
                  Number(e.target.value),
                )
              }
            />
            <TextField
              label="최대 Payload 크기 (bytes)"
              type="number"
              value={Number(security.max_payload_bytes || 262144)}
              onChange={(e) =>
                change("security", "max_payload_bytes", Number(e.target.value))
              }
            />
            <TextField
              label="신뢰할 Reverse Proxy CIDR"
              value={((security.trusted_proxy_cidrs as string[]) || []).join(
                ", ",
              )}
              onChange={(e) =>
                change(
                  "security",
                  "trusted_proxy_cidrs",
                  e.target.value
                    .split(",")
                    .map((x) => x.trim())
                    .filter(Boolean),
                )
              }
              helperText="X-Forwarded-For를 신뢰할 프록시 대역만 입력하세요."
            />
            <TextField
              label="콘솔 CSP 추가 연결 Origin"
              value={(
                (security.additional_connect_origins as string[]) || []
              ).join(", ")}
              onChange={(e) =>
                change(
                  "security",
                  "additional_connect_origins",
                  e.target.value
                    .split(",")
                    .map((x) => x.trim())
                    .filter(Boolean),
                )
              }
              helperText="콘솔이 다른 Host의 Collector·Gateway를 호출해야 할 때만 scheme://host 형식으로 입력하세요. Public URL은 자동 허용됩니다."
            />
          </Section>
          <Divider />
          <EncryptionSection />
          {save.error && <Alert severity="error">{save.error.message}</Alert>}
          <Box>
            <Button
              variant="contained"
              startIcon={<SaveRounded />}
              disabled={!Object.keys(edits).length || save.isPending}
              onClick={() => save.mutate()}
            >
              설정 저장
            </Button>
          </Box>
        </Stack>
      </Card>
    </>
  );
}
/**
 * EncryptionSection shows whether generated keys survive a restart and lets an
 * administrator finish an encryption key rotation without a redeploy.
 */
function EncryptionSection() {
  const qc = useQueryClient();
  const status = useQuery({
    queryKey: ["encryption-status"],
    queryFn: () => get<EncryptionStatus>("/api/v1/system/encryption"),
  });
  const rekey = useMutation({
    mutationFn: () =>
      post<{ resealed: number; failed: number }>(
        "/api/v1/system/encryption/rekey",
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["encryption-status"] }),
  });
  return (
    <Section
      title="비밀값 암호화"
      desc="MOMENTO_ENCRYPTION_KEY로 API key, Tracking Key, OIDC Client Secret, Delivery Header를 암호화 저장합니다. 값은 프로세스 환경변수로만 주입하며 콘솔에서 변경할 수 없습니다."
    >
      {status.isLoading ? (
        <LinearProgress />
      ) : status.error ? (
        <ErrorState error={status.error} />
      ) : (
        <Stack spacing={1.5}>
          <Alert severity={status.data?.enabled ? "success" : "warning"}>
            {status.data?.enabled
              ? `${status.data.algorithm} · Key ID ${status.data.key_id} · 복구 가능한 키 ${status.data.recoverable_keys}개`
              : "MOMENTO_ENCRYPTION_KEY가 설정되지 않았습니다. 지금 발급한 키는 재기동 후 다시 조회할 수 없습니다."}
          </Alert>
          {status.data?.enabled && status.data.unrecoverable_keys > 0 && (
            <Alert severity="info">
              암호화 저장 이전에 발급된 키 {status.data.unrecoverable_keys}개는
              한 번 회전해야 다시 조회할 수 있습니다.
            </Alert>
          )}
          {status.data?.enabled && (
            <Stack
              direction={{ xs: "column", sm: "row" }}
              spacing={1.5}
              alignItems={{ sm: "center" }}
            >
              <Button
                variant="outlined"
                startIcon={<KeyRounded />}
                disabled={rekey.isPending || status.data.pending_reseal === 0}
                onClick={() => rekey.mutate()}
              >
                이전 키로 저장된 {status.data.pending_reseal}건 재암호화
              </Button>
              <Typography variant="caption" color="text.secondary">
                MOMENTO_ENCRYPTION_KEY_PREVIOUS에 이전 키를 남긴 상태에서
                실행하고, 완료 후 변수를 제거하십시오.
              </Typography>
            </Stack>
          )}
          {rekey.data && (
            <Alert severity={rekey.data.failed ? "warning" : "success"}>
              {rekey.data.resealed}건을 재암호화했고 {rekey.data.failed}건이
              실패했습니다.
            </Alert>
          )}
          {rekey.error && <Alert severity="error">{rekey.error.message}</Alert>}
        </Stack>
      )}
    </Section>
  );
}

function Section({
  title,
  desc,
  children,
}: {
  title: string;
  desc: string;
  children: ReactNode;
}) {
  return (
    <Box>
      <Typography fontWeight={720}>{title}</Typography>
      <Typography variant="body2" color="text.secondary" mb={2}>
        {desc}
      </Typography>
      <Stack spacing={2}>{children}</Stack>
    </Box>
  );
}

function PrivacyAdmin() {
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["settings"],
    queryFn: () => get<Settings>("/api/v1/settings"),
  });
  const [local, setLocal] = useState<Record<string, unknown> | null>(null);
  const save = useMutation({
    mutationFn: () => put("/api/v1/settings/privacy", local),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["settings"] }),
  });
  if (q.isLoading) return <Loading />;
  const v = local || q.data?.privacy.value || {};
  const set = (k: string, n: unknown) => setLocal({ ...v, [k]: n });
  return (
    <Stack spacing={2}>
      <Card sx={{ p: 3 }}>
        <Section
          title="Privacy by default"
          desc="이 설정은 수집 워커에서 Raw Event를 저장하기 전에 적용됩니다."
        >
          {[
            ["ip_anonymization", "IP 주소 익명화 (/24, /64)"],
            ["collect_user_agent", "User Agent 수집"],
            ["strip_query_string", "URL Query String 전체 제거"],
            ["collect_user_id", "User ID 수집"],
            ["visitor_profiles", "Visitor Profile 활성화"],
            ["do_not_track", "Do Not Track 존중"],
          ].map(([k, l]) => (
            <FormControlLabel
              key={k}
              control={
                <Checkbox
                  checked={Boolean(v[k])}
                  onChange={(e) => set(k, e.target.checked)}
                />
              }
              label={l}
            />
          ))}
          <TextField
            select
            label="PII 값 탐지 정책"
            value={String(v.pii_detection_mode || "mask")}
            onChange={(e) => set("pii_detection_mode", e.target.value)}
            helperText="Property 값에서 이메일·전화번호·주민번호 패턴이 발견될 때의 처리 방식입니다."
          >
            <MenuItem value="detect">Detect · 지표만 기록</MenuItem>
            <MenuItem value="warn">Warn · 저장하고 품질 경고</MenuItem>
            <MenuItem value="mask">Mask · 민감 값을 치환</MenuItem>
            <MenuItem value="reject">Reject · 이벤트 거부</MenuItem>
          </TextField>
          <TextField
            label="마스킹할 URL Parameter"
            value={((v.masked_parameters as string[]) || []).join(", ")}
            onChange={(e) =>
              set(
                "masked_parameters",
                e.target.value
                  .split(",")
                  .map((x) => x.trim())
                  .filter(Boolean),
              )
            }
          />
          <TextField
            label="차단할 Event Property"
            value={((v.blocked_properties as string[]) || []).join(", ")}
            onChange={(e) =>
              set(
                "blocked_properties",
                e.target.value
                  .split(",")
                  .map((x) => x.trim())
                  .filter(Boolean),
              )
            }
          />
          <TextField
            label="Raw Event 보존 개월"
            type="number"
            value={Number(v.raw_event_retention_months || 13)}
            onChange={(e) =>
              set("raw_event_retention_months", Number(e.target.value))
            }
          />
          <TextField
            label="Debugger / Dead Letter 보존 일수"
            type="number"
            value={Number(v.debug_retention_days || 7)}
            onChange={(e) =>
              set("debug_retention_days", Number(e.target.value))
            }
          />
          <Button
            variant="contained"
            startIcon={<SaveRounded />}
            disabled={!local || save.isPending}
            onClick={() => save.mutate()}
          >
            개인정보 설정 저장
          </Button>
        </Section>
      </Card>
      <DataDeletion />
    </Stack>
  );
}

function DataDeletion() {
  const { site } = useSite();
  const [mode, setMode] = useState("visitor");
  const [value, setValue] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [confirm, setConfirm] = useState("");
  const deletion = useMutation({
    mutationFn: () =>
      post<{ deleted_or_updated: number }>("/api/v1/privacy/delete", {
        site_id: site?.site_id,
        mode,
        value,
        from,
        to,
        confirm,
      }),
  });
  return (
    <Card sx={{ p: 3, borderColor: "#F3C9CB" }}>
      <Typography fontWeight={720} color="error.main">
        분석 데이터 삭제
      </Typography>
      <Typography variant="body2" color="text.secondary" mb={2}>
        Visitor, User ID 또는 Event Property 기준 삭제는 되돌릴 수 없으며 감사
        로그에 기록됩니다.
      </Typography>
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: { xs: "1fr", md: "180px 1fr 160px auto" },
          gap: 2,
        }}
      >
        <TextField
          select
          label="삭제 기준"
          value={mode}
          onChange={(e) => setMode(e.target.value)}
        >
          <MenuItem value="visitor">Visitor ID</MenuItem>
          <MenuItem value="user_id">User ID</MenuItem>
          <MenuItem value="property">Event Property</MenuItem>
          <MenuItem value="period">기간</MenuItem>
          <MenuItem value="site">사이트 전체 Event</MenuItem>
        </TextField>
        {mode === "period" ? (
          <Stack direction="row" spacing={1}>
            <TextField
              type="date"
              label="시작일"
              value={from}
              onChange={(e) => setFrom(e.target.value)}
              slotProps={{ inputLabel: { shrink: true } }}
            />
            <TextField
              type="date"
              label="종료일"
              value={to}
              onChange={(e) => setTo(e.target.value)}
              slotProps={{ inputLabel: { shrink: true } }}
            />
          </Stack>
        ) : (
          <TextField
            label={mode === "site" ? "값 불필요" : "대상 값"}
            value={value}
            disabled={mode === "site"}
            onChange={(e) => setValue(e.target.value)}
          />
        )}
        <TextField
          label='확인 문구 "DELETE"'
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
        />
        <Button
          color="error"
          variant="outlined"
          disabled={
            !site ||
            confirm !== "DELETE" ||
            (mode !== "site" && mode !== "period" && !value) ||
            (mode === "period" && (!from || !to)) ||
            deletion.isPending
          }
          onClick={() => deletion.mutate()}
        >
          영구 삭제
        </Button>
      </Box>
      {deletion.error && (
        <Alert severity="error" sx={{ mt: 2 }}>
          {deletion.error.message}
        </Alert>
      )}
      {deletion.data && (
        <Alert severity="success" sx={{ mt: 2 }}>
          {deletion.data.deleted_or_updated}건을 처리했습니다.
        </Alert>
      )}
    </Card>
  );
}

// The account of one unattended retention pass. Without it the screen showed what
// the operator had asked for and never whether it was happening.
interface RetentionRun {
  started_at: string;
  finished_at: string;
  status: "success" | "failed";
  removed: Record<string, number>;
  error: string | null;
}

interface RetentionPolicy {
  raw_event_months: number;
  session_months: number;
  aggregation_months: number | null;
  realtime_hours: number;
  debug_days: number;
}

// A failing pass is the case that matters: it is silent, it is hourly, and the
// disk fills while nothing on any screen changes. So the failure is an error alert
// with its message, not a status word in a table.
function RetentionRunCard({ run }: { run?: RetentionRun | null }) {
  if (run === undefined) return null;
  if (run === null) {
    return (
      <Alert severity="warning">
        보존 작업이 아직 실행된 기록이 없습니다. 서비스 기동 후 1시간 뒤 첫
        작업이 돌고, 그 뒤로는 매시간 실행됩니다.
      </Alert>
    );
  }
  const removed = Object.entries(run.removed).filter(([, count]) => count > 0);
  const total = removed.reduce((sum, [, count]) => sum + count, 0);
  const ran = new Date(run.started_at).toLocaleString("ko-KR");
  const seconds = Math.max(
    0,
    (new Date(run.finished_at).getTime() - new Date(run.started_at).getTime()) /
      1000,
  );
  if (run.status === "failed") {
    return (
      <Alert severity="error">
        <Typography fontWeight={700} variant="body2">
          {ran} 보존 작업이 실패했습니다
        </Typography>
        <Typography variant="caption" display="block" sx={{ mt: 0.5 }}>
          {run.error || "원인이 기록되지 않았습니다"}
        </Typography>
        <Typography variant="caption" display="block" sx={{ mt: 0.5 }}>
          실패 전까지 삭제된 행은 유지됩니다. 다음 작업이 이어서 진행하지만,
          매시간 계속 실패하면 보존기간이 지난 데이터가 남습니다.
        </Typography>
      </Alert>
    );
  }
  return (
    <Alert severity="success">
      <Typography fontWeight={700} variant="body2">
        {ran} 보존 작업 완료 ·{" "}
        {seconds < 1 ? "1초 미만" : `${Math.round(seconds)}초`} ·{" "}
        {total.toLocaleString("ko-KR")}행 삭제
      </Typography>
      <Typography variant="caption" display="block" sx={{ mt: 0.5 }}>
        {removed.length
          ? removed
              .map(
                ([table, count]) => `${table} ${count.toLocaleString("ko-KR")}`,
              )
              .join(" · ")
          : "삭제 대상이 없었습니다. 보존기간이 지난 데이터가 없다는 뜻입니다."}
      </Typography>
    </Alert>
  );
}

function RetentionAdmin() {
  const { site } = useSite();
  const qc = useQueryClient();
  const query = useQuery({
    queryKey: ["retention", site?.site_id],
    queryFn: () =>
      get<{
        policy: RetentionPolicy;
        updated_at?: string;
        last_run?: RetentionRun | null;
      }>(`/api/v1/sites/${site!.site_id}/retention`),
    enabled: !!site,
  });
  const [local, setLocal] = useState<RetentionPolicy | null>(null);
  const current = local || query.data?.policy;
  const save = useMutation({
    mutationFn: () => put(`/api/v1/sites/${site!.site_id}/retention`, current),
    onSuccess: async () => {
      setLocal(null);
      await qc.invalidateQueries({ queryKey: ["retention", site?.site_id] });
    },
  });
  if (!site) return <NoSite />;
  if (query.isLoading) return <Loading />;
  if (query.error) return <ErrorState error={query.error} />;
  const set = (key: keyof RetentionPolicy, value: number | null) =>
    setLocal({ ...query.data!.policy, ...local, [key]: value });
  return (
    <Stack spacing={2}>
      <Alert severity="info">
        정책은 사이트별로 적용됩니다. Raw Event와 물리화 Session은 매시간
        독립적으로 정리되며 집계 보존기간을 비워 두면 무기한입니다.
      </Alert>
      <RetentionRunCard run={query.data?.last_run} />
      <Card sx={{ p: 3 }}>
        <Section
          title={`${site.name} 보존 정책`}
          desc="법무·보안 정책에 맞춰 데이터 종류별 보존기간을 지정합니다."
        >
          <Box
            sx={{
              display: "grid",
              gridTemplateColumns: {
                xs: "1fr",
                md: "repeat(2,1fr)",
                xl: "repeat(3,1fr)",
              },
              gap: 2,
            }}
          >
            <TextField
              label="Raw Event (개월)"
              type="number"
              value={current?.raw_event_months ?? 13}
              onChange={(event) =>
                set("raw_event_months", Number(event.target.value))
              }
              helperText="1~120개월"
            />
            <TextField
              label="Session 요약 (개월)"
              type="number"
              value={current?.session_months ?? 25}
              onChange={(event) =>
                set("session_months", Number(event.target.value))
              }
              helperText="Raw Event 삭제 후에도 유지되는 요약"
            />
            <TextField
              label="Aggregation (개월)"
              type="number"
              value={current?.aggregation_months ?? ""}
              onChange={(event) =>
                set(
                  "aggregation_months",
                  event.target.value ? Number(event.target.value) : null,
                )
              }
              // Two of these tables hold one row per visitor per day, with the
              // visitor and user id on it. Calling them "집계" reads as anonymous
              // and left an operator believing a blank field kept only totals.
              helperText="비워 두면 무기한. 일별 집계 중 방문자·세션 테이블은 Visitor ID와 User ID를 행마다 가지므로, 비워 두면 Raw Event가 삭제된 뒤에도 사람 단위 기록이 남습니다"
            />
            <TextField
              label="Realtime (시간)"
              type="number"
              value={current?.realtime_hours ?? 24}
              onChange={(event) =>
                set("realtime_hours", Number(event.target.value))
              }
              // Kept for API compatibility, and labelled for what it is: Momento
              // keeps no separate realtime store, so there is nothing for this
              // value to trim. Saying so is better than a control that silently
              // does nothing.
              helperText="1~168시간 · 현재 적용되지 않습니다. 별도의 Realtime 저장소가 없어 삭제할 대상이 없습니다"
            />
            <TextField
              label="Debugger / Dead Letter (일)"
              type="number"
              value={current?.debug_days ?? 7}
              onChange={(event) =>
                set("debug_days", Number(event.target.value))
              }
              helperText="1~90일"
            />
          </Box>
          {save.error && <Alert severity="error">{save.error.message}</Alert>}
          <Button
            variant="contained"
            startIcon={<SaveRounded />}
            disabled={!local || save.isPending}
            onClick={() => save.mutate()}
          >
            보존 정책 저장
          </Button>
        </Section>
      </Card>
    </Stack>
  );
}

interface CustomDimension {
  id: string;
  name: string;
  query_name: string;
  property_key: string;
  scope: string;
  data_type: string;
  description: string;
  active: boolean;
}

function DimensionsAdmin() {
  const { site } = useSite();
  const qc = useQueryClient();
  const query = useQuery({
    queryKey: ["dimensions", site?.site_id],
    queryFn: () =>
      get<CustomDimension[]>(`/api/v1/dimensions?site_id=${site!.site_id}`),
    enabled: !!site,
  });
  const [form, setForm] = useState({
    name: "",
    property_key: "",
    scope: "event",
    data_type: "string",
    description: "",
    active: true,
  });
  const save = useMutation({
    mutationFn: () =>
      post("/api/v1/dimensions", { site_id: site!.site_id, ...form }),
    onSuccess: async () => {
      setForm({
        name: "",
        property_key: "",
        scope: "event",
        data_type: "string",
        description: "",
        active: true,
      });
      await qc.invalidateQueries({ queryKey: ["dimensions", site?.site_id] });
    },
  });
  const remove = useMutation({
    mutationFn: (id: string) => del(`/api/v1/dimensions/${id}`),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: ["dimensions", site?.site_id] }),
  });
  if (!site) return <NoSite />;
  if (query.isLoading) return <Loading />;
  if (query.error) return <ErrorState error={query.error} />;
  return (
    <Box
      sx={{
        display: "grid",
        gridTemplateColumns: { xs: "1fr", lg: "380px 1fr" },
        gap: 2,
      }}
    >
      <Card sx={{ p: 2.5, height: "fit-content" }}>
        <Typography fontWeight={720}>Custom Dimension Registry</Typography>
        <Typography variant="body2" color="text.secondary" mb={2}>
          Event Property에 분석 이름과 Scope를 부여합니다. 저장 후 Query
          Builder에서 custom.이름으로 사용합니다.
        </Typography>
        <Stack spacing={2}>
          <TextField
            label="Dimension 이름"
            value={form.name}
            onChange={(event) =>
              setForm({ ...form, name: event.target.value.toLowerCase() })
            }
            placeholder="membership"
          />
          <TextField
            label="Property key"
            value={form.property_key}
            onChange={(event) =>
              setForm({ ...form, property_key: event.target.value })
            }
            placeholder="membership"
          />
          <TextField
            select
            label="Scope"
            value={form.scope}
            onChange={(event) =>
              setForm({ ...form, scope: event.target.value })
            }
          >
            {[
              ["user", "User"],
              ["session", "Session"],
              ["event", "Event"],
              ["item", "Item (Ecommerce)"],
            ].map(([value, label]) => (
              <MenuItem key={value} value={value}>
                {label}
              </MenuItem>
            ))}
          </TextField>
          <TextField
            select
            label="Data type"
            value={form.data_type}
            onChange={(event) =>
              setForm({ ...form, data_type: event.target.value })
            }
          >
            {["string", "number", "boolean", "date"].map((value) => (
              <MenuItem key={value} value={value}>
                {value}
              </MenuItem>
            ))}
          </TextField>
          <TextField
            label="설명"
            value={form.description}
            onChange={(event) =>
              setForm({ ...form, description: event.target.value })
            }
          />
          <FormControlLabel
            control={
              <Checkbox
                checked={form.active}
                onChange={(event) =>
                  setForm({ ...form, active: event.target.checked })
                }
              />
            }
            label="활성화"
          />
          {save.error && <Alert severity="error">{save.error.message}</Alert>}
          <Button
            variant="contained"
            onClick={() => save.mutate()}
            disabled={!form.name || !form.property_key || save.isPending}
          >
            등록 또는 갱신
          </Button>
        </Stack>
      </Card>
      <DataTable
        columns={[
          { key: "query_name", label: "Query Dimension" },
          { key: "property_key", label: "Property" },
          {
            key: "scope",
            label: "Scope",
            format: (value) => <Chip size="small" label={String(value)} />,
          },
          { key: "data_type", label: "Type" },
          { key: "description", label: "설명" },
          {
            key: "active",
            label: "상태",
            format: (value) => (value ? "활성" : "비활성"),
          },
          {
            key: "id",
            label: "관리",
            format: (value, row) => (
              <Stack direction="row" gap={0.5}>
                <Button
                  size="small"
                  onClick={() =>
                    setForm({
                      name: String(row.name),
                      property_key: String(row.property_key),
                      scope: String(row.scope),
                      data_type: String(row.data_type),
                      description: String(row.description || ""),
                      active: Boolean(row.active),
                    })
                  }
                >
                  편집
                </Button>
                <IconButton
                  size="small"
                  color="error"
                  onClick={() => remove.mutate(String(value))}
                >
                  <DeleteOutlineRounded fontSize="small" />
                </IconButton>
              </Stack>
            ),
          },
        ]}
        rows={(query.data || []) as unknown as Record<string, unknown>[]}
      />
    </Box>
  );
}

interface Network {
  id: string;
  name: string;
  cidr: string;
  description: string;
  internal: boolean;
  created_at: string;
}
function NetworksAdmin() {
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["networks"],
    queryFn: () => get<Network[]>("/api/v1/networks"),
  });
  const [form, setForm] = useState({ name: "", cidr: "", description: "" });
  const create = useMutation({
    mutationFn: () => post("/api/v1/networks", { ...form, internal: true }),
    onSuccess: () => {
      setForm({ name: "", cidr: "", description: "" });
      qc.invalidateQueries({ queryKey: ["networks"] });
    },
  });
  const remove = useMutation({
    mutationFn: (id: string) => del(`/api/v1/networks/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["networks"] }),
  });
  if (q.isLoading) return <Loading />;
  return (
    <Box
      sx={{
        display: "grid",
        gridTemplateColumns: { xs: "1fr", lg: "360px 1fr" },
        gap: 2,
      }}
    >
      <Card sx={{ p: 2.5, height: "fit-content" }}>
        <Typography fontWeight={720}>망 대역 추가</Typography>
        <Typography variant="body2" color="text.secondary" mb={2}>
          C 클래스는 예: 10.20.30.0/24로 입력합니다.
        </Typography>
        <Stack spacing={2}>
          <TextField
            label="망 구분 명"
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            placeholder="본사 업무망"
          />
          <TextField
            label="CIDR"
            value={form.cidr}
            onChange={(e) => setForm({ ...form, cidr: e.target.value })}
            placeholder="10.20.30.0/24"
          />
          <TextField
            label="설명"
            value={form.description}
            onChange={(e) => setForm({ ...form, description: e.target.value })}
          />
          {create.error && (
            <Alert severity="error">{create.error.message}</Alert>
          )}
          <Button
            variant="contained"
            onClick={() => create.mutate()}
            disabled={!form.name || !form.cidr}
          >
            추가
          </Button>
        </Stack>
      </Card>
      <DataTable
        columns={[
          { key: "name", label: "망 구분" },
          {
            key: "cidr",
            label: "CIDR",
            format: (v) => (
              <Typography className="mono" variant="body2">
                {String(v)}
              </Typography>
            ),
          },
          { key: "description", label: "설명" },
          {
            key: "internal",
            label: "분류",
            format: (v) => (
              <Chip
                size="small"
                label={v ? "Internal" : "External"}
                color={v ? "success" : "default"}
              />
            ),
          },
          {
            key: "id",
            label: "",
            align: "right",
            format: (v) => (
              <IconButton
                size="small"
                color="error"
                onClick={() => remove.mutate(String(v))}
              >
                <DeleteOutlineRounded />
              </IconButton>
            ),
          },
        ]}
        rows={q.data as unknown as Record<string, unknown>[]}
      />
    </Box>
  );
}

interface AdminUser {
  id: string;
  email: string;
  display_name: string;
  department: string;
  organization_name: string;
  role: string;
  active: boolean;
  oidc: boolean;
}
function UsersAdmin() {
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["users"],
    queryFn: () => get<AdminUser[]>("/api/v1/users"),
  });
  const [open, setOpen] = useState(false);
  const [edit, setEdit] = useState<AdminUser | null>(null);
  const [form, setForm] = useState({
    email: "",
    display_name: "",
    department: "",
    organization_name: "",
    role: "viewer",
    password: "",
  });
  const create = useMutation({
    mutationFn: () => post("/api/v1/users", form),
    onSuccess: () => {
      setOpen(false);
      qc.invalidateQueries({ queryKey: ["users"] });
    },
  });
  const update = useMutation({
    mutationFn: () =>
      patch(`/api/v1/users/${edit!.id}`, {
        display_name: edit!.display_name,
        department: edit!.department,
        organization_name: edit!.organization_name,
        role: edit!.role,
        active: edit!.active,
      }),
    onSuccess: () => {
      setEdit(null);
      qc.invalidateQueries({ queryKey: ["users"] });
    },
  });
  if (q.isLoading) return <Loading />;
  return (
    <>
      <Stack direction="row" justifyContent="space-between">
        <Typography variant="h6">사용자 및 RBAC</Typography>
        <Button
          variant="contained"
          startIcon={<AddRounded />}
          onClick={() => setOpen(true)}
        >
          사용자 추가
        </Button>
      </Stack>
      <DataTable
        columns={[
          { key: "display_name", label: "사용자" },
          { key: "email", label: "이메일" },
          { key: "department", label: "부서" },
          { key: "organization_name", label: "조직" },
          {
            key: "role",
            label: "권한",
            format: (v) => <Chip size="small" label={String(v)} />,
          },
          { key: "oidc", label: "인증", format: (v) => (v ? "OIDC" : "Local") },
          {
            key: "active",
            label: "상태",
            format: (v) => (
              <Chip
                size="small"
                color={v ? "success" : "default"}
                label={v ? "활성" : "중지"}
              />
            ),
          },
          {
            key: "id",
            label: "",
            align: "right",
            format: (_, row) => (
              <IconButton
                size="small"
                title="사용자 편집"
                onClick={() => setEdit(row as unknown as AdminUser)}
              >
                <EditOutlined />
              </IconButton>
            ),
          },
        ]}
        rows={q.data as unknown as Record<string, unknown>[]}
      />
      <Dialog
        open={open}
        onClose={() => setOpen(false)}
        fullWidth
        maxWidth="sm"
      >
        <DialogTitle>사용자 추가</DialogTitle>
        <DialogContent>
          <Stack spacing={2} pt={1}>
            <TextField
              label="이메일"
              value={form.email}
              onChange={(e) => setForm({ ...form, email: e.target.value })}
            />
            <TextField
              label="표시 이름"
              value={form.display_name}
              onChange={(e) =>
                setForm({ ...form, display_name: e.target.value })
              }
            />
            <Box
              sx={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 2 }}
            >
              <TextField
                label="부서"
                value={form.department}
                onChange={(e) =>
                  setForm({ ...form, department: e.target.value })
                }
              />
              <TextField
                label="조직"
                value={form.organization_name}
                onChange={(e) =>
                  setForm({ ...form, organization_name: e.target.value })
                }
              />
            </Box>
            <TextField
              select
              label="권한"
              value={form.role}
              onChange={(e) => setForm({ ...form, role: e.target.value })}
            >
              {[
                "viewer",
                "analyst",
                "workspace_admin",
                "organization_admin",
                "super_admin",
              ].map((x) => (
                <MenuItem key={x} value={x}>
                  {x}
                </MenuItem>
              ))}
            </TextField>
            <TextField
              label="초기 비밀번호"
              type="password"
              value={form.password}
              onChange={(e) => setForm({ ...form, password: e.target.value })}
              helperText="12자 이상"
            />
            {create.error && (
              <Alert severity="error">{create.error.message}</Alert>
            )}
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setOpen(false)}>취소</Button>
          <Button
            variant="contained"
            disabled={!form.email || form.password.length < 12}
            onClick={() => create.mutate()}
          >
            생성
          </Button>
        </DialogActions>
      </Dialog>
      <Dialog
        open={!!edit}
        onClose={() => setEdit(null)}
        fullWidth
        maxWidth="sm"
      >
        <DialogTitle>사용자 권한 편집</DialogTitle>
        <DialogContent>
          {edit && (
            <Stack spacing={2} pt={1}>
              <TextField label="이메일" value={edit.email} disabled />
              <TextField
                label="표시 이름"
                value={edit.display_name}
                onChange={(e) =>
                  setEdit({ ...edit, display_name: e.target.value })
                }
              />
              <Box
                sx={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 2 }}
              >
                <TextField
                  label="부서"
                  value={edit.department}
                  onChange={(e) =>
                    setEdit({ ...edit, department: e.target.value })
                  }
                />
                <TextField
                  label="조직"
                  value={edit.organization_name}
                  onChange={(e) =>
                    setEdit({ ...edit, organization_name: e.target.value })
                  }
                />
              </Box>
              <TextField
                select
                label="권한"
                value={edit.role}
                onChange={(e) => setEdit({ ...edit, role: e.target.value })}
              >
                {[
                  "viewer",
                  "analyst",
                  "workspace_admin",
                  "organization_admin",
                  "super_admin",
                ].map((x) => (
                  <MenuItem key={x} value={x}>
                    {x}
                  </MenuItem>
                ))}
              </TextField>
              <FormControlLabel
                control={
                  <Checkbox
                    checked={edit.active}
                    onChange={(e) =>
                      setEdit({ ...edit, active: e.target.checked })
                    }
                  />
                }
                label="계정 활성화"
              />
              {update.error && (
                <Alert severity="error">{update.error.message}</Alert>
              )}
            </Stack>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setEdit(null)}>취소</Button>
          <Button
            variant="contained"
            onClick={() => update.mutate()}
            disabled={update.isPending}
          >
            저장
          </Button>
        </DialogActions>
      </Dialog>
    </>
  );
}

interface Schema {
  id: string;
  site_id: string;
  name: string;
  description: string;
  schema: Record<string, unknown>;
  validation_mode: string;
  conversion: boolean;
}
function SchemasAdmin() {
  const qc = useQueryClient();
  const { site } = useSite();
  const q = useQuery({
    queryKey: ["schemas", site?.site_id],
    queryFn: () =>
      get<Schema[]>(`/api/v1/event-definitions?site_id=${site?.site_id || ""}`),
  });
  const [form, setForm] = useState({
    name: "",
    description: "",
    validation_mode: "warn",
    conversion: false,
    schemaText: '{"properties": {}}',
  });
  const save = useMutation({
    mutationFn: () =>
      post("/api/v1/event-definitions", {
        site_id: site?.site_id,
        ...form,
        schema: JSON.parse(form.schemaText),
      }),
    onSuccess: () => {
      setForm({ ...form, name: "", description: "" });
      qc.invalidateQueries({ queryKey: ["schemas"] });
    },
  });
  if (q.isLoading) return <Loading />;
  return (
    <Box
      sx={{
        display: "grid",
        gridTemplateColumns: { xs: "1fr", lg: "380px 1fr" },
        gap: 2,
      }}
    >
      <Card sx={{ p: 2.5, height: "fit-content" }}>
        <Typography fontWeight={720}>Event Schema</Typography>
        <Typography variant="body2" color="text.secondary" mb={2}>
          이벤트 규격과 전환 여부를 등록합니다.
        </Typography>
        <Stack spacing={2}>
          <TextField
            label="Event name"
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
          />
          <TextField
            label="설명"
            value={form.description}
            onChange={(e) => setForm({ ...form, description: e.target.value })}
          />
          <TextField
            select
            label="정책"
            value={form.validation_mode}
            onChange={(e) =>
              setForm({ ...form, validation_mode: e.target.value })
            }
          >
            {["allow", "warn", "reject"].map((x) => (
              <MenuItem key={x} value={x}>
                {x}
              </MenuItem>
            ))}
          </TextField>
          <FormControlLabel
            control={
              <Checkbox
                checked={form.conversion}
                onChange={(e) =>
                  setForm({ ...form, conversion: e.target.checked })
                }
              />
            }
            label="Conversion으로 지정"
          />
          <TextField
            label="JSON Schema"
            multiline
            minRows={5}
            className="mono"
            value={form.schemaText}
            onChange={(e) => setForm({ ...form, schemaText: e.target.value })}
          />
          {save.error && <Alert severity="error">{save.error.message}</Alert>}
          <Button
            variant="contained"
            onClick={() => save.mutate()}
            disabled={!site || !form.name}
          >
            저장
          </Button>
        </Stack>
      </Card>
      <DataTable
        columns={[
          { key: "name", label: "이벤트" },
          { key: "description", label: "설명" },
          {
            key: "validation_mode",
            label: "정책",
            format: (v) => <Chip size="small" label={String(v)} />,
          },
          {
            key: "conversion",
            label: "전환",
            format: (v) => (v ? "Yes" : "—"),
          },
        ]}
        rows={(q.data || []) as unknown as Record<string, unknown>[]}
      />
    </Box>
  );
}

function DebuggerAdmin() {
  const { site } = useSite();
  const q = useQuery({
    queryKey: ["tracking-debugger", site?.site_id],
    queryFn: () =>
      get<{
        events: Record<string, unknown>[];
        errors: Record<string, unknown>[];
      }>(`/api/v1/tracking-debugger?site_id=${site?.site_id || ""}`),
    refetchInterval: 5000,
  });
  if (q.isLoading) return <Loading />;
  if (q.error) return <ErrorState error={q.error} />;
  return (
    <Stack spacing={2}>
      <Alert severity="info">
        최근 수신 이벤트를 5초마다 갱신합니다. 원문 Property는 개인정보 필터가
        적용된 이후의 값입니다.
      </Alert>
      {!!q.data?.errors.length && (
        <DataTable
          columns={[
            { key: "receipt_id", label: "Receipt" },
            { key: "attempts", label: "재시도", align: "right" },
            { key: "error", label: "처리 오류" },
            {
              key: "created_at",
              label: "수신 시각",
              format: (v) => new Date(String(v)).toLocaleString("ko-KR"),
            },
          ]}
          rows={q.data.errors}
        />
      )}
      <DataTable
        columns={[
          {
            key: "received_at",
            label: "수신 시각",
            format: (v) => new Date(String(v)).toLocaleTimeString("ko-KR"),
          },
          {
            key: "event_name",
            label: "이벤트",
            format: (v) => <Chip size="small" label={String(v)} />,
          },
          { key: "visitor_id", label: "Visitor" },
          { key: "page_url", label: "페이지" },
          { key: "network", label: "망 구분" },
          { key: "client_ip", label: "익명화 IP" },
          { key: "traffic_class", label: "트래픽" },
        ]}
        rows={q.data?.events || []}
      />
    </Stack>
  );
}

function AuditAdmin() {
  const q = useQuery({
    queryKey: ["audit"],
    queryFn: () => get<Record<string, unknown>[]>("/api/v1/audit"),
  });
  if (q.isLoading) return <Loading />;
  if (q.error) return <ErrorState error={q.error} />;
  return (
    <DataTable
      columns={[
        {
          key: "created_at",
          label: "시각",
          format: (v) => new Date(String(v)).toLocaleString("ko-KR"),
        },
        { key: "actor", label: "작업자" },
        {
          key: "action",
          label: "작업",
          format: (v) => (
            <Chip size="small" variant="outlined" label={String(v)} />
          ),
        },
        { key: "resource_type", label: "대상" },
        { key: "resource_id", label: "대상 ID" },
        { key: "client_ip", label: "IP" },
      ]}
      rows={q.data!}
    />
  );
}
