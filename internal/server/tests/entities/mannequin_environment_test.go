package entities

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func TestMannequinLivingEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name, setup, tag, command, path, want string
		ticks                                 int
	}{
		{name: "poison", command: "effect give @e[type=mannequin] poison 10 1", path: "Health", want: "12.0f", ticks: 100},
		{name: "lava", setup: "setblock 3 64 3 lava", path: "Health", want: "12.0f", ticks: 20},
		{name: "burn", tag: "Fire:100s", path: "Health", want: "19.0f", ticks: 1},
		{name: "air", setup: "fill 3 64 3 3 66 3 water", path: "Air", want: "200s", ticks: 100},
		{name: "drown", setup: "fill 3 64 3 3 66 3 water", path: "Health", want: "18.0f", ticks: 320},
		{name: "water-breathing", setup: "fill 3 64 3 3 66 3 water", command: "effect give @e[type=mannequin] water_breathing 60", path: "Air", want: "300s", ticks: 1},
		{name: "fire-resistance", setup: "setblock 3 64 3 lava", command: "effect give @e[type=mannequin] fire_resistance 60", path: "Health", want: "20.0f", ticks: 1},
		{name: "wither", command: "effect give @e[type=mannequin] wither 10 1", path: "Health", want: "17.0f", ticks: 60},
		{name: "regeneration", tag: "Health:10f", command: "effect give @e[type=mannequin] regeneration 10", path: "Health", want: "12.0f", ticks: 100},
		{name: "instant-health", tag: "Health:10f", command: "effect give @e[type=mannequin] instant_health", path: "Health", want: "14.0f", ticks: 1},
		{name: "instant-damage", command: "effect give @e[type=mannequin] instant_damage", path: "Health", want: "14.0f", ticks: 1},
		{name: "absorption", tag: "Fire:100s", command: "effect give @e[type=mannequin] absorption 10", path: "Health", want: "20.0f", ticks: 1},
		{name: "resistance", tag: "Fire:100s", command: "effect give @e[type=mannequin] resistance 10 4", path: "Health", want: "20.0f", ticks: 1},
		{name: "water-extinguishes", setup: "fill 3 64 3 3 66 3 water", tag: "Fire:100s", path: "Fire", want: "0s", ticks: 1},
		{name: "freeze", setup: "fill 3 64 3 3 65 3 powder_snow", path: "Health", want: "19.0f", ticks: 160},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := joinMannequinOp(t, "Environment")
			if tc.setup != "" {
				c.ChatCommand(tc.setup)
			}
			tag := "{immovable:1b"
			if tc.tag != "" {
				tag += "," + tc.tag
			}
			tag += "}"
			c.ChatCommand("summon mannequin 3.5 64 3.5 " + tag)
			awaitMannequin(t, c, srv)
			if tc.command != "" {
				c.ChatCommand(tc.command)
			}
			c.Settle()
			servertest.RunTicks(t, srv, tc.ticks)
			if got := dataOfMannequin(t, c, srv, tc.path); got != tc.want {
				t.Errorf("after %d ticks %s = %s, want %s", tc.ticks, tc.path, got, tc.want)
			}
		})
	}
}

func TestMannequinEffectMetadataAndSavedState(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "EffectWatcher")
	c.ChatCommand("summon mannequin 3.5 64 3.5 {immovable:1b}")
	id := awaitMannequin(t, c, srv).id
	given := giveMannequin(t, c, srv, "effect give @e[type=mannequin] poison 10 1", id)
	if !slices.Equal(given.Particles[id], servertest.PoisonParticles) {
		t.Fatalf("poison particles = %v", given.Particles[id])
	}
	servertest.RunTicks(t, srv, 99)
	saved := dataOfMannequin(t, c, srv, "active_effects")
	tag, err := nbt.ParseSNBT("{active_effects:" + saved + "}")
	if err != nil {
		t.Fatal(err)
	}
	list, _ := nbt.Get[nbt.List](tag, "active_effects")
	effects := effect.Decode(list)
	if active := effects.All(); len(active) != 1 || active[0].Duration != 100 || active[0].Amplifier != 1 {
		t.Fatalf("saved poison = %s", saved)
	}
	cleared := giveMannequin(t, c, srv, "effect clear @e[type=mannequin] poison", id)
	if particles, ok := cleared.Particles[id]; !ok || len(particles) != 0 {
		t.Fatalf("cleared particles = %v, sent %v", particles, ok)
	}
	c.ChatCommand(`data merge entity @e[type=mannequin,limit=1] {active_effects:[{id:"minecraft:poison",duration:200,amplifier:1b}],Health:20f}`)
	c.Settle()
	servertest.RunTicks(t, srv, 100)
	if got := dataOfMannequin(t, c, srv, "Health"); got != "12.0f" {
		t.Fatalf("reloaded poison left Health %s", got)
	}
}

// giveMannequin is Client.Give with the tick that sends a mannequin's synched
// delta after its movement.
func giveMannequin(t *testing.T, c *servertest.Client, srv *server.Server, command string, id int32) *servertest.EffectsWitness {
	t.Helper()
	w := servertest.NewEffectsWitness()
	w.Hears(id)
	c.ChatCommand(command)
	c.ReadTicks(srv, 1, func(pid int32, data []byte) bool {
		w.Note(c, pid, data)
		return false
	})
	return w
}
