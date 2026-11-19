package entities

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// mannequinEquipment reads the next equipment update for the mannequin.
func mannequinEquipment(t *testing.T, c *servertest.Client, srv *server.Server, entityID int32) []servertest.EquipmentEntry {
	t.Helper()
	var entries []servertest.EquipmentEntry
	if !c.ReadTicks(srv, 3, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityEquipment {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != entityID {
			return false
		}
		for {
			raw := r.ReadU8()
			stack, _ := r.ReadSlot()
			entries = append(entries, servertest.EquipmentEntry{Slot: raw & 0x7f, Stack: stack})
			if raw&0x80 == 0 {
				break
			}
		}
		if r.Err() != nil || r.Remaining() != 0 {
			t.Fatalf("equipment: %v, remaining %d", r.Err(), r.Remaining())
		}
		return true
	}) {
		t.Fatal("mannequin received no equipment packet")
	}
	return entries
}

func TestAMannequinCarriesLivingTags(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "GuideWatcher")
	c.ChatCommand(`summon mannequin 1.5 64 1.5 {profile:"Notch",equipment:{mainhand:{id:"diamond_sword"},head:{id:"diamond_helmet"}},CustomName:"Guide",CustomNameVisible:1b,Glowing:1b}`)
	spawn := awaitMannequin(t, c, srv)
	if got := metaOfMannequin(t, spawn.meta, protocol.MetadataIndexEntityFlags).ByteVal; got != 0x40 {
		t.Errorf("shared flags = %#x, want 0x40", got)
	}
	if got := metaOfMannequin(t, spawn.meta, protocol.MetadataIndexCustomName).TextVal; got != nbt.String("Guide") {
		t.Errorf("name = %v, want Guide", got)
	}
	if !metaOfMannequin(t, spawn.meta, protocol.MetadataIndexCustomNameVisible).FlagVal {
		t.Error("name is not visible")
	}
	equipped := mannequinEquipment(t, c, srv, spawn.id)
	if len(equipped) != 2 {
		t.Errorf("spawn equipment entries = %d, want 2", len(equipped))
	}
	for slot, item := range map[uint8]registry.Item{0: registry.ItemDiamondSword, 5: registry.ItemDiamondHelmet} {
		if got := servertest.EquipEntry(equipped, slot); got.ItemID != int32(item) || got.Count != 1 {
			t.Errorf("slot %d = %+v, want %v", slot, got, item)
		}
	}
	for path, want := range map[string]string{"equipment.mainhand.id": "minecraft:diamond_sword", "equipment.head.id": "minecraft:diamond_helmet", "CustomName": "Guide", "CustomNameVisible": "1b", "Glowing": "1b"} {
		if got := dataOfMannequin(t, c, srv, path); !strings.Contains(got, want) {
			t.Errorf("saved %s = %s, want %s", path, got, want)
		}
	}
	c.ChatCommand(`data merge entity @e[type=mannequin,limit=1] {equipment:{head:{id:"golden_helmet"}},CustomName:"Guard",HasVisualFire:1b}`)
	var meta []servertest.MetaEntry
	c.ReadTicks(srv, 3, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		if id != protocol.IDPlayEntityMetadata || r.ReadVarInt() != spawn.id {
			return false
		}
		var ok bool
		meta, ok = servertest.ReadMetadata(r)
		if !ok {
			t.Fatal("invalid metadata")
		}
		return true
	})
	if got := metaOfMannequin(t, meta, protocol.MetadataIndexEntityFlags).ByteVal; got != 0x41 {
		t.Errorf("visual-fire flags = %#x, want 0x41", got)
	}
	if got := metaOfMannequin(t, meta, protocol.MetadataIndexCustomName).TextVal; got != nbt.String("Guard") {
		t.Errorf("merged name = %v", got)
	}
	equipped = mannequinEquipment(t, c, srv, spawn.id)
	if len(equipped) != 1 || servertest.EquipEntry(equipped, 5).ItemID != int32(registry.ItemGoldenHelmet) {
		t.Errorf("merged equipment = %+v", equipped)
	}

	if got := dataOfMannequin(t, c, srv, "equipment.mainhand.id"); !strings.Contains(got, "minecraft:diamond_sword") {
		t.Errorf("merge changed main hand: %s", got)
	}
	c.ChatCommand(`data remove entity @e[type=mannequin,limit=1] equipment.mainhand`)
	if got := servertest.EquipEntry(mannequinEquipment(t, c, srv, spawn.id), 0); !got.Empty() {
		t.Errorf("removed main hand = %+v", got)
	}
	if got := dataOfMannequin(t, c, srv, "HasVisualFire"); got != "1b" {
		t.Errorf("saved visual fire = %s", got)
	}
	c.ChatCommand(`data merge entity @e[type=mannequin,limit=1] {Glowing:0b,HasVisualFire:0b,CustomNameVisible:0b}`)
	c.ReadTicks(srv, 3, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		if id != protocol.IDPlayEntityMetadata || r.ReadVarInt() != spawn.id {
			return false
		}
		meta, _ = servertest.ReadMetadata(r)
		return true
	})
	if metaOfMannequin(t, meta, protocol.MetadataIndexEntityFlags).ByteVal != 0 || metaOfMannequin(t, meta, protocol.MetadataIndexCustomNameVisible).FlagVal {
		t.Error("metadata did not reset flags and name visibility")
	}
}
