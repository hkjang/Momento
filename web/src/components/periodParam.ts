// 분석 기간은 화면마다 컴포넌트 상태였으므로 다른 메뉴에 다녀오면 기본값으로 돌아갔고,
// 「최근 90일 이벤트」를 보던 링크를 동료에게 보내도 받는 사람은 30일을 봤다. 기간을
// 주소의 `?days=` 에 두면 새로고침·뒤로 가기·공유가 같은 화면을 연다. 주소는 누구나
// 고쳐 쓸 수 있으므로 그 화면이 실제로 내주는 선택지만 받아들이고, 나머지는 기본값이다.

export const PERIOD_PARAM = "days";

export function parsePeriod(
  raw: string | null | undefined,
  fallback: number,
  options: readonly number[],
): number {
  if (raw == null || !/^\d{1,4}$/.test(raw.trim())) return fallback;
  const value = Number(raw.trim());
  return options.includes(value) ? value : fallback;
}

// 기본값과 같으면 주소에서 뺀다 — 아무것도 고르지 않은 화면의 주소가 깨끗하게 남는다.
export function withPeriod(
  params: URLSearchParams,
  days: number,
  fallback: number,
): URLSearchParams {
  const next = new URLSearchParams(params);
  next.delete("from");
  next.delete("to");
  if (days === fallback) next.delete(PERIOD_PARAM);
  else next.set(PERIOD_PARAM, String(days));
  return next;
}

// 직접 고른 기간. 「지난달」「캠페인이 돈 2주」처럼 오늘로 끝나지 않는 기간은 최근 N일
// 선택지로 물을 방법이 없었다. 날짜는 사이트 시간대의 달력 날짜이고 양 끝을 포함한다
// — 최근 N일(dateRangeValues)이 쓰는 것과 같은 뜻이다.
export interface DateRange {
  from: string;
  to: string;
}

// 가장 긴 직접 기간. 서버의 조회 정책이 실제 경계이고, 이것은 주소에 적힌 터무니없는
// 값(예: 0001-01-01)이 요청까지 가지 않게 하는 상한일 뿐이다.
export const MAX_RANGE_DAYS = 3660;

const DATE_PATTERN = /^(\d{4})-(\d{2})-(\d{2})$/;

function dayNumber(value: string): number | null {
  const match = DATE_PATTERN.exec(value);
  if (!match) return null;
  const [year, month, day] = match.slice(1).map(Number);
  const time = Date.UTC(year, month - 1, day);
  const date = new Date(time);
  // 2026-02-31 처럼 달력에 없는 날은 Date 가 다음 달로 넘겨 버리므로 되짚어 본다.
  if (date.getUTCFullYear() !== year || date.getUTCMonth() !== month - 1 || date.getUTCDate() !== day)
    return null;
  return time / 86_400_000;
}

export function rangeLength(range: DateRange): number {
  return (dayNumber(range.to) ?? 0) - (dayNumber(range.from) ?? 0) + 1;
}

export function parseRange(
  from: string | null | undefined,
  to: string | null | undefined,
): DateRange | null {
  if (!from || !to) return null;
  const start = dayNumber(from.trim());
  const end = dayNumber(to.trim());
  if (start == null || end == null || start > end) return null;
  if (end - start + 1 > MAX_RANGE_DAYS) return null;
  return { from: from.trim(), to: to.trim() };
}

// 화면에서 고른 값이 요청할 만한지. 오늘(사이트 시간대)보다 뒤인 날과 조회 정책보다 긴
// 기간은 서버가 거절하거나 빈 답을 주므로, 적용하기 전에 이유를 말한다.
export function rangeProblem(
  range: { from: string; to: string },
  today: string,
  maxDays?: number,
): string | null {
  const start = dayNumber(range.from);
  const end = dayNumber(range.to);
  if (start == null || end == null) return "시작일과 종료일을 모두 고르세요.";
  if (start > end) return "시작일이 종료일보다 늦습니다.";
  const todayNumber = dayNumber(today);
  if (todayNumber != null && end > todayNumber) return "종료일은 오늘 이후일 수 없습니다.";
  const length = end - start + 1;
  if (maxDays && maxDays > 0 && length > maxDays)
    return `이 사이트의 조회 정책은 최대 ${maxDays}일입니다 (선택한 기간 ${length}일).`;
  if (length > MAX_RANGE_DAYS) return `기간은 최대 ${MAX_RANGE_DAYS}일입니다.`;
  return null;
}

// 최근 N일과 직접 기간은 함께 있을 수 없다 — 하나를 고르면 다른 하나는 주소에서 빠진다.
export function withRange(
  params: URLSearchParams,
  range: DateRange | null,
): URLSearchParams {
  const next = new URLSearchParams(params);
  next.delete(PERIOD_PARAM);
  if (range) {
    next.set("from", range.from);
    next.set("to", range.to);
  } else {
    next.delete("from");
    next.delete("to");
  }
  return next;
}

// 조회 캐시 키. 최근 30일과 30일짜리 직접 기간은 다른 답이다.
export function periodKey(days: number, range: DateRange | null): string {
  return range ? `${range.from}..${range.to}` : String(days);
}
