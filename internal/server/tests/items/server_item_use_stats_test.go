package items

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func expectUsed(t *testing.T, srv *server.Server, item string, want int32) {
	t.Helper()
	if got := itemStat(srv, registry.StatUsed, item); got != want {
		t.Errorf("used %s = %d, want %d", item, got, want)
	}
}

// underFeet is the cell the actor stands on.
func underFeet(srv *server.Server) (x, y, z int32) {
	pos := srv.PlayerPosition("Actor")
	return int32(math.Floor(pos.X)), int32(math.Floor(pos.Y)) - 1, int32(math.Floor(pos.Z))
}

func giveStack(t *testing.T, srv *server.Server, slot int, item string, n int32) {
	t.Helper()
	srv.GiveSlot("Actor", slot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, item), Count: n})
}

// aimedAtWater is a scene whose actor holds item and uses it at a pool of
// water a block ahead, looking down at it.
func aimedAtWater(t *testing.T, item string) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, actor, _ := servertest.ToolScene(t, item)
	// Away from the watcher, whose body a boat's eye check would hit.
	actor.MoveTo(8.5, 64, 0.5, 0, 45)
	actor.Settle()
	x, y, z := underFeet(srv)
	// A pool wide enough for a boat's hull to float in.
	for dx := int32(-2); dx <= 2; dx++ {
		for dz := int32(0); dz <= 4; dz++ {
			srv.SetBlock(x+dx, y, z+dz, registry.MustState("minecraft:water", nil))
		}
	}
	actor.Settle()
	// Aimed ahead and down so the hull floats clear of the actor's own box.
	servertest.UseHeld(actor, 0, 45)
	actor.Settle()
	return srv, actor
}

// TestScoopingWithABucketCountsAsUsed: vanilla: BucketItem.use
func TestScoopingWithABucketCountsAsUsed(t *testing.T) {
	t.Parallel()
	srv, _ := aimedAtWater(t, "minecraft:bucket")
	expectUsed(t, srv, "minecraft:bucket", 1)
}

// TestPouringABucketCountsAsUsedForTheFilledBucket: vanilla: BucketItem.use
func TestPouringABucketCountsAsUsedForTheFilledBucket(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:water_bucket")
	servertest.UseHeld(actor, 0, 90)
	actor.Settle()
	expectUsed(t, srv, "minecraft:water_bucket", 1)
}

// TestFillingAGlassBottleCountsAsUsed: vanilla: BottleItem.turnBottleIntoItem
func TestFillingAGlassBottleCountsAsUsed(t *testing.T) {
	t.Parallel()
	srv, _ := aimedAtWater(t, "minecraft:glass_bottle")
	expectUsed(t, srv, "minecraft:glass_bottle", 1)
}

// TestAFlintAndSteelLightCountsAsUsed: vanilla: ItemStack.useOn
func TestAFlintAndSteelLightCountsAsUsed(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:flint_and_steel")
	actor.UseBlock(3, 63, 0)
	actor.Settle()
	expectUsed(t, srv, "minecraft:flint_and_steel", 1)
}

// TestTillingWithAHoeCountsAsUsed: vanilla: ItemStack.useOn
func TestTillingWithAHoeCountsAsUsed(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:iron_hoe")
	srv.SetBlock(3, 63, 0, registry.MustState("minecraft:dirt", nil))
	actor.Settle()
	actor.UseBlock(3, 63, 0)
	actor.Settle()
	expectUsed(t, srv, "minecraft:iron_hoe", 1)
}

// TestCarvingAPumpkinCountsShearsAsUsed: vanilla: PumpkinBlock.useItemOn
func TestCarvingAPumpkinCountsShearsAsUsed(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:shears")
	srv.SetBlock(3, 63, 0, registry.MustState("minecraft:pumpkin", nil))
	actor.Settle()
	actor.UseBlock(3, 63, 0)
	actor.Settle()
	expectUsed(t, srv, "minecraft:shears", 1)
}

// TestAddingACandleToACakeCountsTheCandleAsUsed: vanilla: CakeBlock.useItemOn
func TestAddingACandleToACakeCountsTheCandleAsUsed(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:candle")
	srv.SetBlock(3, 63, 0, registry.MustState("minecraft:cake", nil))
	actor.Settle()
	actor.UseBlock(3, 63, 0)
	actor.Settle()
	expectUsed(t, srv, "minecraft:candle", 1)
}

// TestFeedingAComposterCountsTheItemAsUsed: vanilla: ComposterBlock.useItemOn
func TestFeedingAComposterCountsTheItemAsUsed(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:wheat_seeds")
	srv.SetBlock(3, 63, 0, registry.MustState("minecraft:composter", nil))
	actor.Settle()
	actor.UseBlock(3, 63, 0)
	actor.Settle()
	expectUsed(t, srv, "minecraft:wheat_seeds", 1)
}

// TestWaxingASignCountsTheHoneycombAsUsed: vanilla: SignBlock.useItemOn, HoneycombItem.tryApplyToSign
func TestWaxingASignCountsTheHoneycombAsUsed(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:honeycomb")
	srv.SetBlock(3, 63, 0, registry.MustState("minecraft:oak_sign", nil))
	actor.Settle()
	actor.UseBlock(3, 63, 0)
	actor.Settle()
	expectUsed(t, srv, "minecraft:honeycomb", 1)
}

// TestFillingACauldronWithABucketCountsAsUsed: vanilla: CauldronInteractions.emptyBucket
func TestFillingACauldronWithABucketCountsAsUsed(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:water_bucket")
	srv.SetBlock(3, 63, 0, registry.MustState("minecraft:cauldron", nil))
	actor.Settle()
	actor.UseBlock(3, 63, 0)
	actor.Settle()
	expectUsed(t, srv, "minecraft:water_bucket", 1)
}

// TestAShotBowCountsAsUsed: vanilla: BowItem.releaseUsing
func TestAShotBowCountsAsUsed(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:bow")
	giveStack(t, srv, player.HotbarSlot+1, "minecraft:arrow", 5)
	actor.Settle()

	servertest.DrawBow(actor, -90, 0)
	actor.Settle()
	for i := 0; srv.BowTicks("Actor") < 15; i++ {
		if i == 40 {
			t.Fatalf("40 ticks drew the bow %d ticks, want 15", srv.BowTicks("Actor"))
		}
		srv.TickSurvival()
	}
	servertest.ReleaseBow(actor)
	actor.Settle()

	expectUsed(t, srv, "minecraft:bow", 1)
}

// TestAnEatenMealCountsAsUsed: vanilla: Consumable.onConsume
func TestAnEatenMealCountsAsUsed(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:bread")
	srv.SetFood("Actor", 10)
	actor.UseItemRightClicks(protocol.HandMain)
	actor.Settle()
	for range 40 {
		srv.TickSurvival()
	}
	actor.Settle()
	expectUsed(t, srv, "minecraft:bread", 1)
}

// TestASpentTotemCountsAsUsed: vanilla: LivingEntity.checkTotemDeathProtection
func TestASpentTotemCountsAsUsed(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:totem_of_undying")
	if !srv.Hurt("Actor", 40) {
		t.Fatal("the blow did not land")
	}
	actor.Settle()
	expectUsed(t, srv, "minecraft:totem_of_undying", 1)
}

// mineStoneWith has a survival actor break stone with the stack and returns
// the server once the cell is air.
func mineStoneWith(t *testing.T, stack protocol.ItemStack) *server.Server {
	t.Helper()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:stone")
	srv.GiveSlot("Actor", player.HotbarSlot, stack)
	srv.SetBlock(3, 64, 0, registry.MustState("minecraft:stone", nil))
	actor.Settle()

	actor.BreakBlock(3, 64, 0)
	actor.Settle()
	for range 200 {
		if srv.BlockAt(3, 64, 0) == registry.MustState("minecraft:air", nil) {
			break
		}
		srv.RunTick()
	}
	actor.Settle()
	if srv.BlockAt(3, 64, 0) != registry.MustState("minecraft:air", nil) {
		t.Fatal("the stone was never broken")
	}
	return srv
}

// TestMiningABlockCountsTheToolAsUsed: vanilla: ItemStack.mineBlock
func TestMiningABlockCountsTheToolAsUsed(t *testing.T) {
	t.Parallel()
	pick := servertest.EnchantedStack(t, "minecraft:netherite_pickaxe", "minecraft:efficiency", 5)
	expectUsed(t, mineStoneWith(t, pick), "minecraft:netherite_pickaxe", 1)
}

// TestAToolWithNoDamagePerBlockCountsAsUsedWhenMining: Item.mineBlock answers
// true for any tool component, whatever it costs.
// vanilla: Item.mineBlock, ItemStack.mineBlock
func TestAToolWithNoDamagePerBlockCountsAsUsedWhenMining(t *testing.T) {
	t.Parallel()
	pick := servertest.ItemStack(t, "minecraft:netherite_pickaxe", 1)
	pick.SetTool(protocol.ToolComponent{DefaultMiningSpeed: 10000, DamagePerBlock: 0})
	expectUsed(t, mineStoneWith(t, pick), "minecraft:netherite_pickaxe", 1)
}

// TestACreativeBreakDoesNotCountTheToolAsUsed: destroyBlock returns before
// mineBlock when the player's mode prevents drops.
// vanilla: ServerPlayerGameMode.destroyBlock
func TestACreativeBreakDoesNotCountTheToolAsUsed(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")
	c.MoveToGround(4.5, 64, 4.5, true)
	c.Settle()
	if srv.PlayerGameMode("Actor") != protocol.GameModeCreative {
		t.Fatal("the scene's player is not in creative")
	}
	srv.GiveSlot("Actor", player.HotbarSlot, servertest.ItemStack(t, "minecraft:diamond_pickaxe", 1))
	srv.SetBlock(5, 64, 4, registry.MustState("minecraft:stone", nil))
	c.Settle()
	c.BreakBlock(5, 64, 4)
	c.Settle()
	if srv.BlockAt(5, 64, 4) != registry.MustState("minecraft:air", nil) {
		t.Fatal("the stone was never broken")
	}
	expectUsed(t, srv, "minecraft:diamond_pickaxe", 0)
}

// TestLayingABoatCountsAsUsed: vanilla: BoatItem.use
func TestLayingABoatCountsAsUsed(t *testing.T) {
	t.Parallel()
	srv, _ := aimedAtWater(t, "minecraft:oak_boat")
	expectUsed(t, srv, "minecraft:oak_boat", 1)
}

// TestAnEggThatSpawnsAMobCountsAsUsed: vanilla: SpawnEggItem.useOn
func TestAnEggThatSpawnsAMobCountsAsUsed(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:cow_spawn_egg")
	actor.UseBlock(3, 63, 0)
	actor.Settle()
	expectUsed(t, srv, "minecraft:cow_spawn_egg", 1)
}

// TestRaisingASpyglassCountsAsUsed: vanilla: SpyglassItem.use
func TestRaisingASpyglassCountsAsUsed(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:spyglass")
	actor.UseItemRightClicks(protocol.HandMain)
	actor.Settle()
	expectUsed(t, srv, "minecraft:spyglass", 1)
}

// TestACycledDebugStickCountsAsUsed: DebugStickItem.useOn answers SUCCESS
// for a block with properties.
// vanilla: ItemStack.useOn, DebugStickItem.useOn
func TestACycledDebugStickCountsAsUsed(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.OpsListing(t, "Actor"))
	c.MoveToGround(4.5, 64, 6.5, true)
	c.FacePlayer(4.5, 64, 6.5, 180, 0)
	srv.GiveSlot("Actor", player.HotbarSlot, servertest.ItemStack(t, "minecraft:debug_stick", 1))
	srv.SetBlock(4, 64, 5, registry.MustState("minecraft:oak_stairs", nil))
	c.Settle()

	c.UseBlock(4, 64, 5)
	c.Settle()
	expectUsed(t, srv, "minecraft:debug_stick", 1)
}

// TestALeadSneakedOntoAFenceCountsAsUsed: with the block's own use skipped by
// the sneak, the lead's useOn ties the mob and the item is counted.
// vanilla: ItemStack.useOn, LeadItem.useOn
func TestALeadSneakedOntoAFenceCountsAsUsed(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Actor")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()
	srv.SetBlock(6, 64, 5, registry.MustState("minecraft:oak_fence", nil))
	giveStack(t, srv, player.HotbarSlot, "minecraft:lead", 2)
	cow := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.Settle()
	c.UseOn(cow)
	c.Settle()
	expectUsed(t, srv, "minecraft:lead", 0)

	c.SendSneak(true)
	c.Settle()
	c.UseBlock(6, 64, 5)
	c.Settle()
	expectUsed(t, srv, "minecraft:lead", 1)
}

// TestACarrotOnAStickCountsAsUsed: vanilla: FoodOnAStickItem.use
func TestACarrotOnAStickCountsAsUsed(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:carrot_on_a_stick")
	servertest.UseHeld(actor, 0, 0)
	actor.Settle()
	expectUsed(t, srv, "minecraft:carrot_on_a_stick", 1)
}

// useKnowledgeBook has the actor use a knowledge book naming recipes and
// reports how many Recipe Book Adds it was sent. vanilla: KnowledgeBookItem.use
func useKnowledgeBook(t *testing.T, recipes ...string) (*server.Server, int) {
	t.Helper()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:knowledge_book")
	book := srv.InventorySlot("Actor", player.HotbarSlot)
	book.Recipes = recipes
	srv.GiveSlot("Actor", player.HotbarSlot, book)
	actor.Settle()
	actor.WatchBook = true
	servertest.UseHeld(actor, 0, 0)
	adds := 0
	actor.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlayRecipeBookAdd {
			adds++
		}
	})
	if got := srv.InventorySlot("Actor", player.HotbarSlot); !got.Empty() {
		t.Errorf("the knowledge book was not spent: %+v", got)
	}
	return srv, adds
}

// TestAKnowledgeBookCountsAsUsed: vanilla: KnowledgeBookItem.use
func TestAKnowledgeBookCountsAsUsed(t *testing.T) {
	t.Parallel()
	srv, adds := useKnowledgeBook(t, "minecraft:oak_boat")
	if adds != 1 {
		t.Errorf("Recipe Book Adds = %d, want 1", adds)
	}
	expectUsed(t, srv, "minecraft:knowledge_book", 1)
}

// TestAKnowledgeBookThatUnlocksNothingIsSpentUncounted: a book naming no
// recipe, or one that does not exist, fails after it is spent.
func TestAKnowledgeBookThatUnlocksNothingIsSpentUncounted(t *testing.T) {
	t.Parallel()
	for _, recipes := range [][]string{nil, {"minecraft:oak_boat", "minecraft:no_such_recipe"}} {
		srv, adds := useKnowledgeBook(t, recipes...)
		if adds != 0 {
			t.Errorf("%v: Recipe Book Adds = %d, want 0", recipes, adds)
		}
		expectUsed(t, srv, "minecraft:knowledge_book", 0)
	}
}
