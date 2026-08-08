package auth

import "testing"

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
