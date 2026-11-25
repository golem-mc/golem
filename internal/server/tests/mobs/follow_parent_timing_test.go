package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: FollowParentGoal.tick, GroundPathNavigation.canUpdatePath
func TestCalfFollowTimingOnFlatGround(t *testing.T) {
	t.Parallel()
	type sample struct {
		tick          int
		calfX, adultX float64
	}
	for _, tc := range []struct {
		name      string
		setup     []string
		want      []sample
		adultY    float64
		firstNear int
	}{
		{
			name: "stationary adult",
			setup: []string{
				"summon minecraft:cow 0.5 180 0.5 {Age:-24000}",
				"summon minecraft:cow 7.5 180 0.5 {NoAI:1b}",
				"summon minecraft:cow -7.5 180 0.5 {NoAI:1b}",
			},
			adultY:    180,
			firstNear: 40,
			want: []sample{
				{1, 0.5, 7.5}, {9, 0.5, 7.5}, {10, 0.5625, 7.5},
				{30, 3.22540789, 7.5}, {45, 5.29038565, 7.5}, {90, 6.13988742, 7.5},
			},
		},
		{
			name: "mounted adult",
			setup: []string{
				`summon minecraft:pig -0.1 180 0.5 {OnGround:1b,Motion:[0.45d,0d,0d],Passengers:[{id:"minecraft:cow",NoAI:1b}]}`,
				"summon minecraft:cow 8.85 180 0.5 {Age:-24000}",
			},
			adultY:    180.86874998,
			firstNear: 45,
			want: []sample{
				{1, 8.85, 0.35}, {3, 8.85, 0.81928706}, {9, 8.85, 1.08105829},
				{10, 8.7875, 1.08429270}, {30, 6.12459211, 1.08429270},
				{45, 4.05961435, 1.08429270}, {90, 2.79711692, 1.08429270},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.MobTestConfig()
			cfg.LevelSeed = "823"
			srv, _ := servertest.Join(t, cfg, "Watcher", func(s *server.Server) {
				servertest.FreezeUntilHeld(t, s)
			})
			servertest.HoldTicks(t, srv)
			srv.RunLine("fill -12 179 -2 12 179 2 minecraft:smooth_stone")
			for _, line := range tc.setup {
				srv.RunLine(line)
			}
			var calf, adult int32
			for _, cow := range srv.Mobs() {
				if cow.Kind != "minecraft:cow" {
					continue
				}
				if cow.Baby {
					calf = cow.ID
				} else if cow.X > 0 || cow.Vehicle != 0 {
					adult = cow.ID
				}
			}
			if calf == 0 || adult == 0 || calf%2 != 0 {
				t.Fatalf("want an even-ID calf and its adult, got IDs %d and %d", calf, adult)
			}
			srv.SeedMob(calf, 823)

			// Vanilla 26.3 /data get Pos samples with an even-ID calf, whose
			// second full goal tick is tick 2. Allow for trig rounding.
			const tolerance = 0.00002
			lastTick, firstNear, next := tc.want[len(tc.want)-1].tick, 0, 0
			for tick := 1; tick <= lastTick; tick++ {
				servertest.RunTicks(t, srv, 1)
				x, y, z, calfOK := srv.MobPos(calf)
				ax, ay, az, adultOK := srv.MobPos(adult)
				if !calfOK || !adultOK {
					t.Fatalf("tick %d: calf present = %t, adult present = %t", tick, calfOK, adultOK)
				}
				if firstNear == 0 && math.Hypot(x-ax, z-az) < 3 {
					firstNear = tick
				}
				want := tc.want[next]
				if tick != want.tick {
					continue
				}
				next++
				if math.Abs(x-want.calfX) > tolerance || math.Abs(y-180) > tolerance || math.Abs(z-0.5) > tolerance {
					t.Errorf("tick %d: calf at (%.8f, %.8f, %.8f), want (%.8f, 180, 0.5)", tick, x, y, z, want.calfX)
				}
				if math.Abs(ax-want.adultX) > tolerance || math.Abs(ay-tc.adultY) > tolerance || math.Abs(az-0.5) > tolerance {
					t.Errorf("tick %d: adult at (%.8f, %.8f, %.8f), want (%.8f, %.8f, 0.5)", tick, ax, ay, az, want.adultX, tc.adultY)
				}
			}
			if firstNear != tc.firstNear {
				t.Errorf("horizontal gap first under three blocks at tick %d, want %d", firstNear, tc.firstNear)
			}
		})
	}
}
