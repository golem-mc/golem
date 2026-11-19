package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestMobEquipmentSingleSlotUpdates(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Smith"))
	actor := connect().Join("Smith")
	watcher := connect().Join("Watcher")
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:cow", 4.5, 64, 7.5)
	if !srv.SetMobNoAI(id) {
		t.Fatal("cow did not spawn")
	}
	uuid, _ := srv.MobUUID(id)
	actor.Settle()
	watcher.Settle()
	servertest.RunTicks(t, srv, 2)
	actor.Settle()
	watcher.Settle()
	slots := []string{"mainhand", "offhand", "feet", "legs", "chest", "head", "body"}
	names := []string{"stone", "dirt", "diamond_boots", "diamond_leggings", "diamond_chestplate", "diamond_helmet", "iron_horse_armor"}
	check := func(slot int, want *protocol.ItemStack) {
		t.Helper()
		servertest.RunTicks(t, srv, 1)
		for _, c := range []*servertest.Client{actor, watcher} {
			count := 0
			c.SettleAndCollect(func(packetID int32, data []byte) {
				if packetID != protocol.IDPlayEntityEquipment {
					return
				}
				r := protocol.NewReader(data)
				if r.ReadVarInt() != id {
					return
				}
				count++
				if want == nil {
					t.Errorf("%s: unexpected equipment update", slots[slot])
					return
				}
				raw := r.ReadU8()
				got, ok := r.ReadSlot()
				if raw != uint8(slot) || !ok || !got.Equal(*want) || r.Err() != nil || r.Remaining() != 0 {
					t.Errorf("%s: slot %d stack %+v, want %+v (error %v)", slots[slot], raw, got, *want, r.Err())
				}
			})
			expected := 0
			if want != nil {
				expected = 1
			}
			if count != expected {
				t.Errorf("%s: got %d packets, want %d", slots[slot], count, expected)
			}
		}
	}
	for slot, name := range names {
		t.Run(slots[slot], func(t *testing.T) {
			command := fmt.Sprintf(`data merge entity %s {equipment:{%s:{id:"minecraft:%s",count:1}}}`, uuid, slots[slot], name)
			actor.ChatCommand(command)
			actor.Settle()
			want := servertest.ItemStack(t, "minecraft:"+name, 1)
			check(slot, &want)
			check(slot, nil)
			if slot == protocol.EquipMainHand {
				srv.SetMobHand(id, want)
				check(slot, nil)
				// Two writes between sends can return to the watcher's baseline.
				changed := want
				changed.Count++
				srv.SetMobHand(id, changed)
				srv.SetMobHand(id, want)
				check(slot, nil)
			}
			// A write that leaves the slot unchanged must not emit a packet.
			actor.ChatCommand(command)
			actor.Settle()
			check(slot, nil)
			actor.ChatCommand(fmt.Sprintf(`data remove entity %s equipment.%s`, uuid, slots[slot]))
			actor.Settle()
			empty := protocol.ItemStack{}
			check(slot, &empty)
			check(slot, nil)
		})
	}
}
