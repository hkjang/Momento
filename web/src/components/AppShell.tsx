import { useEffect, useMemo, useState, type ReactNode } from "react";
import {
  AppBar,
  Avatar,
  Box,
  Chip,
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
import DashboardRounded from "@mui/icons-material/DashboardRounded";
import BoltRounded from "@mui/icons-material/BoltRounded";
import ExploreRounded from "@mui/icons-material/ExploreRounded";
import ArticleOutlined from "@mui/icons-material/ArticleOutlined";
import MouseOutlined from "@mui/icons-material/MouseOutlined";
import PeopleAltOutlined from "@mui/icons-material/PeopleAltOutlined";
import AccountTreeOutlined from "@mui/icons-material/AccountTreeOutlined";
import FilterAltOutlined from "@mui/icons-material/FilterAltOutlined";
import TuneRounded from "@mui/icons-material/TuneRounded";
import SettingsOutlined from "@mui/icons-material/SettingsOutlined";
import MenuRounded from "@mui/icons-material/MenuRounded";
import PersonOutlineRounded from "@mui/icons-material/PersonOutlineRounded";
import KeyRounded from "@mui/icons-material/KeyRounded";
import LogoutRounded from "@mui/icons-material/LogoutRounded";
import HelpOutlineRounded from "@mui/icons-material/HelpOutlineRounded";
import BusinessRounded from "@mui/icons-material/BusinessRounded";
import InsightsRounded from "@mui/icons-material/InsightsRounded";
import PsychologyRounded from "@mui/icons-material/PsychologyRounded";
import HealthAndSafetyRounded from "@mui/icons-material/HealthAndSafetyRounded";
import CalendarMonthRounded from "@mui/icons-material/CalendarMonthRounded";
import { NavLink, useLocation, useNavigate } from "react-router-dom";
import { Logo } from "./Logo";
import { useAuth } from "../contexts/AuthContext";
import { useSite } from "../contexts/SiteContext";
import { get } from "../api/client";

const width = 258;
const groups = [
  {
    label: "분석",
    items: [
      { to: "/", label: "개요", icon: <DashboardRounded /> },
      { to: "/realtime", label: "실시간", icon: <BoltRounded /> },
      { to: "/usage", label: "사내 사용 현황", icon: <BusinessRounded /> },
    ],
  },
  {
    label: "리포트",
    items: [
      { to: "/acquisition", label: "유입", icon: <ExploreRounded /> },
      { to: "/pages", label: "페이지", icon: <ArticleOutlined /> },
      { to: "/events", label: "이벤트", icon: <MouseOutlined /> },
      { to: "/visitors", label: "사용자", icon: <PeopleAltOutlined /> },
      {
        to: "/user-explorer",
        label: "User Explorer",
        icon: <PeopleAltOutlined />,
      },
      { to: "/ecommerce", label: "Ecommerce", icon: <ArticleOutlined /> },
    ],
  },
  {
    label: "탐색",
    items: [
      { to: "/explorer", label: "자유 분석", icon: <TuneRounded /> },
      { to: "/segments", label: "Segment", icon: <FilterAltOutlined /> },
      { to: "/funnel", label: "퍼널", icon: <FilterAltOutlined /> },
      { to: "/path", label: "경로", icon: <AccountTreeOutlined /> },
    ],
  },
  {
    label: "제품 · 경험",
    items: [
      { to: "/adoption", label: "Feature Adoption", icon: <BusinessRounded /> },
      { to: "/cohort", label: "Cohort · Retention", icon: <CalendarMonthRounded /> },
      { to: "/journey", label: "Business Journey", icon: <AccountTreeOutlined /> },
      { to: "/experience", label: "Web Vitals · 오류", icon: <HealthAndSafetyRounded /> },
	  { to: "/features", label: "Feature Intelligence", icon: <InsightsRounded /> },
	  { to: "/search-analytics", label: "Search Analytics", icon: <ExploreRounded /> },
	  { to: "/frustration", label: "Frustration", icon: <HealthAndSafetyRounded /> },
	  { to: "/experiments", label: "Experiment", icon: <TuneRounded /> },
	  { to: "/goals", label: "Goal", icon: <InsightsRounded /> },
    ],
  },
	{
	  label: "전사 · 변경",
	  items: [
		{ to: "/workspace", label: "Workspace Roll-Up", icon: <BusinessRounded /> },
		{ to: "/change-calendar", label: "Change Calendar", icon: <CalendarMonthRounded /> },
	  ],
	},
  {
    label: "지능 · 품질",
    items: [
      { to: "/insights", label: "Insight · 자연어", icon: <InsightsRounded /> },
      { to: "/ai-analytics", label: "AI · Agent · MCP", icon: <PsychologyRounded /> },
      { to: "/data-quality", label: "Data Quality", icon: <HealthAndSafetyRounded /> },
      { to: "/admin/governance", label: "Metric · Contract", icon: <TuneRounded /> },
      { to: "/admin/automation", label: "Report · Action", icon: <BoltRounded /> },
	  { to: "/admin/analytics-engineering", label: "Analytics Engineering", icon: <AccountTreeOutlined /> },
	  { to: "/admin/product-lab", label: "Feature Flag · Lab", icon: <TuneRounded /> },
	  { to: "/admin/privacy-requests", label: "Privacy Requests", icon: <HealthAndSafetyRounded /> },
    ],
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
  "/adoption": ["Feature Adoption", "조직·부서별 기능 도입률과 재사용을 분석합니다."],
  "/cohort": ["Cohort · Retention", "최초 행동 이후 사용자가 다시 돌아오는 비율입니다."],
  "/journey": ["Business Journey", "기능을 넘어 실제 업무 결과까지 이어지는 흐름입니다."],
  "/experience": ["Experience", "Web Vitals와 오류가 사용자 전환에 미친 영향입니다."],
  "/insights": ["Insight · Root Cause", "변화를 자동 감지하고 오프라인 자연어로 분석합니다."],
  "/ai-analytics": ["AI · Agent · MCP", "모델, Agent, MCP Tool의 효과·지연·비용을 분석합니다."],
  "/data-quality": ["Data Quality", "수집 계약, 지연, 중복, PII와 Cardinality 상태입니다."],
	"/workspace": ["Workspace Roll-Up", "전사 서비스의 실제 사용자와 Service Score를 비교합니다."],
	"/features": ["Feature Intelligence", "기능 도입·재사용·점수와 Dead Feature 후보를 분석합니다."],
	"/search-analytics": ["Search Analytics", "검색 성공, Zero Result, CTR과 재검색을 분석합니다."],
	"/frustration": ["Frustration Analytics", "Replay 없이 막힘과 오류 행동 신호를 분석합니다."],
	"/experiments": ["Experiment Analytics", "Variant별 Semantic Metric, Lift와 Confidence를 분석합니다."],
	"/goals": ["Metric Goals", "Semantic KPI의 목표와 달성 상태를 추적합니다."],
	"/change-calendar": ["Change Calendar", "배포·장애·교육·캠페인을 분석 시계열과 연결합니다."],
  "/admin/governance": ["Analytics Governance", "환경, 이벤트 계약, Semantic Metric과 Adoption 분모를 관리합니다."],
  "/admin/automation": ["Report · Action", "분석 결과를 사내 시스템과 Confluence로 전달합니다."],
	"/admin/analytics-engineering": ["Analytics Engineering", "Metric, Goal, Query Cost, 재집계, Catalog와 Lineage를 관리합니다."],
	"/admin/product-lab": ["Feature Flag · Experiment", "기능 Flag와 실험 계약을 관리합니다."],
	"/admin/privacy-requests": ["Privacy Requests", "개인정보 삭제·Export 요청의 승인 Workflow를 관리합니다."],
  "/admin": ["관리", "Momento 운영 설정을 관리합니다."],
  "/profile": ["내 프로필", "개인 정보와 API 키를 관리합니다."],
};

function Navigation({ close }: { close(): void }) {
  return (
    <Box sx={{ px: 1.5, mt: 1, flex: 1, overflowY: "auto" }}>
      {groups.map((group) => (
        <Box key={group.label} sx={{ mb: 2.5 }}>
          <Typography
            variant="caption"
            sx={{
              px: 1.5,
              color: "#77839A",
              fontWeight: 700,
              letterSpacing: ".08em",
            }}
          >
            {group.label}
          </Typography>
          <List dense sx={{ mt: 0.5 }}>
            {group.items.map((item) => (
              <ListItemButton
                key={item.to}
                component={NavLink}
                to={item.to}
                onClick={close}
                end={item.to === "/"}
                sx={{
                  borderRadius: 2,
                  mb: 0.25,
                  color: "#99A4B8",
                  "& .MuiListItemIcon-root": { color: "inherit" },
                  "&.active": {
                    color: "white",
                    bgcolor: "rgba(109,111,241,.2)",
                    "&:before": {
                      content: '""',
                      width: 3,
                      height: 20,
                      borderRadius: 2,
                      bgcolor: "#8587F4",
                      position: "absolute",
                      left: 0,
                    },
                  },
                  "&:hover": {
                    bgcolor: "rgba(255,255,255,.06)",
                    color: "white",
                  },
                }}
              >
                <ListItemIcon sx={{ minWidth: 38 }}>{item.icon}</ListItemIcon>
                <ListItemText
                  primary={item.label}
                  primaryTypographyProps={{ fontSize: 14, fontWeight: 580 }}
                />
              </ListItemButton>
            ))}
          </List>
        </Box>
      ))}
    </Box>
  );
}

export default function AppShell({ children }: { children: ReactNode }) {
  const theme = useTheme();
  const desktop = useMediaQuery(theme.breakpoints.up("lg"));
  const [mobile, setMobile] = useState(false);
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const [version, setVersion] = useState("");
  const { user, logout } = useAuth();
  const { sites, site, select, environments, environment, selectEnvironment } = useSite();
  const location = useLocation();
  const navigate = useNavigate();
  useEffect(() => {
    void get<{ version: string }>("/api/v1/version").then((v) =>
      setVersion(v.version),
    );
  }, []);
  const title = useMemo(
    () => titles[location.pathname] || ["Momento", ""],
    [location.pathname],
  );
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
      <Box sx={{ px: 3, height: 76, display: "flex", alignItems: "center" }}>
        <Logo light />
      </Box>
      <Navigation close={() => setMobile(false)} />
      <Box sx={{ mt: "auto", px: 2, pb: 2 }}>
        <ListItemButton
          component={NavLink}
          to="/admin"
          onClick={() => setMobile(false)}
          sx={{
            color: "#99A4B8",
            borderRadius: 2,
            "&.active": { color: "white", bgcolor: "rgba(109,111,241,.2)" },
          }}
        >
          <ListItemIcon sx={{ color: "inherit", minWidth: 38 }}>
            <SettingsOutlined />
          </ListItemIcon>
          <ListItemText
            primary="관리"
            primaryTypographyProps={{ fontSize: 14, fontWeight: 580 }}
          />
        </ListItemButton>
        <Box
          sx={{
            p: 1.5,
            mt: 1.5,
            borderRadius: 2,
            bgcolor: "rgba(255,255,255,.04)",
          }}
        >
          <Typography variant="caption" color="#78849A">
            DATA STAYS YOURS
          </Typography>
          <Typography variant="body2" color="#C5CCDA" mt={0.3}>
            On-premise · PostgreSQL
          </Typography>
        </Box>
      </Box>
    </Box>
  );
  return (
    <Box sx={{ minHeight: "100vh" }}>
      {desktop ? (
        <Drawer
          variant="permanent"
          sx={{ width, "& .MuiDrawer-paper": { width, border: 0 } }}
        >
          {drawer}
        </Drawer>
      ) : (
        <Drawer
          open={mobile}
          onClose={() => setMobile(false)}
          sx={{ "& .MuiDrawer-paper": { width } }}
        >
          {drawer}
        </Drawer>
      )}
      <Box sx={{ ml: { lg: `${width}px` } }}>
        <AppBar
          position="sticky"
          color="inherit"
          elevation={0}
          sx={{
            borderBottom: "1px solid #E8ECF3",
            bgcolor: "rgba(255,255,255,.88)",
            backdropFilter: "blur(14px)",
          }}
        >
          <Toolbar sx={{ minHeight: "68px!important", gap: 2 }}>
            {!desktop && (
              <IconButton onClick={() => setMobile(true)}>
                <MenuRounded />
              </IconButton>
            )}
            <FormControl
              size="small"
              sx={{
                minWidth: { xs: 150, sm: 230 },
                "& .MuiOutlinedInput-root": { bgcolor: "#F7F8FB" },
              }}
            >
              <Select
                value={site?.site_id || ""}
                displayEmpty
                onChange={(e) => select(e.target.value)}
                renderValue={(value) =>
                  value ? (
                    <Stack direction="row" alignItems="center" gap={1}>
                      <Box
                        sx={{
                          width: 7,
                          height: 7,
                          bgcolor: "#12A875",
                          borderRadius: "50%",
                        }}
                      />
                      <Typography variant="body2" fontWeight={650}>
                        {sites.find((x) => x.site_id === value)?.name}
                      </Typography>
                    </Stack>
                  ) : (
                    "사이트를 생성하세요"
                  )
                }
              >
                {sites.map((s) => (
                  <MenuItem key={s.id} value={s.site_id}>
                    {s.name}
                    <Typography
                      variant="caption"
                      color="text.secondary"
                      ml="auto"
                    >
                      {s.site_id}
                    </Typography>
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
            <FormControl size="small" sx={{ minWidth: 115 }}>
              <Select
                value={environment}
                onChange={(event) => selectEnvironment(event.target.value)}
                renderValue={(value) => String(value).toUpperCase()}
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
            <Tooltip title="도움말">
              <IconButton>
                <HelpOutlineRounded />
              </IconButton>
            </Tooltip>
            <Divider orientation="vertical" flexItem sx={{ my: 1.5 }} />
            <Stack
              direction="row"
              alignItems="center"
              gap={1.2}
              onClick={(e) => setAnchor(e.currentTarget)}
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
              <Box sx={{ display: { xs: "none", sm: "block" } }}>
                <Typography variant="body2" fontWeight={680} lineHeight={1.2}>
                  {user?.display_name}
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  {user?.department || user?.role}
                </Typography>
              </Box>
            </Stack>
            <Menu
              anchorEl={anchor}
              open={!!anchor}
              onClose={() => setAnchor(null)}
              PaperProps={{ sx: { mt: 1, minWidth: 230 } }}
            >
              <Box sx={{ px: 2, py: 1 }}>
                <Typography fontWeight={700}>{user?.display_name}</Typography>
                <Typography variant="caption" color="text.secondary">
                  {user?.email}
                </Typography>
              </Box>
              <Divider />
              <MenuItem
                onClick={() => {
                  setAnchor(null);
                  navigate("/profile");
                }}
              >
                <PersonOutlineRounded fontSize="small" sx={{ mr: 1.5 }} />내
                프로필
              </MenuItem>
              <MenuItem
                onClick={() => {
                  setAnchor(null);
                  navigate("/profile?tab=keys");
                }}
              >
                <KeyRounded fontSize="small" sx={{ mr: 1.5 }} />
                API 키
              </MenuItem>
              <Divider />
              <Box sx={{ px: 2, py: 1 }}>
                <Stack direction="row" justifyContent="space-between">
                  <Typography variant="caption" color="text.secondary">
                    Momento
                  </Typography>
                  <Chip
                    size="small"
                    label={version || "…"}
                    sx={{ height: 20, fontSize: 10 }}
                  />
                </Stack>
              </Box>
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
          sx={{ p: { xs: 2, md: 3.5 }, maxWidth: 1680, mx: "auto" }}
        >
          <Box sx={{ mb: 3 }}>
            <Typography variant="h5">{title[0]}</Typography>
            <Typography color="text.secondary" variant="body2" mt={0.5}>
              {title[1]}
            </Typography>
          </Box>
          {children}
        </Box>
      </Box>
    </Box>
  );
}
