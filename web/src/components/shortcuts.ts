// 전역 키보드 단축키. 명령 팔레트(Ctrl/Cmd+K) 말고는 단축키가 없었으므로, 자주 가는
// 화면으로 가는 데도 마우스로 사이드바 그룹을 펼쳐야 했다. GitHub·Gmail 과 같은 모양을
// 따른다: `g` 다음 글자로 이동, `/` 로 검색, `?` 로 도움말.

export interface GoTarget {
  key: string;
  to: string;
  label: string;
  adminOnly?: boolean;
}

export const GO_TARGETS: readonly GoTarget[] = [
  { key: "o", to: "/", label: "개요" },
  { key: "r", to: "/realtime", label: "실시간" },
  { key: "i", to: "/visitor-insights", label: "방문자 인사이트" },
  { key: "e", to: "/events", label: "이벤트" },
  { key: "p", to: "/pages", label: "페이지" },
  { key: "s", to: "/sessions", label: "세션" },
  { key: "v", to: "/visitors", label: "사용자 활동" },
  { key: "u", to: "/user-explorer", label: "사용자 탐색" },
  { key: "f", to: "/funnel", label: "퍼널" },
  { key: "x", to: "/explorer", label: "탐색" },
  { key: "a", to: "/admin", label: "관리 센터", adminOnly: true },
];

// `g` 를 누른 뒤 다음 글자를 기다리는 시간. 그보다 늦은 글자는 그냥 글자다.
export const SEQUENCE_WINDOW_MS = 1200;

export type ShortcutAction =
  | { kind: "go"; to: string }
  | { kind: "search" }
  | { kind: "help" }
  | { kind: "none" };

export interface ShortcutState {
  pendingSince: number | null;
}

// 한 번의 키 입력이 무엇을 뜻하는지와 다음 상태. 입력 칸에 쓰는 중이거나 수정 키가
// 눌린 입력은 호출하는 쪽이 걸러낸다.
export function readShortcut(
  state: ShortcutState,
  key: string,
  now: number,
  isAdmin: boolean,
): { state: ShortcutState; action: ShortcutAction } {
  const pending =
    state.pendingSince != null && now - state.pendingSince <= SEQUENCE_WINDOW_MS;
  if (pending) {
    const target = GO_TARGETS.find(
      (item) => item.key === key.toLowerCase() && (!item.adminOnly || isAdmin),
    );
    return {
      state: { pendingSince: null },
      action: target ? { kind: "go", to: target.to } : { kind: "none" },
    };
  }
  if (key === "g" || key === "G") return { state: { pendingSince: now }, action: { kind: "none" } };
  if (key === "/") return { state: { pendingSince: null }, action: { kind: "search" } };
  if (key === "?") return { state: { pendingSince: null }, action: { kind: "help" } };
  return { state: { pendingSince: null }, action: { kind: "none" } };
}

// 글자를 쓰는 곳에서는 단축키가 글자를 빼앗으면 안 된다.
export function isTypingTarget(target: {
  tagName?: string;
  isContentEditable?: boolean;
  getAttribute?(name: string): string | null;
} | null): boolean {
  if (!target) return false;
  if (target.isContentEditable) return true;
  const tag = (target.tagName || "").toUpperCase();
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return true;
  const role = target.getAttribute?.("role");
  return role === "textbox" || role === "combobox";
}
