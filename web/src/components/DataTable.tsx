import {
  Card,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
} from "@mui/material";
import { Empty } from "./States";
import type { ReactNode } from "react";

export interface Column {
  key: string;
  label: string;
  align?: "left" | "right";
  format?: (value: unknown, row: Record<string, unknown>) => ReactNode;
}
export default function DataTable({
  columns,
  rows,
}: {
  columns: Column[];
  rows: Record<string, unknown>[];
}) {
  return (
    <Card>
      <TableContainer sx={{ maxHeight: 660 }}>
        <Table stickyHeader>
          <TableHead>
            <TableRow>
              {columns.map((c) => (
                <TableCell key={c.key} align={c.align}>
                  {c.label}
                </TableCell>
              ))}
            </TableRow>
          </TableHead>
          <TableBody>
            {rows.map((row, i) => (
              <TableRow
                hover
                key={String(
                  row.id || row.event || row.page || row.visitor_id || i,
                )}
              >
                {columns.map((c) => (
                  <TableCell key={c.key} align={c.align}>
                    {c.format ? (
                      c.format(row[c.key], row)
                    ) : typeof row[c.key] === "number" ? (
                      Intl.NumberFormat("ko-KR").format(row[c.key] as number)
                    ) : (
                      <Typography variant="body2">
                        {String(row[c.key] ?? "—")}
                      </Typography>
                    )}
                  </TableCell>
                ))}
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {!rows.length && <Empty />}
      </TableContainer>
    </Card>
  );
}
