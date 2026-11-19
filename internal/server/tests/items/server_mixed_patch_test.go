package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestMixedComponentPatchReachesClient(t *testing.T) {
	t.Parallel()
	for _, creative := range []bool{false, true} {
		name := "Give"
		if creative {
			name = "Creative"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, c, _ := servertest.JoinAs(t, servertest.BlockTestConfig(), name, servertest.OpsListing(t, name))
			servertest.HoldTicks(t, srv)
			want := protocol.ItemStack{ItemID: int32(registry.ItemApple), Count: 1, CustomName: &protocol.Text{Text: "probe"}}
			want.Remove(protocol.DataComponentFood)
			if creative {
				c.Send(protocol.IDPlayCreativeSlot, func(b *protocol.Buffer) {
					b.PutI16(player.HotbarSlot)
					b.PutUntrustedSlot(want)
				})
				c.Settle()
				if got := srv.InventorySlot(name, player.HotbarSlot); !got.Equal(want) {
					t.Fatalf("creative mixed patch = %+v, want %+v", got, want)
				}
				// /item sends the accepted stack back through the trusted codec.
				c.ChatCommand("item replace entity @s hotbar.1 from entity @s hotbar.0")
			} else {
				c.ChatCommand(`give @s apple[!food,custom_name="probe"]`)
			}
			if !c.ReadTicks(srv, 20, func(id int32, data []byte) bool {
				if id != protocol.IDPlayContainerSlot {
					return false
				}
				r := protocol.NewReader(data)
				r.ReadVarInt() // container
				r.ReadVarInt() // state
				slot := r.ReadI16()
				got, ok := r.ReadSlot()
				if !ok || r.Err() != nil || r.Remaining() != 0 || !got.Equal(want) {
					t.Fatalf("slot %d: %+v, ok=%v err=%v unread=%d; want %+v", slot, got, ok, r.Err(), r.Remaining(), want)
				}
				return true
			}) {
				t.Fatal("no mixed component slot reached the client")
			}
		})
	}
}
