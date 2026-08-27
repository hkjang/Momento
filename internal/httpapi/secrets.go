package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/auth"
	"github.com/hkjang/Momento/internal/secret"
)

// sealSecret returns the database value for a generated key. Without an
// encryption key nothing is stored, which keeps the previous behaviour where a
// key is shown once and only its hash is kept.
func (s *Server) sealSecret(value string) any {
	if !s.Secrets.Enabled() || value == "" {
		return nil
	}
	sealed, err := s.Secrets.Encrypt(value)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Error("sealing secret failed", "error", err)
		}
		return nil
	}
	return sealed
}

// openSecret reads a stored secret back. The boolean reports whether a value is
// available at all so callers can explain why a key cannot be shown.
func (s *Server) openSecret(stored *string) (string, error) {
	if stored == nil || *stored == "" {
		return "", nil
	}
	return s.Secrets.Decrypt(*stored)
}

func (s *Server) secretUnavailableReason(err error) string {
	switch {
	case errors.Is(err, secret.ErrDisabled):
		return "MOMENTO_ENCRYPTION_KEY is not configured, so this key cannot be recovered. Rotate the key after setting it."
	case errors.Is(err, secret.ErrUnknownKey):
		return "This key was sealed with a different MOMENTO_ENCRYPTION_KEY. Restore the previous key in MOMENTO_ENCRYPTION_KEY_PREVIOUS or rotate the key."
	case err != nil:
		return err.Error()
	default:
		return "This key was created before secret storage was enabled. Rotate it once to store it recoverably."
	}
}

// encryptionStatus tells the console whether secrets survive a restart.
func (s *Server) encryptionStatus(w http.ResponseWriter, r *http.Request) {
	preventCaching(w)
	out := map[string]any{
		"enabled":            s.Secrets.Enabled(),
		"algorithm":          "AES-256-GCM",
		"key_id":             s.Secrets.KeyID(),
		"previous_key_ids":   s.Secrets.PreviousKeyIDs(),
		"environment_hint":   "MOMENTO_ENCRYPTION_KEY",
		"recoverable_keys":   0,
		"unrecoverable_keys": 0,
		"pending_reseal":     0,
	}
	if s.DB == nil {
		writeJSON(w, 200, out)
		return
	}
	recoverable, unrecoverable, pending := 0, 0, 0
	count := func(query string) {
		rows, err := s.DB.Query(r.Context(), query)
		if err != nil {
			return
		}
		defer rows.Close()
		for rows.Next() {
			var stored *string
			if rows.Scan(&stored) != nil {
				continue
			}
			if stored == nil || *stored == "" {
				unrecoverable++
				continue
			}
			if _, err := s.Secrets.Decrypt(*stored); err != nil {
				unrecoverable++
				continue
			}
			recoverable++
			if s.Secrets.NeedsReseal(*stored) {
				pending++
			}
		}
	}
	count(`SELECT tracking_key_secret FROM sites`)
	count(`SELECT server_api_key_secret FROM sites`)
	count(`SELECT token_secret FROM api_keys WHERE revoked_at IS NULL`)
	out["recoverable_keys"] = recoverable
	out["unrecoverable_keys"] = unrecoverable
	out["pending_reseal"] = pending
	writeJSON(w, 200, out)
}

// revealMyKey shows a personal API key again instead of forcing a rotation.
func (s *Server) revealMyKey(w http.ResponseWriter, r *http.Request) {
	preventCaching(w)
	p, _ := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "INVALID_ID", "invalid key id")
		return
	}
	var stored *string
	var name, prefix string
	if s.DB.QueryRow(r.Context(), `SELECT name,key_prefix,token_secret FROM api_keys WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, id, p.ID).Scan(&name, &prefix, &stored) != nil {
		writeError(w, 404, "NOT_FOUND", "key not found")
		return
	}
	plain, err := s.openSecret(stored)
	if err != nil || plain == "" {
		writeJSON(w, 200, map[string]any{"available": false, "name": name, "prefix": prefix, "reason": s.secretUnavailableReason(err)})
		return
	}
	s.audit(r.Context(), &p, "api_key.reveal", "api_key", id.String(), map[string]any{"name": name}, clientIP(r))
	writeJSON(w, 200, map[string]any{"available": true, "name": name, "prefix": prefix, "key": plain})
}

// revealSiteKeys shows the tracking and server keys of a site again.
func (s *Server) revealSiteKeys(w http.ResponseWriter, r *http.Request) {
	preventCaching(w)
	p, _ := auth.FromContext(r.Context())
	id, parsed, err := s.resolveSiteByID(r, "id")
	if !parsed {
		writeError(w, 400, "INVALID_ID", "invalid site id")
		return
	}
	var siteKey string
	var tracking, server *string
	if err != nil || s.DB.QueryRow(r.Context(), `SELECT site_key,tracking_key_secret,server_api_key_secret FROM sites WHERE id=$1`, id).Scan(&siteKey, &tracking, &server) != nil {
		writeError(w, 404, "NOT_FOUND", "site not found")
		return
	}
	trackingPlain, trackingErr := s.openSecret(tracking)
	serverPlain, serverErr := s.openSecret(server)
	out := map[string]any{
		"site_id":                  siteKey,
		"encryption_enabled":       s.Secrets.Enabled(),
		"tracking_key_available":   trackingPlain != "",
		"server_api_key_available": serverPlain != "",
	}
	if trackingPlain != "" {
		out["tracking_key"] = trackingPlain
	} else {
		out["tracking_key_reason"] = s.secretUnavailableReason(trackingErr)
	}
	if serverPlain != "" {
		out["server_api_key"] = serverPlain
	} else {
		out["server_api_key_reason"] = s.secretUnavailableReason(serverErr)
	}
	if trackingPlain != "" || serverPlain != "" {
		s.audit(r.Context(), &p, "site.keys.reveal", "site", id.String(), map[string]any{"site_id": siteKey}, clientIP(r))
	}
	writeJSON(w, 200, out)
}

// rekeySecrets re-seals every stored secret with the current primary key, which
// is the final step of an encryption key rotation.
func (s *Server) rekeySecrets(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	if !auth.RoleAtLeast(p.Role, "organization_admin") {
		writeError(w, 403, "FORBIDDEN", "organization administrator permission required")
		return
	}
	if !s.Secrets.Enabled() {
		writeError(w, 400, "ENCRYPTION_DISABLED", "set MOMENTO_ENCRYPTION_KEY before rotating stored secrets")
		return
	}
	resealed, failed := 0, 0
	for _, target := range []struct{ table, column string }{
		{"sites", "tracking_key_secret"},
		{"sites", "server_api_key_secret"},
		{"api_keys", "token_secret"},
		{"delivery_channels", "headers_secret"},
	} {
		ok, bad := s.resealColumn(r.Context(), target.table, target.column)
		resealed += ok
		failed += bad
	}
	if ok, bad := s.resealOIDCSecret(r.Context()); ok {
		resealed++
	} else if bad {
		failed++
	}
	s.audit(r.Context(), &p, "encryption.rekey", "setting", "encryption", map[string]any{"resealed": resealed, "failed": failed}, clientIP(r))
	writeJSON(w, 200, map[string]any{"resealed": resealed, "failed": failed, "key_id": s.Secrets.KeyID()})
}

func (s *Server) resealColumn(ctx context.Context, table, column string) (int, int) {
	// table and column come from a fixed list above, never from user input.
	rows, err := s.DB.Query(ctx, `SELECT id,`+column+` FROM `+table+` WHERE `+column+` IS NOT NULL`)
	if err != nil {
		return 0, 0
	}
	type pending struct {
		id    uuid.UUID
		value string
	}
	updates := []pending{}
	failed := 0
	for rows.Next() {
		var id uuid.UUID
		var stored string
		if rows.Scan(&id, &stored) != nil {
			continue
		}
		if !s.Secrets.NeedsReseal(stored) {
			continue
		}
		plain, err := s.Secrets.Decrypt(stored)
		if err != nil {
			failed++
			continue
		}
		sealed, err := s.Secrets.Encrypt(plain)
		if err != nil {
			failed++
			continue
		}
		updates = append(updates, pending{id: id, value: sealed})
	}
	rows.Close()
	resealed := 0
	for _, item := range updates {
		if _, err := s.DB.Exec(ctx, `UPDATE `+table+` SET `+column+`=$2 WHERE id=$1`, item.id, item.value); err == nil {
			resealed++
		} else {
			failed++
		}
	}
	return resealed, failed
}

func (s *Server) resealOIDCSecret(ctx context.Context) (bool, bool) {
	var raw []byte
	if s.DB.QueryRow(ctx, `SELECT value FROM settings WHERE key='oidc'`).Scan(&raw) != nil {
		return false, false
	}
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return false, false
	}
	stored, _ := value["client_secret"].(string)
	if stored == "" || !s.Secrets.NeedsReseal(stored) {
		return false, false
	}
	plain, err := s.Secrets.Decrypt(stored)
	if err != nil {
		return false, true
	}
	sealed, err := s.Secrets.Encrypt(plain)
	if err != nil {
		return false, true
	}
	value["client_secret"] = sealed
	body, _ := json.Marshal(value)
	if _, err := s.DB.Exec(ctx, `UPDATE settings SET value=$1,updated_at=now() WHERE key='oidc'`, body); err != nil {
		return false, true
	}
	return true, false
}
