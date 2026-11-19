package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"maps"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The component-driven behaviour of the items: what a stack of a thing
// holds, what its tool does for the drops of a block, what its weapon does
// to a player, what its armour does to a blow, and what its food does to a
// bar - all of it read out of the vanilla item components the registry's
// tables are generated from, and all of it driven through a real connection
// the way the game would. The survival players here are handed their items
// through GiveSlot: a survival client may not use the creative slot packet,
// and these tests are that client.

// TestEnderPearlsStackToSixteen checks the stack size an item's own
// components carry: twenty ender pearls given to a survival player fill one
// slot to its sixteen and put the remaining four in the next slot.
func TestEnderPearlsStackToSixteen(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Collector", servertest.OpsListing(t, "Collector"))

	pearl := servertest.MustItemID(t, "minecraft:ender_pearl")
	if got := registry.MaxStackSize(pearl); got != 16 {
		t.Fatalf("ender pearls stack to %d in this version's components, want 16", got)
	}

	// Twenty given to an empty inventory: one stack of the sixteen the
	// item holds, in the hotbar.
	c.ChatCommand("give @s minecraft:ender_pearl 20")
	updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Stack.ItemID != pearl || updates[0].Stack.Count != 16 {
		t.Errorf("the given pearls = %+v, want one stack of sixteen", updates[0].Stack)
	}
	c.DrainSurvivalTraffic()
	if got := srv.InventorySlot("Collector", player.HotbarSlot); got.ItemID != pearl || got.Count != 16 {
		t.Errorf("the collector's hotbar = %+v, want sixteen ender pearls", got)
	}

	// vanilla: GiveCommand.giveItem gives one max-sized stack at a time.
	c.Settle()
	stack := srv.InventorySlot("Collector", player.HotbarSlot+1)
	if stack.Empty() || stack.ItemID != pearl || stack.Count != 4 {
		t.Fatal("the four pearls that did not fit wait on the ground for their pickup")
	}

	c.DrainSurvivalTraffic()
	if got := srv.InventorySlot("Collector", player.HotbarSlot); got.Count != 16 {
		t.Errorf("the first stack = %+v, wanted still the full sixteen after the pickup", got)
	}
}

// TestGoldenPickaxeBreaksAfterItsMaxDamage checks the wear a tool takes and
// the end of it: a golden pickaxe - thirty-two damage in this version's
// components, and durability the golden tier has always been short of - wears
// one per stone it breaks in survival, and the thirty-second break empties
// the hand. The breaking is something everyone is told: the entity event
// that plays the shatter goes out with the slot the item was in.
func TestGoldenPickaxeBreaksAfterItsMaxDamage(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Miner")

	pickaxe := servertest.MustItemID(t, "minecraft:golden_pickaxe")
	if got := registry.MaxDamage(pickaxe); got != 32 {
		t.Fatalf("a golden pickaxe takes %d damage in this version's components, want 32", got)
	}
	if got := registry.DamagePerBlock(pickaxe); got != 1 {
		t.Fatalf("a golden pickaxe wears %d per block, want 1", got)
	}
	srv.GiveSlot("Miner", player.HotbarSlot, protocol.ItemStack{ItemID: pickaxe, Count: 1})

	// Thirty-one stones break one wear at a time, the stones replaced ahead
	// of the pickaxe by blocks that sit still while it works them.
	const y, z = int32(63), int32(6)
	for i := int32(1); i <= 31; i++ {
		// The Miner stands within the block interaction's reach of the
		// stone being worked: vanilla refuses a dig past the reach, and
		// the row of stones outruns any one standing.
		c.MoveToGround(float64(i)+0.5, 64, 8, true)
		if got := srv.BlockAt(i, y, z); got == 0 {
			t.Fatalf("the block (1, 63, 6) became air before the %dth break", i)
		}
		c.BreakBlock(i, y, z)
		// A golden pickaxe takes four ticks to the stone, so the break
		// lands behind the click and the wear with it: wait for the block
		// to have gone before reading the slot, and before the next dig,
		// which vanilla would leave standing while one waited.
		c.WaitBlockChanged()
		got := srv.InventorySlot("Miner", player.HotbarSlot)
		if got.ItemID != pickaxe || got.Damage != i {
			t.Fatalf("the pickaxe after %d breaks = %+v, want wear %d", i, got, i)
		}
	}

	// The thirty-second break wears the pickaxe past the end of it: the
	// block goes, and the hand comes up empty with the shatter played.
	c.BreakBlock(32, y, z)
	c.WaitBlockChanged()
	if got := srv.BlockAt(32, y, z); got != 0 {
		t.Errorf("block after the last break = %d, want air", got)
	}
	if got := srv.InventorySlot("Miner", player.HotbarSlot); !got.Empty() {
		t.Errorf("the hand after the last break = %+v, want it empty; the pickaxe is broken", got)
	}
}

// TestTheToolDecidesTheDrops checks the tool rules the drops run by: the
// first rule of the held item's tool component that covers a block is the
// one that says whether the item is the correct tool for the drops of it,
// and the block's own side of the bargain is that some blocks give their
// drops up only to the correct tool at all. Stone bends to any pickaxe;
// iron ore holds out for a stone one or better, and gives a wooden pickaxe
// nothing but a hole.
func TestTheToolDecidesTheDrops(t *testing.T) {
	t.Parallel()
	// The wooden pickaxe is twenty-three ticks to the stone and a hundred
	// and fifty to the iron ore it is not correct for, so the digs run on
	// the step clock rather than the wall's.
	clock := servertest.NewStepClock()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Miner", func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	// On the ground: the dig's pace counts the standing.
	c.MoveToGround(0.5, 64, 0.5, true)

	stone := registry.BlockStates["minecraft:stone"]
	if stone == 0 {
		t.Fatal("the block registry has no stone")
	}
	ironOre := registry.BlockStates["minecraft:iron_ore"]
	if ironOre == 0 {
		t.Fatal("the block registry has no iron ore")
	}
	woodenPickaxe := servertest.MustItemID(t, "minecraft:wooden_pickaxe")
	stonePickaxe := servertest.MustItemID(t, "minecraft:stone_pickaxe")
	srv.GiveSlot("Miner", player.HotbarSlot, protocol.ItemStack{ItemID: woodenPickaxe, Count: 1})

	// Stone with a wooden pickaxe: cobblestone, the way it always was. The
	// ground the flat world stands on is stone already, so there is nothing
	// to place - only a block of it to break.
	c.BreakBlock(2, 63, 3)
	c.Settle()
	servertest.StepTicks(t, srv, clock, 40)
	_, drop := c.WaitBreakSettled()
	cobblestone := servertest.MustItemID(t, "minecraft:cobblestone")
	if drop.Stack.ItemID != cobblestone || drop.Stack.Count != 1 {
		t.Errorf("stone under a wooden pickaxe dropped %+v, want one cobblestone", drop.Stack)
	}

	// Iron ore under the same pickaxe: nothing. The block breaks slower -
	// the client's own patience - but the drops it holds need the correct
	// tool, and a wooden pickaxe's rules say it is not that for iron ore.
	if !srv.SetBlock(3, 63, 3, ironOre) {
		t.Fatal("the world did not take the iron ore")
	}
	c.BreakBlock(3, 63, 3)
	c.Settle()
	servertest.StepTicks(t, srv, clock, 170)
	if change := c.WaitBlockChanged(); change.State != 0 {
		t.Errorf("block after the bare-handed break = %d, want air", change.State)
	}
	c.ExpectNothingDroppedish()

	// Iron ore under a stone pickaxe: raw iron, the stone tier clearing the
	// tier the ore demands.
	srv.GiveSlot("Miner", player.HotbarSlot, protocol.ItemStack{ItemID: stonePickaxe, Count: 1})
	if !srv.SetBlock(3, 63, 3, ironOre) {
		t.Fatal("the world did not take the iron ore back")
	}
	c.BreakBlock(3, 63, 3)
	c.Settle()
	servertest.StepTicks(t, srv, clock, 40)
	_, drop = c.WaitBreakSettled()
	rawIron := servertest.MustItemID(t, "minecraft:raw_iron")
	if drop.Stack.ItemID != rawIron || drop.Stack.Count != 1 {
		t.Errorf("iron ore under a stone pickaxe dropped %+v, want one raw iron", drop.Stack)
	}
}

// TestSwordDamageByTheComponents checks what a held item adds to a punch:
// the fist's one plus the item's own attack_damage modifier - an iron sword
// carrying five in this version's components, for six of a hit - and the
// wear the weapon takes for landing it, one for a sword. The wielder stands
// on the ground for this one, keeping the hit un-critical.
func TestSwordDamageByTheComponents(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)

	victim := connect()
	victimID := victim.JoinAs("Victim")

	attacker := connect()
	attackerID := attacker.JoinAs("Attacker")

	// The victim hears the attacker arrive before the fighting starts.
	victim.ExpectPlayerAdded("Attacker")
	victim.ExpectAnnouncement("multiplayer.player.joined", "Attacker")

	attacker.MoveToGround(0.5, 64, 0.5, true)
	victim.MoveToGround(3.5, 64, 0.5, true)
	attacker.Settle()

	sword := servertest.MustItemID(t, "minecraft:iron_sword")
	srv.GiveSlot("Attacker", player.HotbarSlot, protocol.ItemStack{ItemID: sword, Count: 1})
	// The sword's arrival in the hand spends the swing's recovery back to
	// nought; the hit waits the cooldown out, so it lands whole.
	servertest.AwaitFullSwing(t, srv, "Attacker")

	// The hit: one plus the sword's five, held in the main hand.
	damage := servertest.FistDamageTestBase + float64(registry.AttackDamage(sword))
	attacker.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) { b.PutVarInt(victimID) })

	got := victim.ExpectHealth()
	if want := float32(servertest.MaxHealthTestBase - damage); math.Abs(float64(got.Health-want)) > 0.01 {
		t.Errorf("victim health after the sword = %v, want %v (a fist's one plus the sword's %v)",
			got.Health, want, registry.AttackDamage(sword))
	}
	victim.ExpectDamageEvent(victimID, "minecraft:player_attack", attackerID+1)

	// The sword paid a wear for the hit: one, of the two fifty it has.
	attacker.DrainSurvivalTraffic()
	worn := srv.InventorySlot("Attacker", player.HotbarSlot)
	if worn.ItemID != sword || worn.Damage != registry.ItemWeapon(sword).DamagePerAttack {
		t.Errorf("the sword after one landed hit = %+v, want wear %d", worn, registry.ItemWeapon(sword).DamagePerAttack)
	}
}

// TestArmourReducesTheBlow checks the share vanilla's armour formula leaves
// a hit, and the price the armour pays for it: an iron chestplate's six
// points take a twelfth off the iron sword's six - the better part of half
// a heart - and the piece wears a quarter of the blow, at least one, into
// the bargain.
func TestArmourReducesTheBlow(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)

	victim := connect()
	victimID := victim.JoinAs("Victim")

	attacker := connect()
	attacker.JoinAs("Attacker")

	// The victim hears the attacker arrive before the fighting starts.
	victim.ExpectPlayerAdded("Attacker")
	victim.ExpectAnnouncement("multiplayer.player.joined", "Attacker")

	attacker.MoveToGround(0.5, 64, 0.5, true)
	victim.MoveToGround(3.5, 64, 0.5, true)
	attacker.Settle()

	chestplate := servertest.MustItemID(t, "minecraft:iron_chestplate")
	points, toughness := registry.ArmorOf(chestplate)
	srv.GiveSlot("Victim", player.ArmorSlot+1, protocol.ItemStack{ItemID: chestplate, Count: 1})

	sword := servertest.MustItemID(t, "minecraft:iron_sword")
	srv.GiveSlot("Attacker", player.HotbarSlot, protocol.ItemStack{ItemID: sword, Count: 1})
	// The sword's arrival in the hand spends the swing's recovery back to
	// nought; the hit waits the cooldown out, so it lands whole.
	servertest.AwaitFullSwing(t, srv, "Attacker")

	damage := servertest.FistDamageTestBase + float64(registry.AttackDamage(sword))
	reduction := math.Min(20, math.Max(float64(points)/5, float64(points)-4*damage/(float64(toughness)+8))) / 25
	surviving := damage * (1 - reduction)

	attacker.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) { b.PutVarInt(victimID) })
	// The bars arrive by expectHealthish rather than the plainer
	// expectHealth: the chestplate's own wear is corrected on a slot packet
	// that lands ahead of the bar it turned.
	got := victim.ExpectHealthish()
	if want := float32(servertest.MaxHealthTestBase - surviving); math.Abs(float64(got.Health-want)) > 0.01 {
		t.Errorf("victim health behind the chestplate = %v, want %v (the sword's %v less the armour's share)",
			got.Health, want, damage)
	}
	if !(got.Health > float32(servertest.MaxHealthTestBase-damage)) {
		t.Errorf("the armour reduced nothing: health %v is a whole sword's worth of %v", got.Health, damage)
	}

	// The chestplate paid for the turn with its own wear.
	victim.DrainSurvivalTraffic()
	worn := srv.InventorySlot("Victim", player.ArmorSlot+1)
	perPiece := max(1, int(math.Floor(damage/4)))
	if worn.ItemID != chestplate || worn.Damage != int32(perPiece) {
		t.Errorf("the chestplate after one turned blow = %+v, want wear %d", worn, perPiece)
	}
}

// TestEatingBreadRestoresTheFoodBar checks the whole meal: a hungry survival
// player with bread in hand begins eating on the right click, the others see
// the hand state that means it, and after the thirty-two ticks a meal takes,
// the nutrition lands on the food bar with the component's own saturation
// behind it - capped by the bar the meal left - and the bread is gone, with
// the burp to finish.
func TestEatingBreadRestoresTheFoodBar(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	// A player who arrives ten shanks fed: hungry enough to be allowed a
	// meal, fed enough that nothing else happens to the bars while the
	// test waits out one.
	root := nbt.Compound{
		"playerGameType":      nbt.Int(0),
		"Health":              nbt.Float(20),
		"foodLevel":           nbt.Int(10),
		"foodSaturationLevel": nbt.Float(0),
		"foodExhaustionLevel": nbt.Float(0),
	}
	data, err := nbt.Encode("", root)
	if err != nil {
		t.Fatalf("encode player file: %v", err)
	}
	path := servertest.PlayerFile(cfg.LevelName, "Eater")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create player directory: %v", err)
	}
	if err := servertest.WriteGzip(path, data); err != nil {
		t.Fatalf("write player file: %v", err)
	}

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	eater := connect()
	eaterID := eater.JoinAs("Eater")

	watcher.ExpectPlayerAdded("Eater")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Eater")

	servertest.HoldTicks(t, srv)

	bread := servertest.MustItemID(t, "minecraft:bread")
	food, ok := registry.FoodOf(bread)
	if !ok {
		t.Fatal("bread is not food in this version's components")
	}
	srv.GiveSlot("Eater", player.HotbarSlot, protocol.ItemStack{ItemID: bread, Count: 1})

	// The right click with the bread in hand: a meal begins, and the hand
	// state that has everyone watching draw the eating goes out on the
	// metadata.
	eater.UseItemRightClicks(protocol.HandMain)
	eater.Settle()
	if !srv.Eating("Eater") {
		t.Fatal("the meal begins with the Use Item packet")
	}
	watcher.ExpectHandState(eaterID, protocol.EntityFlagHandActive)

	// Thirty-two game ticks later - 1.6 seconds, the meal's full length -
	// the bars arrive with the bread in them: five shanks, and a saturation
	// of six capped by the fifteen the bar now reads.
	// vanilla: Consumables.defaultFood (1.6 seconds, 32 ticks).
	servertest.UntilTicks(t, srv, 32+5, "the meal completes", func() bool { return !srv.Eating("Eater") })
	got := eater.ExpectHealthish()
	if got.Food != 10+food.Nutrition {
		t.Errorf("food level after the meal = %d, want %d (ten plus the bread's %d)",
			got.Food, 10+food.Nutrition, food.Nutrition)
	}
	wantSat := min(float64(food.Saturation), float64(10+food.Nutrition))
	if math.Abs(float64(got.Saturation)-wantSat) > 0.01 {
		t.Errorf("saturation after the meal = %v, want %v (the bread's %v, capped by the food bar)",
			got.Saturation, wantSat, food.Saturation)
	}
	if got.Health != 20 {
		t.Errorf("health after the meal = %v, want it untouched at 20", got.Health)
	}

	// The meal is over and the bread is gone: one item, eaten.
	eater.DrainSurvivalTraffic()
	if srv.Eating("Eater") {
		t.Error("the meal is still in progress after the bread should have been eaten")
	}
	if eaten := srv.InventorySlot("Eater", player.HotbarSlot); !eaten.Empty() {
		t.Errorf("the bread after the meal = %+v, want it eaten", eaten)
	}
}

// hungryPlayerFile is the player file a test that is about a meal begins
// with: survival, fed ten shanks - hungry enough for a meal, fed enough
// that nothing else happens to the bars while it runs - and nothing behind
// the food at all.
func mealedPlayerFile(t *testing.T, cfg *config.Config, name string) {
	t.Helper()
	root := nbt.Compound{
		"playerGameType":      nbt.Int(0),
		"Health":              nbt.Float(20),
		"foodLevel":           nbt.Int(10),
		"foodSaturationLevel": nbt.Float(0),
		"foodExhaustionLevel": nbt.Float(0),
	}
	data, err := nbt.Encode("", root)
	if err != nil {
		t.Fatalf("encode player file: %v", err)
	}
	path := servertest.PlayerFile(cfg.LevelName, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create player directory: %v", err)
	}
	if err := servertest.WriteGzip(path, data); err != nil {
		t.Fatalf("write player file: %v", err)
	}
}

// TestHoldingTheUseFinishesTheMeal checks the end of a meal the client
// holds the button through: the vanilla server completes the use when the
// item's consume time runs out, and what it tells the eater's own client is
// the entity event the client completes its own use on - without which the
// client keeps chewing forever, waiting for a use that never ends. The
// event arrives with the food applied, the item gone from the hand, and the
// sounds a finished meal makes - the burp the eater hears too, and the last
// bite in the neutral voice the food's own completion is played in.
func TestHoldingTheUseFinishesTheMeal(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	mealedPlayerFile(t, cfg, "Eater")

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)

	watcher := connect().Join("Watcher")

	eater := connect()
	eaterID := eater.JoinAs("Eater")

	watcher.ExpectPlayerAdded("Eater")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Eater")

	bread := servertest.MustItemID(t, "minecraft:bread")
	srv.GiveSlot("Eater", player.HotbarSlot, protocol.ItemStack{ItemID: bread, Count: 2})

	eater.UseItemRightClicks(protocol.HandMain)
	eater.Settle()
	if !srv.Eating("Eater") {
		t.Fatal("the meal begins with the Use Item packet")
	}
	watcher.ExpectHandState(eaterID, protocol.EntityFlagHandActive)

	// A meal is thirty-two ticks; the thirty-fifth is safely past it.
	tick(35)

	// The eater's own client is told the use is over, in the entity event
	// the vanilla server sends the eating player alone.
	eater.ExpectEventOn(eaterID, protocol.EntityStatusUseFinished)

	// The food landed, the bread is one shorter, and the sounds of the end
	// reached the eater: the neutral-voiced last bite and the burp.
	heard, counts, _ := servertest.CollectSoundsSettled(eater)
	if n := servertest.CountSounds(heard, "minecraft:entity.generic.eat", protocol.SoundCategoryNeutral); n != 1 {
		t.Errorf("the eater heard %d neutral-voiced eat sounds at the meal's end, want the one the food's completion plays", n)
	}
	if n := servertest.CountSounds(heard, "minecraft:entity.player.burp", protocol.SoundCategoryPlayer); n != 1 {
		t.Errorf("the eater heard %d burps, want the one a finished meal ends in", n)
	}
	if n := servertest.CountSounds(heard, "minecraft:entity.generic.eat", protocol.SoundCategoryPlayer); n != 0 {
		t.Errorf("the eater heard %d of the meal's bites in the players' voice, want none: those are played at the eating body, whose own client makes them for itself", n)
	}
	if counts[protocol.IDPlayContainerSlot] == 0 {
		t.Errorf("no container slot arrived for the eaten bread")
	}
	if got := srv.InventorySlot("Eater", player.HotbarSlot); got.ItemID != bread || got.Count != 1 {
		t.Errorf("the bread after the meal = %+v, want one of the two eaten", got)
	}

	// The others hear the meal being eaten: the bites of it in the
	// players' voice - the six the rule sounds once the meal is past its
	// first fifth, and the last one at its end - and the food's own
	// completion in the neutral voice, and the burp.
	heard, _, handStates := servertest.CollectSoundsSettled(watcher)
	if n := servertest.CountSounds(heard, "minecraft:entity.generic.eat", protocol.SoundCategoryPlayer); n < 6 {
		t.Errorf("the watcher heard %d players-voiced eat sounds, want the six bites and the meal's last one", n)
	}
	for _, s := range heard {
		if s.Name == "minecraft:entity.generic.eat" && s.Category == protocol.SoundCategoryPlayer {
			if s.Volume != 0.5 && s.Volume != 1.0 {
				t.Errorf("a bite played at volume %v, want the half or the whole the dice make it", s.Volume)
			}
		}
	}
	if n := servertest.CountSounds(heard, "minecraft:entity.generic.eat", protocol.SoundCategoryNeutral); n != 1 {
		t.Errorf("the watcher heard %d neutral-voiced eat sounds, want one at the meal's end", n)
	}
	if n := servertest.CountSounds(heard, "minecraft:entity.player.burp", protocol.SoundCategoryPlayer); n != 1 {
		t.Errorf("the watcher heard %d burps, want the one a finished meal ends in", n)
	}

	// The hand comes down on the watchers too, in so many words: the
	// hand-state byte back to nothing, which an entry left out would never
	// say.
	if got := handStates[eaterID]; got != 0 {
		t.Errorf("the watcher was left with the eater's hand state %#02x, want it sent back down to nothing", got)
	}
}

// TestBucketPicksUpAndPlacesTheWater checks the fluid round trip: an empty
// bucket scoops on Use Item - the packet vanilla's BucketItem.use rides -
// while Use Item On answers with the echoes alone, and the full one clicked
// against the ground pours its source out over it and comes up empty - the
// poured water handed to the physics engine, which is what has it flow away
// over the stone.
func TestBucketPicksUpAndPlacesTheWater(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Pourer")

	bucket := servertest.MustItemID(t, "minecraft:bucket")
	waterBucket := servertest.MustItemID(t, "minecraft:water_bucket")
	water, hasWater := registry.FindState("minecraft:water", nil)
	if !hasWater {
		t.Fatal("the block registry has no water")
	}
	if name, props, _ := registry.BlockStateName(water); name != "minecraft:water" ||
		registry.ParseStateProps(props)["level"] != "0" {
		t.Fatal("the block registry's water default is not a source")
	}
	srv.GiveSlot("Pourer", player.HotbarSlot, protocol.ItemStack{ItemID: bucket, Count: 1})

	// The water in the ground where the flat world had stone, one block out
	// of the way. A Use Item On with the empty bucket is only echoed back:
	// the scoop is not this packet's.
	if !srv.SetBlock(4, 63, 4, water) {
		t.Fatal("the world did not take the water")
	}
	c.Settle() // the water's own update crosses the wire before the uses read it
	c.MoveStep(4.5, 64, 4.5, true)
	c.PlaceBlock(4, 63, 4, protocol.FaceTop)
	c.ExpectUseEchoes(4, 63, 4, protocol.FaceTop)
	if got := srv.BlockAt(4, 63, 4); got != water {
		t.Errorf("the water after a Use Item On = %d, want it still there", got)
	}
	if held := srv.InventorySlot("Pourer", player.HotbarSlot); held.ItemID != bucket {
		t.Errorf("the hand after a Use Item On = %+v, want the empty bucket still", held)
	}

	// The scoop rides Use Item, aimed straight down at the water under the
	// player. The block comes up empty and the bucket full.
	servertest.UseHeld(c, 0, 90)
	got := c.ExpectBucketUseResult()
	if got.Update.X != 4 || got.Update.Y != 63 || got.Update.Z != 4 {
		t.Errorf("the scoop changed block (%d, %d, %d), want the water itself", got.Update.X, got.Update.Y, got.Update.Z)
	}
	if got.Update.State != 0 {
		t.Errorf("block after the scoop = %d, want air; the source left with the bucket", got.Update.State)
	}
	if got.Slot.ItemID != waterBucket || got.Slot.Count != 1 {
		t.Errorf("the hand after the scoop = %+v, want one water bucket", got.Slot)
	}
	if held := srv.InventorySlot("Pourer", player.HotbarSlot); held.ItemID != waterBucket || held.Count != 1 {
		t.Errorf("the server's hand after the scoop = %+v, want one water bucket", held)
	}

	// The pour rides Use Item the same way the scoop does, aimed straight
	// down at the steel under the player - the face the water comes out
	// over - and the source lands on it. vanilla: BucketItem.use
	c.MoveStep(5.5, 64, 4.5, true)
	servertest.UseHeld(c, 0, 90)
	got = c.ExpectBucketUseResult()
	if got.Update.X != 5 || got.Update.Y != 64 || got.Update.Z != 4 {
		t.Errorf("the pour changed block (%d, %d, %d), want the block over the face the ray stopped on", got.Update.X, got.Update.Y, got.Update.Z)
	}
	if got.Update.State != water {
		t.Errorf("block after the pour = %d, want %d (a water source)", got.Update.State, water)
	}
	if got.Slot.ItemID != bucket || got.Slot.Count != 1 {
		t.Errorf("the hand after the pour = %+v, want the empty bucket back", got.Slot)
	}
	if held := srv.InventorySlot("Pourer", player.HotbarSlot); held.ItemID != bucket || held.Count != 1 {
		t.Errorf("the server's hand after the pour = %+v, want the empty bucket back", held)
	}
}

// TestThePourWaterlogsTheHitBlock: a water bucket aimed at a waterloggable
// block fills that block's own cell with its waterlogged state rather than
// the cell over the struck face, which is what a LiquidBlockContainer that
// canPlaceLiquid takes. vanilla: BucketItem.use,
// SimpleWaterloggedBlock.canPlaceLiquid
func TestThePourWaterlogsTheHitBlock(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Dial(t, cfg)
	srv.StopTick()
	c.Join("Pourer")

	bucket := servertest.MustItemID(t, "minecraft:bucket")
	waterBucket := servertest.MustItemID(t, "minecraft:water_bucket")
	slab := registry.MustState("minecraft:oak_slab", nil)
	waterlogged, ok := registry.FindState("minecraft:oak_slab", map[string]string{"waterlogged": "true"})
	if !ok {
		t.Fatal("the block registry has no waterlogged oak slab")
	}
	srv.GiveSlot("Pourer", player.HotbarSlot, protocol.ItemStack{ItemID: waterBucket, Count: 1})
	if !srv.SetBlock(4, 63, 4, slab) {
		t.Fatal("the world did not take the slab")
	}
	c.Settle()
	c.MoveStep(4.5, 64, 4.5, true)

	// The look straight down strikes the slab's top; the water goes into the
	// slab itself, leaving the cell over it air.
	servertest.UseHeld(c, 0, 90)
	got := c.ExpectBucketUseResult()
	if got.Update.X != 4 || got.Update.Y != 63 || got.Update.Z != 4 || got.Update.State != waterlogged {
		t.Errorf("the pour changed block (%d,%d,%d) = %d, want the waterlogged slab at (4,63,4) = %d",
			got.Update.X, got.Update.Y, got.Update.Z, got.Update.State, waterlogged)
	}
	if state := srv.BlockAt(4, 63, 4); state != waterlogged {
		t.Errorf("the slab after the pour = %d, want the waterlogged state %d", state, waterlogged)
	}
	if above := srv.BlockAt(4, 64, 4); above != 0 {
		t.Errorf("the cell over the slab = %d, want air; the water went into the slab", above)
	}
	if held := srv.InventorySlot("Pourer", player.HotbarSlot); held.ItemID != bucket || held.Count != 1 {
		t.Errorf("the hand after the pour = %+v, want the empty bucket", held)
	}
}

// TestTheScoopDrainsAWaterloggedBlock: an empty bucket aimed at a
// waterlogged block takes the water out of the block's own cell, leaving
// the block's dry state rather than air, and comes back full. vanilla:
// BucketItem.use, SimpleWaterloggedBlock.pickupBlock
func TestTheScoopDrainsAWaterloggedBlock(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		block    string
		dry      map[string]string
		creative bool
	}{
		{name: "minecraft:oak_slab", block: "minecraft:oak_slab"},
		{name: "minecraft:oak_stairs", block: "minecraft:oak_stairs"},
		{name: "minecraft:chest", block: "minecraft:chest"},
		{name: "minecraft:oak_fence", block: "minecraft:oak_fence"},
		{name: "minecraft:oak_trapdoor", block: "minecraft:oak_trapdoor"},
		{
			// A double slab carries the waterlogged property even though a
			// pour refuses to fill one; water in it is still scooped out.
			name:  "minecraft:oak_slab_double",
			block: "minecraft:oak_slab",
			dry:   map[string]string{"type": "double"},
		},
		{name: "creative_slab", block: "minecraft:oak_slab", creative: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.OnlineMode = false
			if tc.creative {
				cfg.GameMode = "creative"
			}

			srv, c := servertest.Dial(t, cfg)
			srv.StopTick()
			c.Join("Scooper")

			bucket := servertest.MustItemID(t, "minecraft:bucket")
			waterBucket := servertest.MustItemID(t, "minecraft:water_bucket")
			wet := map[string]string{"waterlogged": "true"}
			maps.Copy(wet, tc.dry)
			dry := registry.MustState(tc.block, tc.dry)
			waterlogged := registry.MustState(tc.block, wet)
			srv.GiveSlot("Scooper", player.HotbarSlot, protocol.ItemStack{ItemID: bucket, Count: 1})
			if !srv.SetBlock(4, 63, 4, waterlogged) {
				t.Fatalf("the world did not take the waterlogged %s", tc.block)
			}
			c.Settle()
			c.MoveStep(4.5, 64, 4.5, true)

			// The look straight down strikes the waterlogged block; the
			// water goes back into the bucket and the block keeps its cell.
			// A creative hand keeps the bucket and the filled result adds
			// the water bucket beside it.
			wantSlot := player.HotbarSlot
			if tc.creative {
				wantSlot = player.HotbarSlot + 1
			}
			servertest.UseHeld(c, 0, 90)
			got := c.CollectBucketResult(wantSlot)
			if got.Update.X != 4 || got.Update.Y != 63 || got.Update.Z != 4 || got.Update.State != dry {
				t.Errorf("the scoop changed block (%d,%d,%d) = %d, want the dry %s at (4,63,4) = %d",
					got.Update.X, got.Update.Y, got.Update.Z, got.Update.State, tc.block, dry)
			}
			if state := srv.BlockAt(4, 63, 4); state != dry {
				t.Errorf("the %s after the scoop = %d, want the dry state %d", tc.block, state, dry)
			}
			if tc.creative {
				if held := srv.InventorySlot("Scooper", player.HotbarSlot); held.ItemID != bucket || held.Count != 1 {
					t.Errorf("the hand after the creative scoop = %+v, want the bucket kept", held)
				}
				if added := got.Slots[int16(player.HotbarSlot+1)]; added.ItemID != waterBucket || added.Count != 1 {
					t.Errorf("the filled result = %+v, want one water bucket in the next slot", added)
				}
				return
			}
			if held := srv.InventorySlot("Scooper", player.HotbarSlot); held.ItemID != waterBucket || held.Count != 1 {
				t.Errorf("the hand after the scoop = %+v, want one water bucket", held)
			}
		})
	}
}

// TestAWaterPourEvaporatesInTheNether checks the nether's water-evaporating
// dimension type: a water bucket poured in the nether places no source - the
// extinguish hiss rises from the cell instead, and the hand still empties.
// The watcher hears the hiss the pourer's own client predicts for itself;
// both see the server's large smoke. vanilla: BucketItem.emptyContents
func TestAWaterPourEvaporatesInTheNether(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)

	pourer := connect().Join("Pourer")

	watcher := connect().Join("Watcher")
	pourer.ExpectPlayerAdded("Watcher")
	pourer.ExpectAnnouncement("multiplayer.player.joined", "Watcher")

	waterBucket := servertest.MustItemID(t, "minecraft:water_bucket")
	bucket := servertest.MustItemID(t, "minecraft:bucket")
	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("the block registry has no water")
	}
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	extinguish, ok := registry.SoundEventID("minecraft:block.fire.extinguish")
	if !ok {
		t.Fatal("the sound registry has no block.fire.extinguish")
	}

	carryIntoNether(t, srv, pourer, "Pourer", 8, 8)
	carryIntoNether(t, srv, watcher, "Watcher", 10, 10)

	// The stone the pour's ray stops on, and the air its top face opens
	// onto: the cell a normal pour would fill.
	if !srv.SetBlockInDimension(world.NetherName, 8, 39, 8, stone) {
		t.Fatal("the nether did not take the stone")
	}
	srv.SetBlockInDimension(world.NetherName, 8, 40, 8, 0)
	srv.GiveSlot("Pourer", player.HotbarSlot, protocol.ItemStack{ItemID: waterBucket, Count: 1})
	pourer.Settle()
	watcher.Settle()

	servertest.UseHeld(pourer, 0, 90)
	got := servertest.CollectBucketPourEffects(pourer)

	for _, pos := range [][3]int32{{8, 39, 8}, {8, 40, 8}} {
		if state := srv.BlockInDimension(world.NetherName, pos[0], pos[1], pos[2]); state == water {
			t.Errorf("the nether pour left water at (%d,%d,%d); the dimension should evaporate it", pos[0], pos[1], pos[2])
		}
	}
	if held := srv.InventorySlot("Pourer", player.HotbarSlot); held.ItemID != bucket || held.Count != 1 {
		t.Errorf("the hand after the nether pour = %+v, want the empty bucket", held)
	}
	if got.Particles != 1 {
		t.Errorf("the pourer saw %d particle packets, want the one large smoke", got.Particles)
	}
	if got.Slots[player.HotbarSlot].ItemID != bucket {
		t.Errorf("the pourer's slot correction = %+v, want the empty bucket", got.Slots[player.HotbarSlot])
	}
	if n := servertest.CountSound(got, extinguish); n != 0 {
		t.Errorf("the pourer heard %d extinguish hisses, want none: its own client predicts the sound", n)
	}

	heard := servertest.CollectBucketPourEffects(watcher)
	if n := servertest.CountSound(heard, extinguish); n != 1 {
		t.Errorf("the watcher heard %d extinguish hisses, want one; sounds %v", n, heard.Sounds)
	}
	if heard.Particles != 1 {
		t.Errorf("the watcher saw %d particle packets, want the one large smoke", heard.Particles)
	}

	pourer.Disconnect()
	watcher.Disconnect()
}

// scoopScene digs the flat ground's stone out for a pool at the given east
// offset, pours the source into it, and stands the player in the cell west
// of it, the eye 1.62 over their feet. The state the pool holds comes back.
func scoopScene(t *testing.T, srv *server.Server, c *servertest.Client, poolX int32) uint16 {
	t.Helper()
	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("the block registry has no water")
	}
	if !srv.SetBlock(poolX, 63, 4, water) {
		t.Fatal("the world did not take the pool")
	}
	c.Settle() // the pool's own update crosses the wire before the looks read it
	c.MoveStep(4.5, 64, 4.5, true)
	return water
}

// sawBlockChange collects what one settle carries and reports whether a
// block update crossed the wire, the read a refused use is checked with.
func sawBlockChange(c *servertest.Client) bool {
	c.T.Helper()
	saw := false
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayBlockUpdate {
			saw = true
		}
	})
	return saw
}

// carryIntoNether moves a client's player into the nether at the spot given,
// y 40, and waits for the crossing to arrive, leaving its packets for the
// next read to step over. The cells the player stands in are cleared first:
// the nether's rock may fill them.
func carryIntoNether(t *testing.T, srv *server.Server, c *servertest.Client, name string, x, z float64) {
	t.Helper()
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, int32(x), int32(z)))
	for _, cell := range [][2]int32{{0, 0}, {-1, 0}, {0, -1}, {-1, -1}} {
		for y := int32(40); y <= 41; y++ {
			srv.SetBlockInDimension(world.NetherName, int32(x)+cell[0], y, int32(z)+cell[1], 0)
		}
	}
	srv.MovePlayerToDimension(name, world.NetherName, x, 40, z)
	if !c.Pump(servertest.JoinTimeout, nil, func(int32, []byte) bool {
		return srv.PlayerDimension(name) == world.NetherName &&
			srv.ChunkLoadedInDimension(world.NetherName, int32(x), int32(z))
	}) {
		t.Fatalf("%s never arrived in the nether", name)
	}
}
