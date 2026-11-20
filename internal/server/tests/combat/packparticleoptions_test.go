package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// A pack's spawn_particles effect keeps the options of a dust particle.
// vanilla: SpawnParticlesEffect.CODEC, ParticleTypes.STREAM_CODEC
func TestPackSpawnParticlesDustOptions(t *testing.T) {
	cfg := servertest.MobTestConfig()
	effect := `{"type":"minecraft:spawn_particles","particle":{"type":"minecraft:dust","color":[1.0,0.0,0.0],"scale":1.0},
		"horizontal_position":{"type":"minecraft:entity_position"},"vertical_position":{"type":"minecraft:entity_position"},
		"horizontal_velocity":{},"vertical_velocity":{}}`
	cfg.LevelName = postAttackPack(t, "mainhand", "#minecraft:enchantable/weapon", packPostAttack("attacker", "victim", effect))
	const name = "Striker"
	srv, c, _ := servertest.JoinAs(t, cfg, name)
	c.MoveToGround(.5, 64, .5, true)
	servertest.NightWorld(srv, c)
	zombie := srv.SpawnMobSeeded("minecraft:zombie", 2.5, 64, .5, 4374)
	srv.SetMobNoAI(zombie)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	srv.GiveSlot(name, player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:iron_sword", "pk:targets", 1))
	servertest.HoldTicks(t, srv)
	servertest.AwaitFullSwing(t, srv, name)
	c.Attack(zombie)
	var found bool
	c.ReadTicks(srv, 0, func(packet int32, data []byte) bool {
		if packet != protocol.IDPlayLevelParticles {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != protocol.ParticleDust {
			return false
		}
		color, scale := r.ReadI32(), r.ReadF32()
		if color&0xFFFFFF != 0xFF0000 || scale != 1 {
			t.Errorf("dust colour %#x scale %v, want 0xFF0000 scale 1", color, scale)
		}
		found = true
		return true
	})
	if !found {
		t.Fatal("no dust particle was sent")
	}
}
