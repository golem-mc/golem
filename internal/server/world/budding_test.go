package world

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func budState(t *testing.T, name, facing string, waterlogged bool) uint16 {
	t.Helper()
	wl := "false"
	if waterlogged {
		wl = "true"
	}
	s, ok := registry.FindState(name, map[string]string{"facing": facing, "waterlogged": wl})
	if !ok {
		t.Fatalf("no %s facing %s", name, facing)
	}
	return s
}

// randomTickUntil runs random-tick passes until done reports true.
func randomTickUntil(p *Physics, limit int, done func() bool) bool {
	var clock int64
	for range limit {
		clock++
		p.RandomTick(clock, []ChunkPos{{0, 0}}, 200, 1)
		if done() {
			return true
		}
	}
	return false
}

// vanilla: BuddingAmethystBlock.randomTick
func TestBuddingAmethystGrowsBudsFacingOutward(t *testing.T) {
	w, p := newPhysicsWorld(t)
	p.rng = rand.New(rand.NewSource(5))
	y := int32(GroundLevel + 5)
	setBlock(t, w, p, 0, y, 0, registry.MustState("minecraft:budding_amethyst", nil))
	faces := map[string][3]int32{"up": {0, 1, 0}, "down": {0, -1, 0}, "north": {0, 0, -1}, "south": {0, 0, 1}, "west": {-1, 0, 0}, "east": {1, 0, 0}}
	cluster := func(f string) bool {
		o := faces[f]
		return w.BlockAt(o[0], y+o[1], o[2]) == budState(t, "minecraft:amethyst_cluster", f, false)
	}
	small := func(f string) bool {
		o := faces[f]
		return w.BlockAt(o[0], y+o[1], o[2]) == budState(t, "minecraft:small_amethyst_bud", f, false)
	}
	sawSmall := map[string]bool{}
	ok := randomTickUntil(p, 20000, func() bool {
		all := true
		for f := range faces {
			sawSmall[f] = sawSmall[f] || small(f)
			if !cluster(f) {
				all = false
			}
		}
		return all
	})
	if !ok {
		t.Fatalf("not every face reached a cluster; small seen: %v", sawSmall)
	}
	for f := range faces {
		if !sawSmall[f] {
			t.Errorf("face %s never showed a small bud", f)
		}
	}
}

// vanilla: BuddingAmethystBlock.randomTick
func TestBuddingAmethystWaterloggesBudInWater(t *testing.T) {
	w, p := newPhysicsWorld(t)
	p.rng = rand.New(rand.NewSource(9))
	y := int32(GroundLevel + 5)
	setBlock(t, w, p, 0, y, 0, registry.MustState("minecraft:budding_amethyst", nil))
	setBlock(t, w, p, 1, y, 0, waterByLevel[0])
	want := budState(t, "minecraft:small_amethyst_bud", "east", true)
	if !randomTickUntil(p, 20000, func() bool { return w.BlockAt(1, y, 0) != waterByLevel[0] && w.BlockAt(1, y, 0) != airState }) {
		t.Fatal("no bud grew in the water")
	}
	if got := w.BlockAt(1, y, 0); got != want && got != budState(t, "minecraft:medium_amethyst_bud", "east", true) &&
		got != budState(t, "minecraft:large_amethyst_bud", "east", true) && got != budState(t, "minecraft:amethyst_cluster", "east", true) {
		t.Fatalf("bud in water = %d, want a waterlogged east bud", got)
	}
}

// A flowing water cell is not full, and a wrong-facing bud does not advance.
func TestBuddingAmethystSkipsFlowingWaterAndWrongFacing(t *testing.T) {
	w, p := newPhysicsWorld(t)
	p.rng = rand.New(rand.NewSource(2))
	y := int32(GroundLevel + 5)
	setBlock(t, w, p, 0, y, 0, registry.MustState("minecraft:budding_amethyst", nil))
	flow := waterByLevel[3]
	wrong := budState(t, "minecraft:small_amethyst_bud", "south", false)
	var changes []BlockChange
	for _, c := range [][3]int32{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 1, 0}, {0, -1, 0}} {
		w.SetBlock(c[0], y+c[1], c[2], flow)
	}
	w.SetBlock(0, y, -1, wrong)
	for range 5000 {
		p.buddingAmethystTick(0, y, 0, &changes)
	}
	if len(changes) != 0 {
		t.Fatalf("%d changes, want none: %v", len(changes), changes)
	}
}

// A bud is never grown into a cell outside the build height.
// vanilla: BuddingAmethystBlock.randomTick
func TestBuddingAmethystSkipsOutsideWorldHeight(t *testing.T) {
	for _, y := range []int32{worldMinY, worldMinY + worldHeight - 1} {
		t.Run(fmt.Sprint(y), func(t *testing.T) {
			w, p := newPhysicsWorld(t)
			p.rng = rand.New(rand.NewSource(4))
			setBlock(t, w, p, 0, y, 0, registry.MustState("minecraft:budding_amethyst", nil))
			if y == worldMinY {
				setBlock(t, w, p, 1, y, 0, airState)
			}
			var changes []BlockChange
			for range 3000 {
				p.buddingAmethystTick(0, y, 0, &changes)
			}
			for _, c := range changes {
				if c.Y < worldMinY || c.Y >= worldMinY+worldHeight {
					t.Fatalf("budding block at y=%d changed %d", y, c.Y)
				}
			}
			if len(changes) == 0 {
				t.Fatalf("budding block at y=%d never grew inward", y)
			}
		})
	}
}
