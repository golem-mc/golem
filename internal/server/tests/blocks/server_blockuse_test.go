package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestACakeIsEatenSliceBySlice: a bite feeds two and takes a slice, and a
// candle goes only on an untouched cake. vanilla: CakeBlock
func TestACakeIsEatenSliceBySlice(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:stick")
	srv.SetFood("Actor", 10)

	srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:cake"])
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:red_candle"), Count: 2})
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if got, want := srv.BlockAt(3, 64, 0), servertest.BlockStateWith(t, "minecraft:red_candle_cake", "lit=false"); got != want {
		t.Fatalf("the cake after a candle = %d, want %d (red candle cake)", got, want)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Count != 1 {
		t.Errorf("candles left = %d, want 1", got.Count)
	}

	// A bite of a candle cake is a bite of a fresh cake, and the candle drops.
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{})
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if got, want := srv.BlockAt(3, 64, 0), servertest.BlockStateWith(t, "minecraft:cake", "bites=1"); got != want {
		t.Errorf("the cake after a bite = %d, want %d (bites=1)", got, want)
	}
	if got := srv.PlayerFood("Actor"); got != 12 {
		t.Errorf("food after a slice = %d, want 12", got)
	}
	candles := int32(0)
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item == "minecraft:red_candle" {
			candles += it.Count
		}
	}
	if candles != 1 {
		t.Errorf("the bite dropped %d candles, want 1", candles)
	}

	// No candle on a cut cake: the click is a bite instead.
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:red_candle"), Count: 1})
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if got, want := srv.BlockAt(3, 64, 0), servertest.BlockStateWith(t, "minecraft:cake", "bites=2"); got != want {
		t.Errorf("a cut cake clicked with a candle = %d, want %d (bites=2)", got, want)
	}
}

// TestAnEmptyHandPutsACandleOut. vanilla: CandleBlock.useItemOn
func TestAnEmptyHandPutsACandleOut(t *testing.T) {
	t.Parallel()
	srv, actor, watcher := servertest.ToolScene(t, "minecraft:stick")
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{})

	srv.SetBlock(3, 64, 0, servertest.BlockStateWith(t, "minecraft:candle", "candles=2,lit=true,waterlogged=false"))
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if got, want := srv.BlockAt(3, 64, 0), servertest.BlockStateWith(t, "minecraft:candle", "candles=2,lit=false,waterlogged=false"); got != want {
		t.Errorf("the candle after the hand = %d, want %d (out)", got, want)
	}
	if h := servertest.HearTool(t, watcher); h.Sounds["minecraft:block.candle.extinguish"] != 1 {
		t.Errorf("the watcher heard %v, want one block.candle.extinguish", h.Sounds)
	}
}

// TestAFlowerPotTakesAndGivesBackAPlant. vanilla: FlowerPotBlock
func TestAFlowerPotTakesAndGivesBackAPlant(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:poppy")
	poppy := servertest.ItemIDOrDie(t, "minecraft:poppy")

	srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:flower_pot"])
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if got, want := srv.BlockAt(3, 64, 0), registry.MustState("minecraft:potted_poppy", nil); got != want {
		t.Fatalf("the pot after a poppy = %d, want %d (potted poppy)", got, want)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); !got.Empty() {
		t.Errorf("the hand after potting = %+v, want empty", got)
	}

	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if got, want := srv.BlockAt(3, 64, 0), registry.BlockStates["minecraft:flower_pot"]; got != want {
		t.Errorf("the pot after an empty hand = %d, want %d (empty pot)", got, want)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.ItemID != poppy || got.Count != 1 {
		t.Errorf("the hand after unpotting = %+v, want the poppy back", got)
	}
}

// TestAComposterFillsAndGivesBoneMeal: a sure compostable raises the level,
// level 7 turns ready after 20 ticks, and a hand takes the bone meal out.
// vanilla: ComposterBlock
func TestAComposterFillsAndGivesBoneMeal(t *testing.T) {
	t.Parallel()
	srv, actor, watcher := servertest.ToolScene(t, "minecraft:pumpkin_pie")
	composter := func(level string) uint16 { return servertest.BlockStateWith(t, "minecraft:composter", "level="+level) }

	srv.SetBlock(3, 64, 0, composter("6"))
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if got := srv.BlockAt(3, 64, 0); got != composter("7") {
		t.Fatalf("the composter after a pie = %d, want %d (level 7)", got, composter("7"))
	}
	if got := servertest.HearTool(t, watcher).Events[1500]; len(got) != 1 {
		t.Errorf("composter fill events = %v, want one", got)
	}
	srv.TickPhysics(20)
	if got := srv.BlockAt(3, 64, 0); got != composter("8") {
		t.Fatalf("the composter 20 ticks on = %d, want %d (ready)", got, composter("8"))
	}

	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{})
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if got := srv.BlockAt(3, 64, 0); got != composter("0") {
		t.Errorf("the emptied composter = %d, want %d (level 0)", got, composter("0"))
	}
	meal := 0
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item != "minecraft:bone_meal" {
			continue
		}
		meal += int(it.Count)
		// Born on the rim: the feet y+1.01 vanilla's spawn takes, the
		// centre an item's half box above.
		if want := float64(64) + 1.01 + entity.ItemHalfSize; it.Y != want {
			t.Errorf("the bone meal's centre = %g, want %g", it.Y, want)
		}
	}
	if meal != 1 {
		t.Errorf("the composter gave %d bone meal, want 1", meal)
	}
}

// TestBucketsFillAndEmptyACauldron. vanilla: CauldronInteractions
func TestBucketsFillAndEmptyACauldron(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:water_bucket")

	srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:cauldron"])
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if got, want := srv.BlockAt(3, 64, 0), servertest.BlockStateWith(t, "minecraft:water_cauldron", "level=3"); got != want {
		t.Fatalf("the cauldron after a water bucket = %d, want %d (full water)", got, want)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.ItemID != servertest.ItemIDOrDie(t, "minecraft:bucket") {
		t.Errorf("the hand after pouring = %+v, want an empty bucket", got)
	}
	if got := srv.BlockAt(3, 65, 0); got != 0 {
		t.Errorf("above the cauldron = %d, want air (the water went in, not on top)", got)
	}

	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if got := srv.BlockAt(3, 64, 0); got != registry.BlockStates["minecraft:cauldron"] {
		t.Errorf("the cauldron after an empty bucket = %d, want empty", got)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.ItemID != servertest.ItemIDOrDie(t, "minecraft:water_bucket") {
		t.Errorf("the hand after filling = %+v, want a water bucket", got)
	}
}

// cauldronUse puts the stack in the actor's hand and uses it on a cauldron
// of the given state, answering the cauldron's state afterwards.
func cauldronUse(t *testing.T, srv *server.Server, actor *servertest.Client, cauldron uint16, held protocol.ItemStack) uint16 {
	t.Helper()
	srv.SetBlock(3, 64, 0, cauldron)
	srv.GiveSlot("Actor", player.HotbarSlot, held)
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	return srv.BlockAt(3, 64, 0)
}

// TestBottlesFillAndEmptyACauldron. vanilla: CauldronInteractions.bootStrap
func TestBottlesFillAndEmptyACauldron(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:glass_bottle")
	water := func(level string) uint16 {
		return servertest.BlockStateWith(t, "minecraft:water_cauldron", "level="+level)
	}

	if got := cauldronUse(t, srv, actor, registry.BlockStates["minecraft:cauldron"], servertest.WaterBottle(t)); got != water("1") {
		t.Errorf("the empty cauldron after a water bottle = %d, want %d (level 1)", got, water("1"))
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.ItemID != servertest.ItemIDOrDie(t, "minecraft:glass_bottle") {
		t.Errorf("the hand after pouring = %+v, want a glass bottle", got)
	}

	if got := cauldronUse(t, srv, actor, water("2"), servertest.WaterBottle(t)); got != water("3") {
		t.Errorf("a level 2 cauldron after a water bottle = %d, want %d", got, water("3"))
	}
	if got := cauldronUse(t, srv, actor, water("3"), servertest.WaterBottle(t)); got != water("3") {
		t.Errorf("a full cauldron after a water bottle = %d, want it unchanged", got)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.ItemID != servertest.ItemIDOrDie(t, "minecraft:potion") {
		t.Errorf("the hand after a refused bottle = %+v, want the water bottle kept", got)
	}

	if got := cauldronUse(t, srv, actor, water("2"), servertest.ItemStack(t, "minecraft:glass_bottle", 1)); got != water("1") {
		t.Errorf("a level 2 cauldron after a glass bottle = %d, want %d", got, water("1"))
	}
	filled := srv.InventorySlot("Actor", player.HotbarSlot)
	if contents, ok := filled.PotionContentsOf(); filled.ItemID != servertest.ItemIDOrDie(t, "minecraft:potion") || !ok || !rules.PotionIsWater(contents) {
		t.Errorf("the hand after a glass bottle = %+v, want a water bottle", filled)
	}
	if got := cauldronUse(t, srv, actor, water("1"), servertest.ItemStack(t, "minecraft:glass_bottle", 1)); got != registry.BlockStates["minecraft:cauldron"] {
		t.Errorf("a level 1 cauldron after a glass bottle = %d, want empty", got)
	}
}

// TestCauldronWashesDyedArmour. vanilla: CauldronInteractions.dyedItemIteration
func TestCauldronWashesDyedArmour(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:leather_helmet")
	water := func(level string) uint16 {
		return servertest.BlockStateWith(t, "minecraft:water_cauldron", "level="+level)
	}

	helmet := servertest.ItemStack(t, "minecraft:leather_helmet", 1)
	helmet.SetDyedColor(0x3b5dc6)
	if got := cauldronUse(t, srv, actor, water("3"), helmet); got != water("2") {
		t.Errorf("the cauldron after washing = %d, want %d (level 2)", got, water("2"))
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.ItemID != helmet.ItemID || got.HasDyedColor() {
		t.Errorf("the hand after washing = %+v, want the helmet without its dye", got)
	}

	// Undyed armour is left for the bare hand: nothing is washed.
	if got := cauldronUse(t, srv, actor, water("3"), servertest.ItemStack(t, "minecraft:leather_helmet", 1)); got != water("3") {
		t.Errorf("the cauldron after an undyed helmet = %d, want it unchanged", got)
	}
}

// TestCauldronWashesBannersAndShulkerBoxes. vanilla:
// CauldronInteractions.bannerInteraction, shulkerBoxInteraction
func TestCauldronWashesBannersAndShulkerBoxes(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:white_banner")
	water := func(level string) uint16 {
		return servertest.BlockStateWith(t, "minecraft:water_cauldron", "level="+level)
	}

	banner := servertest.ItemStack(t, "minecraft:white_banner", 2)
	layers := []protocol.BannerPatternLayer{{Pattern: 0, Dye: 1}, {Pattern: 1, Dye: 2}}
	banner.SetBannerPatterns(layers)
	if got := cauldronUse(t, srv, actor, water("3"), banner); got != water("2") {
		t.Errorf("the cauldron after a banner = %d, want %d", got, water("2"))
	}
	var cleaned protocol.ItemStack
	var banners int32
	for slot := range player.HotbarSlot + 9 {
		s := srv.InventorySlot("Actor", slot)
		if s.ItemID == banner.ItemID {
			banners += s.Count
			if got, _ := s.BannerPatternsOf(); len(got) == 1 {
				cleaned = s
			}
		}
	}
	if got, _ := cleaned.BannerPatternsOf(); len(got) != 1 || got[0] != layers[0] {
		t.Errorf("the washed banner's layers = %v, want only %v", got, layers[:1])
	}
	if banners != 2 || cleaned.Count != 1 {
		t.Errorf("banners held = %d with the washed one %d strong, want 2 and 1", banners, cleaned.Count)
	}

	if got := cauldronUse(t, srv, actor, water("3"), servertest.ItemStack(t, "minecraft:white_banner", 1)); got != water("3") {
		t.Errorf("the cauldron after a plain banner = %d, want it unchanged", got)
	}

	if got := cauldronUse(t, srv, actor, water("2"), servertest.ItemStack(t, "minecraft:red_shulker_box", 1)); got != water("1") {
		t.Errorf("the cauldron after a shulker box = %d, want %d", got, water("1"))
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.ItemID != servertest.ItemIDOrDie(t, "minecraft:shulker_box") {
		t.Errorf("the hand after washing a shulker box = %+v, want a plain shulker box", got)
	}
}

// TestCauldronRefusesWhatIsNotPlainWater. vanilla: PotionContents.is,
// CauldronInteractions.bootStrap
func TestCauldronRefusesWhatIsNotPlainWater(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:glass_bottle")
	empty := registry.BlockStates["minecraft:cauldron"]

	effects := servertest.WaterBottle(t)
	effects.SetPotionContents(protocol.PotionContents{
		Potion:        registry.MustPotion("minecraft:water"),
		CustomEffects: []protocol.PotionEffectSpec{{ID: registry.MustMobEffect("minecraft:jump_boost"), Duration: 200}},
	})
	if got := cauldronUse(t, srv, actor, empty, effects); got != empty {
		t.Errorf("the empty cauldron after a water bottle with custom effects = %d, want it unchanged", got)
	}

	splash := servertest.WaterBottle(t)
	splash.ItemID = servertest.ItemIDOrDie(t, "minecraft:splash_potion")
	if got := cauldronUse(t, srv, actor, empty, splash); got != empty {
		t.Errorf("the empty cauldron after a splash water bottle = %d, want it unchanged", got)
	}

	for _, name := range []string{"minecraft:lava_cauldron", "minecraft:powder_snow_cauldron"} {
		state := registry.BlockStates[name]
		if name == "minecraft:powder_snow_cauldron" {
			state = servertest.BlockStateWith(t, name, "level=3")
		}
		for _, held := range []protocol.ItemStack{servertest.WaterBottle(t), servertest.ItemStack(t, "minecraft:glass_bottle", 1)} {
			if got := cauldronUse(t, srv, actor, state, held); got != state {
				t.Errorf("%s after item %d = %d, want it unchanged", name, held.ItemID, got)
			}
		}
	}
}

// TestAWashedBannerStacksWithAPlainOne: the last layer washed off leaves the
// banner's default patterns, so it merges with a plain banner. vanilla:
// BannerPatternLayers.removeLast, PatchedDataComponentMap.set
func TestAWashedBannerStacksWithAPlainOne(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:white_banner")
	banner := servertest.ItemStack(t, "minecraft:white_banner", 1)
	banner.SetBannerPatterns([]protocol.BannerPatternLayer{{Pattern: 0, Dye: 1}})
	cauldronUse(t, srv, actor, servertest.BlockStateWith(t, "minecraft:water_cauldron", "level=3"), banner)
	plain := servertest.ItemStack(t, "minecraft:white_banner", 1)
	for slot := player.HotbarSlot; slot < player.HotbarSlot+9; slot++ {
		if s := srv.InventorySlot("Actor", slot); s.ItemID == banner.ItemID && !s.Same(plain) {
			t.Errorf("slot %d = %+v, want it the same stack as a plain banner", slot, s)
		}
	}
}

// TestACreativePlayerKeepsTheHandOnACauldron. vanilla: ItemUtils.createFilledResult
func TestACreativePlayerKeepsTheHandOnACauldron(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder")
	srv.StopTick()
	srv.GiveSlot("Builder", player.HotbarSlot, servertest.WaterBottle(t))
	srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:cauldron"])
	c.UseBlock(3, 64, 0)
	c.Settle()
	if got, want := srv.BlockAt(3, 64, 0), servertest.BlockStateWith(t, "minecraft:water_cauldron", "level=1"); got != want {
		t.Errorf("the cauldron after a creative pour = %d, want %d", got, want)
	}
	if got := srv.InventorySlot("Builder", player.HotbarSlot); got.ItemID != servertest.ItemIDOrDie(t, "minecraft:potion") {
		t.Errorf("the hand after a creative pour = %+v, want the water bottle kept", got)
	}
}

// TestShearsStopAGrowingVine. vanilla: ShearsItem.useOn
func TestShearsStopAGrowingVine(t *testing.T) {
	t.Parallel()
	srv, actor, watcher := servertest.ToolScene(t, "minecraft:shears")

	srv.SetBlock(3, 66, 0, registry.BlockStates["minecraft:stone"])
	srv.SetBlock(3, 65, 0, servertest.BlockStateWith(t, "minecraft:weeping_vines", "age=4"))
	actor.UseBlock(3, 65, 0)
	actor.Settle()
	if got, want := srv.BlockAt(3, 65, 0), servertest.BlockStateWith(t, "minecraft:weeping_vines", "age=25"); got != want {
		t.Errorf("the vine after the shears = %d, want %d (age 25)", got, want)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Damage != 1 {
		t.Errorf("the shears' wear = %d, want 1", got.Damage)
	}
	if h := servertest.HearTool(t, watcher); h.Sounds["minecraft:block.growing_plant.crop"] != 1 {
		t.Errorf("the watcher heard %v, want one block.growing_plant.crop", h.Sounds)
	}
}

// TestAUsedBlockThrowsWhatDoesNotFit: a survival player's leftover from a
// use is thrown from the thrower's own x and z three tenths of a block
// under the eyes, with a throw's wait before pickup, not laid at the feet.
// vanilla: FlowerPotBlock.use, LivingEntity.createItemStackToDrop
func TestAUsedBlockThrowsWhatDoesNotFit(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:stick")
	stone := protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:stone"), Count: 64}
	for slot := player.ArmorSlot; slot <= player.OffHandSlot; slot++ {
		srv.GiveSlot("Actor", slot, stone)
	}

	srv.SetBlock(3, 64, 0, registry.MustState("minecraft:potted_poppy", nil))
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if got, want := srv.BlockAt(3, 64, 0), registry.BlockStates["minecraft:flower_pot"]; got != want {
		t.Fatalf("the pot after the hand = %d, want %d (empty pot)", got, want)
	}
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 || items[0].Item != "minecraft:poppy" || items[0].Count != 1 {
		t.Fatalf("the unpot left %+v, want one poppy", items)
	}
	want := float64(64) + 1.62 - entity.ThrowSpawnBelow + entity.ItemHalfSize
	if got := items[0].Y; got != want {
		t.Errorf("the poppy left at %g, want %g (three tenths under the eyes)", got, want)
	}
	if items[0].X != 0.5 || items[0].Z != 0.5 {
		t.Errorf("the poppy thrown from (%g, %g), want the thrower's own x and z", items[0].X, items[0].Z)
	}
	if got := items[0].PickupDelay; got != entity.ThrownPickupDelay {
		t.Errorf("the poppy's pickup delay = %d, want %d (a throw's wait)", got, entity.ThrownPickupDelay)
	}
}

// TestTheOffHandIsNotAFreeSlot: with all 36 item slots full and the off
// hand empty, what a use brings back is thrown out, not parked in the off
// hand - a free slot is one of the 36 item slots, and only a stack of the
// same item is merged into the off hand.
// vanilla: Inventory.getFreeSlot, Inventory.getSlotWithRemainingSpace
func TestTheOffHandIsNotAFreeSlot(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:stick")
	stone := protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:stone"), Count: 64}
	for slot := player.MainSlot; slot < player.HotbarSlot+player.HotbarSlots; slot++ {
		srv.GiveSlot("Actor", slot, stone)
	}

	srv.SetBlock(3, 64, 0, registry.MustState("minecraft:potted_poppy", nil))
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if got, want := srv.BlockAt(3, 64, 0), registry.BlockStates["minecraft:flower_pot"]; got != want {
		t.Fatalf("the pot after the hand = %d, want %d (empty pot)", got, want)
	}
	if got := srv.InventorySlot("Actor", player.OffHandSlot); !got.Empty() {
		t.Errorf("the off hand after the unpot = %+v, want empty", got)
	}
	if got := srv.InventorySlot("Actor", player.MainSlot); got.Count != 64 {
		t.Errorf("the first main slot = %+v, want the stone untouched", got)
	}
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 || items[0].Item != "minecraft:poppy" || items[0].Count != 1 {
		t.Fatalf("the unpot left %+v, want one poppy thrown out", items)
	}
}

// TestACreativePlayerVoidsWhatDoesNotFit: what a use's result cannot fit a
// creative player's inventory is voided, nothing dropped. vanilla:
// ItemUtils.createFilledResult, hasInfiniteMaterials
func TestACreativePlayerVoidsWhatDoesNotFit(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder")
	srv.StopTick()
	stone := protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:stone"), Count: 64}
	for slot := player.ArmorSlot; slot <= player.OffHandSlot; slot++ {
		srv.GiveSlot("Builder", slot, stone)
	}

	srv.SetBlock(3, 64, 0, registry.MustState("minecraft:potted_poppy", nil))
	c.UseBlock(3, 64, 0)
	c.Settle()
	if got, want := srv.BlockAt(3, 64, 0), registry.BlockStates["minecraft:flower_pot"]; got != want {
		t.Fatalf("the pot after the hand = %d, want %d (empty pot)", got, want)
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 0 {
		t.Errorf("the unpot dropped %+v, want nothing", items)
	}
}

// TestTheCraftingGridIsNotTheInventory: a creative pour adds its empty
// bucket even with one parked in the 2x2 grid, which contains does not see.
// vanilla: ItemUtils.createFilledResult, Inventory.contains
func TestTheCraftingGridIsNotTheInventory(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder")
	srv.StopTick()
	bucket := servertest.ItemIDOrDie(t, "minecraft:bucket")
	srv.GiveSlot("Builder", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:water_bucket"), Count: 1})
	srv.GiveSlot("Builder", 1, protocol.ItemStack{ItemID: bucket, Count: 1})

	srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:cauldron"])
	c.UseBlock(3, 64, 0)
	c.Settle()
	if got, want := srv.BlockAt(3, 64, 0), servertest.BlockStateWith(t, "minecraft:water_cauldron", "level=3"); got != want {
		t.Fatalf("the cauldron after a water bucket = %d, want %d (full water)", got, want)
	}
	if got := srv.InventorySlot("Builder", player.HotbarSlot); got.ItemID != servertest.ItemIDOrDie(t, "minecraft:water_bucket") {
		t.Errorf("the hand after pouring = %+v, want the water bucket kept", got)
	}
	if got := srv.InventorySlot("Builder", player.HotbarSlot+1); got.ItemID != bucket || got.Count != 1 {
		t.Errorf("the next slot after pouring = %+v, want the empty bucket added", got)
	}
}

// TestAnAdventureHandStillUsesTheBlocks: an adventure hand's right-click on
// a block still answers - the douse, the bite, the chest - while the item's
// own answers below them stay refused, the echoes telling the client so.
// vanilla: ItemStack.useOn, ServerPlayerGameMode.useItemOn
func TestAnAdventureHandStillUsesTheBlocks(t *testing.T) {
	t.Parallel()
	srv, actor, watcher := servertest.AdventureScene(t)

	// The placement stays refused: the echoes answer and the ground keeps
	// its shape.
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:stone"), Count: 1})
	actor.PlaceBlock(2, 63, 0, protocol.FaceTop)
	actor.ExpectUseEchoes(2, 63, 0, protocol.FaceTop)
	if got := srv.BlockAt(2, 64, 0); got != 0 {
		t.Errorf("the adventure placement left %d at (2,64,0), want air", got)
	}

	// The bite: a cake feeds the hungry hand that holds nothing.
	srv.SetBlock(2, 64, 0, registry.BlockStates["minecraft:cake"])
	srv.SetFood("Actor", 10)
	actor.UseBlock(2, 64, 0)
	actor.Settle()
	if got, want := srv.BlockAt(2, 64, 0), servertest.BlockStateWith(t, "minecraft:cake", "bites=1"); got != want {
		t.Errorf("the cake after a bite = %d, want %d (bites=1)", got, want)
	}

	// The douse: a lit candle cake clicked on its upper half goes out, the
	// cursor's height being what says upper half, the hand holding nothing.
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{})
	srv.SetBlock(3, 64, 0, servertest.BlockStateWith(t, "minecraft:candle_cake", "lit=true"))
	actor.PlaceBlockAt(3, 64, 0, protocol.FaceTop, 0.875)
	actor.Settle()
	if got, want := srv.BlockAt(3, 64, 0), servertest.BlockStateWith(t, "minecraft:candle_cake", "lit=false"); got != want {
		t.Errorf("the candle cake after the douse = %d, want %d (lit=false)", got, want)
	}
	if h := servertest.HearTool(t, watcher); h.Sounds["minecraft:block.candle.extinguish"] != 1 {
		t.Errorf("the watcher heard %v, want one block.candle.extinguish", h.Sounds)
	}

	// The chest: a container opens for the adventure hand.
	srv.SetBlock(4, 64, 0, registry.BlockStates["minecraft:chest"])
	actor.UseBlock(4, 64, 0)
	if opened := chestOpens(t, actor); opened != 1 {
		t.Errorf("the chest opened %d windows for the adventure hand, want one", opened)
	}
}

// chestOpens counts the chest windows the client was shown, one per Open
// Screen whose menu is the chest's.
func chestOpens(t *testing.T, c *servertest.Client) int {
	t.Helper()
	opened := 0
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayOpenScreen {
			return
		}
		r := protocol.NewReader(data)
		r.ReadVarInt() // the window ID the packets that follow address it by
		if r.ReadVarInt() == protocol.MenuGeneric9x3 {
			opened++
		}
	})
	return opened
}

// TestAnAdventureHandDoesNotCycleTheRedstone: the uses vanilla gates on
// mayBuild along with the item's - the repeater's delay and the
// comparator's mode - stay put for an adventure hand. The click still
// counts as used, which is the echoes' answer.
// vanilla: RepeaterBlock.useWithoutItem, ComparatorBlock.useWithoutItem
func TestAnAdventureHandDoesNotCycleTheRedstone(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.AdventureScene(t)

	// The repeater's delay stays at the one it was placed with.
	repeater := registry.BlockStates["minecraft:repeater"]
	srv.SetBlock(2, 64, 0, repeater)
	actor.UseBlock(2, 64, 0)
	actor.Settle()
	if got := srv.BlockAt(2, 64, 0); got != repeater {
		t.Errorf("the repeater after an adventure click = %d, want it unchanged", got)
	}

	// The comparator's mode stays at compare.
	comparator := registry.BlockStates["minecraft:comparator"]
	srv.SetBlock(3, 64, 0, comparator)
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if got := srv.BlockAt(3, 64, 0); got != comparator {
		t.Errorf("the comparator after an adventure click = %d, want it unchanged", got)
	}
}

// TestAnAdventureHandPrimesTntOnlyWithCanBreak checks that a refused block
// use consumes the click without falling through to placing fire.
// vanilla: TntBlock.prime, TntBlock.useItemOn
func TestAnAdventureHandPrimesTntOnlyWithCanBreak(t *testing.T) {
	t.Parallel()
	for _, tool := range []string{"minecraft:flint_and_steel", "minecraft:fire_charge"} {
		t.Run(tool, func(t *testing.T) {
			srv, actor, watcher := servertest.AdventureScene(t)
			tnt := registry.BlockStates["minecraft:tnt"]
			srv.SetBlock(3, 64, 0, tnt)
			for _, block := range []string{"", "minecraft:stone", "minecraft:tnt"} {
				stack := servertest.ItemStack(t, tool, 1)
				if block != "" {
					stack = servertest.AdventuredStack(t, tool, protocol.DataComponentCanBreak, block)
				}
				srv.GiveSlot("Actor", player.HotbarSlot, stack)
				actor.PlaceBlock(3, 64, 0, protocol.FaceTop)
				actor.Settle()
				checkAdventureTntUse(t, srv, watcher, tool, tnt, block == "minecraft:tnt")
			}
		})
	}
}

func checkAdventureTntUse(t *testing.T, srv *server.Server, watcher *servertest.Client, tool string, tnt uint16, allowed bool) {
	t.Helper()
	wantBlock, wantDamage, wantCount, wantSounds := tnt, int32(0), int32(1), 0
	if allowed {
		wantBlock, wantSounds = 0, 1
		if tool == "minecraft:flint_and_steel" {
			wantDamage = 1
		} else {
			wantCount = 0
		}
	}
	if got := srv.BlockAt(3, 64, 0); got != wantBlock {
		t.Errorf("TNT = %d, want %d", got, wantBlock)
	}
	if got := srv.BlockAt(3, 65, 0); got != 0 {
		t.Errorf("above TNT = %d, want no fire", got)
	}
	held := srv.InventorySlot("Actor", player.HotbarSlot)
	if held.Damage != wantDamage || held.Count != wantCount {
		t.Errorf("held damage/count = %d/%d, want %d/%d", held.Damage, held.Count, wantDamage, wantCount)
	}
	if got := srv.StatValue("Actor", "minecraft:used", tool); got != int32(wantSounds) {
		t.Errorf("item_used = %d, want %d", got, wantSounds)
	}
	if h := servertest.HearTool(t, watcher); h.Sounds["minecraft:entity.tnt.primed"] != wantSounds {
		t.Errorf("sounds = %v, want %d TNT priming sounds", h.Sounds, wantSounds)
	}
}

// TestABareSneakStillUsesTheBlock: the sneak key skips the block's uses only
// while something is held in either hand, a bare-handed sneak using the
// block still - the composter empties and the chest opens.
// vanilla: ServerPlayerGameMode.useItemOn
func TestABareSneakStillUsesTheBlock(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:stick")
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{})

	actor.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })

	// The ready composter empties for the bare sneaking hand.
	composter := func(level string) uint16 { return servertest.BlockStateWith(t, "minecraft:composter", "level="+level) }
	srv.SetBlock(3, 64, 0, composter("8"))
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if got := srv.BlockAt(3, 64, 0); got != composter("0") {
		t.Errorf("the composter after a sneaking extract = %d, want %d (level 0)", got, composter("0"))
	}
	meal := 0
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item == "minecraft:bone_meal" {
			meal += int(it.Count)
		}
	}
	if meal != 1 {
		t.Errorf("the sneaking extract dropped %d bone meal, want 1", meal)
	}

	// The chest opens for the bare sneaking hand.
	srv.SetBlock(4, 64, 0, registry.BlockStates["minecraft:chest"])
	actor.UseBlock(4, 64, 0)
	if opened := chestOpens(t, actor); opened != 1 {
		t.Errorf("the chest opened %d windows for the bare sneak, want one", opened)
	}
}
