export type PathTransition = {
  source: string;
  target: string;
  count: number;
};

export type PathNode = {
  name: string;
  displayName: string;
  shortName: string;
  depth: number;
};

export type PathLink = {
  source: string;
  target: string;
  value: number;
  sourceName: string;
  targetName: string;
};

export function shortPathLabel(value: string, maxLength = 34) {
  let label = value;
  try {
    const url = new URL(value);
    label = `${url.pathname}${url.search}` || "/";
  } catch {
    // Event names are intentionally kept as-is.
  }
  if (label.length <= maxLength) return label;
  return `${label.slice(0, Math.max(1, maxLength - 1))}…`;
}

export function buildPathFlow(rows: PathTransition[], limit = 60) {
  const transitions = rows
    .map((row) => ({
      source: String(row.source || "(unknown)"),
      target: String(row.target || "(unknown)"),
      count: Number(row.count) || 0,
    }))
    .filter((row) => row.source !== row.target && row.count > 0)
    .slice(0, limit);
  const nodes = new Map<string, PathNode>();
  const links: PathLink[] = transitions.map((row) => {
    // Real journeys commonly contain A → B and B → A. Keeping origin and
    // destination nodes in separate layers makes the Sankey graph acyclic.
    const source = `from:${row.source}`;
    const target = `to:${row.target}`;
    nodes.set(source, {
      name: source,
      displayName: row.source,
      shortName: shortPathLabel(row.source),
      depth: 0,
    });
    nodes.set(target, {
      name: target,
      displayName: row.target,
      shortName: shortPathLabel(row.target),
      depth: 1,
    });
    return {
      source,
      target,
      value: row.count,
      sourceName: row.source,
      targetName: row.target,
    };
  });
  return { nodes: Array.from(nodes.values()), links };
}
