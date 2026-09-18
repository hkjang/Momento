-- MCP with SSO: the settings group the console's "MCP SSO(OAuth)" card edits.
-- Off by default, so a deployment that never opens the card is unchanged. The
-- issuer and email claim are reused from the oidc group; the resource, when
-- empty, is general.public_url plus /mcp.
INSERT INTO settings(key,value) VALUES
('mcp.oauth','{"enabled":false,"resource":"","audience":"","scopes":"analytics:read"}'::jsonb)
ON CONFLICT(key) DO NOTHING;
