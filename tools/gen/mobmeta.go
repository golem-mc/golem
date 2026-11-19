package main

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

func init() {
	register(generator{name: "mobmeta", doc: "internal/server/{entity/,}mobmeta_gen.go: capture groups, ordered encoders and source setters", run: runMobMeta})
}

type metaGroup struct {
	fields  []string
	capture string
	entries []metaEntry
	sources []metaSource
}

type metaSource struct{ path, typ, key, condition string }

type metaEntry struct {
	spawn, delta int
	encode       string
}

func runMobMeta(e *env) error {
	src, err := os.ReadFile(filepath.Join(e.root, "internal/server/mobmeta.go"))
	if err != nil {
		return err
	}
	groups, err := parseMobMeta(src)
	if err != nil {
		return err
	}
	if err := validateMetaSourceTypes(e.root, groups); err != nil {
		return err
	}
	out, err := generateMobMeta(groups)
	if err != nil {
		return err
	}
	entity, err := generateMobMetaSources(groups)
	if err != nil {
		return err
	}
	if err := e.emitBytes("internal/server/entity/mobmeta_gen.go", entity); err != nil {
		return err
	}
	return e.emitBytes("internal/server/mobmeta_gen.go", out)
}

// parseMobMeta validates the complete schema before the generator writes anything.
func parseMobMeta(src []byte) ([]metaGroup, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "mobmeta.go", src, 0)
	if err != nil {
		return nil, err
	}
	fields := map[string]bool{}
	var schema *ast.CompositeLit
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				if s.Name.Name != "mobMetadataFields" && s.Name.Name != "mobMetadataState" {
					continue
				}
				st, ok := s.Type.(*ast.StructType)
				if !ok {
					return nil, fmt.Errorf("%s must be a struct", s.Name)
				}
				for _, field := range st.Fields.List {
					for _, name := range field.Names {
						fields[name.Name] = true
					}
				}
			case *ast.ValueSpec:
				if len(s.Names) == 1 && s.Names[0].Name == "mobMetadataSchema" && len(s.Values) == 1 {
					schema, _ = s.Values[0].(*ast.CompositeLit)
				}
			}
		}
	}
	if schema == nil || len(fields) == 0 {
		return nil, fmt.Errorf("missing metadata schema or state")
	}
	var groups []metaGroup
	covered := map[string]bool{}
	positions := [2]map[int]bool{{}, {}}
	sourceTypes := map[string]string{}
	for i, elt := range schema.Elts {
		call, ok := elt.(*ast.CallExpr)
		if !ok {
			return nil, fmt.Errorf("group %d: expected mobMeta call", i)
		}
		name, ok := call.Fun.(*ast.Ident)
		if !ok || name.Name != "mobMeta" || len(call.Args) < 3 {
			return nil, fmt.Errorf("group %d: unsupported descriptor", i)
		}
		fieldNames, err := metaString(call.Args[0])
		if err != nil {
			return nil, err
		}
		capture, err := metaString(call.Args[2])
		if err != nil {
			return nil, err
		}
		g := metaGroup{fields: strings.Fields(fieldNames), capture: capture}
		g.sources, err = parseMetaSources(call.Args[1], sourceTypes)
		if err != nil {
			return nil, fmt.Errorf("group %d sources: %w", i, err)
		}
		if (len(g.fields) == 0) != (len(g.sources) == 0) {
			return nil, fmt.Errorf("group %d: missing source coverage", i)
		}
		owned := map[string]bool{}
		for _, field := range g.fields {
			if !fields[field] || covered[field] {
				return nil, fmt.Errorf("group %d: unknown or duplicate field %s", i, field)
			}
			covered[field], owned[field] = true, true
		}
		if (len(g.fields) == 0) != (strings.TrimSpace(capture) == "") {
			return nil, fmt.Errorf("group %d: missing capture coverage", i)
		}
		writes, err := metaBody(capture, "s", owned)
		if err != nil {
			return nil, fmt.Errorf("group %d capture: %w", i, err)
		}
		for _, field := range g.fields {
			if !writes[field] {
				return nil, fmt.Errorf("group %d: field %s is never captured", i, field)
			}
		}
		for _, arg := range call.Args[3:] {
			lit, ok := arg.(*ast.CompositeLit)
			if !ok || len(lit.Elts) != 3 {
				return nil, fmt.Errorf("group %d: unsupported encoder entry", i)
			}
			typ, ok := lit.Type.(*ast.Ident)
			if !ok || typ.Name != "mobMetaEntry" {
				return nil, fmt.Errorf("group %d: unresolved encoder entry", i)
			}
			var entry metaEntry
			var err error
			entry.spawn, err = metaPosition(lit.Elts[0])
			if err != nil {
				return nil, err
			}
			entry.delta, err = metaPosition(lit.Elts[1])
			if err != nil {
				return nil, err
			}
			entry.encode, err = metaString(lit.Elts[2])
			if err != nil {
				return nil, err
			}
			if _, err = metaBody(entry.encode, "now", owned); err != nil {
				return nil, fmt.Errorf("group %d encoder: %w", i, err)
			}
			if _, err = metaBody(entry.encode, "was", owned); err != nil {
				return nil, fmt.Errorf("group %d encoder: %w", i, err)
			}
			if len(g.fields) == 0 && entry.delta != -1 {
				return nil, fmt.Errorf("group %d: spawn helper has a delta", i)
			}
			if entry.spawn == -1 && entry.delta == -1 {
				return nil, fmt.Errorf("group %d: unreachable encoder", i)
			}
			for walk, pos := range []int{entry.spawn, entry.delta} {
				if pos < 0 {
					continue
				}
				if positions[walk][pos] {
					return nil, fmt.Errorf("duplicate metadata ordering position %d", pos)
				}
				positions[walk][pos] = true
			}
			g.entries = append(g.entries, entry)
		}
		if len(g.entries) == 0 && fieldNames != "saddled" {
			return nil, fmt.Errorf("group %d: missing encoder", i)
		}
		groups = append(groups, g)
	}
	for field := range fields {
		if !covered[field] {
			return nil, fmt.Errorf("missing capture coverage for %s", field)
		}
	}
	return groups, nil
}

func metaString(expr ast.Expr) (string, error) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", fmt.Errorf("metadata schema requires literal strings")
	}
	return strconv.Unquote(lit.Value)
}

func metaPosition(expr ast.Expr) (int, error) {
	negative := false
	if unary, ok := expr.(*ast.UnaryExpr); ok && unary.Op == token.SUB {
		expr, negative = unary.X, true
	}
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return 0, fmt.Errorf("metadata ordering requires an integer")
	}
	value, err := strconv.Atoi(lit.Value)
	if negative {
		value = -value
	}
	if err != nil || value < -1 {
		return 0, fmt.Errorf("invalid metadata ordering position")
	}
	return value, nil
}

// metaBody checks state selectors and records writes so omissions cannot silently zero a field.
func metaBody(body, receiver string, owned map[string]bool) (map[string]bool, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "body.go", "package server; func _(){\n"+body+"\n}", 0)
	if err != nil {
		return nil, err
	}
	writes := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if err != nil {
			return false
		}
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if root, ok := sel.X.(*ast.Ident); ok && root.Name == receiver && !owned[sel.Sel.Name] {
				err = fmt.Errorf("unresolved %s.%s capture", receiver, sel.Sel.Name)
			}
		}
		if assign, ok := n.(*ast.AssignStmt); ok {
			for _, lhs := range assign.Lhs {
				if sel, ok := lhs.(*ast.SelectorExpr); ok {
					if root, ok := sel.X.(*ast.Ident); ok && root.Name == receiver {
						writes[sel.Sel.Name] = true
					}
				}
			}
		}
		return true
	})
	return writes, err
}

func generateMobMeta(groups []metaGroup) ([]byte, error) {
	var out strings.Builder
	out.WriteString("// Code generated by tools/gen mobmeta from internal/server/mobmeta.go; DO NOT EDIT.\n\npackage server\n\nimport (\n\"slices\"\n\n\"github.com/golem-mc/golem/internal/protocol\"\n\"github.com/golem-mc/golem/internal/server/entity\"\n)\n")
	out.WriteString("type mobMetaEncoder func(*mobMetaWriter, *mobEntity, *mobMetadataState, *mobMetadataState)\n")
	type ordered struct {
		pos  int
		name string
	}
	var orders [2][]ordered
	count := 0
	for i, group := range groups {
		if len(group.fields) > 0 {
			count++
		}
		for j, entry := range group.entries {
			name := fmt.Sprintf("encodeMobMetadata%d_%d", i, j)
			for walk, pos := range []int{entry.spawn, entry.delta} {
				if pos >= 0 {
					orders[walk] = append(orders[walk], ordered{pos, name})
				}
			}
			fmt.Fprintf(&out, "func %s(w *mobMetaWriter, ent *mobEntity, now, was *mobMetadataState) { m, t := ent.mob, ent.mob.Type; _, _ = m, t\n%s\n}\n", name, entry.encode)
		}
	}
	for i, walk := range []string{"Spawn", "Delta"} {
		slices.SortFunc(orders[i], func(a, b ordered) int { return a.pos - b.pos })
		fmt.Fprintf(&out, "var mobMetadata%s = [...]mobMetaEncoder{", walk)
		for _, entry := range orders[i] {
			fmt.Fprintf(&out, "%s,", entry.name)
		}
		out.WriteString("}\n")
	}
	out.WriteString("func mobMetadataTable(w *mobMetaWriter, ent *mobEntity, now, was *mobMetadataState) {\norder := mobMetadataSpawn[:]; if w.delta { order = mobMetadataDelta[:] }; for _, encode := range order { encode(w, ent, now, was) }\n}\n")
	// Hoist the shared kind lookup without making unrelated partial captures read it.
	const familyLookup = "entity.Family(m.Type)"
	captures := make([]string, len(groups))
	familyMask := make([]uint64, (count+63)/64)
	bit := 0
	for i, group := range groups {
		captures[i] = strings.ReplaceAll(group.capture, familyLookup, "family")
		if captures[i] != group.capture {
			familyMask[bit/64] |= 1 << (bit % 64)
		}
		if len(group.fields) > 0 {
			bit++
		}
	}
	var familyGuards []string
	for word, mask := range familyMask {
		if mask != 0 {
			familyGuards = append(familyGuards, fmt.Sprintf("mask[%d] & %#x != 0", word, mask))
		}
	}
	out.WriteString("// mobMetadataStateOf reads every capture group.\nfunc mobMetadataStateOf(m *entity.Mob) mobMetadataState { var s mobMetadataState\n")
	if len(familyGuards) > 0 {
		fmt.Fprintf(&out, "family := %s\n", familyLookup)
	}
	for i, group := range groups {
		if len(group.fields) > 0 {
			fmt.Fprintf(&out, "{\n%s\n}\n", captures[i])
		}
	}
	out.WriteString("return s\n}\n")
	out.WriteString("type mobMetadataMask = entity.MobMetadataMask\n")
	out.WriteString("// captureMobMetadata replaces only selected groups, including their zero/default values.\nfunc captureMobMetadata(m *entity.Mob, s *mobMetadataState, mask mobMetadataMask) { var zero mobMetadataState\n")
	if len(familyGuards) > 0 {
		fmt.Fprintf(&out, "var family entity.MobType\nif %s { family = %s }\n", strings.Join(familyGuards, " || "), familyLookup)
	}
	bit = 0
	for i, group := range groups {
		if len(group.fields) == 0 {
			continue
		}
		fmt.Fprintf(&out, "if mask[%d] & (1 << %d) != 0 {\n", bit/64, bit%64)
		for _, field := range group.fields {
			fmt.Fprintf(&out, "s.%s = zero.%s\n", field, field)
		}
		fmt.Fprintf(&out, "%s\n}\n", captures[i])
		bit++
	}
	out.WriteString("}\n")
	return format.Source([]byte(out.String()))
}
