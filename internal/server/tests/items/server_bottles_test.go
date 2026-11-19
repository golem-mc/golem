package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// TestBottleFillsFromAWaterSource uses a glass bottle aimed down at a water
// source and checks the held stack becomes a water bottle while the source
// stays: a bottle fill takes no source, unlike a bucket.
// vanilla: BottleItem.use
func TestBottleFillsFromAWaterSource(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Filler")

	bottle := servertest.ItemIDOrDie(t, "minecraft:glass_bottle")
	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("the block registry has no water")
	}
	srv.GiveSlot("Filler", player.HotbarSlot, protocol.ItemStack{ItemID: bottle, Count: 1})
	if !srv.SetBlock(4, 63, 4, water) {
		t.Fatal("the world did not take the water")
	}
	c.Settle() // the water's own update crosses the wire before the fill reads it

	c.MoveStep(4.5, 64, 4.5, true)
	servertest.UseHeld(c, 0, 90)

	servertest.PollUntil(t, 5*time.Second, func() bool {
		return srv.InventorySlot("Filler", player.HotbarSlot).ItemID == registry.MustItem("minecraft:potion")
	})
	if got, want := srv.InventorySlot("Filler", player.HotbarSlot), servertest.WaterBottle(t); !got.Equal(want) {
		t.Errorf("the hand after the fill = %+v, want a water bottle %+v", got, want)
	}
	if got := srv.BlockAt(4, 63, 4); got != water {
		t.Errorf("the source after the fill = %d, want it still there", got)
	}
}

// TestABottleFillSpendsOneOfTheStack: a stack of sixteen bottles fills one
// and keeps fifteen in the hand, the water bottle landing in the next slot
// the way the filled result fills the inventory.
// vanilla: ItemUtils.createFilledResult
func TestABottleFillSpendsOneOfTheStack(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Filler")

	bottle := servertest.ItemIDOrDie(t, "minecraft:glass_bottle")
	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("the block registry has no water")
	}
	srv.GiveSlot("Filler", player.HotbarSlot, protocol.ItemStack{ItemID: bottle, Count: 16})
	if !srv.SetBlock(4, 63, 4, water) {
		t.Fatal("the world did not take the water")
	}
	c.Settle()

	c.MoveStep(4.5, 64, 4.5, true)
	servertest.UseHeld(c, 0, 90)

	servertest.PollUntil(t, 5*time.Second, func() bool {
		return srv.InventorySlot("Filler", player.HotbarSlot).Count == 15
	})
	if got := srv.InventorySlot("Filler", player.HotbarSlot); got.ItemID != bottle || got.Count != 15 {
		t.Errorf("the hand after the fill = %+v, want fifteen of the sixteen bottles", got)
	}
	if got, want := srv.InventorySlot("Filler", player.HotbarSlot+1), servertest.WaterBottle(t); !got.Equal(want) {
		t.Errorf("the filled result = %+v, want one water bottle %+v", got, want)
	}
}

// TestACreativeBottleFillKeepsTheBottle: a creative hand's fill leaves the
// bottle where it is and adds what the filled result asks for, the hand not
// being one of those. vanilla: ItemUtils.createFilledResult,
// hasInfiniteMaterials
func TestACreativeBottleFillKeepsTheBottle(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Dipper")

	bottle := servertest.ItemIDOrDie(t, "minecraft:glass_bottle")
	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("the block registry has no water")
	}
	srv.GiveSlot("Dipper", player.HotbarSlot, protocol.ItemStack{ItemID: bottle, Count: 1})
	if !srv.SetBlock(4, 63, 4, water) {
		t.Fatal("the world did not take the water")
	}
	c.Settle()

	c.MoveStep(4.5, 64, 4.5, true)
	servertest.UseHeld(c, 0, 90)

	servertest.PollUntil(t, 5*time.Second, func() bool {
		return srv.InventorySlot("Dipper", player.HotbarSlot+1).ItemID == registry.MustItem("minecraft:potion")
	})
	if got := srv.InventorySlot("Dipper", player.HotbarSlot); got.ItemID != bottle || got.Count != 1 {
		t.Errorf("the hand after the fill = %+v, want the bottle kept", got)
	}
	if got, want := srv.InventorySlot("Dipper", player.HotbarSlot+1), servertest.WaterBottle(t); !got.Equal(want) {
		t.Errorf("the filled result = %+v, want one water bottle %+v", got, want)
	}
}

// TestBottleFillsFromAWaterPlant fills a bottle from the blocks whose own
// cell holds a water source without a waterlogged property: a kelp plant, a
// seagrass and a bubble column all stop the ray and answer it with water the
// way a plain source does. A waterlogged slab fills too, its own waterlogged
// source being a source. vanilla: BottleItem.use,
// KelpPlantBlock.getFluidState, SeagrassBlock.getFluidState,
// BubbleColumnBlock.getFluidState
func TestBottleFillsFromAWaterPlant(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		what, name, props string
	}{
		{"a kelp plant", "minecraft:kelp_plant", ""},
		{"a seagrass", "minecraft:seagrass", ""},
		{"a bubble column", "minecraft:bubble_column", ""},
		{"a waterlogged oak slab", "minecraft:oak_slab", "type=bottom,waterlogged=true"},
	} {
		t.Run(tc.what, func(t *testing.T) {
			cfg := config.Default()
			cfg.OnlineMode = false

			srv, c := servertest.Join(t, cfg, "Filler")

			bottle := servertest.ItemIDOrDie(t, "minecraft:glass_bottle")
			srv.GiveSlot("Filler", player.HotbarSlot, protocol.ItemStack{ItemID: bottle, Count: 1})
			state := servertest.BlockState(t, tc.name)
			if tc.props != "" {
				state = servertest.BlockStateWith(t, tc.name, tc.props)
			}
			if !srv.SetBlock(4, 63, 4, state) {
				t.Fatalf("the world did not take %s", tc.what)
			}
			c.Settle()

			c.MoveStep(4.5, 64, 4.5, true)
			servertest.UseHeld(c, 0, 90)

			servertest.PollUntil(t, 5*time.Second, func() bool {
				return srv.InventorySlot("Filler", player.HotbarSlot).ItemID == registry.MustItem("minecraft:potion")
			})
			if got, want := srv.InventorySlot("Filler", player.HotbarSlot), servertest.WaterBottle(t); !got.Equal(want) {
				t.Errorf("the hand after filling from %s = %+v, want a water bottle %+v", tc.what, got, want)
			}
		})
	}
}

// TestACreativeBottleFillIgnoresAnotherPotion: a creative player already
// carrying a potion of another kind still gets the water bottle, because
// contains matches the item and its components, not the item alone.
// vanilla: Inventory.contains, ItemUtils.createFilledResult
func TestACreativeBottleFillIgnoresAnotherPotion(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Dipper")

	bottle := servertest.ItemIDOrDie(t, "minecraft:glass_bottle")
	awkward := servertest.PotionBottle(t, "minecraft:potion", "minecraft:awkward")
	srv.GiveSlot("Dipper", player.HotbarSlot, protocol.ItemStack{ItemID: bottle, Count: 1})
	srv.GiveSlot("Dipper", player.HotbarSlot+1, awkward)
	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("the block registry has no water")
	}
	if !srv.SetBlock(4, 63, 4, water) {
		t.Fatal("the world did not take the water")
	}
	c.Settle()

	c.MoveStep(4.5, 64, 4.5, true)
	servertest.UseHeld(c, 0, 90)

	servertest.PollUntil(t, 5*time.Second, func() bool {
		return srv.InventorySlot("Dipper", player.HotbarSlot+2).ItemID == registry.MustItem("minecraft:potion")
	})
	if got, want := srv.InventorySlot("Dipper", player.HotbarSlot+2), servertest.WaterBottle(t); !got.Equal(want) {
		t.Errorf("the filled result = %+v, want one water bottle %+v", got, want)
	}
	if got := srv.InventorySlot("Dipper", player.HotbarSlot); got.ItemID != bottle || got.Count != 1 {
		t.Errorf("the hand after the fill = %+v, want the bottle kept", got)
	}
	if got := srv.InventorySlot("Dipper", player.HotbarSlot+1); !got.Equal(awkward) {
		t.Errorf("the other potion = %+v, want the awkward potion %+v untouched", got, awkward)
	}
}

// TestABottleOnANonWaterTargetDoesNothing aims a bottle down at the stone
// under a player in a world with no water there: the ray stops on the stone
// and the fill asks for water, so the hand is left alone.
// vanilla: BottleItem.use
func TestABottleOnANonWaterTargetDoesNothing(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Filler")

	bottle := servertest.ItemIDOrDie(t, "minecraft:glass_bottle")
	srv.GiveSlot("Filler", player.HotbarSlot, protocol.ItemStack{ItemID: bottle, Count: 1})

	c.MoveStep(4.5, 64, 4.5, true)
	servertest.UseHeld(c, 0, 90)
	c.Settle()

	if got := srv.InventorySlot("Filler", player.HotbarSlot); got.ItemID != bottle || got.Count != 1 {
		t.Errorf("the hand after the fill = %+v, want the bottle untouched", got)
	}
	if got := srv.InventorySlot("Filler", player.HotbarSlot+1); !got.Empty() {
		t.Errorf("the filled result = %+v, want nothing", got)
	}
}

// TestABottleOnALavaSourceDoesNothing aims a bottle down at a lava source:
// the ray stops on the source the same way it stops on water, but the fill
// asks for water alone, so nothing happens. vanilla: BottleItem.use
func TestABottleOnALavaSourceDoesNothing(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Filler")

	bottle := servertest.ItemIDOrDie(t, "minecraft:glass_bottle")
	lava, ok := registry.FindState("minecraft:lava", nil)
	if !ok {
		t.Fatal("the block registry has no lava")
	}
	srv.GiveSlot("Filler", player.HotbarSlot, protocol.ItemStack{ItemID: bottle, Count: 1})
	if !srv.SetBlock(4, 63, 4, lava) {
		t.Fatal("the world did not take the lava")
	}
	c.Settle()

	c.MoveStep(4.5, 64, 4.5, true)
	servertest.UseHeld(c, 0, 90)
	c.Settle()

	if got := srv.InventorySlot("Filler", player.HotbarSlot); got.ItemID != bottle || got.Count != 1 {
		t.Errorf("the hand after the fill = %+v, want the bottle untouched", got)
	}
	if got := srv.InventorySlot("Filler", player.HotbarSlot+1); !got.Empty() {
		t.Errorf("the filled result = %+v, want nothing", got)
	}
}

// TestABottleFillPlaysTheFillSound has a watcher stand by while a bottle
// fills from water: the watcher hears the fill in the neutral category from
// the filler, and the filler hears none of it, their client predicting its
// own. vanilla: BottleItem.use, Entity.playSound
func TestABottleFillPlaysTheFillSound(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	filler := connect().Join("Filler")

	watcher := connect().Join("Watcher")
	watcher.Settle()

	bottle := servertest.ItemIDOrDie(t, "minecraft:glass_bottle")
	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("the block registry has no water")
	}
	srv.GiveSlot("Filler", player.HotbarSlot, protocol.ItemStack{ItemID: bottle, Count: 1})
	if !srv.SetBlock(4, 63, 4, water) {
		t.Fatal("the world did not take the water")
	}
	filler.Settle()

	filler.MoveStep(4.5, 64, 4.5, true)
	servertest.UseHeld(filler, 0, 90)

	x, y, z, _ := watcher.AwaitSoundNamed("minecraft:item.bottle.fill")
	pos := srv.PlayerPosition("Filler")
	if x != int32(pos.X*8) || y != int32(pos.Y*8) || z != int32(pos.Z*8) {
		t.Errorf("the fill sound came from (%d,%d,%d), want the filler's (%.1f,%.1f,%.1f)",
			x, y, z, pos.X, pos.Y, pos.Z)
	}

	heard := false
	filler.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySoundEffect && servertest.SoundOf(data) == "minecraft:item.bottle.fill" {
			heard = true
		}
	})
	if heard {
		t.Error("the filler heard the fill sound they predicted themselves")
	}
}

// TestASpectatorBottleFillDoesNothing: a spectator's use passes in vanilla
// before the item's own use runs, so the bottle is left alone.
// vanilla: ServerPlayerGameMode.useItem
func TestASpectatorBottleFillDoesNothing(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.GameMode = "spectator"
	srv, c := servertest.Join(t, cfg, "Ghost")

	bottle := servertest.ItemIDOrDie(t, "minecraft:glass_bottle")
	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("the block registry has no water")
	}
	srv.GiveSlot("Ghost", player.HotbarSlot, protocol.ItemStack{ItemID: bottle, Count: 1})
	if !srv.SetBlock(4, 63, 4, water) {
		t.Fatal("the world did not take the water")
	}
	c.Settle()

	c.MoveStep(4.5, 64, 4.5, true)
	servertest.UseHeld(c, 0, 90)
	c.Settle()

	if got := srv.InventorySlot("Ghost", player.HotbarSlot); got.ItemID != bottle || got.Count != 1 {
		t.Errorf("the hand after the spectator's fill = %+v, want the bottle untouched", got)
	}
	if got := srv.InventorySlot("Ghost", player.HotbarSlot+1); !got.Empty() {
		t.Errorf("the spectator's filled result = %+v, want nothing", got)
	}
}
