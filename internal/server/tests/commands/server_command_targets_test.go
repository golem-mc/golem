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

// TestTeleportToTeamedPlayerNamesTeamFormattedDestination checks that the
// entity-destination feedback, single and multiple, carries the destination's
// team-formatted name.
func TestTeleportToTeamedPlayerNamesTeamFormattedDestination(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Mover"))

	mover := connect().Join("Mover")
	servertest.HoldTicks(t, srv)

	// run sends a command and reads, with no tick run, the feedback it names.
	run := func(command, key string, with ...protocol.Text) {
		t.Helper()
		var want protocol.Buffer
		want.PutText(protocol.Text{Translate: key, With: with})
		want.PutBool(false)
		mover.ChatCommand(command)
		if !mover.ReadTicks(srv, 0, func(id int32, data []byte) bool {
			return id == protocol.IDPlaySystemChat && bytes.Equal(data, want.Bytes())
		}) {
			t.Fatalf("%q: no %s feedback", command, key)
		}
	}
	setup := func(command string) {
		t.Helper()
		mover.ChatCommand(command)
		mover.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
	}
	for _, line := range []string{"team add red", "team modify red color red", "team join red Mover", "tag @s add t"} {
		setup(line)
	}
	setup("summon armor_stand 0 64 0 {Tags:[\"t\"]}")

	dest := servertest.TeamPlayerDisplayComponent("Mover", "", "red")
	run("tp @s", "commands.teleport.success.entity.single", dest, dest)
	two := int64(2)
	run("tp @e[tag=t] @s", "commands.teleport.success.entity.multiple", protocol.Text{Number: &two}, dest)
	mover.Disconnect()
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
