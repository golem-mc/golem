package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestTestCreatePosAndLocate: /test create clears a box beside the player
// and floors it with bedrock under a test instance block, /test pos names
// the looked-at block's place in that test and highlights it, and /test
// locate finds the block by a wildcard over the registered tests. On the
// flat world the surface beside the player at (0.5, 64, 0.5) is y=64. Not
// parallel: the pack registry table is process-wide.
// vanilla: TestCommand.createNewStructure, showPos, locate
func TestTestCreatePosAndLocate(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRegistries(nil) })
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	dir := filepath.Join(cfg.LevelName, "datapacks", "p")
	for name, body := range map[string]string{
		"pack.mcmeta": `{"pack":{"description":"p"}}`,
		"data/minecraft/test_instance/foo.json": `{"type":"minecraft:block_based","environment":"minecraft:default",
			"structure":"minecraft:foo","max_ticks":100}`,
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv, c := servertest.Join(t, cfg, "Tester", servertest.OpsListing(t, "Tester"))
	c.Settle()

	c.Run(t, "Create", func(t *testing.T) {
		c.ChatCommand("test create minecraft:foo 3")
		servertest.AwaitCommandFeedbackText(t, c, "commands.test.create.success", protocol.Text{Text: "minecraft:foo"})
		servertest.ExpectBlockNamed(t, srv, 0, 64, 3, "minecraft:test_instance_block", nil)
		tag, _ := srv.OverworldWorld().BlockEntityTag(0, 64, 3)
		data, _ := nbt.Get[nbt.Compound](tag, "data")
		if size, _ := nbt.Get[nbt.IntArray](data, "size"); !nbt.Equal(size, nbt.IntArray{3, 3, 3}) {
			t.Errorf("size = %v, want [3,3,3]", size)
		}
		if status, _ := nbt.Get[nbt.String](data, "status"); status != "cleared" {
			t.Errorf("status = %q, want cleared", status)
		}
		if test, _ := nbt.Get[nbt.String](data, "test"); test != "minecraft:foo" {
			t.Errorf("test = %q, want minecraft:foo", test)
		}
		// The structure's corner is one up and one south of the block.
		for x := range int32(3) {
			for z := int32(4); z < 7; z++ {
				servertest.ExpectBlockNamed(t, srv, x, 65, z, "minecraft:bedrock", nil)
			}
		}
		servertest.ExpectBlockNamed(t, srv, 1, 66, 5, "minecraft:air", nil)

		c.ChatCommand("test create minecraft:foo 49")
		servertest.AwaitCommandErrorText(t, c, "commands.test.error.too_large", servertest.NumberText(48))
	})

	c.Run(t, "Pos", func(t *testing.T) {
		// Standing on the bedrock looking down at (1,65,5), (1,0,1) from the
		// structure's corner (0,65,4).
		c.MoveTo(1.5, 66, 5.5, 0, 90)
		c.Settle()
		c.ChatCommand("test pos here")
		coords := protocol.Text{
			Translate: "commands.test.coordinates",
			With:      []protocol.Text{servertest.NumberText(1), servertest.NumberText(0), servertest.NumberText(1)},
			Bold:      true, Color: protocol.ColorGreen,
			HoverText: &protocol.Text{Translate: "commands.test.coordinates.copy"},
			ClickCopy: "final BlockPos here = new BlockPos(1, 0, 1);",
		}
		servertest.AwaitCommandFeedbackText(t, c, "commands.test.relative_position", protocol.Text{Text: "minecraft:foo"}, coords)
		var want protocol.Buffer
		want.PutPosition(1, 65, 5)
		want.PutPosition(1, 0, 1)
		if got := c.AwaitPacket(protocol.IDPlayGameTestHighlightPos); !bytes.Equal(got, want.Bytes()) {
			t.Errorf("highlight pos = %x, want %x", got, want.Bytes())
		}
	})

	c.Run(t, "Locate", func(t *testing.T) {
		// From (1,66,5) the block at (0,64,3) is floor(sqrt(5)) = 2 away; the
		// click stands 3 north of it, facing south.
		c.ChatCommand("test locate *:foo")
		servertest.AwaitCommandFeedbackText(t, c, "commands.test.locate.started")
		found := protocol.Text{
			Translate: "chat.square_brackets",
			With: []protocol.Text{{
				Translate: "chat.coordinates",
				With:      []protocol.Text{servertest.NumberText(0), servertest.NumberText(64), servertest.NumberText(3)},
				Extra:     []protocol.Text{{Text: " ->[overworld]"}},
			}},
			Color:        protocol.ColorGreen,
			ClickCommand: "/execute in minecraft:overworld run tp @s 0 64 0 0 0",
			HoverText:    &protocol.Text{Translate: "chat.coordinates.tooltip"},
		}
		servertest.AwaitCommandFeedbackText(t, c, "commands.test.locate.found", found, protocol.Text{Text: "2"})
		servertest.AwaitCommandFeedbackText(t, c, "commands.test.locate.done", servertest.NumberText(1))

		const bad = "test locate nomatch"
		c.ChatCommand(bad)
		c.ExpectCommandError(
			servertest.CommandErrorMessageForTest("argument.resource_selector.not_found", "minecraft:nomatch", "minecraft:test_instance"),
			servertest.CommandErrorContextForTest(bad, len(bad)))
	})
	c.Run(t, "ResetWithoutTemplate", func(t *testing.T) {
		c.ChatCommand("execute positioned 0 64 3 run test resetclosest")
		servertest.AwaitCommandErrorText(t, c, "commands.test.error.structure_not_found", protocol.Text{Text: "minecraft:foo"})
		servertest.AwaitCommandErrorText(t, c, "commands.test.clear.error.no_tests")
		servertest.ExpectBlockNamed(t, srv, 0, 65, 4, "minecraft:bedrock", nil)
	})
}

// Every root child and the retry/grid arguments match the 26.3 commands.json.
func TestTestCommandTreeMatchesReport(t *testing.T) {
	t.Parallel()
	_, c := servertest.Dial(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Tester"))
	c.Login("Tester")
	c.Configure()
	nodes := servertest.DecodeCommands(t, c.AwaitPlay(protocol.IDPlayCommands))
	c.SkipJoin()
	var children []string
	for _, n := range nodes {
		if n.Name != "test" || n.Typ != 1 {
			continue
		}
		for _, i := range n.Children {
			child := nodes[i]
			children = append(children, child.Name)
			if child.Name == "clearall" {
				if len(child.Children) != 1 {
					t.Fatal("clearall has no radius argument")
				}
				radius := nodes[child.Children[0]]
				if radius.Name != "radius" || radius.Parser != 3 || !radius.Executable {
					t.Errorf("clearall radius = %+v", radius)
				}
			} else if !slices.Contains([]string{"create", "pos", "locate", "run", "runmultiple", "verify"}, child.Name) && !child.Executable {
				t.Errorf("%s is not executable", child.Name)
			}
		}
	}
	// vanilla: TestCommand.register
	want := []string{"run", "runmultiple", "runthese", "runclosest", "runthat", "runfailed", "verify", "locate", "resetclosest", "resetthese", "resetthat", "clearthat", "clearthese", "clearall", "stop", "pos", "create"}
	if !slices.Equal(children, want) {
		t.Errorf("test children = %v, want %v", children, want)
	}
	child := func(n servertest.TestCommandNode, name string) servertest.TestCommandNode {
		t.Helper()
		for _, i := range n.Children {
			if nodes[i].Name == name {
				return nodes[i]
			}
		}
		t.Fatalf("%s lacks %s", n.Name, name)
		return servertest.TestCommandNode{}
	}
	root := child(nodes[0], "test")
	for _, name := range []string{"run", "runfailed", "runclosest", "runthese", "runthat"} {
		n := child(root, name)
		if name == "run" {
			n = child(n, "tests")
		}
		for _, variant := range []bool{false, true} {
			if variant {
				if name != "runfailed" {
					continue
				}
				n = child(n, "onlyRequiredTests")
				if n.Parser != 0 || !n.Executable {
					t.Errorf("onlyRequiredTests = %+v", n)
				}
			}
			number := child(n, "numberOfTimes")
			until := child(number, "untilFailed")
			if number.Parser != 3 || !number.Executable || until.Parser != 0 || !until.Executable {
				t.Errorf("retry nodes: %+v, %+v", number, until)
			}
			if name == "run" || name == "runfailed" {
				rotation := child(until, "rotationSteps")
				row := child(rotation, "testsPerRow")
				if rotation.Parser != 3 || !rotation.Executable || row.Parser != 3 || !row.Executable {
					t.Errorf("grid nodes: %+v, %+v", rotation, row)
				}
			}
		}
	}
	for _, name := range []string{"run", "runmultiple", "verify", "locate"} {
		if n := child(child(root, name), "tests"); n.Parser != 48 || !n.Executable {
			t.Errorf("%s selector = %+v", name, n)
		}
	}
	if n := child(child(child(root, "runmultiple"), "tests"), "amount"); n.Parser != 3 || !n.Executable {
		t.Errorf("amount = %+v", n)
	}
	c.Disconnect()
}

// vanilla: TestCommand.run, StructureGridSpawner, RetryOptions
func TestTestRunCommands(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)
	c.Settle()
	run := func(command string, count int64) {
		t.Helper()
		c.ChatCommand(command)
		servertest.AwaitCommandFeedbackText(t, c, "commands.test.run.running", servertest.NumberText(count))
		c.Settle()
	}
	const invalid = "test runclosest -1"
	c.ChatCommand(invalid)
	c.ExpectCommandError(servertest.CommandErrorMessageForTest("argument.integer.low", "0", "-1"),
		servertest.CommandErrorContextForTest(invalid, strings.Index(invalid, "-1")))
	status := func(x int32, want string) {
		t.Helper()
		tag, _ := srv.BlockEntityTagAt(x, 64, 3)
		data, _ := nbt.Get[nbt.Compound](tag, "data")
		if data["status"] != nbt.String(want) {
			t.Errorf("test at %d status = %v, want %s", x, data["status"], want)
		}
	}
	run("test runmultiple always_pass 2", 2)
	status(0, "cleared")
	status(6, "cleared")
	servertest.RunTicks(t, srv, 3)
	status(0, "finished")
	status(6, "finished")
	run("execute positioned 0 64 3 run test runclosest 2", 1)
	servertest.RunTicks(t, srv, 3)
	status(0, "cleared") // the second attempt is prepared at the batch boundary
	servertest.RunTicks(t, srv, 3)
	status(0, "finished")
	c.MoveTo(0.5, 64, 0.5, 0, 0)
	c.Settle()
	srv.SetBlock(0, 65, 2, registry.BlockStone.DefaultState())
	run("test runthat", 1)
	servertest.RunTicks(t, srv, 3)
	status(0, "finished")
	run("test runthese", 2)
	servertest.RunTicks(t, srv, 3)
	status(0, "finished")
	status(6, "finished")
	run("test run always_pass 1 false 1 1", 1)
	tag, _ := srv.BlockEntityTagAt(0, 64, 3)
	data, _ := nbt.Get[nbt.Compound](tag, "data")
	if data["rotation"] != nbt.String("clockwise_90") {
		t.Errorf("rotation = %v", data["rotation"])
	}
	servertest.RunTicks(t, srv, 3)
	run("test runclosest 0", 1)
	servertest.RunTicks(t, srv, 9)
	status(0, "cleared") // unlimited retries still have another prepared attempt
	c.ChatCommand("test clearall")
	servertest.AwaitCommandFeedbackText(t, c, "commands.test.clear.success", servertest.NumberText(2))
	c.ChatCommand("test runthese")
	servertest.AwaitCommandFeedbackText(t, c, "commands.test.no_tests")
	c.ChatCommand("test runmultiple always_pass 0")
	servertest.AwaitCommandFeedbackText(t, c, "commands.test.no_tests")
	run("test runmultiple always_pass 51", 51)
	servertest.ExpectBlockNamed(t, srv, 12, 64, 51, "minecraft:air", nil)
	servertest.RunTicks(t, srv, 3)
	servertest.ExpectBlockNamed(t, srv, 12, 64, 51, "minecraft:test_instance_block", nil)
	servertest.RunTicks(t, srv, 3)
}
