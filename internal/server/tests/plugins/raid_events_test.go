package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Raids.createOrExtendRaid
func TestPluginRaidTriggerEvent(t *testing.T) {
	t.Parallel()
	cancel := true
	var stages []plugin.RaidStage
	srv := servertest.NewTestServerWithPlugins(t, servertest.HostileTestConfig(), []plugin.Registration{{Name: "raids", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		plugin.Listen(s, func(e *plugin.RaidEvent) {
			stages = append(stages, e.Stage)
			if e.Stage == plugin.RaidTrigger {
				if e.Player == nil || e.Player.Name() != "Hero" {
					t.Errorf("trigger player %v", e.Player)
				}
				_ = e.World.Time()
				e.SetCancelled(cancel)
			}
		})
		return nil
	}}}})
	srv.SetDifficulty(2)
	c := servertest.DialServer(t, srv)
	c.Join("Hero")
	servertest.HoldTicks(t, srv)
	pos := srv.PlayerPosition("Hero")
	bell := [3]int32{int32(pos.X) + 3, int32(pos.Y), int32(pos.Z)}
	srv.SetBlock(bell[0], bell[1], bell[2], registry.MustState("minecraft:bell", nil))
	srv.SpawnMob("minecraft:villager", float64(bell[0])+0.5, float64(bell[1]), float64(bell[2])+1.5)
	servertest.UntilTicks(t, srv, 400, "the villager claims the bell", func() bool {
		return srv.VillageNear("minecraft:overworld", bell[0], bell[1], bell[2])
	})
	srv.GiveEffect("Hero", "minecraft:bad_omen", 100000, 0, true)
	c.ReadTicks(srv, 603, func(int32, []byte) bool { return false })
	if len(stages) != 1 || stages[0] != plugin.RaidTrigger || len(srv.Raids()) != 0 {
		t.Fatalf("refused trigger: stages %v, raids %d", stages, len(srv.Raids()))
	}
}
