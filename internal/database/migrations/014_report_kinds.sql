-- The visitor insight delivery (v0.10) and the anomaly alert (v0.11) were accepted
-- by the API but rejected by this constraint, so neither could ever be configured.
-- The constraint now matches the report kinds the service actually builds.
ALTER TABLE scheduled_reports DROP CONSTRAINT IF EXISTS scheduled_reports_report_kind_check;
ALTER TABLE scheduled_reports ADD CONSTRAINT scheduled_reports_report_kind_check
  CHECK (report_kind IN ('overview','adoption','experience','ai','segment','insights','visitor_insight','anomaly'));
