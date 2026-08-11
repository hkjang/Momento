-- Recoverable secret storage. When MOMENTO_ENCRYPTION_KEY is configured Momento
-- keeps a sealed copy of every generated key so a restart never loses it and an
-- administrator never has to rotate a key just to see it again.
ALTER TABLE sites ADD COLUMN IF NOT EXISTS tracking_key_secret text;
ALTER TABLE sites ADD COLUMN IF NOT EXISTS server_api_key_secret text;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS token_secret text;
ALTER TABLE delivery_channels ADD COLUMN IF NOT EXISTS headers_secret text;

-- Origins allowed in the console Content-Security-Policy connect-src directive.
UPDATE settings SET value = value || '{"additional_connect_origins":[]}'::jsonb
WHERE key = 'security' AND NOT value ? 'additional_connect_origins';
