package plugins_test

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func fireAt(srv interface{ BlockAt(x, y, z int32) uint16 }, p plugin.Vec3) bool {
	name, _, _ := registry.BlockStateName(srv.BlockAt(int32(math.Floor(p.X)), int32(math.Floor(p.Y)), int32(math.Floor(p.Z))))
	return name == "minecraft:fire"
}

// vanilla: LightningBolt.setVisualOnly, LightningBolt.tick
func TestPluginStrikeLightning(t *testing.T) {
	srv, h, run, _ := worldEffectsPlugin(t)
	srv.SetDifficulty(3)
	pp := srv.PlayerPosition("Effects")
	visual := plugin.Vec3{X: pp.X + 6.5, Y: pp.Y, Z: pp.Z + .5}
	harm := plugin.Vec3{X: pp.X - 6.5, Y: pp.Y, Z: pp.Z + .5}
	cow := srv.SpawnMob("minecraft:cow", harm.X, harm.Y, harm.Z)
	before, _ := srv.MobHealth(cow)

	var vis, bolt plugin.Entity
	run(func() {
		vis = h.Overworld().StrikeLightning(visual, true)
		bolt = h.Overworld().StrikeLightning(harm, false)
	})
	for _, e := range []plugin.Entity{vis, bolt} {
		if e == nil || e.Type() != registry.EntityTypeLightningBolt || !e.IsValid() {
			t.Fatalf("bolt handle = %v", e)
		}
	}
	srv.TickBolts(4)
	if fireAt(srv, visual) {
		t.Error("a visual-only bolt lit a fire")
	}
	if !fireAt(srv, harm) {
		t.Error("a harmful bolt on hard lit no fire")
	}
	if after, _ := srv.MobHealth(cow); after >= before {
		t.Errorf("cow health %v -> %v, want hurt", before, after)
	}
}
