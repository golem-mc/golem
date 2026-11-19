package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: EntityDataAccessor.setData, BlockDataAccessor.setData,
// CustomData.CODEC, SpawnData.CODEC, TextComponentTagVisitor.visitCompound
func TestDataWriteNestedCompoundHistory(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"), func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon item 4 65 4 {NoGravity:1b,PickupDelay:32767s,Item:{id:"minecraft:stone",count:1}}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	c.ChatCommand(`setblock 2 64 3 spawner`)
	servertest.AwaitSystemChatSaying(t, c, "commands.setblock.success")
	c.ChatCommand(`setblock 3 64 3 chest`)
	servertest.AwaitSystemChatSaying(t, c, "commands.setblock.success")
	c.ChatCommand(`summon armor_stand 4 65 5 {NoGravity:1b}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	c.Settle()
	fixtures := []struct{ target, path, source, merge, modified string }{
		{`entity @e[type=item,limit=1]`, `Item.components."minecraft:custom_data"`, `entity:"@e[type=item,limit=1]"`, `{Item:{components:{"minecraft:custom_data":{v:1,f:2}}}}`, "commands.data.entity.modified"},
		{`entity @e[type=armor_stand,limit=1]`, `equipment.head.components."minecraft:custom_data".nested`, `entity:"@e[type=armor_stand,limit=1]"`, `{equipment:{head:{id:"minecraft:stone",count:1,components:{"minecraft:custom_data":{nested:{v:1,f:2}}}}}}`, "commands.data.entity.modified"},
		{`block 3 64 3`, `Items[0].components."minecraft:custom_data".nested`, `block:"3 64 3"`, `{Items:[{Slot:0b,id:"minecraft:stone",count:1,components:{"minecraft:custom_data":{nested:{v:1,f:2}}}}]}`, "commands.data.block.modified"},
		{`block 2 64 3`, `SpawnData.entity.nested`, `block:"2 64 3"`, `{SpawnData:{entity:{id:"pig",nested:{v:1,f:2}}}}`, "commands.data.block.modified"},
	}
	check := func(c *servertest.Client, target, path, source, want string) {
		t.Helper()
		c.ChatCommand(`data get ` + target + ` ` + path)
		line := servertest.FeedbackComponent(t, c)
		if len(line.With) == 0 {
			t.Fatalf("%s: unexpected query response: %+v", target, line)
		}
		if got := line.With[len(line.With)-1].Plain(); got != want {
			t.Errorf("%s: got %s, want %s", target, got, want)
		}
		for _, plain := range []string{"", ",plain:1b"} {
			c.ChatCommand(`tellraw @s {nbt:'` + path + `',` + source + plain + `}`)
			if got := servertest.FeedbackComponent(t, c).Plain(); got != want {
				t.Errorf("%s plain=%s: got %s, want %s", target, plain, got, want)
			}
		}
	}
	for _, f := range fixtures {
		c.ChatCommand(`data merge ` + f.target + ` ` + f.merge)
		servertest.AwaitSystemChatSaying(t, c, f.modified)
		c.Settle()
		check(c, f.target, f.path, f.source, `{v: 1, f: 2}`)
		c.ChatCommand(`data modify ` + f.target + ` ` + f.path + ` merge value {m:3,bo:4}`)
		servertest.AwaitSystemChatSaying(t, c, f.modified)
		c.Settle()
		check(c, f.target, f.path, f.source, `{v: 1, f: 2, m: 3, bo: 4}`)
		c.ChatCommand(`data remove ` + f.target + ` ` + f.path + `.v`)
		servertest.AwaitSystemChatSaying(t, c, f.modified)
		c.Settle()
		c.ChatCommand(`data modify ` + f.target + ` ` + f.path + `.v set value 5`)
		servertest.AwaitSystemChatSaying(t, c, f.modified)
		c.Settle()
		check(c, f.target, f.path, f.source, `{f: 2, v: 5, m: 3, bo: 4}`)
		c.ChatCommand(`data modify storage sorted value set string ` + f.target + ` ` + f.path)
		servertest.AwaitCommandError(t, c, "commands.data.modify.expected_value", `{bo:4,f:2,m:3,v:5}`)
	}
	srv.FlushMobs()
	c.Conn.Close()
	srv.Shutdown()
	restarted := *cfg
	againSrv, again := servertest.Join(t, &restarted, "Alice", servertest.OpsListing(t, "Alice"), func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
	servertest.HoldTicks(t, againSrv)
	servertest.RunTicks(t, againSrv, 1)
	again.Settle()
	for _, f := range fixtures {
		check(again, f.target, f.path, f.source, `{f: 2, v: 5, m: 3, bo: 4}`)
	}
}
