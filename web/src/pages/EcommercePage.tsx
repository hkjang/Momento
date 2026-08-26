import { useState } from "react";
import { Box, Card, Chip, Stack, Typography } from "@mui/material";
import PaymentsOutlined from "@mui/icons-material/PaymentsOutlined";
import ReceiptLongOutlined from "@mui/icons-material/ReceiptLongOutlined";
import ShoppingCartOutlined from "@mui/icons-material/ShoppingCartOutlined";
import TrendingUpRounded from "@mui/icons-material/TrendingUpRounded";
import { useQuery } from "@tanstack/react-query";
import ReactECharts from "../components/Chart";
import DataTable from "../components/DataTable";
import MetricCard from "../components/MetricCard";
import { get, rangeQuery } from "../api/client";
import { useSite } from "../contexts/SiteContext";
import { ErrorState, Loading, NoSite } from "../components/States";
import RangeSelect from "../components/RangeSelect";
import { narrowerRange } from "../components/queryError";

interface EcommerceData {
  summary: {
    revenue: number;
    refunds: number;
    net_revenue: number;
    transactions: number;
    buyers: number;
    average_order_value: number;
    purchase_conversion_rate: number;
    cart_users: number;
    checkout_users: number;
  };
  funnel: { event: string; users: number }[];
  products: Record<string, unknown>[];
}

const labels: Record<string, string> = {
  view_item: "상품 조회",
  add_to_cart: "장바구니",
  begin_checkout: "결제 시작",
  purchase: "구매",
};

export default function EcommercePage() {
  const { site } = useSite();
  const [days, setDays] = useState(30);
  const query = useQuery({
    queryKey: ["ecommerce", site?.site_id, site?.timezone, days],
    queryFn: () =>
      get<EcommerceData>(
        `/api/v1/sites/${site!.site_id}/ecommerce?${rangeQuery(days, site!.timezone)}`,
      ),
    enabled: !!site,
  });
  const narrower = narrowerRange(days);
  if (!site) return <NoSite />;
  const range = <RangeSelect days={days} setDays={setDays} maxExactDays={site.max_exact_days} timezone={site.timezone} />;
  if (query.isLoading)
    return (
      <Stack spacing={2}>
        {range}
        <Loading />
      </Stack>
    );
  if (query.error)
    return (
      <Stack spacing={2}>
        {range}
        <ErrorState
          error={query.error}
          retry={() => query.refetch()}
          narrowRange={narrower === null ? undefined : () => setDays(narrower)}
        />
      </Stack>
    );
  const data = query.data!;
  return (
    <Stack spacing={2}>
      {range}
      <Box sx={{ display: "flex", justifyContent: "flex-end" }}>
        <Chip label="최근 30일" variant="outlined" />
      </Box>
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: {
            xs: "1fr",
            sm: "repeat(2,1fr)",
            xl: "repeat(4,1fr)",
          },
          gap: 2,
        }}
      >
        <MetricCard
          label="순매출"
          value={data.summary.net_revenue}
          type="currency"
          icon={<PaymentsOutlined />}
        />
        <MetricCard
          label="거래"
          value={data.summary.transactions}
          icon={<ReceiptLongOutlined />}
        />
        <MetricCard
          label="평균 주문 금액"
          value={data.summary.average_order_value}
          type="currency"
          icon={<ShoppingCartOutlined />}
        />
        <MetricCard
          label="구매 전환율"
          value={data.summary.purchase_conversion_rate}
          type="percent"
          icon={<TrendingUpRounded />}
        />
      </Box>
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: { xs: "1fr", lg: "1.2fr .8fr" },
          gap: 2,
        }}
      >
        <Card sx={{ p: 2.5 }}>
          <Typography fontWeight={720}>Commerce Funnel</Typography>
          <Typography variant="body2" color="text.secondary">
            상품 조회부터 구매까지의 고유 사용자 흐름입니다.
          </Typography>
          <ReactECharts
            style={{ height: 330 }}
            option={{
              tooltip: { trigger: "axis" },
              grid: { left: 20, right: 20, bottom: 20, containLabel: true },
              xAxis: {
                type: "category",
                data: data.funnel.map(
                  (item) => labels[item.event] || item.event,
                ),
              },
              yAxis: {
                type: "value",
                splitLine: { lineStyle: { color: "#EEF1F5" } },
              },
              series: [
                {
                  type: "bar",
                  data: data.funnel.map((item, index) => ({
                    value: item.users,
                    itemStyle: {
                      color: ["#5B5CE2", "#7779EA", "#999AF1", "#12A875"][
                        index
                      ],
                      borderRadius: [7, 7, 0, 0],
                    },
                  })),
                  barMaxWidth: 92,
                  label: { show: true, position: "top" },
                },
              ],
            }}
          />
        </Card>
        <Card sx={{ p: 2.5 }}>
          <Typography fontWeight={720} mb={2}>
            매출 구성
          </Typography>
          <Stack spacing={2}>
            {[
              ["총매출", data.summary.revenue],
              ["환불", data.summary.refunds],
              ["순매출", data.summary.net_revenue],
              ["구매자", data.summary.buyers],
              ["장바구니 사용자", data.summary.cart_users],
              ["결제 시작 사용자", data.summary.checkout_users],
            ].map(([label, value], index) => (
              <Stack
                key={String(label)}
                direction="row"
                justifyContent="space-between"
              >
                <Typography variant="body2" color="text.secondary">
                  {label}
                </Typography>
                <Typography variant="body2" fontWeight={700}>
                  {index < 3
                    ? new Intl.NumberFormat("ko-KR", {
                        style: "currency",
                        currency: "KRW",
                        maximumFractionDigits: 0,
                      }).format(Number(value))
                    : Intl.NumberFormat("ko-KR").format(Number(value))}
                </Typography>
              </Stack>
            ))}
          </Stack>
        </Card>
      </Box>
      <Box>
        <Typography variant="h6" mb={1.5}>
          상품 성과
        </Typography>
        <DataTable
          columns={[
            { key: "item_id", label: "상품 ID" },
            { key: "item_name", label: "상품명" },
            { key: "category", label: "카테고리" },
            { key: "brand", label: "브랜드" },
            { key: "quantity", label: "수량", align: "right" },
            {
              key: "revenue",
              label: "상품 매출",
              align: "right",
              format: (value: unknown) =>
                new Intl.NumberFormat("ko-KR", {
                  style: "currency",
                  currency: "KRW",
                  maximumFractionDigits: 0,
                }).format(Number(value)),
            },
            { key: "transactions", label: "거래", align: "right" },
          ]}
          rows={data.products}
        />
      </Box>
    </Stack>
  );
}
