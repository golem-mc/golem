package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// A listener's rewritten destination is absolute, so a coordinate-only /tp
// turns the mount to its angles and the rider keeps its offset to the mount.
// vanilla: Entity.calculatePassengerTransition
func TestPluginTeleportRedirectTurnsMountAndRider(t *testing.T) {
	t.Parallel()
	var host plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "rot", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		host = s
		plugin.Listen(s, func(e *plugin.EntityTeleportEvent) {
			e.To.Yaw, e.To.Pitch = 90, 25
		})
		return nil
	}}}})
	defer srv.HoldChunk(0, 0)()
	host.Scheduler().RunTask(func() {
		host.Dispatch(host.Console(), `summon chicken 3.5 64 3.5 {NoAI:1b,NoGravity:1b,Rotation:[30f,10f],Passengers:[`+
			`{id:"minecraft:pig",NoAI:1b,NoGravity:1b,Rotation:[0f,0f]}]}`)
		host.Dispatch(host.Console(), "tp @e[type=chicken,limit=1] 5.5 64 5.5")
	})
	servertest.RunTicks(t, srv, 1)
	got := map[string][2]float32{}
	for _, m := range srv.Mobs() {
		got[m.Kind] = [2]float32{m.Yaw, m.Pitch}
	}
	if c, p := got["minecraft:chicken"], got["minecraft:pig"]; c != [2]float32{90, 25} || p != [2]float32{60, 15} {
		t.Fatalf("chicken %v, pig %v; want [90 25] and [60 15]", c, p)
	}
}
