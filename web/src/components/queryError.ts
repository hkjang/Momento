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
const privacy = "/admin?section=privacy";

export function describeQueryError(
  error: unknown,
  options: { canNarrowRange?: boolean; canRetry?: boolean } = {},
): QueryRecovery {
  // Read the code off the error by shape rather than importing the client's
  // error class: this module only needs the code, and staying free of that
  // import keeps it usable from anywhere, tests included.
  const code =
    typeof error === "object" &&
    error !== null &&
    typeof (error as { code?: unknown }).code === "string"
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
          {
            kind: "link",
            label: "쿼리 빌더에서 Fast 모드로 실행",
            to: explorer,
          },
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
    case "VISITOR_PROFILES_DISABLED":
      return {
        // Not a failure: an administrator turned this screen off. The server
        // says so in English, which is not the language the rest of the console
        // is in, and the reader was being offered a retry button for a setting.
        title: "방문자 프로필이 꺼져 있습니다",
        explanation:
          "개인정보 정책에서 방문자 프로필 조회를 끄면 이 화면과 신원 화면은 답하지 않습니다. 사람 단위 조회가 필요하면 관리자에게 요청하세요.",
        actions: [{ kind: "link", label: "개인정보 정책", to: privacy }],
      };
    case "INVALID_TIMEZONE":
      return {
        // Every report on the site fails until this is corrected, so the reader
        // needs to be sent to the setting rather than told to try again.
        title: "사이트의 시간대 설정이 올바르지 않습니다",
        explanation:
          "모든 리포트는 사이트의 달력으로 기간을 계산하므로, 시간대가 유효하지 않으면 이 사이트의 어떤 화면도 답할 수 없습니다. 사이트 설정에서 시간대를 고쳐야 합니다.",
        actions: [{ kind: "link", label: "사이트 설정", to: sites }],
        detail: message,
      };
    case "RESPONSE_NOT_ENCODABLE":
      return {
        // The answer was computed and could not be written down — a defect in
        // the report, not a passing condition. Retrying produces it again, so
        // it is not offered first.
        title: "이 리포트가 표현할 수 없는 값을 만들었습니다",
        explanation:
          "서버가 답을 계산했지만 그중 일부가 숫자로 적을 수 없는 값이라 응답을 만들지 못했습니다. 다시 시도해도 같은 결과가 나오므로, 어떤 화면과 기간이었는지와 함께 관리자에게 알려주세요.",
        actions: [],
        detail: message,
      };
    case "DATABASE_SHARED_MEMORY":
      return {
        // Not a wide query. Offering "기간 줄이기" here sends the reader to fix
        // something that is not broken; the fix is a container setting and the
        // person who can make it is the administrator.
        title: "데이터베이스 설정 때문에 조회가 실패했습니다",
        explanation:
          "PostgreSQL이 병렬 조회에 필요한 공유 메모리를 확보하지 못했습니다. Docker로 운영 중이라면 컨테이너의 /dev/shm이 기본값 64MB일 가능성이 큽니다. 데이터가 늘면서 조회 계획이 병렬로 바뀌는 시점에 나타나므로, 어제까지 되던 화면이 오늘 실패할 수 있습니다. 관리자에게 아래 메시지를 그대로 전달하세요.",
        actions: [{ kind: "link", label: "사이트 설정", to: sites }],
        detail: message,
      };
    case "RESPONSE_NOT_JSON":
      return {
        // The request reached something, and that something was not the service.
        // Retrying is worth offering because a sign-in redirect in front of the
        // console clears once the reader signs in again elsewhere.
        title: "서버가 분석 데이터가 아닌 응답을 보냈습니다",
        explanation:
          "요청이 Momento가 아닌 무언가에 닿았습니다. 프록시나 게이트웨이의 오류 페이지, 또는 앞단의 로그인 리디렉션일 수 있습니다. 데이터가 없는 것이 아니라 답이 도착하지 않은 것입니다.",
        actions: retry.length ? retry : [{ kind: "retry", label: "다시 시도" }],
        detail: message,
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
export function narrowerRange(
  days: number,
  options: number[] = [7, 30, 90],
): number | null {
  const shorter = options
    .filter((option) => option < days)
    .sort((a, b) => b - a);
  return shorter.length ? shorter[0] : null;
}

/**
 * allowedRanges drops the periods a site's policy will refuse. Offering a period
 * the server rejects turns a limit into a broken screen, so the choice reflects
 * the limit instead of colliding with it.
 */
export function allowedRanges(
  options: number[],
  maxExactDays?: number,
): number[] {
  if (!maxExactDays || maxExactDays <= 0) return options;
  const allowed = options.filter((option) => option <= maxExactDays);
  // Never leave the control empty: if even the shortest period is over the
  // limit, keep it so the reader sees the refusal and its explanation rather
  // than a control with nothing in it.
  return allowed.length ? allowed : [options[0]];
}
