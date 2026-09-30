// 사이드바의 보기 설정. 그룹을 펼쳐 둔 모양은 새로고침할 때마다 첫 그룹만 펼친 모양으로
// 돌아갔고, 자주 쓰는 화면은 매번 그 그룹을 다시 펼쳐 찾아야 했다.

export const NAV_EXPANDED_KEY = "momento:nav-expanded";
export const NAV_FAVORITES_KEY = "momento:nav-favorites";
export const NAV_COLLAPSED_KEY = "momento:nav-collapsed";
export const FAVORITE_LIMIT = 8;

export function parseExpanded(raw: string | null): Record<string, boolean> {
  if (!raw) return {};
  try {
    const value: unknown = JSON.parse(raw);
    if (!value || typeof value !== "object" || Array.isArray(value)) return {};
    return Object.fromEntries(
      Object.entries(value).filter(([, open]) => typeof open === "boolean"),
    ) as Record<string, boolean>;
  } catch {
    return {};
  }
}

export function parseFavorites(raw: string | null, known: readonly string[]): string[] {
  if (!raw) return [];
  try {
    const value: unknown = JSON.parse(raw);
    if (!Array.isArray(value)) return [];
    const allowed = new Set(known);
    return value
      .filter((item): item is string => typeof item === "string" && allowed.has(item))
      .slice(0, FAVORITE_LIMIT);
  } catch {
    return [];
  }
}

// 이미 있으면 빼고, 없으면 끝에 붙인다. 가득 차면 가장 오래된 것을 밀어낸다.
export function toggleFavorite(favorites: readonly string[], to: string): string[] {
  if (favorites.includes(to)) return favorites.filter((item) => item !== to);
  return [...favorites, to].slice(-FAVORITE_LIMIT);
}
