package registry

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestExportedAPINamesOnlyAliasedProtocolTypes checks that every protocol
// type an exported registry declaration names has an alias in protocoltypes.go.
func TestExportedAPINamesOnlyAliasedProtocolTypes(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	aliased := map[string]bool{}
	used := map[string][]string{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		collect := func(n ast.Node, where string) {
			ast.Inspect(n, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "protocol" {
					used[sel.Sel.Name] = append(used[sel.Sel.Name], where)
				}
				return true
			})
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if !d.Name.IsExported() {
					continue
				}
				if d.Recv != nil && len(d.Recv.List) > 0 && !recvExported(d.Recv.List[0].Type) {
					continue
				}
				collect(d.Type, name+": "+d.Name.Name)
			case *ast.GenDecl:
				for _, s := range d.Specs {
					ts, ok := s.(*ast.TypeSpec)
					if !ok {
						continue
					}
					if name == "protocoltypes.go" {
						if sel, ok := ts.Type.(*ast.SelectorExpr); ok && ts.Assign.IsValid() {
							aliased[ts.Name.Name] = ts.Name.Name == sel.Sel.Name
						}
						continue
					}
					if !ts.Name.IsExported() {
						continue
					}
					if st, ok := ts.Type.(*ast.StructType); ok {
						for _, fld := range st.Fields.List {
							exported := len(fld.Names) == 0
							for _, n := range fld.Names {
								exported = exported || n.IsExported()
							}
							if exported {
								collect(fld.Type, name+": "+ts.Name.Name)
							}
						}
						continue
					}
					collect(ts.Type, name+": "+ts.Name.Name)
				}
			}
		}
	}
	for typ, where := range used {
		if !aliased[typ] {
			t.Errorf("protocol.%s appears in an exported registry API (%s) but has no alias in protocoltypes.go", typ, where[0])
		}
	}
}

func recvExported(e ast.Expr) bool {
	if s, ok := e.(*ast.StarExpr); ok {
		e = s.X
	}
	if ix, ok := e.(*ast.IndexExpr); ok {
		e = ix.X
	}
	id, ok := e.(*ast.Ident)
	return ok && id.IsExported()
}
