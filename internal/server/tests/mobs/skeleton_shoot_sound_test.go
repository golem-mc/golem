package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestASkeletonShootsItsBowWithASound summons a stationary bow skeleton beside
// a survival player at midnight: each arrow comes with entity.skeleton.shoot
// in the hostile category, unless the skeleton is Silent.
// vanilla: AbstractSkeleton.performRangedAttack
func TestASkeletonShootsItsBowWithASound(t *testing.T) {
	t.Parallel()
	for _, silent := range []bool{false, true} {
		name := "audible"
		nbtSilent := ""
		if silent {
			name, nbtSilent = "silent", ",Silent:1b"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Prey"))
			srv.SetDayTime(18000)
			servertest.FreezeUntilHeld(t, srv)
			c := connect()
			c.JoinAs("Prey")
			c.MoveToGround(0.5, 64, 0.5, true)
			c.Settle()
			shootID := servertest.ConduitSoundID(t, "minecraft:entity.skeleton.shoot")

			pos := srv.PlayerPosition("Prey")
			c.ChatCommand(fmt.Sprintf(`summon minecraft:skeleton %.2f %.2f %.2f {attributes:[{id:"minecraft:movement_speed",base:0.0d}],equipment:{mainhand:{id:"minecraft:bow",count:1}}%s}`,
				pos.X+6, pos.Y, pos.Z, nbtSilent))
			c.ExpectMobSpawn(protocol.EntityTypeSkeleton)
			servertest.HoldTicks(t, srv)

			arrow, sounds := false, 0
			for tick := 0; tick < 200 && !arrow; tick++ {
				c.ReadTicks(srv, 1, func(pid int32, data []byte) bool {
					switch pid {
					case protocol.IDPlaySpawnEntity:
						if servertest.ReadThrownSpawn(c, data).EntityType == protocol.EntityTypeArrow {
							arrow = true
						}
					case protocol.IDPlaySoundEffect:
						var s servertest.SoundSee
						s.ReadSoundSee(t, data)
						if s.Sound == shootID {
							sounds++
							if s.Volume != 1 || s.Pitch < 1/1.2-0.001 || s.Pitch > 1/0.8+0.001 || s.Category != protocol.SoundCategoryHostile {
								t.Errorf("shoot sound = %+v", s)
							}
						}
					}
					return false
				})
			}
			if !arrow {
				t.Fatal("no arrow within 200 ticks")
			}
			if want := map[bool]int{false: 1, true: 0}[silent]; sounds != want {
				t.Errorf("shoot sounds = %d, want %d", sounds, want)
			}
		})
	}
}
