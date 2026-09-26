// 표의 현재 페이지는 컴포넌트 상태이고, 보여줄 행의 수는 검색어와 재조회로 줄어든다.
// 둘이 어긋난 채 slice 하면 빈 배열이 나오는데, 표의 Empty 안내문은 필터 결과가 아예
// 없을 때만 나오므로 행도 안내문도 없는 빈 표가 남는다. 검색을 치면 페이지를 0으로
// 돌리는 effect 는 렌더·페인트 뒤에 돌고, 행 수가 그대로인 채 내용만 바뀐 재조회에서는
// 아예 돌지 않는다. 그래서 자르기 전에 페이지를 실제 범위 안으로 끌어온다 — 표시용
// 페이지 번호와 slice 가 같은 값을 읽도록 계산은 여기 한 곳에 둔다.

export function clampPage(
  page: number,
  pageSize: number,
  total: number,
): number {
  // 행이 없으면 어떤 페이지도 행을 가리키지 못하고, pageSize 가 0 이하이면 나눌 수
  // 없다(마지막 페이지가 Infinity 가 된다). 두 경우 모두 첫 페이지를 돌려주어
  // 호출자가 언제나 유효한 시작 위치를 얻게 한다.
  if (total <= 0 || pageSize <= 0) return 0;
  if (!Number.isFinite(page) || page <= 0) return 0;
  const lastPage = Math.ceil(total / pageSize) - 1;
  return Math.min(Math.trunc(page), lastPage);
}
