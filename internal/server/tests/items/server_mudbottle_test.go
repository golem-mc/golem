package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// useBlockEffects right-clicks the block at (x, y, z) on the face given with
// whatever the actor holds, returning the sounds the actor heard by name and
// the number of Level Particles packets it was sent.
func useBlockEffects(t *testing.T, c *servertest.Client, x, y, z, face int32) (map[string]int, int) {
	t.Helper()
	c.PlaceBlockAt(x, y, z, face, 0.5)
	sounds := map[string]int{}
	particles := 0
	c.SettleAndCollect(func(id int32, data []byte) {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySoundEffect:
			if sid := r.ReadVarInt(); sid > 0 {
				if name, ok := servertest.SoundEventName(sid - 1); ok {
					r.ReadVarInt() // the sound category
					sounds[name]++
				}
			}
		case protocol.IDPlayLevelParticles:
			particles++
		}
	})
	return sounds, particles
}

// TestAWaterBottleTurnsDirtIntoMud: a water bottle used on dirt's top face
// leaves mud, gives back a glass bottle, and sends the splash, the bottle
// empty and the five splash particles.
// vanilla: PotionItem.useOn
func TestAWaterBottleTurnsDirtIntoMud(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Mudder")
	c.Settle()

	srv.SetBlock(3, 63, 0, registry.BlockStates["minecraft:dirt"])
	srv.GiveSlot("Mudder", player.HotbarSlot, servertest.WaterBottle(t))
	sounds, particles := useBlockEffects(t, c, 3, 63, 0, protocol.FaceTop)

	if got, want := srv.BlockAt(3, 63, 0), registry.BlockStates["minecraft:mud"]; got != want {
		t.Fatalf("the dirt after the water bottle = %d, want %d (mud)", got, want)
	}
	if got := srv.InventorySlot("Mudder", player.HotbarSlot); got.ItemID != servertest.MustItemID(t, "minecraft:glass_bottle") || got.Count != 1 {
		t.Errorf("the hand after the bottle = %+v, want one glass bottle", got)
	}
	if sounds["minecraft:entity.generic.splash"] != 1 {
		t.Errorf("the sounds heard = %v, want one entity.generic.splash", sounds)
	}
	if sounds["minecraft:item.bottle.empty"] != 1 {
		t.Errorf("the sounds heard = %v, want one item.bottle.empty", sounds)
	}
	if particles != 5 {
		t.Errorf("the splash particles = %d packets, want 5", particles)
	}
}

// TestAWaterBottleTurnsTheTaggedDirtsIntoMud: the other blocks of
// convertible_to_mud convert too, each with the bottle exchange.
// vanilla: PotionItem.useOn
func TestAWaterBottleTurnsTheTaggedDirtsIntoMud(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Mudder")
	c.Settle()

	for i, name := range []string{"minecraft:coarse_dirt", "minecraft:rooted_dirt"} {
		x := int32(3 + i)
		srv.SetBlock(x, 63, 0, registry.BlockStates[name])
		srv.GiveSlot("Mudder", player.HotbarSlot, servertest.WaterBottle(t))
		useBlockEffects(t, c, x, 63, 0, protocol.FaceTop)
		if got, want := srv.BlockAt(x, 63, 0), registry.BlockStates["minecraft:mud"]; got != want {
			t.Errorf("%s after the water bottle = %d, want %d (mud)", name, got, want)
		}
		if got := srv.InventorySlot("Mudder", player.HotbarSlot); got.ItemID != servertest.MustItemID(t, "minecraft:glass_bottle") {
			t.Errorf("%s left the hand %+v, want a glass bottle", name, got)
		}
	}
}

// TestAWaterBottleOnTheBottomFaceLeavesDirt: the bottom face converts
// nothing, so the dirt stays and the potion is kept. vanilla: PotionItem.useOn
func TestAWaterBottleOnTheBottomFaceLeavesDirt(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Mudder")
	c.Settle()

	dirt := registry.BlockStates["minecraft:dirt"]
	srv.SetBlock(3, 63, 0, dirt)
	srv.GiveSlot("Mudder", player.HotbarSlot, servertest.WaterBottle(t))
	sounds, particles := useBlockEffects(t, c, 3, 63, 0, protocol.FaceBottom)

	if got := srv.BlockAt(3, 63, 0); got != dirt {
		t.Errorf("the dirt clicked from below = %d, want %d (unchanged)", got, dirt)
	}
	if got := srv.InventorySlot("Mudder", player.HotbarSlot); got.ItemID != servertest.MustItemID(t, "minecraft:potion") {
		t.Errorf("the hand after a bottom-face click = %+v, want the potion kept", got)
	}
	if len(sounds) != 0 || particles != 0 {
		t.Errorf("a bottom-face click played %v and %d particle packets, want nothing", sounds, particles)
	}
}

// TestANonWaterPotionLeavesDirt: only the water brewing converts; a swiftness
// potion leaves the block and the stack alone. vanilla: PotionItem.useOn
func TestANonWaterPotionLeavesDirt(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Mudder")
	c.Settle()

	dirt := registry.BlockStates["minecraft:dirt"]
	srv.SetBlock(3, 63, 0, dirt)
	srv.GiveSlot("Mudder", player.HotbarSlot, servertest.SwiftnessPotion(t))
	sounds, particles := useBlockEffects(t, c, 3, 63, 0, protocol.FaceTop)

	if got := srv.BlockAt(3, 63, 0); got != dirt {
		t.Errorf("the dirt after a swiftness potion = %d, want %d (unchanged)", got, dirt)
	}
	if got := srv.InventorySlot("Mudder", player.HotbarSlot); got.ItemID != servertest.MustItemID(t, "minecraft:potion") {
		t.Errorf("the hand after a swiftness potion = %+v, want the potion kept", got)
	}
	if len(sounds) != 0 || particles != 0 {
		t.Errorf("a swiftness potion played %v and %d particle packets, want nothing", sounds, particles)
	}
}

// TestAWaterBottleOnASideFaceTurnsDirtIntoMud: only the bottom face refuses,
// so a click on any other face converts. vanilla: PotionItem.useOn
func TestAWaterBottleOnASideFaceTurnsDirtIntoMud(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Mudder")
	c.Settle()

	for i, face := range []int32{protocol.FaceNorth, protocol.FaceEast} {
		x := int32(3 + i)
		srv.SetBlock(x, 63, 0, registry.BlockStates["minecraft:dirt"])
		srv.GiveSlot("Mudder", player.HotbarSlot, servertest.WaterBottle(t))
		useBlockEffects(t, c, x, 63, 0, face)
		if got, want := srv.BlockAt(x, 63, 0), registry.BlockStates["minecraft:mud"]; got != want {
			t.Errorf("the dirt after a %d-face water bottle = %d, want %d (mud)", face, got, want)
		}
		if got := srv.InventorySlot("Mudder", player.HotbarSlot); got.ItemID != servertest.MustItemID(t, "minecraft:glass_bottle") {
			t.Errorf("the %d-face click left the hand %+v, want a glass bottle", face, got)
		}
	}
}

// TestASplashAndLingeringWaterPotionTurnDirtIntoMud: ThrowablePotionItem
// extends PotionItem and does not override useOn, so the thrown water potions
// convert on a block face too, each leaving its glass bottle.
// vanilla: PotionItem.useOn, ThrowablePotionItem
func TestASplashAndLingeringWaterPotionTurnDirtIntoMud(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Mudder")
	c.Settle()

	water := registry.MustPotion("minecraft:water")
	for i, item := range []string{"minecraft:splash_potion", "minecraft:lingering_potion"} {
		x := int32(3 + i)
		srv.SetBlock(x, 63, 0, registry.BlockStates["minecraft:dirt"])
		srv.GiveSlot("Mudder", player.HotbarSlot, servertest.PotionStack(t, item, water, 1))
		useBlockEffects(t, c, x, 63, 0, protocol.FaceTop)
		if got, want := srv.BlockAt(x, 63, 0), registry.BlockStates["minecraft:mud"]; got != want {
			t.Errorf("the dirt after a %s = %d, want %d (mud)", item, got, want)
		}
		if got := srv.InventorySlot("Mudder", player.HotbarSlot); got.ItemID != servertest.MustItemID(t, "minecraft:glass_bottle") {
			t.Errorf("the %s left the hand %+v, want a glass bottle", item, got)
		}
	}
}

// TestANonPotionItemWithWaterContentsLeavesDirt: only the potion family runs
// PotionItem.useOn, so an item that merely carries water potion_contents - a
// water tipped arrow, say - converts nothing and keeps its stack.
// vanilla: PotionItem.useOn
func TestANonPotionItemWithWaterContentsLeavesDirt(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Mudder")
	c.Settle()

	water := registry.MustPotion("minecraft:water")
	for i, item := range []string{"minecraft:tipped_arrow", "minecraft:stick"} {
		x := int32(3 + i)
		dirt := registry.BlockStates["minecraft:dirt"]
		srv.SetBlock(x, 63, 0, dirt)
		held := servertest.PotionStack(t, item, water, 1)
		srv.GiveSlot("Mudder", player.HotbarSlot, held)
		sounds, particles := useBlockEffects(t, c, x, 63, 0, protocol.FaceTop)

		if got := srv.BlockAt(x, 63, 0); got != dirt {
			t.Errorf("the dirt after a %s = %d, want %d (unchanged)", item, got, dirt)
		}
		if got := srv.InventorySlot("Mudder", player.HotbarSlot); !got.Equal(held) {
			t.Errorf("the hand after a %s = %+v, want the stack kept", item, got)
		}
		if len(sounds) != 0 || particles != 0 {
			t.Errorf("a %s played %v and %d particle packets, want nothing", item, sounds, particles)
		}
	}
}

// TestACreativeWaterBottleKeepsThePotion: creative keeps the potion and adds
// the glass bottle the survival exchange would swap in.
// vanilla: ItemUtils.createFilledResult, hasInfiniteMaterials
func TestACreativeWaterBottleKeepsThePotion(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BlockTestConfig(), "Builder")
	c.Settle()

	srv.SetBlock(3, 63, 0, registry.BlockStates["minecraft:dirt"])
	potion := servertest.WaterBottle(t)
	srv.GiveSlot("Builder", player.HotbarSlot, potion)
	useBlockEffects(t, c, 3, 63, 0, protocol.FaceTop)

	if got, want := srv.BlockAt(3, 63, 0), registry.BlockStates["minecraft:mud"]; got != want {
		t.Fatalf("the dirt after the creative bottle = %d, want %d (mud)", got, want)
	}
	if kept := srv.InventorySlot("Builder", player.HotbarSlot); !kept.Equal(potion) {
		t.Errorf("the creative hand = %+v, want the water bottle kept", kept)
	}
	bottles := 0
	for slot := player.MainSlot; slot <= player.OffHandSlot; slot++ {
		if srv.InventorySlot("Builder", slot).ItemID == servertest.MustItemID(t, "minecraft:glass_bottle") {
			bottles++
		}
	}
	if bottles != 1 {
		t.Errorf("the creative inventory holds %d glass bottles, want 1", bottles)
	}
}
