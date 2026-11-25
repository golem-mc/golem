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
	servertest.HoldTicks(t, srv)
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
// target's live angles as both From and the resolved To, so a position-only
// redirect keeps them, for a relative turn and a coordinate-only command alike.
// vanilla: TeleportCommand.performTeleport
func TestPluginTeleportEventSeesLiveAnglesOfLaterTarget(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, cmd string
		chickenTo *[2]float32
		from, to  [2]float32
		chicken   [2]float32
		pig       [2]float32
	}{
		{name: "relative", cmd: "5.5 64 5.5 ~-170 ~120",
			from: [2]float32{20, -90}, to: [2]float32{190, -90}, chicken: [2]float32{190, -90}, pig: [2]float32{190, -90}},
		{name: "coordinates", cmd: "5.5 64 5.5", chickenTo: &[2]float32{90, 25},
			from: [2]float32{-80, 90}, to: [2]float32{-80, 90}, chicken: [2]float32{90, 25}, pig: [2]float32{-80, 90}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var from, to [2]float32
			got := runRotationScenario(t, func(e *plugin.EntityTeleportEvent) {
				if e.Entity.Type() != registry.EntityTypePig {
					if tc.chickenTo != nil {
						e.To.Yaw, e.To.Pitch = tc.chickenTo[0], tc.chickenTo[1]
					}
					return
				}
				from, to = [2]float32{e.From.Yaw, e.From.Pitch}, [2]float32{e.To.Yaw, e.To.Pitch}
				e.To.Pos.X++
			}, "execute positioned 3.5 64 3.5 rotated 0 0 run tp @e[tag=pair,sort=nearest] "+tc.cmd)
			if from != tc.from || to != tc.to {
				t.Errorf("pig event from %v to %v; want %v to %v", from, to, tc.from, tc.to)
			}
			if c, p := got["minecraft:chicken"], got["minecraft:pig"]; c != tc.chicken || p != tc.pig {
				t.Fatalf("chicken %v, pig %v; want %v and %v", c, p, tc.chicken, tc.pig)
			}
		})
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
	servertest.HoldTicks(t, srv)
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
