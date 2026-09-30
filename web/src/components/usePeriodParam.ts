import { useCallback } from "react";
import { useSearchParams } from "react-router-dom";
import { PERIOD_PARAM, parsePeriod, withPeriod } from "./periodParam";

// 분석 도구 모음(AnalysisToolbar)이 내주는 기간.
export const TOOLBAR_PERIODS = [7, 30, 90] as const;

// useState(기본 기간) 자리에 그대로 들어가는 훅. 기간을 바꿔도 방문 기록이 쌓이지 않도록
// 주소를 교체(replace)한다 — 뒤로 가기는 이전 화면으로 가야지 이전 기간으로 가면 안 된다.
export function usePeriodParam(
  fallback: number,
  options: readonly number[] = TOOLBAR_PERIODS,
) {
  const [params, setParams] = useSearchParams();
  const days = parsePeriod(params.get(PERIOD_PARAM), fallback, options);
  const setDays = useCallback(
    (next: number) =>
      setParams((current) => withPeriod(current, next, fallback), {
        replace: true,
      }),
    [fallback, setParams],
  );
  return [days, setDays] as const;
}
