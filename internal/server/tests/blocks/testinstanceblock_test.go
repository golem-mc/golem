package blocks

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// myTest is the data the screen sends for minecraft:my_test, a 2x1x2 box.
var myTest = servertest.TestInstanceData{Test: "minecraft:my_test", Size: [3]int32{2, 1, 2}, IgnoreEntities: true}

// awaitSystemText reads up to the next System Chat and checks it is want.
func awaitSystemText(t *testing.T, c *servertest.Client, want protocol.Text) {
	t.Helper()
	var b protocol.Buffer
	b.PutText(want)
	b.PutBool(false)
	if got := c.AwaitPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, b.Bytes()) {
		text, _ := protocol.ReadText(protocol.NewReader(got))
		t.Errorf("system chat = %+v, want %+v", text, want)
	}
}

// TestTheTestInstanceBlockScreenSetsAndSaves: the screen's packets carry the
// block's data; Init is answered and keeps the player connected, Set stores
// the data, and Save writes the box above the block as the test's template,
// leaving air out.
// vanilla: ServerGamePacketListenerImpl.handleTestInstanceBlockAction, TestInstanceBlockEntity.saveTest
func TestTheTestInstanceBlockScreenSetsAndSaves(t *testing.T) {
	t.Parallel()
	srv, c, dir := structureScene(t)
	servertest.Setblock(t, c, 10, 64, 10, "minecraft:test_instance_block")
	c.Settle()

	c.TestInstanceBlockAction(10, 64, 10, servertest.TestInstanceInit, myTest)
	c.AwaitPlay(protocol.IDPlayTestInstanceBlockStatus)
	c.Settle()

	c.TestInstanceBlockAction(10, 64, 10, servertest.TestInstanceSet, myTest)
	x, y, z, _, tag := c.AwaitBlockEntityData()
	if [3]int32{x, y, z} != [3]int32{10, 64, 10} {
		t.Fatalf("block entity data for %d,%d,%d, want 10,64,10", x, y, z)
	}
	data, _ := nbt.Get[nbt.Compound](tag, "data")
	want := nbt.Compound{
		"test": nbt.String("minecraft:my_test"), "size": nbt.IntArray{2, 1, 2}, "rotation": nbt.String("none"),
		"ignore_entities": nbt.Byte(1), "status": nbt.String("cleared"),
	}
	if !reflect.DeepEqual(data, want) {
		t.Errorf("data = %v, want %v", data, want)
	}

	// The box starts one up and one south of the block.
	stone := servertest.MustState(t, "minecraft:stone", nil)
	air := servertest.MustState(t, "minecraft:air", nil)
	gold := servertest.MustState(t, "minecraft:gold_block", nil)
	for _, p := range [][3]int32{{10, 65, 11}, {11, 65, 11}, {10, 65, 12}, {11, 65, 12}} {
		srv.SetBlock(p[0], p[1], p[2], air)
	}
	srv.SetBlock(10, 65, 11, stone)
	c.TestInstanceBlockAction(10, 64, 10, servertest.TestInstanceSave, myTest)
	c.Settle()
	if _, err := os.Stat(filepath.Join(dir, "generated", "minecraft", "structure", "my_test.nbt")); err != nil {
		t.Fatalf("no template file: %v", err)
	}

	for _, p := range [][3]int32{{20, 64, 20}, {21, 64, 20}, {20, 64, 21}, {21, 64, 21}} {
		srv.SetBlock(p[0], p[1], p[2], gold)
	}
	c.ChatCommand("place template minecraft:my_test 20 64 20")
	servertest.AwaitCommandFeedbackText(t, c, "commands.place.template.success", protocol.Text{Text: "minecraft:my_test"},
		servertest.NumberText(20), servertest.NumberText(64), servertest.NumberText(20))
	servertest.ExpectBlockNamed(t, srv, 20, 64, 20, "minecraft:stone", nil)
	for _, p := range [][3]int32{{21, 64, 20}, {20, 64, 21}, {21, 64, 21}} {
		servertest.ExpectBlockNamed(t, srv, p[0], p[1], p[2], "minecraft:gold_block", nil)
	}
}

// TestATestInstancePacketNeedsACreativeOperator: a survival player who is no
// operator stays connected and changes nothing.
// vanilla: Player.canUseGameMasterBlocks
func TestATestInstancePacketNeedsACreativeOperator(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.GameMode = "survival"
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Guest")
	c.Settle()
	block := servertest.MustState(t, "minecraft:test_instance_block", nil)
	srv.SetBlock(10, 64, 10, block)
	for _, action := range []int32{servertest.TestInstanceInit, servertest.TestInstanceSet, servertest.TestInstanceSave} {
		c.TestInstanceBlockAction(10, 64, 10, action, myTest)
	}
	c.Settle()
	if tag, ok := srv.BlockEntityTagAt(10, 64, 10); ok {
		if _, has := tag["data"]; has {
			t.Errorf("a survival guest's Set stored %v", tag["data"])
		}
	}
	if _, err := os.Stat(filepath.Join(cfg.LevelName, "generated")); err == nil {
		t.Error("a survival guest's Save wrote a template")
	}
}

// testInstanceStatus is a Test Instance Block Status payload.
func testInstanceStatus(status protocol.Text, size *[3]int32) []byte {
	var b protocol.Buffer
	b.PutText(status)
	b.PutBool(size != nil)
	if size != nil {
		for _, n := range size {
			b.PutVarInt(n)
		}
	}
	return b.Bytes()
}

// describeRow is one row of a test's description.
func describeRow(key string, value protocol.Text, extra ...protocol.Text) protocol.Text {
	value.Color = "blue"
	return protocol.Text{Translate: key, With: []protocol.Text{value}, Extra: append([]protocol.Text{{Text: "\n"}}, extra...)}
}

// testPackScene joins an operator to a world whose data pack registers
// pk:house (a block-based test of pk:house_shape with padding 1) and the
// extra files, with a test instance block at 10,64,10. The caller resets
// the process-wide pack registry table.
func testPackScene(t *testing.T, files map[string]string) (*server.Server, *servertest.Client, *config.Config) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	dir := filepath.Join(cfg.LevelName, "datapacks", "p")
	files["pack.mcmeta"] = `{"pack":{"description":"p"}}`
	files["data/pk/test_instance/house.json"] = `{"type":"minecraft:block_based","environment":"minecraft:default",
		"structure":"pk:house_shape","max_ticks":100,"padding":1,"required":true}`
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv, c := servertest.Join(t, cfg, "Actor", servertest.OpsListing(t, "Actor"))
	c.Settle()
	servertest.Setblock(t, c, 10, 64, 10, "minecraft:test_instance_block")
	c.Settle()
	return srv, c, cfg
}

// TestARegisteredTestInstanceIsDescribedSavedAndReset: a data pack's
// block-based test is described on Init and Query, Query adds its template's
// size once saved, Save writes the structure the test names at the padded
// corner, and Reset clears the padded box and places the structure again.
// The core pack's always_pass is described as a function test. Not parallel:
// the pack registry table is process-wide.
// vanilla: ServerGamePacketListenerImpl.handleTestInstanceBlockAction, GameTestInstance.describe,
// TestInstanceBlockEntity.saveTest, TestInstanceBlockEntity.resetTest
func TestARegisteredTestInstanceIsDescribedSavedAndReset(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRegistries(nil) })
	srv, c, cfg := testPackScene(t, map[string]string{
		"data/pk/test_instance/house_turned.json": `{"type":"minecraft:block_based","environment":"minecraft:default",
			"structure":"pk:house_shape","max_ticks":100,"padding":1,"rotation":"clockwise_90"}`,
		"data/pk/test_instance/bad.json": `{"type":"minecraft:block_based","environment":"minecraft:default",
			"structure":"pk:house_shape","max_ticks":100,"setup_ticks":-1}`,
	})

	house := servertest.TestInstanceData{Test: "pk:house", Size: [3]int32{2, 1, 2}}
	described := describeRow("test_instance.description.type", protocol.Text{Translate: "test_instance.type.block_based"},
		describeRow("test_instance.description.structure", protocol.Text{Text: "pk:house_shape"},
			describeRow("test_instance.description.batch", protocol.Text{Text: "minecraft:default"}),
			describeRow("test_instance.description.dimension", protocol.Text{Text: "minecraft:overworld"})))
	expectStatus := func(action int32, data servertest.TestInstanceData, want []byte) {
		t.Helper()
		c.TestInstanceBlockAction(10, 64, 10, action, data)
		if got := c.AwaitPlay(protocol.IDPlayTestInstanceBlockStatus); !bytes.Equal(got, want) {
			text, _ := protocol.ReadText(protocol.NewReader(got))
			t.Errorf("action %d: status %+v (% x), want % x", action, text, got, want)
		}
	}
	expectStatus(servertest.TestInstanceInit, house, testInstanceStatus(described, nil))
	expectStatus(servertest.TestInstanceQuery, house, testInstanceStatus(described, nil))
	expectStatus(servertest.TestInstanceInit, servertest.TestInstanceData{Test: "minecraft:always_pass"}, testInstanceStatus(
		describeRow("test_instance.description.type", protocol.Text{Translate: "test_instance.type.function"},
			describeRow("test_instance.description.function", protocol.Text{Text: "minecraft:always_pass"}),
			describeRow("test_instance.description.structure", protocol.Text{Text: "minecraft:empty"},
				describeRow("test_instance.description.batch", protocol.Text{Text: "minecraft:default"}),
				describeRow("test_instance.description.dimension", protocol.Text{Text: "minecraft:overworld"}))), nil))
	// TestData.CODEC refuses a negative setup_ticks, so the entry is no test.
	expectStatus(servertest.TestInstanceInit, servertest.TestInstanceData{Test: "pk:bad"},
		testInstanceStatus(protocol.Text{Translate: "test_instance.description.no_test", Color: "red"}, nil))

	// With padding 1 the structure starts at 11,66,12, a 2x1x2 box.
	stone := servertest.MustState(t, "minecraft:stone", nil)
	air := servertest.MustState(t, "minecraft:air", nil)
	for _, p := range [][3]int32{{11, 66, 12}, {12, 66, 12}, {11, 66, 13}, {12, 66, 13}} {
		srv.SetBlock(p[0], p[1], p[2], air)
	}
	srv.SetBlock(11, 66, 12, stone)
	c.TestInstanceBlockAction(10, 64, 10, servertest.TestInstanceSave, house)
	c.Settle()
	if _, err := os.Stat(filepath.Join(cfg.LevelName, "generated", "pk", "structure", "house_shape.nbt")); err != nil {
		t.Fatalf("no template file under the structure id: %v", err)
	}
	expectStatus(servertest.TestInstanceQuery, house, testInstanceStatus(described, &[3]int32{2, 1, 2}))

	// The padded box spans 10..13, 65..67, 11..14.
	gold := servertest.MustState(t, "minecraft:gold_block", nil)
	for _, p := range [][3]int32{{11, 66, 12}, {12, 66, 13}, {10, 65, 11}, {13, 67, 14}} {
		srv.SetBlock(p[0], p[1], p[2], gold)
	}
	c.TestInstanceBlockAction(10, 64, 10, servertest.TestInstanceReset, house)
	awaitSystemText(t, c, protocol.Text{Translate: "test_instance_block.reset_success", Color: "green",
		With: []protocol.Text{{Text: "pk:house"}}})
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 11, 66, 12, "minecraft:stone", nil)
	for _, p := range [][3]int32{{12, 66, 13}, {10, 65, 11}, {13, 67, 14}} {
		servertest.ExpectBlockNamed(t, srv, p[0], p[1], p[2], "minecraft:air", nil)
	}
	servertest.ExpectBlockNamed(t, srv, 10, 64, 10, "minecraft:test_instance_block", nil)

	// clockwise_90 starts the template at the north-east cell, so the stone
	// at its origin lands at 12,66,12 and the opposite corner stays air.
	// vanilla: TestInstanceBlockEntity.getStartCorner
	c.TestInstanceBlockAction(10, 64, 10, servertest.TestInstanceReset, servertest.TestInstanceData{Test: "pk:house_turned", Size: house.Size})
	awaitSystemText(t, c, protocol.Text{Translate: "test_instance_block.reset_success", Color: "green",
		With: []protocol.Text{{Text: "pk:house_turned"}}})
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 12, 66, 12, "minecraft:stone", nil)
	for _, p := range [][3]int32{{11, 66, 12}, {11, 66, 13}, {12, 66, 13}} {
		servertest.ExpectBlockNamed(t, srv, p[0], p[1], p[2], "minecraft:air", nil)
	}
}

// testInstanceDataAt reads the block's stored data.
func testInstanceDataAt(srv *server.Server, x, y, z int32) nbt.Compound {
	tag, _ := srv.BlockEntityTagAt(x, y, z)
	data, _ := nbt.Get[nbt.Compound](tag, "data")
	return data
}

// TestRunningAlwaysPassFinishesTheTest: Run on always_pass says the test is
// starting, walls the structure in barriers, and the runner ends the test a
// few ticks later: the block's status is finished with no error, the
// barriers are gone and every player is told it passed.
// vanilla: TestInstanceBlockEntity.runTest, GameTestInfo.tick, ReportGameListener.testPassed
func TestRunningAlwaysPassFinishesTheTest(t *testing.T) {
	t.Parallel()
	srv, c, _ := structureScene(t)
	servertest.HoldTicks(t, srv)
	servertest.Setblock(t, c, 10, 64, 10, "minecraft:test_instance_block")
	c.Settle()

	c.TestInstanceBlockAction(10, 64, 10, servertest.TestInstanceRun,
		servertest.TestInstanceData{Test: "minecraft:always_pass", Size: [3]int32{1, 1, 1}})
	awaitSystemText(t, c, protocol.Text{Translate: "test_instance_block.starting", With: []protocol.Text{{Text: "minecraft:always_pass"}}})
	c.Settle()
	// The empty 1x1x1 structure sits at 10,65,11; its shell's floor is y=64
	// and its ceiling y=66.
	servertest.ExpectBlockNamed(t, srv, 9, 65, 11, "minecraft:barrier", nil)
	servertest.ExpectBlockNamed(t, srv, 10, 66, 11, "minecraft:barrier", nil)
	servertest.ExpectBlockNamed(t, srv, 10, 64, 10, "minecraft:test_instance_block", nil)
	if got := testInstanceDataAt(srv, 10, 64, 10)["status"]; got != nbt.String("cleared") {
		t.Fatalf("status before the setup ticks = %v, want cleared", got)
	}

	// Setup ticks 1 and a tick delay of 1 start the test on the third tick.
	servertest.RunTicks(t, srv, 2)
	if got := testInstanceDataAt(srv, 10, 64, 10)["status"]; got != nbt.String("cleared") {
		t.Errorf("status after 2 ticks = %v, want cleared", got)
	}
	servertest.RunTicks(t, srv, 1)
	data := testInstanceDataAt(srv, 10, 64, 10)
	if data["status"] != nbt.String("finished") || data["error_message"] != nil {
		t.Fatalf("data after 3 ticks = %v, want finished with no error", data)
	}
	if data["test"] != nbt.String("minecraft:always_pass") {
		t.Errorf("test = %v, want minecraft:always_pass", data["test"])
	}
	servertest.ExpectBlockNamed(t, srv, 9, 65, 11, "minecraft:air", nil)
	servertest.ExpectBlockNamed(t, srv, 10, 66, 11, "minecraft:air", nil)

	passed := false
	c.ReadTicks(srv, 2, func(id int32, payload []byte) bool {
		if id == protocol.IDPlaySystemChat {
			text, _ := protocol.ReadText(protocol.NewReader(payload))
			passed = passed || text.Color == protocol.ColorGreen && strings.HasPrefix(text.Text, "minecraft:always_pass passed! (")
		}
		return passed
	})
	if !passed {
		t.Error("no green passed line")
	}
}

// TestATestEnvironmentIsSetUpAndTornDown: running a function test whose
// environment combines a game_rules entry, inline setup and teardown
// functions, a difficulty, a clock time and a weather sets the rules, runs the
// setup function and sets the difficulty, time and weather before the setup
// ticks, and restores all of them, in reverse, once the test has finished. Not parallel: the pack registry table
// is process-wide.
// vanilla: GameTestRunner.runBatch, TestEnvironmentDefinition.AllOf, SetGameRules, Functions, SetDifficulty, ClockTime, Weather
func TestATestEnvironmentIsSetUpAndTornDown(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRegistries(nil) })
	srv, c, _ := testPackScene(t, map[string]string{
		"data/pk/test_environment/rules.json": `{"type":"minecraft:game_rules",
			"rules":{"minecraft:keep_inventory":true,"max_entity_cramming":5}}`,
		"data/pk/test_environment/batch.json": `{"type":"minecraft:all_of","definitions":["pk:rules",
			{"type":"minecraft:function","setup":"pk:setup","teardown":"pk:teardown"},
			{"type":"minecraft:difficulty","difficulty":"hard"},
			{"type":"clock_time","clock":"overworld","time":6000},
			{"type":"weather","weather":"thunder"}]}`,
		"data/pk/function/setup.mcfunction":    "gamerule mob_griefing false\n",
		"data/pk/function/teardown.mcfunction": "gamerule mob_griefing true\n",
		"data/pk/test_instance/env.json": `{"type":"minecraft:function","function":"minecraft:always_pass",
			"environment":"pk:batch","structure":"minecraft:empty","max_ticks":1,"setup_ticks":5}`,
	})
	servertest.HoldTicks(t, srv)
	difficulty := srv.Difficulty()
	if difficulty == 3 {
		t.Fatal("the world starts on hard, so the environment's difficulty shows nothing")
	}
	if srv.GameRule("keep_inventory") || srv.GameRuleValue("max_entity_cramming") != 24 || !srv.GameRule("mob_griefing") {
		t.Fatal("the rules do not start at their defaults")
	}
	_, dayTime := srv.Time()
	if dayTime > 5000 && dayTime < 7000 {
		t.Fatalf("the day time starts at %d, too near the environment's 6000", dayTime)
	}
	if srv.Raining() || srv.Thundering() {
		t.Fatal("the world does not start clear")
	}

	c.TestInstanceBlockAction(10, 64, 10, servertest.TestInstanceRun, servertest.TestInstanceData{Test: "pk:env", Size: [3]int32{1, 1, 1}})
	awaitSystemText(t, c, protocol.Text{Translate: "test_instance_block.starting", With: []protocol.Text{{Text: "pk:env"}}})
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	if got := testInstanceDataAt(srv, 10, 64, 10)["status"]; got != nbt.String("cleared") {
		t.Fatalf("status during the setup ticks = %v, want cleared", got)
	}
	if !srv.GameRule("keep_inventory") || srv.GameRuleValue("max_entity_cramming") != 5 {
		t.Errorf("during the test keep_inventory = %v, max_entity_cramming = %d, want true and 5",
			srv.GameRule("keep_inventory"), srv.GameRuleValue("max_entity_cramming"))
	}
	if srv.GameRule("mob_griefing") {
		t.Error("the setup function did not run: mob_griefing is still true")
	}
	if got := srv.Difficulty(); got != 3 {
		t.Errorf("difficulty during the test = %d, want 3 (hard)", got)
	}
	if _, got := srv.Time(); got < 6000 || got > 6010 {
		t.Errorf("day time during the test = %d, want 6000 and the ticks since", got)
	}
	if !srv.Raining() || !srv.Thundering() {
		t.Errorf("during the test raining = %v, thundering = %v, want both", srv.Raining(), srv.Thundering())
	}

	// Setup ticks 5 and a tick delay of 1 finish always_pass on the seventh tick.
	servertest.RunTicks(t, srv, 5)
	if got := testInstanceDataAt(srv, 10, 64, 10)["status"]; got != nbt.String("finished") {
		t.Fatalf("status after 7 ticks = %v, want finished", got)
	}
	if srv.GameRule("keep_inventory") || srv.GameRuleValue("max_entity_cramming") != 24 {
		t.Errorf("after the test keep_inventory = %v, max_entity_cramming = %d, want false and 24",
			srv.GameRule("keep_inventory"), srv.GameRuleValue("max_entity_cramming"))
	}
	if !srv.GameRule("mob_griefing") {
		t.Error("the teardown function did not run: mob_griefing is still false")
	}
	if got := srv.Difficulty(); got != difficulty {
		t.Errorf("difficulty after the test = %d, want %d", got, difficulty)
	}
	if _, got := srv.Time(); got < dayTime || got > dayTime+10 {
		t.Errorf("day time after the test = %d, want %d and the ticks since", got, dayTime)
	}
	if srv.Raining() || srv.Thundering() {
		t.Errorf("after the test raining = %v, thundering = %v, want clear", srv.Raining(), srv.Thundering())
	}
}

// TestRunningABlockBasedTestFollowsItsTestBlocks: Run on a block-based test
// powers the structure's start block on the starting tick; an accept block
// beside it triggers and the test passes at once, and a fail block beside it
// fails the test with its message, read again on the tick that ends it, as
// the failure leaves the block triggered. Not
// parallel: the pack registry table is process-wide.
// vanilla: BlockBasedTestInstance.run, TestBlock.neighborChanged, TestBlock.ownSignal, TestBlockEntity.trigger
func TestRunningABlockBasedTestFollowsItsTestBlocks(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRegistries(nil) })
	srv, c, _ := testPackScene(t, map[string]string{})
	house := servertest.TestInstanceData{Test: "pk:house", Size: [3]int32{2, 1, 2}}
	const modeStart, modeFail, modeAccept = 0, 2, 3

	// The structure (11..12, 66, 12..13) holds a start block at 11,66,12,
	// the block under test beside it at 12,66,12 and, unless that is one,
	// an accept block out of the start block's reach at 12,66,13: a test
	// with no accept block fails.
	build := func(mode int32, message string) {
		t.Helper()
		air := servertest.MustState(t, "minecraft:air", nil)
		for _, p := range [][3]int32{{11, 66, 12}, {12, 66, 12}, {11, 66, 13}, {12, 66, 13}} {
			srv.SetBlock(p[0], p[1], p[2], air)
		}
		servertest.Setblock(t, c, 11, 66, 12, "minecraft:test_block")
		servertest.Setblock(t, c, 12, 66, 12, "minecraft:test_block")
		c.SetTestBlock(11, 66, 12, modeStart, "")
		c.SetTestBlock(12, 66, 12, mode, message)
		if mode != modeAccept {
			servertest.Setblock(t, c, 12, 66, 13, "minecraft:test_block")
			c.SetTestBlock(12, 66, 13, modeAccept, "")
		}
		c.TestInstanceBlockAction(10, 64, 10, servertest.TestInstanceSave, house)
		c.Settle()
	}
	run := func() {
		t.Helper()
		servertest.HoldTicks(t, srv)
		c.TestInstanceBlockAction(10, 64, 10, servertest.TestInstanceRun, house)
		awaitSystemText(t, c, protocol.Text{Translate: "test_instance_block.starting", With: []protocol.Text{{Text: "pk:house"}}})
		c.Settle()
	}

	build(modeAccept, "")
	run()
	// No setup ticks and a tick delay of 1 start the test on the second tick.
	servertest.RunTicks(t, srv, 2)
	data := testInstanceDataAt(srv, 10, 64, 10)
	if data["status"] != nbt.String("finished") || data["error_message"] != nil {
		t.Fatalf("accept: data = %v, want finished with no error", data)
	}
	servertest.ExpectBlockNamed(t, srv, 10, 66, 11, "minecraft:air", nil)

	build(modeFail, "boom")
	run()
	servertest.RunTicks(t, srv, 2)
	if got := testInstanceDataAt(srv, 10, 64, 10)["status"]; got != nbt.String("running") {
		t.Errorf("fail: status on the starting tick = %v, want running", got)
	}
	servertest.RunTicks(t, srv, 1)
	data = testInstanceDataAt(srv, 10, 64, 10)
	want := protocol.TextTag(protocol.Text{Translate: "test.error.tick", With: []protocol.Text{{Text: "boom"}, servertest.NumberText(1)}})
	if data["status"] != nbt.String("finished") || !nbt.Equal(data["error_message"], want) {
		t.Errorf("fail: data = %v, want finished with error %v", data, want)
	}
}
