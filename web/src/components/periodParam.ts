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
  if (days === fallback) next.delete(PERIOD_PARAM);
  else next.set(PERIOD_PARAM, String(days));
  return next;
}
