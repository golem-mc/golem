package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"math"
	"reflect"
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

// vanilla: ServerPlayer.restoreFrom, AttributeMap.assignBaseValues
func TestARespawnFillsTheRaisedMaxHealthBase(t *testing.T) {
	t.Parallel()
	srv, c, entityID := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Big", servertest.OpsListing(t, "Big"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("attribute Big minecraft:max_health base set 40")
	servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")
	servertest.KillAndRespawn(t, c, entityID, "Big")
	if got := srv.PlayerHealth("Big"); got != 40 {
		t.Errorf("respawn health = %v, want 40", got)
	}
	c.MarkLoaded()
	c.Settle()

	// The next body inherits the base, without the dead body's modifier.
	// Read its health before the teleport reader consumes the respawn packets.
	c.ChatCommand("attribute Big minecraft:max_health modifier add minecraft:test 20 add_value")
	servertest.AwaitSystemChatSaying(t, c, "commands.attribute.modifier.add.success")
	c.ChatCommand("kill")
	servertest.ExpectDeathSequence(t, c, entityID, protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Big")},
	})
	c.PerformRespawn()
	c.AwaitPlay(protocol.IDPlayRespawn)
	if got := c.ExpectHealth().Health; got != 40 {
		t.Errorf("respawn Set Health = %v, want 40", got)
	}
	c.ExpectRespawnThroughTeleport()
	if got := srv.PlayerHealth("Big"); got != 40 {
		t.Errorf("respawn health with the dead body's modifier = %v, want 40", got)
	}
}

// TestALivingPlayersRespawnRequestIsIgnored checks the Client Command a
// living player, with no credits on, has no screen to send: nothing crosses
// and the body keeps its place.
// vanilla: ServerGamePacketListenerImpl.handleClientCommand
func TestALivingPlayersRespawnRequestIsIgnored(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Living")
	c.Settle()

	c.PerformRespawn()
	if pumpTicks(c, srv, 40, nil, func(id int32, data []byte) bool {
		return id == protocol.IDPlayRespawn
	}) {
		t.Fatal("a living player's respawn request sent a Respawn, want it ignored")
	}
	if got := srv.PlayerDimension("Living"); got != world.DimensionName {
		t.Errorf("a living player's respawn request left them in %q, want %q", got, world.DimensionName)
	}
}

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

// TestObstructedBedRespawnFallsBackToWorldSpawn stands a bed whose point is
// set, boxes every cell around it in and seals the space above both halves,
// and checks the respawn refuses it: a bed the body cannot step out of, or
// stand up in, is as unusable as one that is gone.
func TestObstructedBedRespawnFallsBackToWorldSpawn(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	srv, c, entityID := servertest.JoinAs(t, cfg, "Sleeper", servertest.OpsListing(t, "Sleeper"))

	servertest.StandABedAndSetTheSpawn(t, srv, c)

	// Every cell the stand-up search tries around the bed's head (5, 64, 1),
	// except the bed's own two, and the headroom above both halves.
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("no stone state")
	}
	for _, cell := range [][2]int32{
		{4, 1}, {4, 0}, {4, -1}, {5, -1}, {6, -1},
		{6, 0}, {6, 1}, {6, 2}, {5, 2}, {4, 2},
	} {
		if !srv.SetBlock(cell[0], 64, cell[1], stone) {
			t.Fatalf("could not box in (%d, 64, %d)", cell[0], cell[1])
		}
	}
	for _, cell := range [][2]int32{{5, 0}, {5, 1}} {
		if !srv.SetBlock(cell[0], 65, cell[1], stone) {
			t.Fatalf("could not seal (%d, 65, %d)", cell[0], cell[1])
		}
	}

	pos, noBlock := servertest.KillAndRespawn(t, c, entityID, "Sleeper")
	if !noBlock {
		t.Error("an obstructed bed did not send the no-respawn-block game event")
	}
	if pos.X != 0.5 || pos.Y != 64 || pos.Z != 0.5 {
		t.Errorf("respawn = (%v, %v, %v), want the world spawn (0.5, 64, 0.5)", pos.X, pos.Y, pos.Z)
	}
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Sleeper"))
	if _, ok := nbt.Get[nbt.Compound](root, "respawn"); ok {
		t.Error("the obstructed bed's respawn point was kept in the file")
	}
}

// TestBoxedInBedWithHeadroomRespawnsOnTheBed stands a bed whose point is set
// and boxes every cell around it in, leaving the space above both halves
// open, and checks the respawn stands the body on the bed's own nine
// sixteenths rather than falling back for want of a free cell beside it.
func TestBoxedInBedWithHeadroomRespawnsOnTheBed(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	srv, c, entityID := servertest.JoinAs(t, cfg, "Sleeper", servertest.OpsListing(t, "Sleeper"))

	servertest.StandABedAndSetTheSpawn(t, srv, c)

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("no stone state")
	}
	for _, cell := range [][2]int32{
		{4, 1}, {4, 0}, {4, -1}, {5, -1}, {6, -1},
		{6, 0}, {6, 1}, {6, 2}, {5, 2}, {4, 2},
	} {
		if !srv.SetBlock(cell[0], 64, cell[1], stone) {
			t.Fatalf("could not box in (%d, 64, %d)", cell[0], cell[1])
		}
	}

	pos, noBlock := servertest.KillAndRespawn(t, c, entityID, "Sleeper")
	if noBlock {
		t.Error("a bed with headroom was reported as missing")
	}
	// The search reaches the head's own cell last, standing the body on the
	// bed at 64+9/16 facing the atan2 of the cell it is already in.
	if pos.X != 5.5 || math.Abs(pos.Y-(64+9.0/16)) > 1e-9 || pos.Z != 1.5 {
		t.Errorf("respawn = (%v, %v, %v), want the bed's own cell (5.5, %v, 1.5)",
			pos.X, pos.Y, pos.Z, 64+9.0/16)
	}
	c.Disconnect()
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

// TestForcedSpawnOnAPressurePlateIsUsed sets a forced point whose cell holds
// a pressure plate, and checks the respawn uses it rather than reading the
// plate as a full cell and dropping it.
func TestForcedSpawnOnAPressurePlateIsUsed(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	srv, c, entityID := servertest.JoinAs(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	plate, ok := registry.FindState("minecraft:stone_pressure_plate", map[string]string{"powered": "false"})
	if !ok {
		t.Fatal("no stone pressure plate state")
	}
	if !srv.SetBlock(10, 100, 10, plate) {
		t.Fatal("could not stand the pressure plate")
	}
	c.ChatCommand("spawnpoint @s 10 100 10")
	c.Settle()

	pos, noBlock := servertest.KillAndRespawn(t, c, entityID, "Testificate")
	if noBlock {
		t.Error("a forced point on a pressure plate was reported as missing")
	}
	if pos.X != 10.5 || math.Abs(pos.Y-100.1) > 1e-9 || pos.Z != 10.5 {
		t.Errorf("respawn = (%v, %v, %v), want (10.5, 100.1, 10.5)", pos.X, pos.Y, pos.Z)
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

// TestLegacySpawnFieldsAreMigrated checks a pre-v4314 player file, whose
// spawn point is the old top-level SpawnX/Y/Z fields, comes back carrying the
// point and is saved still in that version's shape, so the file's own
// InlineBlockPosFormatFix builds the respawn compound from it.
func TestLegacySpawnFieldsAreMigrated(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	const name = "Wanderer"
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
		"DataVersion":    nbt.Int(4000),
		"SpawnX":         nbt.Int(1),
		"SpawnY":         nbt.Int(2),
		"SpawnZ":         nbt.Int(3),
		"SpawnAngle":     nbt.Float(90),
		"SpawnDimension": nbt.String(world.DimensionName),
		"SpawnForced":    nbt.Byte(1),
	})

	srv, c, _ := servertest.JoinAs(t, cfg, name)

	if spawn, forced, ok := srv.SpawnPointOf(name); !ok || spawn != (world.SpawnPos{X: 1, Y: 2, Z: 3}) || !forced {
		t.Errorf("spawn point = (%d, %d, %d) forced %v, %v; want the old fields' (1, 2, 3), forced, true",
			spawn.X, spawn.Y, spawn.Z, forced, ok)
	}
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, name))
	if _, ok := root["respawn"]; ok {
		t.Error("the file was saved with the current respawn compound")
	}
	for field, want := range map[string]nbt.Tag{
		"SpawnX":         nbt.Int(1),
		"SpawnY":         nbt.Int(2),
		"SpawnZ":         nbt.Int(3),
		"SpawnAngle":     nbt.Float(90),
		"SpawnDimension": nbt.String(world.DimensionName),
		"SpawnForced":    nbt.Byte(1),
	} {
		if got, ok := root[field]; !ok || got != want {
			t.Errorf("%s = %v, %v; want %v", field, got, ok, want)
		}
	}
}

// TestAngleRespawnIsMigrated checks a v4314-4547 player file, whose respawn
// compound carries the old single angle and no dimension, is read with the
// dimension defaulted to the overworld and the angle split into yaw and pitch
// - and is saved with the angle still in the shape its PlayerRespawnDataFix
// expects.
func TestAngleRespawnIsMigrated(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	const name = "Wanderer"
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
		"DataVersion": nbt.Int(4400),
		"respawn": nbt.Compound{
			"pos":   nbt.IntArray{4, 5, 6},
			"angle": nbt.Float(45),
		},
	})

	srv, c, _ := servertest.JoinAs(t, cfg, name)

	if spawn, forced, ok := srv.SpawnPointOf(name); !ok || spawn != (world.SpawnPos{X: 4, Y: 5, Z: 6}) || forced {
		t.Errorf("spawn point = (%d, %d, %d) forced %v, %v; want the angle file's (4, 5, 6), unforced, true",
			spawn.X, spawn.Y, spawn.Z, forced, ok)
	}
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, name))
	respawn, ok := nbt.Get[nbt.Compound](root, "respawn")
	if !ok {
		t.Fatal("the respawn compound was dropped")
	}
	want := nbt.Compound{
		"pos":       nbt.IntArray{4, 5, 6},
		"dimension": nbt.String(world.DimensionName),
		"angle":     nbt.Float(45),
	}
	if !reflect.DeepEqual(respawn, want) {
		t.Errorf("respawn =\n%v\nwant\n%v", respawn, want)
	}
}

// TestLegacySpawnFieldsWithoutDimensionAreMigrated checks a pre-v4314 file
// whose old top-level fields carry no dimension: the v4548 fix defaults it
// to the overworld once the v4314 fix has built the compound, and the file is
// saved again with the fields the v4314 fix reads.
func TestLegacySpawnFieldsWithoutDimensionAreMigrated(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	const name = "Wanderer"
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
		"DataVersion": nbt.Int(4000),
		"SpawnX":      nbt.Int(1),
		"SpawnY":      nbt.Int(2),
		"SpawnZ":      nbt.Int(3),
		"SpawnAngle":  nbt.Float(90),
	})

	srv, c, _ := servertest.JoinAs(t, cfg, name)

	if spawn, forced, ok := srv.SpawnPointOf(name); !ok || spawn != (world.SpawnPos{X: 1, Y: 2, Z: 3}) || forced {
		t.Errorf("spawn point = (%d, %d, %d) forced %v, %v; want the old fields' (1, 2, 3), unforced, true",
			spawn.X, spawn.Y, spawn.Z, forced, ok)
	}
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, name))
	if _, ok := root["respawn"]; ok {
		t.Error("the file was saved with the current respawn compound")
	}
	for field, want := range map[string]nbt.Tag{
		"SpawnX":         nbt.Int(1),
		"SpawnY":         nbt.Int(2),
		"SpawnZ":         nbt.Int(3),
		"SpawnAngle":     nbt.Float(90),
		"SpawnDimension": nbt.String(world.DimensionName),
	} {
		if got, ok := root[field]; !ok || got != want {
			t.Errorf("%s = %v, %v; want %v", field, got, ok, want)
		}
	}
	if _, ok := root["SpawnForced"]; ok {
		t.Error("an unforced spawn was saved with SpawnForced")
	}
}

// TestLegacyRespawnAngleKeepsAnotherDimension checks a v4314-4547 file whose
// respawn names the nether: the dimension rides through the save in the shape
// PlayerRespawnDataFix reads, not only the overworld happy path.
func TestLegacyRespawnAngleKeepsAnotherDimension(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	const name = "Wanderer"
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
		"DataVersion": nbt.Int(4400),
		"respawn": nbt.Compound{
			"pos":       nbt.IntArray{3, 70, 4},
			"dimension": nbt.String(world.NetherName),
			"angle":     nbt.Float(90),
		},
	})

	_, c, _ := servertest.JoinAs(t, cfg, name)
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, name))
	respawn, ok := nbt.Get[nbt.Compound](root, "respawn")
	if !ok {
		t.Fatal("the respawn compound was dropped")
	}
	want := nbt.Compound{
		"pos":       nbt.IntArray{3, 70, 4},
		"dimension": nbt.String(world.NetherName),
		"angle":     nbt.Float(90),
	}
	if !reflect.DeepEqual(respawn, want) {
		t.Errorf("respawn =\n%v\nwant\n%v", respawn, want)
	}
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

// TestALongDeadRejoinLoadsItsChunksBeforeTheHoldsDrop checks a player who
// left after the corpse count ran out (saved DeathTime >= 20): the rejoin
// still sends the join chunks, then opens the death screen, and respawns.
// vanilla: PlayerList.placeNewPlayer
func TestALongDeadRejoinLoadsItsChunksBeforeTheHoldsDrop(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Revenant"))

	c := connect()
	entityID := c.JoinAs("Revenant")
	c.ChatCommand("kill")
	servertest.ExpectDeathSequence(t, c, entityID, protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Revenant")},
	})
	servertest.RunTicks(t, srv, 30)
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Revenant"))
	if dt, _ := nbt.Get[nbt.Short](root, "DeathTime"); dt < 20 {
		t.Fatalf("DeathTime on disk = %v, want at least 20", dt)
	}

	again := connect()
	again.Login("Revenant")
	again.Configure()
	enterWorldDead(again, "Revenant")
	if got := srv.PlayerHealth("Revenant"); got != 0 {
		t.Errorf("health after rejoining = %v, want 0", got)
	}
	again.PerformRespawn()
	again.AwaitPlay(protocol.IDPlayRespawn)
	if got := again.ExpectHealth(); got.Health != 20 {
		t.Errorf("health after the respawn = %v, want 20", got.Health)
	}
	again.Disconnect()
}

// TestADeathScreenRejoinInTheNetherStaysDead checks the same rejoin with the
// death in the nether: the body comes back dead at the spot it fell, kept in
// the nether the file saved, and the respawn brings it home to the overworld
// spawn rather than the other dimension.
func TestADeathScreenRejoinInTheNetherStaysDead(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Revenant"))

	c := connect()
	entityID := c.JoinAs("Revenant")
	spawn := srv.PlayerPosition("Revenant")

	// The nether's rock fills the cell at y=40 and suffocates the body in
	// about 20s; on a loaded runner the stream read below outlasts that, and
	// the /kill then lands on a body already dead. Hold the chunk and stand
	// the body on a stone floor in clear air.
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 8, 8))
	srv.SetBlockInDimension(world.NetherName, 8, 39, 8, servertest.MustState(t, "minecraft:stone", nil))
	srv.SetBlockInDimension(world.NetherName, 8, 40, 8, 0)
	srv.SetBlockInDimension(world.NetherName, 8, 41, 8, 0)
	srv.MovePlayerToDimension("Revenant", world.NetherName, 8, 40, 8)
	// The nether's own stream arrives around the arrival and is read through
	// before the test kills the player there.
	servertest.AwaitViewSent(t, c)
	c.ChatCommand("kill")
	servertest.ExpectDeathSequence(t, c, entityID, protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Revenant")},
	})
	death := srv.PlayerPosition("Revenant")
	c.Disconnect()

	again := connect()
	again.Login("Revenant")
	again.Configure()
	enterWorldDead(again, "Revenant")

	if got := srv.PlayerHealth("Revenant"); got != 0 {
		t.Errorf("health after rejoining on the death screen = %v, want 0", got)
	}
	if got := srv.PlayerDimension("Revenant"); got != world.NetherName {
		t.Errorf("dimension after the dead rejoin = %q, want the nether", got)
	}
	if got := srv.PlayerPosition("Revenant"); math.Abs(got.X-death.X) > 0.001 || math.Abs(got.Z-death.Z) > 0.001 {
		t.Errorf("rejoined dead player stands at (%v, %v), want the spot they fell (%v, %v)",
			got.X, got.Z, death.X, death.Z)
	}

	again.PerformRespawn()
	again.AwaitPlay(protocol.IDPlayRespawn)
	if pos := servertest.ExpectTeleport(t, again); math.Abs(pos.X-spawn.X) > 0.001 || math.Abs(pos.Z-spawn.Z) > 0.001 {
		t.Errorf("respawn = (%v, %v), want the overworld spawn (%v, %v)", pos.X, pos.Z, spawn.X, spawn.Z)
	}
	if got := srv.PlayerHealth("Revenant"); got != 20 {
		t.Errorf("health after the respawn = %v, want 20", got)
	}
	if got := srv.PlayerDimension("Revenant"); got != world.DimensionName {
		t.Errorf("dimension after the respawn = %q, want the overworld", got)
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

// TestABlockedForcedPointOutsideTheOverworldFallsBackToTheSpawn checks that a
// forced point whose upper cell is solid sends the body to the world spawn
// in the overworld and drops the point, in the nether and the end alike.
func TestABlockedForcedPointOutsideTheOverworldFallsBackToTheSpawn(t *testing.T) {
	t.Parallel()
	for _, dim := range []string{world.NetherName, world.EndName} {
		t.Run(dim, func(t *testing.T) {
			t.Parallel()
			srv, c, entityID, _ := forcedPointScene(t, "Boxed", dim, true)
			_, noBlock := servertest.KillAndRespawn(t, c, entityID, "Boxed")
			if !noBlock {
				t.Error("the blocked point's respawn did not say no respawn block")
			}
			if got := srv.PlayerDimension("Boxed"); got != world.DimensionName {
				t.Errorf("the respawn left the body in %s, want the overworld", got)
			}
			if _, _, ok := srv.SpawnPointOf("Boxed"); ok {
				t.Error("the blocked point was kept")
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

// TestCreditsRespawnWithAForcedEndPointLandsThere checks that the respawn
// the credits end with puts the body at a forced point in the end.
// vanilla: ServerPlayer.findRespawnAndUseSpawnBlock
func TestCreditsRespawnWithAForcedEndPointLandsThere(t *testing.T) {
	t.Parallel()
	srv, c, _, at := forcedPointScene(t, "Watcher", world.EndName, false)
	standInEndPortal(t, srv, c, "Watcher", at[0]+3, at[1], at[2]+3)
	servertest.ExpectEndCredits(t, c, srv)
	c.PerformRespawn()
	pos, noBlock := c.ExpectRespawnThroughTeleport()
	if noBlock {
		t.Error("the forced point's respawn said no respawn block")
	}
	if pos.X != float64(at[0])+0.5 || pos.Y != float64(at[1])+0.1 || pos.Z != float64(at[2])+0.5 {
		t.Errorf("respawn = (%v, %v, %v), want the point's cell centre a tenth up", pos.X, pos.Y, pos.Z)
	}
	if got := srv.PlayerDimension("Watcher"); got != world.EndName {
		t.Errorf("the respawn left the body in %s, want the end", got)
	}
}

// TestEndPortalExitWithAnUnforcedEndPointSaysNoRespawnBlock checks that an
// unforced point in the end, which holds no bed or anchor, is refused with
// the missing-block event and the body goes home to the overworld.
// vanilla: ServerPlayer.findRespawnAndUseSpawnBlock
func TestEndPortalExitWithAnUnforcedEndPointSaysNoRespawnBlock(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	const name = "Stranded"
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
		"DataVersion":    nbt.Int(4000),
		"SpawnX":         nbt.Int(40),
		"SpawnY":         nbt.Int(65),
		"SpawnZ":         nbt.Int(40),
		"SpawnDimension": nbt.String(world.EndName),
		"SpawnForced":    nbt.Byte(0),
	})
	srv, c, _ := servertest.JoinAs(t, cfg, name)
	srv.SetSeenCredits(name, true)

	arrival := world.EndArrival()
	srv.MovePlayerToDimension(name, world.EndName,
		float64(arrival.X), float64(arrival.Y), float64(arrival.Z))
	if !pumpTicks(c, srv, 1200, nil, func(int32, []byte) bool {
		return srv.PlayerDimension(name) == world.EndName &&
			srv.ChunkLoadedInDimension(world.EndName, arrival.X, arrival.Z)
	}) {
		t.Fatal("the end never arrived")
	}
	if !srv.SetBlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z, srv.EndPortalState()) {
		t.Fatal("could not stand the end portal on")
	}
	sawNoBlock, sawCredits := false, false
	see := func(id int32, data []byte) {
		if id == protocol.IDPlayGameEvent {
			switch protocol.NewReader(data).ReadU8() {
			case protocol.GameEventNoRespawnBlockAvailable:
				sawNoBlock = true
			case protocol.GameEventWinGame:
				sawCredits = true
			}
		}
	}
	pumpTicks(c, srv, 400, nil, func(id int32, data []byte) bool {
		see(id, data)
		return srv.PlayerDimension(name) == world.DimensionName
	})
	// The event is queued before the crossing, but the dimension can read
	// home before the mailbox has written it; the settle's answer comes
	// behind it.
	c.SettleAndCollect(see)
	if got := srv.PlayerDimension(name); got != world.DimensionName {
		seen, won, traveling := srv.EndExitState(name)
		t.Fatalf("the exit left the body in %s, want the overworld (tick %d, at %+v, portal cooldown %d, "+
			"missing block told %v, credits shown %v, seen credits %v, won %v, traveling %v)",
			got, srv.TicksRun(), srv.PlayerPosition(name), srv.PortalCooldown(name),
			sawNoBlock, sawCredits, seen, won, traveling)
	}
	if !sawNoBlock {
		t.Error("the exit told no missing block for the unforced end point")
	}
}
