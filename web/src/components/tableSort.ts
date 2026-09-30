// 표의 열 정렬. 서버가 돌려준 순서는 대개 이미 의미 있는 순위(사용자 수, 이벤트 수
// 내림차순)이므로 정렬은 세 단계로 돈다 — 첫 방향 → 반대 방향 → 서버 순서. 숫자 열은
// 큰 값을 먼저 보고 싶어 하는 경우가 대부분이라 내림차순에서 시작하고, 글자 열은
// 가나다순에서 시작한다. 렌더 없이 규칙을 시험할 수 있도록 표와 떨어뜨려 둔다.

export type SortDirection = "asc" | "desc";

export interface SortState {
  key: string;
  direction: SortDirection;
}

const collator = new Intl.Collator("ko-KR", { numeric: true, sensitivity: "base" });

// 빈 값은 방향과 무관하게 언제나 뒤로 간다 — 내림차순으로 바꿨다고 "—" 행이 맨 위를
// 차지하면 정렬한 의미가 없다.
function isBlank(value: unknown): boolean {
  return value == null || (typeof value === "string" && value.trim() === "");
}

function comparable(value: unknown): number | string {
  if (typeof value === "number") return value;
  if (typeof value === "boolean") return value ? 1 : 0;
  if (typeof value === "bigint") return Number(value);
  if (value instanceof Date) return value.getTime();
  if (typeof value === "object") {
    try {
      return JSON.stringify(value);
    } catch {
      return String(value);
    }
  }
  return String(value);
}

export function compareValues(a: unknown, b: unknown): number {
  const left = comparable(a);
  const right = comparable(b);
  if (typeof left === "number" && typeof right === "number") {
    // NaN 은 숫자가 아니므로 빈 값처럼 뒤로 보낸다.
    if (Number.isNaN(left) || Number.isNaN(right)) {
      return Number(Number.isNaN(left)) - Number(Number.isNaN(right));
    }
    return left - right;
  }
  // 한쪽만 숫자이면 숫자를 앞에 둔다 — 섞인 열에서도 순서가 매번 같아야 한다.
  if (typeof left === "number") return -1;
  if (typeof right === "number") return 1;
  return collator.compare(left, right);
}

// 안정 정렬: 값이 같은 행은 서버가 준 순서를 지킨다. 입력 배열은 건드리지 않는다.
export function sortRows<T>(
  rows: readonly T[],
  valueOf: (row: T) => unknown,
  direction: SortDirection,
): T[] {
  const sign = direction === "asc" ? 1 : -1;
  return rows
    .map((row, index) => ({ row, index, value: valueOf(row) }))
    .sort((a, b) => {
      const blankA = isBlank(a.value);
      const blankB = isBlank(b.value);
      if (blankA || blankB) {
        if (blankA && blankB) return a.index - b.index;
        return blankA ? 1 : -1;
      }
      return sign * compareValues(a.value, b.value) || a.index - b.index;
    })
    .map((entry) => entry.row);
}

// 열의 첫 정렬 방향. 처음 보이는 비어 있지 않은 값이 숫자이면 내림차순이다.
export function firstDirection(values: Iterable<unknown>): SortDirection {
  for (const value of values) {
    if (isBlank(value)) continue;
    return typeof value === "number" || typeof value === "bigint"
      ? "desc"
      : "asc";
  }
  return "asc";
}

// 같은 열을 누를 때마다 첫 방향 → 반대 방향 → 정렬 해제(서버 순서)로 돈다.
// 다른 열을 누르면 그 열의 첫 방향에서 새로 시작한다.
export function nextSort(
  current: SortState | null,
  key: string,
  first: SortDirection,
): SortState | null {
  if (!current || current.key !== key) return { key, direction: first };
  if (current.direction === first) {
    return { key, direction: first === "asc" ? "desc" : "asc" };
  }
  return null;
}
