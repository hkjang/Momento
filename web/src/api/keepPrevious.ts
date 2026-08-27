/**
 * Keeps the answer already on screen while the next one loads.
 *
 * Changing the period or the model gives the query a new key, and React Query
 * answers a new key with no data at all: every screen unmounted its content,
 * showed skeletons, and mounted it again — the page flickered on every change,
 * and on a screen that refetches it flickered on its own. Holding the previous
 * answer until the new one arrives removes that entirely; the reader sees the
 * numbers they had, dimmed, with the toolbar saying it is loading.
 *
 * Only within the same scope, though. The one thing worse than a blank screen is
 * one site's numbers displayed under another site's name, so a change of site or
 * environment still clears. Every analytical query key carries both, which is
 * what this checks — position independently, because the keys do not agree on an
 * order.
 */
export function keepWithinScope<TData>(
  ...scope: (string | number | undefined)[]
) {
  return (
    previous: TData | undefined,
    previousQuery?: { queryKey: readonly unknown[] },
  ) => {
    if (previous === undefined || !previousQuery) return undefined;
    const key = previousQuery.queryKey;
    const sameScope = scope.every(
      (value) => value === undefined || key.includes(value),
    );
    return sameScope ? previous : undefined;
  };
}
