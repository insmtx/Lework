//go:build !enterprise

package oss

import (
	"context"
	"testing"

	"github.com/insmtx/Leros/backend/types"
)

func TestResolveSingleAvatarMap_PlainURL(t *testing.T) {
	result, err := resolveSingleAvatarMap(context.Background(), nil, 0, "https://example.com/avatar.png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for non-file URL, got %v", result)
	}
}

func TestResolveSingleAvatarMap_Empty(t *testing.T) {
	result, err := resolveSingleAvatarMap(context.Background(), nil, 0, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for empty URL, got %v", result)
	}
}

func TestResolveAvatarURLs_NilSlice(t *testing.T) {
	result := resolveAvatarURLs(context.Background(), nil, 0, nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
}

func TestResolveAvatarURLs_EmptySlice(t *testing.T) {
	result := resolveAvatarURLs(context.Background(), nil, 0, []*types.User{})
	if result != nil {
		t.Errorf("expected nil for empty slice, got %v", result)
	}
}

func TestResolveAvatarURLs_NoFilePrefix(t *testing.T) {
	users := []*types.User{
		{AvatarURL: "https://example.com/1.png"},
		{AvatarURL: "https://example.com/2.png"},
	}
	result := resolveAvatarURLs(context.Background(), nil, 0, users)
	if result != nil {
		t.Errorf("expected nil for no file_ prefix, got %v", result)
	}
}

func TestResolveAvatarURLs_Mixed_NoDB(t *testing.T) {
	users := []*types.User{
		{AvatarURL: "https://example.com/1.png"},
		{AvatarURL: "file_abc123"},
		{AvatarURL: ""},
		{AvatarURL: "file_def456"},
	}
	result := resolveAvatarURLs(context.Background(), nil, 0, users)
	if result != nil {
		t.Logf("got result (unexpected without DB): %v", result)
	}
}

func TestResolveAvatarURLs_DuplicateFilePublicIDs(t *testing.T) {
	users := []*types.User{
		{AvatarURL: "file_same"},
		{AvatarURL: "file_same"},
	}
	result := resolveAvatarURLs(context.Background(), nil, 0, users)
	if result != nil {
		t.Logf("got result (unexpected without DB): %v", result)
	}
}
