package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestStairsShape checks StairBlock.getStairsShape on a subject stair facing
// north at (0, 0, 0), whose ascension side is the cell north of it and whose
// other side the cell south of it: a perpendicular stair across the
// ascension side is an outer corner, one across the other side an inner
// corner — left when the partner faces the counter-clockwise way — each only
// of the same half, and each refused when the corner's own open side holds a
// stair facing the subject's way, which is a straight run instead.
func TestStairsShape(t *testing.T) {
	north, ok := registry.FindState("minecraft:oak_stairs",
		map[string]string{"facing": "north", "half": "bottom", "shape": "straight", "waterlogged": "false"})
	if !ok {
		t.Fatal("the block registry has no north bottom oak stair")
	}
	subject := north // the placed state the shape is computed for
	stone := defaultState(t, "minecraft:stone")

	for _, c := range []struct {
		name  string
		steps map[[3]int32][2]string // cell → facing, half of a stair there
		stone [][3]int32             // cells holding stone, which is no partner
		want  string
	}{
		{
			name: "no stair beside is straight",
			want: "straight",
		},
		{
			name:  "a stair across the ascension facing its counter-clockwise side is an outer left",
			steps: map[[3]int32][2]string{{0, 0, -1}: {"west", "bottom"}},
			want:  "outer_left",
		},
		{
			name:  "a stair across the ascension facing its clockwise side is an outer right",
			steps: map[[3]int32][2]string{{0, 0, -1}: {"east", "bottom"}},
			want:  "outer_right",
		},
		{
			name:  "a stair across the other side facing the counter-clockwise way is an inner left",
			steps: map[[3]int32][2]string{{0, 0, 1}: {"west", "bottom"}},
			want:  "inner_left",
		},
		{
			name:  "a stair across the other side facing the clockwise way is an inner right",
			steps: map[[3]int32][2]string{{0, 0, 1}: {"east", "bottom"}},
			want:  "inner_right",
		},
		{
			name:  "the ascension side's corner is decided before the other side's",
			steps: map[[3]int32][2]string{{0, 0, -1}: {"east", "bottom"}, {0, 0, 1}: {"west", "bottom"}},
			want:  "outer_right",
		},
		{
			name:  "a partner of the other half is no corner",
			steps: map[[3]int32][2]string{{0, 0, -1}: {"east", "top"}},
			want:  "straight",
		},
		{
			name:  "a partner along the same axis is no corner",
			steps: map[[3]int32][2]string{{0, 0, -1}: {"south", "bottom"}},
			want:  "straight",
		},
		{
			name:  "a block that is no stair is no partner",
			stone: [][3]int32{{0, 0, -1}},
			want:  "straight",
		},
		{
			name: "a stair facing the subject's way on the corner's open side keeps it straight",
			steps: map[[3]int32][2]string{
				{0, 0, -1}: {"east", "bottom"},
				{-1, 0, 0}: {"north", "bottom"},
			},
			want: "straight",
		},
		{
			name: "the inner corner is refused by the stair its own open side holds too",
			steps: map[[3]int32][2]string{
				{0, 0, 1}:  {"west", "bottom"},
				{-1, 0, 0}: {"north", "bottom"},
			},
			want: "straight",
		},
		{
			name: "the refusal reads the partner's half as well",
			steps: map[[3]int32][2]string{
				{0, 0, -1}: {"east", "bottom"},
				{-1, 0, 0}: {"north", "top"},
			},
			want: "outer_right",
		},
		{
			// Only a stair refuses the corner: any other block on the
			// corner's open side leaves it standing.
			name: "a block that is no stair does not refuse the corner",
			steps: map[[3]int32][2]string{
				{0, 0, -1}: {"east", "bottom"},
			},
			stone: [][3]int32{{-1, 0, 0}},
			want:  "outer_right",
		},
	} {
		w := testWorld{}
		for cell, spec := range c.steps {
			state, ok := registry.FindState("minecraft:oak_stairs", map[string]string{
				"facing": spec[0], "half": spec[1], "shape": "straight", "waterlogged": "false",
			})
			if !ok {
				t.Fatalf("%s: no state for facing=%s half=%s", c.name, spec[0], spec[1])
			}
			w.set(cell[0], cell[1], cell[2], state)
		}
		for _, cell := range c.stone {
			w.set(cell[0], cell[1], cell[2], stone)
		}
		state := StairsStateWithShape(w, registry.BlockOakStairs, subject, 0, 0, 0)
		_, props, _ := registry.BlockStateName(state)
		if got := registry.ParseStateProps(props)["shape"]; got != c.want {
			t.Errorf("%s: shape = %q, want %q", c.name, got, c.want)
		}
		if got := registry.ParseStateProps(props)["waterlogged"]; got != "false" {
			t.Errorf("%s: the rest of the placed state did not survive: %s", c.name, props)
		}
	}
}
