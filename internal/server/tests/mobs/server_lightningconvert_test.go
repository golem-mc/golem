package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestLightningTurnsAVillagerIntoAWitch: a bolt on a villager outside
// peaceful replaces it with a persistent witch that keeps the villager's
// name; on peaceful the villager stays and is hurt instead.
// vanilla: Villager.thunderHit
func TestLightningTurnsAVillagerIntoAWitch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		difficulty uint8
		converts   bool
	}{{"normal", 2, true}, {"peaceful", 0, false}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.Join(t, servertest.SurvivalTestConfig(), "Struck")
			srv.StopTick()
			srv.SetDifficulty(tc.difficulty)
			pos := srv.PlayerPosition("Struck")
			villager := srv.SpawnMob("minecraft:villager", pos.X+1, pos.Y, pos.Z)
			srv.SetMobNamed(villager, "Hex", false)

			srv.StrikeLightning(pos.X+1, pos.Y, pos.Z)
			srv.TickBolts(1)

			counts := kindsNear(srv)
			if !tc.converts {
				if counts["minecraft:villager"] != 1 || counts["minecraft:witch"] != 0 {
					t.Fatalf("mobs after a peaceful bolt = %v, want the villager alone", counts)
				}
				if hp, _ := srv.MobHealth(villager); hp >= 20 {
					t.Errorf("the peaceful villager has %v health, want it hurt by the bolt", hp)
				}
				return
			}
			if counts["minecraft:villager"] != 0 || counts["minecraft:witch"] != 1 {
				t.Fatalf("mobs after the bolt = %v, want one witch and no villager", counts)
			}
			for _, m := range srv.Mobs() {
				if m.Kind != "minecraft:witch" {
					continue
				}
				if m.Name != "Hex" {
					t.Errorf("the witch is named %q, want the villager's Hex", m.Name)
				}
				if !srv.MobPersistent(m.ID) {
					t.Error("the witch is not persistent")
				}
				// The conversion takes the bolt's first harm tick; the witch is
				// hurt only by a later one.
				if hp, _ := srv.MobHealth(m.ID); hp != 26 {
					t.Errorf("the witch has %v health, want a fresh witch's 26", hp)
				}
			}
		})
	}
}
