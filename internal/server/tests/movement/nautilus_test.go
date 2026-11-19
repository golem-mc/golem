package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestNautilusTamesEquipsAndCarriesRider(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Rider", servertest.OpsListing(t, "Rider"))
	servertest.HoldTicks(t, srv)
	srv.SetPlayerGameMode("Rider", protocol.GameModeSurvival)
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	id := srv.SpawnMobSeeded("minecraft:nautilus", 5.5, 64, 5.5, 42)
	srv.SetMobNoAI(id)
	c.Settle()
	tamed := false
	for attempt := 0; attempt < 30 && !tamed; attempt++ {
		srv.GiveSlot("Rider", player.HotbarSlot, protocol.ItemStack{ItemID: int32(registry.ItemPufferfishBucket), Count: 1})
		c.Settle()
		c.UseOn(id)
		c.SettleAndCollect(func(packet int32, data []byte) {
			if packet == protocol.IDPlayEntityEvent {
				r := protocol.NewReader(data)
				if r.ReadI32() == id && r.ReadU8() == protocol.EntityEventHorseTamed {
					tamed = true
				}
			}
		})
		if held := srv.InventorySlot("Rider", player.HotbarSlot); held.ItemID != int32(registry.ItemWaterBucket) {
			t.Fatalf("taming hand=%+v, want water bucket", held)
		}
	}
	if !tamed {
		t.Fatal("nautilus never tamed")
	}
	for _, item := range []registry.Item{registry.ItemSaddle, registry.ItemIronNautilusArmor} {
		srv.GiveSlot("Rider", player.HotbarSlot, protocol.ItemStack{ItemID: int32(item), Count: 1})
		c.Settle()
		c.UseOn(id)
		c.Settle()
		if held := srv.InventorySlot("Rider", player.HotbarSlot); !held.Empty() {
			t.Fatalf("equipment not consumed: %+v", held)
		}
	}
	sneakClick(c, id)
	screen, columns, slots := openMountScreen(t, c, id)
	if columns != 0 || len(slots) != 2+player.MainInvSlots || slots[0].ItemID != int32(registry.ItemSaddle) || slots[1].ItemID != int32(registry.ItemIronNautilusArmor) {
		t.Fatalf("nautilus screen: columns=%d slots=%v", columns, slots)
	}
	screen.CloseWindow()
	c.Settle()
	c.UseOn(id)
	seated := false
	c.SettleAndCollect(func(packet int32, data []byte) {
		if packet == protocol.IDPlaySetPassengers {
			vehicle, riders := servertest.ReadPassengers(c, data)
			if vehicle == id && len(riders) == 1 && riders[0] == c.PlayerEntityID {
				seated = true
			}
		}
	})
	if !seated {
		t.Fatal("click did not mount tamed nautilus")
	}
	// Server movement accepts a saddled rider's reports, including vertical travel.
	c.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
		b.PutF64(7.5)
		b.PutF64(65)
		b.PutF64(5.5)
		b.PutF32(0)
		b.PutF32(0)
		b.PutBool(false)
	})
	c.Settle()
	if x, y, _, ok := srv.MobPos(id); !ok || x != 7.5 || y != 65 {
		t.Fatalf("ridden position=%.2f %.2f (%v)", x, y, ok)
	}
	c.Send(protocol.IDPlayPlayerCommand, func(b *protocol.Buffer) {
		b.PutVarInt(c.PlayerEntityID)
		b.PutVarInt(protocol.PlayerCommandStartRidingJump)
		b.PutVarInt(80)
	})
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	breath, dash := false, false
	c.SettleAndCollect(func(packet int32, data []byte) {
		if packet == protocol.IDPlayEntityMetadata {
			r := protocol.NewReader(data)
			if r.ReadVarInt() == id {
				for r.Err() == nil {
					index := r.ReadU8()
					if index == protocol.MetadataEnd {
						break
					}
					kind := r.ReadVarInt()
					if index == protocol.MetadataIndexNautilusDash {
						dash = r.ReadBool()
					} else if kind == protocol.MetadataOptionalLivingEntityReference {
						if r.ReadBool() {
							r.ReadUUID()
						}
					} else {
						servertest.SkipMetadataField(c, r, kind)
					}
				}
			}
		}
		if packet == protocol.IDPlayUpdateMobEffect {
			in := servertest.MobEffectFor(data)
			if in.Entity == c.PlayerEntityID && in.Effect == registry.MustMobEffect("minecraft:breath_of_the_nautilus") {
				breath = true
			}
		}
	})
	if !breath || !dash {
		t.Fatalf("rider breath=%v dash metadata=%v", breath, dash)
	}
}
