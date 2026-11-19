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
