import { useState } from "react";
import {
  Alert,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  MenuItem,
  Stack,
  TextField,
  Typography,
} from "@mui/material";
import { dateRangeValues } from "../api/client";
import { rangeProblem, type DateRange } from "./periodParam";
import CopyLinkButton from "./CopyLinkButton";

const CUSTOM = "custom";

// 분석 기간 선택. 최근 N일에 더해, 화면이 setRange 를 넘기면 「직접 선택…」 으로
// 시작일·종료일을 고를 수 있다. 분석 도구 모음과 RangeSelect 가 같은 것을 쓴다.
export default function PeriodField({
  days,
  setDays,
  options,
  range,
  setRange,
  timezone = "UTC",
  maxExactDays,
}: {
  days: number;
  setDays(days: number): void;
  options: number[];
  range?: DateRange | null;
  setRange?(range: DateRange | null): void;
  timezone?: string;
  maxExactDays?: number;
}) {
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<DateRange>({ from: "", to: "" });
  const today = dateRangeValues(1, timezone).to;
  const presetRange = dateRangeValues(days, timezone);
  const problem = rangeProblem(draft, today, maxExactDays);
  const custom = !!(setRange && range);
  const openPicker = () => {
    setDraft(range ?? dateRangeValues(days, timezone));
    setOpen(true);
  };
  return (
    <>
      <TextField
        select
        size="small"
        label="분석 기간"
        value={custom ? CUSTOM : days}
        onChange={(event) => {
          // 「직접 선택…」 은 자기 onClick 으로 다이얼로그를 연다 — 이미 직접 기간일 때
          // 같은 항목을 다시 고르면 Select 는 onChange 를 부르지 않기 때문이다.
          if (event.target.value !== CUSTOM) setDays(Number(event.target.value));
        }}
        sx={{ minWidth: custom ? 230 : 140 }}
      >
        {options.map((option) => (
          <MenuItem key={option} value={option}>
            {`최근 ${option}일`}
          </MenuItem>
        ))}
        {setRange && (
          <MenuItem value={CUSTOM} onClick={openPicker}>
            {range ? `${range.from} ~ ${range.to}` : "직접 선택…"}
          </MenuItem>
        )}
      </TextField>
      {/* 「최근 30일」이 어느 날부터인지는 시간대까지 셈해야 알 수 있었다. */}
      {!custom && (
        <Typography variant="caption" color="text.secondary" noWrap>
          {`${presetRange.from} ~ ${presetRange.to}`}
        </Typography>
      )}
      <CopyLinkButton />
      {setRange && (
        <Dialog open={open} onClose={() => setOpen(false)} maxWidth="xs" fullWidth>
          <DialogTitle>분석 기간 직접 선택</DialogTitle>
          <DialogContent>
            <Stack spacing={2} sx={{ pt: 1 }}>
              <TextField
                type="date"
                label="시작일"
                value={draft.from}
                onChange={(event) => setDraft({ ...draft, from: event.target.value })}
                slotProps={{ inputLabel: { shrink: true }, htmlInput: { max: draft.to || today } }}
              />
              <TextField
                type="date"
                label="종료일"
                value={draft.to}
                onChange={(event) => setDraft({ ...draft, to: event.target.value })}
                slotProps={{ inputLabel: { shrink: true }, htmlInput: { min: draft.from, max: today } }}
              />
              {problem ? (
                <Alert severity="warning">{problem}</Alert>
              ) : (
                <Alert severity="info">
                  {`${timezone} 기준, 양 끝 날짜를 포함합니다.`}
                </Alert>
              )}
            </Stack>
          </DialogContent>
          <DialogActions>
            {range && (
              <Button
                onClick={() => {
                  setRange(null);
                  setOpen(false);
                }}
                sx={{ mr: "auto" }}
              >
                직접 기간 해제
              </Button>
            )}
            <Button onClick={() => setOpen(false)}>취소</Button>
            <Button
              variant="contained"
              disabled={!!problem}
              onClick={() => {
                setRange({ from: draft.from, to: draft.to });
                setOpen(false);
              }}
            >
              적용
            </Button>
          </DialogActions>
        </Dialog>
      )}
    </>
  );
}
