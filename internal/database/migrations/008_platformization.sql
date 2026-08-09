ALTER TABLE raw_events
  ADD COLUMN environment text NOT NULL DEFAULT 'prd',
  ADD COLUMN contract_version integer NOT NULL DEFAULT 1 CHECK(contract_version > 0);
CREATE INDEX raw_events_environment_time_idx ON raw_events(site_id,environment,event_timestamp DESC);
CREATE INDEX raw_events_release_idx ON raw_events(site_id,environment,(properties->>'release_version'),event_timestamp DESC)
  WHERE properties ? 'release_version';

ALTER TABLE sessions ADD COLUMN environment text NOT NULL DEFAULT 'prd';
ALTER TABLE sessions DROP CONSTRAINT sessions_pkey;
ALTER TABLE sessions ADD PRIMARY KEY(site_id,environment,session_id);
CREATE INDEX sessions_environment_time_idx ON sessions(site_id,environment,last_event_at DESC);

ALTER TABLE daily_site_metrics ADD COLUMN environment text NOT NULL DEFAULT 'prd';
ALTER TABLE daily_site_metrics DROP CONSTRAINT daily_site_metrics_pkey;
ALTER TABLE daily_site_metrics ADD PRIMARY KEY(site_id,event_date,environment);

ALTER TABLE daily_site_visitors ADD COLUMN environment text NOT NULL DEFAULT 'prd';
ALTER TABLE daily_site_visitors DROP CONSTRAINT daily_site_visitors_pkey;
ALTER TABLE daily_site_visitors ADD PRIMARY KEY(site_id,event_date,environment,visitor_id);

ALTER TABLE daily_site_sessions ADD COLUMN environment text NOT NULL DEFAULT 'prd';
ALTER TABLE daily_site_sessions DROP CONSTRAINT daily_site_sessions_pkey;
ALTER TABLE daily_site_sessions ADD PRIMARY KEY(site_id,event_date,environment,session_id);

DROP VIEW analytics_events;
CREATE VIEW analytics_events AS
SELECT
  e.*,
  coalesce(i.user_id,e.user_id) AS canonical_user_id,
  CASE WHEN u.user_properties IS NOT NULL AND u.user_properties <> '{}'::jsonb THEN u.user_properties ELSE e.user_properties END AS canonical_user_properties,
  CASE
    WHEN coalesce(i.user_id,e.user_id) IS NOT NULL THEN 'u:' || coalesce(i.user_id,e.user_id)
    ELSE 'v:' || e.visitor_id
  END AS entity_id
FROM raw_events e
LEFT JOIN visitor_identities i ON i.site_id=e.site_id AND i.visitor_id=e.visitor_id
LEFT JOIN identified_users u ON u.site_id=e.site_id AND u.user_id=coalesce(i.user_id,e.user_id);

CREATE TABLE site_environments (
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name text NOT NULL CHECK(name ~ '^[a-z][a-z0-9_-]{0,31}$'),
  label text NOT NULL,
  contract_mode text NOT NULL DEFAULT 'warn' CHECK(contract_mode IN ('allow','warn','reject')),
  cardinality_limit integer NOT NULL DEFAULT 10000 CHECK(cardinality_limit BETWEEN 100 AND 10000000),
  active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(site_id,name)
);
INSERT INTO site_environments(site_id,name,label,contract_mode,cardinality_limit)
SELECT id,v.name,v.label,v.mode,v.cardinality
FROM sites CROSS JOIN (VALUES
  ('dev','Development','allow',50000),
  ('stg','Staging','warn',25000),
  ('prd','Production','warn',10000)
) AS v(name,label,mode,cardinality);

ALTER TABLE event_definitions
  ADD COLUMN current_version integer NOT NULL DEFAULT 1 CHECK(current_version > 0),
  ADD COLUMN owner text NOT NULL DEFAULT '',
  ADD COLUMN deprecated boolean NOT NULL DEFAULT false,
  ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

CREATE TABLE event_contract_versions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id uuid NOT NULL,
  event_name text NOT NULL,
  version integer NOT NULL CHECK(version > 0),
  schema jsonb NOT NULL DEFAULT '{}',
  validation_mode text NOT NULL DEFAULT 'warn' CHECK(validation_mode IN ('allow','warn','reject')),
  status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','active','deprecated')),
  changelog text NOT NULL DEFAULT '',
  created_by uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  activated_at timestamptz,
  UNIQUE(site_id,event_name,version),
  FOREIGN KEY(site_id,event_name) REFERENCES event_definitions(site_id,name) ON DELETE CASCADE
);
INSERT INTO event_contract_versions(site_id,event_name,version,schema,validation_mode,status,activated_at)
SELECT site_id,name,1,schema,validation_mode,'active',now() FROM event_definitions;
CREATE INDEX event_contract_versions_lookup_idx ON event_contract_versions(site_id,event_name,status,version DESC);

CREATE TABLE semantic_metrics (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name text NOT NULL CHECK(name ~ '^[a-z][a-z0-9_]{0,63}$'),
  label text NOT NULL,
  description text NOT NULL DEFAULT '',
  definition jsonb NOT NULL,
  format text NOT NULL DEFAULT 'number' CHECK(format IN ('number','percent','duration','currency')),
  unit text NOT NULL DEFAULT '',
  definition_version integer NOT NULL DEFAULT 1 CHECK(definition_version > 0),
  status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','deprecated')),
  created_by uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(site_id,name)
);
INSERT INTO semantic_metrics(site_id,name,label,description,definition,format)
SELECT s.id,m.name,m.label,m.description,m.definition::jsonb,m.format
FROM sites s CROSS JOIN (VALUES
  ('events','Events','수집된 이벤트 수','{"type":"count"}','number'),
  ('users','Users','Canonical 사용자 수','{"type":"unique_users"}','number'),
  ('sessions','Sessions','세션 수','{"type":"unique_sessions"}','number'),
  ('page_views','Page Views','페이지 조회 수','{"type":"count","event_name":"page_view"}','number'),
  ('conversions','Conversions','전환 이벤트 수','{"type":"count","conversion":true}','number'),
  ('conversion_users','Conversion Users','전환 사용자 수','{"type":"unique_users","conversion":true}','number'),
  ('revenue','Revenue','구매 매출 합계','{"type":"sum","event_name":"purchase","property":"value","fallback_property":"revenue"}','currency')
) AS m(name,label,description,definition,format);

CREATE TABLE data_quality_daily (
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  event_date date NOT NULL,
  environment text NOT NULL,
  event_name text NOT NULL,
  received bigint NOT NULL DEFAULT 0,
  accepted bigint NOT NULL DEFAULT 0,
  duplicates bigint NOT NULL DEFAULT 0,
  warnings bigint NOT NULL DEFAULT 0,
  rejected bigint NOT NULL DEFAULT 0,
  late_events bigint NOT NULL DEFAULT 0,
  missing_user_id bigint NOT NULL DEFAULT 0,
  missing_feature bigint NOT NULL DEFAULT 0,
  unknown_network bigint NOT NULL DEFAULT 0,
  pii_blocked bigint NOT NULL DEFAULT 0,
  cardinality_violations bigint NOT NULL DEFAULT 0,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(site_id,event_date,environment,event_name)
);
CREATE INDEX data_quality_daily_time_idx ON data_quality_daily(site_id,event_date DESC,environment);

CREATE TABLE data_quality_dimension_values (
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  event_date date NOT NULL,
  environment text NOT NULL,
  dimension text NOT NULL,
  value_hash text NOT NULL,
  first_seen timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(site_id,event_date,environment,dimension,value_hash)
);

CREATE TABLE data_quality_issues (
  id bigserial PRIMARY KEY,
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  environment text NOT NULL,
  event_name text NOT NULL DEFAULT '',
  code text NOT NULL,
  severity text NOT NULL CHECK(severity IN ('info','warning','error')),
  message text NOT NULL,
  sample jsonb NOT NULL DEFAULT '{}',
  occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX data_quality_issues_site_time_idx ON data_quality_issues(site_id,occurred_at DESC);

INSERT INTO data_quality_daily(site_id,event_date,environment,event_name,received,accepted,missing_user_id,missing_feature,unknown_network)
SELECT e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date,e.environment,e.event_name,
  count(*),count(*),count(*) FILTER(WHERE e.user_id IS NULL),count(*) FILTER(WHERE coalesce(e.properties->>'feature','')=''),
  count(*) FILTER(WHERE e.network_name='External / Unclassified')
FROM raw_events e JOIN sites s ON s.id=e.site_id
GROUP BY e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date,e.environment,e.event_name;

CREATE TABLE business_journeys (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  owner_id uuid REFERENCES users(id) ON DELETE SET NULL,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  steps jsonb NOT NULL,
  conversion_window_days integer NOT NULL DEFAULT 30 CHECK(conversion_window_days BETWEEN 1 AND 365),
  shared boolean NOT NULL DEFAULT false,
  active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(site_id,owner_id,name)
);
CREATE INDEX business_journeys_site_idx ON business_journeys(site_id,updated_at DESC);

CREATE TABLE adoption_targets (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  organization text NOT NULL DEFAULT '',
  department text NOT NULL DEFAULT '',
  feature text NOT NULL,
  eligible_users integer NOT NULL CHECK(eligible_users >= 0),
  updated_by uuid REFERENCES users(id) ON DELETE SET NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(site_id,organization,department,feature)
);

CREATE TABLE delivery_channels (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name text NOT NULL,
  channel_type text NOT NULL CHECK(channel_type IN ('webhook','confluence','mail','internal_message','ai_agent')),
  endpoint_url text NOT NULL,
  headers jsonb NOT NULL DEFAULT '{}',
  active boolean NOT NULL DEFAULT true,
  created_by uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(site_id,name)
);

CREATE TABLE scheduled_reports (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  channel_id uuid NOT NULL REFERENCES delivery_channels(id) ON DELETE CASCADE,
  name text NOT NULL,
  report_kind text NOT NULL CHECK(report_kind IN ('overview','adoption','experience','ai','segment','insights')),
  definition jsonb NOT NULL DEFAULT '{}',
  interval_minutes integer NOT NULL CHECK(interval_minutes BETWEEN 5 AND 525600),
  next_run_at timestamptz NOT NULL DEFAULT now(),
  enabled boolean NOT NULL DEFAULT true,
  last_run_at timestamptz,
  last_status text,
  last_error text,
  created_by uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX scheduled_reports_due_idx ON scheduled_reports(next_run_at) WHERE enabled;

CREATE TABLE delivery_runs (
  id bigserial PRIMARY KEY,
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  report_id uuid REFERENCES scheduled_reports(id) ON DELETE SET NULL,
  channel_id uuid REFERENCES delivery_channels(id) ON DELETE SET NULL,
  status text NOT NULL CHECK(status IN ('success','failed')),
  response_status integer,
  error text,
  started_at timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz
);
CREATE INDEX delivery_runs_site_time_idx ON delivery_runs(site_id,started_at DESC);

INSERT INTO settings(key,value) VALUES
('automation','{"enabled":false,"allowed_webhook_hosts":[],"delivery_timeout_seconds":10,"max_entity_ids":0}'::jsonb)
ON CONFLICT(key) DO NOTHING;
