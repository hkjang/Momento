// 「망 대역 추가」 폼의 「CIDR」 칸이 보내기 전에 쓰는 판정. 서버의
// createNetwork(internal/httpapi/admin.go:812)는 `net.ParseCIDR` 하나로 값을
// 받거나 400 INVALID_CIDR 로 거절하므로, 여기서 막아도 되는 것은 ParseCIDR 이
// **확실히 거절하는** 값뿐이다 — 화면이 서버보다 좁아지면 서버가 받아 줄 값을
// 넣을 방법이 없어진다. 그래서 판정은 ParseCIDR 이 실제로 보는 것만 본다:
// 「/」 가 있는지, 그 뒤가 십진수이고 범위 안인지, (콜론이 없을 때) 주소가
// 0~255 네 칸인지. IPv6 본문은 판정하지 않고 통과시킨다.
//
// passwordRule.ts 와 같은 자리의 순수 모듈이고, 같은 이유로 문장 상수를
// 내보낸다 — adminErrors.ts 의 INVALID_CIDR 안내와 폼의 helperText 가 같은
// 문장을 쓰게 해서, 사용자가 **틀린 뒤에만** 올바른 표기법을 읽는 일을 없앤다.

export const CIDR_RULE =
  "「CIDR」 칸에 10.20.30.0/24 처럼 주소 뒤에 「/」 와 비트 수(IPv4 는 0~32, IPv6 는 0~128)를 붙여 적으세요.";

export type CIDRJudgement = {
  /** 있으면 「추가」 를 닫고 이 문장을 칸 아래에 보인다. */
  blocking?: string;
  /** 표기가 분명히 틀렸을 때만. 미완성 입력은 색을 쓰지 않는다. */
  error?: true;
  /** 막지는 않지만 알려 둘 것. 오류 색을 쓰지 않는다. */
  hint?: string;
};

const IPV4_MAX_BITS = 32;
const IPV6_MAX_BITS = 128;

/**
 * 주소를 0~255 네 칸으로 읽는다. 읽지 못하면 null.
 *
 * 선행 0(`010.20.30.0`)은 Go 1.17 이후의 ParseCIDR 이 거절하지만 여기서는
 * 통과시킨다 — 막아도 되는 값이긴 해도, 표기 판정을 넓히다 통과시켜야 할 값을
 * 막는 쪽이 더 비싸다. 왕복 한 번으로 끝나는 쪽을 고른다.
 */
function readIPv4(address: string): number[] | null {
  const fields = address.split(".");
  if (fields.length !== 4) return null;
  const octets = [];
  for (const field of fields) {
    if (!/^[0-9]+$/.test(field)) return null;
    const octet = Number(field);
    if (octet > 255) return null;
    octets.push(octet);
  }
  return octets;
}

/** 점 네 칸 표기로. */
function dotted(value: number): string {
  return [2 ** 24, 2 ** 16, 2 ** 8, 1]
    .map((place) => Math.floor(value / place) % 256)
    .join(".");
}

export function judgeCIDR(text: string): CIDRJudgement {
  // 빈 칸은 「추가」 가 이미 `!form.cidr` 로 닫아 둔다. 폼을 열자마자 빨간
  // 글씨가 보이지 않게 아무것도 돌려주지 않는다.
  if (text === "") return {};

  // ParseCIDR 은 **첫** 「/」 에서 자른다. 그래서 `10.20.30.0/24/8` 의 비트 수는
  // "24/8" 이고, 아래 숫자 판정이 그것을 거절한다 — 서버와 같은 결론이다.
  const slash = text.indexOf("/");
  if (slash < 0) {
    // 타이핑 중인 "미완성" 상태. 버튼이 왜 닫혔는지 읽을 거리는 주되, 아직
    // 틀린 것이 아니므로 오류 색은 쓰지 않는다.
    return { blocking: `비트 수가 아직 없습니다. ${CIDR_RULE}` };
  }

  const address = text.slice(0, slash);
  const mask = text.slice(slash + 1);
  // ParseCIDR 도 주소 쪽에 콜론이 있는지로 IPv4·IPv6 을 가른다.
  const ipv6 = address.includes(":");
  const maxBits = ipv6 ? IPV6_MAX_BITS : IPV4_MAX_BITS;

  if (!/^[0-9]+$/.test(mask)) {
    return {
      blocking: `비트 수를 숫자로 적으세요. ${CIDR_RULE}`,
      error: true,
    };
  }
  // 선행 0 은 서버의 dtoi 와 같이 그냥 십진수로 읽는다 — `/024` 는 24 라서
  // ParseCIDR 이 통과시키므로 화면도 통과시켜야 한다.
  const bits = Number(mask);
  if (bits > maxBits) {
    return {
      blocking: `비트 수가 범위를 벗어났습니다. ${
        ipv6 ? "IPv6 는 0~128" : "IPv4 는 0~32"
      } 입니다.`,
      error: true,
    };
  }

  // IPv6 본문은 판정하지 않는다 — 판정 못 하는 것은 통과시킨다.
  if (ipv6) return {};

  const octets = readIPv4(address);
  if (!octets) {
    return {
      blocking: `주소를 10.20.30.0 처럼 0~255 네 칸으로 적으세요. ${CIDR_RULE}`,
      error: true,
    };
  }

  // 호스트 비트가 켜진 값(`10.20.30.1/24`)은 ParseCIDR 이 통과시키므로 막지
  // 않는다. Postgres `cidr` 컬럼이 이것을 어떻게 다루는지는 확인하지 못했으니
  // (adminErrors.ts 의 NETWORK_CREATE_FAILED 주석과 같은 이유) 아무 주장도
  // 하지 않고, 계산해서 알 수 있는 것만 보여 준다.
  const value = octets.reduce((acc, octet) => acc * 256 + octet, 0);
  // 2 ** (32 - bits) 로 나눈 나머지를 떼면 bits=0(전체가 0)과 bits=32(그대로)가
  // 특별 취급 없이 같은 식으로 나온다. 32비트 비트연산은 부호 때문에 피한다.
  const start = value - (value % 2 ** (IPV4_MAX_BITS - bits));
  // 값으로 비교한다 — 표기만 다른 경우(`010.20.30.0`)가 아니라 호스트 비트가
  // 정말 켜진 경우에만 안내가 뜨게.
  if (start !== value) {
    return { hint: `대역의 시작 주소는 ${dotted(start)}/${bits} 입니다.` };
  }
  return {};
}
