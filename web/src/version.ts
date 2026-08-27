import { useQuery } from "@tanstack/react-query";
import packageInfo from "../package.json";
import { api } from "./api/client";

export interface VersionInfo {
  name: string;
  version: string;
  commit: string;
  build_time: string;
}

export const consoleVersion = packageInfo.version;

export function useRuntimeVersion() {
  return useQuery({
    queryKey: ["runtime-version"],
    queryFn: () =>
      api<VersionInfo>("/api/v1/version", {
        cache: "no-store",
        headers: { "Cache-Control": "no-cache" },
      }),
    staleTime: 0,
    refetchOnMount: "always",
    refetchOnWindowFocus: true,
    refetchInterval: 5 * 60 * 1000,
  });
}

export function shortCommit(commit?: string) {
  if (!commit || commit === "unknown") return "개발 빌드";
  return commit.slice(0, 8);
}

/** Formats the build stamp for display, in the reader's own timezone. */
export function buildStamp(info?: VersionInfo) {
  if (!info || !info.build_time || info.build_time === "unknown") return "";
  const parsed = new Date(info.build_time);
  if (Number.isNaN(parsed.getTime())) return "";
  return parsed.toLocaleString();
}
