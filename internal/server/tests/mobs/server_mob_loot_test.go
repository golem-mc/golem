package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"strings"

	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
)

// The mob loot tests: a mob set to CanPickUpLoot — the tag a summon reads —
// takes what lies within reach of its box and wears or holds the pieces it
// can better, the mob griefing rule vetoing the whole pass, and the wire the
// taking is told by: the pickup, the item's removal and the Set Equipment
// that names the new piece. Every body the tests need is summoned through
// its tag, and stands NoAI so it stays where its item is laid.

// overworldHoldsItem reports whether the item entity with the given ID is
// still lying in the overworld.
func overworldHoldsItem(srv *server.Server, id int32) bool {
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.ID == id {
			return true
		}
	}
	return false
}

// overworldHoldsItemNamed reports whether any item entity of the name is
// lying in the overworld.
func overworldHoldsItemNamed(srv *server.Server, name string) bool {
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Item == name {
			return true
		}
	}
	return false
}

// lootSkip is the background a client hears while a loot pickup is being
// watched: the world's own traffic and the packets everything streams. The
// tells of the pickup itself — the equipment, the pickup and the removals —
// are not background here.
func lootSkip(id int32) bool {
	switch id {
	case protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight,
		protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks, protocol.IDPlayChunkData,
		protocol.IDPlayChunkBatchStart, protocol.IDPlayChunkBatchFinished,
		protocol.IDPlayCenterChunk, protocol.IDPlaySoundEffect,
		protocol.IDPlayWorldEvent, protocol.IDPlayEntityPosition,
		protocol.IDPlayEntityPositionRot, protocol.IDPlayEntityRotation,
		protocol.IDPlayHeadRotation, protocol.IDPlayTeleportEntity,
		protocol.IDPlaySetEntityMotion, protocol.IDPlayUnloadChunk,
		protocol.IDPlaySystemChat, protocol.IDPlayEntityMetadata,
		protocol.IDPlayUpdateAttributes, protocol.IDPlayEntityAnimation:
		return true
	}
	return false
}

// equipmentOf parses one Set Equipment packet, answering the entity it is
// about and its entries.
func equipmentOf(data []byte) (int32, []servertest.EquipmentEntry) {
	r := protocol.NewReader(data)
	entityID := r.ReadVarInt()
	var entries []servertest.EquipmentEntry
	for {
		raw := r.ReadU8()
		stack, _ := r.ReadSlot()
		entries = append(entries, servertest.EquipmentEntry{Slot: raw & 0x7F, Stack: stack})
		if raw&0x80 == 0 {
			return entityID, entries
		}
	}
}

// waitForLootTaken watches the wire for one mob's taking of one item: the
// Pickup Item of the stack flying to the mob, the item's body leaving the
// world, and the Set Equipment putting what it took in the given slot. It
// fails the test for any tell that never came, and waits the item's
// disappearance out on the server too.
func waitForLootTaken(t *testing.T, srv *server.Server, c *servertest.Client, mob, item int32, slot uint8, want int32) {
	t.Helper()
	var pickup, removed, dressed bool
	pumpTicks(c, srv, 400, lootSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayPickupItem:
			r := protocol.NewReader(data)
			got, collector, count := r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt()
			if got != item || collector != mob || count != 1 {
				t.Errorf("the pickup packet was item %d collector %d count %d, want the one item %d flying to %d",
					got, collector, count, item, mob)
			}
			pickup = true
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			for n := r.ReadVarInt(); n > 0; n-- {
				if r.ReadVarInt() == item {
					removed = true
				}
			}
		case protocol.IDPlayEntityEquipment:
			if entity, entries := equipmentOf(data); entity == mob {
				if got := servertest.EquipEntry(entries, slot); got.ItemID != want || got.Count != 1 {
					t.Errorf("the slot %d tells of %+v, want the item %d the mob took", slot, got, want)
				}
				dressed = true
			}
		}
		return pickup && removed && dressed
	})
	if !pickup {
		t.Errorf("the item %d never flew to the mob, want the Pickup Item packet", item)
	}
	if !removed {
		t.Errorf("the item %d never left the world, want the Remove Entities packet", item)
	}
	if !dressed {
		t.Errorf("the mob %d was never shown the item %d in slot %d, want the Set Equipment packet", mob, want, slot)
	}
	servertest.UntilTicks(t, srv, 100, "condition", func() bool { return !overworldHoldsItem(srv, item) })
}

// TestALootPickingZombieWearsTheHelmetItFinds drops an iron helmet at the
// feet of a zombie summoned with CanPickUpLoot:1b — bare-headed, its main
// hand empty — and reads the taking as the wire carries it: the helmet
// flying to the zombie, the item's body leaving the world, and the Set
// Equipment that names its head holding the helmet.
func TestALootPickingZombieWearsTheHelmetItFinds(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Looter"))
	srv.SetDayTime(18000) // midnight, so the bare zombie burns nowhere near
	c := connect().Join("Looter")

	c.ChatCommand(`summon minecraft:zombie 8.5 64 8.5 {CanPickUpLoot:1b,NoAI:1b}`)
	zombie := c.ExpectMobSpawn(protocol.EntityTypeZombie)
	item := srv.SpawnItemEntity(8.5, 64.3, 8.5, "minecraft:iron_helmet", 1)
	if item == 0 {
		t.Fatal("the iron helmet could not be laid down")
	}
	waitForLootTaken(t, srv, c, zombie.EntityID, item, protocol.EquipHead, servertest.MustItemID(t, "minecraft:iron_helmet"))
}

// TestMobGriefingOffLeavesTheLootLying drops a helmet beside a
// CanPickUpLoot zombie with the mob griefing rule turned off: the taking is
// the world's to veto, and neither the item nor the wire shows a pickup
// while it is.
func TestMobGriefingOffLeavesTheLootLying(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Vetoer"))
	srv.SetDayTime(18000) // midnight, so the bare zombie burns nowhere near
	c := connect().Join("Vetoer")

	c.ChatCommand("gamerule mob_griefing false")
	c.ChatCommand(`summon minecraft:zombie 8.5 64 8.5 {CanPickUpLoot:1b,NoAI:1b}`)
	zombie := c.ExpectMobSpawn(protocol.EntityTypeZombie)
	item := srv.SpawnItemEntity(8.5, 64.3, 8.5, "minecraft:golden_helmet", 1)
	if item == 0 {
		t.Fatal("the golden helmet could not be laid down")
	}

	servertest.AwaitTicks(t, srv, 40) // past the ten-tick delay a laid stack waits out
	looted := false
	pumpTicks(c, srv, 40, lootSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayPickupItem:
			r := protocol.NewReader(data)
			if got, _, _ := r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt(); got == item {
				looted = true
			}
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			for n := r.ReadVarInt(); n > 0; n-- {
				if r.ReadVarInt() == item {
					looted = true
				}
			}
		case protocol.IDPlayEntityEquipment:
			if entity, _ := equipmentOf(data); entity == zombie.EntityID {
				looted = true
			}
		}
		return false
	})
	if looted {
		t.Error("a pickup was told of while the mob griefing rule was off")
	}
	if !overworldHoldsItem(srv, item) {
		t.Error("the helmet left the ground with mob griefing off, want it lying")
	}
}

// TestTheLootPickerKeepsTheBetterPieceItWears stands summoned bodies with
// tagged equipment beside dropped loot and reads what they do with it: a
// better piece of armour is worn over the worse one, a better weapon
// replaces the held one, a worse piece stays lying where it dropped, and a
// skeleton holds the bow of its kind over any other weapon it might find.
func TestTheLootPickerKeepsTheBetterPieceItWears(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Fitter"))
	srv.SetDayTime(18000) // midnight, so the bare zombies burn nowhere near
	c := connect().Join("Fitter")

	// The cases: the kind summoned with its tag, where, what is dropped at
	// its feet, and — for a taking — the wire slot and item its Set
	// Equipment tells of. An empty want leaves the item to stay lying, the
	// piece the body already has being the better.
	cases := []struct {
		kind string
		tag  string
		x    float64
		item string
		want int32
		slot uint8
	}{
		{
			kind: "minecraft:zombie",
			tag:  `{CanPickUpLoot:1b,NoAI:1b,equipment:{head:{id:"minecraft:leather_helmet",count:1},mainhand:{id:"minecraft:iron_sword",count:1}}}`,
			x:    8.5, item: "minecraft:diamond_helmet",
			want: servertest.MustItemID(t, "minecraft:diamond_helmet"), slot: protocol.EquipHead,
		},
		{
			kind: "minecraft:zombie",
			tag:  `{CanPickUpLoot:1b,NoAI:1b,equipment:{mainhand:{id:"minecraft:golden_sword",count:1}}}`,
			x:    16.5, item: "minecraft:iron_sword",
			want: servertest.MustItemID(t, "minecraft:iron_sword"), slot: protocol.EquipMainHand,
		},
		{
			kind: "minecraft:zombie",
			tag:  `{CanPickUpLoot:1b,NoAI:1b,equipment:{head:{id:"minecraft:diamond_helmet",count:1},mainhand:{id:"minecraft:iron_sword",count:1}}}`,
			x:    24.5, item: "minecraft:iron_helmet",
		},
		{
			kind: "minecraft:skeleton",
			tag:  `{CanPickUpLoot:1b,NoAI:1b,equipment:{mainhand:{id:"minecraft:bow",count:1}}}`,
			x:    32.5, item: "minecraft:netherite_sword",
		},
	}

	for _, tc := range cases {
		c.ChatCommand(fmt.Sprintf("summon %s %g 64 8.5 %s", tc.kind, tc.x, tc.tag))
		mob := c.ExpectMobSpawn(entityTypeOf(tc.kind))
		item := srv.SpawnItemEntity(tc.x, 64.3, 8.5, tc.item, 1)
		if item == 0 {
			t.Fatalf("the %s could not be laid down", tc.item)
		}
		if tc.want == 0 {
			servertest.AwaitTicks(t, srv, 40) // past the ten-tick delay a laid stack waits out
			if !overworldHoldsItem(srv, item) {
				t.Fatalf("the %s was taken, want the piece the body already has kept", tc.item)
			}
			// The dropped stack's arrival and rest drain off the
			// connection, so the next case's summon reads a clean one
			// rather than a leftover item.
			c.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
			continue
		}
		waitForLootTaken(t, srv, c, mob.EntityID, item, tc.slot, tc.want)
	}
}

// entityTypeOf gives the numeric entity type a mob kind spawns as: the two
// kinds the loot tests summon, by the constants the spawn packets compare
// with.
func entityTypeOf(kind string) int32 {
	switch kind {
	case "minecraft:zombie":
		return protocol.EntityTypeZombie
	case "minecraft:skeleton":
		return protocol.EntityTypeSkeleton
	}
	return 0
}

// TestTheReplacedPieceIsDroppedOnTheGround summons a zombie in a guaranteed
// iron helmet — the drop_chances tag a pickup leaves — and drops a diamond
// helmet at its feet: the zombie wears the better piece and the iron helmet
// it displaced comes back to the ground rather than vanishing.
func TestTheReplacedPieceIsDroppedOnTheGround(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Swapper"))
	srv.SetDayTime(18000) // midnight, so the bare zombie burns nowhere near
	c := connect().Join("Swapper")

	c.ChatCommand(`summon minecraft:zombie 8.5 64 8.5 {CanPickUpLoot:1b,NoAI:1b,equipment:{head:{id:"minecraft:iron_helmet",count:1}},drop_chances:{head:2f}}`)
	zombie := c.ExpectMobSpawn(protocol.EntityTypeZombie)
	item := srv.SpawnItemEntity(8.5, 64.3, 8.5, "minecraft:diamond_helmet", 1)
	if item == 0 {
		t.Fatal("the diamond helmet could not be laid down")
	}
	waitForLootTaken(t, srv, c, zombie.EntityID, item, protocol.EquipHead, servertest.MustItemID(t, "minecraft:diamond_helmet"))
	servertest.UntilTicks(t, srv, 100, "condition", func() bool { return overworldHoldsItemNamed(srv, "minecraft:iron_helmet") })
}

// TestALootPickingMobPlaysTheEquipSoundOfWhatItWears has a zombie take an
// iron helmet, which sounds item.armor.equip_iron to everyone in the
// hostile category. A sword and a helmet taken into the hand are silent;
// only the sword posts unequip.
func TestALootPickingMobPlaysTheEquipSoundOfWhatItWears(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, item, equipment string
		slot                  uint8
		wantSound             bool
		wantEvent             string
	}{
		{"helmet", "minecraft:iron_helmet", "", protocol.EquipHead, true, "minecraft:equip"},
		{"sword", "minecraft:iron_sword", "", protocol.EquipMainHand, false, "minecraft:unequip"},
		{"helmet_in_hand", "minecraft:iron_helmet", `,equipment:{head:{id:"minecraft:diamond_helmet",count:1}}`, protocol.EquipMainHand, false, "minecraft:equip"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Looter", servertest.OpsListing(t, "Looter"))
			srv.SetDayTime(18000)
			c.ChatCommand(`summon minecraft:zombie 8.5 64 8.5 {CanPickUpLoot:1b,NoAI:1b` + tc.equipment + `}`)
			zombie := c.ExpectMobSpawn(protocol.EntityTypeZombie)
			c.Settle()
			servertest.HoldTicks(t, srv)
			events := &gameEvents{seen: map[string][][3]float64{}}
			srv.ObserveGameEvents(events.add)
			if srv.SpawnItemEntityDelay(8.5, 64.3, 8.5, tc.item, 1, 0) == 0 {
				t.Fatal("the item could not be laid down")
			}
			heard, equipped := false, false
			c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
				if id == protocol.IDPlayEntityEquipment {
					if mob, entries := equipmentOf(data); mob == zombie.EntityID {
						stack := servertest.EquipEntry(entries, tc.slot)
						equipped = equipped || stack.ItemID == servertest.MustItemID(t, tc.item) && stack.Count == 1
					}
				}
				if id != protocol.IDPlaySoundEffect {
					return false
				}
				name, category, x, y, z, volume, pitch := servertest.ReadAmbient(t, data)
				if !strings.HasPrefix(name, "minecraft:item.armor.equip_") {
					return false
				}
				heard = true
				if name != "minecraft:item.armor.equip_iron" || category != protocol.SoundCategoryHostile ||
					x != 68 || z != 68 || y < 512 || y > 520 || volume != 1 || pitch != 1 {
					t.Errorf("sound %s category %d at %d,%d,%d volume %v pitch %v", name, category, x, y, z, volume, pitch)
				}
				return false
			})
			if !equipped {
				t.Errorf("the picked-up %s was not shown in slot %d", tc.item, tc.slot)
			}
			if heard != tc.wantSound {
				t.Errorf("equip sound heard = %v, want %v", heard, tc.wantSound)
			}
			events.mu.Lock()
			defer events.mu.Unlock()
			for _, event := range []string{"minecraft:equip", "minecraft:unequip"} {
				want := 0
				if event == tc.wantEvent {
					want = 1
				}
				if got := len(events.seen[event]); got != want {
					t.Errorf("%s events = %d, want %d", event, got, want)
				}
			}
		})
	}
}
