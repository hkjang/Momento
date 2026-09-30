import {
  createContext,
  useCallback,
  useContext,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
} from "@mui/material";

// 키 회전·삭제처럼 되돌릴 수 없는 조작은 한 번의 클릭으로 끝나면 안 된다. 기존 키를 쓰던
// SDK·서버 연동이 곧바로 끊기고, 지운 세그먼트·채널·스케줄은 다시 만들어야 한다.
// 화면마다 다이얼로그 상태를 따로 들고 다니지 않도록 앱 루트에 하나를 두고
// `await confirm({...})` 로 묻는다.

export interface ConfirmOptions {
  title: string;
  description?: ReactNode;
  confirmLabel?: string;
  // 기본은 위험한 조작(빨간 버튼)이다 — 이 다이얼로그를 부르는 이유가 그것이다.
  destructive?: boolean;
}

type Confirm = (options: ConfirmOptions) => Promise<boolean>;

const ConfirmContext = createContext<Confirm | null>(null);

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [options, setOptions] = useState<ConfirmOptions | null>(null);
  const resolver = useRef<((value: boolean) => void) | null>(null);

  const confirm = useCallback<Confirm>((next) => {
    // 이전 질문이 답을 받지 못한 채 남아 있으면 취소로 끝낸다 — 약속이 영원히 걸려
    // 있으면 그 뒤의 코드가 조용히 사라진다.
    resolver.current?.(false);
    setOptions(next);
    return new Promise<boolean>((resolve) => {
      resolver.current = resolve;
    });
  }, []);

  const close = (value: boolean) => {
    resolver.current?.(value);
    resolver.current = null;
    setOptions(null);
  };

  const destructive = options?.destructive ?? true;
  return (
    <ConfirmContext.Provider value={confirm}>
      {children}
      <Dialog
        open={!!options}
        onClose={() => close(false)}
        maxWidth="xs"
        fullWidth
        aria-labelledby="confirm-dialog-title"
      >
        <DialogTitle id="confirm-dialog-title">{options?.title}</DialogTitle>
        {options?.description && (
          <DialogContent>
            <DialogContentText component="div">
              {options.description}
            </DialogContentText>
          </DialogContent>
        )}
        <DialogActions>
          <Button onClick={() => close(false)}>취소</Button>
          <Button
            variant="contained"
            color={destructive ? "error" : "primary"}
            onClick={() => close(true)}
            autoFocus={!destructive}
          >
            {options?.confirmLabel || "확인"}
          </Button>
        </DialogActions>
      </Dialog>
    </ConfirmContext.Provider>
  );
}

export function useConfirm(): Confirm {
  const confirm = useContext(ConfirmContext);
  if (!confirm) throw new Error("useConfirm must be used inside ConfirmProvider");
  return confirm;
}
