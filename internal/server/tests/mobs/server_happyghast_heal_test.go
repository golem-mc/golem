package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAHurtHappyGhastHealsSlowlyAndFastInRain: under a clear sky a hurt
// happy ghast regains 1 health per 600 ticks; in rain, one per 20.
// vanilla: HappyGhast.continuousHeal
func TestAHurtHappyGhastHealsSlowlyAndFastInRain(t *testing.T) {
	t.Parallel()
	srv, _, _ := wolfSceneWith(t, "Rider", servertest.MobTestConfig())
	id := srv.SpawnMob("minecraft:happy_ghast", 4.5, 64, 0.5)
	srv.SetMobNoAI(id)
	srv.SetMobHealth(id, 10)
	health := func() float64 {
		h, _ := srv.MobHealth(id)
		return h
	}

	srv.TickMobs(100)
	if got := health(); got != 10 {
		t.Fatalf("health = %v after 100 clear ticks, want 10", got)
	}
	srv.TickMobs(500)
	if got := health(); got != 11 {
		t.Fatalf("health = %v after 600 clear ticks, want 11", got)
	}

	// The cloud layer's bottom is 192.33 and the ghast is 4 tall, so at y=190
	// its box reaches in; one above the top (196.33) stays slow.
	inCloud := srv.SpawnMob("minecraft:happy_ghast", 4.5, 190, 0.5)
	aboveCloud := srv.SpawnMob("minecraft:happy_ghast", 4.5, 200, 0.5)
	for _, g := range []int32{inCloud, aboveCloud} {
		srv.SetMobNoAI(g)
		srv.SetMobHealth(g, 10)
	}
	srv.TickMobs(100)
	if h, _ := srv.MobHealth(inCloud); h != 15 {
		t.Fatalf("health in clouds = %v after 100 ticks, want 15", h)
	}
	if h, _ := srv.MobHealth(aboveCloud); h != 10 {
		t.Fatalf("health above clouds = %v after 100 ticks, want 10", h)
	}

	srv.SetWeatherState(true, false, 100000, 0, 0)
	// The rain level climbs 0.01 a tick and counts as raining past 0.2.
	for range 30 {
		srv.TickWeather()
	}
	srv.TickMobs(100)
	if got := health(); got != 16 {
		t.Fatalf("health = %v after 100 rain ticks, want 16 (one per 20 ticks)", got)
	}
}
