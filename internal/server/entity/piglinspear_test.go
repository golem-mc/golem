package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func spearWorld(t *testing.T) testWorld {
	t.Helper()
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	for x := int32(-14); x <= 14; x++ {
		for z := int32(-6); z <= 6; z++ {
			w.set(x, 63, z, stone)
		}
	}
	return w
}

func TestASpearPiglinChargesAPlayerWithTheSpearInUseAndStabsThem(t *testing.T) {
	w := spearWorld(t)
	m := NewMob(TypePiglin, 1, -9.5, 64, 0.5, 1)
	m.MainHand = protocol.ItemStack{ItemID: int32(registry.ItemGoldenSpear), Count: 1}
	player := Player{X: 9.5, Y: 64, Z: 0.5, EntityID: 77, Huntable: true}
	m.TargetID = player.EntityID
	var used, stabs int
	var stab *SpearStab
	for range 400 {
		for _, ev := range m.tick(w, []Player{player}, nil) {
			if ev.Kind == MobAttackedPlayer && ev.Stab != nil {
				stabs++
				stab = ev.Stab
			}
		}
		if m.UsingItem {
			used++
		}
	}
	if used == 0 {
		t.Error("the spear was never put in use")
	}
	if stabs == 0 || !stab.DealsDamage {
		t.Errorf("stabs = %d (%+v), want a damaging contact", stabs, stab)
	}
}
