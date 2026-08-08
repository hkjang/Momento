CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE organizations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  slug text NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE workspaces (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE users (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email text NOT NULL UNIQUE,
  display_name text NOT NULL,
  department text NOT NULL DEFAULT '',
  organization_name text NOT NULL DEFAULT '',
  password_hash text,
  role text NOT NULL DEFAULT 'viewer' CHECK (role IN ('super_admin','organization_admin','workspace_admin','analyst','viewer')),
  oidc_subject text UNIQUE,
  active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE user_workspace_roles (
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  role text NOT NULL CHECK (role IN ('workspace_admin','analyst','viewer')),
  PRIMARY KEY(user_id, workspace_id)
);
CREATE TABLE sites (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  site_key text NOT NULL UNIQUE,
  name text NOT NULL,
  service_name text NOT NULL DEFAULT '',
  tracking_key_hash text NOT NULL,
  tracking_key_prefix text NOT NULL,
  server_api_key_hash text NOT NULL,
  server_api_key_prefix text NOT NULL,
  allowed_domains text[] NOT NULL DEFAULT '{}',
  session_timeout_minutes integer NOT NULL DEFAULT 30 CHECK (session_timeout_minutes BETWEEN 1 AND 1440),
  active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE settings (
  key text PRIMARY KEY,
  value jsonb NOT NULL,
  secret boolean NOT NULL DEFAULT false,
  updated_by uuid REFERENCES users(id),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE site_settings (
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  key text NOT NULL,
  value jsonb NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(site_id, key)
);
CREATE TABLE network_ranges (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  cidr cidr NOT NULL,
  description text NOT NULL DEFAULT '',
  internal boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE api_keys (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name text NOT NULL,
  key_hash text NOT NULL UNIQUE,
  key_prefix text NOT NULL,
  scopes text[] NOT NULL DEFAULT ARRAY['analytics:read'],
  expires_at timestamptz,
  last_used_at timestamptz,
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE user_sessions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash text NOT NULL UNIQUE,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE oidc_states (
  state_hash text PRIMARY KEY,
  verifier text NOT NULL,
  return_to text NOT NULL DEFAULT '/',
  expires_at timestamptz NOT NULL
);
CREATE TABLE raw_events (
  id bigserial PRIMARY KEY,
  event_id uuid NOT NULL,
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  event_name text NOT NULL,
  event_timestamp timestamptz NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now(),
  visitor_id text NOT NULL,
  session_id text NOT NULL,
  user_id text,
  page_url text,
  page_title text,
  referrer text,
  source text,
  medium text,
  campaign text,
  device_type text,
  browser text,
  os text,
  language text,
  screen text,
  user_agent text,
  country text,
  client_ip inet,
  network_name text,
  properties jsonb NOT NULL DEFAULT '{}',
  user_properties jsonb NOT NULL DEFAULT '{}',
  is_conversion boolean NOT NULL DEFAULT false,
  is_internal boolean NOT NULL DEFAULT false,
  traffic_class text NOT NULL DEFAULT 'normal',
  UNIQUE(site_id, event_id)
);
CREATE INDEX raw_events_site_time_idx ON raw_events(site_id, event_timestamp DESC);
CREATE INDEX raw_events_site_name_time_idx ON raw_events(site_id, event_name, event_timestamp DESC);
CREATE INDEX raw_events_visitor_idx ON raw_events(site_id, visitor_id, event_timestamp DESC);
CREATE INDEX raw_events_properties_gin_idx ON raw_events USING gin(properties);
CREATE INDEX raw_events_user_properties_gin_idx ON raw_events USING gin(user_properties);
CREATE TABLE event_inbox (
  id bigserial PRIMARY KEY,
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  payload jsonb NOT NULL,
  attempts integer NOT NULL DEFAULT 0,
  available_at timestamptz NOT NULL DEFAULT now(),
  processed_at timestamptz,
  last_error text,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX event_inbox_pending_idx ON event_inbox(available_at) WHERE processed_at IS NULL;
CREATE TABLE event_dead_letters (
  id bigserial PRIMARY KEY,
  inbox_id bigint NOT NULL,
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  payload jsonb NOT NULL,
  error text NOT NULL,
  failed_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX event_dead_letters_site_time_idx ON event_dead_letters(site_id,failed_at DESC);
CREATE TABLE event_definitions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  schema jsonb NOT NULL DEFAULT '{}',
  validation_mode text NOT NULL DEFAULT 'warn' CHECK(validation_mode IN ('allow','warn','reject')),
  conversion boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(site_id, name)
);
CREATE TABLE dimensions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name text NOT NULL,
  property_key text NOT NULL,
  scope text NOT NULL CHECK(scope IN ('user','session','event','item')),
  active boolean NOT NULL DEFAULT true,
  UNIQUE(site_id, name)
);
CREATE TABLE saved_reports (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  owner_id uuid REFERENCES users(id) ON DELETE SET NULL,
  kind text NOT NULL,
  name text NOT NULL,
  definition jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE audit_logs (
  id bigserial PRIMARY KEY,
  actor_id uuid REFERENCES users(id) ON DELETE SET NULL,
  action text NOT NULL,
  resource_type text NOT NULL,
  resource_id text,
  detail jsonb NOT NULL DEFAULT '{}',
  client_ip inet,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_logs_time_idx ON audit_logs(created_at DESC);

INSERT INTO settings(key, value) VALUES
 ('general', '{"product_name":"Momento","public_url":"","timezone":"Asia/Seoul"}'),
 ('oidc', '{"enabled":false,"issuer_url":"","client_id":"","client_secret":"","scopes":["openid","profile","email"],"claim_email":"email","claim_name":"name","claim_department":"department","claim_organization":"organization"}'),
 ('privacy', '{"ip_anonymization":true,"collect_user_agent":true,"strip_query_string":false,"masked_parameters":["token","password","email"],"collect_user_id":true,"visitor_profiles":true,"do_not_track":true,"blocked_properties":["email","phone","resident_number"],"raw_event_retention_months":13,"debug_retention_days":7}'),
 ('storage', '{"engine":"postgres","clickhouse_dsn":""}'),
 ('security', '{"collector_rate_limit_per_minute":6000,"max_payload_bytes":262144,"max_events_per_request":100,"trusted_proxy_cidrs":[]}')
ON CONFLICT DO NOTHING;
