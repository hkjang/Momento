import assert from "node:assert/strict";
import test from "node:test";
import { chmodSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

/**
 * web/test/testCommand.test.mjs 와 같은 것을 sdk 쪽에서 묶는다.
 *
 * 이 저장소의 테스트는 `.ts` 모듈을 확장자까지 적어 import 하므로(아래 fixture
 * 의 `../src/index.ts`) 테스트를 돌리는 Node 가 타입 스트리핑을 할 수 있어야
 * 한다. 그런데 npm 은 lifecycle 스크립트의 PATH 에 상위 디렉터리의
 * `node_modules/.bin` 을 모두 앞에 붙이므로, 홈이나 상위 경로에 `node` 패키지가
 * 하나 깔려 있으면 `npm test` 안의 `node` 가 npm 자신을 돌리는 Node 가 아닌 그
 * 쪽으로 해석된다.
 *
 * v0.34.53 이 web 쪽을 그렇게 고쳤지만 sdk 의 test 명령은 맨 `node` 로 남아
 * 있었고, 2026-10-07 이 환경에서 실제로 가려졌다 — npm 은 v22.23.1 로 도는데
 * `~/node_modules/.bin/node`(v20.19.2)가 PATH 를 가려 `npm test` 가 두 파일 모두
 * ERR_UNKNOWN_FILE_EXTENSION ".ts" 로 떨어뜨렸다(같은 27개 테스트가 올바른
 * 인터프리터로는 27/27 통과). ci.yml:53 의 sdk 게이트가 web 게이트보다 먼저
 * 도므로, 이것이 가려지면 로컬에서 CI 를 재현할 길이 막힌다.
 */

const sdkRoot = fileURLToPath(new URL("..", import.meta.url));
// `.ts` 를 import 하는 실제 테스트 파일. 타입 스트리핑이 없으면 반드시 깨진다.
const fixture = "test/signals.test.mjs";

/** package.json 의 test 명령에서 인터프리터 토큰만 떼어낸다. */
function configuredInterpreter() {
  const pkg = JSON.parse(readFileSync(join(sdkRoot, "package.json"), "utf8"));
  const command = pkg.scripts.test;
  assert.ok(command, "package.json 에 test 스크립트가 있어야 한다");
  assert.match(command, /--test\b/, "test 스크립트는 node --test 로 돈다");
  return command.trim().split(/\s+/)[0];
}

/**
 * 타입 스트리핑을 끈 Node 를 `node` 라는 이름으로 PATH 앞에 세운다. 실제 Node 를
 * 그대로 exec 하되 NODE_OPTIONS 로 기능만 끄므로, 환경에 따라 달라지는 옛
 * 바이너리를 찾아다니지 않고도 같은 증상을 만든다.
 */
function shadowedPathDir() {
  const dir = mkdtempSync(join(tmpdir(), "momento-sdk-node-shadow-"));
  const shim = join(dir, "node");
  writeFileSync(
    shim,
    `#!/bin/sh\nNODE_OPTIONS="\${NODE_OPTIONS:+$NODE_OPTIONS }--no-experimental-strip-types" exec ${JSON.stringify(process.execPath)} "$@"\n`,
  );
  chmodSync(shim, 0o755);
  return dir;
}

function runWithShadowedNode(interpreter) {
  const dir = shadowedPathDir();
  const env = {
    ...process.env,
    PATH: `${dir}:${process.env.PATH}`,
    // npm 이 lifecycle 스크립트에 넘겨주는 값과 같은 역할.
    npm_node_execpath: process.execPath,
    NODE_OPTIONS: "",
  };
  // 이 파일 자신이 `node --test` 의 자식이라 NODE_TEST_CONTEXT 가 환경에 들어
  // 있다. 그대로 물려주면 안쪽 `node --test` 가 아무것도 돌리지 않은 채 0 으로
  // 끝나 아래 단언이 공허해진다.
  delete env.NODE_TEST_CONTEXT;
  return spawnSync("sh", ["-c", `${interpreter} --test ${fixture}`], {
    cwd: sdkRoot,
    encoding: "utf8",
    env,
  });
}

test("PATH 앞의 node 가 .ts 를 못 읽으면 맨 node 로 돌린 sdk 테스트는 실제로 깨진다", () => {
  const result = runWithShadowedNode("node");
  assert.notEqual(result.status, 0, "가림이 동작해야 아래 단언이 의미를 갖는다");
  assert.match(
    `${result.stdout}${result.stderr}`,
    /ERR_UNKNOWN_FILE_EXTENSION/,
  );
});

test("sdk package.json 의 test 명령은 PATH 앞의 node 에 가려지지 않는다", () => {
  const result = runWithShadowedNode(configuredInterpreter());
  assert.doesNotMatch(
    `${result.stdout}${result.stderr}`,
    /ERR_UNKNOWN_FILE_EXTENSION/,
    "설정된 인터프리터가 PATH 의 node 로 해석됐다",
  );
  assert.equal(result.status, 0, `${result.stdout}${result.stderr}`);
});
