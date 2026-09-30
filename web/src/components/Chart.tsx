import { useEffect, useRef, type CSSProperties } from "react";
import * as echarts from "echarts/core";
import type { EChartsCoreOption } from "echarts/core";
import { BarChart, LineChart, SankeyChart } from "echarts/charts";
import {
  GridComponent,
  LegendComponent,
  TooltipComponent,
} from "echarts/components";
import { CanvasRenderer } from "echarts/renderers";
import { optionSignature } from "./chartOption";

echarts.use([
  BarChart,
  LineChart,
  SankeyChart,
  GridComponent,
  LegendComponent,
  TooltipComponent,
  CanvasRenderer,
]);

// 움직임을 줄이도록 설정한 사람에게는 진입·전환 애니메이션을 보이지 않는다.
function withMotionPreference(option: unknown): EChartsCoreOption {
  const reduce =
    typeof window !== "undefined" &&
    window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
  const base = option as EChartsCoreOption;
  return reduce ? { ...base, animation: false } : base;
}

export default function Chart({
  option,
  style,
}: {
  option: unknown;
  style?: CSSProperties;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const instance = useRef<echarts.ECharts | null>(null);
  const latest = useRef(option);
  latest.current = option;
  // One instance for the life of the element (chartOption.ts says why).
  useEffect(() => {
    if (!ref.current) return;
    const chart = echarts.init(ref.current);
    instance.current = chart;
    chart.setOption(withMotionPreference(latest.current));
    const observer = new ResizeObserver(() => chart.resize());
    observer.observe(ref.current);
    return () => {
      observer.disconnect();
      chart.dispose();
      instance.current = null;
    };
  }, []);
  // Redraw only when the option says something new. notMerge, because the old
  // behaviour was a fresh chart: a series that disappeared must not linger.
  const signature = optionSignature(option);
  const drawn = useRef(signature);
  useEffect(() => {
    if (!instance.current || drawn.current === signature) return;
    drawn.current = signature;
    instance.current.setOption(withMotionPreference(latest.current), {
      notMerge: true,
    });
  }, [signature]);
  return <div ref={ref} style={style} />;
}
