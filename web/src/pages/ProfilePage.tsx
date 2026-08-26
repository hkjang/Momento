import { useState, type FormEvent } from "react";
import {
  Alert,
  Box,
  Button,
  Card,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  IconButton,
  Stack,
  Tab,
  Tabs,
  TextField,
  Typography,
} from "@mui/material";
import AddRounded from "@mui/icons-material/AddRounded";
import ContentCopyRounded from "@mui/icons-material/ContentCopyRounded";
import DeleteOutlineRounded from "@mui/icons-material/DeleteOutlineRounded";
import SaveRounded from "@mui/icons-material/SaveRounded";
import AutorenewRounded from "@mui/icons-material/AutorenewRounded";
import VisibilityRounded from "@mui/icons-material/VisibilityRounded";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { del, get, patch, post } from "../api/client";
import { useAuth } from "../contexts/AuthContext";
import DataTable from "../components/DataTable";
import { Loading } from "../components/States";
interface APIKey {
  id: string;
  name: string;
  prefix: string;
  scopes: string[];
  expires_at: string | null;
  last_used_at: string | null;
  created_at: string;
  /** True when the key is sealed with MOMENTO_ENCRYPTION_KEY and can be shown again. */
  recoverable: boolean;
}
export default function ProfilePage() {
  const initial =
    new URLSearchParams(location.search).get("tab") === "keys" ? 1 : 0;
  const [tab, setTab] = useState(initial);
  return (
    <Stack spacing={2}>
      <Card sx={{ px: 1 }}>
        <Tabs value={tab} onChange={(_, v) => setTab(v)}>
          <Tab label="개인 정보" />
          <Tab label="API 키 · MCP" />
        </Tabs>
      </Card>
      {tab === 0 ? <ProfileForm /> : <Keys />}
    </Stack>
  );
}
function ProfileForm() {
  const { user, refresh } = useAuth();
  const [form, setForm] = useState({
    display_name: user?.display_name || "",
    department: user?.department || "",
    organization_name: user?.organization_name || "",
    current_password: "",
    new_password: "",
  });
  const save = useMutation({
    mutationFn: () => patch("/api/v1/me", form),
    onSuccess: () => refresh(),
  });
  return (
    <Card sx={{ p: 3, maxWidth: 700 }}>
      <Typography variant="h6">내 정보</Typography>
      <Typography variant="body2" color="text.secondary" mb={3}>
        조직과 부서는 사내 사용 분석의 사용자 속성과 별개인 관리 계정
        정보입니다.
      </Typography>
      <Box
        component="form"
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <Stack spacing={2}>
          <TextField label="이메일" value={user?.email || ""} disabled />
          <TextField
            label="표시 이름"
            value={form.display_name}
            onChange={(e) => setForm({ ...form, display_name: e.target.value })}
          />
          <Box
            sx={{
              display: "grid",
              gridTemplateColumns: { xs: "1fr", sm: "1fr 1fr" },
              gap: 2,
            }}
          >
            <TextField
              label="부서"
              value={form.department}
              onChange={(e) => setForm({ ...form, department: e.target.value })}
            />
            <TextField
              label="조직"
              value={form.organization_name}
              onChange={(e) =>
                setForm({ ...form, organization_name: e.target.value })
              }
            />
          </Box>
          <Divider />
          <Typography fontWeight={700}>비밀번호 변경 (선택)</Typography>
          <TextField
            label="현재 비밀번호"
            type="password"
            value={form.current_password}
            onChange={(e) =>
              setForm({ ...form, current_password: e.target.value })
            }
          />
          <TextField
            label="새 비밀번호"
            type="password"
            value={form.new_password}
            onChange={(e) => setForm({ ...form, new_password: e.target.value })}
            helperText="12자 이상"
          />
          {save.error && <Alert severity="error">{save.error.message}</Alert>}
          {save.isSuccess && <Alert severity="success">저장했습니다.</Alert>}
          <Button
            type="submit"
            variant="contained"
            startIcon={<SaveRounded />}
            sx={{ alignSelf: "flex-start" }}
          >
            저장
          </Button>
        </Stack>
      </Box>
    </Card>
  );
}
function Keys() {
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["my-keys"],
    queryFn: () => get<APIKey[]>("/api/v1/me/keys"),
  });
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [days, setDays] = useState(365);
  const [secret, setSecret] = useState("");
  const [secretRecoverable, setSecretRecoverable] = useState(false);
  const [revealError, setRevealError] = useState("");
  const create = useMutation({
    mutationFn: () =>
      post<{ key: string; recoverable: boolean }>("/api/v1/me/keys", {
        name,
        expires_in_days: days,
      }),
    onSuccess: (d) => {
      setOpen(false);
      setSecretRecoverable(d.recoverable);
      setSecret(d.key);
      qc.invalidateQueries({ queryKey: ["my-keys"] });
    },
  });
  const remove = useMutation({
    mutationFn: (id: string) => del(`/api/v1/me/keys/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["my-keys"] }),
  });
  const rotate = useMutation({
    mutationFn: (id: string) =>
      post<{ key: string; recoverable: boolean }>(
        `/api/v1/me/keys/${id}/rotate`,
      ),
    onSuccess: (d) => {
      setSecretRecoverable(d.recoverable);
      setSecret(d.key);
      qc.invalidateQueries({ queryKey: ["my-keys"] });
    },
  });
  // Encrypted storage means a lost key can be read again instead of rotated.
  const reveal = useMutation({
    mutationFn: (id: string) =>
      post<{ available: boolean; key?: string; reason?: string }>(
        `/api/v1/me/keys/${id}/reveal`,
      ),
    onSuccess: (d) => {
      if (d.available && d.key) {
        setRevealError("");
        setSecretRecoverable(true);
        setSecret(d.key);
        return;
      }
      setRevealError(d.reason || "저장된 키를 찾을 수 없습니다.");
    },
  });
  if (q.isLoading) return <Loading />;
  return (
    <Stack spacing={2}>
      <Card sx={{ p: 2.5, bgcolor: "#F7F8FF" }}>
        <Typography fontWeight={720}>Reporting API와 MCP 인증</Typography>
        <Typography variant="body2" color="text.secondary" mt={0.7}>
          발급한 키를{" "}
          <span className="mono">Authorization: Bearer mom_key_...</span>로
          전달하세요. MCP endpoint는 <span className="mono">POST /mcp</span>
          입니다.
        </Typography>
      </Card>
      {revealError && (
        <Alert severity="warning" onClose={() => setRevealError("")}>
          {revealError}
        </Alert>
      )}
      <Stack direction="row" justifyContent="space-between">
        <Typography variant="h6">개인 API 키</Typography>
        <Button
          variant="contained"
          startIcon={<AddRounded />}
          onClick={() => setOpen(true)}
        >
          키 발급
        </Button>
      </Stack>
      <DataTable
        columns={[
          { key: "name", label: "이름" },
          {
            key: "prefix",
            label: "키",
            format: (v) => (
              <Typography className="mono" variant="body2">
                {String(v)}••••••
              </Typography>
            ),
          },
          {
            key: "last_used_at",
            label: "마지막 사용",
            format: (v) =>
              v ? new Date(String(v)).toLocaleString("ko-KR") : "사용 전",
          },
          {
            key: "expires_at",
            label: "만료",
            format: (v) =>
              v ? new Date(String(v)).toLocaleDateString("ko-KR") : "없음",
          },
          {
            key: "id",
            label: "",
            align: "right",
            format: (v, row) => (
              <Stack direction="row" justifyContent="flex-end">
                <IconButton
                  size="small"
                  title={
                    row.recoverable
                      ? "저장된 키 보기"
                      : "MOMENTO_ENCRYPTION_KEY 설정 후 회전한 키만 다시 볼 수 있습니다"
                  }
                  disabled={!row.recoverable || reveal.isPending}
                  onClick={() => reveal.mutate(String(v))}
                >
                  <VisibilityRounded />
                </IconButton>
                <IconButton
                  size="small"
                  color="primary"
                  title="키 회전"
                  onClick={() => rotate.mutate(String(v))}
                >
                  <AutorenewRounded />
                </IconButton>
                <IconButton
                  size="small"
                  color="error"
                  title="폐기"
                  onClick={() => remove.mutate(String(v))}
                >
                  <DeleteOutlineRounded />
                </IconButton>
              </Stack>
            ),
          },
        ]}
        rows={q.data as unknown as Record<string, unknown>[]}
      />
      <Dialog open={open} onClose={() => setOpen(false)}>
        <DialogTitle>API 키 발급</DialogTitle>
        <DialogContent>
          <Stack spacing={2} pt={1}>
            <TextField
              label="키 이름"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Claude MCP"
            />
            <TextField
              label="유효 기간 (일)"
              type="number"
              value={days}
              onChange={(e) => setDays(Number(e.target.value))}
            />
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setOpen(false)}>취소</Button>
          <Button variant="contained" onClick={() => create.mutate()}>
            발급
          </Button>
        </DialogActions>
      </Dialog>
      <Dialog
        open={!!secret}
        onClose={() => setSecret("")}
        fullWidth
        maxWidth="sm"
      >
        <DialogTitle>개인 API 키</DialogTitle>
        <DialogContent>
          <Alert
            severity={secretRecoverable ? "info" : "warning"}
            sx={{ mb: 2 }}
          >
            {secretRecoverable
              ? "이 키는 암호화되어 저장되었으므로 재기동 후에도 다시 조회할 수 있습니다."
              : "MOMENTO_ENCRYPTION_KEY가 없어 이 키는 다시 확인할 수 없습니다."}
          </Alert>
          <Stack direction="row">
            <TextField
              fullWidth
              value={secret}
              slotProps={{ input: { readOnly: true } }}
            />
            <IconButton
              onClick={() => void navigator.clipboard.writeText(secret)}
            >
              <ContentCopyRounded />
            </IconButton>
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setSecret("")}>확인</Button>
        </DialogActions>
      </Dialog>
    </Stack>
  );
}
