package commands

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"reflect"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// feedbackArgsForTest builds the arguments of a feedback line. They are
// plain text, except that /setblock sends its coordinates as ints.
// vanilla: SetBlockCommand.setBlock
func feedbackArgsForTest(key string, args []string) []protocol.Text {
	with := make([]protocol.Text, 0, len(args))
	for _, arg := range args {
		if key == "commands.setblock.success" {
			n, err := strconv.ParseInt(arg, 10, 64)
			if err != nil {
				panic(err)
			}
			with = append(with, protocol.Text{Number: &n})
			continue
		}
		with = append(with, protocol.Text{Text: arg})
	}
	return with
}

// TestTeleportToCoordinates checks /tp with absolute and relative
// coordinates: the player's client is told where they ended up, and the
// server agrees with it.
func TestTeleportToCoordinates(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// /teleport is a gamemaster's command, so the runner is listed as an
	// operator.
	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	// The spawn the join puts the player at is read out of the join stream
	// itself, which ends at the teleport that puts them there; the rest of
	// the join is skipped past afterwards.
	c.SpawnedAt() // (0.5, 64, 0.5) in the flat world
	c.SkipJoin()

	// An absolute position. Like vanilla's, a whole x or z is the middle of
	// its block, and the feedback names the target - by the display name
	// the vanilla server's own feedback carries - and then the three
	// coordinates, each its own argument in Java's "%f" form - what the
	// vanilla server was seen to send for the same command.
	c.ChatCommand("tp 10 64 20")
	servertest.AwaitCommandFeedbackText(t, c, "commands.teleport.success.location.single",
		servertest.PlayerDisplayComponent("Testificate"),
		protocol.Text{Text: "10.500000"}, protocol.Text{Text: "64.000000"}, protocol.Text{Text: "20.500000"})
	if pos := servertest.ExpectTeleport(t, c); pos.X != 10.5 || pos.Y != 64 || pos.Z != 20.5 {
		t.Errorf("teleport = %v, want (10.5, 64, 20.5)", pos)
	}
	c.Settle()

	// Relative coordinates move from where the player is now.
	c.ChatCommand("tp ~5 ~ ~-5")
	if pos := servertest.ExpectTeleport(t, c); pos.X != 15.5 || pos.Y != 64 || pos.Z != 15.5 {
		t.Errorf("relative teleport = %v, want (15.5, 64, 15.5)", pos)
	}

	// A rotation after the position arrives facing it. Vanilla only takes
	// one after a target, so the player names themselves.
	c.ChatCommand("tp @s ~ ~ ~ 90 -20")
	if pos := servertest.ExpectTeleport(t, c); pos.Yaw != 90 || pos.Pitch != -20 {
		t.Errorf("rotated teleport yaw = %v pitch = %v, want 90 and -20", pos.Yaw, pos.Pitch)
	}
	c.Disconnect()
}

// TestTeleportToAPlayer checks the two-player form: the target arrives where
// the destination stands, and the feedback names them both.
func TestTeleportToAPlayer(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// /teleport is a gamemaster's command, so Mover, who runs it, is
	// listed as an operator; Helper is only ever teleported to.
	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Mover"))

	helper := connect().Join("Helper")

	mover := connect().Join("Mover")

	// Helper sees Mover arrive before Mover can teleport to Helper.
	helper.ExpectPlayerAdded("Mover")
	helper.ExpectAnnouncement("multiplayer.player.joined", "Mover")

	helper.MoveToGround(20.5, 64, 30.5, true)
	helper.Settle()

	mover.ChatCommand("tp Helper")
	// Both the teleport and its feedback ride the mailbox; the reader steps
	// over whatever else is on the way. Both the mover and the destination
	// are named by their display names.
	servertest.AwaitCommandFeedbackText(t, mover, "commands.teleport.success.entity.single",
		servertest.PlayerDisplayComponent("Mover"), servertest.PlayerDisplayComponent("Helper"))
	if pos := servertest.ExpectTeleport(t, mover); pos.X != 20.5 || pos.Z != 30.5 {
		t.Errorf("teleport = %v, want Helper's (20.5, 30.5)", pos)
	}
	mover.Disconnect()
	helper.Disconnect()
}

// TestTeleportTellsTheWatchers checks that the others see where a /tp puts a
// player: the absolute Teleport Entity where the step is too big for a
// relative one, the relative step with the turn where it is not, the head
// turning with the body, and the moves the client reports after measuring
// from the arrival. The packets are the ones a vanilla server sends for
// the same two teleports.
func TestTeleportTellsTheWatchers(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// /tp is a gamemaster's command, so Mover, who runs it, is listed as an
	// operator; Watcher only watches.
	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Mover"))

	watcher := connect().Join("Watcher")

	mover := connect().Join("Mover")

	// Watcher sees Mover arrive before Mover can teleport away.
	moverID := watcher.ExpectPlayerAdded("Mover")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Mover")

	// A teleport further than a relative packet reaches, still inside the
	// four chunks of sight the watcher's render distance leaves. The
	// mover's own teleports are awaited first, so the accepts they need go
	// out before the next command lands.
	mover.ChatCommand("tp @s 30.5 64 30.5 0 0")
	servertest.ExpectTeleport(t, mover)
	data := watcher.AwaitPacket(protocol.IDPlayTeleportEntity)
	r := protocol.NewReader(data)
	if id := r.ReadVarInt(); id != moverID {
		t.Fatalf("teleported entity = %d, want %d", id, moverID)
	}
	r.ReadVarInt() // position path
	x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
	if x != 30.5 || y != 64 || z != 30.5 {
		t.Errorf("teleported mover stands at (%v, %v, %v), want (30.5, 64, 30.5)", x, y, z)
	}
	if yaw, pitch := r.ReadF32(), r.ReadF32(); yaw != 0 || pitch != 0 {
		t.Errorf("teleported mover faces %v/%v, want 0 and 0", yaw, pitch)
	}

	// A second teleport six blocks away with a quarter turn: a teleport is
	// told absolute even where a relative step would reach, and the head turns
	// with the body.
	mover.ChatCommand("tp @s 36.5 64 36.5 90 0")
	servertest.ExpectTeleport(t, mover)
	data = watcher.AwaitPacket(protocol.IDPlayTeleportEntity)
	r = protocol.NewReader(data)
	if id := r.ReadVarInt(); id != moverID {
		t.Fatalf("teleported entity = %d, want %d", id, moverID)
	}
	r.ReadVarInt() // position path
	x, y, z = r.ReadF64(), r.ReadF64(), r.ReadF64()
	if x != 36.5 || y != 64 || z != 36.5 {
		t.Errorf("teleported mover stands at (%v, %v, %v), want (36.5, 64, 36.5)", x, y, z)
	}
	if yaw, pitch := r.ReadF32(), r.ReadF32(); yaw != 90 || pitch != 0 {
		t.Errorf("teleported mover faces %v/%v, want 90 and 0", yaw, pitch)
	}
	data = watcher.AwaitPacket(protocol.IDPlayHeadRotation)
	r = protocol.NewReader(data)
	if id := r.ReadVarInt(); id != moverID {
		t.Fatalf("turned head of entity %d, want %d", id, moverID)
	}
	if yaw := r.ReadU8(); yaw != 64 {
		t.Errorf("turned head faces %d, want the yaw 90's 64", yaw)
	}

	// A move the client reports after measures from the arrival, so the
	// watcher follows the rest of the walk too.
	// The ground state stays the arrival's, or its change would be told
	// absolute.
	mover.Send(protocol.IDPlayPositionAndRotation, func(b *protocol.Buffer) {
		b.PutF64(37.5)
		b.PutF64(64)
		b.PutF64(36.5)
		b.PutF32(90)
		b.PutF32(0)
		b.PutU8(0x00)
	})
	// The schedule's periodic packets carry no step, so they are stepped over.
	var dx, dy, dz int16
	for dx == 0 && dy == 0 && dz == 0 {
		data = watcher.AwaitPacket(protocol.IDPlayEntityPosition)
		r = protocol.NewReader(data)
		if id := r.ReadVarInt(); id != moverID {
			t.Fatalf("stepped entity = %d, want %d", id, moverID)
		}
		r.ReadVarInt() // properties
		dx, dy, dz = r.ReadI16(), r.ReadI16(), r.ReadI16()
	}
	if dx != 4096 || dy != 0 || dz != 0 {
		t.Errorf("stepped mover by (%d, %d, %d), want (4096, 0, 0)", dx, dy, dz)
	}

	mover.Disconnect()
	watcher.Disconnect()
}

// TestTeleportTargetsMovesEveryone checks the form that names who moves:
// every player the selector names ends up where the command says, and the
// feedback counts them.
func TestTeleportTargetsMovesEveryone(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// /teleport is a gamemaster's command, so Alice, who runs it, is
	// listed as an operator; Bob and Carol are only ever moved.
	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))

	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	carol := connect().Join("Carol")
	alice.ExpectPlayerAdded("Carol")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Carol")

	// Alice runs the teleport; she and Bob and Carol all go where it says.
	// The teleports and the feedback all ride the mailbox; the feedback is
	// awaited first, stepping over whatever else is on the way.
	alice.ChatCommand("tp @a 40 64 50")
	// The count the multiple feedback carries travels as a number, the way
	// the vanilla server hands a size to a translation.
	count := int64(3)
	servertest.AwaitCommandFeedbackText(t, alice, "commands.teleport.success.location.multiple",
		protocol.Text{Number: &count},
		protocol.Text{Text: "40.500000"}, protocol.Text{Text: "64.000000"}, protocol.Text{Text: "50.500000"})
	for _, watcher := range []struct {
		name   string
		client *servertest.Client
	}{
		{"Alice", alice},
		{"Bob", bob},
		{"Carol", carol},
	} {
		if pos := servertest.ExpectTeleport(t, watcher.client); pos.X != 40.5 || pos.Y != 64 || pos.Z != 50.5 {
			t.Errorf("%s teleport = %v, want (40.5, 64, 50.5)", watcher.name, pos)
		}
	}

	alice.Disconnect()
	bob.Disconnect()
	carol.Disconnect()
}

// TestTeleportRefusesWordsTheTreeReadsAsTargets checks that the numbers a
// teleport walks the targets branch with are targets first: a row of six
// numbers is a target, a position and a rotation, and the target the world
// has nobody for is refused, as is a single number read as a destination
// nobody answers to.
func TestTeleportRefusesWordsTheTreeReadsAsTargets(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// /teleport is a gamemaster's command, so the runner is listed as an
	// operator.
	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt() // (0.5, 64, 0.5) in the flat world
	c.SkipJoin()

	c.ChatCommand("teleport 1 2 3 4 5 6")
	servertest.ExpectCommandError(t, c, "argument.entity.notfound.entity")

	// One number alone has nowhere to walk but the destination branch.
	c.ChatCommand("teleport 1")
	servertest.ExpectCommandError(t, c, "argument.entity.notfound.entity")

	c.Disconnect()
}

// TestTeleportRotationArrivesWrapped checks that a rotation past the half
// circle arrives wrapped in the Synchronize Player Position, and that the
// local coordinates of the teleports that follow measure from the rotation
// the body keeps. vanilla: TeleportCommand.performTeleport
func TestTeleportRotationArrivesWrapped(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// /teleport is a gamemaster's command, so the runner is listed as an
	// operator.
	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt() // (0.5, 64, 0.5) in the flat world
	c.SkipJoin()

	c.ChatCommand("tp @s 0.5 100 0.5 200 -80")
	if pos := servertest.ExpectTeleport(t, c); pos.Yaw != -160 || pos.Pitch != -80 {
		t.Errorf("rotated teleport faces %v/%v, want -160 and -80", pos.Yaw, pos.Pitch)
	}

	// The next teleport's local offsets measure from the rotation as it
	// was stored, where an unwrapped yaw shows; these are the places a
	// vanilla server answered the same two commands with.
	c.ChatCommand("execute anchored eyes run tp ^1 ^1 ^1")
	if pos := servertest.ExpectTeleport(t, c); servertest.FormatCoordForTest(pos.X) != "-0.717036" ||
		servertest.FormatCoordForTest(pos.Y) != "102.778499" || servertest.FormatCoordForTest(pos.Z) != "0.920240" {
		t.Errorf("local teleport = %v, want (-0.717036, 102.778499, 0.920240)", pos)
	}

	c.Disconnect()
}

// TestTeleportRotationStoresAMobPitchClamped checks the pitch a teleported
// mob's body keeps: the Teleport Entity the watchers are shown carries
// straight up where the command asked past it.
func TestTeleportRotationStoresAMobPitchClamped(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.MobTestConfig(), "Testificate", servertest.OpsListing(t, "Testificate"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	// A NoAI cow stands where the summon put it, so the one Teleport Entity
	// about it is the teleport's own.
	c.ChatCommand("summon minecraft:cow 5.5 64 5.5 {NoAI:1b}")
	mob := c.ExpectMobSpawn(protocol.EntityTypeCow)

	c.ChatCommand("tp @e[type=cow] 0.5 64 0.5 20 160")
	data := c.AwaitPacket(protocol.IDPlayTeleportEntity)
	r := protocol.NewReader(data)
	if id := r.ReadVarInt(); id != mob.EntityID {
		t.Fatalf("teleported entity = %d, want %d", id, mob.EntityID)
	}
	r.ReadVarInt() // position path
	x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
	if x != 0.5 || y != 64 || z != 0.5 {
		t.Errorf("teleported cow stands at (%v, %v, %v), want (0.5, 64, 0.5)", x, y, z)
	}
	yaw, pitch := r.ReadF32(), r.ReadF32()
	if yaw != 20 {
		t.Errorf("teleported cow yaw = %v, want 20", yaw)
	}
	if pitch != 90 {
		t.Errorf("teleported cow pitch = %v, want the straight-up 90 the body keeps", pitch)
	}
	// A NoAI mob never runs travel, so its ground flag stays false and a
	// teleport leaves it so.
	if r.ReadBool() {
		t.Error("the teleported NoAI cow was reported on the ground, want airborne")
	}
	if err := r.Err(); err != nil {
		t.Fatalf("teleport entity: %v", err)
	}
	if r.Remaining() != 0 {
		t.Errorf("teleport entity has %d bytes left over", r.Remaining())
	}

	c.Disconnect()
}

// TestKillDiscardsThrownBodiesAndArmorStands checks that /kill reaches the
// thrown projectiles and the armor stands a selector names: both leave the
// world rather than being answered with a success while staying put.
func TestKillDiscardsThrownBodiesAndArmorStands(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Killer", servertest.OpsListing(t, "Killer"))

	// The loop is stopped so the thrown pearl stays in the air to be killed
	// rather than landing first.
	srv.StopTick()

	c.ChatCommand("summon minecraft:armor_stand 1.5 64 0.5")
	stand := c.ExpectArmorStandSpawn()

	srv.GiveSlot("Killer", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:ender_pearl"), Count: 16})
	c.MoveTo(0.5, 64, 0.5, -90, -60)
	servertest.UseHeld(c, -90, -60)
	pearl := c.ExpectThrownSpawn(srv, protocol.EntityTypeEnderPearl)

	c.ChatCommand("kill @e[type=minecraft:armor_stand]")
	servertest.AwaitCommandFeedbackText(t, c, "commands.kill.success.single",
		servertest.EntityDisplayComponent("minecraft:armor_stand", "entity.minecraft.armor_stand", stand.Identity))

	c.ChatCommand("kill @e[type=minecraft:ender_pearl]")
	servertest.AwaitCommandFeedbackText(t, c, "commands.kill.success.single",
		servertest.EntityDisplayComponent("minecraft:ender_pearl", "entity.minecraft.ender_pearl", pearl.Uuid))

	if got := srv.ArmorStandCount(); got != 0 {
		t.Errorf("armor stands left = %d, want none", got)
	}
	if got := srv.ThrownInDimension(world.DimensionName); len(got) != 0 {
		t.Errorf("thrown bodies left = %d, want none", len(got))
	}
	c.Disconnect()
}

// TestTeleportMovesThrownBodiesAndArmorStands checks that /tp reaches the
// thrown projectiles and the armor stands a selector names, in the same
// dimension and across to the nether and the End: each body is moved, not
// answered with a success while staying put.
func TestTeleportMovesThrownBodiesAndArmorStands(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Mover", servertest.OpsListing(t, "Mover"))
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 0, 0))

	// The loop is stopped so the thrown pearl stays in the air to be moved
	// rather than landing first.
	srv.StopTick()

	c.ChatCommand("summon minecraft:armor_stand 1.5 64 0.5")
	stand := c.ExpectArmorStandSpawn()

	srv.GiveSlot("Mover", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:ender_pearl"), Count: 16})
	c.MoveTo(0.5, 64, 0.5, -90, -60)
	servertest.UseHeld(c, -90, -60)
	pearl := c.ExpectThrownSpawn(srv, protocol.EntityTypeEnderPearl)

	// Same dimension: each body gets its own Teleport Entity to the place
	// the command named.
	c.ChatCommand("tp @e[type=minecraft:armor_stand] 3.5 64 7.5")
	// The stand's teleport is told by the tracker's pass, on a tick.
	c.Settle()
	srv.RunTick()
	data := c.AwaitPacket(protocol.IDPlayTeleportEntity)
	r := protocol.NewReader(data)
	if id := r.ReadVarInt(); id != stand.EntityID {
		t.Fatalf("teleported entity = %d, want the armor stand's %d", id, stand.EntityID)
	}
	r.ReadVarInt() // position path
	if x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64(); x != 3.5 || y != 64 || z != 7.5 {
		t.Errorf("the armor stand stands at (%v, %v, %v), want (3.5, 64, 7.5)", x, y, z)
	}

	c.ChatCommand("tp @e[type=minecraft:ender_pearl] 2.5 70 6.5")
	// The pearl is told on its flight's own schedule, a few ticks' fall
	// later, and does not reach the ground in that time.
	c.Settle()
	for range 10 { // one flight gate, every ten ticks
		srv.RunTick()
	}
	data = c.AwaitPacket(protocol.IDPlayTeleportEntity)
	r = protocol.NewReader(data)
	if id := r.ReadVarInt(); id != pearl.EntityID {
		t.Fatalf("teleported entity = %d, want the pearl's %d", id, pearl.EntityID)
	}
	r.ReadVarInt() // position path
	if x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64(); x != 2.5 || y > 70 || y < 68 || z != 6.5 {
		t.Errorf("the pearl stands at (%v, %v, %v), want (2.5, between 68 and 70, 6.5)", x, y, z)
	}

	// Across dimensions: the body is picked in the overworld and the tail
	// teleports the @s it named into the nether, by twin of the same
	// identity, with none left behind.
	c.ChatCommand("execute as @e[type=minecraft:armor_stand] in minecraft:the_nether run tp @s 0.5 64 0.5")
	c.Settle()
	servertest.UntilTicks(t, srv, 200, "condition", func() bool {
		return len(srv.ArmorStandsInDimension(world.NetherName)) == 1
	})
	stands := srv.ArmorStandsInDimension(world.NetherName)
	if stands[0].UUID != stand.Identity.String() {
		t.Errorf("the crossed stand's identity = %v, want the stand's %v", stands[0].UUID, stand.Identity)
	}
	if got := srv.ArmorStandsInDimension(world.DimensionName); len(got) != 0 {
		t.Errorf("armor stands left in the overworld = %d, want none", len(got))
	}

	// On again into the End, the same twin path for a second dimension.
	c.ChatCommand("execute in minecraft:the_nether as @e[type=minecraft:armor_stand] in minecraft:the_end run tp @s 0.5 64 0.5")
	c.Settle()
	servertest.UntilTicks(t, srv, 200, "condition", func() bool {
		return len(srv.ArmorStandsInDimension(world.EndName)) == 1
	})
	if got := srv.ArmorStandsInDimension(world.NetherName); len(got) != 0 {
		t.Errorf("armor stands left in the nether = %d, want none", len(got))
	}

	c.ChatCommand("execute as @e[type=minecraft:ender_pearl] in minecraft:the_nether run tp @s 0.5 64 0.5")
	c.Settle()
	servertest.UntilTicks(t, srv, 200, "condition", func() bool {
		return len(srv.ThrownInDimension(world.NetherName)) == 1
	})
	pearls := srv.ThrownInDimension(world.NetherName)
	if pearls[0].UUID != pearl.Uuid.String() {
		t.Errorf("the crossed pearl's identity = %v, want the thrown one's %v", pearls[0].UUID, pearl.Uuid)
	}
	if got := srv.ThrownInDimension(world.DimensionName); len(got) != 0 {
		t.Errorf("thrown bodies left in the overworld = %d, want none", len(got))
	}
	c.Disconnect()
}

// TestTeleportFacingTurnsWhereVanillaTurns checks the facing clause's
// angles: they are worked out with the sine-table arithmetic vanilla aims
// with, whose answers differ from the exact ones by steps the size of the
// tables it reads, and it is those steps the places a vanilla server leaves
// a teleported body at turn on.
func TestTeleportFacingTurnsWhereVanillaTurns(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// /teleport is a gamemaster's command, so the runner is listed as an
	// operator.
	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt() // (0.5, 64, 0.5) in the flat world
	c.SkipJoin()

	// A facing level with the eyes and a quarter turn away: the yaw and
	// pitch the arrival holds, and with them the place a local step
	// forward from the eyes takes, are the vanilla server's own for the
	// same command.
	c.ChatCommand("execute anchored eyes facing 5 65.62 5 run tp @s ^ ^ ^1")
	if pos := servertest.ExpectTeleport(t, c); servertest.FormatCoordForTest(pos.X) != "1.207175" ||
		servertest.FormatCoordForTest(pos.Y) != "65.620000" || servertest.FormatCoordForTest(pos.Z) != "1.207039" {
		t.Errorf("faced teleport = %v, want (1.207175, 65.620000, 1.207039)", pos)
	}

	c.Disconnect()
}

// TestTeleportFacingFormsTurnTheRunner checks the facing clause of the
// target-and-position forms: the runner arrives and turns to face the spot
// or entity named, where a vanilla server turns them too. The angles are
// worked out from where the body has landed towards the anchored spot, the
// feet-based way vanilla's own facing measures it, with the sine-table
// arithmetic the other facing test pins down.
func TestTeleportFacingFormsTurnTheRunner(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// /teleport is a gamemaster's command, so Mover, who runs it, is
	// listed as an operator; Helper is only ever looked at.
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Mover"))

	helper := connect().Join("Helper")

	mover := connect().Join("Mover")

	helper.ExpectPlayerAdded("Mover")
	helper.ExpectAnnouncement("multiplayer.player.joined", "Mover")

	helper.MoveToGround(20.5, 64, 30.5, true)
	helper.Settle()

	// A facing spot level with the landing: whole coordinates name the
	// middle of their block, so the spot is (5.5, 100, 5.5). The position
	// packet carries the rotation as a zero relative offset and the turn as
	// a Player Look At, which the client applies; the server takes the body's
	// rotation from the client's confirm. The angles are checked by
	// TestLookAnglesFromAimTheBody in package command.
	// vanilla: LookAt.perform
	mover.ChatCommand("tp @s 0.5 100 0.5 facing 5 100 5")
	servertest.ExpectTeleport(t, mover)
	if got, want := servertest.ExpectLookAt(t, mover), (servertest.LookAtView{X: 5.5, Y: 100, Z: 5.5}); got != want {
		t.Errorf("faced teleport looks at %+v, want %+v", got, want)
	}
	servertest.AwaitCommandFeedbackText(t, mover, "commands.teleport.success.location.single",
		servertest.PlayerDisplayComponent("Mover"),
		protocol.Text{Text: "0.500000"}, protocol.Text{Text: "100.000000"}, protocol.Text{Text: "0.500000"})
	if pos := srv.PlayerPosition("Mover"); pos.X != 0.5 || pos.Y != 100 || pos.Z != 0.5 {
		t.Errorf("faced teleport landed at %v, want (0.5, 100, 0.5)", pos)
	}

	// A facing spot given relative to the landing.
	mover.ChatCommand("tp @s 0.5 100 0.5 facing ~1 ~ ~1")
	servertest.ExpectTeleport(t, mover)
	if got, want := servertest.ExpectLookAt(t, mover), (servertest.LookAtView{X: 1.5, Y: 100, Z: 1.5}); got != want {
		t.Errorf("relatively faced teleport looks at %+v, want %+v", got, want)
	}

	// An entity to face, by its feet and by its eyes.
	mover.ChatCommand("tp @s 0.5 100 0.5 facing entity Helper feet")
	servertest.ExpectTeleport(t, mover)
	feet := servertest.ExpectLookAt(t, mover)
	if feet.Entity != helper.PlayerEntityID || feet.ToEyes || feet.X != 20.5 || feet.Y != 64 || feet.Z != 30.5 {
		t.Errorf("feet-faced teleport looks at %+v, want Helper's feet", feet)
	}

	mover.ChatCommand("tp @s 0.5 100 0.5 facing entity Helper eyes")
	servertest.ExpectTeleport(t, mover)
	if eyes := servertest.ExpectLookAt(t, mover); eyes.Entity != helper.PlayerEntityID || !eyes.ToEyes {
		t.Errorf("eyes-faced teleport looks at %+v, want Helper's eyes", eyes)
	}

	mover.Disconnect()
	helper.Disconnect()
}

// TestALocalFacingTeleportTurnsTheRunner checks a ^ move with a facing clause:
// the ^ moves the body without a rotation of its own, but the facing clause
// still turns it, so the runner does not keep the look it had.
func TestALocalFacingTeleportTurnsTheRunner(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt() // (0.5, 64, 0.5) in the flat world
	c.SkipJoin()

	// The runner's own look, which the ^ move must not keep, set through
	// the command path so the wait on the join teleport cannot drop it.
	c.ChatCommand("tp @s 0.5 64 0.5 -90 0")
	servertest.ExpectTeleport(t, c)

	// The ^ step runs a block east from yaw -90, to (1.5, 64, 0.5); the
	// facing clause then turns the body from there towards (5.5, 64, 5.5).
	// The body is read before the client reads the teleport: the packet
	// carries a zero rotation offset, so the client's accept reports the old
	// look and the server takes it until the client's next move.
	// vanilla: ServerGamePacketListenerImpl.handleAcceptTeleportPacket
	c.ChatCommand("tp @s ^ ^ ^1 facing 5 64 5")
	var pos player.Position
	servertest.UntilTicks(t, srv, 40, "the faced local teleport", func() bool {
		pos = srv.PlayerPosition("Testificate")
		return pos.X != 0.5
	})
	if pos.X != 1.5 || pos.Y != 64 || pos.Z != 0.5 {
		t.Errorf("the faced local teleport landed at %v, want (1.5, 64, 0.5)", pos)
	}
	if servertest.Abs64(float64(pos.Yaw)-(-38.659809)) > 0.01 || pos.Pitch != 0 {
		t.Errorf("the faced local teleport faces %v/%v, want about -38.66 and level", pos.Yaw, pos.Pitch)
	}
	servertest.ExpectTeleport(t, c)
	c.Disconnect()
}

// TestALocalFacingTeleportTurnsAMob checks the same form for a mob: a ^ move
// with a facing clause turns the mob to the spot rather than leaving it with
// its old look.
func TestALocalFacingTeleportTurnsAMob(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.MobTestConfig(), "Testificate", servertest.OpsListing(t, "Testificate"))
	// The runner faces east, so a one-block ^ step lands at (3.5, 64, 4.5).
	c.MoveTo(2.5, 64, 4.5, -90, 0)
	c.Settle()

	c.ChatCommand("summon minecraft:cow 2.5 64 6.5 {NoAI:1b}")
	mob := c.ExpectMobSpawn(protocol.EntityTypeCow)

	c.ChatCommand("tp @e[type=cow] ^ ^ ^1 facing 5 64 0.5")
	data := c.AwaitPacket(protocol.IDPlayTeleportEntity)
	r := protocol.NewReader(data)
	if id := r.ReadVarInt(); id != mob.EntityID {
		t.Fatalf("teleported entity = %d, want %d", id, mob.EntityID)
	}
	r.ReadVarInt() // position path
	r.ReadF64()    // x, y, z
	r.ReadF64()
	r.ReadF64()
	yaw, pitch := r.ReadF32(), r.ReadF32()
	if servertest.Abs64(float64(yaw)-(-153.434948)) > 0.01 || pitch != 0 {
		t.Errorf("the faced local cow faces %v/%v, want about -153.43 and level", yaw, pitch)
	}
	c.Disconnect()
}

// TestFacingAppliesTheAnchorToTheTarget checks that a facing clause's eyes
// anchor is applied to the target's own eye height, not the source's: a mob
// turned towards a point level with it comes out level rather than pitched by
// the runner's eye height. vanilla: Entity.lookAt
func TestFacingAppliesTheAnchorToTheTarget(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.MobTestConfig(), "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("summon minecraft:cow 2.5 64 6.5 {NoAI:1b}")
	mob := c.ExpectMobSpawn(protocol.EntityTypeCow)

	// The runner aims from its eyes, but the aim belongs to the cow: the
	// point is level with the landing, so the cow must be left level.
	c.ChatCommand("execute anchored eyes run tp @e[type=cow] 2.5 64 4.5 facing 2.5 64 100.5")
	data := c.AwaitPacket(protocol.IDPlayTeleportEntity)
	r := protocol.NewReader(data)
	if id := r.ReadVarInt(); id != mob.EntityID {
		t.Fatalf("teleported entity = %d, want %d", id, mob.EntityID)
	}
	r.ReadVarInt() // position path
	r.ReadF64()    // x, y, z
	r.ReadF64()
	r.ReadF64()
	yaw, pitch := r.ReadF32(), r.ReadF32()
	if servertest.Abs64(float64(yaw)) > 0.01 || servertest.Abs64(float64(pitch)) > 0.01 {
		t.Errorf("the anchored eyes facing turned the cow to %v/%v, want 0/0 (level with it)", yaw, pitch)
	}
	c.Disconnect()
}

// TestTeleportRotationStoresPitchClamped checks where a rotation past the
// poles ends up: the packet carries the pitch as the command gave it,
// wrapped, while the body keeps it clamped to straight down or up, the way
// vanilla stores it. It is the body's pitch the local coordinates of the
// teleports that follow measure from, and what a relative rotation adds to.
func TestTeleportRotationStoresPitchClamped(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// /teleport is a gamemaster's command, so the runner is listed as an
	// operator.
	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt() // (0.5, 64, 0.5) in the flat world
	c.SkipJoin()

	// A rotation past both poles: the packet still carries it wrapped, 40
	// and 160.
	c.ChatCommand("tp @s 0.5 100 0.5 400 -200")
	if pos := servertest.ExpectTeleport(t, c); pos.Yaw != 40 || pos.Pitch != 160 {
		t.Errorf("rotated teleport faces %v/%v, want 40 and 160", pos.Yaw, pos.Pitch)
	}

	// The body keeps the pitch clamped to straight down: the local step
	// that follows measures from 90, not 160, and lands a block below
	// where it started, the place a vanilla server leaves the body at.
	c.ChatCommand("tp @s ^ ^ ^1")
	if pos := servertest.ExpectTeleport(t, c); servertest.FormatCoordForTest(pos.X) != "0.500000" ||
		servertest.FormatCoordForTest(pos.Y) != "99.000000" || servertest.FormatCoordForTest(pos.Z) != "0.500000" {
		t.Errorf("local teleport = %v, want (0.5, 99, 0.5)", pos)
	}

	// A relative rotation adds to the clamped pitch the body holds: the
	// packet faces 70 and 100, ten past the pole rather than thirty.
	c.ChatCommand("tp @s ~ ~ ~ ~30 ~10")
	if pos := servertest.ExpectTeleport(t, c); pos.Yaw != 70 || pos.Pitch != 100 {
		t.Errorf("rotated teleport faces %v/%v, want 70 and 100", pos.Yaw, pos.Pitch)
	}

	// And the next step measures from the pitch as it was clamped again.
	c.ChatCommand("tp @s ^ ^ ^1")
	if pos := servertest.ExpectTeleport(t, c); servertest.FormatCoordForTest(pos.X) != "0.500000" ||
		servertest.FormatCoordForTest(pos.Y) != "98.000000" || servertest.FormatCoordForTest(pos.Z) != "0.500000" {
		t.Errorf("local teleport = %v, want (0.5, 98, 0.5)", pos)
	}

	c.Disconnect()
}

// TestKillCommand kills the runner, the whole way to the respawn screen and
// the death message the vanilla server kills with.
func TestKillCommand(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// /kill is a gamemaster's command, so the runner is listed as an
	// operator.
	_, c, entityID := servertest.JoinAs(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	killMessage := protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Testificate")},
	}
	c.ChatCommand("kill")
	servertest.ExpectDeathSequence(t, c, entityID, killMessage)
	c.Disconnect()
}

// TestKillKillsTheInvulnerableModes: /kill's generic_kill is on vanilla's
// bypasses-invulnerability list, so it kills a creative or spectator player
// outright where ordinary damage cannot.
func TestKillKillsTheInvulnerableModes(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"creative", "spectator"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.GameMode = mode

			_, c, entityID := servertest.JoinAs(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

			c.ChatCommand("kill")
			servertest.ExpectDeathSequence(t, c, entityID, protocol.Text{
				Translate: "death.attack.genericKill",
				With:      []protocol.Text{servertest.PlayerDisplayComponent("Testificate")},
			})
			c.Disconnect()
		})
	}
}

// TestASpectatorKeepsItsInventoryAndXpThroughAKill: /kill reaches a
// spectator, but a spectator's death lays no loot down; the respawn carries
// the same inventory and experience over. vanilla: ServerPlayer.die skips
// dropAllDeathLoot for a spectator, restoreFrom keeps its inventory.
func TestASpectatorKeepsItsInventoryAndXpThroughAKill(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "spectator"

	srv, c, _ := servertest.JoinAs(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	given := servertest.TheThingsGiven(t)
	for slot, stack := range given {
		srv.GiveSlot("Testificate", slot, stack)
	}
	srv.SetExperienceLevels("Testificate", 12)
	c.Settle()

	c.ChatCommand("kill")

	// The death is read packet by packet rather than with
	// expectDeathSequence: that helper steps over a Spawn Entity, and the
	// whole point here is that no item entity spawns for a spectator.
	var sawDeath bool
	for id, data := range c.Stream() {
		if id == protocol.IDPlaySpawnEntity {
			r := protocol.NewReader(data)
			r.ReadVarInt() // entity ID
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeItem {
				t.Fatal("a spectator's /kill dropped an item entity")
			}
		}
		if id == protocol.IDPlayCombatDeath {
			sawDeath = true
			break
		}
	}
	if !sawDeath {
		t.Fatal("no combat death arrived for the spectator's /kill")
	}

	c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})

	// The kept inventory and the bar ride out around the Respawn, so both
	// are read from one scan that also watches for a late drop.
	var kept []protocol.ItemStack
	var gotXp servertest.ExperienceReport
	var sawXp, sawRespawn bool
	for id, data := range c.Stream() {
		if id == protocol.IDPlaySpawnEntity {
			r := protocol.NewReader(data)
			r.ReadVarInt() // entity ID
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeItem {
				t.Fatal("a spectator's /kill dropped an item entity")
			}
		}
		switch id {
		case protocol.IDPlayContainerContent:
			kept = servertest.ContainerSlots(t, data)
		case protocol.IDPlaySetExperience:
			r := protocol.NewReader(data)
			gotXp = servertest.ExperienceReport{Progress: r.ReadF32(), Level: r.ReadVarInt(), Total: r.ReadVarInt()}
			sawXp = true
		case protocol.IDPlayRespawn:
			sawRespawn = true
		}
		if sawRespawn && kept != nil && sawXp {
			break
		}
	}
	if !sawRespawn {
		t.Fatal("the Respawn never arrived")
	}
	if kept == nil {
		t.Fatal("the respawn sent no kept inventory")
	}
	servertest.CheckTheThingsGiven(t, kept, given)
	if !sawXp || gotXp.Level != 12 {
		t.Errorf("the respawned spectator's level = %d, want the 12 it died with", gotXp.Level)
	}
	c.Disconnect()
}

// TestKillTargets checks the form that names who dies: the named player is
// killed while the runner is not, and the single-target feedback names them.
func TestKillTargets(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// /kill is a gamemaster's command, so Alice, who runs it, is listed as
	// an operator; Bob is only ever killed.
	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))

	alice := connect().Join("Alice")

	bob := connect()
	bobID := bob.JoinAs("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	alice.ChatCommand("kill Bob")
	servertest.AwaitCommandFeedbackText(t, alice, "commands.kill.success.single", servertest.PlayerDisplayComponent("Bob"))

	// Bob dies the whole death: the empty bar, the damage event, the
	// respawn screen with its message.
	killMessage := protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Bob")},
	}
	servertest.ExpectDeathSequence(t, bob, bobID, killMessage)

	alice.Disconnect()
	bob.Disconnect()
}

// TestSpawnpointMovesTheRespawn checks that a spawn point /spawnpoint set is
// the one the player comes back to, by killing them and respawning them, and
// that the point follows them across sessions in the compound vanilla keeps
// it in, forced the way /spawnpoint sets it.
func TestSpawnpointMovesTheRespawn(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	// /spawnpoint and /kill are a gamemaster's commands, so the runner is
	// listed as an operator.
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Testificate"))
	c := connect()
	entityID := c.JoinAs("Testificate")

	// Stand somewhere that is not the spawn, and make it the respawn.
	c.MoveToGround(12.5, 64, 8.5, true)
	c.Settle()
	c.ChatCommand("spawnpoint")
	// The feedback reads where the spawn point is, which way it faces, and
	// in which dimension, then who it was set for: the block coordinates
	// travel as the numbers they are, the angles as the floats they are,
	// and the player as the display name the vanilla server's own feedback
	// carries.
	cx, cy, cz := int64(12), int64(64), int64(8)
	cyaw, cpitch := float32(0), float32(0)
	servertest.AwaitCommandFeedbackText(t, c, "commands.spawnpoint.success.single",
		protocol.Text{Number: &cx}, protocol.Text{Number: &cy}, protocol.Text{Number: &cz},
		protocol.Text{Float: &cyaw}, protocol.Text{Float: &cpitch},
		protocol.Text{Text: "minecraft:overworld"}, servertest.PlayerDisplayComponent("Testificate"))

	// Die, and come back where the command said rather than at the world
	// spawn.
	killMessage := protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Testificate")},
	}
	c.ChatCommand("kill")
	servertest.ExpectDeathSequence(t, c, entityID, killMessage)
	c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})
	// The /kill feedback rides the mailbox behind the death, so it is
	// stepped over rather than taken as the next packet.
	c.AwaitPacket(protocol.IDPlayRespawn)
	if got := c.ExpectHealth(); got.Health != 20 {
		t.Errorf("health after the respawn = %v, want 20", got.Health)
	}
	// The experience the death dropped comes back as none, the bar reset
	// with the health it followed.
	c.Expect(protocol.IDPlaySetExperience)
	c.Expect(protocol.IDPlayAbilities)
	if pos := servertest.ExpectTeleport(t, c); pos.X != 12.5 || math.Abs(pos.Y-64.1) > 1e-9 || pos.Z != 8.5 {
		t.Errorf("respawn = %v, want the spawn point (12.5, 64.1, 8.5)", pos)
	}

	// On disk the point is the compound the game writes, forced the way
	// /spawnpoint sets it.
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Testificate"))
	want := nbt.Compound{
		"pos":       nbt.IntArray{12, 64, 8},
		"dimension": nbt.String(world.DimensionName),
		"yaw":       nbt.Float(0),
		"pitch":     nbt.Float(0),
		"forced":    nbt.Byte(1),
	}
	respawn, ok := nbt.Get[nbt.Compound](root, "respawn")
	if !ok {
		t.Fatal("the spawnpoint was not saved")
	}
	if !reflect.DeepEqual(respawn, want) {
		t.Errorf("respawn =\n%v\nwant\n%v", respawn, want)
	}

	// And the next connection comes back carrying it, still forced.
	again := connect()
	again.JoinAs("Testificate")
	if spawn, forced, ok := srv.SpawnPointOf("Testificate"); !ok ||
		spawn != (world.SpawnPos{X: 12, Y: 64, Z: 8}) || !forced {
		t.Errorf("the rejoin carried spawn point (%d, %d, %d) forced %v, %v; want (12, 64, 8) forced, true",
			spawn.X, spawn.Y, spawn.Z, forced, ok)
	}
	again.Disconnect()
}

// TestSpawnpointKeepsTheAngles checks that the rotation a /spawnpoint names
// is kept with the point in the file, where vanilla respawns the player
// facing it.
func TestSpawnpointKeepsTheAngles(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	// /spawnpoint is a gamemaster's command, so the runner is listed as an
	// operator.
	_, c, _ := servertest.JoinAs(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("spawnpoint @s 10 -60 10 90 30")
	cx, cy, cz := int64(10), int64(-60), int64(10)
	cyaw, cpitch := float32(90), float32(30)
	servertest.AwaitCommandFeedbackText(t, c, "commands.spawnpoint.success.single",
		protocol.Text{Number: &cx}, protocol.Text{Number: &cy}, protocol.Text{Number: &cz},
		protocol.Text{Float: &cyaw}, protocol.Text{Float: &cpitch},
		protocol.Text{Text: "minecraft:overworld"}, servertest.PlayerDisplayComponent("Testificate"))
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Testificate"))
	want := nbt.Compound{
		"pos":       nbt.IntArray{10, -60, 10},
		"dimension": nbt.String(world.DimensionName),
		"yaw":       nbt.Float(90),
		"pitch":     nbt.Float(30),
		"forced":    nbt.Byte(1),
	}
	respawn, ok := nbt.Get[nbt.Compound](root, "respawn")
	if !ok {
		t.Fatal("the spawnpoint was not saved")
	}
	if !reflect.DeepEqual(respawn, want) {
		t.Errorf("respawn =\n%v\nwant\n%v", respawn, want)
	}
}

// TestSpawnpointInTheNetherNamesTheNether checks that the dimension the
// command ran in is the one the file keeps the point in, where vanilla would
// return the player to.
func TestSpawnpointInTheNetherNamesTheNether(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	// /spawnpoint is a gamemaster's command, so the runner is listed as an
	// operator.
	_, c, _ := servertest.JoinAs(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("execute in minecraft:the_nether run spawnpoint @s 3 70 4")
	cx, cy, cz := int64(3), int64(70), int64(4)
	cyaw, cpitch := float32(0), float32(0)
	servertest.AwaitCommandFeedbackText(t, c, "commands.spawnpoint.success.single",
		protocol.Text{Number: &cx}, protocol.Text{Number: &cy}, protocol.Text{Number: &cz},
		protocol.Text{Float: &cyaw}, protocol.Text{Float: &cpitch},
		protocol.Text{Text: "minecraft:the_nether"}, servertest.PlayerDisplayComponent("Testificate"))
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Testificate"))
	want := nbt.Compound{
		"pos":       nbt.IntArray{3, 70, 4},
		"dimension": nbt.String(world.NetherName),
		"yaw":       nbt.Float(0),
		"pitch":     nbt.Float(0),
		"forced":    nbt.Byte(1),
	}
	respawn, ok := nbt.Get[nbt.Compound](root, "respawn")
	if !ok {
		t.Fatal("the spawnpoint was not saved")
	}
	if !reflect.DeepEqual(respawn, want) {
		t.Errorf("respawn =\n%v\nwant\n%v", respawn, want)
	}
}

// TestSetworldspawnMovesTheWorldSpawn checks both halves of the command: the
// players already in the world are told where the spawn now is, and a player
// joining afterwards arrives there.
func TestSetworldspawnMovesTheWorldSpawn(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// /setworldspawn is a gamemaster's command, so the runner is listed as
	// an operator.
	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Testificate"))

	c := connect().Join("Testificate")

	c.ChatCommand("setworldspawn 5 64 5")
	servertest.AwaitCommandFeedback(t, c, "commands.setworldspawn.success",
		"5", "64", "5", "0.0", "0.0", "minecraft:overworld")

	// Everyone in the world is told, in the packet a compass reads.
	spawn := protocol.NewReader(c.ExpectPlay(protocol.IDPlaySpawnPosition))
	if got := spawn.ReadString(32767); got != "minecraft:overworld" {
		t.Errorf("spawn dimension = %q, want minecraft:overworld", got)
	}
	spawnX, spawnY, spawnZ := spawn.ReadPosition()
	if spawnX != 5 || spawnY != 64 || spawnZ != 5 {
		t.Errorf("world spawn = (%d, %d, %d), want (5, 64, 5)", spawnX, spawnY, spawnZ)
	}

	// A player joining afterwards arrives at the new spawn.
	late := connect()
	late.Login("Late")
	late.Configure()
	if pos := late.SpawnedAt(); pos.X != 5.5 || pos.Y != 64 || pos.Z != 5.5 {
		t.Errorf("late arrival = %v, want the new spawn (5.5, 64, 5.5)", pos)
	}
	late.SkipJoin()

	c.Disconnect()
	late.Disconnect()
}

// TestSetworldspawnStoresTheRotationItIsGiven checks the rotation half of the
// command: the angles given with the position are the ones the world spawn
// keeps, the ones the spawn compound of level.dat carries.
func TestSetworldspawnStoresTheRotationItIsGiven(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("setworldspawn 12 75 12 90 -15")
	servertest.AwaitCommandFeedback(t, c, "commands.setworldspawn.success",
		"12", "75", "12", "90.0", "-15.0", "minecraft:overworld")

	if got := srv.WorldSpawn(); got != (world.SpawnPos{X: 12, Y: 75, Z: 12, Yaw: 90, Pitch: -15}) {
		t.Errorf("world spawn = %+v, want the rotation the command was given", got)
	}

	// A rotation outside the range is wrapped and clamped where the spawn is
	// built, the way RespawnData.of stores it.
	c.ChatCommand("setworldspawn 0 64 0 270 -120")
	servertest.AwaitCommandFeedback(t, c, "commands.setworldspawn.success",
		"0", "64", "0", "-90.0", "-90.0", "minecraft:overworld")

	if got := srv.WorldSpawn(); got != (world.SpawnPos{X: 0, Y: 64, Z: 0, Yaw: -90, Pitch: -90}) {
		t.Errorf("world spawn = %+v, want yaw -90, pitch -90", got)
	}

	// A fractional rotation keeps its point in the feedback: 1.5 is not 15.5.
	c.ChatCommand("setworldspawn 0 64 0 1.5 2.5")
	servertest.AwaitCommandFeedback(t, c, "commands.setworldspawn.success",
		"0", "64", "0", "1.5", "2.5", "minecraft:overworld")

	if got := srv.WorldSpawn(); got != (world.SpawnPos{X: 0, Y: 64, Z: 0, Yaw: 1.5, Pitch: 2.5}) {
		t.Errorf("world spawn = %+v, want yaw 1.5, pitch 2.5", got)
	}

	c.Disconnect()
}

// TestSetworldspawnPacketCarriesTheRotation checks the Set Default Spawn
// Position packet holds the rotation the world spawn keeps, both in the
// broadcast /setworldspawn sends and in what a later join is told.
func TestSetworldspawnPacketCarriesTheRotation(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Testificate"))

	c := connect().Join("Testificate")

	c.ChatCommand("setworldspawn 0 64 0 90 -15")
	servertest.AwaitCommandFeedback(t, c, "commands.setworldspawn.success",
		"0", "64", "0", "90.0", "-15.0", "minecraft:overworld")

	// Everyone in the world is told where the spawn moved, rotation included.
	if x, y, z, yaw, pitch := servertest.ReadSpawnPosition(t, c.ExpectPlay(protocol.IDPlaySpawnPosition)); x != 0 || y != 64 || z != 0 || yaw != 90 || pitch != -15 {
		t.Errorf("broadcast spawn = (%d, %d, %d) yaw %v pitch %v, want (0, 64, 0) yaw 90 pitch -15", x, y, z, yaw, pitch)
	}

	// A player joining afterwards is told the same spawn.
	late := connect()
	late.Login("Late")
	late.Configure()
	data := late.AwaitPacket(protocol.IDPlaySpawnPosition)
	if x, y, z, yaw, pitch := servertest.ReadSpawnPosition(t, data); x != 0 || y != 64 || z != 0 || yaw != 90 || pitch != -15 {
		t.Errorf("join spawn = (%d, %d, %d) yaw %v pitch %v, want (0, 64, 0) yaw 90 pitch -15", x, y, z, yaw, pitch)
	}
	late.SkipJoin()

	// An out-of-range rotation is wrapped and clamped before the packet is
	// built, in the broadcast and in what a later join is told.
	c.ChatCommand("setworldspawn 0 64 0 270 -120")
	servertest.AwaitCommandFeedback(t, c, "commands.setworldspawn.success",
		"0", "64", "0", "-90.0", "-90.0", "minecraft:overworld")
	data = c.AwaitPacket(protocol.IDPlaySpawnPosition)
	if x, y, z, yaw, pitch := servertest.ReadSpawnPosition(t, data); x != 0 || y != 64 || z != 0 || yaw != -90 || pitch != -90 {
		t.Errorf("broadcast spawn = (%d, %d, %d) yaw %v pitch %v, want (0, 64, 0) yaw -90 pitch -90", x, y, z, yaw, pitch)
	}

	later := connect()
	later.Login("Later")
	later.Configure()
	data = later.AwaitPacket(protocol.IDPlaySpawnPosition)
	if x, y, z, yaw, pitch := servertest.ReadSpawnPosition(t, data); x != 0 || y != 64 || z != 0 || yaw != -90 || pitch != -90 {
		t.Errorf("join spawn = (%d, %d, %d) yaw %v pitch %v, want (0, 64, 0) yaw -90 pitch -90", x, y, z, yaw, pitch)
	}
	later.SkipJoin()

	c.Disconnect()
	late.Disconnect()
	later.Disconnect()
}

// TestSpawnCommandsRefusePositionsOutsideTheWorld checks that /setworldspawn
// and /spawnpoint refuse a block position outside the bounds the vanilla
// server reads a spawnable position through — x and z beyond [±30,000,000),
// y beyond [±20,000,000) — with the argument's own message, leave the stored
// spawn where it was, and refuse in every dimension the command runs in.
func TestSpawnCommandsRefusePositionsOutsideTheWorld(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	srv, c, _ := servertest.JoinAs(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	// A world spawn and the runner's own to tell a refusal from a move, in
	// the chunks the join already loaded.
	c.ChatCommand("setworldspawn 4 64 4")
	servertest.AwaitCommandFeedback(t, c, "commands.setworldspawn.success",
		"4", "64", "4", "0.0", "0.0", "minecraft:overworld")
	c.ExpectPlay(protocol.IDPlaySpawnPosition)
	c.ChatCommand("spawnpoint @s 6 64 6")
	cx, cy, cz := int64(6), int64(64), int64(6)
	cyaw, cpitch := float32(0), float32(0)
	servertest.AwaitCommandFeedbackText(t, c, "commands.spawnpoint.success.single",
		protocol.Text{Number: &cx}, protocol.Text{Number: &cy}, protocol.Text{Number: &cz},
		protocol.Text{Float: &cyaw}, protocol.Text{Float: &cpitch},
		protocol.Text{Text: "minecraft:overworld"}, servertest.PlayerDisplayComponent("Testificate"))

	for _, command := range []string{
		"setworldspawn 0 20000000 0",
		"setworldspawn 0 -20000001 0",
		"setworldspawn 30000000 64 0",
		"setworldspawn 0 64 -30000001",
		"spawnpoint @s 0 20000000 0",
		"spawnpoint @s 40000000 64 0",
		"spawnpoint @s 0 64 -40000000",
		"execute in minecraft:the_nether run spawnpoint @s 0 20000000 0",
	} {
		c.ChatCommand(command)
		servertest.ExpectCommandError(t, c, "argument.pos.outofbounds")
	}

	if got := srv.WorldSpawn(); got != (world.SpawnPos{X: 4, Y: 64, Z: 4}) {
		t.Errorf("world spawn = %+v, want the refused commands' (4, 64, 4) unchanged", got)
	}
	if spawn, _, ok := srv.SpawnPointOf("Testificate"); !ok || spawn != (world.SpawnPos{X: 6, Y: 64, Z: 6}) {
		t.Errorf("spawn point = (%d, %d, %d) ok %v, want the refused commands' (6, 64, 6) unchanged",
			spawn.X, spawn.Y, spawn.Z, ok)
	}
	c.Disconnect()
}

// TestClearCommand empties an inventory: the items /give put in come back
// out, the client's window is filled in empty, and the feedback says how
// much went.
func TestClearCommand(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// /give and /clear are a gamemaster's commands, so the runner is
	// listed as an operator.
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	diamond, ok := registry.ItemID("minecraft:diamond")
	if !ok {
		t.Fatal("the registry has no diamond")
	}
	c.ChatCommand("give @s minecraft:diamond 5")
	updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 36 || updates[0].Stack.ItemID != diamond || updates[0].Stack.Count != 5 {
		t.Errorf("give slot update = %+v, want the hotbar holding five diamonds", updates[0])
	}
	five := int64(5)
	servertest.ExpectFeedbackSoon(t, c, chats, "commands.give.success.single",
		protocol.Text{Number: &five},
		protocol.Text{Translate: "chat.square_brackets", Color: "white",
			With:      []protocol.Text{{Extra: []protocol.Text{{Translate: "item.minecraft.diamond"}}}},
			HoverItem: &protocol.HoverItem{ID: "minecraft:diamond", Count: 1}},
		servertest.PlayerDisplayComponent("Testificate"))

	c.ChatCommand("clear")
	// The feedback counts what came out as a number and names who it came
	// from by their display name.
	fiveOut := int64(5)
	servertest.AwaitCommandFeedbackText(t, c, "commands.clear.success.single",
		protocol.Text{Number: &fiveOut}, servertest.PlayerDisplayComponent("Testificate"))

	// The window is filled in empty, which is what the client needs to
	// believe its player carries nothing.
	content := protocol.NewReader(c.AwaitPlay(protocol.IDPlayContainerContent))
	content.ReadVarInt() // window ID
	content.ReadVarInt() // state ID
	slots := content.ReadVarInt()
	for range slots {
		if stack := servertest.MustReadSlot(t, content); !stack.Empty() {
			t.Errorf("cleared inventory slot holds %v, want nothing", stack)
		}
	}

	// A second clear has nothing to take, and says so with the key the
	// vanilla server's ClearCommand refuses with.
	c.ChatCommand("clear")
	servertest.AwaitCommandErrorText(t, c, "clear.failed.single", servertest.PlayerDisplayComponent("Testificate"))
	c.Disconnect()
}

// TestATargetThatMatchesNobodyStopsTheCommand checks a refusal ends the
// command: the not-found message is the whole answer, with no success
// feedback behind it naming nobody, and a teleport to a player who is not
// there leaves the connection standing rather than taking it down.
func TestATargetThatMatchesNobodyStopsTheCommand(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// The commands that walk their targets need a gamemaster's level, and
	// the closing /seed too, so the runner is listed as an operator.
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	for _, cmd := range []string{"kill Nobody", "tp Nobody", "xp add Nobody 5"} {
		c.ChatCommand(cmd)
		var want []byte
		if cmd == "kill Nobody" || cmd == "tp Nobody" {
			want = servertest.CommandErrorMessageForTest("argument.entity.notfound.entity")
		} else {
			want = servertest.CommandErrorMessageForTest("argument.entity.notfound.player")
		}
		if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, want) {
			t.Errorf("/%s answered % X, want the not-found refusal % X", cmd, got, want)
		}
	}

	// The next message is the next command's, not a success the refused
	// ones went on to report.
	c.ChatCommand("seed")
	got := c.ExpectPlay(protocol.IDPlaySystemChat)
	if !bytes.Contains(got, []byte("commands.seed.success")) {
		t.Errorf("after the refusals came % X, want the seed's feedback", got)
	}
	c.Disconnect()
}

// TestClearTakesTheHotbarFirst checks /clear's order: it walks the 36 item
// slots with the hotbar first, then the armour, the off hand and the 2x2
// grid, so a count that stops part way through empties the hotbar before it
// touches anything else.
// vanilla: Inventory.clearOrCountMatchingItems
func TestClearTakesTheHotbarFirst(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	// A stack of ten in each of the four kinds of slot the order walks.
	ten := protocol.ItemStack{ItemID: servertest.MustItem("minecraft:stone"), Count: 10}
	srv.GiveSlot("Testificate", player.HotbarSlot, ten)
	srv.GiveSlot("Testificate", player.MainSlot, ten)
	srv.GiveSlot("Testificate", player.ArmorSlot, ten)
	srv.GiveSlot("Testificate", player.OffHandSlot, ten)

	// Twenty-five stones: the hotbar's ten, the main row's ten and five of
	// the armour's, leaving the off hand untouched.
	c.ChatCommand("clear @s minecraft:stone 25")
	twentyFive := int64(25)
	servertest.AwaitCommandFeedbackText(t, c, "commands.clear.success.single",
		protocol.Text{Number: &twentyFive}, servertest.PlayerDisplayComponent("Testificate"))

	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Empty() {
		t.Errorf("the hotbar slot = %+v, want it emptied first", got)
	}
	if got := srv.InventorySlot("Testificate", player.MainSlot); !got.Empty() {
		t.Errorf("the main slot = %+v, want it emptied second", got)
	}
	if got := srv.InventorySlot("Testificate", player.ArmorSlot); got.Count != 5 {
		t.Errorf("the armour slot = %+v, want five left of ten", got)
	}
	if got := srv.InventorySlot("Testificate", player.OffHandSlot); got.Count != 10 {
		t.Errorf("the off-hand slot = %+v, want the ten untouched", got)
	}
	c.Disconnect()
}

// TestClearTakesTheBootsBeforeTheHelmet checks the armour order /clear walks:
// with the feet and the head each holding five, a count of five takes the
// boots, the slot vanilla's mapping reaches first.
// vanilla: Inventory.clearOrCountMatchingItems, EQUIPMENT_SLOT_MAPPING
func TestClearTakesTheBootsBeforeTheHelmet(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	five := protocol.ItemStack{ItemID: servertest.MustItem("minecraft:stone"), Count: 5}
	helmet := player.ArmorSlot
	boots := player.ArmorSlot + player.ArmorSlots - 1
	srv.GiveSlot("Testificate", helmet, five)
	srv.GiveSlot("Testificate", boots, five)

	c.ChatCommand("clear @s minecraft:stone 5")
	took := int64(5)
	servertest.AwaitCommandFeedbackText(t, c, "commands.clear.success.single",
		protocol.Text{Number: &took}, servertest.PlayerDisplayComponent("Testificate"))

	if got := srv.InventorySlot("Testificate", boots); !got.Empty() {
		t.Errorf("the boots = %+v, want them emptied first", got)
	}
	if got := srv.InventorySlot("Testificate", helmet); got.Count != 5 {
		t.Errorf("the helmet = %+v, want the five untouched", got)
	}
	c.Disconnect()
}

// TestClearCountsWithoutRemoving checks /clear's count-only form: a maxCount
// of zero answers how many matching items the player has and leaves them all.
// vanilla: ClearCommand.clearItems
func TestClearCountsWithoutRemoving(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	srv.GiveSlot("Testificate", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItem("minecraft:stone"), Count: 5})

	c.ChatCommand("clear @s minecraft:stone 0")
	found := int64(5)
	servertest.AwaitCommandFeedbackText(t, c, "commands.clear.test.single",
		protocol.Text{Number: &found}, servertest.PlayerDisplayComponent("Testificate"))

	if got := srv.InventorySlot("Testificate", player.HotbarSlot); got.Count != 5 {
		t.Errorf("the slot = %+v, want the five left", got)
	}
	c.Disconnect()
}

// TestClearTakesTheCursorStack checks /clear's last stop: a stack held on
// the cursor is removed like any other, and the content the client is
// corrected with carries the cursor emptied.
// vanilla: Inventory.clearOrCountMatchingItems
func TestClearTakesTheCursorStack(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	w := servertest.WindowedOf(t, "Testificate", c)

	// Five stone picked from the hotbar onto the cursor: the slot empties
	// and the cursor carries them.
	stone := protocol.ItemStack{ItemID: servertest.MustItem("minecraft:stone"), Count: 5}
	srv.GiveSlot("Testificate", player.HotbarSlot, stone)
	w.Click(int16(player.HotbarSlot), 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{int16(player.HotbarSlot): {}}, stone)

	// The cursor is the last stop, so a clear of the whole five takes it.
	w.ChatCommand("clear @s minecraft:stone 5")
	took := int64(5)
	servertest.AwaitCommandFeedbackText(t, w.Client, "commands.clear.success.single",
		protocol.Text{Number: &took}, servertest.PlayerDisplayComponent("Testificate"))

	content := protocol.NewReader(w.AwaitPlay(protocol.IDPlayContainerContent))
	content.ReadVarInt() // window ID
	content.ReadVarInt() // state ID
	slots := content.ReadVarInt()
	for range slots {
		servertest.MustReadSlot(t, content)
	}
	if carried := servertest.MustReadSlot(t, content); !carried.Empty() {
		t.Errorf("the cursor after /clear = %+v, want it emptied", carried)
	}
	w.Disconnect()
}

// TestSetworldspawnKeepsTheSourceDimension runs /setworldspawn in the
// nether: the feedback names the nether, the world keeps that dimension and
// the Set Default Spawn Position packet carries it.
func TestSetworldspawnKeepsTheSourceDimension(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("execute in minecraft:the_nether run setworldspawn 8 40 8")
	servertest.AwaitCommandFeedback(t, c, "commands.setworldspawn.success",
		"8", "40", "8", "0.0", "0.0", "minecraft:the_nether")

	if got := srv.WorldSpawnDimension(); got != "minecraft:the_nether" {
		t.Errorf("world spawn dimension = %q, want the nether", got)
	}
	data := c.ExpectPlay(protocol.IDPlaySpawnPosition)
	if dim := readSpawnDimension(t, data); dim != "minecraft:the_nether" {
		t.Errorf("broadcast spawn dimension = %q, want the nether", dim)
	}

	c.ChatCommand("setworldspawn 0 64 0")
	servertest.AwaitCommandFeedback(t, c, "commands.setworldspawn.success",
		"0", "64", "0", "0.0", "0.0", "minecraft:overworld")
	if got := srv.WorldSpawnDimension(); got != "minecraft:overworld" {
		t.Errorf("world spawn dimension = %q, want the overworld again", got)
	}

	c.Disconnect()
}

// TestRespawnFollowsTheWorldSpawnDimension checks a player without a spawn
// point of their own comes back in the dimension the world spawn lies in.
func TestRespawnFollowsTheWorldSpawnDimension(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("execute in minecraft:the_nether run setworldspawn 8 40 8")
	servertest.AwaitCommandFeedback(t, c, "commands.setworldspawn.success",
		"8", "40", "8", "0.0", "0.0", "minecraft:the_nether")

	c.ChatCommand("kill")
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayCombatDeath); !ok {
		t.Fatal("no death")
	}
	c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})
	data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn)
	if !ok {
		t.Fatal("no respawn")
	}
	r := protocol.NewReader(data)
	r.ReadVarInt() // dimension type
	if dim := r.ReadString(32767); dim != "minecraft:the_nether" {
		t.Errorf("respawned into %q, want the nether the world spawn lies in", dim)
	}
	c.Disconnect()
}

// readSpawnDimension reads the dimension name a Set Default Spawn Position
// packet starts with.
func readSpawnDimension(t *testing.T, data []byte) string {
	t.Helper()
	r := protocol.NewReader(data)
	dim := r.ReadString(32767)
	if err := r.Err(); err != nil {
		t.Fatalf("spawn position dimension: %v", err)
	}
	return dim
}

// TestWorldSpawnInTheNetherRedirectsEveryReader checks the readers of the
// spawn that used to assume the overworld: the way out of the end, the
// console's dimension and a first join.
func TestWorldSpawnInTheNetherRedirectsEveryReader(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Testificate"))
	c := connect().Join("Testificate")

	if dim := srv.ConsoleSourceDimension(); dim != "minecraft:overworld" {
		t.Fatalf("console dimension = %q before the move, want the overworld", dim)
	}
	c.ChatCommand("execute in minecraft:the_nether run setworldspawn 8 40 8")
	servertest.AwaitCommandFeedback(t, c, "commands.setworldspawn.success",
		"8", "40", "8", "0.0", "0.0", "minecraft:the_nether")

	dim, spot := srv.EndExitDestination()
	if dim != "minecraft:the_nether" || spot.X != 8 || spot.Z != 8 {
		t.Errorf("end exit = %s (%d, %d, %d), want the nether spawn (8, 40, 8)", dim, spot.X, spot.Y, spot.Z)
	}
	if got := srv.ConsoleSourceDimension(); got != "minecraft:the_nether" {
		t.Errorf("console dimension = %q, want the nether the spawn lies in", got)
	}

	other := connect().Join("Newcomer")
	if got := srv.PlayerDimension("Newcomer"); got != "minecraft:the_nether" {
		t.Errorf("a first join started in %q, want the nether", got)
	}
	other.Disconnect()
	c.Disconnect()
}

// TestConsoleSourceReadsSpawnDimensionAndPositionAsOnePair moves the world
// spawn into the nether after the console's source is built: the source
// keeps the overworld spawn it was built at, rather than mixing the old
// dimension with the new position. vanilla: MinecraftServer.createCommandSourceStack
func TestConsoleSourceReadsSpawnDimensionAndPositionAsOnePair(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, config.Default())

	before := srv.WorldSpawn()
	dim, pos := srv.ConsoleSourceAcrossSpawnMove(world.NetherName, world.SpawnPos{X: 8, Y: 40, Z: 8})
	want := player.Position{X: float64(before.X), Y: float64(before.Y), Z: float64(before.Z)}
	if dim != "minecraft:overworld" || pos != want {
		t.Errorf("console source = %s %+v, want the overworld spawn it was built at %+v", dim, pos, want)
	}
	if got := srv.ConsoleSourceDimension(); got != "minecraft:the_nether" {
		t.Errorf("a new console source runs in %q, want the nether the spawn moved to", got)
	}
}
