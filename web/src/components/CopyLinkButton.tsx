import { IconButton, Tooltip } from "@mui/material";
import LinkRounded from "@mui/icons-material/LinkRounded";
import { showToast } from "./toast";

// 지금 보는 화면(기간·환경·필터가 주소에 있다)을 동료에게 그대로 보낸다. 주소 표시줄을
// 선택해 복사할 수도 있지만, 사내 브라우저 정책으로 주소 표시줄이 숨은 환경이 있다.
export default function CopyLinkButton() {
  const copy = async () => {
    const href = window.location.href;
    try {
      await navigator.clipboard.writeText(href);
      showToast("이 화면의 링크를 복사했습니다.");
    } catch {
      // 클립보드 권한이 없거나 http 로 열린 경우: 직접 복사할 수 있게 보여 준다.
      window.prompt("이 링크를 복사하세요", href);
    }
  };
  return (
    <Tooltip title="이 화면 링크 복사 (기간 포함)">
      <IconButton size="small" aria-label="이 화면 링크 복사" onClick={() => void copy()}>
        <LinkRounded fontSize="small" />
      </IconButton>
    </Tooltip>
  );
}
