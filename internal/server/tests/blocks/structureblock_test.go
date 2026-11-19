package blocks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// structureScene is a creative operator and a world directory the tests read
// the saved templates from.
func structureScene(t *testing.T) (*server.Server, *servertest.Client, string) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Actor", servertest.OpsListing(t, "Actor"))
	c.Settle()
	return srv, c, cfg.LevelName
}

// buildShape lays three marker blocks in the 3x3x3 box at (11,64,10).
func buildShape(t *testing.T, c *servertest.Client) {
	t.Helper()
	servertest.Setblock(t, c, 11, 64, 10, "minecraft:stone")
	servertest.Setblock(t, c, 12, 65, 11, "minecraft:gold_block")
	servertest.Setblock(t, c, 13, 66, 12, "minecraft:oak_planks")
}

// saveShape saves the box at (11,64,10) as test:house from a save block at
// (10,64,10), the offset being one block east.
func saveShape(t *testing.T, c *servertest.Client) {
	t.Helper()
	servertest.Setblock(t, c, 10, 64, 10, "minecraft:structure_block[mode=save]")
	c.ProgramStructureBlock(servertest.StructureBlockPacket{
		X: 10, Y: 64, Z: 10, Action: servertest.StructureActionSave, Mode: servertest.StructureModeSave,
		Name: "test:house", Offset: [3]int8{1, 0, 0}, Size: [3]int8{3, 3, 3}, Integrity: 1,
		IgnoreEntities: true, ShowBox: true,
	})
	servertest.AwaitCommandFeedback(t, c, "structure_block.save_success", "test:house")
}

func stateName(state uint16) string {
	name, _, _ := registry.BlockStateName(state)
	return name
}

// TestAStructureBlockSavesATemplateTheReaderLoadsBack: the Save button writes
// the box the block's offset and size name under generated/ as a template
// file, air included, and the world's template reader reads it unchanged.
// vanilla: StructureBlockEntity.saveStructure
func TestAStructureBlockSavesATemplateTheReaderLoadsBack(t *testing.T) {
	t.Parallel()
	_, c, dir := structureScene(t)
	buildShape(t, c)
	saveShape(t, c)

	data, err := os.ReadFile(filepath.Join(dir, "generated", "test", "structure", "house.nbt"))
	if err != nil {
		t.Fatalf("no template file: %v", err)
	}
	tpl, err := world.ParseTemplate(data)
	if err != nil {
		t.Fatal(err)
	}
	if tpl.Size != [3]int32{3, 3, 3} {
		t.Fatalf("template size = %v, want 3x3x3", tpl.Size)
	}
	at := map[[3]int32]string{}
	for _, b := range tpl.Palettes[0] {
		at[b.Pos] = stateName(b.State)
	}
	if len(at) != 27 {
		t.Errorf("template holds %d cells, want 27 (air is saved)", len(at))
	}
	for pos, want := range map[[3]int32]string{{0, 0, 0}: "minecraft:stone", {1, 1, 1}: "minecraft:gold_block", {2, 2, 2}: "minecraft:oak_planks", {2, 0, 0}: "minecraft:air"} {
		if at[pos] != want {
			t.Errorf("template cell %v = %q, want %q", pos, at[pos], want)
		}
	}
}

// TestAPoweredLoadStructureBlockPlacesTheTemplate: a load block powered by a
// lever copies the saved shape, turned by its rotation about the block's
// offset, once per rise.
// vanilla: StructureBlock.neighborChanged, StructureBlockEntity.placeStructure
func TestAPoweredLoadStructureBlockPlacesTheTemplate(t *testing.T) {
	t.Parallel()
	srv, c, _ := structureScene(t)
	buildShape(t, c)
	saveShape(t, c)

	servertest.Setblock(t, c, 30, 64, 10, "minecraft:structure_block[mode=load]")
	c.ProgramStructureBlock(servertest.StructureBlockPacket{
		X: 30, Y: 64, Z: 10, Action: servertest.StructureActionUpdate, Mode: servertest.StructureModeLoad,
		Name: "test:house", Offset: [3]int8{1, 0, 0}, Size: [3]int8{3, 3, 3}, Rotation: 1, Integrity: 1,
		IgnoreEntities: true, ShowBox: true,
	})
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 31, 64, 10, "minecraft:air", nil)

	servertest.Setblock(t, c, 30, 65, 10, "minecraft:lever[face=floor,facing=north]")
	c.MoveToGround(28.5, 64, 8.5, true)
	c.Settle()
	c.UseBlock(30, 65, 10)
	servertest.RunTicks(t, srv, 4)

	// Clockwise 90 sends (x,z) to (-z,x) around the origin (31,64,10).
	servertest.ExpectBlockNamed(t, srv, 31, 64, 10, "minecraft:stone", nil)
	servertest.ExpectBlockNamed(t, srv, 30, 65, 11, "minecraft:gold_block", nil)
	servertest.ExpectBlockNamed(t, srv, 29, 66, 12, "minecraft:oak_planks", nil)

	// The rise placed once: a block broken after it stays gone while the
	// lever holds.
	servertest.Setblock(t, c, 31, 64, 10, "minecraft:air")
	servertest.RunTicks(t, srv, 4)
	servertest.ExpectBlockNamed(t, srv, 31, 64, 10, "minecraft:air", nil)
}

// TestAStructureBlockPacketNeedsACreativeOperator: a player who is no
// operator, and an operator in survival, change nothing.
// vanilla: ServerGamePacketListenerImpl.handleSetStructureBlock
func TestAStructureBlockPacketNeedsACreativeOperator(t *testing.T) {
	t.Parallel()
	packet := servertest.StructureBlockPacket{
		X: 10, Y: 64, Z: 10, Action: servertest.StructureActionUpdate, Mode: servertest.StructureModeSave,
		Name: "test:house", Size: [3]int8{3, 3, 3}, Integrity: 1,
	}
	srv, guest := servertest.Join(t, servertest.BlockTestConfig(), "Guest")
	guest.Settle()
	state, _ := registry.FindState("minecraft:structure_block", map[string]string{"mode": "load"})
	srv.SetBlock(10, 64, 10, state)
	guest.ProgramStructureBlock(packet)
	guest.Settle()
	if state := srv.BlockAt(10, 64, 10); stateName(state) != "minecraft:structure_block" || state != mustState(t, "load") {
		t.Errorf("a non-operator's packet changed the block to %s", stateName(state))
	}
	if tag, ok := srv.BlockEntityTagAt(10, 64, 10); ok {
		if name, _ := nbt.Get[nbt.String](tag, "name"); name != "" {
			t.Errorf("a non-operator's packet named the structure %q", name)
		}
	}

	srv2, actor, _ := structureScene(t)
	servertest.Setblock(t, actor, 10, 64, 10, "minecraft:structure_block[mode=load]")
	actor.ChatCommand("gamemode survival")
	actor.Settle()
	actor.ProgramStructureBlock(packet)
	actor.Settle()
	if state := srv2.BlockAt(10, 64, 10); state != mustState(t, "load") {
		t.Errorf("a survival operator's packet changed the block to %s", stateName(state))
	}
}

func mustState(t *testing.T, mode string) uint16 {
	t.Helper()
	state, ok := registry.FindState("minecraft:structure_block", map[string]string{"mode": mode})
	if !ok {
		t.Fatalf("no structure block state for mode %s", mode)
	}
	return state
}

// TestDetectSizeTakesTheCornersOfTheSameName: the Detect button sets the save
// block's offset and size from the corner blocks sharing its name and
// ignores corners of another name.
// vanilla: StructureBlockEntity.detectSize
func TestDetectSizeTakesTheCornersOfTheSameName(t *testing.T) {
	t.Parallel()
	srv, c, _ := structureScene(t)
	corner := func(x, y, z int32, name string) {
		servertest.Setblock(t, c, x, y, z, "minecraft:structure_block[mode=corner]")
		c.ProgramStructureBlock(servertest.StructureBlockPacket{
			X: x, Y: y, Z: z, Action: servertest.StructureActionUpdate, Mode: servertest.StructureModeCorner,
			Name: name, Offset: [3]int8{0, 1, 0}, Integrity: 1,
		})
		c.Settle()
	}
	scan := func() {
		c.ProgramStructureBlock(servertest.StructureBlockPacket{
			X: 8, Y: 64, Z: 8, Action: servertest.StructureActionScan, Mode: servertest.StructureModeSave,
			Name: "test:box", Offset: [3]int8{0, 1, 0}, Integrity: 1,
		})
	}
	servertest.Setblock(t, c, 8, 64, 8, "minecraft:structure_block[mode=save]")
	corner(10, 64, 10, "test:box")
	scan()
	servertest.AwaitCommandFeedback(t, c, "structure_block.size_failure")

	corner(14, 67, 13, "test:box")
	corner(40, 70, 40, "test:other")
	scan()
	servertest.AwaitCommandFeedback(t, c, "structure_block.size_success", "test:box")
	c.Settle()
	tag, _ := srv.BlockEntityTagAt(8, 64, 8)
	got := map[string]int32{}
	for _, k := range []string{"posX", "posY", "posZ", "sizeX", "sizeY", "sizeZ"} {
		got[k], _ = nbt.IntValue(tag[k])
	}
	want := map[string]int32{"posX": 3, "posY": 1, "posZ": 3, "sizeX": 3, "sizeY": 2, "sizeZ": 2}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %d, want %d", k, got[k], v)
		}
	}
}

func loadPacket(x int32, action int32, name string, size [3]int8) servertest.StructureBlockPacket {
	return servertest.StructureBlockPacket{
		X: x, Y: 64, Z: 10, Action: action, Mode: servertest.StructureModeLoad,
		Name: name, Offset: [3]int8{1, 0, 0}, Size: size, Integrity: 1, IgnoreEntities: true, ShowBox: true,
	}
}

// TestIntegrityDropsBlocksFromASeededLoad: integrity 0 places nothing, and
// the same seed at half integrity places the same blocks twice and another
// seed places different ones.
// vanilla: StructureBlockEntity.placeStructure, BlockRotProcessor
func TestIntegrityDropsBlocksFromASeededLoad(t *testing.T) {
	t.Parallel()
	srv, c, _ := structureScene(t)
	buildShape(t, c)
	saveShape(t, c)
	load := func(x int32, integrity float32, seed int64) {
		servertest.Setblock(t, c, x, 64, 10, "minecraft:structure_block[mode=load]")
		p := loadPacket(x, servertest.StructureActionLoad, "test:house", [3]int8{3, 3, 3})
		p.Integrity, p.Seed = integrity, seed
		c.ProgramStructureBlock(p)
		servertest.AwaitCommandFeedback(t, c, "structure_block.load_success", "test:house")
		c.Settle()
	}
	load(30, 0, 5)
	servertest.ExpectBlockNamed(t, srv, 31, 64, 10, "minecraft:air", nil)
	servertest.ExpectBlockNamed(t, srv, 32, 65, 11, "minecraft:air", nil)
	pattern := func(x int32) (out [3]bool) {
		for i, p := range [][3]int32{{1, 0, 0}, {2, 1, 1}, {3, 2, 2}} {
			out[i] = stateName(srv.BlockAt(x+p[0], 64+p[1], 10+p[2])) != "minecraft:air"
		}
		return out
	}
	load(45, 0.5, 7)
	load(55, 0.5, 7)
	if pattern(45) != pattern(55) {
		t.Errorf("the same seed placed %v and %v", pattern(45), pattern(55))
	}
	load(65, 0.5, 9)
	if pattern(65) == pattern(45) {
		t.Errorf("seeds 7 and 9 placed the same blocks %v", pattern(45))
	}
}

// powerAbove puts a lever above the structure block at (x, 64, 10) and flips it.
func powerAbove(t *testing.T, srv *server.Server, c *servertest.Client, x int32) {
	t.Helper()
	servertest.Setblock(t, c, x, 65, 10, "minecraft:lever[face=floor,facing=north]")
	c.MoveToGround(float64(x)-1.5, 64, 8.5, true)
	c.Settle()
	c.UseBlock(x, 65, 10)
	servertest.RunTicks(t, srv, 4)
}

// TestAPoweredSaveBlockCapturesIntoTheCacheOnly: the rise saves the box for
// the next load but writes no file.
// vanilla: StructureBlock.trigger
func TestAPoweredSaveBlockCapturesIntoTheCacheOnly(t *testing.T) {
	t.Parallel()
	srv, c, dir := structureScene(t)
	buildShape(t, c)
	servertest.Setblock(t, c, 20, 64, 10, "minecraft:structure_block[mode=save]")
	c.ProgramStructureBlock(servertest.StructureBlockPacket{
		X: 20, Y: 64, Z: 10, Action: servertest.StructureActionUpdate, Mode: servertest.StructureModeSave,
		Name: "test:house", Offset: [3]int8{-9, 0, 0}, Size: [3]int8{3, 3, 3}, Integrity: 1, IgnoreEntities: true,
	})
	c.Settle()
	powerAbove(t, srv, c, 20)
	if _, err := os.Stat(filepath.Join(dir, "generated", "test", "structure", "house.nbt")); err == nil {
		t.Error("a power rise wrote the file")
	}
	servertest.Setblock(t, c, 30, 64, 10, "minecraft:structure_block[mode=load]")
	c.ProgramStructureBlock(loadPacket(30, servertest.StructureActionLoad, "test:house", [3]int8{3, 3, 3}))
	servertest.AwaitCommandFeedback(t, c, "structure_block.load_success", "test:house")
	servertest.ExpectBlockNamed(t, srv, 31, 64, 10, "minecraft:stone", nil)
}
