package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAgentCoreHasNoSingerOSDependenciesOrUntypedMaps(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}
		for _, imported := range file.Imports {
			value, unquoteErr := strconv.Unquote(imported.Path.Value)
			if unquoteErr != nil {
				t.Fatalf("unquote import in %s: %v", path, unquoteErr)
			}
			for _, forbidden := range []string{
				"/backend/internal/",
				"/backend/config",
				"/backend/pkg/messaging",
				"/backend/tools",
			} {
				if strings.Contains(value, forbidden) {
					t.Errorf("%s imports forbidden package %s", path, value)
				}
			}
		}
		for _, declaration := range file.Decls {
			generic, ok := declaration.(*ast.GenDecl)
			if !ok || generic.Tok != token.TYPE {
				continue
			}
			for _, spec := range generic.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if typeSpec.Assign.IsValid() {
					t.Errorf("%s contains compatibility type alias %s", path, typeSpec.Name.Name)
				}
				if filepath.Dir(path) != "." {
					switch typeSpec.Name.Name {
					case "Event", "ExecutionRequest", "ExecutionResult":
						t.Errorf("%s redefines canonical agent contract %s", path, typeSpec.Name.Name)
					}
				}
			}
		}
		if filepath.Dir(path) == "." {
			ast.Inspect(file, func(node ast.Node) bool {
				mapType, ok := node.(*ast.MapType)
				if !ok {
					return true
				}
				key, keyOK := mapType.Key.(*ast.Ident)
				untypedValue := false
				switch value := mapType.Value.(type) {
				case *ast.InterfaceType:
					untypedValue = value.Methods.NumFields() == 0
				case *ast.Ident:
					untypedValue = value.Name == "any"
				}
				if keyOK && key.Name == "string" && untypedValue {
					t.Errorf("%s contains untyped map[string]any/interface{}", path)
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir() error = %v", err)
	}
}
