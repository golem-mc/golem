package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestARabbitHopsAwayFromAPlayer: a rabbit three blocks from a survival
// player flees (RabbitAvoidEntityGoal) by hopping, which plays the jump
// sound and tells entity event 1 for the hop animation.
func TestARabbitHopsAwayFromAPlayer(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Runner", "", 0)
	id := srv.SpawnMob("minecraft:rabbit", 3.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeRabbit)
	var sounded, hopped bool
	c.ReadTicks(srv, 300, func(packetID int32, data []byte) bool {
		switch packetID {
		case protocol.IDPlaySoundEffect:
			name, category, _, _, _, _, _ := servertest.ReadAmbient(t, data)
			if name == "minecraft:entity.rabbit.jump" {
				sounded = true
				if category != protocol.SoundCategoryNeutral {
					t.Errorf("the hop sound played in category %d, want neutral", category)
				}
			}
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			if r.ReadI32() == id && r.ReadU8() == protocol.EntityStatusRabbitJump {
				hopped = true
			}
		}
		return sounded && hopped
	})
	if !sounded || !hopped {
		t.Errorf("hop sound heard %v, entity event 1 seen %v, want both", sounded, hopped)
	}
	servertest.AwaitTicks(t, srv, 80)
	m, _ := servertest.MobByID(srv, id)
	if m.X < 6 {
		t.Errorf("the rabbit is at x=%.2f, want it hopped away from the player at x=0.5", m.X)
	}
}

// TestTheKillerBunnyIsNamedAndBites: the rabbit variant 99 is named
// "Killer Bunny" unless it has a name, does not flee, and its bite plays the
// hostile-category attack sound.
func TestTheKillerBunnyIsNamedAndBites(t *testing.T) {
	t.Parallel()
	const name = "Hunter"
	srv, c := servertest.BreedingPlayer(t, "survival", name, "", 0, servertest.OpsListing(t, name))
	c.ChatCommand("summon minecraft:rabbit 3.5 64 0.5 {RabbitType:99}")
	killer := c.ExpectMobSpawn(protocol.EntityTypeRabbit)
	if killer.RabbitType != 99 {
		t.Fatalf("variant %d, want 99", killer.RabbitType)
	}
	if name, ok := killer.Name.(nbt.Compound); !ok || name["translate"] != nbt.String("entity.minecraft.killer_bunny") {
		t.Errorf("name %v, want the entity.minecraft.killer_bunny translation", killer.Name)
	}
	var bit, hurt bool
	c.ReadTicks(srv, 300, func(packetID int32, data []byte) bool {
		if packetID == protocol.IDPlayDamageEvent {
			hurt = true
		}
		if packetID != protocol.IDPlaySoundEffect {
			return false
		}
		sound, category, _, _, _, _, _ := servertest.ReadAmbient(t, data)
		if sound == "minecraft:entity.rabbit.attack" {
			bit = true
			if !hurt {
				t.Error("the attack sound played before the bite's damage event")
			}
			if category != protocol.SoundCategoryHostile {
				t.Errorf("the attack sound played in category %d, want hostile", category)
			}
		}
		return bit
	})
	if !bit {
		t.Error("the killer bunny never bit the player within 300 ticks")
	}
}

// TestARabbitEatsARipeCarrotWhereMobsMayGrief: a rabbit standing in a
// full-grown carrot crop takes one growth stage off it, and leaves the crop
// alone with mob_griefing off.
func TestARabbitEatsARipeCarrotWhereMobsMayGrief(t *testing.T) {
	t.Parallel()
	for _, grief := range []bool{true, false} {
		t.Run(map[bool]string{true: "griefing", false: "no griefing"}[grief], func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.BreedingPlayer(t, "survival", "Farmer", "", 0)
			srv.SetGameRule(world.RuleMobGriefing.String(), grief)
			// A random tick can grow the eaten crop back to age 7 within the wait.
			srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), 0)
			srv.SetBlock(20, 63, 0, registry.MustState("minecraft:farmland", map[string]string{"moisture": "7"}))
			ripe := registry.MustState("minecraft:carrots", map[string]string{"age": "7"})
			srv.SetBlock(20, 64, 0, ripe)
			srv.SpawnMob("minecraft:rabbit", 20.5, 64, 0.5)
			c.ExpectMobSpawn(protocol.EntityTypeRabbit)
			servertest.AwaitTicks(t, srv, 80)
			want := registry.MustState("minecraft:carrots", map[string]string{"age": "6"})
			if !grief {
				want = ripe
			}
			if got := srv.BlockAt(20, 64, 0); got != want {
				name, props, _ := registry.BlockStateName(got)
				t.Errorf("the crop is %s %s, want the state %d", name, props, want)
			}
		})
	}
}
