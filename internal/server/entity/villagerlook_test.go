package entity

import (
	"testing"
)

func TestVillagerLookChoosesVisibleNonPassengerTargetsByKind(t *testing.T) {
	w := flatWorld(t)
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	g := &villagerLookGoal{m: m}
	for _, tc := range []struct {
		k   int
		typ MobType
	}{
		{0, TypeCat}, {1, TypeVillager}, {3, TypeCow}, {4, TypeSquid},
		{6, TypeGlowSquid}, {7, TypeCod}, {8, TypeZombie},
	} {
		far := NewMob(tc.typ, 2, 7.5, 64, 0.5, 2)
		near := NewMob(tc.typ, 3, 3.5, 64, 0.5, 3)
		passenger := NewMob(tc.typ, 4, 0.5, 64, 0.5, 4)
		m.Passengers = []*Mob{passenger}
		tick := &tickView{w: w, others: []*Mob{m, far, near, passenger}}
		if got := g.choose(tick, tc.k).mob; got != near {
			t.Fatalf("choice %d: got %v, want nearest", tc.k, got)
		}
		near.Health = 0
		if got := g.choose(tick, tc.k).mob; got != far {
			t.Fatalf("choice %d picked a dead target", tc.k)
		}
		far.X = 8.51
		if got := g.choose(tick, tc.k).mob; got != nil {
			t.Fatalf("choice %d picked target outside eight blocks", tc.k)
		}
		far.X = 8.5
		if got := g.choose(tick, tc.k).mob; got != far {
			t.Fatalf("choice %d excluded eight-block boundary", tc.k)
		}
		far.X = 3.5
		for y := int32(64); y < 67; y++ {
			w.set(1, y, 0, defaultState(t, "minecraft:stone"))
		}
		if got := g.choose(tick, tc.k).mob; got != nil {
			t.Fatalf("choice %d saw through wall", tc.k)
		}
		for y := int32(64); y < 67; y++ {
			w.set(1, y, 0, 0)
		}
		far.gone = true
		if got := g.choose(tick, tc.k).mob; got != nil {
			t.Fatalf("choice %d picked removed target", tc.k)
		}
	}
	tick := &tickView{w: w, players: []Player{{EntityID: 10, X: 3.5, Y: 64, Z: 0.5}, {EntityID: 11, X: 2.5, Y: 64, Z: 0.5, Spectator: true}}}
	if got := g.choose(tick, 2).player; got != 10 {
		t.Fatalf("player %d", got)
	}
	tick.players[0].Dead = true
	if got := g.choose(tick, 2).player; got != 0 {
		t.Fatal("looked at dead player")
	}
}

func TestVillagerLookTracksPlayersAndStopsWhenTheyDisappear(t *testing.T) {
	w := flatWorld(t)
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	tick := &tickView{w: w, players: []Player{{EntityID: 10, X: 5.5, Y: 64, Z: 0.5}}}
	m.vb.look = villagerLookTarget{player: 10}
	g := &villagerLookGoal{m: m}
	g.Start(tick)
	if !g.ShouldContinue(tick) {
		t.Fatal("player not visible")
	}
	for range 3 {
		g.Tick(tick)
		(&villagerLookSink{m: m}).Tick(tick)
	}
	if m.HeadYaw != -90 {
		t.Fatalf("yaw %v", m.HeadYaw)
	}
	tick.players[0].X, tick.players[0].Z = 0.5, 5.5
	for range 3 {
		g.Tick(tick)
		(&villagerLookSink{m: m}).Tick(tick)
	}
	if m.HeadYaw != 0 {
		t.Fatalf("did not follow moving player: %v", m.HeadYaw)
	}
	tick.players = nil
	if g.ShouldContinue(tick) {
		t.Fatal("kept unloaded player")
	}
}

func TestIdleVillagerWalksToItsLookTargetAndReplans(t *testing.T) {
	w := flatWorld(t)
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	tick := &tickView{w: w, players: []Player{{EntityID: 10, X: 5.5, Y: 64, Z: 0.5}}}
	m.vb.look = villagerLookTarget{player: 10}
	g := &lookWalkGoal{m: m}
	started := false
	for range 100 {
		if g.CanStart(tick) {
			started = true
			break
		}
	}
	if !started {
		t.Fatal("never walked to look target")
	}
	g.Start(tick)
	if !g.ShouldContinue(tick) || g.destination.X != 5 {
		t.Fatal("no walk toward player")
	}
	// A copied tracker survives expiration of LOOK_TARGET.
	m.vb.look = villagerLookTarget{}
	tick.players[0].X = 8.5
	g.Tick(tick)
	if g.destination.X != 8 {
		t.Fatal("did not replan moving player")
	}
	m.vb.activity = actWork
	if g.ShouldContinue(tick) {
		t.Fatal("idle walk continued at work")
	}
	m.vb.activity = actIdle
	tick.players = nil
	g.Tick(tick)
	if g.left != 0 || g.ShouldContinue(tick) {
		t.Fatal("walk retained missing target")
	}
	g.Stop(tick)
	if len(m.navOf().nav.path) != 0 {
		t.Fatal("stop retained navigation")
	}
}
