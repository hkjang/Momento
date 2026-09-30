import { useEffect, useMemo, useState, type ReactNode } from "react";
import {
  Box,
  Button,
  Card,
  Checkbox,
  IconButton,
  InputAdornment,
  ListItemIcon,
  ListItemText,
  Menu,
  MenuItem,
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
  Tooltip,
  Typography,
} from "@mui/material";
import CloseRounded from "@mui/icons-material/CloseRounded";
import DensityMediumRounded from "@mui/icons-material/DensityMediumRounded";
import DensitySmallRounded from "@mui/icons-material/DensitySmallRounded";
import DownloadRounded from "@mui/icons-material/DownloadRounded";
import SearchRounded from "@mui/icons-material/SearchRounded";
import ViewColumnRounded from "@mui/icons-material/ViewColumnRounded";
import { usePreference } from "./preference";
import {
  DENSITY_STORAGE_KEY,
  exportName,
  highlightParts,
  isNumericColumn,
  parseHidden,
  tableStorageKey,
  toggleHidden,
  type Density,
} from "./tablePrefs";
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

// 검색어와 일치한 부분을 강조한다. 형식 함수를 거치지 않은 글자 칸에만 쓴다.
function Highlighted({ text, query }: { text: string; query: string }) {
  const parts = highlightParts(text, query);
  if (parts.length === 1 && !parts[0].match) return <>{text}</>;
  return (
    <>
      {parts.map((part, index) =>
        part.match ? (
          <Box
            key={index}
            component="mark"
            sx={{ bgcolor: "#FFF1B8", color: "inherit", borderRadius: 0.5, px: 0.2 }}
          >
            {part.text}
          </Box>
        ) : (
          <span key={index}>{part.text}</span>
        ),
      )}
    </>
  );
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
  const columnKeys = useMemo(() => columns.map((column) => column.key), [columns]);
  // 숨긴 열은 표마다, 밀도는 모든 표가 함께 기억한다(components/tablePrefs.ts).
  const [hiddenRaw, setHiddenRaw] = usePreference(
    tableStorageKey(title, exportFilename, columnKeys),
  );
  const hidden = useMemo(() => parseHidden(hiddenRaw, columnKeys), [hiddenRaw, columnKeys]);
  const visibleColumns = useMemo(
    () => columns.filter((column) => !hidden.includes(column.key)),
    [columns, hidden],
  );
  const [densityRaw, setDensity] = usePreference(DENSITY_STORAGE_KEY);
  const density: Density =
    densityRaw === "small" || densityRaw === "medium"
      ? densityRaw
      : dense
        ? "small"
        : "medium";
  const [columnMenu, setColumnMenu] = useState<HTMLElement | null>(null);
  const numericKeys = useMemo(
    () =>
      new Set(
        columns
          .filter((column) => isNumericColumn(rows.map((row) => row[column.key])))
          .map((column) => column.key),
      ),
    [columns, rows],
  );
  const alignOf = (column: Column) =>
    column.align ?? (numericKeys.has(column.key) ? "right" : undefined);
  const csvName = exportName(exportFilename, title);
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
  const minWidth = visibleColumns.reduce(
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
            onKeyDown={(event) => {
              if (event.key === "Escape" && query) {
                event.stopPropagation();
                setQuery("");
              }
            }}
            placeholder={searchPlaceholder}
            aria-label={searchPlaceholder}
            sx={{ width: { xs: "100%", sm: 230 } }}
            slotProps={{
              htmlInput: { "data-page-search": true },
              input: {
                startAdornment: (
                  <InputAdornment position="start">
                    <SearchRounded fontSize="small" />
                  </InputAdornment>
                ),
                endAdornment: query ? (
                  <InputAdornment position="end">
                    <IconButton
                      size="small"
                      aria-label="검색어 지우기"
                      onClick={() => setQuery("")}
                      edge="end"
                    >
                      <CloseRounded fontSize="small" />
                    </IconButton>
                  </InputAdornment>
                ) : undefined,
              },
            }}
          />
        )}
        <Stack direction="row" gap={0.5} alignItems="center">
          <Tooltip title={density === "small" ? "넓게 보기" : "촘촘하게 보기"}>
            <IconButton
              size="small"
              aria-label={density === "small" ? "넓게 보기" : "촘촘하게 보기"}
              onClick={() => setDensity(density === "small" ? "medium" : "small")}
            >
              {density === "small" ? (
                <DensityMediumRounded fontSize="small" />
              ) : (
                <DensitySmallRounded fontSize="small" />
              )}
            </IconButton>
          </Tooltip>
          {columns.length > 1 && (
            <Tooltip title="열 표시">
              <IconButton
                size="small"
                aria-label="열 표시"
                aria-haspopup="menu"
                onClick={(event) => setColumnMenu(event.currentTarget)}
                color={hidden.length ? "primary" : "default"}
              >
                <ViewColumnRounded fontSize="small" />
              </IconButton>
            </Tooltip>
          )}
          <Button
            variant="outlined"
            startIcon={<DownloadRounded />}
            onClick={() => downloadCSV(csvName, visibleColumns, sorted)}
            disabled={!filtered.length}
          >
            CSV
          </Button>
        </Stack>
        <Menu
          anchorEl={columnMenu}
          open={!!columnMenu}
          onClose={() => setColumnMenu(null)}
        >
          {columns.map((column, index) => {
            const shown = !hidden.includes(column.key);
            const last = shown && visibleColumns.length <= 1;
            return (
              <MenuItem
                key={`${column.key}-${index}`}
                dense
                disabled={last}
                onClick={() =>
                  setHiddenRaw(
                    JSON.stringify(toggleHidden(hidden, column.key, columnKeys)),
                  )
                }
              >
                <ListItemIcon>
                  <Checkbox size="small" edge="start" checked={shown} tabIndex={-1} disableRipple />
                </ListItemIcon>
                <ListItemText primary={column.label || column.key} />
              </MenuItem>
            );
          })}
          {hidden.length > 0 && (
            <MenuItem dense onClick={() => setHiddenRaw(null)}>
              <ListItemText inset primary="모든 열 보기" />
            </MenuItem>
          )}
        </Menu>
      </Stack>
      <TableContainer sx={{ maxHeight: 660 }}>
        <Table stickyHeader size={density} sx={{ minWidth }}>
          <TableHead>
            <TableRow>
              {visibleColumns.map((column, index) => {
                const active = activeSort?.key === column.key;
                return (
                  <TableCell
                    key={`${column.key}-${index}`}
                    align={alignOf(column)}
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
                {visibleColumns.map((column, columnIndex) => (
                  <TableCell
                    key={`${column.key}-${columnIndex}`}
                    align={alignOf(column)}
                  >
                    {column.format ? (
                      column.format(row[column.key], row)
                    ) : typeof row[column.key] === "number" ? (
                      Intl.NumberFormat("ko-KR").format(
                        row[column.key] as number,
                      )
                    ) : (
                      <Typography variant="body2" noWrap>
                        <Highlighted
                          text={String(row[column.key] ?? "—")}
                          query={query}
                        />
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
