package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestVillageWallStatesVanilla pins taiga village walls of frozen vanilla 26.3
// seed 0: a wall beside a composter connects low, and arms under a log are
// tall. A disk's mark updates the walls above it after the village is placed.
func TestVillageWallStatesVanilla(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(0)
	cases := []struct {
		cx, cz  int32
		x, y, z int32
		east    string
	}{
		{2, 186, 45, 63, 2982, "low"},
		{1, 188, 28, 63, 3009, "tall"},
		{1, 188, 28, 63, 3011, "tall"},
		{1, 188, 28, 63, 3013, "tall"},
	}
	chunks := map[ChunkPos]*Chunk{}
	for _, c := range cases {
		pos := ChunkPos{X: c.cx, Z: c.cz}
		ch := chunks[pos]
		if ch == nil {
			ch = newChunk(c.cx, c.cz)
			g.Generate(ch)
			chunks[pos] = ch
		}
		s := ch.BlockAt(c.x&15, c.y, c.z&15)
		if registry.BlockOf(s) != registry.BlockCobblestoneWall {
			t.Fatalf("(%d,%d,%d): %s, want cobblestone_wall", c.x, c.y, c.z, registry.BlockOf(s).Name())
		}
		if got := registry.StateValue(s, registry.PropEast); got != c.east {
			t.Errorf("(%d,%d,%d) east=%s, want %s", c.x, c.y, c.z, got, c.east)
		}
	}
}
