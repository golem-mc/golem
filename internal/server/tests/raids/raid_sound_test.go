package raids

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Illusioner.getCelebrateSound, Raider.RaiderCelebration
func TestAnIllusionerCelebratesALostRaidWithItsAmbientVoice(t *testing.T) {
	t.Parallel()
	srv, c, bell := bareBell(t, rules.DifficultyNormal)
	villager := spawnVillager(t, srv, bell)
	srv.SetPlayerGameMode("Hero", protocol.GameModeCreative)
	startRaid(t, c, srv, 0)
	run(c, srv, 330)
	illusioner := srv.SpawnMobSeeded("minecraft:illusioner", float64(bell[0])+4.5, float64(bell[1]), float64(bell[2])+0.5, 1)
	srv.SetMobSilent(illusioner, true)
	servertest.UntilTicks(t, srv, 40, "the illusioner joins the raid", func() bool {
		in, _ := srv.MobRaid(illusioner)
		return in
	})
	run(c, srv, 1)
	srv.KillMob(villager)
	srv.SetBlock(bell[0], bell[1], bell[2], registry.MustState("minecraft:air", nil))
	celebrating := false
	c.ReadTicks(srv, 400, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != illusioner {
			return false
		}
		entries, ok := servertest.ReadMetadata(r)
		if flag := servertest.MetaOf(entries, protocol.MetadataIndexRaiderCelebrating); ok && flag != nil && flag.FlagVal {
			celebrating = true
		}
		return celebrating
	})
	if !celebrating {
		t.Fatal("the client received no illusioner celebration flag")
	}
	if raids := srv.Raids(); len(raids) != 1 || raids[0].Status != "loss" {
		t.Fatalf("raids = %+v, want one lost raid", raids)
	}

	// Hurt resets the idle voice's timer to -80. Only a celebration can
	// produce an ambient voice during the following 70 ticks.
	srv.MoveMob(illusioner, float64(bell[0])+4.5, float64(bell[2])+0.5)
	srv.HurtMobByWorld(illusioner, 1, "minecraft:generic")
	srv.SeedMob(illusioner, 3)
	srv.SetMobSilent(illusioner, false)
	voices := 0
	c.ReadTicks(srv, 70, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySoundEffect {
			return false
		}
		sound := servertest.DecodePlaySoundEffect(t, data)
		if sound.Name == "minecraft:entity.illusioner.celebrate" {
			t.Error("illusioner celebration sent an unknown inline sound")
		}
		if sound.RegistryID != int32(registry.SoundEntityIllusionerAmbient)-1 {
			return false
		}
		voices++
		if sound.Category != protocol.SoundCategoryHostile || sound.Volume != 1 || sound.Pitch < 0.8 || sound.Pitch > 1.2 {
			t.Errorf("celebration category/volume/pitch = %d/%g/%g, want hostile/1/[0.8, 1.2]", sound.Category, sound.Volume, sound.Pitch)
		}
		return false
	})
	if voices == 0 {
		t.Error("no registered illusioner ambient voice during the celebration")
	}
}
