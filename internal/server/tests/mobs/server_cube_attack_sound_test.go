package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: AbstractCubeMob.push, AbstractCubeMob.dealDamage
func TestCubeContactHurtsIronGolemAndPlaysAttackSound(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, summon string }{
		{"slime", "summon minecraft:slime 1.5 64 0.5 {Size:1}"},
		{"tiny magma cube", "summon minecraft:magma_cube 1.25 64 0.5 {Size:0}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := servertest.MobTestConfig()
			cfg.GameMode = "creative"
			cfg.ViewDistance = config.MinDistance
			cfg.SimulationDistance = config.MinDistance
			srv, c := servertest.Join(t, cfg, "Watcher")
			c.MoveToGround(4.5, 64, 0.5, true)
			c.Settle()
			servertest.HoldTicks(t, srv)
			srv.RunLine("summon minecraft:iron_golem 0.5 64 0.5 {NoAI:1b}")
			golem := c.ExpectMobSpawn(protocol.EntityTypeIronGolem)
			fullHealth := golem.Health
			srv.RunLine(tc.summon)

			var heard bool
			c.ReadTicks(srv, 5, func(id int32, data []byte) bool {
				switch id {
				case protocol.IDPlayEntityMetadata:
					if protocol.NewReader(data).ReadVarInt() == golem.EntityID {
						c.ReadMobMetadataInto(&golem, data)
					}
				case protocol.IDPlaySoundEffect:
					name, category, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
					if name == "minecraft:entity.slime.attack" {
						heard = true
						if category != protocol.SoundCategoryHostile || volume != 1 || pitch < 0.8 || pitch > 1.2 {
							t.Errorf("attack sound category, volume, pitch = %d, %g, %g, want hostile, 1, 0.8–1.2", category, volume, pitch)
						}
					}
				}
				return heard && golem.Health < fullHealth
			})
			if golem.Health >= fullHealth {
				t.Errorf("iron golem health = %g, want below %g after contact", golem.Health, fullHealth)
			}
			if !heard {
				t.Error("no slime attack sound after touching the iron golem")
			}
		})
	}
}
