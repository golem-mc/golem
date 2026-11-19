package world

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The dimensions around a joined player: which one they are in, how the
// packet that changes it reads, and what survives a log out and a log back
// in there. The portals themselves are in server_portals_test.go; these are
// the join, the persistence, the terrain of the world arrived at and the
// death that comes home to the overworld.

// crossingTicks bounds one crossing in game ticks, including its arrival.
const (
	crossingTicks = 400
	arrivalTicks  = 200
)

// TestLoginNamesThreeDimensions checks the Login (play) packet offers all
// three worlds the server runs, in the order vanilla names its own — the
// client needs the list before any Respawn can name another of them, and a
// player with no file joins the first.
func TestLoginNamesThreeDimensions(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Dial(t, cfg)
	c.Login("Dimensioneer")
	c.Configure()

	r := protocol.NewReader(c.Expect(protocol.IDPlayLogin))
	r.ReadI32()  // entity ID
	r.ReadBool() // hardcore
	if got := r.ReadVarInt(); got != 3 {
		t.Fatalf("dimension count = %d, want 3", got)
	}
	for _, want := range []string{
		world.DimensionName, world.NetherName, world.EndName,
	} {
		if got := r.ReadString(32767); got != want {
			t.Errorf("dimension = %q, want %q", got, want)
		}
	}
	// The rest of the Login fields in order; the dimension the player joins
	// is the overworld for someone with no file to say otherwise.
	r.ReadVarInt() // max players
	r.ReadVarInt() // view distance
	r.ReadVarInt() // simulation distance
	r.ReadBool()   // reduced debug info
	r.ReadBool()   // respawn screen
	r.ReadBool()   // limited crafting
	if got := r.ReadVarInt(); got != registry.MustID("minecraft:dimension_type", world.DimensionType) {
		t.Errorf("dimension type = %d, want the overworld's", got)
	}
	if got := r.ReadString(32767); got != world.DimensionName {
		t.Errorf("dimension name = %q, want %q", got, world.DimensionName)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("login (play): %v", err)
	}
	if r.Remaining() == 0 {
		t.Error("login (play) ended early, want the rest of the connection fields")
	}
}

// TestDimensionPersistsAcrossRejoin checks that a player who logs out of the
// nether logs back into it, standing where they left off: the dimension is a
// field of the player file like the position is, and the join that follows
// describes the world it names rather than the overworld for everyone.
func TestDimensionPersistsAcrossRejoin(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)

	// Into the nether, standing on a spot of it the test names. The spot is
	// a block: the player stands at its centre.
	c := connect().Join("Wanderer")
	srv.MovePlayerToDimension("Wanderer", world.NetherName, 80, 40, -17)
	if got := srv.PlayerDimension("Wanderer"); got != world.NetherName {
		t.Fatalf("dimension after the move = %q, want the nether", got)
	}
	pos := srv.PlayerPosition("Wanderer")
	if pos.X != 80.5 || pos.Y != 40 || pos.Z != -16.5 {
		t.Errorf("nether position = (%v, %v, %v), want (80.5, 40, -16.5)", pos.X, pos.Y, pos.Z)
	}
	c.Disconnect()

	// The file says the nether, under the key a vanilla one keeps it in.
	saved := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Wanderer"))
	if got, ok := nbt.Get[nbt.String](saved, "Dimension"); !ok || string(got) != world.NetherName {
		t.Errorf("saved Dimension = %v, %v; want %q, true", got, ok, world.NetherName)
	}

	// A rejoin starts in the nether, at the same spot.
	again := connect()
	again.Login("Wanderer")
	again.Configure()
	r := protocol.NewReader(again.Expect(protocol.IDPlayLogin))
	r.ReadI32()  // entity ID
	r.ReadBool() // hardcore
	if got := r.ReadVarInt(); got != 3 {
		t.Fatalf("dimension count = %d, want 3", got)
	}
	for range int32(3) {
		r.ReadString(32767)
	}
	r.ReadVarInt() // max players
	r.ReadVarInt() // view distance
	r.ReadVarInt() // simulation distance
	r.ReadBool()   // reduced debug info
	r.ReadBool()   // respawn screen
	r.ReadBool()   // limited crafting
	if got := r.ReadVarInt(); got != registry.MustID("minecraft:dimension_type", world.NetherType) {
		t.Errorf("dimension type = %d, want the nether's", got)
	}
	if got := r.ReadString(32767); got != world.NetherName {
		t.Errorf("dimension name = %q, want %q", got, world.NetherName)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("login (play): %v", err)
	}

	arrived := again.SpawnedAt()
	if arrived.X != 80.5 || arrived.Y != 40 || arrived.Z != -16.5 {
		t.Errorf("rejoined position = (%v, %v, %v), want (80.5, 40, -16.5)",
			arrived.X, arrived.Y, arrived.Z)
	}
	again.EnterWorld("Wanderer")
	if got := srv.PlayerDimension("Wanderer"); got != world.NetherName {
		t.Errorf("dimension after the rejoin = %q, want the nether", got)
	}
}

// TestNetherChunksComeFromTheNetherGenerator checks that the world a nether
// arrival streams is the nether generator's: bedrock underfoot at y=0 and
// netherrack around it, not the overworld's stone with a purple ceiling
// painted on.
func TestNetherChunksComeFromTheNetherGenerator(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Caver")

	srv.MovePlayerToDimension("Caver", world.NetherName, 8, 40, 8)
	// The arrival's chunk stream is read through the way a client reads it —
	// a connection that stops reading would hold the whole server's tick
	// loop up on its socket, timing out everyone else's game.
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("the nether never arrived around the moved player")
	}

	// The chunk under the player fills in from the nether's own generator:
	// the bedrock floor, the bedrock ceiling, and the netherrack between.
	bedrock, ok := registry.FindState("minecraft:bedrock", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:bedrock")
	}
	netherrack, ok := registry.FindState("minecraft:netherrack", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:netherrack")
	}
	servertest.PollUntil(t, 10*time.Second, func() bool {
		return srv.BlockInDimension(world.NetherName, 8, 0, 8) == bedrock
	})
	servertest.PollUntil(t, 10*time.Second, func() bool {
		return srv.BlockInDimension(world.NetherName, 8, 127, 8) == bedrock
	})

	rocky := false
	for y := int32(5); y < 123 && !rocky; y++ {
		for x := int32(0); x < 16 && !rocky; x++ {
			for z := range int32(16) {
				if state := srv.BlockInDimension(world.NetherName, x, y, z); state == netherrack {
					rocky = true
					break
				}
			}
		}
	}
	if !rocky {
		t.Error("no netherrack in the chunk under the nether arrival, want the nether's own rock")
	}
}

// TestTheNetherOfSeed3 sweeps seed 3's nether through one server. A flat
// overworld keeps the spawn cheap, but its level-seed still settles the
// nether's own generator.
func TestTheNetherOfSeed3(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelType = "minecraft:flat"
	cfg.LevelSeed = "3"
	srv, _ := servertest.Start(t, cfg)
	for name, check := range map[string]func(*testing.T, *server.Server){
		"KeepsNetherrackUnderTheLavaSea":                   theNetherKeepsNetherrackUnderTheLavaSea,
		"SeamsItsLavaSeaWithMagmaAndLightsNetherrackFires": theNetherSeamsItsLavaSeaWithMagmaAndLightsNetherrackFires,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			check(t, srv)
		})
	}
}

// theNetherKeepsNetherrackUnderTheLavaSea sweeps the nether the server
// generates under its sea's surface, y 6..28 — over the bedrock floor, below
// the sea's top at y=31: the cavern noise decides rock and open cells there
// as it does above the line, the sea filling only the open ones, so most
// columns carry netherrack down there. The seed fixes the terrain the sweep
// runs over.
// vanilla: NoiseRouterData.nether
func theNetherKeepsNetherrackUnderTheLavaSea(t *testing.T, srv *server.Server) {
	netherrack, ok := registry.FindState("minecraft:netherrack", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:netherrack")
	}
	rock := map[uint16]bool{netherrack: true}
	// The ore veins and blobs replace netherrack under the sea's top — the
	// magma's band 27..36 dips into the sea itself, and the gravel,
	// blackstone and soul sand blobs start as low as y 0..5 — so all of
	// them count as the rock carrying on under the sea.
	for _, name := range []string{
		"minecraft:nether_quartz_ore", "minecraft:nether_gold_ore", "minecraft:magma_block", "minecraft:ancient_debris",
		"minecraft:gravel", "minecraft:blackstone", "minecraft:soul_sand",
	} {
		state, ok := registry.FindState(name, nil)
		if !ok {
			t.Fatalf("the block registry has no %s", name)
		}
		rock[state] = true
	}
	lava, ok := registry.FindState("minecraft:lava", map[string]string{"level": "0"})
	if !ok {
		t.Fatal("the block registry has no minecraft:lava")
	}

	const lo, hi = int32(6), int32(28) // the swept band: above the floor's bedrock, below the sea's top
	var columns, rocky, rockCells, lavaCells int
	for cz := range int32(4) {
		for cx := range int32(4) {
			wx, wz := cx*16, cz*16
			release := srv.HoldChunkInDimension(world.NetherName, wx, wz)
			for x := range int32(16) {
				for z := range int32(16) {
					columns++
					hasRock := false
					for y := lo; y <= hi; y++ {
						switch state := srv.BlockInDimension(world.NetherName, wx+x, y, wz+z); {
						case rock[state]:
							rockCells++
							hasRock = true
						case state == lava:
							lavaCells++
						default:
							name, _, _ := registry.BlockStateName(state)
							t.Errorf("nether block at (%d, %d, %d) = %s, want rock or lava below the sea",
								wx+x, y, wz+z, name)
						}
					}
					if hasRock {
						rocky++
					}
				}
			}
			release()
		}
	}
	if rocky*2 <= columns {
		t.Errorf("%d of %d columns hold netherrack under the lava sea (%d rock cells, %d lava cells), want most of them to",
			rocky, columns, rockCells, lavaCells)
	}
	if lavaCells == 0 {
		t.Error("the sweep found no lava below the sea's top, want the sea filling the cells the noise opens")
	}
}

// theNetherSeamsItsLavaSeaWithMagmaAndLightsNetherrackFires sweeps the
// nether the server generates for seed 3, wastes and a soul sand valley:
// veins of magma in vanilla's band over the lava sea's shore — some of them
// at the sea itself — fires standing on bare netherrack in at least some of
// the chunks, soul fire on the soul soil of the soul sand valley, and the
// wastes' mushrooms over solid ground.
// vanilla: ore_magma, patch_fire, patch_soul_fire, brown_mushroom_nether, red_mushroom_nether
func theNetherSeamsItsLavaSeaWithMagmaAndLightsNetherrackFires(t *testing.T, srv *server.Server) {
	states := map[string]uint16{}
	for _, name := range []string{
		"minecraft:magma_block", "minecraft:soul_soil", "minecraft:fire",
		"minecraft:soul_fire", "minecraft:brown_mushroom", "minecraft:red_mushroom",
		"minecraft:nether_quartz_ore", "minecraft:nether_gold_ore", "minecraft:ancient_debris",
		"minecraft:gravel", "minecraft:blackstone", "minecraft:soul_sand",
	} {
		state, ok := registry.FindState(name, nil)
		if !ok {
			t.Fatalf("the block registry has no %s", name)
		}
		states[name] = state
	}
	lava, ok := registry.FindState("minecraft:lava", map[string]string{"level": "0"})
	if !ok {
		t.Fatal("the block registry has no minecraft:lava")
	}
	netherrack, ok := registry.FindState("minecraft:netherrack", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:netherrack")
	}
	// A fire's ground was netherrack when the fire was lit; the ores run
	// after the fires, so an ore or blob may stand there now.
	fireGround := map[uint16]bool{netherrack: true}
	for _, name := range []string{
		"minecraft:nether_quartz_ore", "minecraft:nether_gold_ore", "minecraft:magma_block",
		"minecraft:ancient_debris", "minecraft:gravel", "minecraft:blackstone", "minecraft:soul_sand",
	} {
		fireGround[states[name]] = true
	}

	const bandLo, bandHi = int32(27), int32(36) // the band vanilla's ore_magma draws from
	var magmaInBand, magmaAtSea, magmaAll, fires, soulFires, mushrooms, fireChunks int
	// This 10x10 window includes wastes and soul-sand valley, and meets
	// the original absolute magma, fire and mushroom coverage bounds.
	const span = 10
	for cz := range int32(span) {
		for cx := range int32(span) {
			wx, wz := cx*16, cz*16
			release := srv.HoldChunkInDimension(world.NetherName, wx, wz)
			chunkHasFire := false
			for x := range int32(16) {
				for z := range int32(16) {
					for y := range int32(128) { // the 128 blocks the nether fills
						state := srv.BlockInDimension(world.NetherName, wx+x, y, wz+z)
						switch state {
						case states["minecraft:magma_block"]:
							magmaAll++
							if y < bandLo || y > bandHi {
								continue
							}
							magmaInBand++
							// The sea read is kept inside the chunk: an
							// unloaded neighbour reads as air and would
							// miss the sea beside the shore.
							for _, d := range [][3]int32{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, 0, 1}, {0, 0, -1}} {
								nx, nz := x+d[0], z+d[2]
								if nx >= 0 && nx < 16 && nz >= 0 && nz < 16 &&
									srv.BlockInDimension(world.NetherName, wx+nx, y+d[1], wz+nz) == lava {
									magmaAtSea++
								}
							}
						case states["minecraft:fire"]:
							fires++
							chunkHasFire = true
							if below := srv.BlockInDimension(world.NetherName, wx+x, y-1, wz+z); !fireGround[below] {
								t.Errorf("fire at (%d, %d, %d) stands on the state %d, want the netherrack it was lit on", wx+x, y, wz+z, below)
							}
						case states["minecraft:soul_fire"]:
							soulFires++
							if below := srv.BlockInDimension(world.NetherName, wx+x, y-1, wz+z); below != states["minecraft:soul_soil"] {
								t.Errorf("soul fire at (%d, %d, %d) stands on the state %d, want soul soil", wx+x, y, wz+z, below)
							}
						case states["minecraft:brown_mushroom"], states["minecraft:red_mushroom"]:
							mushrooms++
							below := srv.BlockInDimension(world.NetherName, wx+x, y-1, wz+z)
							if !registry.IsSolid(below) {
								t.Errorf("mushroom at (%d, %d, %d) floats over the state %d, want solid ground", wx+x, y, wz+z, below)
							}
						}
					}
				}
			}
			if chunkHasFire {
				fireChunks++
			}
			release()
		}
	}
	t.Logf("magma %d in the 27..36 band of %d, %d at the sea; %d fires in %d/%d chunks; %d soul fires; %d mushrooms",
		magmaInBand, magmaAll, magmaAtSea, fires, fireChunks, span*span, soulFires, mushrooms)
	if magmaInBand < 12000 {
		t.Errorf("the sweep found %d magma blocks in the y 27..36 band, want the veins over the sea's shore", magmaInBand)
	}
	if magmaAtSea < 20 {
		t.Errorf("the sweep found %d magma blocks beside the lava sea, want the sea's shore seamed with it", magmaAtSea)
	}
	if fireChunks < 10 {
		t.Errorf("%d of %d chunks carry a fire, want the patches on the open netherrack in at least some", fireChunks, span*span)
	}
	if soulFires < 2 {
		t.Errorf("the sweep found %d soul fires, want it on the valley's soul soil", soulFires)
	}
	if mushrooms < 10 {
		t.Errorf("the sweep found %d mushrooms, want the wastes' patches", mushrooms)
	}
}

// TestANetherDeathRespawnSendsTheRespawnBeforeTheKeptInventory checks the
// order a death in another dimension respawns in with keep_inventory on: the
// Respawn comes first, and the kept inventory and the fresh bars follow it.
// Ahead of the Respawn they would fill the body the client is about to throw
// away, leaving the new one empty and hurt. vanilla: PlayerList.respawn
func TestANetherDeathRespawnSendsTheRespawnBeforeTheKeptInventory(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance = config.MinDistance

	srv, c, entityID := servertest.JoinAs(t, cfg, "Dier", servertest.OpsListing(t, "Dier"))

	srv.SetGameRule("keep_inventory", true)
	given := servertest.TheThingsGiven(t)
	for slot, stack := range given {
		srv.GiveSlot("Dier", slot, stack)
	}

	// Creative while the nether's chunks arrive: the arrival cell can be
	// solid, and a survival body suffocates there on the ticks the stream
	// takes, before the test's own death.
	srv.SetPlayerGameMode("Dier", 1)
	srv.MovePlayerToDimension("Dier", world.NetherName, 8, 40, 8)
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("the nether never arrived around the moved player")
	}
	srv.SetPlayerGameMode("Dier", 0)

	c.ChatCommand("kill")
	servertest.ExpectDeathSequence(t, c, entityID, protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Dier")},
	})

	c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})

	// The death's own empty bar was read with its sequence above, so any
	// state ahead of the Respawn now is the fresh body's, put in the wrong
	// place.
	var sawRespawn, contentBefore, barBefore bool
	var kept []protocol.ItemStack
	freshHealth := float32(-1)
	for id, data := range c.Packets(servertest.ExpectTimeout) {
		switch id {
		case protocol.IDPlayRespawn:
			sawRespawn = true
		case protocol.IDPlayContainerContent:
			if !sawRespawn {
				contentBefore = true
				continue
			}
			if kept == nil {
				kept = servertest.ContainerSlots(t, data)
			}
		case protocol.IDPlaySetHealth:
			if !sawRespawn {
				barBefore = true
				continue
			}
			freshHealth = protocol.NewReader(data).ReadF32()
		}
		if sawRespawn && freshHealth >= 0 && kept != nil {
			break
		}
	}
	if !sawRespawn {
		t.Fatal("the Respawn never arrived")
	}
	if contentBefore {
		t.Error("a Container Content arrived before the Respawn, in the body the client discards")
	}
	if barBefore {
		t.Error("a Set Health arrived before the Respawn, in the body the client discards")
	}
	servertest.CheckTheThingsGiven(t, kept, given)
	if freshHealth != 20 {
		t.Errorf("the respawned body's health = %v, want 20 (no damage between the death and the respawn)", freshHealth)
	}
	c.Disconnect()
}

// theThingsStay checks the server's own copy of what was given: the crossing
// hands the windows back and the inventory keeps the things.
func theThingsStay(t *testing.T, srv *server.Server, given map[int]protocol.ItemStack) {
	t.Helper()
	for slot, want := range given {
		got := srv.InventorySlot("Crosser", slot)
		if got.ItemID != want.ItemID || got.Count != want.Count {
			t.Errorf("the server's slot %d holds item %d x%d, want item %d x%d",
				slot, got.ItemID, got.Count, want.ItemID, want.Count)
		}
	}
}

// crossingInventory reads what a crossing sends a client after its Respawn
// to put their things back - the full window, then the held slot and the
// experience - before the first chunk batch, then drains the complete view.
func crossingInventory(t *testing.T, c *servertest.Client, srv *server.Server) []protocol.ItemStack {
	t.Helper()
	var content, held, experience []byte
	var chunks int32
	firstBatch := true
	whole := (2*c.ChunkRadius + 1) * (2*c.ChunkRadius + 1)
	ended := c.ReadTicks(srv, crossingTicks, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayContainerContent:
			if content == nil {
				content = data
			}
		case protocol.IDPlayHeldItem:
			if held == nil {
				held = data
			}
		case protocol.IDPlaySetExperience:
			if experience == nil {
				experience = data
			}
		case protocol.IDPlayChunkData:
			chunks++
		case protocol.IDPlayChunkBatchFinished:
			if firstBatch {
				firstBatch = false
				if content == nil || held == nil || experience == nil {
					t.Fatalf("the crossing sent the window %v, the held slot %v and the experience %v before its first chunk batch",
						content != nil, held != nil, experience != nil)
				}
			}
			// A later crossing stops this sender. Drain its generation before
			// sending the command and the reader's barrier behind it.
			return chunks >= whole
		}
		return false
	})
	if !ended {
		t.Fatalf("the crossing's view did not arrive within %d ticks", crossingTicks)
	}

	r := protocol.NewReader(held)
	if got := r.ReadVarInt(); got != 0 {
		t.Errorf("held hotbar slot after the crossing = %d, want 0", got)
	}
	r = protocol.NewReader(experience)
	r.ReadF32() // the bar's fill, which a player who earned nothing keeps empty
	if got := r.ReadVarInt(); got != 0 {
		t.Errorf("experience level after the crossing = %d, want 0", got)
	}
	if got := r.ReadVarInt(); got != 0 {
		t.Errorf("experience total after the crossing = %d, want 0", got)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("set experience: %v", err)
	}

	r = protocol.NewReader(content)
	if got := r.ReadVarInt(); got != 0 {
		t.Errorf("crossing's window ID = %d, want 0", got)
	}
	r.ReadVarInt() // state ID
	slots := make([]protocol.ItemStack, 0, r.ReadVarInt())
	for cap(slots) > len(slots) {
		stack, ok := r.ReadSlot()
		if !ok {
			t.Fatalf("crossing's inventory slot %d did not read back", len(slots))
		}
		slots = append(slots, stack)
	}
	carried, ok := r.ReadSlot()
	if !ok {
		t.Fatal("the crossing's carried slot did not read back")
	}
	if !carried.Empty() {
		t.Errorf("the crossing left %d items on the cursor, want none", carried.Count)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("set container content: %v", err)
	}
	return slots
}

// crossAndCheck reads one crossing whole: the Respawn naming the dimension
// arrived at, the inventory it sends behind it, and the arrival's complete
// chunk view, and checks the inventory against what was given, server-side
// and on the wire.
func crossAndCheck(t *testing.T, c *servertest.Client, srv *server.Server, given map[int]protocol.ItemStack, dimensionType int32, dimension string, how string) {
	t.Helper()
	data := c.ExpectWithinTicks(srv, crossingTicks, protocol.IDPlayRespawn)
	r := protocol.NewReader(data)
	if got := r.ReadVarInt(); got != dimensionType {
		t.Errorf("%s's respawn dimension type = %d, want %d", how, got, dimensionType)
	}
	if got := r.ReadString(32767); got != dimension {
		t.Errorf("%s's respawn dimension = %q, want %q", how, got, dimension)
	}
	servertest.CheckTheThingsGiven(t, crossingInventory(t, c, srv), given)
	theThingsStay(t, srv, given)
}

// expectTeleportFlags reads the next Synchronize Player Position and returns
// its flags.
func expectTeleportFlags(t *testing.T, c *servertest.Client) int32 {
	t.Helper()
	data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlaySyncPosition)
	if !ok {
		t.Fatal("no synchronize player position arrived")
	}
	return servertest.SyncPositionFlags(data)
}

// TestATeleportToAPositionLeavesTheTargetsLook checks the form that names
// another player and a position with no rotation: the target keeps the look
// it has, and the Synchronize Player Position carries a zero turn rather than
// the runner's angles.
// vanilla: TeleportCommand.teleportToPos
func TestATeleportToAPositionLeavesTheTargetsLook(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Runner", "Other"))

	other := connect().Join("Other")
	// The target's own look, which the teleport must not change.
	other.MoveTo(0.5, 64, 0.5, 45, 10)
	other.Settle()

	runner := connect().Join("Runner")
	// The runner faces somewhere else entirely.
	runner.MoveTo(0.5, 64, 0.5, 90, 30)
	runner.Settle()

	runner.ChatCommand("tp Other 5 100 5")
	data, ok := servertest.ReadUntil(t, other, servertest.ExpectTimeout, protocol.IDPlaySyncPosition)
	if !ok {
		t.Fatal("no synchronize player position arrived for the target")
	}
	view := servertest.ReadSyncPosition(t, data)
	if view.Yaw != 0 || view.Pitch != 0 {
		t.Errorf("the teleport carried yaw %v pitch %v, want 0/0", view.Yaw, view.Pitch)
	}
	if flags := servertest.SyncPositionFlags(data); flags != 0x18 {
		t.Errorf("the teleport flags = %#x, want 0x18 (rotation relative)", flags)
	}
	if got := srv.PlayerPosition("Other"); got.Yaw != 45 || got.Pitch != 10 {
		t.Errorf("the target's look after the teleport = %v/%v, want 45/10", got.Yaw, got.Pitch)
	}

	other.Disconnect()
	runner.Disconnect()
}

// TestACrossingRespawnDecodesEveryField checks both ends of a living crossing:
// the nether's Respawn and the overworld's on the way home decode field by
// field, with the data kept and each world's own sea level.
func TestACrossingRespawnDecodesEveryField(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.NetherTestConfig(t, servertest.SurvivalTestConfig()), "Testificate", servertest.OpsListing(t, "Testificate"))

	// Creative while the nether's chunks arrive: the arrival cell can be
	// solid, and a survival body suffocates there on the ticks the stream
	// takes, before the test's own death.
	srv.SetPlayerGameMode("Testificate", 1)
	c.ChatCommand("execute in minecraft:the_nether run tp @s 0 70 0")
	nether := c.ExpectWithinTicks(srv, 200, protocol.IDPlayRespawn)
	if len(nether) != 38 {
		t.Errorf("the nether respawn is %d bytes, want 38", len(nether))
	}
	servertest.CheckRespawn(t, nether, registry.MustID("minecraft:dimension_type", world.NetherType), world.NetherName, 32, 0x03, "the nether")
	c.ExpectViewWithinTicks(srv, 200)
	srv.SetPlayerGameMode("Testificate", 0)

	c.ChatCommand("execute in minecraft:overworld run tp @s 0 70 0")
	overworld := c.ExpectWithinTicks(srv, 200, protocol.IDPlayRespawn)
	if len(overworld) != 41 {
		t.Errorf("the overworld respawn is %d bytes, want 41", len(overworld))
	}
	servertest.CheckRespawn(t, overworld, registry.MustID("minecraft:dimension_type", world.DimensionType), world.DimensionName, -63, 0x03, "the overworld")
	c.Disconnect()
}

// TestLoginCarriesTheServersViewDistance checks the Login (play) view distance
// is the server's setting, not the smaller render distance the client asked
// for.
func TestLoginCarriesTheServersViewDistance(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance = 10

	_, c := servertest.Dial(t, cfg)
	c.ViewAsk = 4
	c.Login("Viewer")
	c.Configure()

	r := protocol.NewReader(c.Expect(protocol.IDPlayLogin))
	r.ReadI32()  // entity ID
	r.ReadBool() // hardcore
	for range r.ReadVarInt() {
		r.ReadString(32767)
	}
	r.ReadVarInt() // max players
	if got := r.ReadVarInt(); got != 10 {
		t.Errorf("login view distance = %d, want the server's 10", got)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("login (play): %v", err)
	}
}
