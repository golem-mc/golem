package commands

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: SetCustomDataFunction.run, CustomData.update, CompoundTag.merge/copy
func TestItemCustomDataMutationHistory(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	join := func() (*server.Server, *servertest.Client) {
		s, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"), func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
		servertest.HoldTicks(t, s)
		return s, c
	}
	srv, c := join()
	c.ChatCommand(`setblock 5 64 3 chest`)
	c.ExpectCommandFeedback("commands.setblock.success", "5", "64", "3")
	c.ChatCommand(`item replace block 5 64 3 container.0 with stick[custom_data={nested:{v:1,rows:[{kept:3}]},replace:0}]`)
	servertest.AwaitSystemChatSaying(t, c, itemBlockSetKey)

	check := func(slot int, path, want string) {
		t.Helper()
		path = fmt.Sprintf(`Items[%d].components."minecraft:custom_data"%s`, slot, path)
		c.ChatCommand(`data get block 5 64 3 ` + path)
		line := servertest.FeedbackComponent(t, c)
		if line.Translate != "commands.data.block.query" || len(line.With) != 4 {
			t.Fatalf("unexpected query: %+v", line)
		}
		if got := line.With[3].Plain(); got != want {
			t.Errorf("%s: data get = %s, want %s", path, got, want)
		}
		for _, plain := range []bool{false, true} {
			c.ChatCommand(fmt.Sprintf(`tellraw @s {nbt:%q,block:"5 64 3",plain:%t}`, path, plain))
			if got := servertest.FeedbackComponent(t, c).Plain(); got != want {
				t.Errorf("%s: plain=%t: %s, want %s", path, plain, got, want)
			}
		}
	}
	modify := func(slot int, modifier string) {
		t.Helper()
		c.ChatCommand(fmt.Sprintf(`item modify block 5 64 3 container.%d %s`, slot, modifier))
		servertest.AwaitSystemChatSaying(t, c, itemBlockModifyKey)
	}
	c.ChatCommand(`item replace block 5 64 3 container.1 from block 5 64 3 container.0`)
	servertest.AwaitSystemChatSaying(t, c, itemBlockReplaceKey)
	modify(1, `{type:"set_custom_data",tag:"{nested:{f:2}}"}`)
	check(1, ".nested", `{v: 1, f: 2, rows: [{kept: 3}]}`)
	modify(1, `{type:"set_custom_data",tag:"{replace:{v:1,f:2,m:3,bo:4},rows:[{v:1,f:2,m:3,bo:4}]}"}`)
	modify(1, `{type:"set_custom_data",tag:"{nested:{v:5,m:3,bo:4}}"}`)
	check(1, ".nested", `{v: 5, f: 2, rows: [{kept: 3}], m: 3, bo: 4}`)

	c.ChatCommand(`data merge storage source {object:{m:3,bo:4}}`)
	c.ExpectCommandFeedback("commands.data.storage.modified", "minecraft:source")
	modify(1, `{type:"copy_custom_data",source:{type:"storage",source:"source"},ops:[{source:"object",target:"replace",op:"merge"},{source:"object",target:"copied",op:"replace"}]}`)
	c.ChatCommand(`item replace block 5 64 3 container.2 with stick`)
	servertest.AwaitSystemChatSaying(t, c, itemBlockSetKey)
	modify(2, `{type:"set_custom_data",tag:"{v:1,f:2,m:3,bo:4}"}`)

	checks := []struct {
		slot int
		path string
		want string
	}{
		{0, ".nested", `{v: 1, rows: [{kept: 3}]}`},
		{0, ".replace", `0`},
		{1, ".nested", `{v: 5, f: 2, rows: [{kept: 3}], m: 3, bo: 4}`},
		{1, ".replace", `{v: 1, f: 2, m: 3, bo: 4}`},
		{1, ".rows[0]", `{v: 1, f: 2, m: 3, bo: 4}`},
		{1, ".copied", `{m: 3, bo: 4}`},
		{2, "", `{v: 1, f: 2, m: 3, bo: 4}`},
	}
	for _, tc := range checks {
		check(tc.slot, tc.path, tc.want)
	}
	c.Disconnect()
	srv.Shutdown()
	srv, c = join()
	for _, tc := range checks {
		check(tc.slot, tc.path, tc.want)
	}
	modify(1, `{type:"set_custom_data",tag:"{nested:{f:6}}"}`)
	check(1, ".nested", `{v: 5, f: 6, rows: [{kept: 3}], m: 3, bo: 4}`)
}
