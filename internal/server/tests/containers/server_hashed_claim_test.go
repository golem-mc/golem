package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestHashedClaimOfAFilledShulkerBoxIsConfirmed claims a shulker box of one
// diamond with the container checksum vanilla computes; the claim matches,
// so the server sends no slot correction.
// vanilla: HashOps.hashMap
func TestHashedClaimOfAFilledShulkerBoxIsConfirmed(t *testing.T) {
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	shulker := int32(registry.ItemShulkerBox)
	w.SetCreativeSlot(36, protocol.ItemStack{ItemID: shulker, Count: 1,
		Container: []protocol.ItemStack{{ItemID: servertest.MustItem("minecraft:diamond"), Count: 1}}})

	w.Send(protocol.IDPlayClickContainer, func(b *protocol.Buffer) {
		b.PutVarInt(0)
		b.PutVarInt(w.StateOf(0))
		b.PutI16(9)
		b.PutI8(0)
		b.PutVarInt(protocol.ClickModePickup)
		b.PutVarInt(1) // one changed slot
		b.PutI16(36)
		b.PutBool(true)
		b.PutVarInt(shulker)
		b.PutVarInt(1)
		b.PutVarInt(1) // one added component
		b.PutVarInt(protocol.DataComponentContainer)
		b.PutI32(-0x36B5D714) // 0xC94A28EC, from the 26.3 server's HashOps
		b.PutVarInt(0)        // none removed
		b.PutBool(false)      // nothing carried
	})

	w.ExpectNoneWithinTicks(srv, servertest.QuietTicks, func(id int32, data []byte) bool {
		if id != protocol.IDPlayContainerSlot {
			return false
		}
		r := protocol.NewReader(data)
		window := r.ReadVarInt()
		r.ReadVarInt() // state
		return window == 0 && r.ReadI16() == 36
	})
}
