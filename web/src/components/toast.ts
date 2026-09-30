// 저장·삭제가 끝나도 대부분의 화면은 아무 말이 없었다. 버튼이 잠깐 비활성이 되었다
// 돌아올 뿐이라, 눌렀는지·저장됐는지를 목록이 바뀐 것으로 짐작해야 했다. 조작마다
// 상태를 따로 두지 않도록, 조작이 meta.successMessage 를 선언하면 전역 알림 하나가
// 보여 준다(main.tsx 의 MutationCache). 여기는 그 알림의 작은 저장소다.

export interface Toast {
  id: number;
  message: string;
}

type Listener = (toast: Toast) => void;

const listeners = new Set<Listener>();
let nextId = 1;

export function showToast(message: string): void {
  const text = message.trim();
  if (!text) return;
  const toast = { id: nextId++, message: text };
  for (const listener of listeners) listener(toast);
}

export function subscribeToasts(listener: Listener): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

// A mutation's meta is untyped at the call site; only a non-empty string is a
// message.
export function successMessage(meta: unknown): string | null {
  if (!meta || typeof meta !== "object") return null;
  const value = (meta as { successMessage?: unknown }).successMessage;
  return typeof value === "string" && value.trim() ? value.trim() : null;
}
