package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: LivingEntity.knockback, LivingEntity.travelInAir,
// ServerLevel.tick, MinecraftServer.tickChildren, ServerEntity.sendChanges
func TestZombieHitMotionIsDampedOneTick(t *testing.T) {
	t.Parallel()
	for _, command := range []bool{false, true} {
		name := "melee"
		wantY, wantHorizontal := 0.2752, 0.2184
		if command {
			name = "damage-command"
			wantY, wantHorizontal = 0.3608, 0.4
		}
		t.Run(name, func(t *testing.T) {
			srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Tester",
				servertest.OpsListing(t, "Tester"), func(s *server.Server) {
					servertest.FreezeUntilHeld(t, s)
				})
			servertest.HoldTicks(t, srv)
			c.MoveToGround(0.5, 64, 0.5, true)
			c.Settle()
			servertest.NightWorld(srv, c)
			srv.SetDifficulty(2)
			servertest.RunTicks(t, srv, 2)
			c.Settle()
			c.ChatCommand("summon minecraft:zombie 0.5 64 2.0 {UUID:[I;1,2,3,4]}")
			c.Settle()
			if command {
				c.ChatCommand("damage @s 1 minecraft:mob_attack by 00000001-0000-0002-0000-000300000004")
			}
			if !c.ReadTicks(srv, servertest.HostileTicks, func(id int32, data []byte) bool {
				return id == protocol.IDPlayDamageEvent && protocol.NewReader(data).ReadVarInt() == c.PlayerEntityID
			}) {
				t.Fatal("the zombie never hurt the player")
			}
			x, y, z := readOwnMotion(t, srv, c, c.PlayerEntityID, 2)
			if math.Abs(y-wantY) > 0.003 || math.Abs(math.Hypot(x, z)-wantHorizontal) > 0.003 {
				t.Fatalf("motion = (%.4f, %.4f, %.4f), want lift %.4f and horizontal %.4f", x, y, z, wantY, wantHorizontal)
			}
			// A pending hit is sent once, even if further travel ticks run.
			c.ExpectNoneWithinTicks(srv, 2, servertest.PacketAbout(c.PlayerEntityID, protocol.IDPlaySetEntityMotion))
		})
	}
}
