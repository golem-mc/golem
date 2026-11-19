package blocks

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The potent sulfur block, black-box: its state follows the water above and
// the block below, a wet block gasses the swimmers beside it, and a geyser
// pushes bodies up its water column or erupts on a timer.
// vanilla: PotentSulfurBlock, PotentSulfurBlockEntity

const sulfurFloorY = 70

func sulfurStateOf(t *testing.T, srv *server.Server, x, y, z int32) string {
	t.Helper()
	prop, ok := registry.PropNamed("potent_sulfur_state")
	if !ok {
		t.Fatal("the registry has no potent_sulfur_state")
	}
	state := srv.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockPotentSulfur {
		t.Fatalf("block at %d %d %d = %s, want potent sulfur", x, y, z, registry.BlockOf(state).Name())
	}
	return registry.StateValue(state, prop)
}

// buildSulfurPool lays a 7x7 stone floor at the potent sulfur's level around
// (cx, cz) with two source blocks of water over it, except the sulfur's own
// column, and the block named under the sulfur's cell. The sulfur and its two
// water blocks are set by command afterwards, so the shape updates run.
func buildSulfurPool(t *testing.T, srv *server.Server, cx, cz int32, under string) {
	t.Helper()
	stone := servertest.BlockState(t, "minecraft:stone")
	water := servertest.BlockState(t, "minecraft:water")
	for x := cx - 3; x <= cx+3; x++ {
		for z := cz - 3; z <= cz+3; z++ {
			ok := srv.SetBlock(x, sulfurFloorY-1, z, stone) && srv.SetBlock(x, sulfurFloorY, z, stone)
			if x != cx || z != cz {
				ok = ok && srv.SetBlock(x, sulfurFloorY+1, z, water) && srv.SetBlock(x, sulfurFloorY+2, z, water)
			}
			if !ok {
				t.Fatal("the pool's chunk was not loaded")
			}
		}
	}
	// SetBlock reports false for an unchanged cell, so the stone case is a no-op.
	srv.SetBlock(cx, sulfurFloorY-1, cz, servertest.BlockState(t, under))
}

func placeSulfur(c *servertest.Client, cx, cz int32) {
	c.ChatCommand(fmt.Sprintf("setblock %d %d %d minecraft:potent_sulfur", cx, sulfurFloorY, cz))
	c.Settle()
	c.ChatCommand(fmt.Sprintf("setblock %d %d %d minecraft:water", cx, sulfurFloorY+1, cz))
	c.ChatCommand(fmt.Sprintf("setblock %d %d %d minecraft:water", cx, sulfurFloorY+2, cz))
	c.Settle()
}

func TestPotentSulfurStateFollowsWaterAndBlockBelow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:stone")
	buildSulfurPool(t, srv, 20, 8, "minecraft:lava")
	placeSulfur(c, 8, 8)
	placeSulfur(c, 20, 8)

	if got := sulfurStateOf(t, srv, 8, sulfurFloorY, 8); got != "wet" {
		t.Errorf("sulfur under water = %q, want wet", got)
	}
	if got := sulfurStateOf(t, srv, 20, sulfurFloorY, 8); got != "continuous" {
		t.Errorf("sulfur over lava under water = %q, want continuous", got)
	}
	c.ChatCommand("setblock 8 71 8 minecraft:air")
	c.ChatCommand("setblock 20 71 8 minecraft:air")
	c.Settle()
	for _, x := range []int32{8, 20} {
		if got := sulfurStateOf(t, srv, x, sulfurFloorY, 8); got != "dry" {
			t.Errorf("sulfur at x=%d with the water removed = %q, want dry", x, got)
		}
	}
}

func TestPotentSulfurGasGivesNauseaToASwimmer(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Swimmer", servertest.OpsListing(t, "Swimmer"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("gamemode survival")
	c.Settle()
	buildSulfurPool(t, srv, 8, 8, "minecraft:stone")
	placeSulfur(c, 8, 8)
	if got := sulfurStateOf(t, srv, 8, sulfurFloorY, 8); got != "wet" {
		t.Fatalf("sulfur under water = %q, want wet", got)
	}
	// Two blocks east of the sulfur, the head above the water's top.
	c.MoveToGround(10.5, 72, 8.5, false)
	c.Settle()
	servertest.UntilTicks(t, srv, 20, "nausea on the swimmer", func() bool {
		_, _, ok := srv.PlayerEffect("Swimmer", "minecraft:nausea")
		return ok
	})
	if d, amp, _ := srv.PlayerEffect("Swimmer", "minecraft:nausea"); amp != 0 || d <= 0 || d > 80 {
		t.Errorf("nausea = %d ticks, amplifier %d, want at most 80 and 0", d, amp)
	}
}

func TestPotentSulfurContinuousGeyserLaunchesAnItem(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
	placeSulfur(c, 8, 8)
	if got := sulfurStateOf(t, srv, 8, sulfurFloorY, 8); got != "continuous" {
		t.Fatalf("sulfur over lava under water = %q, want continuous", got)
	}
	srv.SpawnItemEntity(8.5, 71.5, 8.5, "minecraft:stick", 1)
	servertest.UntilTicks(t, srv, 5, "an upward velocity on the item", func() bool {
		items := srv.ItemsInDimension("")
		return len(items) == 1 && items[0].VY > 0.1
	})
}

// A continuous geyser lifts a boat and an armor stand too.
// vanilla: PotentSulfurBlockEntity.LAUNCH_ENTITY_TICKER
func TestPotentSulfurContinuousGeyserLaunchesABoatAndAnArmorStand(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
	placeSulfur(c, 8, 8)
	if got := sulfurStateOf(t, srv, 8, sulfurFloorY, 8); got != "continuous" {
		t.Fatalf("sulfur over lava under water = %q, want continuous", got)
	}
	boat := srv.SpawnBoat(8.5, 73, 8.5, "minecraft:oak_boat")
	srv.SpawnArmorStand(8.5, 73, 8.5)
	servertest.RunTicks(t, srv, 1)
	_, boatY, _, _, _, _ := srv.BoatState(boat)
	stands := srv.ArmorStandsInDimension("")
	if len(stands) != 1 {
		t.Fatalf("%d armor stands, want 1", len(stands))
	}
	standY := stands[0].Y
	servertest.RunTicks(t, srv, 5)
	if _, y, _, _, _, _ := srv.BoatState(boat); y <= boatY {
		t.Errorf("boat y = %v after 5 ticks, started at %v: not lifted", y, boatY)
	}
	if got := srv.ArmorStandsInDimension("")[0].Y; got <= standY {
		t.Errorf("armor stand y = %v after 5 ticks, started at %v: not lifted", got, standY)
	}
}

// A body that is not falling fast banks at most one block of fall in the column.
// vanilla: Entity.checkFallDistanceAccumulation
func TestPotentSulfurGeyserCapsAMobsFallDistance(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
	placeSulfur(c, 8, 8)
	if got := sulfurStateOf(t, srv, 8, sulfurFloorY, 8); got != "continuous" {
		t.Fatalf("sulfur over lava under water = %q, want continuous", got)
	}
	zombie := srv.SpawnMob("minecraft:zombie", 8.5, 74, 8.5)
	uuid, _ := srv.MobUUID(zombie)
	c.ChatCommand("data merge entity " + uuid + " {fall_distance:10d}")
	c.Settle()
	if got, _ := srv.MobFallDistance(zombie); got < 5 {
		t.Fatalf("fall distance = %v before the tick, want the 10 set", got)
	}
	servertest.RunTicks(t, srv, 1)
	if got, _ := srv.MobFallDistance(zombie); got > 1 {
		t.Errorf("fall distance = %v after a tick in the column, want at most 1", got)
	}
}

func TestPotentSulfurGeyserLeavesAPlayersMotionToTheirClient(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
	placeSulfur(c, 8, 8)
	if got := sulfurStateOf(t, srv, 8, sulfurFloorY, 8); got != "continuous" {
		t.Fatalf("sulfur over lava under water = %q, want continuous", got)
	}
	c.MoveTo(8.5, 71.5, 8.5, 0, 0)
	c.ReadTicks(srv, 20, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySetEntityMotion && protocol.NewReader(data).ReadVarInt() == c.PlayerEntityID {
			t.Errorf("the server sent the player a motion packet in a geyser")
		}
		return false
	})
}

func TestPotentSulfurDormantGeyserErupts(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:magma_block")
	placeSulfur(c, 8, 8)
	if got := sulfurStateOf(t, srv, 8, sulfurFloorY, 8); got != "dormant" {
		t.Fatalf("sulfur over magma under water = %q, want dormant", got)
	}
	// Two water blocks: the countdown rolls 10*(2-1)+15..30 strides of 20
	// ticks asleep and 2..3 erupting; the first stride lands within 20 ticks.
	wait := func(want string, lo, hi int) {
		t.Helper()
		for n := 1; n <= hi; n++ {
			servertest.RunTicks(t, srv, 1)
			if sulfurStateOf(t, srv, 8, sulfurFloorY, 8) == want {
				if n < lo {
					t.Fatalf("became %s after %d ticks, want at least %d", want, n, lo)
				}
				return
			}
		}
		t.Fatalf("not %s within %d ticks", want, hi)
	}
	wait("erupting", 481, 800)
	wait("dormant", 40, 60)
}

// A cart with a player aboard is still simulated by the server, so a geyser
// lifts it.
// vanilla: Entity.canSimulateMovement, PotentSulfurBlockEntity.LAUNCH_ENTITY_TICKER
func TestPotentSulfurContinuousGeyserLiftsAPlayerOccupiedMinecart(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
	placeSulfur(c, 8, 8)
	cart := srv.SpawnMinecart(8.5, 73, 8.5)
	c.ChatCommand("ride @s mount @e[type=minecart,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	if name, _ := srv.MinecartRider(cart); name != "Builder" {
		t.Fatalf("the cart's rider = %q, want Builder", name)
	}
	_, y0, _, _, _ := srv.MinecartState(cart)
	servertest.RunTicks(t, srv, 5)
	if _, y, _, _, _ := srv.MinecartState(cart); y <= y0 {
		t.Errorf("cart y = %v after 5 ticks, started at %v: not lifted", y, y0)
	}
}

// A dropped item and a thrown snowball in the column are pushed, and the item's
// banked fall is capped before the velocity test.
// vanilla: PotentSulfurBlockEntity.LAUNCH_ENTITY_TICKER, Entity.checkFallDistanceAccumulation
func TestPotentSulfurGeyserCapsAnItemsFallAndLiftsASnowball(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
	placeSulfur(c, 8, 8)
	if got := sulfurStateOf(t, srv, 8, sulfurFloorY, 8); got != "continuous" {
		t.Fatalf("sulfur over lava under water = %q, want continuous", got)
	}
	item := srv.SpawnItemEntityMotion(8.5, 73, 8.5, "minecraft:stick", 1, 0, 0.4, 0)
	srv.SetItemFallDistance(item, 10)
	// The throw flies one block along z before the push, so it starts that far short of the column.
	srv.ThrowThrown(entity.ThrownSnowball, 8.5, 73, 7.5, 0, 0, 0, 1)
	servertest.RunTicks(t, srv, 1)
	if got := srv.ItemFallDistance(item); got < 0 || got > 1 {
		t.Errorf("item fall distance = %v after a tick in the column, want at most 1", got)
	}
	thrown := srv.ThrownInDimension("")
	if len(thrown) != 1 || thrown[0].VY <= 0 {
		t.Errorf("thrown = %+v, want one snowball pushed upward", thrown)
	}
}

// The geyser's push lands after the entities step: TNT at rest falls one
// tick's gravity, then gains the impulse for the next tick.
// vanilla: ServerLevel.tick (entities, then block entities), PrimedTnt.tick
func TestPotentSulfurGeyserPushesTNTAfterItsMovement(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
	placeSulfur(c, 8, 8)
	srv.SpawnTNT(8.5, 74, 8.5, 80)
	servertest.RunTicks(t, srv, 1)
	tnts := srv.TNTsInDimension("")
	if len(tnts) != 1 {
		t.Fatalf("%d TNT, want 1", len(tnts))
	}
	if y := tnts[0].Y; y > 73.97 || y < 73.95 {
		t.Errorf("TNT y = %v after one tick, want 73.96 (gravity before the impulse)", y)
	}
	servertest.RunTicks(t, srv, 1)
	if y := srv.TNTsInDimension("")[0].Y; y <= tnts[0].Y {
		t.Errorf("TNT y = %v after the second tick, was %v: the impulse was not applied", y, tnts[0].Y)
	}
}

// The geyser lifts a rocket shot at an angle, an eye of ender and a mannequin.
// vanilla: PotentSulfurBlockEntity.LAUNCH_ENTITY_TICKER
func TestPotentSulfurGeyserLaunchesRocketsEyesAndMannequins(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
	placeSulfur(c, 8, 8)
	if got := sulfurStateOf(t, srv, 8, sulfurFloorY, 8); got != "continuous" {
		t.Fatalf("sulfur over lava under water = %q, want continuous", got)
	}
	rocket := srv.SpawnFireworkAtAngle(8.5, 74, 8.5, 0, 0, 0, 100)
	eye := srv.SpawnEyeEntity(8.5, 74, 8.5, 0, 0, 0)
	mannequin := srv.SpawnMannequinForTest(8.5, 74, 8.5)
	servertest.RunTicks(t, srv, 1)
	for kind, id := range map[string]int32{"firework": rocket.ID, "eye": eye.ID, "mannequin": mannequin} {
		vy, ok := srv.TrackedBodyVY(kind, id)
		if !ok || vy <= 0.1 {
			t.Errorf("%s vy = %v (found %v) after a tick in the column, want pushed upward", kind, vy, ok)
		}
	}
}

// A player's cast hook in the column is lifted, and its banked fall is capped
// before the velocity test.
// vanilla: PotentSulfurBlockEntity.LAUNCH_ENTITY_TICKER, Entity.checkFallDistanceAccumulation
func TestPotentSulfurGeyserLaunchesAndCapsABobber(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	srv.GiveSlot("Builder", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:fishing_rod"), Count: 1})
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
	placeSulfur(c, 8, 8)
	if got := sulfurStateOf(t, srv, 8, sulfurFloorY, 8); got != "continuous" {
		t.Fatalf("sulfur over lava under water = %q, want continuous", got)
	}
	servertest.UseHeld(c, 0, 0)
	c.Settle()
	id, ok := srv.PlaceBobberForTest("Builder", 8.5, 74, 8.5, 10)
	if !ok {
		t.Fatal("the cast left no live hook")
	}
	servertest.RunTicks(t, srv, 1)
	if vy, ok := srv.TrackedBodyVY("bobber", id); !ok || vy <= 0.1 {
		t.Errorf("bobber vy = %v (found %v) after a tick in the column, want pushed upward", vy, ok)
	}
	if fall, _ := srv.BodyFallDistance("bobber", id); fall > 1 {
		t.Errorf("bobber fall distance = %v, want at most 1", fall)
	}
}

// A mannequin's banked fall is capped in the column, and a dying one is left
// to fall.
// vanilla: PotentSulfurBlockEntity.LAUNCH_ENTITY_TICKER, EntitySelector.ENTITY_STILL_ALIVE
func TestPotentSulfurGeyserCapsAMannequinAndSparesADyingOne(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
	placeSulfur(c, 8, 8)
	if got := sulfurStateOf(t, srv, 8, sulfurFloorY, 8); got != "continuous" {
		t.Fatalf("sulfur over lava under water = %q, want continuous", got)
	}
	alive := srv.SpawnMannequinForTest(8.5, 74, 8.5)
	dying := srv.SpawnMannequinForTest(8.5, 74, 8.5)
	srv.SetMannequinForTest(alive, entity.MannequinHealth, 10)
	srv.SetMannequinForTest(dying, 0, 10)
	servertest.RunTicks(t, srv, 1)
	if fall, _ := srv.BodyFallDistance("mannequin", alive); fall > 1 {
		t.Errorf("mannequin fall distance = %v, want at most 1", fall)
	}
	if vy, _ := srv.TrackedBodyVY("mannequin", alive); vy <= 0.1 {
		t.Errorf("mannequin vy = %v, want pushed upward", vy)
	}
	if vy, _ := srv.TrackedBodyVY("mannequin", dying); vy > 0 {
		t.Errorf("dying mannequin vy = %v, want gravity alone", vy)
	}
}

// The geyser lifts an orb, an arrow and a falling block, each pushed after
// its own movement.
// vanilla: PotentSulfurBlockEntity.LAUNCH_ENTITY_TICKER
func TestPotentSulfurGeyserLaunchesOrbsArrowsAndFallingBlocks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
	placeSulfur(c, 8, 8)
	if got := sulfurStateOf(t, srv, 8, sulfurFloorY, 8); got != "continuous" {
		t.Fatalf("sulfur over lava under water = %q, want continuous", got)
	}
	srv.SpawnOrb(8.5, 74, 8.5, 1)
	srv.ShootArrow(8.5, 74, 8.5, 0, 0, 0)
	block := srv.SpawnFallingBlock(8, 74, 8, servertest.BlockState(t, "minecraft:sand"))
	_, startY, _, _ := srv.FallingBlockPosition(block)
	servertest.RunTicks(t, srv, 1)
	if orbs := srv.OrbsInWorld(); len(orbs) != 1 || orbs[0].VY <= 0.1 {
		t.Errorf("orbs = %+v, want one pushed upward", orbs)
	}
	if arrows := srv.ArrowsInDimension(""); len(arrows) != 1 || arrows[0].VY <= 0.1 {
		t.Errorf("arrows = %+v, want one pushed upward", arrows)
	}
	servertest.RunTicks(t, srv, 1)
	if _, y, _, ok := srv.FallingBlockPosition(block); !ok || y <= startY {
		t.Errorf("falling block y = %v (found %v) after 2 ticks, started at %v: not lifted", y, ok, startY)
	}
}

// A snowball at rest above the water climbs as vanilla's does: gravity and
// drag, its movement, then the geyser's push each tick. The expected height is
// vanilla's, read from a reference server (floor 70, snowball summoned at 73).
// vanilla: ThrowableProjectile.tick, PotentSulfurBlockEntity.LAUNCH_ENTITY_TICKER
func TestPotentSulfurGeyserSnowballTrajectoryMatchesVanilla(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
	placeSulfur(c, 8, 8)
	if got := sulfurStateOf(t, srv, 8, sulfurFloorY, 8); got != "continuous" {
		t.Fatalf("sulfur over lava under water = %q, want continuous", got)
	}
	c.ChatCommand("summon minecraft:snowball 8.5 73 8.5 {Motion:[0d,0d,0d]}")
	c.Settle()
	servertest.RunTicks(t, srv, 12)
	thrown := srv.ThrownInDimension("")
	if len(thrown) != 1 {
		t.Fatalf("%d thrown bodies, want 1", len(thrown))
	}
	if want := 78.56188739; math.Abs(thrown[0].Y-want) > 1e-4 {
		t.Errorf("snowball y = %v after 12 ticks, vanilla has %v", thrown[0].Y, want)
	}
}

// sulfurEntityData reads the saved state without advancing the held loop.
func sulfurEntityData(t *testing.T, c *servertest.Client, selector string) nbt.Compound {
	t.Helper()
	c.ChatCommand("data get entity " + selector)
	var result nbt.Compound
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySystemChat {
			return
		}
		msg, ok := protocol.ReadText(protocol.NewReader(data))
		if !ok || msg.Translate != "commands.data.entity.query" || len(msg.With) != 2 {
			return
		}
		var err error
		result, err = nbt.ParseSNBT(msg.With[1].Plain())
		if err != nil {
			t.Fatal(err)
		}
	})
	if result == nil {
		t.Fatal("no entity data returned for " + selector)
	}
	return result
}

// vanilla: PotentSulfurBlockEntity.LAUNCH_ENTITY_TICKER, EndCrystal.tick
func TestPotentSulfurGeyserUpdatesACrystalsMotionWithoutMovingIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
	placeSulfur(c, 8, 8)
	c.ChatCommand("summon end_crystal 8.5 74 8.5 {Motion:[0.1d,0d,0.2d],fall_distance:10d}")
	spawned := c.AwaitSpawn(protocol.EntityTypeEndCrystal)
	if math.Abs(spawned.Vx-0.1) > 1e-4 || math.Abs(spawned.Vz-0.2) > 1e-4 {
		t.Errorf("spawn motion = (%v, %v, %v), want (0.1, 0, 0.2)", spawned.Vx, spawned.Vy, spawned.Vz)
	}
	c.Settle()
	target := "@e[type=end_crystal,limit=1]"
	before := sulfurEntityData(t, c, target)
	if fall := nbt.DoubleFieldOr(before, "fall_distance", -1); fall != 10 {
		t.Fatalf("initial fall = %v, want 10", fall)
	}
	servertest.RunTicks(t, srv, 1)
	after := sulfurEntityData(t, c, target)
	motion, _ := nbt.Get[nbt.List](after, "Motion")
	want := []float64{0.1, float64(float32(0.2)), 0.2}
	if len(motion.Items) != 3 {
		t.Fatalf("motion = %v", motion)
	}
	for i, v := range want {
		if got := float64(motion.Items[i].(nbt.Double)); got != v {
			t.Errorf("motion[%d] = %.17g, want %.17g", i, got, v)
		}
	}
	if fall := nbt.DoubleFieldOr(after, "fall_distance", -1); fall != 1 {
		t.Errorf("fall = %v, want 1", fall)
	}
	pos, _ := nbt.Get[nbt.List](after, "Pos")
	if len(pos.Items) != 3 {
		t.Fatalf("position = %v", pos)
	}
	for i, want := range []float64{8.5, 74, 8.5} {
		if got := float64(pos.Items[i].(nbt.Double)); got != want {
			t.Errorf("position[%d] = %v, want %v", i, got, want)
		}
	}
}

// Angled rockets retain their velocity during movement, isolating the strict
// launch limit and positive-volume box intersection from gravity and drag.
// vanilla: FireworkRocketEntity.tick, PotentSulfurBlockEntity.LAUNCH_ENTITY_TICKER
func TestPotentSulfurGeyserLaunchBoundaryAndOverlap(t *testing.T) {
	t.Parallel()
	limit := float64(float32(0.3)) + 0.2
	for _, tc := range []struct {
		name  string
		x, vy float64
		push  bool
	}{
		{"below float limit", 8.5, 0.5, true},
		{"at limit", 8.5, limit, false},
		{"above limit", 8.5, math.Nextafter(limit, math.Inf(1)), false},
		{"touching edge", 9.125, 0, false},
		{"overlapping edge", 9.125 - 1e-6, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
			servertest.HoldTicks(t, srv)
			buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
			placeSulfur(c, 8, 8)
			rocket := srv.SpawnFireworkAtAngle(tc.x, 74, 8.5, 0, tc.vy, 0, 100)
			servertest.RunTicks(t, srv, 1)
			vy, ok := srv.TrackedBodyVY("firework", rocket.ID)
			want := tc.vy
			if tc.push {
				want += float64(float32(0.2))
			}
			if !ok || math.Abs(vy-want) > 1e-12 {
				t.Errorf("vy = %.17g (found %v), want %.17g", vy, ok, want)
			}
		})
	}
}

// Client authority prevents impulses, but does not prevent the server's fall cap.
// vanilla: Entity.checkFallDistanceAccumulation, PotentSulfurBlockEntity.LAUNCH_ENTITY_TICKER
func TestPotentSulfurGeyserCapsAPlayersFallBeforeExclusions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		vy, x float64
		mode  string
		cap   bool
	}{
		{"client controlled", 0, 8.5, "survival", true},
		{"flying", 0, 8.5, "creative", true},
		{"fast fall boundary", -0.5, 8.5, "survival", false},
		{"slow fall", math.Nextafter(-0.5, 0), 8.5, "survival", true},
		{"outside column", 0, 10, "survival", false},
		{"spectator", 0, 8.5, "spectator", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
			servertest.HoldTicks(t, srv)
			buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
			placeSulfur(c, 8, 8)
			c.ChatCommand("gamemode survival")
			c.Settle()
			c.MoveToGround(tc.x, 84, 8.5, false)
			c.MoveToGround(tc.x, 74, 8.5, false)
			c.Settle()
			if fall := srv.PlayerFallDistance("Builder"); fall < 9 {
				t.Fatalf("initial fall = %v, want at least 9", fall)
			}
			c.ChatCommand("gamemode " + tc.mode)
			if tc.name == "flying" {
				c.Send(protocol.IDPlayPlayerAbilitiesIn, func(b *protocol.Buffer) { b.PutI8(protocol.AbilityFlying) })
			}
			c.Settle()
			srv.SetPlayerVelocity("Builder", 0, tc.vy, 0)
			before := srv.PlayerFallDistance("Builder")
			servertest.RunTicks(t, srv, 1)
			want := before
			if tc.cap {
				want = 1
			}
			if got := srv.PlayerFallDistance("Builder"); got != want {
				t.Errorf("fall = %v, want %v", got, want)
			}
		})
	}
}

// vanilla: PotentSulfurBlockEntity.LAUNCH_ENTITY_TICKER, Entity.checkFallDistanceAccumulation
func TestPotentSulfurGeyserCapsEveryBankingTrackedKind(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"boat", "minecart", "orb", "tnt", "falling_block", "armor_stand"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
			servertest.HoldTicks(t, srv)
			buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
			placeSulfur(c, 8, 8)
			var id int32
			switch kind {
			case "boat":
				id = srv.SpawnBoat(8.5, 74, 8.5, "minecraft:oak_boat")
			case "minecart":
				id = srv.SpawnMinecart(8.5, 74, 8.5)
			case "orb":
				id = srv.SpawnOrb(8.5, 74, 8.5, 1)
			case "tnt":
				id = srv.SpawnTNT(8.5, 74, 8.5, 80)
			case "falling_block":
				id = srv.SpawnFallingBlock(8, 74, 8, servertest.BlockState(t, "minecraft:sand"))
			case "armor_stand":
				id = srv.SpawnArmorStand(8.5, 74, 8.5)
			}
			if !srv.SetBodyFallDistance(kind, id, 10) {
				t.Fatal("body not found")
			}
			if fall, ok := srv.TrackedFallDistance(kind, id); !ok || fall != 10 {
				t.Fatalf("initial fall = %v (found %v), want 10", fall, ok)
			}
			servertest.RunTicks(t, srv, 1)
			if fall, ok := srv.TrackedFallDistance(kind, id); !ok || fall != 1 {
				t.Errorf("fall = %v (found %v), want 1", fall, ok)
			}
		})
	}
}

// vanilla: Entity.canSimulateMovement, PotentSulfurBlockEntity.LAUNCH_ENTITY_TICKER
func TestPotentSulfurGeyserCapsButDoesNotPushADrivenBoat(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
	placeSulfur(c, 8, 8)
	boat := srv.SpawnBoat(8.5, 74, 8.5, "minecraft:oak_boat")
	c.ChatCommand("ride @s mount @e[type=oak_boat,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	if rider, _ := srv.BoatRider(boat); rider != "Builder" {
		t.Fatalf("rider = %q", rider)
	}
	srv.SetBodyFallDistance("boat", boat, 10)
	servertest.RunTicks(t, srv, 1)
	if fall, ok := srv.TrackedFallDistance("boat", boat); !ok || fall != 1 {
		t.Errorf("fall = %v (found %v), want 1", fall, ok)
	}
	if _, vy, _, ok := srv.BoatVelocity(boat); !ok || vy != 0 {
		t.Errorf("vy = %v (found %v), want 0", vy, ok)
	}
	if _, y, _, _, _, _ := srv.BoatState(boat); y != 74 {
		t.Errorf("y = %v, want 74", y)
	}
}

// The passenger's next rideTick zeroes its velocity, so inspect the block-entity
// phase while the loop is held to catch an erroneous impulse before that reset.
// vanilla: Entity.rideTick, PotentSulfurBlockEntity.LAUNCH_ENTITY_TICKER
func TestPotentSulfurGeyserCapsButDoesNotPushAPassengerOrDragon(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
	placeSulfur(c, 8, 8)
	boat := srv.SpawnBoat(8.5, 74, 8.5, "minecraft:oak_boat")
	passenger := srv.SpawnMob("minecraft:pig", 8.5, 74, 8.5)
	if !srv.MountMobOnBoat(passenger, boat) {
		t.Fatal("could not mount pig")
	}
	dragon := srv.SpawnMob("minecraft:ender_dragon", 8.5, 74, 8.5)
	for _, id := range []int32{passenger, dragon} {
		identity, _ := srv.MobUUID(id)
		c.ChatCommand("data merge entity " + identity + " {NoAI:1b,NoGravity:1b,Motion:[0d,0d,0d],fall_distance:10d}")
	}
	c.Settle()
	srv.TickBlockEntities()
	for _, id := range []int32{passenger, dragon} {
		if fall, ok := srv.MobFallDistance(id); !ok || fall != 1 {
			t.Errorf("entity %d fall = %v (found %v), want 1", id, fall, ok)
		}
		if _, vy, _, ok := srv.MobVelocity(id); !ok || vy != 0 {
			t.Errorf("entity %d vy = %v (found %v), want 0", id, vy, ok)
		}
	}
}

// Crystals retain their velocity between ticks, exposing the cap's exact
// threshold and its ordering before the launch-speed check.
// vanilla: Entity.checkFallDistanceAccumulation, PotentSulfurBlockEntity.LAUNCH_ENTITY_TICKER
func TestPotentSulfurGeyserCapsFallsBeforeTestingVelocity(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
	placeSulfur(c, 8, 8)
	c.ChatCommand("summon end_crystal 8.5 74 8.5")
	c.Settle()
	target := "@e[type=end_crystal,limit=1]"
	for _, tc := range []struct {
		name               string
		vy, fall, wantFall float64
		push               bool
	}{
		{"too fast to launch", float64(float32(0.3)) + 0.2, 10, 1, false},
		{"falling at boundary", -0.5, 10, 10, true},
		{"falling just slower", math.Nextafter(-0.5, 0), 10, 1, true},
		{"small fall retained", 0, 0.25, 0.25, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c.ChatCommand(fmt.Sprintf("data merge entity %s {Motion:[0d,%.17gd,0d],fall_distance:%.17gd}", target, tc.vy, tc.fall))
			c.Settle()
			servertest.RunTicks(t, srv, 1)
			data := sulfurEntityData(t, c, target)
			if fall := nbt.DoubleFieldOr(data, "fall_distance", -1); fall != tc.wantFall {
				t.Errorf("fall = %v, want %v", fall, tc.wantFall)
			}
			motion, _ := nbt.Get[nbt.List](data, "Motion")
			if len(motion.Items) != 3 {
				t.Fatalf("motion = %v", motion)
			}
			want := tc.vy
			if tc.push {
				want += float64(float32(0.2))
			}
			if vy := float64(motion.Items[1].(nbt.Double)); vy != want {
				t.Errorf("vy = %.17g, want %.17g", vy, want)
			}
		})
	}
}

// vanilla: PotentSulfurBlockEntity.LAUNCH_ENTITY_TICKER, ThrownTrident.tick
func TestPotentSulfurGeyserLaunchesATrident(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
	placeSulfur(c, 8, 8)
	c.ChatCommand("summon trident 8.5 74 8.5 {NoGravity:1b,Motion:[0d,0d,0d]}")
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	data := sulfurEntityData(t, c, "@e[type=trident,limit=1]")
	motion, _ := nbt.Get[nbt.List](data, "Motion")
	if len(motion.Items) != 3 {
		t.Fatalf("motion = %v", motion)
	}
	if vy := float64(motion.Items[1].(nbt.Double)); vy != float64(float32(0.2)) {
		t.Errorf("trident vy = %.17g, want geyser impulse", vy)
	}
}
