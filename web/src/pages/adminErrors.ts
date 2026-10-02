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

export function describeUserError(error: unknown): UserErrorNotice {
  const code =
    typeof error === "object" &&
    error !== null &&
    typeof (error as { code?: unknown }).code === "string"
      ? (error as { code: string }).code
      : "";
  const message = error instanceof Error ? error.message : "";

  switch (code) {
    case "INVALID_PAYLOAD":
      return {
        message:
          "보낸 내용을 서버가 읽지 못했습니다. 화면을 새로 고친 뒤 다시 시도하세요.",
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
              "계정을 만들지 못했습니다. 잠시 후 다시 시도하고, 반복되면 아래 메시지를 관리자에게 전달하세요.",
            detail: message || undefined,
          };
    case "USER_UPDATE_FAILED":
      return {
        message:
          "변경 내용을 저장하지 못했습니다. 잠시 후 다시 시도하고, 반복되면 아래 메시지를 관리자에게 전달하세요.",
        detail: message || undefined,
      };
    case "QUERY_FAILED":
      return {
        message:
          "사용자 정보를 읽지 못했습니다. 잠시 후 다시 시도하고, 반복되면 아래 메시지를 관리자에게 전달하세요.",
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
          "서버가 요청을 처리하지 못했습니다. 네트워크를 확인하고 다시 시도하세요.",
        detail: message || undefined,
      };
    default:
      // A code this module has not been taught yet still has to say something.
      return { message: message || "요청을 완료하지 못했습니다." };
  }
}
