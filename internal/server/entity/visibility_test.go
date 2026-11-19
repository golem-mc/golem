package entity

import (
	"math"
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

// TestTheVisibilityMatchReadsTheKindExactly holds the head match to the
// exact reading vanilla's own takes: the zombie's head halves a zombie's
// reach alone — a husk, a drowned and a zombie villager read none of it —
// the skeleton's skull halves the skeleton's own and not its variants',
// and the creeper's head the creeper's.
func TestTheVisibilityMatchReadsTheKindExactly(t *testing.T) {
	zombieHead := headPiece(t, "minecraft:zombie_head")
	skeletonSkull := headPiece(t, "minecraft:skeleton_skull")
	creeperHead := headPiece(t, "minecraft:creeper_head")
	for _, tc := range []struct {
		name string
		kind MobType
		// head is the piece on the target's head slot, empty for none.
		head protocol.ItemStack
		// want is the visibility range the shape leaves, in blocks.
		want float64
	}{
		{"a zombie wearing the zombie's head", TypeZombie, zombieHead, 17.5},
		{"a husk wearing the zombie's head", TypeHusk, zombieHead, 35},
		{"a drowned wearing the zombie's head", TypeDrowned, zombieHead, 35},
		{"a zombie villager wearing the zombie's head", TypeZombieVillager, zombieHead, 35},
		{"a skeleton wearing the skeleton's skull", TypeSkeleton, skeletonSkull, 8},
		{"a stray wearing the skeleton's skull", TypeStray, skeletonSkull, 16},
		{"a bogged wearing the skeleton's skull", TypeBogged, skeletonSkull, 16},
		{"a zombie wearing the skeleton's skull", TypeZombie, skeletonSkull, 35},
		{"a creeper wearing the creeper's head", TypeCreeper, creeperHead, 8},
		{"a zombie wearing the creeper's head", TypeZombie, creeperHead, 35},
		{"a spider wearing nothing", TypeSpider, protocol.ItemStack{}, 16},
	} {
		m := NewMob(tc.kind, 1, 0.5, 64, 0.5, 3)
		p := Player{Worn: [4]protocol.ItemStack{3: tc.head}}
		if got := m.visibilityRange(&p); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%s: the visibility range = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestSneakingAndInvisibleBareFloorsAtTwoBlocks checks the floor: a target
// that sneaks invisible and bare stacks the sneak's 0.8 over the Invisibility
// effect's bare 0.07 — 1.96 of the zombie's thirty-five — and still shows
// at the two blocks the closest body does, so nothing short of an empty
// world hides a hunted target wholly.
func TestSneakingAndInvisibleBareFloorsAtTwoBlocks(t *testing.T) {
	z := hunter()
	p := Player{Sneaking: true, Invisible: true}
	if got := z.visibilityRange(&p); got != minVisibilityDistance {
		t.Errorf("the stacked shapes' visibility range = %v, want the floor of %v", got, minVisibilityDistance)
	}
}

// TestTheHuntKeepsItsTargetByTheWholeRange checks the other half: the reach
// a hunt may open by is cut by the target's state, the reach an open hunt
// is kept by is not — a sneaking target thirty blocks out, past the
// sneaking reach but inside the follow range, is never dropped.
func TestTheHuntKeepsItsTargetByTheWholeRange(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	z := hunter()
	players := []Player{{
		X: 30.5, Y: 64, Z: 0.5, EntityID: 7, Huntable: true, Sneaking: true,
	}}
	z.TargetID = 7
	for range 40 {
		z.Tick(w, players)
		if z.TargetID != 7 {
			t.Fatalf("the hunt dropped its sneaking target at %v blocks, want the whole follow range kept by",
				math.Sqrt(Dist2(z.X, z.Y, z.Z, players[0].X, players[0].Y, players[0].Z)))
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

// TestTheGlanceKeepsLookingAfterTheTargetHides pins that the visibility cut
// and line of sight gate only the start of a glance.
func TestTheGlanceKeepsLookingAfterTheTargetHides(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	m := NewMob(TypeZombie, 1, 0.5, 64, 0.5, 3)
	g := &lookAtPlayer{m: m, remains: 10}
	p := Player{EntityID: 7, X: 6.5, Y: 64, Z: 0.5}
	g.target = 7
	p.Invisible = true
	stone := defaultState(t, "minecraft:stone")
	for y := int32(64); y <= 67; y++ {
		w.set(2, y, 0, stone)
	}
	tick := &tickView{w: w, players: []Player{p}}
	if !g.ShouldContinue(tick) {
		t.Errorf("the glance ended when the target hid, want it kept within the raw range")
	}
	tick.players[0].X = 9.5
	if g.ShouldContinue(tick) {
		t.Errorf("the glance kept a target past the look range")
	}
}

// TestTheLureSkipsSpectatorsAndTheDead pins that a body nobody can see tempts
// no mob and draws no glance.
func TestTheLureSkipsSpectatorsAndTheDead(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	wheat := protocol.ItemStack{ItemID: itemID(t, "minecraft:wheat"), Count: 1}
	for _, tc := range []struct {
		name   string
		player Player
		want   bool
	}{
		{"alive survival", Player{}, true},
		{"spectator", Player{Spectator: true}, false},
		{"dead", Player{Dead: true}, false},
	} {
		p := tc.player
		p.X, p.Y, p.Z, p.HeldItem = 3.5, 64, 0.5, wheat
		tick := &tickView{w: w, players: []Player{p}}
		if got := NewMob(TypeCow, 1, 0.5, 64, 0.5, 3).nearestTemptingPlayerWithin(tick, temptRange) != nil; got != tc.want {
			t.Errorf("%s: tempted = %v, want %v", tc.name, got, tc.want)
		}
		if got := NewMob(TypeZombie, 1, 0.5, 64, 0.5, 3).nearestPlayerWithin(tick, lookRange, false) != nil; got != tc.want {
			t.Errorf("%s: glanced = %v, want %v", tc.name, got, tc.want)
		}
	}
}
