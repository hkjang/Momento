import { useCallback, useSyncExternalStore } from "react";

// 사람이 고른 보기 설정(표 밀도, 사이드바 접힘, 즐겨찾기…)을 브라우저에 기억한다.
// 같은 설정을 쓰는 곳이 여럿이므로(62개 표가 밀도 하나를 공유한다) 값은 한 곳에 두고
// 모두가 구독한다. 저장소를 막은 브라우저·사생활 보호 창에서는 읽기와 쓰기가 예외를
// 던지므로 모두 삼키고, 그 탭 안에서만 기억한다.

const memory = new Map<string, string | null>();
const listeners = new Map<string, Set<() => void>>();

export function readPreference(key: string): string | null {
  if (memory.has(key)) return memory.get(key) ?? null;
  let value: string | null = null;
  try {
    value = localStorage.getItem(key);
  } catch {
    value = null;
  }
  memory.set(key, value);
  return value;
}

export function writePreference(key: string, value: string | null): void {
  memory.set(key, value);
  try {
    if (value == null) localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  } catch {
    // 이 탭 안에서만 기억한다.
  }
  for (const listener of listeners.get(key) ?? []) listener();
}

function subscribe(key: string, listener: () => void) {
  let set = listeners.get(key);
  if (!set) listeners.set(key, (set = new Set()));
  set.add(listener);
  return () => set.delete(listener);
}

// 원시 문자열 설정. 해석은 호출하는 쪽이 한다(망가진 값은 기본값으로).
export function usePreference(key: string): [string | null, (value: string | null) => void] {
  const value = useSyncExternalStore(
    useCallback((listener) => subscribe(key, listener), [key]),
    () => readPreference(key),
    () => null,
  );
  const set = useCallback((next: string | null) => writePreference(key, next), [key]);
  return [value, set];
}
