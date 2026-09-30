// 표 하나하나의 보기 설정과 표시 규칙. 표는 62곳에서 같은 컴포넌트를 쓰므로, 여기서
// 정한 것이 모든 표의 동작이 된다. 렌더 없이 규칙을 시험할 수 있도록 떨어뜨려 둔다.

export type Density = "small" | "medium";

export const DENSITY_STORAGE_KEY = "momento:table-density";

// 숨긴 열은 표마다 기억한다. 표에는 id 가 없으므로 내보내기 파일 이름, 제목, 열 키
// 순으로 가장 안정적인 것을 쓴다 — 같은 화면의 같은 표는 다음에도 같은 이름을 얻는다.
export function tableStorageKey(
  title: string | undefined,
  exportFilename: string | undefined,
  columnKeys: readonly string[],
): string {
  const id = exportFilename || title || columnKeys.join(",");
  return `momento:table-columns:${id}`;
}

export function parseHidden(raw: string | null, columnKeys: readonly string[]): string[] {
  if (!raw) return [];
  try {
    const value: unknown = JSON.parse(raw);
    if (!Array.isArray(value)) return [];
    const known = new Set(columnKeys);
    return value.filter((key): key is string => typeof key === "string" && known.has(key));
  } catch {
    return [];
  }
}

// 열을 숨기고 켠다. 마지막 남은 열은 숨길 수 없다 — 열이 하나도 없는 표는 되돌릴
// 곳이 없는 빈 상자다.
export function toggleHidden(
  hidden: readonly string[],
  key: string,
  columnKeys: readonly string[],
): string[] {
  if (hidden.includes(key)) return hidden.filter((item) => item !== key);
  const visible = columnKeys.filter((item) => !hidden.includes(item));
  if (visible.length <= 1) return [...hidden];
  return [...hidden, key];
}

// 값이 있는 칸이 모두 숫자인 열은 숫자 열이다. 숫자는 자릿수를 맞춰 읽어야 하므로
// 오른쪽에 붙인다 — 지정하지 않은 열이 왼쪽 정렬이라 1,240 과 9 가 머리를 맞췄다.
export function isNumericColumn(values: Iterable<unknown>): boolean {
  let seen = false;
  for (const value of values) {
    if (value == null || value === "") continue;
    if (typeof value !== "number") return false;
    seen = true;
  }
  return seen;
}

// 내보내기 이름이 없는 표도 CSV 를 내보낼 수 있게 제목에서 파일 이름을 만든다.
export function exportName(
  exportFilename: string | undefined,
  title: string | undefined,
): string {
  if (exportFilename) return exportFilename;
  const slug = (title || "")
    .trim()
    .replace(/[\\/:*?"<>|]+/g, "")
    .replace(/\s+/g, "-")
    .slice(0, 60);
  return `momento-${slug || "table"}`;
}

// 검색어와 일치한 부분을 표시하기 위한 조각. 대소문자는 가리지 않고 글자 그대로 찾는다.
export function highlightParts(
  text: string,
  needle: string,
): { text: string; match: boolean }[] {
  const query = needle.trim().toLocaleLowerCase("ko-KR");
  if (!query || !text) return [{ text, match: false }];
  const lower = text.toLocaleLowerCase("ko-KR");
  // 소문자로 바꾸면 길이가 달라지는 글자(예: İ)가 있으면 위치가 어긋나므로 표시하지 않는다.
  if (lower.length !== text.length) return [{ text, match: false }];
  const parts: { text: string; match: boolean }[] = [];
  let from = 0;
  for (let at = lower.indexOf(query); at !== -1; at = lower.indexOf(query, from)) {
    if (at > from) parts.push({ text: text.slice(from, at), match: false });
    parts.push({ text: text.slice(at, at + query.length), match: true });
    from = at + query.length;
  }
  if (from < text.length) parts.push({ text: text.slice(from), match: false });
  return parts;
}
