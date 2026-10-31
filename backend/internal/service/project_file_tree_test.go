package service

import (
	"testing"

	"github.com/insmtx/Lework/backend/internal/consts"
)

func TestIsPathAllowed(t *testing.T) {
	uploads := consts.RepoDirUploads
	artifacts := consts.RepoDirArtifacts

	tests := []struct {
		path    string
		allowed bool
	}{
		{uploads + "/readme.md", true},
		{uploads + "/sub/dir/file.txt", true},
		{artifacts + "/report.pdf", true},
		{artifacts + "/", true},
		{"src/main.go", false},
		{"", false},
		{uploads, false},
		{artifacts, false},
		{"config.yaml", false},
		{uploads + "_evil/file.txt", false},
	}

	for _, tt := range tests {
		result := isPathAllowed(tt.path)
		if result != tt.allowed {
			t.Errorf("isPathAllowed(%q) = %v, want %v", tt.path, result, tt.allowed)
		}
	}
}

func TestMimeTypeByExt(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"image.png", "image/png"},
		{"data.json", "application/json"},
	}

	for _, tt := range tests {
		got := mimeTypeByExt(tt.filename)
		if got != tt.want {
			t.Errorf("mimeTypeByExt(%q) = %q, want %q", tt.filename, got, tt.want)
		}
	}

	if got := mimeTypeByExt("script.js"); got == "" {
		t.Errorf("mimeTypeByExt(\"script.js\") should return non-empty mime type")
	}

	if got := mimeTypeByExt("noext"); got != "" {
		t.Errorf("mimeTypeByExt(\"noext\") = %q, want \"\"", got)
	}
}
