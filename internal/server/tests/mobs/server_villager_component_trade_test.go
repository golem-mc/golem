package mobs

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestVillagersSendAndKeepTheirComponentTrades(t *testing.T) {
	for _, tc := range []struct {
		profession string
		level      int
		component  int32
	}{
		{"cartographer", 2, protocol.DataComponentMapDecorations},
		{"leatherworker", 1, protocol.DataComponentDyedColor},
		{"farmer", 4, protocol.DataComponentSuspiciousStew},
		{"fletcher", 5, protocol.DataComponentPotionContents},
	} {
		t.Run(tc.profession, func(t *testing.T) {
			t.Parallel()
			srv, c := villagerYard(t, servertest.OpsListing(t, "Villager"))
			switch tc.profession {
			case "cartographer":
				c.ChatCommand(`summon minecraft:villager 1.5 64 0.5 {NoAI:1b,VillagerData:{profession:"minecraft:cartographer",level:2,type:"minecraft:plains"},Offers:{Recipes:[{buy:{id:"minecraft:emerald",count:8},sell:{id:"minecraft:filled_map",count:1,components:{"minecraft:map_id":7,"minecraft:item_name":{translate:"filled_map.monument"},"minecraft:map_decorations":{"+":{type:"minecraft:monument",x:100d,z:200d,rotation:180f}}}},maxUses:12}]}}`)
			case "fletcher":
				// A master fletcher can omit the arrow trade from its two draws.
				set, _ := registry.FindTradeSet("fletcher/level_5")
				set.Amount = len(set.Trades)
				offers := inventory.DrawOffers(nil, set, "minecraft:plains", rand.New(rand.NewSource(1)), nil)
				srv.SpawnVillagerWithOffers("minecraft:fletcher", 5, offers, 1.5, 64, 0.5)
			default:
				c.ChatCommand(fmt.Sprintf(`summon minecraft:villager 1.5 64 0.5 {NoAI:1b,VillagerData:{profession:"minecraft:%s",level:%d,type:"minecraft:plains"}}`, tc.profession, tc.level))
			}
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
					t.Fatal("no merchant offers")
				}
				r := protocol.NewReader(payload)
				window, count := r.ReadVarInt(), r.ReadVarInt()
				saved, _ := srv.VillagerSaved(id)
				offers, _ := inventory.OffersFromTag(saved)
				if int(count) != len(offers) {
					t.Fatalf("wire offers %d, saved %d", count, len(offers))
				}
				found := false
				for i := range count {
					r.ReadVarInt() // cost item
					r.ReadVarInt() // cost count
					if r.ReadVarInt() != 0 {
						t.Fatal("unexpected cost predicate")
					}
					result, ok := r.ReadSlot()
					expected := offers[i].Result
					if !ok || !result.Equal(expected) {
						t.Fatalf("offer %d lost its saved components", i)
					}
					// The markers are a modelled field, not a raw component.
					if tc.component == protocol.DataComponentMapDecorations && len(result.MapDecorations) > 0 {
						found = true
					}
					for _, raw := range result.Raw {
						if raw.Type == tc.component {
							found = true
						}
					}
					if r.ReadBool() {
						r.ReadVarInt()
						r.ReadVarInt()
						r.ReadVarInt()
					}
					r.ReadBool() // out of stock
					r.ReadI32()  // uses
					r.ReadI32()  // max uses
					r.ReadI32()  // xp
					r.ReadI32()  // special price
					r.ReadF32()  // multiplier
					r.ReadI32()  // demand
				}
				if r.Err() != nil || !found {
					t.Fatalf("component %d found %v, read error %v", tc.component, found, r.Err())
				}
				if first == nil {
					first = saved
				} else if !nbt.Equal(first, saved) {
					t.Fatal("reopening redrew the saved offers")
				}
				c.Send(protocol.IDPlayCloseContainerIn, func(b *protocol.Buffer) { b.PutVarInt(window) })
				c.Settle()
			}
		})
	}
}
