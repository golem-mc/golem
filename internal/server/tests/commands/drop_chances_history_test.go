package commands

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// Recorded with tools/java/DropChancesOrder.java against 26.3, with argument 1
// and with no argument. JVM identities select legal native histories, not one
// universal order. mainhand and head collide in the encoded CompoundTag.
func TestDropChancesNativeHistory(t *testing.T) {
	t.Parallel()
	for _, recording := range []struct {
		name   string
		hashes [8]uint32
		first  string
	}{
		{"mainhand-first", [8]uint32{1897221921, 368153528, 622263400, 810196347, 1811787796, 1921553024, 1617937669, 78265654}, "mainhand: 2.0f, head: 2.0f"},
		{"head-first", [8]uint32{1768253416, 1105563869, 1719133872, 45422147, 2017231980, 355635406, 1286190782, 1711580774}, "head: 2.0f, mainhand: 2.0f"},
	} {
		t.Run(recording.name, func(t *testing.T) {
			cfg := servertest.BlockTestConfig()
			cfg.LevelName = t.TempDir()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			ops := servertest.OpsListing(t, "Alice")
			srv, c := servertest.Join(t, cfg, "Alice", ops)
			servertest.HoldTicks(t, srv)
			fixtures := []struct{ slots, want, compact string }{
				{"mainhand,offhand,feet,legs,chest,head", "{" + recording.first + ", feet: 2.0f, chest: 2.0f, legs: 2.0f, offhand: 2.0f}", "{chest:2.0f,feet:2.0f,head:2.0f,legs:2.0f,mainhand:2.0f,offhand:2.0f}"},
				{"mainhand,chest", "{mainhand: 2.0f, chest: 2.0f}", "{chest:2.0f,mainhand:2.0f}"},
				{"mainhand,head", "{" + recording.first + "}", "{head:2.0f,mainhand:2.0f}"},
				{"chest,head", "{head: 2.0f, chest: 2.0f}", "{chest:2.0f,head:2.0f}"},
				{"feet,legs,offhand", "{feet: 2.0f, legs: 2.0f, offhand: 2.0f}", "{feet:2.0f,legs:2.0f,offhand:2.0f}"},
				{"head", "{head: 2.0f}", "{head:2.0f}"},
			}
			selector := func(i int) string { return fmt.Sprintf("@e[tag=chances%d,limit=1]", i) }
			for i, f := range fixtures {
				// Reverse the command input so it cannot supply the native insertion order.
				keys := strings.Split(f.slots, ",")
				slices.Reverse(keys)
				tag := "{" + strings.Join(keys, ":2f,") + ":2f}"
				c.ChatCommand(fmt.Sprintf(`summon zombie 4.5 70 4.5 {NoAI:1b,NoGravity:1b,PersistenceRequired:1b,Tags:["chances%d"],drop_chances:%s}`, i, tag))
				servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
			}
			srv.SetDropChancesOrder(recording.hashes)
			check := func(c *servertest.Client) {
				t.Helper()
				c.Settle()
				for i, f := range fixtures {
					for _, cmd := range []string{
						"data get entity " + selector(i) + " drop_chances",
						`tellraw @s {nbt:'drop_chances',entity:'` + selector(i) + `'}`,
						`tellraw @s {nbt:'drop_chances',entity:'` + selector(i) + `',plain:1b}`,
					} {
						c.ChatCommand(cmd)
						line := servertest.FeedbackComponent(t, c)
						text := line.Plain()
						if strings.HasPrefix(cmd, "data ") {
							if line.Translate != "commands.data.entity.query" || len(line.With) != 2 {
								t.Fatalf("%s: unexpected query: %+v", cmd, line)
							}
							text = line.With[1].Plain()
						}
						if text != f.want {
							t.Errorf("%s: got %s, want %s", cmd, text, f.want)
						}
					}
					c.ChatCommand("data modify storage history compact set string entity " + selector(i) + " drop_chances")
					servertest.AwaitCommandError(t, c, "commands.data.modify.expected_value", f.compact)
				}
			}
			check(c)
			for i := range fixtures {
				// Reading back the ordered native child must retain all slot values.
				c.ChatCommand("data merge entity " + selector(i) + " {Health:2f}")
				servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
			}
			check(c)
			for i, f := range fixtures {
				keys := strings.Split(f.slots, ",")
				key := keys[0]
				c.ChatCommand("data remove entity " + selector(i) + " drop_chances." + key)
				servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
				// Re-inserting a slot rebuilds the codec map, rather than retaining
				// the command compound's mutation history.
				c.ChatCommand("data modify entity " + selector(i) + " drop_chances." + key + " set value 2f")
				servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
			}
			check(c)
			srv.FlushMobs()
			c.Disconnect()
			srv.Shutdown()
			again, connect := servertest.Start(t, cfg, ops)
			servertest.FreezeUntilHeld(t, again)
			back := connect().Join("Alice")
			servertest.HoldTicks(t, again)
			servertest.UntilTicks(t, again, 20, "saved drop chances owners wake", func() bool {
				return len(again.SelectedEntities("minecraft:zombie")) == len(fixtures)
			})
			again.SetDropChancesOrder(recording.hashes)
			check(back)
		})
	}
}
