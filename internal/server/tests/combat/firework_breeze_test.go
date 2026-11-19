package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Projectile.hitTargetOrDeflectSelf, Breeze.deflection
func TestACrossbowRocketIsDeflectedByABreeze(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Launcher", servertest.OpsListing(t, "Launcher"), func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:breeze 8.5 64 4.5 {NoAI:1b}`)
	breeze := c.AwaitSpawn(protocol.EntityTypeBreeze).EntityID
	rocket := srv.SpawnFireworkAtAngle(4.5, 65, 4.5, 1, 0, 0, 100)

	deflected, burst := false, false
	c.ReadTicks(srv, 8, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayEntitySoundEffect:
			deflected = deflected || servertest.SoundIs(data, "minecraft:entity.breeze.deflect")
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			if r.ReadI32() == rocket.ID && r.ReadU8() == protocol.EntityStatusFireworkBurst {
				burst = true
			}
		}
		return false
	})
	if !deflected {
		t.Error("no entity.breeze.deflect within 8 ticks")
	}
	if burst {
		t.Error("the rocket burst on the breeze")
	}
	if hp, ok := srv.MobHealth(breeze); !ok || hp != 30 {
		t.Errorf("the breeze's health = %v (alive %v), want 30", hp, ok)
	}
}
