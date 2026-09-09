// Package apikey generates and hashes API keys for delivery auth.
//
// The plaintext key is returned to the caller exactly once, at creation
// time, and is never persisted. Only its sha256 hex hash is stored, and
// that hash is what a presented Authorization header is checked against.
package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// Generate creates a new random API key. It returns the plaintext (shown
// to the caller once) and its sha256 hex hash (what gets persisted).
func Generate() (plaintext string, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	plaintext = "knobs_" + base64.RawURLEncoding.EncodeToString(buf)
	return plaintext, Hash(plaintext), nil
}

// Hash returns the sha256 hex digest of plaintext, used to look up a
// presented key's row without ever storing the plaintext itself.
func Hash(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
