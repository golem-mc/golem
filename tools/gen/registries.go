package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
)

func init() {
	register(generator{
		name: "registries",
		doc:  "registry/registries_gen.go: the registries and tags a vanilla server sends, from a probe dump ($DUMP)",
		run:  runRegistries,
	})
}

// A dump is what tools/probe saves when DUMP is set:
//
//	DUMP=registries.json go run ./tools/probe 127.0.0.1:25599
//	DUMP=registries.json go run ./tools/gen registries
//
// Take it from a running vanilla server, not the data pack in the game's jar: they
// disagree (the data pack ships 44 enchantment files, the server registers 43),
// and an entry the client cannot resolve drops the connection.
type registryDump struct {
	Version    string `json:"version"`
	Registries []struct {
		Name    string   `json:"name"`
		Entries []string `json:"entries"`
	} `json:"registries"`
	Tags []struct {
		Registry string `json:"registry"`
		Tags     []struct {
			Name string `json:"name"`
			IDs  []int  `json:"ids"`
		} `json:"tags"`
	} `json:"tags"`
}

type namedList struct {
	name    string
	entries []string
}

type namedIDs struct {
	name string
	ids  []int
}

func runRegistries(e *env) error {
	dumpPath := e.dump
	raw, err := os.ReadFile(dumpPath)
	if err != nil {
		return fmt.Errorf("%w; capture one with DUMP=%s go run ./tools/probe <address of a vanilla server>", err, dumpPath)
	}
	var dump registryDump
	if err := json.Unmarshal(raw, &dump); err != nil {
		return fmt.Errorf("%s: %w", dumpPath, err)
	}
	if dump.Version != "" && dump.Version != e.ref.Version {
		return fmt.Errorf("%s was captured from %s, expected %s; run `mise run reference:dump` for this version", dumpPath, dump.Version, e.ref.Version)
	}
	byName := map[string][]string{}
	for _, r := range dump.Registries {
		byName[r.Name] = r.Entries
	}

	// Tags, and the registries that hold them, are sorted by name: a vanilla server
	// sends both in hash order, which differs between runs, and nothing reads the
	// order. Registry entries keep their arrival order, which is the numeric ID.
	type taggedRegistry struct {
		registry string
		tags     []namedList
	}
	type builtinRegistry struct {
		registry string
		tags     []namedIDs
	}
	var tags []taggedRegistry
	var builtin []builtinRegistry
	for _, t := range dump.Tags {
		if entries, ok := byName[t.Registry]; ok {
			reg := taggedRegistry{registry: t.Registry}
			for _, tag := range t.Tags {
				members := make([]string, len(tag.IDs))
				for i, id := range tag.IDs {
					if id < 0 || id >= len(entries) {
						return fmt.Errorf("tag %s of %s names entry %d of %d", tag.Name, t.Registry, id, len(entries))
					}
					members[i] = entries[id]
				}
				reg.tags = append(reg.tags, namedList{tag.Name, members})
			}
			slices.SortFunc(reg.tags, func(a, b namedList) int { return strings.Compare(a.name, b.name) })
			tags = append(tags, reg)
			continue
		}
		reg := builtinRegistry{registry: t.Registry}
		for _, tag := range t.Tags {
			reg.tags = append(reg.tags, namedIDs{tag.Name, tag.IDs})
		}
		slices.SortFunc(reg.tags, func(a, b namedIDs) int { return strings.Compare(a.name, b.name) })
		builtin = append(builtin, reg)
	}

	slices.SortFunc(tags, func(a, b taggedRegistry) int { return strings.Compare(a.registry, b.registry) })
	slices.SortFunc(builtin, func(a, b builtinRegistry) int { return strings.Compare(a.registry, b.registry) })

	// The block report names every block by the numeric ID the dump uses for tag
	// members, which gives the block tags by name. A tag with an unknown ID
	// stops the run instead of being emitted short: that means the dump and the
	// report are from different versions, which a wrong tag would hide.
	registries, err := e.ref.Registries()
	if err != nil {
		return err
	}
	blockNames, err := registries.NamesByID("minecraft:block")
	if err != nil {
		return err
	}
	var blockTags []namedList
	var skipped []string
	for _, reg := range builtin {
		if reg.registry != "minecraft:block" {
			continue
		}
		for _, tag := range reg.tags {
			members := make([]string, 0, len(tag.ids))
			unknown := false
			for _, id := range tag.ids {
				if id < 0 || id >= len(blockNames) {
					unknown = true
					break
				}
				members = append(members, blockNames[id])
			}
			if unknown {
				skipped = append(skipped, tag.name)
				continue
			}
			slices.Sort(members)
			blockTags = append(blockTags, namedList{tag.name, members})
		}
	}
	slices.SortFunc(blockTags, func(a, b namedList) int { return strings.Compare(a.name, b.name) })
	if len(skipped) > 0 {
		slices.Sort(skipped)
		return fmt.Errorf("block IDs in %s are not in the block registry report; is the report the same version as the dump?\n  affected tags: %s",
			dumpPath, strings.Join(skipped, ", "))
	}

	f := e.file("registry", "from the registries a vanilla {version} server sends")
	f.Printf(`// Version is the Minecraft version the contents below were taken from. It is
// sent to the client as the version of the minecraft:core known pack; the
// client only fills in registry entry data from its own copy of that pack if
// the version matches exactly.
const Version = %s

`, gogen.Quote(e.ref.Version))
	f.WriteString(`// Synchronized lists the registries sent to the client during configuration,
// each with all of its entries, in the order a vanilla server sends them. Entry
// data itself is not sent: the client sources it from the known packs
// negotiated beforehand.
//
// The position of an entry within a registry is the numeric ID the rest of the
// protocol refers to it by, so the order must not be changed.
var Synchronized = []Registry{
`)
	for _, r := range dump.Registries {
		f.Printf("\t{Name: %s, Entries: []string{\n", gogen.Quote(r.Name))
		writeList(f, r.Entries, 2)
		f.WriteString("\t}},\n")
	}
	f.WriteString("}\n\n")
	f.WriteString(`// Tags lists the tags of synchronized registries, by entry name. Tags are never
// sourced from known packs, so every tag the client needs has to be sent in
// full - including these, which the entries of other registries refer to.
var Tags = []TaggedRegistry{
`)
	for _, reg := range tags {
		f.Printf("\t{Registry: %s, Tags: []Tag{\n", gogen.Quote(reg.registry))
		for _, tag := range reg.tags {
			f.Printf("\t\t{Name: %s, Entries: []string{\n", gogen.Quote(tag.name))
			writeList(f, tag.entries, 3)
			f.WriteString("\t\t}},\n")
		}
		f.WriteString("\t}},\n")
	}
	f.WriteString("}\n\n")
	f.WriteString(`// BuiltinTags lists the tags of registries that are compiled into the client,
// such as blocks and items. Their entries have no names on the wire, only the
// numeric IDs below, which were captured from a server of this exact version
// and are valid only for it.
//
// These are what let the client load the entries of synchronized registries
// from its own data pack: a dimension type refers to the block tag
// minecraft:infiniburn_overworld, an enchantment to the item tag
// minecraft:enchantable/head_armor, and loading fails outright if they are
// missing.
var BuiltinTags = []BuiltinTaggedRegistry{
`)
	for _, reg := range builtin {
		f.Printf("\t{Registry: %s, Tags: []BuiltinTag{\n", gogen.Quote(reg.registry))
		for _, tag := range reg.tags {
			ids := make([]string, len(tag.ids))
			for i, id := range tag.ids {
				ids[i] = strconv.Itoa(id)
			}
			f.Printf("\t\t{Name: %s, IDs: []int32{%s}},\n", gogen.Quote(tag.name), strings.Join(ids, ", "))
		}
		f.WriteString("\t}},\n")
	}
	f.WriteString("}\n\n")
	f.WriteString(`// BlockTags lists the tags of the block registry by block name, resolved from
// the numeric IDs in BuiltinTags through the block report. BuiltinTags is what
// gets sent to the client; this is the same data in the form the server's own
// block logic can use.
//
// A vanilla block check is either a tag test or a Java instanceof, and the two
// are not interchangeable even when they look alike: fences and walls connect
// by BlockTags.FENCES and BlockTags.WALLS, but bars connect by "instanceof
// IronBarsBlock", which covers glass panes that the minecraft:bars tag leaves
// out. Use a tag here only where vanilla uses one; the instanceof cases are
// derived from the block report's definition types in blocks_gen.go instead.
var BlockTags = map[string][]string{
`)
	for _, tag := range blockTags {
		f.Printf("\t%s: {\n", gogen.Quote(tag.name))
		writeList(f, tag.entries, 2)
		f.WriteString("\t},\n")
	}
	f.WriteString("}\n")

	entries, memberships := 0, 0
	for _, r := range dump.Registries {
		entries += len(r.Entries)
	}
	for _, tag := range blockTags {
		memberships += len(tag.entries)
	}
	e.logf("registries: %d entries: %d", len(dump.Registries), entries)
	e.logf("block tags by name: %d, memberships: %d", len(blockTags), memberships)
	return e.emit("registry/registries_gen.go", f)
}

func writeList(f interface{ WriteString(string) }, items []string, indent int) {
	pad := strings.Repeat("\t", indent)
	for _, item := range items {
		f.WriteString(pad + gogen.Quote(item) + ",\n")
	}
}
