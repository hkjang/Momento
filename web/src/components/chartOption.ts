// A chart's option arrives as a fresh object literal on every render of the
// screen that draws it, so its identity says nothing about whether it changed.
// Chart used to dispose the instance and build a new one whenever the identity
// changed — which on a screen that re-renders while it loads (the toolbar's
// busy state, a refetch) meant every chart was torn down and redrawn with its
// entry animation, over and over, with nothing new in it. This is the stable
// form of an option: equal options give equal strings. Formatter functions are
// included by their source, so swapping one for a different function redraws.
export function optionSignature(option: unknown): string {
  const seen = new WeakSet<object>();
  return JSON.stringify(option, (_key, value: unknown) => {
    if (typeof value === "function") return `fn:${value.toString()}`;
    if (typeof value === "number" && !Number.isFinite(value)) return `num:${value}`;
    if (value && typeof value === "object") {
      if (seen.has(value)) return "[circular]";
      seen.add(value);
    }
    return value;
  }) ?? "";
}
