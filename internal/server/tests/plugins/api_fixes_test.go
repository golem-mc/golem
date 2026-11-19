package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/plugin"
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
