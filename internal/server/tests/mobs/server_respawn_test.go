package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The respawn tests: where a death returns the body, the broken or obstructed
// bed it refuses to use, and the older player files whose spawn data this
// server migrates on load.

// TestBedRespawnUsesTheStandUpPosition checks that a death at a bed returns
// the body to the cell beside the bed, facing it, rather than into the bed
// itself. vanilla: BedBlock.findStandUpPosition, ServerPlayer.RespawnPosAngle
func TestBedRespawnUsesTheStandUpPosition(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	srv, c, entityID := servertest.JoinAs(t, cfg, "Sleeper", servertest.OpsListing(t, "Sleeper"))

	servertest.StandABedAndSetTheSpawn(t, srv, c)

	pos, noBlock := servertest.KillAndRespawn(t, c, entityID, "Sleeper")
	if noBlock {
		t.Error("a bed that is standing was reported as missing")
	}
	// The bed's head is (5, 64, 1) facing south, and the click was made
	// looking south: the walk starts on the bed's west side, so the body
	// lands at (4.5, 64, 1.5), looking east at the bed, yaw -90.
	if pos.X != 4.5 || pos.Y != 64 || pos.Z != 1.5 || pos.Yaw != -90 || pos.Pitch != 0 {
		t.Errorf("respawn = (%v, %v, %v) yaw %v pitch %v, want the stand-up cell (4.5, 64, 1.5) yaw -90 pitch 0",
			pos.X, pos.Y, pos.Z, pos.Yaw, pos.Pitch)
	}
	c.Disconnect()
}

// TestBrokenBedRespawnFallsBackToWorldSpawn breaks a bed that holds the
// player's spawn point, kills them, and checks the fallback: the client
// is told no respawn block is available, the body comes back at the world
// spawn instead of the bed, and the point is dropped from the file.
func TestBrokenBedRespawnFallsBackToWorldSpawn(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	srv, c, entityID := servertest.JoinAs(t, cfg, "Sleeper", servertest.OpsListing(t, "Sleeper"))

	servertest.StandABedAndSetTheSpawn(t, srv, c)

	// The foot takes the head with it, so the whole bed is gone. A hand is
	// a moment's dig in survival, so the head's update is waited on.
	c.BreakBlock(5, 64, 0)
	servertest.AwaitBlockUpdate(t, c, 5, 64, 1, 0)
	c.Settle()
	if state := srv.BlockAt(5, 64, 1); state != 0 {
		t.Fatal("the bed head survived the foot break")
	}

	pos, noBlock := servertest.KillAndRespawn(t, c, entityID, "Sleeper")
	if !noBlock {
		t.Error("a broken bed did not send the no-respawn-block game event")
	}
	if pos.X != 0.5 || pos.Y != 64 || pos.Z != 0.5 {
		t.Errorf("respawn = (%v, %v, %v), want the world spawn (0.5, 64, 0.5)", pos.X, pos.Y, pos.Z)
	}
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Sleeper"))
	if _, ok := nbt.Get[nbt.Compound](root, "respawn"); ok {
		t.Error("the broken bed's respawn point was kept in the file")
	}
}

// TestForcedRespawnUsesTheStoredRotation checks that a forced /spawnpoint
// point returns the body a tenth of a block above the cell centre, facing the
// angles the command stored.
func TestForcedRespawnUsesTheStoredRotation(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	_, c, entityID := servertest.JoinAs(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("spawnpoint @s 10 100 10 90 30")
	cx, cy, cz := int64(10), int64(100), int64(10)
	cyaw, cpitch := float32(90), float32(30)
	servertest.AwaitCommandFeedbackText(t, c, "commands.spawnpoint.success.single",
		protocol.Text{Number: &cx}, protocol.Text{Number: &cy}, protocol.Text{Number: &cz},
		protocol.Text{Float: &cyaw}, protocol.Text{Float: &cpitch},
		protocol.Text{Text: "minecraft:overworld"}, servertest.PlayerDisplayComponent("Testificate"))

	pos, noBlock := servertest.KillAndRespawn(t, c, entityID, "Testificate")
	if noBlock {
		t.Error("a forced point in open air was reported as missing")
	}
	if pos.X != 10.5 || math.Abs(pos.Y-100.1) > 1e-9 || pos.Z != 10.5 || pos.Yaw != 90 || pos.Pitch != 30 {
		t.Errorf("respawn = (%v, %v, %v) yaw %v pitch %v, want (10.5, 100.1, 10.5) yaw 90 pitch 30",
			pos.X, pos.Y, pos.Z, pos.Yaw, pos.Pitch)
	}
	c.Disconnect()
}

// TestRespawnLoadsAnUnloadedBedChunk sets a bed spawn, walks the player far
// enough that the bed's chunk unloads, and checks a death still returns the
// body to the bed. vanilla: Level.getBlockState
func TestRespawnLoadsAnUnloadedBedChunk(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	// A short view keeps the walk-away's chunk stream small.
	cfg.ViewDistance, cfg.SimulationDistance = 2, 2
	srv, c, entityID := servertest.JoinAs(t, cfg, "Sleeper", servertest.OpsListing(t, "Sleeper"))

	servertest.StandABedAndSetTheSpawn(t, srv, c)

	// Walk past the load radius so the bed's chunk is released, and wait for
	// the release's save to take it out of the world.
	c.ChatCommand("tp @s 100 64 100")
	c.Settle()
	// A moved step triggers the chunk sender, which releases the bed's chunk;
	// the teleport alone does not.
	c.StandAt(100.5, 64, 100.5, 0)
	c.Settle()
	servertest.UntilTicks(t, srv, 200, "condition", func() bool { return srv.BlockAt(5, 64, 0) == 0 })

	pos, noBlock := servertest.KillAndRespawn(t, c, entityID, "Sleeper")
	if noBlock {
		t.Error("an unloaded bed was reported as missing")
	}
	if pos.X != 4.5 || pos.Y != 64 || pos.Z != 1.5 {
		t.Errorf("respawn = (%v, %v, %v), want the bed's stand-up cell (4.5, 64, 1.5)",
			pos.X, pos.Y, pos.Z)
	}
	c.Disconnect()
}

// enterWorldDead reads the join sequence of a player whose file says they
// left on the death screen, reporting their own entity ID. It checks the
// join carries no Combat Death — the client opens the death screen from the
// empty health itself — and that the empty bar waits for the chunks: told
// before them, it would let the client stop waiting for its own chunk while
// the body is dead and fall through the ground. It finishes the join the way
// enterWorldAs does.
func enterWorldDead(c *servertest.Client, name string) int32 {
	c.T.Helper()
	var entityID int32
	var loaded bool
	for packetID, data := range c.Stream() {
		c.T.Helper()
		switch {
		case packetID == protocol.IDPlayLogin && entityID == 0:
			entityID = protocol.NewReader(data).ReadI32()
		case packetID == protocol.IDPlayEntityMetadata:
			if entity, _, hasHealth := servertest.PlayerMetadataHealth(data); entity == entityID && hasHealth {
				c.T.Error("the death-screen join's own body carried the empty bar; Set Health tells it once the chunks are out")
			}
		case packetID == protocol.IDPlaySetHealth:
			// The bar follows the chunk fill, so the client never skipped
			// waiting for its chunk while the body was dead. Entities of the
			// world may come between the two.
			if !loaded {
				c.T.Error("the death-screen join's empty bar went out before the chunks")
			}
			if got := protocol.NewReader(data).ReadF32(); got != 0 {
				c.T.Errorf("health on the death-screen join = %v, want 0", got)
			}
			if loaded {
				return entityID
			}
		case packetID == protocol.IDPlayCombatDeath:
			c.T.Error("the death-screen join resent Combat Death; the empty health opens the screen")
		case packetID == protocol.IDPlayChunkBatchFinished && !loaded:
			loaded = true
			c.MarkLoaded()
		}
	}
	c.T.Fatal("the join did not finish")
	return 0
}

// TestADeathScreenRejoinStaysDead checks a player who disconnects on the
// death screen: the next join comes back dead at the spot they fell, the
// empty health reopening the screen on the client, and the respawn from
// there stands them up at their spawn rather than the join having quietly
// raised them alive.
// vanilla: PlayerList.placeNewPlayer
func TestADeathScreenRejoinStaysDead(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Revenant"))

	c := connect()
	entityID := c.JoinAs("Revenant")

	// Die away from the world spawn so the spot the body falls at and the
	// one a respawn returns it to can be told apart.
	spawn := srv.PlayerPosition("Revenant")
	c.MoveTo(spawn.X+4, spawn.Y, spawn.Z, 0, 0)
	c.Settle()
	c.ChatCommand("kill")
	servertest.ExpectDeathSequence(t, c, entityID, protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Revenant")},
	})
	death := srv.PlayerPosition("Revenant")
	c.Disconnect()

	// The file kept the empty bar the death left, which is what makes the
	// next join a death-screen one.
	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Revenant"))
	if health, _ := nbt.Get[nbt.Float](root, "Health"); health != 0 {
		t.Fatalf("health on disk after the death-screen leave = %v, want 0", health)
	}

	again := connect()
	again.Login("Revenant")
	again.Configure()
	enterWorldDead(again, "Revenant")

	if got := srv.PlayerHealth("Revenant"); got != 0 {
		t.Errorf("health after rejoining on the death screen = %v, want 0", got)
	}
	if got := srv.PlayerPosition("Revenant"); math.Abs(got.X-death.X) > 0.001 || math.Abs(got.Z-death.Z) > 0.001 {
		t.Errorf("rejoined dead player stands at (%v, %v), want the spot they fell (%v, %v)",
			got.X, got.Z, death.X, death.Z)
	}

	// And the respawn from the reopened screen returns them to their spawn,
	// alive.
	again.PerformRespawn()
	again.AwaitPlay(protocol.IDPlayRespawn)
	if got := again.ExpectHealth(); got.Health != 20 {
		t.Errorf("health after the respawn = %v, want 20", got.Health)
	}
	again.Expect(protocol.IDPlaySetExperience)
	again.Expect(protocol.IDPlayAbilities)
	if pos := servertest.ExpectTeleport(t, again); math.Abs(pos.X-spawn.X) > 0.001 || math.Abs(pos.Z-spawn.Z) > 0.001 {
		t.Errorf("respawn = (%v, %v), want the spawn (%v, %v)", pos.X, pos.Z, spawn.X, spawn.Z)
	}
	again.Disconnect()
}

// forcedPointScene logs a player in, moves them to dim over an open stone
// platform at y=64 and sets a forced spawn point there with /spawnpoint, the
// point's cell and the one above it left as air unless blockAbove says
// otherwise. It returns the server, the client, the entity ID and the cell.
// The platform reaches the cell the player stands in, off the point, so the
// body takes no in_wall hurts while the loop runs on to the kill.
func forcedPointScene(t *testing.T, name, dim string, blockAbove bool) (*server.Server, *servertest.Client, int32, [3]int32) {
	t.Helper()
	srv, c, entityID := servertest.JoinAs(t, servertest.SurvivalTestConfig(), name, servertest.OpsListing(t, name))

	const x, y, z = 40, 65, 40
	srv.MovePlayerToDimension(name, dim, x, y, z)
	if !pumpTicks(c, srv, 1200, nil, func(int32, []byte) bool {
		return srv.PlayerDimension(name) == dim && srv.ChunkLoadedInDimension(dim, x, z)
	}) {
		t.Fatalf("%s never arrived", dim)
	}
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:stone")
	}
	const standOff = 3
	for dx := int32(-1); dx <= standOff; dx++ {
		for dz := int32(-1); dz <= standOff; dz++ {
			srv.SetBlockInDimension(dim, x+dx, y-1, z+dz, stone)
			srv.SetBlockInDimension(dim, x+dx, y, z+dz, 0)
			srv.SetBlockInDimension(dim, x+dx, y+1, z+dz, 0)
		}
	}
	if blockAbove {
		srv.SetBlockInDimension(dim, x, y+1, z, stone)
	}
	c.StandAt(x+standOff+0.5, y, z+standOff+0.5, 0)
	c.MarkLoaded()
	c.Settle()
	c.ChatCommand("spawnpoint @s 40 65 40")
	for {
		if _, _, ok := srv.SpawnPointOf(name); ok {
			break
		}
		c.Settle()
	}
	return srv, c, entityID, [3]int32{x, y, z}
}

// TestAForcedPointOutsideTheOverworldRespawnsThere checks that a
// /spawnpoint in the nether or the end brings the body back at that cell in
// that dimension, with no anchor involved.
// vanilla: ServerPlayer.findRespawnPositionAndUseSpawnBlock
func TestAForcedPointOutsideTheOverworldRespawnsThere(t *testing.T) {
	t.Parallel()
	for _, dim := range []string{world.NetherName, world.EndName} {
		t.Run(dim, func(t *testing.T) {
			t.Parallel()
			srv, c, entityID, at := forcedPointScene(t, "Forced", dim, false)
			pos, noBlock := servertest.KillAndRespawn(t, c, entityID, "Forced")
			if noBlock {
				t.Error("the forced point's respawn said no respawn block")
			}
			if pos.X != float64(at[0])+0.5 || pos.Y != float64(at[1])+0.1 || pos.Z != float64(at[2])+0.5 {
				t.Errorf("respawn = (%v, %v, %v), want the point's cell centre a tenth up", pos.X, pos.Y, pos.Z)
			}
			if got := srv.PlayerDimension("Forced"); got != dim {
				t.Errorf("the respawn left the body in %s, want %s", got, dim)
			}
			if _, forced, ok := srv.SpawnPointOf("Forced"); !ok || !forced {
				t.Errorf("the point after its respawn = forced %v, %v; want it kept", forced, ok)
			}
		})
	}
}

// standInEndPortal stands the player's feet in an end portal block in the
// end, ready for the portal tick to fire.
func standInEndPortal(t *testing.T, srv *server.Server, c *servertest.Client, name string, x, y, z int32) {
	t.Helper()
	c.StandAt(float64(x)+0.5, float64(y), float64(z)+0.5, 0)
	c.MarkLoaded()
	c.Settle()
	if !srv.SetBlockInDimension(world.EndName, x, y, z, srv.EndPortalState()) {
		t.Fatal("could not stand the end portal on")
	}
}

// TestEndPortalExitWithAForcedEndPointTeleportsThere checks that a player
// past their credits, whose forced point is in the end, is moved to the
// point rather than left standing in the portal.
// vanilla: EndPortalBlock.getPortalDestination, ServerPlayer.teleport
func TestEndPortalExitWithAForcedEndPointTeleportsThere(t *testing.T) {
	t.Parallel()
	srv, c, _, at := forcedPointScene(t, "Homebody", world.EndName, false)
	srv.SetSeenCredits("Homebody", true)
	standInEndPortal(t, srv, c, "Homebody", at[0]+3, at[1], at[2]+3)
	servertest.UntilTicks(t, srv, 400, "condition", func() bool {
		p := srv.PlayerPosition("Homebody")
		return p.X == float64(at[0])+0.5 && p.Z == float64(at[2])+0.5
	})
	if p := srv.PlayerPosition("Homebody"); p.Y != float64(at[1])+0.1 {
		t.Errorf("the exit landed at y=%v, want %v", p.Y, float64(at[1])+0.1)
	}
	if got := srv.PlayerDimension("Homebody"); got != world.EndName {
		t.Errorf("the exit left the body in %s, want the end", got)
	}
}
