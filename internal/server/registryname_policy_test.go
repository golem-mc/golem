package server_test

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// walkNamePolicy includes registry's codecs and helpers, but skips generated
// data and packages whose job is to interpret names supplied by the user.
func walkNamePolicy(t *testing.T, root string, scan func(*ast.File, *token.FileSet, string)) {
	t.Helper()
	fset := token.NewFileSet()
	for _, top := range []string{"internal/server", "registry"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if blockNamePolicyExempt(rel + "/") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, "_gen.go") {
				return nil
			}
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err == nil {
				scan(f, fset, rel)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// registryName also recognizes calls within registry itself and import aliases.
func registryName(f *ast.File, e ast.Expr, names ...string) bool {
	if id, ok := e.(*ast.Ident); ok {
		return f.Name.Name == "registry" && slices.Contains(names, id.Name)
	}
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || !slices.Contains(names, sel.Sel.Name) {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	for _, imp := range f.Imports {
		path, _ := stringLit(imp.Path)
		if path == "github.com/golem-mc/golem/registry" {
			name := "registry"
			if imp.Name != nil {
				name = imp.Name.Name
			}
			return pkg.Name == name
		}
	}
	// Probe snippets omit imports unrelated to their AST shape.
	return pkg.Name == "registry"
}

// reflectedNames identifies reflect.Type.Name even when other imports in this
// single-file check cannot be resolved. Unresolved receivers remain policy checks.
func reflectedNames(f *ast.File, fset *token.FileSet) map[*ast.SelectorExpr]bool {
	if !slices.ContainsFunc(f.Imports, func(imp *ast.ImportSpec) bool {
		path, _ := stringLit(imp.Path)
		return path == "reflect"
	}) {
		return nil
	}
	info := &types.Info{Selections: make(map[*ast.SelectorExpr]*types.Selection)}
	conf := types.Config{Importer: importer.Default(), Error: func(error) {}}
	_, _ = conf.Check(f.Name.Name, fset, []*ast.File{f}, info)
	names := make(map[*ast.SelectorExpr]bool)
	for expr, selection := range info.Selections {
		obj := selection.Obj()
		if obj.Name() == "Name" && obj.Pkg() != nil && obj.Pkg().Path() == "reflect" {
			names[expr] = true
		}
	}
	return names
}

func nameCall(e ast.Expr, reflected map[*ast.SelectorExpr]bool) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Name" && !reflected[sel]
}

func hasString(e ast.Expr) bool {
	if _, ok := stringLit(e); ok {
		return true
	}
	if b, ok := e.(*ast.BinaryExpr); ok && b.Op == token.ADD {
		return hasString(b.X) || hasString(b.Y)
	}
	return false
}

// registryKeyUses binds a codec's validator parameter to its explicit registry.
// Resolve objects so shadowed variables and unrelated comparisons stay checked.
func registryKeyUses(f *ast.File, fset *token.FileSet) map[ast.Expr]string {
	params := make(map[*ast.Ident]string)
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !registryName(f, call.Fun, "textRegistryMalformed") || len(call.Args) != 3 {
			return true
		}
		reg, ok := stringLit(call.Args[1])
		if !ok || reg == "" {
			return true
		}
		validator, ok := call.Args[2].(*ast.FuncLit)
		if ok && len(validator.Type.Params.List) == 1 && len(validator.Type.Params.List[0].Names) == 1 {
			params[validator.Type.Params.List[0].Names[0]] = reg
		}
		return true
	})
	if len(params) == 0 {
		return nil
	}
	info := &types.Info{Defs: make(map[*ast.Ident]types.Object), Uses: make(map[*ast.Ident]types.Object)}
	// Single-file checks may have unresolved imports; local parameter bindings
	// still resolve without importing the rest of the project.
	conf := types.Config{Error: func(error) {}}
	_, _ = conf.Check(f.Name.Name, fset, []*ast.File{f}, info)
	objects := make(map[types.Object]string)
	for param, reg := range params {
		if obj := info.Defs[param]; obj != nil {
			objects[obj] = reg
		}
	}
	uses := make(map[ast.Expr]string)
	for id, obj := range info.Uses {
		if reg := objects[obj]; reg != "" {
			uses[id] = reg
		}
	}
	return uses
}

func entityName(e ast.Expr, keyRegistry string) bool {
	if keyRegistry != "" && keyRegistry != "minecraft:entity_type" {
		return false
	}
	s, ok := stringLit(e)
	if !ok || !strings.HasPrefix(s, "minecraft:") {
		return false
	}
	_, exists := registry.EntityTypeID(s)
	return exists
}

// scanRegistryNames checks lookup APIs wherever they appear: a wrapper's name
// parameter, an initializer and a concatenation are still string lookups.
func scanRegistryNames(f *ast.File, fset *token.FileSet, rel string) []blockNameViolation {
	var out []blockNameViolation
	reflected := reflectedNames(f, fset)
	keyUses := registryKeyUses(f, fset)
	add := func(n ast.Node, kind string) {
		out = append(out, blockNameViolation{file: rel, line: fset.Position(n.Pos()).Line, kind: kind})
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			var called string
			switch fun := n.Fun.(type) {
			case *ast.Ident:
				called = fun.Name
			case *ast.SelectorExpr:
				called = fun.Sel.Name
			}
			if slices.Contains([]string{"putSoundEvent", "soundEffectAt", "entitySoundEffect", "broadcastSound", "broadcastSoundExcept", "broadcastEntitySound"}, called) {
				add(n, "name-based sound playback")
			}
			if registryName(f, n.Fun, "FindState", "MustState", "BlockByName", "BlockTagOf", "BlockTagMembers", "NameInTag", "IsReplaceable") {
				add(n, "string block lookup")
			}
			if registryName(f, n.Fun, "EntityTypeID", "EntityTypeByName") && len(n.Args) > 0 && hasString(n.Args[0]) {
				add(n, "literal entity lookup")
			}
			if registryName(f, n.Fun, "GameEventByName", "SoundEventID", "SoundHolderByName", "EnvAttributeByName", "BiomeByName", "ParticleID", "EntityTypeTagMembers", "EntityTypeInTag", "ItemTagOf", "EntityTypeTagOf", "BiomeTagOf", "BiomeInTag") {
				add(n, "string registry lookup")
			}
			// Compatibility APIs must not bring string lookups back onto hurt paths.
			if registryName(f, n.Fun, "LookupDamageType", "DamageTypeInTag", "DamageTypeByName", "DamageTypeTagOf") {
				add(n, "string damage lookup")
			}
			if registryName(f, n.Fun, "MustID") && len(n.Args) == 2 {
				if reg, ok := stringLit(n.Args[0]); ok && slices.Contains([]string{
					"minecraft:worldgen/biome", "minecraft:point_of_interest_type", "minecraft:villager_profession",
					"minecraft:game_event", "minecraft:damage_type", "minecraft:particle_type", "minecraft:sound_event",
				}, reg) {
					add(n, "string registry lookup")
				}
			}
			if len(n.Args) == 1 && hasString(n.Args[0]) {
				if id, ok := n.Fun.(*ast.Ident); ok && f.Name.Name == "world" && id.Name == "RuleIndex" {
					add(n, "literal gamerule lookup")
				} else if sel, ok := n.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "RuleIndex" {
					if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "world" {
						add(n, "literal gamerule lookup")
					}
				}
			}
			if sel, ok := n.Fun.(*ast.SelectorExpr); ok && len(n.Args) == 2 {
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "strings" &&
					slices.Contains([]string{"Contains", "HasSuffix", "HasPrefix"}, sel.Sel.Name) {
					if nameCall(n.Args[0], reflected) {
						add(n, "string test on Name()")
					} else if part, ok := stringLit(n.Args[1]); ok && part != "" && part != "minecraft:" {
						for _, name := range registry.EntityTypes {
							match := sel.Sel.Name == "HasSuffix" && strings.HasSuffix(name, part) ||
								sel.Sel.Name == "HasPrefix" && strings.HasPrefix(name, part)
							if match {
								add(n, "string test with an entity name part")
								break
							}
						}
					}
				}
			}
		case *ast.IndexExpr:
			if registryName(f, n.X, "AllStates", "BlockStates") {
				add(n, "name-keyed block states")
			}
		case *ast.BinaryExpr:
			if (n.Op == token.EQL || n.Op == token.NEQ) && (entityName(n.X, keyUses[n.Y]) || entityName(n.Y, keyUses[n.X])) {
				add(n, "comparison with an entity name")
			}
		case *ast.SwitchStmt:
			for _, stmt := range n.Body.List {
				clause := stmt.(*ast.CaseClause)
				if slices.ContainsFunc(clause.List, func(e ast.Expr) bool { return entityName(e, keyUses[n.Tag]) }) {
					add(clause, "case on an entity name")
				}
			}
		}
		return true
	})
	return out
}

// nameAllowance is a fixed number of sites in one file, so adding a second
// lookup to an allowed file fails too. issue tracks migration work; boundary
// explains sites that must decode names from external data or generated tables.
type nameAllowance struct {
	count    int
	issue    int
	boundary string
}

func checkNameAllowances(t interface {
	Helper()
	Errorf(string, ...any)
}, violations []blockNameViolation, allowed map[string]nameAllowance) {
	t.Helper()
	seen := map[string]int{}
	for _, v := range violations {
		key := v.file + ": " + v.kind
		seen[key]++
		if seen[key] > allowed[key].count {
			t.Errorf("%s:%d: %s; use a typed registry ID (allowlist key %q)", v.file, v.line, v.kind, key)
		}
	}
	for key, a := range allowed {
		if a.count <= 0 || (a.issue <= 0 && a.boundary == "") {
			t.Errorf("allowance %q needs a positive count and an issue or boundary reason", key)
		}
		if seen[key] < a.count {
			t.Errorf("allowance %q has %d sites, now %d; shrink or delete it", key, a.count, seen[key])
		}
	}
}

func TestNoRegistryLogicOnNames(t *testing.T) {
	t.Parallel()
	var violations []blockNameViolation
	walkNamePolicy(t, moduleRoot(t), func(f *ast.File, fset *token.FileSet, rel string) {
		violations = append(violations, scanRegistryNames(f, fset, rel)...)
	})
	checkNameAllowances(t, violations, registryNameAllowed)
}

func TestRegistryNamePolicyShapes(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, src string
		want      int
	}{
		{"registry import alias", `import r "github.com/golem-mc/golem/registry"; func f() { r.MustState("stone", nil); r.GameEventByName("minecraft:step") }`, 2},
		{"unrelated API", `func f() { other.FindState("stone", nil); other.ParticleID("flame"); thing.IsReplaceable() }`, 0},
		{"closure", `var x = func() { registry.FindState("stone", nil) }`, 1},
		{"cauldronLevels", `func cauldronLevels(name string) { registry.FindState(name, props) }`, 1},
		{"initializer", `var x = registry.MustState("stone", nil)`, 1},
		{"init", `func init() { registry.FindState("stone", nil) }`, 1},
		{"wrappers", `func shapeBlock(name string) { registry.BlockByName(name) }; func mustBlock(name string) { registry.BlockByName(name) }`, 2},
		{"parameter", `func IsLowerHalfBelow(name string) { registry.FindState(name, nil) }`, 1},
		{"constant", `const name = "minecraft:bubble_column"; func bubbleState() { registry.FindState(name, nil) }`, 1},
		{"concat", `var x = registry.BlockByName("minecraft:dead_" + kind)`, 1},
		{"state maps", `func f(name string) { _ = registry.AllStates[name]; _ = registry.BlockStates["stone"] }`, 2},
		{"tag members", `func f(name string) { registry.BlockTagMembers(name); registry.NameInTag(name, t); registry.IsReplaceable(name) }`, 3},
		{"boat suffix", `func f(kind string) { strings.HasSuffix(kind, "_boat"); strings.HasSuffix(kind, "_chest_boat") }`, 2},
		{"tag lookups", `func f(tag string) { registry.ItemTagOf(tag); registry.EntityTypeTagOf(tag); registry.EntityTypeInTag(id, tag); registry.BiomeTagOf(tag); registry.BiomeInTag(biome, tag) }`, 5},
		{"typed alternatives", `func f(b registry.Block, i registry.Item, e registry.EntityType) { _ = b.DefaultState(); _ = b.InTag(registry.TagIDLogs); _ = b.IsReplaceable(); _ = i.InTag(registry.ItemTagIDArrows); _ = e.InTag(registry.EntityTypeTagIDUndead); _ = b == registry.BlockStone }`, 0},
		{"entity lookup", `var x = registry.EntityTypeID("minecraft:blaze")`, 1},
		{"ordinary words", `func f(word string) { _ = word == "item"; switch word { case "player", "zombie": } }`, 0},
		{"entity comparison", `func f(kind string) { _ = kind == "minecraft:ender_dragon"; _ = "minecraft:blaze" != kind; switch kind { case "minecraft:blaze": } }`, 3},
		{"name tests", `func f(b registry.Block) { strings.Contains(b.Name(), "glass_pane"); strings.HasPrefix(b.Name(), prefix); strings.HasSuffix(kind.Name(), "boat") }`, 3},
		{"reflected event type", `import "reflect"; func f(t reflect.Type) { strings.HasSuffix(t.Elem().Name(), "Event") }`, 0},
		{"reflection import alias", `import r "reflect"; func f(t r.Type) { strings.HasSuffix(t.Elem().Name(), "Event") }`, 0},
		{"reflection with registry names", `import "reflect"; func f(t reflect.Type, b registry.Block) { strings.HasSuffix(t.Elem().Name(), "Event"); strings.Contains(b.Name(), "glass_pane"); strings.HasPrefix(kind.Name(), prefix) }`, 2},
		{"nonreflection Elem", `func f(t other.Type) { strings.HasSuffix(t.Elem().Name(), "Event") }`, 1},
		{"shadowed reflection parameter", `import "reflect"; func f(t reflect.Type) { strings.HasSuffix(t.Elem().Name(), "Event"); { t := other; strings.HasSuffix(t.Elem().Name(), "Event") } }`, 1},
		{"reflection entity suffix", `import "reflect"; func f(t reflect.Type) { strings.HasSuffix(t.Elem().Name(), "_boat") }`, 1},
		{"other strings", `func f(name string) { strings.Contains(name, "glass_pane"); strings.HasPrefix(name, "unrelated:"); strings.HasSuffix(name, ".json") }`, 0},
		{"game event", `func f() { registry.GameEventByName("minecraft:step") }`, 1},
		{"typed damage alternatives", `func f(d registry.DamageType) { _ = d.Info(); _ = d.InTag(registry.DamageTypeTagIDIsFire); _ = d == registry.DamageTypeFall }`, 0},
		{"damage type", `func f(name string) { registry.DamageTypeByName(name) }`, 1},
		{"damage tag", `func f(tag string) { registry.DamageTypeTagOf(tag) }`, 1},
		{"damage", `func f() { registry.LookupDamageType("minecraft:fall"); registry.DamageTypeInTag(typ, "minecraft:is_fire") }`, 2},
		{"gamerule", `func f() { world.RuleIndex("minecraft:do_mob_spawning") }`, 1},
		{"sound", `func f() { registry.SoundEventID("minecraft:entity.cat.purr") }`, 1},
		{"sound wrapper", `func f(name string) { soundEffectAt(name); s.broadcastSound(name); registry.SoundHolderByName(name) }`, 3},
		{"environment", `func f() { registry.EnvAttributeByName("minecraft:gameplay/can_start_raid") }`, 1},
		{"biome", `func f() { registry.BiomeByName("minecraft:plains") }`, 1},
		{"particle", `func f() { registry.ParticleID("minecraft:flame") }`, 1},
		{"POI", `func f() { registry.MustID("minecraft:point_of_interest_type", "minecraft:home") }`, 1},
		{"profession", `func f() { registry.MustID("minecraft:villager_profession", "minecraft:farmer") }`, 1},
		{"entity tag members", `func f() { registry.EntityTypeTagMembers(tag) }`, 1},
		{"local gamerule", `func f() { RuleIndex("minecraft:do_mob_spawning") }`, 1},
		{"dynamic gamerule boundary", `func f(name string) { world.RuleIndex(name) }`, 0},
		{"registry helpers", `func f(name string) { FindState(name, nil); NameInTag(name, t); _ = AllStates[name] }`, 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pkg := "p"
			if tt.name == "registry helpers" {
				pkg = "registry"
			}
			if tt.name == "local gamerule" {
				pkg = "world"
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "probe.go", "package "+pkg+"\n"+tt.src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			got := scanRegistryNames(f, fset, "probe.go")
			if len(got) != tt.want {
				t.Fatalf("got %+v, want %d violations", got, tt.want)
			}
			for _, v := range got {
				if v.file != "probe.go" || v.line <= 0 {
					t.Errorf("invalid location: %+v", v)
				}
			}
		})
	}
}

func TestRegistryNamePolicyRegistryCollisions(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, src string
		want      int
	}{
		{"dialog body", `func f(tag any) { textRegistryMalformed(tag, "minecraft:dialog_body_type", func(name string) bool { return name == "minecraft:plain_message" || name == "minecraft:item" }) }`, 0},
		{"reversed comparison", `func f(tag any) { textRegistryMalformed(tag, "minecraft:dialog_body_type", func(name string) bool { return "minecraft:item" != name }) }`, 0},
		{"dialog body switch", `func f(tag any) { textRegistryMalformed(tag, "minecraft:dialog_body_type", func(name string) bool { switch name { case "minecraft:item": return true }; return false }) }`, 0},
		{"entity comparison", `func f(kind string) { _ = kind == "minecraft:item"; _ = "minecraft:item" != kind; switch kind { case "minecraft:item": } }`, 3},
		{"entity registry", `func f(tag any) { textRegistryMalformed(tag, "minecraft:entity_type", func(name string) bool { switch name { case "minecraft:item", "minecraft:blaze": return true }; return name == "minecraft:item" }) }`, 2},
		{"dynamic registry", `func f(tag any, reg string) { textRegistryMalformed(tag, reg, func(name string) bool { return name == "minecraft:item" }) }`, 1},
		{"unrelated value in callback", `func f(tag any, kind string) { textRegistryMalformed(tag, "minecraft:dialog_body_type", func(name string) bool { _ = kind == "minecraft:item"; switch kind { case "minecraft:blaze": }; return name == "minecraft:item" }) }`, 2},
		{"shadowed callback parameter", `func f(tag any) { textRegistryMalformed(tag, "minecraft:dialog_body_type", func(name string) bool { { name := "minecraft:item"; _ = name == "minecraft:item"; switch name { case "minecraft:item": } }; return name == "minecraft:item" }) }`, 2},
		{"captured callback parameter", `func f(tag any) { textRegistryMalformed(tag, "minecraft:dialog_body_type", func(name string) bool { return func() bool { return name == "minecraft:item" }() }) }`, 0},
		{"nested entity callback", `func f(tag any) { textRegistryMalformed(tag, "minecraft:dialog_body_type", func(name string) bool { textRegistryMalformed(tag, "minecraft:entity_type", func(name string) bool { return name == "minecraft:item" }); return name == "minecraft:item" }) }`, 1},
		{"unrelated helper", `func f(tag any) { other.textRegistryMalformed(tag, "minecraft:dialog_body_type", func(name string) bool { return name == "minecraft:item" }) }`, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			src := "package registry\nfunc textRegistryMalformed(tag any, registry string, known func(string) bool) string { return \"\" }\n" + tt.src
			f, err := parser.ParseFile(fset, "probe.go", src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			got := scanRegistryNames(f, fset, "probe.go")
			if len(got) != tt.want {
				t.Fatalf("got %+v, want %d violations", got, tt.want)
			}
		})
	}
}

// namePolicyErrors captures expected policy failures without failing the outer test.
type namePolicyErrors struct{ errors []string }

func (*namePolicyErrors) Helper() {}
func (r *namePolicyErrors) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func TestNamePolicyAllowancesOnlyShrink(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name                string
		sites, count, issue int
		boundary            string
		want                int
	}{
		{"pending migration", 1, 1, 6552, "", 0},
		{"boundary", 1, 1, 0, "data codec", 0},
		{"untracked addition", 1, 0, 0, "", 1},
		{"addition in allowed file", 2, 1, 6552, "", 1},
		{"stale entry", 0, 1, 6552, "", 1},
		{"shrunk entry", 1, 2, 6552, "", 1},
		{"missing reason", 1, 1, 0, "", 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var violations []blockNameViolation
			for range tt.sites {
				violations = append(violations, blockNameViolation{file: "probe.go", line: 1, kind: "lookup"})
			}
			allowed := map[string]nameAllowance{}
			if tt.count > 0 {
				allowed["probe.go: lookup"] = nameAllowance{count: tt.count, issue: tt.issue, boundary: tt.boundary}
			}
			var r namePolicyErrors
			checkNameAllowances(&r, violations, allowed)
			if len(r.errors) != tt.want {
				t.Fatalf("got %v, want %d errors", r.errors, tt.want)
			}
		})
	}
}

func TestNamePoliciesScanRegistry(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	files := map[string]string{
		"internal/server/probe.go":            `package server; var x = registry.EntityTypeID("minecraft:blaze")`,
		"registry/fuel.go":                    `package registry; func fuel() { _, _ = ItemID("minecraft:coal") }`,
		"registry/poi.go":                     `package registry; var poi = map[string]bool{"minecraft:stone": true}; func check() { strings.Contains(block.Name(), "glass_pane") }`,
		"registry/stackfireworks.go":          `package registry; func rocket() { _, _ = ItemID("minecraft:firework_rocket") }`,
		"registry/sound.go":                   `package registry; var sound = "minecraft:block.turtle_egg.break"`,
		"registry/probe_test.go":              `not parsed`,
		"registry/probe_gen.go":               `not parsed`,
		"internal/server/command/probe.go":    `not parsed`,
		"internal/server/datapack/probe.go":   `not parsed`,
		"internal/server/tests/probe.go":      `not parsed`,
		"internal/server/servertest/probe.go": `not parsed`,
	}
	for path, src := range files {
		path = filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	var seen []string
	var kinds []blockNameViolation
	walkNamePolicy(t, root, func(f *ast.File, fset *token.FileSet, rel string) {
		seen = append(seen, rel)
		kinds = append(kinds, scanRegistryNames(f, fset, rel)...)
	})
	if len(seen) != 5 || len(kinds) != 2 {
		t.Fatalf("scanned %v, found %v", seen, kinds)
	}
	if got := blockNameViolations(t, root); len(got) != 1 {
		t.Fatalf("registry block lookups: %v", got)
	}
	if got := soundNameViolations(t, filepath.Join(root, "registry"), root); len(got) != 1 {
		t.Fatalf("registry sound names: %v", got)
	}
	if got := itemNameViolations(t, root); len(got) != 2 {
		t.Fatalf("registry item lookups: %v", got)
	}
}
