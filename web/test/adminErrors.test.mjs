import assert from "node:assert/strict";
import test from "node:test";
import { describeUserError } from "../src/pages/adminErrors.ts";

// internal/httpapi/admin.go 의 createUser(877행~)·updateUser(924행~) 가 실제로
// 돌려주는 (status, code, message) 를 그대로 옮긴 것. 서버 문장은 writeError 의
// 인자 그대로이고, pgx 원문은 users.email 의 UNIQUE 제약
// (internal/database/migrations/001_initial.sql:17) 에서 나오는 것이다.
const DUPLICATE_EMAIL_PGX =
  'ERROR: duplicate key value violates unique constraint "users_email_key" (SQLSTATE 23505)';

// passwordRule.ts 의 PASSWORD_RULE 과 같은 문장. 안내가 그 모듈을 읽으므로 규칙이
// 바뀌면 여기도 함께 바뀌어야 한다 — 두 곳이 갈라지는 것을 이 단언이 잡는다.
const RULE = "12자 이상, 72바이트(한글 24자) 이하.";

// APIError 를 import 하지 않고 같은 shape 만 만든다 — 모듈이 code·message 를
// shape 으로 읽는다는 계약(queryError.ts:44-53 선례)을 테스트도 같은 방식으로 쓴다.
function apiError(status, code, message) {
  const error = new Error(message);
  error.status = status;
  error.code = code;
  return error;
}

// 서버가 실제로 내는 코드 전부와, 각각에 나와야 하는 안내 전문.
// detail: true 면 서버 원문이 유일한 단서라 따로 들고 있어야 하는 경우다.
const SERVER_ERRORS = [
  // --- createUser ---
  {
    label: "생성: 본문을 읽지 못함",
    status: 400,
    code: "INVALID_PAYLOAD",
    message: "unexpected EOF",
    expected:
      "보낸 내용을 서버가 읽지 못했습니다. 화면을 새로 고친 뒤 다시 시도하세요.",
    detail: true,
  },
  {
    label: "생성: 역할이 유효하지 않음",
    status: 400,
    code: "INVALID_USER",
    message: "valid role and password of at least 12 characters are required",
    expected: `권한과 비밀번호를 확인하세요. 권한은 목록에서 고르고, 비밀번호는 ${RULE}`,
    detail: false,
  },
  {
    label: "생성: 비밀번호가 짧음",
    status: 400,
    code: "INVALID_USER",
    message: "password must be at least 12 characters",
    expected: `비밀번호가 규칙에 맞지 않습니다. ${RULE}`,
    detail: false,
  },
  {
    label: "생성: 비밀번호가 72바이트를 넘음",
    status: 400,
    code: "INVALID_USER",
    message: "password must be at most 72 bytes",
    expected: `비밀번호가 규칙에 맞지 않습니다. ${RULE}`,
    detail: false,
  },
  {
    label: "생성: 내 권한보다 높은 계정",
    status: 403,
    code: "ROLE_ABOVE_CALLER",
    message: "you cannot create an account with more authority than your own",
    expected:
      "내 권한보다 높은 권한은 줄 수 없습니다. 권한을 내 계정의 권한 이하로 고르세요.",
    detail: false,
  },
  {
    label: "생성: 해시 실패 (500)",
    status: 500,
    code: "USER_CREATE_FAILED",
    message: "bcrypt: password length exceeds 72 bytes",
    expected:
      "계정을 만들지 못했습니다. 잠시 후 다시 시도하고, 반복되면 아래 메시지를 관리자에게 전달하세요.",
    detail: true,
  },
  {
    label: "생성: 이메일 중복 (409)",
    status: 409,
    code: "USER_CREATE_FAILED",
    message: DUPLICATE_EMAIL_PGX,
    expected:
      "이미 등록된 이메일입니다. 다른 이메일을 쓰거나, 사용자 목록에서 기존 계정을 찾아 편집하세요.",
    detail: false,
  },
  // --- updateUser ---
  {
    label: "편집: 사용자 id 가 올바르지 않음",
    status: 400,
    code: "INVALID_ID",
    message: "invalid user id",
    expected:
      "이 사용자를 가리킬 수 없습니다. 목록을 새로 고친 뒤 다시 시도하세요.",
    detail: false,
  },
  {
    label: "편집: 본문을 읽지 못함",
    status: 400,
    code: "INVALID_PAYLOAD",
    message: "json: cannot unmarshal number into Go struct field",
    expected:
      "보낸 내용을 서버가 읽지 못했습니다. 화면을 새로 고친 뒤 다시 시도하세요.",
    detail: true,
  },
  {
    label: "편집: 역할 값이 올바르지 않음",
    status: 400,
    code: "INVALID_ROLE",
    message: "role is invalid",
    expected:
      "권한 값이 올바르지 않습니다. 목록이 오래되었을 수 있으니 새로 고친 뒤 권한을 다시 고르세요.",
    detail: false,
  },
  {
    label: "편집: 자기 계정 중지",
    status: 400,
    code: "SELF_DISABLE",
    message: "you cannot disable your own account",
    expected: "자기 계정은 중지할 수 없습니다. 다른 관리자에게 요청하세요.",
    detail: false,
  },
  {
    label: "편집: 내 권한보다 높은 역할 부여",
    status: 403,
    code: "ROLE_ABOVE_CALLER",
    message: "you cannot grant more authority than your own",
    expected:
      "내 권한보다 높은 권한은 줄 수 없습니다. 권한을 내 계정의 권한 이하로 고르세요.",
    detail: false,
  },
  {
    label: "편집: 내 권한보다 높은 계정 관리",
    status: 403,
    code: "ROLE_ABOVE_CALLER",
    message:
      "you cannot administer an account with more authority than your own",
    expected:
      "내 권한보다 높은 계정은 편집할 수 없습니다. 목록이 오래되었을 수 있으니 새로 고친 뒤 다시 시도하세요.",
    detail: false,
  },
  {
    label: "편집: 사용자 없음",
    status: 404,
    code: "USER_NOT_FOUND",
    message: "user not found",
    expected:
      "이 사용자가 이미 삭제되었습니다. 목록을 새로 고친 뒤 확인하세요.",
    detail: false,
  },
  {
    label: "편집: 조회 실패",
    status: 500,
    code: "QUERY_FAILED",
    message: "conn closed",
    expected:
      "사용자 정보를 읽지 못했습니다. 잠시 후 다시 시도하고, 반복되면 아래 메시지를 관리자에게 전달하세요.",
    detail: true,
  },
  {
    label: "편집: 자기 역할 변경",
    status: 400,
    code: "SELF_ROLE",
    message: "you cannot change your own role",
    expected:
      "자기 계정의 권한은 바꿀 수 없습니다. 다른 관리자에게 요청하세요.",
    detail: false,
  },
  {
    label: "편집: 자기 비밀번호",
    status: 400,
    code: "SELF_PASSWORD",
    message:
      "change your own password from your profile, where the current one is asked for",
    expected:
      "자기 비밀번호는 내 프로필에서 현재 비밀번호를 확인한 뒤 바꿔야 합니다.",
    detail: false,
  },
  {
    label: "편집: 비밀번호가 규칙에 맞지 않음",
    status: 400,
    code: "WEAK_PASSWORD",
    message: "password must be at least 12 characters",
    expected: `비밀번호가 규칙에 맞지 않습니다. ${RULE}`,
    detail: false,
  },
  {
    label: "편집: 저장 실패",
    status: 500,
    code: "USER_UPDATE_FAILED",
    message: "conn closed",
    expected:
      "변경 내용을 저장하지 못했습니다. 잠시 후 다시 시도하고, 반복되면 아래 메시지를 관리자에게 전달하세요.",
    detail: true,
  },
  // client.ts:81-85 가 코드 없는 응답에 채우는 것.
  {
    label: "응답에 코드가 없음",
    status: 502,
    code: "REQUEST_FAILED",
    message: "HTTP 502",
    expected:
      "서버가 요청을 처리하지 못했습니다. 네트워크를 확인하고 다시 시도하세요.",
    detail: true,
  },
];

test("서버가 내는 코드마다 정해진 한국어 안내가 나온다", () => {
  for (const entry of SERVER_ERRORS) {
    const described = describeUserError(
      apiError(entry.status, entry.code, entry.message),
    );
    assert.equal(
      described.message,
      entry.expected,
      `${entry.label} (${entry.code})`,
    );
  }
});

test("서버 원문은 그것이 유일한 단서인 코드에서만 detail 로 남는다", () => {
  for (const entry of SERVER_ERRORS) {
    const described = describeUserError(
      apiError(entry.status, entry.code, entry.message),
    );
    assert.equal(
      described.detail,
      entry.detail ? entry.message : undefined,
      `${entry.label} (${entry.code}): detail 이 기대와 다르다`,
    );
  }
});

test("한 코드에 두 원인이 실려 오는 경우를 서로 다르게 안내한다", () => {
  const messageFor = (status, code, message) =>
    describeUserError(apiError(status, code, message)).message;

  // INVALID_USER 는 역할/비밀번호 두 원인을 한 코드로 보낸다. 역할 쪽 서버 문장에도
  // "password" 라는 낱말이 들어 있으므로, 단순 포함 검사로는 갈리지 않는다.
  assert.notEqual(
    messageFor(400, "INVALID_USER", "password must be at least 12 characters"),
    messageFor(
      400,
      "INVALID_USER",
      "valid role and password of at least 12 characters are required",
    ),
    "INVALID_USER 의 비밀번호 원인과 역할 원인이 같은 문구를 쓴다",
  );

  // ROLE_ABOVE_CALLER 는 '역할 부여'(admin.go:959)와 '상위 계정 관리'(:976) 두
  // 경우에 서로 다른 서버 문장으로 온다 — 고칠 곳이 다르므로 안내도 달라야 한다.
  assert.notEqual(
    messageFor(
      403,
      "ROLE_ABOVE_CALLER",
      "you cannot grant more authority than your own",
    ),
    messageFor(
      403,
      "ROLE_ABOVE_CALLER",
      "you cannot administer an account with more authority than your own",
    ),
    "역할 부여 거절과 상위 계정 편집 거절이 같은 문구를 쓴다",
  );

  // USER_CREATE_FAILED 는 409(중복)와 500(해시 실패) 둘 다 쓴다. 코드만 보고
  // '이미 등록된 이메일' 이라 단정하면 해시 실패를 거짓으로 설명한다.
  assert.doesNotMatch(
    messageFor(
      500,
      "USER_CREATE_FAILED",
      "bcrypt: password length exceeds 72 bytes",
    ),
    /이미 등록된/,
    "해시 실패를 이메일 중복이라고 거짓으로 설명한다",
  );
  // INSERT 가 중복이 아닌 이유로 실패해 409 로 와도 중복이라 단정하지 않는다.
  assert.doesNotMatch(
    messageFor(409, "USER_CREATE_FAILED", "conn closed"),
    /이미 등록된/,
    "409 라는 이유만으로 이메일 중복이라 단정한다",
  );
});

test("이메일 중복 pgx 원문의 스키마 내부가 화면으로 새지 않는다", () => {
  const described = describeUserError(
    apiError(409, "USER_CREATE_FAILED", DUPLICATE_EMAIL_PGX),
  );
  // message 와 detail 을 합쳐 검사한다 — detail 로 옮겨 담아도 Alert 에 그려지므로
  // 새는 것은 똑같다.
  const shown = `${described.message} ${described.detail ?? ""}`;
  for (const leak of [
    "users_email_key",
    "SQLSTATE",
    "23505",
    "duplicate key",
    "unique constraint",
    "ERROR:",
  ]) {
    assert.ok(
      !shown.includes(leak),
      `pgx 원문의 "${leak}" 가 화면 문구에 섞였다: ${shown}`,
    );
  }
});

test("모르는 코드는 서버 메시지를 그대로 돌려준다", () => {
  // 서버에 코드가 새로 생겨도 안내가 사라지지 않아야 한다.
  assert.equal(
    describeUserError(apiError(400, "SOMETHING_NEW", "a brand new refusal"))
      .message,
    "a brand new refusal",
  );
});

test("메시지도 코드도 없는 실패에는 빈 Alert 대신 안내가 남는다", () => {
  for (const value of [
    null,
    undefined,
    {},
    new Error(""),
    "문자열",
    apiError(500, "SOMETHING_NEW", ""),
  ]) {
    const described = describeUserError(value);
    assert.ok(
      described.message.length > 0,
      `${String(value)}: 안내가 비어 있다`,
    );
  }
});
