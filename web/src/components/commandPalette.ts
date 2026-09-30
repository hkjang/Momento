// 명령 팔레트(Ctrl/Cmd+K)의 규칙. Enter 는 언제나 첫 번째 결과로 갔고 방향키는 아무
// 일도 하지 않았으므로, 두 번째 결과로 가려면 마우스를 잡아야 했다 — 키보드로 열어
// 놓고 키보드로 끝낼 수 없는 팔레트였다. 검색어가 없을 때는 메뉴 정의 순서의 앞 10개를
// 보였는데, 사람이 팔레트를 여는 이유는 대개 방금 보던 화면으로 돌아가기 위해서다.
// 렌더 없이 규칙을 시험할 수 있도록 화면과 떨어뜨려 둔다.

export const RECENT_LIMIT = 5;
export const RECENT_STORAGE_KEY = "momento:recent-routes";

// 목록 끝에서 한 칸 더 가면 반대쪽 끝으로 돈다.
export function moveActive(active: number, delta: number, length: number): number {
  if (length <= 0) return 0;
  return (((active + delta) % length) + length) % length;
}

// 지금 주소에 해당하는 메뉴의 `to`. `/admin?section=users` 처럼 조건이 붙은 메뉴는 그
// 조건이 모두 맞을 때만 고르고, 여럿이 맞으면 조건이 가장 많은(가장 구체적인) 것을
// 고른다. 메뉴가 모르는 조건(`?days=90`)은 무시하므로 기간을 바꿔도 같은 메뉴다.
export function routeFor(
  pathname: string,
  search: string,
  routes: readonly string[],
): string | null {
  const current = new URLSearchParams(search);
  let best: string | null = null;
  let bestSpecificity = -1;
  for (const to of routes) {
    const [path, query = ""] = to.split("?");
    if (path !== pathname) continue;
    const required = [...new URLSearchParams(query)];
    if (!required.every(([key, value]) => current.get(key) === value)) continue;
    if (required.length > bestSpecificity) {
      best = to;
      bestSpecificity = required.length;
    }
  }
  return best;
}

export function rememberRoute(
  recent: readonly string[],
  to: string,
  limit = RECENT_LIMIT,
): string[] {
  return [to, ...recent.filter((item) => item !== to)].slice(0, limit);
}

// 저장소의 값은 다른 버전의 콘솔이 썼을 수도, 사람이 고쳤을 수도 있다.
export function parseRecent(raw: string | null): string[] {
  if (!raw) return [];
  try {
    const value: unknown = JSON.parse(raw);
    return Array.isArray(value)
      ? value.filter((item): item is string => typeof item === "string").slice(0, RECENT_LIMIT)
      : [];
  } catch {
    return [];
  }
}

// 검색어가 없으면 최근 화면을 먼저, 나머지는 정의 순서대로. 사라진 메뉴(권한이 바뀐
// 관리 화면 등)는 최근 목록에 있어도 나오지 않는다.
export function orderWithRecent<T extends { to: string }>(
  routes: readonly T[],
  recent: readonly string[],
  limit: number,
): (T & { recent: boolean })[] {
  const byRoute = new Map(routes.map((route) => [route.to, route]));
  const first = recent
    .map((to) => byRoute.get(to))
    .filter((route): route is T => !!route)
    .map((route) => ({ ...route, recent: true }));
  const seen = new Set(first.map((route) => route.to));
  const rest = routes
    .filter((route) => !seen.has(route.to))
    .map((route) => ({ ...route, recent: false }));
  return [...first, ...rest].slice(0, limit);
}
