package auth

import (
	"strings"
	"testing"
)

func TestPasswordAndTokens(t *testing.T) {
	hash, err := HashPassword("a-strong-password")
	if err != nil {
		t.Fatal(err)
	}
	if !ComparePassword(hash, "a-strong-password") || ComparePassword(hash, "wrong") {
		t.Fatal("password comparison failed")
	}
	plain, digest, prefix, err := NewToken("mom_key_", 32)
	if err != nil {
		t.Fatal(err)
	}
	if HashToken(plain) != digest || len(prefix) > 14 {
		t.Fatal("token material invalid")
	}
}

// A password bcrypt refuses must be refused before hashing, because a handler
// that stored the empty result would leave an account nobody can sign in to.
func TestPasswordProblemTracksWhatBcryptAccepts(t *testing.T) {
	longest := strings.Repeat("a", MaxPasswordBytes)
	if problem := PasswordProblem(longest); problem != "" {
		t.Fatalf("%d bytes is within bcrypt's limit, got %q", MaxPasswordBytes, problem)
	}
	if _, err := HashPassword(longest); err != nil {
		t.Fatalf("bcrypt refused %d bytes: %v", MaxPasswordBytes, err)
	}
	korean := strings.Repeat("가", 25) // 75 bytes, well past twelve characters
	if problem := PasswordProblem(korean); problem == "" {
		t.Fatal("a 75-byte passphrase was accepted")
	}
	if _, err := HashPassword(korean); err == nil {
		t.Fatal("bcrypt accepted 75 bytes, so the bound is no longer bcrypt's and MaxPasswordBytes is stale")
	}
	if problem := PasswordProblem("short"); problem == "" {
		t.Fatal("a five-character password was accepted")
	}
}
