// 표의 시각은 「2026. 9. 30. 오후 3:12:08」 처럼 전부 절대 시각이라, 「방금인가 지난주인가」
// 를 알려면 오늘 날짜와 맞춰 셈해야 했다. 최근 것은 상대 시각으로 말하고, 정확한 시각은
// 마우스를 올리면 보인다. 일주일이 넘은 것은 날짜가 더 읽기 쉽다.

export function relativeTime(value: unknown, now: number): string | null {
  if (value == null || value === "") return null;
  const time = new Date(String(value)).getTime();
  if (!Number.isFinite(time)) return null;
  const seconds = Math.round((now - time) / 1000);
  if (seconds < 0) {
    // 서버와 브라우저의 시계가 조금 어긋나면 미래로 보인다. 1분 안쪽은 「방금」이다.
    return seconds > -60 ? "방금" : null;
  }
  if (seconds < 60) return "방금";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}분 전`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}시간 전`;
  const days = Math.floor(hours / 24);
  if (days < 7) return `${days}일 전`;
  return null;
}

export function absoluteTime(value: unknown): string {
  if (value == null || value === "") return "—";
  const date = new Date(String(value));
  return Number.isFinite(date.getTime()) ? date.toLocaleString("ko-KR") : String(value);
}
