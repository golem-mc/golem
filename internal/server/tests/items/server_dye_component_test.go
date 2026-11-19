package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// A stack's own dye component decides the colour a dye item paints; removing
// it leaves the item unable to dye.
// vanilla: DyeItem.interactLivingEntity, DataComponents.DYE

func dyeStack(t *testing.T, item string, colour string) protocol.ItemStack {
	t.Helper()
	stack := servertest.ItemStack(t, item, 2)
	if colour == "" {
		if !stack.Remove(protocol.DataComponentDye) {
			t.Fatal("the dye component is not removable")
		}
		return stack
	}
	for i, name := range registry.DyeNames {
		if name == colour {
			stack.SetEnum(protocol.DataComponentDye, int32(i))
			return stack
		}
	}
	t.Fatal("no dye colour", colour)
	return stack
}

func TestADyeItemsOwnComponentColoursTheSheep(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "creative", "Painter", "", 0, servertest.OpsListing(t, "Painter"))
	srv.StopTick()
	c.ChatCommand("summon minecraft:sheep 1.5 64 0.5 {Color:0b}")
	sheep := c.ExpectMobSpawn(protocol.EntityTypeSheep)
	srv.GiveSlot("Painter", player.HotbarSlot, dyeStack(t, "minecraft:red_dye", "blue"))
	c.Settle()

	c.UseOn(sheep.EntityID)
	if got := awaitSheepColour(srv, c, sheep.EntityID); got != 0x0b {
		t.Errorf("red_dye[dye=blue] dyed the sheep %#02x, want blue's 0x0b", got)
	}
}

func TestADyeItemWithoutItsComponentDoesNotDyeTheSheep(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Painter", "", 0, servertest.OpsListing(t, "Painter"))
	srv.StopTick()
	c.ChatCommand("summon minecraft:sheep 1.5 64 0.5 {Color:0b}")
	sheep := c.ExpectMobSpawn(protocol.EntityTypeSheep)
	srv.GiveSlot("Painter", player.HotbarSlot, dyeStack(t, "minecraft:red_dye", ""))
	c.Settle()

	c.UseOn(sheep.EntityID)
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayEntityMetadata {
			t.Error("red_dye[!dye] recoloured the sheep")
		}
	})
	if got := srv.InventorySlot("Painter", player.HotbarSlot); got.Count != 2 {
		t.Errorf("red_dye[!dye] left %d in the hand, want the 2 kept", got.Count)
	}
}

func TestANonDyeItemCarryingTheComponentDoesNotDyeTheSheep(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Painter", "", 0, servertest.OpsListing(t, "Painter"))
	srv.StopTick()
	c.ChatCommand("summon minecraft:sheep 1.5 64 0.5 {Color:0b}")
	sheep := c.ExpectMobSpawn(protocol.EntityTypeSheep)
	stick := servertest.ItemStack(t, "minecraft:stick", 2)
	stick.SetEnum(protocol.DataComponentDye, 14)
	srv.GiveSlot("Painter", player.HotbarSlot, stick)
	c.Settle()

	c.UseOn(sheep.EntityID)
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayEntityMetadata {
			t.Error("stick[dye=red] recoloured the sheep")
		}
	})
}
