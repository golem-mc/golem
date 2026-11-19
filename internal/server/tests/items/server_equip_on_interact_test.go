package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// bareZombie stands the client beside a still zombie with nothing on its
// head and returns the zombie's entity ID.
func bareZombie(t *testing.T, srv *server.Server, w *servertest.WindowedClient) int32 {
	t.Helper()
	w.MoveToGround(2.5, 64, 5.5, true)
	w.Settle()
	srv.SpawnMob("minecraft:zombie", 4.5, 64, 5.5)
	id := w.ExpectMobSpawn(protocol.EntityTypeZombie).EntityID
	srv.SetMobNoAI(id)
	srv.SetMobEquipment(id, "head", protocol.ItemStack{})
	w.Settle()
	return id
}

// equipmentUpdates runs n ticks and returns the stacks sent for the mob's wire slot.
func equipmentUpdates(t *testing.T, srv *server.Server, w *servertest.WindowedClient, id int32, n int, wireSlot uint8) []protocol.ItemStack {
	t.Helper()
	var updates []protocol.ItemStack
	w.ReadTicks(srv, n, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlayEntityEquipment {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return false
		}
		for {
			slot := r.ReadU8()
			stack, _ := r.ReadSlot()
			if slot&0x7F == wireSlot {
				updates = append(updates, stack)
			}
			if slot&0x80 == 0 || r.Err() != nil {
				break
			}
		}
		return false
	})
	return updates
}

// TestEquipOnInteractPutsTheHelmetOnAZombie: a helmet whose own equippable
// has equip_on_interact goes on a zombie's bare head when used on it, and
// the hand is emptied.
// vanilla: ItemStack.interactLivingEntity, Equippable.equipOnTarget
func TestEquipOnInteractPutsTheHelmetOnAZombie(t *testing.T) {
	t.Parallel()
	srv, w := givenWindowed(t, servertest.SurvivalTestConfig(), `diamond_helmet[equippable={slot:"head",equip_on_interact:true}]`)
	id := bareZombie(t, srv, w)

	w.UseOn(id)
	heads := equipmentUpdates(t, srv, w, id, 2, 5)
	helmet := servertest.MustItemID(t, "minecraft:diamond_helmet")
	if len(heads) == 0 || heads[len(heads)-1].ItemID != helmet {
		t.Errorf("head updates %+v, want the diamond helmet", heads)
	}
	if got := srv.MobEquipment(id, "head"); got.ItemID != helmet || got.Count != 1 {
		t.Errorf("zombie head = %+v, want one diamond helmet", got)
	}
	expectSlot(t, srv, player.HotbarSlot, "")
}
