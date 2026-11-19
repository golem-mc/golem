package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// validateMetaSourceTypes resolves storage using syntax only, without importing entity.
func validateMetaSourceTypes(root string, groups []metaGroup) error {
	types := map[string]ast.Expr{}
	methods := map[string]map[string]ast.Expr{}
	entries, err := os.ReadDir(filepath.Join(root, "internal/server/entity"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") || strings.HasSuffix(entry.Name(), "_gen.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "internal/server/entity", entry.Name()), nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if t, ok := spec.(*ast.TypeSpec); ok {
						types[t.Name.Name] = t.Type
					}
				}
			case *ast.FuncDecl:
				if d.Recv == nil || len(d.Recv.List) != 1 || d.Type.Results == nil || len(d.Type.Results.List) != 1 {
					continue
				}
				recv := metaDeref(d.Recv.List[0].Type)
				name, ok := recv.(*ast.Ident)
				if !ok {
					continue
				}
				if methods[name.Name] == nil {
					methods[name.Name] = map[string]ast.Expr{}
				}
				if d.Type.Params.NumFields() == 0 {
					methods[name.Name][d.Name.Name] = d.Type.Results.List[0].Type
				}
			}
		}
	}
	var field func(ast.Expr, string) ast.Expr
	field = func(typ ast.Expr, name string) ast.Expr {
		typ = metaDeref(typ)
		if id, ok := typ.(*ast.Ident); ok {
			typ = types[id.Name]
		}
		st, ok := typ.(*ast.StructType)
		if !ok {
			return nil
		}
		for _, f := range st.Fields.List {
			for _, id := range f.Names {
				if id.Name == name {
					return f.Type
				}
			}
			if len(f.Names) == 0 {
				if id, ok := metaDeref(f.Type).(*ast.Ident); ok && id.Name == name {
					return f.Type
				}
				if inherited := field(f.Type, name); inherited != nil {
					return inherited
				}
			}
		}
		return nil
	}
	for _, group := range groups {
		for _, source := range group.sources {
			var typ ast.Expr = ast.NewIdent("Mob")
			parts := strings.SplitSeq(strings.TrimPrefix(source.path, "m."), ".")
			for part := range parts {
				if method, ok := strings.CutSuffix(part, "()"); ok {
					id, ok := metaDeref(typ).(*ast.Ident)
					if !ok {
						typ = nil
					} else {
						typ = methods[id.Name][method]
					}
				} else {
					typ = field(typ, part)
				}
				if typ == nil {
					return fmt.Errorf("unresolved metadata source %s", source.path)
				}
			}
			var actual bytes.Buffer
			if err := format.Node(&actual, token.NewFileSet(), typ); err != nil {
				return err
			}
			normalize := func(s string) string {
				if s == "byte" {
					return "uint8"
				}
				if s == "rune" {
					return "int32"
				}
				return s
			}
			if normalize(actual.String()) != normalize(source.typ) {
				return fmt.Errorf("source %s: declared %s, storage is %s", source.path, source.typ, actual.String())
			}
		}
	}
	return nil
}

func metaDeref(expr ast.Expr) ast.Expr {
	if ptr, ok := expr.(*ast.StarExpr); ok {
		return ptr.X
	}
	return expr
}
