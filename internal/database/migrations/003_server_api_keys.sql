ALTER TABLE sites ADD COLUMN IF NOT EXISTS server_api_key_hash text NOT NULL DEFAULT '';
ALTER TABLE sites ADD COLUMN IF NOT EXISTS server_api_key_prefix text NOT NULL DEFAULT '';
