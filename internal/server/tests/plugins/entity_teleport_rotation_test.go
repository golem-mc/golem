package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
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

// A listener's angles replace a facing clause's: the mount takes them and the
// rider turns by its offset to the mount, not by a zero turn.
// vanilla: Entity.calculatePassengerTransition
func TestPluginTeleportRedirectOverridesFacingForRider(t *testing.T) {
	t.Parallel()
	got := runRotationScenario(t, func(e *plugin.EntityTeleportEvent) { e.To.Yaw, e.To.Pitch = 90, 25 },
		"tp @e[type=chicken,limit=1] 5.5 64 5.5 facing 100 64 5.5")
	if c, p := got["minecraft:chicken"], got["minecraft:pig"]; c != [2]float32{90, 25} || p != [2]float32{-80, 90} {
		t.Fatalf("chicken %v, pig %v; want [90 25] and [-80 90]", c, p)
	}
}

// The event of a target an earlier target already turned reports that
// target's live angles, so a position-only redirect keeps them.
// vanilla: TeleportCommand.performTeleport
func TestPluginTeleportEventSeesLiveAnglesOfLaterTarget(t *testing.T) {
	t.Parallel()
	got := runRotationScenario(t, func(e *plugin.EntityTeleportEvent) {
		if e.Entity.Type() == registry.EntityTypePig {
			e.To.Pos.X++
		}
	}, "execute positioned 3.5 64 3.5 rotated 0 0 run tp @e[tag=pair,sort=nearest] 5.5 64 5.5 ~-170 ~120")
	if c, p := got["minecraft:chicken"], got["minecraft:pig"]; c != [2]float32{190, -90} || p != [2]float32{190, -90} {
		t.Fatalf("chicken %v, pig %v; want [190 -90] twice", c, p)
	}
}

// runRotationScenario teleports a chicken at [170 -80] with a level pig
// passenger by cmd under listener, in one held tick.
func runRotationScenario(t *testing.T, listener func(*plugin.EntityTeleportEvent), cmd string) map[string][2]float32 {
	t.Helper()
	var host plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "rot", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		host = s
		plugin.Listen(s, listener)
		return nil
	}}}})
	defer srv.HoldChunk(0, 0)()
	host.Scheduler().RunTask(func() {
		host.Dispatch(host.Console(), `summon chicken 3.5 64 3.5 {NoAI:1b,NoGravity:1b,Tags:["pair"],Rotation:[170f,-80f],Passengers:[`+
			`{id:"minecraft:pig",NoAI:1b,NoGravity:1b,Tags:["pair"],Rotation:[0f,0f]}]}`)
		host.Dispatch(host.Console(), cmd)
	})
	servertest.RunTicks(t, srv, 1)
	got := map[string][2]float32{}
	for _, m := range srv.Mobs() {
		got[m.Kind] = [2]float32{m.Yaw, m.Pitch}
	}
	return got
}
