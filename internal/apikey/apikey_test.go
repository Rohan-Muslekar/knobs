package apikey_test

import (
	"strings"
	"testing"

	"github.com/Rohan-Muslekar/knobs/internal/apikey"
)

func TestGenerateProducesKnobsPrefixedKey(t *testing.T) {
	plaintext, _, err := apikey.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.HasPrefix(plaintext, "knobs_") {
		t.Fatalf("plaintext = %q, want knobs_ prefix", plaintext)
	}
}

func TestHashOfGeneratedPlaintextMatchesGeneratedHash(t *testing.T) {
	plaintext, hash, err := apikey.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got := apikey.Hash(plaintext); got != hash {
		t.Fatalf("Hash(plaintext) = %q, want %q", got, hash)
	}
}

func TestGenerateCallsProduceDifferentKeys(t *testing.T) {
	plaintext1, hash1, err := apikey.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	plaintext2, hash2, err := apikey.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if plaintext1 == plaintext2 {
		t.Fatalf("two Generate calls produced the same plaintext: %q", plaintext1)
	}
	if hash1 == hash2 {
		t.Fatalf("two Generate calls produced the same hash: %q", hash1)
	}
}

func TestHashIsDeterministic(t *testing.T) {
	const fixed = "knobs_fixed-input-for-determinism-check"
	if apikey.Hash(fixed) != apikey.Hash(fixed) {
		t.Fatalf("Hash(%q) is not deterministic", fixed)
	}
}
