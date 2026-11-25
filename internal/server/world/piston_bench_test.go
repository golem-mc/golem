package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkPistonClockGrid ticks 512 observer-pair clocks driving 1024 pistons.
func BenchmarkPistonClockGrid(b *testing.B) {
	w := newTestWorld(b, NewFlatGenerator())
	b.Cleanup(func() { w.Close() })
	for cx := int32(-2); cx <= 1; cx++ {
		for cz := int32(-2); cz <= 1; cz++ {
			w.Acquire(ChunkPos{X: cx, Z: cz})
		}
	}
	p := NewPhysics(w)
	piston := pistonStateOf(false, dirUp, false)
	east := registry.MustState("minecraft:observer", map[string]string{"facing": "east"})
	west := registry.MustState("minecraft:observer", map[string]string{"facing": "west"})
	place := func(x, z int32, state uint16) {
		old := w.BlockAt(x, GroundLevel, z)
		w.SetBlock(x, GroundLevel, z, state)
		p.NotifyChanged(x, GroundLevel, z, old)
	}
	for g := range int32(8) {
		x := -20 + 5*g
		for z := int32(-32); z < 32; z++ {
			place(x, z, piston)
			place(x+3, z, piston)
			place(x+1, z, east)
		}
	}
	for g := range int32(8) {
		for z := int32(-32); z < 32; z++ {
			place(-18+5*g, z, west)
		}
	}
	for now := int64(1); now <= 100; now++ {
		p.Tick(now)
	}
	w.FlushLighting()
	if len(p.rides) == 0 || len(p.queue) < 1024 {
		b.Fatalf("inactive clock grid: %d rides, %d ticks", len(p.rides), len(p.queue))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		changes, _, _, effects := p.Tick(101 + int64(i))
		p.RecycleTickResults(changes, effects)
	}
}

// BenchmarkPistonPushable classifies the states a piston line meets: air,
// stone, the obsidian family, a slime block, a torch, a chest, an extended
// and a retracted piston and a door.
func BenchmarkPistonPushable(b *testing.B) {
	states := []uint16{
		airState,
		registry.MustState("minecraft:stone", nil),
		registry.MustState("minecraft:obsidian", nil),
		registry.MustState("minecraft:slime_block", nil),
		registry.MustState("minecraft:redstone_torch", nil),
		registry.MustState("minecraft:chest", nil),
		registry.MustState("minecraft:piston", map[string]string{"extended": "true"}),
		registry.MustState("minecraft:piston", map[string]string{"extended": "false"}),
		registry.MustState("minecraft:oak_door", nil),
	}
	p := &Physics{}
	b.ReportAllocs()
	n := 0
	for range b.N {
		for _, s := range states {
			if p.pistonPushable(s, 0, 64, 0, dirEast, dirEast, true) {
				n++
			}
		}
	}
	if n == 0 {
		b.Fatal("nothing pushable")
	}
}
