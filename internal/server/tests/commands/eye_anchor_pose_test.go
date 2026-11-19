package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAnchoredEyesFollowScaleAndPose puts a marker at the player's eyes: the
// eye height of the pose, times the scale attribute.
// vanilla: Entity.getEyePosition
func TestAnchoredEyesFollowScaleAndPose(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, scale string
		sneak       bool
		want        string // eye height in thousandths (a range: 64+1.27 is not exact in a double)
	}{
		{"scale 2", "2", false, "3240"},
		{"sneaking", "1", true, "1269..1270"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.ItemCreativeConfig(), "Actor", servertest.OpsListing(t, "Actor"))
			servertest.HoldTicks(t, srv)
			c.ChatCommand("attribute @s minecraft:scale base set " + tc.scale)
			servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")
			if tc.sneak {
				c.SendSneak(true)
				servertest.RunTicks(t, srv, 3)
			}
			c.ChatCommand(`execute anchored eyes positioned ^ ^ ^ run summon marker ~ ~ ~ {Tags:["k"]}`)
			servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
			c.ChatCommand("scoreboard objectives add y dummy")
			servertest.AwaitSystemChatSaying(t, c, "commands.scoreboard.objectives.add.success")
			c.ChatCommand("execute store result score p y run data get entity @s Pos[1] 1000")
			c.ChatCommand("execute store result score m y run data get entity @e[tag=k,limit=1] Pos[1] 1000")
			c.ChatCommand("scoreboard players operation m y -= p y")
			c.ChatCommand("execute if score m y matches " + tc.want)
			servertest.AwaitSystemChatSaying(t, c, "commands.execute.conditional.pass")
		})
	}
}
