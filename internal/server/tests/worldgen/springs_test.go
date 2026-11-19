package worldgen

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The overworld's cave fluids, black-box: the springs carry the fluid tick
// vanilla schedules on placing them, so a held chunk's springs pour out on
// its first tick (see overworldSpringsFlowWhenTheirChunkTicks), while the
// deep aquifer fill, which nothing schedules, keeps its place.

// startFluidWorld stands a server on the overworld of seed 12345, with the
// main loop stopped so a scan and the hand-run ticks see a still world.
func startFluidWorld(t *testing.T) *server.Server {
	t.Helper()

	cfg := config.Default()
	cfg.OnlineMode = false
	// The unprefixed "normal" rather than "minecraft:normal": the harness
	// swaps the default spelling of it for its flat test ground.
	cfg.LevelType = "normal"
	cfg.LevelSeed = "12345"

	srv, _ := servertest.Start(t, cfg)
	servertest.StopTicking(t, srv)
	return srv
}

// fluidStates returns the state IDs of every state of a fluid block, so a
// test can tell a generated source (the default first state) from a flowing
// state no generation may write.
func fluidStates(t *testing.T, name string) map[uint16]bool {
	t.Helper()
	states, ok := registry.AllStates[name]
	if !ok {
		t.Fatalf("the block registry has no %s", name)
	}
	m := make(map[uint16]bool, len(states))
	for _, s := range states {
		m[s.ID] = true
	}
	return m
}

// springRock returns the states a spring of the fluid may stand in:
// vanilla's valid_blocks lists, which both fluids share and the water's
// adds the frozen ground of the cold heights to.
// vanilla: configured_feature spring_water, spring_lava_overworld
func springRock(t *testing.T, water bool) map[uint16]bool {
	t.Helper()
	names := []string{
		"minecraft:stone", "minecraft:granite", "minecraft:diorite",
		"minecraft:andesite", "minecraft:deepslate", "minecraft:tuff",
		"minecraft:calcite", "minecraft:dirt",
	}
	if water {
		names = append(names, "minecraft:snow_block", "minecraft:powder_snow", "minecraft:packed_ice")
	}
	m := make(map[uint16]bool, len(names))
	for _, name := range names {
		state, ok := registry.FindState(name, nil)
		if !ok {
			t.Fatalf("the block registry has no %s", name)
		}
		m[state] = true
	}
	return m
}

// springPocket reports whether the fluid at (x, y, z) stands in the shape
// the vanilla SpringFeature places: rock above it and below it, and of the
// four sides and the floor, four rock and exactly one open side.
// vanilla: SpringFeature.place
func springPocket(srv *server.Server, x, y, z int32, rock map[uint16]bool) bool {
	if !rock[srv.BlockAt(x, y+1, z)] || !rock[srv.BlockAt(x, y-1, z)] {
		return false
	}
	rocks, holes := 1, 0 // the floor is rock, never one of the holes
	for _, d := range [][2]int32{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
		switch s := srv.BlockAt(x+d[0], y, z+d[1]); {
		case rock[s]:
			rocks++
		case s == 0 || s == caveAir():
			holes++
		}
	}
	return rocks == 4 && holes == 1
}

// caveAir is the cave air a lake leaves, which a spring's hole may be.
func caveAir() uint16 {
	air, _ := registry.BlockByName("minecraft:cave_air")
	return air.DefaultState()
}

// interior reports whether the block sits far enough inside its chunk that
// every neighbour is one chunk's own cells, which a scan's neighbour reads
// can only be honest about.
func interior(x, z int32) bool {
	localX, localZ := x&15, z&15
	return localX >= 1 && localX <= 14 && localZ >= 1 && localZ <= 14
}

// overworldGeneratedLavaStaysPut checks the deep fill's stillness: a
// held chunk of the bare rock a player has not touched ticks thirty-odd
// times — past lava's own flow interval — and the pool cell and the blocks
// around it stay as generated. The aquifer fill carries no scheduled tick,
// unlike the springs, so nothing sets it running.
func overworldGeneratedLavaStaysPut(t *testing.T, srv *server.Server) {

	source := mustDefaultState(t, "minecraft:lava")
	// A 3x3 window contains an exposed aquifer pool; the assertion ticks
	// one pocket rather than measuring the regional fluid yield.
	var at [3]int32
	found := false
	// The deep band the fill runs in: a small sweep finds a pool surface —
	// a source under air, which a spring, rock above it by its rule, can
	// never be — interior to its chunk so the neighbourhood reads are
	// single-chunk.
	srv.ScanOverworldBlocksAt(-16, -16, 31, 31, -64, -52, func(x, y, z int32, s uint16) {
		if !found && s == source && interior(x, z) && srv.BlockAt(x, y+1, z) == 0 {
			at, found = [3]int32{x, y, z}, true
		}
	})
	if !found {
		t.Fatal("no lava pool surface below y -52 within the 3x3 chunks, want the fill to stand in")
	}

	release := srv.HoldChunk(at[0], at[2])
	defer release()

	neighbourhood := func() map[[3]int32]uint16 {
		cells := make(map[[3]int32]uint16)
		for dy := int32(-2); dy <= 2; dy++ {
			for dx := int32(-2); dx <= 2; dx++ {
				for dz := int32(-2); dz <= 2; dz++ {
					pos := [3]int32{at[0] + dx, at[1] + dy, at[2] + dz}
					cells[pos] = srv.BlockAt(pos[0], pos[1], pos[2])
				}
			}
		}
		return cells
	}
	before := neighbourhood()
	for range 31 {
		srv.RunTick()
	}
	after := neighbourhood()

	for pos, was := range before {
		if got := after[pos]; got != was {
			t.Errorf("block at (%d, %d, %d) turned from %d to %d after 31 ticks of an untouched chunk",
				pos[0], pos[1], pos[2], was, got)
		}
	}
	if got := srv.BlockAt(at[0], at[1], at[2]); got != source {
		t.Errorf("the scanned lava cell turned from the source to %d after 31 ticks", got)
	}
}

// overworldSpringsFlowWhenTheirChunkTicks checks the fluid tick the
// generator schedules with each spring: a held chunk's water spring pours
// into the one opening of its rock pocket on the chunk's first ticks — the
// waterfall vanilla's SPRING_WATER makes. Without the scheduled tick the
// source would stand until a neighbour changed.
func overworldSpringsFlowWhenTheirChunkTicks(t *testing.T, srv *server.Server) {

	source := mustDefaultState(t, "minecraft:water")
	rock := springRock(t, true)

	// Chunk (-4, 3) has an interior water spring; only this pocket must tick,
	// rather than the whole region used to measure fluid yields.
	var at [3]int32
	found := false
	srv.ScanOverworldBlocksAt(-64, 48, -49, 63, -64, 192, func(x, y, z int32, s uint16) {
		if !found && s == source && interior(x, z) && springPocket(srv, x, y, z, rock) {
			at, found = [3]int32{x, y, z}, true
		}
	})
	if !found {
		t.Fatal("no water spring in chunk (-4, 3), want one to run")
	}

	// The found chunk was released by the scan; holding it regenerates the
	// same spring and queues its tick.
	// A held chunk entity-ticks only; a forced one also runs scheduled fluid
	// ticks with no player near.
	srv.ForceChunkInDimension(world.DimensionName, world.ChunkOf(float64(at[0])), world.ChunkOf(float64(at[2])))
	release := srv.HoldChunk(at[0], at[2])
	defer release()

	// The pocket's one open side, the hole the spring pours into.
	var open [2]int32
	opened := false
	for _, d := range [][2]int32{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
		if s := srv.BlockAt(at[0]+d[0], at[1], at[2]+d[1]); s == 0 || s == caveAir() {
			open, opened = d, true
		}
	}
	if !opened {
		t.Fatal("the found water spring has no open side to pour into")
	}

	for range 6 {
		srv.RunTick()
	}
	water := fluidStates(t, "minecraft:water")
	if got := srv.BlockAt(at[0]+open[0], at[1], at[2]+open[1]); !water[got] {
		t.Error("the spring's hole stayed air after the chunk ticked, want the water pouring into it")
	}
}

// mustDefaultState resolves a fluid's default state, which every generated
// cell has to carry.
func mustDefaultState(t *testing.T, name string) uint16 {
	t.Helper()
	state, ok := registry.FindState(name, nil)
	if !ok {
		t.Fatalf("the block registry has no %s", name)
	}
	return state
}
