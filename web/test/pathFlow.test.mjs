import assert from "node:assert/strict";
import test from "node:test";
import { buildPathFlow, shortPathLabel } from "../src/pages/pathFlow.ts";

test("왕복 이동을 순환 없는 두 계층 그래프로 변환한다", () => {
  const flow = buildPathFlow([
    { source: "/home", target: "/search", count: 12 },
    { source: "/search", target: "/home", count: 7 },
    { source: "/home", target: "/home", count: 3 },
  ]);

  assert.deepEqual(
    flow.links.map(({ source, target, value }) => ({ source, target, value })),
    [
      { source: "from:/home", target: "to:/search", value: 12 },
      { source: "from:/search", target: "to:/home", value: 7 },
    ],
  );
  assert.ok(flow.links.every((link) => link.source.startsWith("from:")));
  assert.ok(flow.links.every((link) => link.target.startsWith("to:")));
});

test("제한된 링크가 참조하는 노드를 빠짐없이 생성한다", () => {
  const rows = Array.from({ length: 80 }, (_, index) => ({
    source: `/from-${index}`,
    target: `/to-${index}`,
    count: index + 1,
  }));
  const flow = buildPathFlow(rows, 60);
  const nodeNames = new Set(flow.nodes.map((node) => node.name));

  assert.equal(flow.links.length, 60);
  assert.ok(
    flow.links.every(
      (link) => nodeNames.has(link.source) && nodeNames.has(link.target),
    ),
  );
});

test("긴 URL은 차트에서 경로 중심으로 줄이고 원문은 보존한다", () => {
  const url = "https://service.example.com/approval/documents/very-long-document-name";
  const flow = buildPathFlow([{ source: url, target: "/done", count: 2 }]);

  assert.equal(flow.nodes[0].displayName, url);
  assert.equal(flow.nodes[0].shortName, shortPathLabel(url));
  assert.ok(flow.nodes[0].shortName.startsWith("/approval/"));
  assert.ok(flow.nodes[0].shortName.endsWith("…"));
});
