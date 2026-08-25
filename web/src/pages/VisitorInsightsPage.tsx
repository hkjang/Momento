import { useState } from "react";
import {
  Alert,
  Box,
  Button,
  Card,
  Chip,
  Divider,
  MenuItem,
  Snackbar,
  Stack,
  TextField,
  Tooltip,
  Typography,
} from "@mui/material";
import ContentCopyRounded from "@mui/icons-material/ContentCopyRounded";
import DownloadRounded from "@mui/icons-material/DownloadRounded";
import InsightsRounded from "@mui/icons-material/InsightsRounded";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { get, post, rangeQuery } from "../api/client";
import { useSite } from "../contexts/SiteContext";
import AnalysisToolbar from "../components/AnalysisToolbar";
import DataTable from "../components/DataTable";
import MetricCard from "../components/MetricCard";
import { ErrorState, Loading, NoSite } from "../components/States";
import {
  anomalySeverityLabel,
  anomalyStateLabel,
  buildInsightMarkdown,
  formatCredit,
  stateSummary,
  changeTone,
  formatChange,
  formatInsightValue,
  lifecycleName,
  severityLabel,
  type Anomaly,
  type AnomalyReport,
  type AnomalyTransition,
  type AttributionModel,
  type AttributionReport,
  type FindingSeverity,
  type InsightAudience,
  type VisitorInsightReport,
} from "./visitorInsights";

const anomalyColor: Record<Anomaly["severity"], "error" | "warning" | "success" | "default"> = {
  critical: "error",
  warning: "warning",
  positive: "success",
  normal: "default",
  insufficient_history: "default",
  unknown: "default",
};

const severityColor: Record<FindingSeverity, "error" | "warning" | "info" | "success"> = {
  critical: "error",
  warning: "warning",
  info: "info",
  positive: "success",
};

const kpiType: Record<string, "percent" | "duration" | undefined> = {
  percent: "percent",
  duration: "duration",
};

export default function VisitorInsightsPage() {
  const { site, environment } = useSite();
  const navigate = useNavigate();
  const [days, setDays] = useState(30);
  const [toast, setToast] = useState("");
  const [model, setModel] = useState("last_non_direct");
  const [halfLife, setHalfLife] = useState(7);
  const q = useQuery({
    queryKey: ["visitor-insights", site?.site_id, environment, days],
    enabled: !!site,
    queryFn: () =>
      get<VisitorInsightReport>(
        `/api/v1/sites/${site!.site_id}/visitor-insights?${rangeQuery(days, site!.timezone)}`,
      ),
  });
  const anomalies = useQuery({
    queryKey: ["anomalies", site?.site_id, environment],
    enabled: !!site,
    queryFn: () =>
      get<AnomalyReport>(`/api/v1/sites/${site!.site_id}/anomalies?environment=${environment}`),
  });
  const attribution = useQuery({
    queryKey: ["attribution", site?.site_id, environment, days, model, halfLife],
    enabled: !!site,
    queryFn: () =>
      get<{ report: AttributionReport; models: AttributionModel[] }>(
        `/api/v1/sites/${site!.site_id}/attribution?${rangeQuery(days, site!.timezone)}&model=${model}&half_life_days=${halfLife}`,
      ),
  });
  const saveSegment = useMutation({
    mutationFn: (audience: InsightAudience) =>
      post<{ id: string }>("/api/v1/segments", {
        site_id: site!.site_id,
        name: `${audience.label} (자동 생성)`,
        description: `방문자 인사이트에서 생성 · ${audience.action}`,
        definition: audience.segment,
        shared: false,
      }),
    onSuccess: () => {
      setToast("Segment를 저장했습니다. Segment 화면으로 이동합니다.");
      window.setTimeout(() => navigate("/segments"), 900);
    },
    onError: (error: Error) => setToast(`Segment 저장 실패: ${error.message}`),
  });
  if (!site) return <NoSite />;
  const toolbar = (
    <AnalysisToolbar
      days={days}
      setDays={setDays}
      environment={environment}
      timezone={site.timezone}
      updatedAt={q.dataUpdatedAt}
      refreshing={q.isFetching}
      refresh={() => void q.refetch()}
      comparePrevious
    />
  );
  if (q.isLoading)
    return (
      <Stack spacing={2}>
        {toolbar}
        <Loading />
      </Stack>
    );
  if (q.error)
    return (
      <Stack spacing={2}>
        {toolbar}
        <ErrorState error={q.error} retry={() => q.refetch()} />
      </Stack>
    );
  const report = q.data!;
  const markdown = buildInsightMarkdown(report, site.name, anomalies.data);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(markdown);
      setToast("인사이트 요약을 클립보드에 복사했습니다.");
    } catch {
      setToast("클립보드 사용이 차단되어 있습니다. 파일로 내려받으세요.");
    }
  };
  const download = () => {
    const url = URL.createObjectURL(
      new Blob([markdown], { type: "text/markdown;charset=utf-8" }),
    );
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `momento-visitor-insights-${site.site_id}-${days}d.md`;
    anchor.click();
    URL.revokeObjectURL(url);
  };

  return (
    <Stack spacing={2}>
      {toolbar}
      <Card sx={{ p: 2.5 }}>
        <Stack
          direction={{ xs: "column", md: "row" }}
          justifyContent="space-between"
          alignItems={{ md: "center" }}
          gap={1.5}
        >
          <Stack direction="row" gap={1.2} alignItems="flex-start">
            <InsightsRounded color="primary" />
            <Box>
              <Typography fontWeight={720}>{report.headline}</Typography>
              <Typography variant="caption" color="text.secondary">
                {report.from.slice(0, 10)} ~ {report.to.slice(0, 10)} · 비교 기간{" "}
                {report.previous_from.slice(0, 10)} ~{" "}
                {report.previous_to.slice(0, 10)}
              </Typography>
            </Box>
          </Stack>
          <Stack direction="row" gap={1}>
            <Button
              variant="contained"
              size="small"
              startIcon={<ContentCopyRounded />}
              onClick={() => void copy()}
            >
              요약 복사
            </Button>
            <Button
              variant="outlined"
              size="small"
              startIcon={<DownloadRounded />}
              onClick={download}
            >
              Markdown
            </Button>
          </Stack>
        </Stack>
      </Card>

      {anomalies.data && (
        <Card sx={{ p: 2.5 }}>
          <Stack direction="row" justifyContent="space-between" alignItems="center" gap={1} mb={1}>
            <Typography variant="h6">이상 감지</Typography>
            <Chip
              size="small"
              label={`${anomalies.data.evaluated_date.slice(0, 10)} · 같은 요일 최근 ${anomalies.data.baseline_weeks}주 비교`}
            />
          </Stack>
          <Typography variant="body2" color="text.secondary">
            {anomalies.data.note}
          </Typography>
          <Typography variant="caption" color="text.secondary" display="block" mb={2}>
            Action 채널에는{" "}
            {(anomalies.data.notify_on || ["new", "recovered"])
              .map((state) => anomalyStateLabel[state as AnomalyTransition["state"]] || state)
              .join(" · ")}{" "}
            상태만 전송합니다. 같은 이상을 매 실행마다 반복 통보하지 않습니다.
          </Typography>
          {anomalies.data.detected.length ? (
            <Stack spacing={1.2}>
              {anomalies.data.detected.map((anomaly) => (
                <Card key={anomaly.metric} variant="outlined" sx={{ p: 1.8 }}>
                  <Stack direction="row" gap={1} alignItems="center" flexWrap="wrap">
                    <Chip
                      size="small"
                      color={anomalyColor[anomaly.severity]}
                      label={anomalySeverityLabel[anomaly.severity]}
                    />
                    {transitionOf(anomalies.data.transitions, anomaly.metric) && (
                      <Tooltip title="신규는 이번에 처음 감지된 이상이고, 지속은 이전에 이미 알린 이상입니다. 알림은 기본적으로 신규와 회복만 전송합니다.">
                        <Chip
                          size="small"
                          variant="outlined"
                          color={
                            transitionOf(anomalies.data.transitions, anomaly.metric)!.state === "new"
                              ? "error"
                              : "default"
                          }
                          label={stateSummary(transitionOf(anomalies.data.transitions, anomaly.metric)!)}
                        />
                      </Tooltip>
                    )}
                    <Typography fontWeight={700}>{anomaly.label}</Typography>
                    <Chip
                      size="small"
                      variant="outlined"
                      label={`${anomaly.direction === "above" ? "▲" : "▼"} ${formatChange(anomaly.change_percent)}`}
                    />
                    <Chip size="small" variant="outlined" label={`${anomaly.robust_z.toFixed(1)}σ`} />
                  </Stack>
                  <Typography variant="body2" color="text.secondary" mt={0.6}>
                    {anomaly.evidence}
                  </Typography>
                  {anomaly.action && (
                    <Typography variant="body2" color="primary.main" mt={0.3}>
                      다음 행동 · {anomaly.action}
                    </Typography>
                  )}
                </Card>
              ))}
            </Stack>
          ) : (
            <Alert severity="success">
              감시 지표에서 기준선을 벗어난 변화가 없습니다.
            </Alert>
          )}
          {(anomalies.data.transitions || [])
            .filter((transition) => transition.state === "recovered")
            .map((transition) => (
              <Alert key={transition.metric} severity="info" sx={{ mt: 1.2 }}>
                <strong>{transition.label}</strong> 회복 · {transition.evidence}
              </Alert>
            ))}
          <Box mt={1.5}>
            <DataTable
              rows={anomalies.data.checked as unknown as Record<string, unknown>[]}
              exportFilename="momento-anomaly-check"
              columns={[
                { key: "label", label: "지표" },
                { key: "value", label: "당일", align: "right", format: (v) => Number(v).toLocaleString("ko-KR") },
                { key: "baseline", label: "기준선", align: "right", format: (v) => Number(v).toLocaleString("ko-KR") },
                { key: "robust_z", label: "편차", align: "right", format: (v) => `${Number(v).toFixed(1)}σ` },
                { key: "samples", label: "비교 표본", align: "right" },
                {
                  key: "severity",
                  label: "판정",
                  format: (v) => (
                    <Chip
                      size="small"
                      color={anomalyColor[v as Anomaly["severity"]]}
                      label={anomalySeverityLabel[v as Anomaly["severity"]]}
                    />
                  ),
                },
              ]}
            />
          </Box>
        </Card>
      )}

      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: { xs: "1fr 1fr", md: "repeat(5,1fr)" },
          gap: 2,
        }}
      >
        {report.kpis.map((kpi) => (
          <MetricCard
            key={kpi.key}
            label={kpi.label}
            value={kpi.current}
            previous={kpi.previous}
            type={kpiType[kpi.format]}
            tone={changeTone(kpi)}
          />
        ))}
      </Box>

      <Card sx={{ p: 2.5 }}>
        <Typography variant="h6">핵심 인사이트</Typography>
        <Typography variant="body2" color="text.secondary" mb={2}>
          영향이 큰 순서입니다. 각 항목은 사용한 근거를 함께 제시하므로 그대로
          공유하거나 검증할 수 있습니다.
        </Typography>
        <Stack spacing={1.5}>
          {report.findings.map((finding) => (
            <Card key={finding.id} variant="outlined" sx={{ p: 2 }}>
              <Stack direction="row" gap={1.2} alignItems="center" mb={1}>
                <Chip
                  size="small"
                  color={severityColor[finding.severity]}
                  label={severityLabel[finding.severity] || finding.severity}
                />
                <Typography fontWeight={700}>{finding.title}</Typography>
              </Stack>
              <Stack spacing={0.4}>
                <Typography variant="body2">근거 · {finding.evidence}</Typography>
                <Typography variant="body2" color="text.secondary">
                  원인 후보 · {finding.cause}
                </Typography>
                <Typography variant="body2" color="primary.main">
                  다음 행동 · {finding.action}
                </Typography>
              </Stack>
            </Card>
          ))}
        </Stack>
      </Card>

      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: { xs: "1fr", lg: "1fr 1fr" },
          gap: 2,
        }}
      >
        <Card sx={{ p: 2.5 }}>
          <Typography variant="h6">신규 · 재방문</Typography>
          <Typography variant="body2" color="text.secondary" mb={2}>
            신규는 선택한 환경에서 처음 관측된 사용자입니다.
          </Typography>
          <DataTable
            rows={report.lifecycle as unknown as Record<string, unknown>[]}
            exportFilename="momento-lifecycle"
            columns={[
              { key: "kind", label: "구분", format: (v) => lifecycleName(String(v)) },
              { key: "users", label: "방문자", align: "right" },
              {
                key: "share_percent",
                label: "비중",
                align: "right",
                format: (v) => `${Number(v).toFixed(1)}%`,
              },
              {
                key: "sessions_per_user",
                label: "1인당 방문",
                align: "right",
                format: (v) => Number(v).toFixed(2),
              },
              {
                key: "conversion_rate",
                label: "전환율",
                align: "right",
                format: (v) => `${Number(v).toFixed(1)}%`,
              },
            ]}
          />
          <Divider sx={{ my: 2 }} />
          <Typography fontWeight={700} mb={1}>
            실행 대상
          </Typography>
          <Stack spacing={1}>
            {report.audiences.map((audience) => (
              <Stack
                key={audience.key}
                direction="row"
                justifyContent="space-between"
                alignItems="center"
                gap={1}
              >
                <Box>
                  <Typography variant="body2" fontWeight={600}>
                    {audience.label}
                  </Typography>
                  <Typography variant="caption" color="text.secondary">
                    {audience.action}
                  </Typography>
                </Box>
                <Stack direction="row" gap={0.5} alignItems="center">
                  <Chip
                    size="small"
                    color={audience.users > 0 ? "primary" : "default"}
                    label={`${audience.users.toLocaleString("ko-KR")}명`}
                  />
                  {audience.segment && audience.users > 0 && (
                    <Tooltip
                      title={
                        audience.segment_note ||
                        "이 조건으로 Segment를 저장해 Query·Funnel·Action에서 재사용합니다."
                      }
                    >
                      <Button
                        size="small"
                        disabled={saveSegment.isPending}
                        onClick={() => saveSegment.mutate(audience)}
                      >
                        Segment 만들기
                      </Button>
                    </Tooltip>
                  )}
                </Stack>
              </Stack>
            ))}
          </Stack>
        </Card>

        <Card sx={{ p: 2.5 }}>
          <Typography variant="h6">유입 채널</Typography>
          <Typography variant="body2" color="text.secondary" mb={2}>
            Source·Medium을 채널 그룹으로 분류했습니다. 한 사용자가 여러 채널로
            방문하면 채널 합계는 전체 방문자보다 클 수 있습니다.
          </Typography>
          <DataTable
            rows={report.channels as unknown as Record<string, unknown>[]}
            exportFilename="momento-channels"
            columns={[
              { key: "channel", label: "채널" },
              { key: "users", label: "방문자", align: "right" },
              {
                key: "user_share_percent",
                label: "비중",
                align: "right",
                format: (v) => `${Number(v).toFixed(1)}%`,
              },
              {
                key: "conversion_rate",
                label: "전환율",
                align: "right",
                format: (v) => `${Number(v).toFixed(1)}%`,
              },
              {
                key: "change_percent",
                label: "전기간 대비",
                align: "right",
                format: (v) => (
                  <Typography
                    variant="body2"
                    color={
                      Number(v) > 1
                        ? "success.main"
                        : Number(v) < -1
                          ? "error.main"
                          : "text.secondary"
                    }
                  >
                    {formatChange(Number(v))}
                  </Typography>
                ),
              },
            ]}
          />
        </Card>
      </Box>

      {attribution.data && (
        <Card sx={{ p: 2.5 }}>
          <Stack
            direction={{ xs: "column", md: "row" }}
            justifyContent="space-between"
            alignItems={{ md: "center" }}
            gap={1.5}
          >
            <Box>
              <Typography variant="h6">전환 기여도</Typography>
              <Typography variant="body2" color="text.secondary">
                {attribution.data.report.description}
              </Typography>
            </Box>
            <Stack direction="row" gap={1.5}>
              <TextField
                select
                size="small"
                label="기여 모델"
                value={model}
                onChange={(event) => setModel(event.target.value)}
                sx={{ minWidth: 220 }}
              >
                {attribution.data.models.map((item) => (
                  <MenuItem key={item.key} value={item.key}>
                    {item.label}
                    {item.multi_touch ? " · 다중" : ""}
                  </MenuItem>
                ))}
              </TextField>
              {model === "time_decay" && (
                <TextField
                  select
                  size="small"
                  label="반감기"
                  value={halfLife}
                  onChange={(event) => setHalfLife(Number(event.target.value))}
                  sx={{ minWidth: 120 }}
                >
                  {[1, 3, 7, 14, 30].map((value) => (
                    <MenuItem key={value} value={value}>
                      {value}일
                    </MenuItem>
                  ))}
                </TextField>
              )}
            </Stack>
          </Stack>
          <Stack direction="row" gap={1} mt={1.5} flexWrap="wrap">
            <Chip size="small" label={`전환 ${attribution.data.report.total_conversions.toLocaleString("ko-KR")}건`} />
            <Chip
              size="small"
              color="primary"
              label={`배분 ${formatCredit(attribution.data.report.attributed_conversions)}건`}
            />
            {attribution.data.report.multi_touch && (
              <Tooltip title="다중 터치 모델은 하나의 전환을 여러 방문에 나눠 배분하므로 채널별 배분 전환이 소수로 표시됩니다.">
                <Chip size="small" color="secondary" variant="outlined" label="다중 터치 배분" />
              </Tooltip>
            )}
            {attribution.data.report.average_path_touches > 0 && (
              <Chip
                size="small"
                variant="outlined"
                label={`평균 경로 ${attribution.data.report.average_path_touches.toFixed(1)} 방문`}
              />
            )}
            {attribution.data.report.half_life_days ? (
              <Chip size="small" variant="outlined" label={`반감기 ${attribution.data.report.half_life_days}일`} />
            ) : null}
            {attribution.data.report.unattributed_conversions > 0 && (
              <Tooltip title={`Lookback ${attribution.data.report.lookback_days}일 안에 방문 기록이 없어 배분하지 못한 전환입니다.`}>
                <Chip
                  size="small"
                  color="warning"
                  variant="outlined"
                  label={`미배분 ${formatCredit(attribution.data.report.unattributed_conversions)}건`}
                />
              </Tooltip>
            )}
            <Chip size="small" variant="outlined" label={`Lookback ${attribution.data.report.lookback_days}일`} />
          </Stack>
          <Box mt={2}>
            <DataTable
              rows={attribution.data.report.channels as unknown as Record<string, unknown>[]}
              exportFilename={`momento-attribution-${model}`}
              columns={[
                { key: "channel", label: "채널" },
                {
                  key: "credited_conversions",
                  label: "배분 전환",
                  align: "right",
                  format: (v) => formatCredit(Number(v)),
                },
                {
                  key: "credit_share_percent",
                  label: "비중",
                  align: "right",
                  format: (v) => `${Number(v).toFixed(1)}%`,
                },
                { key: "credited_users", label: "전환 사용자", align: "right" },
                { key: "touched_conversions", label: "관여 전환", align: "right" },
                {
                  key: "touch_share_percent",
                  label: "관여 비중",
                  align: "right",
                  format: (v) => `${Number(v).toFixed(1)}%`,
                },
                {
                  key: "assist_only_conversions",
                  label: "관여만",
                  align: "right",
                  format: (v) =>
                    Number(v) > 0 ? (
                      <Tooltip title="경로에는 있었지만 이 모델에서 배분받지 못한 전환입니다. 모델을 바꾸면 평가가 달라집니다.">
                        <span>{Number(v).toLocaleString("ko-KR")}</span>
                      </Tooltip>
                    ) : (
                      "0"
                    ),
                },
              ]}
            />
          </Box>
          <Typography variant="caption" color="text.secondary" display="block" mt={1}>
            {attribution.data.report.note}
          </Typography>
        </Card>
      )}

      <DataTable
        title="진입 페이지"
        description="첫 페이지별 세션과 이탈률입니다. 비중이 큰데 이탈률이 높은 페이지가 가장 먼저 개선할 대상입니다."
        rows={report.landing_pages as unknown as Record<string, unknown>[]}
        searchable
        exportFilename={`momento-landing-${days}d.csv`}
        columns={[
          { key: "page", label: "진입 페이지" },
          { key: "sessions", label: "세션", align: "right" },
          {
            key: "session_share_percent",
            label: "비중",
            align: "right",
            format: (v) => `${Number(v).toFixed(1)}%`,
          },
          {
            key: "bounce_rate",
            label: "이탈률",
            align: "right",
            format: (v) => (
              <Chip
                size="small"
                color={Number(v) >= 70 ? "error" : Number(v) >= 50 ? "warning" : "default"}
                label={`${Number(v).toFixed(1)}%`}
              />
            ),
          },
          {
            key: "engagement_rate",
            label: "참여율",
            align: "right",
            format: (v) => `${Number(v).toFixed(1)}%`,
          },
          {
            key: "conversion_rate",
            label: "전환율",
            align: "right",
            format: (v) => `${Number(v).toFixed(1)}%`,
          },
          {
            key: "average_seconds",
            label: "평균 체류",
            align: "right",
            format: (v) => formatInsightValue(Number(v), "duration"),
          },
        ]}
      />

      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: { xs: "1fr", lg: "repeat(3,1fr)" },
          gap: 2,
        }}
      >
        <BucketCard
          title="방문 빈도"
          description="기간 내 방문 횟수 분포입니다."
          rows={report.frequency}
        />
        <BucketCard
          title="최근 활동"
          description="마지막 활동 시점 분포입니다. 오래된 구간이 휴면 위험군입니다."
          rows={report.recency}
        />
        <Card sx={{ p: 2.5 }}>
          <Typography variant="h6">기기</Typography>
          <Typography variant="body2" color="text.secondary" mb={2}>
            기기 간 전환율 격차는 경험 문제의 신호입니다.
          </Typography>
          <DataTable
            rows={report.devices as unknown as Record<string, unknown>[]}
            exportFilename="momento-devices"
            columns={[
              { key: "device", label: "기기" },
              { key: "users", label: "방문자", align: "right" },
              {
                key: "share_percent",
                label: "비중",
                align: "right",
                format: (v) => `${Number(v).toFixed(1)}%`,
              },
              {
                key: "conversion_rate",
                label: "전환율",
                align: "right",
                format: (v) => `${Number(v).toFixed(1)}%`,
              },
            ]}
          />
        </Card>
      </Box>

      <Alert severity="info">
        <Stack spacing={0.3}>
          {report.notes.map((note) => (
            <Typography key={note} variant="body2">
              {note}
            </Typography>
          ))}
        </Stack>
      </Alert>
      <Snackbar
        open={!!toast}
        autoHideDuration={3000}
        onClose={() => setToast("")}
        message={toast}
      />
    </Stack>
  );
}

/** transitionOf finds the alert state for one metric, if Momento has one. */
function transitionOf(
  transitions: AnomalyTransition[] | undefined,
  metric: string,
): AnomalyTransition | undefined {
  return (transitions || []).find((transition) => transition.metric === metric);
}

function BucketCard({
  title,
  description,
  rows,
}: {
  title: string;
  description: string;
  rows: VisitorInsightReport["frequency"];
}) {
  return (
    <Card sx={{ p: 2.5 }}>
      <Typography variant="h6">{title}</Typography>
      <Typography variant="body2" color="text.secondary" mb={2}>
        {description}
      </Typography>
      <DataTable
        rows={rows as unknown as Record<string, unknown>[]}
        exportFilename={`momento-${title}`}
        columns={[
          { key: "label", label: "구간" },
          { key: "users", label: "사용자", align: "right" },
          {
            key: "share_percent",
            label: "비중",
            align: "right",
            format: (v) => `${Number(v).toFixed(1)}%`,
          },
          {
            key: "conversion_rate",
            label: "전환율",
            align: "right",
            format: (v) => `${Number(v).toFixed(1)}%`,
          },
        ]}
      />
    </Card>
  );
}
