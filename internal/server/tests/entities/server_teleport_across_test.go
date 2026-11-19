package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestTeleportAcrossCarriesServerOnlyBodies checks that /tp into another
// dimension moves a painting, item frame, leash knot, rocket, bobber and
// area effect cloud there, under the same UUID, leaving none behind.
func TestTeleportAcrossCarriesServerOnlyBodies(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Carter", servertest.OpsListing(t, "Carter"))
	srv.StopTick()

	bodies := []struct {
		kind, selector string
		spawn          func() server.TestTrackedBody
	}{
		{"painting", "minecraft:painting", func() server.TestTrackedBody { return srv.SpawnPaintingEntity(40, 64, 40, 0) }},
		{"frame", "minecraft:item_frame", func() server.TestTrackedBody { return srv.SpawnFrameEntity(42, 64, 40) }},
		{"knot", "minecraft:leash_knot", func() server.TestTrackedBody { return srv.SpawnKnotEntity(44, 64, 40) }},
		{"cloud", "minecraft:area_effect_cloud", func() server.TestTrackedBody { return srv.SpawnCloudEntity(46.5, 64, 40.5) }},
		{"firework", "minecraft:firework_rocket", func() server.TestTrackedBody { return srv.SpawnFireworkEntity(48.5, 64, 40.5) }},
		{"bobber", "minecraft:fishing_bobber", func() server.TestTrackedBody { return srv.SpawnBobberEntity(50.5, 64, 40.5) }},
	}
	for _, b := range bodies {
		body := b.spawn()
		if body.ID == 0 {
			t.Fatalf("%s: the spawn hook came to nothing", b.kind)
		}
		c.ChatCommand("execute in minecraft:the_nether run tp @e[type=" + b.selector + ",limit=1] 8.5 70 8.5")
		c.Settle()
		got := srv.ServerOnlyBodiesInDimension(b.kind, world.NetherName)
		if len(got) != 1 {
			t.Errorf("%s: %d in the nether, want 1", b.kind, len(got))
			continue
		}
		if got[0].UUID != body.UUID {
			t.Errorf("%s: the crossed body's UUID = %v, want %v", b.kind, got[0].UUID, body.UUID)
		}
		if left := srv.ServerOnlyBodiesInDimension(b.kind, world.DimensionName); len(left) != 0 {
			t.Errorf("%s: %d left in the overworld, want none", b.kind, len(left))
		}
	}
}
