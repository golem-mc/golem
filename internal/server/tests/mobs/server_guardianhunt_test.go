package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
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
