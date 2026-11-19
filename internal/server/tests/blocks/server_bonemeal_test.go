package blocks

import (
	"fmt"
	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"maps"
	"slices"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The bone meal beyond the crops, black-box, the dice-pinned results clicked
// until they land.

// boneMealClicks right-clicks the block up to tries times, each ack waited
// for, until want holds there.
func boneMealClicks(t *testing.T, c *servertest.Client, srv *server.Server, x, y, z int32, tries int, want func(name string, props map[string]string) bool) {
	t.Helper()
	for range tries {
		c.UseBlock(x, y, z)
		c.SettleKeeping()
		srv.FlushBlockAcks()
		c.ExpectBlockAck()
		name, props := servertest.StateProps(srv.BlockAt(x, y, z))
		if want(name, props) {
			return
		}
	}
	t.Fatalf("the block at (%d, %d, %d) never took the shape the bone meal aims for", x, y, z)
}

// TestBoneMealLightsCaveVines checks the relight: the berries the bone meal
// grows onto a bare cave vine emit light 14, and the block changes the
// growth sends relight the column, so the cell shines once the relight the
// click queues has run.
func TestBoneMealLightsCaveVines(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:cave_vines", map[string]string{"berries": "false"})
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	if light := srv.BlockLightAt(5, 64, 0); light != 0 {
		t.Fatalf("the bare vine stands in %d light, want the dark it starts in", light)
	}

	c.UseBlock(5, 64, 0)
	c.Settle()

	if name, props := servertest.StateProps(srv.BlockAt(5, 64, 0)); name != "minecraft:cave_vines" || props["berries"] != "true" {
		t.Errorf("the vine stands as %s %v, want berries=true", name, props)
	}
	// The relight runs off the click's own path: wait for it to land.
	servertest.WaitFor(t, srv, 1200, func() bool { return srv.BlockLightAt(5, 64, 0) == 14 })
}

// TestBoneMealAdvancesASapling checks the sapling: stage 0 to 1, the one
// step short of a tree the engine makes of it.
func TestBoneMealAdvancesASapling(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:oak_sapling", nil)
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	boneMealClicks(t, c, srv, 5, 64, 0, 20, func(name string, props map[string]string) bool {
		return name == "minecraft:oak_sapling" && props["stage"] == "1"
	})
}

// TestBoneMealGrowsASaplingIntoATree checks the growth the stage was one
// step short of: clicked until the roll lands, the sapling becomes the
// trunk of the oak this server's overworld generator plants, its canopy
// of non-persistent leaves standing at the distances the settle walk
// leaves them, so no leaf keeps the walk's distance seven.
func TestBoneMealGrowsASaplingIntoATree(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, 2) // a seed whose draws plant the plain oak, not the fancy one

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:oak_sapling", nil)
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	boneMealClicks(t, c, srv, 5, 64, 0, 30, func(name string, props map[string]string) bool {
		return name == "minecraft:oak_log"
	})

	// The trunk: 4 to 6 logs standing where the sapling stood, the grass
	// under it turned to dirt (vanilla placeBelowTrunkBlock).
	trunk := int32(0)
	for {
		name, _ := servertest.StateProps(srv.BlockAt(5, 64+trunk, 0))
		if name != "minecraft:oak_log" {
			break
		}
		trunk++
	}
	if trunk < 4 || trunk > 6 {
		t.Errorf("the tree stands %d logs tall, want the oak's 4 to 6", trunk)
	}
	if name, _ := servertest.StateProps(srv.BlockAt(5, 63, 0)); name != "minecraft:dirt" {
		t.Errorf("the trunk's ground became %s, want the dirt it leaves behind", name)
	}
	top := 64 + trunk - 1

	// The canopy: a leaf over the crown, and the wide ring solid on its
	// far sides, where the corner clip a shorter tree never takes.
	if name, _ := servertest.StateProps(srv.BlockAt(5, top+1, 0)); name != "minecraft:oak_leaves" {
		t.Errorf("above the crown stands %s, want the plus of leaves", name)
	}
	for _, cell := range [][2]int32{{-2, 0}, {2, 0}, {0, -2}, {0, 2}} {
		name, _ := servertest.StateProps(srv.BlockAt(5+cell[0], top-1, cell[1]))
		if name != "minecraft:oak_leaves" {
			t.Errorf("the canopy at (%d, %d, %d) holds %s, want a leaf",
				5+cell[0], top-1, cell[1], name)
		}
	}

	// Every leaf of the canopy, counted within the shape the tree can
	// take: persistent=false the way the feature's leaves are planted,
	// and a distance the walk reached, one of its one to six.
	leaves := 0
	for dx := int32(-2); dx <= 2; dx++ {
		for dz := int32(-2); dz <= 2; dz++ {
			for dy := top - 2; dy <= top+1; dy++ {
				name, props := servertest.StateProps(srv.BlockAt(5+dx, dy, dz))
				if name != "minecraft:oak_leaves" {
					continue
				}
				leaves++
				if props["persistent"] != "false" {
					t.Errorf("the leaf at (%d, %d, %d) is persistent, want the feature's loose one", 5+dx, dy, dz)
				}
				d, err := strconv.Atoi(props["distance"])
				if err != nil || d < 1 || d > 6 {
					t.Errorf("the leaf at (%d, %d, %d) stands at distance %q, want the settled one to six", 5+dx, dy, dz, props["distance"])
				}
			}
		}
	}
	// The canopy's cells are the three layers and the plus, minus the
	// three the trunk's column takes over and the four corners a layer
	// the clip can trim.
	if leaves < 49 || leaves > 61 {
		t.Errorf("the tree grew %d leaves, want the 49 to 61 the oak's canopy holds", leaves)
	}
}

// TestASaplingWithNoRoomDoesNotGrow checks the refusal the free-spot check
// makes of a stone over the sapling: the tree that does not fit is not
// planted — the clicks answer with the sparkle and the ack, and the
// sapling, the stone and the sky around them keep standing.
func TestASaplingWithNoRoomDoesNotGrow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:oak_sapling", map[string]string{"stage": "1"})
	servertest.SetFarmBlock(t, c, srv, 5, 65, 0, "minecraft:stone", nil)
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	for range 20 {
		c.UseBlock(5, 64, 0)
		c.SettleKeeping()
		c.ExpectBoneMealLanded(srv, 5, 64, 0)
	}
	if name, props := servertest.StateProps(srv.BlockAt(5, 64, 0)); name != "minecraft:oak_sapling" || props["stage"] != "1" {
		t.Errorf("the sapling stands as %s %v, want the stage-1 sapling it was", name, props)
	}
	if name, _ := servertest.StateProps(srv.BlockAt(5, 65, 0)); name != "minecraft:stone" {
		t.Errorf("the stone over the sapling became %s, want it standing", name)
	}
	// Nothing grew into the room the stone denies the tree: above the
	// stone, only the open sky the sapling was standing in.
	for dx := int32(-2); dx <= 2; dx++ {
		for dz := int32(-2); dz <= 2; dz++ {
			for y := int32(66); y <= 72; y++ {
				if name, _ := servertest.StateProps(srv.BlockAt(5+dx, y, dz)); name != "minecraft:air" {
					t.Errorf("the room at (%d, %d, %d) holds %s, want the open air", 5+dx, y, dz, name)
				}
			}
		}
	}
}

// TestASaplingBesideAWallGrows checks the room the tree needs: only the
// trunk's own column is checked, so a wall two blocks out clips the canopy
// but does not refuse the tree (vanilla getMaxFreeTreeHeight checks a 3x3,
// not the 5x5 crown).
func TestASaplingBesideAWallGrows(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:oak_sapling", nil)
	for y := int32(65); y <= 70; y++ {
		servertest.SetFarmBlock(t, c, srv, 7, y, 0, "minecraft:stone", nil)
	}
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	boneMealClicks(t, c, srv, 5, 64, 0, 30, func(name string, props map[string]string) bool {
		return name == "minecraft:oak_log"
	})
}

// TestASaplingUnderACeilingDoesNotGrow checks the room the tree needs above
// its crown: the check reaches one above the plus of leaves, so a block on
// the trunk's column there refuses the tree whole rather than growing a
// shorter one (vanilla getMaxFreeTreeHeight checks to height+1).
func TestASaplingUnderACeilingDoesNotGrow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	// Seed 0 lands the 4-tall roll within a few clicks, the roll whose
	// plus the ceiling at y+5 only just misses: a check to height, not
	// height+1, let that roll grow.
	servertest.SeedPhysics(t, srv, 0)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:oak_sapling", map[string]string{"stage": "1"})
	servertest.SetFarmBlock(t, c, srv, 5, 69, 0, "minecraft:stone", nil)
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	for range 30 {
		c.UseBlock(5, 64, 0)
		c.SettleKeeping()
		c.ExpectBoneMealLanded(srv, 5, 64, 0)
	}
	if name, props := servertest.StateProps(srv.BlockAt(5, 64, 0)); name != "minecraft:oak_sapling" || props["stage"] != "1" {
		t.Errorf("the sapling stands as %s %v, want the stage-1 sapling it was", name, props)
	}
}

// TestASaplingBesideALeafGrows checks what the room check counts as free:
// leaves are in replaceable_by_trees, so a standing leaf beside the trunk
// does not refuse the tree the way a solid block there would. The leaf is
// persistent, though, which the canopy leaves standing (vanilla
// FoliagePlacer.tryPlaceLeaf's isPersistent guard).
func TestASaplingBesideALeafGrows(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:oak_sapling", nil)
	servertest.SetFarmBlock(t, c, srv, 6, 67, 0, "minecraft:oak_leaves", map[string]string{"persistent": "true"})
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	boneMealClicks(t, c, srv, 5, 64, 0, 30, func(name string, props map[string]string) bool {
		return name == "minecraft:oak_log"
	})
	if name, props := servertest.StateProps(srv.BlockAt(6, 67, 0)); name != "minecraft:oak_leaves" || props["persistent"] != "true" {
		t.Errorf("the persistent leaf beside the trunk became %s %v, want it still persistent", name, props)
	}
}

// TestASaplingCanopyOverwritesReplaceableCells checks the leaf placement:
// a leaf replaces a replaceable_by_trees cell, not only air (vanilla
// tryPlaceLeaf), so a water source inside the crown becomes a waterlogged
// leaf, and so does a seagrass cell, whose fluid is a water source of its
// own (FoliagePlacer's isFluidAtPosition reads the fluid state). Falling
// water waterlogs nothing: only a source does (isSourceOfType).
func TestASaplingCanopyOverwritesReplaceableCells(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:oak_sapling", nil)
	servertest.SetFarmBlock(t, c, srv, 6, 67, 0, "minecraft:water", nil)
	servertest.SetFarmBlock(t, c, srv, 4, 67, 0, "minecraft:seagrass", nil)
	servertest.SetFarmBlock(t, c, srv, 4, 68, 0, "minecraft:water", map[string]string{"level": "8"})
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	boneMealClicks(t, c, srv, 5, 64, 0, 30, func(name string, props map[string]string) bool {
		return name == "minecraft:oak_log"
	})
	name, props := servertest.StateProps(srv.BlockAt(6, 67, 0))
	if name != "minecraft:oak_leaves" || props["waterlogged"] != "true" {
		t.Errorf("the cell in the crown holds %s %v, want the leaf written over the waterlogged water source", name, props)
	}
	name, props = servertest.StateProps(srv.BlockAt(4, 67, 0))
	if name != "minecraft:oak_leaves" || props["waterlogged"] != "true" {
		t.Errorf("the seagrass cell in the crown holds %s %v, want the waterlogged leaf over the seagrass's water source", name, props)
	}
	name, props = servertest.StateProps(srv.BlockAt(4, 68, 0))
	if name != "minecraft:oak_leaves" || props["waterlogged"] == "true" {
		t.Errorf("the cell over the falling water holds %s %v, want the dry leaf over the falling water", name, props)
	}
}

// TestASaplingLeavesAnExistingLogInTheTrunkColumn checks the trunk's write:
// the room check accepts a log in the column, but the trunk writes only over
// air or a replaceable_by_trees cell (vanilla TrunkPlacer.placeLog over
// validTreePos), so a log already standing there keeps its block.
func TestASaplingLeavesAnExistingLogInTheTrunkColumn(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:oak_sapling", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 66, 0, "minecraft:birch_log", nil)
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	boneMealClicks(t, c, srv, 5, 64, 0, 30, func(name string, props map[string]string) bool {
		return name == "minecraft:oak_log"
	})
	if name, _ := servertest.StateProps(srv.BlockAt(5, 66, 0)); name != "minecraft:birch_log" {
		t.Errorf("the log in the trunk column became %s, want the birch log it was", name)
	}
}

// TestBoneMealSwingsTheUseArm checks what makes the use show on the
// screen: the vanilla answer to a target the bone meal accepts is the
// arm swing the server broadcasts, which is the animation the client
// plays its own hand — the use on the wire carries nothing else to draw.
func TestBoneMealSwingsTheUseArm(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:oak_sapling", nil)
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	c.UseBlock(5, 64, 0)
	swung := false
	for gotID, data := range c.Packets(servertest.ExpectTimeout) {
		switch gotID {
		case protocol.IDPlaySwingAnimation:
			r := protocol.NewReader(data)
			if id := r.ReadVarInt(); id != c.PlayerEntityID {
				t.Errorf("swinging entity = %d, want the user's own %d", id, c.PlayerEntityID)
			}
			if got := r.ReadVarInt(); got != protocol.HandMain {
				t.Errorf("hand = %d, want %d (the main hand)", got, protocol.HandMain)
			}
			swung = true
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
			protocol.IDPlayUpdateLight, protocol.IDPlayUpdateTime,
			protocol.IDPlayWorldEvent, protocol.IDPlayAckBlockChange,
			protocol.IDPlaySetHealth, protocol.IDPlaySystemChat:
			// The sparkle, the stage, the light, the click's own ack and
			// a peaceful world's topped-up bars — beside the swing, not
			// it.
		default:
			t.Fatalf("unexpected packet 0x%02X while waiting for the use swing", gotID)
		}
		if swung {
			return
		}
	}
	t.Fatal("the bone meal's use never swung the arm")
}

// plainsBoneMealFlowers is the plains can_spawn_from_bone_meal flower list,
// which every plains grass spread picks its flowers from.
var plainsBoneMealFlowers = []string{
	"minecraft:dandelion", "minecraft:poppy", "minecraft:azure_bluet",
	"minecraft:oxeye_daisy", "minecraft:cornflower",
	"minecraft:orange_tulip", "minecraft:red_tulip", "minecraft:pink_tulip", "minecraft:white_tulip",
}

// TestBoneMealSeedsAGrassBlock checks the spread: short grass and the plains
// flowers seeded on the grass around the click, nothing beyond it.
func TestBoneMealSeedsAGrassBlock(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	for x := int32(5); x <= 7; x++ {
		for z := int32(-1); z <= 1; z++ {
			servertest.SetFarmBlock(t, c, srv, x, 63, z, "minecraft:grass_block", nil)
		}
	}
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	c.UseBlock(6, 63, 0)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	c.ExpectBlockAck()

	// The air over the click is the walk's first candidate, seeded without
	// fail; a short grass an earlier try seeded can be grown tall by a
	// later one, the vanilla spread's own order.
	seeded := 0
	for x := int32(5); x <= 7; x++ {
		for z := int32(-1); z <= 1; z++ {
			name, props := servertest.StateProps(srv.BlockAt(x, 64, z))
			switch {
			case name == "minecraft:tall_grass":
				if props["half"] != "lower" {
					t.Errorf("the grass seeded (%d, 64, %d) with %s %v, want a tall grass's lower half", x, z, name, props)
				}
				seeded++
			case name == "minecraft:short_grass" || slices.Contains(plainsBoneMealFlowers, name):
				seeded++
			default:
				if name != "minecraft:air" {
					t.Errorf("the grass seeded (%d, 64, %d) with %s, want air or plains vegetation", x, z, name)
				}
			}
		}
	}
	if seeded == 0 {
		t.Error("the bone meal seeded no grass or flower above the grass block")
	}
	// Nothing the walk can reach: no candidate over stone, and above the
	// patch only a tall grass's upper half.
	for x := int32(3); x <= 9; x++ {
		for z := int32(-3); z <= 3; z++ {
			name, props := servertest.StateProps(srv.BlockAt(x, 65, z))
			if name == "minecraft:air" {
				continue
			}
			overPatch := x >= 5 && x <= 7 && z >= -1 && z <= 1
			if !overPatch || name != "minecraft:tall_grass" || props["half"] != "upper" {
				t.Errorf("the grass seeded (%d, 65, %d) with %s %v, want nothing above the surface", x, z, name, props)
				continue
			}
			if below, belowProps := servertest.StateProps(srv.BlockAt(x, 64, z)); below != "minecraft:tall_grass" || belowProps["half"] != "lower" {
				t.Errorf("the tall grass's upper half at (%d, 65, %d) stands over %s %v, want its lower half", x, z, below, belowProps)
			}
		}
	}
	if name, _ := servertest.StateProps(srv.BlockAt(6, 63, 0)); name != "minecraft:grass_block" {
		t.Errorf("the clicked grass block became %s, want it standing", name)
	}
}

// grassFlowerNames is the set a biome's grass spread may place: the flower
// names given plus the two grasses the spread seeds besides.
func grassFlowerNames(flowers ...string) map[string]bool {
	set := map[string]bool{
		"minecraft:short_grass": true,
		"minecraft:tall_grass":  true,
	}
	for _, name := range flowers {
		set[name] = true
	}
	return set
}

// TestBoneMealGrowsTheBiomeFlowers checks the flower the grass spread seeds:
// the biome's own can_spawn_from_bone_meal features, not a fixed poppy or
// dandelion. A swamp grows only blue orchids; a plains and a flower forest stay
// inside their lists (the feature's position noise picks the kind per cell, and
// the world-package TestBoneMealFlowerForestMatchesPlaceFeature covers that
// pick, so the flower forest wants no particular kind here). The patch is cleared between clicks so
// one small lawn yields the samples of many.
func TestBoneMealGrowsTheBiomeFlowers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		biome   string
		allowed []string
		want    []string
	}{
		{
			name:    "swamp",
			biome:   "minecraft:swamp",
			allowed: []string{"minecraft:blue_orchid"},
			want:    []string{"minecraft:blue_orchid"},
		},
		{
			name:    "plains",
			biome:   "minecraft:plains",
			allowed: plainsBoneMealFlowers,
			want:    []string{"minecraft:oxeye_daisy"},
		},
		{
			name:  "flower_forest",
			biome: "minecraft:flower_forest",
			allowed: []string{
				"minecraft:dandelion", "minecraft:poppy", "minecraft:allium", "minecraft:azure_bluet",
				"minecraft:red_tulip", "minecraft:orange_tulip", "minecraft:white_tulip", "minecraft:pink_tulip",
				"minecraft:oxeye_daisy", "minecraft:cornflower", "minecraft:lily_of_the_valley",
			},
			want: nil,
		},
		{
			name:  "meadow",
			biome: "minecraft:meadow",
			allowed: []string{
				"minecraft:allium", "minecraft:poppy", "minecraft:azure_bluet",
				"minecraft:dandelion", "minecraft:cornflower", "minecraft:oxeye_daisy",
				"minecraft:wildflowers",
			},
			want: []string{"minecraft:wildflowers"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

			srv.StopTick()
			servertest.SeedPhysics(t, srv, servertest.CropsSeed)
			// The lawn stays inside one chunk: the biome is per chunk, and
			// a patch across the border would sample the neighbour's.
			srv.SetOverworldBiome(6, 1, tc.biome)
			for x := int32(5); x <= 7; x++ {
				for z := int32(0); z <= 2; z++ {
					servertest.SetFarmBlock(t, c, srv, x, 63, z, "minecraft:grass_block", nil)
				}
			}
			servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

			air := registry.MustState("minecraft:air", nil)
			grown := map[string]bool{}
			// The position noise decides the kind, so the loop only has to
			// show each wanted state once on the fixed seed.
			for range 120 {
				c.UseBlock(6, 63, 1)
				c.SettleKeeping()
				srv.FlushBlockAcks()
				c.ExpectBlockAck()
				for x := int32(5); x <= 7; x++ {
					for z := int32(0); z <= 2; z++ {
						for y := int32(64); y <= 66; y++ {
							if name, _ := servertest.StateProps(srv.BlockAt(x, y, z)); name != "minecraft:air" {
								grown[name] = true
							}
							srv.SetBlock(x, y, z, air)
						}
					}
				}
				seen := true
				for _, name := range tc.want {
					if !grown[name] {
						seen = false
						break
					}
				}
				if seen {
					break
				}
			}

			allowed := grassFlowerNames(tc.allowed...)
			for name := range grown {
				if !allowed[name] {
					t.Errorf("the bone meal grew a %s where the %s's features do not carry one", name, tc.biome)
				}
			}
			for _, name := range tc.want {
				if !grown[name] {
					t.Errorf("the bone meal never grew a %s in the %s", name, tc.biome)
				}
			}
			if len(grown) == 0 {
				t.Fatal("the bone meal grew nothing")
			}
		})
	}
}

// TestBoneMealGrowsShortGrassTall checks the tall grass: short grass and
// fern grown into their two-half forms.
func TestBoneMealGrowsShortGrassTall(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:short_grass", nil)
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	c.UseBlock(5, 64, 0)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	c.ExpectBlockAck()

	name, props := servertest.StateProps(srv.BlockAt(5, 64, 0))
	if name != "minecraft:tall_grass" || props["half"] != "lower" {
		t.Errorf("the short grass became %s %v, want tall_grass half=lower", name, props)
	}
	name, props = servertest.StateProps(srv.BlockAt(5, 65, 0))
	if name != "minecraft:tall_grass" || props["half"] != "upper" {
		t.Errorf("above it stands %s %v, want tall_grass half=upper", name, props)
	}
}

// TestBoneMealPopsATallFlower checks the tall flower: a lilac given up as an
// item entity, the flower standing on.
func TestBoneMealPopsATallFlower(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:lilac", map[string]string{"half": "lower"})
	servertest.SetFarmBlock(t, c, srv, 5, 65, 0, "minecraft:lilac", map[string]string{"half": "upper"})
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	c.UseBlock(5, 64, 0)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	c.ExpectBlockAck()

	item := c.ExpectDroppedItem()
	lilac, ok := registry.ItemID("minecraft:lilac")
	if !ok {
		t.Fatal("the item registry has no lilac")
	}
	if item.Stack.ItemID != lilac || item.Stack.Count != 1 {
		t.Errorf("the popped item = %+v, want one lilac (item %d)", item.Stack, lilac)
	}
	if name, props := servertest.StateProps(srv.BlockAt(5, 64, 0)); name != "minecraft:lilac" || props["half"] != "lower" {
		t.Errorf("the lilac's lower half became %s %v, want it standing", name, props)
	}
	if name, props := servertest.StateProps(srv.BlockAt(5, 65, 0)); name != "minecraft:lilac" || props["half"] != "upper" {
		t.Errorf("the lilac's upper half became %s %v, want it standing", name, props)
	}
}

// TestBoneMealGrowsATorchflowerCrop checks the crop: one bone meal one age,
// and the step past the last age the torchflower flower itself.
func TestBoneMealGrowsATorchflowerCrop(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:farmland", map[string]string{"moisture": "7"})
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:torchflower_crop", map[string]string{"age": "0"})
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	c.UseBlock(5, 64, 0)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	c.ExpectBlockAck()
	if name, props := servertest.StateProps(srv.BlockAt(5, 64, 0)); name != "minecraft:torchflower_crop" || props["age"] != "1" {
		t.Errorf("the crop stands as %s %v, want age=1 after one bone meal", name, props)
	}

	boneMealClicks(t, c, srv, 5, 64, 0, 5, func(name string, props map[string]string) bool {
		return name == "minecraft:torchflower"
	})
}

// TestBoneMealRipensAMangrovePropagule checks the hanging propagule: one
// age per bone meal with no roll, four clicks from 0 to 4, and the fully
// grown one no target at all, the click only acked.
func TestBoneMealRipensAMangrovePropagule(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:mangrove_propagule", map[string]string{"hanging": "true", "age": "0"})
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	for want := 1; want <= 4; want++ {
		c.UseBlock(5, 64, 0)
		c.SettleKeeping()
		srv.FlushBlockAcks()
		c.ExpectBlockAck()
		name, props := servertest.StateProps(srv.BlockAt(5, 64, 0))
		if name != "minecraft:mangrove_propagule" || props["hanging"] != "true" || props["age"] != strconv.Itoa(want) {
			t.Fatalf("the propagule stands as %s %v, want hanging age=%d after the click", name, props, want)
		}
	}

	// The fully grown propagule is no target: the click only acks, the
	// age and the item standing.
	c.UseBlock(5, 64, 0)
	c.SettleAndCollect(func(gotID int32, data []byte) {
		if gotID == protocol.IDPlayWorldEvent &&
			protocol.NewReader(data).ReadI32() == protocol.WorldEventBonemeal {
			t.Fatal("the fully grown propagule played the bone meal sparkle")
		}
	})
	if name, props := servertest.StateProps(srv.BlockAt(5, 64, 0)); name != "minecraft:mangrove_propagule" || props["age"] != "4" {
		t.Errorf("the propagule stands as %s %v, want age=4 standing", name, props)
	}
}

// TestBoneMealGrowsAFancyOak checks the fancy oak, weight 1 of the oak
// grower's 10: seed 58 lands the first bone meal's dice inside
// it, and the fancy oak stands with a trunk taller than the plain oak's
// six and branch logs off the trunk's column (vanilla TreeGrower.OAK,
// FancyTrunkPlacer).
func TestBoneMealGrowsAFancyOak(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, 58)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:oak_sapling", map[string]string{"stage": "1"})
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	c.UseBlock(5, 64, 0)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	c.ExpectBlockAck()

	trunk := int32(0)
	for {
		if name, _ := servertest.StateProps(srv.BlockAt(5, 64+trunk, 0)); name != "minecraft:oak_log" {
			break
		}
		trunk++
	}
	if trunk <= 6 {
		t.Fatalf("the fancy oak's trunk stands %d logs tall, want taller than the plain oak's 6", trunk)
	}
	branch := false
	for dx := int32(-6); dx <= 6 && !branch; dx++ {
		for dz := int32(-6); dz <= 6 && !branch; dz++ {
			if dx == 0 && dz == 0 {
				continue
			}
			for y := int32(64); y <= 64+trunk+8 && !branch; y++ {
				if name, _ := servertest.StateProps(srv.BlockAt(5+dx, y, dz)); name == "minecraft:oak_log" {
					branch = true
				}
			}
		}
	}
	if !branch {
		t.Error("the fancy oak grew no branch logs off the trunk's column")
	}
	// The canopy is settled the way the plain oak's is: every leaf loose
	// and at a distance the distance walk reached.
	for dx := int32(-10); dx <= 10; dx++ {
		for dz := int32(-10); dz <= 10; dz++ {
			for y := int32(63); y <= 64+trunk+10; y++ {
				name, props := servertest.StateProps(srv.BlockAt(5+dx, y, dz))
				if name != "minecraft:oak_leaves" {
					continue
				}
				if props["persistent"] != "false" {
					t.Errorf("the leaf at (%d, %d, %d) is persistent, want the feature's loose one", 5+dx, y, dz)
				}
				if d, err := strconv.Atoi(props["distance"]); err != nil || d < 1 || d > 6 {
					t.Errorf("the leaf at (%d, %d, %d) stands at distance %q, want the settled one to six", 5+dx, y, dz, props["distance"])
				}
			}
		}
	}
}

// TestBoneMealFancyOakWithNoRoomDoesNotGrow checks the fancy oak's free
// height: the same seed that grows one in the open, with a stone on its own
// trunk column, plants nothing and leaves the sapling standing.
func TestBoneMealFancyOakWithNoRoomDoesNotGrow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, 58)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:oak_sapling", map[string]string{"stage": "1"})
	servertest.SetFarmBlock(t, c, srv, 5, 65, 0, "minecraft:stone", nil)
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	c.UseBlock(5, 64, 0)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	c.ExpectBlockAck()

	if name, props := servertest.StateProps(srv.BlockAt(5, 64, 0)); name != "minecraft:oak_sapling" || props["stage"] != "1" {
		t.Errorf("the sapling stands as %s %v, want the stage-1 sapling it was", name, props)
	}
	for dx := int32(-6); dx <= 6; dx++ {
		for dz := int32(-6); dz <= 6; dz++ {
			for y := int32(64); y <= 80; y++ {
				if name, _ := servertest.StateProps(srv.BlockAt(5+dx, y, dz)); name == "minecraft:oak_log" {
					t.Fatalf("a log stands at (%d, %d, %d) though the stone denies the tree", 5+dx, y, dz)
				}
			}
		}
	}
}

// TestBoneMealGrowsEachWoodSaplingIntoItsOwnTree checks the species beyond
// the oak: two applications of bone meal each — the first the stage, the
// second the growth the stage opened — and every one of the birch, the
// spruce, the jungle and the acacia becomes a trunk of its own log with a
// canopy of its own leaves, the trunk inside the roll vanilla grows it by,
// the leaves loose and at the distances the settle walk leaves them
// (vanilla TreeFeatures.BIRCH, SPRUCE, JUNGLE_TREE_NO_VINE and ACACIA).
func TestBoneMealGrowsEachWoodSaplingIntoItsOwnTree(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	species := []struct {
		sapling, log, leaves string
		x, reach             int32
		minTrunk, maxTrunk   int32
		cone, slanted        bool
	}{
		{sapling: "minecraft:birch_sapling", log: "minecraft:birch_log", leaves: "minecraft:birch_leaves",
			x: 5, reach: 2, minTrunk: 5, maxTrunk: 7},
		{sapling: "minecraft:spruce_sapling", log: "minecraft:spruce_log", leaves: "minecraft:spruce_leaves",
			x: 17, reach: 3, minTrunk: 5, maxTrunk: 8, cone: true},
		{sapling: "minecraft:jungle_sapling", log: "minecraft:jungle_log", leaves: "minecraft:jungle_leaves",
			x: 29, reach: 2, minTrunk: 4, maxTrunk: 12},
		{sapling: "minecraft:acacia_sapling", log: "minecraft:acacia_log", leaves: "minecraft:acacia_leaves",
			x: 41, reach: 6, minTrunk: 5, maxTrunk: 9, slanted: true},
	}
	for _, s := range species {
		servertest.SetFarmBlock(t, c, srv, s.x, 63, 0, "minecraft:grass_block", nil)
		servertest.SetFarmBlock(t, c, srv, s.x, 64, 0, s.sapling, nil)
	}
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	for _, s := range species {
		// The player stands by the sapling — a click out of reach is
		// only acked — and its two applications are clicked in, the
		// first to the stage and the second to the tree.
		c.MoveTo(float64(s.x)-0.5, 64, 1.5, 0, 0)
		c.Settle()
		boneMealClicks(t, c, srv, s.x, 64, 0, 20, func(name string, props map[string]string) bool {
			return name == s.sapling && props["stage"] == "1"
		})
		boneMealClicks(t, c, srv, s.x, 64, 0, 30, func(name string, props map[string]string) bool {
			return name == s.log
		})

		// The trunk: the species' own logs standing where the sapling
		// stood, over the ground turned to dirt.
		trunk := int32(0)
		for {
			if name, _ := servertest.StateProps(srv.BlockAt(s.x, 64+trunk, 0)); name != s.log {
				break
			}
			trunk++
		}
		if !s.slanted {
			if trunk < s.minTrunk || trunk > s.maxTrunk {
				t.Errorf("the %s tree stands %d logs in its column, want the %d to %d of its vanilla roll",
					s.log, trunk, s.minTrunk, s.maxTrunk)
			}
		}
		if s.slanted && trunk == 0 {
			t.Errorf("the %s never took the sapling's own cell", s.log)
		}
		if name, _ := servertest.StateProps(srv.BlockAt(s.x, 63, 0)); name != "minecraft:dirt" {
			t.Errorf("the ground under the %s trunk became %s, want the dirt it leaves behind", s.log, name)
		}

		if s.cone {
			// The cone: the tip leaf one above the crown, and rows
			// two out, which the spruce's radius always reaches.
			if name, _ := servertest.StateProps(srv.BlockAt(s.x, 64+trunk, 0)); name != s.leaves {
				t.Errorf("above the spruce's crown stands %s, want the cone's tip leaf", name)
			}
			wide := false
			for dy := int32(64); dy <= 64+trunk+2 && !wide; dy++ {
				if name, _ := servertest.StateProps(srv.BlockAt(s.x+2, dy, 0)); name == s.leaves {
					wide = true
				}
			}
			if !wide {
				t.Error("the spruce's cone grew no rows two blocks out, want the radius its vanilla roll reaches")
			}
		}
		if s.slanted {
			// The fork: the trunk leans off its own column, and the
			// tree top stands the vanilla roll tall over the ground.
			off, top := int32(0), int32(0)
			for dx := -s.reach; dx <= s.reach; dx++ {
				for dz := -s.reach; dz <= s.reach; dz++ {
					for dy := int32(64); dy <= 64+s.maxTrunk+2; dy++ {
						if name, _ := servertest.StateProps(srv.BlockAt(s.x+dx, dy, dz)); name != s.log {
							continue
						}
						if dx != 0 || dz != 0 {
							off++
						}
						top = max(top, dy)
					}
				}
			}
			if off == 0 {
				t.Errorf("the %s grew no logs off its own column, want the forked trunk", s.log)
			}
			if top < 64+s.minTrunk-1 || top > 64+s.maxTrunk-1 {
				t.Errorf("the %s stands %d blocks tall at its top log, want the %d to %d of its vanilla roll",
					s.log, top-63, s.minTrunk, s.maxTrunk)
			}
		}

		// The canopy: the species' own leaves alone, every one loose
		// and settled to a distance the walk reached.
		leaves := int32(0)
		for dx := -s.reach; dx <= s.reach; dx++ {
			for dz := -s.reach; dz <= s.reach; dz++ {
				for dy := int32(64); dy <= 64+s.maxTrunk+2; dy++ {
					name, props := servertest.StateProps(srv.BlockAt(s.x+dx, dy, dz))
					if name != s.leaves {
						continue
					}
					leaves++
					if props["persistent"] != "false" {
						t.Errorf("the leaf at (%d, %d, %d) is persistent, want the feature's loose one", s.x+dx, dy, dz)
					}
					if d, err := strconv.Atoi(props["distance"]); err != nil || d < 1 || d > 6 {
						t.Errorf("the leaf at (%d, %d, %d) stands at distance %q, want the settled one to six", s.x+dx, dy, dz, props["distance"])
					}
				}
			}
		}
		switch {
		case s.slanted:
			if leaves < 30 {
				t.Errorf("the %s grew %d leaves, want the two crowned shapes at least", s.log, leaves)
			}
		case s.cone:
			if leaves < 13 {
				t.Errorf("the %s grew %d leaves, want the cone's few rows at least", s.log, leaves)
			}
		default:
			if leaves < 49 || leaves > 61 {
				t.Errorf("the %s tree grew %d leaves, want the 49 to 61 the shared canopy holds", s.log, leaves)
			}
			// The shared canopy's shape: a leaf over the crown, and
			// the wide ring solid on its far sides.
			top := 64 + trunk - 1
			if name, _ := servertest.StateProps(srv.BlockAt(s.x, top+1, 0)); name != s.leaves {
				t.Errorf("above the crown stands %s, want the plus of leaves", name)
			}
			for _, cell := range [][2]int32{{-2, 0}, {2, 0}, {0, -2}, {0, 2}} {
				if name, _ := servertest.StateProps(srv.BlockAt(s.x+cell[0], top-1, cell[1])); name != s.leaves {
					t.Errorf("the canopy at (%d, %d, %d) holds %s, want a leaf",
						s.x+cell[0], top-1, cell[1], name)
				}
			}
		}
	}
}

// TestTheSpruceAndAcaciaAskAWiderRoomThanTheOak checks the room the
// species' free-height rule asks of the world: the spruce checks a 5x5 from
// two above the sapling up and the acacia from one (vanilla
// TwoLayersFeatureSize(2, 0, 2) and (1, 0, 2)), so a wall two blocks out
// refuses both their trees, while the birch and the jungle share the oak's
// 3x3 (TwoLayersFeatureSize(1, 0, 1)) and grow beside the same wall, the
// canopies the stone clips.
func TestTheSpruceAndAcaciaAskAWiderRoomThanTheOak(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	refused := []struct {
		sapling, log string
		x, z         int32
	}{
		{sapling: "minecraft:spruce_sapling", log: "minecraft:spruce_log", x: 5, z: 4},
		{sapling: "minecraft:acacia_sapling", log: "minecraft:acacia_log", x: 5, z: -4},
	}
	unfazed := []struct {
		sapling, log string
		x, z         int32
	}{
		{sapling: "minecraft:birch_sapling", log: "minecraft:birch_log", x: 11, z: 4},
		{sapling: "minecraft:jungle_sapling", log: "minecraft:jungle_log", x: 11, z: -4},
	}
	for _, s := range append(append([]struct {
		sapling, log string
		x, z         int32
	}{}, refused...), unfazed...) {
		servertest.SetFarmBlock(t, c, srv, s.x, 64, s.z, s.sapling, map[string]string{"stage": "1"})
		for y := int32(65); y <= 73; y++ {
			servertest.SetFarmBlock(t, c, srv, s.x+2, y, s.z, "minecraft:stone", nil)
		}
	}
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	for _, s := range refused {
		c.MoveTo(float64(s.x)-0.5, 64, float64(s.z)+1.5, 0, 0)
		c.Settle()
		for range 20 {
			c.UseBlock(s.x, 64, s.z)
			c.SettleKeeping()
			c.ExpectBoneMealLanded(srv, s.x, 64, s.z)
		}
		if name, props := servertest.StateProps(srv.BlockAt(s.x, 64, s.z)); name != s.sapling || props["stage"] != "1" {
			t.Errorf("the %s stands as %s %v, want the stage-1 sapling it was", s.sapling, name, props)
		}
		for dx := int32(-3); dx <= 3; dx++ {
			for dz := int32(-3); dz <= 3; dz++ {
				for dy := int32(64); dy <= 75; dy++ {
					if name, _ := servertest.StateProps(srv.BlockAt(s.x+dx, dy, s.z+dz)); name == s.log {
						t.Errorf("a %s log stands at (%d, %d, %d) though the wall denies the tree",
							s.log, s.x+dx, dy, s.z+dz)
					}
				}
			}
		}
		for y := int32(65); y <= 73; y++ {
			if name, _ := servertest.StateProps(srv.BlockAt(s.x+2, y, s.z)); name != "minecraft:stone" {
				t.Errorf("the wall stone at (%d, %d, %d) became %s, want it standing", s.x+2, y, s.z, name)
			}
		}
	}
	for _, s := range unfazed {
		c.MoveTo(float64(s.x)-0.5, 64, float64(s.z)+1.5, 0, 0)
		c.Settle()
		boneMealClicks(t, c, srv, s.x, 64, s.z, 30, func(name string, props map[string]string) bool {
			return name == s.log
		})
	}
}

// TestBoneMealTurnsABambooSaplingToBamboo checks the sapling: the stalk
// grown over it and the sapling itself, which the vanilla updateShape turns
// bamboo once bamboo stands above it.
func TestBoneMealTurnsABambooSaplingToBamboo(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:bamboo_sapling", nil)
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	c.UseBlock(5, 64, 0)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	c.ExpectBlockAck()

	if name, props := servertest.StateProps(srv.BlockAt(5, 65, 0)); name != "minecraft:bamboo" || props["leaves"] != "small" {
		t.Errorf("above the sapling stands %s %v, want a bamboo with small leaves", name, props)
	}
	if name, props := servertest.StateProps(srv.BlockAt(5, 64, 0)); name != "minecraft:bamboo" || props["leaves"] != "none" {
		t.Errorf("the sapling stands as %s %v, want the bamboo it became", name, props)
	}
}

// TestBoneMealCyclesTheStalkBelow checks the stalk: the cell grown over a
// young stalk takes the older age, and the stalk it grew over cycles its
// own, the vanilla updateShape's answer to the taller neighbour.
func TestBoneMealCyclesTheStalkBelow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 62, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:bamboo", map[string]string{"age": "0", "leaves": "none", "stage": "0"})
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:bamboo", map[string]string{"age": "0", "leaves": "none", "stage": "0"})
	servertest.SetFarmBlock(t, c, srv, 5, 65, 0, "minecraft:bamboo", map[string]string{"age": "0", "leaves": "small", "stage": "0"})
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	c.UseBlock(5, 63, 0)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	c.ExpectBlockAck()

	if name, props := servertest.StateProps(srv.BlockAt(5, 66, 0)); name != "minecraft:bamboo" || props["age"] != "1" {
		t.Errorf("the cell grown over the stalk stands as %s %v, want a bamboo of age=1", name, props)
	}
	if name, props := servertest.StateProps(srv.BlockAt(5, 65, 0)); name != "minecraft:bamboo" || props["age"] != "1" {
		t.Errorf("the stalk grown over stands as %s %v, want its age cycled to 1", name, props)
	}
}

// TestBoneMealPlantsTheWaterItFaces checks the water: the click on the
// sturdy face a water cell borders seeds a water plant there and plays the
// bone meal sparkle at the water cell.
func TestBoneMealPlantsTheWaterItFaces(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Diver", servertest.OpsListing(t, "Diver"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	// The flat world's ground at (5, 62, 0) is already the stone the click
	// strikes, so only the water it faces needs planting.
	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:water", nil)
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	c.UseBlock(5, 62, 0)
	c.SettleKeeping()
	c.ExpectBoneMealLanded(srv, 5, 63, 0)

	name, props := servertest.StateProps(srv.BlockAt(5, 63, 0))
	if name != "minecraft:seagrass" && !registryBlockInTag(name, registry.TagIDUnderwaterBonemeals) {
		t.Errorf("the water stands as %s %v, want a water plant seeded", name, props)
	}
}

// TestBoneMealWaterPlantDoesNotSwingTheArm checks the water path's answer:
// vanilla's useOn returns SUCCESS there, so only observers receive the
// swing; the actor predicts it, unlike the growCrop branch's SUCCESS_SERVER.
func TestBoneMealWaterPlantDoesNotSwingTheArm(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Diver", servertest.OpsListing(t, "Diver"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:water", nil)
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	c.UseBlock(5, 62, 0)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	gotEvent, gotAck := false, false
	for gotID, data := range c.Packets(servertest.ExpectTimeout) {
		switch gotID {
		case protocol.IDPlayWorldEvent:
			if event := protocol.NewReader(data).ReadI32(); event == protocol.WorldEventBonemeal {
				gotEvent = true
			}
		case protocol.IDPlayAckBlockChange:
			gotAck = true
		case protocol.IDPlaySwingAnimation:
			t.Fatal("the water-plant bone meal swung the server's arm, want the client's own swing")
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
			protocol.IDPlayUpdateLight, protocol.IDPlayUpdateTime,
			protocol.IDPlaySoundEffect, protocol.IDPlayLevelParticles,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity, protocol.IDPlaySetHealth,
			protocol.IDPlaySystemChat:
			// The block updates and effects the click also sent.
		default:
			t.Fatalf("unexpected packet 0x%02X while waiting for the water-plant answer", gotID)
		}
		if gotEvent && gotAck {
			return
		}
	}
	t.Fatalf("the water-plant bone meal never answered within %s (event %v, ack %v)", servertest.ExpectTimeout, gotEvent, gotAck)
}

// TestBoneMealReadsTheFaceNotTheSequence checks the click's fields: the
// face the water grows from and the sequence the ack answers travel apart,
// so a click whose sequence is not its face still seeds the water and acks
// its own sequence.
func TestBoneMealReadsTheFaceNotTheSequence(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Diver", servertest.OpsListing(t, "Diver"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	// The stone-under-water click the water test sends, this time with the
	// sequence number 7.
	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:water", nil)
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	c.UseBlockSeq(5, 62, 0, protocol.FaceTop, 7)
	c.SettleKeeping()
	if seq := c.ExpectBoneMealLanded(srv, 5, 63, 0); seq != 7 {
		t.Errorf("the ack answered sequence %d, want the click's 7", seq)
	}

	name, props := servertest.StateProps(srv.BlockAt(5, 63, 0))
	if name != "minecraft:seagrass" && !registryBlockInTag(name, registry.TagIDUnderwaterBonemeals) {
		t.Errorf("the water stands as %s %v, want a water plant seeded", name, props)
	}
}

// TestBoneMealRipensASweetBerryBush checks the bush: one age per bone meal,
// the third the last.
func TestBoneMealRipensASweetBerryBush(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:sweet_berry_bush", map[string]string{"age": "0"})
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	c.UseBlock(5, 64, 0)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	c.ExpectBlockAck()
	c.UseBlock(5, 64, 0)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	c.ExpectBlockAck()

	name, props := servertest.StateProps(srv.BlockAt(5, 64, 0))
	if name != "minecraft:sweet_berry_bush" || props["age"] != "2" {
		t.Errorf("the bush stands as %s %v, want age=2 after two bone meals", name, props)
	}
}

// TestBoneMealThickensASeaPickle checks the pickle: filled to four, the
// water on the coral beside it seeded a pickle of its own.
func TestBoneMealThickensASeaPickle(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Diver", servertest.OpsListing(t, "Diver"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 6, 63, 0, "minecraft:brain_coral_block", nil)
	servertest.SetFarmBlock(t, c, srv, 7, 63, 0, "minecraft:brain_coral_block", nil)
	servertest.SetFarmBlock(t, c, srv, 7, 64, 0, "minecraft:water", nil)
	servertest.SetFarmBlock(t, c, srv, 6, 64, 0, "minecraft:sea_pickle", map[string]string{"pickles": "1", "waterlogged": "true"})
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	c.UseBlock(6, 64, 0)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	c.ExpectBlockAck()
	name, props := servertest.StateProps(srv.BlockAt(6, 64, 0))
	if name != "minecraft:sea_pickle" || props["pickles"] != "4" {
		t.Fatalf("the pickle stands as %s %v, want pickles=4 after one bone meal", name, props)
	}

	// The water beside the coral is one of the spread's cells; the dice
	// decide the click.
	boneMealClicks(t, c, srv, 6, 64, 0, 30, func(name string, props map[string]string) bool {
		if name != "minecraft:sea_pickle" || props["pickles"] != "4" {
			return false
		}
		spreadName, spreadProps := servertest.StateProps(srv.BlockAt(7, 64, 0))
		return spreadName == "minecraft:sea_pickle" && spreadProps["waterlogged"] == "true"
	})
	_, spreadProps := servertest.StateProps(srv.BlockAt(7, 64, 0))
	count, err := strconv.Atoi(spreadProps["pickles"])
	if err != nil || count < 1 || count > 4 {
		t.Errorf("the spread pickle carries pickles %v, want one to four", spreadProps)
	}
}

// stemSeed pins the stem test's one fruit roll: from age 6 the click that
// lands age 7 rolls one in three at speed 9, and this seed comes up inside
// it; no later click can roll again.
const stemSeed = int64(1)

// TestBoneMealGrowsAPumpkinFromAStem checks the stem: the click that lands
// age 7 sets the pumpkin beside the stem over the fruit tag's ground and
// turns the stem to face it.
func TestBoneMealGrowsAPumpkinFromAStem(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, stemSeed)

	for x := int32(4); x <= 6; x++ {
		for z := int32(-1); z <= 1; z++ {
			servertest.SetFarmBlock(t, c, srv, x, 63, z, "minecraft:farmland", map[string]string{"moisture": "7"})
		}
	}
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:pumpkin_stem", map[string]string{"age": "6"})
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	sides := []struct {
		dx, dz int32
		facing string
	}{{0, -1, "north"}, {0, 1, "south"}, {-1, 0, "west"}, {1, 0, "east"}}
	boneMealClicks(t, c, srv, 5, 64, 0, 5, func(name string, props map[string]string) bool {
		if name != "minecraft:attached_pumpkin_stem" {
			return false
		}
		for _, s := range sides {
			if neighbour, _ := servertest.StateProps(srv.BlockAt(5+s.dx, 64, 0+s.dz)); neighbour == "minecraft:pumpkin" {
				return props["facing"] == s.facing
			}
		}
		return false
	})
}

// TestBoneMealSpreadsGlowLichen checks the lichen: the carried face spreads
// where the spreader allows, here the down-face the stone below holds, so
// the one click ends with the lichen on two faces of its cell.
func TestBoneMealSpreadsGlowLichen(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	// No stone stands beside the lichen: every wrap-around spread onto the
	// neighbours' walls would be a second legal outcome, and the click is
	// meant to be read against one. The flat world's own stone is the
	// ground the down-face needs.
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:glow_lichen", map[string]string{"north": "true"})
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	c.UseBlock(5, 64, 0)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	c.ExpectBlockAck()

	want := map[string]string{
		"north": "true", "down": "true", "up": "false",
		"south": "false", "west": "false", "east": "false", "waterlogged": "false",
	}
	if name, props := servertest.StateProps(srv.BlockAt(5, 64, 0)); name != "minecraft:glow_lichen" || !maps.Equal(props, want) {
		t.Errorf("the lichen stands as %s %v, want %v", name, props, want)
	}
}

// TestBoneMealThickensAPinkPetals checks the flower bed: one bone meal adds
// one petal, the fourth the last.
func TestBoneMealThickensAPinkPetals(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:pink_petals", map[string]string{"facing": "north", "flower_amount": "1"})
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	c.UseBlock(5, 64, 0)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	c.ExpectBlockAck()

	name, props := servertest.StateProps(srv.BlockAt(5, 64, 0))
	if name != "minecraft:pink_petals" || props["flower_amount"] != "2" {
		t.Errorf("the petals stand as %s %v, want flower_amount=2", name, props)
	}
}

// TestBoneMealGrowsAPaleHangingMossDown checks the hanging moss: one bone
// meal grows the chain one cell down, the old tip un-tipped, the new one
// tipped.
func TestBoneMealGrowsAPaleHangingMossDown(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	// The stone the moss hangs from: a base with nothing above it does not
	// survive the wake the grown tip gives it, vanilla's canSurvive.
	servertest.SetFarmBlock(t, c, srv, 5, 66, 0, "minecraft:stone", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 65, 0, "minecraft:pale_hanging_moss", map[string]string{"tip": "true"})
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	c.UseBlock(5, 65, 0)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	c.ExpectBlockAck()

	name, props := servertest.StateProps(srv.BlockAt(5, 65, 0))
	if name != "minecraft:pale_hanging_moss" || props["tip"] != "false" {
		t.Errorf("the old tip stands as %s %v, want tip=false", name, props)
	}
	name, props = servertest.StateProps(srv.BlockAt(5, 64, 0))
	if name != "minecraft:pale_hanging_moss" || props["tip"] != "true" {
		t.Errorf("below it hangs %s %v, want a tipped pale_hanging_moss", name, props)
	}
}

// TestBoneMealCostsOneInSurvival checks the cost: one paid out of the stack
// whether or not the dice came up.
func TestBoneMealCostsOneInSurvival(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Farmer", servertest.OpsListing(t, "Farmer"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:farmland", map[string]string{"moisture": "7"})
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:wheat", map[string]string{"age": "0"})
	boneMeal, ok := registry.ItemID("minecraft:bone_meal")
	if !ok {
		t.Fatal("the item registry has no bone meal")
	}
	srv.GiveSlot("Farmer", player.HotbarSlot, protocol.ItemStack{ItemID: boneMeal, Count: 3})
	c.MoveToGround(4.5, 64, 1.5, true)
	c.Settle()

	c.UseBlock(5, 64, 0)
	slot, stack := c.ExpectContainerSlot()
	if slot != int16(player.HotbarSlot) {
		t.Errorf("corrected slot = %d, want %d (the stack was spent from)", slot, player.HotbarSlot)
	}
	if stack.ItemID != boneMeal || stack.Count != 2 {
		t.Errorf("held slot = %+v, want two bone meals left of three", stack)
	}
	ripened := servertest.AwaitBlockState(t, c, 5, 64, 0)
	age, err := strconv.Atoi(servertest.CropsProps(t, ripened, "minecraft:wheat")["age"])
	if err != nil {
		t.Fatalf("the ripened wheat carries no age: %v", err)
	}
	if age < 2 || age > 5 {
		t.Errorf("the bone meal ripened the wheat to age=%d, want two to five", age)
	}
}

// TestBoneMealSpreadsARedShrub checks the bush spread on a red shrub: a copy
// in one horizontal neighbour on grass, one bone meal spent.
func TestBoneMealSpreadsARedShrub(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Farmer", servertest.OpsListing(t, "Farmer"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	neighbours := [4][2]int32{{4, 0}, {6, 0}, {5, -1}, {5, 1}}
	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	for _, n := range neighbours {
		servertest.SetFarmBlock(t, c, srv, n[0], 63, n[1], "minecraft:grass_block", nil)
	}
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:red_shrub", nil)
	boneMeal, ok := registry.ItemID("minecraft:bone_meal")
	if !ok {
		t.Fatal("the item registry has no bone meal")
	}
	srv.GiveSlot("Farmer", player.HotbarSlot, protocol.ItemStack{ItemID: boneMeal, Count: 3})
	c.MoveToGround(3.5, 64, 2.5, true)
	c.Settle()

	c.UseBlock(5, 64, 0)
	slot, stack := c.ExpectContainerSlot()
	if slot != int16(player.HotbarSlot) || stack.ItemID != boneMeal || stack.Count != 2 {
		t.Errorf("held slot %d = %+v, want two bone meals left of three", slot, stack)
	}
	c.SettleKeeping()

	shrubs := 0
	for _, n := range neighbours {
		switch name, _ := servertest.StateProps(srv.BlockAt(n[0], 64, n[1])); name {
		case "minecraft:red_shrub":
			shrubs++
		case "minecraft:air":
		default:
			t.Errorf("the neighbour (%d, 64, %d) became %s", n[0], n[1], name)
		}
	}
	if shrubs != 1 {
		t.Errorf("%d neighbours hold a red shrub, want one", shrubs)
	}
	if name, _ := servertest.StateProps(srv.BlockAt(5, 64, 0)); name != "minecraft:red_shrub" {
		t.Errorf("the bonemealed red shrub became %s", name)
	}
}

// TestBoneMealIsFreeInCreative checks the same click in creative: the wheat
// ripens, the stack untouched.
func TestBoneMealIsFreeInCreative(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Farmer", servertest.OpsListing(t, "Farmer"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:farmland", map[string]string{"moisture": "7"})
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:wheat", map[string]string{"age": "0"})
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	c.UseBlock(5, 64, 0)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	c.ExpectBlockAck()

	ripened := servertest.AwaitBlockState(t, c, 5, 64, 0)
	age, err := strconv.Atoi(servertest.CropsProps(t, ripened, "minecraft:wheat")["age"])
	if err != nil {
		t.Fatalf("the ripened wheat carries no age: %v", err)
	}
	if age < 2 || age > 5 {
		t.Errorf("the bone meal ripened the wheat to age=%d, want two to five", age)
	}
	c.SettleAndCollect(func(packetID int32, _ []byte) {
		switch packetID {
		case protocol.IDPlayContainerSlot, protocol.IDPlayEntityEquipment:
			t.Fatalf("packet 0x%02X corrected the held stack behind a creative click", packetID)
		}
	})
}

func registryBlockInTag(name string, tag registry.BlockTagID) bool {
	b, ok := registry.BlockByName(name)
	return ok && b.InTag(tag)
}

func TestBoneMealGrowsAzaleaTree(t *testing.T) {
	for _, plant := range []string{"azalea", "flowering_azalea"} {
		t.Run(plant, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))
			srv.StopTick()
			servertest.SeedPhysics(t, srv, servertest.CropsSeed)
			servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:dirt", nil)
			servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:"+plant, nil)
			servertest.GiveFarmItem(t, c, "minecraft:bone_meal")
			boneMealClicks(t, c, srv, 5, 64, 0, 30, func(name string, _ map[string]string) bool { return name == "minecraft:oak_log" })
			if registry.BlockOf(srv.BlockAt(5, 63, 0)) != registry.BlockRootedDirt {
				t.Error("azalea did not place rooted dirt")
			}
			for x := int32(0); x <= 10; x++ {
				for z := int32(-5); z <= 5; z++ {
					for y := int32(64); y <= 76; y++ {
						if registry.BlockOf(srv.BlockAt(x, y, z)) == registry.BlockAzaleaLeaves {
							return
						}
					}
				}
			}
			t.Error("azalea tree has no azalea leaves")
		})
	}
}

func TestBoneMealGrowsStandingMangrove(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))
	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:dirt", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:mangrove_propagule", map[string]string{"hanging": "false"})
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")
	boneMealClicks(t, c, srv, 5, 64, 0, 30, func(name string, _ map[string]string) bool {
		// Mangrove's trunk is raised above the propagule's cell.
		return name != "minecraft:mangrove_propagule"
	})
	for x := int32(-3); x <= 13; x++ {
		for z := int32(-8); z <= 8; z++ {
			for y := int32(64); y <= 90; y++ {
				if registry.BlockOf(srv.BlockAt(x, y, z)) == registry.BlockMangroveLog {
					return
				}
			}
		}
	}
	t.Fatal("standing propagule did not grow mangrove logs with bone meal")
}

// patchConfig is the creative setup with only the chunks around the test patch.
func patchConfig() *config.Config {
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	return cfg
}

func survivalPatchConfig() *config.Config {
	cfg := servertest.SurvivalConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	return cfg
}

// TestBoneMealGrowsAHugeMushroom checks the 0.4 roll: clicked until it lands,
// a red mushroom on dirt becomes a stem topped with cap blocks.
func TestBoneMealGrowsAHugeMushroom(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, patchConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:dirt", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:red_mushroom", nil)
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	boneMealClicks(t, c, srv, 5, 64, 0, 60, func(name string, props map[string]string) bool {
		return name == "minecraft:mushroom_stem"
	})
	caps := 0
	for dy := range int32(10) {
		for dx := int32(-3); dx <= 3; dx++ {
			for dz := int32(-3); dz <= 3; dz++ {
				if name, _ := servertest.StateProps(srv.BlockAt(5+dx, 64+dy, dz)); name == "minecraft:red_mushroom_block" {
					caps++
				}
			}
		}
	}
	if caps == 0 {
		t.Error("the huge mushroom has no red_mushroom_block cap")
	}
}

// TestBoneMealSpreadsMoss checks the moss patch: the stone round the clicked
// moss block turns to moss and vegetation stands above it.
func TestBoneMealSpreadsMoss(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, patchConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	if name, _ := servertest.StateProps(srv.BlockAt(7, 63, 0)); name != "minecraft:stone" {
		t.Fatalf("the test floor is %s, want stone", name)
	}
	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:moss_block", nil)
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	c.UseBlock(5, 63, 0)
	c.Settle()
	srv.FlushBlockAcks()

	moss, plants := 0, 0
	for dx := int32(-3); dx <= 3; dx++ {
		for dz := int32(-3); dz <= 3; dz++ {
			if name, _ := servertest.StateProps(srv.BlockAt(5+dx, 63, dz)); name == "minecraft:moss_block" && (dx != 0 || dz != 0) {
				moss++
			}
			switch name, _ := servertest.StateProps(srv.BlockAt(5+dx, 64, dz)); name {
			case "minecraft:moss_carpet", "minecraft:short_grass", "minecraft:azalea":
				plants++
			}
		}
	}
	if moss == 0 {
		t.Error("no neighbouring stone turned to moss_block")
	}
	if plants == 0 {
		t.Error("no moss_carpet, short_grass or azalea stands above the patch")
	}
}

// TestBoneMealRefusesMossUnderABlock checks the air test above the moss: a
// covered moss block refuses and keeps the item.
func TestBoneMealRefusesMossUnderABlock(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, survivalPatchConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:moss_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:stone", nil)
	boneMeal, ok := registry.ItemID("minecraft:bone_meal")
	if !ok {
		t.Fatal("the item registry has no bone meal")
	}
	srv.GiveSlot("Gardener", player.HotbarSlot, protocol.ItemStack{ItemID: boneMeal, Count: 3})
	c.MoveToGround(4.5, 64, 1.5, true)
	c.Settle()

	c.UseBlock(5, 63, 0)
	c.Settle()
	srv.FlushBlockAcks()

	if got := srv.InventorySlot("Gardener", player.HotbarSlot); got.ItemID != boneMeal || got.Count != 3 {
		t.Errorf("held slot = %+v, want the three bone meals kept", got)
	}

	if name, _ := servertest.StateProps(srv.BlockAt(6, 63, 0)); name != "minecraft:stone" {
		t.Errorf("the stone beside the covered moss became %s, want it left alone", name)
	}
}

// TestBoneMealGrowsAFungusIntoAHugeFungus checks that a crimson fungus on
// crimson nylium, bone-mealed until the 40% roll lands, becomes a crimson stem
// standing where it was.
func TestBoneMealGrowsAFungusIntoAHugeFungus(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:crimson_nylium", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:crimson_fungus", nil)
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	boneMealClicks(t, c, srv, 5, 64, 0, 40, func(name string, _ map[string]string) bool {
		return name == "minecraft:crimson_stem"
	})
	if name, _ := servertest.StateProps(srv.BlockAt(5, 65, 0)); name != "minecraft:crimson_stem" {
		t.Errorf("above the first stem stands %s, want the stem to continue", name)
	}
}

// TestBoneMealOnWarpedNyliumPlacesCover checks that bone meal on warped nylium
// scatters warped roots or nether sprouts over the nylium around it.
func TestBoneMealOnWarpedNyliumPlacesCover(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	for dx := int32(-2); dx <= 2; dx++ {
		for dz := int32(-2); dz <= 2; dz++ {
			servertest.SetFarmBlock(t, c, srv, 5+dx, 63, dz, "minecraft:warped_nylium", nil)
		}
	}
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	c.UseBlock(5, 63, 0)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	c.ExpectBlockAck()

	found := false
	for dx := int32(-2); dx <= 2; dx++ {
		for dz := int32(-2); dz <= 2; dz++ {
			switch name, _ := servertest.StateProps(srv.BlockAt(5+dx, 64, dz)); name {
			case "minecraft:warped_roots", "minecraft:nether_sprouts":
				found = true
			}
		}
	}
	if !found {
		t.Error("bone meal on warped nylium placed no warped roots or nether sprouts")
	}
}

// TestBoneMealPlainsTulipsFollowThePatchNoise checks flower_plain's
// NoiseThresholdProvider: tulips grow only where the position noise is below
// its threshold. The lawn at x=20624 lies in such a patch on every seed (the
// noise has a fixed seed of its own); the lawn at x=4 does not.
// vanilla: NoiseThresholdProvider.getState
func TestBoneMealPlainsTulipsFollowThePatchNoise(t *testing.T) {
	t.Parallel()
	tulips := map[string]bool{
		"minecraft:orange_tulip": true, "minecraft:red_tulip": true,
		"minecraft:pink_tulip": true, "minecraft:white_tulip": true,
	}
	for _, tc := range []struct {
		name    string
		x       int32
		wantTul bool
	}{{"below the threshold", 20624, true}, {"above the threshold", 4, false}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))
			if tc.x > 16 {
				c.ChatCommand(fmt.Sprintf("tp @s %d.5 64 1.5", tc.x))
				servertest.UntilTicks(t, srv, 400, "lawn chunk sent", func() bool {
					return srv.PlayerPosition("Gardener").X > 16 && srv.ChunkLoadedInDimension(world.DimensionName, tc.x, 1)
				})
				servertest.RunTicks(t, srv, 20)
				c.Settle()
			}
			srv.StopTick()
			servertest.SeedPhysics(t, srv, servertest.CropsSeed)
			// One chunk, so the biome read is the lawn's own.
			srv.SetOverworldBiome(tc.x, 1, "minecraft:plains")
			for x := tc.x; x <= tc.x+2; x++ {
				for z := int32(0); z <= 2; z++ {
					servertest.SetFarmBlock(t, c, srv, x, 63, z, "minecraft:grass_block", nil)
				}
			}
			servertest.GiveFarmItem(t, c, "minecraft:bone_meal")
			air := registry.MustState("minecraft:air", nil)
			grown, tulipSeen := 0, false
			for range 40 {
				c.UseBlock(tc.x+1, 63, 1)
				c.SettleKeeping()
				srv.FlushBlockAcks()
				c.ExpectBlockAck()
				for x := tc.x; x <= tc.x+2; x++ {
					for z := int32(0); z <= 2; z++ {
						for y := int32(64); y <= 66; y++ {
							name, _ := servertest.StateProps(srv.BlockAt(x, y, z))
							if tulips[name] {
								tulipSeen = true
							}
							if name == "minecraft:dandelion" {
								grown++
							}
							srv.SetBlock(x, y, z, air)
						}
					}
				}
			}
			if tulipSeen != tc.wantTul {
				t.Errorf("tulips grown = %v, want %v (dandelions grown: %d)", tulipSeen, tc.wantTul, grown)
			}
		})
	}
}
