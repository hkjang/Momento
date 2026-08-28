/**
 * The query policy form holds five numbers. The endpoint that answers it sends
 * a sixth field — `defaults`, a boolean saying whether the site has a stored
 * policy or is running under the built-in one — and the screen used to cast the
 * whole response into the form.
 *
 * TypeScript accepts that cast and the browser does not care: the form renders
 * one control per key it holds, so an administrator saw a sixth number field
 * labelled `defaults` holding a boolean, next to the five real limits. It is the
 * screen that tells somebody what limits are in force, and it was inventing one.
 *
 * Taking the fields by name rather than by whatever arrived also means a new
 * field on the endpoint cannot appear on this form on its own.
 */
export const queryPolicyFields = [
  "max_exact_days",
  "max_complexity_score",
  "background_threshold",
  "fast_sample_percent",
  "preview_sample_percent",
] as const;

export type QueryPolicyForm = Record<
  (typeof queryPolicyFields)[number],
  number
>;

export const defaultQueryPolicyForm: QueryPolicyForm = {
  max_exact_days: 180,
  max_complexity_score: 90,
  background_threshold: 60,
  fast_sample_percent: 10,
  preview_sample_percent: 1,
};

/**
 * readQueryPolicy takes the five limits out of whatever the endpoint answered,
 * keeping the current value for anything missing so a partial response cannot
 * blank a control the administrator is looking at.
 */
export function readQueryPolicy(
  answered: unknown,
  current: QueryPolicyForm = defaultQueryPolicyForm,
): QueryPolicyForm {
  const source = (answered || {}) as Record<string, unknown>;
  const form = { ...current };
  for (const field of queryPolicyFields) {
    const value = source[field];
    if (typeof value === "number" && Number.isFinite(value))
      form[field] = value;
  }
  return form;
}

/** The Korean label for each limit, so the form never shows a raw key. */
export const queryPolicyLabel: Record<string, string> = {
  max_exact_days: "Exact 최대 기간(일)",
  max_complexity_score: "최대 복잡도 점수",
  background_threshold: "백그라운드 기준",
  fast_sample_percent: "Fast 표본(%)",
  preview_sample_percent: "Preview 표본(%)",
};
