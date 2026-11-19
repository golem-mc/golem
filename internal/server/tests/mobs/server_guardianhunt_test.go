package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// layLake floods the lake the wandering guardian tests swim in.
func layLake(t *testing.T, srv *server.Server) {
	t.Helper()
	water := registry.MustState("minecraft:water", map[string]string{"level": "0"})
	for x := int32(lakeXLo); x <= lakeXHi; x++ {
		for z := int32(lakeZLo); z <= pondZHi; z++ {
			for y := int32(60); y <= pondY+1; y++ {
				if srv.SetBlock(x, y, z, water); srv.BlockAt(x, y, z) != water {
					t.Fatalf("the lake's chunk at (%d, %d) was not loaded", x, z)
				}
			}
		}
	}
}

// TestAGuardianHuntsASquidWithItsBeam: with no player to hunt, a guardian
// takes the squid in the lake as its target, tells the client so, and the
// beam's 1 magic damage and the 5 left of its blow of 6 take the squid from
// 10 to 4.
// vanilla: Guardian.GuardianAttackSelector, GuardianAttackGoal.tick
func TestAGuardianHuntsASquidWithItsBeam(t *testing.T) {
	t.Parallel()
	srv, c, _ := guardianTests(t, rules.DifficultyNormal)
	c.ChatCommand("gamemode creative")
	c.Settle()
	layLake(t, srv)
	guardian := spawnGuardian(t, srv, c, "minecraft:guardian", pondCentreX, 62, -20.5)
	squid := summonSquid(t, c, "minecraft:squid", protocol.EntityTypeSquid, pondCentreX, -26.5)
	srv.SetMobNoAI(squid)
	w := newBeamWatch(t, srv, c, guardian, "minecraft:guardian")
	for range 700 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(pid int32, data []byte) {
			if pid == protocol.IDPlayEntityMetadata && protocol.NewReader(data).ReadVarInt() == guardian {
				c.ReadMobMetadataInto(&w.meta, data)
				if w.meta.GuardianTarget != 0 {
					w.targetedAt = 1
					if w.meta.GuardianTarget != squid {
						t.Fatalf("the beam target is entity %d, want the squid %d", w.meta.GuardianTarget, squid)
					}
				}
			}
		})
		if hp, _ := srv.MobHealth(squid); hp < 10 {
			if hp != 4 {
				t.Errorf("the squid has %v health after the beam, want 4", hp)
			}
			if w.targetedAt != 1 {
				t.Error("the beam hurt the squid without the client being told its target")
			}
			return
		}
	}
	t.Fatal("the guardian never hurt the squid in 700 ticks")
}

// TestAGuardianPricksAMobThatStrikesIt: a mob's blow on a guardian with its
// spikes out costs the attacker 2, and none while the guardian swims to a
// point.
// vanilla: Guardian.hurtServer
func TestAGuardianPricksAMobThatStrikesIt(t *testing.T) {
	t.Parallel()
	prick := func(t *testing.T, still bool) (attackerLost, guardianLost, pushed float64) {
		srv, c, _ := guardianTests(t, rules.DifficultyNormal)
		c.ChatCommand("gamemode creative")
		c.Settle()
		layLake(t, srv)
		guardian := spawnGuardian(t, srv, c, "minecraft:guardian", pondCentreX, 62, -20.5)
		attacker := spawnGuardian(t, srv, c, "minecraft:pig", pondCentreX, 62, -22.5)
		srv.SetMobNoAI(attacker)
		if still {
			srv.SetMobNoAI(guardian)
		} else {
			meta := servertest.SpawnedMob{EntityID: guardian, TypeID: guardianType(t, "minecraft:guardian")}
			for tick := 0; !meta.GuardianMoving; tick++ {
				if tick == 400 {
					t.Fatal("the guardian never swam anywhere in 400 ticks")
				}
				srv.TickMobs(1)
				c.SettleAndCollect(func(pid int32, data []byte) {
					if pid == protocol.IDPlayEntityMetadata && protocol.NewReader(data).ReadVarInt() == guardian {
						c.ReadMobMetadataInto(&meta, data)
					}
				})
			}
		}
		before, _ := srv.MobHealth(attacker)
		gBefore, _ := srv.MobHealth(guardian)
		if !srv.StrikeMobWith(attacker, guardian, 15) {
			t.Fatal("the attacker or the guardian was not found")
		}
		after, _ := srv.MobHealth(attacker)
		gAfter, _ := srv.MobHealth(guardian)
		vx, _, vz, _ := srv.MobVelocity(attacker)
		return before - after, gBefore - gAfter, math.Hypot(vx, vz)
	}
	t.Run("spikes out", func(t *testing.T) {
		attackerLost, guardianLost, pushed := prick(t, true)
		if pushed == 0 {
			t.Error("the thorns did not knock the attacker back")
		}
		if attackerLost != 2 {
			t.Errorf("striking a still guardian cost the attacker %v health, want 2", attackerLost)
		}
		if guardianLost != 15 {
			t.Errorf("the blow cost the guardian %v health, want 15", guardianLost)
		}
	})
	t.Run("swimming", func(t *testing.T) {
		if attackerLost, _, _ := prick(t, false); attackerLost != 0 {
			t.Errorf("striking a swimming guardian cost the attacker %v health, want none", attackerLost)
		}
	})
}

// TestAnElderGuardianSwimsBackHome: an elder outside its home swims toward
// it until it is inside the radius.
// vanilla: MoveTowardsRestrictionGoal
func TestAnElderGuardianSwimsBackHome(t *testing.T) {
	t.Parallel()
	srv, c, _ := guardianTests(t, rules.DifficultyNormal)
	c.ChatCommand("gamemode creative")
	c.Settle()
	layLake(t, srv)
	id := spawnGuardian(t, srv, c, "minecraft:elder_guardian", pondCentreX, 62, -34.5)
	if !srv.RestrictMob(id, 8, 62, -12, 8) {
		t.Fatal("the elder was not found")
	}
	start, _ := fishAt(srv, id)
	for range 1500 {
		srv.TickMobs(1)
		c.Settle()
		m, _ := fishAt(srv, id)
		if math.Hypot(m.X-8.5, m.Z+11.5) < 8 {
			return
		}
	}
	m, _ := fishAt(srv, id)
	t.Fatalf("the elder swam from (%.1f, %.1f) to (%.1f, %.1f), never inside the radius 8 of (8, -12)", start.X, start.Z, m.X, m.Z)
}
