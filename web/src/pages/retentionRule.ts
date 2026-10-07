// 「보존 정책」 다섯 칸의 범위 판정과 그 범위를 말하는 문장들. 범위의 정본은
// 서버의 validateRetention(internal/httpapi/advanced_analytics.go:80-97)이고, 이
// 모듈은 그 숫자를 **화면에서 한 번만** 적는 자리다.
//
// 그 전에는 같은 숫자가 세 곳에 따로 적혀 있었다 — 서버의 Go, adminErrors.ts 의
// INVALID_RETENTION 안내 다섯 문장, AdminPage.tsx 의 helperText 문자열. 그래서 두
// 칸은 helperText 가 범위를 **아예 말하지 않았고**(「Session 요약 (개월)」 은
// 요약의 뜻만, 「Aggregation (개월)」 은 무기한의 뜻만 말했다), 사용자는 저장을
// 눌러 400 을 받은 뒤에야 1~120 을 읽었다. 이제 안내와 helperText 가 같은 상수를
// 쓰므로 둘이 갈라질 수 없다.
//
// cidrRule.ts·passwordRule.ts 와 같은 자리의 순수 모듈이고, 같은 이유로 문장을
// 내보낸다 — 사용자가 **틀린 뒤에만** 범위를 읽는 일을 없앤다.

export type RetentionColumn =
  | "raw_event_months"
  | "session_months"
  | "aggregation_months"
  | "realtime_hours"
  | "debug_days";

export type RetentionLimit = {
  /** 서버 거절 문장에서 이 칸을 찾는 열쇠. */
  column: RetentionColumn;
  /**
   * 이 칸의 TextField label 과 **글자 그대로** 같아야 한다 — 안내가 「」 로
   * 감싸 부르는 이름이 화면에 없으면 눈으로 칸을 찾을 수 없다.
   */
  label: string;
  min: number;
  max: number;
  /** 칸 이름이 쓰는 단위. 범위를 한국어로 말할 때 숫자 뒤에 붙는다. */
  unit: string;
  /** 서버가 null 을 통과시키는 칸(`AggregationMonths != nil` 가드). */
  nullable?: true;
  /**
   * 범위가 아닌 것을 말하는 기존 설명. helperText 에서 범위 뒤에 「 · 」 로
   * 이어 붙는다. 범위만 말하는 칸은 비어 있다.
   */
  note?: string;
};

export const RETENTION_LIMITS: RetentionLimit[] = [
  {
    column: "raw_event_months",
    label: "Raw Event (개월)",
    min: 1,
    max: 120,
    unit: "개월",
  },
  {
    column: "session_months",
    label: "Session 요약 (개월)",
    min: 1,
    max: 120,
    unit: "개월",
    note: "Raw Event 삭제 후에도 유지되는 요약",
  },
  {
    column: "aggregation_months",
    label: "Aggregation (개월)",
    min: 1,
    max: 1200,
    unit: "개월",
    nullable: true,
    // Two of these tables hold one row per visitor per day, with the visitor
    // and user id on it. Calling them "집계" reads as anonymous and left an
    // operator believing a blank field kept only totals. 비워 둘 수 있다는 것은
    // 이 문장이 이미 말하므로 helperText 의 범위 쪽은 그것을 되풀이하지 않는다.
    note: "비워 두면 무기한. 일별 집계 중 방문자·세션 테이블은 Visitor ID와 User ID를 행마다 가지므로, 비워 두면 Raw Event가 삭제된 뒤에도 사람 단위 기록이 남습니다",
  },
  {
    column: "realtime_hours",
    label: "Realtime (시간)",
    min: 1,
    max: 168,
    unit: "시간",
    // Kept for API compatibility, and labelled for what it is: Momento keeps no
    // separate realtime store, so there is nothing for this value to trim.
    // Saying so is better than a control that silently does nothing.
    note: "현재 적용되지 않습니다. 별도의 Realtime 저장소가 없어 삭제할 대상이 없습니다",
  },
  {
    column: "debug_days",
    label: "Debugger / Dead Letter (일)",
    min: 1,
    max: 90,
    unit: "일",
  },
];

/**
 * 칸 이름으로 범위를 찾는다. 모르는 이름은 **던진다** — 조용히 빈 문자열을
 * 돌려주면 helperText 가 사라진 것을 아무도 보지 못하고, 그 자리는 배선 실수
 * 말고는 닿지 않는다.
 */
export function retentionLimit(column: string): RetentionLimit {
  const limit = RETENTION_LIMITS.find((entry) => entry.column === column);
  if (!limit) throw new Error(`알 수 없는 보존 정책 칸: ${column}`);
  return limit;
}

/** 두 끝만. `1~120개월` */
function span(limit: RetentionLimit): string {
  return `${limit.min}~${limit.max}${limit.unit}`;
}

/**
 * 범위를 짧게 말하는 문구. 비워 둘 수 있는 칸은 그것을 먼저 말한다 — 이 문구를
 * 쓰는 자리에는 그 사실을 말해 주는 다른 문장이 없다.
 */
export function retentionRangeText(column: string): string {
  const limit = retentionLimit(column);
  return limit.nullable ? `비워 두거나 ${span(limit)}` : span(limit);
}

/**
 * 칸 아래에 늘 보이는 기본 안내 — 범위 먼저, 그 칸만의 설명을 뒤에.
 *
 * 범위 쪽은 `span` 을 쓴다(「비워 두거나」 를 붙이지 않는다). 비워 둘 수 있는
 * 칸은 `note` 가 이미 「비워 두면 무기한」 을 말하고 있어서, 여기서 또 말하면 한
 * 줄 안에서 같은 것을 두 번 말한다.
 */
export function retentionHelperText(column: string): string {
  const limit = retentionLimit(column);
  return [span(limit), limit.note].filter(Boolean).join(" · ");
}

/**
 * 서버가 이 칸을 거절했을 때 Alert 이 쓰는 문장(adminErrors.ts 의
 * INVALID_RETENTION), 그리고 화면이 보내기 전에 막을 때 칸 아래에 쓰는 문장.
 * **둘이 같은 문장**이라는 것이 이 모듈의 요점이다.
 */
export function retentionNotice(column: string): string {
  const limit = retentionLimit(column);
  const blank = limit.nullable ? "비워 두거나 " : "";
  return `「${limit.label}」 은 ${blank}${limit.min}${limit.unit}에서 ${limit.max}${limit.unit} 사이로 적으세요.`;
}

export type RetentionJudgement = {
  /** 있으면 이 문장을 칸 아래에 보인다. */
  blocking?: string;
  /** 값이 분명히 틀렸을 때만 오류 색을 쓴다. */
  error?: true;
};

/**
 * 보내기 전에 한 칸을 본다. 서버가 거절할 값만 막는다 — 화면이 서버보다
 * 좁아지면 서버가 받아 줄 값을 넣을 방법이 없어진다.
 *
 * 정수 판정이 범위 판정보다 **앞**이다. 서버의 다섯 필드는 `int`·`*int` 라서
 * 1.5 는 범위에 닿기도 전에 JSON 디코딩에서 400 으로 떨어지고, 1.5 는 1~120
 * 안이라 범위 판정으로는 잡히지 않는다. `Number("abc")` 의 NaN 도 여기서 걸린다.
 */
export function judgeRetention(
  column: string,
  value: number | null,
): RetentionJudgement {
  const limit = retentionLimit(column);

  if (value === null) {
    // 비워 둘 수 있는 칸만 통과. 나머지 네 칸은 `Number("")` 가 0 을 만들어
    // 그 경로로 오는 일이 더 흔하지만(아래 범위 판정이 잡는다), 빈 값이 그대로
    // 올 수 있는 자리이므로 함께 막는다.
    if (limit.nullable) return {};
    return { blocking: retentionNotice(column), error: true };
  }

  if (!Number.isInteger(value)) {
    return { blocking: `「${limit.label}」 은 정수로 적으세요.`, error: true };
  }

  if (value < limit.min || value > limit.max) {
    return { blocking: retentionNotice(column), error: true };
  }

  return {};
}

/**
 * TextField 가 그대로 받는 모양. 막을 것이 있으면 그 문장이 기본 안내를
 * **대신한다** — 한 칸이 범위를 두 번 말하지 않게.
 *
 * `label` 도 함께 돌려준다. 안내 문장이 「」 로 부르는 이름과 화면에 찍히는
 * 이름이 **같은 상수**여야 눈으로 칸을 찾을 수 있는데, 둘을 따로 적으면 한쪽만
 * 고쳐져 Alert 이 화면에 없는 칸을 가리키게 된다.
 */
export function retentionFieldProps(
  column: string,
  value: number | null,
): { label: string; helperText: string; error: boolean } {
  const judged = judgeRetention(column, value);
  return {
    label: retentionLimit(column).label,
    helperText: judged.blocking ?? retentionHelperText(column),
    error: !!judged.error,
  };
}
