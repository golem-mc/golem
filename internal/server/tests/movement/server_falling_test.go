package movement

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The falling blocks: the gravity blocks that leave their place as an entity —
// the sand and the rest of the generated gravity set, the body falling under
// its own physics, landing where it can place its block and dropping it as an
// item where it cannot. The tests drive real connections like the rest of the
// package's do: a block the command leaves hanging, the body it becomes on
// the wire, and the block or the item the landing leaves behind.
// vanilla: FallingBlock.tick

// TestSandFallsAsAnEntityAndLands checks the whole fall on the wire: a sand
// block the command leaves hanging over air is taken out as a block and put
// back as a falling block entity — the spawn carrying the block state in its
// data field — which falls and lands as the block again at the bottom of its
// column.
func TestSandFallsAsAnEntityAndLands(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	sand, ok := registry.FindState("minecraft:sand", nil)
	if !ok {
		t.Fatal("the registry has no sand")
	}

	// A sand block left hanging seven blocks above the flat world's floor.
	c.ChatCommand("setblock 5 70 3 sand")

	// The block is put into the world as a block first — the update the
	// command's own placement carries — and taken out of it as a block
	// again when the fall starts, the update the fall's own cell becoming
	// air.
	c.AwaitBlockState(5, 70, 3, sand)
	c.AwaitBlockState(5, 70, 3, 0)

	// The body arrives as an entity: at the cell's centre standing on its
	// bottom, still, and carrying the sand's state ID in the spawn's data
	// field.
	spawn := c.AwaitFallingSpawn()
	if spawn.X != 5.5 || spawn.Y != 70 || spawn.Z != 3.5 {
		t.Errorf("spawn at (%g, %g, %g), want (5.5, 70, 3.5)", spawn.X, spawn.Y, spawn.Z)
	}
	if spawn.Data != int32(sand) {
		t.Errorf("spawn data = %d, want %d (the sand block state)", spawn.Data, sand)
	}
	if spawn.Uuid == uuid.Nil {
		t.Error("spawn UUID is the zero UUID, want a real identity")
	}

	// The body falls and lands as the block again, at the floor, and its
	// body is taken out of every view.
	waitForBlock(t, srv, 5, 64, 3, func(s uint16) bool { return s == sand })
	if got := srv.BlockAt(5, 70, 3); got != 0 {
		t.Errorf("block the sand fell from = %d, want 0 (air)", got)
	}
	// The landed block is drawn as a transient block until the client
	// re-meshes, told before the body's Remove Entities: reading the
	// removal after it proves the order.
	r := protocol.NewReader(c.ExpectWithinTicks(srv, 40, protocol.IDPlayAddTransientBlock))
	x, y, z := r.ReadPosition()
	state := r.ReadVarInt()
	if err := r.Err(); err != nil {
		t.Fatalf("add transient block: %v", err)
	}
	if x != 5 || y != 64 || z != 3 || state != int32(sand) {
		t.Errorf("add transient block = (%d, %d, %d) state %d, want (5, 64, 3) state %d", x, y, z, state, sand)
	}
	c.AwaitFallingRemoval(spawn.EntityID)
}

// TestFallingSandBuriesAnItemAndTheItemPushesOut checks the burial's
// answer: an item lying where a falling block lands is left inside the
// block the landing placed, and the item's own tick answers a buried body
// with vanilla's moveTowardsClosestSpace — the walk towards the nearest
// side whose neighbour is not a full block's shape, out of the sand and
// back into the open air.
func TestFallingSandBuriesAnItemAndTheItemPushesOut(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	sand, ok := registry.FindState("minecraft:sand", nil)
	if !ok {
		t.Fatal("the registry has no sand")
	}

	// A dirt item resting on the flat world's floor, and a sand block
	// hanging above it: the sand falls through the item — an item has no
	// collision to land on — and places its block in the very cell the
	// item's box lies in.
	srv.SpawnItemEntity(12.5, 64.125, 8.5, "minecraft:dirt", 1)
	c.ChatCommand("setblock 12 66 8 sand")
	c.AwaitBlockState(12, 66, 8, 0)
	waitForBlock(t, srv, 12, 64, 8, func(s uint16) bool { return s == sand })

	// The burial's answer: within a couple of seconds the item's box is
	// out of the sand's cell again, wherever the nearest free side led.
	servertest.WaitFor(t, srv, 160, func() bool {
		for _, it := range srv.ItemsInDimension(world.DimensionName) {
			if it.Item != "minecraft:dirt" {
				continue
			}
			buried := it.X+0.125 > 12 && it.X-0.125 < 13 &&
				it.Y+0.125 > 64 && it.Y-0.125 < 65 &&
				it.Z+0.125 > 8 && it.Z-0.125 < 9
			return !buried
		}
		return false
	})
}

// TestFallingSandLandsOnATorchDropsAsAnItem checks the landing's refusal: a
// falling block whose feet come to rest in a cell it cannot place in — the
// torch's, which a placement may not replace — is gone and its block is
// lying there as an item instead, the torch unharmed beneath it.
func TestFallingSandLandsOnATorchDropsAsAnItem(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	torch, ok := registry.FindState("minecraft:torch", nil)
	if !ok {
		t.Fatal("the registry has no torch")
	}

	// A torch standing on the floor, and a sand block hanging above it:
	// the sand falls through the torch's cell — the torch has no collision
	// to land on — and comes to rest with its feet inside the torch's own
	// cell, which is not a placement it may make.
	c.ChatCommand("setblock 8 64 3 torch")
	c.AwaitBlockState(8, 64, 3, torch)
	c.ChatCommand("setblock 8 66 3 sand")
	c.AwaitBlockState(8, 66, 3, 0)

	// The block arrives as an item at the body's feet, and nothing was
	// placed: the torch still stands, and the cells about it are air.
	item := c.AwaitSpawn(protocol.EntityTypeItem)
	if item.X != 8.5 || item.Z != 3.5 {
		t.Errorf("item at (%g, %g), want (8.5, 3.5)", item.X, item.Z)
	}
	waitForBlock(t, srv, 8, 64, 3, func(s uint16) bool { return s == torch })
	if got := srv.BlockAt(8, 65, 3); got != 0 {
		t.Errorf("block above the torch = %d, want 0 (air)", got)
	}
	if got := srv.BlockAt(8, 66, 3); got != 0 {
		t.Errorf("block the sand fell from = %d, want 0 (air)", got)
	}
}

// TestFallingConcretePowderSetsAsConcrete checks the one gravity block that
// changes kind in the fall: a concrete powder whose landing is in water —
// the cell it comes to rest in a source's — sets the concrete of its colour
// instead of itself.
func TestFallingConcretePowderSetsAsConcrete(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	water, ok := registry.FindState("minecraft:water", map[string]string{"level": "0"})
	if !ok {
		t.Fatal("the registry has no water source")
	}
	_, ok = registry.FindState("minecraft:white_concrete_powder", nil)
	if !ok {
		t.Fatal("the registry has no white concrete powder")
	}
	concrete, ok := registry.FindState("minecraft:white_concrete", nil)
	if !ok {
		t.Fatal("the registry has no white concrete")
	}

	// A water source sitting on the floor, and a powder hanging above it:
	// the powder falls through the air and into the water's cell, which is
	// the landing vanilla's freeToLand gives a powder in water.
	c.ChatCommand("setblock 12 64 3 minecraft:water")
	c.AwaitBlockState(12, 64, 3, water)
	c.ChatCommand("setblock 12 66 3 minecraft:white_concrete_powder")
	c.AwaitBlockState(12, 66, 3, 0)
	c.AwaitFallingSpawn()

	// The landing sets the concrete where the water was.
	waitForBlock(t, srv, 12, 64, 3, func(s uint16) bool { return s == concrete })
	if got := srv.BlockAt(12, 65, 3); got != 0 {
		t.Errorf("block above the concrete = %d, want 0 (air)", got)
	}
}

// TestFallingBlockSurvivesARestart checks the saved half of the fall: a body
// caught in the air by the server going down comes back with the next servertest.Start
// of the same world, still carrying its block, and lands where it would
// have.
func TestFallingBlockSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	// The hang the test leaves the sand in is a /setblock, whose runner is
	// listed as an operator.
	srv, c := servertest.Join(t, cfg, "Builder", servertest.OpsListing(t, "Builder"))

	// A sand block left hanging a hundred and thirty-six blocks up: a fall
	// of some seconds, of which the restart takes none.
	c.ChatCommand("setblock 5 200 3 sand")
	c.AwaitFallingSpawn()

	// The world goes down under the falling body, which is saved with its
	// chunk the way the other saved kinds are.
	srv.Shutdown()

	// A server of its own, as after a restart, pointed at the same world:
	// the chunk's wake stands the body up mid-fall again — its spawn
	// racing the join's own reads — and the fall finishes the way it
	// started. The body that arrives is a new one to this session, a new
	// entity ID with the state it carried, and its own removal is the one
	// waited for.
	restarted := *cfg
	restartedSrv, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Builder")
	again.Configure()
	awake := again.JoinCaptureFalling("Builder")
	if awake.EntityID == 0 {
		// The wake the join's own batch outran: the body arrived on the
		// ticks after it, kept for the reads after by markLoaded.
		awake = again.AwaitFallingSpawn()
	}
	if awake.X != 5.5 || awake.Z != 3.5 {
		t.Errorf("woken body at (%g, %g), want (5.5, 3.5)", awake.X, awake.Z)
	}

	again.AwaitFallingRemoval(awake.EntityID)
	sand, ok := registry.FindState("minecraft:sand", nil)
	if !ok {
		t.Fatal("the registry has no sand")
	}
	waitForBlock(t, restartedSrv, 5, 64, 3, func(s uint16) bool { return s == sand })
}

// TestAFallingAnvilHurtsThePlayerUnderIt checks the landing's hurt on the
// wire: an anvil hung over a survival player comes down through their cell
// and squashes them for vanilla's per-block damage, told as a Damage Event
// of the falling_anvil type with the body that fell behind it, and the
// health the blow left.
func TestAFallingAnvilHurtsThePlayerUnderIt(t *testing.T) {
	t.Parallel()
	for _, block := range []string{"minecraft:anvil", "minecraft:chipped_anvil", "minecraft:damaged_anvil"} {
		t.Run(block, func(t *testing.T) {
			t.Parallel()

			srv, c, playerID := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
			// Knockback lifts only a grounded body.
			c.MoveToGround(0.5, 64, 0.5, true)
			c.Settle()
			servertest.HoldTicks(t, srv)

			// An anvil hanging six blocks over the player's own spawn cell: the
			// fall is five whole blocks past the first, ten half-hearts at the
			// anvil's two a block.
			c.ChatCommand("setblock 0 70 0 " + block)
			c.Settle()
			servertest.RunTicks(t, srv, 3)
			spawn := c.AwaitFallingSpawn()

			var (
				health   servertest.HealthReport
				hurtType int32
				cause    int32
				hurt     bool
				motion   []float64
			)
			found := c.ReadTicks(srv, 1200, func(id int32, data []byte) bool {
				c.T.Helper()
				switch id {
				case protocol.IDPlaySetHealth:
					health = servertest.ExpectHealthReport(c, data)
				case protocol.IDPlayDamageEvent:
					r := protocol.NewReader(data)
					if got := r.ReadVarInt(); got != playerID {
						c.T.Errorf("damage landed on entity %d, want the player's %d", got, playerID)
					}
					hurtType = r.ReadVarInt()
					cause = r.ReadVarInt()
					r.ReadVarInt() // the direct source, the falling body too
					r.ReadBool()   // the position, which only /damage sets
					hurt = true
				case protocol.IDPlaySetEntityMotion:
					r := protocol.NewReader(data)
					if r.ReadVarInt() != playerID {
						return false
					}
					vx, vy, vz := r.ReadLpVec3()
					motion = []float64{vx, vy, vz}
				}
				return hurt && motion != nil
			})
			if !found {
				t.Fatalf("no damage event and shove from the anvil's fall within %s", servertest.ExpectTimeout)
			}

			if want := registry.MustID("minecraft:damage_type", "minecraft:falling_anvil"); hurtType != want {
				t.Errorf("damage type = %d, want %d (falling_anvil)", hurtType, want)
			}
			if want := spawn.EntityID + 1; cause != want {
				t.Errorf("damage cause = %d, want the falling anvil's %d", cause, want)
			}
			if health.Health != 10 {
				t.Errorf("health after the anvil's fall = %v, want 10", health.Health)
			}
			// The falling body stands behind the blow, so vanilla shoves the victim
			// up off the ground by the four tenths a hit deals - and, the anvil
			// coming straight down with no away to push along, draws the same four
			// tenths of horizontal in a random direction.
			if len(motion) != 3 || math.Abs(motion[1]-0.2752) > 0.01 ||
				math.Abs(math.Hypot(motion[0], motion[2])-0.2184) > 0.01 {
				t.Errorf("the shove = %v, want a random horizontal 0.2184 and upward 0.2752 after travel", motion)
			}
		})
	}
}

// TestAWornOutAnvilBreaksWithItsSound checks the landing of an anvil its
// fall wears past its last state: a damaged anvil dropped 26 blocks rolls
// wear at certainty (vanilla's 0.05 a block past the first reaches 1 at 19),
// so the client hears World Event 1029 at the landing cell and the anvil
// leaves neither a block nor an item.
// vanilla: FallingBlockEntity.tick, AnvilBlock.onBrokenAfterFall
func TestAWornOutAnvilBreaksWithItsSound(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)

	c.ChatCommand("setblock 8 90 3 damaged_anvil")
	c.Settle()

	isItem := func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt() // entity ID
		r.ReadUUID()
		return r.ReadVarInt() == protocol.EntityTypeItem
	}
	var (
		broken    bool
		x, y, z   int32
		itemSeen  bool
		landSound bool
	)
	found := c.ReadTicks(srv, 200, func(id int32, data []byte) bool {
		itemSeen = itemSeen || isItem(id, data)
		if id != protocol.IDPlayWorldEvent {
			return false
		}
		r := protocol.NewReader(data)
		switch r.ReadI32() {
		case protocol.WorldEventAnvilBroken:
			x, y, z = r.ReadPosition()
			broken = true
		case protocol.WorldEventAnvilLand:
			landSound = true
		}
		return broken
	})
	if !found {
		t.Fatal("no World Event 1029 from the worn-out anvil within 200 ticks")
	}
	if x != 8 || y != 64 || z != 3 {
		t.Errorf("1029 at (%d, %d, %d), want the landing cell (8, 64, 3)", x, y, z)
	}
	if landSound {
		t.Error("the broken anvil played the landing sound 1031 as well")
	}
	if itemSeen || c.ReadTicks(srv, 5, isItem) {
		t.Error("the broken anvil dropped an item")
	}
	if got := srv.BlockAt(8, 64, 3); got != 0 {
		t.Errorf("block at the landing cell = %d, want 0 (air)", got)
	}
}

// TestAFallingAnvilHurtsAShortMob checks the hurt box reaches a body shorter
// than the block: the falling body's own box, not one shrunk to miss it. A
// still baby chicken stands in the anvil's cell and takes the blow, told as
// a Damage Event and the flinch a watching client shows.
func TestAFallingAnvilHurtsAShortMob(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	c.ChatCommand("summon minecraft:chicken 5.5 64 5.5 {NoAI:1b,Age:-24000}")
	chicken := c.AwaitSpawn(protocol.EntityTypeChicken).EntityID

	// The anvil comes down through the chicken's own cell.
	c.ChatCommand("setblock 5 70 5 minecraft:anvil")

	var (
		hurtType int32
		hurt     bool
		flashed  bool
	)
	found := c.ReadTicks(srv, 1200, func(id int32, data []byte) bool {
		c.T.Helper()
		switch id {
		case protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
			if r.ReadVarInt() != chicken {
				return false
			}
			hurtType = r.ReadVarInt()
			hurt = true
		case protocol.IDPlayHurtAnimation:
			r := protocol.NewReader(data)
			if r.ReadVarInt() != chicken {
				return false
			}
			flashed = true
		}
		return hurt && flashed
	})
	if !found {
		t.Fatalf("no damage event and flinch for the chicken within %s", servertest.ExpectTimeout)
	}
	if want := registry.MustID("minecraft:damage_type", "minecraft:falling_anvil"); hurtType != want {
		t.Errorf("damage type = %d, want %d (falling_anvil)", hurtType, want)
	}
}

// TestAFallingAnvilHelmetTurnsTheBlow checks the damages_helmet handling: a
// player in a helmet takes three quarters of the anvil's blow, and the helmet
// wears for it.
func TestAFallingAnvilHelmetTurnsTheBlow(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	helmet, ok := registry.ItemID("minecraft:iron_helmet")
	if !ok {
		t.Fatal("the item registry has no iron helmet")
	}
	srv.GiveSlot("Builder", player.ArmorSlot, protocol.ItemStack{ItemID: helmet, Count: 1})
	c.Settle()

	c.ChatCommand("setblock 0 70 0 minecraft:anvil")

	var health servertest.HealthReport
	found := c.ReadTicks(srv, 1200, func(id int32, data []byte) bool {
		c.T.Helper()
		if id == protocol.IDPlaySetHealth {
			health = servertest.ExpectHealthReport(c, data)
		}
		return id == protocol.IDPlayDamageEvent
	})
	if !found {
		t.Fatalf("no damage event from the anvil's fall within %s", servertest.ExpectTimeout)
	}
	// Ten half-hearts softened to three quarters is 7.5, which the helmet's
	// two armour points cut by the armour formula; twenty less that.
	want := 20 - 7.5*(1-0.4/25)
	if math.Abs(float64(health.Health)-want) > 0.01 {
		t.Errorf("health behind the helmet = %v, want %v", health.Health, want)
	}
	if got := srv.InventorySlot("Builder", player.ArmorSlot); got.Damage == 0 {
		t.Error("the helmet took no wear for the blow")
	}
}

// TestAStalactiteFallsAndHurtsThePlayerUnderIt checks a speleothem's fall: a
// pointed dripstone stalactite whose hold went leaves its cells as falling
// block entities, one per block down to the tip, instead of popping into
// items — the tip carries the falling_stalactite hurt its landing deals, and
// the player under it takes it.
// vanilla: SpeleothemBlock.spawnFallingStalactite
func TestAStalactiteFallsAndHurtsThePlayerUnderIt(t *testing.T) {
	t.Parallel()
	srv, c, playerID := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	frustum, ok := registry.FindState("minecraft:pointed_dripstone", map[string]string{
		"vertical_direction": "down", "thickness": "frustum", "waterlogged": "false",
	})
	if !ok {
		t.Fatal("no pointed dripstone stalactite frustum")
	}
	tip, ok := registry.FindState("minecraft:pointed_dripstone", map[string]string{
		"vertical_direction": "down", "thickness": "tip", "waterlogged": "false",
	})
	if !ok {
		t.Fatal("no pointed dripstone stalactite tip")
	}

	// A two-block stalactite hung under a stone block, over the player's own
	// spawn cell. Its upper block is a frustum over the tip, the shape a
	// two-block column takes.
	c.ChatCommand("setblock 0 68 0 minecraft:stone")
	c.ChatCommand("setblock 0 67 0 minecraft:pointed_dripstone[vertical_direction=down,thickness=frustum]")
	c.ChatCommand("setblock 0 66 0 minecraft:pointed_dripstone[vertical_direction=down,thickness=tip]")

	// Breaking the stone leaves the column without its hold: the whole
	// stalactite becomes a falling body per block, from the broken block
	// down to the tip, and no item pops where it stood.
	c.ChatCommand("setblock 0 68 0 minecraft:air")

	// The wire is the check, not the item count: a slow machine's late ticks
	// let the bodies land, and drop, before a count is read.
	popped := 0
	countItems := func(entityType int32) {
		if entityType == protocol.EntityTypeItem {
			popped++
		}
	}
	upper := c.AwaitFallingSpawnSeeing(countItems)
	lower := c.AwaitFallingSpawnSeeing(countItems)
	if upper.Data != int32(frustum) {
		t.Errorf("the upper body carries %d, want %d (the stalactite block)", upper.Data, frustum)
	}
	if lower.Data != int32(tip) {
		t.Errorf("the lower body carries %d, want %d (the stalactite tip)", lower.Data, tip)
	}
	if upper.Y != 67 || lower.Y != 66 {
		t.Errorf("the bodies hang at y %g and %g, want 67 and 66", upper.Y, lower.Y)
	}
	if popped != 0 {
		t.Errorf("%d items popped where the stalactite stood, want none", popped)
	}

	// The tip's landing hurts the player under it: a falling_stalactite blow
	// with the tip's body behind it, at vanilla's per-block damage. Each
	// broken body plays the dripstone landing sound as it gives up its place.
	var (
		health   servertest.HealthReport
		hurtType int32
		cause    int32
		hurt     bool
		landed   bool
		itemSeen bool
	)
	// An item that pops is read off the wire: the player under it picks it
	// up as soon as its delay is out, which a slow machine's poll can miss.
	spawnedItem := func(data []byte) bool {
		r := protocol.NewReader(data)
		r.ReadVarInt() // entity ID
		r.ReadUUID()
		return r.ReadVarInt() == protocol.EntityTypeItem
	}
	found := c.ReadTicks(srv, 1200, func(id int32, data []byte) bool {
		c.T.Helper()
		switch id {
		case protocol.IDPlaySetHealth:
			health = servertest.ExpectHealthReport(c, data)
		case protocol.IDPlayWorldEvent:
			if protocol.NewReader(data).ReadI32() == protocol.WorldEventDripstoneLand {
				landed = true
			}
		case protocol.IDPlaySpawnEntity:
			itemSeen = itemSeen || spawnedItem(data)
		case protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != playerID {
				c.T.Errorf("damage landed on entity %d, want the player's %d", got, playerID)
			}
			hurtType = r.ReadVarInt()
			cause = r.ReadVarInt()
			r.ReadVarInt() // the direct source, the falling body too
			r.ReadBool()   // the position, which only /damage sets
			hurt = true
		}
		return hurt && landed
	})
	if !found {
		t.Fatalf("no damage event and landing sound from the stalactite's fall within %s", servertest.ExpectTimeout)
	}

	if want := registry.MustID("minecraft:damage_type", "minecraft:falling_stalactite"); hurtType != want {
		t.Errorf("damage type = %d, want %d (falling_stalactite)", hurtType, want)
	}
	if want := lower.EntityID + 1; cause != want {
		t.Errorf("damage cause = %d, want the falling tip's %d", cause, want)
	}
	// The tip fell the two blocks from its cell to the floor: one whole
	// block past the first, at the tip's six a block.
	if health.Health != 14 {
		t.Errorf("health after the stalactite's fall = %v, want 14", health.Health)
	}

	// A stalactite cannot hold itself up where it comes down, so each body
	// breaks into an item on landing rather than placing a block.
	if !itemSeen && !c.ReadTicks(srv, 1200, func(id int32, data []byte) bool {
		return id == protocol.IDPlaySpawnEntity && spawnedItem(data)
	}) {
		t.Errorf("no item came of the broken stalactite within %s", servertest.ExpectTimeout)
	}
}

// TestAFallingWaterloggableBlockLandsWaterloggedOnlyInASource checks the
// landing's block update: an oak slab body coming to rest in a water source
// is placed waterlogged with its other properties kept, and in flowing water
// is placed dry.
// vanilla: FallingBlockEntity.tick
func TestAFallingWaterloggableBlockLandsWaterloggedOnlyInASource(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)

	for _, tc := range []struct {
		x     int32
		water string
		wet   string
	}{{12, "level=0", "true"}, {16, "level=1", "false"}} {
		want := registry.MustState("minecraft:oak_slab", map[string]string{"type": "top", "waterlogged": tc.wet})
		c.ChatCommand("setblock " + itoa(tc.x) + " 64 3 minecraft:water[" + tc.water + "]")
		c.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
		c.ChatCommand("summon minecraft:falling_block " + itoa(tc.x) + ".5 64.2 3.5 {BlockState:{id:\"minecraft:oak_slab\",properties:{type:\"top\",waterlogged:\"false\"}}}")
		c.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
		if !c.ReadTicks(srv, 10, func(id int32, data []byte) bool {
			for _, ch := range servertest.BlockChangesIn(t, id, data) {
				if ch.X == tc.x && ch.Y == 64 && ch.Z == 3 && uint32(ch.State) == uint32(want) {
					return true
				}
			}
			return false
		}) {
			t.Fatalf("no block update to %v at x=%d within 10 ticks", want, tc.x)
		}
	}
}

func itoa(n int32) string { return strconv.Itoa(int(n)) }

// TestFallingPoppyOnStoneDropsAsAnItem checks the landing's survival test: a
// falling poppy that comes to rest on stone cannot survive there, so it is
// dropped as an item and nothing is placed.
func TestFallingPoppyOnStoneDropsAsAnItem(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	c.ChatCommand("setblock 8 63 3 stone")
	c.ChatCommand("summon minecraft:falling_block 8.5 66 3.5 {BlockState:{id:\"minecraft:poppy\"}}")

	item := c.AwaitSpawn(protocol.EntityTypeItem)
	if item.X != 8.5 || item.Z != 3.5 {
		t.Errorf("item at (%g, %g), want (8.5, 3.5)", item.X, item.Z)
	}
	if got := srv.BlockAt(8, 64, 3); got != 0 {
		t.Errorf("block at the landing cell = %d, want 0 (air)", got)
	}
}

// TestFallingBlockDropFirstStepsOnTheNextTick checks the item a falling block
// breaks into is where its feet were, with its spawn hop, in the tick it
// appears: it takes its first physics step in the following tick.
// vanilla: EntityTickList.ensureActiveIsNotIterated
func TestFallingBlockDropFirstStepsOnTheNextTick(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.FreezeUntilHeld(t, srv)

	torch, _ := registry.FindState("minecraft:torch", nil)
	c.ChatCommand("setblock 8 64 3 torch")
	c.AwaitBlockState(8, 64, 3, torch)
	c.ChatCommand(`summon falling_block 8.5 66 3.5 {BlockState:{Name:"minecraft:sand"},Time:1,Motion:[0d,0d,0d]}`)
	c.AwaitSpawn(protocol.EntityTypeFallingBlock)
	servertest.HoldTicks(t, srv)
	for range 30 {
		servertest.RunTicks(t, srv, 1)
		items := srv.ItemsInDimension(world.DimensionName)
		if len(items) == 0 {
			continue
		}
		if it := items[0]; it.VY != 0.2 {
			t.Fatalf("item velocity Y = %g in its spawn tick, want the 0.2 hop unstepped", it.VY)
		}
		servertest.RunTicks(t, srv, 1)
		if it := srv.ItemsInDimension(world.DimensionName)[0]; it.VY == 0.2 {
			t.Fatalf("item velocity Y = %g a tick later, want it stepped", it.VY)
		}
		return
	}
	t.Fatal("the sand never dropped as an item")
}
