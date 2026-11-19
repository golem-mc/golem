package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
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
