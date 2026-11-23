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
	// vanilla: ExtraCodecs.compactListCodec writes a one-element list as the element.
	if items := lock["items"]; items != nbt.String(lockKey) {
		t.Errorf("the reloaded lock names %v, want the single %s", items, lockKey)
	}

	w2.UseBlock(3, 64, 0)
	expectLockedRefusal(t, w2, protocol.Text{Translate: "container.chest"}, 3.5, 64.5, 0.5)
	servertest.GiveHeldItem(w2.Client, lockKey)
	w2.UseBlock(3, 64, 0)
	w2.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 3, 64, 0)
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
