// 표의 캡션 한 줄은 두 가지를 겸한다 — 이 표가 무엇인지(description)와 지금 몇 건을
// 보고 있는지(건수). 그런데 description 을 넘긴 표에서 `description || 건수` 로 고르면
// 검색 중 일치 건수가 어디에도 남지 않는다. 일치가 10건 이하로 줄면 TablePagination
// 자체가 사라져(`filtered.length > 10`) `1–N / N` 도 없어지므로 규모를 알 수단이 0이
// 된다. 그래서 검색 중에는 description 과 건수를 둘 다 내보내고, 검색어가 없을 때는
// 기존 출력을 한 글자도 바꾸지 않는다 — 어느 쪽을 보여줄지의 판단을 이 한 곳에 둔다.

const numbers = Intl.NumberFormat("ko-KR");

export function tableCaption({
  description,
  total,
  matched,
  searching,
}: {
  description?: string;
  total: number;
  matched: number;
  searching: boolean;
}): string {
  // 검색어가 없으면 matched === total 이라 건수를 겹쳐 적을 이유가 없다. 기존 캡션을
  // 그대로 돌려주어 검색하지 않는 모든 화면의 문구가 변하지 않게 한다.
  if (!searching) return description || `${numbers.format(total)}개 항목`;
  const counts = `전체 ${numbers.format(total)}개 중 ${numbers.format(matched)}개 일치`;
  return description ? `${description} · ${counts}` : counts;
}
