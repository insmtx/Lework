package authn

import (
	"strings"
	"testing"
)

func TestNewTokenIsRandomAndHashed(t *testing.T) {
	raw1, hash1, _ := NewToken()
	raw2, _, _ := NewToken()
	if raw1 == raw2 {
		t.Fatal("tokens repeat")
	}
	if hash1 != HashToken(raw1) {
		t.Fatal("hash mismatch")
	}
	if hash1 == raw1 {
		t.Fatal("hash equals raw token")
	}
}

func TestNewShortCodeLengthAndAlphabet(t *testing.T) {
	code, err := NewShortCode(5)
	if err != nil {
		t.Fatalf("NewShortCode: %v", err)
	}
	if len(code) != 5 {
		t.Fatalf("len = %d, want 5", len(code))
	}
	for _, r := range code {
		if !strings.ContainsRune("23456789ABCDEFGHJKMNPQRSTUVWXYZ", r) {
			t.Fatalf("code %q contains %q", code, r)
		}
	}
	if _, err := NewShortCode(3); err == nil {
		t.Fatal("length 3 accepted")
	}
}
