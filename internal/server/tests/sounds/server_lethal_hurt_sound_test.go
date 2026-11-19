package sounds

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// A mob a blow kills plays its death sound and not the hurt sound; a blow it
// survives plays the hurt sound. vanilla: LivingEntity.hurtServer, die
func TestLethalBlowPlaysOnlyTheDeathSound(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, health, command string
		attack                bool
		wantHurt, wantDeath   int
	}{
		{"melee kill", "1f", "", true, 0, 1},
		{"damage kill", "1f", "damage @e[type=pig,limit=1] 5", false, 0, 1},
		{"survived", "20f", "damage @e[type=pig,limit=1] 1", false, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), "Slayer", servertest.OpsListing(t, "Slayer"))
			c.MoveToGround(2.5, 64, 5.5, true)
			c.Settle()
			servertest.HoldTicks(t, srv)
			servertest.AwaitFullSwing(t, srv, "Slayer")
			c.ChatCommand("summon minecraft:pig 5.5 64 5.5 {NoAI:1b,Health:" + tc.health + "}")
			pig := c.ExpectMobSpawn(protocol.EntityTypePig).EntityID
			c.Settle()
			if tc.attack {
				c.Attack(pig)
			} else {
				c.ChatCommand(tc.command)
			}
			hurt, death := 0, 0
			for range 30 {
				srv.TickMobs(1)
				c.SettleAndCollect(func(id int32, data []byte) {
					if id != protocol.IDPlaySoundEffect {
						return
					}
					switch servertest.SoundOf(data) {
					case "minecraft:entity.pig.hurt":
						hurt++
					case "minecraft:entity.pig.death":
						death++
					}
				})
			}
			if hurt != tc.wantHurt || death != tc.wantDeath {
				t.Errorf("hurt sounds %d, death sounds %d; want %d and %d", hurt, death, tc.wantHurt, tc.wantDeath)
			}
		})
	}
}
