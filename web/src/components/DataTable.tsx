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
  TextField,
  Typography,
} from "@mui/material";
import DownloadRounded from "@mui/icons-material/DownloadRounded";
import SearchRounded from "@mui/icons-material/SearchRounded";
import { Empty } from "./States";

export interface Column {
  key: string;
  label: string;
  align?: "left" | "right" | "center";
  minWidth?: number;
  format?: (value: unknown, row: Record<string, unknown>) => ReactNode;
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

function searchableValue(value: unknown): string {
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

function csvValue(value: unknown): string {
  const text = searchableValue(value).replaceAll('"', '""');
  return `"${text}"`;
}

function downloadCSV(
  filename: string,
  columns: Column[],
  rows: Record<string, unknown>[],
) {
  const csv = [
    columns.map((column) => csvValue(column.label)).join(","),
    ...rows.map((row) =>
      columns.map((column) => csvValue(row[column.key])).join(","),
    ),
  ].join("\n");
  const url = URL.createObjectURL(
    new Blob(["\uFEFF", csv], { type: "text/csv;charset=utf-8" }),
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
  const hasSearch = searchable ?? rows.length > 8;
  const filtered = useMemo(() => {
    const needle = query.trim().toLocaleLowerCase("ko-KR");
    if (!needle) return rows;
    return rows.filter((row) =>
      columns.some((column) =>
        searchableValue(row[column.key])
          .toLocaleLowerCase("ko-KR")
          .includes(needle),
      ),
    );
  }, [columns, query, rows]);
  useEffect(() => setPage(0), [query, rows.length]);
  const paged = filtered.slice(page * pageSize, page * pageSize + pageSize);
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
          `${page}-${index}`,
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
              {description ||
                `${Intl.NumberFormat("ko-KR").format(filtered.length)}개 항목`}
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
              onClick={() => downloadCSV(exportFilename, columns, filtered)}
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
              {columns.map((column) => (
                <TableCell
                  key={column.key}
                  align={column.align}
                  sx={{ minWidth: column.minWidth }}
                >
                  {column.label}
                </TableCell>
              ))}
            </TableRow>
          </TableHead>
          <TableBody>
            {paged.map((row, index) => (
              <TableRow hover key={rowKey(row, index)}>
                {columns.map((column) => (
                  <TableCell key={column.key} align={column.align}>
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
          page={Math.min(
            page,
            Math.max(0, Math.ceil(filtered.length / pageSize) - 1),
          )}
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
