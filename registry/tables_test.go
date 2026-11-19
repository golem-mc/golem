package registry

import (
	"slices"
	"testing"
)

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
