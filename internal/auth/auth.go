package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type Principal struct {
	ID               uuid.UUID `json:"id"`
	Email            string    `json:"email"`
	DisplayName      string    `json:"display_name"`
	Department       string    `json:"department"`
	OrganizationName string    `json:"organization_name"`
	Role             string    `json:"role"`
	Scopes           []string  `json:"-"`
	AuthType         string    `json:"-"`
}

type contextKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(contextKey{}).(Principal)
	return p, ok
}

func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(h), err
}

func ComparePassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func NewToken(prefix string, bytes int) (plain, hash, shortPrefix string, err error) {
	raw := make([]byte, bytes)
	if _, err = rand.Read(raw); err != nil {
		return "", "", "", err
	}
	plain = prefix + base64.RawURLEncoding.EncodeToString(raw)
	hash = HashToken(plain)
	shortPrefix = plain
	if len(shortPrefix) > 14 {
		shortPrefix = shortPrefix[:14]
	}
	return
}

func HashToken(token string) string {
	s := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(s[:])
}

type Service struct{ DB *pgxpool.Pool }

func (s Service) Bootstrap(ctx context.Context, email, password string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var orgID, workspaceID uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO organizations(name,slug) VALUES('Momento','default') ON CONFLICT(slug) DO UPDATE SET name=excluded.name RETURNING id`).Scan(&orgID); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT id FROM workspaces WHERE organization_id=$1 ORDER BY created_at LIMIT 1`, orgID).Scan(&workspaceID); err != nil {
		if err := tx.QueryRow(ctx, `INSERT INTO workspaces(organization_id,name) VALUES($1,'Default Workspace') RETURNING id`, orgID).Scan(&workspaceID); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO users(email,display_name,password_hash,role,organization_name)
		VALUES(lower($1),$1,$2,'super_admin','Momento') ON CONFLICT(email) DO NOTHING`, email, hash)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Service) Login(ctx context.Context, email, password string) (Principal, string, error) {
	var p Principal
	var hash *string
	err := s.DB.QueryRow(ctx, `SELECT id,email,display_name,department,organization_name,role,password_hash FROM users WHERE email=lower($1) AND active`, email).
		Scan(&p.ID, &p.Email, &p.DisplayName, &p.Department, &p.OrganizationName, &p.Role, &hash)
	if err != nil || hash == nil || !ComparePassword(*hash, password) {
		return Principal{}, "", errors.New("invalid credentials")
	}
	plain, err := s.CreateSession(ctx, p.ID)
	if err != nil {
		return Principal{}, "", err
	}
	p.AuthType = "session"
	return p, plain, nil
}

func (s Service) CreateSession(ctx context.Context, userID uuid.UUID) (string, error) {
	plain, tokenHash, _, err := NewToken("mom_sess_", 32)
	if err != nil {
		return "", err
	}
	_, err = s.DB.Exec(ctx, `INSERT INTO user_sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '12 hours')`, userID, tokenHash)
	return plain, err
}

func (s Service) Authenticate(r *http.Request) (Principal, error) {
	token := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(h), "bearer ") {
		token = strings.TrimSpace(h[7:])
	}
	if token == "" {
		if c, err := r.Cookie("momento_session"); err == nil {
			token = c.Value
		}
	}
	if token == "" {
		return Principal{}, errors.New("authentication required")
	}
	hash := HashToken(token)
	var p Principal
	if strings.HasPrefix(token, "mom_key_") {
		err := s.DB.QueryRow(r.Context(), `SELECT u.id,u.email,u.display_name,u.department,u.organization_name,u.role,k.scopes
			FROM api_keys k JOIN users u ON u.id=k.user_id WHERE k.key_hash=$1 AND k.revoked_at IS NULL AND (k.expires_at IS NULL OR k.expires_at>now()) AND u.active`, hash).
			Scan(&p.ID, &p.Email, &p.DisplayName, &p.Department, &p.OrganizationName, &p.Role, &p.Scopes)
		if err != nil {
			return Principal{}, errors.New("invalid API key")
		}
		p.AuthType = "api_key"
		_, _ = s.DB.Exec(r.Context(), `UPDATE api_keys SET last_used_at=now() WHERE key_hash=$1`, hash)
		return p, nil
	}
	err := s.DB.QueryRow(r.Context(), `SELECT u.id,u.email,u.display_name,u.department,u.organization_name,u.role
		FROM user_sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>now() AND u.active`, hash).
		Scan(&p.ID, &p.Email, &p.DisplayName, &p.Department, &p.OrganizationName, &p.Role)
	if err != nil {
		return Principal{}, errors.New("invalid session")
	}
	p.AuthType = "session"
	return p, nil
}

func (s Service) Logout(ctx context.Context, token string) {
	if token != "" {
		_, _ = s.DB.Exec(ctx, `DELETE FROM user_sessions WHERE token_hash=$1`, HashToken(token))
	}
}

func SetSessionCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: "momento_session", Value: token, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: int((12 * time.Hour).Seconds())})
}

func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: "momento_session", Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

var roleRank = map[string]int{"viewer": 1, "analyst": 2, "workspace_admin": 3, "organization_admin": 4, "super_admin": 5}

func RoleAtLeast(actual, required string) bool {
	return roleRank[actual] >= roleRank[required]
}

// RoleAbove reports whether granting `role` would hand out more authority than
// the caller holds. Nothing checked this: a workspace_admin, the lowest
// administrative role, could set any user's role to super_admin — including their
// own — and super_admin passes every workspace check there is.
func RoleAbove(role, callerRole string) bool {
	return roleRank[role] > roleRank[callerRole]
}
