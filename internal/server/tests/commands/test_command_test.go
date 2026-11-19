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

// vanilla: TestCommand.reset, clear, StructureUtils.findNearestTest
func TestTestResetAndClear(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)
	c.Settle()
	put := func(x, z int32, id string, size [3]int32, rotation string) {
		t.Helper()
		srv.SetBlock(x, 64, z, registry.BlockTestInstanceBlock.DefaultState())
		srv.SetBlockEntityTagAt(x, 64, z, "minecraft:test_instance_block", nbt.Compound{
			"data": nbt.Compound{"test": nbt.String(id), "size": nbt.IntArray{size[0], size[1], size[2]},
				"rotation": nbt.String(rotation), "ignore_entities": nbt.Byte(1), "status": nbt.String("finished"),
				"error_message": nbt.String("failed")},
			"errors": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				nbt.Compound{"pos": nbt.IntArray{x, 65, z + 1}, "text": nbt.String("failed")},
			}},
		})
	}
	put(4, 4, "minecraft:always_pass", [3]int32{1, 1, 1}, "none")
	put(7, 0, "minecraft:always_pass", [3]int32{1, 1, 1}, "none")
	gold := registry.BlockGoldBlock.DefaultState()
	srv.SetBlock(4, 65, 5, gold)
	srv.SetBlock(7, 65, 1, gold)
	srv.SetBlock(6, 65, 1, registry.BlockBarrier.DefaultState())
	srv.SetBlock(8, 65, 1, gold)

	// Manhattan distances 8 and 7 choose (7,64,0), while Euclidean
	// distance would choose (4,64,4). /execute's position is the origin.
	c.ChatCommand("execute positioned 0 64 0 run test resetclosest")
	servertest.AwaitCommandFeedbackText(t, c, "commands.test.reset.success", servertest.NumberText(1))
	servertest.ExpectBlockNamed(t, srv, 4, 65, 5, "minecraft:gold_block", nil)
	servertest.ExpectBlockNamed(t, srv, 7, 65, 1, "minecraft:air", nil)
	servertest.ExpectBlockNamed(t, srv, 6, 65, 1, "minecraft:air", nil)
	servertest.ExpectBlockNamed(t, srv, 8, 65, 1, "minecraft:gold_block", nil)
	tag, _ := srv.BlockEntityTagAt(7, 64, 0)
	data, _ := nbt.Get[nbt.Compound](tag, "data")
	if data["status"] != nbt.String("cleared") || data["error_message"] != nil || tag["errors"] != nil {
		t.Errorf("reset data = %v, errors = %v", data, tag["errors"])
	}
	if !nbt.BoolField(data, "ignore_entities") || data["test"] != nbt.String("minecraft:always_pass") {
		t.Errorf("reset lost settings: %v", data)
	}

	// The look selects the structure's box, through intervening blocks.
	c.MoveTo(4.5, 64, 0.5, 0, 0)
	c.Settle()
	srv.SetBlock(4, 65, 2, registry.BlockStone.DefaultState())
	c.ChatCommand("test resetthat")
	servertest.AwaitCommandFeedbackText(t, c, "commands.test.reset.success", servertest.NumberText(1))
	servertest.ExpectBlockNamed(t, srv, 4, 65, 5, "minecraft:air", nil)
	c.MoveTo(4.5, 64, 5.5, 0, 0)
	c.Settle()
	c.ChatCommand("test resetthat")
	servertest.AwaitCommandErrorText(t, c, "commands.test.clear.error.no_tests")
	c.MoveTo(4.5, 64, 0.5, 0, 0)
	c.Settle()

	srv.SetBlock(4, 65, 5, gold)
	srv.SetBlock(7, 65, 1, gold)
	c.ChatCommand("test resetthese")
	servertest.AwaitCommandFeedbackText(t, c, "commands.test.reset.success", servertest.NumberText(2))
	servertest.ExpectBlockNamed(t, srv, 4, 65, 5, "minecraft:air", nil)
	servertest.ExpectBlockNamed(t, srv, 7, 65, 1, "minecraft:air", nil)

	// Clear accepts unregistered tests and uses the saved size and rotation.
	put(12, 0, "missing:test", [3]int32{2, 1, 3}, "clockwise_90")
	srv.SetBlock(14, 65, 2, gold)
	srv.SetBlock(15, 65, 2, registry.BlockBarrier.DefaultState())
	srv.SetBlock(15, 65, 1, gold)
	c.ChatCommand("execute positioned 7 64 0 run test clearall -1")
	servertest.AwaitCommandFeedbackText(t, c, "commands.test.clear.success", servertest.NumberText(1))
	servertest.ExpectBlockNamed(t, srv, 7, 64, 0, "minecraft:air", nil)
	servertest.ExpectBlockNamed(t, srv, 4, 64, 4, "minecraft:test_instance_block", nil)
	c.ChatCommand("test clearall")
	servertest.AwaitCommandFeedbackText(t, c, "commands.test.clear.success", servertest.NumberText(2))
	for _, p := range [][3]int32{{4, 64, 4}, {12, 64, 0}, {14, 65, 2}, {15, 65, 2}} {
		servertest.ExpectBlockNamed(t, srv, p[0], p[1], p[2], "minecraft:air", nil)
	}
	servertest.ExpectBlockNamed(t, srv, 15, 65, 1, "minecraft:gold_block", nil)
	if _, ok := srv.BlockEntityTagAt(12, 64, 0); ok {
		t.Error("cleared test instance block retained its block entity")
	}
	for _, sub := range []string{"resetclosest", "resetthese", "resetthat", "clearall", "clearthat", "clearthese"} {
		c.ChatCommand("test " + sub)
		servertest.AwaitCommandErrorText(t, c, "commands.test.clear.error.no_tests")
	}

	put(4, 4, "minecraft:always_pass", [3]int32{1, 1, 1}, "none")
	put(7, 0, "minecraft:always_pass", [3]int32{1, 1, 1}, "none")
	c.ChatCommand("test clearthat")
	servertest.AwaitCommandFeedbackText(t, c, "commands.test.clear.success", servertest.NumberText(1))
	servertest.ExpectBlockNamed(t, srv, 4, 64, 4, "minecraft:air", nil)
	servertest.ExpectBlockNamed(t, srv, 7, 64, 0, "minecraft:test_instance_block", nil)
	c.ChatCommand("test clearthese")
	servertest.AwaitCommandFeedbackText(t, c, "commands.test.clear.success", servertest.NumberText(1))
	servertest.ExpectBlockNamed(t, srv, 7, 64, 0, "minecraft:air", nil)
	put(7, 0, "missing:test", [3]int32{1, 1, 1}, "none")
	c.ChatCommand("execute positioned 7 64 0 run test resetclosest")
	servertest.AwaitCommandErrorText(t, c, "commands.test.error.non_existant_test", protocol.Text{Text: "missing:test"})
	servertest.AwaitCommandErrorText(t, c, "commands.test.clear.error.no_tests")
	servertest.ExpectBlockNamed(t, srv, 7, 64, 0, "minecraft:test_instance_block", nil)
}

// Stop returns one without feedback and leaves the detached test unticked.
// vanilla: TestCommand.stopTests
func TestTestStop(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRegistries(nil) })
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	for name, body := range map[string]string{
		"pack.mcmeta": `{"pack":{"description":"stop"}}`,
		"data/test/test_environment/stopping.json": `{"type":"minecraft:game_rules","rules":{"minecraft:keep_inventory":true}}`,
		"data/test/test_instance/stopping.json": `{"type":"minecraft:function","function":"minecraft:always_pass",
			"environment":"test:stopping","structure":"minecraft:empty","max_ticks":1,"setup_ticks":5}`,
	} {
		path := filepath.Join(cfg.LevelName, "datapacks", "p", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv, c := servertest.Join(t, cfg, "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)
	servertest.Setblock(t, c, 10, 64, 10, "minecraft:test_instance_block")
	c.TestInstanceBlockAction(10, 64, 10, servertest.TestInstanceRun,
		servertest.TestInstanceData{Test: "test:stopping", Size: [3]int32{1, 1, 1}})
	servertest.AwaitCommandFeedbackText(t, c, "test_instance_block.starting", protocol.Text{Text: "test:stopping"})
	c.Settle()
	if !srv.GameRule("keep_inventory") {
		t.Fatal("test environment was not set up")
	}
	c.ChatCommand("execute store result storage test:stop result int 1 run test stop")
	c.Settle()
	if srv.GameRule("keep_inventory") {
		t.Error("stop did not tear down the test environment")
	}
	c.ChatCommand("data get storage test:stop result")
	servertest.AwaitCommandFeedback(t, c, "commands.data.storage.query", "test:stop", "1")
	servertest.RunTicks(t, srv, 8)
	tag, _ := srv.BlockEntityTagAt(10, 64, 10)
	data, _ := nbt.Get[nbt.Compound](tag, "data")
	if data["status"] != nbt.String("cleared") {
		t.Errorf("stopped test was ticked: %v", data)
	}
	servertest.ExpectBlockNamed(t, srv, 9, 65, 11, "minecraft:barrier", nil)

	// Even an unsuccessful reset stops the current run before searching.
	c.TestInstanceBlockAction(10, 64, 10, servertest.TestInstanceRun,
		servertest.TestInstanceData{Test: "test:stopping", Size: [3]int32{1, 1, 1}})
	servertest.AwaitCommandFeedbackText(t, c, "test_instance_block.starting", protocol.Text{Text: "test:stopping"})
	c.ChatCommand("execute positioned 100 64 100 run test resetclosest")
	servertest.AwaitCommandErrorText(t, c, "commands.test.clear.error.no_tests")
	if srv.GameRule("keep_inventory") {
		t.Error("empty reset did not tear down the test environment")
	}
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

// vanilla: FailedTestTracker, ReportGameListener.handleRetry, GameTestBatchFactory
func TestTestRunFailedAndEnvironments(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRegistries(nil) })
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	for name, body := range map[string]string{
		"pack.mcmeta":                      `{"pack":{"description":"run"}}`,
		"data/p/test_environment/one.json": `{"type":"minecraft:game_rules","rules":{"minecraft:keep_inventory":true}}`,
		"data/p/test_environment/two.json": `{"type":"minecraft:game_rules","rules":{"minecraft:keep_inventory":false}}`,
		"data/p/test_instance/pass.json":   `{"type":"minecraft:function","function":"minecraft:always_pass","structure":"minecraft:empty","environment":"p:one","max_ticks":1,"setup_ticks":1}`,
		"data/p/test_instance/bad.json":    `{"type":"minecraft:function","function":"p:missing","structure":"minecraft:empty","environment":"p:two","max_ticks":1,"setup_ticks":1,"required":false}`,
	} {
		path := filepath.Join(cfg.LevelName, "datapacks", "p", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv, c := servertest.Join(t, cfg, "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)
	run := func(command string, count int64) {
		t.Helper()
		c.ChatCommand("test " + command)
		servertest.AwaitCommandFeedbackText(t, c, "commands.test.run.running", servertest.NumberText(count))
		c.Settle()
	}
	run("run p:pass", 1)
	if !srv.GameRule("keep_inventory") {
		t.Fatal("pass environment was not activated")
	}
	servertest.RunTicks(t, srv, 3)
	if srv.GameRule("keep_inventory") {
		t.Fatal("pass environment was not restored")
	}
	run("run p:bad 0 true", 1)
	servertest.RunTicks(t, srv, 4)
	c.ChatCommand("test runfailed true")
	servertest.AwaitCommandFeedbackText(t, c, "commands.test.no_tests")
	run("runfailed false 2 false", 1)
	servertest.RunTicks(t, srv, 8)
	second := c.ReadTicks(srv, 0, func(id int32, payload []byte) bool {
		if id != protocol.IDPlaySystemChat {
			return false
		}
		text, _ := protocol.ReadText(protocol.NewReader(payload))
		return strings.Contains(text.Text, "[Run:    2, Ok:    0, Fail:    2, Left:    0]")
	})
	if !second {
		t.Error("failed test did not finish its second command retry")
	}
	run("run p:*", 2)
	servertest.RunTicks(t, srv, 8)
	if srv.GameRule("keep_inventory") {
		t.Error("batched environments were not restored")
	}
	c.ChatCommand("test verify p:bad")
	c.Settle()
	servertest.RunTicks(t, srv, 8)
	for x, want := range map[int32]string{0: "finished", 6: "running"} {
		tag, _ := srv.BlockEntityTagAt(x, 64, 3)
		data, _ := nbt.Get[nbt.Compound](tag, "data")
		if data["rotation"] != nbt.String("none") || data["status"] != nbt.String(want) {
			t.Errorf("halted verification at x=%d = %v", x, data)
		}
	}
}

// Each verification batch reuses a 10x10 grid and advances the extra rotation.
// vanilla: TestCommand.verify, StructureGridSpawner.onBatchStart
func TestTestVerify(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("test verify always_pass")
	c.Settle()
	for rotation, name := range []string{"none", "clockwise_90", "180", "counterclockwise_90"} {
		tag, _ := srv.BlockEntityTagAt(0, 64, 3)
		data, _ := nbt.Get[nbt.Compound](tag, "data")
		if data["rotation"] != nbt.String(name) || data["status"] != nbt.String("cleared") {
			t.Fatalf("batch %d data = %v", rotation, data)
		}
		// Row length 10, X stride 1+5, Z stride 1+1+6.
		servertest.ExpectBlockNamed(t, srv, 54, 64, 75, "minecraft:test_instance_block", nil)
		servertest.RunTicks(t, srv, 3)
	}
	tag, _ := srv.BlockEntityTagAt(0, 64, 3)
	data, _ := nbt.Get[nbt.Compound](tag, "data")
	if data["status"] != nbt.String("finished") {
		t.Errorf("final status = %v", data["status"])
	}
}
