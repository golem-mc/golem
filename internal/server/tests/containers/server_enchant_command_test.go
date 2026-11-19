package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The /enchant command, seen from the wire: the held stack comes back with
// the enchantment at the level the command named, the runner's own client
// hears about it as the slot's update and every watcher as the equipment,
// and the refusals vanilla answers with arrive as the bare red failures —
// the incompatible, the itemless, the non-living target, the level past what
// the enchantment reaches.

// enchantNameTextForTest is an enchantment's name with its level spelled the
// way the success feedback carries it: the description in gray, followed by
// the level's numerals, which a lone level of a single-level enchantment
// does not carry.
// vanilla: Enchantment.getFullname
func enchantNameTextForTest(t *testing.T, enchantment string, level int32) protocol.Text {
	t.Helper()
	id, ok := registry.EnchantmentID(enchantment)
	if !ok {
		t.Fatalf("the enchantment registry has no %s", enchantment)
	}
	def, ok := registry.EnchantmentEffectsByID(id)
	if !ok {
		t.Fatalf("the enchantment registry has no effects for %s", enchantment)
	}
	name := protocol.Text{
		Translate: "enchantment." + strings.Replace(enchantment, ":", ".", 1),
		Color:     protocol.ColorGray,
	}
	if registry.EnchantmentInTag(id, "minecraft:curse") {
		name.Color = protocol.ColorRed
	}
	if level != 1 || def.MaxLevel != 1 {
		name.Extra = []protocol.Text{
			{Text: " "},
			{Translate: "enchantment.level." + strconv.Itoa(int(level))},
		}
	}
	return name
}

// awaitEquipmentHolding reads packets until the equipment packet for the
// entity names a main-hand stack the want predicate accepts, and returns it.
func awaitEquipmentHolding(t *testing.T, c *servertest.Client, entityID int32, want func(protocol.ItemStack) bool) protocol.ItemStack {
	t.Helper()
	var held protocol.ItemStack
	if c.Pump(servertest.ExpectTimeout, nil, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityEquipment {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != entityID {
			return false
		}
		for {
			raw := r.ReadU8()
			stack, ok := r.ReadSlot()
			if !ok {
				return false
			}
			if raw&0x7F == 0 && want(stack) {
				held = stack
				return true
			}
			if raw&0x80 == 0 {
				return false
			}
		}
	}) {
		return held
	}
	t.Fatalf("no equipment carrying the want arrived for entity %d", entityID)
	return protocol.ItemStack{}
}

// TestEnchantEnchantsTheHeldItem checks /enchant on the runner's own held
// sword: the stack gains the enchantment at the level named, which the
// runner's own client is told as the hotbar slot's update and the watcher as
// the equipment the body holds; a second enchantment behind no level lands
// at level one, and the feedback names the enchantment with its level and
// the target.
func TestEnchantEnchantsTheHeldItem(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// /enchant is a gamemaster's command, so Bob, the runner, is an
	// operator; Alice stands in the world to watch what his body comes
	// to hold.
	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Bob"))

	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	bobID := alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	sword, ok := registry.ItemID("minecraft:diamond_sword")
	if !ok {
		t.Fatal("the registry has no diamond sword")
	}
	sharpness, ok := registry.EnchantmentID("minecraft:sharpness")
	if !ok {
		t.Fatal("the registry has no sharpness")
	}
	unbreaking, ok := registry.EnchantmentID("minecraft:unbreaking")
	if !ok {
		t.Fatal("the registry has no unbreaking")
	}

	bob.ChatCommand("give @s minecraft:diamond_sword")
	updates, _ := servertest.AwaitSlotUpdates(t, bob, 1)
	if updates[0].Slot != player.HotbarSlot || updates[0].Stack.ItemID != sword || len(updates[0].Stack.Enchantments) != 0 {
		t.Errorf("give slot update = %+v, want a bare sword in the hotbar", updates[0])
	}

	// A level of three rides the word the command names.
	bob.ChatCommand("enchant @s sharpness 3")
	updates, chats := servertest.AwaitSlotUpdates(t, bob, 1)
	if updates[0].Slot != player.HotbarSlot || updates[0].Stack.ItemID != sword {
		t.Errorf("enchant slot update = %+v, want the sword still held", updates[0])
	}
	if got := updates[0].Stack.Enchantments[sharpness]; got != 3 {
		t.Errorf("enchant slot update carries sharpness %d, want level 3", got)
	}
	servertest.ExpectFeedbackSoon(t, bob, chats, "commands.enchant.success.single",
		enchantNameTextForTest(t, "minecraft:sharpness", 3),
		servertest.PlayerDisplayComponent("Bob"))

	// The watcher is told about the enchanted equipment the body holds.
	held := awaitEquipmentHolding(t, alice, bobID, func(stack protocol.ItemStack) bool {
		return stack.Enchantments[sharpness] == 3
	})
	if held.ItemID != sword || held.Count != 1 {
		t.Errorf("watched equipment = %+v, want one diamond sword", held)
	}

	// A second, compatible enchantment behind no level word lands at one,
	// beside the three the sword already carries, and its feedback spells
	// the level out.
	bob.ChatCommand("enchant @s unbreaking")
	updates, chats = servertest.AwaitSlotUpdates(t, bob, 1)
	if len(updates[0].Stack.Enchantments) != 2 {
		t.Errorf("enchant slot update = %+v, want two enchantments", updates[0])
	}
	if got := updates[0].Stack.Enchantments[sharpness]; got != 3 {
		t.Errorf("enchant slot update carries sharpness %d, want level 3 still", got)
	}
	if got := updates[0].Stack.Enchantments[unbreaking]; got != 1 {
		t.Errorf("enchant slot update carries unbreaking %d, want level 1", got)
	}
	servertest.ExpectFeedbackSoon(t, bob, chats, "commands.enchant.success.single",
		enchantNameTextForTest(t, "minecraft:unbreaking", 1),
		servertest.PlayerDisplayComponent("Bob"))

	held = awaitEquipmentHolding(t, alice, bobID, func(stack protocol.ItemStack) bool {
		return stack.Enchantments[unbreaking] == 1 && stack.Enchantments[sharpness] == 3
	})
	if held.ItemID != sword {
		t.Errorf("watched equipment = %+v, want the diamond sword", held)
	}
	bob.Disconnect()
	alice.Disconnect()
}

// TestEnchantRefusesAnItemOutsideTheSupportedSet checks the incompatible
// refusal for an item the enchantment never lands on: dirt is no weapon, so
// sharpness will not take it, and the refusal names the dirt in hand.
func TestEnchantRefusesAnItemOutsideTheSupportedSet(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// /enchant is a gamemaster's command, so the runner is listed as an
	// operator.
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("give @s minecraft:dirt")
	servertest.AwaitSlotUpdates(t, c, 1)
	c.ChatCommand("enchant @s sharpness 1")
	servertest.AwaitItemFailure(t, c, "commands.enchant.failed.incompatible",
		protocol.Text{Text: "Dirt"})
	c.Disconnect()
}

// TestEnchantRefusesAnExclusiveEnchantment checks the incompatible refusal
// for an enchantment that will not share the item with one already on it: bane
// of arthropods does not sit beside sharpness, whose exclusive set it is one
// of, and the refusal names the sword itself.
func TestEnchantRefusesAnExclusiveEnchantment(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// /enchant is a gamemaster's command, so the runner is listed as an
	// operator.
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("give @s minecraft:diamond_sword[enchantments={sharpness:5}]")
	servertest.AwaitSlotUpdates(t, c, 1)
	c.ChatCommand("enchant @s bane_of_arthropods 2")
	servertest.AwaitItemFailure(t, c, "commands.enchant.failed.incompatible",
		protocol.Text{Text: "Diamond Sword"})
	c.Disconnect()
}

// TestEnchantRefusesABareHandAndALevelTooHigh checks the itemless refusal and
// that a level past what the enchantment reaches is refused wherever the
// targets stand, the level check coming before anything reads a hand.
func TestEnchantRefusesABareHandAndALevelTooHigh(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// /enchant is a gamemaster's command, so the runner is listed as an
	// operator.
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	// Nothing is held, and the refusal names the runner.
	c.ChatCommand("enchant @s sharpness 3")
	servertest.AwaitItemFailure(t, c, "commands.enchant.failed.itemless",
		protocol.Text{Text: "Testificate"})

	// A level past the five sharpness reaches is refused with the level
	// and the five, bare hand or not.
	c.ChatCommand("enchant @s sharpness 6")
	servertest.AwaitItemFailure(t, c, "commands.enchant.failed.level",
		servertest.NumberText(6), servertest.NumberText(5))
	c.Disconnect()
}

// TestEnchantRefusesATargetThatIsNotLiving checks the entity refusal: an
// item lying in the world holds nothing that could be enchanted, and the
// single-target form names it and gives up.
func TestEnchantRefusesATargetThatIsNotLiving(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// /enchant is a gamemaster's command, so the runner is listed as an
	// operator.
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand(`summon minecraft:item 6.5 64 6.5 {Item:{id:"minecraft:stone",count:1}}`)
	c.ChatCommand("enchant @e[type=minecraft:item,limit=1] sharpness 1")
	servertest.AwaitItemFailure(t, c, "commands.enchant.failed.entity",
		protocol.Text{Text: "Stone"})
	c.Disconnect()
}

// TestEnchantTargetsSeveralPlayers checks the several-target form: a run
// with two bare hands to take the enchantment answers that nothing happened,
// and one where both players hold a sword enchants them both, counting them
// in the feedback instead of naming them.
func TestEnchantTargetsSeveralPlayers(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// /enchant is a gamemaster's command, so Alice, the runner, is an
	// operator; Bob is only ever enchanted.
	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))

	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	sharpness, ok := registry.EnchantmentID("minecraft:sharpness")
	if !ok {
		t.Fatal("the registry has no sharpness")
	}

	// Neither player holds anything, so no target takes the enchantment.
	alice.ChatCommand("enchant @a sharpness 2")
	servertest.AwaitCommandFailure(t, alice, "commands.enchant.failed")

	alice.ChatCommand("give @s minecraft:diamond_sword")
	servertest.AwaitSlotUpdates(t, alice, 1)
	alice.ChatCommand("give Bob minecraft:diamond_sword")
	servertest.AwaitSlotUpdates(t, bob, 1)

	// Both players take the enchantment, each in their own hotbar, and the
	// feedback counts the two targets.
	alice.ChatCommand("enchant @a sharpness 2")
	updates, chats := servertest.AwaitSlotUpdates(t, alice, 1)
	if got := updates[0].Stack.Enchantments[sharpness]; got != 2 {
		t.Errorf("enchant slot update carries sharpness %d, want level 2", got)
	}
	servertest.ExpectFeedbackSoon(t, alice, chats, "commands.enchant.success.multiple",
		enchantNameTextForTest(t, "minecraft:sharpness", 2),
		servertest.NumberText(2))
	updates, _ = servertest.AwaitSlotUpdates(t, bob, 1)
	if got := updates[0].Stack.Enchantments[sharpness]; got != 2 {
		t.Errorf("enchant slot update carries sharpness %d, want level 2", got)
	}

	alice.Disconnect()
	bob.Disconnect()
}

// TestEnchantRefusesTheEnchantmentAlreadyHeld checks the incompatible
// refusal for the enchantment the item already carries: vanilla's
// areCompatible answers false for the same enchantment, so a second
// sharpness is refused and the sword itself is named.
func TestEnchantRefusesTheEnchantmentAlreadyHeld(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("give @s minecraft:diamond_sword[enchantments={sharpness:3}]")
	servertest.AwaitSlotUpdates(t, c, 1)
	c.ChatCommand("enchant @s sharpness 2")
	servertest.AwaitItemFailure(t, c, "commands.enchant.failed.incompatible",
		protocol.Text{Text: "Diamond Sword"})
	c.Disconnect()
}

// TestEnchantEnchantsAMobHeldWeapon checks a mob target: the cow's held
// sword gains the enchantment at the level named, and every watcher is told
// as the equipment the body holds.
func TestEnchantEnchantsAMobHeldWeapon(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	cow, ok := registry.EntityTypeID("minecraft:cow")
	if !ok {
		t.Fatal("the registry has no cow")
	}
	sword, ok := registry.ItemID("minecraft:diamond_sword")
	if !ok {
		t.Fatal("the registry has no diamond sword")
	}
	sharpness, ok := registry.EnchantmentID("minecraft:sharpness")
	if !ok {
		t.Fatal("the registry has no sharpness")
	}

	c.ChatCommand(`summon minecraft:cow 6.5 64 6.5 {NoAI:1b,equipment:{mainhand:{id:"minecraft:diamond_sword",count:1}}}`)
	mobID := c.ExpectSummonedMob(cow)
	c.ChatCommand("enchant @e[type=minecraft:cow,limit=1] sharpness 2")
	held := awaitEquipmentHolding(t, c, mobID, func(stack protocol.ItemStack) bool {
		return stack.Enchantments[sharpness] == 2
	})
	if held.ItemID != sword || held.Count != 1 {
		t.Errorf("watched equipment = %+v, want one diamond sword", held)
	}
	c.Disconnect()
}

// TestEnchantColoursACurseRed checks the feedback's curse name: a curse
// wears the red its translation is given rather than the gray of the other
// enchantments.
func TestEnchantColoursACurseRed(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("give @s minecraft:diamond_helmet")
	servertest.AwaitSlotUpdates(t, c, 1)
	c.ChatCommand("enchant @s binding_curse")
	_, chats := servertest.AwaitSlotUpdates(t, c, 1)
	servertest.ExpectFeedbackSoon(t, c, chats, "commands.enchant.success.single",
		enchantNameTextForTest(t, "minecraft:binding_curse", 1),
		servertest.PlayerDisplayComponent("Testificate"))
	c.Disconnect()
}

// TestEnchantAtLevelZeroChangesNothing checks that a zero level succeeds
// without touching the held stack, the upgrade merge of a zero leaving it as
// it was.
func TestEnchantAtLevelZeroChangesNothing(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("give @s minecraft:diamond_sword")
	servertest.AwaitSlotUpdates(t, c, 1)
	c.ChatCommand("enchant @s sharpness 0")
	servertest.AwaitCommandFeedbackText(t, c, "commands.enchant.success.single",
		enchantNameTextForTest(t, "minecraft:sharpness", 0),
		servertest.PlayerDisplayComponent("Testificate"))
	if held := srv.InventorySlot("Testificate", player.HotbarSlot); len(held.Enchantments) != 0 {
		t.Errorf("a zero level left the sword with %v", held.Enchantments)
	}
	c.Disconnect()
}

// TestEnchantSkipsTargetsThatCannotTakeIt checks the several-target form
// where one target cannot take the enchantment: it is skipped rather than
// refusing the run, and the feedback names the one target it enchanted.
func TestEnchantSkipsTargetsThatCannotTakeIt(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))

	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	sharpness, ok := registry.EnchantmentID("minecraft:sharpness")
	if !ok {
		t.Fatal("the registry has no sharpness")
	}

	// Alice holds a sword sharpness can take; Bob holds dirt it cannot.
	alice.ChatCommand("give @s minecraft:diamond_sword")
	servertest.AwaitSlotUpdates(t, alice, 1)
	alice.ChatCommand("give Bob minecraft:dirt")
	servertest.AwaitSlotUpdates(t, bob, 1)

	alice.ChatCommand("enchant @a sharpness 2")
	updates, chats := servertest.AwaitSlotUpdates(t, alice, 1)
	if got := updates[0].Stack.Enchantments[sharpness]; got != 2 {
		t.Errorf("enchant slot update carries sharpness %d, want level 2", got)
	}
	servertest.ExpectFeedbackSoon(t, alice, chats, "commands.enchant.success.single",
		enchantNameTextForTest(t, "minecraft:sharpness", 2),
		servertest.PlayerDisplayComponent("Alice"))

	// The skipped target's dirt is left as it was.
	if held := srv.InventorySlot("Bob", player.HotbarSlot); len(held.Enchantments) != 0 {
		t.Errorf("the skipped target's dirt carries %v", held.Enchantments)
	}

	alice.Disconnect()
	bob.Disconnect()
}
