package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// mustState resolves a block with properties, failing the test on a miss.
func mustState(t *testing.T, name string, props map[string]string) uint16 {
	t.Helper()
	s, ok := registry.FindState(name, props)
	if !ok {
		t.Fatalf("no state for %s %v", name, props)
	}
	return s
}

func TestCanBeReplaced(t *testing.T) {
	clicked := func(face int32, cursorY float32) PlacementContext {
		return PlacementContext{Face: face, CursorY: cursorY, ReplacingClickedOn: true}
	}
	neighbour := PlacementContext{Face: protocol.FaceTop}
	bottom := mustState(t, "minecraft:oak_slab", map[string]string{"type": "bottom"})
	top := mustState(t, "minecraft:oak_slab", map[string]string{"type": "top"})
	double := mustState(t, "minecraft:oak_slab", map[string]string{"type": "double"})
	snow := func(layers string) uint16 { return mustState(t, "minecraft:snow", map[string]string{"layers": layers}) }
	candle := func(n string) uint16 { return mustState(t, "minecraft:white_candle", map[string]string{"candles": n}) }
	litter := func(n string) uint16 {
		return mustState(t, "minecraft:leaf_litter", map[string]string{"segment_amount": n})
	}
	petals4 := mustState(t, "minecraft:pink_petals", map[string]string{"flower_amount": "4"})
	grass := registry.BlockStates["minecraft:short_grass"]
	vine1 := mustState(t, "minecraft:vine", map[string]string{"north": "true"})
	vine5 := mustState(t, "minecraft:vine", map[string]string{
		"up": "true", "north": "true", "south": "true", "west": "true", "east": "true"})
	scaffold := mustState(t, "minecraft:scaffolding", nil)
	sneaking := clicked(protocol.FaceTop, 0.5)
	sneaking.Sneaking = true

	cases := []struct {
		what  string
		state uint16
		item  string
		ctx   PlacementContext
		want  bool
	}{
		{"air", 0, "minecraft:stone", clicked(protocol.FaceTop, 0.5), true},
		{"short grass by stone", grass, "minecraft:stone", clicked(protocol.FaceTop, 0.5), true},
		{"short grass by its own item", grass, "minecraft:short_grass", clicked(protocol.FaceTop, 0.5), false},

		{"bottom slab, top face", bottom, "minecraft:oak_slab", clicked(protocol.FaceTop, 0.5), true},
		{"bottom slab, side", bottom, "minecraft:oak_slab", clicked(protocol.FaceNorth, 0.25), false},
		{"bottom slab, another kind", bottom, "minecraft:stone_slab", clicked(protocol.FaceTop, 0.5), false},
		{"bottom slab as the neighbour", bottom, "minecraft:oak_slab", neighbour, true},
		{"top slab, bottom face", top, "minecraft:oak_slab", clicked(protocol.FaceBottom, 0.5), true},
		{"top slab, lower side", top, "minecraft:oak_slab", clicked(protocol.FaceEast, 0.25), true},
		{"top slab, upper side", top, "minecraft:oak_slab", clicked(protocol.FaceEast, 0.75), false},
		{"double slab", double, "minecraft:oak_slab", neighbour, false},

		{"one snow layer by stone", snow("1"), "minecraft:stone", clicked(protocol.FaceTop, 0.125), true},
		{"two snow layers by stone", snow("2"), "minecraft:stone", clicked(protocol.FaceTop, 0.25), false},
		{"snow on snow, top face", snow("3"), "minecraft:snow", clicked(protocol.FaceTop, 0.375), true},
		{"snow on snow, side", snow("3"), "minecraft:snow", clicked(protocol.FaceNorth, 0.25), false},
		{"snow as the neighbour", snow("3"), "minecraft:snow", neighbour, true},
		{"full snow", snow("8"), "minecraft:snow", neighbour, false},

		{"candle of its colour", candle("3"), "minecraft:white_candle", clicked(protocol.FaceTop, 0.5), true},
		{"candle of another colour", candle("1"), "minecraft:red_candle", clicked(protocol.FaceTop, 0.5), false},
		{"four candles", candle("4"), "minecraft:white_candle", clicked(protocol.FaceTop, 0.5), false},
		{"candle while sneaking", candle("1"), "minecraft:white_candle", sneaking, false},

		{"litter on litter", litter("3"), "minecraft:leaf_litter", clicked(protocol.FaceTop, 0.5), true},
		{"full litter by litter", litter("4"), "minecraft:leaf_litter", clicked(protocol.FaceTop, 0.5), false},
		{"full litter by stone", litter("4"), "minecraft:stone", clicked(protocol.FaceTop, 0.5), true},
		{"full petals by stone", petals4, "minecraft:stone", clicked(protocol.FaceTop, 0.5), false},

		{"vine with a free face by vine", vine1, "minecraft:vine", clicked(protocol.FaceNorth, 0.5), true},
		{"full vine by vine", vine5, "minecraft:vine", clicked(protocol.FaceNorth, 0.5), false},
		{"full vine by stone", vine5, "minecraft:stone", clicked(protocol.FaceNorth, 0.5), true},

		// ScaffoldingBlock.canBeReplaced answers the item in hand alone,
		// for no scaffolding carries the replaceable tag.
		{"scaffolding by its own item", scaffold, "minecraft:scaffolding", clicked(protocol.FaceTop, 0.5), true},
		{"scaffolding by stone", scaffold, "minecraft:stone", clicked(protocol.FaceTop, 0.5), false},
	}
	for _, c := range cases {
		if got := CanBeReplaced(c.state, blockNamed(c.item), c.ctx); got != c.want {
			t.Errorf("%s: CanBeReplaced = %v, want %v", c.what, got, c.want)
		}
	}
}

func TestStackedPlacementState(t *testing.T) {
	water := mustState(t, "minecraft:water", nil)
	cases := []struct {
		what    string
		name    string
		current uint16
		want    map[string]string
	}{
		{"slab on a waterlogged slab", "minecraft:oak_slab",
			mustState(t, "minecraft:oak_slab", map[string]string{"type": "top", "waterlogged": "true"}),
			map[string]string{"type": "double", "waterlogged": "false"}},
		{"snow on seven layers", "minecraft:snow",
			mustState(t, "minecraft:snow", map[string]string{"layers": "7"}),
			map[string]string{"layers": "8"}},
		{"candle on two lit", "minecraft:white_candle",
			mustState(t, "minecraft:white_candle", map[string]string{"candles": "2", "lit": "true"}),
			map[string]string{"candles": "3", "lit": "true", "waterlogged": "false"}},
		{"pickle on a waterlogged pickle", "minecraft:sea_pickle",
			mustState(t, "minecraft:sea_pickle", map[string]string{"pickles": "1", "waterlogged": "true"}),
			map[string]string{"pickles": "2", "waterlogged": "true"}},
		{"egg on a cracked egg", "minecraft:turtle_egg",
			mustState(t, "minecraft:turtle_egg", map[string]string{"eggs": "3", "hatch": "1"}),
			map[string]string{"eggs": "4", "hatch": "1"}},
		{"petals keep their facing", "minecraft:pink_petals",
			mustState(t, "minecraft:pink_petals", map[string]string{"flower_amount": "1", "facing": "east"}),
			map[string]string{"flower_amount": "2", "facing": "east"}},
		{"fresh slab in water", "minecraft:oak_slab", water,
			map[string]string{"type": "bottom", "waterlogged": "true"}},
		{"fresh pickle on land", "minecraft:sea_pickle", 0,
			map[string]string{"pickles": "1", "waterlogged": "false"}},
		{"fresh pickle into seagrass", "minecraft:sea_pickle", registry.BlockStates["minecraft:seagrass"],
			map[string]string{"pickles": "1", "waterlogged": "true"}},
		{"fresh slab into falling water", "minecraft:oak_slab",
			mustState(t, "minecraft:water", map[string]string{"level": "8"}),
			map[string]string{"type": "bottom", "waterlogged": "false"}},
	}
	for _, c := range cases {
		w := testWorld{}
		w.set(0, 0, 0, c.current)
		got, ok := StackedPlacementState(w, blockNamed(c.name), registry.BlockStates[c.name], 0, 0, 0)
		if !ok {
			t.Errorf("%s: not handled", c.what)
			continue
		}
		name, raw, _ := registry.BlockStateName(got)
		props := registry.ParseStateProps(raw)
		for k, v := range c.want {
			if name != c.name || props[k] != v {
				t.Errorf("%s: got %s %v, want %s with %v", c.what, name, props, c.name, c.want)
				break
			}
		}
	}

	if _, ok := StackedPlacementState(testWorld{}, registry.BlockStone, 1, 0, 0, 0); ok {
		t.Error("stone was handled as a stacking block")
	}
}
