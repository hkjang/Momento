// The CSV a table exports is opened in a spreadsheet, and a spreadsheet does not
// read a cell that starts with "=" as text. Most of what the tables show —
// page URLs, campaigns, event names, user properties — was written by a
// visitor's browser, so whoever can reach the collector could decide what the
// analyst's Excel evaluates: a HYPERLINK that leaks the cell next to it, or a
// DDE call that runs a command. Kept apart from the table so the rule can be
// tested without rendering it.

export interface CSVColumn {
  key: string;
  label: string;
}

export function cellText(value: unknown): string {
  if (value == null) return "";
  if (typeof value === "object") {
    try {
      return JSON.stringify(value);
    } catch {
      return String(value);
    }
  }
  return String(value);
}

// The apostrophe is what the spreadsheets themselves use to say "this is
// text". It is added only to a string that would otherwise be evaluated, so a
// negative number stays a number and ids, dates and JSON come through
// untouched. Leading whitespace is looked past because some importers trim it
// before deciding.
export function csvValue(value: unknown): string {
  let text = cellText(value);
  if (typeof value === "string" && /^[\s]*[=+\-@]/.test(text)) {
    text = `'${text}`;
  }
  return `"${text.replaceAll('"', '""')}"`;
}

export function buildCSV(
  columns: CSVColumn[],
  rows: Record<string, unknown>[],
): string {
  return [
    columns.map((column) => csvValue(column.label)).join(","),
    ...rows.map((row) =>
      columns.map((column) => csvValue(row[column.key])).join(","),
    ),
  ].join("\n");
}
