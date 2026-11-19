package sounds

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// A blow that kills an iron golem and changes its crack level plays the damage
// sound after the death sound. vanilla: IronGolem.hurtServer
func TestKilledIronGolemPlaysCrackSoundAfterDeath(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), "Slayer", servertest.OpsListing(t, "Slayer"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	c.ChatCommand("summon minecraft:iron_golem 5.5 64 5.5 {NoAI:1b}")
	c.ExpectMobSpawn(protocol.EntityTypeIronGolem)
	c.Settle()
	c.ChatCommand("kill @e[type=iron_golem]")
	var sounds []string
	for range 2 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlaySoundEffect {
				return
			}
			switch s := servertest.SoundOf(data); s {
			case "minecraft:entity.iron_golem.death", "minecraft:entity.iron_golem.damage":
				sounds = append(sounds, s)
			}
		})
	}
	want := []string{"minecraft:entity.iron_golem.death", "minecraft:entity.iron_golem.damage"}
	if len(sounds) != 2 || sounds[0] != want[0] || sounds[1] != want[1] {
		t.Errorf("sounds %v; want %v", sounds, want)
	}
}
