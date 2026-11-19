package entities

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// TestANameTagNamesAnArmorStandAndAMannequin right-clicks each body with a
// named name tag: the client gets the custom name in an Entity Metadata, the
// tag is used up and /data shows the name.
// vanilla: NameTagItem.interactLivingEntity
func TestANameTagNamesAnArmorStandAndAMannequin(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"armor_stand", "mannequin"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			cfg := mannequinConfig()
			cfg.GameMode = "survival"
			srv, c := servertest.Join(t, cfg, "Namer", servertest.OpsListing(t, "Namer"))
			servertest.HoldTicks(t, srv)

			c.ChatCommand("summon minecraft:" + kind + " 1.5 64 1.5")
			var id int32
			c.ReadTicks(srv, 5, func(pid int32, data []byte) bool {
				r := protocol.NewReader(data)
				if pid == protocol.IDPlaySpawnEntity {
					eid := r.ReadVarInt()
					r.ReadUUID()
					if typ := r.ReadVarInt(); typ == protocol.EntityTypeMannequin || typ == protocol.EntityTypeArmorStand {
						id = eid
						return true
					}
				}
				return false
			})
			if id == 0 {
				t.Fatalf("no %s spawned", kind)
			}

			slot := player.HotbarSlot
			srv.GiveSlot("Namer", slot, protocol.ItemStack{
				ItemID: servertest.MustItem("minecraft:name_tag"), Count: 1, CustomName: &protocol.Text{Text: "Bob"},
			})
			c.Settle()
			c.InteractAt(id, protocol.HandMain, 0.5, 0.5, 0.5)

			var name nbt.Tag
			c.ReadTicks(srv, 5, func(pid int32, data []byte) bool {
				if pid != protocol.IDPlayEntityMetadata {
					return false
				}
				r := protocol.NewReader(data)
				if r.ReadVarInt() != id {
					return false
				}
				meta, ok := servertest.ReadMetadata(r)
				if !ok {
					t.Fatal("unreadable metadata")
				}
				if e := servertest.MetaOf(meta, protocol.MetadataIndexCustomName); e != nil {
					name = e.TextVal
					return true
				}
				return false
			})
			if !reflect.DeepEqual(name, nbt.String("Bob")) {
				t.Errorf("custom name entry = %v, want Bob", name)
			}
			if got := srv.InventorySlot("Namer", slot); !got.Empty() {
				t.Errorf("the tag was not used up: %+v", got)
			}
		})
	}
}
