package commands

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// Recorded by tools/java/NestedBlockNbt.java using vanilla's block loaders,
// save codecs and both TextComponentTagVisitor styles.
func TestNestedBlockCodecInsertionHistory(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/nested_block_nbt.tsv")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures [][]string
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		fixtures = append(fixtures, strings.Split(line, "\t"))
	}
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	cfg.LevelName = t.TempDir()
	join := func() (*server.Server, *servertest.Client) {
		s, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"), func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
		servertest.HoldTicks(t, s)
		return s, c
	}
	srv, c := join()
	// MovingPistonBlock.newBlockEntity returns null: supply the codec fixture
	// to an entity made by real piston movement before loading the other tags.
	for i, fixture := range fixtures {
		if fixture[0] == "moving_piston" {
			createMovingPiston(t, srv, c, int32(i+2), 64, 3)
			fixture[0] += "[facing=up]"
		}
	}
	recipeText := make(map[int]string)
	check := func(i int, fixture []string) {
		t.Helper()
		pos := fmt.Sprintf("%d 64 3", i+2)
		for style, command := range []string{
			"data get block " + pos + " " + fixture[2],
			`tellraw @s {nbt:"` + fixture[2] + `",block:"` + pos + `"}`,
			`tellraw @s {nbt:"` + fixture[2] + `",block:"` + pos + `",plain:1b}`,
		} {
			c.ChatCommand(command)
			line := servertest.FeedbackComponent(t, c)
			got := line.Plain()
			want := fixture[3]
			if style == 0 {
				if line.Translate != "commands.data.block.query" || len(line.With) != 4 {
					t.Fatalf("%s: unexpected feedback %+v", command, line)
				}
				got = line.With[3].Plain()
			}
			if style == 2 {
				want = fixture[4]
			}
			if fixture[2] == "RecipesUsed" {
				// Reference2IntOpenHashMap traverses JVM object identities. The two
				// recorded JVM runs produced opposite ties; counts and stability
				// across reads/writes are the reproducible contract.
				gotTag, gotErr := nbt.ParseSNBT(got)
				wantTag, wantErr := nbt.ParseSNBT(want)
				if gotErr != nil || wantErr != nil || nbt.Stringify(gotTag) != nbt.Stringify(wantTag) {
					t.Fatalf("recipe counts = %s, want %s", got, want)
				}
				if recipeText[i] == "" {
					recipeText[i] = got
				}
				want = recipeText[i]
			}
			if got != want {
				t.Errorf("%s/%s: got %s, want %s", fixture[0], command, got, want)
			}
		}
	}
	for i, fixture := range fixtures {
		pos := fmt.Sprintf("%d 64 3", i+2)
		c.ChatCommand("setblock " + pos + " " + fixture[0] + fixture[1])
		servertest.AwaitSystemChatSaying(t, c, "commands.setblock.success")
		c.Settle()
		check(i, fixture)
		c.ChatCommand("data merge block " + pos + " {x:99}")
		c.ExpectCommandFeedback("commands.data.block.modified", fmt.Sprint(i+2), "64", "3")
		check(i, fixture)
		c.ChatCommand("data remove block " + pos + " " + fixture[2])
		c.ExpectCommandFeedback("commands.data.block.modified", fmt.Sprint(i+2), "64", "3")
		c.ChatCommand("data modify storage nested tag set value " + fixture[1])
		c.ExpectCommandFeedback("commands.data.storage.modified", "minecraft:nested")
		c.ChatCommand("data modify block " + pos + " " + fixture[2] + " set from storage nested tag." + fixture[2])
		c.ExpectCommandFeedback("commands.data.block.modified", fmt.Sprint(i+2), "64", "3")
		check(i, fixture)
	}
	// A codec copy rebuilds a grown compound's table after each command reload.
	c.ChatCommand(`data modify block 4 64 3 lock.components."minecraft:custom_data" set value {v:1,f:2,a:1,b:1,c:1,d:1,e:1,g:1,h:1,i:1,j:1,k:1,l:1}`)
	c.ExpectCommandFeedback("commands.data.block.modified", "4", "64", "3")
	for key := range strings.SplitSeq("a,b,c,d,e,g,h,i,j,k,l,f", ",") {
		c.ChatCommand(`data remove block 4 64 3 lock.components."minecraft:custom_data".` + key)
		c.ExpectCommandFeedback("commands.data.block.modified", "4", "64", "3")
	}
	c.ChatCommand(`data modify block 4 64 3 lock.components."minecraft:custom_data".f set value 2`)
	c.ExpectCommandFeedback("commands.data.block.modified", "4", "64", "3")
	fixtures[2][3] = `{components: {"minecraft:custom_data": {v: 1, f: 2}}}`
	fixtures[2][4] = fixtures[2][3]
	check(2, fixtures[2])
	for _, cmd := range []string{
		`data modify block 2 64 3 SpawnData.entity.nested.Aa set value 9`,
		`data remove block 2 64 3 SpawnData.entity.nested.BB`,
		`data modify block 2 64 3 SpawnData.entity.nested.BB set value 8`,
		`data modify block 2 64 3 SpawnData.entity.Passengers append value {id:"pig",v:3,f:4}`,
	} {
		c.ChatCommand(cmd)
		c.ExpectCommandFeedback("commands.data.block.modified", "2", "64", "3")
	}
	fixtures[0] = fixtures[len(fixtures)-1]
	check(0, fixtures[0])
	c.ChatCommand(`data modify storage nested text set string block 2 64 3 SpawnData.entity`)
	servertest.AwaitCommandError(t, c, "commands.data.modify.expected_value", `{Passengers:[{f:2,id:"pig",v:1},{f:4,id:"pig",v:3}],bo:4,f:2,id:"minecraft:pig",m:3,nested:{Aa:9,BB:8},v:1}`)
	c.Disconnect()
	srv.Shutdown()
	srv, c = join()
	for i, fixture := range fixtures {
		check(i, fixture)
	}
}
