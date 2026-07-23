package account

import (
	"testing"
)

func TestIsFilePublicID(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"file_abc123", true},
		{"file_", true},
		{"file_xxx", true},
		{"", false},
		{"https://example.com/avatar.png", false},
		{"File_abc", false},
		{"FILE_abc", false},
		{"file", false},
		{"file_", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsFilePublicID(tt.input); got != tt.want {
				t.Errorf("IsFilePublicID(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
