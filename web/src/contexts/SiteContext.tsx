import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { get, type Site } from "../api/client";

interface SiteValue {
  sites: Site[];
  site: Site | null;
  select(id: string): void;
  refresh(): Promise<void>;
}
const SiteContext = createContext<SiteValue | null>(null);
export function SiteProvider({ children }: { children: ReactNode }) {
  const [sites, setSites] = useState<Site[]>([]);
  const [selected, setSelected] = useState(
    localStorage.getItem("momento:selected-site") || "",
  );
  const refresh = async () => {
    const next = await get<Site[]>("/api/v1/sites");
    setSites(next);
    if (!selected && next[0]) setSelected(next[0].site_id);
  };
  useEffect(() => {
    void refresh();
  }, []);
  const site = sites.find((s) => s.site_id === selected) || sites[0] || null;
  const value = useMemo(
    () => ({
      sites,
      site,
      select: (id: string) => {
        setSelected(id);
        localStorage.setItem("momento:selected-site", id);
      },
      refresh,
    }),
    [sites, site],
  );
  return <SiteContext.Provider value={value}>{children}</SiteContext.Provider>;
}
export function useSite() {
  const value = useContext(SiteContext);
  if (!value) throw new Error("SiteProvider missing");
  return value;
}
