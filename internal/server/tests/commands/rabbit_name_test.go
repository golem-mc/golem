package commands

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/google/uuid"
)

// vanilla: Rabbit.setVariant, SummonCommand.spawnEntity
func TestSummonRabbitFeedbackUsesCreatedName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, tag, translation string
	}{
		{"ordinary", "RabbitType:0", "entity.minecraft.rabbit"},
		{"killer", "RabbitType:99", "entity.minecraft.killer_bunny"},
		{"named killer", `RabbitType:99,CustomName:"Bob"`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, c := servertest.Join(t, servertest.MobTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))
			c.ChatCommand("tick freeze")
			servertest.AwaitSystemChatSaying(t, c, "commands.tick.status.frozen")
			identity := uuid.MustParse("00000001-0000-0002-0000-000300000004")
			want := servertest.EntityDisplayComponent("minecraft:rabbit", tc.translation, identity)
			if tc.translation == "" {
				want = namedDisplay("minecraft:rabbit", "Bob", 4)
			}
			c.ChatCommand(fmt.Sprintf(`summon rabbit ~-2 ~ ~3 {UUID:[I;1,2,3,4],NoAI:1b,NoGravity:1b,Tags:["k"],%s}`, tc.tag))
			servertest.AwaitCommandFeedbackText(t, c, "commands.summon.success", want)
			c.ChatCommand(`data get entity @e[tag=k,limit=1] NoAI`)
			servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.query", want, servertest.NBTArg("1b"))
			c.ChatCommand(`kill @e[tag=k,limit=1]`)
			servertest.AwaitCommandFeedbackText(t, c, "commands.kill.success.single", want)
		})
	}
}
