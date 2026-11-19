package containers

// The container's lock, vanilla's LockCode: a lock laid on a chest, a barrel
// or any other container by its block entity's NBT refuses the open of every
// player whose main hand does not answer it, telling them the overlay
// message and the locked sound instead of opening a window.

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// lockKey is the item the tests' locks name as their key.
const lockKey = "minecraft:tripwire_hook"

// lockSound is the one sound a container's lock refuses an open with.
// vanilla: SoundEvents.CHEST_LOCKED
const lockSound = "minecraft:block.chest.locked"

// lockedChest lays a chest locked by the tripwire hook at (x, y, z), with an
// optional NBT field for a custom name, and drains the placement behind it.
func lockedChest(t *testing.T, w *servertest.WindowedClient, x, y, z int32, tail string) {
	t.Helper()
	fields := `lock:{items:["` + lockKey + `"]}`
	if tail != "" {
		fields = tail + `,` + fields
	}
	servertest.Setblock(t, w.Client, x, y, z, `minecraft:chest{`+fields+`}`)
	w.Drain()
}

// expectLockedRefusal drains the answer to a locked container's use: the
// overlay message naming the container, then the locked sound at the given
// spot - the message is written to the clicker ahead of the sound the
// mailbox takes - and no chest or barrel window at all.
func expectLockedRefusal(t *testing.T, w *servertest.WindowedClient, name protocol.Text, sx, sy, sz float64) {
	t.Helper()
	w.ExpectSystemChat("container.isLocked", true, name)
	sname, x, y, z := w.AwaitChestSound()
	if sname != lockSound || x != sx || y != sy || z != sz {
		t.Errorf("the locked sound played %q at (%v, %v, %v), want %q at (%v, %v, %v)",
			sname, x, y, z, lockSound, sx, sy, sz)
	}
	if opened := anyWindowOpens(t, w.Client); opened != 0 {
		t.Errorf("the locked container opened %d windows, want none", opened)
	}
}

// anyWindowOpens counts every Open Screen the client was shown, whichever
// menu it names: a refusal must put no window at all in front of the player.
func anyWindowOpens(t *testing.T, c *servertest.Client) int {
	t.Helper()
	opened := 0
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayOpenScreen {
			opened++
		}
	})
	return opened
}

// TestALockedChestRefusesTheWrongItemAndOpensToItsKey lays a locked chest
// and right-clicks it: an empty hand and a held item the lock does not name
// each get the overlay message and the locked sound and no window, while
// the named item opens the chest as though it had never been locked.
func TestALockedChestRefusesTheWrongItemAndOpensToItsKey(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	lockedChest(t, w, 3, 64, 0, "")

	w.UseBlock(3, 64, 0)
	expectLockedRefusal(t, w, protocol.Text{Translate: "container.chest"}, 3.5, 64.5, 0.5)

	servertest.GiveHeldItem(w.Client, "minecraft:stick")
	w.UseBlock(3, 64, 0)
	expectLockedRefusal(t, w, protocol.Text{Translate: "container.chest"}, 3.5, 64.5, 0.5)

	servertest.GiveHeldItem(w.Client, lockKey)
	w.UseBlock(3, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 3, 64, 0)
	w.CloseWindow()
	w.ExpectWindowZero()
}

// TestALockedChestNamesItselfInTheOverlay names the locked chest: the
// overlay message carries the chest's own name, and the right key still
// opens it under that name.
func TestALockedChestNamesItselfInTheOverlay(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	lockedChest(t, w, 3, 64, 0, `CustomName:{text:"The Vault"}`)

	w.UseBlock(3, 64, 0)
	expectLockedRefusal(t, w, protocol.Text{Text: "The Vault"}, 3.5, 64.5, 0.5)

	servertest.GiveHeldItem(w.Client, lockKey)
	w.UseBlock(3, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 3, 64, 0)
}

// TestALockedChestKeepsItsLockThroughAChunkSave lays a locked chest, saves
// the world and reopens it: the lock comes back with the chunk, its tag
// intact and its refusal with it, and the named key still opens the chest.
func TestALockedChestKeepsItsLockThroughAChunkSave(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	srv, w := servertest.OpenWindowedOps(t, cfg)
	lockedChest(t, w, 3, 64, 0, "")
	w.Disconnect()
	srv.Shutdown()

	back, w2 := servertest.OpenWindowedOps(t, cfg)
	defer w2.Disconnect()
	tag, ok := back.BlockEntityTagAt(3, 64, 0)
	if !ok {
		t.Fatal("the chest's block entity did not come back with the chunk")
	}
	lock, ok := nbt.Get[nbt.Compound](tag, "lock")
	if !ok {
		t.Fatal("the reloaded chest lost its lock")
	}
	items, ok := nbt.Get[nbt.List](lock, "items")
	if !ok || len(items.Items) != 1 || items.Items[0] != nbt.String(lockKey) {
		t.Errorf("the reloaded lock names %v, want the single %s", items.Items, lockKey)
	}

	w2.UseBlock(3, 64, 0)
	expectLockedRefusal(t, w2, protocol.Text{Translate: "container.chest"}, 3.5, 64.5, 0.5)
	servertest.GiveHeldItem(w2.Client, lockKey)
	w2.UseBlock(3, 64, 0)
	w2.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 3, 64, 0)
}

// TestALockedBarrelRefusesTheWrongItem lays a locked barrel: its refusal
// names the barrel and plays the one locked sound and opens no window, while
// the named key opens it.
func TestALockedBarrelRefusesTheWrongItem(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	servertest.Setblock(t, w.Client, 3, 64, 0,
		`minecraft:barrel{lock:{items:["`+lockKey+`"]}}`)
	w.Drain()

	w.UseBlock(3, 64, 0)
	expectLockedRefusal(t, w, protocol.Text{Translate: "container.barrel"}, 3.5, 64.5, 0.5)

	servertest.GiveHeldItem(w.Client, lockKey)
	w.UseBlock(3, 64, 0)
	w.ExpectContainerOpenType(protocol.MenuGeneric9x3, protocol.BlockEntityBarrel, 3, 64, 0)
}

// TestASpectatorCarriesEveryKey opens a locked chest as a spectator, the one
// whose key opens everything: the window opens and nothing is said.
// vanilla: LockCode.canUnlock
func TestASpectatorCarriesEveryKey(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.GameMode = "spectator"
	_, w := servertest.OpenWindowedOps(t, cfg)
	defer w.Disconnect()
	lockedChest(t, w, 3, 64, 0, "")

	w.UseBlock(3, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 3, 64, 0)
	if chats := w.SystemChatsBeforeSettle(); len(chats) != 0 {
		t.Errorf("the spectator was told %d messages by the locked chest, want the open alone", len(chats))
	}
}

// TestALockedHalfLocksTheDoubleChest locks the right half of a double chest
// and clicks the other: the whole pair refuses, named by the double chest's
// own title, its locked sound between the halves.
func TestALockedHalfLocksTheDoubleChest(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	servertest.PlaceChestPair(t, w, "minecraft:chest", 4, 64, 0)
	servertest.Setblock(t, w.Client, 5, 64, 0,
		`minecraft:chest[facing=north,type=right]{lock:{items:["`+lockKey+`"]}}`)
	w.Drain()

	// The clear half answers for the pair, so the message names the whole
	// double chest and the sound plays between the halves.
	w.UseBlock(4, 64, 0)
	expectLockedRefusal(t, w, protocol.Text{Translate: "container.chestDouble"}, 5.0, 64.5, 0.5)
	servertest.GiveHeldItem(w.Client, lockKey)
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x6, true, 4, 64, 0)
}

// TestALockedFurnaceRefusesTheWrongItem locks a kind other than a chest: the
// gate is every container window's, so a furnace refuses the wrong item and
// opens to the named one.
func TestALockedFurnaceRefusesTheWrongItem(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	servertest.Setblock(t, w.Client, 3, 64, 0, `minecraft:furnace{lock:{items:["`+lockKey+`"]}}`)
	w.Drain()

	w.UseBlock(3, 64, 0)
	expectLockedRefusal(t, w, protocol.Text{Translate: "container.furnace"}, 3.5, 64.5, 0.5)

	servertest.GiveHeldItem(w.Client, lockKey)
	w.UseBlock(3, 64, 0)
	w.ExpectContainerOpen(protocol.MenuFurnace, true, 3, 64, 0)
}

// TestALockedChestOpensToAnItemTag locks a chest with an item tag: vanilla
// expands the tag, so a plank opens the chest and the hook does not.
func TestALockedChestOpensToAnItemTag(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	servertest.Setblock(t, w.Client, 3, 64, 0, `minecraft:chest{lock:{items:"#minecraft:planks"}}`)
	w.Drain()

	w.UseBlock(3, 64, 0)
	expectLockedRefusal(t, w, protocol.Text{Translate: "container.chest"}, 3.5, 64.5, 0.5)

	servertest.GiveHeldItem(w.Client, "minecraft:oak_planks")
	w.UseBlock(3, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 3, 64, 0)
}

// TestALockedChestDropsTheCountItsCodecRejects lays a lock whose count the
// codec rejects but whose items decode: vanilla drops the count and keeps the
// items, so the chest opens to its key and refuses everyone else.
// vanilla: ItemPredicate.CODEC
func TestALockedChestDropsTheCountItsCodecRejects(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	servertest.Setblock(t, w.Client, 3, 64, 0, `minecraft:chest{lock:{count:"2",items:["`+lockKey+`"]}}`)
	w.Drain()

	w.UseBlock(3, 64, 0)
	expectLockedRefusal(t, w, protocol.Text{Translate: "container.chest"}, 3.5, 64.5, 0.5)

	servertest.GiveHeldItem(w.Client, lockKey)
	w.UseBlock(3, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 3, 64, 0)
}

// TestALockedChestRefusesAnEmptyHolderSet lays a lock whose items list holds
// only a tag, which the list codec drops as a bad entry: vanilla keeps an
// empty holder set that matches no stack, so the chest refuses every hand.
// vanilla: ListCodec, HolderSetCodec
func TestALockedChestRefusesAnEmptyHolderSet(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	servertest.Setblock(t, w.Client, 3, 64, 0, `minecraft:chest{lock:{items:["#minecraft:planks"]}}`)
	w.Drain()

	w.UseBlock(3, 64, 0)
	expectLockedRefusal(t, w, protocol.Text{Translate: "container.chest"}, 3.5, 64.5, 0.5)

	servertest.GiveHeldItem(w.Client, "minecraft:oak_planks")
	w.UseBlock(3, 64, 0)
	expectLockedRefusal(t, w, protocol.Text{Translate: "container.chest"}, 3.5, 64.5, 0.5)
}

// TestALockedChestTestsDamageAndCustomDataPredicates locks chests to an iron
// pickaxe by exact damage, a durability range and a partial custom_data
// match: the matching stack opens each, a nonmatching one is refused.
// vanilla: DataComponentMatchers.test, DamagePredicate, CustomDataPredicate
func TestALockedChestTestsDamageAndCustomDataPredicates(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	const pick = "minecraft:iron_pickaxe"
	id, _ := registry.ItemID(pick)
	locks := []string{
		`components:{"minecraft:damage":5}`,
		`components:{"minecraft:damage":5b}`,
		`predicates:{"minecraft:damage":{durability:{min:200,max:250}}}`,
		`predicates:{"minecraft:custom_data":{key:1b}}`,
		`predicates:{"minecraft:custom_data":{key:1b}}`,
	}
	withData := func(damage int32, tag nbt.Compound) protocol.ItemStack {
		st := protocol.ItemStack{ItemID: id, Count: 1, Damage: damage}
		if tag != nil {
			st.SetCustomData(tag)
		}
		return st
	}
	// Iron pickaxe has 250 durability: damage 5 leaves 245.
	hands := [][2]protocol.ItemStack{
		{withData(5, nil), withData(6, nil)},
		{withData(5, nil), withData(6, nil)},
		{withData(5, nil), withData(100, nil)},
		{withData(0, nbt.Compound{"key": nbt.Byte(1), "x": nbt.Int(2)}), withData(0, nbt.Compound{"key": nbt.Byte(2)})},
		{withData(0, nbt.Compound{"key": nbt.Byte(1)}), withData(0, nil)}, // right item, no custom data
	}
	for i, lock := range locks {
		x, z := int32(2), int32(2*i-3) // apart, so no double chest, and in reach
		servertest.Setblock(t, w.Client, x, 64, z,
			`minecraft:chest{lock:{items:["`+pick+`"],`+lock+`}}`)
		w.Drain()
		w.SetCreativeSlot(player.HotbarSlot, hands[i][1])
		w.Settle()
		w.UseBlock(x, 64, z)
		expectLockedRefusal(t, w, protocol.Text{Translate: "container.chest"}, float64(x)+0.5, 64.5, float64(z)+0.5)
		w.SetCreativeSlot(player.HotbarSlot, hands[i][0])
		w.Settle()
		w.UseBlock(x, 64, z)
		w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, x, 64, z)
		w.CloseWindow()
	}
}

// TestALockedChestReadsFractionalDamageBoundsAsInts locks a chest by partial
// damage bounds written as doubles, which the integer codec truncates: -0.5d is
// 0, so the undamaged pickaxe opens and a damaged one is refused.
// vanilla: DamagePredicate, MinMaxBounds.Ints.CODEC
func TestALockedChestReadsFractionalDamageBoundsAsInts(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	const pick = "minecraft:iron_pickaxe"
	id, _ := registry.ItemID(pick)
	locks := []string{
		`predicates:{"minecraft:damage":{damage:-0.5d}}`,
		`predicates:{"minecraft:damage":{damage:{max:-0.5d}}}`,
		`predicates:{"minecraft:damage":{damage:{min:-0.5d,max:-0.5d}}}`,
	}
	for i, lock := range locks {
		x, z := int32(2), int32(2*i-3) // apart, so no double chest, and in reach
		servertest.Setblock(t, w.Client, x, 64, z,
			`minecraft:chest{lock:{items:["`+pick+`"],`+lock+`}}`)
		w.Drain()
		w.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: id, Count: 1, Damage: 5})
		w.Settle()
		w.UseBlock(x, 64, z)
		expectLockedRefusal(t, w, protocol.Text{Translate: "container.chest"}, float64(x)+0.5, 64.5, float64(z)+0.5)
		w.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: id, Count: 1})
		w.Settle()
		w.UseBlock(x, 64, z)
		w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, x, 64, z)
		w.CloseWindow()
	}
}

// TestALockedChestReadsNumericBooleanPredicates keeps numeric boolean flags,
// including fractions, as lock restrictions.
// vanilla: FireworkExplosionPredicate.FireworkPredicate, NbtOps.getBooleanValue
func TestALockedChestReadsNumericBooleanPredicates(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	id, _ := registry.ItemID("minecraft:firework_star")
	star := func(trail bool) protocol.ItemStack {
		return protocol.ItemStack{ItemID: id, Count: 1, Raw: []protocol.RawComponent{
			(protocol.FireworkExplosion{Shape: 2, Trail: trail}).RawComponent(),
		}}
	}
	cases := []struct {
		flag  string
		trail bool
	}{
		{"1b", true},
		{"1s", true},
		{"1", true},
		{"1L", true},
		{"0.5f", true},
		{"-0.5d", true},
		{"0", false},
		{"0.0d", false},
	}
	for i, c := range cases {
		x, z := int32(3-6*(i/4)), int32(2*(i%4)-3) // apart and within reach
		servertest.Setblock(t, w.Client, x, 64, z,
			`minecraft:chest{lock:{items:["minecraft:firework_star"],predicates:{"minecraft:firework_explosion":{has_trail:`+c.flag+`}}}}`)
		w.Drain()
		w.SetCreativeSlot(player.HotbarSlot, star(!c.trail))
		w.Settle()
		w.UseBlock(x, 64, z)
		expectLockedRefusal(t, w, protocol.Text{Translate: "container.chest"}, float64(x)+0.5, 64.5, float64(z)+0.5)
		w.SetCreativeSlot(player.HotbarSlot, star(c.trail))
		w.Settle()
		w.UseBlock(x, 64, z)
		w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, x, 64, z)
		w.CloseWindow()
	}
}

// TestALockedChestTestsEnchantmentAndContainerPredicates locks chests by an
// exact enchantments component, a partial enchantments level range and a
// partial container contents test: the matching stack opens each, a
// nonmatching one is refused.
// vanilla: DataComponentExactPredicate, EnchantmentsPredicate, ContainerPredicate
func TestALockedChestTestsEnchantmentAndContainerPredicates(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	sword, _ := registry.ItemID("minecraft:iron_sword")
	box, _ := registry.ItemID("minecraft:shulker_box")
	sharp, _ := registry.EnchantmentID("minecraft:sharpness")
	diamond, _ := registry.ItemID("minecraft:diamond")
	dirt, _ := registry.ItemID("minecraft:dirt")
	enchanted := func(level int32) protocol.ItemStack {
		return protocol.ItemStack{ItemID: sword, Count: 1, Enchantments: map[int32]int32{sharp: level}}
	}
	boxOf := func(item int32) protocol.ItemStack {
		return protocol.ItemStack{ItemID: box, Count: 1, Container: []protocol.ItemStack{{ItemID: item, Count: 1}}}
	}
	cases := []struct {
		item, lock string
		ok, bad    protocol.ItemStack
	}{
		{"iron_sword", `components:{"minecraft:enchantments":{"minecraft:sharpness":3}}`, enchanted(3), enchanted(4)},
		{"iron_sword", `predicates:{"minecraft:enchantments":[{enchantments:"minecraft:sharpness",levels:{min:2}}]}`, enchanted(2), enchanted(1)},
		{"shulker_box", `predicates:{"minecraft:container":{items:{contains:[{items:"minecraft:diamond"}]}}}`, boxOf(diamond), boxOf(dirt)},
	}
	for i, c := range cases {
		x, z := int32(3), int32(2*i)
		servertest.Setblock(t, w.Client, x, 64, z,
			`minecraft:chest{lock:{items:["minecraft:`+c.item+`"],`+c.lock+`}}`)
		w.Drain()
		w.SetCreativeSlot(player.HotbarSlot, c.bad)
		w.Settle()
		w.UseBlock(x, 64, z)
		expectLockedRefusal(t, w, protocol.Text{Translate: "container.chest"}, float64(x)+0.5, 64.5, float64(z)+0.5)
		w.SetCreativeSlot(player.HotbarSlot, c.ok)
		w.Settle()
		w.UseBlock(x, 64, z)
		w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, x, 64, z)
		w.CloseWindow()
	}
}

// TestALockedChestTestsEffectiveComponentDefaults locks chests by predicates
// over components a stack does not hold in its patch: the plain stack opens
// each by its item's default, a stack that differs is refused.
// vanilla: SingleComponentItemPredicate.matches, DataComponentExactPredicate.test, NbtPredicate.matches
func TestALockedChestTestsEffectiveComponentDefaults(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	sword, _ := registry.ItemID("minecraft:iron_sword")
	box, _ := registry.ItemID("minecraft:shulker_box")
	potion, _ := registry.ItemID("minecraft:potion")
	healing, _ := registry.PotionID("minecraft:healing")
	diamond, _ := registry.ItemID("minecraft:diamond")
	plainSword := protocol.ItemStack{ItemID: sword, Count: 1}
	stone, _ := registry.ItemID("minecraft:stone")
	keyed := plainSword
	keyed.SetCustomData(nbt.Compound{"key": nbt.Byte(1)})
	lored := plainSword
	lored.SetLore([]protocol.Text{{Text: "lore"}})
	noModifiers := plainSword
	noModifiers.SetAttributeModifiers(nil)
	emptyBox := protocol.ItemStack{ItemID: box, Count: 1}
	fullBox := protocol.ItemStack{ItemID: box, Count: 1, Container: []protocol.ItemStack{{ItemID: diamond, Count: 1}}}
	plainPotion := protocol.ItemStack{ItemID: potion, Count: 1}
	healingPotion := plainPotion
	healingPotion.SetPotion(healing)
	rocket, _ := registry.ItemID("minecraft:firework_rocket")
	plainRocket := protocol.ItemStack{ItemID: rocket, Count: 1}
	longRocket := plainRocket
	longRocket.SetRaw(protocol.FireworksRawComponent(2, nil))
	cases := []struct {
		item, lock string
		ok, bad    protocol.ItemStack
	}{
		{"iron_sword", `predicates:{"minecraft:attribute_modifiers":{modifiers:{contains:[{attribute:"minecraft:attack_damage",amount:5.0d}]}}}`,
			plainSword, noModifiers},
		{"iron_sword", `components:{"minecraft:lore":[]}`, plainSword, lored},
		{"iron_sword", `predicates:{"minecraft:custom_data":{}}`, plainSword, protocol.ItemStack{ItemID: stone, Count: 1}},
		{"iron_sword", `predicates:{"minecraft:custom_data":{key:1b}}`, keyed, plainSword},
		{"shulker_box", `predicates:{"minecraft:container":{items:{size:0}}}`, emptyBox, fullBox},
		{"shulker_box", `predicates:{"minecraft:container":{items:{size:-0.5d}}}`, emptyBox, fullBox},
		{"potion", `predicates:{"minecraft:potion_contents":{effects:{size:0}}}`, plainPotion, healingPotion},
		{"firework_rocket", `predicates:{"minecraft:fireworks":{flight_duration:1,explosions:{size:0}}}`, plainRocket, longRocket},
		{"firework_rocket", `components:{"minecraft:fireworks":{flight_duration:1}}`, plainRocket, longRocket},
	}
	for i, c := range cases {
		x, z := int32(2-4*(i/5)), int32(2*(i%5)-4) // within reach
		servertest.Setblock(t, w.Client, x, 64, z,
			`minecraft:chest{lock:{items:["minecraft:`+c.item+`"],`+c.lock+`}}`)
		w.Drain()
		w.SetCreativeSlot(player.HotbarSlot, c.bad)
		w.Settle()
		w.UseBlock(x, 64, z)
		expectLockedRefusal(t, w, protocol.Text{Translate: "container.chest"}, float64(x)+0.5, 64.5, float64(z)+0.5)
		w.SetCreativeSlot(player.HotbarSlot, c.ok)
		w.Settle()
		w.UseBlock(x, 64, z)
		w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, x, 64, z)
		w.CloseWindow()
	}
}

// TestALockedChestResolvesSynchronizedTags locks chests by #tags: an
// enchantment tag the pack defines with the stack's enchantment opens it, one
// without that member or an empty one refuses it, and a trim material tag
// that does not exist fails to decode, dropping the constraint so the
// trimmed stack opens. Tags are process-wide: do not parallelize.
// vanilla: HolderSetCodec, MappedRegistry.get(TagKey)
func TestALockedChestResolvesSynchronizedTags(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	for name, values := range map[string]string{"sharp": `["minecraft:sharpness"]`, "other": `["minecraft:mending"]`, "empty": `[]`} {
		servertest.WritePackFile(t, cfg, "p", "pk/tags/enchantment/"+name+".json", `{"values":`+values+`}`)
	}
	for name, values := range map[string]string{"golds": `["minecraft:gold"]`, "coppers": `["minecraft:copper"]`} {
		servertest.WritePackFile(t, cfg, "p", "pk/tags/trim_material/"+name+".json", `{"values":`+values+`}`)
	}
	_, w := servertest.OpenWindowedOps(t, cfg)
	defer w.Disconnect()
	sword, _ := registry.ItemID("minecraft:iron_sword")
	sharp, _ := registry.EnchantmentID("minecraft:sharpness")
	enchanted := protocol.ItemStack{ItemID: sword, Count: 1, Enchantments: map[int32]int32{sharp: 1}}
	chest, _ := registry.ItemID("minecraft:iron_chestplate")
	trimmed := protocol.ItemStack{ItemID: chest, Count: 1}
	trimmed.SetTrim(registry.MustID("minecraft:trim_material", "minecraft:gold"), registry.MustID("minecraft:trim_pattern", "minecraft:coast"))
	cases := []struct {
		item, lock string
		stack      protocol.ItemStack
		opens      bool
	}{
		{"iron_sword", `"minecraft:enchantments":[{enchantments:"#pk:sharp"}]`, enchanted, true},
		{"iron_sword", `"minecraft:enchantments":[{enchantments:"#pk:other"}]`, enchanted, false},
		{"iron_sword", `"minecraft:enchantments":[{enchantments:"#pk:empty"}]`, enchanted, false},
		{"iron_chestplate", `"minecraft:trim":{material:"#minecraft:nothing"}`, trimmed, true},
		{"iron_chestplate", `"minecraft:trim":{material:"#pk:golds"}`, trimmed, true},
		{"iron_chestplate", `"minecraft:trim":{material:"#pk:coppers"}`, trimmed, false},
	}
	for i, c := range cases {
		x, z := int32(2-4*(i%2)), int32(2*(i/2)) // within reach
		servertest.Setblock(t, w.Client, x, 64, z,
			`minecraft:chest{lock:{items:["minecraft:`+c.item+`"],predicates:{`+c.lock+`}}}`)
		w.Drain()
		w.SetCreativeSlot(player.HotbarSlot, c.stack)
		w.Settle()
		w.UseBlock(x, 64, z)
		if c.opens {
			w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, x, 64, z)
			w.CloseWindow()
		} else {
			expectLockedRefusal(t, w, protocol.Text{Translate: "container.chest"}, float64(x)+0.5, 64.5, float64(z)+0.5)
		}
	}
}

// TestAChestLockedToACustomNameOpensToThatKey locks the chest to a key whose
// custom_name is named exactly: the renamed stack opens it and the bare item
// does not.
func TestAChestLockedToACustomNameOpensToThatKey(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	servertest.Setblock(t, w.Client, 3, 64, 0,
		`minecraft:chest{lock:{items:["`+lockKey+`"],components:{"minecraft:custom_name":"Skeleton Key"}}}`)
	w.Drain()

	w.UseBlock(3, 64, 0)
	expectLockedRefusal(t, w, protocol.Text{Translate: "container.chest"}, 3.5, 64.5, 0.5)

	servertest.GiveHeldItem(w.Client, lockKey)
	w.UseBlock(3, 64, 0)
	expectLockedRefusal(t, w, protocol.Text{Translate: "container.chest"}, 3.5, 64.5, 0.5)

	name := protocol.Text{Text: "Skeleton Key"}
	id, _ := registry.ItemID(lockKey)
	w.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: id, Count: 1, CustomName: &name})
	w.Settle()
	w.UseBlock(3, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 3, 64, 0)
}

// awaitFakeGiveItem reads past the fake item a fitted /give drops for the
// pickup animation until it is removed, so its one tick of motion and its
// removal cannot land in the middle of what the test reads next.
// vanilla: GiveCommand.giveItem, ItemEntity.makeFakeItem
func awaitFakeGiveItem(c *servertest.Client) {
	c.T.Helper()
	var fake int32
	for {
		r := protocol.NewReader(c.AwaitPlay(protocol.IDPlaySpawnEntity))
		fake = r.ReadVarInt()
		r.ReadUUID()
		if r.ReadVarInt() == protocol.EntityTypeItem {
			break
		}
	}
	for {
		r := protocol.NewReader(c.AwaitPlay(protocol.IDPlayRemoveEntities))
		for range r.ReadVarInt() {
			if r.ReadVarInt() == fake {
				c.Settle()
				return
			}
		}
	}
}

// placeGivenLocked gives the player a container item carrying a lock that
// names the tripwire hook, places it on the floor at (x, 64, 0) and drains
// the placement, then leaves a stick in the hand.
func placeGivenLocked(t *testing.T, w *servertest.WindowedClient, item string, x int32) {
	t.Helper()
	w.ChatCommand(`give @s ` + item + `[lock={items:"` + lockKey + `"}]`)
	awaitFakeGiveItem(w.Client)
	w.PlaceBlock(x, 63, 0, protocol.FaceTop)
	w.Settle()
	w.Drain()
	servertest.GiveHeldItem(w.Client, "minecraft:stick")
}

// TestAPlacedLockedChestKeepsItsStacksLock places a chest from a stack
// carrying the lock component: the placed chest refuses an empty-handed use
// with the locked message and sound and opens to the named key.
// vanilla: BaseContainerBlockEntity.applyImplicitComponents
func TestAPlacedLockedChestKeepsItsStacksLock(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	placeGivenLocked(t, w, "minecraft:chest", 3)

	w.UseBlock(3, 64, 0)
	expectLockedRefusal(t, w, protocol.Text{Translate: "container.chest"}, 3.5, 64.5, 0.5)

	servertest.GiveHeldItem(w.Client, lockKey)
	w.UseBlock(3, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 3, 64, 0)
}

// TestAPlacedLockedBarrelKeepsItsStacksLock is the barrel's counterpart.
func TestAPlacedLockedBarrelKeepsItsStacksLock(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	placeGivenLocked(t, w, "minecraft:barrel", 3)

	w.UseBlock(3, 64, 0)
	expectLockedRefusal(t, w, protocol.Text{Translate: "container.barrel"}, 3.5, 64.5, 0.5)

	servertest.GiveHeldItem(w.Client, lockKey)
	w.UseBlock(3, 64, 0)
	w.ExpectContainerOpenType(protocol.MenuGeneric9x3, protocol.BlockEntityBarrel, 3, 64, 0)
}

// TestAPlacedLockedBeaconKeepsItsStacksLock is the beacon's counterpart: its
// menu is gated by the same lock.
// vanilla: BeaconBlockEntity.applyImplicitComponents, BeaconBlockEntity.createMenu
func TestAPlacedLockedBeaconKeepsItsStacksLock(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	placeGivenLocked(t, w, "minecraft:beacon", 3)

	w.UseBlock(3, 64, 0)
	expectLockedRefusal(t, w, protocol.Text{Translate: "container.beacon"}, 3.5, 64.5, 0.5)

	servertest.GiveHeldItem(w.Client, lockKey)
	w.UseBlock(3, 64, 0)
	w.ExpectContainerOpen(protocol.MenuBeacon, false, 3, 64, 0)
}

// TestAPlacedUnlockedChestOpensForAnyone checks a stack without the
// component places an unlocked chest.
func TestAPlacedUnlockedChestOpensForAnyone(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	w.ChatCommand(`give @s minecraft:chest`)
	awaitFakeGiveItem(w.Client)
	w.PlaceBlock(3, 63, 0, protocol.FaceTop)
	w.Settle()
	w.Drain()
	servertest.GiveHeldItem(w.Client, "minecraft:stick")

	w.UseBlock(3, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 3, 64, 0)
}
