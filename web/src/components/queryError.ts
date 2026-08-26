/**
 * queryError turns a failed request into something the reader can act on.
 *
 * The analytical endpoints already answer a timeout with advice — narrow the
 * range, use a segment, run it in Fast mode, have it delivered on a schedule.
 * That advice was printed as plain text next to a generic "요청을 완료하지
 * 못했습니다", which leaves the reader to work out where any of those live. The
 * recovery below names the same steps and carries the destination with it.
 */

export type RecoveryAction =
  | { kind: "retry"; label: string }
  | { kind: "narrow"; label: string }
  | { kind: "link"; label: string; to: string };

export interface QueryRecovery {
  title: string;
  explanation: string;
  actions: RecoveryAction[];
  /** The server's own message, kept for the cases it is the only detail. */
  detail?: string;
}

const explorer = "/explorer";
const automation = "/admin/automation";
const segments = "/segments";
const sites = "/admin?section=sites";

export function describeQueryError(
  error: unknown,
  options: { canNarrowRange?: boolean; canRetry?: boolean } = {},
): QueryRecovery {
  // Read the code off the error by shape rather than importing the client's
  // error class: this module only needs the code, and staying free of that
  // import keeps it usable from anywhere, tests included.
  const code =
    typeof error === "object" && error !== null && typeof (error as { code?: unknown }).code === "string"
      ? (error as { code: string }).code
      : "";
  const message = error instanceof Error ? error.message : "";
  const retry: RecoveryAction[] = options.canRetry
    ? [{ kind: "retry", label: "다시 시도" }]
    : [];
  const narrow: RecoveryAction[] = options.canNarrowRange
    ? [{ kind: "narrow", label: "기간 줄이기" }]
    : [];

  switch (code) {
    case "QUERY_TIMEOUT":
      return {
        title: "조회가 25초 제한을 넘었습니다",
        explanation:
          "기간이 넓거나 대상이 많아 제한 시간 안에 끝나지 않았습니다. 아래 중 하나로 같은 답을 얻을 수 있습니다.",
        actions: [
          ...narrow,
          { kind: "link", label: "Segment로 대상 좁히기", to: segments },
          { kind: "link", label: "쿼리 빌더에서 Fast 모드로 실행", to: explorer },
          { kind: "link", label: "정기 배달로 받기", to: automation },
          ...retry,
        ],
      };
    case "RANGE_EXCEEDS_POLICY":
      return {
        title: "이 기간은 사이트 정책이 허용하지 않습니다",
        explanation:
          "관리자가 정한 최대 정확 조회 기간을 넘었습니다. 더 짧은 기간을 고르거나, 이 범위가 정기적으로 필요하면 정기 배달로 받으세요.",
        actions: [
          ...narrow,
          { kind: "link", label: "정기 배달로 받기", to: automation },
        ],
        detail: message,
      };
    case "QUERY_CANCELED":
      return {
        title: "조회가 중단되었습니다",
        explanation:
          "화면을 벗어나거나 새로고침하면 실행 중인 조회가 취소됩니다. 결과가 필요하면 다시 실행하세요.",
        actions: retry.length ? retry : [{ kind: "retry", label: "다시 시도" }],
      };
    case "QUERY_FAILED":
      return {
        title: "조회를 처리하지 못했습니다",
        explanation:
          "일시적인 문제일 수 있습니다. 반복된다면 아래 메시지를 그대로 담아 관리자에게 알려주세요.",
        actions: retry.length ? retry : [{ kind: "retry", label: "다시 시도" }],
        detail: message,
      };
    case "TOO_MANY_SEGMENTS":
      return {
        title: "비교할 수 있는 Segment는 3개까지입니다",
        explanation:
          "열이 늘어날수록 같은 측정을 그만큼 반복하므로 비교 대상을 3개로 제한합니다. 하나를 빼고 다시 실행하세요.",
        actions: [{ kind: "link", label: "Segment 관리", to: segments }],
      };
    case "INVALID_SEGMENT":
      return {
        title: "Segment 조건을 사용할 수 없습니다",
        explanation:
          "조건이 삭제되었거나 이 사이트에서 평가할 수 없는 필드를 사용하고 있습니다.",
        actions: [{ kind: "link", label: "Segment 확인", to: segments }],
        detail: message,
      };
    case "INVALID_RANGE":
      return {
        title: "조회 기간이 올바르지 않습니다",
        explanation: "시작일과 종료일을 확인하세요.",
        actions: narrow,
        detail: message,
      };
    case "UNKNOWN_SITE":
      return {
        title: "사이트를 찾을 수 없습니다",
        explanation:
          "삭제되었거나 접근 권한이 없는 사이트입니다. 사이트 목록에서 다시 선택하세요.",
        actions: [{ kind: "link", label: "사이트 목록", to: sites }],
      };
    case "FORBIDDEN":
      return {
        title: "이 데이터를 볼 권한이 없습니다",
        explanation:
          "관리자에게 해당 사이트의 조회 권한을 요청하세요. 개인정보 설정에 따라 차단된 화면일 수도 있습니다.",
        actions: [],
      };
    default:
      return {
        title: "요청을 완료하지 못했습니다",
        explanation: message || "데이터를 불러오지 못했습니다.",
        actions: retry,
      };
  }
}

/**
 * slowQueryNotice sets expectations before the deadline rather than after it.
 * A reader watching a skeleton for fifteen seconds has no way to know whether
 * the screen is working or already lost.
 */
export function slowQueryNotice(elapsedMs: number): string | null {
  if (elapsedMs < 8000) return null;
  if (elapsedMs < 20000)
    return "조회가 평소보다 오래 걸리고 있습니다. 기간이 넓거나 대상이 많으면 시간이 늘어납니다.";
  return "25초 제한에 가까워지고 있습니다. 완료되지 않으면 기간을 줄이거나 Segment로 대상을 좁혀 다시 실행하세요.";
}

/**
 * narrowerRange returns the next shorter range a screen offers, or null when the
 * reader is already on the shortest one. Offering "기간 줄이기" at seven days
 * would be a button that changes nothing.
 */
export function narrowerRange(days: number, options: number[] = [7, 30, 90]): number | null {
  const shorter = options.filter((option) => option < days).sort((a, b) => b - a);
  return shorter.length ? shorter[0] : null;
}

/**
 * allowedRanges drops the periods a site's policy will refuse. Offering a period
 * the server rejects turns a limit into a broken screen, so the choice reflects
 * the limit instead of colliding with it.
 */
export function allowedRanges(options: number[], maxExactDays?: number): number[] {
  if (!maxExactDays || maxExactDays <= 0) return options;
  const allowed = options.filter((option) => option <= maxExactDays);
  // Never leave the control empty: if even the shortest period is over the
  // limit, keep it so the reader sees the refusal and its explanation rather
  // than a control with nothing in it.
  return allowed.length ? allowed : [options[0]];
}
