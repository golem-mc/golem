package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func lovedItemLook(m *Mob) *gatedGoal {
	for _, goal := range m.ai.goals {
		if g, ok := goal.(*gatedGoal); ok {
			if look, ok := g.Goal.(*lookAtPlayer); ok && look.brain {
				return g
			}
		}
	}
	return nil
}

func TestPiglinLovedItemLookSelectsTheNearestVisibleMatch(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	gold := protocol.ItemStack{ItemID: int32(registry.ItemGoldIngot), Count: 1}
	block := protocol.ItemStack{ItemID: int32(registry.ItemGoldBlock), Count: 1}
	m := NewMob(TypePiglin, 1, 0.5, 64, 0.5, 5891)
	g := lovedItemLook(m)
	if g == nil || lovedItemLook(NewMob(TypePiglinBrute, 2, 0.5, 64, 0.5, 5891)) != nil {
		t.Fatal("only regular piglins must have the loved-item look")
	}
	for _, tc := range []struct {
		name string
		p    Player
		want bool
	}{
		{"main hand at fourteen", Player{X: 14.5, HeldItem: gold}, true},
		{"off hand tag member", Player{X: 10.5, OffHandItem: block}, true},
		{"beyond fourteen", Player{X: 14.6, HeldItem: gold}, false},
		{"empty", Player{X: 10.5}, false},
		{"non loved item", Player{X: 10.5, HeldItem: protocol.ItemStack{ItemID: int32(registry.ItemIronIngot), Count: 1}}, false},
		{"spectator", Player{X: 10.5, HeldItem: gold, Spectator: true}, false},
		{"dead", Player{X: 10.5, HeldItem: gold, Dead: true}, false},
		{"invisible", Player{X: 10.5, HeldItem: gold, Invisible: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.p
			p.EntityID, p.Y, p.Z = 10, 64, 0.5
			tick := &tickView{w: w, players: []Player{
				{EntityID: 9, X: 2.5, Y: 64, Z: 0.5}, p,
			}}
			if got := g.CanStart(tick); got != tc.want {
				t.Fatalf("can look = %v, want %v", got, tc.want)
			}
			if tc.want && g.Goal.(*lookAtPlayer).target != p.EntityID {
				t.Fatal("the nearer empty-handed player hid the loved-item holder")
			}
		})
	}
	tick := &tickView{w: w, players: []Player{
		{EntityID: 10, X: 14.5, Y: 64, Z: 0.5, HeldItem: gold},
		{EntityID: 11, X: 10.5, Y: 64, Z: 0.5, OffHandItem: block},
	}}
	if !g.CanStart(tick) || g.Goal.(*lookAtPlayer).target != 11 {
		t.Fatal("did not select the nearest loved-item holder")
	}
	stone := defaultState(t, "minecraft:stone")
	for y := int32(64); y <= 67; y++ {
		w.set(5, y, 0, stone)
	}
	if g.CanStart(tick) {
		t.Fatal("gold behind a wall drew the piglin's gaze")
	}
}

func TestPiglinLovedItemLookActivitiesAndLifetime(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	m := NewMob(TypePiglin, 1, 0.5, 64, 0.5, 5891)
	g := lovedItemLook(m)
	tick := &tickView{w: w, players: []Player{{EntityID: 10, X: 10.5, Y: 64, Z: 0.5,
		HeldItem: protocol.ItemStack{ItemID: int32(registry.ItemGoldIngot), Count: 1}}}}
	if !g.CanStart(tick) {
		t.Fatal("idle piglin did not look")
	}
	m.barter.celebrate = 300
	m.barter.ride = 9
	if !g.CanStart(tick) {
		t.Fatal("celebration did not take priority over the ride's shorter gaze")
	}
	m.barter.celebrate, m.barter.ride = 0, 9
	if g.CanStart(tick) {
		t.Fatal("riding piglin looked beyond eight blocks")
	}
	tick.players[0].X = 8.5
	if !g.CanStart(tick) {
		t.Fatal("riding piglin did not look within eight blocks")
	}
	m.barter.ride = 0
	for _, memory := range []*int{&m.barter.admiring, &m.barter.retreat} {
		*memory = 100
		if g.CanStart(tick) {
			t.Error("admiring or retreating piglin opened a loved-item gaze")
		}
		*memory = 0
	}
	m.TargetID = 9
	if g.CanStart(tick) {
		t.Fatal("fighting piglin opened a loved-item gaze")
	}
	m.TargetID = 0
	g.Start(tick)
	look := g.Goal.(*lookAtPlayer)
	if look.remains < 45 || look.remains > 90 || !g.RequiresEveryTick() {
		t.Fatalf("look lasts %d ticks, every tick %v", look.remains, g.RequiresEveryTick())
	}
	// LOOK_TARGET survives putting the item away and leaving the selection
	// radius, until the sink times out or the target is no longer visible.
	tick.players[0].HeldItem = protocol.ItemStack{}
	tick.players[0].X = 15.5
	if !g.ShouldContinue(tick) {
		t.Fatal("visible look target was forgotten before the sink expired")
	}
	left := look.remains
	for range left {
		if !g.ShouldContinue(tick) {
			t.Fatal("gaze expired early")
		}
		g.Tick(tick)
	}
	if g.ShouldContinue(tick) {
		t.Fatal("gaze outlasted its sink")
	}
	look.remains = 10
	tick.players[0].Spectator = true
	if g.ShouldContinue(tick) {
		t.Fatal("gaze followed a spectator")
	}
	tick.players[0].Spectator = false
	stone := defaultState(t, "minecraft:stone")
	for y := int32(64); y <= 67; y++ {
		w.set(5, y, 0, stone)
	}
	if g.ShouldContinue(tick) {
		t.Fatal("gaze followed a target hidden by a wall")
	}
}
