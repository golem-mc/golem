package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// cowOn stands a NoAI cow on a block of the kind given, with the main loop
// stopped, wearing boots, and returns it with the server.
func cowOn(t *testing.T, floor string, boots protocol.ItemStack) (*server.Server, int32) {
	t.Helper()
	return mobOn(t, "minecraft:cow", floor, boots)
}

// mobOn is cowOn for the mob kind given.
func mobOn(t *testing.T, kind, floor string, boots protocol.ItemStack) (*server.Server, int32) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Watcher")
	c.Settle()
	srv.StopTick()
	release := srv.HoldChunk(5, 5)
	t.Cleanup(release)
	srv.SetBlock(5, 63, 5, servertest.BlockState(t, floor))
	if srv.BlockAt(5, 63, 5) != servertest.BlockState(t, floor) {
		t.Fatal("the floor's chunk was not loaded")
	}
	id := srv.SpawnMob(kind, 5.5, 64, 5.5)
	srv.SetMobNoAI(id)
	if boots.Count > 0 {
		srv.SetMobWorn(id, 0, boots)
	}
	return srv, id
}

// TestAMobOnAMagmaBlockTakesHotFloorDamage: a cow standing on magma loses a
// point whenever its invulnerability window opens, one in ten ticks; on
// stone it loses nothing, and frost walker boots hold the burn off.
// vanilla: MagmaBlock.stepOn
func TestAMobOnAMagmaBlockTakesHotFloorDamage(t *testing.T) {
	t.Parallel()
	frost := servertest.EnchantedStack(t, "minecraft:diamond_boots", "minecraft:frost_walker", 1)
	for _, tc := range []struct {
		name, floor string
		boots       protocol.ItemStack
		hurt        bool
	}{
		{"magma", "minecraft:magma_block", protocol.ItemStack{}, true},
		{"stone", "minecraft:stone", protocol.ItemStack{}, false},
		{"frost walker", "minecraft:magma_block", frost, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, id := cowOn(t, tc.floor, tc.boots)
			before, _ := srv.MobHealth(id)
			srv.TickMobs(35)
			after, _ := srv.MobHealth(id)
			if tc.hurt && before-after < 3 {
				t.Errorf("health %v -> %v after 35 ticks on magma, want at least 3 lost", before, after)
			}
			if !tc.hurt && after != before {
				t.Errorf("health %v -> %v, want unchanged", before, after)
			}
		})
	}
}

// TestAFireImmuneMobOnAMagmaBlockTakesNoDamage: a magma cube is fire immune,
// so the magma floor leaves its health alone while a cow on it is hurt.
// vanilla: MagmaBlock.stepOn
func TestAFireImmuneMobOnAMagmaBlockTakesNoDamage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind string
		hurt bool
	}{
		{"minecraft:magma_cube", false},
		{"minecraft:cow", true},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			srv, id := mobOn(t, tc.kind, "minecraft:magma_block", protocol.ItemStack{})
			before, _ := srv.MobHealth(id)
			srv.TickMobs(35)
			after, _ := srv.MobHealth(id)
			if tc.hurt != (after < before) {
				t.Errorf("health %v -> %v on magma, hurt want %v", before, after, tc.hurt)
			}
		})
	}
}

// campfireState is the campfire state with the lit property given.
func campfireState(t *testing.T, lit string) uint16 {
	t.Helper()
	state, ok := registry.FindState("minecraft:campfire", map[string]string{"lit": lit})
	if !ok {
		t.Fatalf("no campfire state with lit=%s", lit)
	}
	return state
}

// TestAMobInALitCampfireIsBitten: a cow standing in a lit campfire loses
// health without catching fire; in an unlit one it loses none.
// vanilla: CampfireBlock.entityInside
func TestAMobInALitCampfireIsBitten(t *testing.T) {
	t.Parallel()
	for _, lit := range []string{"true", "false"} {
		t.Run("lit="+lit, func(t *testing.T) {
			t.Parallel()
			srv, id := cowOn(t, "minecraft:stone", protocol.ItemStack{})
			srv.SetBlock(5, 64, 5, campfireState(t, lit))
			before, _ := srv.MobHealth(id)
			srv.TickMobs(35)
			after, _ := srv.MobHealth(id)
			if (lit == "true") != (after < before) {
				t.Errorf("lit=%s: health %v -> %v", lit, before, after)
			}
			if fire := srv.MobFireTicks(id); fire != 0 {
				t.Errorf("lit=%s: a campfire set the cow alight for %d ticks", lit, fire)
			}
		})
	}
}

// TestRainPutsOutOnlyAnUnroofedBurningMob: in rain a burning cow under open
// sky is extinguished, one under a roof keeps burning.
// vanilla: Entity.isInRain, Level.isRainingAt
func TestRainPutsOutOnlyAnUnroofedBurningMob(t *testing.T) {
	t.Parallel()
	srv, roofed := cowOn(t, "minecraft:stone", protocol.ItemStack{})
	srv.SetBlock(5, 63, 9, servertest.BlockState(t, "minecraft:stone"))
	open := srv.SpawnMob("minecraft:cow", 5.5, 64, 9.5)
	srv.SetMobNoAI(open)
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			srv.SetBlock(5+dx, 67, 5+dz, servertest.BlockState(t, "minecraft:stone"))
		}
	}
	srv.SetWeatherState(true, false, 100000, 100000, 0)
	srv.SetMobFire(roofed, 1000)
	srv.SetMobFire(open, 1000)
	// The rain level fades in 0.01 a tick and counts as raining past 0.2.
	for range 30 {
		srv.TickWeather()
	}
	srv.TickMobs(5)
	if !srv.Raining() {
		t.Fatal("the weather did not turn to rain")
	}
	if srv.MobFireTicks(roofed) == 0 {
		t.Error("the roofed cow was put out by rain it is sheltered from")
	}
	if srv.MobFireTicks(open) != 0 {
		t.Error("the unroofed cow kept burning in the rain")
	}
}
