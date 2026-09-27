package authz

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryPermissionIsConsumed is the AST lock for the permission directory.
// It walks every non-test production file under internal/ (skipping this
// package), collects selector references of the form authz.<Ident>, and fails
// if any directory permission is never referenced. A zero-consumer permission
// is therefore a build-breaking defect rather than a silent one.
func TestEveryPermissionIsConsumed(t *testing.T) {
	// Tests run with the package directory as the working directory, so ".." is
	// internal/. The authz package itself is skipped: it defines the constants.
	referenced := make(map[string]struct{})
	fset := token.NewFileSet()
	err := filepath.WalkDir("..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "authz" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := selector.X.(*ast.Ident)
			if !ok || ident.Name != "authz" {
				return true
			}
			referenced[selector.Sel.Name] = struct{}{}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/: %v", err)
	}

	for ident := range catalogPermissions {
		if _, ok := referenced[ident]; !ok {
			t.Errorf("permission constant %s is not referenced by any internal/ production file", ident)
		}
	}
}
