package containers

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
)

// A blast that destroys a container must take it out of the world before it
// reads its slots. Anything that changes the container after the read but
// before the removal — a hopper move, a dispense — leaves the stack the
// spill already took duplicated on the ground, because the block entity the
// change went to is still standing when the blast removes it.

// itemTotal counts one item across a container's slots and the item
// entities lying in the overworld, so a test can follow the stack it seeded
// through a blast, past the chest block's own drop.
func itemTotal(srv *server.Server, name string, id int32, slots []protocol.ItemStack) int32 {
	var n int32
	for _, s := range slots {
		if s.ItemID == id {
			n += s.Count
		}
	}
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item == name {
			n += it.Count
		}
	}
	return n
}
