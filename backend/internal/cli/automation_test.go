package cli

import "testing"

func TestAutomationTargetHeaders(t *testing.T) {
	if headers := automationTargetHeaders(nil); headers != nil {
		t.Fatalf("nil target headers = %#v, want nil", headers)
	}
	zero := uint(0)
	if headers := automationTargetHeaders(&zero); headers != nil {
		t.Fatalf("zero target headers = %#v, want nil", headers)
	}
	target := uint(42)
	headers := automationTargetHeaders(&target)
	if headers["X-Leros-Target-User-Id"] != "42" {
		t.Fatalf("target headers = %#v, want user ID 42", headers)
	}
}
