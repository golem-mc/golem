package server_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// trackMapAllowed are the per-kind tracking records that predate the generic
// tracker, as "file Type.field". The list only shrinks: a kind moved onto
// internal/server/tracker.go deletes its entry, and a new entry is never added.
var trackMapAllowed = []string{}

// trackRecordName matches a field name or value type that records what a
// watcher was told of a body.
var trackRecordName = regexp.MustCompile(`(?i)tracked|told|shown`)

// TestNoPerKindTrackingMap holds the rule that a kind's bodies are tracked
// through bodyTrack in internal/server/tracker.go: a struct field keyed by a watcher's
// UUID that records what that watcher was told is a copy of the tracker.
func TestNoPerKindTrackingMap(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	seen := map[string]bool{}
	fset := token.NewFileSet()
	walkErr := filepath.WalkDir(filepath.Join(root, "internal", "server"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			// The black-box tests and their harness hold no bodies.
			if rel == "internal/server/tests" || rel == "internal/server/servertest" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || rel == "internal/server/tracker.go" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// Only files that name uuid can hold such a field; skipping the rest
		// keeps the parse to a fraction of the tree.
		if !strings.Contains(string(src), "uuid.UUID") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := spec.Type.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range st.Fields.List {
				m, ok := field.Type.(*ast.MapType)
				if !ok || !isUUIDType(m.Key) {
					continue
				}
				value := typeName(m.Value)
				for _, name := range field.Names {
					if !trackRecordName.MatchString(name.Name) && !trackRecordName.MatchString(value) {
						continue
					}
					key := rel + " " + spec.Name.Name + "." + name.Name
					if slices.Contains(trackMapAllowed, key) {
						seen[key] = true
						continue
					}
					t.Errorf("%s:%d: %s.%s is a per-kind tracking map (map[uuid.UUID]%s); "+
						"embed bodyTrack[S] from internal/server/tracker.go and give the kind a trackedKind instead",
						rel, fset.Position(name.Pos()).Line, spec.Name.Name, name.Name, value)
				}
			}
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking server: %v", walkErr)
	}
	for _, key := range trackMapAllowed {
		if !seen[key] {
			t.Errorf("trackMapAllowed entry %q no longer occurs; delete it from internal/server/structure_policy_test.go", key)
		}
	}
}

// isUUIDType reports whether e spells uuid.UUID.
func isUUIDType(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "uuid" && sel.Sel.Name == "UUID"
}

// typeName is the name of a type expression's outermost named type, "" for
// a literal type such as struct{}.
func typeName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.StarExpr:
		return typeName(e.X)
	case *ast.IndexExpr:
		return typeName(e.X)
	case *ast.IndexListExpr:
		return typeName(e.X)
	}
	return ""
}
