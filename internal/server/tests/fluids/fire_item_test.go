package fluids

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestAnItemDroppedIntoFireBurnsUp drops a stone into a fire standing on the
// flat world's ground and watches it burn: the fire takes an item's five
// points of hurt a point at a bite, and the body goes with no sound — the
// removal the client is told of, the fire's bite silent, as vanilla's is
// (only the lava hurt plays the fizz).
func TestAnItemDroppedIntoFireBurnsUp(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Dropper")

	fire, ok := registry.FindState("minecraft:fire", nil)
	if !ok {
		t.Fatal("the block registry has no fire")
	}
	if !srv.SetBlock(0, 64, 0, fire) {
		t.Fatal("the fire was not placed")
	}

	w := newFluidWatcher(t, c, 0)
	dropIntoThePool(t, c, "minecraft:stone")
	w.watch(srv, 160)
	if w.item.entityID == 0 {
		t.Fatal("the drop never arrived")
	}
	if !w.item.removed {
		t.Error("the item in fire was never taken away, want it burned up")
	}
	if w.item.burned {
		t.Error("the item burning in fire played a burn, want the fire's bite silent")
	}
}
