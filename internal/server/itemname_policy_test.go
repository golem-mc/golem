package server_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestNoItemLogicOnNames(t *testing.T) {
	t.Parallel()
	var violations []blockNameViolation
	for _, v := range itemNameViolations(t, moduleRoot(t)) {
		violations = append(violations, blockNameViolation{file: v.file, line: v.line, kind: v.kind})
	}
	checkNameAllowances(t, violations, registryItemNameAllowed)
}

func TestItemNamePolicyCatchesProbe(t *testing.T) {
	t.Parallel()
	src := `package p
func f(id int32) {
	registry.ItemID("minecraft:bow")
	name, _ := registry.ItemName(id)
	if name == "minecraft:bow" {}
	_ = registry.Equippable[name]
	other, _ := registry.ItemName(id)
	consume(other)
}
func g(s string) {
	if s == "minecraft:ink_sac" {}
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "probe.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	got := scanItemNames(f, fset, "probe.go")
	want := []string{"literal item ID lookup", "index on registry.Equippable", "comparison on an item name", "comparison with an item name literal"}
	var kinds []string
	for _, v := range got {
		kinds = append(kinds, v.kind)
		if v.file != "probe.go" || v.line <= 0 {
			t.Errorf("invalid location: %+v", v)
		}
	}
	slices.Sort(kinds)
	slices.Sort(want)
	if !slices.Equal(kinds, want) {
		t.Fatalf("probe produced %q, want %q", kinds, want)
	}
}

func TestItemNamePolicyShapes(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		src  string
		want int
	}{
		{"aliased item lookup", `import r "github.com/golem-mc/golem/registry"; func f() { r.MustItem("minecraft:bow"); r.ItemByName("minecraft:bow"); r.ItemHasTag(id, tag) }`, 3},
		{"typed alternatives", `func f(i registry.Item) { _ = i == registry.ItemBow; _ = i.InTag(registry.ItemTagIDArrows) }`, 0},
		{"item wrapper", `func f(name string) { registry.MustItem(name); registry.ItemByName(name) }`, 2},
		{"item constant", `const item = "minecraft:bow"; var x = registry.MustItem(item)`, 1},
		{"item concatenation", `var x = registry.ItemByName("minecraft:" + kind)`, 1},
		{"typed item lookup", `func f() { registry.MustItem("minecraft:bow"); registry.ItemByName("minecraft:bow") }`, 2},
		{"string item tag", `func f(tag string) { registry.ItemHasTag(id, "minecraft:arrows"); registry.ItemHasTag(id, tag) }`, 2},
		{"item lookup initializer", `var x = registry.MustItem("minecraft:bow")`, 1},
		{"must lookup", `func f() { mustItemID("bow"); helper.mustItemID("bow") }`, 2},
		{"dynamic lookup", `func f(name string) { registry.ItemID(name); mustItemID(name); other.ItemID("bow") }`, 0},
		{"reversed and unequal", `func f() { name, ok := registry.ItemName(1); _ = ok; _ = "bow" != name }`, 1},
		{"switch", `func f() { name, _ := registry.ItemName(1); switch name { case "bow", "shield": } }`, 1},
		{"expression switch", `func f() { name, _ := registry.ItemName(1); switch { case name == "bow": } }`, 1},
		{"var declaration", `func f() { var name, ok = registry.ItemName(1); _ = ok; _ = name == "bow" }`, 1},
		{"assignment", `func f() { var name string; name, _ = registry.ItemName(1); _ = name == "bow" }`, 1},
		{"other result", `func f() { _, ok := registry.ItemName(1); _ = ok == "bow" }`, 0},
		{"other function", `func f() { name, _ := registry.ItemName(1); consume(name) }; func g(name string) { _ = name == "bow" }`, 0},
		{"closure", `func f() { _ = func() { name, _ := registry.ItemName(1); _ = name == "bow" } }`, 1},
		{"separate closure", `func f() { name, _ := registry.ItemName(1); consume(name); _ = func(name string) { _ = name == "bow" } }`, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "probe.go", "package p\n"+tt.src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			if got := scanItemNames(f, fset, "probe.go"); len(got) != tt.want {
				t.Fatalf("got %v, want %d violations", got, tt.want)
			}
		})
	}
}

type itemNameViolation struct {
	file string
	line int
	kind string
}

func itemNameViolations(t *testing.T, root string) []itemNameViolation {
	t.Helper()
	var out []itemNameViolation
	walkNamePolicy(t, root, func(f *ast.File, fset *token.FileSet, rel string) {
		out = append(out, scanItemNames(f, fset, rel)...)
	})
	return out
}

func scanItemNames(f *ast.File, fset *token.FileSet, rel string) []itemNameViolation {
	var out []itemNameViolation
	add := func(p token.Pos, kind string) {
		out = append(out, itemNameViolation{file: rel, line: fset.Position(p).Line, kind: kind})
	}
	literal := func(p token.Pos, e ast.Expr) {
		s, ok := stringLit(e)
		if !isItemOnlyName(s, ok) {
			return
		}
		add(p, "comparison with an item name literal")
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BinaryExpr:
			if n.Op == token.EQL || n.Op == token.NEQ {
				literal(n.Pos(), n.X)
				literal(n.Pos(), n.Y)
			}
		case *ast.CaseClause:
			for _, e := range n.List {
				literal(n.Pos(), e)
			}
		case *ast.CallExpr:
			must := false
			switch fn := n.Fun.(type) {
			case *ast.Ident:
				must = fn.Name == "mustItemID"
			case *ast.SelectorExpr:
				must = fn.Sel.Name == "mustItemID"
			}
			if (must || registryName(f, n.Fun, "ItemID", "MustItem", "ItemByName")) && len(n.Args) > 0 {
				if _, ok := stringLit(n.Args[0]); ok {
					add(n.Pos(), "literal item ID lookup")
				} else if registryName(f, n.Fun, "MustItem", "ItemByName") {
					add(n.Pos(), "string item lookup")
				}
			}
			if registryName(f, n.Fun, "ItemHasTag") {
				add(n.Pos(), "string item tag lookup")
			}
		case *ast.IndexExpr:
			if registryName(f, n.X, "Equippable") {
				add(n.Pos(), "index on registry.Equippable")
			}
		case *ast.FuncDecl:
			scanItemNameFunction(f, n.Body, add)
		case *ast.FuncLit:
			scanItemNameFunction(f, n.Body, add)
		}
		return true
	})
	// A literal comparison on an ItemName result is already reported as such.
	named := map[int]bool{}
	for _, v := range out {
		if v.kind == "comparison on an item name" || v.kind == "case on an item name" {
			named[v.line] = true
		}
	}
	return slices.DeleteFunc(out, func(v itemNameViolation) bool {
		return v.kind == "comparison with an item name literal" && named[v.line]
	})
}

// isItemOnlyName reports whether s is an item ID that is not also a block or
// entity name (those belong to the block-name policy).
func isItemOnlyName(s string, ok bool) bool {
	if !ok {
		return false
	}
	if _, ok := registry.ItemID(s); !ok {
		return false
	}
	if _, ok := registry.BlockByName(s); ok {
		return false
	}
	_, ok = registry.EntityTypeID(s)
	return !ok
}

// scanItemNameFunction reports comparisons and switches on item names in body.
func scanItemNameFunction(f *ast.File, body *ast.BlockStmt, add func(token.Pos, string)) {
	scanNameFunction(body, func(c *ast.CallExpr) bool { return registryName(f, c.Fun, "ItemName") },
		"comparison on an item name", "case on an item name", add)
}

// scanNameFunction reports the == / != comparisons of a string literal with a
// name in body, and (when caseKind is not empty) the switches on one. A name is
// a variable assigned the first result of a call isSource accepts, or such a
// call used directly. It works per function: a closure is scanned on its own.
func scanNameFunction(body *ast.BlockStmt, isSource func(*ast.CallExpr) bool, compareKind, caseKind string, add func(token.Pos, string)) {
	if body == nil {
		return
	}
	// Two passes also catch assignments and comparisons in separate branches.
	names := map[string]bool{}
	assign := func(lhs []ast.Expr, rhs []ast.Expr) {
		if len(lhs) == 0 || len(rhs) == 0 {
			return
		}
		call, ok := rhs[0].(*ast.CallExpr)
		if !ok || !isSource(call) {
			return
		}
		if id, ok := lhs[0].(*ast.Ident); ok && id.Name != "_" {
			names[id.Name] = true
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			assign(n.Lhs, n.Rhs)
		case *ast.ValueSpec:
			lhs := make([]ast.Expr, len(n.Names))
			for i, name := range n.Names {
				lhs[i] = name
			}
			assign(lhs, n.Values)
		}
		return true
	})
	isName := func(e ast.Expr) bool {
		switch e := e.(type) {
		case *ast.Ident:
			return names[e.Name]
		case *ast.CallExpr:
			return isSource(e)
		}
		return false
	}
	compares := func(a, b ast.Expr) bool {
		_, literal := stringLit(b)
		return isName(a) && literal
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.BinaryExpr:
			if (n.Op == token.EQL || n.Op == token.NEQ) && (compares(n.X, n.Y) || compares(n.Y, n.X)) {
				add(n.Pos(), compareKind)
			}
		case *ast.SwitchStmt:
			if caseKind != "" && isName(n.Tag) {
				for _, stmt := range n.Body.List {
					c := stmt.(*ast.CaseClause)
					for _, e := range c.List {
						if _, ok := stringLit(e); ok {
							add(c.Pos(), caseKind)
							break
						}
					}
				}
			}
		}
		return true
	})
}
