package mobs

import (
	"math"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// splashPool builds a stone basin with a 3x3 pool of water at y 140 and
// stands the player beside it, above any terrain.
func splashPool(t *testing.T, prep ...func(*server.Server)) (*server.Server, *servertest.Client, *atomic.Int32, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), prep...)
	srv.SetDayTime(18000)
	c := connect().Join("Swimmer")
	stone := registry.BlockStone.DefaultState()
	for x := int32(-1); x <= 9; x++ {
		for z := int32(-1); z <= 9; z++ {
			srv.SetBlock(x, 139, z, stone)
		}
	}
	for x := int32(2); x <= 6; x++ {
		for z := int32(2); z <= 6; z++ {
			ring := x == 2 || x == 6 || z == 2 || z == 6
			state := registry.BlockWater.DefaultState()
			if ring {
				state = stone
			}
			srv.SetBlock(x, 140, z, state)
		}
	}
	c.MoveToGround(0.5, 140, 0.5, true)
	c.Settle()
	var splashes, flaps, bounces atomic.Int32
	srv.ObserveGameEvents(func(event string, x, y, z float64) {
		switch event {
		case "minecraft:splash":
			splashes.Add(1)
		case "minecraft:flap":
			flaps.Add(1)
		case "minecraft:bounce":
			bounces.Add(1)
		}
	})
	return srv, c, &splashes, &flaps, &bounces
}

// soundsHeard returns the names of the sounds the client receives next.
func soundsHeard(t *testing.T, c *servertest.Client) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySoundEffect {
			name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
			names[name] = true
		}
	})
	return names
}

func TestAMobFallingIntoWaterSplashes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind, sound string
		silent      bool
	}{
		{"minecraft:cow", "minecraft:entity.generic.splash", false},
		{"minecraft:zombie", "minecraft:entity.hostile.splash", false},
		{"minecraft:cow", "", true},
	} {
		t.Run(tc.kind+tc.sound, func(t *testing.T) {
			t.Parallel()
			srv, c, splashes, _, _ := splashPool(t)
			id := srv.SpawnMob(tc.kind, 4.5, 144.5, 4.5)
			srv.SetMobSilent(id, tc.silent)
			servertest.UntilTicks(t, srv, 200, "the splash game event", func() bool { return splashes.Load() > 0 })
			names := soundsHeard(t, c)
			if tc.silent {
				for name := range names {
					if name == "minecraft:entity.generic.splash" || name == "minecraft:entity.hostile.splash" {
						t.Fatalf("a silent mob made %s", name)
					}
				}
				return
			}
			if !names[tc.sound] {
				t.Fatalf("heard %v, want %s", names, tc.sound)
			}
		})
	}
}

func TestAnItemTossedIntoWaterSplashes(t *testing.T) {
	t.Parallel()
	srv, c, splashes, _, _ := splashPool(t)
	srv.SpawnItemEntity(4.5, 144, 4.5, "minecraft:stone", 1)
	servertest.UntilTicks(t, srv, 200, "the splash game event", func() bool { return splashes.Load() > 0 })
	if names := soundsHeard(t, c); !names["minecraft:entity.generic.splash"] {
		t.Fatalf("heard %v, want entity.generic.splash", names)
	}
}

func TestAFlyingParrotFlaps(t *testing.T) {
	t.Parallel()
	srv, c, _, flaps, _ := splashPool(t)
	srv.SpawnMob("minecraft:parrot", 4.5, 146, 4.5)
	servertest.UntilTicks(t, srv, 400, "the flap game event", func() bool { return flaps.Load() > 0 })
	if names := soundsHeard(t, c); !names["minecraft:entity.parrot.fly"] {
		t.Fatalf("heard %v, want entity.parrot.fly", names)
	}
}

func TestAnItemDroppedOnASlimeBlockPostsABounceEvent(t *testing.T) {
	t.Parallel()
	srv, _, _, _, bounces := splashPool(t)
	srv.SetBlock(0, 140, 8, registry.BlockSlimeBlock.DefaultState())
	srv.SpawnItemEntity(0.5, 144, 8.5, "minecraft:stone", 1)
	servertest.UntilTicks(t, srv, 200, "the bounce game event", func() bool { return bounces.Load() > 0 })
}

// spiderSplashVolume drops a spider, with a skeleton rider or alone, into the
// pool and returns the loudest splash heard: a seated rider dipping in too
// splashes at its own zero movement.
func spiderSplashVolume(t *testing.T, ridden bool) float32 {
	t.Helper()
	srv, c, splashes, _, _ := splashPool(t)
	want := int32(1)
	if ridden {
		want = 2 // the rider dips in too
		srv.RunLine(`summon minecraft:spider 4.5 144.5 4.5 {Passengers:[{id:"minecraft:skeleton"}]}`)
	} else {
		srv.SpawnMob("minecraft:spider", 4.5, 144.5, 4.5)
	}
	servertest.UntilTicks(t, srv, 200, "the splash game events", func() bool { return splashes.Load() >= want })
	volume := float32(-1)
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySoundEffect {
			return
		}
		if name, _, _, _, _, v, _ := servertest.ReadAmbient(t, data); name == "minecraft:entity.hostile.splash" {
			volume = max(volume, v)
		}
	})
	if volume < 0 {
		t.Fatal("no hostile splash heard")
	}
	return volume
}

// TestARiddenMobSplashesAtItsControllingPassengersMovement: a spider whose
// skeleton rider controls it splashes at 0.9 of the rider's movement, the
// seated rider's gravity step (0.08*0.98), not at 0.2 of its own fall.
// vanilla: Entity.doWaterSplashEffect, Entity.rideTick
func TestARiddenMobSplashesAtItsControllingPassengersMovement(t *testing.T) {
	t.Parallel()
	const riderVolume = 0.9 * 0.08 * 0.98
	if v := spiderSplashVolume(t, true); math.Abs(float64(v)-riderVolume) > 1e-4 {
		t.Errorf("ridden splash volume %v, want %v (0.9 of the rider's gravity step)", v, riderVolume)
	}
	if v := spiderSplashVolume(t, false); v <= riderVolume+1e-4 {
		t.Errorf("unridden splash volume %v, want above %v (0.2 of its own fall)", v, riderVolume)
	}
}

// TestASlimeResizedInWaterSplashes merges a new size onto a held slime: in
// the pool a seed whose first roll of twenty is zero splashes, one whose roll
// is not stays quiet, and the same lucky seed beside the pool stays quiet.
// vanilla: AbstractCubeMob.onSyncedDataUpdated
func TestASlimeResizedInWaterSplashes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		x      float64
		seed   int64
		splash bool
	}{
		{"in water, lucky roll", 4.5, 11, true},
		{"in water, other roll", 4.5, 0, false},
		{"out of water", 0.5, 11, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c, splashes, _, _ := splashPool(t, servertest.OpsListing(t, "Swimmer"))
			servertest.HoldTicks(t, srv)
			id := srv.SpawnMob("minecraft:slime", tc.x, 140, 4.5)
			srv.SetMobNoAI(id)
			// Settled in for a few ticks: the spawn's own first touch of
			// water does not splash.
			servertest.RunTicks(t, srv, 5)
			if n := splashes.Load(); n != 0 {
				t.Fatalf("the slime splashed %d times before the resize", n)
			}
			srv.SeedMob(id, tc.seed)
			c.ChatCommand("data merge entity @e[type=minecraft:slime,limit=1] {Size:2}")
			c.Settle()
			servertest.RunTicks(t, srv, 2)
			names := soundsHeard(t, c)
			if got := splashes.Load() > 0; got != tc.splash {
				t.Fatalf("splash game event %v, want %v", got, tc.splash)
			}
			if names["minecraft:entity.slime.splash"] || names["minecraft:entity.hostile.splash"] || names["minecraft:entity.generic.splash"] {
				if !tc.splash {
					t.Fatalf("heard %v, want no splash", names)
				}
			} else if tc.splash {
				t.Fatalf("heard %v, want a splash sound", names)
			}
		})
	}
}

// projectileSplashes summons a projectile falling into the pool and reports
// whether the client heard entity.generic.splash once the splash game event
// was posted within 20 ticks.
// vanilla: Entity.updateFluidInteraction, Entity.doWaterSplashEffect
func projectileSplashes(t *testing.T, kind string) {
	t.Helper()
	srv, c, splashes, _, _ := splashPool(t)
	srv.RunLine("summon " + kind + " 4.5 143 4.5 {Motion:[0d,-0.6d,0d]}")
	servertest.UntilTicks(t, srv, 20, "the splash game event", func() bool { return splashes.Load() >= 1 })
	if names := soundsHeard(t, c); !names["minecraft:entity.generic.splash"] {
		t.Fatalf("heard %v, want entity.generic.splash", names)
	}
}

func TestAnArrowSplashesIntoWater(t *testing.T) {
	t.Parallel()
	projectileSplashes(t, "minecraft:arrow")
}

func TestASnowballSplashesIntoWater(t *testing.T) {
	t.Parallel()
	projectileSplashes(t, "minecraft:snowball")
}

// TestACastHookSplashesIntoWater casts from the basin's rim south into the
// pool.
func TestACastHookSplashesIntoWater(t *testing.T) {
	t.Parallel()
	srv, c, splashes, _, _ := splashPool(t)
	srv.GiveSlot("Swimmer", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:fishing_rod"), Count: 1})
	c.MoveTo(4.5, 141, 2.5, 0, 40)
	c.Settle()
	servertest.UseHeld(c, 0, 40)
	servertest.UntilTicks(t, srv, 40, "the splash game event", func() bool { return splashes.Load() >= 1 })
	if names := soundsHeard(t, c); !names["minecraft:entity.generic.splash"] {
		t.Fatalf("heard %v, want entity.generic.splash", names)
	}
}

func TestAWoolItemSplashTripsNoSculkSensor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		item  string
		quiet bool
	}{
		{"minecraft:white_wool", true},
		{"minecraft:stone", false},
	} {
		t.Run(tc.item, func(t *testing.T) {
			t.Parallel()
			srv, _, splashes, _, _ := splashPool(t)
			srv.SetBlock(8, 140, 8, registry.BlockSculkSensor.DefaultState())
			srv.SpawnItemEntity(4.5, 144, 4.5, tc.item, 1)
			servertest.UntilTicks(t, srv, 200, "the splash game event", func() bool { return splashes.Load() > 0 })
			servertest.RunTicks(t, srv, 40)
			phase := registry.StateValue(srv.BlockAt(8, 140, 8), registry.PropSculkSensorPhase)
			if quiet := phase == "inactive"; quiet != tc.quiet {
				t.Fatalf("%s: sensor phase %q, want quiet=%v", tc.item, phase, tc.quiet)
			}
		})
	}
}
