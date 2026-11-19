package server

import (
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// gameTestTicker holds the run a test instance block's Run started; a new
// Run replaces it.
// vanilla: GameTestTicker, GameTestRunner.currentEnvironment
type gameTestTicker struct {
	mu  sync.Mutex
	run []*gameTest // every test the run started, reruns included
	// rerun are the flaky tests to run again once the batch has ended.
	// vanilla: GameTestRunner.scheduledForRerun
	rerun []*gameTest
	// pending holds unprepared batches; no later batch replaces structures
	// until the preceding batch has finished.
	pending   [][]*gameTest
	failed    map[string]bool                // holder id -> required
	grids     map[*world.World]*gameTestGrid // written under start
	request   command.TestRun
	lastBatch []*gameTest
	// teardown restores what the batch's environment replaced; whoever
	// detaches the run or ends the batch takes it.
	teardown func()
	// start is held while a batch's environment is torn down or set up, so
	// two never interleave: by a Run replacing the run, and by the tick loop
	// ending a batch.
	start sync.Mutex
}

// gameTest is one test the runner ticks, written only by the tick loop once
// started.
// vanilla: GameTestInfo
type gameTest struct {
	w    *world.World
	pos  [3]int32 // the test instance block
	id   string
	test datapack.TestInstance
	data testInstanceData // the block's data as the runner set it
	// tick counts up from minus the setup ticks; the test starts at 0.
	tick         int32
	started      bool
	done         bool
	began        time.Time
	ran          time.Duration
	err          *gameTestError
	tries        *gameTestTries // shared with the test's reruns
	retries      *command.TestRetries
	prepared     bool
	batch        int
	grid, verify bool
	// eachTick is set once a block-based test's start block fired: its test
	// blocks are then read every tick until max_ticks.
	// vanilla: GameTestHelper.onEachTick
	eachTick bool
}

// gameTestTries counts a test's attempts and passes across its reruns.
// vanilla: ReportGameListener
type gameTestTries struct{ attempts, successes int32 }

// gameTestError is why a test failed: description is the block's error
// message, message the chat line's.
// vanilla: GameTestException
type gameTestError struct {
	description protocol.Text
	message     string
}

// failAt fails the test with an assertion at the current tick.
// vanilla: GameTestInfo.fail(Component), GameTestAssertException
func (t *gameTest) failAt(message protocol.Text) {
	desc := protocol.Text{Translate: "test.error.tick", With: []protocol.Text{message, {Number: new(int64(t.tick))}}}
	t.err = &gameTestError{description: desc, message: desc.Plain()}
}

// failWith fails the test with an error whose description is its plain
// message, as a timeout or an unknown failure is shown.
// vanilla: ReportGameListener.reportFailure (Util.describeError)
func (t *gameTest) failWith(message string) {
	t.err = &gameTestError{description: protocol.Text{Text: message}, message: message}
}

// finish marks the test done, stopping its clock.
// vanilla: GameTestInfo.finish
func (t *gameTest) finish() {
	if !t.done {
		t.done = true
		if t.started {
			t.ran = time.Since(t.began)
		}
	}
}

// gameTestScope is the test world's command scope.
func (s *Server) gameTestScope(w *world.World) worldScope {
	return worldScope{srv: s, in: func() *dimension { return s.dimensionOfWorld(w) }, quiet: true}
}

// prepareGameTest readies the block's test to run in place: the block's data
// reset to the template's size, the structure placed again and walled in
// barriers. It returns the test, counting down its setup ticks, and the
// block's new data. tries is the attempts so far of the test a rerun
// repeats, nil for a first run.
// vanilla: GameTestInfo.prepareTestStructure, GameTestInfo.placeStructure, StructureSpawner.IN_PLACE
func (s *Server) prepareGameTest(w *world.World, x, y, z int32, data testInstanceData, test datapack.TestInstance, tries *gameTestTries) (*gameTest, testInstanceData) {
	// vanilla: GameTestInfo.createTestInstanceBlock
	size := [3]int32{1, 1, 1}
	if t, ok := s.structures.Get(test.Structure); ok {
		size = t.Size
	}
	data = testInstanceData{test: data.test, size: size, rotation: data.rotation}
	if tries == nil {
		tries = &gameTestTries{}
	}
	gt := &gameTest{w: w, pos: [3]int32{x, y, z}, id: data.test, test: test, data: data, tries: tries, prepared: true}
	ws := s.gameTestScope(w)
	if !ws.placeTestStructure(w, x, y, z, data, test) {
		gt.failAt(protocol.Text{Translate: "test.error.structure.failure", With: []protocol.Text{{Text: data.test}}})
	}
	// vanilla: TestInstanceBlockEntity.encaseStructure
	barrier := registry.BlockBarrier.DefaultState()
	var walls []world.BlockChange
	forTestBoundary(x, y, z, data, test, func(bx, by, bz int32) {
		if registry.BlockOf(w.BlockAt(bx, by, bz)) != registry.BlockTestInstanceBlock {
			walls = append(walls, world.BlockChange{X: bx, Y: by, Z: bz, State: barrier})
		}
	})
	ws.FillCells(walls, nil, nil, nil, false, false, true)
	// vanilla: ReportGameListener.testStructureLoaded
	tries.attempts++
	// vanilla: GameTestInfo.startExecution (a tick delay of 1)
	gt.tick = -(test.SetupTicks + 2)
	return gt, data
}

// startGameTests replaces the ticker's run with tests, tearing down the
// replaced run's environment before setting up the batch's, and says the
// batch is starting.
// vanilla: GameTestTicker.clear, GameTestRunner.runBatch, TestCommand.trackAndStartRunner
func (s *Server) startGameTests(tests ...*gameTest) {
	s.startGameTestRun(tests, command.TestRun{})
}

// startGameTestRun owns the grid and the queued batches until stopped.
func (s *Server) startGameTestRun(tests []*gameTest, request command.TestRun) {
	s.gameTests.start.Lock()
	defer s.gameTests.start.Unlock()
	s.stopGameTestsLocked()
	s.gameTests.request, s.gameTests.grids, s.gameTests.lastBatch = request, nil, nil
	batches := batchGameTests(tests)
	tests = s.prepareGameTestBatch(batches[0])
	teardown := s.setUpTestEnvironment(tests[0].w, tests[0].test.Env)
	s.sayGameTestBatchStarting(tests[0])
	s.gameTests.mu.Lock()
	s.gameTests.run, s.gameTests.teardown = tests, teardown
	s.gameTests.pending = batches[1:]
	s.gameTests.mu.Unlock()
}

// batchGameTests partitions each environment and dimension into batches of 50.
// Inline environment holders belong to their own test instance.
// vanilla: GameTestBatchFactory.fromGameTestInfo
func batchGameTests(tests []*gameTest) [][]*gameTest {
	type key struct {
		w        *world.World
		env      string
		rotation int32
	}
	groups := map[key][]*gameTest{}
	var order []key
	for _, t := range tests {
		env := t.test.Environment
		if env == "" {
			env = "inline:" + t.id
		}
		k := key{w: t.w, env: env, rotation: -1}
		if t.verify {
			k.env, k.rotation = t.id, t.data.rotation
		}
		if _, has := groups[k]; !has {
			order = append(order, k)
		}
		groups[k] = append(groups[k], t)
	}
	var out [][]*gameTest
	for _, k := range order {
		group := groups[k]
		for i := 0; len(group) > 0; i++ {
			limit := 50 // vanilla: MAX_TESTS_PER_BATCH
			if group[0].verify {
				limit = 100
			} // vanilla: VERIFY_TEST_BATCH_SIZE
			n := min(limit, len(group))
			for _, t := range group[:n] {
				t.batch = i
				if t.verify {
					t.batch = int(t.data.rotation)
				}
			}
			out = append(out, group[:n])
			group = group[n:]
		}
	}
	return out
}

// prepareGameTestBatch leaves a screen's already prepared test in place.
func (s *Server) prepareGameTestBatch(tests []*gameTest) []*gameTest {
	if tests[0].verify {
		for _, t := range s.gameTests.lastBatch {
			s.gameTestScope(t.w).ClearTest(t.pos[0], t.pos[1], t.pos[2])
		}
		s.gameTests.grids = nil
	}
	ready := make([]*gameTest, 0, len(tests))
	for _, t := range tests {
		if t.grid && !t.prepared {
			t = s.spawnGameTest(t)
		} else if !t.prepared || t.done {
			t = s.rerunGameTest(t)
		}
		ready = append(ready, t)
	}
	s.gameTests.lastBatch = ready
	return ready
}

// sayGameTestBatchStarting announces the environment and partition index.
// vanilla: TestCommand.TestBatchSummaryDisplayer.testBatchStarting
func (s *Server) sayGameTestBatchStarting(t *gameTest) {
	env := t.test.Environment
	if env == "" {
		env = "[unregistered]" // vanilla: Holder.getRegisteredName
	}
	s.gameTestSays(protocol.Text{Translate: "commands.test.batch.starting", With: []protocol.Text{{Text: env}, {Number: new(int64(t.batch))}}}, true)
}

// tickGameTests steps the running tests, reporting each as it ends. Once all
// have, the batch's environment ends and the flaky ones due another attempt
// start again as a new batch in a fresh one, or, with none, the run's
// summary is said.
// vanilla: GameTestTicker.tick, GameTestRunner.runBatch, GameTestRunner.runScheduledRerunTests
func (s *Server) tickGameTests() {
	s.gameTests.mu.Lock()
	run := s.gameTests.run
	s.gameTests.mu.Unlock()
	if run == nil {
		return
	}
	for _, t := range run {
		if t.verify && t.done && t.err != nil {
			s.haltGameTestRun(run, t.w)
			return
		}
		if t.done {
			continue
		}
		s.tickGameTest(t)
		if t.done && t.err != nil {
			s.gameTests.mu.Lock()
			if len(s.gameTests.run) > 0 && s.gameTests.run[0] == run[0] {
				if s.gameTests.failed == nil {
					s.gameTests.failed = map[string]bool{}
				}
				s.gameTests.failed[t.id] = t.test.Required
			}
			s.gameTests.mu.Unlock()
		}
		if t.done && s.reportGameTest(t) {
			s.gameTests.mu.Lock()
			if len(s.gameTests.run) > 0 && s.gameTests.run[0] == run[0] {
				s.gameTests.rerun = append(s.gameTests.rerun, t)
			}
			s.gameTests.mu.Unlock()
		}
		if t.verify && t.done && t.err != nil {
			s.haltGameTestRun(run, t.w)
			return
		}
	}
	for _, t := range run {
		if !t.done {
			return
		}
	}
	last := run[len(run)-1]
	for _, pos := range last.w.ForcedChunks() {
		last.w.SetChunkForced(pos, false)
	}
	// A Run holding start detaches this run and ends its environment itself;
	// otherwise the batch ends on a later tick. The tick loop never blocks.
	if !s.gameTests.start.TryLock() {
		return
	}
	defer s.gameTests.start.Unlock()
	s.gameTests.mu.Lock()
	current := len(s.gameTests.run) > 0 && s.gameTests.run[0] == run[0]
	var teardown func()
	var next []*gameTest
	if current {
		teardown, s.gameTests.teardown = s.gameTests.teardown, nil
		if len(s.gameTests.pending) == 0 && len(s.gameTests.rerun) > 0 {
			s.gameTests.pending = batchGameTests(s.gameTests.rerun)
			s.gameTests.rerun = nil
		}
		if len(s.gameTests.pending) == 0 {
			s.gameTests.run = nil
		} else {
			next, s.gameTests.pending = s.gameTests.pending[0], s.gameTests.pending[1:]
		}
	}
	s.gameTests.mu.Unlock()
	if !current {
		return
	}
	// vanilla: GameTestRunner.runBatch (endCurrentEnvironment)
	if teardown != nil {
		teardown()
	}
	if len(next) == 0 {
		s.sayGameTestSummary(run)
		return
	}
	again := s.prepareGameTestBatch(next)
	teardown = s.setUpTestEnvironment(again[0].w, again[0].test.Env)
	s.sayGameTestBatchStarting(again[0])
	// start is held, so no Run has replaced the run since it was read.
	s.gameTests.mu.Lock()
	s.gameTests.run, s.gameTests.teardown = append(s.gameTests.run, again...), teardown
	s.gameTests.mu.Unlock()
}

// haltGameTestRun stops a failed verification without blocking the tick loop
// on a command that is replacing the run.
// vanilla: GameTestRunner.testFailed (haltOnError)
func (s *Server) haltGameTestRun(run []*gameTest, w *world.World) {
	if !s.gameTests.start.TryLock() {
		return
	}
	defer s.gameTests.start.Unlock()
	s.gameTests.mu.Lock()
	current := len(s.gameTests.run) > 0 && s.gameTests.run[0] == run[0]
	s.gameTests.mu.Unlock()
	if current {
		for _, p := range w.ForcedChunks() {
			w.SetChunkForced(p, false)
		}
		s.stopGameTestsLocked()
	}
}

// rerunGameTest readies another attempt of t in place: the test instance
// block put back with its data cleared, and the structure placed again.
// vanilla: GameTestRunner.rerunTest, GameTestInfo.copyReset, GameTestInfo.createTestInstanceBlock
func (s *Server) rerunGameTest(t *gameTest) *gameTest {
	x, y, z := t.pos[0], t.pos[1], t.pos[2]
	if registry.BlockOf(t.w.BlockAt(x, y, z)) != registry.BlockTestInstanceBlock {
		s.gameTestScope(t.w).SetBlock(x, y, z, registry.BlockTestInstanceBlock.DefaultState())
	}
	again, data := s.prepareGameTest(t.w, x, y, z, testInstanceData{test: t.id, rotation: t.data.rotation}, t.test, t.tries)
	again.retries, again.batch = t.retries, t.batch
	again.grid, again.verify = t.grid, t.verify
	tag, ok := t.w.BlockEntityTag(x, y, z)
	if ok {
		tag = maps.Clone(tag)
	} else {
		tag = nbt.Compound{}
	}
	tag["data"] = data.tag()
	s.storeTestInstanceTag(t.w, x, y, z, tag)
	return again
}

// tickGameTest steps one test: a failure ends it on the tick after, the
// test starts once its setup ticks have passed, and one with no result past
// its max_ticks times out.
// vanilla: GameTestInfo.tick, GameTestInfo.tickInternal, GameTestInfo.startTest
func (s *Server) tickGameTest(t *gameTest) {
	if t.err != nil {
		t.finish()
	}
	if t.tick++; t.tick < 0 {
		return
	}
	if !t.started {
		t.started = true
		t.began = time.Now()
		s.setTestInstanceStatus(t.w, t.pos, 1, nil)
		s.runGameTest(t)
	}
	if t.eachTick && t.tick < t.test.MaxTicks {
		s.checkTestBlocks(t)
	}
	if t.tick > t.test.MaxTicks {
		t.failWith(protocol.Text{Translate: "test.error.timeout.no_result", With: []protocol.Text{{Number: new(int64(t.test.MaxTicks))}}}.Plain())
	}
}

// runGameTest runs the test: a block-based one fires its start block, a
// function test its function. always_pass, the only test function vanilla
// registers outside its own test server, succeeds at once.
// vanilla: FunctionGameTestInstance.run, BuiltinTestFunctions, BlockBasedTestInstance.run
func (s *Server) runGameTest(t *gameTest) {
	if t.test.Function == "" {
		s.startTestBlocks(t)
		return
	}
	if t.test.Function != "minecraft:always_pass" {
		t.failWith("Trying to access missing test function: " + t.test.Function)
		return
	}
	s.succeedGameTest(t)
}

// succeedGameTest ends a test that has not failed, discarding every entity
// but a player within a block of its structure.
// vanilla: GameTestInfo.succeed
func (s *Server) succeedGameTest(t *gameTest) {
	if t.err != nil {
		return
	}
	t.finish()
	_, lo, hi := testStructureBox(t.pos[0], t.pos[1], t.pos[2], t.data, t.test)
	s.gameTestScope(t.w).discardEntitiesIn([6]float64{float64(lo[0] - 1), float64(lo[1] - 1), float64(lo[2] - 1),
		float64(hi[0] + 2), float64(hi[1] + 2), float64(hi[2] + 2)})
}

// testBlocksIn returns the positions of the test's structure's test blocks,
// by mode.
// vanilla: BlockBasedTestInstance.findTestBlocks, GameTestHelper.forEveryBlockInStructure
func testBlocksIn(t *gameTest) map[string][][3]int32 {
	_, lo, hi := testStructureBox(t.pos[0], t.pos[1], t.pos[2], t.data, t.test)
	blocks := map[string][][3]int32{}
	for z := lo[2]; z <= hi[2]; z++ {
		for y := lo[1]; y <= hi[1]; y++ {
			for x := lo[0]; x <= hi[0]; x++ {
				if state := t.w.BlockAt(x, y, z); registry.BlockOf(state) == registry.BlockTestBlock {
					mode := world.TestBlockMode(state)
					blocks[mode] = append(blocks[mode], [3]int32{x, y, z})
				}
			}
		}
	}
	return blocks
}

// testBlockError is the failure key names for a structure missing a test
// block of the mode or holding too many.
func testBlockError(key, mode string) protocol.Text {
	return protocol.Text{Translate: key, With: []protocol.Text{{Translate: "test_block.mode." + mode}}}
}

// startTestBlocks powers the structure's one start block, waking what it
// reaches, and has the test blocks read from this tick on.
// vanilla: BlockBasedTestInstance.run, TestBlockEntity.trigger
func (s *Server) startTestBlocks(t *gameTest) {
	starts := testBlocksIn(t)["start"]
	if len(starts) == 0 {
		t.failAt(testBlockError("test_block.error.missing", "start"))
		return
	}
	if len(starts) != 1 {
		t.failAt(testBlockError("test_block.error.too_many", "start"))
		return
	}
	x, y, z := starts[0][0], starts[0][1], starts[0][2]
	t.w.SetTestBlockPowered(x, y, z, true)
	s.signalNeighborsChanged(t.w, x, y, z)
	message, _ := nbt.Get[nbt.String](t.w.TestBlockAt(x, y, z), "message")
	world.LogTestBlock("start", x, y, z, string(message))
	t.eachTick = true
}

// checkTestBlocks is the block-based test's check each tick: a triggered
// accept block passes it, else a triggered fail block fails it with the
// block's message, and each triggered log block logs again and is reset.
// vanilla: BlockBasedTestInstance.run (onEachTick), forAllTriggeredTestBlocks
func (s *Server) checkTestBlocks(t *gameTest) {
	blocks := testBlocksIn(t)
	p := s.physicsFor(t.w)
	if len(blocks["accept"]) == 0 {
		t.failAt(testBlockError("test_block.error.missing", "accept"))
		return
	}
	for _, pos := range blocks["accept"] {
		if p.TestBlockTriggered(pos[0], pos[1], pos[2]) {
			s.succeedGameTest(t)
			return
		}
	}
	// A failing block is left triggered, as vanilla's throw skips its reset.
	for _, pos := range blocks["fail"] {
		if p.TestBlockTriggered(pos[0], pos[1], pos[2]) {
			message, _ := nbt.Get[nbt.String](t.w.TestBlockAt(pos[0], pos[1], pos[2]), "message")
			t.failAt(protocol.Text{Text: string(message)})
			return
		}
	}
	for _, pos := range blocks["log"] {
		if p.TestBlockTriggered(pos[0], pos[1], pos[2]) {
			message, _ := nbt.Get[nbt.String](t.w.TestBlockAt(pos[0], pos[1], pos[2]), "message")
			world.LogTestBlock("log", pos[0], pos[1], pos[2], string(message))
			p.ResetTestBlocks(pos, pos)
		}
	}
}

// reportGameTest tells everyone how the ended test went and stores it on the
// block; a passed test's barriers come down. A flaky test, one with more than
// one attempt, reports each attempt, and only its last on the block; it
// returns whether the test is to run again.
// vanilla: ReportGameListener.testPassed, ReportGameListener.testFailed, GameTestRunner.runBatch (removeBarriers)
func (s *Server) reportGameTest(t *gameTest) (rerun bool) {
	tries, flaky := t.tries, t.test.MaxAttempts > 1
	if t.retries != nil && t.retries.Tries != 1 && (t.err == nil || !flaky) {
		return s.reportGameTestRetry(t)
	}
	if t.err == nil {
		tries.successes++
		switch {
		case !flaky:
			s.reportGameTestPassed(t, fmt.Sprintf("%s passed! (%dms / %dgameticks)", t.id, t.ran.Milliseconds(), t.tick))
		case tries.successes >= t.test.RequiredSuccesses:
			s.reportGameTestPassed(t, fmt.Sprintf("%s passed %d times of %d attempts.", t.id, tries.successes, tries.attempts))
		default:
			s.gameTestBroadcast(protocol.ColorGreen, fmt.Sprintf("Flaky test %s succeeded, attempt: %d successes: %d", t.id, tries.attempts, tries.successes))
			rerun = true
		}
		s.gameTestScope(t.w).removeTestBarriers(t.w, t.pos[0], t.pos[1], t.pos[2], t.data, t.test)
		return rerun
	}
	if !flaky {
		s.reportGameTestFailed(t, t.err)
		return false
	}
	text := fmt.Sprintf("Flaky test %s failed, attempt: %d/%d", t.id, tries.attempts, t.test.MaxAttempts)
	if t.test.RequiredSuccesses > 1 {
		text += fmt.Sprintf(", successes: %d (%d required)", tries.successes, t.test.RequiredSuccesses)
	}
	s.gameTestBroadcast(protocol.ColorYellow, text)
	if t.test.MaxAttempts-tries.attempts+tries.successes >= t.test.RequiredSuccesses {
		return true
	}
	// The block shows the cause's message (Util.describeError).
	// vanilla: ExhaustedAttemptsException
	s.reportGameTestFailed(t, &gameTestError{description: protocol.Text{Text: t.err.message},
		message: fmt.Sprintf("Not enough successes: %d out of %d attempts. Required successes: %d. max attempts: %d. cause: %s",
			tries.successes, tries.attempts, t.test.RequiredSuccesses, t.test.MaxAttempts, t.err.message)})
	return false
}

// reportGameTestRetry reports command retries independently of flaky successes.
// vanilla: ReportGameListener.handleRetry, RetryOptions.hasTriesLeft
func (s *Server) reportGameTestRetry(t *gameTest) bool {
	tries, options := t.tries, t.retries
	passed, verdict := t.err == nil, "failed"
	if passed {
		tries.successes++
		verdict = "passed"
	} else {
		s.reportGameTestFailed(t, t.err)
	}
	report := fmt.Sprintf("[Run: %4d, Ok: %4d, Fail: %4d", tries.attempts, tries.successes, tries.attempts-tries.successes)
	if options.Tries > 0 {
		report += fmt.Sprintf(", Left: %4d", options.Tries-tries.attempts)
	}
	text := fmt.Sprintf("%-53s%s %s! %dms", report+"]", t.id, verdict, t.ran.Milliseconds())
	if passed {
		s.reportGameTestPassed(t, text)
		s.gameTestScope(t.w).removeTestBarriers(t.w, t.pos[0], t.pos[1], t.pos[2], t.data, t.test)
	} else {
		s.gameTestBroadcast(protocol.ColorRed, text)
	}
	return (options.Tries < 1 || tries.attempts < options.Tries) && (!options.UntilFailed || tries.attempts == tries.successes)
}

// reportGameTestPassed marks the block finished and says text in green.
// vanilla: ReportGameListener.reportPassed
func (s *Server) reportGameTestPassed(t *gameTest, text string) {
	s.setTestInstanceStatus(t.w, t.pos, 2, nil)
	s.gameTestBroadcast(protocol.ColorGreen, text)
}

// reportGameTestFailed stores err on the block and says the failure.
// vanilla: ReportGameListener.reportFailure, ReportGameListener.visualizeFailedTest
func (s *Server) reportGameTestFailed(t *gameTest, err *gameTestError) {
	s.setTestInstanceStatus(t.w, t.pos, 2, &err.description)
	color, prefix := protocol.ColorRed, ""
	if !t.test.Required {
		color, prefix = protocol.ColorYellow, "(optional) "
	}
	s.gameTestBroadcast(color, prefix+t.id+" failed! "+err.message)
}

// sayGameTestSummary is the server's summary of a run once every test in
// it, reruns included, has ended. The test coordinates it would add for a
// run outside its dimension are left out.
// vanilla: TestCommand.TestSummaryDisplayer
func (s *Server) sayGameTestSummary(run []*gameTest) {
	failedRequired, failedOptional := 0, 0
	for _, t := range run {
		if t.err != nil && t.test.Required {
			failedRequired++
		} else if t.err != nil {
			failedOptional++
		}
	}
	s.gameTestSays(protocol.Text{Translate: "commands.test.summary", Color: "white", With: []protocol.Text{{Number: new(int64(len(run)))}}}, true)
	if failedRequired > 0 {
		s.gameTestSays(protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{{Translate: "commands.test.summary.failed",
			With: []protocol.Text{{Number: new(int64(failedRequired))}}}}}, false)
	} else {
		s.gameTestSays(protocol.Text{Translate: "commands.test.summary.all_required_passed", Color: protocol.ColorGreen}, true)
	}
	if failedOptional > 0 {
		s.gameTestSays(protocol.Text{Translate: "commands.test.summary.optional_failed", With: []protocol.Text{{Number: new(int64(failedOptional))}}}, false)
	}
}

// gameTestBroadcast sends every player a test's result line, unlogged.
// vanilla: ReportGameListener.say
func (s *Server) gameTestBroadcast(color, text string) {
	s.broadcast(protocol.IDPlaySystemChat, systemChatPayload(protocol.Text{Text: text, Color: color}, false))
}

// gameTestSays is the runner's server source answering: the console logs the
// line, and a broadcast one goes to the operators too.
// vanilla: CommandSourceStack.sendSuccess, sendFailure (MinecraftServer.createCommandSourceStack)
func (s *Server) gameTestSays(text protocol.Text, broadcast bool) {
	slog.Info(text.Plain(), protocol.LogComponentKey, text)
	if broadcast {
		s.consoleSource().InformAdmins(text)
	}
}

// setTestInstanceStatus stores status and the error message in the block's
// data, when the block is still a test instance block.
// vanilla: TestInstanceBlockEntity.Data.withStatus, Data.withError
func (s *Server) setTestInstanceStatus(w *world.World, pos [3]int32, status int32, errorMessage *protocol.Text) {
	x, y, z := pos[0], pos[1], pos[2]
	if registry.BlockOf(w.BlockAt(x, y, z)) != registry.BlockTestInstanceBlock {
		return
	}
	tag, ok := w.BlockEntityTag(x, y, z)
	if !ok {
		return
	}
	tag = maps.Clone(tag)
	data, ok := nbt.Get[nbt.Compound](tag, "data")
	if !ok {
		data = testInstanceData{}.tag()
	}
	data = maps.Clone(data)
	data["status"] = nbt.String(testInstanceStatuses[status])
	delete(data, "error_message")
	if errorMessage != nil {
		data["error_message"] = protocol.TextTag(*errorMessage)
	}
	tag["data"] = data
	s.storeTestInstanceTag(w, x, y, z, tag)
}

// storeTestInstanceTag writes the block's entity and tells its watchers.
func (s *Server) storeTestInstanceTag(w *world.World, x, y, z int32, tag nbt.Compound) {
	w.SetBlockEntityTag(x, y, z, kindTestInstanceBlock, tag)
	s.markChanged(w, x, y, z)
	s.tellBlockEntity(w, x, y, z, kindTestInstanceBlock, false)
}
