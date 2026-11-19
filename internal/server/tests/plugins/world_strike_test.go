package plugins_test

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
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

// vanilla: ServerLevel.explode, Explosion.getIndirectSourceEntity, DamageSources.explosion
func TestPluginExplodeAttributesToSource(t *testing.T) {
	cfg := servertest.SurvivalTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, h, run := worldPlugin(t, cfg)
	servertest.FreezeUntilHeld(t, srv)
	servertest.ReleaseTicks(t, srv)
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("Effects")
	servertest.HoldTicks(t, srv)
	srv.SetTickFrozen(true)
	srv.SetDifficulty(3)
	pp := srv.PlayerPosition("Effects")
	pos := plugin.Vec3{X: pp.X, Y: pp.Y, Z: pp.Z}
	var creeper plugin.Entity
	run(func() {
		var err error
		creeper, err = h.Overworld().Spawn(registry.EntityTypeCreeper, plugin.Location{World: h.Overworld(), Pos: plugin.Vec3{X: pos.X + 20, Y: pos.Y, Z: pos.Z}}, nil)
		if err != nil {
			t.Fatal(err)
		}
	})
	message := protocol.Text{
		Translate: "death.attack.explosion.player",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Effects"), servertest.EntityDisplayComponent("minecraft:creeper", "entity.minecraft.creeper", uuid.UUID(creeper.UUID()))},
	}
	var want protocol.Buffer
	want.PutText(message)
	want.PutBool(false)
	run(func() { h.Overworld().Explode(pos, 10, false, plugin.ExplosionKeep, creeper) })
	died := false
	c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySystemChat {
			died = string(data) == string(want.Bytes())
		}
		return died
	})
	if !died {
		t.Error("no \"was blown up by Creeper\" death message")
	}
}
