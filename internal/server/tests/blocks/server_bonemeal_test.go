package blocks

import (
	"slices"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
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

// TestMushroomBoneMealCeiling checks that a small mushroom whose huge form
// cannot fit under the build ceiling refuses the bone meal: red needs
// Y+4+2 and brown Y+4+3 inside the world, and only an accepted use costs one,
// whether or not the roll grows the mushroom.
func TestMushroomBoneMealCeiling(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Farmer", servertest.OpsListing(t, "Farmer"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	boneMeal, ok := registry.ItemID("minecraft:bone_meal")
	if !ok {
		t.Fatal("the item registry has no bone meal")
	}
	for _, tc := range []struct {
		mushroom string
		y        int32
		accepted bool
	}{
		{"minecraft:red_mushroom", 313, true},
		{"minecraft:red_mushroom", 314, false},
		{"minecraft:brown_mushroom", 312, true},
		{"minecraft:brown_mushroom", 313, false},
	} {
		servertest.SetFarmBlock(t, c, srv, 5, tc.y-1, 0, "minecraft:mycelium", nil)
		servertest.SetFarmBlock(t, c, srv, 5, tc.y, 0, tc.mushroom, nil)
		srv.GiveSlot("Farmer", player.HotbarSlot, protocol.ItemStack{ItemID: boneMeal, Count: 8})
		c.MoveToGround(4.5, float64(tc.y), 1.5, true)
		c.Settle()

		c.UseBlock(5, tc.y, 0)
		c.SettleKeeping()
		srv.FlushBlockAcks()

		want := int32(8)
		if tc.accepted {
			want = 7
		}
		if got := srv.InventorySlot("Farmer", player.HotbarSlot); got.Count != want {
			t.Errorf("%s at Y=%d: %d bone meal left, want %d", tc.mushroom, tc.y, got.Count, want)
		}
	}
}

// TestNyliumBonemealUsesDimensionCeiling checks that nylium checks the active
// dimension's build height rather than the overworld's: at Y=255 in the Nether
// bone meal is refused with no growth event, while at Y=254 with air above it
// consumes one and plays the growth event.
// vanilla: NyliumBlock.isValidBonemealTarget, BoneMealItem.growCrop
func TestNyliumBonemealUsesDimensionCeiling(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.NetherTestConfig(t, servertest.SurvivalConfig()), "Farmer")
	servertest.HoldTicks(t, srv)
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	srv.MovePlayerToDimension("Farmer", world.NetherName, 4.5, 254, -2.5)
	if !c.Pump(servertest.JoinTimeout, nil, func(int32, []byte) bool {
		return srv.PlayerDimension("Farmer") == world.NetherName &&
			srv.ChunkLoadedInDimension(world.NetherName, 4, 0)
	}) {
		t.Fatal("the Nether never loaded around the test column")
	}
	c.Settle()

	boneMeal := int32(registry.ItemBoneMeal)
	air := servertest.BlockState(t, "minecraft:air")
	var seq int32

	for _, nylium := range []string{"minecraft:crimson_nylium", "minecraft:warped_nylium"} {
		state := servertest.BlockState(t, nylium)
		for _, tc := range []struct {
			y         int32
			wantCount int32
			wantEvent bool
		}{
			{y: 255, wantCount: 3, wantEvent: false},
			{y: 254, wantCount: 2, wantEvent: true},
		} {
			srv.SetBlockInDimension(world.NetherName, 4, 255, 0, air)
			srv.SetBlockInDimension(world.NetherName, 4, tc.y, 0, state)
			srv.GiveSlot("Farmer", player.HotbarSlot, protocol.ItemStack{ItemID: boneMeal, Count: 3})
			c.Settle()

			seq++
			c.UseBlockSeq(4, tc.y, 0, protocol.FaceNorth, seq)
			c.SettleKeeping()
			srv.FlushBlockAcks()

			gotEvent, gotAck := false, false
			for gotID, data := range c.Packets(servertest.ExpectTimeout) {
				switch gotID {
				case protocol.IDPlayWorldEvent:
					r := protocol.NewReader(data)
					if r.ReadI32() == protocol.WorldEventBonemeal {
						ex, ey, ez := r.ReadPosition()
						if ex != 4 || ey != tc.y || ez != 0 {
							t.Errorf("%s at Y=%d: bone meal event at (%d, %d, %d), want (4, %d, 0)", nylium, tc.y, ex, ey, ez, tc.y)
						}
						gotEvent = true
					}
				case protocol.IDPlayAckBlockChange:
					if protocol.NewReader(data).ReadVarInt() == seq {
						gotAck = true
					}
				}
				if gotAck {
					break
				}
			}
			if !gotAck {
				t.Fatalf("%s at Y=%d: block ack %d did not arrive", nylium, tc.y, seq)
			}
			if gotEvent != tc.wantEvent {
				t.Errorf("%s at Y=%d: growth level event sent = %v, want %v", nylium, tc.y, gotEvent, tc.wantEvent)
			}
			if got := srv.InventorySlot("Farmer", player.HotbarSlot); got.Count != tc.wantCount {
				t.Errorf("%s at Y=%d: %d bone meal left, want %d", nylium, tc.y, got.Count, tc.wantCount)
			}
		}
	}
}
