import { useSyncExternalStore } from "react";

// 연결이 끊기면 모든 조회가 한꺼번에 실패하고, 각 화면은 저마다의 오류 문구로 그것을
// 알렸다. 원인이 하나일 때는 한 곳에서 한 번 말한다.
function subscribe(listener: () => void) {
  window.addEventListener("online", listener);
  window.addEventListener("offline", listener);
  return () => {
    window.removeEventListener("online", listener);
    window.removeEventListener("offline", listener);
  };
}

export function useOnline(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => navigator.onLine,
    () => true,
  );
}
