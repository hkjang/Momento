/**
 * describeUserError turns a refused user create/update into something a Korean
 * console can act on.
 *
 * The two dialogs printed `error.message` — the server's own sentence — so the
 * most common refusal of all, an address that is already taken, arrived as the
 * driver's text: `ERROR: duplicate key value violates unique constraint
 * "users_email_key" (SQLSTATE 23505)`. internal/httpapi/admin.go answers the
 * failed INSERT with `writeError(w, 409, "USER_CREATE_FAILED", err.Error())`,
 * so the constraint's name and the SQLSTATE travelled from the schema to the
 * browser, and the reader was left to infer "이메일이 이미 있다" from it. The
 * rest were English too: `you cannot grant more authority than your own`,
 * `password must be at least 12 characters`.
 *
 * Shaped after components/queryError.ts: the code is read off the error by
 * shape rather than by importing APIError, and an unknown code falls back to
 * the server's message so a refusal added to the server later is still shown
 * rather than swallowed.
 */

// The extension is written out because this module is also loaded straight by
// node --test, which resolves the specifier literally (tsconfig.app.json sets
// allowImportingTsExtensions, so tsc and vite take it too). A type-only import
// would not need it — those are stripped before resolution — but PASSWORD_RULE
// is a value, and the rule the Alert quotes must be the one the form quotes.
import { PASSWORD_RULE } from "./passwordRule.ts";
import { CIDR_RULE } from "./cidrRule.ts";
// 보존 정책 다섯 칸의 범위와 그 범위를 말하는 문장은 retentionRule.ts 가 단일
// 출처다 — 칸 아래의 helperText 와 이 Alert 이 같은 상수를 쓰게 해서, 사용자가
// 저장을 눌러 400 을 받은 뒤에야 범위를 읽는 일을 없앤다.
import { RETENTION_LIMITS, retentionNotice } from "./retentionRule.ts";
// 「JSON Schema」 칸의 구문 안내는 schemaTextRule.ts 가 단일 출처다 — 칸 아래의
// helperText 와 이 Alert 이 같은 상수를 쓰게 해서, 사용자가 「저장」 을 눌러
// 헛클릭 왕복을 한 번 치른 뒤에야 안내를 읽는 일을 없앤다.
import { JSON_SCHEMA_RULE } from "./schemaTextRule.ts";

export interface UserErrorNotice {
  /** The Korean guidance to show. Never empty. */
  message: string;
  /** The server's own message, kept only where it is the sole remaining clue. */
  detail?: string;
}

/** Two codes carry more than one cause, so the sentence has to be read. */
const PASSWORD_PROBLEM = /password must be/;
/**
 * `administer` only appears in the refusal to touch an account above the
 * caller (admin.go:976); the other two ROLE_ABOVE_CALLER sentences are about
 * the role being granted. The thing to correct differs, so the guidance does.
 */
const ABOUT_THE_TARGET = /administer/;
/**
 * A uniqueness violation, which is the one thing a 409 from the INSERT is
 * usually about. Status alone is not enough: that INSERT answers 409 whatever
 * went wrong, so a dropped connection would be reported as a taken address.
 */
const UNIQUENESS_VIOLATION = /duplicate key|unique constraint|23505/i;

const PASSWORD_NOTICE = `비밀번호가 규칙에 맞지 않습니다. ${PASSWORD_RULE}`;
const ROLE_TOO_HIGH =
  "내 권한보다 높은 권한은 줄 수 없습니다. 권한을 내 계정의 권한 이하로 고르세요.";
// The console already hides these before they can be sent (roleScope.ts), so
// reaching one means the list on screen is older than the row behind it —
// which is why every one of them says to refresh rather than only what is
// forbidden.
const STALE = "목록이 오래되었을 수 있으니 새로 고친 뒤";

/**
 * The one place a refusal is read off the error, so the two describe\*
 * functions below cannot come to disagree about what the server said. Reading
 * is shared; the code sets each function answers is not — see
 * describeSiteError.
 */
function refusal(error: unknown): { code: string; message: string } {
  return {
    code:
      typeof error === "object" &&
      error !== null &&
      typeof (error as { code?: unknown }).code === "string"
        ? (error as { code: string }).code
        : "",
    message: error instanceof Error ? error.message : "",
  };
}

/** The sentence every 500 gets: say nothing about the cause, keep the clue. */
const PASS_TO_ADMIN =
  "잠시 후 다시 시도하고, 반복되면 아래 메시지를 관리자에게 전달하세요.";
const UNREADABLE_PAYLOAD =
  "보낸 내용을 서버가 읽지 못했습니다. 화면을 새로 고친 뒤 다시 시도하세요.";
const LOST_REQUEST =
  "서버가 요청을 처리하지 못했습니다. 네트워크를 확인하고 다시 시도하세요.";

/** 「Custom Dimension」 등록·갱신의 서버 계약은 다른 관리 폼과 분리한다. */
export function describeDimensionError(error: unknown): UserErrorNotice {
  const { code, message } = refusal(error);

  switch (code) {
    case "INVALID_PAYLOAD":
      return { message: UNREADABLE_PAYLOAD, detail: message || undefined };
    case "UNKNOWN_SITE":
      return {
        message:
          "이 사이트를 찾을 수 없습니다. 화면을 새로 고친 뒤 다시 시도하세요.",
      };
    case "INVALID_DIMENSION":
      // saveDimension 은 어느 칸이 틀렸는지 구분하지 않는다. 두 칸에 적용되는
      // PropertyKeyPattern 의 첫 글자 제한과 전체 길이까지 함께 안내한다.
      return {
        message:
          "「Dimension 이름」과 「Property key」를 확인하세요. 두 칸 모두 첫 글자는 영문(A–Z, a–z) 또는 _로 시작하고, 이후에는 영문·숫자·_·.·-만 사용할 수 있습니다. 전체 길이는 1~128자입니다.",
      };
    case "INVALID_SCOPE":
      return {
        message:
          "「Scope」 값이 올바르지 않습니다. 화면을 새로 고친 뒤 User·Session·Event·Item (Ecommerce) 중에서 다시 고르세요.",
      };
    case "INVALID_DATA_TYPE":
      return {
        message:
          "「Data type」 값이 올바르지 않습니다. 화면을 새로 고친 뒤 string·number·boolean·date 중에서 다시 고르세요.",
      };
    case "DIMENSION_SAVE_FAILED":
      // ON CONFLICT(site_id,name) DO UPDATE 이므로 이름 중복으로 단정하지 않는다.
      // DB 실패의 원문은 본문과 분리해 관리자에게 전달할 단서로만 남긴다.
      return {
        message: `Custom Dimension을 저장하지 못했습니다. ${PASS_TO_ADMIN}`,
        detail: message || undefined,
      };
    case "REQUEST_FAILED":
      return { message: LOST_REQUEST, detail: message || undefined };
    default:
      return { message: message || "요청을 완료하지 못했습니다." };
  }
}

export function describeUserError(error: unknown): UserErrorNotice {
  const { code, message } = refusal(error);

  switch (code) {
    case "INVALID_PAYLOAD":
      return {
        message:
          UNREADABLE_PAYLOAD,
        detail: message || undefined,
      };
    case "INVALID_USER":
      // One code, two causes (admin.go:892 role / :896 password). The role
      // sentence mentions "password" as well, so it is matched on the phrase
      // PasswordProblem actually produces rather than on the word.
      return {
        message: PASSWORD_PROBLEM.test(message)
          ? PASSWORD_NOTICE
          : `권한과 비밀번호를 확인하세요. 권한은 목록에서 고르고, 비밀번호는 ${PASSWORD_RULE}`,
      };
    case "WEAK_PASSWORD":
      return { message: PASSWORD_NOTICE };
    case "ROLE_ABOVE_CALLER":
      return {
        message: ABOUT_THE_TARGET.test(message)
          ? `내 권한보다 높은 계정은 편집할 수 없습니다. ${STALE} 다시 시도하세요.`
          : ROLE_TOO_HIGH,
      };
    case "USER_CREATE_FAILED":
      // 409 from the INSERT and 500 from a failed hash share this code. Only
      // the uniqueness markers justify naming the address; without them the
      // honest answer is that the account was not created, plus the message.
      return UNIQUENESS_VIOLATION.test(message)
        ? {
            message:
              "이미 등록된 이메일입니다. 다른 이메일을 쓰거나, 사용자 목록에서 기존 계정을 찾아 편집하세요.",
          }
        : {
            message:
              `계정을 만들지 못했습니다. ${PASS_TO_ADMIN}`,
            detail: message || undefined,
          };
    case "USER_UPDATE_FAILED":
      return {
        message:
          `변경 내용을 저장하지 못했습니다. ${PASS_TO_ADMIN}`,
        detail: message || undefined,
      };
    case "QUERY_FAILED":
      return {
        message:
          `사용자 정보를 읽지 못했습니다. ${PASS_TO_ADMIN}`,
        detail: message || undefined,
      };
    case "INVALID_ID":
      return {
        message:
          "이 사용자를 가리킬 수 없습니다. 목록을 새로 고친 뒤 다시 시도하세요.",
      };
    case "INVALID_ROLE":
      return {
        message: `권한 값이 올바르지 않습니다. ${STALE} 권한을 다시 고르세요.`,
      };
    case "USER_NOT_FOUND":
      return {
        message:
          "이 사용자가 이미 삭제되었습니다. 목록을 새로 고친 뒤 확인하세요.",
      };
    case "SELF_DISABLE":
      return {
        message: "자기 계정은 중지할 수 없습니다. 다른 관리자에게 요청하세요.",
      };
    case "SELF_ROLE":
      return {
        message:
          "자기 계정의 권한은 바꿀 수 없습니다. 다른 관리자에게 요청하세요.",
      };
    case "SELF_PASSWORD":
      return {
        message:
          "자기 비밀번호는 내 프로필에서 현재 비밀번호를 확인한 뒤 바꿔야 합니다.",
      };
    case "REQUEST_FAILED":
      // client.ts fills this in when the response carried no code of its own,
      // which is what a proxy's error page or a lost connection looks like.
      return {
        message:
          LOST_REQUEST,
        detail: message || undefined,
      };
    default:
      // A code this module has not been taught yet still has to say something.
      return { message: message || "요청을 완료하지 못했습니다." };
  }
}

/**
 * describeSiteError does for the two site dialogs — 「새 분석 사이트」 and
 * 「사이트 분석 설정」 — what describeUserError does for the user ones.
 *
 * Both printed `error.message`, so the commonest refusal arrived in English:
 * 「IANA 시간대」 is a free-text box, and typing `Seoul` instead of
 * `Asia/Seoul` fetched `timezone must be a valid IANA timezone`
 * (admin.go:249·343) without saying what a valid name looks like. The 500s
 * were worse — createSite and updateSite answer with `err.Error()`
 * (admin.go:269·352·376), so pgx's own text travelled to the browser.
 *
 * Kept separate from describeUserError on purpose: the two handlers share only
 * the names of a few codes, and `INVALID_PAYLOAD` aside, what each one means
 * and what the reader has to correct differ. Merging them would make one
 * switch answer for two contracts. Only `refusal` — reading the code and
 * message off the error — is shared.
 */
export function describeSiteError(error: unknown): UserErrorNotice {
  const { code, message } = refusal(error);

  switch (code) {
    case "INVALID_PAYLOAD":
      return { message: UNREADABLE_PAYLOAD, detail: message || undefined };
    case "INVALID_NAME":
      // The 생성 button is disabled while the box is empty (AdminPage.tsx),
      // so this arrives when it holds spaces only and the server's TrimSpace
      // takes them off. Saying "이름을 입력하세요" alone would read as a lie
      // to someone looking at a box that is not blank.
      return {
        message:
          "사이트 이름을 입력하세요. 공백만 적으면 이름이 비어 있는 것으로 처리됩니다.",
      };
    case "INVALID_TIMEZONE":
      // The one refusal worth naming the fix for: time.LoadLocation wants a
      // 지역/도시 name, and the box offers no list to pick from.
      return {
        message:
          "시간대 이름이 올바르지 않습니다. 「IANA 시간대」 칸에 Asia/Seoul 처럼 지역/도시 형태로 적으세요.",
      };
    case "INVALID_ENGAGEMENT_THRESHOLD":
      return { message: "「참여 기준 시간(초)」 은 1초에서 300초 사이로 적으세요." };
    case "INVALID_TIMEOUT":
      // 설정 다이얼로그에서만 온다 — 생성 쪽에는 이 칸이 없고, createSite 는
      // 0 을 30 으로 바꿔 넣는다.
      return { message: "「세션 만료(분)」 은 1분에서 1440분 사이로 적으세요." };
    case "INVALID_ID":
      return {
        message:
          "이 사이트를 가리킬 수 없습니다. 목록을 새로 고친 뒤 다시 시도하세요.",
      };
    case "NOT_FOUND":
      return {
        message:
          "이 사이트가 이미 삭제되었습니다. 목록을 새로 고친 뒤 확인하세요.",
      };
    case "SITE_CREATE_FAILED":
      // Deliberately says nothing about the cause. This 500 is what an empty
      // workspaces table looks like (admin.go:264 → `no rows in result set`)
      // and what a database failure looks like, and there is no marker that
      // separates them for certain. Naming a duplicate name would be plainly
      // false: sites has UNIQUE on site_key only, not on name
      // (001_initial.sql:34-49).
      return {
        message: `사이트를 만들지 못했습니다. ${PASS_TO_ADMIN}`,
        detail: message || undefined,
      };
    case "SITE_UPDATE_FAILED":
      return {
        message: `사이트 설정을 저장하지 못했습니다. ${PASS_TO_ADMIN}`,
        detail: message || undefined,
      };
    case "REQUEST_FAILED":
      return { message: LOST_REQUEST, detail: message || undefined };
    default:
      return { message: message || "요청을 완료하지 못했습니다." };
  }
}

/**
 * describeNetworkError does for 「망 구분 추가」 what the two above do for the
 * user and site dialogs.
 *
 * Its Alert printed `error.message` too (AdminPage.tsx), and this form is the
 * one with a hand-typed CIDR in it, so the commonest refusal of all arrived as
 * the server's three words: `CIDR is invalid` (admin.go:813). That names what
 * is wrong and nothing about what to write instead, while the box right above
 * it already shows the shape — so the guidance quotes that same example.
 *
 * Kept separate from the other two for the same reason they are separate from
 * each other: createNetwork answers three codes of its own and the thing the
 * reader has to correct is a different field. Only `refusal` is shared.
 */
export function describeNetworkError(error: unknown): UserErrorNotice {
  const { code, message } = refusal(error);

  switch (code) {
    case "INVALID_PAYLOAD":
      return { message: UNREADABLE_PAYLOAD, detail: message || undefined };
    case "INVALID_CIDR":
      // net.ParseCIDR refused the text outright (admin.go:813), which is a
      // missing or out-of-range prefix length rather than a misplaced one —
      // ParseCIDR accepts host bits to the right of the netmask. So the
      // guidance is about the notation, and the example matches the 칸's own
      // placeholder.
      //
      // The notation half now lives in cidrRule.ts, which the 폼 reads for its
      // helperText. The 폼 screens out most of what reaches this branch, so the
      // two have to say the same thing for the few that still get through —
      // sharing the constant is what makes that true by construction rather
      // than by someone remembering to edit both.
      return { message: `CIDR 표기가 올바르지 않습니다. ${CIDR_RULE}` };
    case "NETWORK_CREATE_FAILED":
      // Deliberately says nothing about the cause. network_ranges carries no
      // UNIQUE at all (001_initial.sql:64-70 is id PRIMARY KEY and four plain
      // columns), so calling this an already-registered 망 or CIDR would be
      // plainly false. The column is Postgres `cidr`, which may well refuse a
      // value net.ParseCIDR accepted — but that has not been reproduced
      // against a database here, so it is not named either. The server's text
      // is kept as the one remaining clue.
      return {
        message: `망 구분을 추가하지 못했습니다. ${PASS_TO_ADMIN}`,
        detail: message || undefined,
      };
    case "REQUEST_FAILED":
      return { message: LOST_REQUEST, detail: message || undefined };
    default:
      return { message: message || "요청을 완료하지 못했습니다." };
  }
}

/**
 * describeEventDefinitionError does for 「Event Schema」 what the three above do
 * for the user, site and 망 구분 forms — with one difference that changes the
 * shape of the function.
 *
 * This is the only one of the four whose commonest failure never reaches the
 * server. The 「JSON Schema」 칸 is five rows of free text, and the mutation
 * evaluates `JSON.parse(form.schemaText)` inside `mutationFn` (AdminPage.tsx),
 * so one stray character makes V8 throw before the request is built —
 * react-query catches that synchronous throw and puts it in `save.error`, where
 * the Alert printed it: `Unexpected token '}', "{"properties": }" is not valid
 * JSON`. Confirmed in a real browser against the production bundle, not
 * inferred from the library's source.
 *
 * That error carries no `code`, so a switch on the code alone drops it into the
 * default branch and hands the engine's English straight back. Hence the syntax
 * branch sits *before* the switch.
 *
 * Kept separate from the other three for the reason they are separate from each
 * other: upsertEventDefinition (admin.go:1395~) answers its own small set of
 * codes and the field the reader has to correct is its own. Only `refusal` and
 * the shared sentences are reused.
 */
export function describeEventDefinitionError(error: unknown): UserErrorNotice {
  const { code, message } = refusal(error);

  // Before the switch, and deliberately so — see above. `instanceof` holds
  // because the throw and this check happen in the same realm (the bundle's
  // own JSON.parse call). The message pattern is only a second layer for the
  // day that stops being true, and it is guarded by the absence of a code:
  // every refusal that came from the server has one, because client.ts fills
  // in REQUEST_FAILED when the response carried none.
  if (error instanceof SyntaxError || (!code && /JSON/.test(message))) {
    return {
      // Says which box, because the error itself cannot: the screen has two
      // other free-text boxes and the engine's text names none of them. The
      // sentence lives in schemaTextRule.ts because the box now says it while
      // the text is still being typed — this Alert is only the case where the
      // reader got here without reading it.
      message: JSON_SCHEMA_RULE,
      // The engine's own text is the only clue to *where* the text broke, so
      // it is kept — as the caption, never as the guidance.
      detail: message || undefined,
    };
  }

  switch (code) {
    case "INVALID_PAYLOAD":
      return { message: UNREADABLE_PAYLOAD, detail: message || undefined };
    case "INVALID_MODE":
      // 「정책」 은 allow·warn·reject 세 개짜리 select 이므로(AdminPage.tsx),
      // 여기 닿았다는 것은 화면이 보낸 값이 서버가 아는 세 개와 어긋났다는
      // 뜻이다 — 고를 값이 틀린 게 아니라 화면이 오래된 것이다.
      return {
        message:
          "「정책」 값이 올바르지 않습니다. 화면을 새로 고친 뒤 allow·warn·reject 중에서 다시 고르세요.",
      };
    case "DEFINITION_SAVE_FAILED":
      // Deliberately says nothing about the cause. Six different places answer
      // with this one code (Begin, the INSERT, the version lookup, two Execs,
      // Commit), and the INSERT selects the site by `site_key`, so a site_key
      // that matches nothing inserts zero rows and arrives here as
      // pgx.ErrNoRows — indistinguishable from a database outage. Calling it an
      // already-registered event would be plainly false either way: the INSERT
      // carries `ON CONFLICT(site_id,name) DO UPDATE`, so a repeated name is
      // absorbed rather than refused.
      return {
        message: `이벤트 규격을 저장하지 못했습니다. ${PASS_TO_ADMIN}`,
        detail: message || undefined,
      };
    case "REQUEST_FAILED":
      return { message: LOST_REQUEST, detail: message || undefined };
    default:
      return { message: message || "요청을 완료하지 못했습니다." };
  }
}

/**
 * 「보존 정책」 칸 다섯 개가 범위를 벗어났을 때 읽을 수 있는 것을 돌려준다.
 *
 * 이 Alert 도 `error.message` 를 그대로 띄웠다(AdminPage.tsx). 다섯 칸이 전부
 * `type="number"` 자유 입력이라 범위를 벗어난 값이 흔한데, 그때 올라오는 것은
 * 화면에 **없는** 영문 컬럼명이었다 — `raw_event_months must be between 1 and
 * 120`. 읽는 사람은 `raw_event_months` 가 「Raw Event (개월)」 칸이라는 것을
 * 스스로 번역해야 했고, 500 으로 떨어지면 putRetentionPolicy 가
 * `err.Error()` 를 그대로 싣기 때문에(advanced_analytics.go:117) pgx 원문이
 * 브라우저까지 흘렀다.
 *
 * 앞의 네 describe\* 와 합치지 않았다 — 핸들러마다 답하는 코드 집합이 다르고,
 * 고쳐야 할 칸도 다르다. `refusal` 과 공용 문장만 재사용한다.
 */
export function describeRetentionError(error: unknown): UserErrorNotice {
  const { code, message } = refusal(error);

  switch (code) {
    case "INVALID_PAYLOAD":
      return { message: UNREADABLE_PAYLOAD, detail: message || undefined };
    case "INVALID_RETENTION": {
      // 코드 하나에 원인이 다섯이므로 문장을 읽어 가른다 — PASSWORD_PROBLEM
      // (37행)과 같은 모양이다. 다섯 컬럼명은 `_months` 를 셋이 공유하지만
      // 어느 것도 다른 것의 부분문자열이 아니라서 단순 포함 검사로 갈린다.
      const field = RETENTION_LIMITS.find((entry) =>
        message.includes(entry.column),
      );
      if (field) return { message: retentionNotice(field.column) };
      // 서버가 검사를 더 늘린 경우. 영문을 본문에 올리지 않고 중립 문구로
      // 되돌리되, 어느 칸인지의 유일한 단서인 원문은 caption 으로 남긴다.
      return {
        message:
          "보존기간 값이 허용 범위를 벗어났습니다. 각 칸 아래에 적힌 범위를 확인하고 다시 저장하세요.",
        detail: message || undefined,
      };
    }
    case "UNKNOWN_SITE":
      // RetentionAdmin 은 useSite() 의 사이트로만 요청하므로 사이트가 지워진
      // 직후에만 닿는다. 그 도달성을 확인하지 않았으니 원인을 단정하지 않고
      // 화면이 오래되었을 가능성만 말한다.
      return {
        message:
          "이 사이트를 찾을 수 없습니다. 화면을 새로 고친 뒤 다시 시도하세요.",
      };
    case "RETENTION_SAVE_FAILED":
      // 원인을 특정하지 않는다. INSERT 가 `ON CONFLICT(site_id) DO UPDATE`
      // (advanced_analytics.go:115) 라 '이미 등록된 정책' 은 거짓이고, 같은
      // 500 을 DB 장애도 쓴다. 둘을 확실히 갈라 주는 표지가 없다.
      return {
        message: `보존 정책을 저장하지 못했습니다. ${PASS_TO_ADMIN}`,
        detail: message || undefined,
      };
    case "REQUEST_FAILED":
      return { message: LOST_REQUEST, detail: message || undefined };
    default:
      return { message: message || "요청을 완료하지 못했습니다." };
  }
}
