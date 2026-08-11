import { useState } from "react";
import {
  Alert,
  Box,
  Button,
  Card,
  Chip,
  Divider,
  Snackbar,
  Stack,
  Typography,
} from "@mui/material";
import ContentCopyRounded from "@mui/icons-material/ContentCopyRounded";
import DownloadRounded from "@mui/icons-material/DownloadRounded";
import InsightsRounded from "@mui/icons-material/InsightsRounded";
import { useQuery } from "@tanstack/react-query";
import { get, rangeQuery } from "../api/client";
import { useSite } from "../contexts/SiteContext";
import AnalysisToolbar from "../components/AnalysisToolbar";
import DataTable from "../components/DataTable";
import MetricCard from "../components/MetricCard";
import { ErrorState, Loading, NoSite } from "../components/States";
import {
  buildInsightMarkdown,
  changeTone,
  formatChange,
  formatInsightValue,
  lifecycleName,
  severityLabel,
  type FindingSeverity,
  type VisitorInsightReport,
} from "./visitorInsights";

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
  const [days, setDays] = useState(30);
  const [toast, setToast] = useState("");
  const q = useQuery({
    queryKey: ["visitor-insights", site?.site_id, environment, days],
    enabled: !!site,
    queryFn: () =>
      get<VisitorInsightReport>(
        `/api/v1/sites/${site!.site_id}/visitor-insights?${rangeQuery(days, site!.timezone)}`,
      ),
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
  const markdown = buildInsightMarkdown(report, site.name);
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
                <Chip
                  size="small"
                  color={audience.users > 0 ? "primary" : "default"}
                  label={`${audience.users.toLocaleString("ko-KR")}명`}
                />
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
