package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// tridentSpawnMetadata reads the next n ticks until a trident is spawned
// and returns the metadata its pairing told beside the spawn: the packet
// right after it where that is metadata about it, none otherwise.
func tridentSpawnMetadata(t *testing.T, c *servertest.Client, srv *server.Server, n int) []tridentMetadata {
	t.Helper()
	var id int32
	for gotID, data := range tridentPackets(c, srv, n) {
		if id != 0 {
			if gotID == protocol.IDPlayEntityMetadata {
				if m := readTridentMetadata(c, data); m.entityID == id {
					return []tridentMetadata{m}
				}
			}
			return nil
		}
		if gotID != protocol.IDPlaySpawnEntity {
			continue
		}
		r := protocol.NewReader(data)
		spawned := r.ReadVarInt()
		r.ReadUUID()
		if r.ReadVarInt() == protocol.EntityTypeTrident {
			id = spawned
		}
	}
	t.Fatalf("no trident came into the world within %d ticks", n)
	return nil
}

// TestAThrownTridentsGlintIsItsStacks throws two plain tridents, one shown
// a glint by its override alone and one with enchantments whose glint is
// hidden, and summons an enchanted one: the first spawns with the glint
// set, the second and the third with no metadata at all, since a summoned
// trident's glint is never set from its stack.
// vanilla: ThrownTrident constructors and readAdditionalSaveData,
// ItemStack.hasFoil, SynchedEntityData.getNonDefaultValues
func TestAThrownTridentsGlintIsItsStacks(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BowTestConfig(), servertest.OpsListing(t, "Thrower"))
	c := connect().Join("Thrower")
	trident := servertest.MustItem("minecraft:trident")
	impaling, ok := registry.EnchantmentID("minecraft:impaling")
	if !ok {
		t.Fatal("the enchantment registry has no impaling")
	}
	c.MoveTo(0.5, 64, 0.5, -90, 0)

	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: trident, Count: 1, GlintOverride: protocol.GlintShown})
	startTridentCharge(t, srv, c, "Thrower", -90, 0, 12)
	endTridentCharge(t, srv, c, "Thrower")
	metas := tridentSpawnMetadata(t, c, srv, 20)
	if len(metas) != 1 || !metas[0].foil {
		t.Errorf("the glint-override trident spawned with metadata %+v, want the glint set", metas)
	}

	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: trident, Count: 1,
		Enchantments: map[int32]int32{impaling: 2}, GlintOverride: protocol.GlintHidden})
	startTridentCharge(t, srv, c, "Thrower", -90, 0, 12)
	endTridentCharge(t, srv, c, "Thrower")
	if metas := tridentSpawnMetadata(t, c, srv, 20); len(metas) != 0 {
		t.Errorf("the hidden-glint trident spawned with metadata %+v, want none", metas)
	}

	c.ChatCommand(`summon minecraft:trident 0.5 70 6.5 {item:{id:"minecraft:trident",count:1,components:{"minecraft:enchantments":{"minecraft:impaling":2}}}}`)
	if metas := tridentSpawnMetadata(t, c, srv, 20); len(metas) != 0 {
		t.Errorf("the summoned enchanted trident spawned with metadata %+v, want none", metas)
	}
}
