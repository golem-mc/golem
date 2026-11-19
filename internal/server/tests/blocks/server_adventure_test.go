package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The adventure component tests: the hands adventure mode refuses unless
// the held stack names their target — minecraft:can_place_on for the
// blocks an item builds on, minecraft:can_break for the blocks it digs.
// The components ride the wire as raw bytes (see adventuredStack below);
// the placement and the dig gates read them out of the held stack.

// adventureWorld joins one player in adventure mode, the mode that may
// build and dig only where its items say it may, and stands them on the
// flat world's stone, the ground flag of the step the dig's speed reads.
// The tick keeps running: the answers these tests read — the echoes of a
// placement, the block correction of a refused dig — travel as the tick's
// end flushes them, the acknowledgement behind each.
func adventureWorld(t *testing.T, setup ...func(*server.Server)) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := servertest.SurvivalTestConfig()
	cfg.GameMode = "adventure"
	srv, c := servertest.Join(t, cfg, "Actor", setup...)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	return srv, c
}

// adventuredTagStack builds a stack carrying one predicate whose block set is
// a named tag rather than an inline list.
func adventuredTagStack(t *testing.T, item string, component int32, tag string) protocol.ItemStack {
	t.Helper()
	stack := servertest.ItemStack(t, item, 1)
	var value protocol.Buffer
	value.PutVarInt(1) // one BlockPredicate
	value.PutBool(true)
	value.PutVarInt(0) // the named form: a tag follows
	value.PutString(tag)
	value.PutBool(false) // no state property matchers
	value.PutBool(false) // no block-entity NBT
	value.PutVarInt(0)   // no exact component matchers
	value.PutVarInt(0)   // no partial component matchers
	stack.Raw = append(stack.Raw, protocol.RawComponent{Type: component, Data: value.Bytes()})
	return stack
}

// TestAdventureBuildsOnlyOnCanPlaceOnBlocks: an adventure player holding a
// stone whose minecraft:can_place_on names dirt builds on the dirt and is
// refused on the grass block beside it. The refusal answers the vanilla
// way, the echoes of the use telling the client its prediction is undone,
// while the build against dirt lands the block and costs the stack.
// vanilla: ItemStack.useOn, ItemStack.canPlaceOnBlockInAdventureMode
func TestAdventureBuildsOnlyOnCanPlaceOnBlocks(t *testing.T) {
	t.Parallel()
	srv, c := adventureWorld(t)

	stack := servertest.AdventuredStack(t, "minecraft:stone", protocol.DataComponentCanPlaceOn, "minecraft:dirt")
	stack.Count = 3
	srv.GiveSlot("Actor", player.HotbarSlot, stack)
	c.Settle()

	// Clicking the top face of the dirt the predicate names: the stone
	// lands in the cell above it, the placement costing the stack. The ack
	// is read first — it flushes at the tick end behind everything the
	// placement queued, sweeping past the slot the cost is told in while
	// keeping the block updates for the echoes the read after it wants.
	srv.SetBlock(2, 63, 0, registry.BlockStates["minecraft:dirt"])
	c.Settle()
	stone := registry.BlockStates["minecraft:stone"]
	c.PlaceBlock(2, 63, 0, protocol.FaceTop)
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("the placement acknowledged as sequence %d, want 1", seq)
	}
	echoes := c.ExpectUseEchoes(2, 63, 0, protocol.FaceTop)
	if echoes[1] != stone {
		t.Errorf("the echo above the dirt = %d, want the placed stone (%d)", echoes[1], stone)
	}
	if got := srv.BlockAt(2, 64, 0); got != stone {
		t.Errorf("above the dirt = %d, want the stone (%d)", got, stone)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Count != 2 {
		t.Errorf("the hand after building = a stack of %d, want 2 (the placement cost one)", got.Count)
	}

	// Clicking the grass block beside it: refused, the echoes reverting the
	// prediction, the world keeping its shape.
	grass := registry.BlockStates["minecraft:grass_block"]
	srv.SetBlock(3, 63, 0, grass)
	c.Settle()
	c.PlaceBlock(3, 63, 0, protocol.FaceTop)
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("the refusal acknowledged as sequence %d, want 1", seq)
	}
	echoes = c.ExpectUseEchoes(3, 63, 0, protocol.FaceTop)
	if echoes[0] != grass {
		t.Errorf("the echo of the clicked grass = %d, want the block standing (%d)", echoes[0], grass)
	}
	if echoes[1] != 0 {
		t.Errorf("the echo above the grass = %d, want air", echoes[1])
	}
	if got := srv.BlockAt(3, 64, 0); got != 0 {
		t.Errorf("above the grass = %d, want air", got)
	}
}

// TestAdventureBuildsWithAGivenCanPlaceOn: the can_place_on a /give writes
// gates placement as the one a creative client sends does: on dirt, not on
// grass. vanilla: ItemParser, AdventureModePredicate.CODEC
func TestAdventureBuildsWithAGivenCanPlaceOn(t *testing.T) {
	t.Parallel()
	srv, c := adventureWorld(t, servertest.OpsListing(t, "Actor"))

	c.ChatCommand(`give @s stone[can_place_on={blocks:"minecraft:dirt"}] 2`)
	servertest.AwaitSlotUpdates(t, c, 1)
	// Past the pickup effect's entity the give spawns and removes.
	c.Settle()
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Count != 2 {
		t.Fatalf("the hand after /give = %+v, want the two stones", got)
	}

	stone := registry.BlockStates["minecraft:stone"]
	srv.SetBlock(2, 63, 0, registry.BlockStates["minecraft:dirt"])
	srv.SetBlock(3, 63, 0, registry.BlockStates["minecraft:grass_block"])
	c.Settle()
	c.PlaceBlock(2, 63, 0, protocol.FaceTop)
	c.ExpectBlockAck()
	if got := srv.BlockAt(2, 64, 0); got != stone {
		t.Errorf("above the dirt = %d, want the stone (%d)", got, stone)
	}
	c.Settle()
	c.PlaceBlock(3, 63, 0, protocol.FaceTop)
	c.ExpectBlockAck()
	if got := srv.BlockAt(3, 64, 0); got != 0 {
		t.Errorf("above the grass = %d, want air", got)
	}
}

// TestAdventureDigsOnlyCanBreakBlocks: an adventure player holding shears
// whose minecraft:can_break names the oak leaves mines those leaves the
// way survival does — the share a tick of shears clears the whole of the
// block — while the START of a dig on a block the predicate does not name,
// and the START of a bare hand's dig, are answered with the block's own
// state, the correction that stops the client's prediction.
// vanilla: Player.blockActionRestricted, ItemStack.canBreakBlockInAdventureMode,
// ServerPlayerGameMode.handleBlockBreakAction
func TestAdventureDigsOnlyCanBreakBlocks(t *testing.T) {
	t.Parallel()
	srv, c := adventureWorld(t)

	srv.GiveSlot("Actor", player.HotbarSlot,
		servertest.AdventuredStack(t, "minecraft:shears", protocol.DataComponentCanBreak, "minecraft:oak_leaves"))
	c.Settle()
	leaves, ok := registry.FindState("minecraft:oak_leaves", map[string]string{"persistent": "true"})
	if !ok {
		t.Fatal("no oak leaves state with persistent")
	}

	// The stone the predicate does not name: refused, the dig never booked.
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("no stone state")
	}
	srv.SetBlock(digTarget[0], digTarget[1], digTarget[2], stone)
	c.Settle()
	c.StartDigging(digTarget[0], digTarget[1], digTarget[2])
	if got := c.ExpectBlockUpdate(); got.X != digTarget[0] || got.Y != digTarget[1] ||
		got.Z != digTarget[2] || got.State != stone {
		t.Errorf("the answer to the started dig = %+v, want the stone's own state at %v", got, digTarget)
	}
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("the refused start acknowledged as sequence %d, want 1", seq)
	}
	if got := srv.BlockAt(digTarget[0], digTarget[1], digTarget[2]); got != stone {
		t.Errorf("the stone after the refused dig = %d, want it standing (%d)", got, stone)
	}
	servertest.ExpectNoDroppedItems(t, srv)

	// The leaves the predicate names: the dig runs, shears clearing the
	// block with one share, its drops falling.
	srv.SetBlock(digTarget[0], digTarget[1], digTarget[2], leaves)
	c.Settle()
	c.StartDigging(digTarget[0], digTarget[1], digTarget[2])
	c.Settle()
	servertest.ExpectAir(t, srv, digTarget)
	servertest.ExpectOneDroppedItem(t, srv, "minecraft:oak_leaves")

	// A bare hand has no component to dig with: the same correction answers.
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{})
	c.Settle()
	srv.SetBlock(digTarget[0], digTarget[1], digTarget[2], leaves)
	c.Settle()
	c.StartDigging(digTarget[0], digTarget[1], digTarget[2])
	if got := c.ExpectBlockUpdate(); got.State != leaves {
		t.Errorf("the answer to the bare hand's dig = %d, want the leaves standing (%d)", got.State, leaves)
	}
	if got := srv.BlockAt(digTarget[0], digTarget[1], digTarget[2]); got != leaves {
		t.Errorf("the leaves after the bare hand's dig = %d, want them standing (%d)", got, leaves)
	}
}

// TestAPickaxeDigsOnlyCanBreakBlocks: a pickaxe whose minecraft:can_break
// names the netherrack digs it — Efficiency V clearing the whole of the
// block with one share — and the START of a dig on the stone its component
// does not name is answered with the correction, whatever the tool would
// have done to it.
// vanilla: Player.blockActionRestricted
func TestAPickaxeDigsOnlyCanBreakBlocks(t *testing.T) {
	t.Parallel()
	srv, c := adventureWorld(t)

	pickaxe := servertest.EnchantedStack(t, "minecraft:diamond_pickaxe", "minecraft:efficiency", 5)
	pickaxe.Raw = append(pickaxe.Raw, servertest.AdventureComponentRaw(t, protocol.DataComponentCanBreak, "minecraft:netherrack"))
	srv.GiveSlot("Actor", player.HotbarSlot, pickaxe)
	// A tick puts Efficiency's mining_efficiency on the player.
	servertest.RunTicks(t, srv, 1)

	netherrack, ok := registry.FindState("minecraft:netherrack", nil)
	if !ok {
		t.Fatal("no netherrack state")
	}
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("no stone state")
	}

	// The stone the component does not name: refused, the dig never booked,
	// whatever the pickaxe would have made of it.
	srv.SetBlock(digTarget[0], digTarget[1], digTarget[2], stone)
	c.Settle()
	c.StartDigging(digTarget[0], digTarget[1], digTarget[2])
	if got := c.ExpectBlockUpdate(); got.State != stone {
		t.Errorf("the answer to the started dig = %d, want the stone's own state (%d)", got.State, stone)
	}
	if got := srv.BlockAt(digTarget[0], digTarget[1], digTarget[2]); got != stone {
		t.Errorf("the stone after the refused dig = %d, want it standing (%d)", got, stone)
	}
	servertest.ExpectNoDroppedItems(t, srv)

	// The netherrack the component names: one share, its drops falling.
	srv.SetBlock(digTarget[0], digTarget[1], digTarget[2], netherrack)
	c.Settle()
	c.StartDigging(digTarget[0], digTarget[1], digTarget[2])
	c.Settle()
	servertest.ExpectAir(t, srv, digTarget)
	servertest.ExpectOneDroppedItem(t, srv, "minecraft:netherrack")
}

// TestTheCreativeClientKeepsItsCanPlaceOnStack: the untrusted slot the Set
// Creative Mode Slot packet carries takes a stack holding can_place_on,
// checked against the component's own codec, and the slot keeps it — an
// adventure player's hand reaches the server carrying these, and the
// server must be able to hold one at all.
func TestTheCreativeClientKeepsItsCanPlaceOnStack(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder")
	c.Settle()

	stack := servertest.AdventuredStack(t, "minecraft:stone", protocol.DataComponentCanPlaceOn, "minecraft:dirt", "minecraft:grass_block")
	c.SetCreativeSlot(player.HotbarSlot, stack)
	c.Settle()
	got := srv.InventorySlot("Builder", player.HotbarSlot)
	if got.ItemID != stack.ItemID || got.Count != 1 || len(got.Raw) != 1 ||
		got.Raw[0].Type != protocol.DataComponentCanPlaceOn {
		t.Fatalf("the creative slot holds %+v, want one %d carrying can_place_on", got, stack.ItemID)
	}
}

// TestASpectatorDigsNothing: the restructured dig START that answers
// adventure's can_break keeps spectator exactly where it was — the START
// of a spectator's dig answers nothing but its acknowledgement, books no
// dig and moves no block.
func TestASpectatorDigsNothing(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.GameMode = "spectator"
	srv, c := servertest.Join(t, cfg, "Ghost")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("no stone state")
	}
	srv.SetBlock(digTarget[0], digTarget[1], digTarget[2], stone)
	c.Settle()
	c.StartDigging(digTarget[0], digTarget[1], digTarget[2])
	updates := 0
	c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlayBlockUpdate {
			updates++
		}
	})
	if updates != 0 {
		t.Errorf("the spectator's dig heard %d block updates, want none", updates)
	}
	if got := srv.BlockAt(digTarget[0], digTarget[1], digTarget[2]); got != stone {
		t.Errorf("the stone after the spectator's dig = %d, want it standing (%d)", got, stone)
	}
}

// TestACreativeSlotRefusesAnUnknownCanPlaceOnTag: a stack whose can_place_on
// names a block tag the server never sent is refused, because relaying it
// would take every client that decodes the stack down. A tag the server does
// send reads.
// vanilla: ByteBufCodecs tag decode, MappedRegistry.get
func TestACreativeSlotRefusesAnUnknownCanPlaceOnTag(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder")
	c.Settle()

	unknown := adventuredTagStack(t, "minecraft:stone", protocol.DataComponentCanPlaceOn, "minecraft:no_such_tag")
	c.SetCreativeSlot(player.HotbarSlot, unknown)
	c.Settle()
	if got := srv.InventorySlot("Builder", player.HotbarSlot); !got.Empty() {
		t.Errorf("the slot holds %+v, want the unknown tag refused", got)
	}

	known := adventuredTagStack(t, "minecraft:stone", protocol.DataComponentCanPlaceOn, "minecraft:logs")
	c.SetCreativeSlot(player.HotbarSlot, known)
	c.Settle()
	if got := srv.InventorySlot("Builder", player.HotbarSlot); got.ItemID != known.ItemID {
		t.Errorf("the slot holds %+v, want the stack with the known tag", got)
	}
}

// TestAnAdventureBucketUsesItsCanPlaceOnBlocks: an adventure hand's bucket
// scoops and pours where its minecraft:can_place_on names the clicked block,
// the gate vanilla's BucketItem.use answers to mayUseItemAt; without the
// component the same click is refused.
// vanilla: BucketItem.use, Player.mayUseItemAt
func TestAnAdventureBucketUsesItsCanPlaceOnBlocks(t *testing.T) {
	t.Parallel()
	srv, c := adventureWorld(t)

	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("the block registry has no water")
	}
	bucket := servertest.ItemIDOrDie(t, "minecraft:bucket")
	waterBucket := servertest.ItemIDOrDie(t, "minecraft:water_bucket")

	// The scoop: the empty bucket's component names the water it drains.
	srv.GiveSlot("Actor", player.HotbarSlot,
		servertest.AdventuredStack(t, "minecraft:bucket", protocol.DataComponentCanPlaceOn, "minecraft:water"))
	c.Settle()
	srv.SetBlock(4, 63, 4, water)
	c.Settle()
	c.MoveStep(4.5, 64, 4.5, true)
	servertest.UseHeld(c, 0, 90)
	got := c.ExpectBucketUseResult()
	if got.Update.X != 4 || got.Update.Y != 63 || got.Update.Z != 4 || got.Update.State != 0 {
		t.Errorf("the scoop changed block (%d,%d,%d) = %d, want the water emptied",
			got.Update.X, got.Update.Y, got.Update.Z, got.Update.State)
	}
	if got.Slot.ItemID != waterBucket {
		t.Errorf("the hand after the scoop = %+v, want a water bucket", got.Slot)
	}

	// The pour: the filled bucket's component names the stone it pours on.
	srv.GiveSlot("Actor", player.HotbarSlot,
		servertest.AdventuredStack(t, "minecraft:water_bucket", protocol.DataComponentCanPlaceOn, "minecraft:stone"))
	c.Settle()
	servertest.UseHeld(c, -90, 58.3)
	got = c.ExpectBucketUseResult()
	if got.Update.X != 5 || got.Update.Y != 64 || got.Update.Z != 4 || got.Update.State != water {
		t.Errorf("the pour changed block (%d,%d,%d) = %d, want a water source over the stone",
			got.Update.X, got.Update.Y, got.Update.Z, got.Update.State)
	}
	if got.Slot.ItemID != bucket {
		t.Errorf("the hand after the pour = %+v, want the empty bucket", got.Slot)
	}

	// A bucket whose component names nothing is refused: the water stays and
	// the hand keeps its bucket.
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: bucket, Count: 1})
	c.Settle()
	srv.SetBlock(4, 63, 4, water)
	c.Settle()
	c.MoveStep(4.5, 64, 4.5, true)
	servertest.UseHeld(c, 0, 90)
	c.Settle()
	if got := srv.BlockAt(4, 63, 4); got != water {
		t.Errorf("the water after a plain bucket's click = %d, want it standing", got)
	}
	if held := srv.InventorySlot("Actor", player.HotbarSlot); held.ItemID != bucket {
		t.Errorf("the hand after a plain bucket's click = %+v, want the empty bucket", held)
	}
}

// TestAnAdventureSpawnEggGatesOnTheBlockBehindTheLiquid: an adventure hand's
// egg used on a liquid source is gated on the block behind the hit face —
// the cell opposite the face the ray entered the liquid through, the stone
// under the water's top here — not the liquid itself. The component naming
// that stone spawns; the one naming the liquid is refused.
// vanilla: SpawnEggItem.use, Player.mayUseItemAt
func TestAnAdventureSpawnEggGatesOnTheBlockBehindTheLiquid(t *testing.T) {
	t.Parallel()
	srv, c := adventureWorld(t)
	srv.SetBlock(2, 64, 3, servertest.MustState(t, "minecraft:water", map[string]string{"level": "0"}))
	c.Settle()

	// From the eye (0.5, 65.62, 0.5) at the water's top, so the ray enters
	// through the top face and mayUseItemAt reads the stone below.
	dx, dy, dz := 2.0, 64.9-65.62, 3.0
	yaw := float32(-math.Atan2(dx, dz) * 180 / math.Pi)
	pitch := float32(-math.Atan2(dy, math.Hypot(dx, dz)) * 180 / math.Pi)
	use := func() {
		c.Send(protocol.IDPlayUseItem, func(b *protocol.Buffer) {
			b.PutVarInt(protocol.HandMain)
			b.PutVarInt(1)
			b.PutF32(yaw)
			b.PutF32(pitch)
		})
	}

	// The component names the stone behind the liquid: the egg spawns into
	// the liquid's cell.
	srv.GiveSlot("Actor", player.HotbarSlot,
		servertest.AdventuredStack(t, "minecraft:cow_spawn_egg", protocol.DataComponentCanPlaceOn, "minecraft:stone"))
	c.Settle()
	use()
	cow := c.AwaitSpawn(protocol.EntityTypeCow)
	if cow.X != 2.5 || cow.Y != 64 || cow.Z != 3.5 {
		t.Errorf("cow spawned at (%v, %v, %v), want (2.5, 64, 3.5)", cow.X, cow.Y, cow.Z)
	}

	// The component names the liquid itself: the gate reads the stone, so
	// the click is refused and no body arrives.
	srv.GiveSlot("Actor", player.HotbarSlot,
		servertest.AdventuredStack(t, "minecraft:cow_spawn_egg", protocol.DataComponentCanPlaceOn, "minecraft:water"))
	c.Settle()
	use()
	spawns := 0
	c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlaySpawnEntity {
			spawns++
		}
	})
	if spawns != 0 {
		t.Errorf("the water-named egg spawned %d bodies, want none", spawns)
	}
}

// digTarget is the cell the dig tests break, beside the actor on the flat
// world's stone.
var digTarget = [3]int32{2, 64, 0}
