package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// TestALightningBoltLightsAnIdleItem ticks a stone item resting on stone
// once, so its fire rests at -1, then strikes it: the bolt's +1 reaches 0
// and lights the eight-second burn.
// vanilla: Entity.applyEffectsFromBlocks, Entity.thunderHit
func TestALightningBoltLightsAnIdleItem(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	w.set(0, 63, 0, stone)
	it := &Item{ID: 7, X: 0.5, Y: 64 + halfSize, Z: 0.5, Health: 5, InvulnerableTime: 40,
		Stack: protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}}
	it.Step(w, nil)
	if it.fireTicks != -1 {
		t.Fatalf("an idle item's fire after one tick = %d, want -1", it.fireTicks)
	}
	if it.ThunderHit() {
		t.Fatal("the invulnerable item was destroyed by the bolt")
	}
	if it.fireTicks != rules.LightningIgniteTicks {
		t.Errorf("the struck item's fire = %d, want %d", it.fireTicks, rules.LightningIgniteTicks)
	}
}
