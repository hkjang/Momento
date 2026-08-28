import { useEffect, useMemo, useState } from "react";
import {
  Alert,
  Box,
  Button,
  Card,
  Chip,
  Divider,
  Snackbar,
  Stack,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Tooltip,
  Typography,
} from "@mui/material";
import ContentCopyRounded from "@mui/icons-material/ContentCopyRounded";
import DevicesRounded from "@mui/icons-material/DevicesRounded";
import ExpandMoreRounded from "@mui/icons-material/ExpandMoreRounded";
import PersonSearchRounded from "@mui/icons-material/PersonSearchRounded";
import SearchRounded from "@mui/icons-material/SearchRounded";
import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { get, rangeQuery } from "../api/client";
import { useSite } from "../contexts/SiteContext";
import DataTable from "../components/DataTable";
import { ErrorState, Loading, NoSite } from "../components/States";
import {
  buildTraceMarkdown,
  entryExit,
  formatDuration,
  formatGap,
  matchedByLabel,
  type TraceSession,
  type VisitorSearchResult,
  type VisitorTrace,
} from "./visitorTrace";

export default function UserExplorerPage() {
  const { site, environment } = useSite();
  const [params, setParams] = useSearchParams();
  const subject = params.get("visitor") || "";
  // ?q= lets a report hand over a search instead of a single visitor: the
  // Frustration table knows which signal fired, not who hit it.
  const requestedQuery = params.get("q") || "";
  const scope = params.get("scope") === "device" ? "device" : "person";
  const [input, setInput] = useState(subject || requestedQuery);
  const [query, setQuery] = useState(requestedQuery);
  const [pages, setPages] = useState<VisitorTrace[]>([]);
  const [cursor, setCursor] = useState("");
  const [toast, setToast] = useState("");

  useEffect(() => {
    setInput(subject || requestedQuery);
    setPages([]);
    setCursor("");
  }, [subject, scope, requestedQuery]);

  useEffect(() => {
    if (requestedQuery) setQuery(requestedQuery);
  }, [requestedQuery]);

  const search = useQuery({
    queryKey: ["visitor-search", site?.site_id, environment, query],
    enabled: !!site && query.length >= 2,
    queryFn: () =>
      get<{ results: VisitorSearchResult[] }>(
        `/api/v1/sites/${site!.site_id}/visitor-search?q=${encodeURIComponent(query)}&${rangeQuery(90, site!.timezone)}`,
      ),
  });
  const trace = useQuery({
    queryKey: ["visitor-trace", site?.site_id, environment, subject, scope, cursor],
    enabled: !!site && !!subject,
    queryFn: () =>
      get<VisitorTrace>(
        `/api/v1/sites/${site!.site_id}/visitors/${encodeURIComponent(subject)}/timeline?${rangeQuery(365, site!.timezone)}&scope=${scope}&limit=200${cursor ? `&before=${encodeURIComponent(cursor)}` : ""}`,
      ),
  });

  useEffect(() => {
    if (!trace.data) return;
    setPages((previous) => {
      const seen = new Set(previous.flatMap((page) => page.sessions.map((s) => s.session_id + page.paging.next_before)));
      if (seen.size && previous.some((page) => page.paging.next_before === trace.data!.paging.next_before)) {
        return previous;
      }
      return [...previous, trace.data!];
    });
  }, [trace.data]);

  const open = (value: string, nextScope: "person" | "device" = "person") => {
    setParams({ visitor: value, scope: nextScope });
  };

  // Sessions from every loaded page, newest first, without duplicates from the
  // cursor boundary.
  const sessions = useMemo(() => {
    const merged = new Map<string, TraceSession>();
    for (const page of pages) {
      for (const session of page.sessions) {
        const key = `${session.visitor_id}|${session.session_id}`;
        const existing = merged.get(key);
        if (!existing) {
          merged.set(key, session);
          continue;
        }
        const ids = new Set(existing.events.map((event) => event.event_id));
        merged.set(key, {
          ...existing,
          events: [...session.events.filter((event) => !ids.has(event.event_id)), ...existing.events].sort(
            (a, b) => a.timestamp.localeCompare(b.timestamp),
          ),
        });
      }
    }
    return [...merged.values()].sort((a, b) => b.started_at.localeCompare(a.started_at));
  }, [pages]);

  if (!site) return <NoSite />;
  const latest = pages.length ? pages[pages.length - 1] : trace.data;
  const merged: VisitorTrace | undefined = latest ? { ...latest, sessions } : undefined;

  return (
    <Stack spacing={2}>
      <Card sx={{ p: 2.5 }}>
        <Stack direction="row" gap={1.2} alignItems="center" mb={0.5}>
          <PersonSearchRounded color="primary" />
          <Typography fontWeight={720}>방문자 추적</Typography>
        </Stack>
        <Typography variant="body2" color="text.secondary" mb={2}>
          User ID, Visitor ID, 부서, 조직, 페이지, 이벤트, 기능 이름으로 실제
          방문자를 찾습니다. 조회 사실은 Audit Log에 기록되며, 개인정보 설정에서
          Visitor Profile을 비활성화하면 이 화면과 API가 차단됩니다.
        </Typography>
        <Stack direction={{ xs: "column", sm: "row" }} gap={1}>
          <TextField
            size="small"
            label="검색어 또는 ID"
            placeholder="EMP001, 디지털플랫폼, /search, feature_used"
            value={input}
            onChange={(event) => setInput(event.target.value)}
            onKeyDown={(event) => {
              if (event.key !== "Enter" || !input.trim()) return;
              setQuery(input.trim());
            }}
            sx={{ minWidth: 320, flex: 1 }}
          />
          <Button
            variant="contained"
            startIcon={<SearchRounded />}
            disabled={input.trim().length < 2}
            onClick={() => setQuery(input.trim())}
          >
            검색
          </Button>
          <Button
            variant="outlined"
            disabled={!input.trim()}
            onClick={() => open(input.trim())}
          >
            바로 추적
          </Button>
        </Stack>
        {query.length >= 2 && (
          <Box mt={2}>
            {search.isLoading ? (
              <Loading />
            ) : search.error ? (
              <ErrorState error={search.error} />
            ) : (
              <DataTable
                title={`검색 결과 · "${query}"`}
                description="최근 활동 순입니다. 행을 클릭하면 사람 단위로 추적을 시작합니다."
                rows={(search.data?.results || []) as unknown as Record<string, unknown>[]}
                exportFilename="momento-visitor-search"
                columns={[
                  {
                    key: "matched_by",
                    label: "일치",
                    format: (v, row) => (
                      <Stack direction="row" gap={0.5} alignItems="center">
                        <Chip
                          size="small"
                          label={matchedByLabel[v as VisitorSearchResult["matched_by"]] || String(v)}
                        />
                        <Typography variant="caption" sx={{ overflowWrap: "anywhere" }}>
                          {String(row.matched_value || "")}
                        </Typography>
                      </Stack>
                    ),
                  },
                  {
                    key: "user_id",
                    label: "User",
                    format: (v) => (v ? String(v) : "익명"),
                  },
                  {
                    key: "visitor_id",
                    label: "Visitor",
                    format: (v) => (
                      <Typography variant="caption" className="mono">
                        {String(v).slice(0, 16)}
                      </Typography>
                    ),
                  },
                  { key: "sessions", label: "세션", align: "right" },
                  { key: "events", label: "이벤트", align: "right" },
                  { key: "conversions", label: "전환", align: "right" },
                  {
                    key: "last_seen",
                    label: "최근 활동",
                    format: (v) => (v ? new Date(String(v)).toLocaleString("ko-KR") : "—"),
                  },
                  {
                    key: "visitor_id",
                    label: "",
                    align: "right",
                    format: (v) => (
                      <Button size="small" onClick={() => open(String(v))}>
                        추적
                      </Button>
                    ),
                  },
                ]}
              />
            )}
          </Box>
        )}
      </Card>

      {!subject && (
        <Alert severity="info">
          추적할 방문자를 검색하거나, 세션·사용자 리포트에서 `추적` 버튼으로
          들어오세요. 사람 단위 범위는 SSO User ID로 연결된 모든 기기의 활동을
          하나의 시간순 기록으로 합칩니다.
        </Alert>
      )}

      {subject && trace.isLoading && !merged && <Loading />}
      {subject && trace.error && <ErrorState error={trace.error} retry={() => trace.refetch()} />}

      {merged && (
        <Box
          sx={{
            display: "grid",
            gridTemplateColumns: { xs: "1fr", lg: "320px 1fr" },
            gap: 2,
            alignItems: "start",
          }}
        >
          <Stack spacing={2}>
            <Card sx={{ p: 2.5 }}>
              <Typography variant="caption" color="text.secondary">
                {merged.user_id ? "SSO USER" : "ANONYMOUS VISITOR"}
              </Typography>
              <Typography
                className="mono"
                fontWeight={720}
                sx={{ overflowWrap: "anywhere" }}
              >
                {merged.user_id || merged.visitor_id}
              </Typography>
              <ToggleButtonGroup
                size="small"
                exclusive
                value={merged.scope}
                onChange={(_, value) => {
                  if (value) open(subject, value);
                }}
                sx={{ mt: 1.5 }}
              >
                <ToggleButton value="person" disabled={!merged.user_id}>
                  사람 단위
                </ToggleButton>
                <ToggleButton value="device">단일 Visitor</ToggleButton>
              </ToggleButtonGroup>
              {!merged.user_id && (
                <Typography variant="caption" color="text.secondary" display="block" mt={1}>
                  아직 identify되지 않은 방문자여서 기기 단위로만 추적할 수 있습니다.
                </Typography>
              )}
              <Stack direction="row" gap={0.5} mt={1.5} flexWrap="wrap">
                {merged.visitor_ids.map((visitorID) => (
                  <Tooltip key={visitorID} title={visitorID}>
                    <Chip
                      size="small"
                      icon={<DevicesRounded />}
                      variant={visitorID === merged.visitor_id ? "filled" : "outlined"}
                      label={visitorID.slice(0, 12)}
                      onClick={() => open(visitorID, "device")}
                    />
                  </Tooltip>
                ))}
              </Stack>
              <Divider sx={{ my: 2 }} />
              <Stack spacing={1}>
                {[
                  ["활동일", `${merged.summary.active_days}일`],
                  ["세션", merged.summary.sessions.toLocaleString("ko-KR")],
                  ["이벤트", merged.summary.events.toLocaleString("ko-KR")],
                  ["페이지뷰", merged.summary.page_views.toLocaleString("ko-KR")],
                  ["전환", merged.summary.conversions.toLocaleString("ko-KR")],
                  [
                    "최초 활동",
                    merged.summary.first_seen
                      ? new Date(merged.summary.first_seen).toLocaleString("ko-KR")
                      : "—",
                  ],
                  [
                    "최근 활동",
                    merged.summary.last_seen
                      ? new Date(merged.summary.last_seen).toLocaleString("ko-KR")
                      : "—",
                  ],
                ].map(([label, value]) => (
                  <Stack key={label} direction="row" justifyContent="space-between" gap={2}>
                    <Typography variant="caption" color="text.secondary">
                      {label}
                    </Typography>
                    <Typography variant="caption" fontWeight={650} textAlign="right">
                      {value}
                    </Typography>
                  </Stack>
                ))}
              </Stack>
              <Button
                fullWidth
                size="small"
                variant="outlined"
                startIcon={<ContentCopyRounded />}
                sx={{ mt: 2 }}
                onClick={() => {
                  navigator.clipboard
                    .writeText(buildTraceMarkdown(merged, site.name))
                    .then(() => setToast("추적 기록을 클립보드에 복사했습니다."))
                    .catch(() => setToast("클립보드 사용이 차단되어 있습니다."));
                }}
              >
                추적 기록 복사
              </Button>
            </Card>

            {!!merged.identity_links.length && (
              <Card sx={{ p: 2.5 }}>
                <Typography fontWeight={700} mb={1}>
                  식별 연결
                </Typography>
                <Typography variant="body2" color="text.secondary" mb={1.5}>
                  각 기기가 이 사람으로 연결된 시점입니다. fingerprint 없이
                  identify와 SSO만 사용합니다.
                </Typography>
                <Stack spacing={1}>
                  {merged.identity_links.map((link) => (
                    <Box key={link.visitor_id}>
                      <Typography variant="caption" className="mono">
                        {link.visitor_id.slice(0, 16)}
                      </Typography>
                      <Typography variant="caption" color="text.secondary" display="block">
                        연결 {new Date(link.linked_at).toLocaleString("ko-KR")} · {link.source}
                      </Typography>
                    </Box>
                  ))}
                </Stack>
              </Card>
            )}

            {!!merged.other_sites.length && (
              <Card sx={{ p: 2.5 }}>
                <Typography fontWeight={700} mb={1}>
                  다른 서비스 활동
                </Typography>
                <Stack spacing={0.8}>
                  {merged.other_sites.map((other) => (
                    <Stack key={other.site_id} direction="row" justifyContent="space-between" gap={1}>
                      <Typography variant="body2">{other.name}</Typography>
                      <Typography variant="caption" color="text.secondary">
                        {new Date(other.last_seen).toLocaleDateString("ko-KR")}
                      </Typography>
                    </Stack>
                  ))}
                </Stack>
              </Card>
            )}

            {!!merged.user_properties && Object.keys(merged.user_properties).length > 0 && (
              <Card sx={{ p: 2.5 }}>
                <Typography fontWeight={700} mb={1}>
                  User Property
                </Typography>
                <Typography
                  component="pre"
                  className="mono"
                  sx={{ m: 0, fontSize: 11, whiteSpace: "pre-wrap" }}
                >
                  {JSON.stringify(merged.user_properties, null, 2)}
                </Typography>
              </Card>
            )}

            <TopList title="많이 본 페이지" rows={merged.summary.top_pages} />
            <TopList title="많이 쓴 기능" rows={merged.summary.top_features} />
          </Stack>

          <Stack spacing={2}>
            <Card sx={{ p: 2.5 }}>
              <Stack
                direction={{ xs: "column", sm: "row" }}
                justifyContent="space-between"
                alignItems={{ sm: "center" }}
                gap={1}
              >
                <Box>
                  <Typography fontWeight={720}>세션 타임라인</Typography>
                  <Typography variant="body2" color="text.secondary">
                    최근 순서입니다. 세션 안의 이벤트는 시간순이며 이전 이벤트와의
                    간격을 함께 표시합니다.
                  </Typography>
                </Box>
                <Chip
                  size="small"
                  label={`세션 ${sessions.length}개 · 이벤트 ${sessions.reduce((sum, session) => sum + session.events.length, 0)}건 로드`}
                />
              </Stack>
            </Card>
            {sessions.map((session) => (
              <SessionCard key={`${session.visitor_id}|${session.session_id}`} session={session} />
            ))}
            {!sessions.length && (
              <Card sx={{ p: 6, textAlign: "center" }}>
                <Typography color="text.secondary">
                  선택한 기간에 활동이 없습니다.
                </Typography>
              </Card>
            )}
            {merged.paging.has_more && (
              <Button
                variant="outlined"
                startIcon={<ExpandMoreRounded />}
                disabled={trace.isFetching}
                onClick={() => setCursor(merged.paging.next_before)}
              >
                이전 기록 더 보기
              </Button>
            )}
          </Stack>
        </Box>
      )}
      <Snackbar
        open={!!toast}
        autoHideDuration={3000}
        onClose={() => setToast("")}
        message={toast}
      />
    </Stack>
  );
}

function TopList({ title, rows }: { title: string; rows: { value: string; events: number }[] }) {
  if (!rows?.length) return null;
  return (
    <Card sx={{ p: 2.5 }}>
      <Typography fontWeight={700} mb={1}>
        {title}
      </Typography>
      <Stack spacing={0.7}>
        {rows.map((row) => (
          <Stack key={row.value} direction="row" justifyContent="space-between" gap={1}>
            <Typography variant="body2" sx={{ overflowWrap: "anywhere" }}>
              {row.value}
            </Typography>
            <Typography variant="caption" color="text.secondary">
              {row.events.toLocaleString("ko-KR")}
            </Typography>
          </Stack>
        ))}
      </Stack>
    </Card>
  );
}

function SessionCard({ session }: { session: TraceSession }) {
  const path = entryExit(session);
  return (
    <Card sx={{ p: 2.5 }}>
      <Stack direction="row" gap={1} flexWrap="wrap" alignItems="center">
        <Typography fontWeight={700}>
          {new Date(session.started_at).toLocaleString("ko-KR")}
        </Typography>
        <Chip size="small" label={formatDuration(session.duration_seconds)} />
        <Chip
          size="small"
          color={session.engaged ? "success" : "default"}
          label={session.engaged ? "참여 세션" : "비참여"}
        />
        {session.conversions > 0 && (
          <Chip size="small" color="success" label={`전환 ${session.conversions}`} />
        )}
        {session.partial && (
          <Tooltip title="이 세션의 일부 이벤트만 현재 페이지에 로드되었습니다. 이전 기록 더 보기로 나머지를 불러오세요.">
            <Chip size="small" color="warning" variant="outlined" label="부분 로드" />
          </Tooltip>
        )}
      </Stack>
      <Stack direction="row" gap={1} flexWrap="wrap" mt={1}>
        {[
          session.device_type,
          session.browser,
          session.os,
          session.source ? `${session.source}${session.medium ? ` / ${session.medium}` : ""}` : "direct",
          session.campaign,
          session.network,
        ]
          .filter(Boolean)
          .map((label) => (
            <Chip key={label} size="small" variant="outlined" label={label} />
          ))}
      </Stack>
      {path && (
        <Typography variant="body2" color="text.secondary" mt={1} sx={{ overflowWrap: "anywhere" }}>
          {path}
        </Typography>
      )}
      <Divider sx={{ my: 1.5 }} />
      <Stack>
        {session.events.map((event, index) => (
          <Box
            key={event.event_id}
            sx={{ display: "grid", gridTemplateColumns: "16px 110px 1fr", gap: 1.2 }}
          >
            <Box sx={{ position: "relative", display: "flex", justifyContent: "center" }}>
              <Box
                sx={{
                  width: 9,
                  height: 9,
                  mt: 0.7,
                  borderRadius: "50%",
                  bgcolor: event.is_conversion
                    ? "#12A875"
                    : event.marker === "identified"
                      ? "#B4690E"
                      : "primary.main",
                  zIndex: 1,
                }}
              />
              {index < session.events.length - 1 && (
                <Box
                  sx={{
                    position: "absolute",
                    width: 1,
                    bgcolor: "#DDE2EC",
                    top: 14,
                    bottom: -6,
                  }}
                />
              )}
            </Box>
            <Box pt={0.1}>
              <Typography variant="caption" color="text.secondary" display="block">
                {new Date(event.timestamp).toLocaleTimeString("ko-KR")}
              </Typography>
              {formatGap(event.seconds_since_previous) && (
                <Typography variant="caption" color="text.disabled">
                  {formatGap(event.seconds_since_previous)}
                </Typography>
              )}
            </Box>
            <Box sx={{ pb: 2 }}>
              <Stack direction="row" gap={0.7} alignItems="center" flexWrap="wrap">
                <Chip
                  size="small"
                  color={event.is_conversion ? "success" : "default"}
                  label={event.event_name}
                />
                {event.marker === "identified" && (
                  <Chip size="small" color="warning" label="사용자 식별" />
                )}
                {event.traffic_class && event.traffic_class !== "normal" && (
                  <Chip size="small" variant="outlined" label={event.traffic_class} />
                )}
              </Stack>
              {event.page_url && (
                <Typography variant="body2" mt={0.5} sx={{ overflowWrap: "anywhere" }}>
                  {event.page_title ? `${event.page_title} · ` : ""}
                  {event.page_url}
                </Typography>
              )}
              {event.properties && Object.keys(event.properties).length > 0 && (
                <Typography variant="caption" color="text.secondary" className="mono">
                  {JSON.stringify(event.properties)}
                </Typography>
              )}
            </Box>
          </Box>
        ))}
      </Stack>
    </Card>
  );
}
