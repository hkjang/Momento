import {
  useCallback,
  createContext,
  useContext,
  useEffect,
  useMemo,
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
  const refresh = useCallback(async () => {
    const next = await get<Site[]>("/api/v1/sites");
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
    void refresh();
  }, [refresh]);
  const site = sites.find((s) => s.site_id === selected) || sites[0] || null;
  const siteID = site?.site_id;
  useEffect(() => {
    if (!siteID) {
      setEnvironments([]);
      return;
    }
    void get<SiteEnvironment[]>(
      `/api/v1/sites/${siteID}/environments`,
    ).then((items) => {
      const active = items.filter((item) => item.active);
      setEnvironments(active);
      if (!active.some((item) => item.name === environment)) {
        const next = active.find((item) => item.name === "prd")?.name || active[0]?.name || "prd";
        setEnvironment(next);
        localStorage.setItem("momento:selected-environment", next);
      }
    });
  }, [siteID, environment]);
  const value = useMemo(
    () => ({
      sites,
      site,
      environments,
      environment,
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
    [sites, site, environments, environment, refresh, queryClient],
  );
  return <SiteContext.Provider value={value}>{children}</SiteContext.Provider>;
}
export function useSite() {
  const value = useContext(SiteContext);
  if (!value) throw new Error("SiteProvider missing");
  return value;
}
