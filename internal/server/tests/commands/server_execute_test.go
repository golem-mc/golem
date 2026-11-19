package commands

import (
	"bytes"
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// boxCreeper plants a creeper in a shell of stone whose walls keep its fuse
// cold: a body that cannot reach the runner never lights, and the test reads
// its count in peace.
func boxCreeper(t *testing.T, srv *server.Server, x, z float64) {
	t.Helper()
	cx, cz := int32(x), int32(z)
	stone := srv.BlockAt(0, 63, 0)
	for y := int32(64); y <= 66; y++ {
		for dx := int32(-1); dx <= 1; dx++ {
			for dz := int32(-1); dz <= 1; dz++ {
				if dx == 0 && dz == 0 && y <= 65 {
					continue // the cell the body stands in stays air
				}
				srv.SetLoadedBlock(cx+dx, y, cz+dz, stone)
			}
		}
	}
	srv.SpawnMob("minecraft:creeper", x, 64, z)
}

// waitMob holds the test until the mob book has committed the body the
// matcher names: a summon's creation lands on the server's own pass, which
// can still be a breath behind the command's answer.
func waitMob(t *testing.T, srv *server.Server, match func(m server.TestMob) bool) {
	t.Helper()
	servertest.UntilTicks(t, srv, 100, fmt.Sprintf("the body never stood: %v", srv.Mobs()),
		func() bool { return slices.ContainsFunc(srv.Mobs(), match) })
}

// waitMobGone holds the test until the herd no longer holds the body the
// matcher names: a kill's death leaves the book on the tick after the hurt,
// which can still be a breath behind the command's own answer.
func waitMobGone(t *testing.T, srv *server.Server, match func(m server.TestMob) bool) {
	t.Helper()
	servertest.UntilTicks(t, srv, 100, "the body never left",
		func() bool { return !slices.ContainsFunc(srv.Mobs(), match) })
}

// sayPayloadForTest builds the Disguised Chat payload a /say tail sends: the
// message, the say_command chat type and who the server says said it.
func sayPayloadForTest(sender any, message string) []byte {
	var b protocol.Buffer
	b.PutText(protocol.Text{Text: message})
	b.PutVarInt(registry.MustID("minecraft:chat_type", "minecraft:say_command") + 1)
	b.PutText(servertest.ChatSenderComponent(sender))
	b.PutBool(false) // no target, which only a direct message has
	return b.Bytes()
}

// conditionalPassCountForTest builds the feedback a numeric conditional's
// pass sends, its count the number component it travels as.
func conditionalPassCountForTest(count int64) []byte {
	var b protocol.Buffer
	b.PutText(protocol.Text{
		Translate: "commands.execute.conditional.pass_count",
		With:      []protocol.Text{{Number: &count}},
	})
	b.PutBool(false)
	return b.Bytes()
}

// conditionalFailCountForTest builds the refusal a numeric conditional's
// failure throws, its count beside it.
func conditionalFailCountForTest(count int64) []byte {
	var b protocol.Buffer
	b.PutText(protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{{
		Translate: "commands.execute.conditional.fail_count",
		With:      []protocol.Text{{Number: &count}},
	}}})
	b.PutBool(false)
	return b.Bytes()
}

// plainFeedbackForTest builds the one-line feedback a bare translation key
// sends, the way the generic conditional's pass does.
// plainFeedbackTextsForTest builds the expected first System Chat payload for
// a command's own success feedback, whose arguments are the plain texts the
// vanilla server formats the set and filled coordinates as.
func plainFeedbackTextsForTest(key string, with ...string) []byte {
	var b protocol.Buffer
	b.PutText(protocol.Text{Translate: key, With: feedbackArgsForTest(key, with)})
	b.PutBool(false)
	return b.Bytes()
}

// commandErrorNumbersForTest builds the expected first System Chat payload for
// an execution refusal whose arguments are the numbers they travel as.
func commandErrorNumbersForTest(key string, numbers ...int64) []byte {
	var b protocol.Buffer
	inner := protocol.Text{Translate: key}
	for _, n := range numbers {
		inner.With = append(inner.With, protocol.Text{Number: &n})
	}
	b.PutText(protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{inner}})
	b.PutBool(false)
	return b.Bytes()
}

// expectNoSystemChat fails the test when a system chat arrives before the
// ping the helper sends answers: a fork whose every context dropped must
// leave the runner nothing to read, and this is how the test hears that.
func expectNoSystemChat(t *testing.T, c *servertest.Client) {
	t.Helper()
	c.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(1) })
	for id, data := range c.Stream() {
		t.Helper()
		switch id {
		case protocol.IDPlayPingResponse:
			return
		case protocol.IDPlaySystemChat:
			t.Fatalf("a system chat arrived where silence was wanted: % X", data)
		}
	}
	t.Fatal("the ping was never answered")
}

// TestExecuteRunsTheTailPerBody checks the as fork: one tail per body it
// names, each reading the world and its own relative arguments from where
// that body stands once an at has carried the chain there — and from where
// the runner stands when no at has, which is the difference between the two
// forks the vanilla chain draws.
func TestExecuteRunsTheTailPerBody(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")

	first := srv.SpawnMob("minecraft:cow", 4.5, 64, 4.5)
	second := srv.SpawnMob("minecraft:cow", 8.5, 64, 8.5)
	srv.SetMobNoAI(first)
	srv.SetMobNoAI(second)
	c.Settle()

	// One say per body the as named, each spoken as that cow.
	c.ChatCommand("execute as @e[type=minecraft:cow] run say hi")
	cowText := func(id int32) protocol.Text {
		raw, _ := srv.MobUUID(id)
		return servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", uuid.MustParse(raw))
	}
	wantSays := [][]byte{sayPayloadForTest(cowText(first), "hi"), sayPayloadForTest(cowText(second), "hi")}
	for range 2 {
		got := c.ExpectPlay(protocol.IDPlayDisguisedChat)
		i := slices.IndexFunc(wantSays, func(want []byte) bool { return bytes.Equal(got, want) })
		if i < 0 {
			t.Errorf("the as fork said % X, want one say per body as that cow", got)
			continue
		}
		wantSays = slices.Delete(wantSays, i, i+1)
	}
	expectNoSystemChat(t, c)

	// With an at, a relative teleport moves each cow from where that cow
	// stands: four blocks on from their own two places.
	c.ChatCommand("execute as @e[type=minecraft:cow] at @s run tp @s ~2 ~ ~")
	c.Settle()
	for _, id := range []int32{first, second} {
		x, _, z, ok := srv.MobPos(id)
		if !ok {
			t.Fatalf("the cow the fork carried vanished")
		}
		wantX, wantZ := 6.5, 4.5
		if id == second {
			wantX, wantZ = 10.5, 8.5
		}
		if x < wantX-0.5 || x > wantX+0.5 || z < wantZ-0.5 || z > wantZ+0.5 {
			t.Errorf("cow %d stood at (%f, %f), want near (%f, %f)", id, x, z, wantX, wantZ)
		}
	}

	// Without the at, the relative coordinates read from where the
	// runner stands — both cows land by the runner, two blocks on.
	c.ChatCommand("execute as @e[type=minecraft:cow] run tp @s ~2 ~ ~")
	c.Settle()
	for _, id := range []int32{first, second} {
		x, _, z, ok := srv.MobPos(id)
		if !ok {
			t.Fatalf("the cow the fork carried vanished")
		}
		if x < 2.0 || x > 3.0 || z < 0.4 || z > 0.6 {
			t.Errorf("cow %d stood at (%f, %f), want by the runner at (2.5, 0.5)", id, x, z)
		}
	}
	c.Disconnect()
}

// TestExecuteAtReadsTheWorldWhereTheBodyStands checks the at fork against the
// ground: a setblock under the runner's own feet lands in the runner's cell,
// and one under a body's feet lands in the body's.
func TestExecuteAtReadsTheWorldWhereTheBodyStands(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")

	// The loop is stopped so the creeper stands still on its cell: a slow
	// machine would otherwise let it walk off before the at reads it.
	srv.StopTick()
	srv.SpawnMob("minecraft:creeper", 4.5, 64, 8.5)
	c.Settle()

	// The runner's own cell, two blocks above the ground.
	c.ChatCommand("execute run setblock ~ ~ ~ minecraft:stone")
	c.Settle()
	if got := srv.BlockNameAt(0, 64, 0); got != "minecraft:stone" {
		t.Errorf("the block at the runner's feet is %q, want the stone the tail set", got)
	}

	// The creeper's cell, wherever the at carried the chain to.
	c.ChatCommand("execute at @e[type=minecraft:creeper] run setblock ~ ~ ~ minecraft:stone")
	c.Settle()
	if got := srv.BlockNameAt(4, 64, 8); got != "minecraft:stone" {
		t.Errorf("the block under the creeper is %q, want the stone the tail set", got)
	}
	if got := srv.BlockNameAt(0, 64, 0); got != "minecraft:stone" {
		t.Errorf("the block at the runner's feet is %q, want the stone still there", got)
	}
	c.Disconnect()
}

// TestExecutePositionedAndAlignSettleThePlace checks the two modifiers that
// move the chain's reading place alone: a positioned carries the whole
// number the centre correction adds, an align floors the axes it names, and
// a relative argument after them reads from where they left the place.
func TestExecutePositionedAndAlignSettleThePlace(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")

	// The centre correction: a whole number absolute coordinate names the
	// block's centre, so the cow summoned at it stands at three and a
	// half, not at three.
	c.ChatCommand("execute positioned 3 64 5 run summon minecraft:cow ~ ~ ~")
	c.Settle()
	waitMob(t, srv, func(m server.TestMob) bool {
		return m.Kind == "minecraft:cow" && m.X > 3.4 && m.X < 3.6 && m.Z > 5.4 && m.Z < 5.6
	})

	// The align: the chain reads from the corner of the block the place
	// stands in, so the cow summoned afterwards stands on the corner. The
	// place is moved three blocks off the runner first: a corner of the
	// runner's own block would stand the cow inside the runner, and the
	// push would shove it off the corner before it is read.
	c.ChatCommand("execute positioned ~3 ~ ~3 align xz run summon minecraft:cow ~ ~ ~")
	c.Settle()
	waitMob(t, srv, func(m server.TestMob) bool {
		return m.Kind == "minecraft:cow" && m.X > 2.9 && m.X < 3.1 && m.Z > 2.9 && m.Z < 3.1
	})
	c.Disconnect()
}

func TestExecutePositionedAsPreservesRotation(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")
	servertest.HoldTicks(t, srv)
	c.MoveToGrounded(12.5, 64, 8.5, 0, 0, true)
	c.Settle()

	c.ChatCommand("execute rotated 90 0 positioned as @s run tp @s ^ ^ ^5")
	if pos := servertest.ExpectTeleport(t, c); pos.X != 7.5 || pos.Y != 64 || pos.Z != 8.5 {
		t.Errorf("teleport = %v, want (7.5, 64, 8.5)", pos)
	}
	servertest.AwaitCommandFeedbackText(t, c, "commands.teleport.success.location.single",
		servertest.PlayerDisplayComponent("Testificate"),
		protocol.Text{Text: "7.500000"}, protocol.Text{Text: "64.000000"}, protocol.Text{Text: "8.500000"})
	c.Disconnect()
}

// TestExecuteInCarriesTheChainToTheNether checks the dimension step: the
// chain's business happens in the world named, at the position the
// dimension's own scale stretches the runner's into.
func TestExecuteInCarriesTheChainToTheNether(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.NetherTestConfig(t, servertest.SurvivalTestConfig()), "Testificate", servertest.OpsListing(t, "Testificate"))

	// The runner stands at the spawn's half block, which the nether's
	// eighth scale carries to a sixteenth block over; the six tenths of
	// a block the setblock adds lands that in the first block, where an
	// unscaled chain would have put it in the second. A mob would prove
	// the same, but a nether body stands in a chunk nobody holds, and
	// the sweep that collects unheld chunks' mobs takes it before any
	// reading could reach it — the block stays.
	c.ChatCommand("execute in minecraft:the_nether run setblock ~0.6 ~ ~ minecraft:stone")
	// The setblock's own word of success is what the test waits on: the
	// command lands on the server's read pass, which can still be a
	// breath behind the chat packet the client sent.
	if got := c.ExpectWithinTicks(srv, 200, protocol.IDPlaySystemChat); !bytes.Equal(got, plainFeedbackTextsForTest("commands.setblock.success", "0", "64", "0")) {
		t.Fatalf("the nether setblock answered % X, want the set word", got)
	}
	name := func(dim string, x, y, z int32) string {
		n, _, _ := registry.BlockStateName(srv.LoadBlockInDimension(dim, x, y, z))
		return n
	}
	if got := name("minecraft:the_nether", 0, 64, 0); got != "minecraft:stone" {
		t.Errorf("the nether holds %q at (0, 64, 0), want the stone the scaled chain set", got)
	}
	if got := name("minecraft:the_nether", 1, 64, 0); got == "minecraft:stone" {
		t.Errorf("the nether holds stone at (1, 64, 0) too, want it only where the scaled chain put it")
	}
	if got := name("minecraft:overworld", 0, 64, 0); got == "minecraft:stone" {
		t.Errorf("the overworld holds stone at (0, 64, 0), want none — the setblock went where the chain said")
	}
	c.Disconnect()
}

// expectLivingRespawnIntoNether reads up to the next Respawn and checks it
// swaps the client into the nether keeping all of its data, the flag a
// living crossing carries.
func expectLivingRespawnIntoNether(t *testing.T, c *servertest.Client) {
	t.Helper()
	data := c.AwaitPacket(protocol.IDPlayRespawn)
	r := protocol.NewReader(data)
	if got := r.ReadVarInt(); got != registry.MustID("minecraft:dimension_type", world.NetherType) {
		t.Errorf("respawn dimension type = %d, want the nether's", got)
	}
	if got := r.ReadString(32767); got != world.NetherName {
		t.Errorf("respawn dimension = %q, want %q", got, world.NetherName)
	}
	r.ReadI64()    // hashed seed
	r.ReadVarInt() // game mode
	r.ReadVarInt() // previous game mode
	r.ReadBool()   // debug world
	// The flag is the world's, the overworld's answer carried across the
	// crossing, not the dimension arrived at's own.
	if !r.ReadBool() {
		t.Error("the nether respawn does not flag its superflat world")
	}
	r.ReadBool()   // death location
	r.ReadVarInt() // portal cooldown
	r.ReadVarInt() // sea level
	if got := r.ReadU8(); got != 0x03 {
		t.Errorf("respawn data kept = %#02x, want 0x03 (a living crossing keeps everything)", got)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("respawn: %v", err)
	}
}

// netherPlatform lays a 3x3 stone floor under (x, y, z) in the nether with
// two blocks of air over it, so a body teleported there stands still
// rather than falling into whatever the generator put below.
func netherPlatform(t *testing.T, srv *server.Server, x, y, z int32) {
	t.Helper()
	stone, air := servertest.BlockState(t, "minecraft:stone"), servertest.BlockState(t, "minecraft:air")
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			for dy, state := range []uint16{stone, air, air} {
				if !srv.SetBlockInDimension(world.NetherName, x+dx, y-1+int32(dy), z+dz, state) {
					t.Fatalf("the nether chunk at (%d, %d) is not loaded", x+dx, z+dz)
				}
			}
		}
	}
}

// TestExecuteInTakesADimensionWithoutItsNamespace checks that in and if
// dimension read a dimension without its minecraft: namespace as the same
// dimension, the way a resource location argument does.
func TestExecuteInTakesADimensionWithoutItsNamespace(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")
	name := func(x, y, z int32) string {
		n, _, _ := registry.BlockStateName(srv.LoadBlockInDimension(world.NetherName, x, y, z))
		return n
	}

	c.ChatCommand("execute in the_nether run setblock ~ ~ ~ minecraft:stone")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, plainFeedbackTextsForTest("commands.setblock.success", "0", "64", "0")) {
		t.Fatalf("the nether setblock answered % X, want the set word", got)
	}
	if got := name(0, 64, 0); got != "minecraft:stone" {
		t.Errorf("the nether holds %q at (0, 64, 0), want the stone the chain set", got)
	}

	// The conditional answers no packet for itself, so the block is what
	// is waited on, on the server's clock rather than the wall's.
	c.ChatCommand("execute in minecraft:the_nether if dimension the_nether run setblock ~ ~1 ~ minecraft:dirt")
	servertest.WaitForTicks(t, srv, 200, func() bool { return name(0, 65, 0) == "minecraft:dirt" })
	c.Disconnect()
}

// TestExecuteInTeleportCarriesThePlayerAcross checks the teleport tail of a
// chain whose dimension is not the runner's: the player crosses into the
// dimension named, with the Respawn a living crossing sends, and stands
// where the tail said in that dimension rather than there in their own.
func TestExecuteInTeleportCarriesThePlayerAcross(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")

	c.ChatCommand("execute in minecraft:the_nether run tp @s 0 70 0")
	expectLivingRespawnIntoNether(t, c)

	// A whole x and z is the middle of its block.
	if pos := servertest.ExpectTeleport(t, c); pos.X != 0.5 || pos.Y != 70 || pos.Z != 0.5 {
		t.Errorf("teleport = %v, want (0.5, 70, 0.5)", pos)
	}
	if got := srv.PlayerDimension("Testificate"); got != world.NetherName {
		t.Errorf("dimension after the teleport = %q, want the nether", got)
	}
	if pos := srv.PlayerPosition("Testificate"); pos.X != 0.5 || pos.Y != 70 || pos.Z != 0.5 {
		t.Errorf("nether position = (%v, %v, %v), want (0.5, 70, 0.5)", pos.X, pos.Y, pos.Z)
	}

	// The nether's own stream read through, so the server is not left
	// holding chunks nobody is reading.
	servertest.AwaitViewSent(t, c)
	c.Disconnect()
}

// TestTeleportToAnEntityArrivesInItsDimension checks tp to an entity of
// another dimension: the runner crosses into the destination's dimension,
// not the one the command ran in.
// vanilla: TeleportCommand.teleportToEntity
func TestTeleportToAnEntityArrivesInItsDimension(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Other"))

	probe := connect().Join("Probe")
	srv.MovePlayerToDimension("Probe", world.NetherName, 8, 70, 8)
	servertest.AwaitViewSent(t, probe)
	want := srv.PlayerPosition("Probe")

	other := connect().Join("Other")

	other.ChatCommand("tp @s Probe")
	expectLivingRespawnIntoNether(t, other)
	if pos := servertest.ExpectTeleport(t, other); pos.X != want.X || pos.Y != want.Y || pos.Z != want.Z {
		t.Errorf("teleport = %v, want the probe's (%v, %v, %v)", pos, want.X, want.Y, want.Z)
	}
	if got := srv.PlayerDimension("Other"); got != world.NetherName {
		t.Errorf("dimension after the teleport = %q, want the probe's nether", got)
	}
	servertest.AwaitViewSent(t, other)
	other.Disconnect()
	probe.Disconnect()
}

// TestExecuteInTeleportCarriesEntitiesAcross checks the teleport tail of a
// chain in another dimension for bodies that are not players: an item and
// a cow each leave the overworld and stand in the nether where the tail
// said, a new entity under the UUID they had. A second player in the
// nether holds the arrival's chunk, which a chunk nobody holds would give
// up to its entity file within the tick.
func TestExecuteInTeleportCarriesEntitiesAcross(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Testificate"))

	// The holder stands in the arrival's chunk, out of pickup range of
	// the arrival itself.
	holder := connect().Join("Holder")
	srv.MovePlayerToDimension("Holder", world.NetherName, 4, 70, 4)
	servertest.AwaitViewSent(t, holder)
	netherPlatform(t, srv, 12, 70, 12)

	c := connect().Join("Testificate")

	itemID := srv.SpawnItemEntity(2.5, 64.125, 2.5, "minecraft:stone", 3)
	var itemUUID string
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.ID == itemID {
			itemUUID = it.UUID
		}
	}
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	cowUUID, ok := srv.MobUUID(cowID)
	if itemUUID == "" || !ok {
		t.Fatal("spawning the item and the cow failed")
	}

	c.ChatCommand("execute in minecraft:the_nether run tp " + itemUUID + " 12.5 70 12.5")
	c.ChatCommand("execute in minecraft:the_nether run tp " + cowUUID + " 12.5 70 12.5")

	servertest.UntilTicks(t, srv, 200, "condition", func() bool {
		items, mobs := 0, 0
		for _, it := range srv.ItemsInDimension(world.NetherName) {
			if it.UUID == itemUUID {
				items++
			}
		}
		for _, m := range srv.MobsInDimension(world.NetherName) {
			if m.UUID == cowUUID {
				mobs++
			}
		}
		return items == 1 && mobs == 1
	})
	for _, it := range srv.ItemsInDimension(world.NetherName) {
		if it.UUID != itemUUID {
			continue
		}
		if it.ID == itemID {
			t.Errorf("the item arrived under its old entity id %d, want a new one", it.ID)
		}
		if it.Item != "minecraft:stone" || it.Count != 3 {
			t.Errorf("the item arrived holding %q x%d, want the stack of three stone", it.Item, it.Count)
		}
		if it.X != 12.5 || it.Z != 12.5 {
			t.Errorf("the item arrived at (%v, %v), want (12.5, 12.5)", it.X, it.Z)
		}
	}
	for _, m := range srv.MobsInDimension(world.NetherName) {
		if m.UUID != cowUUID {
			continue
		}
		if m.ID == cowID || m.Kind != "minecraft:cow" {
			t.Errorf("the cow arrived as %s under id %d, want a cow under a new id (old %d)", m.Kind, m.ID, cowID)
		}
	}
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.UUID == itemUUID {
			t.Error("the item is still in the overworld")
		}
	}
	if _, ok := srv.MobUUID(cowID); ok {
		t.Error("the cow is still in the overworld")
	}
	c.Disconnect()
	holder.Disconnect()
}

// TestTeleportAcrossSkipsABodyKilledAfterTheSelector holds open the window
// between a teleport's selector and its move: the cow selected is killed
// before the move, and the dying body is not raised again, healthy, in the
// nether.
func TestTeleportAcrossSkipsABodyKilledAfterTheSelector(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")
	release := srv.HoldChunkInDimension(world.NetherName, 12, 12)
	defer release()
	netherPlatform(t, srv, 12, 70, 12)

	cowUUID, ok := srv.MobUUID(srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5))
	if !ok {
		t.Fatal("spawning the cow failed")
	}
	srv.TeleportAfter("minecraft:cow", world.NetherName, 12.5, 70, 12.5, func() {
		c.ChatCommand("kill " + cowUUID)
		c.AwaitPacket(protocol.IDPlaySystemChat)
	})

	for _, m := range srv.MobsInDimension(world.NetherName) {
		if m.UUID == cowUUID {
			t.Errorf("the killed cow stands in the nether at (%v, %v, %v), want it left to die where it was", m.X, m.Y, m.Z)
		}
	}
	c.Disconnect()
}

// TestTeleportAcrossSkipsAnItemBeingPickedUp holds open the window between
// a pickup's claim on an item and the giving of its stack: a teleport
// across the dimensions landing there finds the body already claimed and
// raises no twin of it, so the stack the collector was handed is not
// standing in the nether besides.
func TestTeleportAcrossSkipsAnItemBeingPickedUp(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")

	itemID := srv.SpawnItemEntity(0.5, 64.125, 0.5, "minecraft:stone", 3)
	teleported := false
	srv.PickupBetween(itemID, func() {
		teleported = true
		srv.TeleportAfter("minecraft:item", world.NetherName, 12.5, 70, 12.5, func() {})
	})

	// The pickup lands the whole stack in the collector's hotbar.
	servertest.UntilTicks(t, srv, 200, "condition", func() bool {
		return srv.InventorySlot("Testificate", player.HotbarSlot).Count == 3
	})
	if !teleported {
		t.Fatal("the pickup never held its window open")
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); got.ItemID == 0 || got.Count != 3 {
		t.Errorf("the hotbar holds %+v, want the three stone the item carried", got)
	}
	// The stack exists once: nothing stands in the nether the teleport was
	// aimed at, nor in the overworld the item lay in.
	if items := srv.ItemsInDimension(world.NetherName); len(items) != 0 {
		t.Errorf("the nether holds %v, want nothing: an item being picked up is not carried twice", items)
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 0 {
		t.Errorf("the overworld still holds %v, want the picked-up stack gone", items)
	}
	c.Disconnect()
}

// TestKillDuringPickupWindowStaysDead holds open the window between a
// pickup's claim on an item and the giving of its stack: a /kill landing
// there takes the body out of every view before the settle runs, and the
// settle is not to raise the body again, whatever the give could not fit —
// the leftover dies with the body the kill already removed.
func TestKillDuringPickupWindowStaysDead(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")

	// The collector's pockets are full of dirt, but one stack of stone has
	// four spaces left: the pickup of ten stone is a partial one, six left
	// for the body to keep or lose.
	dirt, _ := registry.ItemID("minecraft:dirt")
	stone, _ := registry.ItemID("minecraft:stone")
	full := func(item int32, count int32) protocol.ItemStack {
		return protocol.ItemStack{ItemID: item, Count: count}
	}
	for slot := player.MainSlot + 1; slot <= player.OffHandSlot; slot++ {
		srv.GiveSlot("Testificate", slot, full(dirt, 64))
	}
	srv.GiveSlot("Testificate", player.MainSlot, full(stone, 60))

	// The item lies out of the collector's reach, so nothing claims it
	// before the test has read the selector and armed the window; the
	// teleport below is what walks it into the collector's hands.
	itemID := srv.SpawnItemEntity(12.5, 64.125, 8.5, "minecraft:stone", 10)
	selected := srv.SelectedEntities("minecraft:item")
	killed := false
	srv.PickupBetween(itemID, func() {
		killed = true
		for _, e := range selected {
			e.Kill()
		}
	})
	at := srv.PlayerPosition("Testificate")
	srv.TeleportAfter("minecraft:item", world.DimensionName, at.X, at.Y, at.Z, func() {})

	// The pickup's give ran, so the window was held and the kill landed in
	// it: the four stone that fit are in the collector's stack.
	servertest.UntilTicks(t, srv, 200, "condition", func() bool {
		return srv.InventorySlot("Testificate", player.MainSlot).Count == 64
	})
	if !killed {
		t.Fatal("the pickup never held its window open")
	}
	// The kill's removal is not to be undone: the body stays dead once the
	// settle has run, not back among the living with the leftover on it.
	servertest.WaitForTicks(t, srv, 3, func() bool {
		return len(srv.ItemsInDimension(world.DimensionName)) == 0
	})
	c.Disconnect()
}

// TestTeleportTargetsToAnEntityArrivesInItsDimension checks the form that
// names who moves and an entity to move them to: the target crosses into
// the destination's dimension, the runner left where they were.
// vanilla: TeleportCommand.teleportToEntity
func TestTeleportTargetsToAnEntityArrivesInItsDimension(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Other"))

	probe := connect().Join("Probe")
	srv.MovePlayerToDimension("Probe", world.NetherName, 8, 70, 8)
	servertest.AwaitViewSent(t, probe)
	want := srv.PlayerPosition("Probe")

	other := connect().Join("Other")

	cowUUID, ok := srv.MobUUID(srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5))
	if !ok {
		t.Fatal("spawning the cow failed")
	}

	other.ChatCommand("tp @e[type=minecraft:cow,limit=1] Probe")
	servertest.AwaitCommandFeedbackText(t, other, "commands.teleport.success.entity.single",
		servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", mustUUID(t, cowUUID)),
		servertest.PlayerDisplayComponent("Probe"))

	servertest.UntilTicks(t, srv, 200, "condition", func() bool {
		for _, m := range srv.MobsInDimension(world.NetherName) {
			if m.UUID == cowUUID {
				return true
			}
		}
		return false
	})
	for _, m := range srv.MobsInDimension(world.NetherName) {
		if m.UUID != cowUUID {
			continue
		}
		// A cow that has begun to stroll may be a step from where it
		// landed; the dimension and the neighbourhood are the check.
		if m.X-want.X > 1 || want.X-m.X > 1 || m.Z-want.Z > 1 || want.Z-m.Z > 1 {
			t.Errorf("the cow arrived at (%v, %v), want by the probe at (%v, %v)", m.X, m.Z, want.X, want.Z)
		}
	}
	for _, m := range srv.MobsInDimension(world.DimensionName) {
		if m.UUID == cowUUID {
			t.Error("the cow is still in the overworld")
		}
	}
	if got := srv.PlayerDimension("Other"); got != world.DimensionName {
		t.Errorf("the runner's dimension after the teleport = %q, want the overworld they stood in", got)
	}
	other.Disconnect()
	probe.Disconnect()
}

// TestExecuteConditionalEntityAnswers checks the entity test at a chain's
// own end: the count the vanilla handler answers with in both directions,
// and the silence a forked chain's failure keeps.
func TestExecuteConditionalEntityAnswers(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")

	// Nothing of the kind stands: the bare conditional reports its
	// failure, the way a chain's own end does.
	c.ChatCommand("execute if entity @e[type=minecraft:creeper]")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("the failing entity test answered % X, want the conditional failure", got)
	}

	// One creeper: the count the test counted. The body stands in a shell
	// of stone, whose walls keep its fuse cold — a creeper that could
	// reach the runner would walk to them and blow the test's own bodies
	// up before the later readings ran.
	boxCreeper(t, srv, 0.5, 6.5)
	c.Settle()
	c.ChatCommand("execute if entity @e[type=minecraft:creeper]")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, conditionalPassCountForTest(1)) {
		t.Errorf("the passing entity test answered % X, want one body counted", got)
	}

	// The unless form turns the answers over.
	c.ChatCommand("execute unless entity @e[type=minecraft:creeper]")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, conditionalFailCountForTest(1)) {
		t.Errorf("the failing unless answered % X, want the counted refusal", got)
	}

	// A conditional the chain walks past answers nothing for itself, but
	// its tail's success still reaches the runner: the creeper's kill is
	// heard, the body it names with it.
	srv.SpawnMob("minecraft:cow", 2.5, 64, 8.5)
	c.Settle()
	c.ChatCommand("execute if entity @e[type=minecraft:creeper] run kill @e[type=minecraft:creeper]")
	awaitSystemChatSaying(t, c, "commands.kill.success.single")
	// The kill's body leaves the herd on the tick after its death, which
	// can still be a breath behind the silence the fork kept; the next
	// conditional must see it gone, so the test holds until it is.
	waitMobGone(t, srv, func(m server.TestMob) bool { return m.Kind == "minecraft:creeper" })

	// The creeper gone, a forked chain's failure says nothing: the test
	// drops the one cow the as carried and the chain ends unheard.
	c.ChatCommand("execute as @e[type=minecraft:cow] if entity @e[type=minecraft:creeper]")
	expectNoSystemChat(t, c)

	// And a forked chain's pass still says what it counted, once per
	// body the fork carried.
	boxCreeper(t, srv, 0.5, 6.5)
	srv.SpawnMob("minecraft:cow", 2.5, 64, 4.5)
	c.Settle()
	c.ChatCommand("execute as @e[type=minecraft:cow] if entity @e[type=minecraft:creeper]")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
		t.Errorf("the forked pass answered % X, want one body counted", got)
	}
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
		t.Errorf("the forked pass answered % X, want the second body counted", got)
	}
	c.Disconnect()
}

// TestExecuteIfBlockAndBlocksJudgeTheGround checks the block tests: the
// single block against its name or a tag's membership, the region against
// its copy in both the all and the masked readings, the region too large to
// compare, and the silence an unloaded corner keeps under a fork.
func TestExecuteIfBlockAndBlocksJudgeTheGround(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")

	// The ground is stone: the bare test runs its tail, the wrong block
	// at a chain's own end reports the failure, and the tag the stone
	// belongs to holds it too.
	c.ChatCommand("execute if block 0 63 0 minecraft:stone run say hi")
	if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
		t.Errorf("the passing block test said % X, want the tail's say", got)
	}
	c.ChatCommand("execute if block 0 63 0 minecraft:dirt")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("the failing block test answered % X, want the conditional failure", got)
	}
	c.ChatCommand("execute if block 0 63 0 #minecraft:base_stone_overworld run say hi")
	if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
		t.Errorf("the passing tag test said % X, want the tail's say", got)
	}

	// A region of stone against a copy of itself: the all reading counts
	// the cells it compared, and one air hole under the masked reading
	// comes out of the count without spoiling the match.
	for x := int32(4); x <= 6; x++ {
		for z := int32(4); z <= 6; z++ {
			srv.SetLoadedBlock(x, 63, z, srv.BlockAt(0, 63, 0))
		}
	}
	c.ChatCommand("execute if blocks 4 61 4 6 63 6 8 61 8 all")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, conditionalPassCountForTest(27)) {
		t.Errorf("the matching region answered % X, want twenty-seven cells counted", got)
	}
	srv.SetLoadedBlock(4, 61, 4, 0)
	c.ChatCommand("execute if blocks 4 61 4 6 63 6 8 61 8 masked")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, conditionalPassCountForTest(26)) {
		t.Errorf("the masked region answered % X, want the air hole out of the count", got)
	}
	c.ChatCommand("execute if blocks 4 61 4 6 63 6 8 61 8 all")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("the mismatched all reading answered % X, want the conditional failure", got)
	}

	// A region wider than the cap refuses with the cap and the size it
	// measured, as two numbers of their own kinds.
	c.ChatCommand("execute if blocks 0 61 0 32 93 32 0 61 0 all")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, commandErrorNumbersForTest("commands.execute.blocks.toobig", 32768, 35937)) {
		t.Errorf("the oversized region answered % X, want the size refusal", got)
	}

	// An unloaded corner stops the fork without a word.
	c.ChatCommand("execute if blocks 0 61 0 2 63 2 1000000 61 1000000 all run say hi")
	expectNoSystemChat(t, c)
	c.Disconnect()
}

// TestExecuteIfBlocksFailsOnALaterMismatch checks that a region differing
// only in its last compared cell fails the if form and passes the unless
// form, and that a masked comparison over air alone still passes with a
// count of zero.
// vanilla: ExecuteCommand.checkRegions, checkIfRegions, checkUnlessRegions
func TestExecuteIfBlocksFailsOnALaterMismatch(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")

	stone := srv.BlockAt(0, 63, 0)
	for x := int32(4); x <= 6; x++ {
		for z := int32(4); z <= 6; z++ {
			srv.SetLoadedBlock(x, 63, z, stone)
			srv.SetLoadedBlock(x+4, 63, z+4, stone)
		}
	}
	// The scan runs x, then y, then z, so 6 63 6 is the last cell compared.
	srv.SetLoadedBlock(10, 63, 10, 0)

	c.ChatCommand("execute if blocks 4 61 4 6 63 6 8 61 8 all")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("the if form answered % X, want the conditional failure", got)
	}
	c.ChatCommand("execute unless blocks 4 61 4 6 63 6 8 61 8 all")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, plainFeedbackTextsForTest("commands.execute.conditional.pass")) {
		t.Errorf("the unless form answered % X, want the conditional pass", got)
	}

	// Air against air under the masked reading skips every cell: the
	// regions match with nothing counted.
	c.ChatCommand("execute if blocks 4 100 4 6 102 6 8 100 8 masked")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, conditionalPassCountForTest(0)) {
		t.Errorf("the masked air region answered % X, want a pass counting zero", got)
	}
	c.ChatCommand("execute unless blocks 4 100 4 6 102 6 8 100 8 masked")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, conditionalFailCountForTest(0)) {
		t.Errorf("the unless form over masked air answered % X, want the failure counting zero", got)
	}
	c.Disconnect()
}

// TestExecuteIfBlocksOffsetFromLowerCorner checks that the destination is
// measured from the box's lower corner, however the corners are typed.
// vanilla: ExecuteCommand.checkRegions
func TestExecuteIfBlocksOffsetFromLowerCorner(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")

	stone := srv.BlockAt(0, 63, 0)
	srv.SetLoadedBlock(10, 70, 10, stone)
	srv.SetLoadedBlock(10, 72, 10, stone)

	c.ChatCommand("execute if blocks 11 70 10 10 70 10 10 72 10 all")
	if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got, conditionalPassCountForTest(2)) {
		t.Errorf("the reversed-corner region answered % X, want two cells counted", got)
	}
	c.Disconnect()
}

// TestExecuteIfItemsMatchesSlotsAndPredicates checks the items test: the
// slot words that name a body's slots and a block's container, the item, tag
// and any predicates, and the count and the unless form the answer carries.
func TestExecuteIfItemsMatchesSlotsAndPredicates(t *testing.T) {
	t.Parallel()
	srv, c := dialItem(t, "Testificate")

	// Five stone in the first hotbar slot.
	c.ChatCommand("give @s minecraft:stone 5")
	c.Settle()

	// The star words span the hotbar and the whole container; the slot the
	// stone landed in matches, and each answer counts the items it found.
	for _, command := range []string{
		"execute if items entity @s container.* minecraft:stone",
		"execute if items entity @s container.0 *",
		"execute if items entity @s weapon.mainhand *",
		"execute if items entity @s hotbar.* minecraft:stone",
	} {
		c.ChatCommand(command)
		if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(5)) {
			t.Errorf("%q answered % X, want five items counted", command, got)
		}
	}

	// A slot holding none of the item fails.
	c.ChatCommand("execute if items entity @s hotbar.5 minecraft:stone")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("the empty slot answered % X, want the conditional failure", got)
	}

	// The unless form turns the answers over: the wrong item passes, the
	// right one fails with the count.
	c.ChatCommand("execute unless items entity @s weapon.mainhand minecraft:dirt")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, plainFeedbackTextsForTest("commands.execute.conditional.pass")) {
		t.Errorf("the passing unless answered % X, want the conditional pass", got)
	}
	c.ChatCommand("execute unless items entity @s weapon.mainhand minecraft:stone")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalFailCountForTest(5)) {
		t.Errorf("the failing unless answered % X, want five items counted", got)
	}

	// The tag predicate matches by membership: the pickaxe belongs to the
	// pickaxes tag, the stone does not.
	c.ChatCommand("give @s minecraft:diamond_pickaxe 1")
	c.Settle()
	c.ChatCommand("execute if items entity @s hotbar.* #minecraft:pickaxes")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
		t.Errorf("the tag test answered % X, want one pickaxe counted", got)
	}
	c.ChatCommand("execute if items entity @s hotbar.* #minecraft:swords")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("the wrong tag answered % X, want the conditional failure", got)
	}

	// A block's container answers the same way: three stone in the barrel's
	// first slot are three items of the star range.
	c.ChatCommand("setblock 2 64 3 minecraft:barrel")
	c.Settle()
	c.ChatCommand("item replace block 2 64 3 container.0 with minecraft:stone 3")
	c.Settle()
	if got := srv.InventoryAt(2, 64, 3)[0]; got.ItemID != servertest.MustItem("minecraft:stone") || got.Count != 3 {
		t.Fatalf("barrel slot 0 = %+v, want the three stone the test set", got)
	}
	c.ChatCommand("execute if items block 2 64 3 container.* minecraft:stone")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(3)) {
		t.Errorf("the block container test answered % X, want three items counted", got)
	}
	c.ChatCommand("execute if items block 2 64 3 container.0 *")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(3)) {
		t.Errorf("the block slot test answered % X, want three items counted", got)
	}
	c.Disconnect()
}

// TestExecuteIfItemsEmptyBracketIsAPlainType checks the optional condition
// list the item predicate's top rule leaves out: an empty pair of brackets
// names the item alone, so it matches where the bare name would.
func TestExecuteIfItemsEmptyBracketIsAPlainType(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")

	c.ChatCommand("item replace entity @s hotbar.0 with minecraft:stone 5")
	c.Settle()
	c.ChatCommand("execute if items entity @s hotbar.* stone[]")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(5)) {
		t.Errorf("the empty bracket answered % X, want five items counted", got)
	}
	c.Disconnect()
}

// TestExecuteIfItemsEvaluatesComponentPredicates checks that the items test
// runs the same predicate /clear does: count bounds and custom_data, and that
// /clear takes the custom_data form too.
func TestExecuteIfItemsEvaluatesComponentPredicates(t *testing.T) {
	t.Parallel()
	srv, c := dialItem(t, "Testificate")
	check := func(cmd string, want int) {
		t.Helper()
		c.ChatCommand("execute if items entity @s hotbar.* " + cmd)
		got := c.ExpectSystemChatPastMobs()
		if want == 0 {
			if !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
				t.Errorf("%s answered % X, want the conditional failure", cmd, got)
			}
		} else if !bytes.Equal(got, conditionalPassCountForTest(int64(want))) {
			t.Errorf("%s answered % X, want %d counted", cmd, got, want)
		}
	}

	c.ChatCommand("item replace entity @s hotbar.0 with minecraft:stone 5")
	c.Settle()
	check("stone[count=5]", 5)
	check("stone[count=2]", 0)

	c.ChatCommand(`item replace entity @s hotbar.1 with minecraft:stick[custom_data={kind:"wand",power:3}] 2`)
	c.Settle()
	check(`*[custom_data~{kind:"wand"}]`, 2)
	check(`*[custom_data~{kind:"sword"}]`, 0)
	check(`stick[custom_data={kind:"wand",power:3}]`, 2)
	check(`stick[custom_data={kind:"wand"}]`, 0)

	c.ChatCommand(`clear @s *[custom_data~{kind:"wand"}]`)
	c.Settle()
	if got := srv.InventorySlot("Testificate", player.HotbarSlot+1); !got.Empty() {
		t.Errorf("hotbar slot 1 holds %+v after clearing the wand", got)
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); got.Count != 5 {
		t.Errorf("hotbar slot 0 holds %+v, want the five stone", got)
	}
	c.Disconnect()
}

// TestExecuteIfItemsCountsEveryStackAndRefusesNonContainers checks the count
// summed across a body's slots, the armour star word, the not-a-container
// refusal and the not-found refusal a selector naming nobody throws.
func TestExecuteIfItemsCountsEveryStackAndRefusesNonContainers(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")

	// Two stone stacks in two hotbar slots count together.
	c.ChatCommand("item replace entity @s hotbar.0 with minecraft:stone 5")
	c.ChatCommand("item replace entity @s hotbar.1 with minecraft:stone 3")
	c.Settle()
	c.ChatCommand("execute if items entity @s hotbar.* minecraft:stone")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(8)) {
		t.Errorf("the two stacks answered % X, want the eight counted together", got)
	}

	// armor.* reaches every piece the player wears.
	for _, tc := range []struct{ word, item string }{
		{"armor.head", "minecraft:diamond_helmet"},
		{"armor.chest", "minecraft:diamond_chestplate"},
		{"armor.legs", "minecraft:diamond_leggings"},
		{"armor.feet", "minecraft:diamond_boots"},
	} {
		c.ChatCommand("item replace entity @s " + tc.word + " with " + tc.item + " 1")
	}
	c.Settle()
	c.ChatCommand("execute if items entity @s armor.* *")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(4)) {
		t.Errorf("armor.* answered % X, want the four worn pieces counted", got)
	}

	// A block that holds nothing is refused as not a container, by place.
	c.ChatCommand("setblock 2 64 3 minecraft:ender_chest")
	c.Settle()
	c.ChatCommand("execute if items block 2 64 3 container.* *")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, commandErrorNumbersForTest(itemSourceNoContainerKey, 2, 64, 3)) {
		t.Errorf("the ender chest answered % X, want the not-a-container refusal", got)
	}

	// A selector that names nobody throws the argument's not-found refusal.
	c.ChatCommand("execute if items entity @e[type=minecraft:wither] hotbar.* *")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, servertest.CommandErrorMessageForTest("argument.entity.notfound.entity")) {
		t.Errorf("the empty selector answered % X, want the not-found refusal", got)
	}
	c.Disconnect()
}

// TestExecuteIfItemsUnknownSlot checks that a slot word outside the grammar is
// refused while the input is read, not left to fail the conditional.
func TestExecuteIfItemsUnknownSlot(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")

	const command = "execute if items entity @s foo.* *"
	c.ChatCommand(command)
	c.ExpectCommandError(
		servertest.CommandErrorMessageForTest(slotUnknownKey, "foo.*"),
		servertest.CommandErrorContextForTest(command, len("execute if items entity @s foo.*")),
	)
	c.Disconnect()
}

// TestExecuteScoreAndStopwatchRefuse checks the tests whose registries this
// server keeps none of: the score's objective refuses the way an empty
// scoreboard does — reported at a chain's own end, silent under a fork —
// and the stopwatch refuses an id it does not hold.
func TestExecuteScoreAndStopwatchRefuse(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")

	srv.SpawnMob("minecraft:cow", 0.5, 64, 6.5)
	c.Settle()

	c.ChatCommand("execute if score Testificate fakeobj = Testificate fakeobj")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("arguments.objective.notFound", "fakeobj")) {
		t.Errorf("the score test answered % X, want the empty scoreboard's refusal", got)
	}

	// A fake holder's name is any word, '#' and '$' leading included.
	c.ChatCommand("execute if score #a fakeobj = $a-name-longer-than-sixteen fakeobj")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("arguments.objective.notFound", "fakeobj")) {
		t.Errorf("the score test over fake holders answered % X, want the empty scoreboard's refusal", got)
	}

	c.ChatCommand("execute as @e[type=minecraft:cow] if score Testificate fakeobj = Testificate fakeobj")
	expectNoSystemChat(t, c)

	c.ChatCommand("execute if stopwatch fakeobj 0..")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.stopwatch.does_not_exist", "minecraft:fakeobj")) {
		t.Errorf("the stopwatch test answered % X, want the empty registry's refusal", got)
	}
	c.Disconnect()
}

// TestExecuteStoreRefuses checks the store's kinds: the score and the
// bossbar refuse an objective or a bar that does not exist, the block's
// data refuses once the position is loaded, and the entity's and the storage's writes go
// by unheard, the way this server's shapes allow.
func TestExecuteStoreRefuses(t *testing.T) {
	t.Parallel()
	_, c := servertest.StartExecuteWorld(t, "Testificate")

	c.ChatCommand("execute store result score @s nonexistent run say hi")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("arguments.objective.notFound", "nonexistent")) {
		t.Errorf("the score store answered % X, want the objective's refusal", got)
	}

	c.ChatCommand("execute store result bossbar fakeobj value run say hi")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.bossbar.unknown", "minecraft:fakeobj")) {
		t.Errorf("the bossbar store answered % X, want the empty registry's refusal", got)
	}

	// An unloaded block refuses before the data is asked for.
	c.ChatCommand("execute store result block 1000000 64 1000000 foo byte 1 run say hi")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("argument.pos.unloaded")) {
		t.Errorf("the unloaded store answered % X, want the position's refusal", got)
	}

	// A loaded one refuses at the block entity's data, which no block
	// here carries.
	c.ChatCommand("execute store result block 0 63 0 foo byte 1 run say hi")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.data.block.invalid")) {
		t.Errorf("the block store answered % X, want the data's refusal", got)
	}

	// The entity's and the storage's writes are refused by nothing and
	// written to nothing, so the tail runs and nothing else arrives.
	c.ChatCommand("execute store result entity @s foo byte 1 run say hi")
	if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
		t.Errorf("the entity store's tail said % X, want the say", got)
	}
	expectNoSystemChat(t, c)
	c.ChatCommand("execute store result storage foo bar byte 1 run say hi")
	if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
		t.Errorf("the storage store's tail said % X, want the say", got)
	}
	expectNoSystemChat(t, c)
	c.Disconnect()
}

// TestExecuteSummonCarriesTheChainAsTheBody checks the summon step: a cow
// stood up where the chain reads the world from becomes the body the tail's
// @s names, and a kind the game will not stand up refuses where the vanilla
// argument does.
func TestExecuteSummonCarriesTheChainAsTheBody(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")

	// The cow stands where the runner stands and the tail teleports it,
	// two blocks on: the @s named the summoned body, not the runner.
	c.ChatCommand("execute summon minecraft:cow run tp @s ~2 ~ ~")
	c.Settle()
	waitMob(t, srv, func(m server.TestMob) bool {
		return m.Kind == "minecraft:cow" && m.X > 1.9 && m.X < 3.1 && m.Z > 0.4 && m.Z < 0.6
	})

	// A player is no kind the summon stands up, whatever context it runs
	// under: the refusal the vanilla server sends, reported bare.
	c.ChatCommand("execute summon minecraft:player run say hi")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("entity.not_summonable", "minecraft:player")) {
		t.Errorf("the player summon answered % X, want the summonable refusal", got)
	}

	c.Disconnect()
}

// TestExecuteSummonTNTIsTheChainsBody checks that a summon the server stands
// up through its own call still hands the chain the new body as @s: the tag
// lands on the TNT, not on the runner. The fuse is 80 ticks, well past the
// three commands.
func TestExecuteSummonTNTIsTheChainsBody(t *testing.T) {
	t.Parallel()
	_, c := servertest.StartExecuteWorld(t, "Testificate")

	c.ChatCommand("execute summon minecraft:tnt run tag @s add fresh")
	c.Settle()

	c.ChatCommand("execute if entity @e[type=minecraft:tnt,tag=fresh]")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
		t.Errorf("the tagged TNT test answered % X, want one body counted", got)
	}
	c.ChatCommand("execute if entity @a[tag=fresh]")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("the tagged player test answered % X, want the conditional failure", got)
	}
	c.Disconnect()
}

// TestExecuteOnAndTheParseRefusals checks the steps that end a chain before
// it starts: the on relation finds nobody here and stops without a word,
// the predicate's name is refused while the input is still being read, and
// a single score holder that names several bodies is refused the same way,
// at the end of the word it was reading.
func TestExecuteOnAndTheParseRefusals(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")

	srv.SpawnMob("minecraft:cow", 0.5, 64, 6.5)
	c.Settle()

	// No relation holds anybody here: the fork comes to nothing and the
	// chain ends unheard.
	c.ChatCommand("execute on attacker run say hi")
	expectNoSystemChat(t, c)

	// The predicate is refused at parse, where the vanilla argument
	// resolves its name: the error and the context line, the marker at
	// the end of the word that named nothing.
	command := "execute if predicate minecraft:nope run say hi"
	c.ChatCommand(command)
	c.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.resource_or_id.no_such_element", "minecraft:nope", "minecraft:predicate"),
		servertest.CommandErrorContextForTest(command, len("execute if predicate minecraft:nope")),
	)

	// A single score holder is refused the same way, the marker at the
	// end of the selector word that named too many.
	command = "execute if score @e[type=minecraft:cow] fakeobj = @s fakeobj"
	c.ChatCommand(command)
	c.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.entity.toomany"),
		servertest.CommandErrorContextForTest(command, len("execute if score @e[type=minecraft:cow]")),
	)
	c.Disconnect()
}

// TestExecuteAnchoredAndFacingAimTheTail checks the two modifiers that turn
// the chain: an anchored eyes lifts the point the facing aims from, and a
// facing entity aims the chain at the body it names, with the fork carrying
// the tail over every body the selector found.
func TestExecuteAnchoredAndFacingAimTheTail(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")

	// The facing carries the tail over the two bodies it named, each say
	// one of the fork's shares.
	srv.SpawnMob("minecraft:cow", 3.5, 64, 0.5)
	srv.SpawnMob("minecraft:cow", 6.5, 64, 0.5)
	c.Settle()
	c.ChatCommand("execute facing entity @e[type=minecraft:cow] feet run say hi")
	for range 2 {
		if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
			t.Errorf("the facing fork said % X, want one say per body", got)
		}
	}
	expectNoSystemChat(t, c)

	// A facing entity nobody answers drops the fork unheard.
	c.ChatCommand("execute facing entity @e[type=minecraft:creeper] feet run say hi")
	expectNoSystemChat(t, c)

	// The anchored and the facing position turn the chain without
	// carrying anything: the tail runs once, from where it stood.
	c.ChatCommand("execute anchored eyes facing 3 64 0 run say hi")
	if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
		t.Errorf("the faced chain said % X, want the one say", got)
	}

	// The data test of a body this server carries no NBT of answers with
	// the count it read, which is none.
	c.ChatCommand("execute if data entity @s foo")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("the data test answered % X, want the conditional failure of an empty read", got)
	}
	c.Disconnect()
}

// TestExecuteForkedTeleportStillTellsTheRunner checks that a tail whose
// chain forked its way to it still hands its success line to the runner: the
// fork replaces the body the commands act as, not the one they answer to, so
// a teleport of every body the as named tells the player who ran it where
// each one went, while a forked refusal stays unheard. The as carries the
// body, not the ground, so the tail's relative destination reads the
// runner's position and the cow lands at the runner's feet.
func TestExecuteForkedTeleportStillTellsTheRunner(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")

	srv.SpawnMob("minecraft:cow", 3.5, 64, 0.5)
	c.Settle()

	c.ChatCommand("execute as @e[type=minecraft:cow] run tp @s ~ ~ ~")
	awaitTeleportForkFeedback(t, c, "0.500000", "64.000000", "0.500000")
	expectNoSystemChat(t, c)

	// A forked tail's refusal is a different story: the fork the selector
	// leaves empty-handed says nothing at all.
	c.ChatCommand("execute as @e[type=minecraft:creeper] run tp @s ~ ~ ~")
	expectNoSystemChat(t, c)

	c.Disconnect()
}

// awaitTeleportForkFeedback reads packets until the System Chat line a
// forked teleport hands the runner arrives, and checks it piecewise: the
// success line's key, the body it names by the translation, insertion and
// show-entity hover a display name carries, and the place it names. The
// pieces rather than a byte shape, because the body's identity is fresh.
func awaitTeleportForkFeedback(t *testing.T, c *servertest.Client, x, y, z string) {
	t.Helper()
	var skipped []servertest.PushedPacket
	defer func() { c.Unread(skipped...) }()
	for packetID, data := range c.Stream() {
		if packetID != protocol.IDPlaySystemChat {
			skipped = append(skipped, servertest.PushedPacket{Id: packetID, Data: data})
			continue
		}
		checkTeleportForkFeedback(t, data, x, y, z)
		return
	}
	t.Fatal("no teleported fork feedback arrived")
}

// checkTeleportForkFeedback fails unless the System Chat payload is the
// teleport's success line: the key, a display name of the cow for its first
// argument and the three coordinates after it.
func checkTeleportForkFeedback(t *testing.T, data []byte, x, y, z string) {
	t.Helper()
	// The payload is the component's NBT with the overlay flag after it.
	// The wire form of a component leaves the root tag unnamed; the empty
	// name the file form wants is put back in after the root tag's type.
	network := append([]byte{data[0], 0, 0}, data[1:len(data)-1]...)
	_, tag, err := nbt.Decode(network)
	if err != nil {
		t.Fatalf("decode teleport feedback: %v", err)
	}
	if data[len(data)-1] != 0 {
		t.Error("the teleport feedback is an overlay message")
	}
	compound, ok := tag.(nbt.Compound)
	if !ok {
		t.Fatalf("teleport feedback is a %s, want a compound", tag.Type())
	}
	if got, _ := nbt.Get[nbt.String](compound, "translate"); string(got) != "commands.teleport.success.location.single" {
		t.Errorf("teleport feedback translate = %q, want commands.teleport.success.location.single", got)
	}
	with, ok := nbt.Get[nbt.List](compound, "with")
	if !ok || len(with.Items) != 4 {
		t.Fatalf("teleport feedback with = %v, want four arguments", with)
	}
	name, ok := with.Items[0].(nbt.Compound)
	if !ok {
		t.Fatalf("the teleport feedback's first argument is a %s, want a compound", with.Items[0].Type())
	}
	if got, _ := nbt.Get[nbt.String](name, "translate"); string(got) != "entity.minecraft.cow" {
		t.Errorf("the teleported body's name translate = %q, want entity.minecraft.cow", got)
	}
	insertion, _ := nbt.Get[nbt.String](name, "insertion")
	if _, err := uuid.Parse(string(insertion)); err != nil {
		t.Errorf("the teleport feedback's insertion = %q, want a UUID", insertion)
	}
	// The coordinates travel as one-word components, wrapped in the empty
	// key the wire form of a bare text carries - which is the same wrapper
	// a heterogeneous list goes into, so the reading undoes it again and
	// the coordinates come back as bare strings.
	for i, want := range []string{x, y, z} {
		got, ok := with.Items[i+1].(nbt.String)
		if !ok {
			t.Errorf("the teleport feedback's coordinate %d is a %s, want a bare string", i, with.Items[i+1].Type())
			continue
		}
		if string(got) != want {
			t.Errorf("the teleport feedback's coordinate %d = %q, want %s", i, got, want)
		}
	}
}

// awaitSystemChatSaying reads packets until a System Chat line whose payload
// carries the given translation key arrives, and puts back everything it
// stepped over. The key rather than a whole byte shape, because the line's
// arguments carry a fresh identity.
func awaitSystemChatSaying(t *testing.T, c *servertest.Client, key string) {
	t.Helper()
	var skipped []servertest.PushedPacket
	defer func() { c.Unread(skipped...) }()
	for packetID, data := range c.Stream() {
		if packetID != protocol.IDPlaySystemChat {
			skipped = append(skipped, servertest.PushedPacket{Id: packetID, Data: data})
			continue
		}
		if bytes.Contains(data, []byte(key)) {
			return
		}
		skipped = append(skipped, servertest.PushedPacket{Id: packetID, Data: data})
	}
	t.Fatalf("no system chat saying %s arrived", key)
}

// startExecuteWorldForTwo is startExecuteWorld with a second player for the
// as chains to name: the runner an operator, the guest an ordinary player
// standing beside them at the spawn.
func startExecuteWorldForTwo(t *testing.T, runner, guest string) (*server.Server, *servertest.Client, *servertest.Client, int32) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, runner))
	r := connect().Join(runner)
	g := connect().Join(guest)
	guestID := g.PlayerEntityID
	r.ExpectPlayerAdded(guest)
	r.Settle()
	return srv, r, g, guestID
}

// expectNoDeath fails when a combat death reaches the connection before the
// ping the helper sends answers: how a test hears that the caller was left
// standing where the chain meant the body it named to fall.
func expectNoDeath(t *testing.T, c *servertest.Client) {
	t.Helper()
	c.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(0x5A) })
	for id, data := range c.Stream() {
		t.Helper()
		switch id {
		case protocol.IDPlayPingResponse:
			return
		case protocol.IDPlayCombatDeath:
			t.Fatalf("a combat death arrived where none was wanted: % X", data)
		}
	}
	t.Fatal("the ping was never answered")
}

// holdsWith scans the whole inventory window of the named player for the
// item and answers what it found.
func holdsWith(srv *server.Server, name string, itemID int32) bool {
	for slot := range 46 {
		if srv.InventorySlot(name, slot).ItemID == itemID {
			return true
		}
	}
	return false
}

// stoodAt reports whether the named player stands within the half-block box
// around the coordinates.
func stoodAt(srv *server.Server, name string, x, z float64) bool {
	p := srv.PlayerPosition(name)
	return p.X >= x-0.5 && p.X <= x+0.5 && p.Z >= z-0.5 && p.Z <= z+0.5
}

// TestExecuteAsActsOnTheBodyItNames has the caller run tails whose target
// the body an as names: the @s a give, a clear, a bare teleport, a bare
// spawnpoint and a bare kill reach, while the caller stays where they were
// with what they had — the position and the permission of the chain staying
// the caller's own. The console, naming no body, is refused.
// vanilla: CommandSourceStack.withEntity
func TestExecuteAsActsOnTheBodyItNames(t *testing.T) {
	t.Parallel()
	srv, alpha, beta, betaID := startExecuteWorldForTwo(t, "Alpha", "Beta")

	diamond, _ := registry.ItemID("minecraft:diamond")

	// The give under as: Beta holds the diamond, Alpha does not.
	alpha.ChatCommand("execute as Beta run give @s diamond")
	alpha.Settle()
	if !holdsWith(srv, "Beta", diamond) {
		t.Error("Beta holds no diamond")
	}
	if holdsWith(srv, "Alpha", diamond) {
		t.Error("Alpha was given the diamond meant for Beta")
	}

	// A bare clear under as: Beta's inventory empties.
	alpha.ChatCommand("execute as Beta run clear")
	alpha.Settle()
	if holdsWith(srv, "Beta", diamond) {
		t.Error("Beta kept the diamond the clear was to take")
	}

	// A bare gamemode under as: Beta plays the mode named, Alpha theirs.
	alpha.ChatCommand("execute as Beta run gamemode adventure")
	alpha.Settle()
	if got := srv.PlayerGameMode("Beta"); got != protocol.GameModeAdventure {
		t.Errorf("Beta's game mode = %d, want %d (adventure)", got, protocol.GameModeAdventure)
	}
	if got := srv.PlayerGameMode("Alpha"); got != protocol.GameModeSurvival {
		t.Errorf("Alpha's game mode = %d, want %d (survival)", got, protocol.GameModeSurvival)
	}

	// A bare teleport under as: Beta moves two blocks east of Alpha, whose
	// own place the chain reads the world from and never leaves.
	alpha.ChatCommand("execute as Beta run tp ~2 ~ ~")
	alpha.Settle()
	if !stoodAt(srv, "Beta", 2.5, 0.5) {
		t.Errorf("Beta stood at %v, want by (2.5, 0.5)", srv.PlayerPosition("Beta"))
	}
	if !stoodAt(srv, "Alpha", 0.5, 0.5) {
		t.Errorf("Alpha stood at %v, want (0.5, 0.5)", srv.PlayerPosition("Alpha"))
	}

	// The @s the tail names is Beta, but the relative step still reads
	// from Alpha: one west of the caller puts Beta a block west of them.
	alpha.ChatCommand("execute as Beta run tp @s ~-1 ~ ~")
	alpha.Settle()
	if !stoodAt(srv, "Beta", -0.5, 0.5) {
		t.Errorf("Beta stood at %v, want by (-0.5, 0.5)", srv.PlayerPosition("Beta"))
	}

	// With an at, the chain reads from where Beta stands: two east of
	// Beta's own place, not of Alpha's.
	alpha.ChatCommand("execute as Beta at @s run tp @s ~2 ~ ~")
	alpha.Settle()
	if !stoodAt(srv, "Beta", 1.5, 0.5) {
		t.Errorf("Beta stood at %v, want by (1.5, 0.5) of their own spot", srv.PlayerPosition("Beta"))
	}

	// A bare spawnpoint under as: Beta's respawn moves to the block Alpha
	// stands in — the chain's position stays the caller's own — forced the
	// way /spawnpoint sets it.
	alpha.ChatCommand("execute as Beta run spawnpoint")
	alpha.Settle()
	if spawn, forced, ok := srv.SpawnPointOf("Beta"); !ok || spawn != (world.SpawnPos{X: 0, Y: 64, Z: 0}) || !forced {
		t.Errorf("Beta's spawn point = (%d, %d, %d) forced %v, %v; want (0, 64, 0) forced, true",
			spawn.X, spawn.Y, spawn.Z, forced, ok)
	}

	// A bare kill under as: Beta dies, Alpha does not.
	alpha.ChatCommand("execute as Beta run kill")
	data := beta.AwaitPacket(protocol.IDPlayCombatDeath)
	if got := protocol.NewReader(data).ReadVarInt(); got != betaID {
		t.Errorf("the death fell on entity %d, want Beta's %d", got, betaID)
	}
	expectNoDeath(t, alpha)

	// The console runs the bare forms with no body to name, and is refused
	// the way a source without one is: an entity for kill and tp, a player
	// for the rest.
	for line, want := range map[string]string{
		"kill":              "An entity is required to run this command here",
		"tp 0 64 0":         "An entity is required to run this command here",
		"clear":             "A player is required to run this command here",
		"gamemode creative": "A player is required to run this command here",
		"spawnpoint":        "A player is required to run this command here",
	} {
		if reply := srv.RunLine(line); len(reply) != 1 || reply[0].Plain() != want {
			t.Errorf("console %q answered %v, want %q", line, reply, want)
		}
	}
}

// TestExecuteAsOnABodyThatIsNoPlayer has an as name a cow: the commands that
// act on players alone refuse it, and the ones that act on any body kill it.
func TestExecuteAsOnABodyThatIsNoPlayer(t *testing.T) {
	t.Parallel()
	srv, alpha, _, _ := startExecuteWorldForTwo(t, "Alpha", "Beta")

	cowID := srv.SpawnMob("minecraft:cow", 4.5, 64, 4.5)
	alpha.Settle()

	// A gamemode's body must be a player, and a forked chain's refusals go
	// unheard: the cow is refused, leaving every player's mode alone.
	alpha.ChatCommand("execute as @e[type=minecraft:cow] run gamemode creative")
	alpha.Settle()
	if got := srv.PlayerGameMode("Alpha"); got != protocol.GameModeSurvival {
		t.Errorf("Alpha's game mode = %d, want %d (survival)", got, protocol.GameModeSurvival)
	}
	if got := srv.PlayerGameMode("Beta"); got != protocol.GameModeSurvival {
		t.Errorf("Beta's game mode = %d, want %d (survival)", got, protocol.GameModeSurvival)
	}

	// A kill's body may be any: the cow dies, the runner does not.
	alpha.ChatCommand("execute as @e[type=minecraft:cow] run kill")
	waitMobGone(t, srv, func(m server.TestMob) bool { return m.ID == cowID })
	expectNoDeath(t, alpha)
}

// TestExecuteAsSaysAsTheBodyItNames checks who a /say, /me or /msg under an
// as is attributed to: the display name of the executing entity, not the
// runner's. vanilla: CommandSourceStack.withEntity, SayCommand, MeCommand,
// MsgCommand
func TestExecuteAsSaysAsTheBodyItNames(t *testing.T) {
	t.Parallel()
	srv, alpha, beta, _ := startExecuteWorldForTwo(t, "Alpha", "Beta")

	// A player body: the say and the emote name Beta, whoever ran the chain.
	alpha.ChatCommand("execute as Beta run say hi")
	servertest.ExpectDisguisedChat(t, alpha, "minecraft:say_command", "Beta", "hi", "")
	servertest.ExpectDisguisedChat(t, beta, "minecraft:say_command", "Beta", "hi", "")

	alpha.ChatCommand("execute as Beta run me waves")
	servertest.ExpectDisguisedChat(t, alpha, "minecraft:emote_command", "Beta", "waves", "")
	servertest.ExpectDisguisedChat(t, beta, "minecraft:emote_command", "Beta", "waves", "")

	// A whisper speaks as Beta: the body reads the outgoing line, the target
	// reads the incoming one, and the runner reads nothing else.
	alpha.ChatCommand("execute as Beta run msg Alpha secret")
	servertest.ExpectDisguisedChat(t, beta, "minecraft:msg_command_outgoing", "Beta", "secret", "Alpha")
	servertest.ExpectDisguisedChat(t, alpha, "minecraft:msg_command_incoming", "Beta", "secret", "")

	// A mob body is named by its display name; a whisper it sends has no
	// player to read the outgoing line, so the runner reads it as the
	// outgoing type's decorated system line.
	cowID := srv.SpawnMob("minecraft:cow", 4.5, 64, 4.5)
	cowUUID, _ := srv.MobUUID(cowID)
	cow := servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", uuid.MustParse(cowUUID))
	alpha.Settle()
	alpha.ChatCommand("execute as @e[type=minecraft:cow] run say moo")
	servertest.ExpectDisguisedChat(t, alpha, "minecraft:say_command", cow, "moo", "")
	servertest.ExpectDisguisedChat(t, beta, "minecraft:say_command", cow, "moo", "")
	alpha.ChatCommand("execute as @e[type=minecraft:cow] run msg Alpha moo")
	alpha.ExpectSystemChat("commands.message.display.outgoing", false,
		servertest.PlayerDisplayComponent("Alpha"), protocol.Text{Text: "moo"})
	servertest.ExpectDisguisedChat(t, alpha, "minecraft:msg_command_incoming", cow, "moo", "")

	// The console's own chain speaks as the body too: a whisper the body
	// sends reaches the body, and the console reads nothing.
	if reply := srv.RunLine("execute as Beta run say console"); len(reply) != 0 {
		t.Errorf("the console's say answered %v, want nothing", reply)
	}
	servertest.ExpectDisguisedChat(t, alpha, "minecraft:say_command", "Beta", "console", "")
	servertest.ExpectDisguisedChat(t, beta, "minecraft:say_command", "Beta", "console", "")
	if reply := srv.RunLine("execute as Beta run msg Alpha console"); len(reply) != 0 {
		t.Errorf("the console's msg answered %v, want nothing", reply)
	}
	servertest.ExpectDisguisedChat(t, beta, "minecraft:msg_command_outgoing", "Beta", "console", "Alpha")
	servertest.ExpectDisguisedChat(t, alpha, "minecraft:msg_command_incoming", "Beta", "console", "")

	alpha.Disconnect()
	beta.Disconnect()
}

// TestExecuteAsGamemodeSaysOwnForTheBodyItNames checks the feedback a bare
// gamemode under as sends: the "your own" wording is the target being the
// body the @s names — Beta, not the runner — so the runner reads the self
// line and the body is not told its mode changed.
func TestExecuteAsGamemodeSaysOwnForTheBodyItNames(t *testing.T) {
	t.Parallel()
	srv, alpha, beta, _ := startExecuteWorldForTwo(t, "Alpha", "Beta")

	alpha.ChatCommand("execute as Beta run gamemode creative")
	servertest.AwaitCommandFeedbackText(t, alpha, "commands.gamemode.success.self", protocol.Text{Translate: "gameMode.creative"})
	if got := srv.PlayerGameMode("Beta"); got != protocol.GameModeCreative {
		t.Errorf("Beta's game mode = %d, want %d (creative)", got, protocol.GameModeCreative)
	}
	expectNoSystemChat(t, beta)
}

// TestExecuteGamemodeSaysOwnForTheRunner checks the "your own game mode"
// wording when the body an execute left on the source is the runner's own:
// an at that moves the chain alone, and an as chain that comes back to the
// runner. The runner reads the self line and is not told gameMode.changed,
// and the log echoes the self wording under their name rather than the
// "Set Probe's game mode" one. vanilla: GameModeCommand.logGamemodeChange.
// Not parallel: it swaps the default logger.
func TestExecuteGamemodeSaysOwnForTheRunner(t *testing.T) {
	logs := &servertest.SyncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	srv, c := servertest.StartExecuteWorld(t, "Probe")
	srv.SpawnMob("minecraft:pig", 4.5, 64, 4.5)
	c.Settle()

	// An at moves only where the chain reads the world from: the source's
	// body stays Probe, so naming Probe back is the runner's own body.
	c.ChatCommand("execute at @e[type=minecraft:pig,limit=1,sort=nearest] run gamemode adventure Probe")
	servertest.AwaitCommandFeedbackText(t, c, "commands.gamemode.success.self", protocol.Text{Translate: "gameMode.adventure"})
	if got := srv.PlayerGameMode("Probe"); got != protocol.GameModeAdventure {
		t.Errorf("Probe's game mode after the at = %d, want adventure", got)
	}
	expectNoSystemChat(t, c)

	// An as chain that ends back on Probe makes the @s the runner again,
	// two wrapper levels deep.
	c.ChatCommand("execute as @e[type=minecraft:pig,limit=1] at @s as Probe run gamemode creative @s")
	servertest.AwaitCommandFeedbackText(t, c, "commands.gamemode.success.self", protocol.Text{Translate: "gameMode.creative"})
	if got := srv.PlayerGameMode("Probe"); got != protocol.GameModeCreative {
		t.Errorf("Probe's game mode after the as chain = %d, want creative", got)
	}
	expectNoSystemChat(t, c)

	got := logs.String()
	for _, want := range []string{
		`msg="[Probe: Set own game mode to Adventure Mode]"`,
		`msg="[Probe: Set own game mode to Creative Mode]"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the log lacks the self echo %s: %s", want, got)
		}
	}
	if strings.Contains(got, "[Probe: Set Probe's game mode") {
		t.Errorf("the log used the other wording for the runner's own body: %s", got)
	}
}

// TestExecuteRunTimeRefusalsWaitForTheChain checks that an unknown dimension
// and an unsummonable type are refused only once the chain reaches them: a
// chain that has no context left, or has forked, stays silent, and one with
// a context still standing hears the bare message, as does the terminal if
// dimension and a clone from an unknown dimension.
// vanilla: BuildContexts.execute, DimensionArgument.getDimension,
// ResourceArgument.getSummonableEntityType
func TestExecuteRunTimeRefusalsWaitForTheChain(t *testing.T) {
	t.Parallel()
	_, c := servertest.StartExecuteWorld(t, "Testificate")

	for _, command := range []string{
		"execute if entity @e[tag=nobody] in minecraft:foo run say hi",
		"execute as @a in minecraft:foo run say hi",
		"execute if entity @e[tag=nobody] run summon minecraft:player",
		"execute if entity @e[tag=nobody] if dimension minecraft:foo run say hi",
		"execute as @a if dimension minecraft:foo run say hi",
	} {
		c.ChatCommand(command)
		expectNoSystemChat(t, c)
	}

	for _, tc := range []struct{ command, key, with string }{
		{"execute in minecraft:foo run say hi", "argument.dimension.invalid", "minecraft:foo"},
		{"execute if dimension minecraft:foo", "argument.dimension.invalid", "minecraft:foo"},
		{"execute positioned ~ ~ ~ run summon minecraft:player", "entity.not_summonable", "minecraft:player"},
		{"clone from minecraft:foo 0 64 0 0 64 0 4 64 0", "argument.dimension.invalid", "minecraft:foo"},
		{"clone 0 64 0 0 64 0 to minecraft:foo 4 64 0", "argument.dimension.invalid", "minecraft:foo"},
	} {
		c.ChatCommand(tc.command)
		if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest(tc.key, tc.with)) {
			t.Errorf("%q answered % X, want the bare %s", tc.command, got, tc.key)
		}
	}
	c.Disconnect()
}

// TestExecuteIfItemsReadsAnItemEntityAndAMobsArmour checks that an item
// entity answers its stack as contents and a mob answers its worn pieces by
// the armor words.
func TestExecuteIfItemsReadsAnItemEntityAndAMobsArmour(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")

	srv.SpawnItemEntity(0.5, 64, 6.5, "minecraft:stone", 3)
	cow := srv.SpawnMob("minecraft:cow", 0.5, 64, 8.5)
	helmet, ok := registry.ItemID("minecraft:iron_helmet")
	if !ok {
		t.Fatal("no iron_helmet item")
	}
	srv.SetMobWorn(cow, 3, protocol.ItemStack{ItemID: helmet, Count: 1})
	c.Settle()

	c.ChatCommand("execute if items entity @e[type=minecraft:item] contents minecraft:stone")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(3)) {
		t.Errorf("the item entity's contents answered % X, want its three stone", got)
	}

	c.ChatCommand("execute if items entity @e[type=minecraft:cow] armor.head minecraft:iron_helmet")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
		t.Errorf("the cow's armor.head answered % X, want its one helmet", got)
	}

	c.ChatCommand("execute if items entity @e[type=minecraft:cow] armor.* *")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
		t.Errorf("the cow's armor.* answered % X, want its one worn piece", got)
	}

	for _, refused := range []string{
		"execute if items entity @e[type=minecraft:cow] armor.chest *",
		"execute if items entity @e[type=minecraft:cow] contents *",
		"execute if items entity @e[type=minecraft:item] armor.head *",
	} {
		c.ChatCommand(refused)
		if got := c.ExpectSystemChatPastMobs(); bytes.Equal(got, conditionalPassCountForTest(1)) {
			t.Errorf("%q passed, want a refusal", refused)
		}
	}

	c.ChatCommand(`summon minecraft:armor_stand 2.5 64 2.5 {equipment:{head:{id:"minecraft:iron_helmet",count:1}}}`)
	c.Settle()
	c.ChatCommand("execute if items entity @e[type=minecraft:armor_stand] armor.head minecraft:iron_helmet")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
		t.Errorf("the stand's armor.head answered % X, want its one helmet", got)
	}
	c.Disconnect()
}

// TestExecuteIfBlocksComparesBlockEntityData checks that two cells with the
// same block state but different block entity data do not match, and that
// identical data matches whatever the positions.
// vanilla: ExecuteCommand.checkRegions
func TestExecuteIfBlocksComparesBlockEntityData(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")
	// A spawner near a player counts its Delay down each tick, so a tick
	// between the two setblocks would leave their data unequal.
	srv.StopTick()

	c.ChatCommand(`setblock 4 70 4 minecraft:spawner{SpawnData:{entity:{id:"minecraft:skeleton"}}}`)
	c.ChatCommand(`setblock 8 70 8 minecraft:spawner{SpawnData:{entity:{id:"minecraft:skeleton"}}}`)
	c.Drain()
	c.ChatCommand("execute if blocks 4 70 4 4 70 4 8 70 8 all")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, conditionalPassCountForTest(1)) {
		t.Errorf("equal block entities answered % X, want a pass counting one", got)
	}

	c.ChatCommand(`setblock 8 70 8 minecraft:spawner{SpawnData:{entity:{id:"minecraft:zombie"}}}`)
	c.Drain()
	c.ChatCommand("execute if blocks 4 70 4 4 70 4 8 70 8 all")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("differing block entity data answered % X, want the conditional failure", got)
	}
}

// TestExecuteForkedGamemodeTellsTheRunnerWhoChanged checks that a gamemode
// run for a named player under an as over a mob still hands the runner the
// "other" success line: a fork silences only a tail's failures.
func TestExecuteForkedGamemodeTellsTheRunnerWhoChanged(t *testing.T) {
	t.Parallel()
	srv, alpha, beta, _ := startExecuteWorldForTwo(t, "Alpha", "Beta")
	_ = beta

	srv.SpawnMob("minecraft:pig", 3.5, 64, 0.5)
	alpha.Settle()

	alpha.ChatCommand("execute as @e[type=minecraft:pig,limit=1] run gamemode creative Beta")
	var skipped []servertest.PushedPacket
	defer func() { alpha.Unread(skipped...) }()
	for id, data := range alpha.Stream() {
		if id == protocol.IDPlaySystemChat && bytes.Contains(data, []byte("commands.gamemode.success.other")) {
			if got := srv.PlayerGameMode("Beta"); got != protocol.GameModeCreative {
				t.Errorf("Beta's game mode = %d, want %d (creative)", got, protocol.GameModeCreative)
			}
			return
		}
		skipped = append(skipped, servertest.PushedPacket{Id: id, Data: data})
	}
	t.Fatal("the runner heard no commands.gamemode.success.other")
}

// TestExecuteOnFollowsEntityRelations covers /execute on: the related body
// becomes the executor, and nobody found drops the branch silently.
func TestExecuteOnFollowsEntityRelations(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")
	servertest.HoldTicks(t, srv)
	pigID := srv.SpawnMob("minecraft:pig", 4.5, 64, 4.5)
	srv.SetMobNoAI(pigID)
	tamed := srv.SpawnMob("minecraft:wolf", 8.5, 64, 8.5)
	wild := srv.SpawnMob("minecraft:wolf", 12.5, 64, 12.5)
	zombie := srv.SpawnMob("minecraft:zombie", 16.5, 64, 16.5)
	for _, id := range []int32{tamed, wild, zombie} {
		srv.SetMobNoAI(id)
	}
	srv.TameMobFor(tamed, "Testificate")
	srv.SetMobTargetPlayer(zombie, "Testificate")
	c.Settle()

	say := func(who any) []byte { return sayPayloadForTest(who, "x") }
	mobText := func(id int32, typ, key string) protocol.Text {
		raw, _ := srv.MobUUID(id)
		return servertest.EntityDisplayComponent(typ, key, uuid.MustParse(raw))
	}
	expectSay := func(want []byte) {
		t.Helper()
		if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, want) {
			t.Fatalf("said % X, want % X", got, want)
		}
	}
	// A silent branch: the next say is the probe's, nothing came before.
	expectSilence := func(command string) {
		t.Helper()
		c.ChatCommand(command)
		c.ChatCommand("say x")
		expectSay(say("Testificate"))
	}

	c.ChatCommand("ride @s mount @e[type=minecraft:pig,limit=1]")
	c.Settle()
	pig := mobText(pigID, "minecraft:pig", "entity.minecraft.pig")
	c.ChatCommand("execute on vehicle run say x")
	expectSay(say(pig))
	c.ChatCommand("execute as @e[type=minecraft:pig,limit=1] on passengers run say x")
	expectSay(say("Testificate"))
	c.ChatCommand("execute as @e[type=minecraft:pig,limit=1] on controller run say x")
	expectSay(say("Testificate"))
	expectSilence("execute as @e[type=minecraft:pig,limit=1] on owner run say x")

	c.ChatCommand("execute as @e[type=minecraft:wolf,sort=nearest,limit=1,x=8,y=64,z=8] on owner run say x")
	expectSay(say("Testificate"))
	expectSilence("execute as @e[type=minecraft:wolf,sort=furthest,limit=1,x=8,y=64,z=8] on owner run say x")

	c.ChatCommand("execute as @e[type=minecraft:zombie] on target run say x")
	expectSay(say("Testificate"))

	_ = srv.ShootPlayerArrow("Testificate", 0.5, 70, 0.5, 0, 0, 0)
	c.ChatCommand("execute as @e[type=minecraft:arrow] on origin run say x")
	if got := c.AwaitPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, say("Testificate")) {
		t.Fatalf("the arrow's origin said % X, want the shooter", got)
	}

	srv.SpawnPlayerTNT("Testificate", 0.5, 90, 0.5, 400)
	c.ChatCommand("execute as @e[type=minecraft:tnt] on origin run say x")
	if got := c.AwaitPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, say("Testificate")) {
		t.Fatalf("the primed TNT's origin said % X, want the igniter", got)
	}
}

func TestExecuteOnAttackerSurvivesRetaliation(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")
	servertest.HoldTicks(t, srv)
	zombie := srv.SpawnMob("minecraft:zombie", 2.5, 64, 0.5)
	c.Attack(zombie)
	c.Settle()
	if health, ok := srv.MobHealth(zombie); !ok || health >= 20 {
		t.Fatalf("the attack did not land: health=%v, present=%v", health, ok)
	}
	servertest.UntilTicks(t, srv, 20, "the zombie to retaliate", func() bool {
		return srv.MobTargetID(zombie) != 0
	})
	c.ChatCommand("execute as @e[type=minecraft:zombie] on attacker run say remembered")
	if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "remembered")) {
		t.Fatalf("attacker after retaliation said % X", got)
	}
	servertest.RunTicks(t, srv, 101)
	if health, ok := srv.MobHealth(zombie); !ok || health <= 0 {
		t.Fatalf("the zombie did not survive to expiry: health=%v, present=%v", health, ok)
	}
	c.ChatCommand("execute as @e[type=minecraft:zombie] on attacker run say expired")
	c.ChatCommand("say probe")
	if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "probe")) {
		t.Fatalf("expired attacker still executed: % X", got)
	}
}

func TestExecuteOnMinecartAndProjectileRelations(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")
	servertest.HoldTicks(t, srv)
	srv.SpawnMinecart(0.5, 64, 0.5)
	c.ChatCommand("ride @s mount @e[type=minecraft:minecart,limit=1]")
	c.Settle()
	c.ChatCommand("execute as @e[type=minecraft:minecart] on passengers run say passenger")
	if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "passenger")) {
		t.Fatalf("cart passenger said % X", got)
	}
	c.ChatCommand("execute as @e[type=minecraft:minecart] on controller run say controller")
	c.ChatCommand("say probe")
	if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "probe")) {
		t.Fatalf("cart controller unexpectedly executed: % X", got)
	}
	srv.SpawnPlayerFirework("Testificate", 0.5, 70, 0.5)
	c.ChatCommand("execute as @e[type=minecraft:firework_rocket] on origin run say rocket")
	if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "rocket")) {
		t.Fatalf("rocket origin said % X", got)
	}
	c.ChatCommand("ride @s dismount")
	srv.GiveSlot("Testificate", 36, protocol.ItemStack{ItemID: int32(registry.ItemFishingRod), Count: 1})
	servertest.UseHeld(c, 0, 0)
	c.Settle()
	c.ChatCommand("execute as @e[type=minecraft:fishing_bobber] on origin run say angler")
	if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "angler")) {
		t.Fatalf("bobber origin said % X", got)
	}
}

func TestExecuteOnItemOriginAndCartMobPassenger(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Testificate", 36, protocol.ItemStack{ItemID: int32(registry.ItemStick), Count: 1})
	c.DropKey(false)
	c.Settle()
	c.ChatCommand("execute as @e[type=minecraft:item] on origin run say dropper")
	if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "dropper")) {
		t.Fatalf("item origin said % X", got)
	}
	srv.SpawnMinecart(0.5, 64, 0.5)
	c.ChatCommand("summon minecraft:pig 0.5 64 0.5")
	c.ChatCommand("ride @e[type=pig,limit=1] mount @e[type=minecart,limit=1]")
	c.Settle()
	c.ChatCommand("execute as @e[type=minecraft:minecart] on passengers run tag @s add rode")
	c.ExpectPlay(protocol.IDPlaySystemChat)
	c.ChatCommand("execute if entity @e[type=pig,tag=rode]")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, conditionalPassCountForTest(1)) {
		t.Fatalf("cart mob passenger not reached: % X", got)
	}
}
