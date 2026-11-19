package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// fakeChests is a ChestAccess over a fixed set of chest cells, each blocked
// or locked as listed.
type fakeChests struct {
	cells           [][3]int32
	blocked, locked map[[3]int32]bool
}

func (f fakeChests) ChestsIn(x0, y0, z0, x1, y1, z1 int32, into [][3]int32) [][3]int32 {
	return append(into, f.cells...)
}
func (f fakeChests) ChestBlocked(x, y, z int32) bool { return f.blocked[[3]int32{x, y, z}] }
func (f fakeChests) ChestLocked(x, y, z int32) bool  { return f.locked[[3]int32{x, y, z}] }
func (fakeChests) ChestInUse(x, y, z int32) bool     { return false }
func (fakeChests) ChestPartner(x, y, z int32) ([3]int32, bool) {
	return [3]int32{}, false
}
func (fakeChests) ChestEmpty(x, y, z int32) (empty, ok bool)                          { return false, true }
func (fakeChests) ChestHolds(x, y, z int32, item protocol.ItemStack) bool             { return false }
func (fakeChests) ChestTake(x, y, z int32) protocol.ItemStack                         { return protocol.ItemStack{} }
func (fakeChests) ChestPut(x, y, z int32, held protocol.ItemStack) protocol.ItemStack { return held }

// A blocked or locked copper chest nearer than a usable one is neither picked
// nor spent as a visited slot.
// vanilla: TransportItemsBetweenContainers.isTargetValidToPick
func TestACopperGolemPicksNoBlockedOrLockedCopperChest(t *testing.T) {
	t.Parallel()
	near, far := [3]int32{2, 64, 0}, [3]int32{6, 64, 0}
	for _, tc := range []struct {
		name  string
		chest fakeChests
	}{
		{"blocked", fakeChests{cells: [][3]int32{near, far}, blocked: map[[3]int32]bool{near: true}}},
		{"locked", fakeChests{cells: [][3]int32{near, far}, locked: map[[3]int32]bool{near: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newTestWorld()
			copper := defaultState(t, "minecraft:copper_chest")
			w.set(near[0], near[1], near[2], copper)
			w.set(far[0], far[1], far[2], copper)
			m := NewMob(TypeCopperGolem, 1, 0.5, 64, 0.5, 1)
			m.Chests = tc.chest
			g := &copperTransport{m: m}
			g.updateInvalidTarget(&tickView{w: w})
			if want := (Waypoint{far[0], far[1], far[2]}); !g.has || g.target != want {
				t.Fatalf("target = %+v (has %v), want the farther chest %+v", g.target, g.has, want)
			}
			if _, ok := g.visited[Waypoint{near[0], near[1], near[2]}]; ok {
				t.Error("the nearer chest was marked visited")
			}
		})
	}
}
