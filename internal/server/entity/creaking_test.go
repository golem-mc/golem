package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func newCreaking() *Mob { return NewMob(TypeCreaking, 1, 0.5, 64, 0.5, 5) }

func creakingFloor(t *testing.T) testWorld {
	w := newTestWorld()
	wideFloor(w, t)
	return w
}

// frozenAfterTick ticks once and reports whether the creaking holds still.
func frozenAfterTick(m *Mob, w testWorld, players ...Player) bool {
	m.Tick(w, players)
	return m.CreakingView().Frozen
}

// vanilla: Creaking.checkCanMove, LivingEntity.isLookingAtMe
func TestACreakingFreezesUnderAClearGazeOnly(t *testing.T) {
	w := creakingFloor(t)
	m := newCreaking()
	if !frozenAfterTick(m, w, starer(m, 8)) {
		t.Fatal("a clear gaze did not freeze the creaking")
	}
	m = newCreaking()
	for y := int32(64); y <= 68; y++ {
		w.set(4, y, 0, defaultState(t, "minecraft:stone"))
	}
	if frozenAfterTick(m, w, starer(m, 8)) {
		t.Error("a gaze through a wall froze the creaking")
	}
	if m.CreakingView().Active {
		t.Error("a gaze through a wall woke the creaking")
	}
}

// vanilla: Creaking.checkCanMove, LivingEntity.PLAYER_NOT_WEARING_DISGUISE_ITEM
func TestAGazeDisguiseHidesOnlyFromAnAwakeCreaking(t *testing.T) {
	w := creakingFloor(t)
	pumpkin, ok := registry.ItemID("minecraft:carved_pumpkin")
	if !ok {
		t.Fatal("no carved pumpkin item")
	}
	m := newCreaking()
	p := starer(m, 8)
	p.Worn[wornHead] = protocol.ItemStack{ItemID: pumpkin, Count: 1}
	if !frozenAfterTick(m, w, p) {
		t.Fatal("a pumpkin spared a sleeping creaking from the gaze")
	}
	if !m.CreakingView().Active {
		t.Fatal("the gaze did not wake the creaking")
	}
	if frozenAfterTick(m, w, p) {
		t.Error("a pumpkin did not hide the player from an awake creaking")
	}
	p.Worn[wornHead] = protocol.ItemStack{}
	if !frozenAfterTick(m, w, p) {
		t.Error("the bare gaze did not hold the awake creaking")
	}
}
