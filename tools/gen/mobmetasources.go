package main

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
)

func parseMetaSources(expr ast.Expr, types map[string]string) ([]metaSource, error) {
	if id, ok := expr.(*ast.Ident); ok && id.Name == "nil" {
		return nil, nil
	}
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil, fmt.Errorf("expected mobMetaSources")
	}
	name, ok := lit.Type.(*ast.Ident)
	if !ok || name.Name != "mobMetaSources" {
		return nil, fmt.Errorf("unresolved source descriptor")
	}
	var sources []metaSource
	seen := map[string]bool{}
	for _, elt := range lit.Elts {
		entry, ok := elt.(*ast.CompositeLit)
		if !ok || entry.Type != nil || len(entry.Elts) != 4 {
			return nil, fmt.Errorf("expected four source strings")
		}
		var s metaSource
		for i, dst := range []*string{&s.path, &s.typ, &s.key, &s.condition} {
			value, err := metaString(entry.Elts[i])
			if err != nil {
				return nil, err
			}
			*dst = value
		}
		path := strings.TrimSuffix(s.path, "()")
		if !metaSelector(path) {
			return nil, fmt.Errorf("unsupported source selector %q", s.path)
		}
		if seen[s.path] {
			return nil, fmt.Errorf("duplicate source %s", s.path)
		}
		seen[s.path] = true
		if previous, ok := types[s.path]; ok && previous != s.typ {
			return nil, fmt.Errorf("conflicting source type for %s", s.path)
		}
		types[s.path] = s.typ
		typ, err := parser.ParseExpr(s.typ)
		if err != nil || !metaSourceType(typ) {
			return nil, fmt.Errorf("unsupported source type %q", s.typ)
		}
		if strings.HasSuffix(s.path, "()") && (s.typ != "uint64" || s.key != "" || s.condition != "") {
			return nil, fmt.Errorf("external revision requires uint64 without a comparison override")
		}
		for _, expression := range []string{s.key, s.condition} {
			if expression != "" {
				if _, err := parser.ParseExpr(expression); err != nil {
					return nil, fmt.Errorf("invalid source expression: %w", err)
				}
			}
		}
		sources = append(sources, s)
	}
	return sources, nil
}

func metaSelector(path string) bool {
	parts := strings.Split(path, ".")
	if len(parts) < 2 || parts[0] != "m" {
		return false
	}
	for _, part := range parts[1:] {
		if !token.IsIdentifier(part) {
			return false
		}
	}
	return true
}

func metaSourceType(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name != "any"
	case *ast.SelectorExpr:
		_, ok := t.X.(*ast.Ident)
		return ok
	case *ast.ArrayType:
		n, ok := t.Len.(*ast.BasicLit)
		return ok && n.Kind == token.INT && metaSourceType(t.Elt)
	}
	return false
}

func metaPathName(path string) string {
	var out strings.Builder
	for part := range strings.SplitSeq(strings.TrimPrefix(path, "m."), ".") {
		out.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return out.String()
}

func generateMobMetaSources(groups []metaGroup) ([]byte, error) {
	type target struct {
		bit            int
		key, condition string
	}
	type source struct {
		metaSource
		targets []target
	}
	var sources []source
	indices := map[string]int{}
	var names []string
	for _, g := range groups {
		if len(g.fields) == 0 {
			continue
		}
		bit := len(names)
		names = append(names, metaPathName(g.fields[0]))
		for _, s := range g.sources {
			i, ok := indices[s.path]
			if !ok {
				i = len(sources)
				indices[s.path] = i
				sources = append(sources, source{metaSource: s})
			}
			key := s.key
			if key == "" {
				key = s.path
			}
			sources[i].targets = append(sources[i].targets, target{bit, key, s.condition})
		}
	}
	words := (len(names) + 63) / 64
	var out strings.Builder
	fmt.Fprintf(&out, "// MobMetadataMask names pending logical captures, independently of protocol indices.\ntype MobMetadataMask [%d]uint64\n", words)
	out.WriteString("// MobMetadataGroup identifies one schema capture group.\ntype MobMetadataGroup uint8\nconst (\n")
	for i, name := range names {
		fmt.Fprintf(&out, "MobMetadata%s MobMetadataGroup = %d\n", name, i)
	}
	out.WriteString(")\n// Has reports whether the capture group is pending.\nfunc (mask MobMetadataMask) Has(group MobMetadataGroup) bool { return mask[group/64] & (1 << (group%64)) != 0 }\n")
	// Revision sources reconcile here instead of overriding promoted effect operations.
	out.WriteString("type mobMetadataExternal struct {\n")
	for i, s := range sources {
		if strings.HasSuffix(s.path, "()") {
			fmt.Fprintf(&out, "source%d uint64\n", i)
		}
	}
	out.WriteString("}\n// MetadataRevision reconciles external revisions; the caller holds the mob lock.\nfunc (m *Mob) MetadataRevision() uint64 {\n")
	for i, s := range sources {
		if !strings.HasSuffix(s.path, "()") {
			continue
		}
		fmt.Fprintf(&out, "if now := %s; now != m.metadataExternal.source%d { m.metadataExternal.source%d = now; m.metadataRevision++\n", s.path, i, i)
		for _, t := range s.targets {
			fmt.Fprintf(&out, "m.metadataDirty[%d] |= 1 << %d\n", t.bit/64, t.bit%64)
		}
		out.WriteString("}\n")
	}
	out.WriteString("return m.metadataRevision\n}\n// MetadataDirty returns the pending capture groups under the mob lock.\nfunc (m *Mob) MetadataDirty() MobMetadataMask { return m.metadataDirty }\n// ClearMetadataDirty is called only by the server snapshot consumer, after capture.\nfunc (m *Mob) ClearMetadataDirty() { m.metadataDirty = MobMetadataMask{} }\n")
	emitInvalidate := func(name string, mask []uint64) {
		fmt.Fprintf(&out, "// %s marks captures after a bulk storage replacement.\nfunc (m *Mob) %s() { m.metadataRevision++\n", name, name)
		for word, bits := range mask {
			if bits != 0 {
				fmt.Fprintf(&out, "m.metadataDirty[%d] |= %#x\n", word, bits)
			}
		}
		out.WriteString("}\n")
	}
	all := make([]uint64, words)
	for bit := range names {
		all[bit/64] |= 1 << (bit % 64)
	}
	emitInvalidate("InvalidateMetadata", all)
	var ancestors []string
	masks := map[string][]uint64{}
	for _, s := range sources {
		if strings.HasSuffix(s.path, "()") {
			continue
		}
		parts := strings.Split(s.path, ".")
		for depth := 2; depth < len(parts); depth++ {
			path := strings.Join(parts[:depth], ".")
			if masks[path] == nil {
				ancestors = append(ancestors, path)
				masks[path] = make([]uint64, words)
			}
			for _, t := range s.targets {
				masks[path][t.bit/64] |= 1 << (t.bit % 64)
			}
		}
		name := "SetMetadata" + metaPathName(s.path)
		fmt.Fprintf(&out, "// %s stores and invalidates; simulation side effects belong to its caller.\nfunc (m *Mob) %s(value %s) {\n", name, name, s.typ)
		for i, t := range s.targets {
			if t.condition != "" {
				fmt.Fprintf(&out, "enabled%d := %s\n", i, t.condition)
			}
			fmt.Fprintf(&out, "before%d := %s\n", i, t.key)
		}
		fmt.Fprintf(&out, "%s = value\nchanged := false\n", s.path)
		for i, t := range s.targets {
			guard := ""
			if t.condition != "" {
				guard = fmt.Sprintf("enabled%d && ", i)
			}
			fmt.Fprintf(&out, "if %sbefore%d != (%s) { m.metadataDirty[%d] |= 1 << %d; changed = true }\n", guard, i, t.key, t.bit/64, t.bit%64)
		}
		out.WriteString("if changed { m.metadataRevision++ }\n}\n")
	}
	for _, path := range ancestors {
		emitInvalidate("InvalidateMetadata"+metaPathName(path), masks[path])
	}
	body := out.String()
	var src strings.Builder
	src.WriteString("// Code generated by tools/gen mobmeta from internal/server/mobmeta.go; DO NOT EDIT.\n\npackage entity\n\n")
	for _, path := range []string{"github.com/golem-mc/golem/internal/protocol", "github.com/google/uuid"} {
		name := path[strings.LastIndexByte(path, '/')+1:]
		if strings.Contains(body, name+".") {
			fmt.Fprintf(&src, "import %q\n", path)
		}
	}
	src.WriteString(body)
	return format.Source([]byte(src.String()))
}
