package commands

import (
	"os"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

func tagOrderList(keys string) string {
	return `["` + strings.Join(strings.Split(strings.ReplaceAll(keys, `\uE000`, "\uE000"), ","), `","`) + `"]`
}

func expectTagOrder(t *testing.T, c *servertest.Client, target, rich, plain, compact string) {
	t.Helper()
	for _, query := range []struct{ cmd, want string }{
		{"data get entity " + target + " Tags", rich},
		{`tellraw @s {nbt:"Tags",entity:"` + target + `"}`, rich},
		{`tellraw @s {nbt:"Tags",entity:"` + target + `",plain:1b}`, plain},
	} {
		c.ChatCommand(query.cmd)
		line := servertest.FeedbackComponent(t, c)
		text := line.Plain()
		if strings.HasPrefix(query.cmd, "data ") {
			if line.Translate != "commands.data.entity.query" || len(line.With) != 2 {
				t.Fatalf("%s: %+v", query.cmd, line)
			}
			text = line.With[1].Plain()
		}
		if text != query.want {
			t.Errorf("%s: %s, want %s", query.cmd, text, query.want)
		}
	}
	if compact != "" {
		c.ChatCommand("data modify storage order text set string entity " + target + " Tags")
		servertest.AwaitCommandError(t, c, "commands.data.modify.expected_value", compact)
	}
}

// Fixtures record Entity's HashSet and save/load sequence with vanilla's NBT
// visitors; collision trees can have a different order after a fresh load.
// vanilla: Entity.saveWithoutId, Entity.load, HashMap.TreeNode
func TestScoreboardTagHashOrder(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/entity_tag_order.tsv")
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		t.Run(f[0], func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.LevelName = t.TempDir()
			ops := servertest.OpsListing(t, "Alice")
			srv, c := servertest.Join(t, cfg, "Alice", ops)
			servertest.HoldTicks(t, srv)
			const target = "@e[type=oak_boat,limit=1]"
			c.ChatCommand(`summon oak_boat 1 64 1 {NoGravity:1b,Tags:` + tagOrderList(f[1]) + `}`)
			servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
			for op := range strings.FieldsSeq(f[2]) {
				key := op[1:]
				feedback := "commands.data.entity.modified"
				switch op[0] {
				case '+':
					c.ChatCommand("tag " + target + " add " + key)
					feedback = "commands.tag.add.success.single"
				case '-':
					c.ChatCommand("tag " + target + " remove " + key)
					feedback = "commands.tag.remove.success.single"
				case '>':
					c.ChatCommand("data modify entity " + target + " Tags append value " + key)
				case '=':
					c.ChatCommand("data modify entity " + target + " Tags set value " + tagOrderList(key))
				}
				servertest.AwaitSystemChatSaying(t, c, feedback)
			}
			expectTagOrder(t, c, target, f[3], f[4], f[5])
			srv.FlushMobs()
			srv.FlushSaves()
			servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{}, "minecraft:oak_boat", func(tag nbt.Compound) {
				if got := nbt.Stringify(tag["Tags"]); got != f[5] {
					t.Errorf("saved Tags = %s, want %s", got, f[5])
				}
			})
			c.Disconnect()
			srv.Shutdown()
			again, back := servertest.Join(t, cfg, "Alice", ops)
			servertest.HoldTicks(t, again)
			expectTagOrder(t, back, target, f[6], f[6], "")
		})
	}
}

func TestPlayerScoreboardTagHashOrder(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	ops := servertest.OpsListing(t, "Alice")
	srv, c := servertest.Join(t, cfg, "Alice", ops)
	servertest.HoldTicks(t, srv)
	for _, tag := range []string{"BB", "Aa", "order", "written"} {
		c.ChatCommand("tag @s add " + tag)
		servertest.AwaitSystemChatSaying(t, c, "commands.tag.add.success.single")
	}
	const want = `["BB", "Aa", "written", "order"]`
	const compact = `["BB","Aa","written","order"]`
	expectTagOrder(t, c, "@s", want, want, compact)
	c.Disconnect()
	srv.Shutdown()
	saved := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Alice"))
	if got := nbt.Stringify(saved["Tags"]); got != compact {
		t.Errorf("saved player tags = %s", got)
	}
	again, back := servertest.Join(t, cfg, "Alice", ops)
	servertest.HoldTicks(t, again)
	expectTagOrder(t, back, "@s", want, want, compact)
}

// A second load retains the grown table and changes this collision tree's order.
// vanilla: EntityType.create, Entity.load, HashMap.TreeNode
func TestFangsScoreboardTagHashOrder(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))
	servertest.HoldTicks(t, srv)
	const tags = `["AaBBBBAa","BBAaBBBB","BBAaAaAa","BBAaBBAa","AaBBAaAa","AaAaBBBB","AaAaBBAa","BBBBAaBB","AaAaAaBB","AaAaAaAa","AaBBBBBB","BBBBAaAa","AaBBAaBB","BBBBBBBB","BBBBBBAa","BBAaAaBB"]`
	c.ChatCommand(`summon evoker_fangs 1 64 1 {Warmup:10000,Tags:` + tags + `}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	// Recorded with sequential insertion into a fresh JDK 25 HashSet.
	const want = `["AaBBAaAa", "AaBBBBAa", "AaBBAaBB", "BBAaBBBB", "BBAaAaAa", "BBAaBBAa", "BBAaAaBB", "AaAaBBBB", "AaAaBBAa", "BBBBAaBB", "BBBBBBBB", "BBBBBBAa", "BBBBAaAa", "AaAaAaBB", "AaAaAaAa", "AaBBBBBB"]`
	expectTagOrder(t, c, "@e[type=evoker_fangs,limit=1]", want, want, "")
}
