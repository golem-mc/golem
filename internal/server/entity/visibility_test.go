package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The visibility the targeting conditions read, as the unit-side checks:
// the scan's range cut by the target's own state before a hunt may name
// it, and the hunt that has opened held to the whole range instead. The
// black-box tests in internal/server/server_hostile_visibility_test.go carry the
// same across the wire.

// headPiece is the stack the tests put on a target's head slot, the Worn
// index the match reads.
func headPiece(t *testing.T, item string) protocol.ItemStack {
	t.Helper()
	id, ok := registry.ItemID(item)
	if !ok {
		t.Fatalf("the item registry has no %s", item)
	}
	return protocol.ItemStack{ItemID: id, Count: 1}
}

// TestTheScanCutsItsReachByTheTargetsVisibility probes the scan at each
// shape's own boundary: the reach a standing, sneaking, invisible or
// head-hidden target leaves, and no farther than half a block past it.
func TestTheScanCutsItsReachByTheTargetsVisibility(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	zombieHead := headPiece(t, "minecraft:zombie_head")
	for _, tc := range []struct {
		name string
		// player is the target exactly as the shape dresses them, placed
		// by distance from the scan's own rows.
		player Player
		// distance is how far east of the zombie the target stands.
		distance float64
		// want names whether the scan finds the target there.
		want bool
	}{
		{"standing inside the follow range", Player{}, 34.9, true},
		{"standing outside the follow range", Player{}, 35.1, false},
		{"sneaking inside the sneaking reach", Player{Sneaking: true}, 27.9, true},
		{"sneaking outside the sneaking reach", Player{Sneaking: true}, 28.1, false},
		{"invisible and bare inside the floored reach", Player{Invisible: true}, 2.4, true},
		{"invisible and bare outside the floored reach", Player{Invisible: true}, 2.5, false},
		{"invisible in one helmet inside the quarter cover", Player{Invisible: true, Worn: [4]protocol.ItemStack{3: headPiece(t, "minecraft:iron_helmet")}}, 6.0, true},
		{"invisible in one helmet outside the quarter cover", Player{Invisible: true, Worn: [4]protocol.ItemStack{3: headPiece(t, "minecraft:iron_helmet")}}, 6.2, false},
		{"wearing the zombie's head inside the halved reach", Player{Worn: [4]protocol.ItemStack{3: zombieHead}}, 17.4, true},
		{"wearing the zombie's head outside the halved reach", Player{Worn: [4]protocol.ItemStack{3: zombieHead}}, 17.6, false},
	} {
		z := hunter()
		p := tc.player
		p.X, p.Y, p.Z, p.EntityID, p.Huntable = 0.5+tc.distance, 64, 0.5, 7, true
		tick := &tickView{w: w, players: []Player{p}}
		found := z.nearestHuntable(tick, nil, true) != nil
		if found != tc.want {
			if tc.want {
				t.Errorf("%s: the scan found nothing at %v blocks, want the shape's reach to take them", tc.name, tc.distance)
			} else {
				t.Errorf("%s: the scan found a target at %v blocks, want the shape's reach to refuse them", tc.name, tc.distance)
			}
		}
	}
}

// TestTheLookAndLureCutTheirReachByTheTargetsVisibility pins that the idle
// glance and the lure read the same visibility cut as the hunt scan, and
// that only the glance needs line of sight.
func TestTheLookAndLureCutTheirReachByTheTargetsVisibility(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	wheat := protocol.ItemStack{ItemID: itemID(t, "minecraft:wheat"), Count: 1}
	for _, tc := range []struct {
		name     string
		player   Player
		distance float64
		want     bool
	}{
		{"standing inside the eight blocks", Player{}, 7.9, true},
		{"standing outside the eight blocks", Player{}, 8.1, false},
		{"sneaking inside the sneaking reach", Player{Sneaking: true}, 6.3, true},
		{"sneaking outside the sneaking reach", Player{Sneaking: true}, 6.5, false},
		{"invisible and bare inside the floor", Player{Invisible: true}, 1.9, true},
		{"invisible and bare outside the floor", Player{Invisible: true}, 3.0, false},
	} {
		p := tc.player
		p.X, p.Y, p.Z = 0.5+tc.distance, 64, 0.5
		tick := &tickView{w: w, players: []Player{p}}
		if got := NewMob(TypeZombie, 1, 0.5, 64, 0.5, 3).nearestPlayerWithin(tick, lookRange, true) != nil; got != tc.want {
			t.Errorf("look, %s: noticed = %v, want %v", tc.name, got, tc.want)
		}
	}

	for _, tc := range []struct {
		name     string
		player   Player
		distance float64
		want     bool
	}{
		{"standing inside the ten blocks", Player{}, 9.9, true},
		{"standing outside the ten blocks", Player{}, 10.1, false},
		{"sneaking inside the eight blocks", Player{Sneaking: true}, 7.9, true},
		{"sneaking outside the eight blocks", Player{Sneaking: true}, 8.1, false},
		{"invisible and bare inside the floor", Player{Invisible: true}, 1.9, true},
		{"invisible and bare outside the floor", Player{Invisible: true}, 5.0, false},
	} {
		p := tc.player
		p.X, p.Y, p.Z, p.HeldItem = 0.5+tc.distance, 64, 0.5, wheat
		tick := &tickView{w: w, players: []Player{p}}
		if got := NewMob(TypeCow, 1, 0.5, 64, 0.5, 3).nearestTemptingPlayerWithin(tick, temptRange) != nil; got != tc.want {
			t.Errorf("lure, %s: tempted = %v, want %v", tc.name, got, tc.want)
		}
	}

	// A block between the two blinds the glance and not the lure.
	stone := defaultState(t, "minecraft:stone")
	for y := int32(64); y <= 67; y++ {
		w.set(2, y, 0, stone)
	}
	p := Player{X: 4.5, Y: 64, Z: 0.5, HeldItem: wheat}
	tick := &tickView{w: w, players: []Player{p}}
	if NewMob(TypeZombie, 1, 0.5, 64, 0.5, 3).nearestPlayerWithin(tick, lookRange, true) != nil {
		t.Errorf("the glance found a player behind a wall")
	}
	if NewMob(TypeCow, 1, 0.5, 64, 0.5, 3).nearestTemptingPlayerWithin(tick, temptRange) == nil {
		t.Errorf("the lure lost a player behind a wall, want line of sight ignored")
	}
}
