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
  MenuItem,
  Stack,
  Tab,
  Tabs,
  TextField,
  Typography,
} from "@mui/material";
import AddRounded from "@mui/icons-material/AddRounded";
import ContentCopyRounded from "@mui/icons-material/ContentCopyRounded";
import DeleteOutlineRounded from "@mui/icons-material/DeleteOutlineRounded";
import KeyRounded from "@mui/icons-material/KeyRounded";
import SaveRounded from "@mui/icons-material/SaveRounded";
import EditOutlined from "@mui/icons-material/EditOutlined";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { del, get, patch, post, put, type Site } from "../api/client";
import { useAuth } from "../contexts/AuthContext";
import { useSite } from "../contexts/SiteContext";
import DataTable from "../components/DataTable";
import { ErrorState, Loading, NoSite } from "../components/States";

const adminTabs = [
  "사이트",
  "SSO · 일반",
  "개인정보",
  "보존 정책",
  "네트워크 망",
  "사용자 · 권한",
  "이벤트 스키마",
  "사용자 정의 차원",
  "Tracking Debugger",
  "감사 로그",
];
export default function AdminPage() {
  const { user } = useAuth();
  const [tab, setTab] = useState(0);
  if (user?.role === "analyst" || user?.role === "viewer")
    return <Alert severity="warning">관리자 권한이 필요합니다.</Alert>;
  return (
    <Stack spacing={2}>
      <Card sx={{ px: 1 }}>
        <Tabs
          value={tab}
          onChange={(_, v) => setTab(v)}
          variant="scrollable"
          scrollButtons="auto"
        >
          {adminTabs.map((x) => (
            <Tab key={x} label={x} />
          ))}
        </Tabs>
      </Card>
      {tab === 0 && <SitesAdmin />}
      {tab === 1 && (
        <SettingsAdmin groups={["general", "oidc", "storage", "security"]} />
      )}{" "}
      {tab === 2 && <PrivacyAdmin />}
      {tab === 3 && <RetentionAdmin />}
      {tab === 4 && <NetworksAdmin />}
      {tab === 5 && <UsersAdmin />}
      {tab === 6 && <SchemasAdmin />}
      {tab === 7 && <DimensionsAdmin />}
      {tab === 8 && <DebuggerAdmin />}
      {tab === 9 && <AuditAdmin />}
    </Stack>
  );
}

function CopyField({ value }: { value: string }) {
  return (
    <TextField
      fullWidth
      multiline={value.includes("\n")}
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
function SecretDialog({
  title,
  secret,
  close,
}: {
  title: string;
  secret: string;
  close(): void;
}) {
  return (
    <Dialog open onClose={close} maxWidth="sm" fullWidth>
      <DialogTitle>{title}</DialogTitle>
      <DialogContent>
        <Alert severity="warning" sx={{ mb: 2 }}>
          이 값은 지금 한 번만 표시됩니다. 안전한 곳에 복사하세요.
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
  const [secret, setSecret] = useState("");
  const [name, setName] = useState("");
  const [service, setService] = useState("");
  const [domains, setDomains] = useState("");
  const [timezone, setTimezone] = useState("Asia/Seoul");
  const [engagementThreshold, setEngagementThreshold] = useState(10);
  const [editing, setEditing] = useState<Site | null>(null);
  const create = useMutation({
    mutationFn: () =>
      post<{ tracking_key: string; server_api_key: string }>("/api/v1/sites", {
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
      setSecret(
        `Tracking Key: ${d.tracking_key}\nServer API Key: ${d.server_api_key}`,
      );
      setOpen(false);
      setName("");
      await qc.invalidateQueries({ queryKey: ["admin-sites"] });
      await refresh();
    },
  });
  const rotate = useMutation({
    mutationFn: (id: string) =>
      post<{ tracking_key: string }>(`/api/v1/sites/${id}/rotate-key`),
    onSuccess: (d) => setSecret(d.tracking_key),
  });
  const rotateServer = useMutation({
    mutationFn: (id: string) =>
      post<{ server_api_key: string }>(`/api/v1/sites/${id}/rotate-server-key`),
    onSuccess: (d) => setSecret(d.server_api_key),
  });
  if (q.isLoading) return <Loading />;
  if (q.error) return <ErrorState error={q.error} />;
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
        {q.data!.map((site) => (
          <Card key={site.id} sx={{ p: 2.5 }}>
            <Stack direction="row" justifyContent="space-between">
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
              <Stack direction="row">
                <Button
                  size="small"
                  startIcon={<EditOutlined />}
                  onClick={() => setEditing(site)}
                >
                  설정
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
        ))}
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
          title="새 Tracking Key"
          secret={secret}
          close={() => setSecret("")}
        />
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
          </Section>
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

interface RetentionPolicy {
  raw_event_months: number;
  session_months: number;
  aggregation_months: number | null;
  realtime_hours: number;
  debug_days: number;
}

function RetentionAdmin() {
  const { site } = useSite();
  const qc = useQueryClient();
  const query = useQuery({
    queryKey: ["retention", site?.site_id],
    queryFn: () =>
      get<{ policy: RetentionPolicy; updated_at?: string }>(
        `/api/v1/sites/${site!.site_id}/retention`,
      ),
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
              helperText="비워 두면 무기한"
            />
            <TextField
              label="Realtime (시간)"
              type="number"
              value={current?.realtime_hours ?? 24}
              onChange={(event) =>
                set("realtime_hours", Number(event.target.value))
              }
              helperText="1~168시간"
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
