package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: LivingEntity.collectEquipmentChanges, ServerEntity.sendPairingData
func TestMobEquipmentTickUpdatesEveryWatcher(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Smith"))
	actor := connect().Join("Smith")
	watcher := connect().Join("Watcher")
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:cow", 4.5, 64, 7.5)
	if !srv.SetMobNoAI(id) {
		t.Fatal("cow did not spawn")
	}
	actor.Settle()
	watcher.Settle()
	uuid, _ := srv.MobUUID(id)
	check := func(name string, command string, want map[uint8]protocol.ItemStack) {
		t.Helper()
		if command != "" {
			actor.ChatCommand(command)
			actor.Settle()
		}
		servertest.RunTicks(t, srv, 2)
		for i, c := range []*servertest.Client{actor, watcher} {
			packets := 0
			c.SettleAndCollect(func(packetID int32, data []byte) {
				if packetID != protocol.IDPlayEntityEquipment {
					return
				}
				r := protocol.NewReader(data)
				if r.ReadVarInt() != id {
					return
				}
				packets++
				key := fmt.Sprintf("%s/watcher%d", name, i)
				got := map[uint8]protocol.ItemStack{}
				for {
					slot := r.ReadU8()
					stack, ok := r.ReadSlot()
					if !ok {
						t.Fatal("invalid equipment stack")
					}
					got[slot&0x7f] = stack
					if slot&0x80 == 0 {
						break
					}
				}
				if r.Err() != nil || r.Remaining() != 0 {
					t.Fatalf("%s: invalid equipment packet: %v, %d trailing bytes", key, r.Err(), r.Remaining())
				}
				if len(got) != len(want) {
					t.Fatalf("%s: got %d slots, want %d", key, len(got), len(want))
				}
				for slot, stack := range want {
					if actual, ok := got[slot]; !ok || !actual.Equal(stack) {
						t.Errorf("%s: slot %d = %+v, want %+v", key, slot, actual, stack)
					}
				}
			})
			if want == nil && packets != 0 || want != nil && packets != 1 {
				t.Errorf("%s: watcher %d got %d equipment packets", name, i, packets)
			}
		}
	}
	check("bare", "", nil)
	names := []string{"stone", "dirt", "diamond_boots", "diamond_leggings", "diamond_chestplate", "diamond_helmet", "iron_horse_armor"}
	slots := []string{"mainhand", "offhand", "feet", "legs", "chest", "head", "body"}
	want := map[uint8]protocol.ItemStack{}
	equipment := ""
	for i, name := range names {
		if i > 0 {
			equipment += ","
		}
		equipment += fmt.Sprintf(`%s:{id:"minecraft:%s",count:1}`, slots[i], name)
		want[uint8(i)] = servertest.ItemStack(t, "minecraft:"+name, 1)
	}
	check("equipped", "data merge entity "+uuid+" {equipment:{"+equipment+"}}", want)
	check("unchanged", "", nil)
	worn := want[protocol.EquipFeet]
	worn.Damage = 1
	counted := want[protocol.EquipOffHand]
	counted.Count = 2
	check("wear-and-count", `data merge entity `+uuid+` {equipment:{feet:{components:{"minecraft:damage":1}},offhand:{count:2}}}`,
		map[uint8]protocol.ItemStack{protocol.EquipFeet: worn, protocol.EquipOffHand: counted})
	check("unchanged-after-wear", "", nil)
	empty := map[uint8]protocol.ItemStack{}
	for i := range names {
		empty[uint8(i)] = protocol.ItemStack{}
	}
	check("removed", "data modify entity "+uuid+" equipment set value {}", empty)
	check("still-bare", "", nil)
}
