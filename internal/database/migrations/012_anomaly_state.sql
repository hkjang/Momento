-- Alert state. Without it an hourly anomaly schedule reports the same drop every
-- hour, so the channel stops being read. Momento remembers what it already said
-- and reports transitions: newly detected, still open, or recovered.
CREATE TABLE IF NOT EXISTS anomaly_alerts (
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  environment text NOT NULL,
  metric text NOT NULL,
  severity text NOT NULL,
  robust_z double precision NOT NULL DEFAULT 0,
  value double precision NOT NULL DEFAULT 0,
  baseline double precision NOT NULL DEFAULT 0,
  first_detected_on date NOT NULL,
  last_detected_on date NOT NULL,
  notified_on date,
  resolved_on date,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (site_id, environment, metric)
);
CREATE INDEX IF NOT EXISTS anomaly_alerts_open_idx ON anomaly_alerts(site_id, environment) WHERE resolved_on IS NULL;
