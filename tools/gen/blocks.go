package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{
		name: "blocks",
		doc:  "registry/blocks_gen.go: placeable states, block IDs and class families",
		run:  runBlocks,
	})
}

// Block state IDs are compiled into the client, not sent, so they only mean
// something to a client of this exact version. BlockStates maps every item that
// has a block of the same name to that block's default state, which is what a
// placed block uses.
//
// BlockIDs is each block's ID in the minecraft:block registry (its position in the
// registries report). The wire names block IDs only inside a block predicate's set
// (can_place_on, can_break); this turns them back into names.

// Fence gates and leaves are instanceof checks in vanilla too, but their tags
// (minecraft:fence_gates, minecraft:leaves) list exactly the same blocks, so
// registry.IsFenceGate and IsLeaves read BlockTags; registry_test.go pins both. A
// type list is only worth keeping where no tag matches (bars), since it needs a
// manual revisit for every new type. The same goes for replaceable blocks: the
// minecraft:replaceable tag is what vanilla tests (BlockState.canBeReplaced).

func runBlocks(e *env) error {
	blocks, err := e.ref.Blocks()
	if err != nil {
		return err
	}
	definitions, err := blockDefinitions(e, blocks)
	if err != nil {
		return err
	}
	registries, err := e.ref.Registries()
	if err != nil {
		return err
	}
	items, ok := registries["minecraft:item"]
	if !ok {
		return fmt.Errorf("the registries report has no minecraft:item")
	}
	blockRegistry, ok := registries["minecraft:block"]
	if !ok {
		return fmt.Errorf("the registries report has no minecraft:block")
	}
	// The block report and the registry report have to come from the same run,
	// or the state IDs and the registry IDs answer to different versions. A
	// block the registry does not name stops the generation rather than
	// emitting a table with a hole in it.
	var unknown []string
	for name := range blocks.All() {
		if _, ok := blockRegistry.Entries.Get(name); !ok {
			unknown = append(unknown, name)
		}
		if definitions[name].Type == "" {
			return fmt.Errorf("%s has no definition type", name)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		return fmt.Errorf("blocks in the block report missing from the registries report; is it the same version?\n  %s",
			strings.Join(unknown[:min(5, len(unknown))], ", "))
	}

	// An item places a block when both share a name. Most blocks have a single
	// state, but a block with properties has several; the default state is the
	// one a placed block starts in, so that is the state ID we keep.
	placeable := map[string]int{}
	for name := range items.Entries.All() {
		block, ok := blocks.Get(name)
		if !ok {
			continue
		}
		defaultID, found := -1, false
		for _, s := range block.States {
			if s.Default {
				defaultID, found = s.ID, true
				break
			}
		}
		// A block with no default state would be one the game never places,
		// but every block has one in practice; the guard is for safety.
		if !found {
			continue
		}
		placeable[name] = defaultID
	}

	// BlockTypes is each block's definition type, which the per-family rules
	// key on.
	blockTypes := map[string]string{}
	for name, definition := range definitions {
		blockTypes[name] = definition.Type
	}

	// BarsBlocks stands in for a Java instanceof, which has nothing to do with
	// whether a block has an item form, so it is collected by walking the block
	// report rather than the item registry the loop above follows.
	bars := map[string]bool{}
	gravity := map[string]bool{}
	solidify := map[string]string{}
	landEvents := map[string]int{}
	brokenEvents := map[string]int{}
	for name, definition := range definitions {
		kind := definition.Type
		if barsTypes[kind] {
			bars[name] = true
		}
		if fallingTypes[kind] {
			gravity[name] = true
		}
		if solidifyTypes[kind] {
			if definition.Concrete == nil {
				return fmt.Errorf("concrete powder %s carries no concrete block in its definition", name)
			}
			solidify[name] = *definition.Concrete
		}
		if ev, ok := fallingLandEvents[kind]; ok {
			landEvents[name] = ev
		}
		if ev, ok := fallingBrokenEvents[kind]; ok {
			brokenEvents[name] = ev
		}
	}
	groups, err := blockGroupsFor(definitions)
	if err != nil {
		return err
	}

	f := e.file("registry", "from the block report of a vanilla {version} server")
	f.WriteString(`// BlockStates maps an item name to the default block state ID a placed block
// of that kind starts in. Only items that share a name with a block are here:
// a diamond does not place anything, but a stone places state 1.
//
// Block state IDs are compiled into the client rather than sent, so these
// numbers are only valid for the exact version above. Anything written to
// disk stores the name instead, which is what makes a world outlive them.
var BlockStates = map[string]uint16{
`)
	for _, name := range sortedKeys(placeable) {
		f.Printf("\t%s: %d,\n", gogen.Quote(name), placeable[name])
	}
	f.WriteString("}\n\n")

	f.WriteString(`// BlockIDs maps a block name to its ID in the minecraft:block registry. That
// registry is compiled into the client rather than sent, and the wire names
// its entries only where a block predicate's block set travels inline - the
// minecraft:can_place_on and minecraft:can_break components - so this table
// is what matches such a set's IDs against the named blocks the rest of the
// registry works in. Like the state IDs, the numbers only mean anything to a
// client of the exact version above.
var BlockIDs = map[string]int32{
`)
	for _, name := range sortedKeys(blockRegistryIDs(blockRegistry)) {
		id, _ := blockRegistry.Entries.Get(name)
		f.Printf("\t%s: %d,\n", gogen.Quote(name), id.ProtocolID)
	}
	f.WriteString("}\n\n")

	f.WriteString(`// BlockTypes names each block's definition type. The class groups share
// these report rows and compile to a block-indexed bitmap during initialization.
var BlockTypes, blockGroups = func() (map[string]string, [BlockCount]BlockGroup) {
	rows := [...]struct { name, kind string; groups BlockGroup } {
`)
	for _, name := range sortedKeys(blockTypes) {
		flags := "0"
		if g := groups[name]; len(g) > 0 {
			parts := make([]string, len(g))
			for i, group := range g {
				parts[i] = "Group" + group
			}
			flags = strings.Join(parts, " | ")
		}
		f.Printf("\t{%s, %s, %s},\n", gogen.Quote(name), gogen.Quote(blockTypes[name]), flags)
	}
	f.WriteString(`	}
	types := make(map[string]string, len(rows))
	var groups [BlockCount]BlockGroup
	for _, row := range rows {
		types[row.name] = row.kind
		b, _ := BlockByName(row.name)
		groups[b] = row.groups
	}
	return types, groups
}()

`)

	typeSet := map[string]bool{}
	for _, typ := range blockTypes {
		typeSet[typ] = true
	}
	typeNames := sortedKeys(typeSet)
	typeIdents, err := idents("BlockType", typeNames)
	if err != nil {
		return err
	}
	f.WriteString(`// BlockType is a block's definition type, numbered from 1 in name order;
// BlockTypeNone is a block with no type.
type BlockType uint16

const (
	BlockTypeNone BlockType = 0
`)
	for i, id := range typeIdents {
		f.Printf("\t%s BlockType = %d\n", id, i+1)
	}
	f.Printf("\tBlockTypeCount = %d\n)\n\n", len(typeNames)+1)
	f.WriteString("var blockTypeNames = [BlockTypeCount]string{\n")
	for i, name := range typeNames {
		f.Printf("\t%d: %s,\n", i+1, gogen.Quote(name))
	}
	f.WriteString("}\n\n")

	f.WriteString(`// BarsBlocks is the set of blocks whose Java class extends IronBarsBlock:
// iron bars, glass panes, stained glass panes, and copper bars (including
// waxed variants). They share the CrossCollisionBlock north/south/east/west
// connection properties and the IronBarsBlock.attachsTo connection rule, which
// connects to any other IronBarsBlock, walls, and sturdy-faced blocks — but
// not to fences or fence gates. The connection and obstruction logic uses
// this set the same way it uses IsFence for fences.
//
// This is the one block group that cannot come from the tag of the same name.
// minecraft:bars holds only iron bars and the copper bars family, because
// IronBarsBlock uses it in skipRendering for face culling; attachsTo tests
// instanceof IronBarsBlock, which every glass pane satisfies as well.
`)
	f.Set("BarsBlocks", sortedKeys(bars))
	f.WriteString("\n")

	f.WriteString(`// GravityBlocks is the set of blocks that leave their place as a falling
// block entity when nothing below holds them up: the sand family, gravel,
// the concrete powders, the anvils, the dragon egg, the suspicious sands and
// gravels, and scaffolding. The set is the vanilla data's own: each member's
// definition type names the Java class that falls — the FallingBlock
// subclasses, which schedule the fall check wherever the block lands or a
// neighbour changes, and the two Fallable strays (brushable and scaffolding)
// whose own ticks run the same check.
`)
	f.Set("GravityBlocks", sortedKeys(gravity))
	f.WriteString("\n")

	f.WriteString(`// ConcreteOf maps each concrete powder to the concrete of its colour, the
// pair the vanilla block definitions carry: a powder that lands in water
// sets the block its definition's concrete field names instead of itself.
var ConcreteOf = map[string]string{
`)
	for _, name := range sortedKeys(solidify) {
		f.Printf("\t%s: %s,\n", gogen.Quote(name), gogen.Quote(solidify[name]))
	}
	f.WriteString("}\n\n")

	f.WriteString(`// FallingLandEvent and FallingBrokenEvent name the level event a falling
// block's landing plays, by block: where one of the anvil family places
// itself the client hears the anvil land (1031), where one gives its place
// up as an item it hears the anvil destroyed (1029), where a suspicious
// sand or gravel gives its place up the block break plays (2001), and where
// a pointed dripstone or sulfur spike gives its place up the speleothem's
// landing plays (1045 and 1052). Both are empty of entries for the blocks
// whose landing plays nothing the server has to ask for.
var FallingLandEvent = map[string]int32{
`)
	for _, name := range sortedKeys(landEvents) {
		f.Printf("\t%s: %d,\n", gogen.Quote(name), landEvents[name])
	}
	f.WriteString("}\n\nvar FallingBrokenEvent = map[string]int32{\n")
	for _, name := range sortedKeys(brokenEvents) {
		f.Printf("\t%s: %d,\n", gogen.Quote(name), brokenEvents[name])
	}
	f.WriteString("}\n")

	last := ""
	for _, name := range sortedKeys(placeable) {
		if last == "" || placeable[name] >= placeable[last] {
			last = name
		}
	}
	e.logf("placeable items: %d (last: %s = %d), block types: %d, bars: %d, gravity: %d, concrete pairs: %d, fall events: %d, block registry: %d",
		len(placeable), last, placeable[last], len(blockTypes), len(bars), len(gravity),
		len(solidify), len(landEvents)+len(brokenEvents), blockRegistry.Entries.Len())
	return e.emit("registry/blocks_gen.go", f)
}

// blockRegistryIDs lists the registry's names as a set, for sorting.
func blockRegistryIDs(r vanilla.Registry) map[string]bool {
	m := make(map[string]bool, r.Entries.Len())
	for name := range r.Entries.All() {
		m[name] = true
	}
	return m
}

// blockGroupsFor lists, per block, the families it belongs to in the order the
// families are declared. A family naming a type or block the report lacks
// stops the generation: the report and the table are from different versions.
func blockGroupsFor(blocks map[string]vanilla.BlockDefinition) (map[string][]string, error) {
	known := map[string]bool{}
	for _, b := range blocks {
		known[b.Type] = true
	}
	for _, g := range blockGroupSpecs {
		for _, t := range g.types {
			if !known[t] {
				return nil, fmt.Errorf("missing block definition type: %s", t)
			}
		}
		for _, name := range append(slices.Clone(g.members), g.excludes...) {
			if _, ok := blocks[name]; !ok {
				return nil, fmt.Errorf("missing explicit block: %s", name)
			}
		}
	}
	result := map[string][]string{}
	for name, b := range blocks {
		var groups []string
		for _, g := range blockGroupSpecs {
			inGroup := slices.Contains(g.types, b.Type) || slices.Contains(g.members, name)
			if inGroup && !slices.Contains(g.excludes, name) {
				groups = append(groups, g.name)
			}
		}
		result[name] = groups
	}
	return result, nil
}
