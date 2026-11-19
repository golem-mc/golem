package server_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// blockNames are the registry names of every block.
var blockNames = func() []string {
	names := make([]string, 0, registry.BlockCount)
	for b := range registry.Block(registry.BlockCount) {
		names = append(names, b.Name())
	}
	return names
}()

// isBlockName reports whether s is a registry block name.
func isBlockName(s string) bool {
	_, ok := registry.BlockByName(s)
	return ok
}

// isBlockNamePart reports whether a literal passed to a strings function
// tests part of a block name. Only literals that match a real block name
// count, so item, entity and text prefixes are not flagged.
func isBlockNamePart(fn, s string) bool {
	if s == "" || s == "minecraft:" {
		return false
	}
	for _, n := range blockNames {
		switch fn {
		case "HasPrefix", "TrimPrefix":
			if strings.HasPrefix(n, s) && (strings.HasPrefix(s, "minecraft:") || strings.HasSuffix(s, "_")) {
				return true
			}
		case "HasSuffix", "TrimSuffix":
			if strings.HasSuffix(n, s) && strings.HasPrefix(s, "_") {
				return true
			}
		default:
			if strings.Contains(n, s) && len(s) >= 3 && (strings.HasPrefix(s, "_") || strings.HasSuffix(s, "_")) {
				return true
			}
		}
	}
	return false
}

// nameKeyedMaps are the generated registry maps keyed by block name, whose
// typed forms are Block.Type, Block.NoCollision, Block.Concrete and
// Block.Scraped, Block.Stripped, Block.Unwaxed, Block.Waxed and Block.Flattened.
var nameKeyedMaps = map[string]bool{"BlockTypes": true, "NoCollision": true, "ConcreteOf": true, "CopperScraped": true, "AxeStrippables": true, "CopperWaxOff": true, "ShovelFlattenables": true}

// blockNamePolicyExempt skips command and data-pack parsers and test helpers
// whose inputs are registry names.
func blockNamePolicyExempt(rel string) bool {
	for _, dir := range []string{"internal/server/command/", "internal/server/datapack/", "internal/server/tests/", "internal/server/servertest/", "tools/"} {
		if strings.HasPrefix(rel, dir) {
			return true
		}
	}
	return false
}

// TestNoBlockLogicOnNames holds the rule that block logic runs on
// registry.Block, a tag or a per-block table: a name suffix or prefix test, a
// map[string]bool of block names and a case on a block name are string
// patterns that the typed tables replaced.
func TestNoBlockLogicOnNames(t *testing.T) {
	t.Parallel()
	checkNameAllowances(t, blockNameViolations(t, moduleRoot(t)), registryBlockNameAllowed)
}

// TestBlockNamePolicyCatchesProbe checks that the scan fails on each shape it
// forbids.
func TestBlockNamePolicyCatchesProbe(t *testing.T) {
	t.Parallel()
	src := `package p

import "strings"

var set = map[string]bool{"minecraft:stone": true}
var x = registry.MustState("minecraft:stone", nil)
var set2 = map[string]struct{}{"minecraft:dirt": {}}

func f(name string) bool {
	switch name {
	case "minecraft:grass_block":
		return true
	}
	n, _ := registry.BlockStateName(1)
	_ = n == "minecraft:tnt"
	_ = "minecraft:tnt" != registryStateName(2)
	_ = registry.BlockOf(3).Name() == "minecraft:fire"
	_ = unrelated(name) == "minecraft:stone"
	_ = registry.FindState("minecraft:stone", nil)
	_ = registry.MustState("minecraft:stone", nil)
	_ = registry.BlockByName("minecraft:stone")
	_ = registry.BlockStates["minecraft:stone"]
	_ = registry.BlockTagOf("minecraft:logs")
	return strings.HasSuffix(name, "_slab") || strings.HasPrefix(name, "minecraft:red_") ||
		strings.Contains(name, "_wall") || registry.NoCollision[name] || strings.TrimPrefix(name, "minecraft:waxed_") != ""
}
`
	f, err := parser.ParseFile(token.NewFileSet(), "probe.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, v := range scanBlockNames(f, token.NewFileSet(), "probe.go") {
		kinds = append(kinds, v.kind)
	}
	if got, want := len(kinds), 11; got != want {
		t.Fatalf("probe produced %d violations %q, want %d", got, kinds, want)
	}
}

type blockNameViolation struct {
	file string
	line int
	kind string
}

func blockNameViolations(t *testing.T, root string) []blockNameViolation {
	t.Helper()
	var out []blockNameViolation
	walkNamePolicy(t, root, func(f *ast.File, fset *token.FileSet, rel string) {
		out = append(out, scanBlockNames(f, fset, rel)...)
	})
	return out
}

// scanBlockNames lists the block-name pattern uses in f.
func scanBlockNames(f *ast.File, fset *token.FileSet, rel string) []blockNameViolation {
	var out []blockNameViolation
	add := func(p token.Pos, kind string) {
		out = append(out, blockNameViolation{file: rel, line: fset.Position(p).Line, kind: kind})
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if !ok || len(n.Args) != 2 {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "strings" {
				return true
			}
			switch sel.Sel.Name {
			case "HasSuffix", "HasPrefix", "TrimPrefix", "TrimSuffix", "Contains":
				if lit, ok := stringLit(n.Args[1]); ok && isBlockNamePart(sel.Sel.Name, lit) {
					add(n.Pos(), "strings."+sel.Sel.Name+" with a block-name literal")
				}
			}
		case *ast.CompositeLit:
			m, ok := n.Type.(*ast.MapType)
			if !ok || !isBlockNameSet(m) {
				return true
			}
			for _, e := range n.Elts {
				kv, ok := e.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if lit, ok := stringLit(kv.Key); ok && isBlockName(lit) {
					add(n.Pos(), "set literal of block names")
					break
				}
			}
		case *ast.SelectorExpr:
			if pkg, ok := n.X.(*ast.Ident); ok && pkg.Name == "registry" && nameKeyedMaps[n.Sel.Name] {
				add(n.Pos(), "registry."+n.Sel.Name+", a name-keyed map")
			}
		case *ast.FuncDecl:
			scanBlockNameFunction(f, n.Body, add)
		case *ast.FuncLit:
			scanBlockNameFunction(f, n.Body, add)
		case *ast.CaseClause:
			for _, e := range n.List {
				if lit, ok := stringLit(e); ok && isBlockName(lit) {
					add(e.Pos(), "case on a block name")
					break
				}
			}
		}
		return true
	})
	return out
}

// scanBlockNameFunction reports comparisons of a literal block name with the
// result of registry.BlockStateName, registryStateName or
// registry.BlockOf(...).Name(). Case clauses are reported by scanBlockNames.
func scanBlockNameFunction(f *ast.File, body *ast.BlockStmt, add func(token.Pos, string)) {
	scanNameFunction(body, func(c *ast.CallExpr) bool { return isBlockNameSource(f, c) }, "comparison on a block name", "", add)
}

// isBlockNameSource reports whether c yields a block name: its first result.
func isBlockNameSource(f *ast.File, c *ast.CallExpr) bool {
	if registryName(f, c.Fun, "BlockStateName") {
		return true
	}
	if id, ok := c.Fun.(*ast.Ident); ok {
		return id.Name == "registryStateName"
	}
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if inner, ok := sel.X.(*ast.CallExpr); ok && sel.Sel.Name == "Name" && len(c.Args) == 0 {
		return registryName(f, inner.Fun, "BlockOf")
	}
	return false
}

func isBlockNameSet(m *ast.MapType) bool {
	if k, ok := m.Key.(*ast.Ident); !ok || k.Name != "string" {
		return false
	}
	switch v := m.Value.(type) {
	case *ast.Ident:
		return v.Name == "bool"
	case *ast.StructType:
		return len(v.Fields.List) == 0
	}
	return false
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

func TestBlockNamePolicyLocalSources(t *testing.T) {
	t.Parallel()
	src := `package registry
func f() {
 name, _ := BlockStateName(1)
 _ = name == "minecraft:stone"
 _ = BlockOf(1).Name() == "minecraft:stone"
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "probe.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	if got := scanBlockNames(f, fset, "probe.go"); len(got) != 2 {
		t.Fatalf("got %v, want 2 violations", got)
	}
}
