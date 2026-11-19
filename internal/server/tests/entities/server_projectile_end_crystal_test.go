package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// crystalFlight stands a crystal at (5.5, 70, 0.5), starts a projectile with
// start 4 blocks west of it and flying east, and reports whether the crystal
// was removed, whether an Explosion arrived, and how many crystals stand.
func crystalFlight(t *testing.T, ticks int, invulnerable bool, start func(srv *server.Server)) (removed, exploded bool, left int, srv *server.Server) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Watcher")
	c.MoveToGrounded(0.5, 64, 0.5, 0, 0, true)
	var id int32
	if invulnerable {
		id = srv.SpawnInvulnerableEndCrystal(5.5, 70, 0.5)
	} else {
		id = srv.SpawnEndCrystal(5.5, 70, 0.5)
	}
	c.Settle()
	servertest.HoldTicks(t, srv)
	start(srv)
	servertest.RunTicks(t, srv, ticks)
	c.SettleAndCollect(func(pid int32, data []byte) {
		switch pid {
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			for range r.ReadVarInt() {
				if r.ReadVarInt() == id {
					removed = true
				}
			}
		case protocol.IDPlayExplosion:
			exploded = true
		}
	})
	return removed, exploded, srv.EndCrystalCount(), srv
}

// TestAnArrowBlowsUpAnEndCrystal shoots an arrow into a crystal.
// vanilla: EndCrystal.hurtServer
func TestAnArrowBlowsUpAnEndCrystal(t *testing.T) {
	t.Parallel()
	// Cross the west face at x=4.5; ending a tick on it is not a hit.
	removed, exploded, left, _ := crystalFlight(t, 1, false, func(srv *server.Server) { srv.ShootArrow(1.5, 70.5, 0.5, 3.25, 0, 0) })
	if !removed || !exploded || left != 0 {
		t.Errorf("removed=%v exploded=%v crystals left=%d, want removed, exploded, none", removed, exploded, left)
	}
}

// TestASnowballBlowsUpAnEndCrystal throws a snowball, which hurts for 0.
// vanilla: Snowball.onHitEntity
func TestASnowballBlowsUpAnEndCrystal(t *testing.T) {
	t.Parallel()
	removed, exploded, left, _ := crystalFlight(t, 3, false, func(srv *server.Server) {
		srv.ThrowThrown(entity.ThrownSnowball, 1.5, 70.5, 0.5, -90, 0, 0, 1)
	})
	if !removed || !exploded || left != 0 {
		t.Errorf("removed=%v exploded=%v crystals left=%d, want removed, exploded, none", removed, exploded, left)
	}
}

// TestAWindChargePassesThroughAnEndCrystal throws a wind charge past one.
// vanilla: AbstractWindCharge.canHitEntity
func TestAWindChargePassesThroughAnEndCrystal(t *testing.T) {
	t.Parallel()
	removed, exploded, left, _ := crystalFlight(t, 4, false, func(srv *server.Server) {
		srv.ThrowThrown(entity.ThrownWindCharge, 1.5, 70.5, 0.5, -90, 0, 0, 1)
	})
	if removed || exploded || left != 1 {
		t.Errorf("removed=%v exploded=%v crystals left=%d, want the crystal standing", removed, exploded, left)
	}
}

// TestAnArrowIsTurnedAwayByAnInvulnerableEndCrystal shoots an arrow at a
// crystal that refuses the hurt: the crystal stands and the arrow bounces back.
// vanilla: EndCrystal.hurtServer, AbstractArrow.onHitEntity
func TestAnArrowIsTurnedAwayByAnInvulnerableEndCrystal(t *testing.T) {
	t.Parallel()
	removed, exploded, left, srv := crystalFlight(t, 1, true, func(srv *server.Server) { srv.ShootArrow(1.5, 70.5, 0.5, 3.25, 0, 0) })
	if removed || exploded || left != 1 {
		t.Errorf("removed=%v exploded=%v crystals left=%d, want the crystal standing", removed, exploded, left)
	}
	arrows := srv.ArrowsInDimension("minecraft:overworld")
	if len(arrows) != 1 || arrows[0].VX >= 0 || arrows[0].X >= 5.5 {
		t.Errorf("arrows = %+v, want one bounced back west of the crystal", arrows)
	}
}

// vanilla: AABB.clipPoint, ProjectileUtil.getManyEntityHitResult, computeMargin
func TestAnArrowEndingOnAnEndCrystalsFacePassesThrough(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		invulnerable bool
	}{
		{"vulnerable", false},
		{"invulnerable", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The first tick ends on the west face at x=4.5 and the next
			// starts there. Both clips exclude the face; neither tick has
			// an age-grown margin to catch the miss.
			removed, exploded, left, srv := crystalFlight(t, 2, tc.invulnerable, func(srv *server.Server) {
				srv.ShootArrow(1.5, 70.5, 0.5, 3, 0, 0)
			})
			if removed || exploded || left != 1 {
				t.Errorf("removed=%v exploded=%v crystals left=%d, want the crystal standing", removed, exploded, left)
			}
			arrows := srv.ArrowsInDimension("minecraft:overworld")
			if len(arrows) != 1 || arrows[0].VX <= 0 || arrows[0].X <= 6.5 {
				t.Errorf("arrows = %+v, want one continuing east beyond the crystal", arrows)
			}
		})
	}
}
