package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The mob equipment tests: what a spawn stands its zombies up wearing —
// Mob.populateDefaultEquipmentSlots' armour roll by the difficulty and
// Zombie's own iron sword, spear or shovel — the wire it arrives shown by,
// the blow a chestplate turns, and the sun a helmet keeps off a zombie's
// head. Where a test needs a body wearing a known piece it summons one
// through the equipment tag; the armour the spawn rolled itself is read off
// the bodies it plants and the packets they arrive with.

// fullMoonMidnight is the clock that both keeps a spawn's difficulty at its
// largest and the sun out of the world: day sixty-four of the world's clock
// — a full moon, whose brightness shares its whole quarter out — at its
// midnight, the hours no zombie burns in.
const fullMoonMidnight = 64*24000 + 18000

// spawnGroup reads one mob's spawn grouping as the wire carries it — the
// body, its state, whatever it arrives holding and wearing, and its pace —
// answering the entity ID and the Set Equipment entries, nil for a bare
// spawn the pairing named no equipment packet for.
func spawnGroup(c *servertest.Client, wantType int32) (int32, []servertest.EquipmentEntry) {
	c.T.Helper()
	spawn := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlaySpawnEntity))
	id := spawn.ReadVarInt()
	spawn.ReadUUID()
	if got := spawn.ReadVarInt(); got != wantType {
		c.T.Errorf("spawned entity type = %d, want %d", got, wantType)
	}
	if err := spawn.Err(); err != nil {
		c.T.Fatalf("spawn entity: %v", err)
	}
	c.ExpectMobPacket(protocol.IDPlayEntityMetadata)

	// The equipment a body arrives with stands between its state and its
	// pace, and only a body with something on arrives with one; the pace
	// packet ends the grouping. Whatever else a running loop tells about
	// any body between them is stepped past the way a client does.
	var entries []servertest.EquipmentEntry
	for gotID, data := range c.Stream() {
		c.T.Helper()
		switch gotID {
		case protocol.IDPlayEntityEquipment:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != id {
				c.T.Fatalf("equipment for entity %d while reading the spawn of %d", got, id)
			}
			for {
				raw := r.ReadU8()
				stack, _ := r.ReadSlot()
				entries = append(entries, servertest.EquipmentEntry{Slot: raw & 0x7F, Stack: stack})
				if raw&0x80 == 0 {
					break
				}
			}
			if err := r.Err(); err != nil {
				c.T.Fatalf("spawn equipment: %v", err)
			}
		case protocol.IDPlayUpdateAttributes:
			if got := protocol.NewReader(data).ReadVarInt(); got != id {
				c.T.Fatalf("attributes for entity %d while reading the spawn of %d", got, id)
			}
			return id, entries
		}
		if servertest.HostileSkip(gotID) {
			continue
		}
		c.T.Fatalf("unexpected packet 0x%02X in the spawn grouping of %d", gotID, id)
	}
	c.T.Fatalf("the spawn grouping of %d never finished", id)
	return 0, nil
}

// helmetItemIDs is the six helmets Mob.getEquipmentForSlot hands the spawn's
// material roll: leather, copper, golden, chainmail, iron, diamond.
func helmetItemIDs(t *testing.T) map[int32]bool {
	t.Helper()
	helmets := map[int32]bool{}
	for _, name := range []string{
		"minecraft:leather_helmet", "minecraft:copper_helmet", "minecraft:golden_helmet",
		"minecraft:chainmail_helmet", "minecraft:iron_helmet", "minecraft:diamond_helmet",
	} {
		helmets[servertest.MustItemID(t, name)] = true
	}
	return helmets
}

// zombieToolItemIDs is the three iron tools Zombie's own tool roll arms a
// zombie with: the sword, the spear and the shovel.
func zombieToolItemIDs(t *testing.T) map[int32]bool {
	t.Helper()
	tools := map[int32]bool{}
	for _, name := range []string{"minecraft:iron_sword", "minecraft:iron_spear", "minecraft:iron_shovel"} {
		tools[servertest.MustItemID(t, name)] = true
	}
	return tools
}

// slotsAbove names the armour slots the wire numbers above the one given.
func slotsAbove(slot uint8) []uint8 {
	switch slot {
	case protocol.EquipFeet:
		return []uint8{protocol.EquipLegs, protocol.EquipChest, protocol.EquipHead}
	case protocol.EquipLegs:
		return []uint8{protocol.EquipChest, protocol.EquipHead}
	case protocol.EquipChest:
		return []uint8{protocol.EquipHead}
	}
	return nil
}

// TestSpawnedZombiesArriveWearingArmour checks the spawn's own roll over the
// wire: zombies stood up where the difficulty is hard and the moon is full
// sometimes arrive wearing a helmet of one of the six materials — always the
// head first, no piece standing without the ones above it — and sometimes
// holding nothing but an iron sword, spear or shovel. The seeds the sixty
// spawns mint are the same every run, so the share is a fixture, not a
// chance.
func TestSpawnedZombiesArriveWearingArmour(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	srv, connect := servertest.Start(t, cfg)
	srv.SetDifficulty(rules.DifficultyHard)
	srv.SetDayTime(fullMoonMidnight)
	c := connect().Join("Quartermaster")
	c.Settle()

	// The loop stands stopped while the spawns are read, so no tick's
	// telling about an earlier body lands between the packets of a later
	// one's grouping.
	srv.StopTick()

	helmets := helmetItemIDs(t)
	tools := zombieToolItemIDs(t)
	const spawns = 60
	var armoured, tooled int
	for i := range spawns {
		srv.SpawnMob("minecraft:zombie", 4.5+float64(i%10), 64, 8.5+float64(i/10))
		id, entries := spawnGroup(c, protocol.EntityTypeZombie)
		if id == 0 {
			t.Fatal("a spawned zombie carries no entity ID")
		}
		if head := servertest.EquipEntry(entries, protocol.EquipHead); !head.Empty() {
			if !helmets[head.ItemID] {
				name, _ := registry.ItemName(head.ItemID)
				t.Errorf("a spawned zombie wears %s on its head, want one of the six helmets", name)
			}
			armoured++
			// The roll dresses the body head down, so no piece it
			// puts on stands without its head.
			for _, slot := range []uint8{protocol.EquipFeet, protocol.EquipLegs, protocol.EquipChest} {
				if piece := servertest.EquipEntry(entries, slot); !piece.Empty() {
					for _, above := range slotsAbove(slot) {
						if servertest.EquipEntry(entries, above).Empty() {
							t.Errorf("a spawned zombie wears a piece at slot %d with the slot %d above it bare", slot, above)
						}
					}
				}
			}
		} else {
			for _, piece := range entries {
				if piece.Slot != protocol.EquipMainHand {
					t.Errorf("a spawned zombie with no helmet carries slot %d, want no armour below a bare head", piece.Slot)
				}
			}
		}
		if held := servertest.EquipEntry(entries, protocol.EquipMainHand); !held.Empty() {
			if !tools[held.ItemID] {
				name, _ := registry.ItemName(held.ItemID)
				t.Errorf("a spawned zombie holds %s, want the iron sword, spear or shovel", name)
			}
			tooled++
		}
	}
	if armoured == 0 {
		t.Errorf("none of %d spawned zombies wore armour, want some dressed by the hard moonlit roll", spawns)
	}
	if tooled == 0 {
		t.Errorf("none of %d spawned zombies held an iron tool, want some armed by the hard roll", spawns)
	}
}
