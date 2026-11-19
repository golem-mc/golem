package entity

import (
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
)

var (
	gossipA = uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	gossipB = uuid.MustParse("00000000-0000-0000-0000-00000000000b")
)

func TestGossipDecaysByTheDay(t *testing.T) {
	g := Gossips{gossipA: {25, 0, 25, 20, 2}}
	g.decay()
	if got := g[gossipA]; got != [5]int32{15, 0, 24, 20, 0} {
		t.Errorf("after a day = %v", got)
	}
}

func TestVillagersGossipOncePerCooldownOnBothSides(t *testing.T) {
	a := NewMob(TypeVillager, 1, 0, 64, 0, 1)
	b := NewMob(TypeVillager, 2, 1, 64, 0, 2)
	a.GameTime, b.GameTime = 1200, 1200
	b.OnReputationEvent(ReputationVillagerKilled, gossipA)
	a.gossipWith(b)
	if a.PlayerReputation(gossipA) == 0 {
		t.Fatal("no gossip passed")
	}
	if a.vg.lastGossip != 1200 || b.vg.lastGossip != 1200 {
		t.Errorf("cooldowns %d, %d", a.vg.lastGossip, b.vg.lastGossip)
	}
	b.OnReputationEvent(ReputationVillagerHurt, gossipB)
	a.GameTime, b.GameTime = 2399, 2399
	b.gossipWith(a)
	if a.PlayerReputation(gossipB) != 0 || b.PlayerReputation(gossipB) == 0 {
		t.Error("gossip passed inside the cooldown")
	}
	a.GameTime, b.GameTime = 2400, 2400
	a.gossipWith(b)
	if a.PlayerReputation(gossipB) == 0 {
		t.Error("no gossip after the cooldown")
	}
}

func TestAVillagerReportsAnAttackerOnceAPlayerMatches(t *testing.T) {
	w := flatWorld(t)
	players := []Player{{EntityID: 7, UUID: gossipA, X: 1, Y: 64, Z: 0}}
	m := NewMob(TypeVillager, 1, 0, 64, 0, 1)
	m.NoAI = true
	m.vg.hurtBy = 7
	events := m.tick(w, players, nil)
	if m.PlayerReputation(gossipA) != -25 || m.vg.hurtBy != 0 {
		t.Errorf("reputation %d, hurtBy %d", m.PlayerReputation(gossipA), m.vg.hurtBy)
	}
	angry := false
	for _, e := range events {
		angry = angry || e.Kind == MobStatus && e.Status == protocol.EntityStatusVillagerAngry
	}
	if !angry {
		t.Error("no angry status")
	}
	n := NewMob(TypeVillager, 2, 0, 64, 0, 2)
	n.NoAI = true
	n.vg.hurtBy = 99
	n.tick(w, players, nil)
	if len(n.vg.gossips) != 0 || n.vg.hurtBy != 0 {
		t.Errorf("an unknown attacker gave %v, hurtBy %d", n.vg.gossips, n.vg.hurtBy)
	}
}
