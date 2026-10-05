import assert from "node:assert/strict";
import test from "node:test";
import { CIDR_RULE, judgeCIDR } from "../src/pages/cidrRule.ts";

// 서버가 정본이고 화면은 거울이다. createNetwork(internal/httpapi/admin.go:812)
// 는 `net.ParseCIDR` 하나로 CIDR 을 받아들이거나 400 INVALID_CIDR 로 거절하므로,
// 화면이 막아도 되는 값은 ParseCIDR 이 **확실히 거절하는** 값뿐이다.
//
// 아래 표는 추측이 아니라 Go 1.26 의 `net.ParseCIDR` 에 같은 문자열을 넣어 받은
// 결과를 그대로 옮긴 것이다(2026-10-06 측정). 눈여겨볼 두 줄:
//   - "10.20.30.0/024" 와 "10.20.30.0/00000000024" 는 **통과한다** — 비트 수의
//     선행 0 은 Go 의 dtoi 가 그냥 십진수로 읽는다. 숫자 판정을 정규식으로만
//     하고 값으로 비교하지 않으면 여기서 화면이 서버보다 좁아진다.
//   - "010.20.30.0/24" 는 **거절된다**(Go 1.17 이후 IPv4 본문의 선행 0 금지).
//     그래도 화면은 이것을 막지 않는다 — 막아도 되는 값이지만, 표기 판정을
//     넓히다가 통과시켜야 할 값을 막는 쪽이 더 비싸다. 아래 SUBSET 단언이
//     "막지 않는 것" 은 자유롭게 허용하므로 이 선택과 공존한다.
const PARSE_CIDR = [
  ["10.20.30", false],
  ["10.20.30.0/33", false],
  ["10.20.30.0/abc", false],
  ["1.2.3.4.5/24", false],
  ["10.20.30.1/24", true],
  ["10.20.30.0/24", true],
  ["0.0.0.0/0", true],
  ["2001:db8::/32", true],
  ["10.20.30.0/024", true],
  ["010.20.30.0/24", false],
  ["10.20.30.0/", false],
  ["10.20.30.0/-1", false],
  ["10.20.30.0/+24", false],
  ["10.20.30.0/24/8", false],
  ["10.20.30.0/24 ", false],
  [" 10.20.30.0/24", false],
  ["/24", false],
  ["10.20.30.256/24", false],
  ["zzz/32", false],
  ["zzz:/32", false],
  ["::ffff:1.2.3.4/96", true],
  ["2001:db8::/129", false],
  ["2001:db8::/abc", false],
  ["10.20.30.0/0032", true],
  ["10.20.30.0/٢٤", false],
  ["10.20.30.0/2_4", false],
  ["1.2.3.4/32", true],
  ["10.20.30.0/00000000024", true],
];

const blocks = (text) => Boolean(judgeCIDR(text).blocking);

// 기준 4(a). 이 단언이 이 모듈의 계약 전부다 — 화면이 서버보다 좁아지면 사용자는
// 서버가 받아 줄 값을 화면에서 넣을 수 없게 된다. 되돌릴 방법이 없는 종류의
// 결함이라, 개별 문구 단언보다 이것이 먼저다.
test("차단하는 값은 net.ParseCIDR 이 거절하는 값의 부분집합이다", () => {
  for (const [text, parses] of PARSE_CIDR) {
    if (parses) {
      assert.equal(
        blocks(text),
        false,
        `${JSON.stringify(text)} 는 서버가 받아들이는데 화면이 막았다`,
      );
    }
  }
});

// 기준 1. 왕복을 없애려고 만든 모듈이므로, 왕복해야만 알 수 있던 네 가지는
// 실제로 막혀야 한다. 위 부분집합 단언만 있으면 "아무것도 막지 않는" 구현이
// 통과하므로 두 단언이 함께 있어야 뜻이 생긴다.
test("왕복해야 알 수 있던 표기 오류를 보내기 전에 막는다", () => {
  for (const text of [
    "10.20.30",
    "10.20.30.0/33",
    "10.20.30.0/abc",
    "1.2.3.4.5/24",
  ]) {
    assert.equal(blocks(text), true, `${JSON.stringify(text)} 를 막지 않았다`);
  }
});

test("막을 때는 왜 막혔는지 한국어로 읽을 거리를 함께 준다", () => {
  for (const text of [
    "10.20.30",
    "10.20.30.0/33",
    "10.20.30.0/abc",
    "1.2.3.4.5/24",
  ]) {
    const { blocking } = judgeCIDR(text);
    assert.match(
      blocking ?? "",
      /[가-힣]/,
      `${JSON.stringify(text)}: 안내가 한국어가 아니다`,
    );
  }
});

// 기준 2. net.ParseCIDR 은 호스트 비트가 켜진 값을 통과시키므로 화면도
// 통과시킨다. Postgres `cidr` 컬럼이 이것을 거절하는지는 확인하지 못했으니
// (adminErrors.ts 의 NETWORK_CREATE_FAILED 주석과 같은 이유) 오류로 다루지
// 않고, 계산한 네트워크 주소만 중립 안내로 보여 준다.
test("호스트 비트가 켜진 값은 막지 않고 시작 주소만 알려 준다", () => {
  const judgement = judgeCIDR("10.20.30.1/24");
  assert.equal(judgement.blocking, undefined);
  assert.equal(judgement.error, undefined);
  assert.match(judgement.hint ?? "", /10\.20\.30\.0\/24/);
});

test("시작 주소 안내는 같은 비트 수로 계산하고 경계에서도 맞다", () => {
  assert.match(judgeCIDR("10.20.30.255/24").hint ?? "", /10\.20\.30\.0\/24/);
  assert.match(judgeCIDR("255.255.255.255/0").hint ?? "", /0\.0\.0\.0\/0/);
  assert.match(judgeCIDR("10.20.30.5/31").hint ?? "", /10\.20\.30\.4\/31/);
  assert.match(judgeCIDR("172.16.5.3/12").hint ?? "", /172\.16\.0\.0\/12/);
});

// 기준 3. 이미 올바른 값에 오류 색을 올리거나 고칠 거리를 띄우면, 사용자는
// 맞게 적어 놓고도 틀린 줄 알게 된다.
test("올바른 값에는 오류도 고칠 거리도 띄우지 않는다", () => {
  for (const text of ["10.20.30.0/24", "0.0.0.0/0", "2001:db8::/32"]) {
    const judgement = judgeCIDR(text);
    assert.equal(judgement.blocking, undefined, `${text}: 막았다`);
    assert.equal(judgement.error, undefined, `${text}: 오류로 표시했다`);
    assert.equal(judgement.hint, undefined, `${text}: 고칠 거리를 띄웠다`);
  }
});

// IPv6 본문은 화면이 판정하지 않는다 — 판정 못 하는 것은 통과시킨다. 비트 수만
// 본다는 것을, 본문이 분명히 깨진 값이 통과하는 것으로 묶어 둔다.
test("IPv6 는 비트 수만 보고 주소 본문은 판정하지 않는다", () => {
  assert.equal(blocks("2001:db8::/128"), false);
  assert.equal(blocks("::/0"), false);
  assert.equal(blocks("zzz:/32"), false, "본문은 판정 대상이 아니다");
  assert.equal(blocks("2001:db8::/129"), true, "129 는 IPv6 범위 밖이다");
  assert.equal(blocks("2001:db8::/33"), false, "33 은 IPv6 에서는 유효하다");
});

// 비트 수의 선행 0 — 위 표의 그 두 줄을 따로 한 번 더 묶는다. 정규식으로
// 숫자인지만 보고 `Number()` 로 값을 읽으면 통과하고, 글자 수나 모양으로
// 판정하면 여기서 깨진다.
test("비트 수의 선행 0 은 서버와 같이 십진수로 읽는다", () => {
  assert.equal(blocks("10.20.30.0/024"), false);
  assert.equal(blocks("10.20.30.0/00000000024"), false);
  assert.equal(blocks("10.20.30.0/0032"), false);
  assert.equal(blocks("10.20.30.0/033"), true, "033 은 33 이라 범위 밖이다");
});

// 타이핑 중 소음. 「/」 를 적기 전 모든 글자가 빨간 오류로 보이면 폼이 사용자를
// 꾸짖는 꼴이 된다 — 버튼은 닫되 색은 쓰지 않는다.
test("「/」 를 적기 전 미완성 입력은 막기만 하고 오류로 칠하지 않는다", () => {
  for (const text of ["1", "10.2", "10.20.30"]) {
    const judgement = judgeCIDR(text);
    assert.equal(judgement.blocking !== undefined, true, `${text}: 열렸다`);
    assert.equal(judgement.error, undefined, `${text}: 오류로 칠했다`);
  }
});

test("표기가 분명히 틀린 값은 오류로 칠한다", () => {
  for (const text of ["10.20.30.0/33", "10.20.30.0/abc", "1.2.3.4.5/24"]) {
    assert.equal(judgeCIDR(text).error, true, `${text}: 오류로 칠하지 않았다`);
  }
});

// 빈 칸은 「추가」 가 이미 `!form.cidr` 로 닫아 둔다. 그 위에 오류를 겹쳐
// 띄우면 폼을 열자마자 빨간 글씨가 보인다.
test("빈 칸은 오류로 칠하지 않는다", () => {
  const judgement = judgeCIDR("");
  assert.equal(judgement.error, undefined);
  assert.equal(judgement.hint, undefined);
});

// 기준 4(c) 의 반대쪽. adminErrors.ts 의 INVALID_CIDR 안내가 이 상수를 읽으므로,
// 상수가 그 문장의 뒷부분과 글자 단위로 같아야 그 테스트가 손대지 않고 통과한다.
test("CIDR_RULE 은 서버 거절 안내가 쓰는 문장 그대로다", () => {
  assert.equal(
    CIDR_RULE,
    "「CIDR」 칸에 10.20.30.0/24 처럼 주소 뒤에 「/」 와 비트 수(IPv4 는 0~32, IPv6 는 0~128)를 붙여 적으세요.",
  );
});
