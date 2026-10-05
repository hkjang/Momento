import assert from "node:assert/strict";
import test from "node:test";
import {
  describeEventDefinitionError,
  describeNetworkError,
  describeSiteError,
  describeUserError,
} from "../src/pages/adminErrors.ts";

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

// ---------------------------------------------------------------------------
// 사이트 추가(createSite, admin.go:221~)·사이트 설정(updateSite, :314~)
// ---------------------------------------------------------------------------

// 두 500 이 Alert 로 싣고 오는 pgx 원문. 위쪽은 workspaces 조회가 비는 경우
// (admin.go:264 의 QueryRow 가 pgx.ErrNoRows 를 돌려준다), 아래쪽은 DB 쪽 장애다.
const NO_WORKSPACE_PGX = "no rows in result set";
const BROKEN_DB_PGX =
  'ERROR: insert or update on table "sites" violates foreign key constraint "sites_workspace_id_fkey" (SQLSTATE 23503)';

// Alert 본문에 섞이면 안 되는 Postgres/pgx 표지. detail 쪽에는 남아야 한다 —
// 관리자에게 전달할 유일한 단서다.
const PG_MARKERS = ["SQLSTATE", "violates", "relation", "no rows in result set"];

// 서버가 두 사이트 핸들러에서 실제로 내는 코드 전부.
const SITE_ERRORS = [
  // --- createSite ---
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
    label: "생성: 이름이 공백뿐",
    status: 400,
    code: "INVALID_NAME",
    message: "site name is required",
    expected:
      "사이트 이름을 입력하세요. 공백만 적으면 이름이 비어 있는 것으로 처리됩니다.",
    detail: false,
  },
  {
    label: "생성: 시간대 이름이 IANA 가 아님",
    status: 400,
    code: "INVALID_TIMEZONE",
    message: "timezone must be a valid IANA timezone",
    expected:
      "시간대 이름이 올바르지 않습니다. 「IANA 시간대」 칸에 Asia/Seoul 처럼 지역/도시 형태로 적으세요.",
    detail: false,
  },
  {
    label: "생성: 참여 기준 시간이 범위를 벗어남",
    status: 400,
    code: "INVALID_ENGAGEMENT_THRESHOLD",
    message: "engagement threshold must be between 1 and 300 seconds",
    expected: "「참여 기준 시간(초)」 은 1초에서 300초 사이로 적으세요.",
    detail: false,
  },
  {
    label: "생성: workspaces 가 비어 있음 (500)",
    status: 500,
    code: "SITE_CREATE_FAILED",
    message: NO_WORKSPACE_PGX,
    expected:
      "사이트를 만들지 못했습니다. 잠시 후 다시 시도하고, 반복되면 아래 메시지를 관리자에게 전달하세요.",
    detail: true,
  },
  {
    label: "생성: DB 쪽 실패 (500)",
    status: 500,
    code: "SITE_CREATE_FAILED",
    message: BROKEN_DB_PGX,
    expected:
      "사이트를 만들지 못했습니다. 잠시 후 다시 시도하고, 반복되면 아래 메시지를 관리자에게 전달하세요.",
    detail: true,
  },
  // --- updateSite ---
  {
    label: "편집: 사이트 id 가 올바르지 않음",
    status: 400,
    code: "INVALID_ID",
    message: "invalid site id",
    expected:
      "이 사이트를 가리킬 수 없습니다. 목록을 새로 고친 뒤 다시 시도하세요.",
    detail: false,
  },
  {
    label: "편집: 사이트 없음",
    status: 404,
    code: "NOT_FOUND",
    message: "site not found",
    expected:
      "이 사이트가 이미 삭제되었습니다. 목록을 새로 고친 뒤 확인하세요.",
    detail: false,
  },
  {
    label: "편집: 본문을 읽지 못함",
    status: 400,
    code: "INVALID_PAYLOAD",
    message: "json: cannot unmarshal string into Go struct field",
    expected:
      "보낸 내용을 서버가 읽지 못했습니다. 화면을 새로 고친 뒤 다시 시도하세요.",
    detail: true,
  },
  {
    label: "편집: 세션 만료가 범위를 벗어남",
    status: 400,
    code: "INVALID_TIMEOUT",
    message: "session timeout must be between 1 and 1440 minutes",
    expected: "「세션 만료(분)」 은 1분에서 1440분 사이로 적으세요.",
    detail: false,
  },
  {
    label: "편집: 시간대 이름이 IANA 가 아님",
    status: 400,
    code: "INVALID_TIMEZONE",
    message: "timezone must be a valid IANA timezone",
    expected:
      "시간대 이름이 올바르지 않습니다. 「IANA 시간대」 칸에 Asia/Seoul 처럼 지역/도시 형태로 적으세요.",
    detail: false,
  },
  {
    label: "편집: 참여 기준 시간이 범위를 벗어남",
    status: 400,
    code: "INVALID_ENGAGEMENT_THRESHOLD",
    message: "engagement threshold must be between 1 and 300 seconds",
    expected: "「참여 기준 시간(초)」 은 1초에서 300초 사이로 적으세요.",
    detail: false,
  },
  {
    label: "편집: 저장 실패 (500)",
    status: 500,
    code: "SITE_UPDATE_FAILED",
    message: BROKEN_DB_PGX,
    expected:
      "사이트 설정을 저장하지 못했습니다. 잠시 후 다시 시도하고, 반복되면 아래 메시지를 관리자에게 전달하세요.",
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

test("사이트 쪽 서버 코드마다 정해진 한국어 안내가 나온다", () => {
  for (const entry of SITE_ERRORS) {
    const described = describeSiteError(
      apiError(entry.status, entry.code, entry.message),
    );
    assert.equal(
      described.message,
      entry.expected,
      `${entry.label} (${entry.code})`,
    );
  }
});

// 결함 자체를 짚는 단언: 지금 Alert 은 error.message 를 그대로 띄우므로, 서버
// 영문이 본문에 그대로 올라오는 코드가 하나라도 있으면 여기서 떨어진다.
test("아는 코드의 안내는 서버의 영문 문장이 아니다", () => {
  for (const entry of SITE_ERRORS) {
    const described = describeSiteError(
      apiError(entry.status, entry.code, entry.message),
    );
    assert.notEqual(
      described.message,
      entry.message,
      `${entry.label} (${entry.code}): 서버 영문이 그대로 본문에 올라왔다`,
    );
  }
});

test("사이트 쪽 서버 원문은 그것이 유일한 단서인 코드에서만 detail 로 남는다", () => {
  for (const entry of SITE_ERRORS) {
    const described = describeSiteError(
      apiError(entry.status, entry.code, entry.message),
    );
    assert.equal(
      described.detail,
      entry.detail ? entry.message : undefined,
      `${entry.label} (${entry.code}): detail 이 기대와 다르다`,
    );
  }
});

test("500 의 pgx 원문은 Alert 본문이 아니라 detail 로만 남는다", () => {
  for (const entry of [
    { code: "SITE_CREATE_FAILED", message: NO_WORKSPACE_PGX },
    { code: "SITE_CREATE_FAILED", message: BROKEN_DB_PGX },
    { code: "SITE_UPDATE_FAILED", message: BROKEN_DB_PGX },
  ]) {
    const described = describeSiteError(apiError(500, entry.code, entry.message));
    for (const marker of PG_MARKERS) {
      assert.ok(
        !described.message.includes(marker),
        `${entry.code}: Postgres 표지 "${marker}" 가 Alert 본문에 섞였다: ${described.message}`,
      );
    }
    // 지우지는 않는다 — 관리자에게 전달할 유일한 단서다.
    assert.equal(
      described.detail,
      entry.message,
      `${entry.code}: 서버 원문이 detail 에서 사라졌다`,
    );
  }
});

// sites.name 에는 UNIQUE 가 없다(001_initial.sql:34-49, UNIQUE 는 site_key 뿐).
// 500 을 '이미 있는 사이트 이름' 으로 설명하면 거짓말이 된다.
test("사이트 생성 500 을 이름 중복이라고 설명하지 않는다", () => {
  for (const message of [NO_WORKSPACE_PGX, BROKEN_DB_PGX]) {
    assert.doesNotMatch(
      describeSiteError(apiError(500, "SITE_CREATE_FAILED", message)).message,
      /이미 (있는|등록된|존재)/,
      "SITE_CREATE_FAILED 를 중복이라고 단정한다",
    );
  }
});

test("사이트 쪽도 모르는 코드는 서버 메시지를 그대로 돌려준다", () => {
  assert.equal(
    describeSiteError(apiError(400, "SITE_SOMETHING_NEW", "a brand new refusal"))
      .message,
    "a brand new refusal",
  );
});

test("사이트 쪽도 메시지·코드가 없는 실패에 빈 Alert 을 남기지 않는다", () => {
  for (const value of [
    null,
    undefined,
    {},
    new Error(""),
    "문자열",
    apiError(500, "SITE_SOMETHING_NEW", ""),
  ]) {
    const described = describeSiteError(value);
    assert.ok(
      described.message.length > 0,
      `${String(value)}: 안내가 비어 있다`,
    );
  }
});

// ---------------------------------------------------------------------------
// 망 구분 추가(createNetwork, admin.go:800~)
// ---------------------------------------------------------------------------

// createNetwork 의 500 이 Alert 로 싣고 오는 pgx 원문. 아래쪽은 network_ranges.cidr
// 가 Postgres `cidr` 타입(001_initial.sql:64-70)이라 넷마스크 오른쪽에 비트가 선
// 값을 거절할 때 나올 꼴이다 — Go 의 net.ParseCIDR 은 그런 값을 통과시키므로
// INVALID_CIDR 400 이 아니라 이 500 으로 온다. **Postgres 로 재현하지 않았다.**
// 재현 여부와 무관하게 서버 원문은 믿을 수 없는 문자열이고, 이 단언이 묶는 것은
// "그런 문자열이 오면 본문으로 새지 않는다" 뿐이다.
const BAD_CIDR_PGX =
  'ERROR: invalid cidr value: "10.0.0.5/24" (SQLSTATE 22P02)';
const NETWORK_DB_PGX =
  'ERROR: relation "network_ranges" does not exist (SQLSTATE 42P01)';

// 사이트 쪽 PG_MARKERS 에 cidr 타입이 내는 표지를 더한 것.
const NETWORK_PG_MARKERS = [...PG_MARKERS, "invalid cidr value"];

// 서버가 createNetwork 에서 실제로 내는 코드 전부 — 셋뿐이다(admin.go:809·813·823).
const NETWORK_ERRORS = [
  {
    label: "추가: 본문을 읽지 못함",
    status: 400,
    code: "INVALID_PAYLOAD",
    message: "unexpected EOF",
    expected:
      "보낸 내용을 서버가 읽지 못했습니다. 화면을 새로 고친 뒤 다시 시도하세요.",
    detail: true,
  },
  {
    label: "추가: CIDR 표기가 틀림",
    status: 400,
    code: "INVALID_CIDR",
    message: "CIDR is invalid",
    expected:
      "CIDR 표기가 올바르지 않습니다. 「CIDR」 칸에 10.20.30.0/24 처럼 주소 뒤에 「/」 와 비트 수(IPv4 는 0~32, IPv6 는 0~128)를 붙여 적으세요.",
    detail: false,
  },
  {
    label: "추가: DB 쪽 실패 (500)",
    status: 500,
    code: "NETWORK_CREATE_FAILED",
    message: NETWORK_DB_PGX,
    expected:
      "망 구분을 추가하지 못했습니다. 잠시 후 다시 시도하고, 반복되면 아래 메시지를 관리자에게 전달하세요.",
    detail: true,
  },
  {
    label: "추가: DB 가 CIDR 값을 거절 (500)",
    status: 500,
    code: "NETWORK_CREATE_FAILED",
    message: BAD_CIDR_PGX,
    expected:
      "망 구분을 추가하지 못했습니다. 잠시 후 다시 시도하고, 반복되면 아래 메시지를 관리자에게 전달하세요.",
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

test("망 구분 쪽 서버 코드마다 정해진 한국어 안내가 나온다", () => {
  for (const entry of NETWORK_ERRORS) {
    const described = describeNetworkError(
      apiError(entry.status, entry.code, entry.message),
    );
    assert.equal(
      described.message,
      entry.expected,
      `${entry.label} (${entry.code})`,
    );
  }
});

// 결함 자체를 짚는 단언: 지금 Alert(AdminPage.tsx:3244-3246) 은 error.message 를
// 그대로 띄우므로, 항등 스텁에서는 아는 코드마다 여기서 떨어진다.
test("망 구분 쪽도 아는 코드의 안내는 서버의 영문 문장이 아니다", () => {
  for (const entry of NETWORK_ERRORS) {
    const described = describeNetworkError(
      apiError(entry.status, entry.code, entry.message),
    );
    assert.notEqual(
      described.message,
      entry.message,
      `${entry.label} (${entry.code}): 서버 영문이 그대로 본문에 올라왔다`,
    );
  }
});

test("망 구분 쪽 서버 원문은 그것이 유일한 단서인 코드에서만 detail 로 남는다", () => {
  for (const entry of NETWORK_ERRORS) {
    const described = describeNetworkError(
      apiError(entry.status, entry.code, entry.message),
    );
    assert.equal(
      described.detail,
      entry.detail ? entry.message : undefined,
      `${entry.label} (${entry.code}): detail 이 기대와 다르다`,
    );
  }
});

test("망 구분 500 의 pgx 원문은 Alert 본문이 아니라 detail 로만 남는다", () => {
  for (const message of [NETWORK_DB_PGX, BAD_CIDR_PGX]) {
    const described = describeNetworkError(
      apiError(500, "NETWORK_CREATE_FAILED", message),
    );
    for (const marker of NETWORK_PG_MARKERS) {
      assert.ok(
        !described.message.includes(marker),
        `NETWORK_CREATE_FAILED: Postgres 표지 "${marker}" 가 Alert 본문에 섞였다: ${described.message}`,
      );
    }
    // 지우지는 않는다 — 관리자에게 전달할 유일한 단서다.
    assert.equal(
      described.detail,
      message,
      "NETWORK_CREATE_FAILED: 서버 원문이 detail 에서 사라졌다",
    );
  }
});

// network_ranges 에는 UNIQUE 가 없다(001_initial.sql:64-70 — id PRIMARY KEY 뿐).
// 500 을 '이미 등록된 망/CIDR' 로 설명하면 거짓말이 된다.
test("망 구분 추가 500 을 중복이라고 설명하지 않는다", () => {
  for (const message of [NETWORK_DB_PGX, BAD_CIDR_PGX]) {
    assert.doesNotMatch(
      describeNetworkError(apiError(500, "NETWORK_CREATE_FAILED", message))
        .message,
      /이미 (있는|등록된|존재)/,
      "NETWORK_CREATE_FAILED 를 중복이라고 단정한다",
    );
  }
});

// 기준 1: CIDR 칸은 손으로 적는 자유 입력이라 가장 흔한 실패가 이것이다. 안내가
// 다음에 할 행동(표기 형태)을 말해 주어야 한다.
test("CIDR 안내는 다음에 적을 표기 형태를 보여 준다", () => {
  const described = describeNetworkError(
    apiError(400, "INVALID_CIDR", "CIDR is invalid"),
  );
  // 화면 placeholder(AdminPage.tsx:3238) 와 같은 예시를 든다.
  assert.match(described.message, /10\.20\.30\.0\/24/);
  assert.match(described.message, /CIDR/);
});

test("망 구분 쪽도 모르는 코드는 서버 메시지를 그대로 돌려준다", () => {
  assert.equal(
    describeNetworkError(
      apiError(400, "NETWORK_SOMETHING_NEW", "a brand new refusal"),
    ).message,
    "a brand new refusal",
  );
});

test("망 구분 쪽도 메시지·코드가 없는 실패에 빈 Alert 을 남기지 않는다", () => {
  for (const value of [
    null,
    undefined,
    {},
    new Error(""),
    "문자열",
    apiError(500, "NETWORK_SOMETHING_NEW", ""),
  ]) {
    const described = describeNetworkError(value);
    assert.ok(
      described.message.length > 0,
      `${String(value)}: 안내가 비어 있다`,
    );
  }
});

// ---------------------------------------------------------------------------
// Event Schema 저장(upsertEventDefinition, admin.go:1395~)
// ---------------------------------------------------------------------------

// 이 화면의 실패는 둘로 갈린다. 아래 SCHEMA_TEXT 처럼 「JSON Schema」 칸의 JSON 이
// 깨지면 요청은 서버에 가지도 않는다 — AdminPage.tsx 의 mutationFn 안에서
// JSON.parse 가 먼저 던지고, react-query 가 그 동기 throw 를 save.error 에 담는다
// (실제 브라우저에서 확인함). 그 에러에는 code 가 없어서, 코드만 보는 안내는
// default 로 떨어뜨려 V8 의 영문 문장을 그대로 내보낸다.
const BROKEN_SCHEMA_TEXT = '{"properties": }';

// **손으로 만든 가짜 객체를 쓰지 않는다.** 엔진이 실제로 던지는 것을 받아 둔다 —
// 문장도 프로퍼티도 엔진이 정하는 것이고, 안내가 막아야 하는 것은 바로 그 값이다.
function realParseError(text) {
  try {
    JSON.parse(text);
  } catch (e) {
    return e;
  }
  throw new Error(`${text} 는 깨진 JSON 이 아니다 — 테스트 전제가 틀렸다`);
}

// V8 이 내는 표지. 이 중 하나라도 안내 본문에 있으면 영문이 그대로 샌 것이다.
const ENGINE_MARKERS = [
  "Unexpected token",
  "is not valid JSON",
  "JSON.parse",
  "position",
];

// upsertEventDefinition 의 500 이 싣고 오는 꼴. 이 500 은 서로 다른 여섯 자리가
// 함께 쓴다(Begin·INSERT·version 조회·두 Exec·Commit). 특히 INSERT 는
// `SELECT id,... FROM sites WHERE site_key=$1` 이라 site_key 가 맞지 않으면 0행
// 삽입이 되어 pgx.ErrNoRows 로 같은 500 이 된다. **Postgres 로 재현하지 않았다** —
// 이 단언이 묶는 것은 "그런 문자열이 오면 본문으로 새지 않는다" 뿐이다.
const DEFINITION_FK_PGX =
  'ERROR: insert or update on table "event_contract_versions" violates foreign key constraint "event_contract_versions_site_id_fkey" (SQLSTATE 23503)';
const DEFINITION_NO_ROWS_PGX = "no rows in result set";

// 서버가 이 핸들러에서 실제로 내는 코드 전부 — 셋뿐이다(admin.go:1409·1415 및
// DEFINITION_SAVE_FAILED 를 쓰는 여섯 자리) + client.ts 가 채우는 REQUEST_FAILED.
const DEFINITION_ERRORS = [
  {
    label: "저장: 본문을 읽지 못함",
    status: 400,
    code: "INVALID_PAYLOAD",
    message: "unexpected EOF",
    expected:
      "보낸 내용을 서버가 읽지 못했습니다. 화면을 새로 고친 뒤 다시 시도하세요.",
    detail: true,
  },
  {
    label: "저장: 정책 값이 셋 중 하나가 아님",
    status: 400,
    code: "INVALID_MODE",
    message: "validation mode must be allow, warn, or reject",
    expected:
      "「정책」 값이 올바르지 않습니다. 화면을 새로 고친 뒤 allow·warn·reject 중에서 다시 고르세요.",
    detail: false,
  },
  {
    label: "저장: DB 쪽 실패 (500)",
    status: 500,
    code: "DEFINITION_SAVE_FAILED",
    message: DEFINITION_FK_PGX,
    expected:
      "이벤트 규격을 저장하지 못했습니다. 잠시 후 다시 시도하고, 반복되면 아래 메시지를 관리자에게 전달하세요.",
    detail: true,
  },
  {
    label: "저장: 삽입이 0행이 됨 (500)",
    status: 500,
    code: "DEFINITION_SAVE_FAILED",
    message: DEFINITION_NO_ROWS_PGX,
    expected:
      "이벤트 규격을 저장하지 못했습니다. 잠시 후 다시 시도하고, 반복되면 아래 메시지를 관리자에게 전달하세요.",
    detail: true,
  },
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

// 기준 3(a): 깨진 JSON 에서 나오는 **진짜** SyntaxError 의 안내에 엔진 원문이 없다.
test("깨진 JSON 의 안내에 V8 의 영문 문장이 섞이지 않는다", () => {
  const described = describeEventDefinitionError(
    realParseError(BROKEN_SCHEMA_TEXT),
  );
  for (const marker of ENGINE_MARKERS) {
    assert.ok(
      !described.message.includes(marker),
      `엔진 원문 표지 "${marker}" 가 안내 본문에 섞였다: ${described.message}`,
    );
  }
  assert.match(described.message, /[가-힣]/, "안내가 한국어가 아니다");
});

// 기준 1: 어느 칸을 고쳐야 하는지 말한다.
test("깨진 JSON 의 안내는 고쳐야 할 칸을 가리킨다", () => {
  const described = describeEventDefinitionError(
    realParseError(BROKEN_SCHEMA_TEXT),
  );
  assert.match(described.message, /JSON Schema/);
});

// 기준 1: 원문은 지우지 않는다 — 어디가 깨졌는지 아는 유일한 단서다.
test("깨진 JSON 의 엔진 원문은 본문이 아니라 detail 로만 남는다", () => {
  const error = realParseError(BROKEN_SCHEMA_TEXT);
  const described = describeEventDefinitionError(error);
  assert.equal(described.detail, error.message);
});

// 엔진이 문장을 바꾸더라도 판정이 흔들리지 않아야 한다 — 여러 꼴의 깨진 JSON 에서
// 모두 같은 안내로 가는지 본다(판정이 한 문장의 생김새에 매달려 있지 않다는 증거).
test("여러 꼴의 깨진 JSON 이 모두 같은 안내로 간다", () => {
  const first = describeEventDefinitionError(
    realParseError(BROKEN_SCHEMA_TEXT),
  ).message;
  for (const text of ["{", "", "{'a':1}", '{"a":1,}', "not json at all"]) {
    assert.equal(
      describeEventDefinitionError(realParseError(text)).message,
      first,
      `${JSON.stringify(text)}: 다른 안내로 갔다`,
    );
  }
});

// 기준 3(b): 아는 코드 네 개의 안내가 서버 영문이 아니다.
test("Event Schema 쪽 서버 코드마다 정해진 한국어 안내가 나온다", () => {
  for (const entry of DEFINITION_ERRORS) {
    const described = describeEventDefinitionError(
      apiError(entry.status, entry.code, entry.message),
    );
    assert.equal(
      described.message,
      entry.expected,
      `${entry.label} (${entry.code})`,
    );
  }
});

// 결함 자체를 짚는 단언: 지금 Alert(AdminPage.tsx:3775) 은 error.message 를 그대로
// 띄우므로, 항등 스텁에서는 아는 코드마다 여기서 떨어진다.
test("Event Schema 쪽도 아는 코드의 안내는 서버의 영문 문장이 아니다", () => {
  for (const entry of DEFINITION_ERRORS) {
    const described = describeEventDefinitionError(
      apiError(entry.status, entry.code, entry.message),
    );
    assert.notEqual(
      described.message,
      entry.message,
      `${entry.label} (${entry.code}): 서버 영문이 그대로 본문에 올라왔다`,
    );
  }
});

test("Event Schema 쪽 서버 원문은 그것이 유일한 단서인 코드에서만 detail 로 남는다", () => {
  for (const entry of DEFINITION_ERRORS) {
    const described = describeEventDefinitionError(
      apiError(entry.status, entry.code, entry.message),
    );
    assert.equal(
      described.detail,
      entry.detail ? entry.message : undefined,
      `${entry.label} (${entry.code}): detail 이 기대와 다르다`,
    );
  }
});

// 기준 3(d): 500 의 pgx 원문이 본문이 아니라 detail 로만 남는다.
test("Event Schema 500 의 pgx 원문은 Alert 본문이 아니라 detail 로만 남는다", () => {
  for (const message of [DEFINITION_FK_PGX, DEFINITION_NO_ROWS_PGX]) {
    const described = describeEventDefinitionError(
      apiError(500, "DEFINITION_SAVE_FAILED", message),
    );
    for (const marker of PG_MARKERS) {
      assert.ok(
        !described.message.includes(marker),
        `DEFINITION_SAVE_FAILED: Postgres 표지 "${marker}" 가 Alert 본문에 섞였다: ${described.message}`,
      );
    }
    assert.equal(
      described.detail,
      message,
      "DEFINITION_SAVE_FAILED: 서버 원문이 detail 에서 사라졌다",
    );
  }
});

// 이름 중복은 `ON CONFLICT(site_id,name) DO UPDATE` 가 흡수하므로(admin.go:1421)
// 이 500 을 '이미 등록된 이벤트' 라고 쓰면 거짓말이 된다. 원인이 미확정이라는 것이
// 이 단언이 묶는 것이다.
test("Event Schema 저장 500 을 중복이라고 설명하지 않는다", () => {
  for (const message of [DEFINITION_FK_PGX, DEFINITION_NO_ROWS_PGX]) {
    assert.doesNotMatch(
      describeEventDefinitionError(
        apiError(500, "DEFINITION_SAVE_FAILED", message),
      ).message,
      /이미 (있는|등록된|존재)/,
      "DEFINITION_SAVE_FAILED 를 중복이라고 단정한다",
    );
  }
});

// 기준 3(c): 모르는 코드는 서버 메시지 그대로 — 세 describe\* 와 같은 계약.
test("Event Schema 쪽도 모르는 코드는 서버 메시지를 그대로 돌려준다", () => {
  assert.equal(
    describeEventDefinitionError(
      apiError(400, "DEFINITION_SOMETHING_NEW", "a brand new refusal"),
    ).message,
    "a brand new refusal",
  );
});

// 코드가 있는 실패는 JSON 분기로 가로채이지 않아야 한다 — 서버 문장에 'JSON' 이
// 들어 있어도 그것은 서버의 거절이고, 「JSON Schema」 칸 얘기가 아니다.
test("코드가 붙은 실패는 JSON 구문 안내로 가로채이지 않는다", () => {
  const described = describeEventDefinitionError(
    apiError(400, "INVALID_PAYLOAD", "invalid JSON body: unexpected EOF"),
  );
  assert.equal(
    described.message,
    "보낸 내용을 서버가 읽지 못했습니다. 화면을 새로 고친 뒤 다시 시도하세요.",
  );
});

test("Event Schema 쪽도 메시지·코드가 없는 실패에 빈 Alert 을 남기지 않는다", () => {
  for (const value of [
    null,
    undefined,
    {},
    new Error(""),
    "문자열",
    apiError(500, "DEFINITION_SOMETHING_NEW", ""),
  ]) {
    const described = describeEventDefinitionError(value);
    assert.ok(
      described.message.length > 0,
      `${String(value)}: 안내가 비어 있다`,
    );
  }
});
