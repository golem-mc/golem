package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: ExperienceOrb.readAdditionalSaveData, Entity.load
func TestSummonReadsParsedScalars(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))
	servertest.HoldTicks(t, srv)

	query := func(t *testing.T, selector, field, want string) {
		t.Helper()
		c.ChatCommand("data get entity " + selector + " " + field)
		line := servertest.FeedbackComponent(t, c)
		if line.Translate != "commands.data.entity.query" || len(line.With) != 2 ||
			!textMatches(line.With[1], servertest.NBTArg(want)) {
			t.Errorf("%s %s = %s, want %s", selector, field, line.Plain(), want)
		}
	}

	for _, tc := range []struct{ value, want string }{
		{"0x10s", "16s"},
		{"65537", "1s"},
		{"-0.5d", "-1s"},
		{`"16"`, "0s"},
	} {
		t.Run("Value/"+tc.value, func(t *testing.T) {
			c.ChatCommand("summon experience_orb 4.5 70 4.5 {Value:" + tc.value + ",NoGravity:TRUE}")
			servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
			selector := "@e[type=experience_orb,limit=1]"
			query(t, selector, "Value", tc.want)
			query(t, selector, "NoGravity", "1b")
			c.ChatCommand("kill " + selector)
			servertest.AwaitSystemChatSaying(t, c, "commands.kill.success.single")
		})
	}

	for _, kind := range []struct{ name, tag string }{
		{"experience_orb", "Value:1s"},
		{"item", `Item:{id:"minecraft:stone",count:1}`},
		{"tnt", "fuse:1000s"},
	} {
		for _, gravity := range []string{"bool(1)", "TRUE", "0x101", "-0.5d"} {
			t.Run(kind.name+"/NoGravity/"+gravity, func(t *testing.T) {
				c.ChatCommand("summon " + kind.name + " 4.5 70 4.5 {" + kind.tag + ",NoGravity:" + gravity + "}")
				servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
				selector := "@e[type=" + kind.name + ",limit=1]"
				query(t, selector, "NoGravity", "1b")
				c.ChatCommand("kill " + selector)
				servertest.AwaitSystemChatSaying(t, c, "commands.kill.success.single")
			})
		}
	}
}
