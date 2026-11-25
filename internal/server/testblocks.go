package server

import (
	"errors"
	"log/slog"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// testBlockModes are the modes in wire order.
// vanilla: TestBlockMode
var testBlockModes = [...]string{"start", "log", "fail", "accept"}

// handleSetTestBlock handles Set Test Block: a creative gamemaster sets the
// block's mode and message.
// vanilla: ServerGamePacketListenerImpl.handleSetTestBlock
func (c *client) handleSetTestBlock(r *protocol.Reader) error {
	x, y, z := r.ReadPosition()
	mode := r.ReadVarInt()
	message := r.ReadString(32767)
	if err := r.Err(); err != nil {
		return c.decodeFailure("set_test_block", err)
	}
	// An id outside the mapper reads as the first mode.
	// vanilla: ByIdMap.OutOfBoundsStrategy.ZERO
	if mode < 0 || int(mode) >= len(testBlockModes) {
		mode = 0
	}
	if !c.canUseGameMasterBlocks() {
		return nil
	}
	w := c.theWorld()
	state := w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockTestBlock {
		return nil
	}
	tag, ok := w.BlockEntityTag(x, y, z)
	if !ok {
		tag = nbt.Compound{}
	}
	tag["mode"] = nbt.String(testBlockModes[mode])
	tag["message"] = nbt.String(message)
	if _, has := tag["powered"]; !has {
		tag["powered"] = nbt.Byte(0)
	}
	if next, ok := registry.WithProp(state, registry.PropMode, testBlockModes[mode]); ok && next != state && w.SetBlock(x, y, z, next) {
		c.srv.markChanged(w, x, y, z)
	}
	w.SetBlockEntityTag(x, y, z, world.KindTestBlock, tag)
	c.srv.markChanged(w, x, y, z)
	c.srv.tellCarriedBlockEntity(w, x, y, z, world.KindTestBlock)
	return nil
}

// kindTestInstanceBlock is the test instance block's entity id.
const kindTestInstanceBlock = "minecraft:test_instance_block"

// The test instance block's actions in wire order.
// vanilla: ServerboundTestInstanceBlockActionPacket.Action
const (
	testInstanceBlockInit = iota
	testInstanceBlockQuery
	testInstanceBlockSet
	testInstanceBlockReset
	testInstanceBlockSave
	testInstanceBlockExport
	testInstanceBlockRun
)

// readTestInstanceData reads the data, failing on a test id that is no
// identifier or an error message the component codec refuses.
// vanilla: TestInstanceBlockEntity.Data.STREAM_CODEC
func readTestInstanceData(r *protocol.Reader) (world.TestInstanceData, error) {
	var d world.TestInstanceData
	if r.ReadBool() {
		id := r.ReadString(32767)
		if r.Err() == nil && !protocol.ValidIdentifier(id) {
			return d, errors.New("test instance data carries an invalid test id")
		}
		d.Test = registry.Qualify(id)
	}
	for i := range d.Size {
		d.Size[i] = r.ReadVarInt()
	}
	// vanilla: Rotation.BY_ID wraps, Status.ID_MAP falls back to the first
	d.Rotation = protocol.IDWrap(r.ReadVarInt(), 4)
	d.IgnoreEntities = r.ReadBool()
	d.Status = r.ReadVarInt()
	if r.ReadBool() {
		text, ok := protocol.ReadText(r)
		if !ok && r.Err() == nil {
			return d, errors.New("test instance data carries an unreadable error message")
		}
		d.ErrorMessage = &text
	}
	return d, r.Err()
}

// alwaysPassTest is the core pack's one test instance, which vanilla builds in
// code rather than reading from a file.
// vanilla: GameTestInstances.bootstrap
var alwaysPassTest = datapack.TestInstance{
	Type: "minecraft:function", Function: "minecraft:always_pass", Environment: "minecraft:default",
	Dimension: "minecraft:overworld", Structure: "minecraft:empty", MaxTicks: 1, SetupTicks: 1,
	MaxAttempts: 1, RequiredSuccesses: 1,
}

// registeredTest returns the test instance registered as id. An entry the
// codec refuses counts as missing: vanilla would not have started.
func registeredTest(id string) (datapack.TestInstance, bool) {
	if id == "" {
		return datapack.TestInstance{}, false
	}
	for _, reg := range registry.Current() {
		if reg.Name != "minecraft:test_instance" {
			continue
		}
		if tag, ok := reg.Data[id]; ok {
			test, err := datapack.DecodeTestInstance(tag, testEnvironments())
			return test, err == nil
		}
		if id != "minecraft:always_pass" || reg.ID(id) < 0 {
			return datapack.TestInstance{}, false
		}
		test := alwaysPassTest
		env, err := datapack.DecodeTestEnvironment(nbt.String(test.Environment), testEnvironments())
		test.Env = env
		return test, err == nil
	}
	return datapack.TestInstance{}, false
}

// describeTest is the test's description on the block's screen: one blue
// valued row per line.
// vanilla: GameTestInstance.describe, FunctionGameTestInstance.describe
func describeTest(test datapack.TestInstance) protocol.Text {
	row := func(key string, value protocol.Text) protocol.Text {
		value.Color = "blue"
		return protocol.Text{Translate: key, With: []protocol.Text{value}, Extra: []protocol.Text{{Text: "\n"}}}
	}
	env := test.Environment
	if env == "" {
		env = "[unregistered]" // vanilla: Holder.getRegisteredName
	}
	info := row("test_instance.description.structure", protocol.Text{Text: test.Structure})
	info.Extra = append(info.Extra,
		row("test_instance.description.batch", protocol.Text{Text: env}),
		row("test_instance.description.dimension", protocol.Text{Text: test.Dimension}))
	kind := "test_instance.type.block_based"
	if test.Function != "" {
		kind = "test_instance.type.function"
	}
	out := row("test_instance.description.type", protocol.Text{Translate: kind})
	if test.Function != "" {
		out.Extra = append(out.Extra, row("test_instance.description.function", protocol.Text{Text: test.Function}))
	}
	out.Extra = append(out.Extra, info)
	return out
}

// handleTestInstanceBlockAction handles the test instance block's screen:
// Query and Init describe the block's test, every other action first stores
// the sent data. Run starts the test in place.
// vanilla: ServerGamePacketListenerImpl.handleTestInstanceBlockAction
func (c *client) handleTestInstanceBlockAction(r *protocol.Reader) error {
	x, y, z := r.ReadPosition()
	action := r.ReadVarInt()
	data, err := readTestInstanceData(r)
	if err != nil {
		return c.decodeFailure("test_instance_block_action", err)
	}
	// vanilla: ByIdMap.OutOfBoundsStrategy.ZERO
	if action < 0 || action > testInstanceBlockRun {
		action = testInstanceBlockInit
	}
	w := c.theWorld()
	if !c.canUseGameMasterBlocks() || registry.BlockOf(w.BlockAt(x, y, z)) != registry.BlockTestInstanceBlock {
		return nil
	}
	test, registered := registeredTest(data.Test)
	if action == testInstanceBlockInit || action == testInstanceBlockQuery {
		var b protocol.Buffer
		if registered {
			b.PutText(describeTest(test))
		} else {
			b.PutText(protocol.Text{Translate: "test_instance.description.no_test", Color: "red"})
		}
		// vanilla: TestInstanceBlockEntity.getStructureSize
		var t *world.Template
		found := false
		if registered && action == testInstanceBlockQuery {
			t, found = c.srv.structures.Get(test.Structure)
		}
		b.PutBool(found)
		if found {
			b.PutVarInt(t.Size[0])
			b.PutVarInt(t.Size[1])
			b.PutVarInt(t.Size[2])
		}
		return c.writePacket(protocol.IDPlayTestInstanceBlockStatus, "test instance block status", b.Bytes())
	}
	pos := []protocol.Text{{Number: new(int64(x))}, {Number: new(int64(y))}, {Number: new(int64(z))}}
	// clearErrors drops the error markers a finished test left.
	// vanilla: TestInstanceBlockEntity.clearErrorMarkers
	clearErrors := false
	var started *gameTest
	switch action {
	case testInstanceBlockReset:
		c.removeTestBarriers(w, x, y, z, data, test)
		clearErrors = true
		if registered && c.placeTestStructure(w, x, y, z, data, test) {
			_ = c.Message(protocol.Text{Translate: "test_instance_block.reset_success", Color: "green",
				With: []protocol.Text{{Text: data.Test}}})
		}
		// vanilla: TestInstanceBlockEntity.Data.withStatus
		data.Status, data.ErrorMessage = 0, nil
	case testInstanceBlockSave, testInstanceBlockExport:
		// Outside the game test server there is nowhere to export to.
		// vanilla: TestInstanceBlockEntity.export
		if c.saveTestInstance(w, x, y, z, data, test, registered) && action == testInstanceBlockExport {
			_ = c.Message(protocol.Text{Text: "Test structure exporting is disabled", Color: "red"})
		}
	case testInstanceBlockRun:
		// vanilla: TestInstanceBlockEntity.runTest
		switch {
		case !registered:
			_ = c.Message(protocol.Text{Translate: "test_instance_block.error.no_test", Color: "red", With: pos})
		case !c.placeTestStructure(w, x, y, z, data, test):
			_ = c.Message(protocol.Text{Translate: "test_instance_block.error.no_test_structure", Color: "red", With: pos})
		default:
			clearErrors = true
			_ = c.Message(protocol.Text{Translate: "test_instance_block.starting", With: []protocol.Text{{Text: data.Test}}})
			started, data = c.srv.prepareGameTest(w, x, y, z, data, test, nil)
		}
	}
	tag, ok := w.BlockEntityTag(x, y, z)
	if !ok {
		tag = nbt.Compound{}
	}
	tag["data"] = data.Tag()
	if clearErrors {
		delete(tag, "errors")
	}
	c.srv.storeTestInstanceTag(w, x, y, z, tag)
	if started != nil {
		c.srv.startGameTests(started)
	}
	return nil
}

// testStructurePos is the structure's north-west corner: one up and one
// south of the block, then the padding on every axis.
// vanilla: TestInstanceBlockEntity.getStructurePos, STRUCTURE_OFFSET
func testStructurePos(x, y, z int32, padding int32) [3]int32 {
	return [3]int32{x + padding, y + 1 + padding, z + 1 + padding}
}

// saveTestInstance saves the structure box as the template the test names,
// or as the test id for an unregistered test, reporting false when the block
// has no test.
// vanilla: TestInstanceBlockEntity.saveTest
func (c *client) saveTestInstance(w *world.World, x, y, z int32, data world.TestInstanceData, test datapack.TestInstance, registered bool) bool {
	id := data.Test
	if registered {
		id = test.Structure
	}
	if id == "" {
		_ = c.Message(protocol.Text{Translate: "test_instance_block.error.unable_to_save", Color: "red", With: []protocol.Text{
			{Number: new(int64(x))}, {Number: new(int64(y))}, {Number: new(int64(z))},
		}})
		return false
	}
	world.SaveTemplate(w, c.srv.structures, id, testStructurePos(x, y, z, test.Padding), data.Size, data.IgnoreEntities, "", true,
		func(b registry.Block) bool { return b == registry.BlockAir || b == registry.BlockStructureVoid }, c.srv.entitiesToSave(w))
	return true
}

// testStructureBox is the rotated structure's rotation and its lowest and
// highest cells, without the padding.
// vanilla: TestInstanceBlockEntity.getStructureBoundingBox, Rotation.getRotated
func testStructureBox(x, y, z int32, data world.TestInstanceData, test datapack.TestInstance) (rot world.Rotation, lo, hi [3]int32) {
	rot = world.Rotation((test.Rotation + data.Rotation) % 4)
	size := data.Size
	if rot == world.RotateClockwise90 || rot == world.RotateCounterclockwise90 {
		size[0], size[2] = size[2], size[0]
	}
	corner := testStructurePos(x, y, z, test.Padding)
	for i := range corner {
		far := corner[i] + size[i] - 1
		lo[i], hi[i] = min(corner[i], far), max(corner[i], far)
	}
	return rot, lo, hi
}

// forTestBoundary calls f for every cell of the barrier shell one block
// around the structure box: its floor, its walls and, unless the test has
// sky access, its ceiling.
// vanilla: TestInstanceBlockEntity.processStructureBoundary
func forTestBoundary(x, y, z int32, data world.TestInstanceData, test datapack.TestInstance, f func(x, y, z int32)) {
	_, lo, hi := testStructureBox(x, y, z, data, test)
	for i := range lo {
		lo[i], hi[i] = lo[i]-1, hi[i]+1
	}
	for cy := lo[1]; cy <= hi[1]; cy++ {
		for cx := lo[0]; cx <= hi[0]; cx++ {
			for cz := lo[2]; cz <= hi[2]; cz++ {
				wall := cx == lo[0] || cx == hi[0] || cz == lo[2] || cz == hi[2] || cy == lo[1]
				if wall || cy == hi[1] && !test.SkyAccess {
					f(cx, cy, cz)
				}
			}
		}
	}
}

// removeTestBarriers turns the barriers in the test's barrier shell to air.
// An unregistered test reads as padding 0 without sky access.
// vanilla: TestInstanceBlockEntity.removeBarriers
func (ws worldScope) removeTestBarriers(w *world.World, x, y, z int32, data world.TestInstanceData, test datapack.TestInstance) {
	air := registry.BlockAir.DefaultState()
	var cells []world.BlockChange
	forTestBoundary(x, y, z, data, test, func(cx, cy, cz int32) {
		if registry.BlockOf(w.BlockAt(cx, cy, cz)) == registry.BlockBarrier {
			cells = append(cells, world.BlockChange{X: cx, Y: cy, Z: cz, State: air})
		}
	})
	if len(cells) > 0 {
		ws.FillCells(cells, nil, nil, nil, false, false, true)
	}
}

// discardEntitiesIn discards every entity but a player whose box overlaps
// box in the scope's dimension; a mob goes without dying.
// vanilla: Entity.discard
func (ws worldScope) discardEntitiesIn(box [6]float64) {
	s := ws.srv
	for _, e := range ws.Entities(command.EntityQuery{Dimension: ws.in().name, Box: &box}) {
		snap, ok := e.(*entitySnapshot)
		if !ok {
			continue // a player
		}
		if ent, ok := snap.ref.(*mobEntity); ok {
			s.mobsMu.Lock()
			ent.mob.Discard()
			s.mobsMu.Unlock()
			continue
		}
		snap.ops.kill(s, snap.ref)
	}
	s.flushDestroyed()
}

// placeTestStructure clears the registered test's padded box and places its
// structure there, reporting false when its template is missing.
// vanilla: TestInstanceBlockEntity.placeStructure, StructureUtils.clearSpaceForStructure
func (ws worldScope) placeTestStructure(w *world.World, x, y, z int32, data world.TestInstanceData, test datapack.TestInstance) bool {
	t, ok := ws.srv.structures.Get(test.Structure)
	if !ok {
		return false
	}
	rot, lo, hi := testStructureBox(x, y, z, data, test)
	corner := testStructurePos(x, y, z, test.Padding)
	for cx := lo[0] >> 4; cx <= hi[0]>>4; cx++ {
		for cz := lo[2] >> 4; cz <= hi[2]>>4; cz++ {
			ws.SetChunkForced(cx, cz, true)
		}
	}
	// Both clearSpaceForStructure and removeEntities discard what touches the
	// padded box, so once covers them.
	p := test.Padding
	ws.clearTestSpace(w, [3]int32{lo[0] - p, lo[1] - p, lo[2] - p}, [3]int32{hi[0] + p, hi[1] + p, hi[2] + p})
	// The start corner is where the rotated template's origin lands.
	// vanilla: TestInstanceBlockEntity.getStartCorner
	origin := corner
	switch rot {
	case world.RotateClockwise90:
		origin[0] += data.Size[2] - 1
	case world.Rotate180:
		origin[0] += data.Size[0] - 1
		origin[2] += data.Size[2] - 1
	case world.RotateCounterclockwise90:
		origin[2] += data.Size[0] - 1
	}
	placed, ok := t.Place(world.TemplatePlacement{Origin: origin, Rotation: rot, Integrity: 1, IgnoreEntities: data.IgnoreEntities})
	if ok {
		ws.PlaceTemplate(placed, true)
		command.PlaceTemplateEntities(ws, placed.Entities, slog.Debug)
	}
	return true
}

// clearTestSpace clears the box to air, drops its block ticks and block
// events, and discards every entity but a player touching it. Its floor is
// at or above the ground row StructureUtils.clearBlock fills with stone.
// vanilla: StructureUtils.clearSpaceForStructure
func (ws worldScope) clearTestSpace(w *world.World, lo, hi [3]int32) {
	minY, maxY := w.MinY(), w.MinY()+w.Height()-1
	air := registry.BlockAir.DefaultState()
	var clear []world.BlockChange
	for cy := max(lo[1], minY); cy <= min(hi[1], maxY); cy++ {
		for cx := lo[0]; cx <= hi[0]; cx++ {
			for cz := lo[2]; cz <= hi[2]; cz++ {
				clear = append(clear, world.BlockChange{X: cx, Y: cy, Z: cz, State: air})
			}
		}
	}
	ws.FillCells(clear, nil, nil, nil, false, false, true)
	ws.in().physics.ClearTicksIn(lo[0], lo[1], lo[2], hi[0], hi[1], hi[2])
	ws.in().physics.ResetTestBlocks(lo, hi)
	ws.discardEntitiesIn([6]float64{float64(lo[0]), float64(lo[1]), float64(lo[2]),
		float64(hi[0] + 1), float64(hi[1] + 1), float64(hi[2] + 1)})
}
