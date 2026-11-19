package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"math"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The natural spawning tests: the pass that fills the dark with monsters,
// driven the way every other test drives the server — through a real
// connection — with the passes themselves run by hand. The main loop is
// stopped for them and each pass takes a seed of its own, so a run is the
// same run every time: the flat world the harness grows, the player standing
// where the join puts them, the clock set where the test wants it, and the
// dice seeded. The command tests keep the loop running, being about what a
// command says and does rather than about what the world grows.

// midnight and noon are the two times of day the tests set the clock to:
// midnight is the darkest hour the day timeline carries and noon the
// brightest.
const (
	midnight = 18000
	noon     = 6000
)

// spawnPasses bounds how many seeded passes a test throws at the world before
// it gives up on a zombie: each pass is cheap, and the rate a flat world's
// night produces monsters at leaves a few hundred passes a comfortable
// margin over what a run needs.
const spawnPasses = 400

// zombieTests is the flat world's own fixture: the player stands where the
// join puts them, at the centre of the world spawn's own chunk, and the
// server's main loop is stopped so that only the passes the test runs by hand
// decide what the world grows.
func zombieTests(t *testing.T, cfg *config.Config, setup ...func(*server.Server)) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, cfg, "Watcher", setup...)
	servertest.AwaitChunksSettled(t, srv)
	srv.StopTick()
	return srv, c
}

// TestMonstersDoNotSpawnOutsideTheWorldBorder runs the midnight passes inside a
// 96-block border and expects every monster in the chunks wholly inside it
// (blocks -32..47).
func TestMonstersDoNotSpawnOutsideTheWorldBorder(t *testing.T) {
	t.Parallel()
	srv, _ := zombieTests(t, servertest.SpawningTestConfig())
	srv.SetOverworldBorder(0.5, 0.5, 96)
	srv.SetDayTime(midnight)

	for seed := int64(1); seed <= spawnPasses; seed++ {
		srv.SpawnPass(seed)
	}
	mobs := srv.Mobs()
	if len(mobs) == 0 {
		t.Fatal("no monster spawned inside the border")
	}
	for _, m := range mobs {
		if m.Vehicle != 0 {
			continue
		}
		if m.X < -32 || m.X >= 48 || m.Z < -32 || m.Z >= 48 {
			t.Errorf("%s spawned at (%.1f, %.1f), outside the chunks wholly inside the border", m.Kind, m.X, m.Z)
		}
	}
}

// TestZombiesSpawnAtNight runs the spawn pass through a midnight and expects
// the flat world's dark surface to grow monsters: the biome's list names
// zombie, spider, skeleton and creeper, the hour's darkness lets the surface
// through the light check, and the ground under the open sky holds them.
// Every monster the passes stood up is one the plains list names,
// standing on the surface, further from the player than the twenty-four
// blocks the distance rule keeps clear and inside the hundred and
// twenty-eight a monster may be spawned at. A jockey's finalize stands its
// own bodies up too — the spider's skeleton rider aloft, the baby zombie's
// jockey hen on the ground — and the assertion follows them alongside.
func TestZombiesSpawnAtNight(t *testing.T) {
	t.Parallel()
	srv, _ := zombieTests(t, servertest.SpawningTestConfig())
	srv.SetDayTime(midnight)

	for seed := int64(1); seed <= spawnPasses; seed++ {
		srv.SpawnPass(seed)
	}
	mobs := srv.Mobs()
	if len(mobs) == 0 {
		t.Fatal("no monster spawned in a midnight's worth of passes")
	}
	spawnable := map[string]bool{
		"minecraft:zombie":   true,
		"minecraft:spider":   true,
		"minecraft:creeper":  true,
		"minecraft:skeleton": true,
		// The plains list's weight-10 enderman.
		"minecraft:enderman": true,
		// The plains list's weight-5 zombie villager.
		"minecraft:zombie_villager": true,
		// The plains list's weight-5 witch.
		"minecraft:witch": true,
		// The plains list's weight-5 zombie horse.
		"minecraft:zombie_horse": true,
		// The hen a baby zombie's finalize stood beneath it as a jockey.
		"minecraft:chicken": true,
	}
	const px, py, pz = 0.5, 64.0, 0.5
	for _, m := range mobs {
		if !spawnable[m.Kind] {
			t.Errorf("a pass grew a %s, want one of the monsters the server simulates", m.Kind)
		}
		if m.Vehicle != 0 {
			// A jockey rider rides where its vehicle's back holds it,
			// aloft off the surface the spawn put them on.
			continue
		}
		if m.Y != 64 {
			t.Errorf("a zombie stands at y=%v, want the flat surface's 64", m.Y)
		}
		dx, dz := m.X-px, m.Z-pz
		distSq := dx*dx + dz*dz
		if distSq <= 576 {
			t.Errorf("a zombie stands %.2f blocks from the player, inside the 24 the spawn rule keeps clear", math.Sqrt(distSq))
		}
		if distSq > 128*128 {
			t.Errorf("a zombie stands %.2f blocks from the player, outside the 128 a spawn may be at", math.Sqrt(distSq))
		}
	}
}

// TestSpawnPassReachesSimulationDistance pins the reach of the natural spawn
// to the server's simulation distance rather than to what the client asked to
// be sent: the test's client asks for four chunks of world while the
// simulation distance is the default ten, and a midnight's passes are
// expected both to hold the simulation distance's world for the player and to
// grow monsters in chunks the client never asked for - the ones the server
// keeps loaded for the world's own sake. A chunk is counted beyond the ask
// when it stands outside the four-chunk square the login's render distance
// covers.
func TestSpawnPassReachesSimulationDistance(t *testing.T) {
	t.Parallel()
	cfg := servertest.SpawningTestConfig()
	// The simulation distance the harness would have taken down to the
	// render distance stays at its default ten: the whole point of the test
	// is the reach the server keeps when the client asks for less.
	srv, _ := zombieTests(t, cfg, servertest.FullSimulationDistance)
	srv.SetDayTime(midnight)

	// The holds the load radius keeps are taken after the batch that ends
	// the loading screen, so they are waited for before the passes run.
	servertest.WaitFor(t, srv, 600, func() bool { return srv.LoadedChunks() == 441 })
	srv.SetDayTime(midnight)

	for seed := int64(1); seed <= spawnPasses; seed++ {
		srv.SpawnPass(seed)
	}
	beyond := 0
	for _, m := range srv.Mobs() {
		cx := world.ChunkOf(m.X)
		cz := world.ChunkOf(m.Z)
		if cx < -4 || cx > 4 || cz < -4 || cz > 4 {
			beyond++
		}
	}
	if beyond == 0 {
		t.Fatal("no monster stands beyond the four chunks the client asked for, want the passes to reach the simulation distance")
	}
}

// TestZombiesDoNotSpawnInDaylight runs the same passes at noon and expects
// nothing: the surface's sky light with the day's darkening taken off is
// brighter than any roll of the spawn light test, and the random sky-light
// die alone cannot carry a spawn.
func TestZombiesDoNotSpawnInDaylight(t *testing.T) {
	t.Parallel()
	srv, _ := zombieTests(t, servertest.SpawningTestConfig())
	srv.SetDayTime(noon)

	for seed := int64(1); seed <= spawnPasses; seed++ {
		srv.SpawnPass(seed)
	}
	if mobs := srv.Mobs(); len(mobs) != 0 {
		t.Errorf("%d monsters spawned at noon, want none", len(mobs))
	}
}

// TestZombiesDoNotSpawnOnPeaceful sets the difficulty to peaceful and runs
// the midnight passes: the spawn rules refuse every monster before the dice
// are even thrown at a place for it.
func TestZombiesDoNotSpawnOnPeaceful(t *testing.T) {
	t.Parallel()
	srv, _ := zombieTests(t, servertest.SpawningTestConfig())
	srv.SetDifficulty(0)
	srv.SetDayTime(midnight)

	for seed := int64(1); seed <= spawnPasses; seed++ {
		srv.SpawnPass(seed)
	}
	if mobs := srv.Mobs(); len(mobs) != 0 {
		t.Errorf("%d monsters spawned on peaceful, want none", len(mobs))
	}
}

// TestZombiesSpawnOnlyInDarkness puts a light source down inside the ground
// the spawns land on, runs the midnight passes, and expects the monsters to
// have kept out of its light: the block light the source spreads is above the
// limit the dimension type carries, which is no light at all, so every spawn
// the passes managed is one in a cell the light has not reached. The dark
// rest of the field still grows its zombies, which is what makes the
// assertion about the light rather than about nothing spawning at all.
func TestZombiesSpawnOnlyInDarkness(t *testing.T) {
	t.Parallel()
	srv, _ := zombieTests(t, servertest.SpawningTestConfig())
	srv.SetDayTime(midnight)

	glowstone, ok := registry.FindState("minecraft:glowstone", nil)
	if !ok {
		t.Fatal("the block registry has no glowstone")
	}
	if !srv.SetBlock(32, 64, 0, glowstone) {
		t.Fatal("the glowstone's chunk was not loaded")
	}
	srv.RelightAt(32, 0)

	for seed := int64(1); seed <= spawnPasses; seed++ {
		srv.SpawnPass(seed)
	}
	mobs := srv.Mobs()
	if len(mobs) == 0 {
		t.Fatal("no monster spawned beside a lit patch, want the dark rest of the field to grow some")
	}
	for _, m := range mobs {
		if light := srv.BlockLightAt(int32(math.Floor(m.X)), int32(math.Floor(m.Y)), int32(math.Floor(m.Z))); light != 0 {
			t.Errorf("a zombie spawned in block light %d, want only the unlit cells", light)
		}
	}
}

// TestMonsterSpawnCapStopsSpawning runs the midnight passes until the world
// is full and then some: the caps the pass counts — seventy monsters for the
// seventeen by seventeen of chunks one player's reach covers, the same
// seventy over each player's own neighbourhood — stop the spawns at that
// share, and no later pass adds another. The cap is read before each chunk's
// dice and the packs are up to four, so the world settles a little over the
// cap rather than exactly on it, the way the vanilla server's own pass does:
// the last pack to be let through is what carries it over. A spider jockey's
// skeleton rider is not one of the pack's four (vanilla's pack count only
// counts the mobs the loop itself stands up), nor is a zombie horse's zombie,
// so riders are left out of the upper bound.
func TestMonsterSpawnCapStopsSpawning(t *testing.T) {
	t.Parallel()
	srv, _ := zombieTests(t, servertest.SpawningTestConfig())
	srv.SetDayTime(midnight)

	for seed := int64(1); seed <= 2000; seed++ {
		srv.SpawnPass(seed)
	}
	// The cap counts the monster category alone, so a baby zombie's jockey
	// hen is not counted.
	monsters, riders := 0, 0
	for _, m := range srv.Mobs() {
		if entity.MonsterKind(entity.MobTypeOf(m.Kind)) {
			monsters++
			if m.Vehicle != 0 {
				riders++
			}
		}
	}
	// The cap counts riders too, so the pack mobs alone stay within 69 + 4.
	if monsters < 70 || monsters-riders > 73 {
		t.Fatalf("the world holds %d monsters (%d riders) after two thousand passes, want the cap's seventy and the last pack's overspill", monsters, riders)
	}
	got := srv.MobCount()
	for seed := int64(2001); seed <= 2100; seed++ {
		srv.SpawnPass(seed)
	}
	if still := srv.MobCount(); still != got {
		t.Errorf("the world holds %d monsters after the cap was reached, want it held at %d", still, got)
	}
}

// The summon tests. A command runs as an operator here, the way the vanilla
// server's gamemasters' level asks for; the ops.json the list is read from is
// the one the operator tests write.

// TestSummonSpawnsZombieWithFeedback runs /summon as an operator and expects
// the zombie it names to stand three blocks south of them, shown to them by
// the spawn the mobs are shown by, with the success line vanilla's own
// command answers with — the line naming what stood up by the name the client
// renders its kind with.
func TestSummonSpawnsZombieWithFeedback(t *testing.T) {
	t.Parallel()
	cfg := servertest.SpawningTestConfig()
	srv, c := servertest.Join(t, cfg, "Op", servertest.WriteOpsFile(t, servertest.OpRecord{
		UUID:  protocol.OfflineUUID("Op").String(),
		Name:  "Op",
		Level: 4,
	}))

	c.ChatCommand("summon minecraft:zombie ~ ~ ~3")

	// The feedback and what the command did both ride the mailbox; the
	// reading steps over to the feedback with everything else kept.
	servertest.AwaitSummonFeedback(t, c, "minecraft:zombie", "entity.minecraft.zombie")

	data := c.ExpectPlay(protocol.IDPlaySpawnEntity)
	r := protocol.NewReader(data)
	r.ReadVarInt() // the entity ID, which is the server's own to hand out
	r.ReadUUID()   // the mob's own identity, a fresh one like every spawn's
	if got := r.ReadVarInt(); got != protocol.EntityTypeZombie {
		t.Errorf("spawn entity type = %d, want %d", got, protocol.EntityTypeZombie)
	}
	x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
	if x != 0.5 || y != 64 || z != 3.5 {
		t.Errorf("spawn entity position = (%v, %v, %v), want (0.5, 64, 3.5)", x, y, z)
	}

	mobs := srv.Mobs()
	if len(mobs) != 1 {
		t.Fatalf("%d mobs stand in the world after the summon, want the one", len(mobs))
	}
	// Where it stands now is not asked after: the spawn above pinned where
	// it stood up, and a tick of its own may have walked it on since.
	if mobs[0].Kind != "minecraft:zombie" {
		t.Errorf("the summon stood up a %s, want a zombie", mobs[0].Kind)
	}

	c.Disconnect()
}

// TestSummonCentresWholeCoordinates summons at absolute coordinates: like
// vanilla's Vec3Argument, a whole x or z moves to the middle of its block,
// while the y and a coordinate written with a '.' stay as written.
func TestSummonCentresWholeCoordinates(t *testing.T) {
	t.Parallel()
	cfg := servertest.SpawningTestConfig()
	_, c := servertest.Join(t, cfg, "Op", servertest.WriteOpsFile(t, servertest.OpRecord{
		UUID:  protocol.OfflineUUID("Op").String(),
		Name:  "Op",
		Level: 4,
	}))

	c.ChatCommand("summon minecraft:zombie -3 64 2.0")
	servertest.AwaitSummonFeedback(t, c, "minecraft:zombie", "entity.minecraft.zombie")

	r := protocol.NewReader(c.ExpectPlay(protocol.IDPlaySpawnEntity))
	r.ReadVarInt()
	r.ReadUUID()
	r.ReadVarInt()
	x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
	if x != -2.5 || y != 64 || z != 2 {
		t.Errorf("spawn entity position = (%v, %v, %v), want (-2.5, 64, 2)", x, y, z)
	}

	c.Disconnect()
}

// TestSummonReadsNBTAndSpawns runs the summon with a compound tag after the
// position, which the vanilla argument reads: the tag's shape is asked after
// and its fields are set on the mob, and the summon stands it up all the
// same, the success line naming it by its kind where the tag names no name.
func TestSummonReadsNBTAndSpawns(t *testing.T) {
	t.Parallel()
	cfg := servertest.SpawningTestConfig()
	_, c := servertest.Join(t, cfg, "Op", servertest.WriteOpsFile(t, servertest.OpRecord{
		UUID:  protocol.OfflineUUID("Op").String(),
		Name:  "Op",
		Level: 4,
	}))

	c.ChatCommand(`summon minecraft:zombie ~ ~ ~3 {NoAI:1b}`)

	servertest.AwaitSummonFeedback(t, c, "minecraft:zombie", "entity.minecraft.zombie")
	c.ExpectPlay(protocol.IDPlaySpawnEntity)

	c.Disconnect()
}

// TestSummonAcceptsTrailingCommaInNBT runs the summon with a comma before the
// compound's close, which the SNBT grammar allows.
func TestSummonAcceptsTrailingCommaInNBT(t *testing.T) {
	t.Parallel()
	cfg := servertest.SpawningTestConfig()
	_, c := servertest.Join(t, cfg, "Op", servertest.WriteOpsFile(t, servertest.OpRecord{
		UUID:  protocol.OfflineUUID("Op").String(),
		Name:  "Op",
		Level: 4,
	}))

	c.ChatCommand(`summon minecraft:zombie ~ ~ ~3 {NoAI:1b,}`)

	servertest.AwaitSummonFeedback(t, c, "minecraft:zombie", "entity.minecraft.zombie")
	c.ExpectPlay(protocol.IDPlaySpawnEntity)

	c.Disconnect()
}

// TestSummonedItemsAreNamedByTheirStack summons item entities, whose success
// line names them the way ItemEntity's name is its stack's: a block item by
// the block's translation, and a tag carrying no stack at all by the air the
// vanilla entity holds for its one tick, a success rather than a refusal.
func TestSummonedItemsAreNamedByTheirStack(t *testing.T) {
	t.Parallel()
	cfg := servertest.SpawningTestConfig()
	_, c := servertest.Join(t, cfg, "Op", servertest.WriteOpsFile(t, servertest.OpRecord{
		UUID:  protocol.OfflineUUID("Op").String(),
		Name:  "Op",
		Level: 4,
	}))

	c.ChatCommand(`summon minecraft:item ~ ~ ~3 {Item:{id:"minecraft:dirt",count:2}}`)
	servertest.AwaitSummonFeedback(t, c, "minecraft:item", "block.minecraft.dirt")
	c.ChatCommand(`summon minecraft:item ~ ~ ~3 {Item:{id:"minecraft:diamond",count:1}}`)
	servertest.AwaitSummonFeedback(t, c, "minecraft:item", "item.minecraft.diamond")
	c.ChatCommand(`summon minecraft:item ~ ~ ~3`)
	servertest.AwaitSummonFeedback(t, c, "minecraft:item", "block.minecraft.air")

	c.Disconnect()
}

// TestSummonedItemsResolveAShortItemID summons items whose Item id carries no
// namespace, which the vanilla codec defaults to minecraft: the success line
// still names the block, and the entities stand with the stack the tag named,
// the count defaulting to one when the tag leaves it out.
func TestSummonedItemsResolveAShortItemID(t *testing.T) {
	t.Parallel()
	cfg := servertest.SpawningTestConfig()
	srv, c := servertest.Join(t, cfg, "Op", servertest.WriteOpsFile(t, servertest.OpRecord{
		UUID:  protocol.OfflineUUID("Op").String(),
		Name:  "Op",
		Level: 4,
	}))

	c.ChatCommand(`summon minecraft:item ~ ~ ~3 {Item:{id:"stone",count:5}}`)
	servertest.AwaitSummonFeedback(t, c, "minecraft:item", "block.minecraft.stone")
	c.ChatCommand(`summon minecraft:item ~ ~ ~10 {Item:{id:"stone"}}`)
	servertest.AwaitSummonFeedback(t, c, "minecraft:item", "block.minecraft.stone")

	servertest.UntilTicks(t, srv, 200, "condition", func() bool { return srv.ItemCount() == 2 })
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 2 {
		t.Fatalf("item entities in the overworld = %d, want the two summoned", len(items))
	}
	counts := map[int32]int{}
	for _, it := range items {
		if it.Item != "minecraft:stone" {
			t.Errorf("a summoned item holds %q, want minecraft:stone", it.Item)
		}
		counts[it.Count]++
	}
	if counts[5] != 1 || counts[1] != 1 {
		t.Errorf("the summoned item counts = %v, want one five and one defaulted one", counts)
	}

	c.Disconnect()
}

// TestSummonRefusesBadNBT runs the summon with a tail that is no compound at
// all, which the vanilla argument refuses before anything is stood up.
func TestSummonRefusesBadNBT(t *testing.T) {
	t.Parallel()
	cfg := servertest.SpawningTestConfig()
	srv, c := servertest.Join(t, cfg, "Op", servertest.WriteOpsFile(t, servertest.OpRecord{
		UUID:  protocol.OfflineUUID("Op").String(),
		Name:  "Op",
		Level: 4,
	}))

	c.ChatCommand("summon minecraft:zombie ~ ~ ~3 oops")
	// The NBT the summon runs on is read while the command parses, so the
	// refusal is a parse error: the wrapped message and the context line
	// pointing at the word that was meant to be one.
	c.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.nbt.expected.compound"),
		servertest.CommandErrorContextForTest("summon minecraft:zombie ~ ~ ~3 oops", 35))
	if got := srv.MobCount(); got != 0 {
		t.Errorf("%d mobs stand in the world after a refused summon, want none", got)
	}

	c.Disconnect()
}

// TestSummonGiantSpawnsAHundredHealthMob summons the giant and reads its
// spawn packet and its max health.
func TestSummonGiantSpawnsAHundredHealthMob(t *testing.T) {
	t.Parallel()
	cfg := servertest.SpawningTestConfig()
	srv, c := servertest.Join(t, cfg, "Op", servertest.WriteOpsFile(t, servertest.OpRecord{
		UUID:  protocol.OfflineUUID("Op").String(),
		Name:  "Op",
		Level: 4,
	}))

	c.ChatCommand("summon minecraft:giant ~ ~ ~3")
	data := c.ExpectPlay(protocol.IDPlaySpawnEntity)
	r := protocol.NewReader(data)
	r.ReadVarInt()
	r.ReadUUID()
	want, _ := registry.EntityTypeID("minecraft:giant")
	if got := r.ReadVarInt(); got != want {
		t.Errorf("spawn entity type = %d, want the giant's %d", got, want)
	}
	if mobs := srv.Mobs(); len(mobs) != 1 || mobs[0].Kind != "minecraft:giant" {
		t.Fatalf("mobs after the summon = %+v, want the one giant", mobs)
	}

	c.ChatCommand("attribute @e[type=giant,limit=1] minecraft:max_health get")
	var value string
	c.SettleAndCollect(func(packetID int32, data []byte) {
		if packetID != protocol.IDPlaySystemChat {
			return
		}
		if text, ok := protocol.ReadText(protocol.NewReader(data)); ok && text.Translate == "commands.attribute.value.get.success" {
			value = text.With[len(text.With)-1].Text
		}
	})
	if v, err := strconv.ParseFloat(value, 64); err != nil || v != 100 {
		t.Errorf("giant max_health = %q, want 100", value)
	}
	c.Disconnect()
}

// TestSummonRefusesUnsupportedEntities runs the summon at the names this
// server has no mob behind and expects the vanilla server's own refusals: a
// name no entity type answers to is told so by the resource argument's
// message, and the player and the fishing bobber are told so by the
// not-summonable one.
func TestSummonRefusesUnsupportedEntities(t *testing.T) {
	t.Parallel()
	cfg := servertest.SpawningTestConfig()
	srv, c := servertest.Join(t, cfg, "Op", servertest.WriteOpsFile(t, servertest.OpRecord{
		UUID:  protocol.OfflineUUID("Op").String(),
		Name:  "Op",
		Level: 4,
	}))

	for _, tc := range []struct {
		name    string
		command string
		key     string
		with    []string
		// cursor is where the reading stopped, for the refusals that are
		// parse errors and travel with the context line; -1 for the ones
		// that are failures of the command's run and travel alone.
		cursor int
	}{
		{"unknown type", "summon minecraft:notathing", "argument.resource.not_found",
			[]string{"minecraft:notathing", "minecraft:entity_type"}, 26},
		// The summonable check is vanilla's run-time getSummonableEntityType,
		// so its refusal travels without the context line.
		{"the player", "summon minecraft:player", "entity.not_summonable",
			[]string{"minecraft:player"}, -1},
		{"the fishing bobber", "summon minecraft:fishing_bobber", "entity.not_summonable",
			[]string{"minecraft:fishing_bobber"}, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c.ChatCommand(tc.command)
			if tc.cursor < 0 {
				// A summon that parses but cannot put its mob in the world
				// is refused the way every command failure is: inside the
				// empty red component, with no context line, for the
				// reading stopped nowhere.
				c.ExpectCommandFailure(servertest.CommandErrorMessageForTest(tc.key, tc.with...))
				servertest.ExpectNoSystemChat(t, c)
				return
			}
			// The refusals of the reading itself: the wrapped message and
			// the context line pointing where the vanilla parser stopped,
			// at the end of the name it refused.
			c.ExpectCommandError(
				servertest.CommandErrorMessageForTest(tc.key, tc.with...),
				servertest.CommandErrorContextForTest(tc.command, tc.cursor))
		})
	}
	if got := srv.MobCount(); got != 0 {
		t.Errorf("%d mobs stand in the world after refused summons, want none", got)
	}

	c.Disconnect()
}

// TestSummonRefusedToNonOps runs the summon as a player who is no operator:
// the tree they were sent carries no summon at all, so the word is an unknown
// command to them the way it is to a vanilla client, and the server says so
// the same way it says it about any word it does not know. The operator list
// the test points the server at is an empty one, which is what takes the
// floating default level away from everyone it does not name — which is
// everyone.
func TestSummonRefusedToNonOps(t *testing.T) {
	t.Parallel()
	cfg := servertest.SpawningTestConfig()
	srv, c := servertest.Dial(t, cfg, servertest.WriteOpsFile(t, servertest.OpRecord{}))
	c.Login("Nobody")
	c.Configure()

	// The command tree arrives during the join; read it out and check the
	// summon is not in it before the rest of the join is read.
	var nodes []servertest.TestCommandNode
	sawCommands := false
	for packetID, data := range c.Stream() {
		switch packetID {
		case protocol.IDPlayCommands:
			nodes = servertest.DecodeCommands(t, data)
			sawCommands = true
		case protocol.IDPlayChunkBatchFinished:
			t.Fatal("chunk batch finished before commands packet was sent")
		}
		if sawCommands {
			break
		}
	}
	if !sawCommands {
		t.Fatal("read join packet: nothing came")
	}
	for _, node := range nodes {
		if node.Typ == 1 && node.Name == "summon" {
			t.Error("the command tree a non-operator was sent carries a summon node")
		}
	}
	c.EnterWorld("Nobody")

	c.ChatCommand("summon minecraft:zombie")
	c.ExpectCommandError(
		servertest.CommandErrorMessageForTest("command.unknown.command"),
		servertest.CommandErrorContextForTest("summon minecraft:zombie", 0),
	)
	if got := srv.MobCount(); got != 0 {
		t.Errorf("%d mobs stand in the world after a refused summon, want none", got)
	}

	c.Disconnect()
}

// The creature pass tests. Every four hundredth game tick the persistent
// categories join the monsters in the spawn walk, and the animals stand up
// under their own cap of ten and their own rule: the grass block underfoot
// and the undarkened light at the cell over eight — which the noon and the
// midnight pass alike, the check being where the cell stands, not what hour
// it is. The tests run them at noon, where no monster can appear, so the
// herd the passes grow is all theirs.

// creaturePasses bounds how many seeded creature passes a test throws at the
// world: the pass runs once per call, and a flat grassland's noon leaves a
// few hundred calls room to fill the cap over.
const creaturePasses = 400

// creatureTest is the creature pass's own fixture: the same flat world and
// standing player the monster tests use, the loop stopped so the passes the
// test runs by hand decide what the world grows. The flat ground the
// generator fills is stone, which is ground no animal may stand up on — the
// rule asks the grass block — so the fixture lays the grass meadow the
// animals are rolled onto over the reach the passes walk, all of it the
// plains biome the flat generator fills.
func creatureTest(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := zombieTests(t, servertest.SpawningTestConfig())
	srv.SetDayTime(noon)
	// The simulation distance's chunks load behind the join; the grass is
	// laid once they all stand, or the ones that came later stay stone and
	// a slow machine's passes find no grass to roll on.
	servertest.AwaitChunksSettled(t, srv)
	layGrass(t, srv, -79, 79, -79, 79)
	return srv, c
}

// layGrass sets the grass block over the surface of the stretch named, the
// meadow the creature tests roll their spawns onto: the flat world's own
// ground is stone, and the animal rule keeps every creature off it. Columns
// the loaded reach does not cover are left alone, the pass never rolling in
// a chunk it cannot see.
func layGrass(t *testing.T, srv *server.Server, x0, x1, z0, z1 int32) {
	t.Helper()
	grass, ok := registry.FindState("minecraft:grass_block", nil)
	if !ok {
		t.Fatal("the block registry has no grass block")
	}
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			srv.SetBlock(x, 63, z, grass)
		}
	}
}

// TestCreaturesSpawnOnGrass runs the creature pass through a noon on the
// four-hundredth tick and expects the grassland to grow animals: every mob
// the passes stood up is one of the farmyard kinds the plains list names
// and this server simulates, standing on the surface's grass, further from
// the player than the twenty-four blocks the distance rule keeps clear.
func TestCreaturesSpawnOnGrass(t *testing.T) {
	t.Parallel()
	srv, _ := creatureTest(t)

	for seed := int64(1); seed <= creaturePasses; seed++ {
		srv.SpawnCreaturePass(seed)
	}
	mobs := srv.Mobs()
	if len(mobs) == 0 {
		t.Fatal("no creature spawned in a noon's worth of passes")
	}
	spawnable := map[string]bool{
		"minecraft:cow":     true,
		"minecraft:pig":     true,
		"minecraft:sheep":   true,
		"minecraft:chicken": true,
		"minecraft:horse":   true,
		"minecraft:donkey":  true,
	}
	const px, py, pz = 0.5, 64.0, 0.5
	for _, m := range mobs {
		if !spawnable[m.Kind] {
			t.Errorf("the pass grew a %s, want one of the creatures the server simulates", m.Kind)
		}
		if m.Y != 64 {
			t.Errorf("a %s stands at y=%v, want the flat surface's 64", m.Kind, m.Y)
		}
		dx, dz := m.X-px, m.Z-pz
		distSq := dx*dx + dz*dz
		if distSq <= 576 {
			t.Errorf("a %s stands %.2f blocks from the player, inside the 24 the spawn rule keeps clear", m.Kind, math.Sqrt(distSq))
		}
		if name := srv.BlockNameAt(int32(math.Floor(m.X)), 63, int32(math.Floor(m.Z))); name != "minecraft:grass_block" {
			t.Errorf("a %s stands on %s, want the grass block the animal rule asks for", m.Kind, name)
		}
	}
}

// TestCreaturesSpawnFarBelowAHighPlayer lifts the player 170 blocks over the
// meadow: animals may spawn beyond the 128-block despawn distance, a creature
// never being removed when far away, while monsters stay within it.
// vanilla: NaturalSpawner.isValidSpawnPostitionForType,
// NaturalSpawner.isValidPositionForMob
func TestCreaturesSpawnFarBelowAHighPlayer(t *testing.T) {
	t.Parallel()
	srv, c := creatureTest(t)
	srv.SetPlayerGameMode("Watcher", 1)
	const px, py, pz = 0.5, 234.0, 0.5
	c.MoveToGround(px, py, pz, false)
	c.Settle()
	if at := srv.PlayerPosition("Watcher"); at.Y != py {
		t.Fatalf("the player stands at y=%v, want %v", at.Y, py)
	}
	distSq := func(m server.TestMob) float64 {
		dx, dy, dz := m.X-px, m.Y-py, m.Z-pz
		return dx*dx + dy*dy + dz*dz
	}

	for seed := int64(1); seed <= 100; seed++ {
		srv.SpawnCreaturePass(seed)
	}
	creatures := map[int32]bool{}
	far := 0
	for _, m := range srv.Mobs() {
		creatures[m.ID] = true
		if distSq(m) > 128*128 {
			far++
		}
	}
	if far == 0 {
		t.Fatalf("%d creatures spawned, none beyond 128 blocks of the player", len(creatures))
	}

	srv.SetDayTime(midnight)
	for seed := int64(1); seed <= 100; seed++ {
		srv.SpawnPass(seed)
	}
	for _, m := range srv.Mobs() {
		if !creatures[m.ID] && distSq(m) > 128*128 {
			t.Errorf("a %s spawned %.2f blocks from the player, beyond the monsters' 128", m.Kind, math.Sqrt(distSq(m)))
		}
	}
}

// TestCreaturesSpawnOnPeaceful sets the difficulty to peaceful and runs the
// same passes: the animals stand in peaceful worlds as they do in every
// other — the peaceful refusal is the monsters' gate, SpawnPlacements
// keeping it out of the creature rule.
func TestCreaturesSpawnOnPeaceful(t *testing.T) {
	t.Parallel()
	srv, _ := creatureTest(t)
	srv.SetDifficulty(0)

	for seed := int64(1); seed <= creaturePasses; seed++ {
		srv.SpawnCreaturePass(seed)
	}
	if mobs := srv.Mobs(); len(mobs) == 0 {
		t.Fatal("no creature spawned on peaceful, want the animals unhindered by the hour's difficulty")
	}
}

// TestCreaturesSpawnOnlyOnGrass lays a stone floor over a stretch of the
// field, runs the same passes, and expects the animals to have kept off it:
// the block below a spawn must be the grass block the rule names, so every
// spawn the passes managed is one on the grass the stone left alone.
func TestCreaturesSpawnOnlyOnGrass(t *testing.T) {
	t.Parallel()
	srv, _ := creatureTest(t)

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	for x := int32(40); x <= 79; x++ {
		for z := int32(-30); z <= 30; z++ {
			srv.SetBlock(x, 63, z, stone)
		}
	}

	for seed := int64(1); seed <= creaturePasses; seed++ {
		srv.SpawnCreaturePass(seed)
	}
	mobs := srv.Mobs()
	if len(mobs) == 0 {
		t.Fatal("no creature spawned beside the stone floor, want the grass rest of the field to grow some")
	}
	for _, m := range mobs {
		if name := srv.BlockNameAt(int32(math.Floor(m.X)), 63, int32(math.Floor(m.Z))); name != "minecraft:grass_block" {
			t.Errorf("a %s stands on %s, want only the grass the stone left", m.Kind, name)
		}
	}
}

// TestCreatureSpawnCapStopsSpawning runs the creature passes until the world
// is full and then some: the creature category's cap is ten for the
// seventeen by seventeen of chunks one player's reach covers, and the packs
// are four strong, so the world settles a little over the cap the way the
// monsters' does, and no later pass adds another.
func TestCreatureSpawnCapStopsSpawning(t *testing.T) {
	t.Parallel()
	srv, _ := creatureTest(t)

	for seed := int64(1); seed <= 2000; seed++ {
		srv.SpawnCreaturePass(seed)
	}
	got := srv.MobCount()
	if got < 10 || got > 13 {
		t.Fatalf("the world holds %d creatures after two thousand passes, want the cap's ten and the last pack's overspill", got)
	}
	for seed := int64(2001); seed <= 2100; seed++ {
		srv.SpawnCreaturePass(seed)
	}
	if still := srv.MobCount(); still != got {
		t.Errorf("the world holds %d creatures after the cap was reached, want it held at %d", still, got)
	}
}

// TestASummonTagStandsTheBabiesUp runs the summon whose tag stands over the
// spawn with the baby state in it, each kind's tag read the way its own
// readAdditionalSaveData reads it: the zombie's the IsBaby boolean — the
// one its baby-ness is kept under, the Age an animal's tag keeps being no
// field of the zombie's read — and the animals' the Age int. A zombie
// tagged with an Age stands up grown, one tagged IsBaby stands up the baby
// the wire tells the flag and the haste's modifier of, and a chicken tagged
// with the Age of a baby stands up the chick its age says.
func TestASummonTagStandsTheBabiesUp(t *testing.T) {
	t.Parallel()
	cfg := servertest.SpawningTestConfig()
	// Creative, so the zombies never turn on the player, night, so they
	// never catch fire, and every summon on the ground apart from the
	// rest: the aggressive flag a target raises, the burning flag, or the
	// health a fall takes would otherwise land at a time a slow machine
	// decides, perhaps while a later spawn is waited for.
	cfg.GameMode = "creative"
	srv, c := servertest.Dial(t, cfg, servertest.WriteOpsFile(t, servertest.OpRecord{
		UUID:  protocol.OfflineUUID("Op").String(),
		Name:  "Op",
		Level: 4,
	}))
	srv.SetDayTime(midnight)
	c.Join("Op")

	// An Age is no field of the zombie's read: the summon stands the
	// grown zombie up all the same, no flag in the metadata it is shown
	// with.
	c.ChatCommand("summon minecraft:zombie ~4 ~ ~ {Age:-24000}")
	servertest.AwaitSummonFeedback(t, c, "minecraft:zombie", "entity.minecraft.zombie")
	grown := c.ExpectMobSpawn(protocol.EntityTypeZombie)
	if grown.HasBaby {
		t.Error("a zombie tagged with an Age stood up carrying the baby flag")
	}

	// The zombie's own field: the boolean stands a baby up, the flag and
	// the haste's modifier both on the wire the arrival carries.
	c.ChatCommand("summon minecraft:zombie ~-4 ~ ~ {IsBaby:1b}")
	servertest.AwaitSummonFeedback(t, c, "minecraft:zombie", "entity.minecraft.zombie")

	data := c.ExpectMobPacket(protocol.IDPlaySpawnEntity)
	r := protocol.NewReader(data)
	babyID := r.ReadVarInt()
	r.ReadUUID()
	if got := r.ReadVarInt(); got != protocol.EntityTypeZombie {
		t.Errorf("the tagged summon stood up type %d, want %d", got, protocol.EntityTypeZombie)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("the baby zombie's spawn: %v", err)
	}

	meta := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlayEntityMetadata))
	if got := meta.ReadVarInt(); got != babyID {
		t.Fatalf("metadata for entity %d, want the baby zombie's %d", got, babyID)
	}
	babySeen := false
	for meta.Err() == nil {
		index := meta.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		fieldType := meta.ReadVarInt()
		if index == protocol.MetadataIndexBaby {
			if fieldType != protocol.MetadataBoolean {
				t.Fatalf("the baby zombie's field type = %d, want %d (a boolean)", fieldType, protocol.MetadataBoolean)
			}
			if meta.ReadU8() == 0 {
				t.Error("the zombie tagged IsBaby stood up with the flag off")
			}
			babySeen = true
			continue
		}
		if fieldType == protocol.MetadataFloat {
			meta.ReadF32()
			continue
		}
		if fieldType == protocol.MetadataByte {
			meta.ReadU8()
			continue
		}
		t.Fatalf("unexpected metadata field %d of type %d on the baby zombie", index, fieldType)
	}
	if err := meta.Err(); err != nil {
		t.Fatalf("the baby zombie's metadata: %v", err)
	}
	if !babySeen {
		t.Error("the zombie tagged IsBaby stood up with no baby flag in its metadata")
	}

	attrs := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlayUpdateAttributes))
	if got := attrs.ReadVarInt(); got != babyID {
		t.Errorf("attributes for entity %d, want the baby zombie's %d", got, babyID)
	}
	if got := attrs.ReadVarInt(); got != 1 {
		t.Fatalf("attribute count = %d, want the pace's one", got)
	}
	if got := attrs.ReadVarInt(); got != protocol.AttributeMovementSpeed {
		t.Errorf("attribute = %d, want %d (movement_speed)", got, protocol.AttributeMovementSpeed)
	}
	if got := attrs.ReadF64(); got != float64(float32(0.23)) {
		t.Errorf("the baby zombie's base pace = %v, want the vanilla 0.23 the modifier rides", got)
	}
	if got := attrs.ReadVarInt(); got != 1 {
		t.Fatalf("modifier count = %d, want the haste's one", got)
	}
	if name := attrs.ReadString(256); name != "minecraft:baby" {
		t.Errorf("the modifier's name = %q, want the vanilla baby's", name)
	}
	if got := attrs.ReadF64(); got != 0.5 {
		t.Errorf("the baby modifier's amount = %v, want the vanilla half again", got)
	}
	if op := attrs.ReadVarInt(); op != 1 {
		t.Errorf("the baby modifier's operation = %d, want the multiplied base's 1", op)
	}
	if err := attrs.Err(); err != nil {
		t.Fatalf("the baby zombie's attributes: %v", err)
	}
	if attrs.Remaining() != 0 {
		t.Errorf("the baby zombie's attributes have %d bytes left over", attrs.Remaining())
	}

	// The animals keep the Age their read has always read: the chick the
	// minus twenty thousand four hundred stands up is the baby the flag
	// on the wire says.
	c.ChatCommand("summon minecraft:chicken ~ ~ ~4 {Age:-24000}")
	servertest.AwaitSummonFeedback(t, c, "minecraft:chicken", "entity.minecraft.chicken")
	chick := c.ExpectMobSpawn(protocol.EntityTypeChicken)
	if !chick.HasBaby || !chick.Baby {
		t.Errorf("a chicken tagged with a baby's Age stood up baby=%v present=%v, want the flag on", chick.Baby, chick.HasBaby)
	}

	mobs := srv.Mobs()
	if len(mobs) != 3 {
		t.Fatalf("%d mobs stand in the world after the three summons, want the three", len(mobs))
	}
}

// TestSpawnBodiesSkipSpectators checks the bodies the pass measures its mobs
// against: a spectator contributes none, so the pass may stand a mob where
// the spectator floats, while a standing player's box is in the list.
func TestSpawnBodiesSkipSpectators(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, servertest.SpawningTestConfig())

	watching := entity.SpawnPlayer{EntityID: 1, X: 40.5, Y: 64, Z: 40.5, Spectator: true}
	standing := entity.SpawnPlayer{EntityID: 2, X: 1.5, Y: 64, Z: 1.5, Width: entity.PlayerWidth, Height: entity.PlayerHeight}
	bodies := srv.SpawnBodiesForTest([]entity.SpawnPlayer{watching, standing})

	if len(bodies) != 1 {
		t.Fatalf("the body list holds %d bodies, want only the standing player's", len(bodies))
	}
	if bodies[0].MinX != standing.X-entity.PlayerHalf || bodies[0].MinY != standing.Y ||
		bodies[0].MaxX != standing.X+entity.PlayerHalf || bodies[0].MaxY != standing.Y+entity.PlayerHeight {
		t.Errorf("the body is x %v..%v y %v..%v, want the standing player's box",
			bodies[0].MinX, bodies[0].MaxX, bodies[0].MinY, bodies[0].MaxY)
	}
}

// TestSpawnBodiesUsePlayerBox checks that a player's body is the box of their
// pose and scale: a crawler's is 0.6 high, a scaled player's is scaled.
func TestSpawnBodiesUsePlayerBox(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, servertest.SpawningTestConfig())

	crawling := entity.SpawnPlayer{EntityID: 1, X: 1.5, Y: 64, Z: 1.5, Width: 0.6, Height: 0.6}
	big := entity.SpawnPlayer{EntityID: 2, X: 9.5, Y: 64, Z: 9.5, Width: 1.2, Height: 3.6}
	bodies := srv.SpawnBodiesForTest([]entity.SpawnPlayer{crawling, big})
	if len(bodies) != 2 {
		t.Fatalf("%d bodies, want 2", len(bodies))
	}
	for i, p := range []entity.SpawnPlayer{crawling, big} {
		b := bodies[i]
		if math.Abs(b.MaxY-b.MinY-p.Height) > 1e-9 || math.Abs(b.MaxX-b.MinX-p.Width) > 1e-9 {
			t.Errorf("body %d is %v wide %v high, want %v x %v", i, b.MaxX-b.MinX, b.MaxY-b.MinY, p.Width, p.Height)
		}
	}
}

// The water category tests. The two water categories the vanilla walk holds —
// WATER_CREATURE, the squid's, and UNDERGROUND_WATER_CREATURE, the glow
// squid's — are friendly but not persistent, so they join the walk on every
// pass, each under its own cap of five and each with a rule of its own: the
// squid's a band of water heights around the sea level, the glow squid's
// water in the dark thirty-three and more under it. The tests run on the
// seeded overworld: a superflat carries its own sea level of the world's
// floor, a band no squid can reach the water of, while the seeded sea level
// is the overworld's sixty-three.

// waterSeed is the level seed the water spawn tests grow their world from:
// deterministic like every level seed, its dry-land spawn at the origin
// holding an ocean within the spawning reach.
const waterSeedNum int64 = 12

var waterSeed = strconv.FormatInt(waterSeedNum, 10)

// waterSpawningTests is the water categories' own fixture: the seeded
// overworld of waterSeed, the player standing on the spawn the dry land
// at the origin is, the loop stopped and the clock at noon — where the
// monster rules refuse the surface, and the creatures stay off their
// four-hundredth tick — so the passes the test runs by hand leave the water
// categories alone to decide what stands up.
//
// The loop is stopped before the join, not after it, because the join is
// what loads the world: a seeded chunk queues its springs' fluid ticks as
// it loads, and a loop still running would pour them for as many ticks as
// the join took — a machine-dependent spreading of flowing water the
// underground pass stands glow squid up in, beside the squid these tests
// count. The join itself needs no tick; the chunk stream and the
// announcement ride their own goroutines.
// testSeededSpawnWorldFluidsStayStill pins the stillness this buys.
func waterSpawningTests(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := servertest.SpawningTestConfig()
	// The unprefixed "normal" rather than "minecraft:normal": the harness
	// swaps the default spelling of it for its flat test ground.
	cfg.LevelType = "normal"
	cfg.LevelSeed = waterSeed
	// Water spawning needs the generated ocean and aquifers, not structures.
	cfg.GenerateStructures = false
	cfg.LevelName = (servertest.RegionFixture{
		Seed: waterSeedNum,
		Min:  world.ChunkPos{X: -4, Z: -4}, Max: world.ChunkPos{X: 4, Z: 4},
	}).Copy(t)
	srv, connect := servertest.Start(t, cfg)
	srv.StopTick()
	c := connect().Join("Watcher")
	srv.SetDayTime(noon)
	servertest.AwaitChunksSettled(t, srv)
	return srv, c
}

// waterPasses bounds how many seeded passes a water spawn test throws at the
// world: the ocean's list spends most of its rolls on the kinds this server
// does not simulate, so a few hundred passes leave the cap room to fill over.
const waterPasses = 400

// TestSeededWaterSpawning runs the water spawn checks on the seeded ocean.
// The fluid, water creature and fish checks read one server, which runs its
// passes once between them; the glow squid digs its pool into a copy of its
// own.
func TestSeededWaterSpawning(t *testing.T) {
	t.Parallel()
	t.Run("GlowSquidSpawnInDarkDeepWater", func(t *testing.T) {
		t.Parallel()
		testGlowSquidSpawnInDarkDeepWater(t)
	})
	srv, _ := waterSpawningTests(t)
	t.Run("SeededSpawnWorldFluidsStayStill", func(t *testing.T) {
		testSeededSpawnWorldFluidsStayStill(t, srv)
	})
	var herd waterHerd
	for seed := int64(1); seed <= waterPasses; seed++ {
		srv.SpawnPass(seed)
	}
	herd.capped = srv.Mobs()
	for seed := int64(1); seed <= waterPasses; seed++ {
		srv.SpawnPass(waterPasses + seed)
	}
	herd.after = srv.Mobs()
	t.Run("WaterCreaturesSpawnInOcean", func(t *testing.T) {
		testWaterCreaturesSpawnInOcean(t, srv, herd)
	})
	t.Run("FishSpawnInOcean", func(t *testing.T) {
		testFishSpawnInOcean(t, srv, herd)
	})
}

// waterHerd is the seeded ocean's herd after waterPasses passes (seeds 1 on)
// and after as many more (seeds waterPasses+1 on).
type waterHerd struct {
	capped, after []server.TestMob
}

// testSeededSpawnWorldFluidsStayStill pins the foundation the water tests'
// herd counts stand on: the seeded overworld the fixture joins is the one
// the generator wrote, every cell of its water and lava the still source it
// was placed as — the sea, the aquifer's pools and the springs in their rock
// pockets. A world that had been left ticking would hold flowing water the
// springs poured, and flowing cells deep under the ground are dark open
// water the underground pass stands glow squid up in (their cap is their
// own, separate from the squid's), so a herd the squid count reads would
// grow past the cap the test had just watched it reach.
func testSeededSpawnWorldFluidsStayStill(t *testing.T, srv *server.Server) {

	// The spawn pass only walks loaded chunks. Inspect the settled fixture's
	// entire loaded set; the global cap's 17x17 includes unloaded positions.
	// Springs can sit up to y=192 and nothing pours upward.
	const waterScanCeiling = 192
	// Inspect both fluids in one pass: releasing and reacquiring the entire
	// reach for a second scan can regenerate untouched chunks.
	states := map[uint16]string{}
	for _, name := range []string{"minecraft:water", "minecraft:lava"} {
		source := servertest.MustDefaultState(t, name)
		for state := range servertest.FluidStates(t, name) {
			if state != source {
				states[state] = name
			}
		}
	}
	var flowing, waterSources, lavaSources int
	chunks := srv.LoadedChunks()
	srv.ScanLoadedOverworldBlocks(-64, waterScanCeiling,
		func(x, y, z int32, s uint16) {
			switch s {
			case registry.BlockWater.DefaultState():
				waterSources++
			case registry.BlockLava.DefaultState():
				lavaSources++
			}
			if name, ok := states[s]; ok {
				if flowing == 0 {
					t.Errorf("%s flows through the spawn reach at (%d, %d, %d), want every generated fluid the still source it was placed as", name, x, y, z)
				}
				flowing++
			}
		})
	if waterSources == 0 || lavaSources == 0 {
		t.Fatalf("scan found %d water and %d lava sources, want both fluids", waterSources, lavaSources)
	}
	if srv.LoadedChunks() != chunks {
		t.Fatalf("scan changed loaded chunks from %d to %d", chunks, srv.LoadedChunks())
	}
	if flowing > 0 {
		t.Errorf("%d flowing fluid cells in the spawn reach", flowing)
	}
}

// testWaterCreaturesSpawnInOcean checks placement and the shared WATER_CREATURE
// cap: either squid or nautiluses may fill it before the other kind spawns.
func testWaterCreaturesSpawnInOcean(t *testing.T, srv *server.Server, herd waterHerd) {
	waterCreatures := 0
	const px, py, pz = 0.5, 64.0, 0.5
	for _, m := range herd.capped {
		if !entity.WaterCreatureKind(entity.MobTypeOf(m.Kind)) {
			continue
		}
		waterCreatures++
		x, y, z := int32(math.Floor(m.X)), int32(math.Floor(m.Y)), int32(math.Floor(m.Z))
		// The sea's water reads by the rules' own word, which counts a
		// kelp or seagrass cell's fluid as the water it holds.
		if got := srv.BlockAt(x, y, z); !rules.IsWater(got) {
			t.Errorf("a water creature stands at (%d, %d, %d) in block %d, want the sea's water", x, y, z, got)
		}
		minY, maxY := 63-13, 62
		if m.Kind == "minecraft:nautilus" {
			// vanilla: Nautilus.checkNautilusSpawnRules
			minY, maxY = 63-25, 63-5
		}
		if m.Y < float64(minY) || m.Y > float64(maxY) {
			t.Errorf("%s stands at y=%v, want the band [%d, %d]", m.Kind, m.Y, minY, maxY)
		}
		// The distance rule is three-dimensional, from the spawn block's
		// centre in x and z and its y as it stands: a mob below the player
		// can be under 24 blocks away on the flat and still clear of it.
		dx, dy, dz := float64(x)+0.5-px, float64(y)-py, float64(z)+0.5-pz
		distSq := dx*dx + dy*dy + dz*dz
		if distSq <= 576 {
			t.Errorf("a water creature stands %.2f blocks from the player, inside the 24 the spawn rule keeps clear", math.Sqrt(distSq))
		}
		if distSq > 128*128 {
			t.Errorf("a water creature stands %.2f blocks from the player, outside the 128 a spawn may be at", math.Sqrt(distSq))
		}
	}
	if waterCreatures < 5 || waterCreatures > 9 {
		t.Fatalf("the ocean holds %d water creatures after the passes, want the cap's five and the last pack's overspill", waterCreatures)
	}
	if still := waterCreatureCount(herd.after); still != waterCreatures {
		t.Errorf("the ocean holds %d water creatures after the cap was reached, want it held at %d", still, waterCreatures)
	}
}

// waterCreatureCount counts all mobs sharing the WATER_CREATURE cap.
func waterCreatureCount(mobs []server.TestMob) int {
	n := 0
	for _, m := range mobs {
		if entity.WaterCreatureKind(entity.MobTypeOf(m.Kind)) {
			n++
		}
	}
	return n
}

// testGlowSquidSpawnInDarkDeepWater digs a dark water pool thirty-three and
// more below the sea level into the seeded overworld and runs the same
// passes: the underground category's rule — the water in the dark that deep
// — is the only water the passes find there, the seeded ocean's water all
// standing too high for it, so the glow squid it stands up in the pool are
// the test's own, and the water creatures the surface category stands in the
// ocean beside it are the rule's other branch: each bounded by its own cap.
func testGlowSquidSpawnInDarkDeepWater(t *testing.T) {
	srv, _ := waterSpawningTests(t)

	// The pool: two whole chunks' worth of columns, water from the
	// eighteenth block to the twenty-first and air the three above, deep
	// enough under the ground that no sky light reaches it. The chunks sit
	// within the spawning reach of the spawn and clear of the twenty-four
	// blocks its rule keeps empty.
	pos := srv.PlayerPosition("Watcher")
	px0 := int32(math.Floor(pos.X/16))*16 + 32
	pz0 := int32(math.Floor(pos.Z/16))*16 - 16
	drainDeepWater(t, srv, px0, pz0)
	digDeepPool(t, srv, px0, px0+31, pz0, pz0+31, 18)
	for _, x := range []int32{px0 + 8, px0 + 24} {
		for _, z := range []int32{pz0 + 8, pz0 + 24} {
			srv.RelightAt(x, z)
		}
	}

	for seed := int64(1); seed <= waterPasses; seed++ {
		srv.SpawnPass(seed)
	}
	mobs := srv.Mobs()
	glow := 0
	for _, m := range mobs {
		if m.Kind != "minecraft:glow_squid" {
			continue
		}
		glow++
		x, y, z := int32(math.Floor(m.X)), int32(math.Floor(m.Y)), int32(math.Floor(m.Z))
		if got := srv.BlockAt(x, y, z); !rules.IsWater(got) {
			t.Errorf("a glow squid stands at (%d, %d, %d) in block %d, want the pool's water", x, y, z, got)
		}
		if m.Y > 63-33 {
			t.Errorf("a glow squid stands at y=%v, want water %d below the sea level or deeper", m.Y, 63-33)
		}
		if x < px0 || x >= px0+32 || z < pz0 || z >= pz0+32 {
			t.Errorf("a glow squid stands at (%d, %d), want the pool the test dug", x, z)
		}
	}
	if glow == 0 {
		t.Fatal("no glow squid spawned in a dark pool a seeded world of passes rolled over")
	}
	if glow < 5 || glow > 9 {
		t.Fatalf("the pool holds %d glow squid after the passes, want the cap's five and the last pack's overspill", glow)
	}
	for seed := int64(1); seed <= 100; seed++ {
		srv.SpawnPass(waterPasses + seed)
	}
	if still := glowSquidCount(srv.Mobs()); still != glow {
		t.Errorf("the pool holds %d glow squid after the cap was reached, want it held at %d", still, glow)
	}
	if waterCreatures := waterCreatureCount(srv.Mobs()); waterCreatures < 5 {
		t.Errorf("the ocean beside the pool holds %d water creatures, want its own category's cap reached", waterCreatures)
	}
}

// drainDeepWater fills the seeded world's own water below the glow squid's
// depth with stone: the carved caves and aquifers hold dark water of their own
// near the player, which would spend the glow squid's cap before the test's
// pool does.
func drainDeepWater(t *testing.T, srv *server.Server, poolX, poolZ int32) {
	t.Helper()
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	for x := poolX - 192; x < poolX+128; x++ {
		for z := poolZ - 192; z < poolZ+128; z++ {
			if x >= poolX && x < poolX+32 && z >= poolZ && z < poolZ+32 {
				continue
			}
			for y := int32(-64); y <= 63-33; y++ {
				if rules.IsWater(srv.BlockAt(x, y, z)) {
					srv.SetBlock(x, y, z, stone)
				}
			}
		}
	}
}

// glowSquidCount counts the glow squid of a herd.
func glowSquidCount(mobs []server.TestMob) int {
	n := 0
	for _, m := range mobs {
		if m.Kind == "minecraft:glow_squid" {
			n++
		}
	}
	return n
}

// digDeepPool carves a water pool into the loaded world: water cells from
// the y given up three, then air the three above them, over every column in
// the rectangle named. The water cells' light reads none of the sky's, the
// stone the pool sits under holding it out.
func digDeepPool(t *testing.T, srv *server.Server, x0, x1, z0, z1, y int32) {
	t.Helper()
	water, ok := registry.FindState("minecraft:water", map[string]string{"level": "0"})
	if !ok {
		t.Fatal("the block registry has no still water state")
	}
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			for dy := range int32(4) {
				if !srv.SetBlock(x, y+dy, z, water) {
					t.Fatalf("the pool's chunk at (%d, %d) was not loaded", x, z)
				}
			}
			for dy := int32(4); dy < 7; dy++ {
				srv.SetBlock(x, y+dy, z, 0)
			}
		}
	}
}

// TestSpawnBodyFollowsPlayerPoseAndScale checks the obstruction box of a real
// player: 0.9 high at scale 0.5, 1.5 high sneaking.
// vanilla: LevelReader.isUnobstructed with LivingEntity.getBoundingBox
func TestSpawnBodyFollowsPlayerPoseAndScale(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, scale string
		sneak       bool
		height      float64
	}{
		{"scale 0.5", "0.5", false, 0.9},
		{"sneaking", "1", true, 1.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.ItemCreativeConfig(), "Actor", servertest.OpsListing(t, "Actor"))
			servertest.HoldTicks(t, srv)
			c.ChatCommand("attribute @s minecraft:scale base set " + tc.scale)
			servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")
			if tc.sneak {
				c.SendSneak(true)
				c.Settle()
			}
			height := func() float64 {
				bodies := srv.PlayerSpawnBodiesForTest()
				if len(bodies) != 1 {
					t.Fatalf("%d bodies, want 1", len(bodies))
				}
				return bodies[0].MaxY - bodies[0].MinY
			}
			servertest.UntilTicks(t, srv, 20, "the body takes the pose's height", func() bool {
				return math.Abs(height()-tc.height) < 1e-4
			})
		})
	}
}
