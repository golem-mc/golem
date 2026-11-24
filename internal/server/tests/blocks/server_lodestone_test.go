package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The lodestone compass, black-box: a compass used on a lodestone plays the
// lock sound at the block and becomes the compass that points there, the
// stack of one changes in place and a bigger stack or a creative hand splits
// one off, the dimension the lodestone stands in is part of what the
// component carries, the tracker survives a save and a reload, and a tick
// clears the target of a tracked compass whose lodestone is gone.

// lodestoneCompass is the stack the compass in the hand becomes when used on
// the lodestone at the given place, the form the slot packets carry it in.
func lodestoneCompass(t *testing.T, count int32, dimension string, x, y, z int32) protocol.ItemStack {
	t.Helper()
	locked := servertest.ItemStack(t, "minecraft:compass", count)
	locked.SetLodestoneTracker(protocol.LodestoneTracker{
		Target:  &protocol.LodestoneTarget{Dimension: dimension, X: x, Y: y, Z: z},
		Tracked: true,
	})
	return locked
}

// clearedLodestoneCompass is the compass a tick leaves behind once its
// lodestone is gone: the tracker with no target — the needle spins — and the
// tracked flag still set.
func clearedLodestoneCompass(t *testing.T, count int32) protocol.ItemStack {
	t.Helper()
	cleared := servertest.ItemStack(t, "minecraft:compass", count)
	cleared.SetLodestoneTracker(protocol.LodestoneTracker{Tracked: true})
	return cleared
}

// shapeLodestone puts a lodestone into the ground the players stand on,
// before any of them joins.
func shapeLodestone(t *testing.T, srv *server.Server, x, y, z int32) {
	t.Helper()
	state, ok := registry.FindState("minecraft:lodestone", nil)
	if !ok {
		t.Fatal("the registry has no lodestone")
	}
	srv.SetLoadedBlock(x, y, z, state)
}

// lodestoneClick is what one right-click with a held stack put on the wire:
// the sounds it played and the window slots it set.
type lodestoneClick struct {
	sounds  []servertest.PlaySoundEffect
	updates []servertest.SlotUpdate
}

// clickBlockAndCollect right-clicks the block at the position with the main
// hand and reads the click's own answers up to the ack that ends them — over
// the two echo block updates and the world's own traffic in between.
func clickBlockAndCollect(t *testing.T, c *servertest.Client, x, y, z int32) lodestoneClick {
	t.Helper()
	return clickBlockAndCollectWithHand(t, c, protocol.HandMain, x, y, z)
}

// clickBlockAndCollectWithHand is clickBlockAndCollect with the hand chosen.
func clickBlockAndCollectWithHand(t *testing.T, c *servertest.Client, hand, x, y, z int32) lodestoneClick {
	t.Helper()
	useBlockWithHand(c, hand, x, y, z)
	var click lodestoneClick
	for packetID, data := range c.Packets(servertest.ExpectTimeout) {
		switch packetID {
		case protocol.IDPlayAckBlockChange:
			return click
		case protocol.IDPlaySoundEffect:
			click.sounds = append(click.sounds, servertest.DecodePlaySoundEffect(t, data))
		case protocol.IDPlayContainerSlot:
			r := protocol.NewReader(data)
			r.ReadVarInt() // the window, the player's own here
			r.ReadVarInt() // the state ID
			click.updates = append(click.updates, servertest.SlotUpdate{
				Slot:  r.ReadI16(),
				Stack: servertest.MustReadSlot(t, r),
			})
		}
	}
	t.Fatalf("the click on (%d, %d, %d) was never acknowledged within %s", x, y, z, servertest.ExpectTimeout)
	return click
}

// useBlockWithHand right-clicks the block at the position with the given
// hand, the way a client does when it is the thing to do with the block.
func useBlockWithHand(c *servertest.Client, hand, x, y, z int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayUseItemOn, func(b *protocol.Buffer) {
		b.PutVarInt(hand)
		b.PutPosition(x, y, z)
		b.PutVarInt(protocol.FaceTop)
		b.PutF32(0.5)
		b.PutF32(0.5)
		b.PutF32(0.5)
		b.PutBool(false)
		b.PutBool(false)
		b.PutVarInt(1)
	})
}

// slotUpdateOf picks the slot the click set, failing the test when the click
// never touched it.
func slotUpdateOf(t *testing.T, click lodestoneClick, slot int16) protocol.ItemStack {
	t.Helper()
	for _, update := range click.updates {
		if update.Slot == slot {
			return update.Stack
		}
	}
	t.Fatalf("the click set no slot %d; it set %+v", slot, click.updates)
	return protocol.ItemStack{}
}

// expectLockSound checks the lock sound among what the click played: the
// lodestone compass lock, in the players category, at the block for
// everyone — the clicker included — at vanilla's volume and pitch.
func expectLockSound(t *testing.T, click lodestoneClick, x, y, z int32) {
	t.Helper()
	id, ok := registry.SoundEventID("minecraft:item.lodestone_compass.lock")
	if !ok {
		t.Fatal("the sound registry has no lodestone compass lock")
	}
	for _, sound := range click.sounds {
		if sound.RegistryID != id {
			continue
		}
		if sound.Category != protocol.SoundCategoryPlayer {
			t.Errorf("the lock sound's category = %d, want %d (players)", sound.Category, protocol.SoundCategoryPlayer)
		}
		// The coordinates travel as eighths of a block; the block's centre
		// is what a block's sound is played at.
		if wx, wy, wz := (x*8)+4, (y*8)+4, (z*8)+4; sound.X != wx || sound.Y != wy || sound.Z != wz {
			t.Errorf("the lock sound at (%d %d %d), want the block's centre (%d %d %d)",
				sound.X, sound.Y, sound.Z, wx, wy, wz)
		}
		if sound.Volume != 1 || sound.Pitch != 1 {
			t.Errorf("the lock sound's volume, pitch = %v, %v, want 1, 1", sound.Volume, sound.Pitch)
		}
		return
	}
	t.Errorf("the click played no lodestone compass lock among %+v", click.sounds)
}

// TestACompassLocksOntoALodestone checks the click the feature is: a
// survival compass used on a lodestone plays the lock sound there and
// becomes the lodestone compass pointing at the block, the stack of one
// changed in place and the client told so.
// vanilla: CompassItem.useOn
func TestACompassLocksOntoALodestone(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	shapeLodestone(t, srv, 2, 63, 1)

	c := connect().Join("Wanderer")
	srv.GiveSlot("Wanderer", player.HotbarSlot, servertest.ItemStack(t, "minecraft:compass", 1))

	click := clickBlockAndCollect(t, c, 2, 63, 1)
	expectLockSound(t, click, 2, 63, 1)
	if len(click.updates) != 1 {
		t.Fatalf("the click set %d slots (%+v), want the held one alone", len(click.updates), click.updates)
	}
	want := lodestoneCompass(t, 1, "minecraft:overworld", 2, 63, 1)
	if got := slotUpdateOf(t, click, int16(player.HotbarSlot)); !got.Equal(want) {
		t.Errorf("slot %d = %+v, want %+v", player.HotbarSlot, got, want)
	}
	c.Disconnect()
}

// TestAStackOfTwoCompassesSplitsOneOntoALodestone checks the bigger stack's
// answer: one compass of the held stack is spent and the locked one lands
// beside it, the held stack kept plain under its player.
// vanilla: CompassItem.useOn
func TestAStackOfTwoCompassesSplitsOneOntoALodestone(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	shapeLodestone(t, srv, 2, 63, 1)

	c := connect().Join("Wanderer")
	srv.GiveSlot("Wanderer", player.HotbarSlot, servertest.ItemStack(t, "minecraft:compass", 2))

	click := clickBlockAndCollect(t, c, 2, 63, 1)
	expectLockSound(t, click, 2, 63, 1)
	if len(click.updates) != 2 {
		t.Fatalf("the click set %d slots (%+v), want the spent held stack and the split-off compass",
			len(click.updates), click.updates)
	}
	plain := servertest.ItemStack(t, "minecraft:compass", 1)
	if got := slotUpdateOf(t, click, int16(player.HotbarSlot)); !got.Equal(plain) {
		t.Errorf("slot %d = %+v, want the held stack spent to %+v", player.HotbarSlot, got, plain)
	}
	locked := lodestoneCompass(t, 1, "minecraft:overworld", 2, 63, 1)
	if got := slotUpdateOf(t, click, int16(player.HotbarSlot+1)); !got.Equal(locked) {
		t.Errorf("slot %d = %+v, want %+v", player.HotbarSlot+1, got, locked)
	}
	c.Disconnect()
}

// TestALodestoneInTheNetherLocksItsDimensionIntoTheCompass checks the
// dimension is part of what the tracker carries: a lodestone locked at in
// the nether says so, which is what makes a compass point there across the
// worlds.
// vanilla: CompassItem.useOn, GlobalPos.of
func TestALodestoneInTheNetherLocksItsDimensionIntoTheCompass(t *testing.T) {
	t.Parallel()
	// Each test edits its own copy of the arrival terrain.
	cfg := servertest.NetherTestConfig(t, servertest.SurvivalTestConfig())
	srv, c := servertest.Join(t, cfg, "Crosser")
	srv.GiveSlot("Crosser", player.HotbarSlot, servertest.ItemStack(t, "minecraft:compass", 1))

	srv.MovePlayerToDimension("Crosser", "minecraft:the_nether", 8.5, 40, 8.5)
	c.MarkLoaded()
	// The crossing sends its chunks asynchronously; the loaded chunk the
	// lodestone goes into is what the batch's end says has arrived.
	c.ExpectViewWithinTicks(srv, 200)
	state, ok := registry.FindState("minecraft:lodestone", nil)
	if !ok {
		t.Fatal("the registry has no lodestone")
	}
	if !srv.SetBlockInDimension("minecraft:the_nether", 5, 39, 8, state) {
		t.Fatal("the nether at the lodestone was not loaded")
	}

	click := clickBlockAndCollect(t, c, 5, 39, 8)
	expectLockSound(t, click, 5, 39, 8)
	if len(click.updates) != 1 {
		t.Fatalf("the click set %d slots (%+v), want the held one alone", len(click.updates), click.updates)
	}
	want := lodestoneCompass(t, 1, "minecraft:the_nether", 5, 39, 8)
	if got := slotUpdateOf(t, click, int16(player.HotbarSlot)); !got.Equal(want) {
		t.Errorf("slot %d = %+v, want %+v", player.HotbarSlot, got, want)
	}
	c.Disconnect()
}

// TestALodestoneCompassSurvivesASaveAndReload checks the file the tracker
// reaches disk in and what a rerun of the server makes of it: the shape a
// vanilla codec writes — the target's dimension named and its block three
// ints, tracked at its default left out — and the rejoin handing the
// stack back as the lodestone compass it was.
func TestALodestoneCompassSurvivesASaveAndReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	srv, connect := servertest.Start(t, cfg)
	shapeLodestone(t, srv, 2, 63, 1)

	c := connect().Join("Keeper")
	srv.GiveSlot("Keeper", player.HotbarSlot, servertest.ItemStack(t, "minecraft:compass", 1))

	click := clickBlockAndCollect(t, c, 2, 63, 1)
	want := lodestoneCompass(t, 1, "minecraft:overworld", 2, 63, 1)
	if got := slotUpdateOf(t, click, int16(player.HotbarSlot)); !got.Equal(want) {
		t.Fatalf("slot %d = %+v, want %+v", player.HotbarSlot, got, want)
	}
	c.Disconnect()
	srv.Shutdown()

	// The file names the tracker the vanilla way: the target compound with
	// the dimension and the three ints of the block, nothing else beside it.
	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Keeper"))
	items, ok := nbt.Get[nbt.List](root, "Inventory")
	if !ok || len(items.Items) != 1 {
		t.Fatalf("Inventory = %v, %v; want the one stack that holds something", items, ok)
	}
	entry, ok := items.Items[0].(nbt.Compound)
	if !ok {
		t.Fatalf("the stack entry is a %s, want a compound", items.Items[0].Type())
	}
	components, ok := nbt.Get[nbt.Compound](entry, "components")
	if !ok {
		t.Fatal("the stack carries no components")
	}
	tracker, ok := nbt.Get[nbt.Compound](components, "minecraft:lodestone_tracker")
	if !ok {
		t.Fatalf("the components carry no lodestone tracker: %v", components)
	}
	if _, hasTracked := tracker["tracked"]; hasTracked {
		t.Errorf("the tracker = %v, want tracked left out at its default", tracker)
	}
	target, ok := nbt.Get[nbt.Compound](tracker, "target")
	if !ok {
		t.Fatalf("the tracker carries no target: %v", tracker)
	}
	if got, _ := nbt.Get[nbt.String](target, "dimension"); got != "minecraft:overworld" {
		t.Errorf("the target's dimension = %q, want minecraft:overworld", got)
	}
	pos, ok := nbt.Get[nbt.IntArray](target, "pos")
	if !ok || len(pos) != 3 {
		t.Fatalf("the target's pos = %v, %v; want the three ints of the block", pos, ok)
	}
	for i, wantX := range []int32{2, 63, 1} {
		if pos[i] != wantX {
			t.Errorf("pos[%d] = %d, want %d", i, pos[i], wantX)
		}
	}

	// A server of its own, as after a restart, pointed at the same world.
	restarted := *cfg
	_, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Keeper")
	again.Configure()
	slots := again.Inventory()
	var found int
	for slot, stack := range slots {
		if stack.Empty() {
			continue
		}
		found++
		if slot != player.HotbarSlot {
			t.Errorf("slot %d holds %+v, want it empty", slot, stack)
			continue
		}
		if !stack.Equal(want) {
			t.Errorf("slot %d = %+v, want %+v", slot, stack, want)
		}
	}
	if found != 1 {
		t.Errorf("%d slots hold something, want the one compass", found)
	}
	again.Disconnect()
}

// TestABrokenLodestoneClearsTheCompassTarget checks the tracked flag's other
// side: once the lodestone is gone a tick takes the target off the compass,
// which spins, while the component and the tracked flag stay.
// vanilla: CompassItem.inventoryTick, LodestoneTracker.tick
func TestABrokenLodestoneClearsTheCompassTarget(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	shapeLodestone(t, srv, 2, 63, 1)

	c := connect().Join("Wanderer")
	srv.GiveSlot("Wanderer", player.HotbarSlot, servertest.ItemStack(t, "minecraft:compass", 1))

	click := clickBlockAndCollect(t, c, 2, 63, 1)
	want := lodestoneCompass(t, 1, "minecraft:overworld", 2, 63, 1)
	if got := slotUpdateOf(t, click, int16(player.HotbarSlot)); !got.Equal(want) {
		t.Fatalf("slot %d = %+v, want %+v", player.HotbarSlot, got, want)
	}

	// The lodestone broken, and a few ticks past its going.
	air, _ := registry.FindState("minecraft:air", nil)
	if !srv.SetBlock(2, 63, 1, air) {
		t.Fatal("the lodestone did not go")
	}
	servertest.AwaitTicks(t, srv, 5)
	cleared := clearedLodestoneCompass(t, 1)
	if got := srv.InventorySlot("Wanderer", player.HotbarSlot); !got.Equal(cleared) {
		t.Errorf("after the break slot %d = %+v, want the target cleared to %+v",
			player.HotbarSlot, got, cleared)
	}
	c.Disconnect()
}

// TestLodestoneCompassUnloadedTarget checks a compass pointing into an
// unloaded chunk is judged by the saved chunk: a never-generated one and one
// saved without a lodestone clear the target, a saved lodestone keeps it, and
// no check loads the chunk.
// vanilla: LodestoneTracker.tick, PoiManager.existsAtPosition
func TestLodestoneCompassUnloadedTarget(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	const dim = "minecraft:overworld"
	const never, withLodestone, without = 98765, 200000, 300000
	shapeLodestone(t, srv, withLodestone, 63, withLodestone)
	stone, _ := registry.FindState("minecraft:stone", nil)
	srv.SetLoadedBlock(without, 63, without, stone)

	c := connect().Join("Wanderer")
	servertest.HoldTicks(t, srv)
	slots := []struct {
		slot int
		at   int32
		kept bool
	}{{1, never, false}, {2, withLodestone, true}, {3, without, false}}
	for _, s := range slots {
		srv.GiveSlot("Wanderer", s.slot, lodestoneCompass(t, 1, dim, s.at, 63, s.at))
	}
	// One tick judges all three, the saved chunks read inside it.
	cleared := map[int16]bool{}
	c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
		if id != protocol.IDPlayContainerSlot {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt() // the window
		r.ReadVarInt() // the state ID
		slot := r.ReadI16()
		if servertest.MustReadSlot(t, r).Equal(clearedLodestoneCompass(t, 1)) {
			cleared[slot] = true
		}
		return false
	})
	for _, s := range slots {
		if cleared[int16(s.slot)] == s.kept {
			t.Errorf("target %d: slot %d cleared on the client = %v after one tick, want %v",
				s.at, s.slot, cleared[int16(s.slot)], !s.kept)
		}
	}
	for _, s := range slots {
		want := clearedLodestoneCompass(t, 1)
		if s.kept {
			want = lodestoneCompass(t, 1, dim, s.at, 63, s.at)
		}
		if got := srv.InventorySlot("Wanderer", s.slot); !got.Equal(want) {
			t.Errorf("target %d: slot %d = %+v, want %+v", s.at, s.slot, got, want)
		}
		if srv.ChunkLoadedInDimension(dim, s.at, s.at) {
			t.Errorf("target %d: the check loaded its chunk", s.at)
		}
	}
	c.Disconnect()
}

// TestOnlyCompassTicksLodestoneTracker checks that a stick carrying a tracker
// to an air block keeps it, while a compass loses its target.
// vanilla: Item.inventoryTick, CompassItem.inventoryTick
func TestOnlyCompassTicksLodestoneTracker(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Wanderer")
	servertest.HoldTicks(t, srv)
	tracker := protocol.LodestoneTracker{
		Target:  &protocol.LodestoneTarget{Dimension: "minecraft:overworld", X: 2, Y: 64, Z: 1},
		Tracked: true,
	}
	stick := servertest.ItemStack(t, "minecraft:stick", 1)
	stick.SetLodestoneTracker(tracker)
	compass := servertest.ItemStack(t, "minecraft:compass", 1)
	compass.SetLodestoneTracker(tracker)
	srv.GiveSlot("Wanderer", player.HotbarSlot, stick)
	srv.GiveSlot("Wanderer", player.HotbarSlot+1, compass)

	servertest.RunTicks(t, srv, 1)
	if got := srv.InventorySlot("Wanderer", player.HotbarSlot); !got.Equal(stick) {
		t.Errorf("stick = %+v, want it unchanged %+v", got, stick)
	}
	cleared := clearedLodestoneCompass(t, 1)
	if got := srv.InventorySlot("Wanderer", player.HotbarSlot+1); !got.Equal(cleared) {
		t.Errorf("compass = %+v, want %+v", got, cleared)
	}
	c.Disconnect()
}
