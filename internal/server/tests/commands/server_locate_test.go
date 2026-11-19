package commands

import (
	"bytes"
	"math"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func locateCoordinates(x int64, y protocol.Text, z int64, yWord string) protocol.Text {
	return protocol.Text{
		Translate: "chat.square_brackets",
		With: []protocol.Text{{
			Translate: "chat.coordinates",
			With:      []protocol.Text{{Number: &x}, y, {Number: &z}},
		}},
		Color:        protocol.ColorGreen,
		ClickCommand: "/tp @s " + strconv.FormatInt(x, 10) + " " + yWord + " " + strconv.FormatInt(z, 10),
		HoverText:    &protocol.Text{Translate: "chat.coordinates.tooltip"},
	}
}

// expectLocateAnswer holds the loop and reads the answer a locate command
// sends directly, with no tick run.
func expectLocateAnswer(t *testing.T, srv *server.Server, c *servertest.Client, want []byte) {
	t.Helper()
	if !c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		return id == protocol.IDPlaySystemChat && bytes.Equal(data, want)
	}) {
		t.Fatalf("no system chat % X arrived", want)
	}
}

func locateFeedback(key string, with ...protocol.Text) []byte {
	return servertest.SystemChatPayload(key, false, with...)
}

func locateError(key string, with ...protocol.Text) []byte {
	var b protocol.Buffer
	b.PutText(protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{{Translate: key, With: with}}})
	b.PutBool(false)
	return b.Bytes()
}

func num(v int64) protocol.Text { return protocol.Text{Number: &v} }

// TestLocate runs the /locate checks on the flat world against one server: an
// op "Locator" and a player below level 2. The order matters: the overworld
// checks run before any point of interest is placed, and the crossings into
// the nether and the End come last, each into a dimension the player is not
// in already.
func TestLocate(t *testing.T) {
	t.Parallel()
	servertest.LimitGeneration(t)
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Locator", servertest.OpsListing(t, "Locator"))
	stranger := servertest.DialServer(t, srv).Join("Stranger")
	srv.StopTick()
	c.Settle()

	// The command is unknown below level 2.
	stranger.Run(t, "RefusedWithoutPermission", func(t *testing.T) {
		const command = "locate biome minecraft:plains"
		stranger.ChatCommand(command)
		stranger.ExpectCommandError(
			servertest.CommandErrorMessageForTest("command.unknown.command"),
			servertest.CommandErrorContextForTest(command, 0))
	})

	// The flat world is plains everywhere, so the first sampled column is the
	// runner's own and the distance is zero, at the runner's height clamped
	// into the world.
	// vanilla: BiomeSource.findClosestBiome3d
	c.Run(t, "BiomeFindsTheStandingColumn", func(t *testing.T) {
		pos := srv.PlayerPosition("Locator")
		x, y, z := int64(math.Floor(pos.X)), int64(math.Floor(pos.Y)), int64(math.Floor(pos.Z))

		c.ChatCommand("locate biome minecraft:plains")
		servertest.AwaitCommandFeedbackText(t, c, "commands.locate.biome.success",
			protocol.Text{Text: "minecraft:plains"}, locateCoordinates(x, num(y), z, strconv.FormatInt(y, 10)), num(0))

		// A tag names the member found beside it.
		c.ChatCommand("locate biome #minecraft:is_overworld")
		servertest.AwaitCommandFeedbackText(t, c, "commands.locate.biome.success",
			protocol.Text{Text: "#minecraft:is_overworld (minecraft:plains)"},
			locateCoordinates(x, num(y), z, strconv.FormatInt(y, 10)), num(0))
	})

	// A biome the dimension's generator cannot make is not found, for an id
	// and for a tag, and an unknown id is a parse error.
	c.Run(t, "BiomeNotFound", func(t *testing.T) {
		c.ChatCommand("locate biome minecraft:desert")
		servertest.AwaitCommandErrorText(t, c, "commands.locate.biome.not_found", protocol.Text{Text: "minecraft:desert"})

		c.ChatCommand("locate biome #minecraft:is_ocean")
		servertest.AwaitCommandErrorText(t, c, "commands.locate.biome.not_found", protocol.Text{Text: "#minecraft:is_ocean"})

		const bad = "locate biome minecraft:nonexistent"
		c.ChatCommand(bad)
		c.ExpectCommandError(
			servertest.CommandErrorMessageForTest("argument.resource.not_found", "minecraft:nonexistent", "minecraft:worldgen/biome"),
			servertest.CommandErrorContextForTest(bad, len(bad)))
	})

	// No point of interest is tracked yet, so none is found, and a name
	// outside the registry is a parse error.
	c.Run(t, "Poi", func(t *testing.T) {
		c.ChatCommand("locate poi minecraft:home")
		servertest.AwaitCommandErrorText(t, c, "commands.locate.poi.not_found", protocol.Text{Text: "minecraft:home"})

		const bad = "locate poi minecraft:nonexistent"
		c.ChatCommand(bad)
		c.ExpectCommandError(
			servertest.CommandErrorMessageForTest("argument.resource.not_found", "minecraft:nonexistent", "minecraft:point_of_interest_type"),
			servertest.CommandErrorContextForTest(bad, len(bad)))
	})

	// A portal block within 256 blocks is a point of interest and the nearest
	// of two is reported.
	c.Run(t, "PoiFindsANetherPortal", func(t *testing.T) {
		servertest.HoldTicks(t, srv)
		pos := srv.PlayerPosition("Locator")
		x, y, z := int32(math.Floor(pos.X)), int32(math.Floor(pos.Y)), int32(math.Floor(pos.Z))
		portal := registry.MustState("minecraft:nether_portal", map[string]string{"axis": "x"})

		srv.SetBlock(x+200, y, z+100, portal)
		srv.SetBlock(x+3, y+2, z-5, portal)
		c.ChatCommand("locate poi minecraft:nether_portal")
		expectLocateAnswer(t, srv, c, locateFeedback("commands.locate.poi.success",
			protocol.Text{Text: "minecraft:nether_portal"}, locateCoordinates(int64(x+3), protocol.Text{Text: "~"}, int64(z-5), "~"), num(5)))
	})

	// A bell and a lodestone are found by type, the bell by the village tag
	// with the member in the name, and an absent type is not found. The
	// portals placed above are in no village tag.
	c.Run(t, "PoiTypesAndTags", func(t *testing.T) {
		servertest.HoldTicks(t, srv)
		pos := srv.PlayerPosition("Locator")
		x, y, z := int32(math.Floor(pos.X)), int32(math.Floor(pos.Y)), int32(math.Floor(pos.Z))

		srv.SetBlock(x+4, y+1, z+3, registry.MustState("minecraft:bell", map[string]string{"attachment": "floor", "facing": "north", "powered": "false"}))
		srv.SetBlock(x-6, y+1, z+8, registry.MustState("minecraft:lodestone", nil))

		c.ChatCommand("locate poi minecraft:meeting")
		expectLocateAnswer(t, srv, c, locateFeedback("commands.locate.poi.success",
			protocol.Text{Text: "minecraft:meeting"}, locateCoordinates(int64(x+4), protocol.Text{Text: "~"}, int64(z+3), "~"), num(5)))
		c.ChatCommand("locate poi minecraft:lodestone")
		expectLocateAnswer(t, srv, c, locateFeedback("commands.locate.poi.success",
			protocol.Text{Text: "minecraft:lodestone"}, locateCoordinates(int64(x-6), protocol.Text{Text: "~"}, int64(z+8), "~"), num(10)))
		c.ChatCommand("locate poi #minecraft:village")
		expectLocateAnswer(t, srv, c, locateFeedback("commands.locate.poi.success",
			protocol.Text{Text: "#minecraft:village (minecraft:meeting)"}, locateCoordinates(int64(x+4), protocol.Text{Text: "~"}, int64(z+3), "~"), num(5)))
		c.ChatCommand("locate poi minecraft:armorer")
		expectLocateAnswer(t, srv, c, locateError("commands.locate.poi.not_found", protocol.Text{Text: "minecraft:armorer"}))
	})

	// The nether and the End answer with their own biomes, and the
	// overworld's are not found there. Leaves the player in the End.
	c.Run(t, "BiomeInOtherDimensions", func(t *testing.T) {
		servertest.CarryIntoNether(t, srv, c, "Locator", 5, 5)
		c.ChatCommand("locate biome minecraft:plains")
		servertest.AwaitCommandErrorText(t, c, "commands.locate.biome.not_found", protocol.Text{Text: "minecraft:plains"})
		c.ChatCommand("locate biome #minecraft:is_nether")
		servertest.AwaitSystemChatSaying(t, c, "commands.locate.biome.success")

		moveToTheEnd(t, srv, c)
		// Within 64 chunks of the origin the End is all the_end, so the first
		// sampled column is the runner's own.
		pos := srv.PlayerPosition("Locator")
		x, y, z := int64(math.Floor(pos.X)), int64(math.Floor(pos.Y)), int64(math.Floor(pos.Z))
		c.ChatCommand("locate biome minecraft:the_end")
		servertest.AwaitCommandFeedbackText(t, c, "commands.locate.biome.success",
			protocol.Text{Text: "minecraft:the_end"}, locateCoordinates(x, num(y), z, strconv.FormatInt(y, 10)), num(0))
		c.ChatCommand("locate biome minecraft:nether_wastes")
		servertest.AwaitCommandErrorText(t, c, "commands.locate.biome.not_found", protocol.Text{Text: "minecraft:nether_wastes"})
	})

	// The nether's generator places the fortress and the bastion remnant; the
	// overworld stronghold is not there.
	c.Run(t, "FortressInTheNether", func(t *testing.T) {
		servertest.CarryIntoNether(t, srv, c, "Locator", 5, 5)

		c.ChatCommand("locate structure minecraft:fortress")
		servertest.AwaitSystemChatSaying(t, c, "commands.locate.structure.success")
		c.ChatCommand("locate structure minecraft:bastion_remnant")
		servertest.AwaitSystemChatSaying(t, c, "commands.locate.structure.success")
		c.ChatCommand("locate structure minecraft:ruined_portal_nether")
		servertest.AwaitSystemChatSaying(t, c, "commands.locate.structure.success")
		c.ChatCommand("locate structure #minecraft:ruined_portal")
		servertest.AwaitSystemChatSaying(t, c, "commands.locate.structure.success")
		c.ChatCommand("locate structure minecraft:nether_fossil")
		servertest.AwaitSystemChatSaying(t, c, "commands.locate.structure.success")
		c.ChatCommand("locate structure minecraft:stronghold")
		servertest.AwaitCommandErrorText(t, c, "commands.locate.structure.not_found", protocol.Text{Text: "minecraft:stronghold"})
	})

	c.Run(t, "EndCityInTheEnd", func(t *testing.T) {
		moveToTheEnd(t, srv, c)
		c.ChatCommand("locate structure minecraft:end_city")
		servertest.AwaitSystemChatSaying(t, c, "commands.locate.structure.success")
		c.ChatCommand("locate structure minecraft:fortress")
		servertest.AwaitCommandErrorText(t, c, "commands.locate.structure.not_found", protocol.Text{Text: "minecraft:fortress"})
	})
}

// moveToTheEnd moves the Locator from another dimension into the End and
// waits for the crossing to arrive.
func moveToTheEnd(t *testing.T, srv *server.Server, c *servertest.Client) {
	t.Helper()
	srv.MovePlayerToDimension("Locator", world.EndName, 5, 60, 5)
	if !c.Pump(servertest.JoinTimeout, nil, func(int32, []byte) bool {
		return srv.PlayerDimension("Locator") == world.EndName && srv.ChunkLoadedInDimension(world.EndName, 5, 5)
	}) {
		t.Fatal("never arrived in the End")
	}
}

// TestLocateStronghold stands an op on the overworld of seed 12345 and checks
// /locate structure against the nearest stronghold the generator places.
// vanilla: LocateCommand.locateStructure, LocateCommand.showLocateResult
func TestLocateStronghold(t *testing.T) {
	t.Parallel()
	servertest.LimitGeneration(t)
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	// The unprefixed "normal": the harness swaps the default spelling for its flat ground.
	cfg.LevelType = "normal"
	cfg.LevelSeed = "12345"
	srv, c := servertest.Join(t, cfg, "Locator", servertest.OpsListing(t, "Locator"))
	srv.StopTick()
	pos := srv.PlayerPosition("Locator")
	px, py, pz := int32(math.Floor(pos.X)), int32(math.Floor(pos.Y)), int32(math.Floor(pos.Z))
	stx, stz, _, ok := world.NewOverworldGenerator(12345).NearestStructure([]string{"minecraft:stronghold"}, px, py, pz, 100)
	if !ok {
		t.Fatal("the generator places no stronghold")
	}
	tx, tz := int64(stx), int64(stz)
	dx, dz := float64(tx-int64(px)), float64(tz-int64(pz))
	dist := int64(math.Floor(math.Sqrt(dx*dx + dz*dz)))

	// A stronghold is where the overworld generator puts it, shown without a
	// height; a structure nothing generates is not found, and a name outside
	// the registry is invalid.
	c.Run(t, "Structure", func(t *testing.T) {
		c.ChatCommand("locate structure minecraft:stronghold")
		servertest.AwaitCommandFeedbackText(t, c, "commands.locate.structure.success",
			protocol.Text{Text: "minecraft:stronghold"}, locateCoordinates(tx, protocol.Text{Text: "~"}, tz, "~"), num(dist))

		c.ChatCommand("locate structure #minecraft:eye_of_ender_located")
		servertest.AwaitCommandFeedbackText(t, c, "commands.locate.structure.success",
			protocol.Text{Text: "#minecraft:eye_of_ender_located (minecraft:stronghold)"},
			locateCoordinates(tx, protocol.Text{Text: "~"}, tz, "~"), num(dist))

		c.ChatCommand("locate structure minecraft:village_plains")
		servertest.AwaitSystemChatSaying(t, c, "commands.locate.structure.success")

		c.ChatCommand("locate structure minecraft:fortress")
		servertest.AwaitCommandErrorText(t, c, "commands.locate.structure.not_found", protocol.Text{Text: "minecraft:fortress"})

		c.ChatCommand("locate structure minecraft:nonexistent")
		servertest.AwaitCommandErrorText(t, c, "commands.locate.structure.invalid", protocol.Text{Text: "minecraft:nonexistent"})

		c.ChatCommand("locate structure #minecraft:nonexistent")
		servertest.AwaitCommandErrorText(t, c, "commands.locate.structure.invalid", protocol.Text{Text: "#minecraft:nonexistent"})
	})

	// /execute store result reads the distance /locate printed.
	c.Run(t, "StoresTheDistance", func(t *testing.T) {
		c.ChatCommand(`bossbar add test:bar "The Test Bar"`)
		c.ChatCommand("execute store result bossbar test:bar value run locate structure minecraft:stronghold")
		c.ChatCommand("bossbar get test:bar value")
		servertest.AwaitCommandFeedbackText(t, c, "commands.bossbar.get.value", testBarDisplay, num(dist))
	})
}
