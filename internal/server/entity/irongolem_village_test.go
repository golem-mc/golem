package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"testing"
)

func TestGolemFlowerConditionsAndInterruption(t *testing.T) {
	w := newTestWorld()
	floor(w, -8, -8, 8, 8, 63, defaultState(t, "minecraft:stone"))
	m := NewMob(TypeIronGolem, 1, 0.5, 64, 0.5, 2162)
	v := NewMob(TypeVillager, 2, 2.5, 64, 0.5, 1)
	farther := NewMob(TypeVillager, 3, 5.5, 64, 0.5, 1)
	tick := &tickView{w: w, others: []*Mob{farther, v}}
	g := &offerFlowerGoal{m: m}
	m.ForceGolemFlowerOffer()
	if g.CanStart(tick) {
		t.Fatal("offering a flower at night")
	}
	w.bright = true
	tick.w = w
	if !g.CanStart(tick) || g.target != v {
		t.Fatal("did not choose the nearest visible villager")
	}
	g.Start(tick)
	if m.OfferFlowerTicks != 400 || tick.events[0].Status != protocol.EntityStatusIronGolemOfferFlower {
		t.Fatal("missing flower start")
	}
	g.Tick(tick)
	if m.HeadYaw != -30 {
		t.Fatalf("flower head turn = %g, want -30 degrees", m.HeadYaw)
	}
	g.Stop(tick)
	if m.OfferFlowerTicks != 0 || tick.events[1].Status != protocol.EntityStatusIronGolemStopFlower {
		t.Fatal("interruption did not clear flower")
	}
	v.Dead, farther.Dead = true, true
	m.ForceGolemFlowerOffer()
	if g.CanStart(tick) {
		t.Fatal("offering to a dead villager")
	}
	v.Dead = false
	v.Y = 69
	m.ForceGolemFlowerOffer()
	if g.CanStart(tick) {
		t.Fatal("offering outside the vertical box")
	}
}
