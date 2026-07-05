package service

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestServerProjectionLayersDoNotDependOnRuntimeEventContracts(t *testing.T) {
	roots := []string{".", "../api", "../runnable"}
	forbidden := []string{
		"/backend/agent",
		"/backend/agent/runtime",
		"/backend/internal/worker/agentrun/domain",
	}
	for _, root := range roots {
		root := root
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if parseErr != nil {
				t.Fatalf("parse %s: %v", path, parseErr)
			}
			for _, imported := range file.Imports {
				value, unquoteErr := strconv.Unquote(imported.Path.Value)
				if unquoteErr != nil {
					t.Fatalf("unquote import in %s: %v", path, unquoteErr)
				}
				for _, prefix := range forbidden {
					if strings.Contains(value, prefix) {
						t.Errorf("%s imports forbidden package %s", path, value)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WalkDir(%s) error = %v", root, err)
		}
	}
}
