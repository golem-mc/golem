package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// endermitesOf lists the endermites standing in the world.
func endermitesOf(srv *server.Server) []server.TestMob {
	var found []server.TestMob
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:endermite" {
			found = append(found, m)
		}
	}
	return found
}

// pearlLandings throws one pearl per seed straight down from the player's
// feet, restoring their health after each landing, and returns the places
// the player stood at the throws that left an endermite behind, with the
// total thrown.
func pearlLandings(srv *server.Server, seeds int) (places []server.TestMob, thrown int) {
	for seed := int64(1); seed <= int64(seeds); seed++ {
		srv.SetHealth("Watcher", 20)
		before := len(endermitesOf(srv))
		at := srv.PlayerPosition("Watcher")
		srv.ThrowEnderPearlOf("Watcher", at.X, at.Y+1, at.Z, 0, 90, seed)
		srv.TickEntities(10)
		thrown++
		if found := endermitesOf(srv); len(found) > before {
			spawned := found[len(found)-1]
			spawned.X, spawned.Y, spawned.Z = spawned.X-at.X, spawned.Y-at.Y, spawned.Z-at.Z
			places = append(places, spawned)
		}
	}
	return places, thrown
}

func endermiteTests(t *testing.T, difficulty uint8) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := zombieTests(t, servertest.SpawningTestConfig())
	srv.SetDifficulty(difficulty)
	return srv, c
}

// TestAPearlLandingSpawnsAnEndermiteAtTheOwnersOldPlace checks the 5% roll:
// of 400 pearls some but far from all leave an endermite, standing where the
// thrower stood before the landing carried them off.
// vanilla: ThrownEnderpearl.onHit
func TestAPearlLandingSpawnsAnEndermiteAtTheOwnersOldPlace(t *testing.T) {
	t.Parallel()
	srv, _ := endermiteTests(t, 2)
	places, thrown := pearlLandings(srv, 400)
	if len(places) < 5 || len(places) > 45 {
		t.Fatalf("%d of %d landings left an endermite, want about 5%%", len(places), thrown)
	}
	for _, p := range places {
		if p.X != 0 || p.Y != 0 || p.Z != 0 {
			t.Errorf("an endermite stood %v,%v,%v from the owner's old place", p.X, p.Y, p.Z)
		}
	}
}

// TestNoEndermiteFromAPearlOnPeacefulOrWithoutSpawnRules pins the two
// refusals of the landing's spawn.
func TestNoEndermiteFromAPearlOnPeacefulOrWithoutSpawnRules(t *testing.T) {
	t.Parallel()
	srv, _ := endermiteTests(t, 0)
	if places, _ := pearlLandings(srv, 200); len(places) != 0 {
		t.Errorf("%d endermites on peaceful, want none", len(places))
	}
	srv.SetDifficulty(2)
	srv.SetGameRule(world.RuleSpawnMonsters.String(), false)
	if places, _ := pearlLandings(srv, 200); len(places) != 0 {
		t.Errorf("%d endermites with spawn_monsters off, want none", len(places))
	}
	srv.SetGameRule(world.RuleSpawnMonsters.String(), true)
	srv.SetGameRule(world.RuleSpawnMobs.String(), false)
	if places, _ := pearlLandings(srv, 200); len(places) != 0 {
		t.Errorf("%d endermites with spawn_mobs off, want none", len(places))
	}
}

// TestAnEndermiteIsDiscardedAfterItsLifetime checks Endermite.life: 2400
// ticks and it is gone, unless it is persistent.
// vanilla: Endermite.aiStep
func TestAnEndermiteIsDiscardedAfterItsLifetime(t *testing.T) {
	t.Parallel()
	srv, _ := endermiteTests(t, 2)
	srv.SpawnMob("minecraft:endermite", 0.5, 64, 0.5)
	srv.SpawnMob("minecraft:endermite", 1.5, 64, 0.5)
	srv.TickMobs(2399)
	if got := len(endermitesOf(srv)); got != 2 {
		t.Fatalf("%d endermites after 2399 ticks, want 2", got)
	}
	srv.TickMobs(1)
	if got := len(endermitesOf(srv)); got != 0 {
		t.Errorf("%d endermites after 2400 ticks, want none", got)
	}
}

// TestAPersistentEndermiteStays pins the PersistenceRequired exemption.
func TestAPersistentEndermiteStays(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SpawningTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"))
	srv.StopTick()
	srv.SetDifficulty(2)
	endermite, _ := registry.EntityTypeID("minecraft:endermite")
	c.ChatCommand("summon minecraft:endermite 0.5 64 0.5 {PersistenceRequired:1b,NoAI:1b}")
	c.ExpectMobSpawn(endermite)
	srv.TickMobs(2500)
	if got := len(endermitesOf(srv)); got != 1 {
		t.Errorf("%d persistent endermites after 2500 ticks, want 1", got)
	}
}

// TestAnEndermiteHasItsAttributes pins the health, which Endermite.
// createAttributes sets at 8.
func TestAnEndermiteHasItsAttributes(t *testing.T) {
	t.Parallel()
	srv, _ := endermiteTests(t, 2)
	id := srv.SpawnMob("minecraft:endermite", 0.5, 64, 0.5)
	if hp, _ := srv.MobHealth(id); hp != 8 {
		t.Errorf("endermite health %v, want 8", hp)
	}
}
