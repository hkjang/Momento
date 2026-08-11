package secret

import (
	"errors"
	"strings"
	"testing"
)

const testKey = "momento-test-encryption-key-01"

func TestEncryptRoundTripSurvivesANewProcess(t *testing.T) {
	t.Parallel()

	writer, err := New(testKey)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sealed, err := writer.Encrypt("mom_key_secret-value")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if strings.Contains(sealed, "secret-value") {
		t.Fatalf("sealed value leaks the plaintext: %q", sealed)
	}

	// A restart rebuilds the cipher from the same environment variable.
	reader, err := New(testKey)
	if err != nil {
		t.Fatalf("New after restart: %v", err)
	}
	got, err := reader.Decrypt(sealed)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "mom_key_secret-value" {
		t.Fatalf("plaintext = %q, want %q", got, "mom_key_secret-value")
	}
}

func TestEncryptIsNotDeterministic(t *testing.T) {
	t.Parallel()

	c, _ := New(testKey)
	first, _ := c.Encrypt("same")
	second, _ := c.Encrypt("same")
	if first == second {
		t.Fatal("two encryptions of the same value produced the same ciphertext")
	}
}

func TestKeyMaterialAcceptsEncodedAndPassphraseForms(t *testing.T) {
	t.Parallel()

	for name, material := range map[string]string{
		"hex":       strings.Repeat("ab", 32),
		"base64":    "bW9tZW50by1lbmNyeXB0aW9uLWtleS0zMi1ieXRlcyE=",
		"pass":      "a-long-enough-passphrase",
		"base64raw": "bW9tZW50by1lbmNyeXB0aW9uLWtleS0zMi1ieXRlcyE",
	} {
		t.Run(name, func(t *testing.T) {
			c, err := New(material)
			if err != nil {
				t.Fatalf("New(%q): %v", material, err)
			}
			sealed, err := c.Encrypt("value")
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}
			if got, err := c.Decrypt(sealed); err != nil || got != "value" {
				t.Fatalf("Decrypt = %q, %v", got, err)
			}
		})
	}
}

func TestShortPassphraseIsRejected(t *testing.T) {
	t.Parallel()

	if _, err := New("too-short"); err == nil {
		t.Fatal("a short passphrase was accepted")
	}
}

func TestDisabledCipherKeepsClearTextReadable(t *testing.T) {
	t.Parallel()

	c, err := New("")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.Enabled() {
		t.Fatal("cipher without key material reports enabled")
	}
	if got, err := c.Decrypt("mom_key_legacy"); err != nil || got != "mom_key_legacy" {
		t.Fatalf("legacy value = %q, %v", got, err)
	}
	if _, err := c.Encrypt("value"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Encrypt error = %v, want ErrDisabled", err)
	}
	if got, err := c.Decrypt(Prefix + "abcd1234:AAAA"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Decrypt = %q, %v, want ErrDisabled", got, err)
	}
}

func TestRotationOpensValuesSealedWithThePreviousKey(t *testing.T) {
	t.Parallel()

	old, _ := New("previous-encryption-key-value")
	sealed, _ := old.Encrypt("rotate-me")

	rotated, err := New(testKey, "previous-encryption-key-value")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := rotated.Decrypt(sealed)
	if err != nil || got != "rotate-me" {
		t.Fatalf("Decrypt = %q, %v", got, err)
	}
	if !rotated.NeedsReseal(sealed) {
		t.Fatal("a value sealed with the previous key should need a reseal")
	}
	fresh, _ := rotated.Encrypt("rotate-me")
	if rotated.NeedsReseal(fresh) {
		t.Fatal("a value sealed with the primary key should not need a reseal")
	}
}

func TestWrongKeyCannotOpenTheValue(t *testing.T) {
	t.Parallel()

	writer, _ := New(testKey)
	sealed, _ := writer.Encrypt("secret")
	other, _ := New("completely-different-key-material")
	if _, err := other.Decrypt(sealed); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("Decrypt error = %v, want ErrUnknownKey", err)
	}
}

func TestPreviousKeyWithoutPrimaryIsRejected(t *testing.T) {
	t.Parallel()

	if _, err := New("", "previous-encryption-key-value"); err == nil {
		t.Fatal("previous key without a primary key was accepted")
	}
}

func TestEmptyValuesStayEmpty(t *testing.T) {
	t.Parallel()

	c, _ := New(testKey)
	if sealed, err := c.Encrypt(""); sealed != "" || err != nil {
		t.Fatalf("Encrypt(\"\") = %q, %v", sealed, err)
	}
	if got, err := c.Decrypt(""); got != "" || err != nil {
		t.Fatalf("Decrypt(\"\") = %q, %v", got, err)
	}
}

func TestMalformedSealedValueIsReported(t *testing.T) {
	t.Parallel()

	c, _ := New(testKey)
	for _, value := range []string{Prefix, Prefix + "abcd1234", Prefix + "abcd1234:", Prefix + ":payload"} {
		if _, err := c.Decrypt(value); err == nil {
			t.Fatalf("malformed value %q was accepted", value)
		}
	}
}
