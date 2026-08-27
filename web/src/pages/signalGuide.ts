/**
 * signalGuide turns the raw signal names the API returns into something a
 * reader can act on, and says whether the tracker produces the signal by itself.
 *
 * The Frustration report scores nine signals. Until the tracker learned to
 * detect them, seven of the nine only ever arrived from hand-written
 * instrumentation, so an empty table looked like "no friction" when it actually
 * meant "nothing was measuring". The report now says which is which.
 */

export type SignalOrigin = "automatic" | "manual";

export interface SignalDescription {
  label: string;
  meaning: string;
  action: string;
  origin: SignalOrigin;
}

export const SIGNAL_GUIDE: Record<string, SignalDescription> = {
  rage_click: {
    label: "Rage Click",
    meaning:
      "같은 요소를 1초 안에 3번 이상 눌렀습니다. 눌렀는데 반응이 없다고 느낀 상태입니다.",
    action:
      "해당 요소가 로딩 중 비활성인지, 클릭 영역이 실제로 눌리는지 확인합니다.",
    origin: "automatic",
  },
  dead_click: {
    label: "Dead Click",
    meaning:
      "누를 수 있어 보이는 요소를 눌렀지만 화면 변화, 이동, 스크롤, 포커스 이동이 전혀 없었습니다.",
    action:
      "장식용 요소가 버튼처럼 보이는지, 이벤트 핸들러가 빠졌는지 확인합니다.",
    origin: "automatic",
  },
  rapid_back: {
    label: "Rapid Back",
    meaning:
      "페이지에 도착한 지 3초 안에 뒤로 돌아갔습니다. 원하던 내용이 아니었다는 신호입니다.",
    action: "직전 화면의 링크 문구와 도착 화면의 내용이 일치하는지 확인합니다.",
    origin: "automatic",
  },
  form_retry: {
    label: "Form Retry",
    meaning: "같은 양식을 다시 제출했거나 입력 검증에 실패했습니다.",
    action:
      "어떤 항목이 반복 실패하는지 field 속성으로 확인하고 안내 문구를 고칩니다.",
    origin: "automatic",
  },
  repeated_search: {
    label: "Repeated Search",
    meaning:
      "같은 검색어를 2분 안에 다시 검색했습니다. 결과가 쓸 수 없었다는 뜻입니다.",
    action: "해당 검색어의 Zero Result와 CTR을 검색 분석에서 함께 봅니다.",
    origin: "automatic",
  },
  error_after_click: {
    label: "Error After Click",
    meaning:
      "클릭 2초 안에 오류가 발생했습니다. 사용자 행동과 실패가 직접 연결된 가장 무거운 신호입니다.",
    action: "element_id로 어떤 조작이 실패를 유발했는지 특정합니다.",
    origin: "automatic",
  },
  slow_interaction: {
    label: "Slow Interaction",
    meaning:
      "입력에 대한 응답이 500ms를 넘었습니다. INP 기준의 '나쁨' 구간입니다.",
    action:
      "Core Web Vitals의 INP와 함께 보고 해당 상호작용의 처리 시간을 줄입니다.",
    origin: "automatic",
  },
  collection_dropped: {
    label: "Collection Dropped",
    meaning:
      "브라우저가 오프라인인 동안 쌓인 이벤트가 저장 한도(200건)를 넘어 오래된 것부터 버려졌습니다. events_dropped가 잃어버린 건수입니다.",
    action:
      "이 신호가 반복되면 해당 구간의 수치는 실제보다 낮습니다. 네트워크가 끊기는 환경인지 확인하고, 결론을 내기 전에 이 건수를 함께 봅니다.",
    origin: "automatic",
  },
  error: {
    label: "JavaScript Error",
    meaning: "처리되지 않은 예외 또는 Promise 거부입니다.",
    action: "메시지와 파일 위치로 배포 버전별 오류 추이를 확인합니다.",
    origin: "automatic",
  },
  resource_error: {
    label: "Resource Error",
    meaning: "이미지, 스크립트, 스타일 같은 자원을 불러오지 못했습니다.",
    action: "사내망 경로와 CSP, 캐시 무효화를 확인합니다.",
    origin: "automatic",
  },
};

export function describeSignal(name: string): SignalDescription {
  return (
    SIGNAL_GUIDE[name] || {
      label: name,
      meaning: "사이트가 직접 보내는 사용자 정의 신호입니다.",
      action: "이 신호를 보내는 코드의 의도를 확인합니다.",
      origin: "manual",
    }
  );
}

/**
 * frustrationSetupHint explains an empty report. Zero signals across zero
 * affected sessions cannot be distinguished from a healthy service without
 * saying what is being measured.
 */
export function frustrationSetupHint(
  summary: { total_sessions?: number; affected_sessions?: number } | undefined,
): string | null {
  if (!summary) return null;
  if (!summary.total_sessions)
    return "이 기간에 세션이 없습니다. 먼저 추적 스니펫이 설치되어 이벤트가 수집되는지 확인하세요.";
  if (!summary.affected_sessions)
    return '세션은 수집되지만 Frustration 신호가 하나도 없습니다. Rage Click·Dead Click·Rapid Back·Form Retry·Error After Click·Slow Interaction은 tracker 0.22.0부터 자동 감지되므로, 스니펫이 이전 버전이면 갱신하고 `data-frustration-signals="false"`로 꺼두지 않았는지 확인하세요.';
  return null;
}

/**
 * searchSetupHint separates "nobody searched" from "searching is not measured"
 * and from "the term itself is deliberately not collected".
 */
export function searchSetupHint(
  summary: { searches?: number } | undefined,
  queries: { query?: unknown }[] | undefined,
): string | null {
  if (!summary) return null;
  if (!summary.searches)
    return "이 기간에 검색이 없습니다. tracker 0.22.0부터 결과 페이지의 q·query·search·keyword 같은 질의 문자열로 검색을 자동 인식합니다. URL이 바뀌지 않는 검색이라면 `analytics.trackSearch(질의, 결과수)`를 호출하세요.";
  const rows = queries || [];
  if (
    rows.length > 0 &&
    rows.every((row) => !row.query || row.query === "(not set)")
  )
    return '검색 횟수는 집계되지만 검색어가 수집되지 않았습니다. 검색어는 개인정보가 섞일 수 있어 기본적으로 보내지 않습니다. 필요하면 스니펫에 `data-collect-search-terms="true"`를 추가하세요.';
  return null;
}

/**
 * zeroResultReadiness reports whether the site publishes its result count. The
 * zero-result rate is the most actionable search metric and it cannot be
 * derived from the query, so a missing hook has to be visible.
 */
export function zeroResultReadiness(
  summary: { searches?: number; zero_results?: number } | undefined,
): string | null {
  if (!summary?.searches) return null;
  if (summary.zero_results) return null;
  return '결과 0건 검색이 한 건도 없습니다. 검색 결과 영역에 `data-momento-search-results="건수"`를 넣으면 결과 수가 함께 기록되어 Zero Result 비율을 신뢰할 수 있습니다.';
}

export interface FrictionImpact {
  signal: string;
  affected_people: number;
  unaffected_people: number;
  affected_conversion_rate: number;
  unaffected_conversion_rate: number;
  gap_points: number;
  verdict: "worse" | "better" | "similar" | "insufficient";
  reliable: boolean;
  estimated_lost_conversions: number;
  evidence: string;
}

export const impactVerdictLabel: Record<FrictionImpact["verdict"], string> = {
  worse: "전환 손실",
  better: "전환과 함께 발생",
  similar: "차이 없음",
  insufficient: "판단 보류",
};

/**
 * frictionHeadline names what to fix first. The report already ranks signals by
 * the conversions the gap accounts for, so the headline is the first reliable
 * harmful row — and when there is none, saying so is the finding.
 */
export function frictionHeadline(
  impact: FrictionImpact[] | undefined,
): string | null {
  if (!impact?.length) return null;
  const harmful = impact.filter((row) => row.verdict === "worse");
  if (!harmful.length) {
    const judged = impact.filter((row) => row.reliable);
    if (!judged.length)
      return "비교할 만한 인원이 모이지 않아 신호별 영향을 판단하지 않았습니다.";
    return "측정된 신호 중 전환율을 뚜렷하게 떨어뜨리는 것은 없습니다. 신호 발생 자체를 줄이는 것보다 다른 개선이 우선입니다.";
  }
  const first = harmful[0];
  const label = describeSignal(first.signal).label;
  return `먼저 고칠 것은 ${label}입니다. 겪은 ${first.affected_people.toLocaleString("ko-KR")}명의 전환율이 ${Math.abs(first.gap_points).toFixed(1)}%p 낮아 전환 약 ${first.estimated_lost_conversions.toLocaleString("ko-KR")}건에 해당합니다.`;
}

/**
 * metricMeaning explains the metric names that could be mistaken for each other.
 * Two session counts answer two different questions and used to share one name,
 * so the reader has to be told which is which.
 */
export const metricMeaning: Record<string, string> = {
  users: "사람 단위. 같은 SSO User로 연결된 여러 브라우저는 한 명입니다.",
  sessions:
    "이 기간에 활동이 있었던 Session. 자정을 넘긴 Session은 양쪽 기간에 모두 포함됩니다.",
  sessions_started:
    "이 기간에 시작된 Session. 첫 화면이 보여주는 값이며, 연속된 기간을 더했을 때 합이 맞습니다.",
  conversions: "전환으로 등록된 Event의 건수입니다. 사람 수가 아닙니다.",
  conversion_users: "전환한 사람의 수입니다.",
  conversion_sessions: "전환이 일어난 Session의 수입니다.",
  revenue: "`purchase` Event의 `value` 또는 `revenue` Property 합계입니다.",
};

/**
 * ecommerceSetupHint separates "nobody bought anything" from "buying is not
 * measured", and from the two halves of a purchase that a site can leave out.
 *
 * The ecommerce screen reads events a site sends itself — view_item,
 * add_to_cart, begin_checkout, purchase — and reads the money and the products
 * out of the purchase's own properties. Every one of those can be missing while
 * the screen still renders a complete-looking report of zeroes, and a zero is
 * indistinguishable from a bad month unless the screen says which it is.
 */
export function ecommerceSetupHint(
  summary: { transactions?: number; revenue?: number } | undefined,
  funnel: { event?: string; users?: number }[] | undefined,
  products: unknown[] | undefined,
): string | null {
  if (!summary) return null;
  const steps = funnel || [];
  const anyStep = steps.some((step) => (step.users || 0) > 0);
  if (!anyStep && !summary.transactions)
    return '이 기간에 이커머스 이벤트가 하나도 없습니다. 상품 조회·장바구니·결제 시작·구매는 자동으로 감지되지 않으므로 `analytics.track("view_item", …)`처럼 사이트가 직접 보내야 합니다.';
  if ((summary.transactions || 0) > 0 && !summary.revenue)
    return "구매는 수집되지만 금액이 비어 있습니다. `purchase` 이벤트에 `value`(또는 `revenue`) 속성을 숫자로 담아야 매출·객단가·환불이 계산됩니다.";
  if ((summary.transactions || 0) > 0 && (products || []).length === 0)
    return "구매는 수집되지만 상품 정보가 없습니다. 상품별 표는 `purchase` 이벤트의 `items` 배열을 읽으므로, 각 항목에 `item_id`·`price`·`quantity`를 담아 보내세요.";
  return null;
}
