import {
  useCallback,
  createContext,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useQueryClient } from "@tanstack/react-query";
import { get, type Site, type SiteEnvironment } from "../api/client";

interface SiteValue {
  sites: Site[];
  site: Site | null;
  environments: SiteEnvironment[];
  environment: string;
  select(id: string): void;
  selectEnvironment(name: string): void;
  refresh(): Promise<void>;
  /**
   * What went wrong reading the site list, when something did.
   *
   * Without it a failed request left `sites` empty, `site` null, and every screen
   * rendering NoSite — "분석할 사이트가 없습니다" — to somebody who has sites and
   * cannot reach the server. Being told to create your first site while your
   * sites are sitting there is worse than an error: it invites the reader to fix
   * the wrong thing.
   */
  loadError: unknown;
}
const SiteContext = createContext<SiteValue | null>(null);
export function SiteProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const [sites, setSites] = useState<Site[]>([]);
  const [selected, setSelected] = useState(
    localStorage.getItem("momento:selected-site") || "",
  );
  const [environment, setEnvironment] = useState(
    localStorage.getItem("momento:selected-environment") || "prd",
  );
  const [environments, setEnvironments] = useState<SiteEnvironment[]>([]);
  const [loadError, setLoadError] = useState<unknown>(null);
  // The selected environment as the environments effect needs to read it, without
  // being a reason for that effect to run again.
  const environmentRef = useRef(environment);
  environmentRef.current = environment;
  const refresh = useCallback(async () => {
    let next: Site[];
    try {
      next = await get<Site[]>("/api/v1/sites");
    } catch (error) {
      // Kept rather than thrown: the caller after creating a site wants to know,
      // and the shell needs it to tell an empty list from an unreachable server.
      setLoadError(error);
      throw error;
    }
    setLoadError(null);
    setSites(next);
    setSelected((current) => {
      const available = next.some((item) => item.site_id === current);
      const selectedSite = available ? current : next[0]?.site_id || "";
      if (selectedSite) {
        localStorage.setItem("momento:selected-site", selectedSite);
      } else {
        localStorage.removeItem("momento:selected-site");
      }
      return selectedSite;
    });
  }, []);
  useEffect(() => {
    // The rejection is recorded inside refresh; this call is the one nobody is
    // waiting on, so it must not become an unhandled rejection.
    refresh().catch(() => {});
  }, [refresh]);
  const site = sites.find((s) => s.site_id === selected) || sites[0] || null;
  const siteID = site?.site_id;
  // The environment list belongs to the site, and only to the site.
  //
  // This used to depend on the selected environment as well, and set it, so
  // every switch between prd and stg fetched the list again to learn nothing.
  // The current value is read through a ref instead, which is what it is for: an
  // input to the effect that must not restart it.
  //
  // It also had no cancellation. Switching sites quickly leaves two reads in
  // flight and the slower one wins, so the selector could end up offering the
  // previous site's environments under the new site's name — the same shape as
  // the query keys that did not carry the environment, arriving one layer down.
  useEffect(() => {
    if (!siteID) {
      setEnvironments([]);
      return;
    }
    let current = true;
    get<SiteEnvironment[]>(`/api/v1/sites/${siteID}/environments`)
      .then((items) => {
        if (!current) return;
        const active = items.filter((item) => item.active);
        setEnvironments(active);
        if (!active.some((item) => item.name === environmentRef.current)) {
          const next =
            active.find((item) => item.name === "prd")?.name ||
            active[0]?.name ||
            "prd";
          setEnvironment(next);
          localStorage.setItem("momento:selected-environment", next);
        }
      })
      // A site always has at least one environment, so failing to read them is a
      // failure rather than an empty answer. The selector keeps what it had and
      // the shell reports it, instead of silently offering nothing to choose.
      .catch((error) => {
        if (current) setLoadError(error);
      });
    return () => {
      current = false;
    };
  }, [siteID]);
  const value = useMemo(
    () => ({
      sites,
      site,
      environments,
      environment,
      loadError,
      select: (id: string) => {
        setSelected(id);
        localStorage.setItem("momento:selected-site", id);
      },
      selectEnvironment: (name: string) => {
        setEnvironment(name);
        localStorage.setItem("momento:selected-environment", name);
        void queryClient.invalidateQueries();
      },
      refresh,
    }),
    [sites, site, environments, environment, loadError, refresh, queryClient],
  );
  return <SiteContext.Provider value={value}>{children}</SiteContext.Provider>;
}
export function useSite() {
  const value = useContext(SiteContext);
  if (!value) throw new Error("SiteProvider missing");
  return value;
}
