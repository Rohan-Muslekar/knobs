package auth_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/Rohan-Muslekar/knobs/internal/auth"
)

func TestHashAndCheck(t *testing.T) {
	a := auth.New("secret", true)
	h, err := a.Hash("hunter2")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !a.Check(h, "hunter2") {
		t.Fatal("Check should accept the correct password")
	}
	if a.Check(h, "wrong") {
		t.Fatal("Check should reject a wrong password")
	}
}

func TestIssueVerifyRoundTrip(t *testing.T) {
	a := auth.New("secret", true)
	id := uuid.New()
	tok, err := a.Issue(id)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	got, err := a.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got != id {
		t.Fatalf("round trip mismatch: %s vs %s", got, id)
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	tok, _ := auth.New("secret", true).Issue(uuid.New())
	if _, err := auth.New("other", true).Verify(tok); err == nil {
		t.Fatal("Verify must reject a token signed with a different secret")
	}
}
