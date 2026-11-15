package authn

import (
	"testing"
)

func TestHashPasswordRejectsWrongPassword(t *testing.T) {
	encoded, err := HashPassword("correct horse")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !VerifyPassword(encoded, "correct horse") {
		t.Fatal("correct password rejected")
	}
	if VerifyPassword(encoded, "Correct horse") {
		t.Fatal("wrong password accepted")
	}
	if VerifyPassword("not-a-phc-string", "x") {
		t.Fatal("malformed hash accepted")
	}
}
