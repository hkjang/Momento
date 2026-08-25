// A delivery channel usually needs one or two credentials, and asking for them as
// a JSON document turns a two field task into a syntax exercise. The editor works
// in name and value rows and validates the same rules the collector enforces.

export interface HeaderRow {
  name: string;
  value: string;
}

export const emptyHeaderRow: HeaderRow = { name: "", value: "" };

/** parseHeaderRows accepts an existing definition so a saved channel can be edited. */
export function parseHeaderRows(json: string): HeaderRow[] {
  if (!json.trim()) return [{ ...emptyHeaderRow }];
  try {
    const parsed = JSON.parse(json) as Record<string, unknown>;
    const rows = Object.entries(parsed).map(([name, value]) => ({
      name,
      value: typeof value === "string" ? value : String(value ?? ""),
    }));
    return rows.length ? rows : [{ ...emptyHeaderRow }];
  } catch {
    return [{ ...emptyHeaderRow }];
  }
}

/** buildHeaders drops blank rows and trims, so a stray empty row is not an error. */
export function buildHeaders(rows: HeaderRow[]): Record<string, string> {
  const headers: Record<string, string> = {};
  for (const row of rows) {
    const name = row.name.trim();
    if (!name) continue;
    headers[name] = row.value.trim();
  }
  return headers;
}

/**
 * headerIssues reports what the server would reject, before the request is sent.
 * The rules mirror the collector: no Host override, no line breaks, and a header
 * with a name needs a value to be worth sending.
 */
export function headerIssues(rows: HeaderRow[]): string[] {
  const issues: string[] = [];
  const seen = new Set<string>();
  for (const row of rows) {
    const name = row.name.trim();
    if (!name) {
      if (row.value.trim()) issues.push("값만 입력된 Header가 있습니다. 이름을 채우거나 행을 지우십시오.");
      continue;
    }
    const key = name.toLowerCase();
    if (key === "host") {
      issues.push("Host Header는 재정의할 수 없습니다.");
    }
    if (seen.has(key)) {
      issues.push(`Header 이름이 중복되었습니다: ${name}`);
    }
    seen.add(key);
    if (/[\r\n]/.test(name) || /[\r\n]/.test(row.value)) {
      issues.push(`${name}에 줄바꿈이 포함되어 있습니다.`);
    }
    if (!row.value.trim()) {
      issues.push(`${name}의 값이 비어 있습니다.`);
    }
  }
  return [...new Set(issues)];
}

/** commonHeaderNames are the names an internal integration usually needs. */
export const commonHeaderNames = [
  "Authorization",
  "X-Api-Key",
  "X-Auth-Token",
  "Content-Type",
] as const;
