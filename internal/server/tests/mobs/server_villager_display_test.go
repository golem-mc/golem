package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: AbstractVillager.getOffers, ShowTradesToPlayer
func TestAFreshLibrarianGeneratesOffersToDisplay(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "FreshCustomer", "minecraft:emerald", 1)
	servertest.HoldTicks(t, srv)
	srv.SetDayTime(2000)
	id := srv.SpawnVillagerWithOffers("minecraft:librarian", 1, nil, 2.5, 64, 0.5)
	awaitVillagerDisplay(t, srv, c, id, func(stack protocol.ItemStack) bool {
		// Two of the three novice trades are drawn, so the bookshelf is optional.
		return stack.Count == 1 && (stack.ItemID == servertest.MustItemID(t, "minecraft:bookshelf") ||
			stack.ItemID == servertest.MustItemID(t, "minecraft:enchanted_book") && len(stack.StoredEnchantments) > 0)
	})
	offers, _ := srv.VillagerSaved(id)
	if offers == nil {
		t.Fatal("display did not generate offers")
	}
}

func awaitVillagerDisplay(t *testing.T, srv *server.Server, c *servertest.Client, id int32, matches func(protocol.ItemStack) bool) {
	t.Helper()
	c.WatchVariant(srv, "minecraft:villager")
	shown := false
	for range 10 {
		servertest.RunTicks(t, srv, 2)
		c.SettleAndCollect(func(pid int32, data []byte) {
			if pid != protocol.IDPlayEntityEquipment {
				return
			}
			r := protocol.NewReader(data)
			if r.ReadVarInt() != id {
				return
			}
			for {
				slot := r.ReadU8()
				stack, ok := r.ReadSlot()
				if !ok {
					t.Fatalf("equipment: %v", r.Err())
				}
				if slot&0x7f == protocol.EquipMainHand && matches(stack) {
					shown = true
				}
				if slot&0x80 == 0 {
					break
				}
			}
		})
		if shown {
			return
		}
	}
	t.Fatal("no main-hand equipment containing the expected sold stack")
}
