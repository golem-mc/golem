package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

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
