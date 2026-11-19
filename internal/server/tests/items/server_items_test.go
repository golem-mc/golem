package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The item-entity tests: what a survival break leaves on the ground, what
// the pickaxe rule makes of bare hands, the pickup a walking player makes,
// the throw of a Q, the cost of building, and the one-packet break of the
// instant blocks. Item entities are a whole other protocol citizen — spawn,
// metadata, movement, pickup, removal — so the readers here are their own
// rather than the block-change ones: what a break broadcasts and what an
// item adds to it overlap, and each is wrong noise for the other.

// TestBreakingStoneWithAPickaxeDropsCobblestone checks the survival reward
// for digging: stone broken with a pickaxe in the hand drops itself as an
// item entity — cobblestone, the stone's loot table yield without silk
// touch — at the centre of the block it came out of.
func TestBreakingStoneWithAPickaxeDropsCobblestone(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, c := servertest.Join(t, cfg, "Miner")

	// A wooden pickaxe in the hand, the way the creative menu would leave
	// one in a survival player's hotbar for the test.
	pickaxe, ok := registry.ItemID("minecraft:wooden_pickaxe")
	if !ok {
		t.Fatal("the item registry has no wooden pickaxe")
	}
	srv.GiveSlot("Miner", player.HotbarSlot, protocol.ItemStack{ItemID: pickaxe, Count: 1})

	// The break settles as one: the block gone from the world and told to
	// everyone, and the cobblestone out of it lying in the hole.
	const x, y, z = int32(2), int32(63), int32(2)
	c.MoveStep(0.5, 64, 0.5, true)
	c.BreakBlock(x, y, z)

	change, item := c.WaitBreakSettled()
	if change.X != x || change.Y != y || change.Z != z {
		t.Errorf("block update at (%d, %d, %d), want the block that was broken", change.X, change.Y, change.Z)
	}
	if change.State != 0 {
		t.Errorf("block after break = %d, want 0 (air)", change.State)
	}
	cobblestone, _ := registry.ItemID("minecraft:cobblestone")
	if item.Stack.ItemID != cobblestone || item.Stack.Count != 1 {
		t.Errorf("the drop = %+v, want one cobblestone (item %d)", item.Stack, cobblestone)
	}
	// The drop's box is centred within a quarter block of the block it
	// came out of on each axis, Block.popResource's jitter, so its
	// position - the feet, as every entity's is - sits half the item's
	// height under that: pos + 0.5 ± 0.25, and pos.y + 0.375 ± 0.25.
	const jitter = 0.25 + 1e-9
	if math.Abs(item.X-float64(x)-0.5) > jitter ||
		math.Abs(item.Y-float64(y)-0.375) > jitter ||
		math.Abs(item.Z-float64(z)-0.5) > jitter {
		t.Errorf("the drop spawned at (%v, %v, %v), want its box within a quarter block of the centre of (%d, %d, %d)",
			item.X, item.Y, item.Z, x, y, z)
	}
	if got := srv.BlockAt(x, y, z); got != 0 {
		t.Errorf("world block after break = %d, want 0 (air)", got)
	}
}

// TestBreakingStoneWithBareHandsDropsNothing checks the tool rule the drop
// table carries: stone is pickaxe-mined, and without a pickaxe held the
// block still breaks — slower, the client's business — but leaves nothing
// behind for the trouble. The bare hand is a hundred and fifty ticks to the
// stone, so the dig runs on the step clock rather than the wall's.
func TestBreakingStoneWithBareHandsDropsNothing(t *testing.T) {
	t.Parallel()
	clock := servertest.NewStepClock()
	cfg := servertest.SurvivalConfig()

	srv, c := servertest.Join(t, cfg, "Miner", func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	// On the ground: the dig's pace counts the standing.
	c.MoveToGround(0.5, 64, 0.5, true)

	const x, y, z = int32(2), int32(63), int32(2)
	c.BreakBlock(x, y, z)
	// The settle has the dig on the books before the steps run: the
	// packets are handled as they are read, and steps taken before the
	// reader reaches them are ticks the dig's count misses.
	c.Settle()
	servertest.StepTicks(t, srv, clock, 170)

	// The break is processed: the world heard it and told everyone, the
	// breaker included.
	change := c.WaitBlockChanged()
	if change.State != 0 {
		t.Errorf("block after bare-hand break = %d, want 0 (stone breaks, it just drops nothing)", change.State)
	}
	// The silence is the assertion: a cobblestone would have been spawned
	// and sent before the ping's answer could arrive.
	c.ExpectNothingItemish()
}

// TestBreakingARailByHandDropsTheRail breaks a rail with nothing held:
// vanilla registers the rail without requiresCorrectToolForDrops, so a bare
// hand has earned the drops and one rail lies in the hole.
func TestBreakingARailByHandDropsTheRail(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, c := servertest.Join(t, cfg, "Miner")

	const x, y, z = int32(2), int32(63), int32(2)
	rail, ok := registry.FindState("minecraft:rail", map[string]string{"shape": "north_south"})
	if !ok {
		t.Fatal("the block registry has no north-south rail")
	}
	if !srv.SetBlock(x, y, z, rail) {
		t.Fatal("the world did not take the rail")
	}
	c.Settle()

	c.BreakBlock(x, y, z)

	change := c.WaitBlockChanged()
	if change.State != 0 {
		t.Errorf("block after break = %d, want 0 (air)", change.State)
	}
	if got := srv.BlockAt(x, y, z); got != 0 {
		t.Errorf("world block after break = %d, want 0", got)
	}
	servertest.ExpectOneDroppedItem(t, srv, "minecraft:rail")
}

// TestDirtDropsAndIsPickedUpWhenWalkedOnto checks the whole item round trip
// in one: a survival break of dirt — no tool needed — drops dirt, and a
// player who steps over it picks it up into the hotbar, sees the slot they
// gained, and is shown the item flying to them before it goes away.
func TestDirtDropsAndIsPickedUpWhenWalkedOnto(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, c := servertest.Dial(t, cfg)
	c.Login("Digger")
	c.Configure()
	entityID := c.EnterWorldAndReportEntity("Digger")

	// Dirt where the flat world has stone, one block out of the player's
	// way: broken by hand it drops itself, the drop settling in the hole.
	dirt := registry.BlockStates["minecraft:dirt"]
	if dirt == 0 {
		t.Fatal("the block registry has no dirt")
	}
	// The join's first chunk batch carries only the chunks generated by
	// then, and World.SetBlock refuses an unloaded one.
	servertest.PollUntil(t, 10*time.Second, func() bool { return srv.ChunkLoadedInDimension(world.DimensionName, 0, 0) })
	if !srv.SetBlock(3, 63, 3, dirt) {
		t.Fatal("the world did not take the dirt")
	}
	c.BreakBlock(3, 63, 3)

	item := c.ExpectDroppedItem()
	dirtItem, _ := registry.ItemID("minecraft:dirt")
	if item.Stack.ItemID != dirtItem || item.Stack.Count != 1 {
		t.Errorf("the drop = %+v, want one dirt (item %d)", item.Stack, dirtItem)
	}

	// Step into the hole the block came out of. The player's body grown by
	// the pickup range overlaps where the drop settles, and within the
	// drop's ten-tick pickup delay the item flies to them.
	c.MoveStep(3.5, 63, 3.5, true)

	slot, stack := c.ExpectContainerSlot()
	if slot != int16(player.HotbarSlot) {
		t.Errorf("pickup filled slot %d, want %d (the first empty hotbar slot)", slot, player.HotbarSlot)
	}
	if stack.ItemID != dirtItem || stack.Count != 1 {
		t.Errorf("slot %d = %+v, want the picked-up dirt", slot, stack)
	}
	c.ExpectPickup(item.EntityID, entityID)
}

// TestCreativePlayerPicksUpADroppedStack checks a creative player takes a
// dropped stack like a survival one.
// vanilla: ItemEntity.playerTouch
func TestCreativePlayerPicksUpADroppedStack(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.GameMode = "creative"

	srv, c := servertest.Dial(t, cfg)
	c.Login("Builder")
	c.Configure()
	entityID := c.EnterWorldAndReportEntity("Builder")

	// The stack stands where the player does, so the pickup range covers it
	// once the drop's ten-tick delay runs out.
	srv.SpawnItemEntity(0.5, 64, 0.5, "minecraft:dirt", 1)
	item := c.ExpectDroppedItem()

	slot, stack := c.ExpectContainerSlot()
	if slot != int16(player.HotbarSlot) {
		t.Errorf("pickup filled slot %d, want %d (the first empty hotbar slot)", slot, player.HotbarSlot)
	}
	dirtItem, _ := registry.ItemID("minecraft:dirt")
	if stack.ItemID != dirtItem || stack.Count != 1 {
		t.Errorf("slot %d = %+v, want the picked-up dirt", slot, stack)
	}
	c.ExpectPickup(item.EntityID, entityID)
}

// TestCreativePlayerPickingUpWithNoRoomVoidsTheStack checks the creative
// pickup with a full inventory: the whole stack is taken and the item
// removed though nothing fit, the take playing with its full count.
// vanilla: ItemEntity.playerTouch, Inventory.add
func TestCreativePlayerPickingUpWithNoRoomVoidsTheStack(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.GameMode = "creative"

	srv, c := servertest.Dial(t, cfg)
	c.Login("Builder")
	c.Configure()
	entityID := c.EnterWorldAndReportEntity("Builder")

	// Every item slot holds a full stack dirt cannot merge with, so the
	// pickup has nowhere to land.
	stone, _ := registry.ItemID("minecraft:stone")
	for slot := player.MainSlot; slot < player.MainSlot+player.MainInvSlots; slot++ {
		srv.GiveSlot("Builder", slot, protocol.ItemStack{ItemID: stone, Count: 64})
	}

	// The stack's centre half an item above the ground, where its box rests
	// rather than sits buried in the block below and gets pushed out.
	srv.SpawnItemEntity(0.5, 64+entity.ItemHalfSize, 0.5, "minecraft:dirt", 3)
	item := c.ExpectDroppedItem()

	collected, collector, count := servertest.ReadPickupItem(c, c.ExpectItemPacket(protocol.IDPlayPickupItem))
	if collected != item.EntityID || collector != entityID {
		t.Errorf("pickup collected %d by %d, want %d by %d", collected, collector, item.EntityID, entityID)
	}
	if count != 3 {
		t.Errorf("pickup count = %d, want 3 (the whole stack a creative player voids)", count)
	}

	remove := protocol.NewReader(c.ExpectItemPacket(protocol.IDPlayRemoveEntities))
	if got := remove.ReadVarInt(); got != 1 {
		t.Fatalf("removed entities = %d, want the one that was picked up", got)
	}
	if got := remove.ReadVarInt(); got != item.EntityID {
		t.Errorf("removed entity = %d, want %d", got, item.EntityID)
	}
	if err := remove.Err(); err != nil {
		t.Fatalf("remove entities: %v", err)
	}
	if remove.Remaining() != 0 {
		t.Errorf("remove entities has %d bytes left over", remove.Remaining())
	}
}

// TestCreativePlayerPickingUpAPartialFitVoidsTheRemainder checks a creative
// pickup the inventory can only partly hold: the part that fits lands, the
// remainder is voided, the item removed, and the take plays the full count.
// vanilla: ItemEntity.playerTouch, Inventory.add
func TestCreativePlayerPickingUpAPartialFitVoidsTheRemainder(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.GameMode = "creative"

	srv, c := servertest.Dial(t, cfg)
	c.Login("Builder")
	c.Configure()
	entityID := c.EnterWorldAndReportEntity("Builder")

	// Every item slot holds a full stack dirt cannot merge with, but the
	// first main slot holds dirt with room for four more.
	dirt, _ := registry.ItemID("minecraft:dirt")
	stone, _ := registry.ItemID("minecraft:stone")
	for slot := player.MainSlot; slot < player.MainSlot+player.MainInvSlots; slot++ {
		srv.GiveSlot("Builder", slot, protocol.ItemStack{ItemID: stone, Count: 64})
	}
	srv.GiveSlot("Builder", player.MainSlot, protocol.ItemStack{ItemID: dirt, Count: 60})

	// The stack's centre half an item above the ground, where its box rests
	// rather than sits buried in the block below and gets pushed out.
	srv.SpawnItemEntity(0.5, 64+entity.ItemHalfSize, 0.5, "minecraft:dirt", 64)
	item := c.ExpectDroppedItem()

	slot, stack := c.ExpectContainerSlot()
	if slot != int16(player.MainSlot) {
		t.Errorf("pickup filled slot %d, want %d (the slot with room)", slot, player.MainSlot)
	}
	if stack.ItemID != dirt || stack.Count != 64 {
		t.Errorf("slot %d = %+v, want it topped up to 64 dirt", slot, stack)
	}

	collected, collector, count := servertest.ReadPickupItem(c, c.ExpectItemPacket(protocol.IDPlayPickupItem))
	if collected != item.EntityID || collector != entityID {
		t.Errorf("pickup collected %d by %d, want %d by %d", collected, collector, item.EntityID, entityID)
	}
	if count != 64 {
		t.Errorf("pickup count = %d, want 64 (the whole stack a creative player voids)", count)
	}

	remove := protocol.NewReader(c.ExpectItemPacket(protocol.IDPlayRemoveEntities))
	if got := remove.ReadVarInt(); got != 1 {
		t.Fatalf("removed entities = %d, want the one that was picked up", got)
	}
	if got := remove.ReadVarInt(); got != item.EntityID {
		t.Errorf("removed entity = %d, want %d", got, item.EntityID)
	}
	if err := remove.Err(); err != nil {
		t.Fatalf("remove entities: %v", err)
	}
	if remove.Remaining() != 0 {
		t.Errorf("remove entities has %d bytes left over", remove.Remaining())
	}
}

// TestAnItemWaitsForItsChunkToLoad lays a stack where no chunk is loaded:
// it neither falls through the air the missing terrain reads as nor counts
// down its pickup delay.
// vanilla: ServerLevel.tick
func TestAnItemWaitsForItsChunkToLoad(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.SurvivalConfig(), "Watcher")
	srv.StopTick()

	const x, y, z = 1000.5, 64 + entity.ItemHalfSize, 1000.5
	if srv.ChunkLoadedInDimension(world.DimensionName, 1000, 1000) {
		t.Fatal("the far chunk is loaded, want one nobody holds")
	}
	id := srv.SpawnItemEntity(x, y, z, "minecraft:dirt", 1)
	srv.TickEntities(20)

	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 || items[0].ID != id {
		t.Fatalf("items = %+v, want the one stack", items)
	}
	if got := items[0]; got.Y != y || got.PickupDelay != entity.BreakPickupDelay {
		t.Errorf("after 20 ticks the stack stands at y %v with pickup delay %d, want y %v and %d untouched",
			got.Y, got.PickupDelay, y, entity.BreakPickupDelay)
	}
}

// TestSpectatorDoesNotPickUpADroppedStack checks a spectator walking over a
// stack leaves it where it lies.
// vanilla: Player.aiStep, EntitySelector.NO_SPECTATORS
func TestSpectatorDoesNotPickUpADroppedStack(t *testing.T) {
	t.Parallel()
	clock := servertest.NewStepClock()
	cfg := servertest.SurvivalConfig()
	cfg.GameMode = "spectator"

	srv, c := servertest.Join(t, cfg, "Ghost", func(s *server.Server) {
		s.SetTickClock(clock, nil)
	})
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	// The stack stands where the spectator does. Past the ten-tick delay
	// the tick would have handed it over, did any body but a spectator's
	// reach it.
	srv.SpawnItemEntity(0.5, 64, 0.5, "minecraft:dirt", 1)
	c.ExpectDroppedItem()
	servertest.StepTicks(t, srv, clock, 15)

	// The silence is the assertion: a pickup would have been a Container
	// Slot, a Take Item Entity and a removal before the ping's answer.
	c.ExpectNothingItemish()
}

// TestDropKeyThrowsOneItem checks the Q key: one item of the held stack
// leaves the hand — the slot told back one stone lighter — and lands in the
// world in front of the player's eyes, moving away in the look direction
// with vanilla's strength.
func TestDropKeyThrowsOneItem(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, c := servertest.Join(t, cfg, "Dropper")

	stone, _ := registry.ItemID("minecraft:stone")
	srv.GiveSlot("Dropper", player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 3})
	c.DropKey(false)

	// The held slot, one stone lighter.
	slot, stack := c.ExpectContainerSlot()
	if slot != int16(player.HotbarSlot) {
		t.Errorf("corrected slot = %d, want %d (the held one)", slot, player.HotbarSlot)
	}
	if stack.ItemID != stone || stack.Count != 2 {
		c.T.Errorf("held slot = %+v, want two stones left of three", stack)
	}

	// And the thrown item: south (+Z) at the throw strength, from the
	// thrower's own x and z with the feet three tenths of a block under
	// the eyes, rising on the vanilla drop's own lift.
	item := c.ExpectDroppedItem()
	if item.Stack.ItemID != stone || item.Stack.Count != 1 {
		c.T.Errorf("thrown item = %+v, want one stone", item.Stack)
	}
	const nearEnough = 0.021 // the spread's two hundredths, and rounding room
	if math.Abs(item.Vz-0.3) > nearEnough || math.Abs(item.Vx) > nearEnough {
		c.T.Errorf("throw velocity = (%v, %v, %v), want 0.3 south (+Z) and nothing sideways but the spread",
			item.Vx, item.Vy, item.Vz)
	}
	if item.Vy <= 0 || item.Vy >= 0.2 {
		c.T.Errorf("throw lift = %v, want the vanilla drop's rise of a tenth under a triangle spread a tenth either way", item.Vy)
	}
	if math.Abs(item.X-0.5) > 1e-9 || math.Abs(item.Z-0.5) > 1e-9 ||
		math.Abs(item.Y-(64+1.62-entity.ThrowSpawnBelow)) > 1e-9 {
		c.T.Errorf("thrown from (%v, %v, %v), want the thrower's own x and z with the feet at %v",
			item.X, item.Y, item.Z, 64+1.62-entity.ThrowSpawnBelow)
	}
}

// TestASneakingThrowSpawnsUnderTheCrouchingEyes checks that the throw's
// spawn height follows the pose: vanilla's getEyeY is the pose's own eye
// height, so a crouching thrower's item leaves from a block and a quarter
// up, not from the standing eyes.
func TestASneakingThrowSpawnsUnderTheCrouchingEyes(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, c := servertest.Join(t, cfg, "Croucher")

	stone, _ := registry.ItemID("minecraft:stone")
	srv.GiveSlot("Croucher", player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 3})

	// The crouch first. The metadata the sneak broadcasts is the proof the
	// pose has settled before the throw is sent.
	c.SendSneak(true)
	if got := c.ExpectMetadata(); got.Pose != protocol.PoseSneaking {
		c.T.Errorf("pose = %d, want %d (sneaking)", got.Pose, protocol.PoseSneaking)
	}

	c.DropKey(false)

	// The held slot, one stone lighter, ahead of the throw's spawn.
	slot, stack := c.ExpectContainerSlot()
	if slot != int16(player.HotbarSlot) {
		c.T.Errorf("corrected slot = %d, want %d (the held one)", slot, player.HotbarSlot)
	}
	if stack.ItemID != stone || stack.Count != 2 {
		c.T.Errorf("held slot = %+v, want two stones left of three", stack)
	}

	item := c.ExpectDroppedItem()
	if item.Stack.ItemID != stone || item.Stack.Count != 1 {
		c.T.Errorf("thrown item = %+v, want one stone", item.Stack)
	}
	// The crouching body's eyes sit 1.27 up, the vanilla POSES entry the
	// pose's own height comes from.
	const nearEnough = 1e-6
	if math.Abs(item.Y-(64+1.27-entity.ThrowSpawnBelow)) > nearEnough {
		c.T.Errorf("thrown from y = %v, want the feet %v under the crouching eyes at %v",
			item.Y, entity.ThrowSpawnBelow, 64+1.27)
	}
	if math.Abs(item.X-0.5) > nearEnough || math.Abs(item.Z-0.5) > nearEnough {
		c.T.Errorf("thrown from (%v, %v), want the thrower's own x and z", item.X, item.Z)
	}
}

// TestDeathDropsSpawnUnderTheEyesAndScatter checks the toss a death gives
// each carried stack: the item laid at the body's own x and z with its feet
// three tenths of a block under the eyes, thrown on a random heading at up
// to half a block a tick and rising the fifth a random drop gets. A death
// drops a whole inventory at once, so the thirty-six tosses show the heading
// covering a full circle and the speed a full half, rather than the tenth
// either way a fixed scatter would give.
// vanilla: LivingEntity.createItemStackToDrop
func TestDeathDropsSpawnUnderTheEyesAndScatter(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, c, _ := servertest.JoinAs(t, cfg, "Dier")

	stone, _ := registry.ItemID("minecraft:stone")
	for slot := player.MainSlot; slot < player.OffHandSlot; slot++ {
		srv.GiveSlot("Dier", slot, protocol.ItemStack{ItemID: stone, Count: 1})
	}

	// A fall of thirty blocks: twenty health against twenty-seven damage.
	c.MoveStep(0.5, 94, 0.5, false)
	c.MoveStep(0.5, 64, 0.5, true)

	// The eyes of a standing body sit 1.62 above the feet, so the item's
	// feet land 1.32 above the ground it died on.
	const wantY = 64 + 1.62 - entity.ThrowSpawnBelow
	var fastest float64
	for i := range player.MainInvSlots {
		item := c.ExpectDroppedItem()
		if math.Abs(item.X-0.5) > 1e-9 || math.Abs(item.Z-0.5) > 1e-9 ||
			math.Abs(item.Y-wantY) > 1e-9 {
			t.Errorf("drop %d spawned at (%v, %v, %v), want the body's own x and z with the feet at %v",
				i, item.X, item.Y, item.Z, wantY)
		}
		if math.Abs(item.Vy-0.2) > 1e-3 {
			t.Errorf("drop %d rose at %v, want the fifth a random drop gets", i, item.Vy)
		}
		speed := math.Hypot(item.Vx, item.Vz)
		if speed > 0.5+1e-3 {
			t.Errorf("drop %d flew sideways at %v, want under half a block a tick", i, speed)
		}
		if speed > fastest {
			fastest = speed
		}
	}
	if fastest < 0.3 {
		t.Errorf("the fastest toss was %v, want the spread approaching half a block a tick", fastest)
	}
}

// TestADeathWhileSneakingDropsUnderTheCrouchingEyes checks that the death
// drop follows the pose the body died in: a crouching death's eyes sit 1.27
// above the feet, so its carried stack leaves three tenths under those, not
// under the standing eyes.
func TestADeathWhileSneakingDropsUnderTheCrouchingEyes(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, c, _ := servertest.JoinAs(t, cfg, "Dier")

	stone, _ := registry.ItemID("minecraft:stone")
	srv.GiveSlot("Dier", player.MainSlot, protocol.ItemStack{ItemID: stone, Count: 1})

	// The crouch first, the metadata proof the pose has settled before the
	// fall sends the body down.
	c.SendSneak(true)
	if got := c.ExpectMetadata(); got.Pose != protocol.PoseSneaking {
		c.T.Errorf("pose = %d, want %d (sneaking)", got.Pose, protocol.PoseSneaking)
	}

	// A fall of thirty blocks: twenty health against twenty-seven damage.
	c.MoveStep(0.5, 94, 0.5, false)
	c.MoveStep(0.5, 64, 0.5, true)

	const wantY = 64 + 1.27 - entity.ThrowSpawnBelow
	item := c.ExpectDroppedItem()
	if math.Abs(item.Y-wantY) > 1e-9 {
		t.Errorf("the crouching death's drop spawned at y = %v, want %v under the crouching eyes",
			item.Y, wantY)
	}
}

// TestSurvivalPlacementConsumesTheStack checks the cost of building: one
// item out of the held stack per block placed, told back to the client with
// the slot update. Creative builds out of nothing and gets no such packet.
func TestSurvivalPlacementConsumesTheStack(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, c := servertest.Join(t, cfg, "Builder")
	// Where the clicked ground lies within the block interaction's reach of
	// the builder: vanilla refuses a placement past the reach.
	c.MoveToGround(4.5, 64, 4.5, true)
	c.Settle()

	stone, _ := registry.ItemID("minecraft:stone")
	srv.GiveSlot("Builder", player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 2})

	// A block placed on top of the stone at (5, 63, 5), away from the player.
	c.PlaceBlock(5, 63, 5, protocol.FaceTop)

	slot, stack := c.ExpectContainerSlot()
	if slot != int16(player.HotbarSlot) {
		c.T.Errorf("corrected slot = %d, want %d (the stack was placed from)", slot, player.HotbarSlot)
	}
	if stack.ItemID != stone || stack.Count != 1 {
		c.T.Errorf("held slot = %+v, want one stone left of two", stack)
	}
	if got := srv.BlockAt(5, 64, 5); got != 1 {
		c.T.Errorf("placed block = %d, want 1 (stone)", got)
	}
}

// TestInstantBreakGrassBreaksOnStartDigging checks the one-packet survival
// break: short grass has nothing to dig through, so the client never sends
// the finished-digging status — the block has to be gone on the
// started-digging click alone or it never goes at all.
func TestInstantBreakGrassBreaksOnStartDigging(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, c := servertest.Join(t, cfg, "Mower")

	// Short grass on top of the ground, away from the player.
	grass := registry.BlockStates["minecraft:short_grass"]
	if grass == 0 {
		t.Fatal("the block registry has no short grass")
	}
	if !srv.SetBlock(1, 64, 1, grass) {
		t.Fatal("the world did not take the grass")
	}

	// The click that starts digging is the whole break: the change and its
	// acknowledgement say the server processed it on this packet alone.
	c.StartDiggingHere(1, 64, 1)
	change := c.WaitBlockChanged()
	if change.X != 1 || change.Y != 64 || change.Z != 1 {
		t.Errorf("instant break update at (%d, %d, %d), want (1, 64, 1)", change.X, change.Y, change.Z)
	}
	if change.State != 0 {
		t.Fatalf("block after a started dig = %d, want 0 (instant blocks break on the click alone)", change.State)
	}
	if got := srv.BlockAt(1, 64, 1); got != 0 {
		t.Errorf("world block after a started dig = %d, want 0 (air)", got)
	}
	// The drop table gives short grass nothing by default — the seed is a
	// chance the table never reaches — so nothing was spawned either.
	c.ExpectNothingItemish()
}

// TestCreativeSwordBreaksNothing checks the refusal a tool's
// can_destroy_blocks_in_creative puts on a creative dig: the sword — and the
// trident and the mace, by the same component — may not break blocks in
// creative, and the refused dig is answered with the block's own state
// resent and the acknowledgement behind it. A pickaxe, whose component leaves
// the flag out, digs all the same.
func TestCreativeSwordBreaksNothing(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Swordsman") // creative, the mode the refusal guards
	c.MoveToGround(4.5, 64, 4.5, true)
	c.Settle()

	stone := servertest.BlockState(t, "minecraft:stone")
	if got := srv.BlockAt(5, 63, 5); got != stone {
		t.Fatalf("the ground at (5, 63, 5) is %d, want %d (stone)", got, stone)
	}

	servertest.HoldItem(t, c, "minecraft:iron_sword")
	c.StartDiggingHere(5, 63, 5)
	change := c.WaitBlockChanged()
	if change.X != 5 || change.Y != 63 || change.Z != 5 {
		t.Errorf("the sword's dig answered at (%d, %d, %d), want (5, 63, 5)", change.X, change.Y, change.Z)
	}
	if change.State != stone {
		t.Errorf("the sword's dig changed the block to %d, want %d (the state resent, nothing broken)", change.State, stone)
	}
	if got := srv.BlockAt(5, 63, 5); got != stone {
		t.Errorf("world block after the sword's dig = %d, want %d (stone standing)", got, stone)
	}

	// The pickaxe's tool component leaves can_destroy_blocks_in_creative
	// out, which is true: its dig breaks the block all the same.
	servertest.HoldItem(t, c, "minecraft:iron_pickaxe")
	c.StartDiggingHere(5, 63, 5)
	if change := c.WaitBlockChanged(); change.State != 0 {
		t.Errorf("the pickaxe's dig left the block at %d, want 0 (air)", change.State)
	}
}

// TestDeathDropsTheBodySlot checks that a death scatters the stack in the
// body equipment slot too.
func TestDeathDropsTheBodySlot(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Dier")

	armor, _ := registry.ItemID("minecraft:leather_horse_armor")
	srv.GiveBody("Dier", protocol.ItemStack{ItemID: armor, Count: 1})

	c.MoveStep(0.5, 94, 0.5, false)
	c.MoveStep(0.5, 64, 0.5, true)
	if item := c.ExpectDroppedItem(); item.Stack.ItemID != armor {
		t.Errorf("death dropped %+v, want the horse armor", item.Stack)
	}
}

// TestDeathDropsTheInventory checks what a death leaves behind: everything
// the player was carrying is scattered as item entities where they fell,
// and the bars come back full with the inventory empty behind them.
func TestDeathDropsTheInventory(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, c := servertest.Join(t, cfg, "Dier")

	diamond, _ := registry.ItemID("minecraft:diamond")
	stone, _ := registry.ItemID("minecraft:stone")
	srv.GiveSlot("Dier", player.HotbarSlot, protocol.ItemStack{ItemID: diamond, Count: 7})
	// Slot 9 is the first of the main inventory rows, out of the hand.
	srv.GiveSlot("Dier", 9, protocol.ItemStack{ItemID: stone, Count: 3})

	// A fall of thirty blocks: twenty health against twenty-seven damage.
	c.MoveStep(0.5, 94, 0.5, false)
	c.MoveStep(0.5, 64, 0.5, true)

	// The stacks came off the body before the respawn screen did: two item
	// entities, holding exactly what was carried.
	gotDiamond, gotStone := false, false
	for !gotDiamond || !gotStone {
		item := c.ExpectDroppedItem()
		switch {
		case item.Stack.ItemID == diamond && item.Stack.Count == 7:
			gotDiamond = true
		case item.Stack.ItemID == stone && item.Stack.Count == 3:
			gotStone = true
		default:
			c.T.Errorf("death dropped an unexpected %+v", item.Stack)
		}
	}

	// The respawn, asked for and granted, with the bars full again — which
	// the empty inventory has nothing to say about, a whole death having
	// emptied it. The packets the respawn sends are written straight to the
	// connection among whatever the mailbox still has, so they are waited
	// for by name in order rather than one strict read each.
	c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})
	var sawRespawn, sawHealth bool
	for packetID, data := range c.Packets(servertest.ExpectTimeout) {
		switch packetID {
		case protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayTeleportEntity, protocol.IDPlayChunkData,
			protocol.IDPlayChunkBatchStart, protocol.IDPlayChunkBatchFinished,
			protocol.IDPlayCenterChunk, protocol.IDPlaySetEntityMotion:
			// Mailbox traffic still in flight; read past it.
		case protocol.IDPlayCombatDeath, protocol.IDPlayEntityEvent, protocol.IDPlaySystemChat:
			// The death's own memorial traffic, still draining from the
			// mailbox; read past it.
		case protocol.IDPlayContainerSlot:
			// The slots the death emptied, told to the dying client the way
			// the vanilla server's container sync is; the emptying itself
			// is the death-screen test's to check.
		case protocol.IDPlayRemoveEntities:
			// The bodies the client held, taken back out ahead of the
			// Respawn, which leaves the client's world standing; the tick
			// pairs whatever is in range at the spawn again.
		case protocol.IDPlayRespawn:
			sawRespawn = true
		case protocol.IDPlaySetHealth:
			// The itemBackground reader treats health as background,
			// so the respawn's own is looked for here, by hand.
			if !sawRespawn {
				c.T.Fatal("the bars arrived before the respawn they belong to")
			}
			r := protocol.NewReader(data)
			if got := r.ReadF32(); got != 20 {
				c.T.Errorf("health after respawn = %v, want 20 (the bars a death empties)", got)
			}
			r.ReadVarInt() // food
			r.ReadF32()    // saturation
			if err := r.Err(); err != nil {
				c.T.Fatalf("set health: %v", err)
			}
			if r.Remaining() != 0 {
				c.T.Errorf("set health has %d bytes left over", r.Remaining())
			}
			sawHealth = true
		default:
			c.T.Fatalf("unexpected packet 0x%02X in the respawn sequence", packetID)
		}
		if sawHealth {
			break
		}
	}
	if !sawHealth {
		c.T.Fatalf("no health after the respawn within %s", servertest.ExpectTimeout)
	}
}

// TestLateJoinerSeesTheItemsOnTheGround checks the joiner's half of the item
// entities: a player who connects after something was dropped is shown it,
// with the stack it holds, in the join sequence alongside the world the
// players already in it can see.
func TestLateJoinerSeesTheItemsOnTheGround(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, connect := servertest.Start(t, cfg)

	digger := connect().Join("Digger")

	dirt := registry.BlockStates["minecraft:dirt"]
	if dirt == 0 {
		t.Fatal("the block registry has no dirt")
	}
	if !srv.SetBlock(4, 63, 4, dirt) {
		t.Fatal("the world did not take the dirt")
	}
	digger.BreakBlock(4, 63, 4)
	digger.ExpectDroppedItem()

	// The player who arrives now, with the dirt already lying in the world.
	// Their join sequence carries the item entity along with the rest of
	// what is there.
	late := connect()
	late.Login("Latecomer")
	late.Configure()

	var items []protocol.ItemStack
	itemSpawnsSeen := 0
	for packetID, data := range late.Packets(servertest.JoinTimeout) {
		// The item's spawn rides the tick after its chunk went out, which
		// a slow machine can put past the join's own chunk batch: the read
		// goes on until it has come.
		if len(items) > 0 {
			break
		}
		switch packetID {
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			r.ReadVarInt() // entity ID
			r.ReadUUID()
			if r.ReadVarInt() != protocol.EntityTypeItem {
				continue // a player's body, which the join already tells them about
			}
			r.ReadF64()
			r.ReadF64()
			r.ReadF64()
			r.ReadLpVec3()
			r.ReadAngle()
			r.ReadAngle()
			r.ReadAngle()
			r.ReadVarInt() // data, of which an item carries none
			if err := r.Err(); err != nil {
				late.T.Fatalf("spawn entity (item): %v", err)
			}
			itemSpawnsSeen++
		case protocol.IDPlayEntityMetadata:
			if itemSpawnsSeen == 0 {
				continue // the player bodies the join spawns
			}
			itemSpawnsSeen--
			m := protocol.NewReader(data)
			m.ReadVarInt() // entity ID, already counted above
			if index := m.ReadU8(); index != protocol.MetadataIndexItemSlot {
				late.T.Fatalf("item metadata index %d, want %d", index, protocol.MetadataIndexItemSlot)
			}
			if fieldType := m.ReadVarInt(); fieldType != protocol.MetadataSlot {
				late.T.Fatalf("item metadata field type %d, want %d", fieldType, protocol.MetadataSlot)
			}
			stack, ok := m.ReadSlot()
			if !ok {
				late.T.Fatal("the late joiner's item stack did not read back")
			}
			items = append(items, stack)
		}
	}
	if len(items) == 0 {
		t.Fatalf("the late joiner saw no item within %s", servertest.JoinTimeout)
	}

	// Half a second more for a second copy, which would be the bug.
	late.ReadTicks(srv, 10, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		r.ReadUUID()
		if r.ReadVarInt() == protocol.EntityTypeItem {
			items = append(items, protocol.ItemStack{})
		}
		return false
	})

	dirtItem, _ := registry.ItemID("minecraft:dirt")
	if len(items) != 1 || items[0].ItemID != dirtItem || items[0].Count != 1 {
		t.Errorf("the late joiner was shown %v, want the single dirt lying on the ground", items)
	}
}

// TestASneakingPlayerDoesNotReachAnItemAStandingPlayerDoes checks that the
// pickup reach follows the pose's height: a stack whose box bottom is 2.25
// above the feet is inside a standing reach (to 2.3) and outside a sneaking
// one (height 1.5, to 2.0). Only the first tick is checked while sneaking:
// the stack falls after that and drops into the sneaking reach too.
// vanilla: Player.aiStep
func TestASneakingPlayerDoesNotReachAnItemAStandingPlayerDoes(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Sneaker")
	c.MoveTo(0.5, 64, 0.5, 0, 0)
	servertest.HoldTicks(t, srv)
	c.SendSneak(true)
	c.Settle()
	servertest.UntilTicks(t, srv, 10, "the player takes the sneaking pose",
		func() bool { return srv.PlayerPose("Sneaker") == protocol.PoseSneaking })
	// The spawn y is the stack's centre; its box is 0.25 tall.
	feet := srv.PlayerPosition("Sneaker")
	srv.SpawnItemEntityDelay(feet.X, feet.Y+2.25+0.125, feet.Z, "minecraft:stone", 1, 0)

	servertest.RunTicks(t, srv, 1)
	if n := srv.ItemCount(); n != 1 {
		t.Fatalf("%d item entities after a tick, want the stack out of a sneaking player's reach", n)
	}

	c.SendSneak(false)
	c.Settle()
	servertest.RunTicks(t, srv, 5)
	if n := srv.ItemCount(); n != 0 {
		t.Errorf("%d item entities after standing up, want the stack taken", n)
	}
}
