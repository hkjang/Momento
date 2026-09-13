// A re-aggregation job's status column reads the row's `status`, but that word
// alone does not tell the operator why a job is still queued. Maintenance puts a
// job back to `pending` — leaving the reason in `error` — when its worker was
// interrupted or when it lost a lock to the ingest worker and will be retried on
// the next tick. Such a row is "waiting to retry", not "never started", and the
// operator should not read the leftover error as a failure either.

export type AggregateJobChip = {
  label: string;
  color: "success" | "error" | "warning" | "default";
};

export function describeAggregateJobStatus(
  row: Record<string, unknown>,
): AggregateJobChip {
  const status = String(row.status ?? "");
  const error = typeof row.error === "string" ? row.error.trim() : "";
  if (status === "pending" && error)
    return { label: "재시도 대기", color: "warning" };
  if (status === "success") return { label: status, color: "success" };
  if (status === "failed") return { label: status, color: "error" };
  return { label: status, color: "default" };
}
