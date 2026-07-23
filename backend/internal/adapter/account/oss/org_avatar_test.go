//go:build !enterprise

package oss

import (
	"context"
	"testing"

	"github.com/insmtx/Leros/backend/types"
)

func TestResolveSingleOrgLogoMap_PlainURL(t *testing.T) {
	result, err := resolveSingleOrgLogoMap(context.Background(), nil, 0, "https://example.com/logo.png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for non-file URL, got %v", result)
	}
}

func TestResolveSingleOrgLogoMap_Empty(t *testing.T) {
	result, err := resolveSingleOrgLogoMap(context.Background(), nil, 0, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for empty URL, got %v", result)
	}
}

func TestResolveOrgLogoURLs_NilSlice(t *testing.T) {
	result := resolveOrgLogoURLs(context.Background(), nil, 0, nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
}

func TestResolveOrgLogoURLs_EmptySlice(t *testing.T) {
	result := resolveOrgLogoURLs(context.Background(), nil, 0, []*types.Organization{})
	if result != nil {
		t.Errorf("expected nil for empty slice, got %v", result)
	}
}

func TestResolveOrgLogoURLs_NoFilePrefix(t *testing.T) {
	orgs := []*types.Organization{
		{Logo: "https://example.com/logo1.png"},
		{Logo: "https://example.com/logo2.png"},
	}
	result := resolveOrgLogoURLs(context.Background(), nil, 0, orgs)
	if result != nil {
		t.Errorf("expected nil for no file_ prefix, got %v", result)
	}
}

func TestResolveOrgLogoURLs_Mixed_NoDB(t *testing.T) {
	orgs := []*types.Organization{
		{Logo: "https://example.com/logo1.png"},
		{Logo: "file_abc123"},
		{Logo: ""},
		{Logo: "file_def456"},
	}
	result := resolveOrgLogoURLs(context.Background(), nil, 0, orgs)
	if result != nil {
		t.Logf("got result (unexpected without DB): %v", result)
	}
}

func TestResolveOrgLogoURLs_DuplicateFilePublicIDs(t *testing.T) {
	orgs := []*types.Organization{
		{Logo: "file_same"},
		{Logo: "file_same"},
	}
	result := resolveOrgLogoURLs(context.Background(), nil, 0, orgs)
	if result != nil {
		t.Logf("got result (unexpected without DB): %v", result)
	}
}
