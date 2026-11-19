package entity

import (
	"testing"
)

// TestTraderLlamaDefendsItsTraderOncePerHurt: the llama leashed to a trader
// takes the trader's attacker as its target, and not again for the same hurt.
// vanilla: TraderLlama.TraderLlamaDefendWanderingTraderGoal
func TestTraderLlamaDefendsItsTraderOncePerHurt(t *testing.T) {
	w := newTestWorld()
	trader := NewMob(TypeWanderingTrader, 10, 0.5, 64, 0.5, 1)
	zombie := NewMob(TypeZombie, 11, 2.5, 64, 0.5, 2)
	llama := NewMob(TypeTraderLlama, 12, 0.5, 64, 3.5, 3)
	llama.LeashHolderID = trader.ID
	view := &tickView{w: w, others: []*Mob{trader, zombie, llama}}
	g := &traderDefendGoal{m: llama}
	if g.CanStart(view) {
		t.Fatal("started with an unhurt trader")
	}
	trader.retaliate, trader.lastDamageStamp, trader.Age = zombie.ID, 5, 6
	if !g.CanStart(view) {
		t.Fatal("did not start on the trader's attacker")
	}
	g.Start(view)
	if llama.TargetID != zombie.ID {
		t.Fatalf("target %d, want %d", llama.TargetID, zombie.ID)
	}
	g.Stop(view)
	if llama.TargetID != 0 || g.CanStart(view) {
		t.Error("the same hurt started the goal again")
	}
	trader.lastDamageStamp, trader.Age = 8, 9
	if !g.CanStart(view) {
		t.Error("a fresh hurt did not start the goal")
	}
	trader.Age = 8 + traderGrudgeTicks + 1
	if g.CanStart(view) {
		t.Error("a forgotten hurt started the goal")
	}
	llama.LeashHolderID = 0
	trader.Age = 9
	if g.CanStart(view) {
		t.Error("an unleashed llama defended a trader")
	}
}
