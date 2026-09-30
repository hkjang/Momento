import { useCallback, useMemo } from "react";
import { useSearchParams } from "react-router-dom";
import {
  PERIOD_PARAM,
  parsePeriod,
  parseRange,
  periodKey,
  rangeLength,
  withPeriod,
  withRange,
  type DateRange,
} from "./periodParam";

// 분석 도구 모음(AnalysisToolbar)이 내주는 기간.
export const TOOLBAR_PERIODS = [7, 30, 90] as const;

// 화면이 조회에 쓰는 기간. custom 이 있으면 그 날짜들이고, 없으면 오늘로 끝나는 최근
// days 일이다. days 는 어느 쪽이든 기간의 길이이므로 「N일」 문구와 정책 비교에 그대로
// 쓸 수 있다.
export interface Period {
  days: number;
  custom: DateRange | null;
  key: string;
}

// useState(기본 기간) 자리에 그대로 들어가는 훅. 기간을 바꿔도 방문 기록이 쌓이지 않도록
// 주소를 교체(replace)한다 — 뒤로 가기는 이전 화면으로 가야지 이전 기간으로 가면 안 된다.
export function usePeriodParam(
  fallback: number,
  options: readonly number[] = TOOLBAR_PERIODS,
) {
  const [params, setParams] = useSearchParams();
  const custom = parseRange(params.get("from"), params.get("to"));
  const days = custom
    ? rangeLength(custom)
    : parsePeriod(params.get(PERIOD_PARAM), fallback, options);
  const from = custom?.from;
  const to = custom?.to;
  const period = useMemo<Period>(() => {
    const range = from && to ? { from, to } : null;
    return { days, custom: range, key: periodKey(days, range) };
  }, [days, from, to]);
  const setDays = useCallback(
    (next: number) =>
      setParams((current) => withPeriod(current, next, fallback), {
        replace: true,
      }),
    [fallback, setParams],
  );
  const setRange = useCallback(
    (range: DateRange | null) =>
      setParams((current) => withRange(current, range), { replace: true }),
    [setParams],
  );
  return [days, setDays, period, setRange] as const;
}
