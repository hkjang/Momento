import { useEffect } from "react";

// 설정 폼과 Segment 편집기는 저장하지 않은 채 탭을 닫거나 새로고침하면 고친 내용이
// 말없이 사라졌다. 고친 것이 있으면 브라우저가 떠나기 전에 한 번 묻는다. 콘솔 안의
// 메뉴 이동은 막지 않는다 — 라우터(BrowserRouter)가 이동 차단을 지원하지 않고, 메뉴
// 이동은 되돌아오면 되는 조작이기 때문이다.
export function useUnsavedWarning(dirty: boolean) {
  useEffect(() => {
    if (!dirty) return;
    const warn = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      // 오래된 브라우저는 returnValue 가 있어야 묻는다.
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);
}
