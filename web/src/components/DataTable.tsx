import { useEffect, useMemo, useState, type ReactNode } from "react";
import {
  Box,
  Button,
  Card,
  InputAdornment,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TablePagination,
  TableRow,
  TableSortLabel,
  TextField,
  Typography,
} from "@mui/material";
import DownloadRounded from "@mui/icons-material/DownloadRounded";
import SearchRounded from "@mui/icons-material/SearchRounded";
import { Empty } from "./States";
import { buildCSV, cellText } from "./csvExport";
import { clampPage } from "./tablePaging";
import {
  firstDirection,
  nextSort,
  sortRows,
  type SortState,
} from "./tableSort";
import { tableCaption } from "./tableSummary";

export interface Column {
  key: string;
  label: string;
  align?: "left" | "right" | "center";
  minWidth?: number;
  format?: (value: unknown, row: Record<string, unknown>) => ReactNode;
  // 정렬 여부. 생략하면 제목이 있고 값이 하나라도 있는 열만 정렬할 수 있다 — 버튼만
  // 그리는 열은 제목이 비어 있거나 행에 그 키가 없으므로 저절로 빠진다. 제목을 단
  // 버튼 열(예: key "id" 의 「관리」)은 false 로 끈다.
  sortable?: boolean;
  // 화면 값과 정렬 기준이 다를 때(예: 가공한 문자열, 중첩 필드) 정렬에 쓸 값.
  sortValue?: (row: Record<string, unknown>) => unknown;
}

function columnValue(column: Column, row: Record<string, unknown>): unknown {
  return column.sortValue ? column.sortValue(row) : row[column.key];
}

interface DataTableProps {
  columns: Column[];
  rows: Record<string, unknown>[];
  title?: string;
  description?: string;
  searchable?: boolean;
  searchPlaceholder?: string;
  exportFilename?: string;
  dense?: boolean;
  initialPageSize?: number;
  getRowKey?: (row: Record<string, unknown>, index: number) => string;
}

function downloadCSV(
  filename: string,
  columns: Column[],
  rows: Record<string, unknown>[],
) {
  const url = URL.createObjectURL(
    new Blob(["\uFEFF", buildCSV(columns, rows)], {
      type: "text/csv;charset=utf-8",
    }),
  );
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename.endsWith(".csv") ? filename : `${filename}.csv`;
  anchor.click();
  URL.revokeObjectURL(url);
}

export default function DataTable({
  columns,
  rows,
  title,
  description,
  searchable,
  searchPlaceholder = "표에서 검색",
  exportFilename,
  dense = true,
  initialPageSize = 25,
  getRowKey,
}: DataTableProps) {
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(initialPageSize);
  const [sort, setSort] = useState<SortState | null>(null);
  const hasSearch = searchable ?? rows.length > 8;
  const filtered = useMemo(() => {
    const needle = query.trim().toLocaleLowerCase("ko-KR");
    if (!needle) return rows;
    return rows.filter((row) =>
      columns.some((column) =>
        cellText(row[column.key]).toLocaleLowerCase("ko-KR").includes(needle),
      ),
    );
  }, [columns, query, rows]);
  const sortableKeys = useMemo(() => {
    const keys = new Set<string>();
    for (const column of columns) {
      const allowed =
        column.sortable ??
        (!!column.label.trim() &&
          (!!column.sortValue ||
            rows.some((row) => {
              const value = row[column.key];
              return value != null && value !== "";
            })));
      if (allowed) keys.add(column.key);
    }
    return keys;
  }, [columns, rows]);
  // 정렬한 열이 다음 조회에서 사라지면(열 구성이 바뀐 경우) 서버 순서로 돌아간다.
  const activeSort = sort && sortableKeys.has(sort.key) ? sort : null;
  const sorted = useMemo(() => {
    if (!activeSort) return filtered;
    const column = columns.find((item) => item.key === activeSort.key);
    if (!column) return filtered;
    return sortRows(
      filtered,
      (row) => columnValue(column, row),
      activeSort.direction,
    );
  }, [activeSort, columns, filtered]);
  const toggleSort = (column: Column) => {
    const first = firstDirection(rows.map((row) => columnValue(column, row)));
    setSort((current) => nextSort(current, column.key, first));
    setPage(0);
  };
  useEffect(() => setPage(0), [query, rows.length]);
  // 페이지를 0으로 돌리는 위 effect 는 렌더 뒤에 돌고, 행 수가 그대로인 채 내용만
  // 바뀐 재조회에서는 돌지 않는다. 자르기 전에 범위 안으로 끌어와 빈 표를 막는다.
  const safePage = clampPage(page, pageSize, filtered.length);
  const paged = sorted.slice(
    safePage * pageSize,
    safePage * pageSize + pageSize,
  );
  const showToolbar = !!title || hasSearch || !!exportFilename;
  const minWidth = columns.reduce(
    (width, column) => width + (column.minWidth || 132),
    0,
  );
  const rowKey =
    getRowKey ||
    ((row: Record<string, unknown>, index: number) =>
      String(
        row.id ||
          row.event ||
          row.event_name ||
          row.page ||
          row.visitor_id ||
          row.name ||
          `${safePage}-${index}`,
      ));

  return (
    <Card sx={{ overflow: "hidden" }}>
      {showToolbar && (
        <Stack
          direction={{ xs: "column", sm: "row" }}
          alignItems={{ xs: "stretch", sm: "center" }}
          gap={1.5}
          sx={{
            px: 2,
            py: 1.6,
            borderBottom: "1px solid",
            borderColor: "divider",
          }}
        >
          <Box sx={{ minWidth: 0, flex: 1 }}>
            {title && <Typography fontWeight={720}>{title}</Typography>}
            <Typography variant="caption" color="text.secondary">
              {tableCaption({
                description,
                total: rows.length,
                matched: filtered.length,
                searching: !!query.trim(),
              })}
            </Typography>
          </Box>
          {hasSearch && (
            <TextField
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={searchPlaceholder}
              aria-label={searchPlaceholder}
              sx={{ width: { xs: "100%", sm: 230 } }}
              slotProps={{
                input: {
                  startAdornment: (
                    <InputAdornment position="start">
                      <SearchRounded fontSize="small" />
                    </InputAdornment>
                  ),
                },
              }}
            />
          )}
          {exportFilename && (
            <Button
              variant="outlined"
              startIcon={<DownloadRounded />}
              onClick={() => downloadCSV(exportFilename, columns, sorted)}
              disabled={!filtered.length}
            >
              CSV
            </Button>
          )}
        </Stack>
      )}
      <TableContainer sx={{ maxHeight: 660 }}>
        <Table stickyHeader size={dense ? "small" : "medium"} sx={{ minWidth }}>
          <TableHead>
            <TableRow>
              {columns.map((column, index) => {
                const active = activeSort?.key === column.key;
                return (
                  <TableCell
                    key={`${column.key}-${index}`}
                    align={column.align}
                    sx={{ minWidth: column.minWidth }}
                    aria-sort={
                      active
                        ? activeSort.direction === "asc"
                          ? "ascending"
                          : "descending"
                        : undefined
                    }
                  >
                    {sortableKeys.has(column.key) ? (
                      <TableSortLabel
                        active={active}
                        direction={active ? activeSort.direction : "asc"}
                        onClick={() => toggleSort(column)}
                        title="눌러서 정렬 · 세 번째로 누르면 원래 순서"
                      >
                        {column.label}
                      </TableSortLabel>
                    ) : (
                      column.label
                    )}
                  </TableCell>
                );
              })}
            </TableRow>
          </TableHead>
          <TableBody>
            {paged.map((row, index) => (
              <TableRow hover key={rowKey(row, index)}>
                {columns.map((column, columnIndex) => (
                  <TableCell key={`${column.key}-${columnIndex}`} align={column.align}>
                    {column.format ? (
                      column.format(row[column.key], row)
                    ) : typeof row[column.key] === "number" ? (
                      Intl.NumberFormat("ko-KR").format(
                        row[column.key] as number,
                      )
                    ) : (
                      <Typography variant="body2" noWrap>
                        {String(row[column.key] ?? "—")}
                      </Typography>
                    )}
                  </TableCell>
                ))}
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {!filtered.length && (
          <Empty
            title={query ? "검색 결과가 없습니다" : "아직 데이터가 없습니다"}
            description={
              query
                ? "검색어를 바꾸거나 필터를 초기화해 보세요."
                : "데이터가 생성되면 이 표에 표시됩니다."
            }
          />
        )}
      </TableContainer>
      {filtered.length > 10 && (
        <TablePagination
          component="div"
          count={filtered.length}
          page={safePage}
          onPageChange={(_, value) => setPage(value)}
          rowsPerPage={pageSize}
          onRowsPerPageChange={(event) => {
            setPageSize(Number(event.target.value));
            setPage(0);
          }}
          rowsPerPageOptions={[10, 25, 50, 100].map((value) => ({
            label: `${value}개`,
            value,
          }))}
          labelRowsPerPage="페이지당"
          labelDisplayedRows={({ from, to, count }) =>
            `${from}–${to} / ${count}`
          }
        />
      )}
    </Card>
  );
}
