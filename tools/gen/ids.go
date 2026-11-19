package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
)

func init() {
	register(generator{
		name: "ids",
		doc:  "registry/blockids_gen.go, registry/itemids_gen.go, registry/mobeffectids_gen.go and registry/entitytypeids_gen.go: typed block, item and entity type IDs and mob effect constants",
		run:  runIDs,
	})
}

// A constant is the type's name followed by the registry path in PascalCase,
// split on underscores and periods (minecraft:tnt is BlockTnt): a reader turns any
// name into its constant without looking it up, and a version bump renames
// nothing it keeps.

var identPath = regexp.MustCompile(`^[a-z0-9_.]+$`)

func ident(prefix, name string) (string, error) {
	path, ok := strings.CutPrefix(name, "minecraft:")
	if !ok || !identPath.MatchString(path) {
		return "", fmt.Errorf("cannot name %s", name)
	}
	var b strings.Builder
	b.WriteString(prefix)
	for _, part := range strings.FieldsFunc(path, func(r rune) bool { return r == '_' || r == '.' }) {
		if part != "" {
			b.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}
	return b.String(), nil
}

// idents names every entry, refusing two that share a constant.
func idents(prefix string, names []string) ([]string, error) {
	out := make([]string, len(names))
	seen := map[string]string{}
	for i, name := range names {
		id, err := ident(prefix, name)
		if err != nil {
			return nil, err
		}
		if other, dup := seen[id]; dup {
			return nil, fmt.Errorf("%s and %s both name %s", other, name, id)
		}
		seen[id] = name
		out[i] = id
	}
	return out, nil
}

// runRegistryIDs writes file with one untyped constant per entry of a registry.
func runRegistryIDs(e *env, registry, prefix, file, doc string) error {
	registries, err := e.ref.Registries()
	if err != nil {
		return err
	}
	names, err := registries.NamesByID(registry)
	if err != nil {
		return err
	}
	idents, err := idents(prefix, names)
	if err != nil {
		return err
	}
	f := e.file("protocol", "from the registry report of a vanilla {version} server")
	f.Printf("// %s\nconst (\n", doc)
	for id, ident := range idents {
		f.Printf("\t%s = %d\n", ident, id)
	}
	f.WriteString(")\n")
	if err := e.emit(file, f); err != nil {
		return err
	}
	e.logf("%s: %d", registry, len(names))
	return nil
}

func runIDs(e *env) error {
	blocks, err := e.ref.Blocks()
	if err != nil {
		return err
	}
	registries, err := e.ref.Registries()
	if err != nil {
		return err
	}
	blockNames, err := registries.NamesByID("minecraft:block")
	if err != nil {
		return err
	}
	// The block report and the registry report have to come from the same run.
	var missing []string
	for _, name := range blockNames {
		if _, ok := blocks.Get(name); !ok {
			missing = append(missing, name)
		}
	}
	for _, name := range blocks.Keys() {
		if !slices.Contains(blockNames, name) {
			missing = append(missing, name)
		}
	}
	slices.Sort(missing)
	if len(missing) > 0 {
		return fmt.Errorf("block report and registry report disagree; same version?\n  %s", strings.Join(missing[:min(5, len(missing))], ", "))
	}

	// A block's states are a contiguous run of state IDs in block registry
	// order, which is what lets BlockOf be built from one range per block.
	type span struct{ first, deflt int }
	spans := make([]span, len(blockNames))
	next := 0
	for i, name := range blockNames {
		b, _ := blocks.Get(name)
		if len(b.States) == 0 {
			return fmt.Errorf("%s has no states", name)
		}
		lo, hi, deflts, deflt := b.States[0].ID, b.States[0].ID, 0, 0
		for _, s := range b.States {
			lo, hi = min(lo, s.ID), max(hi, s.ID)
			if s.Default {
				deflts++
				deflt = s.ID
			}
		}
		if lo != next || hi-lo+1 != len(b.States) {
			return fmt.Errorf("states of %s are not the contiguous run after %d", name, next-1)
		}
		if deflts != 1 {
			return fmt.Errorf("%s has %d default states", name, deflts)
		}
		spans[i] = span{lo, deflt}
		next = hi + 1
	}
	stateCount := next
	if stateCount > 1<<16 {
		return fmt.Errorf("%d states do not fit a uint16", stateCount)
	}
	blockIdents, err := idents("Block", blockNames)
	if err != nil {
		return err
	}

	f := e.file("registry", "from the block and registry reports of a vanilla {version} server")
	f.Printf(`// Block is a block's protocol ID in the minecraft:block registry.
type Block uint16

// BlockCount is the number of blocks; every Block is below it.
const BlockCount = %d

// StateCount is the number of block states; every state ID is below it.
const StateCount = %d

// The blocks, one constant per registry entry.
const (
`, len(blockNames), stateCount)
	for id, ident := range blockIdents {
		f.Printf("\t%s Block = %d\n", ident, id)
	}
	f.WriteString(")\n\n")
	f.WriteString("// blockInfo is each block's name, first state ID and default state ID, by\n")
	f.WriteString("// Block. A block's states run from its first to the next block's first.\n")
	f.WriteString("var blockInfo = [BlockCount]struct {\n\tname         string\n\tfirst, deflt uint16\n}{\n")
	for i, name := range blockNames {
		f.Printf("\t{%s, %d, %d},\n", gogen.Quote(name), spans[i].first, spans[i].deflt)
	}
	f.WriteString("}\n")
	if err := e.emit("registry/blockids_gen.go", f); err != nil {
		return err
	}

	itemNames, err := registries.NamesByID("minecraft:item")
	if err != nil {
		return err
	}
	itemIdents, err := idents("Item", itemNames)
	if err != nil {
		return err
	}
	f = e.file("registry", "from the registry report of a vanilla {version} server")
	f.Printf(`// Item is an item's protocol ID in the minecraft:item registry, the ID slots
// and packets carry.
type Item int32

// ItemCount is the number of items; every Item is below it.
const ItemCount = %d

// The items, one constant per registry entry.
const (
`, len(itemNames))
	for id, ident := range itemIdents {
		f.Printf("\t%s Item = %d\n", ident, id)
	}
	f.WriteString(")\n")
	if err := e.emit("registry/itemids_gen.go", f); err != nil {
		return err
	}

	effectNames, err := registries.NamesByID("minecraft:mob_effect")
	if err != nil {
		return err
	}
	effectIdents, err := idents("MobEffect", effectNames)
	if err != nil {
		return err
	}
	f = e.file("registry", "from the registry report of a vanilla {version} server")
	f.WriteString("// The mob effects by minecraft:mob_effect ID, one untyped constant per registry\n// entry so each stays assignable to the int32 effect IDs.\nconst (\n")
	for id, ident := range effectIdents {
		f.Printf("\t%s = %d\n", ident, id)
	}
	f.WriteString(")\n")
	if err := e.emit("registry/mobeffectids_gen.go", f); err != nil {
		return err
	}

	entityNames, err := registries.NamesByID("minecraft:entity_type")
	if err != nil {
		return err
	}
	entityIdents, err := idents("EntityType", entityNames)
	if err != nil {
		return err
	}
	f = e.file("registry", "from the registry report of a vanilla {version} server")
	f.Printf("// EntityTypeCount is the number of entity types; every EntityType is below it.\nconst EntityTypeCount = %d\n\n// The entity types, one constant per minecraft:entity_type registry entry.\nconst (\n", len(entityNames))
	for id, ident := range entityIdents {
		f.Printf("\t%s EntityType = %d\n", ident, id)
	}
	f.WriteString(")\n")
	if err := e.emit("registry/entitytypeids_gen.go", f); err != nil {
		return err
	}
	e.logf("blocks: %d, states: %d, items: %d, mob effects: %d, entity types: %d", len(blockNames), stateCount, len(itemNames), len(effectNames), len(entityNames))
	return nil
}
