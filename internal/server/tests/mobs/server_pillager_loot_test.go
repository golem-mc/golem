package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// ominousBottles counts the ominous bottles lying in the overworld.
func ominousBottles(srv *server.Server) int {
	n := 0
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Item == "minecraft:ominous_bottle" {
			n += int(it.Count)
		}
	}
	return n
}

// TestACaptainPillagerDropsAnOminousBottle: the pillager's loot table is the
// raid captain's alone, one ominous bottle; a pillager that leads no wave
// drops none.
// vanilla: loot_table entities/pillager (raider is_captain)
func TestACaptainPillagerDropsAnOminousBottle(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.HostileTestConfig(), "Target")
	servertest.HoldTicks(t, srv)
	plain := srv.SpawnMob(pillagerKind, 40.5, 64, 40.5)
	srv.KillMob(plain)
	servertest.RunTicks(t, srv, 2)
	if got := ominousBottles(srv); got != 0 {
		t.Fatalf("a pillager without the banner dropped %d ominous bottles, want 0", got)
	}
	captain := srv.SpawnCaptain(pillagerKind, 44.5, 64, 44.5)
	srv.KillMob(captain)
	servertest.RunTicks(t, srv, 2)
	if got := ominousBottles(srv); got != 1 {
		t.Errorf("a captain pillager dropped %d ominous bottles, want 1", got)
	}
}
