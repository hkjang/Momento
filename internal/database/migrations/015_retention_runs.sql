-- Retention runs unattended every hour and left no evidence anywhere. The screen
-- showed the policy and when someone last edited it; a failing pass produced one
-- line on stderr. An operator in a closed network without a log pipeline had no
-- way to tell whether their retention obligation was being met, or whether the
-- job had stopped a month ago while the disk filled.
CREATE TABLE retention_runs (
  id bigserial PRIMARY KEY,
  started_at timestamptz NOT NULL,
  finished_at timestamptz NOT NULL DEFAULT now(),
  status text NOT NULL CHECK (status IN ('success','failed')),
  -- Rows removed per table, so a pass that did nothing is distinguishable from
  -- one that never ran.
  removed jsonb NOT NULL DEFAULT '{}',
  error text
);
CREATE INDEX retention_runs_started_idx ON retention_runs(started_at DESC);
