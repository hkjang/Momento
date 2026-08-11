-- Anomaly alerting delivers only when something is actually wrong, so a delivery
-- run needs a third outcome: nothing to report.
ALTER TABLE delivery_runs DROP CONSTRAINT IF EXISTS delivery_runs_status_check;
ALTER TABLE delivery_runs ADD CONSTRAINT delivery_runs_status_check
  CHECK (status IN ('success','failed','skipped'));
