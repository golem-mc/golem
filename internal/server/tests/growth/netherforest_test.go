package growth

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The nether's forests and their cover, black-box: the nether of the test
// harness's own flat world read back through the server's chunk path — its
// seed the flat overworld settles, so the forests the sweep reads are the
// same every run. The crimson forest has to grow its roots and fungi on
// the nylium, hang weeping vines under its ceilings and carry nether wart
// blocks, and the warped forest grows warped roots and fungi, nether
// sprouts and twisting vines, and warped wart blocks. Every plant has to
// stand on nylium, the only ground vanilla's forest plants survive on;
// every vine on the block that holds it; every wart beside the netherrack
// or wart of its patch. The counts are pinned within ±15% against the
// measured cover, so a count, weight or placement regression fails. The
// wart blocks are mostly the huge fungi's caps (the surface rule's floor
// patches are a few dozen), so their pins follow the fungus draws of the
// vegetation features the reference digests in internal/server/world/testdata pin.
// vanilla: crimson_forest_vegetation, warped_forest_vegetation, nether_sprouts,
// weeping_vines, twisting_vines, SurfaceRuleData.nether's wart patches

// The two forests of the seed the flat test world settles its nether on,
// in block coordinates measured off that seed's biome source: the crimson
// one just south-east of the origin, the warped one farther out the same
// way. The bounds are chunk edges, and the forest's biome fills every
// column of each.
var (
	crimsonForestSweep = [4]int32{32, 32, 127, 127} // x0, z0, x1, z1
	warpedForestSweep  = [4]int32{240, 256, 335, 351}
)

// startForestNether stands a server on the harness's flat world, whose
// settled nether seed grows the forests these tests' wants are measured
// against.
func startForestNether(t *testing.T) *server.Server {
	t.Helper()

	cfg := config.Default()
	cfg.OnlineMode = false

	srv, _ := servertest.Start(t, cfg)
	return srv
}

// forestCover is what one sweep of a forest found. The plants count by
// kind, the vines of the forest's own family (heads of every age and
// bodies both) and the wart blocks count as totals split into the floors'
// patches and the cells beside a fellow warts grow together in; the held
// counts are how many of each stood on the ground that supports them, and
// floors is the forest's nylium floor cells.
type forestCover struct {
	plants                                map[string]int
	vines, warts, wartFloors, wartPatches int
	heldPlants, heldVines, heldWarts      int
	floors                                int
}

// sweepForestCover walks every block of the chunk square x0..z1 in the
// nether and counts the forest's cover out of it, checking each plant,
// vine and wart against what supports it. The crimson forest grows
// weeping vines that hang and nether wart blocks, the warped one twisting
// vines that climb and warped wart blocks.
// vanilla: the forest plants' supports tags, WeepingVinesFeature,
// TwistingVinesFeature, SurfaceRuleData's forest floors
func sweepForestCover(t *testing.T, srv *server.Server, bounds [4]int32, forest string) forestCover {
	t.Helper()

	mustFind := func(name string) uint16 {
		s, ok := registry.FindState(name, nil)
		if !ok {
			t.Fatalf("the block registry has no %s", name)
		}
		return s
	}
	crimson, warped := mustFind("minecraft:crimson_nylium"), mustFind("minecraft:warped_nylium")
	netherrack := mustFind("minecraft:netherrack")
	var wart, vineBody uint16
	switch forest {
	case "crimson":
		wart = mustFind("minecraft:nether_wart_block")
		vineBody = mustFind("minecraft:weeping_vines_plant")
	case "warped":
		wart = mustFind("minecraft:warped_wart_block")
		vineBody = mustFind("minecraft:twisting_vines_plant")
	}
	// A weeping vine hangs from netherrack, a wart blob or its own column;
	// a twisting vine climbs out of the warped floors' ground or its own
	// column. Wart also rests on the ores the late features can leave
	// under a floor patch, and on the bedrock the nether's band sits on.
	stem := mustFind("minecraft:" + forest + "_stem")
	hangFrom := map[uint16]bool{netherrack: true, wart: true}
	// A neighbouring chunk's huge fungus can replace a vine body with stem
	// without updating the head above it (HugeFungusFeature.placeStem).
	climbFrom := map[uint16]bool{netherrack: true, warped: true, wart: true, stem: true}
	// The huge fungi's caps are wart blocks around a stem, so a stem
	// holds a wart too; the surface rule's patches never rest on one.
	rock := map[uint16]bool{
		netherrack:                              true,
		mustFind("minecraft:nether_quartz_ore"): true,
		mustFind("minecraft:nether_gold_ore"):   true,
		mustFind("minecraft:bedrock"):           true,
	}
	wartGround := map[uint16]bool{wart: true, stem: true}
	for s := range rock {
		wartGround[s] = true
	}

	// The nether cave carver leaves cave air, not air, over the floors it
	// opens.
	caveAir := mustFind("minecraft:cave_air")
	open := func(s uint16) bool { return s == 0 || s == caveAir }

	var cover forestCover
	cover.plants = map[string]int{}
	for cz := world.ChunkOf(float64(bounds[1])); cz <= world.ChunkOf(float64(bounds[3])); cz++ {
		for cx := world.ChunkOf(float64(bounds[0])); cx <= world.ChunkOf(float64(bounds[2])); cx++ {
			release := srv.HoldChunkInDimension(world.NetherName, cx*16, cz*16)
			for x := range int32(16) {
				for z := range int32(16) {
					wx, wz := cx*16+x, cz*16+z
					for y := int32(1); y < 127; y++ {
						s := srv.BlockInDimension(world.NetherName, wx, y, wz)
						if s == 0 {
							continue
						}
						name, _, ok := registry.BlockStateName(s)
						if !ok {
							continue
						}
						switch {
						case isForestPlant(name):
							cover.plants[name]++
							if below := srv.BlockInDimension(world.NetherName, wx, y-1, wz); below == crimson || below == warped {
								cover.heldPlants++
							}
						case s == crimson || s == warped:
							if open(srv.BlockInDimension(world.NetherName, wx, y+1, wz)) {
								cover.floors++
							}
						case isVineOf(name, forest):
							cover.vines++
							if forest == "crimson" {
								above := srv.BlockInDimension(world.NetherName, wx, y+1, wz)
								if hangFrom[above] || above == vineBody {
									cover.heldVines++
								}
							} else if below := srv.BlockInDimension(world.NetherName, wx, y-1, wz); climbFrom[below] || below == vineBody {
								cover.heldVines++
							}
						case s == wart:
							cover.warts++
							if open(srv.BlockInDimension(world.NetherName, wx, y+1, wz)) &&
								rock[srv.BlockInDimension(world.NetherName, wx, y-1, wz)] {
								// A floor patch: the surface rule wrote it
								// where the nylium would have gone; a fungus
								// cap rests on its stem or wart, not on rock.
								cover.wartFloors++
							}
							// A cap's underside drops single blocks (15% of
							// its cells), so a held wart may touch its
							// support only along a diagonal.
							// A wart on the sweep's rim may rest on a
							// block of a chunk the sweep did not hold.
							rim := wx == bounds[0] || wx == bounds[2] || wz == bounds[1] || wz == bounds[3]
							if rim || touchesAny(srv, wartGround, wx, y, wz) {
								cover.heldWarts++
							}
							if srv.BlockInDimension(world.NetherName, wx+1, y, wz) == wart ||
								srv.BlockInDimension(world.NetherName, wx-1, y, wz) == wart ||
								srv.BlockInDimension(world.NetherName, wx, y, wz+1) == wart ||
								srv.BlockInDimension(world.NetherName, wx, y, wz-1) == wart {
								cover.wartPatches++
							}
						}
					}
				}
			}
			release()
		}
	}
	return cover
}

// touchesAny reports whether any of the 26 blocks around (x, y, z) is in set.
func touchesAny(srv *server.Server, set map[uint16]bool, x, y, z int32) bool {
	for dx := int32(-1); dx <= 1; dx++ {
		for dy := int32(-1); dy <= 1; dy++ {
			for dz := int32(-1); dz <= 1; dz++ {
				if (dx != 0 || dy != 0 || dz != 0) && set[srv.BlockInDimension(world.NetherName, x+dx, y+dy, z+dz)] {
					return true
				}
			}
		}
	}
	return false
}

// isForestPlant names the small cover the vegetation features place.
func isForestPlant(name string) bool {
	switch name {
	case "minecraft:crimson_roots", "minecraft:warped_roots",
		"minecraft:crimson_fungus", "minecraft:warped_fungus",
		"minecraft:nether_sprouts":
		return true
	}
	return false
}

// isVineOf reports whether the named block is one of the forest's own
// vines — weeping in the crimson forest, twisting in the warped — head or
// body both. The other family never grows in this forest.
func isVineOf(name, forest string) bool {
	switch forest {
	case "crimson":
		return name == "minecraft:weeping_vines" || name == "minecraft:weeping_vines_plant"
	default:
		return name == "minecraft:twisting_vines" || name == "minecraft:twisting_vines_plant"
	}
}

// withinPin reports whether got sits inside the ±15% window a count is
// pinned to, the margin the ore sweeps use: the generation is exact, but
// the window keeps a harmless reordering of the features' dice from
// breaking a pin while still failing a placement regression.
func withinPin(got int, want float64) bool {
	return float64(got) >= 0.85*want && float64(got) <= 1.15*want
}

// checkCover holds the swept cover against its measured counts: every
// plant on nylium, every vine on its holder, every wart beside rock or its
// own patch, the wart floors at most a percent of the forest's floors and
// the forest's own kinds inside their pins.
func checkCover(t *testing.T, cover forestCover, wants map[string]int, vines, warts, wartFloors, wartPatches int) {
	t.Helper()

	for name, want := range wants {
		t.Logf("%s: %d (want ~%d)", name, cover.plants[name], want)
		if !withinPin(cover.plants[name], float64(want)) {
			t.Errorf("%s: %d over the forest's sweep, want about %d (the measured cover)",
				name, cover.plants[name], want)
		}
	}
	var plants int
	for _, n := range cover.plants {
		plants += n
	}
	if plants != cover.heldPlants {
		t.Errorf("%d of %d plants stand on something other than nylium, want all of them",
			cover.heldPlants, plants)
	}
	t.Logf("vines: %d of %d held, warts: %d of %d held (floors %d, beside a fellow %d)",
		cover.heldVines, cover.vines, cover.heldWarts, cover.warts, cover.wartFloors, cover.wartPatches)
	t.Logf("floors: %d nylium, wart floors %d (%.2f%% of forest floors)",
		cover.floors, cover.wartFloors, 100*float64(cover.wartFloors)/float64(cover.floors+cover.wartFloors))
	if !withinPin(cover.vines, float64(vines)) {
		t.Errorf("%d vine blocks over the forest's sweep, want about %d (the measured cover)",
			cover.vines, vines)
	}
	if cover.vines != cover.heldVines {
		t.Errorf("%d of %d vine blocks sit off the block that holds them", cover.heldVines, cover.vines)
	}
	if !withinPin(cover.warts, float64(warts)) {
		t.Errorf("%d wart blocks over the forest's sweep, want about %d (the measured cover)",
			cover.warts, warts)
	}
	// A cap's underside starts single blocks on 15% of its cells, so a
	// few warts touch nothing; more than half a percent is a broken cap.
	if (cover.warts-cover.heldWarts)*200 > cover.warts {
		t.Errorf("%d of %d wart blocks float free of rock, stems and their own patch",
			cover.warts-cover.heldWarts, cover.warts)
	}
	if cover.wartFloors < wartFloors {
		t.Errorf("%d of the warts are floor patches, want at least %d", cover.wartFloors, wartFloors)
	}
	if cover.wartPatches < wartPatches {
		t.Errorf("%d of the warts grew beside a fellow, want at least %d grown together in patches",
			cover.wartPatches, wartPatches)
	}
	// The wart noise covers a whisker of the floors, vanilla's ~0.3%; a
	// threshold that catches too much turns the forests' nylium into wart.
	if cover.wartFloors*100 > cover.floors+cover.wartFloors {
		t.Errorf("%d of %d forest floors are wart patches (%.1f%%), want at most 1%%",
			cover.wartFloors, cover.floors+cover.wartFloors,
			100*float64(cover.wartFloors)/float64(cover.floors+cover.wartFloors))
	}
}

// TestNetherForestsGrowTheirCover sweeps both forests of one server's
// nether; the sweeps only read, so they share it.
func TestNetherForestsGrowTheirCover(t *testing.T) {
	t.Parallel()
	srv := startForestNether(t)
	t.Run("CrimsonForestGrowsItsCover", func(t *testing.T) {
		t.Parallel()
		testCrimsonForestGrowsItsCover(t, srv)
	})
	t.Run("WarpedForestGrowsItsCover", func(t *testing.T) {
		t.Parallel()
		testWarpedForestGrowsItsCover(t, srv)
	})
}

// testCrimsonForestGrowsItsCover checks the crimson forest's cover: its
// roots and fungi on the nylium, weeping vines under the ceilings, and
// nether wart blocks as the huge fungi's caps and a few floor patches.
func testCrimsonForestGrowsItsCover(t *testing.T, srv *server.Server) {
	cover := sweepForestCover(t, srv, crimsonForestSweep, "crimson")

	if got := cover.plants["minecraft:warped_fungus"]; got < 1 {
		t.Errorf("%d warped fungi strayed into the crimson forest, want the vegetation's one-in-99 stray", got)
	}
	checkCover(t, cover, map[string]int{
		"minecraft:crimson_roots":  832,
		"minecraft:crimson_fungus": 106,
	}, 724, 7760, 2, 5800)
}

// testWarpedForestGrowsItsCover checks the warped forest's cover: warped
// roots, nether sprouts and fungi on the nylium, twisting vines climbing
// off the floors, and the warped wart blocks of the huge fungi's caps.
func testWarpedForestGrowsItsCover(t *testing.T, srv *server.Server) {
	cover := sweepForestCover(t, srv, warpedForestSweep, "warped")

	if got := cover.plants["minecraft:crimson_roots"] + cover.plants["minecraft:crimson_fungus"]; got < 1 {
		t.Errorf("%d crimson strays over the warped forest, want the vegetation's one-in-100 strays", got)
	}
	checkCover(t, cover, map[string]int{
		"minecraft:warped_roots":   1549,
		"minecraft:nether_sprouts": 1265,
		"minecraft:warped_fungus":  242,
	}, 709, 15753, 0, 11800) // Vanilla 26.3 seed 0, chunks 15,16..20,21: 709 vines.
}
