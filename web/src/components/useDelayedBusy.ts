import { useEffect, useState } from "react";

/**
 * Reports that something is loading, but only once it has been loading long
 * enough to be worth saying so.
 *
 * A progress bar that appears and disappears on every background refetch reads
 * as a flickering screen rather than as feedback, and most refetches finish
 * before a reader could act on the news. Below the delay nothing is shown at
 * all; past it the bar stays until the work is actually done.
 */
export function useDelayedBusy(busy: boolean, delayMs = 250) {
  const [shown, setShown] = useState(false);
  useEffect(() => {
    if (!busy) {
      setShown(false);
      return;
    }
    const timer = window.setTimeout(() => setShown(true), delayMs);
    return () => window.clearTimeout(timer);
  }, [busy, delayMs]);
  return shown;
}
