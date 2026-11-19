package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestASummonedProjectilePostsAShootVibrationOnItsFirstTick: a body the
// summon command makes has not been shot, so a sculk sensor beside it hears
// the first tick; one saved as HasBeenShot stays silent.
// vanilla: Projectile.tick
func TestASummonedProjectilePostsAShootVibrationOnItsFirstTick(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, summon string
		hears        bool
	}{
		{"arrow", `summon minecraft:arrow 1.5 64.5 2.5 {NoGravity:1b}`, true},
		{"arrow-shot", `summon minecraft:arrow 1.5 64.5 2.5 {NoGravity:1b,HasBeenShot:1b}`, false},
		{"snowball", `summon minecraft:snowball 1.5 64.5 2.5 {NoGravity:1b}`, true},
		{"snowball-shot", `summon minecraft:snowball 1.5 64.5 2.5 {NoGravity:1b,HasBeenShot:1b}`, false},
		{"firework", `summon minecraft:firework_rocket 1.5 64.5 2.5 {NoGravity:1b,LifeTime:200}`, true},
		{"firework-shot", `summon minecraft:firework_rocket 1.5 64.5 2.5 {NoGravity:1b,LifeTime:200,HasBeenShot:1b}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c, _ := servertest.BoatPlayer(t, "survival", "Shooter", 0)
			srv.SetBlock(4, 64, 2, servertest.MustState(t, "minecraft:sculk_sensor", nil))
			c.Settle()
			srv.RunLine(tc.summon)
			if tc.hears {
				servertest.UntilTicks(t, srv, 10, "the sensor hears the shot", func() bool {
					return sensorPhase(t, srv) == "active"
				})
				return
			}
			servertest.RunTicks(t, srv, 10)
			if phase := sensorPhase(t, srv); phase != "inactive" {
				t.Fatalf("sensor phase = %q for a body already shot, want inactive", phase)
			}
		})
	}
}
