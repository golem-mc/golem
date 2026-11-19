package explosions

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// A primed TNT discards its team before exploding; a TNT minecart explodes
// while still on its team, sparing teammates when friendly fire is off.
// vanilla: PrimedTnt.tick, MinecartTNT.explode, Entity.doTeamsAllowDamage
func TestABlastSparesTheTeamOfItsSource(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"minecraft:tnt", "minecraft:tnt_minecart"} {
		for _, friendlyFire := range []bool{false, true} {
			t.Run(fmt.Sprint(kind, "/friendlyFire=", friendlyFire), func(t *testing.T) {
				t.Parallel()
				srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), "Alice")
				servertest.HoldTicks(t, srv)
				srv.RunLine("team add t")
				srv.RunLine(fmt.Sprint("team modify t friendlyFire ", friendlyFire))
				srv.RunLine("team join t Alice")
				zombie := srv.SpawnMob("minecraft:zombie", 2.5, 64, 2.5)
				zombieUUID, ok := srv.MobUUID(zombie)
				if !ok {
					t.Fatal("SpawnMob came to nothing")
				}
				srv.RunLine("team join t " + zombieUUID)
				srv.RunLine("summon " + kind + " 2.5 64 0.5 {fuse:2,Team:\"t\"}")

				var boom servertest.Explosion
				seen := c.ReadTicksSkipping(srv, 60, tntSkip, func(id int32, data []byte) bool {
					if id == protocol.IDPlayExplosion {
						boom = servertest.ReadExplosion(c, data)
						return true
					}
					return false
				})
				if !seen {
					t.Fatal("the TNT never went up")
				}
				if !boom.Pushed || boom.KX == 0 && boom.KY == 0 && boom.KZ == 0 {
					t.Errorf("Explosion knockback = %v (%v, %v, %v), want a push either way", boom.Pushed, boom.KX, boom.KY, boom.KZ)
				}
				spared := kind == "minecraft:tnt_minecart" && !friendlyFire
				health := srv.PlayerHealth("Alice")
				if spared && health != 20 {
					t.Errorf("health = %v, want 20: the blast spares its own team", health)
				}
				if !spared && health >= 20 {
					t.Errorf("health = %v, want below 20: the source has no team immunity", health)
				}
				zombieHealth, _ := srv.MobHealth(zombie)
				if spared && zombieHealth != 20 {
					t.Errorf("zombie health = %v, want 20: the blast spares its own team", zombieHealth)
				}
				if !spared && zombieHealth >= 20 {
					t.Errorf("zombie health = %v, want below 20: the source has no team immunity", zombieHealth)
				}
			})
		}
	}
}
