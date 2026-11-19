package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

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
