package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
)

// creeperShotBy kills a creeper at one health with an arrow the given kind of
// mob owns, and returns the item names that lie where it died.
func creeperShotBy(t *testing.T, srv *server.Server, shooterKind string) []string {
	t.Helper()
	return creeperShotThen(t, srv, shooterKind, 1, false)
}

// creeperShotThen shoots a creeper of the given health and, when kill is set,
// /kills it after the arrow landed.
func creeperShotThen(t *testing.T, srv *server.Server, shooterKind string, health float64, kill bool) []string {
	t.Helper()
	creeper := srv.SpawnMob("minecraft:creeper", 4.5, 65, 0.5)
	shooter := srv.SpawnMob(shooterKind, 9.5, 65, 0.5)
	srv.SetMobNoAI(creeper)
	srv.SetMobNoAI(shooter)
	srv.SetMobHealth(creeper, health)
	if srv.ShootMobArrowAt(shooter, creeper) == 0 {
		t.Fatal("the arrow was not stood in the world")
	}
	for range 10 {
		srv.TickEntities(1)
		srv.TickMobs(1)
	}
	if kill {
		if !srv.KillMob(creeper) {
			t.Fatal("the creeper was not alive to kill")
		}
		for range 10 {
			srv.TickEntities(1)
			srv.TickMobs(1)
		}
	}
	var names []string
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		names = append(names, it.Item)
	}
	return names
}

func countDiscs(names []string) int {
	n := 0
	for _, name := range names {
		if strings.HasPrefix(name, "minecraft:music_disc_") {
			n++
		}
	}
	return n
}

// TestCreeperShotBySkeletonDropsADisc pins the creeper's second loot pool,
// which wants a skeleton-tagged attacker; a cow's arrow does not open it.
func TestCreeperShotBySkeletonDropsADisc(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)
	if got := creeperShotBy(t, srv, "minecraft:skeleton"); countDiscs(got) != 1 {
		t.Errorf("items after a skeleton shot the creeper = %v, want one music disc", got)
	}

	srv, _ = flameScene(t)
	if got := creeperShotBy(t, srv, "minecraft:cow"); countDiscs(got) != 0 {
		t.Errorf("items after a cow shot the creeper = %v, want no disc", got)
	}
}

// TestCreeperKilledAfterSkeletonArrowDropsNoDisc pins that the loot attacker
// is the source of the killing blow: a /kill after the arrow has none.
func TestCreeperKilledAfterSkeletonArrowDropsNoDisc(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)
	if got := creeperShotThen(t, srv, "minecraft:skeleton", 40, true); countDiscs(got) != 0 {
		t.Errorf("items after /kill following a skeleton's arrow = %v, want no disc", got)
	}
}

// flameScene stops the loop and returns the server a burning arrow is flown
// against.
func flameScene(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()
	return srv, c
}
