import {
  Box,
  Button,
  IconButton,
  MenuItem,
  Stack,
  TextField,
  Typography,
} from "@mui/material";
import AddRounded from "@mui/icons-material/AddRounded";
import AccountTreeRounded from "@mui/icons-material/AccountTreeRounded";
import DeleteOutlineRounded from "@mui/icons-material/DeleteOutlineRounded";

export type SegmentNode = {
  combinator?: "and" | "or";
  rules?: SegmentNode[];
  field?: string;
  operator?: string;
  value?: unknown;
};

export const builtInSegmentFields = [
  "event.name",
  "event.has",
  "page.url",
  "device.type",
  "browser",
  "os",
  "country",
  "traffic.source",
  "traffic.medium",
  "traffic.campaign",
  "traffic.class",
  "network",
  "user.id",
  "visitor.id",
  "session.id",
  "user.department",
  "user.organization",
  "feature",
  "button",
  "is_conversion",
];

const operators = [
  "=",
  "!=",
  "contains",
  "not contains",
  "startsWith",
  "endsWith",
  ">",
  ">=",
  "<",
  "<=",
  "exists",
  "not exists",
  "in",
  "not in",
];

export const emptyRule = (): SegmentNode => ({
  field: "event.name",
  operator: "=",
  value: "",
});

export const emptySegment = (): SegmentNode => ({
  combinator: "and",
  rules: [emptyRule()],
});

function valueForInput(rule: SegmentNode) {
  if (Array.isArray(rule.value)) return rule.value.join(", ");
  return rule.value == null ? "" : String(rule.value);
}

function GroupEditor({
  node,
  fields,
  depth,
  onChange,
  onDelete,
}: {
  node: SegmentNode;
  fields: string[];
  depth: number;
  onChange(next: SegmentNode): void;
  onDelete?: () => void;
}) {
  const rules = node.rules || [];
  const updateRule = (index: number, next: SegmentNode) =>
    onChange({
      ...node,
      rules: rules.map((rule, i) => (i === index ? next : rule)),
    });
  const deleteRule = (index: number) =>
    onChange({ ...node, rules: rules.filter((_, i) => i !== index) });
  return (
    <Box
      sx={{
        border: "1px solid",
        borderColor: depth ? "#D8DDEA" : "#C9D0E2",
        borderRadius: 2,
        p: 1.5,
        bgcolor: depth ? "#FAFBFD" : "white",
      }}
    >
      <Stack direction="row" alignItems="center" gap={1} mb={1.25}>
        <Typography variant="caption" color="text.secondary" fontWeight={700}>
          조건 그룹
        </Typography>
        <TextField
          select
          size="small"
          value={node.combinator || "and"}
          onChange={(event) =>
            onChange({
              ...node,
              combinator: event.target.value as "and" | "or",
            })
          }
          sx={{ width: 92 }}
        >
          <MenuItem value="and">AND</MenuItem>
          <MenuItem value="or">OR</MenuItem>
        </TextField>
        <Box sx={{ flex: 1 }} />
        {onDelete && (
          <IconButton size="small" color="error" onClick={onDelete}>
            <DeleteOutlineRounded fontSize="small" />
          </IconButton>
        )}
      </Stack>
      <Stack spacing={1}>
        {rules.map((rule, index) =>
          rule.rules ? (
            <GroupEditor
              key={index}
              node={rule}
              fields={fields}
              depth={depth + 1}
              onChange={(next) => updateRule(index, next)}
              onDelete={() => deleteRule(index)}
            />
          ) : (
            <Stack
              key={index}
              direction={{ xs: "column", md: "row" }}
              spacing={1}
              alignItems={{ md: "center" }}
            >
              <TextField
                select
                size="small"
                label="Dimension"
                value={rule.field || "event.name"}
                onChange={(event) =>
                  updateRule(index, { ...rule, field: event.target.value })
                }
                sx={{ minWidth: 210 }}
              >
                {fields.map((field) => (
                  <MenuItem key={field} value={field}>
                    {field}
                  </MenuItem>
                ))}
              </TextField>
              <TextField
                select
                size="small"
                label="Operator"
                value={rule.operator || "="}
                onChange={(event) =>
                  updateRule(index, { ...rule, operator: event.target.value })
                }
                sx={{ minWidth: 155 }}
              >
                {operators.map((operator) => (
                  <MenuItem key={operator} value={operator}>
                    {operator}
                  </MenuItem>
                ))}
              </TextField>
              <TextField
                size="small"
                label={
                  rule.operator === "in" || rule.operator === "not in"
                    ? "값 (쉼표 구분)"
                    : "값"
                }
                value={valueForInput(rule)}
                disabled={
                  rule.operator === "exists" || rule.operator === "not exists"
                }
                onChange={(event) => {
                  const value =
                    rule.operator === "in" || rule.operator === "not in"
                      ? event.target.value
                          .split(",")
                          .map((item) => item.trim())
                          .filter(Boolean)
                      : event.target.value;
                  updateRule(index, { ...rule, value });
                }}
                sx={{ flex: 1 }}
              />
              <IconButton
                size="small"
                disabled={rules.length <= 1}
                onClick={() => deleteRule(index)}
              >
                <DeleteOutlineRounded fontSize="small" />
              </IconButton>
            </Stack>
          ),
        )}
      </Stack>
      <Stack direction="row" gap={1} mt={1.25}>
        <Button
          size="small"
          startIcon={<AddRounded />}
          onClick={() => onChange({ ...node, rules: [...rules, emptyRule()] })}
        >
          조건 추가
        </Button>
        <Button
          size="small"
          startIcon={<AccountTreeRounded />}
          disabled={depth >= 4}
          onClick={() =>
            onChange({ ...node, rules: [...rules, emptySegment()] })
          }
        >
          하위 그룹
        </Button>
      </Stack>
    </Box>
  );
}

export default function SegmentBuilder({
  value,
  onChange,
  customFields = [],
}: {
  value: SegmentNode;
  onChange(next: SegmentNode): void;
  customFields?: string[];
}) {
  const fields = [...builtInSegmentFields, ...customFields];
  return (
    <GroupEditor node={value} fields={fields} depth={0} onChange={onChange} />
  );
}
