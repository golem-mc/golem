package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestPiercingArrowKeepsItsImpactVelocityAtAWall checks a body hit uses the
// incoming flight even when the same step embeds the shaft in a wall.
// vanilla: AbstractArrow.stepMoveAndHit, Projectile.calculateHorizontalHurtKnockbackDirection
func TestPiercingArrowKeepsItsImpactVelocityAtAWall(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		wall, frozen bool
	}{{"flight", false, false}, {"wall", true, false}, {"frozen_wall", true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.CombatTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, c, victimID := servertest.JoinAs(t, cfg, "Victim")
			c.MoveToGround(0.5, 64, 3.5, true)
			c.Settle()
			servertest.HoldTicks(t, srv)
			if tc.wall {
				srv.SetBlock(0, 65, 5, registry.BlockStone.DefaultState())
			}
			srv.ShootPiercingArrow(0.5, 65, 1.5, 0, 0, 4, 1, false)

			var motions [][3]float64
			var health float32
			c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
				r := protocol.NewReader(data)
				switch id {
				case protocol.IDPlaySetEntityMotion:
					if r.ReadVarInt() == victimID {
						vx, vy, vz := r.ReadLpVec3()
						motions = append(motions, [3]float64{vx, vy, vz})
					}
				case protocol.IDPlaySetHealth:
					health = r.ReadF32()
				}
				return false
			})
			if health != 12 {
				t.Errorf("health after one tick = %v, want 12", health)
			}
			vx, vy, vz := srv.PlayerVelocity("Victim")
			if math.Abs(vx) > 0.01 || vy <= 0 || vz < 0.39 {
				t.Errorf("one-tick victim velocity = [%v %v %v], want upward and forward along +Z", vx, vy, vz)
			}
			// Players still travel after the world's single step has ended.
			if tc.frozen {
				srv.SetTickFrozen(true)
			}
			motions = append(motions, victimShoves(c, srv, victimID, 1)...)
			if len(motions) != 1 {
				t.Fatalf("one-tick victim motions = %v, want one shove", motions)
			}
			vx, vy, vz = motions[0][0], motions[0][1], motions[0][2]
			if math.Abs(vx) > 0.01 || vy <= 0 || vz < 0.2 {
				t.Errorf("victim motion = %v, want upward and forward along +Z", motions[0])
			}
		})
	}
}

// TestPiercingArrowKeepsAMobsImpactVelocityAtAWall checks the same flight
// snapshot reaches the mob hurt, which is applied before the player hurts.
func TestPiercingArrowKeepsAMobsImpactVelocityAtAWall(t *testing.T) {
	t.Parallel()
	cfg := servertest.CombatTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Watcher")
	c.MoveToGround(3.5, 64, 3.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	cow := srv.SpawnMobSeeded("minecraft:cow", 0.5, 64, 3.5, 1)
	srv.SetMobNoAI(cow)
	srv.SetBlock(0, 65, 5, registry.BlockStone.DefaultState())
	srv.ShootPiercingArrow(0.5, 65, 1.5, 0, 0, 4, 1, false)
	c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })
	if health, ok := srv.MobHealth(cow); !ok || health != 2 {
		t.Errorf("cow health = %v, present = %v, want 2 and true", health, ok)
	}
	vx, vy, vz, ok := srv.MobVelocity(cow)
	if !ok || math.Abs(vx) > 0.01 || vy <= 0 || vz < 0.39 {
		t.Errorf("cow motion = [%v %v %v], present = %v, want upward and forward along +Z", vx, vy, vz, ok)
	}
}
