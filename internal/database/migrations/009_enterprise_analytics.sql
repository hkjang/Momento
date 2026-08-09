ALTER TABLE raw_events ADD COLUMN session_properties jsonb NOT NULL DEFAULT '{}';
CREATE INDEX raw_events_session_properties_gin_idx ON raw_events USING gin(session_properties);

ALTER TABLE semantic_metrics
  ADD COLUMN owner text NOT NULL DEFAULT '',
  ADD COLUMN entity_scope text NOT NULL DEFAULT 'event' CHECK(entity_scope IN ('event','session','user','item')),
  ADD COLUMN tags text[] NOT NULL DEFAULT '{}';

ALTER TABLE data_quality_daily ADD COLUMN pii_detected bigint NOT NULL DEFAULT 0;

CREATE TABLE metric_goals (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  metric_name text NOT NULL,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  target_value numeric NOT NULL,
  comparator text NOT NULL DEFAULT 'gte' CHECK(comparator IN ('gte','lte')),
  period text NOT NULL DEFAULT 'month' CHECK(period IN ('day','week','month','quarter')),
  environment text NOT NULL DEFAULT 'prd',
  organization text NOT NULL DEFAULT '',
  department text NOT NULL DEFAULT '',
  starts_on date,
  ends_on date,
  owner text NOT NULL DEFAULT '',
  active boolean NOT NULL DEFAULT true,
  created_by uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(site_id,name)
);
CREATE INDEX metric_goals_site_idx ON metric_goals(site_id,active,updated_at DESC);

CREATE TABLE query_policies (
  site_id uuid PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
  max_exact_days integer NOT NULL DEFAULT 180 CHECK(max_exact_days BETWEEN 1 AND 3650),
  max_complexity_score integer NOT NULL DEFAULT 90 CHECK(max_complexity_score BETWEEN 10 AND 500),
  background_threshold integer NOT NULL DEFAULT 60 CHECK(background_threshold BETWEEN 10 AND 500),
  fast_sample_percent numeric NOT NULL DEFAULT 10 CHECK(fast_sample_percent BETWEEN 0.1 AND 100),
  preview_sample_percent numeric NOT NULL DEFAULT 1 CHECK(preview_sample_percent BETWEEN 0.1 AND 100),
  updated_by uuid REFERENCES users(id) ON DELETE SET NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO query_policies(site_id) SELECT id FROM sites ON CONFLICT DO NOTHING;

CREATE TABLE query_audit (
  id bigserial PRIMARY KEY,
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  actor_id uuid REFERENCES users(id) ON DELETE SET NULL,
  environment text NOT NULL,
  query_mode text NOT NULL CHECK(query_mode IN ('exact','fast','preview')),
  complexity_score integer NOT NULL,
  sample_percent numeric NOT NULL,
  date_from timestamptz NOT NULL,
  date_to timestamptz NOT NULL,
  dimensions text[] NOT NULL DEFAULT '{}',
  metrics text[] NOT NULL DEFAULT '{}',
  duration_ms integer,
  result_rows integer,
  status text NOT NULL CHECK(status IN ('running','success','rejected','failed')),
  error text,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX query_audit_site_time_idx ON query_audit(site_id,created_at DESC);

CREATE TABLE aggregate_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  environment text NOT NULL DEFAULT 'prd',
  job_type text NOT NULL CHECK(job_type IN ('late_event','date_range','full_rebuild')),
  date_from date,
  date_to date,
  status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','success','failed')),
  reason text NOT NULL DEFAULT '',
  requested_by uuid REFERENCES users(id) ON DELETE SET NULL,
  attempts integer NOT NULL DEFAULT 0,
  started_at timestamptz,
  finished_at timestamptz,
  error text,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX aggregate_jobs_pending_idx ON aggregate_jobs(created_at) WHERE status='pending';
CREATE INDEX aggregate_jobs_site_idx ON aggregate_jobs(site_id,created_at DESC);
CREATE UNIQUE INDEX aggregate_jobs_late_dedupe_idx ON aggregate_jobs(site_id,environment,date_from)
  WHERE job_type='late_event' AND status IN ('pending','running');

CREATE TABLE analytics_annotations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id uuid REFERENCES sites(id) ON DELETE CASCADE,
  workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  environment text NOT NULL DEFAULT 'prd',
  occurred_at timestamptz NOT NULL,
  ended_at timestamptz,
  kind text NOT NULL CHECK(kind IN ('deployment','release','incident','campaign','training','feature_flag','organization','manual')),
  title text NOT NULL,
  description text NOT NULL DEFAULT '',
  source text NOT NULL DEFAULT 'manual',
  metadata jsonb NOT NULL DEFAULT '{}',
  created_by uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX analytics_annotations_workspace_time_idx ON analytics_annotations(workspace_id,occurred_at DESC);
CREATE INDEX analytics_annotations_site_time_idx ON analytics_annotations(site_id,occurred_at DESC);

CREATE TABLE workspace_journeys (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  owner_id uuid REFERENCES users(id) ON DELETE SET NULL,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  steps jsonb NOT NULL,
  conversion_window_days integer NOT NULL DEFAULT 30 CHECK(conversion_window_days BETWEEN 1 AND 365),
  shared boolean NOT NULL DEFAULT false,
  active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(workspace_id,owner_id,name)
);
CREATE INDEX workspace_journeys_workspace_idx ON workspace_journeys(workspace_id,updated_at DESC);

CREATE TABLE feature_flags (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  flag_key text NOT NULL CHECK(flag_key ~ '^[A-Za-z][A-Za-z0-9_.-]{0,127}$'),
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  variants jsonb NOT NULL DEFAULT '[]',
  status text NOT NULL DEFAULT 'active' CHECK(status IN ('draft','active','paused','archived')),
  starts_at timestamptz,
  ends_at timestamptz,
  owner text NOT NULL DEFAULT '',
  created_by uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(site_id,flag_key)
);

CREATE TABLE experiments (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  feature_flag_id uuid REFERENCES feature_flags(id) ON DELETE SET NULL,
  experiment_key text NOT NULL CHECK(experiment_key ~ '^[A-Za-z][A-Za-z0-9_.-]{0,127}$'),
  name text NOT NULL,
  hypothesis text NOT NULL DEFAULT '',
  primary_metric text NOT NULL,
  variants jsonb NOT NULL,
  audience jsonb NOT NULL DEFAULT '{}',
  environment text NOT NULL DEFAULT 'prd',
  status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','running','completed','archived')),
  starts_at timestamptz,
  ends_at timestamptz,
  owner text NOT NULL DEFAULT '',
  created_by uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(site_id,experiment_key)
);
CREATE INDEX experiments_site_idx ON experiments(site_id,status,updated_at DESC);

CREATE TABLE privacy_requests (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  request_type text NOT NULL CHECK(request_type IN ('delete','export','property_delete')),
  identity_type text NOT NULL CHECK(identity_type IN ('user_id','visitor_id','period')),
  identity_value text NOT NULL DEFAULT '',
  date_from date,
  date_to date,
  status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','approved','running','completed','rejected','failed')),
  reason text NOT NULL DEFAULT '',
  requested_by uuid REFERENCES users(id) ON DELETE SET NULL,
  approved_by uuid REFERENCES users(id) ON DELETE SET NULL,
  result jsonb NOT NULL DEFAULT '{}',
  requested_at timestamptz NOT NULL DEFAULT now(),
  approved_at timestamptz,
  completed_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX privacy_requests_site_idx ON privacy_requests(site_id,requested_at DESC);

UPDATE settings SET value=value || '{"pii_detection_mode":"mask"}'::jsonb WHERE key='privacy';
