package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestTeamFriendlyFireGatesMobTouches: a slime's touch and a pufferfish's
// sting hurt a player, unless the mob shares the player's team and the
// team's friendlyFire is off.
// vanilla: Entity.doTeamsAllowDamage, AbstractCubeMob.dealDamage, Pufferfish.playerTouch
func TestTeamFriendlyFireGatesMobTouches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, nbt, selector string
		poison              bool
	}{
		{"slime", "{Size:1}", "slime", false},
		{"pufferfish", "{PuffState:2}", "pufferfish", true},
	}
	for _, tc := range cases {
		for _, teamed := range []bool{false, true} {
			name := tc.name + "/control"
			if teamed {
				name = tc.name + "/teamed"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				const player = "Target"
				srv, c := servertest.Join(t, servertest.MobTestConfig(), player, servertest.WriteOpsFile(t, servertest.OpRecord{
					UUID: protocol.OfflineUUID(player).String(), Name: player, Level: 4,
				}))
				c.MoveToGround(0.5, 64, 0.5, true)
				c.Settle()
				srv.SetDifficulty(rules.DifficultyNormal)
				srv.SetHealth(player, 20)
				servertest.HoldTicks(t, srv)
				c.ChatCommand("summon minecraft:" + tc.name + " 30.5 64 0.5 " + tc.nbt)
				servertest.AwaitSummonFeedback(t, c, "minecraft:"+tc.name, "entity.minecraft."+tc.name)
				id := srv.MobIDsOfKind("minecraft:" + tc.name)[0]
				if teamed {
					srv.RunLine("team add t")
					srv.RunLine("team modify t friendlyFire false")
					srv.RunLine("team join t " + player)
					srv.RunLine("team join t @e[type=minecraft:" + tc.selector + ",limit=1]")
				}
				for range 30 {
					srv.MoveMob(id, 0.5, 0.5)
					servertest.RunTicks(t, srv, 2)
					if !teamed && srv.PlayerHealth(player) < 20 {
						break
					}
				}
				_, _, poisoned := srv.PlayerEffect(player, "minecraft:poison")
				hurt := srv.PlayerHealth(player) < 20
				if teamed && (hurt || poisoned) {
					t.Errorf("a teammate's %s hurt the player (health %v, poisoned %v) with friendlyFire off", tc.name, srv.PlayerHealth(player), poisoned)
				}
				if !teamed && !hurt {
					t.Errorf("control: the %s did not hurt the player", tc.name)
				}
				if !teamed && tc.poison && !poisoned {
					t.Errorf("control: the %s did not poison the player", tc.name)
				}
			})
		}
	}
}
