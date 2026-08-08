import { useState } from "react";
import {
  Alert,
  Box,
  Button,
  Card,
  Chip,
  Divider,
  Stack,
  TextField,
  Typography,
} from "@mui/material";
import SearchRounded from "@mui/icons-material/SearchRounded";
import { useQuery } from "@tanstack/react-query";
import { get, rangeQuery } from "../api/client";
import { useSite } from "../contexts/SiteContext";
import { ErrorState, Loading, NoSite } from "../components/States";

interface TimelineEvent {
  event_id: string;
  event_name: string;
  timestamp: string;
  session_id: string;
  page_url?: string;
  source?: string;
  device_type?: string;
  network?: string;
  properties: Record<string, unknown>;
  is_conversion: boolean;
}

interface Timeline {
  visitor_id: string;
  user_id?: string;
  linked_visitor_ids?: string[];
  user_properties?: Record<string, unknown>;
  first_seen?: string;
  last_seen?: string;
  sessions: number;
  conversions: number;
  events: TimelineEvent[];
}

interface Identity {
  user_id: string;
  visitor_count: number;
  visitor_ids: string[];
  first_seen: string;
  linked_at: string;
  last_seen: string;
  events: number;
  conversions: number;
}

export default function UserExplorerPage() {
  const { site } = useSite();
  const [input, setInput] = useState("");
  const [visitor, setVisitor] = useState("");
  const recent = useQuery({
    queryKey: ["visitor-suggestions", site?.site_id, site?.timezone],
    queryFn: () =>
      get<{ visitor_id: string; user_id?: string }[]>(
        `/api/v1/sites/${site!.site_id}/visitors?${rangeQuery(30, site!.timezone)}`,
      ),
    enabled: !!site,
  });
  const timeline = useQuery({
    queryKey: ["visitor-timeline", site?.site_id, site?.timezone, visitor],
    queryFn: () =>
      get<Timeline>(
        `/api/v1/sites/${site!.site_id}/visitors/${encodeURIComponent(visitor)}/timeline?${rangeQuery(365, site!.timezone)}&limit=300`,
      ),
    enabled: !!site && !!visitor,
  });
  const identities = useQuery({
    queryKey: ["identity-graph", site?.site_id],
    queryFn: () =>
      get<Identity[]>(`/api/v1/sites/${site!.site_id}/identities`),
    enabled: !!site,
  });
  if (!site) return <NoSite />;
  return (
    <Stack spacing={2}>
      <Card sx={{ p: 2.5 }}>
        <Typography fontWeight={720}>Visitor Timeline 검색</Typography>
        <Typography variant="body2" color="text.secondary" mb={2}>
          개인정보 설정에서 Visitor Profile을 비활성화하면 이 화면과 API가
          차단됩니다.
        </Typography>
        <Stack direction={{ xs: "column", sm: "row" }} gap={1}>
          <TextField
            size="small"
            label="Visitor ID"
            value={input}
            onChange={(event) => setInput(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && input.trim())
                setVisitor(input.trim());
            }}
            sx={{ minWidth: 320, flex: 1 }}
          />
          <Button
            variant="contained"
            startIcon={<SearchRounded />}
            disabled={!input.trim()}
            onClick={() => setVisitor(input.trim())}
          >
            조회
          </Button>
        </Stack>
        {!!recent.data?.length && (
          <Stack direction="row" gap={0.75} mt={2} flexWrap="wrap">
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ alignSelf: "center" }}
            >
              최근 Visitor
            </Typography>
            {recent.data.slice(0, 8).map((item) => (
              <Chip
                key={item.visitor_id}
                size="small"
                label={item.user_id || item.visitor_id}
                variant="outlined"
                onClick={() => {
                  setInput(item.visitor_id);
                  setVisitor(item.visitor_id);
                }}
              />
            ))}
          </Stack>
        )}
      </Card>
      {!!identities.data?.length && (
        <Card sx={{ p: 2.5 }}>
          <Typography fontWeight={720}>Deterministic Identity Graph</Typography>
          <Typography variant="body2" color="text.secondary" mb={2}>
            SDK identify 또는 SSO User ID로 연결된 여러 Visitor를 한 사용자로
            분석합니다. 확률 기반 fingerprint는 사용하지 않습니다.
          </Typography>
          <Stack spacing={1.5}>
            {identities.data.slice(0, 12).map((identity) => (
              <Box
                key={identity.user_id}
                sx={{
                  display: "grid",
                  gridTemplateColumns: { xs: "1fr", md: "220px 1fr auto" },
                  gap: 1.5,
                  alignItems: "center",
                  p: 1.5,
                  border: "1px solid",
                  borderColor: "divider",
                  borderRadius: 2,
                }}
              >
                <Box>
                  <Typography className="mono" fontWeight={700}>
                    {identity.user_id}
                  </Typography>
                  <Typography variant="caption" color="text.secondary">
                    Visitor {identity.visitor_count}개 · 이벤트 {identity.events.toLocaleString("ko-KR")}개
                  </Typography>
                </Box>
                <Stack direction="row" gap={0.75} flexWrap="wrap">
                  {identity.visitor_ids.slice(0, 8).map((visitorID) => (
                    <Chip
                      key={visitorID}
                      size="small"
                      variant="outlined"
                      label={visitorID}
                      onClick={() => {
                        setInput(visitorID);
                        setVisitor(visitorID);
                      }}
                    />
                  ))}
                </Stack>
                <Typography variant="caption" color="text.secondary">
                  최근 {new Date(identity.last_seen).toLocaleString("ko-KR")}
                </Typography>
              </Box>
            ))}
          </Stack>
        </Card>
      )}
      {timeline.isLoading && <Loading />}
      {timeline.error && <ErrorState error={timeline.error} />}
      {timeline.data && (
        <Box
          sx={{
            display: "grid",
            gridTemplateColumns: { xs: "1fr", lg: "300px 1fr" },
            gap: 2,
          }}
        >
          <Card sx={{ p: 2.5, height: "fit-content" }}>
            <Typography variant="caption" color="text.secondary">
              VISITOR
            </Typography>
            <Typography
              className="mono"
              fontWeight={720}
              sx={{ overflowWrap: "anywhere" }}
            >
              {timeline.data.visitor_id}
            </Typography>
            {timeline.data.user_id && (
              <Chip
                sx={{ mt: 1 }}
                size="small"
                label={`User ${timeline.data.user_id}`}
              />
            )}
            {!!timeline.data.linked_visitor_ids?.length && (
              <Stack direction="row" gap={0.5} mt={1} flexWrap="wrap">
                {timeline.data.linked_visitor_ids.map((visitorID) => (
                  <Chip
                    key={visitorID}
                    size="small"
                    variant={
                      visitorID === timeline.data.visitor_id
                        ? "filled"
                        : "outlined"
                    }
                    label={visitorID}
                    onClick={() => {
                      setInput(visitorID);
                      setVisitor(visitorID);
                    }}
                  />
                ))}
              </Stack>
            )}
            <Divider sx={{ my: 2 }} />
            <Stack spacing={1.2}>
              {[
                ["세션", timeline.data.sessions],
                ["전환", timeline.data.conversions],
                ["이벤트", timeline.data.events.length],
                [
                  "최초 활동",
                  timeline.data.first_seen
                    ? new Date(timeline.data.first_seen).toLocaleString("ko-KR")
                    : "—",
                ],
                [
                  "최근 활동",
                  timeline.data.last_seen
                    ? new Date(timeline.data.last_seen).toLocaleString("ko-KR")
                    : "—",
                ],
              ].map(([label, value]) => (
                <Stack
                  key={String(label)}
                  direction="row"
                  justifyContent="space-between"
                  gap={2}
                >
                  <Typography variant="caption" color="text.secondary">
                    {label}
                  </Typography>
                  <Typography
                    variant="caption"
                    fontWeight={650}
                    textAlign="right"
                  >
                    {value}
                  </Typography>
                </Stack>
              ))}
            </Stack>
            {timeline.data.user_properties &&
              Object.keys(timeline.data.user_properties).length > 0 && (
                <Alert severity="info" sx={{ mt: 2 }}>
                  <Typography
                    component="pre"
                    className="mono"
                    sx={{ m: 0, fontSize: 11, whiteSpace: "pre-wrap" }}
                  >
                    {JSON.stringify(timeline.data.user_properties, null, 2)}
                  </Typography>
                </Alert>
              )}
          </Card>
          <Card sx={{ p: 2.5 }}>
            <Typography fontWeight={720} mb={2}>
              최근 1년 Event Timeline
            </Typography>
            <Stack>
              {timeline.data.events.map((event, index) => (
                <Box
                  key={event.event_id}
                  sx={{
                    display: "grid",
                    gridTemplateColumns: "18px 150px 1fr",
                    gap: 1.5,
                  }}
                >
                  <Box
                    sx={{
                      position: "relative",
                      display: "flex",
                      justifyContent: "center",
                    }}
                  >
                    <Box
                      sx={{
                        width: 9,
                        height: 9,
                        mt: 0.7,
                        borderRadius: "50%",
                        bgcolor: event.is_conversion
                          ? "#12A875"
                          : "primary.main",
                        zIndex: 1,
                      }}
                    />
                    {index < timeline.data.events.length - 1 && (
                      <Box
                        sx={{
                          position: "absolute",
                          width: 1,
                          bgcolor: "#DDE2EC",
                          top: 14,
                          bottom: -8,
                        }}
                      />
                    )}
                  </Box>
                  <Typography variant="caption" color="text.secondary" pt={0.2}>
                    {new Date(event.timestamp).toLocaleString("ko-KR")}
                  </Typography>
                  <Box sx={{ pb: 2.5 }}>
                    <Stack
                      direction="row"
                      gap={0.75}
                      alignItems="center"
                      flexWrap="wrap"
                    >
                      <Chip
                        size="small"
                        color={event.is_conversion ? "success" : "default"}
                        label={event.event_name}
                      />
                      {event.device_type && (
                        <Chip
                          size="small"
                          variant="outlined"
                          label={event.device_type}
                        />
                      )}
                      {event.network && (
                        <Typography variant="caption">
                          {event.network}
                        </Typography>
                      )}
                    </Stack>
                    {event.page_url && (
                      <Typography
                        variant="body2"
                        mt={0.6}
                        sx={{ overflowWrap: "anywhere" }}
                      >
                        {event.page_url}
                      </Typography>
                    )}
                    {event.properties &&
                      Object.keys(event.properties).length > 0 && (
                        <Typography
                          variant="caption"
                          color="text.secondary"
                          className="mono"
                        >
                          {JSON.stringify(event.properties)}
                        </Typography>
                      )}
                  </Box>
                </Box>
              ))}
              {!timeline.data.events.length && (
                <Typography color="text.secondary">
                  해당 기간에 Event가 없습니다.
                </Typography>
              )}
            </Stack>
          </Card>
        </Box>
      )}
    </Stack>
  );
}
