package advancements

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestLightningNearAVillagerObtainsLightningRodWithVillagerNoFire: a bolt
// that sets nothing on fire counts when a villager the bolt did not hit
// stands within 15 blocks, and not when the bolt's own box hurts the villager
// or none is near.
// vanilla: LightningStrikeTrigger.TriggerInstance.matches
func TestLightningNearAVillagerObtainsLightningRodWithVillagerNoFire(t *testing.T) {
	t.Parallel()
	const adv, crit = "minecraft:adventure/lightning_rod_with_villager_no_fire", "lightning_rod_with_villager_no_fire"
	for _, tc := range []struct {
		name     string
		villager float64 // villager's offset from the bolt along x, -1000 for none
		want     bool
	}{{"bystander", 8, true}, {"struck", 1, false}, {"none", -1000, false}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.Join(t, servertest.SurvivalTestConfig(), "Actor")
			srv.StopTick()
			srv.SetDifficulty(0)
			pos := srv.PlayerPosition("Actor")
			if tc.villager > -100 {
				srv.SpawnMob("minecraft:villager", pos.X+10+tc.villager, pos.Y, pos.Z)
			}
			// A rod in the strike cell leaves nowhere for the fire.
			srv.SetBlock(int32(pos.X)+10, int32(pos.Y), int32(pos.Z),
				stateOf(t, "minecraft:lightning_rod", map[string]string{"facing": "up", "powered": "false", "waterlogged": "false"}))
			srv.StrikeLightning(pos.X+10, pos.Y, pos.Z)
			srv.TickBolts(40)
			if got := srv.AdvancementObtained("Actor", adv, crit); got != tc.want {
				t.Errorf("obtained = %v, want %v", got, tc.want)
			}
		})
	}
}
