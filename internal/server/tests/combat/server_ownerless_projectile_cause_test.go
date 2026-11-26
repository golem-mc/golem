package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAnOwnerlessProjectileIsBothSourceIDsOfTheDamageEventOnAMob checks that
// a cow hurt by an arrow or trident with no shooter is told the projectile
// as the cause and as the direct entity.
// vanilla: AbstractArrow.onHitEntity, ThrownTrident.onHitEntity
func TestAnOwnerlessProjectileIsBothSourceIDsOfTheDamageEventOnAMob(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		shoot func(*server.Server) int32
	}{
		{"arrow", func(s *server.Server) int32 { return s.ShootArrow(10.5, 65, 0.5, -2, 0, 0) }},
		{"trident", func(s *server.Server) int32 { return s.ShootTrident(10.5, 65, 0.5, -2, 0, 0) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Watcher")
			c.MoveToGround(0.5, 64, 8.5, true)
			c.Settle()
			cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
			c.ExpectMobSpawn(protocol.EntityTypeCow)
			srv.SetMobNoAI(cow)
			srv.SetMobHealth(cow, 100)
			proj := tc.shoot(srv)

			found := false
			c.ReadTicks(srv, 60, func(pid int32, data []byte) bool {
				if pid != protocol.IDPlayDamageEvent {
					return false
				}
				r := protocol.NewReader(data)
				if r.ReadVarInt() != cow {
					return false
				}
				r.ReadVarInt() // damage type
				cause, direct := r.ReadVarInt(), r.ReadVarInt()
				if cause != proj+1 || direct != proj+1 {
					t.Errorf("cause, direct = %d, %d, want both %d", cause, direct, proj+1)
				}
				found = true
				return true
			})
			if !found {
				t.Fatal("no damage event for the cow")
			}
		})
	}
}
