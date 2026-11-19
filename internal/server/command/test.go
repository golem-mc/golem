package command

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"
)

// Test manages test instance blocks.
var Test = Command{
	Name:   "test",
	Level:  PermLevelGamemasters,
	Build:  testNode,
	Run:    discardResult(runTest),
	Result: runTest,
}

// TestInstanceBlock is a test instance block a /test search found.
type TestInstanceBlock struct {
	Dimension string
	X, Y, Z   int32
	// Test is the block's qualified test id, "" for none.
	Test string
	// Structure is empty when the test is not registered.
	Structure string
	// Rotation is the test's rotation turned by the block's, in clockwise
	// quarter turns. vanilla: TestInstanceBlockEntity.getRotation
	Rotation int32
	// StructurePos is the structure's north-west corner; Lo and Hi are its
	// box's lowest and highest cells.
	StructurePos, Lo, Hi [3]int32
}

const (
	testInstanceRegistry = "minecraft:test_instance"

	// vanilla: TestCommand.TEST_NEARBY_SEARCH_RADIUS, TEST_FULL_SEARCH_RADIUS
	testNearbyRadius = 15
	testFullRadius   = 250
	// vanilla: TestCommand.createNewStructure, StructureUtils.MAX_STRUCTURE_SIZE
	testMaxSize = 48
	// vanilla: TestCommand.DEFAULT_X_SIZE (and Y, Z)
	testDefaultSize = 5
	// vanilla: TestCommand.TEST_POS_Z_OFFSET_FROM_PLAYER
	testZOffset = 3
	// testPickReach is how far /test pos looks, in blocks. vanilla: TestCommand.showPos
	testPickReach = 10.0

	resourceSelectorNotFoundKey = "argument.resource_selector.not_found"
)

// testNode builds the /test tree.
// vanilla: TestCommand.register
func testNode() *Node {
	integer := func(name string, executable bool, children ...*Node) *Node {
		return &Node{Type: NodeArgument, Name: name, Parser: ParserInteger, Executable: executable, Children: children}
	}
	create := &Node{Type: NodeLiteral, Name: "create", Children: []*Node{{
		Type: NodeArgument, Name: "id", Parser: ParserResourceLoc, Executable: true,
		Children: []*Node{integer("width", true, integer("height", false, integer("depth", true)))},
	}}}
	pos := &Node{Type: NodeLiteral, Name: "pos", Executable: true, Children: []*Node{{
		Type: NodeArgument, Name: "var", Parser: ParserString, Executable: true,
		Properties: func(b *protocol.Buffer) { b.PutVarInt(0) }, // single word
	}}}
	locate := &Node{Type: NodeLiteral, Name: "locate", Children: []*Node{{
		Type: NodeArgument, Name: "tests", Parser: ParserResourceSelector, Executable: true,
		Properties: func(b *protocol.Buffer) { b.PutString(testInstanceRegistry) },
		Check:      checkTestSelector, Suggests: AskServerSuggestion, Completions: testCompletions,
	}}}
	retryNode := func(n *Node, build bool) *Node {
		untilFailed := &Node{Type: NodeArgument, Name: "untilFailed", Parser: ParserBool, Executable: true}
		if build {
			untilFailed.Children = []*Node{integer("rotationSteps", true, integer("testsPerRow", true))}
		}
		number := integer("numberOfTimes", true, untilFailed)
		number.Int = &IntArg{Min: 0, HasMin: true}
		n.Executable = true
		n.Children = append(n.Children, number)
		return n
	}
	selector := func() *Node {
		return &Node{Type: NodeArgument, Name: "tests", Parser: ParserResourceSelector, Executable: true,
			Properties: func(b *protocol.Buffer) { b.PutString(testInstanceRegistry) },
			Check:      checkTestSelector, Suggests: AskServerSuggestion, Completions: testCompletions}
	}
	multiple := selector()
	multiple.Children = []*Node{integer("amount", true)}
	children := []*Node{
		{Type: NodeLiteral, Name: "run", Children: []*Node{retryNode(selector(), true)}},
		{Type: NodeLiteral, Name: "runmultiple", Children: []*Node{multiple}},
	}
	for _, name := range []string{"runthese", "runclosest", "runthat"} {
		children = append(children, retryNode(&Node{Type: NodeLiteral, Name: name}, false))
	}
	failed := retryNode(&Node{Type: NodeLiteral, Name: "runfailed", Children: []*Node{
		retryNode(&Node{Type: NodeArgument, Name: "onlyRequiredTests", Parser: ParserBool}, true),
	}}, true)
	children = append(children, failed,
		&Node{Type: NodeLiteral, Name: "verify", Children: []*Node{selector()}}, locate)
	for _, name := range []string{"resetclosest", "resetthese", "resetthat", "clearthat", "clearthese", "clearall", "stop"} {
		n := &Node{Type: NodeLiteral, Name: name, Executable: true}
		if name == "clearall" {
			n.Children = []*Node{integer("radius", true)}
		}
		children = append(children, n)
	}
	children = append(children, pos, create)
	return &Node{Type: NodeLiteral, Name: "test", Required: PermLevelGamemasters, Children: children}
}

// testInstanceIDs lists the registered test instances.
func testInstanceIDs() []string {
	for _, reg := range registry.Current() {
		if reg.Name == testInstanceRegistry {
			return reg.Entries
		}
	}
	return nil
}

// testCompletions offers the test ids, the minecraft namespace dropped.
// vanilla: TestCommand.suggestTestInstanceIds, displayTestId
func testCompletions(_ any, partial string) (int, []string) {
	matches := ResourceSuggestions(strings.ToLower(partial), testInstanceIDs()...)
	for i, id := range matches {
		matches[i] = strings.TrimPrefix(id, "minecraft:")
	}
	return 0, matches
}

// readResourcePattern reads a selector's pattern, namespaced, and its length
// in the word.
// vanilla: ResourceSelectorArgument.readPattern, ensureNamespaced
func readResourcePattern(word string) (string, int) {
	n := 0
	for n < len(word) && (isCharValid(word[n]) || word[n] == '*' || word[n] == '?') {
		n++
	}
	pattern := word[:n]
	if !strings.Contains(pattern, ":") {
		pattern = "minecraft:" + pattern
	}
	return pattern, n
}

// selectTests is the registered tests the pattern matches.
// vanilla: ResourceSelectorArgument.parse
func selectTests(pattern string) []string {
	var out []string
	for _, id := range testInstanceIDs() {
		if wildcardMatch(id, pattern) {
			out = append(out, id)
		}
	}
	return out
}

// checkTestSelector refuses a selector no registered test matches, or one
// with more than the pattern in the word.
func checkTestSelector(word string) *ParseError {
	pattern, n := readResourcePattern(word)
	if len(selectTests(pattern)) == 0 {
		return &ParseError{Key: resourceSelectorNotFoundKey, With: []string{pattern, testInstanceRegistry}, Cursor: n}
	}
	if n < len(word) {
		return &ParseError{Key: keyExpectedSeparator, Cursor: n}
	}
	return nil
}

// wildcardMatch matches name against a pattern where * is any run of
// characters and ? any one, case-sensitively.
// vanilla: FilenameUtils.wildcardMatch
func wildcardMatch(name, pattern string) bool {
	star, mark := -1, 0
	i, j := 0, 0
	for i < len(name) {
		switch {
		case j < len(pattern) && (pattern[j] == '?' || pattern[j] == name[i]):
			i++
			j++
		case j < len(pattern) && pattern[j] == '*':
			star, mark = j, i
			j++
		case star >= 0:
			mark++
			i, j = mark, star+1
		default:
			return false
		}
	}
	for j < len(pattern) && pattern[j] == '*' {
		j++
	}
	return j == len(pattern)
}

// runTest dispatches /test to its subcommand.
func runTest(src Source, _ string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete test command")
		return 0, nil
	}
	switch args[0] {
	case "runclosest", "runthese", "runthat", "run", "runmultiple", "runfailed", "verify":
		return testRunSelection(src, args)
	case "stop":
		src.StopTests()
		return 1, nil
	case "resetclosest", "resetthese", "resetthat", "clearthat", "clearthese", "clearall":
		return testResetOrClear(src, args)
	case "create":
		return testCreate(src, args[1:])
	case "pos":
		name := "pos"
		if len(args) > 1 {
			name = args[1]
		}
		return testPos(src, name)
	case "locate":
		if len(args) < 2 {
			src.Debug("incomplete test locate")
			return 0, nil
		}
		pattern, _ := readResourcePattern(args[1])
		return testLocate(src, selectTests(pattern))
	}
	src.Debug("unknown test subcommand", "sub", args[0])
	return 0, nil
}

// testBlocks selects by source position; a looked-at box ignores intervening
// blocks and is ordered by its block's distance, not the ray's entry point.
// vanilla: TestFinder.Builder, StructureUtils.findNearestTest, lookedAtTestPos
func testBlocks(src Source, mode string, radius int) ([]TestInstanceBlock, error) {
	p := src.Position()
	x, y, z := floorInt32(p.X), floorInt32(p.Y), floorInt32(p.Z)
	if mode == "resetclosest" || mode == "runclosest" {
		radius = testNearbyRadius
	}
	blocks := src.TestInstanceBlocks(x, y, z, radius, false)
	switch mode {
	case "resetclosest", "runclosest":
		abs := func(n int32) int32 { return max(n, -n) }
		distance := func(b TestInstanceBlock) int32 { return abs(b.X-x) + abs(b.Y-y) + abs(b.Z-z) }
		slices.SortStableFunc(blocks, func(a, b TestInstanceBlock) int { return int(distance(a) - distance(b)) })
		blocks = blocks[:min(1, len(blocks))]
	case "resetthat", "clearthat", "runthat":
		self, ok := src.SelfEntity().(Target)
		if !ok {
			return nil, refuse(src, "permissions.requires.player")
		}
		eye := self.EyePosition()
		look := self.Position()
		dx, dy, dz := entity.LookVector(look.Yaw, look.Pitch)
		blocks = slices.DeleteFunc(blocks, func(b TestInstanceBlock) bool {
			return !testBoxInLook(b, [3]float64{eye.X, eye.Y, eye.Z}, [3]float64{dx * testFullRadius, dy * testFullRadius, dz * testFullRadius})
		})
		distance := func(b TestInstanceBlock) int64 {
			dx, dy, dz := int64(b.X-x), int64(b.Y-y), int64(b.Z-z)
			return dx*dx + dy*dy + dz*dz
		}
		slices.SortStableFunc(blocks, func(a, b TestInstanceBlock) int { return int(distance(a) - distance(b)) })
		blocks = blocks[:min(1, len(blocks))]
	}
	return blocks, nil
}

// TestRetries are /test's command retries, independent of a test's flakiness.
// vanilla: RetryOptions
type TestRetries struct {
	Tries       int32
	UntilFailed bool
}

// TestRun describes in-place blocks or registered tests to spawn in a grid.
// vanilla: TestFinder, TestCommand.run, verify
type TestRun struct {
	Blocks                   []TestInstanceBlock
	Tests                    []string
	Retries                  TestRetries
	Rotation, Copies, PerRow int32
	Origin                   [3]int32
	Verify                   bool
}

// testRunSelection builds the selection and options the runner consumes.
// vanilla: TestCommand.run, verify, createGameTestInfo
func testRunSelection(src Source, args []string) (int32, error) {
	src.StopTests()
	request := TestRun{Retries: TestRetries{Tries: 1, UntilFailed: true}, Copies: 1, PerRow: 8, Verify: args[0] == "verify"}
	if request.Verify {
		request.PerRow = 10
	} // vanilla: VERIFY_TEST_GRID_AXIS_SIZE
	words := args[1:]
	switch args[0] {
	case "run", "runmultiple", "verify":
		pattern, _ := readResourcePattern(words[0])
		request.Tests, words = selectTests(pattern), words[1:]
	case "runfailed":
		onlyRequired := false
		if len(words) > 0 && (words[0] == "true" || words[0] == "false") {
			onlyRequired, words = words[0] == "true", words[1:]
		}
		request.Tests = src.FailedTests(onlyRequired)
	default:
		blocks, err := testBlocks(src, args[0], testFullRadius)
		if err != nil {
			return 0, err
		}
		for _, b := range blocks {
			ok, err := testReady(src, b)
			if err != nil {
				return 0, err
			}
			if ok {
				request.Blocks = append(request.Blocks, b)
			}
		}
	}
	if args[0] == "runmultiple" && len(words) > 0 {
		n, err := strconv.ParseInt(words[0], 10, 32)
		if err != nil {
			return 0, err
		}
		request.Copies, words = int32(max(0, n)), nil
	}
	if len(words) > 0 {
		n, err := strconv.ParseInt(words[0], 10, 32)
		if err != nil {
			return 0, err
		}
		request.Retries = TestRetries{Tries: int32(n)}
	}
	if len(words) > 1 {
		request.Retries.UntilFailed = words[1] == "true"
	}
	for i, dst := range []*int32{&request.Rotation, &request.PerRow} {
		if len(words) > i+2 {
			n, err := strconv.ParseInt(words[i+2], 10, 32)
			if err != nil {
				return 0, err
			}
			*dst = int32(n)
		}
	}
	if request.Rotation < 0 || request.Rotation > 3 {
		return 0, fmt.Errorf("rotationSteps must be a value from 0-3. Got value %d", request.Rotation)
	}
	var selected []string
	for _, id := range request.Tests {
		test, ok := src.TestInstance(id)
		if !ok {
			continue
		}
		if _, ok := src.LoadTemplate(test.Structure); !ok {
			if err := src.ErrorWith("commands.test.error.structure_not_found", protocol.Text{Text: test.Structure}); err != nil {
				return 0, err
			}
			continue
		}
		if _, ok := src.InContext(Context{HasDim: true, Dim: test.Dimension}); !ok {
			if err := src.Message(protocol.Text{Text: "Could not resolve level for dimension: " + test.Dimension, Color: protocol.ColorRed}); err != nil {
				return 0, err
			}
			continue
		}
		selected = append(selected, id)
	}
	request.Tests = selected
	request.Origin[0], request.Origin[1], request.Origin[2] = testPlayerPos(src)
	count := int64(len(request.Blocks)) + int64(len(selected))*int64(request.Copies)
	if count == 0 && !request.Verify {
		return resultOf(0, src.Feedback("commands.test.no_tests"))
	}
	if !request.Verify {
		if err := src.Feedback("commands.test.run.running", numberText(count)); err != nil {
			return 0, err
		}
	}
	src.RunTests(request)
	return 1, nil
}

// testReady reports a block's invalid test or missing template without throwing.
// vanilla: TestCommand.createGameTestInfo, verifyStructureExists
func testReady(src Source, b TestInstanceBlock) (bool, error) {
	if b.Structure == "" {
		name := protocol.Text{Text: b.Test}
		if b.Test == "" {
			name = protocol.Text{Translate: "test_instance_block.invalid_test"}
		}
		return false, src.ErrorWith("commands.test.error.non_existant_test", name)
	}
	if _, ok := src.LoadTemplate(b.Structure); !ok {
		return false, src.ErrorWith("commands.test.error.structure_not_found", protocol.Text{Text: b.Structure})
	}
	return true, nil
}

// testBoxInLook requires entry through a face strictly between the ray's ends;
// looking from inside the box does not select it.
// vanilla: AABB.clip, getDirection, clipPoint
func testBoxInLook(b TestInstanceBlock, from, delta [3]float64) bool {
	const epsilon = 1e-7 // vanilla: AABB.EPSILON
	for axis, d := range delta {
		if math.Abs(d) <= epsilon {
			continue
		}
		face := float64(b.Lo[axis])
		if d < 0 {
			face = float64(b.Hi[axis] + 1)
		}
		t := (face - from[axis]) / d
		if t <= 0 || t >= 1 {
			continue
		}
		j, k := (axis+1)%3, (axis+2)%3
		pj, pk := from[j]+t*delta[j], from[k]+t*delta[k]
		if float64(b.Lo[j])-epsilon < pj && pj < float64(b.Hi[j]+1)+epsilon &&
			float64(b.Lo[k])-epsilon < pk && pk < float64(b.Hi[k]+1)+epsilon {
			return true
		}
	}
	return false
}

// testResetOrClear stops the runner before searching, including when no test
// matches. Reset deliberately uses the clear error for an empty selection.
// vanilla: TestCommand.reset, clear
func testResetOrClear(src Source, args []string) (int32, error) {
	src.StopTests()
	radius := testFullRadius
	if args[0] == "clearall" && len(args) > 1 {
		n, err := strconv.ParseInt(args[1], 10, 32)
		if err != nil {
			return 0, err
		}
		radius = int(max(0, min(1024, n))) // vanilla: TestCommand.MAX_CLEAR_RADIUS
	}
	blocks, err := testBlocks(src, args[0], radius)
	if err != nil {
		return 0, err
	}
	reset := strings.HasPrefix(args[0], "reset")
	var count int32
	for _, b := range blocks {
		if reset {
			ok, err := testReady(src, b)
			if err != nil {
				return 0, err
			}
			if !ok {
				continue
			}
			if src.ResetTest(b.X, b.Y, b.Z) {
				if err := src.Message(protocol.Text{Translate: "test_instance_block.reset_success", Color: protocol.ColorGreen,
					With: []protocol.Text{{Text: b.Test}}}); err != nil {
					return 0, err
				}
			}
		} else {
			src.ClearTest(b.X, b.Y, b.Z)
		}
		count++
	}
	if count == 0 {
		return 0, refuse(src, "commands.test.clear.error.no_tests")
	}
	key := "commands.test.clear.success"
	if reset {
		key = "commands.test.reset.success"
	}
	return resultOf(count, feedbackToAdmins(src, key, numberText(int64(count))))
}

// testPlayerPos is the block the player stands in, or the source's.
// vanilla: TestCommand.playerAndTestInfo
func testPlayerPos(src Source) (x, y, z int32) {
	pos := src.Position()
	if self, ok := src.SelfEntity().(Target); ok {
		pos = self.Position()
	}
	return floorInt32(pos.X), floorInt32(pos.Y), floorInt32(pos.Z)
}

// testCreate runs /test create <id> [width [height depth]].
// vanilla: TestCommand.createNewStructure, createTestPositionAround
func testCreate(src Source, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete test create")
		return 0, nil
	}
	id, ok := parseResourceLocation(args[0])
	if !ok {
		src.Debug("test id is not a resource location", "id", args[0])
		return 0, nil
	}
	size := [3]int32{testDefaultSize, testDefaultSize, testDefaultSize}
	for i, word := range args[1:min(len(args), 4)] {
		n, err := strconv.ParseInt(word, 10, 32)
		if err != nil {
			src.Debug("bad test size", "size", word)
			return 0, nil
		}
		size[i] = int32(n)
	}
	if len(args) == 2 { // width alone sizes every axis
		size = [3]int32{size[0], size[0], size[0]}
	}
	if size[0] > testMaxSize || size[1] > testMaxSize || size[2] > testMaxSize {
		return 0, refuse(src, "commands.test.error.too_large", numberText(int64(testMaxSize)))
	}
	px, _, pz := testPlayerPos(src)
	release := src.HoldChunk(px, pz)
	y := src.HeightmapHeight("world_surface", px, pz)
	release()
	src.CreateTest(id, px, y, pz+testZOffset, size)
	return resultOf(1, feedbackToAdmins(src, "commands.test.create.success", protocol.Text{Text: id}))
}

// testContaining is the first test instance block within radius of the
// point whose structure box holds it.
// vanilla: StructureUtils.findTestContainingPos
func testContaining(src Source, x, y, z int32, radius int) (TestInstanceBlock, bool) {
	for _, b := range src.TestInstanceBlocks(x, y, z, radius, false) {
		if x >= b.Lo[0] && x <= b.Hi[0] && y >= b.Lo[1] && y <= b.Hi[1] && z >= b.Lo[2] && z <= b.Hi[2] {
			return b, true
		}
	}
	return TestInstanceBlock{}, false
}

// testPos runs /test pos [var]: the looked-at block's position in the test
// that holds it, with a line of Java to copy.
// vanilla: TestCommand.showPos
func testPos(src Source, varName string) (int32, error) {
	self, ok := src.SelfEntity().(Target)
	if !ok {
		return 0, refuse(src, "permissions.requires.player")
	}
	x, y, z := self.PickBlock(testPickReach)
	b, found := testContaining(src, x, y, z, testNearbyRadius)
	if !found {
		b, found = testContaining(src, x, y, z, testFullRadius)
	}
	if !found {
		return 0, refuse(src, "commands.test.error.no_test_containing_pos", numberText(int64(x)), numberText(int64(y)), numberText(int64(z)))
	}
	rel := [3]int32{x - b.StructurePos[0], y - b.StructurePos[1], z - b.StructurePos[2]}
	coords := protocol.Text{
		Translate: "commands.test.coordinates",
		With:      []protocol.Text{numberText(int64(rel[0])), numberText(int64(rel[1])), numberText(int64(rel[2]))},
		Bold:      true, Color: protocol.ColorGreen,
		HoverText: &protocol.Text{Translate: "commands.test.coordinates.copy"},
		ClickCopy: fmt.Sprintf("final BlockPos %s = new BlockPos(%d, %d, %d);", varName, rel[0], rel[1], rel[2]),
	}
	// vanilla sends the name flattened; the key renders the same.
	name := protocol.Text{Text: b.Test}
	if b.Test == "" {
		name = protocol.Text{Translate: "test_instance_block.invalid_test"} // vanilla: TestInstanceBlockEntity.INVALID_TEST_NAME
	}
	if err := src.Feedback("commands.test.relative_position", name, coords); err != nil {
		return 0, err
	}
	self.HighlightTestPos([3]int32{x, y, z}, rel)
	return 1, nil
}

// dimensionPath is a dimension id's path, its namespace dropped.
func dimensionPath(dim string) string { return dim[strings.IndexByte(dim, ':')+1:] }

// testLocate runs /test locate <tests>: every block of the selected tests
// within reach in any dimension, with a click to go there.
// vanilla: TestCommand.locate
func testLocate(src Source, selected []string) (int32, error) {
	if err := src.Message(protocol.Text{Translate: "commands.test.locate.started"}); err != nil {
		return 0, err
	}
	px, py, pz := floorInt32(src.Position().X), floorInt32(src.Position().Y), floorInt32(src.Position().Z)
	here := src.DimensionName()
	var found int32
	dims := map[string]bool{}
	for _, b := range src.TestInstanceBlocks(px, py, pz, testFullRadius, true) {
		if b.Test == "" || !slices.Contains(selected, b.Test) {
			continue
		}
		dims[b.Dimension] = true
		found++
		// The click stands 3 blocks before the test's front, facing it.
		// vanilla: Rotation.rotate(Direction.NORTH), Direction.toYRot
		step := [4][2]int32{{0, -3}, {3, 0}, {0, 3}, {-3, 0}}[b.Rotation&3]
		tp := fmt.Sprintf("/execute in %s run tp @s %d %d %d %d 0", b.Dimension, b.X+step[0], b.Y, b.Z+step[1], (b.Rotation&3)*90)
		distance := "N/A"
		if b.Dimension == here {
			dx, dz := px-b.X, pz-b.Z
			distance = strconv.Itoa(int(math.Floor(float64(float32(math.Sqrt(float64(float32(dx*dx + dz*dz))))))))
		}
		coordinates := protocol.Text{
			Translate: "chat.square_brackets",
			With: []protocol.Text{{
				Translate: "chat.coordinates", With: []protocol.Text{numberText(int64(b.X)), numberText(int64(b.Y)), numberText(int64(b.Z))},
				Extra: []protocol.Text{{Text: " ->[" + dimensionPath(b.Dimension) + "]"}},
			}},
			Color: protocol.ColorGreen, ClickCommand: tp,
			HoverText: &protocol.Text{Translate: "chat.coordinates.tooltip"},
		}
		if err := src.Feedback("commands.test.locate.found", coordinates, protocol.Text{Text: distance}); err != nil {
			return 0, err
		}
	}
	if found == 0 {
		return 0, refuse(src, "commands.test.error.no_test_instances")
	}
	if err := testPlayerCoordinates(src, dims); err != nil {
		return 0, err
	}
	return resultOf(found, feedbackToAdmins(src, "commands.test.locate.done", numberText(int64(found))))
}

// testPlayerCoordinates sends a click back to where the player stands,
// unless the player and every test found are in the overworld.
// vanilla: TestCommand.outputPlayerCoordinates, isPlayerInCurrentTestDimension
func testPlayerCoordinates(src Source, dims map[string]bool) error {
	self, ok := src.SelfEntity().(Target)
	if !ok {
		return nil
	}
	dim := self.DimensionName()
	if len(dims) <= 1 && dims[dim] && dims["minecraft:overworld"] {
		return nil
	}
	pos := self.Position()
	x, y, z := floorInt32(pos.X), floorInt32(pos.Y), floorInt32(pos.Z)
	return src.FeedbackText(protocol.Text{
		Translate: "chat.square_brackets",
		With: []protocol.Text{{Translate: "test.player.coordinates",
			With: []protocol.Text{numberText(int64(x)), numberText(int64(y)), numberText(int64(z)), {Text: dimensionPath(dim)}}}},
		Color: protocol.ColorGold,
		ClickCommand: fmt.Sprintf("/execute in %s run tp @s %d %d %d %d %d", dim, x, y, z,
			floorInt32(float64(pos.Yaw)), floorInt32(float64(pos.Pitch))),
		HoverText: &protocol.Text{Translate: "chat.coordinates.tooltip"},
	})
}
