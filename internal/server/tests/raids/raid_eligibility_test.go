package raids

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Raider.finalizeSpawn
func TestOnlyNonNaturalWitchesJoinRaids(t *testing.T) {
	t.Parallel()
	srv, c, bell := village(t, rules.DifficultyNormal)
	startRaid(t, c, srv, 0)
	x, y, z := float64(bell[0])+2.5, float64(bell[1]), float64(bell[2])+0.5
	egg := srv.SpawnMob("minecraft:witch", x, y, z)
	natural := srv.SpawnMobNatural("minecraft:witch", x+2, y, z)
	srv.SetMobNoAI(egg)
	srv.SetMobNoAI(natural)
	servertest.RunTicks(t, srv, 40)
	if in, _ := srv.MobRaid(egg); !in {
		t.Error("the spawn-egg witch did not join the raid")
	}
	if in, _ := srv.MobRaid(natural); in {
		t.Error("the natural witch joined the raid")
	}
}
