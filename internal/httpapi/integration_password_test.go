package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/auth"
)

// bcrypt refuses a password longer than 72 bytes. Both handlers that store a
// password used to hash without looking at the error, and the profile handler
// then wrote the empty result over the hash the person had: one request with
// a long passphrase — twenty-five Korean characters is enough — locked them out
// for good, since nothing resets a password from the outside. Account creation
// did the same and left the address taken by an account nobody could enter.
func TestAPasswordBcryptRefusesIsRefusedBeforeItReachesTheRow(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	const email = "passphrase@test.local"
	const current = "a-long-enough-password"
	tooLong := strings.Repeat("가", 25) // 75 bytes
	hash, err := auth.HashPassword(current)
	if err != nil {
		t.Fatal(err)
	}
	var userID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,display_name,password_hash,role,organization_name)
		VALUES($1,$1,$2,'viewer','Test') ON CONFLICT(email) DO UPDATE SET password_hash=excluded.password_hash RETURNING id`, email, hash).Scan(&userID); err != nil {
		t.Fatalf("create the user: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE email=$1`, email) })
	const token = "mom_sess_passphrase"
	if _, err := pool.Exec(ctx, `INSERT INTO user_sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '1 hour')
		ON CONFLICT(token_hash) DO UPDATE SET user_id=excluded.user_id,expires_at=excluded.expires_at`, userID, auth.HashToken(token)); err != nil {
		t.Fatalf("create a session: %v", err)
	}
	call := func(token, method, path, body string) (int, string) {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(&http.Cookie{Name: "momento_session", Value: token})
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		return recorder.Code, recorder.Body.String()
	}
	canLogin := func(password string) bool {
		t.Helper()
		_, _, err := f.server.Auth.Login(ctx, email, password)
		return err == nil
	}

	code, body := call(token, http.MethodPatch, "/api/v1/me", mustJSON(t, map[string]any{
		"display_name": "Passphrase", "current_password": current, "new_password": tooLong,
	}))
	if code != http.StatusBadRequest {
		t.Fatalf("a %d-byte new password answered %d, want 400: %s", len(tooLong), code, truncateBody(body))
	}
	if !canLogin(current) {
		t.Fatal("the refused change still replaced the stored hash: the person can no longer sign in with the password they had")
	}

	// The change that is allowed lands, and lands together with the profile.
	const next = "another-long-enough-password"
	code, body = call(token, http.MethodPatch, "/api/v1/me", mustJSON(t, map[string]any{
		"display_name": "Renamed", "current_password": current, "new_password": next,
	}))
	if code != http.StatusOK {
		t.Fatalf("a valid password change answered %d: %s", code, truncateBody(body))
	}
	if canLogin(current) || !canLogin(next) {
		t.Fatal("after a successful change the old password still works or the new one does not")
	}
	var name string
	if err := pool.QueryRow(ctx, `SELECT display_name FROM users WHERE id=$1`, userID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Renamed" {
		t.Fatalf("display name after the same request is %q, want Renamed", name)
	}
	var changed bool
	if err := pool.QueryRow(ctx, `SELECT (detail->>'password_changed')::boolean FROM audit_logs WHERE actor_id=$1 AND action='profile.update' ORDER BY created_at DESC LIMIT 1`, userID).Scan(&changed); err != nil {
		t.Fatalf("read the audit entry: %v", err)
	}
	if !changed {
		t.Fatal("the audit entry for a profile update that changed the password does not say so")
	}

	// Creating an account with such a password is refused rather than inserted
	// without a usable hash.
	const created = "unenterable@test.local"
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE email=$1`, created) })
	code, body = call(f.sessionCook, http.MethodPost, "/api/v1/users", mustJSON(t, map[string]any{
		"email": created, "display_name": "x", "role": "viewer", "password": tooLong,
	}))
	if code != http.StatusBadRequest {
		t.Fatalf("creating a user with a %d-byte password answered %d, want 400: %s", len(tooLong), code, truncateBody(body))
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE email=$1`, created).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatal("the refused account was inserted anyway, with a hash nobody can match")
	}
}
