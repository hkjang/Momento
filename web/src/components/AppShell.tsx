import { useEffect, useMemo, useState, type ReactNode } from "react";
import {
  AppBar,
  Avatar,
  Box,
  Breadcrumbs,
  Chip,
  Collapse,
  Dialog,
  DialogContent,
  Divider,
  Drawer,
  FormControl,
  IconButton,
  List,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  Menu,
  MenuItem,
  Select,
  Stack,
  Toolbar,
  Tooltip,
  Typography,
  useMediaQuery,
  useTheme,
} from "@mui/material";
import AccountTreeOutlined from "@mui/icons-material/AccountTreeOutlined";
import AdsClickOutlined from "@mui/icons-material/AdsClickOutlined";
import ArticleOutlined from "@mui/icons-material/ArticleOutlined";
import BoltRounded from "@mui/icons-material/BoltRounded";
import BusinessRounded from "@mui/icons-material/BusinessRounded";
import CalendarMonthRounded from "@mui/icons-material/CalendarMonthRounded";
import ChevronRightRounded from "@mui/icons-material/ChevronRightRounded";
import DashboardRounded from "@mui/icons-material/DashboardRounded";
import ExpandMoreRounded from "@mui/icons-material/ExpandMoreRounded";
import ExploreRounded from "@mui/icons-material/ExploreRounded";
import FilterAltOutlined from "@mui/icons-material/FilterAltOutlined";
import HealthAndSafetyRounded from "@mui/icons-material/HealthAndSafetyRounded";
import InsightsRounded from "@mui/icons-material/InsightsRounded";
import KeyRounded from "@mui/icons-material/KeyRounded";
import LogoutRounded from "@mui/icons-material/LogoutRounded";
import ManageAccountsRounded from "@mui/icons-material/ManageAccountsRounded";
import MenuRounded from "@mui/icons-material/MenuRounded";
import MouseOutlined from "@mui/icons-material/MouseOutlined";
import PeopleAltOutlined from "@mui/icons-material/PeopleAltOutlined";
import ScheduleOutlined from "@mui/icons-material/ScheduleOutlined";
import PersonOutlineRounded from "@mui/icons-material/PersonOutlineRounded";
import PsychologyRounded from "@mui/icons-material/PsychologyRounded";
import SearchRounded from "@mui/icons-material/SearchRounded";
import SettingsOutlined from "@mui/icons-material/SettingsOutlined";
import TuneRounded from "@mui/icons-material/TuneRounded";
import VisibilityOutlined from "@mui/icons-material/VisibilityOutlined";
import { NavLink, useLocation, useNavigate } from "react-router-dom";
import { Logo } from "./Logo";
import { useAuth } from "../contexts/AuthContext";
import { useSite } from "../contexts/SiteContext";
import { consoleVersion, shortCommit, useRuntimeVersion } from "../version";

const drawerWidth = 272;

type NavItem = {
  to: string;
  label: string;
  description: string;
  icon: ReactNode;
  keywords?: string;
  adminOnly?: boolean;
};

type NavGroup = {
  label: string;
  icon: ReactNode;
  items: NavItem[];
};

const navGroups: NavGroup[] = [
  {
    label: "모니터링",
    icon: <DashboardRounded />,
    items: [
      {
        to: "/",
        label: "개요",
        description: "핵심 KPI와 변화 추이",
        icon: <DashboardRounded />,
        keywords: "overview dashboard kpi",
      },
      {
        to: "/realtime",
        label: "실시간",
        description: "최근 30분 활성 사용자",
        icon: <BoltRounded />,
        keywords: "realtime active",
      },
      {
        to: "/workspace",
        label: "전사 Roll-Up",
        description: "서비스별 Score와 전사 사용자",
        icon: <BusinessRounded />,
        keywords: "workspace service score",
      },
      {
        to: "/change-calendar",
        label: "변경 캘린더",
        description: "배포·장애·교육 Annotation",
        icon: <CalendarMonthRounded />,
        keywords: "release deployment incident",
      },
      {
        to: "/data-quality",
        label: "데이터 품질",
        description: "수집 계약·PII·Cardinality 상태",
        icon: <HealthAndSafetyRounded />,
        keywords: "quality schema pii",
      },
    ],
  },
  {
    label: "웹 분석",
    icon: <VisibilityOutlined />,
    items: [
      {
        to: "/acquisition",
        label: "유입",
        description: "소스·캠페인·Referrer",
        icon: <ExploreRounded />,
      },
      {
        to: "/pages",
        label: "페이지",
        description: "콘텐츠 조회와 전환",
        icon: <ArticleOutlined />,
      },
      {
        to: "/events",
        label: "이벤트",
        description: "행동 이벤트와 Parameter",
        icon: <MouseOutlined />,
      },
      {
        to: "/visitors",
        label: "사용자",
        description: "방문자와 식별 사용자",
        icon: <PeopleAltOutlined />,
      },
      {
        to: "/sessions",
        label: "세션",
        description: "세션별 참여·전환과 유입",
        icon: <ScheduleOutlined />,
      },
      {
        to: "/user-explorer",
        label: "User Explorer",
        description: "사용자별 Event Timeline",
        icon: <PersonOutlineRounded />,
      },
      {
        to: "/ecommerce",
        label: "Ecommerce",
        description: "매출·상품·구매 전환",
        icon: <AdsClickOutlined />,
      },
    ],
  },
  {
    label: "제품 분석",
    icon: <InsightsRounded />,
    items: [
      {
        to: "/usage",
        label: "사내 사용 현황",
        description: "조직·부서·망별 사용량",
        icon: <BusinessRounded />,
      },
      {
        to: "/adoption",
        label: "Feature Adoption",
        description: "조직별 기능 도입과 재사용",
        icon: <InsightsRounded />,
      },
      {
        to: "/features",
        label: "Feature Intelligence",
        description: "Feature Score와 Dead Feature",
        icon: <TuneRounded />,
      },
      {
        to: "/cohort",
        label: "Cohort · Retention",
        description: "최초 행동 이후 재방문",
        icon: <CalendarMonthRounded />,
      },
      {
        to: "/journey",
        label: "Business Journey",
        description: "업무 결과까지 이어지는 흐름",
        icon: <AccountTreeOutlined />,
      },
      {
        to: "/goals",
        label: "Metric Goals",
        description: "KPI 목표와 달성 상태",
        icon: <AdsClickOutlined />,
      },
    ],
  },
  {
    label: "탐색 · 실험",
    icon: <ExploreRounded />,
    items: [
      {
        to: "/explorer",
        label: "자유 분석",
        description: "Dimension과 Metric 조합",
        icon: <TuneRounded />,
      },
      {
        to: "/segments",
        label: "Segment",
        description: "중첩 조건 대상 정의",
        icon: <FilterAltOutlined />,
      },
      {
        to: "/funnel",
        label: "퍼널",
        description: "단계별 전환과 이탈",
        icon: <FilterAltOutlined />,
      },
      {
        to: "/path",
        label: "경로",
        description: "사용자의 주요 이동 흐름",
        icon: <AccountTreeOutlined />,
      },
      {
        to: "/experiments",
        label: "Experiment",
        description: "Variant Lift와 Confidence",
        icon: <AdsClickOutlined />,
      },
    ],
  },
  {
    label: "경험 · AI",
    icon: <PsychologyRounded />,
    items: [
      {
        to: "/experience",
        label: "Web Vitals · 오류",
        description: "성능과 오류의 전환 영향",
        icon: <HealthAndSafetyRounded />,
      },
      {
        to: "/search-analytics",
        label: "Search Analytics",
        description: "검색 성공과 Zero Result",
        icon: <SearchRounded />,
      },
      {
        to: "/frustration",
        label: "Frustration",
        description: "막힘·재시도·Rage Click",
        icon: <BoltRounded />,
      },
      {
        to: "/insights",
        label: "Insight · 자연어",
        description: "변화 탐지와 Root Cause",
        icon: <InsightsRounded />,
      },
      {
        to: "/ai-analytics",
        label: "AI · Agent · MCP",
        description: "모델·도구의 효과와 비용",
        icon: <PsychologyRounded />,
      },
    ],
  },
];

const adminCommands: NavItem[] = [
  {
    to: "/admin",
    label: "관리 센터",
    description: "운영 설정과 관리 작업의 시작점",
    icon: <SettingsOutlined />,
    adminOnly: true,
    keywords: "admin settings",
  },
  {
    to: "/admin?section=sites",
    label: "사이트 관리",
    description: "사이트·도메인·Tracking Key",
    icon: <BusinessRounded />,
    adminOnly: true,
  },
  {
    to: "/admin?section=settings",
    label: "SSO · 일반 설정",
    description: "Keycloak OIDC와 공통 설정",
    icon: <ManageAccountsRounded />,
    adminOnly: true,
  },
  {
    to: "/admin?section=privacy",
    label: "개인정보 정책",
    description: "PII·Consent·수집 정책",
    icon: <HealthAndSafetyRounded />,
    adminOnly: true,
  },
  {
    to: "/admin?section=networks",
    label: "네트워크 망",
    description: "CIDR별 사내망 이름",
    icon: <AccountTreeOutlined />,
    adminOnly: true,
  },
  {
    to: "/admin?section=users",
    label: "사용자 · 권한",
    description: "RBAC와 Workspace 권한",
    icon: <PeopleAltOutlined />,
    adminOnly: true,
  },
  {
    to: "/admin/governance",
    label: "Analytics Governance",
    description: "환경·Contract·Semantic Metric",
    icon: <TuneRounded />,
    adminOnly: true,
  },
  {
    to: "/admin/analytics-engineering",
    label: "Analytics Engineering",
    description: "Goal·Query Cost·Aggregate·Lineage",
    icon: <AccountTreeOutlined />,
    adminOnly: true,
  },
  {
    to: "/admin/product-lab",
    label: "Feature Flag · Lab",
    description: "Feature Flag와 Experiment 계약",
    icon: <AdsClickOutlined />,
    adminOnly: true,
  },
  {
    to: "/admin/privacy-requests",
    label: "Privacy Requests",
    description: "삭제·Export 승인 Workflow",
    icon: <HealthAndSafetyRounded />,
    adminOnly: true,
  },
  {
    to: "/admin/automation",
    label: "Report · Action",
    description: "Scheduled Report와 Webhook",
    icon: <BoltRounded />,
    adminOnly: true,
  },
];

const titles: Record<string, [string, string]> = {
  "/": ["개요", "서비스 사용 흐름을 한눈에 확인합니다."],
  "/realtime": ["실시간", "최근 30분의 활성 사용자와 이벤트입니다."],
  "/usage": ["사내 사용 현황", "조직·부서·서비스·기능·버튼·망별 사용량입니다."],
  "/acquisition": ["유입 분석", "소스와 캠페인이 만든 방문을 확인합니다."],
  "/pages": ["페이지", "콘텐츠별 조회와 전환 성과입니다."],
  "/events": ["이벤트", "수집된 행동과 주요 이벤트를 탐색합니다."],
  "/visitors": ["사용자", "익명 방문자와 식별 사용자의 활동입니다."],
  "/user-explorer": [
    "User Explorer",
    "Visitor 단위 Event Timeline을 확인합니다.",
  ],
  "/ecommerce": ["Ecommerce", "매출·거래·상품과 구매 전환을 분석합니다."],
  "/explorer": ["자유 분석", "Dimension과 Metric을 조합해 분석합니다."],
  "/segments": ["Segment", "중첩 AND/OR 조건으로 분석 대상을 정의합니다."],
  "/funnel": ["퍼널", "단계별 전환과 이탈을 분석합니다."],
  "/path": ["경로", "사용자가 이동한 주요 흐름입니다."],
  "/adoption": [
    "Feature Adoption",
    "조직·부서별 기능 도입률과 재사용을 분석합니다.",
  ],
  "/cohort": [
    "Cohort · Retention",
    "최초 행동 이후 사용자가 다시 돌아오는 비율입니다.",
  ],
  "/journey": [
    "Business Journey",
    "기능을 넘어 실제 업무 결과까지 이어지는 흐름입니다.",
  ],
  "/experience": [
    "Experience",
    "Web Vitals와 오류가 사용자 전환에 미친 영향입니다.",
  ],
  "/insights": [
    "Insight · Root Cause",
    "변화를 자동 감지하고 오프라인 자연어로 분석합니다.",
  ],
  "/ai-analytics": [
    "AI · Agent · MCP",
    "모델, Agent, MCP Tool의 효과·지연·비용을 분석합니다.",
  ],
  "/data-quality": [
    "Data Quality",
    "수집 계약, 지연, 중복, PII와 Cardinality 상태입니다.",
  ],
  "/workspace": [
    "Workspace Roll-Up",
    "전사 서비스의 실제 사용자와 Service Score를 비교합니다.",
  ],
  "/features": [
    "Feature Intelligence",
    "기능 도입·재사용·점수와 Dead Feature 후보를 분석합니다.",
  ],
  "/search-analytics": [
    "Search Analytics",
    "검색 성공, Zero Result, CTR과 재검색을 분석합니다.",
  ],
  "/frustration": [
    "Frustration Analytics",
    "Replay 없이 막힘과 오류 행동 신호를 분석합니다.",
  ],
  "/experiments": [
    "Experiment Analytics",
    "Variant별 Semantic Metric, Lift와 Confidence를 분석합니다.",
  ],
  "/goals": ["Metric Goals", "Semantic KPI의 목표와 달성 상태를 추적합니다."],
  "/change-calendar": [
    "Change Calendar",
    "배포·장애·교육·캠페인을 분석 시계열과 연결합니다.",
  ],
  "/admin/governance": [
    "Analytics Governance",
    "환경, 이벤트 계약, Semantic Metric과 Adoption 분모를 관리합니다.",
  ],
  "/admin/automation": [
    "Report · Action",
    "분석 결과를 사내 시스템과 Confluence로 전달합니다.",
  ],
  "/admin/analytics-engineering": [
    "Analytics Engineering",
    "Metric, Goal, Query Cost, 재집계, Catalog와 Lineage를 관리합니다.",
  ],
  "/admin/product-lab": [
    "Feature Flag · Experiment",
    "기능 Flag와 실험 계약을 관리합니다.",
  ],
  "/admin/privacy-requests": [
    "Privacy Requests",
    "개인정보 삭제·Export 요청의 승인 Workflow를 관리합니다.",
  ],
  "/admin": [
    "관리 센터",
    "Momento 운영과 데이터 거버넌스를 한곳에서 관리합니다.",
  ],
  "/profile": ["내 프로필", "개인 정보와 API 키를 관리합니다."],
};

function matchesPath(pathname: string, to: string) {
  const path = to.split("?")[0];
  return path === "/" ? pathname === "/" : pathname === path;
}

function Navigation({ close }: { close(): void }) {
  const location = useLocation();
  const [expanded, setExpanded] = useState<Record<string, boolean>>(() =>
    Object.fromEntries(
      navGroups.map((group, index) => [
        group.label,
        index === 0 ||
          group.items.some((item) => matchesPath(location.pathname, item.to)),
      ]),
    ),
  );
  useEffect(() => {
    const active = navGroups.find((group) =>
      group.items.some((item) => matchesPath(location.pathname, item.to)),
    );
    if (active)
      setExpanded((current) => ({ ...current, [active.label]: true }));
  }, [location.pathname]);

  return (
    <Box sx={{ px: 1.25, mt: 0.5, flex: 1, overflowY: "auto" }}>
      {navGroups.map((group) => {
        const active = group.items.some((item) =>
          matchesPath(location.pathname, item.to),
        );
        const open = expanded[group.label];
        return (
          <Box key={group.label} sx={{ mb: 0.5 }}>
            <ListItemButton
              onClick={() =>
                setExpanded((current) => ({ ...current, [group.label]: !open }))
              }
              aria-expanded={open}
              sx={{
                minHeight: 42,
                borderRadius: 2,
                color: active ? "#FFFFFF" : "#9CA7BB",
                bgcolor: active ? "rgba(255,255,255,.055)" : "transparent",
                "&:hover": {
                  bgcolor: "rgba(255,255,255,.065)",
                  color: "#FFFFFF",
                },
              }}
            >
              <ListItemIcon
                sx={{
                  color: "inherit",
                  minWidth: 36,
                  "& svg": { fontSize: 19 },
                }}
              >
                {group.icon}
              </ListItemIcon>
              <ListItemText
                primary={group.label}
                primaryTypographyProps={{ fontSize: 13.5, fontWeight: 700 }}
              />
              {open ? (
                <ExpandMoreRounded fontSize="small" />
              ) : (
                <ChevronRightRounded fontSize="small" />
              )}
            </ListItemButton>
            <Collapse in={open} timeout="auto" unmountOnExit>
              <List
                dense
                disablePadding
                sx={{
                  mt: 0.25,
                  ml: 1.15,
                  pl: 1.3,
                  borderLeft: "1px solid rgba(255,255,255,.08)",
                }}
              >
                {group.items.map((item) => (
                  <ListItemButton
                    key={item.to}
                    component={NavLink}
                    to={item.to}
                    onClick={close}
                    end={item.to === "/"}
                    sx={{
                      minHeight: 38,
                      borderRadius: 1.8,
                      mb: 0.2,
                      color: "#929DB1",
                      "&.active": {
                        color: "#FFFFFF",
                        bgcolor: "rgba(109,111,241,.22)",
                        boxShadow: "inset 3px 0 #9294FF",
                      },
                      "&:hover": {
                        bgcolor: "rgba(255,255,255,.06)",
                        color: "white",
                      },
                    }}
                  >
                    <ListItemText
                      primary={item.label}
                      primaryTypographyProps={{
                        fontSize: 13.5,
                        fontWeight: 580,
                        noWrap: true,
                      }}
                    />
                  </ListItemButton>
                ))}
              </List>
            </Collapse>
          </Box>
        );
      })}
    </Box>
  );
}

function CommandPalette({
  open,
  close,
  isAdmin,
}: {
  open: boolean;
  close(): void;
  isAdmin: boolean;
}) {
  const navigate = useNavigate();
  const [query, setQuery] = useState("");
  const routes = useMemo(
    () => [
      ...navGroups.flatMap((group) =>
        group.items.map((item) => ({ ...item, group: group.label })),
      ),
      ...adminCommands
        .filter(() => isAdmin)
        .map((item) => ({ ...item, group: "관리" })),
    ],
    [isAdmin],
  );
  const filtered = useMemo(() => {
    const needle = query.trim().toLocaleLowerCase("ko-KR");
    if (!needle) return routes.slice(0, 10);
    return routes
      .filter((item) =>
        `${item.label} ${item.description} ${item.keywords || ""} ${item.group}`
          .toLocaleLowerCase("ko-KR")
          .includes(needle),
      )
      .slice(0, 12);
  }, [query, routes]);
  useEffect(() => {
    if (!open) setQuery("");
  }, [open]);
  const select = (to: string) => {
    navigate(to);
    close();
  };
  return (
    <Dialog
      open={open}
      onClose={close}
      fullWidth
      maxWidth="sm"
      PaperProps={{
        sx: { position: "fixed", top: 72, m: 0, overflow: "hidden" },
      }}
    >
      <Box
        sx={{
          px: 2,
          py: 1.4,
          borderBottom: "1px solid",
          borderColor: "divider",
        }}
      >
        <FormControl fullWidth>
          <Box
            component="input"
            autoFocus
            value={query}
            onChange={(event) => setQuery(event.currentTarget.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && filtered[0]) select(filtered[0].to);
            }}
            placeholder="메뉴, 기능, 설정 검색…"
            aria-label="Momento 메뉴 검색"
            sx={{
              border: 0,
              outline: 0,
              font: "inherit",
              fontSize: 17,
              width: "100%",
              py: 1,
              pl: 4.5,
              bgcolor: "transparent",
            }}
          />
          <SearchRounded
            sx={{
              position: "absolute",
              top: 9,
              left: 5,
              color: "text.secondary",
            }}
          />
        </FormControl>
      </Box>
      <DialogContent sx={{ p: 1, maxHeight: 520 }}>
        {!filtered.length && (
          <Box sx={{ py: 6, textAlign: "center" }}>
            <Typography fontWeight={700}>일치하는 메뉴가 없습니다</Typography>
            <Typography variant="body2" color="text.secondary" mt={0.5}>
              기능 이름이나 업무 목적을 입력해 보세요.
            </Typography>
          </Box>
        )}
        {filtered.map((item) => (
          <ListItemButton
            key={`${item.group}-${item.to}`}
            onClick={() => select(item.to)}
            sx={{ borderRadius: 2, py: 1.1 }}
          >
            <ListItemIcon sx={{ minWidth: 42, color: "primary.main" }}>
              {item.icon}
            </ListItemIcon>
            <ListItemText
              primary={item.label}
              secondary={item.description}
              primaryTypographyProps={{ fontWeight: 680 }}
              secondaryTypographyProps={{ noWrap: true }}
            />
            <Chip
              size="small"
              label={item.group}
              variant="outlined"
              sx={{ ml: 1 }}
            />
          </ListItemButton>
        ))}
      </DialogContent>
      <Stack
        direction="row"
        gap={2}
        sx={{
          px: 2,
          py: 1,
          bgcolor: "#F8F9FC",
          borderTop: "1px solid",
          borderColor: "divider",
        }}
      >
        <Typography variant="caption" color="text.secondary">
          Enter 이동
        </Typography>
        <Typography variant="caption" color="text.secondary">
          Esc 닫기
        </Typography>
        <Typography variant="caption" color="text.secondary" ml="auto">
          Ctrl/Cmd + K
        </Typography>
      </Stack>
    </Dialog>
  );
}

export default function AppShell({ children }: { children: ReactNode }) {
  const theme = useTheme();
  const desktop = useMediaQuery(theme.breakpoints.up("lg"));
  const compactHeader = useMediaQuery(theme.breakpoints.down("md"));
  const [mobile, setMobile] = useState(false);
  const [profileAnchor, setProfileAnchor] = useState<HTMLElement | null>(null);
  const [commandOpen, setCommandOpen] = useState(false);
  const runtimeVersion = useRuntimeVersion();
  const { user, logout } = useAuth();
  const { sites, site, select, environments, environment, selectEnvironment } =
    useSite();
  const location = useLocation();
  const navigate = useNavigate();
  const isAdmin = !!user && !["analyst", "viewer"].includes(user.role);

  const deployedVersion = runtimeVersion.data?.version;
  const versionMismatch =
    !!deployedVersion && deployedVersion !== consoleVersion;
  useEffect(() => {
    const shortcut = (event: KeyboardEvent) => {
      if (
        (event.metaKey || event.ctrlKey) &&
        event.key.toLocaleLowerCase() === "k"
      ) {
        event.preventDefault();
        setCommandOpen((value) => !value);
      }
    };
    window.addEventListener("keydown", shortcut);
    return () => window.removeEventListener("keydown", shortcut);
  }, []);

  const title = titles[location.pathname] || ["Momento", ""];
  const activeGroup =
    navGroups.find((group) =>
      group.items.some((item) => matchesPath(location.pathname, item.to)),
    )?.label || (location.pathname.startsWith("/admin") ? "관리" : "Momento");

  const drawer = (
    <Box
      sx={{
        height: "100%",
        bgcolor: "#111827",
        color: "white",
        display: "flex",
        flexDirection: "column",
      }}
    >
      <Box
        sx={{
          px: 2.75,
          height: 72,
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <Logo light />
        <Tooltip title="메뉴 검색 (Ctrl/Cmd + K)">
          <IconButton
            size="small"
            onClick={() => setCommandOpen(true)}
            sx={{ color: "#98A3B7", bgcolor: "rgba(255,255,255,.05)" }}
          >
            <SearchRounded fontSize="small" />
          </IconButton>
        </Tooltip>
      </Box>
      <Navigation close={() => setMobile(false)} />
      <Box sx={{ px: 1.25, pb: 1.5 }}>
        {isAdmin && (
          <ListItemButton
            component={NavLink}
            to="/admin"
            onClick={() => setMobile(false)}
            sx={{
              color: "#B7C0D0",
              borderRadius: 2,
              minHeight: 44,
              bgcolor: "rgba(255,255,255,.035)",
              "&.active": { color: "white", bgcolor: "rgba(109,111,241,.22)" },
              "&:hover": { color: "white", bgcolor: "rgba(255,255,255,.07)" },
            }}
          >
            <ListItemIcon sx={{ color: "inherit", minWidth: 38 }}>
              <SettingsOutlined />
            </ListItemIcon>
            <ListItemText
              primary="관리 센터"
              secondary="설정 · 거버넌스"
              primaryTypographyProps={{ fontSize: 13.5, fontWeight: 700 }}
              secondaryTypographyProps={{ fontSize: 10.5, color: "#6F7B90" }}
            />
            <Chip
              label="ADMIN"
              size="small"
              sx={{
                height: 19,
                fontSize: 9,
                color: "#BFC1FF",
                bgcolor: "rgba(109,111,241,.18)",
              }}
            />
          </ListItemButton>
        )}
        <Stack
          direction="row"
          alignItems="center"
          justifyContent="space-between"
          sx={{
            px: 1.5,
            pt: 1.3,
            mt: 1,
            borderTop: "1px solid rgba(255,255,255,.06)",
          }}
        >
          <Box>
            <Typography variant="caption" color="#6F7B90" fontWeight={700}>
              ON-PREMISE
            </Typography>
            <Typography variant="caption" color="#AEB7C7" display="block">
              PostgreSQL · Raw Data
            </Typography>
          </Box>
          <Box
            sx={{
              width: 7,
              height: 7,
              bgcolor: "#25C78B",
              borderRadius: "50%",
              boxShadow: "0 0 0 4px rgba(37,199,139,.1)",
            }}
          />
        </Stack>
      </Box>
    </Box>
  );

  return (
    <Box sx={{ minHeight: "100vh" }}>
      <CommandPalette
        open={commandOpen}
        close={() => setCommandOpen(false)}
        isAdmin={isAdmin}
      />
      {desktop ? (
        <Drawer
          variant="permanent"
          sx={{
            width: drawerWidth,
            "& .MuiDrawer-paper": { width: drawerWidth, border: 0 },
          }}
        >
          {drawer}
        </Drawer>
      ) : (
        <Drawer
          open={mobile}
          onClose={() => setMobile(false)}
          sx={{ "& .MuiDrawer-paper": { width: drawerWidth } }}
        >
          {drawer}
        </Drawer>
      )}
      <Box sx={{ ml: { lg: `${drawerWidth}px` } }}>
        <AppBar
          position="sticky"
          color="inherit"
          elevation={0}
          sx={{
            borderBottom: "1px solid",
            borderColor: "divider",
            bgcolor: "rgba(255,255,255,.9)",
            backdropFilter: "blur(16px)",
          }}
        >
          <Toolbar
            sx={{ minHeight: "68px!important", gap: { xs: 1, sm: 1.5 } }}
          >
            {!desktop && (
              <IconButton
                onClick={() => setMobile(true)}
                aria-label="메뉴 열기"
              >
                <MenuRounded />
              </IconButton>
            )}
            <FormControl
              size="small"
              sx={{ minWidth: { xs: 132, sm: 220 }, maxWidth: 280 }}
            >
              <Select
                value={site?.site_id || ""}
                displayEmpty
                onChange={(event) => select(event.target.value)}
                renderValue={(value) =>
                  value ? (
                    <Stack
                      direction="row"
                      alignItems="center"
                      gap={1}
                      minWidth={0}
                    >
                      <Box
                        sx={{
                          width: 7,
                          height: 7,
                          flex: "0 0 auto",
                          bgcolor: "success.main",
                          borderRadius: "50%",
                        }}
                      />
                      <Typography variant="body2" fontWeight={680} noWrap>
                        {sites.find((item) => item.site_id === value)?.name}
                      </Typography>
                    </Stack>
                  ) : (
                    "사이트를 생성하세요"
                  )
                }
              >
                {sites.map((item) => (
                  <MenuItem key={item.id} value={item.site_id}>
                    <Stack minWidth={0}>
                      <Typography variant="body2" fontWeight={650} noWrap>
                        {item.name}
                      </Typography>
                      <Typography
                        variant="caption"
                        color="text.secondary"
                        className="mono"
                      >
                        {item.site_id}
                      </Typography>
                    </Stack>
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
            <FormControl size="small" sx={{ minWidth: { xs: 84, sm: 112 } }}>
              <Select
                value={environment}
                onChange={(event) => selectEnvironment(event.target.value)}
                renderValue={(value) => (
                  <Stack direction="row" gap={0.7} alignItems="center">
                    <Box
                      sx={{
                        width: 6,
                        height: 6,
                        borderRadius: "50%",
                        bgcolor:
                          value === "prd"
                            ? "success.main"
                            : value === "stg"
                              ? "warning.main"
                              : "info.main",
                      }}
                    />
                    <Typography variant="caption" fontWeight={800}>
                      {String(value).toUpperCase()}
                    </Typography>
                  </Stack>
                )}
              >
                {environments.map((item) => (
                  <MenuItem key={item.name} value={item.name}>
                    <Stack>
                      <Typography variant="body2" fontWeight={650}>
                        {item.name.toUpperCase()}
                      </Typography>
                      <Typography variant="caption" color="text.secondary">
                        {item.label}
                      </Typography>
                    </Stack>
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
            <Box sx={{ flex: 1 }} />
            <Tooltip title="메뉴와 기능 검색 (Ctrl/Cmd + K)">
              <IconButton
                onClick={() => setCommandOpen(true)}
                aria-label="메뉴 검색"
              >
                <SearchRounded />
              </IconButton>
            </Tooltip>
            <Divider
              orientation="vertical"
              flexItem
              sx={{ my: 1.5, display: { xs: "none", sm: "block" } }}
            />
            <Stack
              direction="row"
              alignItems="center"
              gap={1.1}
              onClick={(event) => setProfileAnchor(event.currentTarget)}
              sx={{ cursor: "pointer", py: 0.5 }}
            >
              <Avatar
                sx={{
                  width: 34,
                  height: 34,
                  bgcolor: "#E8E8FF",
                  color: "#5052C9",
                  fontWeight: 750,
                  fontSize: 14,
                }}
              >
                {user?.display_name?.slice(0, 1).toUpperCase()}
              </Avatar>
              {!compactHeader && (
                <Box>
                  <Typography variant="body2" fontWeight={680} lineHeight={1.2}>
                    {user?.display_name}
                  </Typography>
                  <Typography variant="caption" color="text.secondary">
                    {user?.department || user?.role}
                  </Typography>
                </Box>
              )}
            </Stack>
            <Menu
              anchorEl={profileAnchor}
              open={!!profileAnchor}
              onClose={() => setProfileAnchor(null)}
              PaperProps={{ sx: { mt: 1, minWidth: 250 } }}
            >
              <Box sx={{ px: 2, py: 1.2 }}>
                <Typography fontWeight={700}>{user?.display_name}</Typography>
                <Typography variant="caption" color="text.secondary">
                  {user?.email}
                </Typography>
                <Chip
                  size="small"
                  label={user?.role.replaceAll("_", " ")}
                  sx={{ mt: 1, height: 22 }}
                />
              </Box>
              <Divider />
              <MenuItem
                onClick={() => {
                  setProfileAnchor(null);
                  navigate("/profile");
                }}
              >
                <PersonOutlineRounded fontSize="small" sx={{ mr: 1.5 }} />내
                프로필
              </MenuItem>
              <MenuItem
                onClick={() => {
                  setProfileAnchor(null);
                  navigate("/profile?tab=keys");
                }}
              >
                <KeyRounded fontSize="small" sx={{ mr: 1.5 }} />
                API 키
              </MenuItem>
              {isAdmin && (
                <MenuItem
                  onClick={() => {
                    setProfileAnchor(null);
                    navigate("/admin");
                  }}
                >
                  <SettingsOutlined fontSize="small" sx={{ mr: 1.5 }} />
                  관리 센터
                </MenuItem>
              )}
              <Divider />
              <Tooltip
                placement="left"
                title={
                  <Stack spacing={0.25}>
                    <span>
                      서버 {deployedVersion ? `v${deployedVersion}` : runtimeVersion.isError ? "확인 실패" : "확인 중"}
                    </span>
                    <span>콘솔 v{consoleVersion}</span>
                    {runtimeVersion.data && (
                      <span>커밋 {shortCommit(runtimeVersion.data.commit)}</span>
                    )}
                  </Stack>
                }
              >
                <Stack
                  direction="row"
                  justifyContent="space-between"
                  alignItems="center"
                  sx={{ px: 2, py: 1 }}
                >
                  <Box>
                    <Typography variant="caption" color="text.secondary">
                      Momento {deployedVersion ? "서버" : "콘솔"}
                    </Typography>
                    {versionMismatch && (
                      <Typography
                        display="block"
                        variant="caption"
                        color="warning.main"
                        fontSize={10}
                      >
                        콘솔 v{consoleVersion}와 버전이 다릅니다
                      </Typography>
                    )}
                  </Box>
                  <Chip
                    size="small"
                    color={versionMismatch ? "warning" : "default"}
                    label={
                      deployedVersion
                        ? `v${deployedVersion}`
                        : `Console v${consoleVersion}`
                    }
                    sx={{ height: 22, fontSize: 10 }}
                  />
                </Stack>
              </Tooltip>
              <MenuItem
                sx={{ color: "error.main" }}
                onClick={() => void logout()}
              >
                <LogoutRounded fontSize="small" sx={{ mr: 1.5 }} />
                로그아웃
              </MenuItem>
            </Menu>
          </Toolbar>
        </AppBar>
        <Box
          component="main"
          sx={{ p: { xs: 2, md: 3.5 }, maxWidth: 1720, mx: "auto" }}
        >
          <Box sx={{ mb: 3 }}>
            <Breadcrumbs
              separator="/"
              sx={{
                mb: 0.8,
                "& .MuiBreadcrumbs-separator": { color: "#B0B7C3" },
              }}
            >
              <Typography variant="caption" color="text.secondary">
                Momento
              </Typography>
              <Typography variant="caption" color="text.secondary">
                {activeGroup}
              </Typography>
            </Breadcrumbs>
            <Stack
              direction={{ xs: "column", sm: "row" }}
              justifyContent="space-between"
              gap={1}
            >
              <Box>
                <Typography variant="h5">{title[0]}</Typography>
                <Typography color="text.secondary" variant="body2" mt={0.5}>
                  {title[1]}
                </Typography>
              </Box>
              <Chip
                label={`${site?.name || "사이트 미선택"} · ${environment.toUpperCase()}`}
                variant="outlined"
                sx={{
                  alignSelf: { xs: "flex-start", sm: "center" },
                  display: { xs: "none", md: "flex" },
                }}
              />
            </Stack>
          </Box>
          {children}
        </Box>
      </Box>
    </Box>
  );
}
