package main

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Every mutable schema selector is audited, including writes through kind-state aliases.
func TestMobMetadataSourceWritePolicy(t *testing.T) {
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "internal/server/mobmeta.go"))
	if err != nil {
		t.Fatal(err)
	}
	groups, err := parseMobMeta(src)
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]bool{"Type": true}
	for _, group := range groups {
		for _, source := range group.sources {
			if !strings.HasSuffix(source.path, "()") {
				sources[strings.TrimPrefix(source.path, "m.")] = true
			}
		}
	}
	// Type-checking the production sources can take over 10s on a busy host.
	// Export data lets go/types resolve aliases, promoted fields and adapter
	// receivers without importing or executing any game package in this test.
	cmd := exec.Command("mise", "exec", "--", "go", "list", "-deps", "-export", "-json", "./internal/server/...")
	cmd.Dir = root
	data, err := cmd.Output()
	if err != nil {
		t.Fatalf("export data: %v", err)
	}
	type pkg struct {
		ImportPath, Export, Dir string
		GoFiles, Imports        []string
	}
	var packages []pkg
	exports := map[string]string{}
	dec := json.NewDecoder(bytes.NewReader(data))
	for dec.More() {
		var p pkg
		if err := dec.Decode(&p); err != nil {
			t.Fatal(err)
		}
		exports[p.ImportPath] = p.Export
		if p.ImportPath == "github.com/golem-mc/golem/internal/server/entity" {
			packages = append(packages, p)
			continue
		}
		if !strings.HasPrefix(p.ImportPath, "github.com/golem-mc/golem/internal/server") {
			continue
		}
		if slices.Contains(p.Imports, "github.com/golem-mc/golem/internal/server/entity") {
			packages = append(packages, p)
		}
	}
	fs := token.NewFileSet()
	importer := importer.ForCompiler(fs, "gc", func(path string) (io.ReadCloser, error) { return os.Open(exports[path]) })
	for _, p := range packages {
		var files []*ast.File
		for _, name := range p.GoFiles {
			f, err := parser.ParseFile(fs, filepath.Join(p.Dir, name), nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, f)
		}
		info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
		cfg := types.Config{Importer: importer}
		if _, err := cfg.Check(p.ImportPath, fs, files, info); err != nil {
			t.Fatal(err)
		}
		paths := metadataWritePaths(info, sources)
		for _, f := range files {
			rel, _ := filepath.Rel(root, fs.Position(f.Pos()).Filename)
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				if metadataWriteExempt(rel, fn.Name.Name) {
					continue
				}
				for _, pos := range directMetadataWrites(fn.Body, info, paths) {
					t.Errorf("%s: raw metadata source write/address; use its generated setter", fs.Position(pos))
				}
			}
		}
	}
}

func metadataWriteExempt(file, name string) bool {
	// LoadFrom invalidates the full snapshot after the pointer-based codecs;
	// saving through the same declarations only reads their field pointers.
	return file == "internal/server/entity/mob.go" && name == "NewMob" ||
		file == "internal/server/entity/mobsave.go" && name == "LoadFrom" ||
		file == "internal/server/entity/mobcodec.go" && (name == "entityFields" || name == "mobFields" || name == "kindFields") ||
		file == "internal/server/entity/mobmeta_gen.go" && strings.HasPrefix(name, "SetMetadata")
}

func TestMobMetadataCodecExemptionScope(t *testing.T) {
	fs := token.NewFileSet()
	src := `package entity
 type Mob struct { NoGravity bool }
 func (m *Mob) entityFields() { _ = &m.NoGravity }
 func (m *Mob) mobFields() { _ = &m.NoGravity }
 func (m *Mob) kindFields() { _ = &m.NoGravity }
 func (m *Mob) scalarFields() { m.NoGravity = true }
 func (m *Mob) liveWriter() { m.NoGravity = true }
 `
	f, err := parser.ParseFile(fs, "probe.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	if _, err := new(types.Config).Check("github.com/golem-mc/golem/internal/server/entity", fs, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	paths := metadataWritePaths(info, map[string]bool{"NoGravity": true})
	for file, want := range map[string]int{
		"internal/server/entity/mobcodec.go": 2,
		"internal/server/entity/parrot.go":   5,
		"internal/server/mobdata.go":         5,
	} {
		t.Run(file, func(t *testing.T) {
			count := 0
			for _, decl := range f.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && !metadataWriteExempt(file, fn.Name.Name) {
					count += len(directMetadataWrites(fn.Body, info, paths))
				}
			}
			if count != want {
				t.Fatalf("got %d violations, want %d; only bulk codec declarations may bypass setters", count, want)
			}
		})
	}
}

func metadataWritePaths(info *types.Info, sources map[string]bool) map[string]map[string]bool {
	paths := map[string]map[string]bool{"Mob": sources}
	for expr := range info.Types {
		if metadataOwner(info.TypeOf(expr)) != "Mob" {
			continue
		}
		for path := range sources {
			typ := info.TypeOf(expr)
			base := typ
			if ptr, ok := base.(*types.Pointer); ok {
				base = ptr.Elem()
			}
			pkg := base.(*types.Named).Obj().Pkg()
			parts := strings.Split(path, ".")
			// Lookup indices expose embedded fields hidden by promoted selectors.
			canonicalType := typ
			var names []string
			var owners []types.Type
			for _, part := range parts {
				_, indices, _ := types.LookupFieldOrMethod(canonicalType, true, pkg, part)
				for _, index := range indices {
					storage := canonicalType
					if ptr, ok := storage.(*types.Pointer); ok {
						storage = ptr.Elem()
					}
					field := storage.Underlying().(*types.Struct).Field(index)
					names = append(names, field.Name())
					owners = append(owners, canonicalType)
					canonicalType = field.Type()
				}
			}
			for i, typ := range owners {
				owner := metadataOwner(typ)
				if owner == "" || i > 1 && ast.IsExported(owner) {
					continue
				}
				if paths[owner] == nil {
					paths[owner] = map[string]bool{}
				}
				paths[owner][strings.Join(names[i:], ".")] = true
			}
			for i, part := range parts[:len(parts)-1] {
				obj, _, _ := types.LookupFieldOrMethod(typ, true, pkg, part)
				if obj == nil {
					break
				}
				typ = obj.Type()
				owner := metadataOwner(typ)
				// Coordinates and item-stack components are protected at their
				// owning kind state, rather than globally at generic value types.
				if owner == "" || i > 0 && ast.IsExported(owner) {
					continue
				}
				if paths[owner] == nil {
					paths[owner] = map[string]bool{}
				}
				paths[owner][strings.Join(parts[i+1:], ".")] = true
			}
		}
		break
	}
	return paths
}

func metadataOwner(typ types.Type) string {
	if ptr, ok := typ.(*types.Pointer); ok {
		typ = ptr.Elem()
	}
	n, ok := typ.(*types.Named)
	if !ok || n.Obj().Pkg() == nil || n.Obj().Pkg().Path() != "github.com/golem-mc/golem/internal/server/entity" {
		return ""
	}
	return n.Obj().Name()
}

func directMetadataWrites(body ast.Node, info *types.Info, paths map[string]map[string]bool) []token.Pos {
	// A replacement boundary must immediately reconcile the exact generated
	// ancestor union. Scalar setters cannot use this escape hatch.
	boundaries := map[token.Pos]bool{}
	ast.Inspect(body, func(node ast.Node) bool {
		block, ok := node.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for i, stmt := range block.List {
			assign, ok := stmt.(*ast.AssignStmt)
			if !ok || i+1 == len(block.List) {
				continue
			}
			next, ok := block.List[i+1].(*ast.ExprStmt)
			if !ok {
				continue
			}
			call, ok := next.X.(*ast.CallExpr)
			if !ok || len(call.Args) != 0 {
				continue
			}
			method, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			for _, target := range assign.Lhs {
				expr, path := target, ""
				for {
					if metadataOwner(info.TypeOf(expr)) == "Mob" {
						if path != "" && !paths["Mob"][path] && types.ExprString(expr) == types.ExprString(method.X) && method.Sel.Name == "InvalidateMetadata"+metaPathName("m."+path) {
							boundaries[target.Pos()] = true
						}
						break
					}
					field, ok := expr.(*ast.SelectorExpr)
					if !ok {
						break
					}
					if path != "" {
						path = "." + path
					}
					path, expr = field.Sel.Name+path, field.X
				}
			}
		}
		return true
	})
	matches := func(target ast.Expr, address bool) bool {
		if _, local := target.(*ast.Ident); local {
			return false
		}
		var suffix string
		for expr := target; ; {
			_, dereferenced := target.(*ast.StarExpr)
			if protected := paths[metadataOwner(info.TypeOf(expr))]; protected != nil && (suffix != "" || dereferenced) {
				for path := range protected {
					if suffix == path || strings.HasPrefix(suffix, path+".") ||
						((!address || paths[metadataOwner(info.TypeOf(target))] == nil) && (suffix == "" || strings.HasPrefix(path, suffix+"."))) {
						return true
					}
				}
			}
			switch x := expr.(type) {
			case *ast.SelectorExpr:
				if suffix != "" {
					suffix = "." + suffix
				}
				suffix = x.Sel.Name + suffix
				expr = x.X
			case *ast.StarExpr:
				expr = x.X
			case *ast.IndexExpr:
				expr = x.X
			case *ast.ParenExpr:
				expr = x.X
			default:
				return false
			}
		}
	}
	var out []token.Pos
	ast.Inspect(body, func(node ast.Node) bool {
		var targets []ast.Expr
		switch n := node.(type) {
		case *ast.AssignStmt:
			targets = n.Lhs
		case *ast.IncDecStmt:
			targets = []ast.Expr{n.X}
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				targets = []ast.Expr{n.X}
			}
		}
		_, address := node.(*ast.UnaryExpr)
		for _, target := range targets {
			if !boundaries[target.Pos()] && matches(target, address) {
				out = append(out, target.Pos())
			}
		}
		return true
	})
	return out
}

func TestMobMetadataDirectWritePolicyShapes(t *testing.T) {
	fs := token.NewFileSet()
	// Give the probe Mob the real package identity; no game code is needed.
	src := `package entity
 type Mob struct { Health float64; Antenna struct { Count int } }
 func f(m *Mob) {
  alias := m
  alias.Health++
  alias.Health, m.Health = 1, 2
  _ = &alias.Health
  m.Antenna.Count--
  *alias = Mob{}
  _ = m.Health
 }
 `
	f, err := parser.ParseFile(fs, "probe.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	if _, err := new(types.Config).Check("github.com/golem-mc/golem/internal/server/entity", fs, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	if got := directMetadataWrites(f, info, metadataWritePaths(info, map[string]bool{"Health": true, "Antenna": true})); len(got) != 6 {
		t.Fatalf("got %d violations, want two increments, two tuple stores, address and replacement", len(got))
	}
}

func TestMobMetadataNestedWritePolicyShapes(t *testing.T) {
	fs := token.NewFileSet()
	src := `package entity
 type Waypoint struct { X int }
 type inner struct { Flag bool }
 type promotedState struct { Dance int }
 type kindState struct { promotedState; Flag bool; Pos Waypoint; Jump inner; Other int }
 type Mob struct { State kindState; Type int }
 func (m *Mob) InvalidateMetadataState() {}
 func (m *Mob) InvalidateMetadataType() {}
 func alias(m *Mob) { s := &m.State; s.Flag = true; s.Jump.Flag = true; *s = kindState{} }
 func ownerless(s *kindState) { s.Flag = true }
 func promoted(m *Mob) { p := &m.State.promotedState; p.Dance++; m.State.Dance = 1; m.State.promotedState.Dance = 0; *p = promotedState{} }
 func component(m *Mob) { m.State.Pos.X++; _ = &m.State.Pos; _ = &m.State.Flag }
 func replacement(m *Mob) { m.State = kindState{}; m.InvalidateMetadataState() }
 func wrongUnion(m *Mob) { m.State = kindState{}; m.InvalidateMetadataType() }
 func immutable(m *Mob) { m.Type = 1; m.InvalidateMetadataType() }
 func unrelated(m *Mob) { s := &m.State; s.Other++; pos := Waypoint{}; pos.X++ }
 `
	f, err := parser.ParseFile(fs, "probe.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	if _, err := new(types.Config).Check("github.com/golem-mc/golem/internal/server/entity", fs, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	paths := metadataWritePaths(info, map[string]bool{"State.Dance": true, "State.Flag": true, "State.Pos.X": true, "State.Jump.Flag": true, "Type": true})
	want := map[string]int{"alias": 3, "promoted": 4, "ownerless": 1, "component": 3, "replacement": 0, "wrongUnion": 1, "immutable": 1, "unrelated": 0}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if count, ok := want[fn.Name.Name]; ok {
			if got := directMetadataWrites(fn.Body, info, paths); len(got) != count {
				t.Errorf("%s: got %d violations, want %d", fn.Name.Name, len(got), count)
			}
		}
	}
}
