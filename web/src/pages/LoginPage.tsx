import { useEffect, useState, type FormEvent } from "react";
import {
  Alert,
  Box,
  Button,
  Card,
  CircularProgress,
  Divider,
  Stack,
  TextField,
  Typography,
} from "@mui/material";
import LoginRounded from "@mui/icons-material/LoginRounded";
import ShieldOutlined from "@mui/icons-material/ShieldOutlined";
import QueryStatsRounded from "@mui/icons-material/QueryStatsRounded";
import HubOutlined from "@mui/icons-material/HubOutlined";
import { useAuth } from "../contexts/AuthContext";
import { api } from "../api/client";
import { Logo } from "../components/Logo";
import { consoleVersion, shortCommit, useRuntimeVersion } from "../version";

interface Options {
  oidc_enabled: boolean;
}
export default function LoginPage() {
  const { login } = useAuth();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [options, setOptions] = useState<Options | null>(null);
  const runtimeVersion = useRuntimeVersion();
  useEffect(() => {
    void api<Options>("/api/v1/auth/options", { cache: "no-store" })
      .then(setOptions)
      .catch(() => setOptions({ oidc_enabled: false }));
  }, []);
  const deployedVersion = runtimeVersion.data?.version;
  const versionMismatch =
    !!deployedVersion && deployedVersion !== consoleVersion;
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await login(email, password);
    } catch (err) {
      setError(err instanceof Error ? err.message : "로그인하지 못했습니다.");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Box
      sx={{
        minHeight: "100vh",
        display: "grid",
        gridTemplateColumns: { xs: "1fr", md: "minmax(420px, .92fr) 1.08fr" },
        bgcolor: "#fff",
      }}
    >
      <Box
        sx={{
          display: "flex",
          flexDirection: "column",
          px: { xs: 3, sm: 8, lg: 14 },
          py: 5,
        }}
      >
        <Logo />
        <Box sx={{ my: "auto", width: "100%", maxWidth: 420, py: 8 }}>
          <Typography variant="h4" sx={{ mb: 1 }}>
            다시 만나 반갑습니다
          </Typography>
          <Typography color="text.secondary" sx={{ mb: 4 }}>
            사내 서비스의 중요한 순간을 데이터로 확인하세요.
          </Typography>
          {error && (
            <Alert severity="error" sx={{ mb: 2 }}>
              {error}
            </Alert>
          )}
          <Box component="form" onSubmit={submit}>
            <Stack spacing={2.2}>
              <TextField
                label="관리자 이메일"
                type="email"
                autoComplete="username"
                required
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                fullWidth
              />
              <TextField
                label="비밀번호"
                type="password"
                autoComplete="current-password"
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                fullWidth
              />
              <Button
                type="submit"
                variant="contained"
                size="large"
                disabled={busy}
                startIcon={
                  busy ? (
                    <CircularProgress size={16} color="inherit" />
                  ) : (
                    <LoginRounded />
                  )
                }
              >
                로그인
              </Button>
              {options?.oidc_enabled && (
                <>
                  <Divider>또는</Divider>
                  <Button
                    variant="outlined"
                    size="large"
                    startIcon={<ShieldOutlined />}
                    onClick={() => {
                      location.href = "/api/v1/auth/oidc/login";
                    }}
                  >
                    Keycloak SSO로 로그인
                  </Button>
                </>
              )}
            </Stack>
          </Box>
        </Box>
        <Typography
          variant="caption"
          color={versionMismatch ? "warning.main" : "text.secondary"}
          title={
            runtimeVersion.data
              ? `서버 빌드 ${shortCommit(runtimeVersion.data.commit)} · 콘솔 v${consoleVersion}`
              : `콘솔 v${consoleVersion}`
          }
        >
          Momento {deployedVersion ? `v${deployedVersion}` : `Console v${consoleVersion}`}
          {versionMismatch ? ` · Console v${consoleVersion}` : ""} · On-premise
          analytics
        </Typography>
      </Box>
      <Box
        sx={{
          display: { xs: "none", md: "flex" },
          m: 2,
          borderRadius: 5,
          overflow: "hidden",
          position: "relative",
          color: "white",
          background:
            "radial-gradient(circle at 72% 18%,rgba(94,234,212,.2),transparent 28%),radial-gradient(circle at 15% 82%,rgba(129,140,248,.32),transparent 30%),linear-gradient(145deg,#111827,#20254A 58%,#3730A3)",
        }}
      >
        <Box
          sx={{
            position: "absolute",
            inset: 0,
            opacity: 0.1,
            backgroundImage:
              "linear-gradient(rgba(255,255,255,.3) 1px,transparent 1px),linear-gradient(90deg,rgba(255,255,255,.3) 1px,transparent 1px)",
            backgroundSize: "44px 44px",
          }}
        />
        <Box sx={{ m: "auto", width: "78%", position: "relative" }}>
          <Typography
            variant="overline"
            sx={{ color: "#8BFBDF", fontWeight: 700, letterSpacing: ".16em" }}
          >
            PRIVATE BY DESIGN
          </Typography>
          <Typography
            sx={{
              mt: 1,
              mb: 5,
              fontSize: { md: 38, lg: 48 },
              lineHeight: 1.14,
              fontWeight: 730,
              letterSpacing: "-.045em",
            }}
          >
            누가, 어느 조직에서,
            <br />
            어떤 기능을 사용하는지
            <br />
            한눈에.
          </Typography>
          <Stack direction={{ md: "column", lg: "row" }} spacing={2}>
            {[
              {
                icon: <QueryStatsRounded />,
                title: "Event first",
                text: "모든 행동을 원본 그대로",
              },
              {
                icon: <HubOutlined />,
                title: "Internal insight",
                text: "부서·조직·망별 분석",
              },
              {
                icon: <ShieldOutlined />,
                title: "On-premise",
                text: "데이터는 사내에만",
              },
            ].map((item) => (
              <Card
                key={item.title}
                sx={{
                  flex: 1,
                  p: 2.2,
                  color: "white",
                  bgcolor: "rgba(255,255,255,.08)",
                  borderColor: "rgba(255,255,255,.12)",
                  backdropFilter: "blur(12px)",
                }}
              >
                {item.icon}
                <Typography fontWeight={700} mt={1}>
                  {item.title}
                </Typography>
                <Typography
                  variant="body2"
                  sx={{ color: "rgba(255,255,255,.64)" }}
                >
                  {item.text}
                </Typography>
              </Card>
            ))}
          </Stack>
        </Box>
      </Box>
    </Box>
  );
}
