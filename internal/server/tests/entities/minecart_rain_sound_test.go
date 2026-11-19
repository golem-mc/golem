package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Entity.applyEffectsFromBlocks, Entity.playEntityOnFireExtinguishedSound, AbstractMinecart.tick
func TestMinecartRainExtinguishFeedback(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Listener", servertest.OpsListing(t, "Listener"))
	servertest.HoldTicks(t, srv)
	// Stay within earshot, above terrain that could shelter the cart from rain.
	c.ChatCommand("tp @s 8.5 150 4.5")
	c.Settle()
	if !srv.SetBlock(4, 150, 4, registry.BlockLava.DefaultState()) {
		t.Fatal("could not place lava")
	}
	c.ChatCommand("summon minecraft:minecart 4.5 150.5 4.5 {Invulnerable:1b,NoGravity:1b}")
	c.AwaitSpawn(protocol.EntityTypeMinecart)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	target := "@e[type=minecraft:minecart,limit=1]"
	if got := awaitEntityDataCompound(t, srv, c, target, "Fire"); got != "300s" {
		t.Fatalf("dry lava Fire = %s, want 300s", got)
	}
	c.Settle()
	srv.SetWeatherState(true, false, 100000, 100000, 0)
	for range 21 {
		srv.TickWeather()
	}
	for _, step := range []struct {
		name   string
		ticks  int
		sounds int
		fire   string
		remove bool
	}{
		{"rainy lava", 4, 4, "300s", false},
		{"lava removed", 1, 1, "0s", true},
		{"already extinguished", 4, 0, "0s", false},
	} {
		t.Run(step.name, func(t *testing.T) {
			if step.remove && !srv.SetBlock(4, 150, 4, 0) {
				t.Fatal("could not remove lava")
			}
			heard, _, _ := servertest.CollectSoundsTicks(c, srv, step.ticks)
			count := 0
			for _, sound := range heard {
				if sound.Name != "minecraft:entity.generic.extinguish_fire" {
					continue
				}
				count++
				if sound.Category != protocol.SoundCategoryNeutral || sound.Volume != 0.7 || sound.Pitch < 1.2 || sound.Pitch > 2 {
					t.Errorf("extinguish sound = %+v, want neutral, volume 0.7, pitch 1.2–2", sound)
				}
			}
			if count != step.sounds {
				t.Errorf("extinguish sounds over %d ticks = %d, want %d: %+v", step.ticks, count, step.sounds, heard)
			}
			if got := awaitEntityDataCompound(t, srv, c, target, "Fire"); got != step.fire {
				t.Errorf("Fire = %s, want %s", got, step.fire)
			}
		})
	}
}
