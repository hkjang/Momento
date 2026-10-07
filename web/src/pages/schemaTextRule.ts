// 「Event Schema」 폼의 「JSON Schema」 칸이 「저장」 을 누르기 전에 쓰는 판정.
//
// 이 칸은 다섯 줄 자유 입력인데 「저장」 은 `!site || !form.name` 만 보았고
// helperText 도 없었다(AdminPage.tsx 의 SchemasAdmin). 그래서 깨진 JSON 으로
// 저장을 누르면 뮤테이션 안의 `JSON.parse(form.schemaText)` 가 던져 **요청이
// 서버에 가지도 않는데**, 사용자는 그 헛클릭 왕복을 한 번 치른 뒤에야 안내를
// 읽었다. v0.34.57 이 그 뒤늦은 안내를 한국어로 만들었으니(adminErrors.ts 의
// describeEventDefinitionError) 남은 것은 그 문장을 **누르기 전에** 보여 주는
// 것뿐이다.
//
// cidrRule.ts·retentionRule.ts 와 같은 자리의 순수 모듈이고 같은 이유로 문장을
// 내보낸다 — adminErrors.ts 의 JSON 구문 안내와 칸 아래의 helperText 가 같은
// 상수를 쓰게 해서 둘이 갈라질 수 없게 한다.
//
// 이 칸이 그 두 선례와 다른 점 하나: 판정자가 서버가 아니라 **화면 자신의 같은
// `JSON.parse`** 다. 그래서 '화면이 서버보다 좁아진다' 는 종류의 결함이 구조적으로
// 불가능하고, 그것이 이 칸에서 「저장」 을 닫아도 되는 이유다 — 요청이 만들어지지도
// 않으니 닫아서 잃는 것이 없다. 다른 칸(`name`·`정책`·`Conversion`)이나 다른
// 화면의 저장 버튼에는 이 논리가 **적용되지 않는다**: 거기서는 서버가 판정의
// 정본이고 화면은 거울이다.

/**
 * 깨진 JSON 에 보여 주는 문장. adminErrors.ts 의 describeEventDefinitionError
 * 가 저장 실패 Alert 에서 쓰는 것과 **같은 상수**다 — 두 자리가 한 곳에서
 * 나오므로 글자가 갈라질 수 없다.
 */
export const JSON_SCHEMA_RULE =
  "「JSON Schema」 칸의 내용이 올바른 JSON 이 아닙니다. 중괄호·대괄호의 짝과 쉼표 위치를 확인하고, 키와 문자열 값은 겹따옴표로 감싸세요. 규격을 비워 둘 때는 {} 로 적습니다.";

/** cidrRule.ts 의 CIDRJudgement 와 같은 모양 — 같은 디렉터리, 같은 역할. */
export type SchemaTextJudgement = {
  /** 있으면 「저장」 을 닫고 이 문장을 칸 아래에 보인다. */
  blocking?: string;
  /** 구문이 분명히 깨졌을 때만. 빈 칸은 색을 쓰지 않는다. */
  error?: true;
  /**
   * 엔진 원문. **어디가** 깨졌는지에 대한 유일한 단서라 버리지 않지만,
   * 영문이므로 안내 본문에는 넣지 않는다 — 보일 자리는 별도 캡션이고, 저장
   * 실패 Alert 이 이미 같은 값을 캡션으로 쓴다(adminErrors.ts).
   */
  detail?: string;
};

/**
 * 판정의 본체는 뮤테이션이 돌릴 그 `JSON.parse` 를 한 번 미리 돌려 보는 것뿐이다.
 *
 * **구문만** 본다. 타입은 보지 않는다 — 서버의 upsertEventDefinition 은
 * `Schema map[string]any`(internal/httpapi/admin.go) 로 디코드하므로 `[1,2]`·
 * `"hi"`·`5` 는 400 INVALID_PAYLOAD 로 거절하지만 **`null` 은 통과한다**(nil map
 * → json.Marshal 이 `null`). 그래서 "객체가 아니면 막는다" 는 규칙은 `null` 에서
 * 화면을 서버보다 좁게 만든다. cidrRule 이 IPv6 본문을 통과시킨 것과 같은
 * 원칙이다 — 판정이 서버와 갈릴 수 있는 것은 통과시킨다.
 */
export function judgeSchemaText(text: string): SchemaTextJudgement {
  // 빈 칸도 `JSON.parse("")` 가 던지므로 지금도 저장이 되지 않는 값이다. 그래서
  // 「저장」 은 닫되, 폼을 열자마자 빨간 글씨가 보이지 않게 오류 색은 쓰지
  // 않는다 — cidrRule.ts 가 미완성 입력을 다루는 것과 같은 자리.
  if (text.trim() === "") {
    return { blocking: `규격이 비어 있습니다. ${JSON_SCHEMA_RULE}` };
  }

  try {
    JSON.parse(text);
  } catch (error) {
    return {
      blocking: JSON_SCHEMA_RULE,
      error: true,
      detail: error instanceof Error ? error.message || undefined : undefined,
    };
  }
  return {};
}

/** 깨진 곳이 없을 때 칸 아래에 늘 서 있는 설명. */
export const SCHEMA_TEXT_HELP =
  "속성 규격을 JSON 으로 적습니다. 비워 둘 때는 {} 로 적으세요.";
