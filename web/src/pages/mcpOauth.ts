// The MCP SSO card shows an operator the two addresses an MCP client and a
// Keycloak administrator need — the resource identifier (what the Audience
// mapper carries, and the URL a client is given) and the metadata document a
// refused client is pointed at. Both are derived the way the server derives
// them, so what the card shows is what /mcp will actually claim.

export type SettingGroup = Record<string, unknown>;

function text(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

/** The resource identifier: the explicit setting, else Public URL + /mcp. */
export function mcpResource(
  mcpOauth: SettingGroup,
  general: SettingGroup,
): string {
  const explicit = text(mcpOauth.resource);
  if (explicit) return explicit;
  const publicUrl = text(general.public_url).replace(/\/+$/, "");
  return publicUrl ? `${publicUrl}/mcp` : "";
}

/** RFC 9728: the document lives under the origin, at the path-specific address. */
export function mcpMetadataUrl(resource: string): string {
  try {
    const parsed = new URL(resource);
    return `${parsed.origin}/.well-known/oauth-protected-resource/mcp`;
  } catch {
    return "";
  }
}

/**
 * Why the switch, once on, would still behave as off. The server refuses the
 * save without an issuer and falls back to the request's Host header without
 * a public URL; the card says so before the operator finds out from a client.
 */
export function mcpOauthReadiness(
  mcpOauth: SettingGroup,
  oidc: SettingGroup,
  general: SettingGroup,
): { ready: boolean; reason: string } {
  if (!text(oidc.issuer_url))
    return {
      ready: false,
      reason:
        "Keycloak / OIDC 의 Issuer URL 이 비어 있습니다. 토큰을 검증할 발급자가 없으면 저장이 거부됩니다.",
    };
  if (!mcpResource(mcpOauth, general))
    return {
      ready: false,
      reason:
        "Public URL 과 리소스 식별자가 모두 비어 있습니다. 요청의 Host 헤더로 만든 주소를 쓰게 되므로 프록시 뒤에서는 Public URL 을 채우세요.",
    };
  return { ready: true, reason: "" };
}
