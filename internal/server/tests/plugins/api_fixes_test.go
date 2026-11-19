package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Entity.teleportTo
func TestPluginTeleportCauseAndVerdict(t *testing.T) {
	var causes []plugin.TeleportCause
	cancel := false
	srv, host, _ := blockScene(t, "minecraft:stone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityTeleportEvent) {
			causes = append(causes, e.Cause)
			e.SetCancelled(cancel)
		})
	})
	p := host.Players()[0]
	to := plugin.Location{World: p.Location().World, Pos: plugin.Vec3{X: 3, Y: 64, Z: 0.5}}
	var ok bool
	onTick(t, srv, host, func() { ok = p.Teleport(to, plugin.TeleportEnderPearl) })
	if !ok || len(causes) != 1 || causes[0] != plugin.TeleportEnderPearl {
		t.Fatalf("ok %v causes %v", ok, causes)
	}
	cancel = true
	onTick(t, srv, host, func() { ok = p.Teleport(to, plugin.TeleportPlugin) })
	if ok || len(causes) != 2 || causes[1] != plugin.TeleportPlugin {
		t.Fatalf("cancelled: ok %v causes %v", ok, causes)
	}
}

// vanilla: PrimedTnt.explode
func TestPluginEntityExplodeCarriesExploder(t *testing.T) {
	var got *plugin.EntityExplodeEvent
	srv, _ := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityExplodeEvent) { got = e })
	})
	srv.SpawnTNT(4.5, 62, 4.5, 1)
	for range 3 {
		srv.RunTick()
	}
	if got == nil {
		t.Fatal("no EntityExplodeEvent")
	}
	if got.Type != registry.EntityTypeTnt || got.Location.World == nil || got.Location.Pos != got.Pos {
		t.Errorf("type %v location %+v", got.Type, got.Location)
	}
}

func TestPluginCommandAliasRepeatingName(t *testing.T) {
	var err error
	servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "dup", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		err = s.Commands().Register(plugin.Command{Name: "tpoffline", Aliases: []string{"tpoffline", "tpo", "tpo"}, Children: []plugin.Node{{Literal: "x"}}})
		return nil
	}}}})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
}
