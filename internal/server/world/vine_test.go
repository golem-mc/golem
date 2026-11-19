package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// vineWall puts a stone wall at x=5 and one vine hanging on its west face,
// returning the count of vines in the column below the first.
func vineWall(t *testing.T, w *World, p *Physics) {
	t.Helper()
	stone := rsState(t, "minecraft:stone", nil)
	for y := int32(GroundLevel); y < GroundLevel+12; y++ {
		setBlock(t, w, p, 5, y, 0, stone)
	}
	setBlock(t, w, p, 4, GroundLevel+8, 0, rsState(t, "minecraft:vine", map[string]string{"east": "true"}))
}

func countVines(w *World) int {
	n := 0
	for y := int32(GroundLevel); y < GroundLevel+12; y++ {
		if registryStateName(w.BlockAt(4, y, 0)) == "minecraft:vine" {
			n++
		}
	}
	return n
}

func TestVineSpreadsDownOnRandomTicks(t *testing.T) {
	w, p := newPhysicsWorld(t)
	vineWall(t, w, p)
	for pass := range 400 {
		p.RandomTick(int64(pass), []ChunkPos{{0, 0}}, 50, 1)
	}
	if got := countVines(w); got < 2 {
		t.Fatalf("vines in the column after the dice: %d, want the vine to have spread", got)
	}
	name, props, _ := registry.BlockStateName(w.BlockAt(4, GroundLevel+7, 0))
	if name == "minecraft:vine" && registry.ParseStateProps(props)["east"] != "true" {
		t.Errorf("the vine below hangs on %s, want its east face", props)
	}
}

func TestVineDoesNotSpreadWhenTheRuleIsOff(t *testing.T) {
	w, p := newPhysicsWorld(t)
	p.SetVineSpreadRule(func() bool { return false })
	vineWall(t, w, p)
	for pass := range 400 {
		p.RandomTick(int64(pass), []ChunkPos{{0, 0}}, 50, 1)
	}
	if got := countVines(w); got != 1 {
		t.Fatalf("vines in the column with spread_vines off: %d, want 1", got)
	}
	p.SetVineSpreadRule(func() bool { return true })
	for pass := range 400 {
		p.RandomTick(int64(pass), []ChunkPos{{0, 0}}, 50, 1)
	}
	if got := countVines(w); got < 2 {
		t.Fatalf("vines after turning spread_vines back on: %d, want the vine to have spread", got)
	}
}

func TestVineStopsSpreadingFromACrowd(t *testing.T) {
	w, p := newPhysicsWorld(t)
	vine := rsState(t, "minecraft:vine", map[string]string{"east": "true"})
	crowd := [][3]int32{{4, 0, 0}, {4, 1, 0}, {4, -1, 0}, {4, 0, 3}, {0, 0, -4}}
	for i, c := range crowd {
		w.SetBlock(c[0], GroundLevel+8+c[1], c[2], vine)
		got := p.vineCanSpread(4, GroundLevel+8, 0)
		if want := i+1 < 5; got != want {
			t.Fatalf("with %d vines in reach canSpread = %v, want %v", i+1, got, want)
		}
	}
}

// tickVine runs the vine at (4, y, 0) through n random ticks.
func tickVine(w *World, p *Physics, y int32, n int) {
	for range n {
		state := w.BlockAt(4, y, 0)
		if registry.BlockOf(state) != registry.BlockVine {
			return
		}
		var changes []BlockChange
		p.vineRandomTick(4, y, 0, state, &changes)
	}
}

func vineFaceAt(t *testing.T, w *World, x, y, z int32, face string) bool {
	t.Helper()
	name, props, _ := registry.BlockStateName(w.BlockAt(x, y, z))
	return name == "minecraft:vine" && registry.ParseStateProps(props)[face] == "true"
}
