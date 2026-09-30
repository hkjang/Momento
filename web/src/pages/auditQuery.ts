// 감사 로그는 최신 500건만 왔으므로, 그보다 오래된 「누가 이 키를 회전했나」는 콘솔에서
// 답할 수 없었다. 서버가 작업·작업자·대상 필터와 before_id 페이징을 받으므로, 화면은
// 여기서 요청 주소와 다음 페이지 위치를 정한다.

export const AUDIT_PAGE_SIZE = 200;

export interface AuditFilters {
  action: string;
  actor: string;
  resource: string;
}

export const emptyAuditFilters: AuditFilters = { action: "", actor: "", resource: "" };

export function auditPath(
  filters: AuditFilters,
  beforeId?: number,
  limit = AUDIT_PAGE_SIZE,
): string {
  const params = new URLSearchParams();
  for (const key of ["action", "actor", "resource"] as const) {
    const value = filters[key].trim();
    if (value) params.set(key, value);
  }
  params.set("limit", String(limit));
  if (beforeId) params.set("before_id", String(beforeId));
  return `/api/v1/audit?${params}`;
}

// 꽉 찬 페이지 뒤에만 다음 페이지가 있을 수 있다. 다음 페이지는 이 페이지에서 가장
// 오래된(가장 작은) id 보다 앞의 항목이다.
export function nextAuditCursor(
  page: readonly { id?: unknown }[],
  limit = AUDIT_PAGE_SIZE,
): number | undefined {
  if (page.length < limit) return undefined;
  const ids = page
    .map((entry) => Number(entry.id))
    .filter((id) => Number.isFinite(id) && id > 0);
  return ids.length ? Math.min(...ids) : undefined;
}

// 상세는 서버가 남긴 JSON 이다. 비어 있으면 표에 "—" 가 나오도록 빈 문자열을 돌려준다.
export function auditDetailText(detail: unknown): string {
  if (detail == null) return "";
  if (typeof detail === "object" && !Array.isArray(detail) && !Object.keys(detail).length)
    return "";
  try {
    return typeof detail === "string" ? detail : JSON.stringify(detail);
  } catch {
    return String(detail);
  }
}
