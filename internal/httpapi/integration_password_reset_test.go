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

// A person who has lost their password, or whose password is no longer
// trusted, needs someone to set a new one. The profile route asks for the
// current password and nothing else set one, so the only way back in was a
// direct write to the users table. This is the administrator's route for it,
// and it is bounded the way the rest of user administration is: not on an
// account with more authority than the caller's, not on the caller's own.
func TestAnAdministratorCanResetAPasswordWithinItsOwnAuthority(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	user := func(email, role, password string) uuid.UUID {
		t.Helper()
		hash, err := auth.HashPassword(password)
		if err != nil {
			t.Fatal(err)
		}
		var id uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO users(email,display_name,password_hash,role,organization_name)
			VALUES($1,$1,$2,$3,'Test') ON CONFLICT(email) DO UPDATE SET role=excluded.role,password_hash=excluded.password_hash RETURNING id`, email, hash, role).Scan(&id); err != nil {
			t.Fatalf("create %s: %v", email, err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE email=$1`, email) })
		return id
	}
	session := func(id uuid.UUID, token string) string {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO user_sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '1 hour')
			ON CONFLICT(token_hash) DO UPDATE SET user_id=excluded.user_id,expires_at=excluded.expires_at`, id, auth.HashToken(token)); err != nil {
			t.Fatalf("create a session: %v", err)
		}
		return token
	}
	call := func(token, method, path, body string) (int, string) {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(&http.Cookie{Name: "momento_session", Value: token})
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		return recorder.Code, recorder.Body.String()
	}
	canLogin := func(email, password string) bool {
		t.Helper()
		_, _, err := f.server.Auth.Login(ctx, email, password)
		return err == nil
	}
	edit := func(role, password string) string {
		return mustJSON(t, map[string]any{"display_name": "x", "department": "", "organization_name": "Test", "role": role, "password": password})
	}

	const lost = "lost-my-password"
	const next = "a-brand-new-password"
	orgAdmin := user("reset-org@test.local", "organization_admin", lost)
	target := user("reset-target@test.local", "analyst", lost)
	superior := user("reset-super@test.local", "super_admin", lost)
	orgToken := session(orgAdmin, "mom_sess_reset_org")
	targetToken := session(target, "mom_sess_reset_target")

	t.Run("a reset lets the person in with the new password and ends the sessions opened with the old one", func(t *testing.T) {
		if code, _ := call(targetToken, http.MethodGet, "/api/v1/me", ""); code != http.StatusOK {
			t.Fatalf("the target's session does not work before the reset: %d", code)
		}
		code, body := call(orgToken, http.MethodPatch, "/api/v1/users/"+target.String(), edit("analyst", next))
		if code != http.StatusOK {
			t.Fatalf("an organization_admin could not reset an analyst's password: %d %s", code, truncateBody(body))
		}
		if canLogin("reset-target@test.local", lost) || !canLogin("reset-target@test.local", next) {
			t.Fatal("after the reset the old password still works or the new one does not")
		}
		if code, _ := call(targetToken, http.MethodGet, "/api/v1/me", ""); code != http.StatusUnauthorized {
			t.Errorf("a session opened with the old password still answers %d after the reset, want 401", code)
		}
		var reset bool
		if err := pool.QueryRow(ctx, `SELECT (detail->>'password_reset')::boolean FROM audit_logs WHERE actor_id=$1 AND action='user.update' AND resource_id=$2 ORDER BY created_at DESC LIMIT 1`, orgAdmin, target.String()).Scan(&reset); err != nil {
			t.Fatalf("read the audit entry: %v", err)
		}
		if !reset {
			t.Error("the audit entry for an update that reset the password does not say so")
		}
	})

	t.Run("an edit without a password leaves the password alone", func(t *testing.T) {
		code, body := call(orgToken, http.MethodPatch, "/api/v1/users/"+target.String(), edit("analyst", ""))
		if code != http.StatusOK {
			t.Fatalf("a plain edit answered %d: %s", code, truncateBody(body))
		}
		if !canLogin("reset-target@test.local", next) {
			t.Fatal("an edit that named no password changed it")
		}
	})

	t.Run("a password the profile route would refuse is refused here too", func(t *testing.T) {
		for _, bad := range []string{"short", strings.Repeat("가", 4), strings.Repeat("가", 25)} {
			if code, _ := call(orgToken, http.MethodPatch, "/api/v1/users/"+target.String(), edit("analyst", bad)); code != http.StatusBadRequest {
				t.Errorf("a %d-byte, %d-character password answered %d, want 400", len(bad), len([]rune(bad)), code)
			}
		}
		if !canLogin("reset-target@test.local", next) {
			t.Fatal("a refused password replaced the stored hash anyway")
		}
	})

	t.Run("not on an account with more authority than the caller's", func(t *testing.T) {
		code, _ := call(orgToken, http.MethodPatch, "/api/v1/users/"+superior.String(), edit("super_admin", next))
		if code != http.StatusForbidden {
			t.Errorf("an organization_admin reset a super_admin's password: %d", code)
		}
		// Nor by demoting it first: the account being administered is what is
		// bounded, not only the role being granted.
		code, _ = call(orgToken, http.MethodPatch, "/api/v1/users/"+superior.String(), edit("viewer", ""))
		if code != http.StatusForbidden {
			t.Errorf("an organization_admin demoted a super_admin: %d", code)
		}
		var role string
		if err := pool.QueryRow(ctx, `SELECT role FROM users WHERE id=$1`, superior).Scan(&role); err != nil {
			t.Fatal(err)
		}
		if role != "super_admin" || !canLogin("reset-super@test.local", lost) {
			t.Fatalf("the refused requests changed the super_admin anyway: role %q, old password works %v", role, canLogin("reset-super@test.local", lost))
		}
		// A super_admin administers everyone, another super_admin included.
		if code, body := call(f.sessionCook, http.MethodPatch, "/api/v1/users/"+superior.String(), edit("super_admin", next)); code != http.StatusOK {
			t.Fatalf("a super_admin could not reset another super_admin's password: %d %s", code, truncateBody(body))
		}
		if !canLogin("reset-super@test.local", next) {
			t.Fatal("the super_admin's reset answered 200 and the new password does not work")
		}
	})

	t.Run("not on the caller's own account, which asks for the current password on the profile route", func(t *testing.T) {
		if code, _ := call(orgToken, http.MethodPatch, "/api/v1/users/"+orgAdmin.String(), edit("organization_admin", next)); code != http.StatusBadRequest {
			t.Errorf("an administrator reset its own password without the current one: %d", code)
		}
		if !canLogin("reset-org@test.local", lost) {
			t.Fatal("the refused self-reset changed the password anyway")
		}
	})

	t.Run("an account that does not exist is 404, not a silent 200", func(t *testing.T) {
		if code, _ := call(orgToken, http.MethodPatch, "/api/v1/users/"+uuid.New().String(), edit("viewer", next)); code != http.StatusNotFound {
			t.Errorf("editing an unknown user answered %d, want 404", code)
		}
	})
}
