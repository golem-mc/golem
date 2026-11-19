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
		name: "blockstates",
		doc:  "registry/blockstates_gen.go: every block's state IDs with their properties",
		run:  runBlockStates,
	})
}

// The global block state palette gives each state the numeric ID that Chunk Data
// and Block Update carry. This table resolves a block name and property values to
// that ID (a log on its side, a furnace facing east) instead of always sending the
// default state. The item registry tells which blocks have an item form: those
// without (soul fire, end portal, potted plants) are not in BlockStates, which maps
// item names, so a block with no properties and no item gets its single state in
// AllStates.

// encodeProps writes a state's properties as a sorted "k=v,k=v" string.
func encodeProps(props map[string]string) string {
	pairs := make([]string, 0, len(props))
	for k, v := range props {
		pairs = append(pairs, k+"="+v)
	}
	slices.Sort(pairs)
	return strings.Join(pairs, ",")
}

func runBlockStates(e *env) error {
	blocks, err := e.ref.Blocks()
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

	type state struct {
		id    int
		props string
	}
	// Blocks with properties are here with every state they have; blocks
	// without properties only when they have no item form.
	table := map[string][]state{}
	for name, block := range blocks.All() {
		if len(block.Properties) == 0 {
			if _, hasItem := items.Entries.Get(name); hasItem {
				continue
			}
		}
		// Default state first, the rest in report order.
		sorted := slices.Clone(block.States)
		slices.SortStableFunc(sorted, func(a, b vanilla.BlockState) int { return boolRank(a.Default) - boolRank(b.Default) })
		entries := make([]state, len(sorted))
		for i, s := range sorted {
			entries[i] = state{s.ID, encodeProps(s.Properties)}
		}
		table[name] = entries
	}

	f := e.file("registry", "from the block report of a vanilla {version} server")
	f.WriteString(`import "strings"

// StateEntry is one block state: its global block state ID and its properties
// as a sorted "key=value,key=value" string, or "" for a block with no
// properties.
type StateEntry struct {
	ID    uint16
	Props string
}

// AllStates maps a block name to every state it has, the default first.
// Blocks with properties appear here with every state they have; blocks
// without properties only when they have no item form, since those are the
// ones BlockStates (which maps item names) cannot carry - the soul fire, the
// end portal, the potted plants.
//
// FindState uses this to resolve a block with non-default properties - a
// log placed on its side, a furnace facing east - to the state ID the
// client expects.
var AllStates = map[string][]StateEntry{
`)
	stateCount := 0
	for _, name := range sortedKeys(table) {
		parts := make([]string, len(table[name]))
		for i, s := range table[name] {
			parts[i] = fmt.Sprintf("{%d, %s}", s.id, gogen.Quote(s.props))
		}
		stateCount += len(parts)
		f.Printf("\t%s: {%s},\n", gogen.Quote(name), strings.Join(parts, ", "))
	}
	f.WriteString("}\n\n")
	f.WriteString(`// HasProperty reports whether the block has the named property.
func HasProperty(name, prop string) bool {
	states, ok := AllStates[name]
	if !ok {
		return false
	}
	return strings.Contains(states[0].Props, prop+"=")
}

// ParseStateProps decodes a "key=value,key=value" string into a map.
func ParseStateProps(s string) map[string]string {
	m := map[string]string{}
	if s == "" {
		return m
	}
	for _, pair := range strings.Split(s, ",") {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			m[kv[0]] = kv[1]
		}
	}
	return m
}
`)
	e.logf("block states: %d blocks, %d states", len(table), stateCount)
	return e.emit("registry/blockstates_gen.go", f)
}

func boolRank(defaultState bool) int {
	if defaultState {
		return 0
	}
	return 1
}
