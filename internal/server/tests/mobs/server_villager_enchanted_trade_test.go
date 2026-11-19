package mobs

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func TestAnArmorerSendsAndKeepsItsEnchantedTrades(t *testing.T) {
	t.Parallel()
	srv, c := villagerYard(t, servertest.OpsListing(t, "Villager"))
	c.ChatCommand(`summon minecraft:villager 1.5 64 0.5 {NoAI:1b,VillagerData:{profession:"minecraft:armorer",level:5,type:"minecraft:plains"}}`)
	id := c.AwaitSpawn(villagerType(t)).EntityID
	var first nbt.Compound
	for range 2 {
		c.UseOn(id)
		var payload []byte
		c.SettleAndCollect(func(pid int32, data []byte) {
			if pid == protocol.IDPlayMerchantOffers {
				payload = data
			}
		})
		if payload == nil {
			t.Fatal("armorer sent no offers")
		}
		r := protocol.NewReader(payload)
		window := r.ReadVarInt()
		count := r.ReadVarInt()
		saved, _ := srv.VillagerSaved(id)
		offers, _ := inventory.OffersFromTag(saved)
		if int(count) != len(offers) {
			t.Fatalf("wire count %d, saved %d", count, len(offers))
		}
		enchanted := 0
		for i := range count {
			item, cost, predicate := r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt()
			if predicate != 0 {
				t.Fatalf("cost predicate %d", predicate)
			}
			result, ok := r.ReadSlot()
			if !ok {
				t.Fatal("invalid result stack")
			}
			if item != offers[i].CostA.ItemID || cost != offers[i].CostA.Count || !reflect.DeepEqual(result.Enchantments, offers[i].Result.Enchantments) {
				t.Fatalf("offer %d did not match the saved enchanted item and price", i)
			}
			if len(result.Enchantments) > 0 {
				enchanted++
				if cost < 13 || cost > 35 {
					t.Errorf("enchanted diamond price %d, want base 8..16 plus 5..19", cost)
				}
			}
			if r.ReadBool() {
				r.ReadVarInt()
				r.ReadVarInt()
				r.ReadVarInt()
			}
			r.ReadBool() // out of stock
			r.ReadI32()
			r.ReadI32()
			r.ReadI32()
			r.ReadI32()
			r.ReadF32()
			r.ReadI32()
		}
		if enchanted != 2 {
			t.Fatalf("%d enchanted diamond offers, want two", enchanted)
		}
		if first == nil {
			first = saved
		} else if !reflect.DeepEqual(first, saved) {
			t.Fatal("reopening redrew the saved offers")
		}
		c.Send(protocol.IDPlayCloseContainerIn, func(b *protocol.Buffer) { b.PutVarInt(window) })
		c.Settle()
	}
}
