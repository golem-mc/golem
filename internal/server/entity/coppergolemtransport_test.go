package entity

import (
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
