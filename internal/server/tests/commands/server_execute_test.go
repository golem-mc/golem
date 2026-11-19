package commands

import (
	"bytes"
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
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
