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
