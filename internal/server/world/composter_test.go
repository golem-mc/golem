package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestAFullComposterTurnsReady: level 7 books the ready tick, 20 ticks on.
// vanilla: ComposterBlock.onPlace, tick
func TestAFullComposterTurnsReady(t *testing.T) {
	w, p := newPhysicsWorld(t)
	setBlock(t, w, p, 0, GroundLevel, 0, registry.MustState("minecraft:composter", map[string]string{"level": "7"}))
	for tick := int64(1); tick <= 20; tick++ {
		p.Tick(tick)
		ready := w.BlockAt(0, GroundLevel, 0) == registry.MustState("minecraft:composter", map[string]string{"level": "8"})
		if ready != (tick == 20) {
			t.Fatalf("ready at tick %d = %v, want only from tick 20", tick, ready)
		}
	}
}
