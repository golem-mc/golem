package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The saplings, black-box: the random tick the vanilla SaplingBlock runs on
// its own clock, which carries the stage-0 sapling to stage 1 and then grows
// the tree the bone meal's own roll grows. The dice are pinned and the tick
// rule turned up the way the farm tests pin and turn them up, so a pass
// count the test can afford is a verdict the dice cannot flip.

// saplingSeed is the seed the sapling test pins the dice with.
const saplingSeed = int64(21)

// saplingTickSpeed is the random tick rule the sapling test runs at, against
// the default three, so the passes below are tens of hits on the one cell the
// test watches.
const saplingTickSpeed = 50

// TestSaplingGrowsOnRandomTicks checks the sapling's own random tick: an oak
// sapling on dirt in the open sky, under a turned-up random tick rule, is
// carried through the stage it stands at and grown into a tree.
func TestSaplingGrowsOnRandomTicks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, saplingSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), saplingTickSpeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:dirt", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:oak_sapling", nil)

	srv.TickRandomTicks(20000)

	for y := int32(64); y <= 80; y++ {
		if name, _, _ := registry.BlockStateName(srv.BlockAt(5, y, 0)); name == "minecraft:oak_log" {
			return
		}
	}
	name, props, _ := registry.BlockStateName(srv.BlockAt(5, 64, 0))
	t.Errorf("the sapling stands as %s %s after 20000 random ticks, want the oak grown", name, props)
}

// TestMangrovePropaguleAgesOnRandomTicks checks the hanging branch of the
// vanilla MangrovePropaguleBlock.randomTick: it random-ticks at midnight,
// where its above cell is lit at four, and climbs an age per hit with no roll
// and no light check, so it ripens where a sapling would draw nothing.
func TestMangrovePropaguleAgesOnRandomTicks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, saplingSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), saplingTickSpeed)
	srv.SetDayTime(18000)

	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:mangrove_propagule", map[string]string{"hanging": "true", "age": "0"})

	srv.TickRandomTicks(20000)

	name, props := servertest.StateProps(srv.BlockAt(5, 64, 0))
	if name != "minecraft:mangrove_propagule" || props["hanging"] != "true" || props["age"] != "4" {
		t.Errorf("the propagule stands as %s %v after 20000 random ticks, want hanging age=4", name, props)
	}
}

// TestMangrovePropaguleGrowsOnRandomTicks checks the standing branch: a
// standing propagule advances through stage 1 and grows its configured tree.
func TestMangrovePropaguleGrowsOnRandomTicks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, saplingSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), saplingTickSpeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:dirt", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:mangrove_propagule", map[string]string{"hanging": "false", "stage": "0"})

	srv.TickRandomTicks(20000)

	for x := int32(-3); x <= 13; x++ {
		for z := int32(-8); z <= 8; z++ {
			for y := int32(64); y <= 90; y++ {
				if registry.BlockOf(srv.BlockAt(x, y, z)) == registry.BlockMangroveLog {
					return
				}
			}
		}
	}
	t.Fatal("standing propagule did not grow mangrove logs")
}

func TestConfiguredSaplingsGrowOnRandomTicks(t *testing.T) {
	for _, tc := range []struct {
		name        string
		sapling     string
		log, leaves registry.Block
		square      bool
	}{
		{"dark_oak", "dark_oak_sapling", registry.BlockDarkOakLog, registry.BlockDarkOakLeaves, true},
		{"cherry", "cherry_sapling", registry.BlockCherryLog, registry.BlockCherryLeaves, false},
		{"pale_oak", "pale_oak_sapling", registry.BlockPaleOakLog, registry.BlockPaleOakLeaves, true},
		{"mangrove", "mangrove_propagule", registry.BlockMangroveLog, registry.BlockMangroveLeaves, false},
		{"mega_spruce", "spruce_sapling", registry.BlockSpruceLog, registry.BlockSpruceLeaves, true},
		{"mega_jungle", "jungle_sapling", registry.BlockJungleLog, registry.BlockJungleLeaves, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))
			srv.StopTick()
			servertest.SeedPhysics(t, srv, saplingSeed)
			srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), saplingTickSpeed)
			// Mangrove roots need ground around the trunk; keep the whole plot
			// in the join fixture's loaded chunks, above its terrain.
			for x := int32(1); x <= 15; x++ {
				for z := int32(1); z <= 15; z++ {
					srv.SetBlock(x, 99, z, registry.BlockDirt.DefaultState())
				}
			}
			side := int32(1)
			if tc.square {
				side = 2
			}
			for dx := range side {
				for dz := range side {
					servertest.SetFarmBlock(t, c, srv, 7+dx, 100, 7+dz, "minecraft:"+tc.sapling, nil)
				}
			}
			srv.TickRandomTicks(20000)
			found := map[registry.Block]bool{}
			for x := range int32(16) {
				for z := range int32(16) {
					for y := int32(100); y < 140; y++ {
						found[registry.BlockOf(srv.BlockAt(x, y, z))] = true
					}
				}
			}
			if !found[tc.log] || !found[tc.leaves] {
				t.Fatalf("growth missing log %s or leaves %s", tc.log.Name(), tc.leaves.Name())
			}
			if tc.name == "mangrove" && !found[registry.BlockMangroveRoots] {
				t.Error("mangrove grew without roots")
			}
			if tc.square {
				for dx := range int32(2) {
					for dz := range int32(2) {
						if got := registry.BlockOf(srv.BlockAt(7+dx, 100, 7+dz)); got != tc.log {
							t.Errorf("trunk (%d,%d) = %s, want %s", dx, dz, got.Name(), tc.log.Name())
						}
					}
				}
			}
		})
	}
}

func TestSingleDarkOakSaplingDoesNotGrow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))
	srv.StopTick()
	servertest.SeedPhysics(t, srv, saplingSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), saplingTickSpeed)
	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:dirt", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:dark_oak_sapling", nil)
	srv.TickRandomTicks(20000)
	state := srv.BlockAt(5, 64, 0)
	if registry.BlockOf(state) != registry.BlockDarkOakSapling || registry.StateValue(state, registry.PropStage) != "1" {
		t.Fatalf("single sapling became %d, want stage-1 dark oak sapling", state)
	}
}

// TestBoneMealGrowsAPoplarSapling checks the poplar grower: a poplar sapling
// on grass, bone-mealed until it grows, stands as one of the three poplars,
// a trunk of poplar logs where the sapling stood.
func TestBoneMealGrowsAPoplarSapling(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))
	srv.StopTick()
	servertest.SeedPhysics(t, srv, saplingSeed)
	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:poplar_sapling", nil)
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	boneMealClicks(t, c, srv, 5, 64, 0, 30, func(name string, _ map[string]string) bool {
		return name == "minecraft:poplar_log"
	})
	if name, _ := servertest.StateProps(srv.BlockAt(5, 65, 0)); name != "minecraft:poplar_log" {
		t.Errorf("above the trunk's foot stands %s, want the poplar's trunk", name)
	}
}
