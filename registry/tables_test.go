package registry

import (
	"slices"
	"strings"
	"testing"
)

// TestDenseLightFaces checks the dense face table against the generated map
// for every state ID, absent states included.
func TestDenseLightFaces(t *testing.T) {
	for id := range len(lightFacesCache) {
		if got, want := lightFacesCache[id], lightFacesGen[uint16(id)]; got != want {
			t.Errorf("state %d: faces %v, want %v", id, got, want)
		}
	}
}

// TestDenseStateNames checks BlockStateName against a map built the way the
// generated tables were read before stateByID became a slice.
func TestDenseStateNames(t *testing.T) {
	want := make(map[uint16]stateInfo)
	for name, states := range AllStates {
		for _, s := range states {
			if prev, ok := want[s.ID]; ok {
				t.Fatalf("state %d is both %s and %s", s.ID, prev.name, name)
			}
			want[s.ID] = stateInfo{name: name, props: s.Props}
		}
	}
	for name, id := range BlockStates {
		if _, ok := want[id]; !ok {
			want[id] = stateInfo{name: name}
		}
	}
	for id := range 65536 {
		name, props, ok := BlockStateName(uint16(id))
		w, wok := want[uint16(id)]
		if ok != wok || name != w.name || props != w.props {
			t.Errorf("state %d: (%q, %q, %v), want (%q, %q, %v)", id, name, props, ok, w.name, w.props, wok)
		}
	}
}

// TestCloneItems checks the pick block table against the registries: every
// block a state can name is tabled, every item it hands out exists, and the
// per-state keys name a state in the props form BlockStateName returns.
func TestCloneItems(t *testing.T) {
	blocks := map[string]bool{}
	for name := range AllStates {
		blocks[name] = true
	}
	for name := range BlockStates {
		blocks[name] = true
	}
	for name := range CloneItems {
		if !blocks[name] {
			t.Errorf("CloneItems has %q, which no block state names", name)
		}
	}
	for key := range CloneItemStates {
		block, props, _ := strings.Cut(key, "|")
		if !blocks[block] {
			t.Errorf("CloneItemStates has %q, which no block state names", key)
		}
		state, ok := FindState(block, ParseStateProps(props))
		if !ok {
			t.Errorf("CloneItemStates[%q]: no such block state", key)
			continue
		}
		if _, got, _ := BlockStateName(state); got != props {
			t.Errorf("CloneItemStates[%q]: state's props are %q", key, got)
		}
	}
	for block, item := range CloneItems {
		if item != "minecraft:air" {
			if _, ok := ItemID(item); !ok {
				t.Errorf("CloneItems[%q] = %q, an item the registry does not carry", block, item)
			}
		}
	}
	for key, item := range CloneItemStates {
		if _, ok := ItemID(item); !ok {
			t.Errorf("CloneItemStates[%q] = %q, an item the registry does not carry", key, item)
		}
	}
}

// TestBlockShapeEntries checks the generated shape table: every entry names
// a known state and a shape the box list carries, the accessor answers for
// each with that shape's boxes, and no box turns inside out or leaves the
// span two cells give (the pitcher crop reaches under its floor).
func TestBlockShapeEntries(t *testing.T) {
	seen := map[uint16]bool{}
	for _, e := range blockShapeEntries {
		if _, _, ok := BlockStateName(e.state); !ok {
			t.Errorf("entry %d names a state no block reports", e.state)
		}
		if int(e.shape) >= len(blockShapeBoxes) {
			t.Errorf("entry %d names shape %d, past the %d in the list",
				e.state, e.shape, len(blockShapeBoxes))
			continue
		}
		if seen[e.state] {
			t.Errorf("entry %d is named twice", e.state)
		}
		seen[e.state] = true
		boxes, ok := BlockShapeBoxes(e.state)
		if !ok {
			t.Errorf("BlockShapeBoxes(%d) misses an entry", e.state)
			continue
		}
		for i := range boxes {
			if boxes[i] != blockShapeBoxes[e.shape][i] {
				t.Errorf("BlockShapeBoxes(%d) box %d = %v, want %v",
					e.state, i, boxes[i], blockShapeBoxes[e.shape][i])
			}
		}
	}
	for _, boxes := range blockShapeBoxes {
		for _, b := range boxes {
			for i := range 3 {
				if b[i] > b[i+3] {
					t.Errorf("shape box %v turns inside out", b)
					break
				}
			}
			for _, v := range b {
				if v < -2 || v > 32 {
					t.Errorf("shape box %v leaves the span two cells give", b)
					break
				}
			}
		}
	}
}

// TestShapeFamiliesAreNotFullCubes pins the first state of each block the
// table reshapes away from the full cube its collision height reads as.
func TestShapeFamiliesAreNotFullCubes(t *testing.T) {
	want := map[string][][6]int8{
		"minecraft:cactus":              {{2, 0, 2, 30, 30, 30}},
		"minecraft:honey_block":         {{2, 0, 2, 30, 30, 30}},
		"minecraft:brewing_stand":       {{2, 0, 2, 30, 4, 30}, {14, 4, 14, 18, 28, 18}},
		"minecraft:conduit":             {{10, 10, 10, 22, 22, 22}},
		"minecraft:dried_ghast":         {{6, 0, 6, 26, 20, 26}},
		"minecraft:heavy_core":          {{8, 0, 8, 24, 16, 24}},
		"minecraft:lectern":             {{0, 0, 0, 32, 4, 32}, {8, 4, 8, 24, 28, 24}},
		"minecraft:sea_pickle":          {{12, 0, 12, 20, 12, 20}},
		"minecraft:turtle_egg":          {{6, 0, 6, 24, 14, 24}},
		"minecraft:copper_golem_statue": {{6, 0, 6, 26, 28, 26}},
	}
	seen := map[string]bool{}
	for id := range blockShapes {
		name, _, ok := BlockStateName(uint16(id))
		boxes, carried := BlockShapeBoxes(uint16(id))
		if !ok || !carried || seen[name] || want[name] == nil {
			continue
		}
		seen[name] = true
		if !slices.Equal(boxes, want[name]) {
			t.Errorf("%s boxes = %v, want %v", name, boxes, want[name])
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("%s has no carried shape", name)
		}
	}
}

// TestShapedBlocksCarryTheirPerStateBoxes pins states whose boxes depend on
// their properties, in 32nds of a block, against the reference server's
// collision shapes.
func TestShapedBlocksCarryTheirPerStateBoxes(t *testing.T) {
	chorus := func(open ...string) map[string]string {
		p := map[string]string{"north": "false", "east": "false", "south": "false",
			"west": "false", "up": "false", "down": "false"}
		for _, d := range open {
			p[d] = "true"
		}
		return p
	}
	for _, c := range []struct {
		name  string
		props map[string]string
		want  [][6]int8
	}{
		{"minecraft:sea_pickle", map[string]string{"pickles": "1"}, [][6]int8{{12, 0, 12, 20, 12, 20}}},
		{"minecraft:sea_pickle", map[string]string{"pickles": "2"}, [][6]int8{{6, 0, 6, 26, 12, 26}}},
		{"minecraft:sea_pickle", map[string]string{"pickles": "3"}, [][6]int8{{4, 0, 4, 28, 12, 28}}},
		{"minecraft:sea_pickle", map[string]string{"pickles": "4"}, [][6]int8{{4, 0, 4, 28, 14, 28}}},
		{"minecraft:chorus_plant", chorus(), [][6]int8{{6, 6, 6, 26, 26, 26}}},
		{"minecraft:chorus_plant", chorus("north", "east"),
			[][6]int8{{6, 6, 0, 26, 26, 26}, {26, 6, 6, 32, 26, 26}}},
		{"minecraft:lectern", nil, [][6]int8{{0, 0, 0, 32, 4, 32}, {8, 4, 8, 24, 28, 24}}},
		{"minecraft:composter", map[string]string{"level": "0"},
			[][6]int8{{0, 0, 0, 32, 4, 32}, {0, 4, 0, 4, 32, 32}, {4, 4, 0, 32, 32, 4},
				{4, 4, 28, 32, 32, 32}, {28, 4, 4, 32, 32, 28}}},
		{"minecraft:decorated_pot", nil, [][6]int8{{2, 0, 2, 30, 32, 30}}},
		{"minecraft:sniffer_egg", map[string]string{"hatch": "0"}, [][6]int8{{2, 0, 4, 30, 32, 28}}},
	} {
		id := MustState(c.name, c.props)
		got, ok := BlockShapeBoxes(id)
		if !ok || !slices.Equal(got, c.want) {
			t.Errorf("%s %v boxes = %v (carried %v), want %v", c.name, c.props, got, ok, c.want)
		}
	}
}
