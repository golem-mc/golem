package server

import (
	"maps"
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// testInstanceDataOf reads the saved settings, leaving status and errors cleared.
// vanilla: TestInstanceBlockEntity.Data.CODEC
func testInstanceDataOf(tag nbt.Compound) world.TestInstanceData {
	data, _ := world.ReadTestInstanceData(tag["data"])
	data.Status, data.ErrorMessage = 0, nil
	return data
}

// TestInstanceBlocks lists the test instance blocks within radius blocks
// (by squared distance) of the point, in chunks in memory or saved.
// vanilla: StructureUtils.findTestBlocks, PoiManager.findAll
func (ws worldScope) TestInstanceBlocks(x, y, z int32, radius int, everyDimension bool) []command.TestInstanceBlock {
	dims := []*dimension{ws.in()}
	if everyDimension {
		dims = ws.srv.allDims()
	}
	r := int32(radius)
	limit := int64(radius) * int64(radius)
	var out []command.TestInstanceBlock
	for _, dim := range dims {
		w := dim.w
		for cx := (x - r) >> 4; cx <= (x+r)>>4; cx++ {
			for cz := (z - r) >> 4; cz <= (z+r)>>4; cz++ {
				pos := world.ChunkPos{X: cx, Z: cz}
				if !w.HasChunk(pos) {
					continue
				}
				if chunk, _ := w.Acquire(pos); chunk == nil {
					continue
				}
				colLo := [3]int32{cx << 4, w.MinY(), cz << 4}
				colHi := [3]int32{cx<<4 + 15, w.MinY() + w.Height() - 1, cz<<4 + 15}
				for _, c := range w.CarriedOfKind(kindTestInstanceBlock, colLo, colHi) {
					p := c.Pos
					dx, dy, dz := int64(p[0]-x), int64(p[1]-y), int64(p[2]-z)
					if dx*dx+dy*dy+dz*dz > limit || registry.BlockOf(w.BlockAt(p[0], p[1], p[2])) != registry.BlockTestInstanceBlock {
						continue
					}
					data := testInstanceDataOf(c.Tag)
					test, registered := registeredTest(data.Test)
					structure := ""
					if registered {
						structure = test.Structure
					}
					rot, lo, hi := testStructureBox(p[0], p[1], p[2], data, test)
					out = append(out, command.TestInstanceBlock{
						Dimension: dim.name, X: p[0], Y: p[1], Z: p[2], Test: data.Test, Structure: structure, Rotation: int32(rot),
						StructurePos: testStructurePos(p[0], p[1], p[2], test.Padding), Lo: lo, Hi: hi,
					})
				}
				w.Release(pos)
			}
		}
	}
	return out
}

// ResetTest restores the saved test settings after placing its template.
// vanilla: TestInstanceBlockEntity.resetTest
func (ws worldScope) ResetTest(x, y, z int32) bool {
	w := ws.in().w
	tag, ok := w.BlockEntityTag(x, y, z)
	if !ok {
		return false
	}
	data := testInstanceDataOf(tag)
	test, registered := registeredTest(data.Test)
	ws.removeTestBarriers(w, x, y, z, data, test)
	placed := registered && ws.placeTestStructure(w, x, y, z, data, test)
	tag = maps.Clone(tag)
	delete(tag, "errors")
	tag["data"] = data.Tag()
	ws.srv.storeTestInstanceTag(w, x, y, z, tag)
	return placed
}

// ClearTest removes the padded structure, its barrier shell and its block.
// vanilla: TestCommand.clear
func (ws worldScope) ClearTest(x, y, z int32) {
	w := ws.in().w
	tag, _ := w.BlockEntityTag(x, y, z)
	data := testInstanceDataOf(tag)
	test, _ := registeredTest(data.Test)
	_, lo, hi := testStructureBox(x, y, z, data, test)
	for i := range lo {
		lo[i] -= test.Padding
		hi[i] += test.Padding
	}
	ws.clearTestSpace(w, lo, hi)
	ws.removeTestBarriers(w, x, y, z, data, test)
	ws.SetBlock(x, y, z, registry.BlockAir.DefaultState())
}

// StopTests ends a detached run's environment under the same gate as Run.
// vanilla: GameTestTicker.clear
func (ws worldScope) StopTests() {
	ticker := &ws.srv.gameTests
	ticker.start.Lock()
	defer ticker.start.Unlock()
	ws.srv.stopGameTestsLocked()
}

// stopGameTestsLocked requires start, so environment changes cannot interleave.
func (s *Server) stopGameTestsLocked() {
	ticker := &s.gameTests
	ticker.mu.Lock()
	teardown := ticker.teardown
	ticker.run, ticker.rerun, ticker.teardown = nil, nil, nil
	ticker.pending = nil
	ticker.mu.Unlock()
	ticker.grids, ticker.lastBatch = nil, nil
	if teardown != nil {
		teardown()
	}
}

// RunTests retains the block's combined rotation as the extra rotation.
// vanilla: TestCommand.createGameTestInfo
func (ws worldScope) RunTests(request command.TestRun) {
	var tests []*gameTest
	for _, b := range request.Blocks {
		test, ok := registeredTest(b.Test)
		if !ok {
			continue
		}
		tests = append(tests, &gameTest{w: ws.in().w, pos: [3]int32{b.X, b.Y, b.Z}, id: b.Test, test: test,
			data: world.TestInstanceData{Test: b.Test, Rotation: b.Rotation}, retries: &request.Retries})
	}
	for _, id := range request.Tests {
		test, ok := registeredTest(id)
		dim := ws.srv.findDimension(test.Dimension)
		if !ok || dim == nil {
			continue
		}
		rotations, copies := int32(1), request.Copies
		if request.Verify {
			rotations, copies = 4, 100
		} // vanilla: TestCommand.verify
		for rotation := range rotations {
			for range copies {
				tests = append(tests, &gameTest{w: dim.w, id: id, test: test, grid: true, verify: request.Verify,
					data: world.TestInstanceData{Test: id, Rotation: (request.Rotation + rotation) & 3}, retries: &request.Retries})
			}
		}
	}
	ws.srv.gameTests.mu.Lock()
	ws.srv.gameTests.failed = nil
	ws.srv.gameTests.mu.Unlock()
	if len(tests) != 0 {
		ws.srv.startGameTestRun(tests, request)
	}
}

// TestInstance resolves a registered test.
func (ws worldScope) TestInstance(id string) (datapack.TestInstance, bool) { return registeredTest(id) }

// FailedTests snapshots the previous run's failed holders.
// vanilla: FailedTestTracker.getLastFailedTests
func (ws worldScope) FailedTests(onlyRequired bool) []string {
	ticker := &ws.srv.gameTests
	ticker.mu.Lock()
	defer ticker.mu.Unlock()
	var ids []string
	for id, required := range ticker.failed {
		if !onlyRequired || required {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// CreateTest clears the structure box, puts a test instance block for id at
// the point and floors the structure with bedrock at the test's padding.
// vanilla: StructureUtils.createNewEmptyTest, TestCommand.createNewStructure
func (ws worldScope) CreateTest(id string, x, y, z int32, size [3]int32) {
	w := ws.in().w
	data := world.TestInstanceData{Test: id, Size: size}
	box := testStructurePos(x, y, z, 0)
	ws.clearTestSpace(w, box, [3]int32{box[0] + size[0] - 1, box[1] + size[1] - 1, box[2] + size[2] - 1})
	ws.SetBlock(x, y, z, registry.BlockTestInstanceBlock.DefaultState())
	ws.srv.storeTestInstanceTag(w, x, y, z, nbt.Compound{"data": data.Tag()})
	test, _ := registeredTest(id)
	low := testStructurePos(x, y, z, test.Padding)
	bedrock := registry.BlockBedrock.DefaultState()
	var floor []world.BlockChange
	for cx := low[0]; cx < low[0]+size[0]; cx++ {
		for cz := low[2]; cz < low[2]+size[2]; cz++ {
			floor = append(floor, world.BlockChange{X: cx, Y: low[1], Z: cz, State: bedrock})
		}
	}
	ws.FillCells(floor, nil, nil, nil, false, false, true)
}

// lookClip is the player's look ray from the eyes over reach blocks and the
// first block outline it meets.
// vanilla: Entity.pick, ClipContext.Block.OUTLINE
func (c *client) lookClip(reach float64) (hit world.RayHit, ok bool, end [3]float64) {
	pos := c.player.Position()
	lx, ly, lz := entity.LookVector(pos.Yaw, pos.Pitch)
	eyeY := pos.Y + c.dims(c.player.Appearance().Pose).EyeHeight
	hit, ok = c.theWorld().ClipBlocks(pos.X, eyeY, pos.Z, lx*reach, ly*reach, lz*reach, nil)
	return hit, ok, [3]float64{pos.X + lx*reach, eyeY + ly*reach, pos.Z + lz*reach}
}

// PickBlock is the block the look meets within reach, or on a miss the one
// holding the ray's end.
// vanilla: Entity.pick, BlockHitResult.miss
func (c *client) PickBlock(reach float64) (x, y, z int32) {
	hit, ok, end := c.lookClip(reach)
	if ok {
		return hit.X, hit.Y, hit.Z
	}
	return int32(math.Floor(end[0])), int32(math.Floor(end[1])), int32(math.Floor(end[2]))
}

// HighlightTestPos sends Game Test Highlight Pos.
// vanilla: ClientboundGameTestHighlightPosPacket
func (c *client) HighlightTestPos(absolute, relative [3]int32) {
	var b protocol.Buffer
	b.PutPosition(absolute[0], absolute[1], absolute[2])
	b.PutPosition(relative[0], relative[1], relative[2])
	c.send(protocol.IDPlayGameTestHighlightPos, b.Bytes())
}
