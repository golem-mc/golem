package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// vanilla: AbstractArrow.stepMoveAndHit, onHitEntity, onHitBlock
func TestAPiercingArrowEmbedsAfterHittingABodyInTheSameTick(t *testing.T) {
	for _, victim := range []string{"cow", "player"} {
		t.Run(victim, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.CombatTestConfig()
			cfg.ViewDistance = config.MinDistance
			cfg.SimulationDistance = config.MinDistance
			srv, c := servertest.Join(t, cfg, "Tester",
				servertest.OpsListing(t, "Tester"), func(s *server.Server) {
					servertest.FreezeUntilHeld(t, s)
				})
			servertest.HoldTicks(t, srv)
			command := func(cmd, key string) {
				c.ChatCommand(cmd)
				servertest.AwaitSystemChatSaying(t, c, key)
			}
			command("fill -1 100 0 2 100 6 minecraft:stone", "commands.fill.success")
			command("fill -1 101 5 2 103 5 minecraft:stone", "commands.fill.success")
			var cowID int32
			if victim == "cow" {
				command("tp @s 2.5 101 1.5", "commands.teleport.success.location.single")
				cowID = srv.SpawnMob("minecraft:cow", 0.5, 101, 3.5)
				srv.SetMobNoAI(cowID)
			} else {
				command("tp @s 0.5 101 3.5", "commands.teleport.success.location.single")
			}
			command(`summon minecraft:arrow 0.5 102 1.5 {NoGravity:1b,Motion:[0d,0d,4d],PierceLevel:1b}`, "commands.summon.success")
			arrows := srv.ArrowsInDimension(world.DimensionName)
			if len(arrows) != 1 {
				t.Fatalf("arrows before step = %v, want one", arrows)
			}
			arrowID := arrows[0].ID
			var embedded, removed bool
			c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
				if id == protocol.IDPlayRemoveEntities {
					r := protocol.NewReader(data)
					for range r.ReadVarInt() {
						if r.ReadVarInt() == arrowID {
							removed = true
						}
					}
				}
				if id != protocol.IDPlayEntityMetadata {
					return false
				}
				r := protocol.NewReader(data)
				if r.ReadVarInt() != arrowID {
					return false
				}
				entries, ok := servertest.ReadMetadata(r)
				if !ok {
					t.Fatal("unreadable arrow metadata")
				}
				if entry := servertest.MetaOf(entries, protocol.MetadataIndexArrowInGround); entry != nil {
					embedded = entry.FlagVal
				}
				return false
			})
			if victim == "cow" {
				if hp, ok := srv.MobHealth(cowID); !ok || hp != 2 {
					t.Errorf("cow health = %v (exists %v), want 2", hp, ok)
				}
			} else if hp := srv.PlayerHealth("Tester"); hp != 12 {
				t.Errorf("player health = %v, want 12", hp)
			}
			if !embedded {
				t.Error("client did not receive embedded arrow metadata")
			}
			if removed {
				t.Error("client was told to remove the piercing arrow")
			}
			arrows = srv.ArrowsInDimension(world.DimensionName)
			if len(arrows) != 1 || !srv.ArrowAlive(arrowID) {
				t.Fatalf("arrows after one tick = %v, want the surviving embedded arrow", arrows)
			}
			a := arrows[0]
			if a.ID != arrowID || a.X != 0.5 || a.Y != 102 || math.Abs(a.Z-4.95) > 1e-7 || a.VX != 0 || a.VY != 0 || a.VZ != 0 || a.PierceLevel != 0 {
				t.Errorf("embedded arrow = %+v, want original arrow at (0.5,102,4.95), stopped with piercing cleared", a)
			}
		})
	}
}
