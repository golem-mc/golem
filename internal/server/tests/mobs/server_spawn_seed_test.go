package mobs

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestNaturalSpawnSeededOutcome pins spawn order, positions, facing and pack
// babies on an unchanged world; subsequent passes also expose shifted RNG draws.
func TestNaturalSpawnSeededOutcome(t *testing.T) {
	srv, _ := zombieTests(t, servertest.SpawningTestConfig())
	srv.SetDayTime(midnight)
	hash := sha256.New()
	for seed := int64(1); seed <= 80; seed++ {
		srv.SpawnPass(seed)
		for _, m := range srv.Mobs() {
			fmt.Fprintf(hash, "%s %g %g %g %g %t %t\n", m.Kind, m.X, m.Y, m.Z, m.Yaw, m.Baby, m.Vehicle != 0)
		}
	}
	got := fmt.Sprintf("%x", hash.Sum(nil))
	const want = "8930dbb3f1fa588adb3b7304d667ca0397a284bebdccf30909fad9261cc2b02f"
	if got != want {
		t.Fatalf("seeded spawn outcome = %s, want %s", got, want)
	}
}
