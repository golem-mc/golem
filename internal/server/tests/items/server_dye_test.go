package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The dyeing of sheep, driven the way the shearing and the milking are —
// through a real connection. A right-click with a dye item in hand
// recolours a live, unsheared sheep whose wool is not already that colour,
// plays the dye sound to everyone but the clicker (whose own client plays
// it), and spends one dye outside creative, where interactOn hands the
// item a copy to spend and the original stays whole.
// vanilla: DyeItem.interactLivingEntity, Player.interactOn

// awaitSheepColour reads the wire until the sheep's own byte arrives and
// hands it back — the low nibble the colour, bit 0x10 the sheared flag —
// at the mob readers' own tolerance, the wool a shearing drops consumed
// with the spawn-and-stack pairings the shearing test reads them by.
func awaitSheepColour(srv *server.Server, c *servertest.Client, entityID int32) uint8 {
	c.T.Helper()
	var colour uint8
	if c.ReadTicks(srv, 200, func(packetID int32, data []byte) bool {
		switch packetID {
		case protocol.IDPlayEntityMetadata:
			meta := protocol.NewReader(data)
			if got := meta.ReadVarInt(); got != entityID {
				c.T.Fatalf("metadata for entity %d while the sheep's colour was waited for, want %d", got, entityID)
			}
			index := meta.ReadU8()
			fieldType := meta.ReadVarInt()
			if index != protocol.MetadataIndexSheepColour || fieldType != protocol.MetadataByte {
				c.T.Fatalf("the sheep's update carried field %d of type %d, want the colour byte", index, fieldType)
			}
			colour = meta.ReadU8()
			end := meta.ReadU8()
			if err := meta.Err(); err != nil {
				c.T.Fatalf("the sheep's colour metadata: %v", err)
			}
			if end != protocol.MetadataEnd {
				c.T.Fatalf("the sheep's colour byte arrived with %#02x behind it, want the array's end alone", end)
			}
			return true
		case protocol.IDPlaySpawnEntity:
			drop := servertest.ParseItemSpawn(data)
			if drop.EntityID == 0 {
				c.T.Fatalf("unexpected packet 0x%02X waiting for the sheep's colour", packetID)
			}
			c.ReadDropStack(&drop)
		case protocol.IDPlayEntitySoundEffect, protocol.IDPlaySoundEffect,
			protocol.IDPlayUpdateTime,
			protocol.IDPlayUpdateLight, protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity, protocol.IDPlayUnloadChunk,
			protocol.IDPlayEntityEquipment, protocol.IDPlayContainerSlot,
			protocol.IDPlayEntityEvent, protocol.IDPlaySetEntityMotion,
			protocol.IDPlayRemoveEntities:
			// Background: the shearing or dyeing sound, the clock, the
			// lighter hand.
		default:
			c.T.Fatalf("unexpected packet 0x%02X waiting for the sheep's colour", packetID)
		}
		return false
	}) {
		return colour
	}
	c.T.Fatalf("the sheep's colour byte never arrived within 10s")
	return 0
}

// TestDyeingASheepRecoloursItAndSpendsTheDye: a white sheep right-clicked
// with blue dye wears blue on the wire's own metadata, one dye of the hand
// is spent on it, and the dye sound reaches everyone but the clicker — an
// Entity Sound Effect naming the sheep, in the players category a colour's
// painting plays in, which the clicker's own client plays itself.
// A second click of the same blue on the blue sheep answers PASS: nothing
// recolours, nothing is spent.
func TestDyeingASheepRecoloursItAndSpendsTheDye(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Dyer"))
	dyer := connect().Join("Dyer")
	dyer.MoveToGround(2.5, 64, 5.5, true)
	dyer.Settle()
	// With the world still the sheep stands where it was planted, and what
	// the dyeing sends is the only thing left to arrive.
	srv.StopTick()

	dyer.ChatCommand("summon minecraft:sheep 5.5 64 5.5 {Color:0b}")
	sheep := dyer.ExpectMobSpawn(protocol.EntityTypeSheep)
	if sheep.HasSheepByte && sheep.SheepByte&0x0f != 0 {
		dyer.T.Fatalf("the summoned sheep wore %s, want the white its tag named", servertest.SheepDyeNameOf(sheep.SheepByte&0x0f))
	}

	blue, ok := registry.ItemID("minecraft:blue_dye")
	if !ok {
		t.Fatal("the item registry has no blue dye")
	}
	srv.GiveSlot("Dyer", player.HotbarSlot, protocol.ItemStack{ItemID: blue, Count: 3})
	dyer.Settle()

	// A watcher to hear the dyeing: the sound goes to everyone but the
	// player whose own client plays it.
	watcher := connect().Join("Watcher")
	watcher.Settle()
	dyer.Settle() // the watcher's arrival, out of the way of the dyeing

	dyer.UseOn(sheep.EntityID)
	if got := awaitSheepColour(srv, dyer, sheep.EntityID); got != 0x0b {
		dyer.T.Errorf("the dyed sheep's byte = %#02x, want blue's 0x0b", got)
	}
	// The settle also waits the spending out: the slot's count is the
	// handler's own last act.
	dyer.Settle()
	if got := srv.InventorySlot("Dyer", player.HotbarSlot); got.ItemID != blue || got.Count != 2 {
		dyer.T.Errorf("the dyer holds %d blue dye, want the 2 left of 3", got.Count)
	}

	// The clicker's queue was read past the dyeing whole: a dye sound in
	// it would be one the server sent the player whose client plays its
	// own copy.
	dyer.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayEntitySoundEffect && servertest.SoundOf(data) == "minecraft:item.dye.use" {
			dyer.T.Error("the clicker was sent its own dyeing's sound")
		}
	})

	heard := 0
	watcher.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayEntitySoundEffect || servertest.SoundOf(data) != "minecraft:item.dye.use" {
			return
		}
		heard++
		sound, category, entityID, volume, pitch := servertest.ReadEntitySound(watcher.T, data)
		if sound != "minecraft:item.dye.use" || category != protocol.SoundCategoryPlayer {
			watcher.T.Errorf("the dye sound = %q in category %d, want item.dye.use in the players one", sound, category)
		}
		if entityID != sheep.EntityID {
			watcher.T.Errorf("the dye sound named entity %d, want the sheep %d", entityID, sheep.EntityID)
		}
		if volume != 1 || pitch != 1 {
			watcher.T.Errorf("the dye sound at volume %v pitch %v, want vanilla's 1.0 and 1.0", volume, pitch)
		}
	})
	if heard != 1 {
		watcher.T.Errorf("the watcher heard %d dye sounds, want one", heard)
	}

	// The same blue on the blue sheep is no dyeing at all.
	dyer.UseOn(sheep.EntityID)
	dyer.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayEntityMetadata {
			dyer.T.Error("the same-colour dye recoloured the sheep, want no change")
		}
	})
	if got := srv.InventorySlot("Dyer", player.HotbarSlot); got.Count != 2 {
		dyer.T.Errorf("the same-colour dye left %d in the hand, want the 2 kept", got.Count)
	}
}

// TestACreativeDyeingKeepsTheDye: a creative hand's dyeing recolours all
// the same — interactOn hands the item a copy to spend, infinite
// materials keeping the original whole.
// vanilla: Player.interactOn's hasInfiniteMaterials
func TestACreativeDyeingKeepsTheDye(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "creative", "Painter", "", 0, servertest.OpsListing(t, "Painter"))
	srv.StopTick()

	c.ChatCommand("summon minecraft:sheep 1.5 64 0.5 {Color:0b}")
	sheep := c.ExpectMobSpawn(protocol.EntityTypeSheep)

	blue, ok := registry.ItemID("minecraft:blue_dye")
	if !ok {
		t.Fatal("the item registry has no blue dye")
	}
	srv.GiveSlot("Painter", player.HotbarSlot, protocol.ItemStack{ItemID: blue, Count: 1})
	c.Settle()

	c.UseOn(sheep.EntityID)
	if got := awaitSheepColour(srv, c, sheep.EntityID); got != 0x0b {
		c.T.Errorf("the creatively dyed sheep's byte = %#02x, want blue's 0x0b", got)
	}
	c.Settle()
	if got := srv.InventorySlot("Painter", player.HotbarSlot); got.ItemID != blue || got.Count != 1 {
		c.T.Errorf("the creative dyer holds %+v, want the one dye kept", got)
	}
}

// TestAShearedSheepTakesNoDye: the fleece off, a dye has nothing to paint —
// the sheep keeps its colour, the dye is kept and nothing goes out.
// vanilla: DyeItem.interactLivingEntity's !isSheared
func TestAShearedSheepTakesNoDye(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Shearer")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	shears, ok := registry.ItemID("minecraft:shears")
	if !ok {
		t.Fatal("the item registry has no shears")
	}
	srv.GiveSlot("Shearer", player.HotbarSlot, protocol.ItemStack{ItemID: shears, Count: 1})
	srv.SpawnMob("minecraft:sheep", 5.5, 64, 5.5)
	sheep := c.ExpectMobSpawn(protocol.EntityTypeSheep)

	c.UseOn(sheep.EntityID)
	if got := awaitSheepColour(srv, c, sheep.EntityID); got&0x10 == 0 {
		c.T.Fatalf("the shearing left byte %#02x, want the sheared bit on it", got)
	}
	c.Settle()

	red, ok := registry.ItemID("minecraft:red_dye")
	if !ok {
		t.Fatal("the item registry has no red dye")
	}
	srv.GiveSlot("Shearer", player.HotbarSlot, protocol.ItemStack{ItemID: red, Count: 2})
	c.Settle()
	c.UseOn(sheep.EntityID)
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayEntityMetadata {
			c.T.Error("a sheared sheep was recoloured, want no dyeing of it")
		}
	})
	if got := srv.InventorySlot("Shearer", player.HotbarSlot); got.ItemID != red || got.Count != 2 {
		c.T.Errorf("the dye on the sheared sheep left %d red, want the 2 kept", got.Count)
	}
}

// TestAnAdventureHandDyesTheSheep: an adventure hand's right-click takes
// the same gate the survival one does — the colour changes and the dye is
// spent. Player.interactOn has no adventure refusal for a dye item.
func TestAnAdventureHandDyesTheSheep(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "adventure", "Dyer", "", 0, servertest.OpsListing(t, "Dyer"))
	srv.StopTick()

	c.ChatCommand("summon minecraft:sheep 1.5 64 0.5 {Color:0b}")
	sheep := c.ExpectMobSpawn(protocol.EntityTypeSheep)

	blue, ok := registry.ItemID("minecraft:blue_dye")
	if !ok {
		t.Fatal("the item registry has no blue dye")
	}
	srv.GiveSlot("Dyer", player.HotbarSlot, protocol.ItemStack{ItemID: blue, Count: 2})
	c.Settle()

	c.UseOn(sheep.EntityID)
	if got := awaitSheepColour(srv, c, sheep.EntityID); got != 0x0b {
		c.T.Errorf("the adventure dyer's sheep = %#02x, want blue's 0x0b", got)
	}
	c.Settle()
	if got := srv.InventorySlot("Dyer", player.HotbarSlot); got.ItemID != blue || got.Count != 1 {
		c.T.Errorf("the adventure dyer holds %+v, want the 1 left of 2", got)
	}
}

// TestTheOffHandDyesTheSheep: the off hand's dye recolours the sheep and is
// spent from the off hand, the hand the click named.
func TestTheOffHandDyesTheSheep(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Dyer", "", 0, servertest.OpsListing(t, "Dyer"))
	srv.StopTick()

	c.ChatCommand("summon minecraft:sheep 1.5 64 0.5 {Color:0b}")
	sheep := c.ExpectMobSpawn(protocol.EntityTypeSheep)

	blue, ok := registry.ItemID("minecraft:blue_dye")
	if !ok {
		t.Fatal("the item registry has no blue dye")
	}
	srv.GiveSlot("Dyer", player.OffHandSlot, protocol.ItemStack{ItemID: blue, Count: 2})
	c.Settle()

	c.InteractAt(sheep.EntityID, protocol.HandOff, 0.1, 0.9, 0.2)
	if got := awaitSheepColour(srv, c, sheep.EntityID); got != 0x0b {
		c.T.Errorf("the off-hand dyer's sheep = %#02x, want blue's 0x0b", got)
	}
	c.Settle()
	if got := srv.InventorySlot("Dyer", player.OffHandSlot); got.ItemID != blue || got.Count != 1 {
		c.T.Errorf("the off-hand dyer holds %+v, want the 1 left of 2", got)
	}
}

// TestADeadSheepTakesNoDye: a body lying in its death animation is no live
// sheep, so the dye does not take, is not spent and nothing goes out.
// vanilla: DyeItem.interactLivingEntity's isAlive
func TestADeadSheepTakesNoDye(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Dyer", "", 0, servertest.OpsListing(t, "Dyer"))
	srv.StopTick()

	c.ChatCommand("summon minecraft:sheep 1.5 64 0.5 {Color:0b}")
	sheep := c.ExpectMobSpawn(protocol.EntityTypeSheep)

	red, ok := registry.ItemID("minecraft:red_dye")
	if !ok {
		t.Fatal("the item registry has no red dye")
	}
	srv.GiveSlot("Dyer", player.HotbarSlot, protocol.ItemStack{ItemID: red, Count: 2})
	c.Settle()

	if !srv.KillMob(sheep.EntityID) {
		t.Fatal("no live sheep answered the kill")
	}
	c.Settle()

	c.UseOn(sheep.EntityID)
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayEntityMetadata {
			c.T.Error("a dead sheep was recoloured, want no dyeing of it")
		}
	})
	if got := srv.InventorySlot("Dyer", player.HotbarSlot); got.ItemID != red || got.Count != 2 {
		c.T.Errorf("the dye on the dead sheep left %d red, want the 2 kept", got.Count)
	}
}
