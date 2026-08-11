// Package secret seals administrator supplied secrets so that a service restart
// never loses them. Values are encrypted with AES-256-GCM using the key material
// from MOMENTO_ENCRYPTION_KEY, which lets Momento keep an API key readable after
// a restart without storing it in clear text.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Prefix marks a sealed value. Anything without it is legacy clear text written
// before an encryption key was configured and is returned unchanged.
const Prefix = "enc:v1:"

// MinPassphraseLength is the shortest accepted passphrase when the key material
// is not a base64 or hex encoded 32 byte key.
const MinPassphraseLength = 16

var (
	// ErrDisabled is returned when a value must be sealed but no key is configured.
	ErrDisabled = errors.New("secret encryption is disabled: set MOMENTO_ENCRYPTION_KEY")
	// ErrUnknownKey is returned when a stored value was sealed with another key.
	ErrUnknownKey = errors.New("stored secret was sealed with a different encryption key")
)

type keyEntry struct {
	id   string
	aead cipher.AEAD
}

// Cipher is a keyring: the first key seals new values and every key can open
// existing ones, which makes key rotation possible without downtime.
type Cipher struct {
	keys []keyEntry
}

// New builds a keyring. An empty primary key returns a disabled cipher so that
// deployments without MOMENTO_ENCRYPTION_KEY keep working exactly as before.
func New(primary string, previous ...string) (*Cipher, error) {
	if strings.TrimSpace(primary) == "" {
		if len(nonEmpty(previous)) > 0 {
			return nil, errors.New("MOMENTO_ENCRYPTION_KEY_PREVIOUS requires MOMENTO_ENCRYPTION_KEY")
		}
		return &Cipher{}, nil
	}
	entry, err := newKeyEntry(primary)
	if err != nil {
		return nil, fmt.Errorf("MOMENTO_ENCRYPTION_KEY: %w", err)
	}
	c := &Cipher{keys: []keyEntry{entry}}
	for _, material := range nonEmpty(previous) {
		old, err := newKeyEntry(material)
		if err != nil {
			return nil, fmt.Errorf("MOMENTO_ENCRYPTION_KEY_PREVIOUS: %w", err)
		}
		if old.id == entry.id {
			continue
		}
		c.keys = append(c.keys, old)
	}
	return c, nil
}

func nonEmpty(values []string) []string {
	out := []string{}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}

func newKeyEntry(material string) (keyEntry, error) {
	raw, ok := decodeKey(strings.TrimSpace(material))
	if !ok {
		trimmed := strings.TrimSpace(material)
		if len(trimmed) < MinPassphraseLength {
			return keyEntry{}, fmt.Errorf("must be a base64 or hex encoded 32 byte key, or a passphrase of at least %d characters", MinPassphraseLength)
		}
		sum := sha256.Sum256([]byte(trimmed))
		raw = sum[:]
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return keyEntry{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return keyEntry{}, err
	}
	return keyEntry{id: fingerprint(raw), aead: aead}, nil
}

func decodeKey(value string) ([]byte, bool) {
	if raw, err := hex.DecodeString(value); err == nil && len(raw) == 32 {
		return raw, true
	}
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if raw, err := encoding.DecodeString(value); err == nil && len(raw) == 32 {
			return raw, true
		}
	}
	return nil, false
}

// fingerprint identifies a key inside a sealed value without revealing it.
func fingerprint(raw []byte) string {
	sum := sha256.Sum256(append([]byte("momento-encryption-key-id:"), raw...))
	return hex.EncodeToString(sum[:4])
}

// Enabled reports whether new values can be sealed.
func (c *Cipher) Enabled() bool { return c != nil && len(c.keys) > 0 }

// KeyID returns the fingerprint of the sealing key, or an empty string when the
// cipher is disabled.
func (c *Cipher) KeyID() string {
	if !c.Enabled() {
		return ""
	}
	return c.keys[0].id
}

// PreviousKeyIDs returns the fingerprints kept only for decryption.
func (c *Cipher) PreviousKeyIDs() []string {
	out := []string{}
	if !c.Enabled() {
		return out
	}
	for _, entry := range c.keys[1:] {
		out = append(out, entry.id)
	}
	return out
}

// Encrypt seals a value. An empty input stays empty so callers can store NULL.
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if !c.Enabled() {
		return "", ErrDisabled
	}
	entry := c.keys[0]
	nonce := make([]byte, entry.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := entry.aead.Seal(nonce, nonce, []byte(plaintext), []byte(entry.id))
	return Prefix + entry.id + ":" + base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Decrypt opens a sealed value. Legacy clear text is returned unchanged so an
// existing deployment can adopt encryption without a data migration.
func (c *Cipher) Decrypt(stored string) (string, error) {
	if !Sealed(stored) {
		return stored, nil
	}
	parts := strings.SplitN(strings.TrimPrefix(stored, Prefix), ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", errors.New("sealed secret is malformed")
	}
	if !c.Enabled() {
		return "", ErrDisabled
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("sealed secret is malformed")
	}
	for _, entry := range c.keys {
		if entry.id != parts[0] {
			continue
		}
		if len(raw) < entry.aead.NonceSize() {
			return "", errors.New("sealed secret is malformed")
		}
		nonce := raw[:entry.aead.NonceSize()]
		opened, err := entry.aead.Open(nil, nonce, raw[entry.aead.NonceSize():], []byte(entry.id))
		if err != nil {
			return "", ErrUnknownKey
		}
		return string(opened), nil
	}
	return "", ErrUnknownKey
}

// NeedsReseal reports whether a stored value should be re-encrypted with the
// current primary key, which is how a rotation finishes.
func (c *Cipher) NeedsReseal(stored string) bool {
	if !c.Enabled() || stored == "" {
		return false
	}
	if !Sealed(stored) {
		return true
	}
	return !strings.HasPrefix(stored, Prefix+c.KeyID()+":")
}

// Sealed reports whether a stored value is an encrypted Momento secret.
func Sealed(value string) bool { return strings.HasPrefix(value, Prefix) }
