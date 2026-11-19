package commands

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func ints(v ...int32) nbt.List {
	l := nbt.List{Elem: nbt.TypeInt}
	for _, n := range v {
		l.Items = append(l.Items, nbt.Int(n))
	}
	return l
}

func paletteEntry(name string, props map[string]string) nbt.Compound {
	e := nbt.Compound{"Name": nbt.String(name)}
	if len(props) > 0 {
		p := nbt.Compound{}
		for k, v := range props {
			p[k] = nbt.String(v)
		}
		e["Properties"] = p
	}
	return e
}

func templateBlock(state int32, x, y, z int32, tag nbt.Compound) nbt.Compound {
	b := nbt.Compound{"pos": ints(x, y, z), "state": nbt.Int(state)}
	if tag != nil {
		b["nbt"] = tag
	}
	return b
}

// smallTemplate is a 3x1x2 structure: stone, gold and diamond blocks, a stair
// facing east and a chest holding an item.
func smallTemplate() nbt.Compound {
	palette := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		paletteEntry("minecraft:stone", nil),
		paletteEntry("minecraft:gold_block", nil),
		paletteEntry("minecraft:oak_stairs", map[string]string{"facing": "east"}),
		paletteEntry("minecraft:diamond_block", nil),
		paletteEntry("minecraft:chest", map[string]string{"facing": "north"}),
	}}
	chest := nbt.Compound{"Items": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
		"Slot": nbt.Byte(3), "id": nbt.String("minecraft:apple"), "count": nbt.Int(5),
	}}}}
	blocks := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		templateBlock(0, 0, 0, 0, nil),
		templateBlock(1, 1, 0, 0, nil),
		templateBlock(2, 2, 0, 0, nil),
		templateBlock(3, 0, 0, 1, nil),
		templateBlock(4, 2, 0, 1, chest),
	}}
	return nbt.Compound{
		"size": ints(3, 1, 2), "palette": palette, "blocks": blocks, "DataVersion": nbt.Int(4440),
	}
}

// startPlaceWorld starts a world whose generated folder holds the small
// template as test:small and joins an operator in creative mode.
func startPlaceWorld(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	return startPlaceWorldWith(t, map[string]nbt.Compound{"small": smallTemplate()})
}

// startPlaceWorldWith is startPlaceWorld for the given templates, each saved
// as test:<name>.
func startPlaceWorldWith(t *testing.T, templates map[string]nbt.Compound) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	cfg.LevelName = t.TempDir()
	dir := filepath.Join(cfg.LevelName, "generated", "test", "structure")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, tpl := range templates {
		raw, err := nbt.Encode("", tpl)
		if err != nil {
			t.Fatal(err)
		}
		var zipped bytes.Buffer
		zw := gzip.NewWriter(&zipped)
		if _, err := zw.Write(raw); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".nbt"), zipped.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	return srv, c
}

func expectPlaced(t *testing.T, c *servertest.Client, key, id string, x, y, z int64) {
	t.Helper()
	servertest.ExpectFeedbackSoon(t, c, nil, key,
		protocol.Text{Text: id}, protocol.Text{Number: &x}, protocol.Text{Number: &y}, protocol.Text{Number: &z})
}

func wantBlock(t *testing.T, srv *server.Server, x, y, z int32, name string, props map[string]string) {
	t.Helper()
	servertest.ExpectBlockNamed(t, srv, x, y, z, name, props)
}

// TestPlaceTemplatePlacesTheBlocks checks the plain /place template: every
// block lands at its offset from the position, and the chest keeps its item.
func TestPlaceTemplatePlacesTheBlocks(t *testing.T) {
	t.Parallel()
	srv, c := startPlaceWorld(t)
	c.ChatCommand("place template test:small 10 70 10")
	expectPlaced(t, c, "commands.place.template.success", "test:small", 10, 70, 10)
	wantBlock(t, srv, 10, 70, 10, "minecraft:stone", nil)
	wantBlock(t, srv, 11, 70, 10, "minecraft:gold_block", nil)
	wantBlock(t, srv, 12, 70, 10, "minecraft:oak_stairs", map[string]string{"facing": "east"})
	wantBlock(t, srv, 10, 70, 11, "minecraft:diamond_block", nil)
	wantBlock(t, srv, 12, 70, 11, "minecraft:chest", map[string]string{"facing": "north"})
	tag, ok := srv.BlockEntityTagAt(12, 70, 11)
	if !ok {
		t.Fatal("the chest has no block entity")
	}
	items, _ := nbt.Get[nbt.List](tag, "Items")
	if len(items.Items) != 1 {
		t.Fatalf("chest Items = %v, want the template's one stack", tag["Items"])
	}
	stack, _ := items.Items[0].(nbt.Compound)
	if id, _ := nbt.Get[nbt.String](stack, "id"); id != "minecraft:apple" {
		t.Errorf("chest item = %v, want minecraft:apple", stack)
	}
	c.Disconnect()
}

// TestPlaceTemplateRotatesAndMirrors checks that clockwise_90 turns the
// layout and the stair and the chest, and left_right flips Z, leaving a stair
// facing along X as it is.
func TestPlaceTemplateRotatesAndMirrors(t *testing.T) {
	t.Parallel()
	srv, c := startPlaceWorld(t)
	c.ChatCommand("place template test:small 20 70 20 clockwise_90")
	expectPlaced(t, c, "commands.place.template.success", "test:small", 20, 70, 20)
	// (x, z) -> (-z, x)
	wantBlock(t, srv, 20, 70, 20, "minecraft:stone", nil)
	wantBlock(t, srv, 20, 70, 21, "minecraft:gold_block", nil)
	wantBlock(t, srv, 20, 70, 22, "minecraft:oak_stairs", map[string]string{"facing": "south"})
	wantBlock(t, srv, 19, 70, 20, "minecraft:diamond_block", nil)
	wantBlock(t, srv, 19, 70, 22, "minecraft:chest", map[string]string{"facing": "east"})

	c.ChatCommand("place template test:small 40 70 40 none left_right")
	expectPlaced(t, c, "commands.place.template.success", "test:small", 40, 70, 40)
	// z -> -z
	wantBlock(t, srv, 40, 70, 40, "minecraft:stone", nil)
	wantBlock(t, srv, 40, 70, 39, "minecraft:diamond_block", nil)
	wantBlock(t, srv, 42, 70, 40, "minecraft:oak_stairs", map[string]string{"facing": "east"})
	wantBlock(t, srv, 42, 70, 39, "minecraft:chest", map[string]string{"facing": "south"})
	c.Disconnect()
}

// TestPlaceTemplateIntegrityDropsReproducibleBlocks checks that integrity
// 0.3 with a seed drops the same blocks every time, and that some stay.
func TestPlaceTemplateIntegrityDropsReproducibleBlocks(t *testing.T) {
	t.Parallel()
	srv, c := startPlaceWorld(t)
	cells := [][3]int32{{0, 0, 0}, {1, 0, 0}, {2, 0, 0}, {0, 0, 1}, {2, 0, 1}}
	snapshot := func(ox int32) (got [5]bool) {
		air, _ := registry.FindState("minecraft:air", nil)
		for i, d := range cells {
			got[i] = srv.BlockAt(ox+d[0], 70, 60+d[2]) != air
		}
		return got
	}
	c.ChatCommand("place template test:small 10 70 60 none none 0.3 1")
	expectPlaced(t, c, "commands.place.template.success", "test:small", 10, 70, 60)
	first := snapshot(10)
	c.ChatCommand("place template test:small 30 70 60 none none 0.3 1")
	expectPlaced(t, c, "commands.place.template.success", "test:small", 30, 70, 60)
	if second := snapshot(30); second != first {
		t.Errorf("second placement kept %v, first kept %v", second, first)
	}
	// Seed 1 draws 0.7308782 for the palette roll (nextInt(1) still consumes
	// a draw), then 0.10047, 0.41008, 0.40744, 0.20771, 0.03624
	// (java.util.Random) for stone, diamond, gold, the stairs and the chest
	// in placement order; a block stays when its draw is at most the
	// integrity.
	if want := [5]bool{true, false, true, false, true}; first != want {
		t.Errorf("integrity 0.3 seed 1 kept %v, want %v", first, want)
	}
	c.Disconnect()
}

// TestPlaceTemplateRefusals checks the missing template and the unloaded
// position.
func TestPlaceTemplateRefusals(t *testing.T) {
	t.Parallel()
	_, c := startPlaceWorld(t)
	c.ChatCommand("place template test:nope 10 70 10")
	servertest.AwaitItemFailure(t, c, "commands.place.template.invalid", protocol.Text{Text: "test:nope"})
	c.ChatCommand("place template test:small 100000 70 100000")
	servertest.AwaitCommandFailure(t, c, "argument.pos.unloaded")
	c.Disconnect()
}

// TestPlaceFeatureAnswersByKey checks that a tree feature grows on grass, one
// without a loaded-chunk generator or without room fails, and an unknown key
// is refused as the argument is read.
func TestPlaceFeatureAnswersByKey(t *testing.T) {
	t.Parallel()
	srv, c := startPlaceWorld(t)
	grass, _ := registry.FindState("minecraft:grass_block", nil)
	stone, _ := registry.FindState("minecraft:stone", nil)
	srv.SetBlock(30, 69, 30, grass)
	c.ChatCommand("place feature minecraft:oak 30 70 30")
	expectPlaced(t, c, "commands.place.feature.success", "minecraft:oak", 30, 70, 30)
	wantBlock(t, srv, 30, 70, 30, "minecraft:oak_log", map[string]string{"axis": "y"})
	podzol, _ := registry.FindState("minecraft:podzol", nil)
	srv.SetBlock(50, 69, 50, podzol)
	c.ChatCommand("place feature minecraft:mega_spruce 50 70 50")
	expectPlaced(t, c, "commands.place.feature.success", "minecraft:mega_spruce", 50, 70, 50)
	srv.SetBlock(60, 69, 60, grass)
	c.ChatCommand("place feature minecraft:huge_red_mushroom 60 70 60")
	expectPlaced(t, c, "commands.place.feature.success", "minecraft:huge_red_mushroom", 60, 70, 60)
	wantBlock(t, srv, 60, 70, 60, "minecraft:mushroom_stem", nil)
	sand, _ := registry.FindState("minecraft:sand", nil)
	srv.SetBlock(20, 69, 20, sand)
	c.ChatCommand("place feature minecraft:dead_bush 20 70 20")
	expectPlaced(t, c, "commands.place.feature.success", "minecraft:dead_bush", 20, 70, 20)
	wantBlock(t, srv, 20, 70, 20, "minecraft:dead_bush", nil)
	srv.SetBlock(40, 69, 40, grass)
	srv.SetBlock(40, 72, 40, stone)
	c.ChatCommand("place feature minecraft:oak 40 70 40")
	servertest.AwaitCommandFailure(t, c, "commands.place.feature.failed")
	c.ChatCommand("place feature minecraft:ore_iron 0 64 6")
	servertest.AwaitCommandFailure(t, c, "commands.place.feature.failed")
	c.ChatCommand("place feature minecraft:nonsense 0 64 6")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got,
		servertest.CommandErrorMessageForTest("argument.resource_or_id.no_such_element", "minecraft:nonsense", "minecraft:worldgen/feature")) {
		t.Errorf("an unknown feature answered % X, want the unknown element refusal", got)
	}
	c.Disconnect()
}

// TestPlaceTemplateSuggestsSavedTemplates checks that the template argument
// completes to the ids of the template files.
func TestPlaceTemplateSuggestsSavedTemplates(t *testing.T) {
	t.Parallel()
	_, c := startPlaceWorld(t)
	c.CommandSuggestion(1, "/place template test:sm")
	_, _, _, matches := c.ExpectCommandSuggestions()
	if len(matches) != 1 || matches[0] != "test:small" {
		t.Errorf("suggestions = %v, want [test:small]", matches)
	}
	c.Disconnect()
}

// TestPlaceJigsawAndStructureAnswerByKey checks that a pool the data pack
// names fails to place and an unknown pool or structure is invalid.
func TestPlaceJigsawAndStructureAnswerByKey(t *testing.T) {
	t.Parallel()
	_, c := startPlaceWorld(t)
	c.ChatCommand("place jigsaw minecraft:village/plains/town_centers minecraft:empty 5")
	servertest.AwaitCommandFailure(t, c, "commands.place.jigsaw.failed")
	c.ChatCommand("place jigsaw test:nope minecraft:empty 5")
	servertest.AwaitItemFailure(t, c, "commands.place.jigsaw.invalid", protocol.Text{Text: "test:nope"})
	c.ChatCommand("place structure test:nope")
	servertest.AwaitItemFailure(t, c, "commands.place.structure.invalid", protocol.Text{Text: "test:nope"})
	c.Disconnect()
}

// TestPlaceJigsawGrowsThePool checks that /place jigsaw builds the pool's
// piece at the position and reports success.
// vanilla: PlaceCommand.placeJigsaw, JigsawPlacement.generateJigsaw
func TestPlaceJigsawGrowsThePool(t *testing.T) {
	t.Parallel()
	srv, c := startPlaceWorld(t)
	c.ChatCommand("place jigsaw minecraft:village/plains/town_centers minecraft:bottom 2 8 70 8")
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.place.jigsaw.success", intArg(8), intArg(70), intArg(8))
	placed := 0
	for x := int32(-8); x <= 24; x++ {
		for z := int32(-8); z <= 24; z++ {
			if srv.BlockAt(x, 70, z) != srv.BlockAt(x, 120, z) {
				placed++
			}
		}
	}
	if placed == 0 {
		t.Fatal("no block was placed around the position")
	}
	c.Disconnect()
}

// TestPlaceStructureDesertPyramid checks that a desert pyramid is built on a
// flat area and that a structure that finds no point here fails.
func TestPlaceStructureDesertPyramid(t *testing.T) {
	t.Parallel()
	srv, c := startPlaceWorld(t)
	c.ChatCommand("place structure minecraft:desert_pyramid 8 70 8")
	expectPlaced(t, c, "commands.place.structure.success", "minecraft:desert_pyramid", 8, 70, 8)
	wantBlock(t, srv, 8, 65, 8, "minecraft:cut_sandstone", nil)
	// vanilla: OceanMonumentStructure.findGenerationPoint wants ocean biomes
	// all around.
	c.ChatCommand("place structure minecraft:monument 8 70 8")
	servertest.AwaitCommandFailure(t, c, "commands.place.structure.failed")
	c.Disconnect()
}

// TestPlaceStructureFillsChestsAndSpawnsMobs checks that a placed structure
// keeps its pieces' loot tables and structure mobs.
// vanilla: StructurePiece.createChest, SwampHutPiece.postProcess
func TestPlaceStructureFillsChestsAndSpawnsMobs(t *testing.T) {
	t.Parallel()
	srv, c := startPlaceWorld(t)
	c.ChatCommand("place structure minecraft:jungle_pyramid 40 70 40")
	expectPlaced(t, c, "commands.place.structure.success", "minecraft:jungle_pyramid", 40, 70, 40)
	chests := 0
	for x := int32(30); x <= 50; x++ {
		for y := int32(55); y <= 75; y++ {
			for z := int32(30); z <= 50; z++ {
				if registry.BlockOf(srv.BlockAt(x, y, z)) != registry.BlockChest {
					continue
				}
				chests++
				tag, ok := srv.BlockEntityTagAt(x, y, z)
				if loot, _ := nbt.Get[nbt.String](tag, "LootTable"); !ok || loot != "minecraft:chests/jungle_temple" {
					t.Errorf("chest at (%d,%d,%d): tag %v, %v; want LootTable minecraft:chests/jungle_temple", x, y, z, tag, ok)
				}
			}
		}
	}
	if chests == 0 {
		t.Fatal("the jungle pyramid placed no chest")
	}
	c.ChatCommand("place structure minecraft:swamp_hut 8 70 8")
	expectPlaced(t, c, "commands.place.structure.success", "minecraft:swamp_hut", 8, 70, 8)
	c.AwaitSpawn(protocol.EntityTypeWitch)
	c.Disconnect()
}

// columnsAround records the blocks within 16 of (x, z) on the horizontal
// and from y 0 to 160.
func columnsAround(srv *server.Server, x, z int32) []uint16 {
	var out []uint16
	for bx := x - 16; bx <= x+16; bx++ {
		for bz := z - 16; bz <= z+16; bz++ {
			for y := int32(0); y <= 160; y++ {
				out = append(out, srv.BlockAt(bx, y, bz))
			}
		}
	}
	return out
}

// TestPlaceStructureBuildsWideStructures checks that a structure spanning
// more than three chunks is built, and that one reaching unloaded chunks
// answers with the position error. vanilla: PlaceCommand.placeStructure
func TestPlaceStructureBuildsWideStructures(t *testing.T) {
	t.Parallel()
	srv, c := startPlaceWorld(t)
	for _, tc := range []struct {
		id   string
		x, z int32
	}{{"minecraft:pillager_outpost", 8, 8}, {"minecraft:abandoned_camp_forest", 40, 40}} {
		before := columnsAround(srv, tc.x, tc.z)
		c.ChatCommand(fmt.Sprintf("place structure %s %d 70 %d", tc.id, tc.x, tc.z))
		expectPlaced(t, c, "commands.place.structure.success", tc.id, int64(tc.x), 70, int64(tc.z))
		if slices.Equal(before, columnsAround(srv, tc.x, tc.z)) {
			t.Fatalf("%s changed no block within 16 of the position", tc.id)
		}
	}
	c.ChatCommand("place structure minecraft:village_plains 8 70 8")
	servertest.AwaitCommandFailure(t, c, "argument.pos.unloaded")
	c.Disconnect()
}

func TestPlaceBuiltinTemplate(t *testing.T) {
	t.Parallel()
	srv, c := startPlaceWorld(t)
	c.MoveToGround(10, 70, 10, true)
	c.Settle()
	c.ChatCommand("place template minecraft:igloo/top ~ ~ ~")
	expectPlaced(t, c, "commands.place.template.success", "minecraft:igloo/top", 10, 70, 10)
	wantBlock(t, srv, 10, 70, 13, "minecraft:snow_block", nil)
	c.Disconnect()
}

func TestStructureBlockLoadsBuiltinTemplate(t *testing.T) {
	t.Parallel()
	srv, c := startPlaceWorld(t)
	servertest.Setblock(t, c, 10, 70, 10, "minecraft:structure_block[mode=load]")
	c.ProgramStructureBlock(servertest.StructureBlockPacket{
		X: 10, Y: 70, Z: 10, Action: servertest.StructureActionLoad, Mode: servertest.StructureModeLoad,
		Name: "minecraft:igloo/top", Offset: [3]int8{1, 0, 0}, Size: [3]int8{7, 5, 8},
		Integrity: 1, IgnoreEntities: true,
	})
	servertest.AwaitCommandFeedback(t, c, "structure_block.load_success", "minecraft:igloo/top")
	wantBlock(t, srv, 11, 70, 13, "minecraft:snow_block", nil)
	c.Disconnect()
}

// TestPlaceEmptyBuiltinTemplate: the jar's minecraft:empty, the always_pass
// test's structure, places.
func TestPlaceEmptyBuiltinTemplate(t *testing.T) {
	t.Parallel()
	_, c := startPlaceWorld(t)
	c.ChatCommand("place template minecraft:empty 10 70 10")
	servertest.AwaitCommandFeedbackText(t, c, "commands.place.template.success", protocol.Text{Text: "minecraft:empty"},
		servertest.NumberText(10), servertest.NumberText(70), servertest.NumberText(10))
	c.Disconnect()
}

func rowTemplate(palettes []nbt.List, blocks nbt.List, x, y, z int32) nbt.Compound {
	t := nbt.Compound{"size": ints(x, y, z), "blocks": blocks, "DataVersion": nbt.Int(4440)}
	if len(palettes) == 1 {
		t["palette"] = palettes[0]
		return t
	}
	list := nbt.List{Elem: nbt.TypeList}
	for _, p := range palettes {
		list.Items = append(list.Items, p)
	}
	t["palettes"] = list
	return t
}

func paletteOf(entries ...nbt.Compound) nbt.List {
	l := nbt.List{Elem: nbt.TypeCompound}
	for _, e := range entries {
		l.Items = append(l.Items, e)
	}
	return l
}

func blocksOf(blocks ...nbt.Compound) nbt.List {
	l := nbt.List{Elem: nbt.TypeCompound}
	for _, b := range blocks {
		l.Items = append(l.Items, b)
	}
	return l
}

// TestPlaceTemplateRollsPaletteFromSeededStream checks that with several
// palettes and integrity below one the palette roll and the integrity rolls
// share the seeded stream: seed 2 rolls palette 1, then draws 0.2934 and
// 0.9014 (java.util.Random), so the first block stays and the second drops.
// vanilla: StructurePlaceSettings.getRandomPalette, BlockRotProcessor
func TestPlaceTemplateRollsPaletteFromSeededStream(t *testing.T) {
	t.Parallel()
	pal := rowTemplate([]nbt.List{
		paletteOf(paletteEntry("minecraft:stone", nil), paletteEntry("minecraft:dirt", nil)),
		paletteOf(paletteEntry("minecraft:gold_block", nil), paletteEntry("minecraft:diamond_block", nil)),
	}, blocksOf(templateBlock(0, 0, 0, 0, nil), templateBlock(1, 1, 0, 0, nil)), 2, 1, 1)
	srv, c := startPlaceWorldWith(t, map[string]nbt.Compound{"pal": pal})
	c.ChatCommand("place template test:pal 10 70 10 none none 0.3 2")
	expectPlaced(t, c, "commands.place.template.success", "test:pal", 10, 70, 10)
	wantBlock(t, srv, 10, 70, 10, "minecraft:gold_block", nil)
	wantBlock(t, srv, 11, 70, 10, "minecraft:air", nil)
	c.Disconnect()
}

// TestPlaceTemplateConnectsFences checks that the placed fences reconnect to
// each other and to a fence outside the template, and that strict leaves them
// as saved.
// vanilla: StructureTemplate.placeInWorld, updateShapeAtEdge
func TestPlaceTemplateConnectsFences(t *testing.T) {
	t.Parallel()
	fences := rowTemplate([]nbt.List{paletteOf(paletteEntry("minecraft:oak_fence", nil))},
		blocksOf(templateBlock(0, 0, 0, 0, nil), templateBlock(0, 1, 0, 0, nil)), 2, 1, 1)
	srv, c := startPlaceWorldWith(t, map[string]nbt.Compound{"fences": fences})
	fence, _ := registry.FindState("minecraft:oak_fence", nil)
	srv.SetBlock(9, 70, 10, fence)
	c.ChatCommand("place template test:fences 10 70 10")
	expectPlaced(t, c, "commands.place.template.success", "test:fences", 10, 70, 10)
	wantBlock(t, srv, 10, 70, 10, "minecraft:oak_fence", map[string]string{"east": "true", "west": "true"})
	wantBlock(t, srv, 11, 70, 10, "minecraft:oak_fence", map[string]string{"west": "true", "east": "false"})
	wantBlock(t, srv, 9, 70, 10, "minecraft:oak_fence", map[string]string{"east": "true"})

	c.ChatCommand("place template test:fences 10 70 20 none none 1 0 strict")
	expectPlaced(t, c, "commands.place.template.success", "test:fences", 10, 70, 20)
	wantBlock(t, srv, 10, 70, 20, "minecraft:oak_fence", map[string]string{"east": "false"})
	c.Disconnect()
}

// TestPlaceTemplateWaterlogsBlocks checks that a waterloggable block takes
// the water of the source it replaces or of a source beside it, and stays dry
// with none.
// vanilla: StructureTemplate.placeInWorld
func TestPlaceTemplateWaterlogsBlocks(t *testing.T) {
	t.Parallel()
	slab := paletteEntry("minecraft:oak_slab", map[string]string{"type": "bottom"})
	wet := rowTemplate([]nbt.List{paletteOf(slab)}, blocksOf(
		templateBlock(0, 0, 0, 0, nil), templateBlock(0, 2, 0, 0, nil), templateBlock(0, 4, 0, 0, nil),
	), 5, 1, 1)
	srv, c := startPlaceWorldWith(t, map[string]nbt.Compound{"wet": wet})
	water, _ := registry.FindState("minecraft:water", map[string]string{"level": "0"})
	srv.SetBlock(30, 70, 30, water)
	srv.SetBlock(32, 71, 30, water)
	c.ChatCommand("place template test:wet 30 70 30")
	expectPlaced(t, c, "commands.place.template.success", "test:wet", 30, 70, 30)
	wantBlock(t, srv, 30, 70, 30, "minecraft:oak_slab", map[string]string{"waterlogged": "true"})
	wantBlock(t, srv, 32, 70, 30, "minecraft:oak_slab", map[string]string{"waterlogged": "true"})
	wantBlock(t, srv, 34, 70, 30, "minecraft:oak_slab", map[string]string{"waterlogged": "false"})
	c.Disconnect()
}

// TestPlaceTemplateReplacesContainerAndSeedsLoot checks that a tagged chest
// placed over a chest with items does not keep them, and that a chest with a
// loot table takes the next draw of the seeded stream as its LootTableSeed.
// vanilla: StructureTemplate.placeInWorld
func TestPlaceTemplateReplacesContainerAndSeedsLoot(t *testing.T) {
	t.Parallel()
	chest := paletteEntry("minecraft:chest", map[string]string{"facing": "north"})
	loot := rowTemplate([]nbt.List{paletteOf(chest)}, blocksOf(
		templateBlock(0, 0, 0, 0, nbt.Compound{"id": nbt.String("minecraft:chest")}),
		templateBlock(0, 2, 0, 0, nbt.Compound{"id": nbt.String("minecraft:chest"), "LootTable": nbt.String("test:pending")}),
	), 3, 1, 1)
	srv, c := startPlaceWorldWith(t, map[string]nbt.Compound{"loot": loot})
	c.ChatCommand("setblock 50 70 50 minecraft:chest[facing=north]")
	c.ChatCommand("item replace block 50 70 50 container.0 with minecraft:apple")
	c.Settle()
	if tag, _ := srv.BlockEntityTagAt(50, 70, 50); tag == nil || tag["Items"] == nil {
		t.Fatalf("the chest in place holds no items to replace: %v", tag)
	}
	c.ChatCommand("place template test:loot 50 70 50 none none 1 5")
	expectPlaced(t, c, "commands.place.template.success", "test:loot", 50, 70, 50)
	tag, _ := srv.BlockEntityTagAt(50, 70, 50)
	if items, _ := nbt.Get[nbt.List](tag, "Items"); len(items.Items) != 0 {
		t.Errorf("the replaced chest kept its items: %v", tag)
	}
	// vanilla draws one long per container block entity carrying template
	// data, from createRandom(seed) apart from the integrity stream.
	rng := levelgen.NewLegacyRandom(5)
	rng.NextLong() // the first chest
	want := rng.NextLong()
	tag, _ = srv.BlockEntityTagAt(52, 70, 50)
	if got, _ := nbt.LongValue(tag["LootTableSeed"]); got != want {
		t.Errorf("LootTableSeed = %d, want %d (tag %v)", got, want, tag)
	}
	c.Disconnect()
}

// TestPlaceTemplatePlacesEntities checks that an armor stand of the template
// stands at its position turned with the template.
// vanilla: StructureTemplate.placeEntities
func TestPlaceTemplatePlacesEntities(t *testing.T) {
	t.Parallel()
	pos := nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(0.5), nbt.Double(0), nbt.Double(1.5)}}
	stand := nbt.Compound{"pos": pos, "blockPos": ints(0, 0, 1), "nbt": nbt.Compound{
		"id": nbt.String("minecraft:armor_stand"), "UUID": nbt.IntArray{1, 2, 3, 4},
	}}
	ent := rowTemplate([]nbt.List{paletteOf(paletteEntry("minecraft:stone", nil))}, blocksOf(templateBlock(0, 0, 0, 0, nil)), 2, 1, 2)
	ent["entities"] = nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{stand}}
	_, c := startPlaceWorldWith(t, map[string]nbt.Compound{"ent": ent})
	c.ChatCommand("place template test:ent 10 70 10 clockwise_90")
	expectPlaced(t, c, "commands.place.template.success", "test:ent", 10, 70, 10)
	// (x, z) -> (1 - z, x) about the block corner
	got := c.AwaitSpawn(protocol.EntityTypeArmorStand)
	if got.X != 9.5 || got.Y != 70 || got.Z != 10.5 {
		t.Errorf("armor stand at (%v, %v, %v), want (9.5, 70, 10.5)", got.X, got.Y, got.Z)
	}
	c.Disconnect()
}

func doubles(v ...float64) nbt.List {
	l := nbt.List{Elem: nbt.TypeDouble}
	for _, n := range v {
		l.Items = append(l.Items, nbt.Double(n))
	}
	return l
}

// entityTemplate is a one-block template carrying the entities at the
// template-relative centres given.
func entityTemplate(entities ...nbt.Compound) nbt.Compound {
	ent := rowTemplate([]nbt.List{paletteOf(paletteEntry("minecraft:stone", nil))}, blocksOf(templateBlock(0, 0, 0, 0, nil)), 2, 2, 2)
	list := nbt.List{Elem: nbt.TypeCompound}
	for _, e := range entities {
		list.Items = append(list.Items, e)
	}
	ent["entities"] = list
	return ent
}

func entityAt(pos nbt.List, block nbt.List, tag nbt.Compound) nbt.Compound {
	return nbt.Compound{"pos": pos, "blockPos": block, "nbt": tag}
}

// TestPlaceTemplateTurnsHangingEntities checks that an item frame and a
// painting take the facing the template's rotation and then its mirror give
// them, and that the mirror reads the rotated facing.
// vanilla: HangingEntity.rotate, HangingEntity.mirror
func TestPlaceTemplateTurnsHangingEntities(t *testing.T) {
	t.Parallel()
	frame := entityAt(doubles(0.5, 0.5, 0.03), ints(0, 0, 0), nbt.Compound{
		"id": nbt.String("minecraft:item_frame"), "Facing": nbt.Byte(2), // north
	})
	painting := entityAt(doubles(0.5, 0.5, 0.03), ints(0, 0, 0), nbt.Compound{
		"id": nbt.String("minecraft:painting"), "facing": nbt.Byte(2), // north
	})
	_, c := startPlaceWorldWith(t, map[string]nbt.Compound{"hang": entityTemplate(frame, painting)})
	for i, tc := range []struct {
		args string
		want int32 // Direction.get3DDataValue
	}{
		{"none none", 2},
		{"clockwise_90 none", 5},
		// rotated to east, then front_back flips the east-west axis
		{"clockwise_90 front_back", 4},
		{"none left_right", 3},
	} {
		x := int32(10 + 10*i)
		c.ChatCommand("place template test:hang " + itoa(x) + " 70 10 " + tc.args)
		expectPlaced(t, c, "commands.place.template.success", "test:hang", int64(x), 70, 10)
		if got := c.AwaitSpawn(protocol.EntityTypeItemFrame); got.Data != tc.want {
			t.Errorf("%s: item frame facing %d, want %d", tc.args, got.Data, tc.want)
		}
		if got := c.AwaitSpawn(protocol.EntityTypePainting); got.Data != tc.want {
			t.Errorf("%s: painting facing %d, want %d", tc.args, got.Data, tc.want)
		}
	}
	c.Disconnect()
}

func itoa(v int32) string { return strconv.Itoa(int(v)) }

// TestPlaceTemplatePlacesBoatsAndItems checks that a boat and an item entity
// of the template stand at their transformed positions.
// vanilla: StructureTemplate.placeEntities
func TestPlaceTemplatePlacesBoatsAndItems(t *testing.T) {
	t.Parallel()
	boat := entityAt(doubles(0.5, 0, 1.5), ints(0, 0, 1), nbt.Compound{"id": nbt.String("minecraft:oak_boat")})
	item := entityAt(doubles(1.5, 0, 0.5), ints(1, 0, 0), nbt.Compound{
		"id": nbt.String("minecraft:item"), "Item": nbt.Compound{"id": nbt.String("minecraft:apple"), "count": nbt.Int(3)},
	})
	_, c := startPlaceWorldWith(t, map[string]nbt.Compound{"floaters": entityTemplate(boat, item)})
	c.ChatCommand("place template test:floaters 10 70 10 clockwise_90")
	expectPlaced(t, c, "commands.place.template.success", "test:floaters", 10, 70, 10)
	// (x, z) -> (1 - z, x) about the block corner
	if got := c.AwaitSpawn(protocol.EntityTypeOakBoat); got.X != 9.5 || got.Z != 10.5 {
		t.Errorf("boat at (%v, %v), want (9.5, 10.5)", got.X, got.Z)
	}
	if got := c.AwaitSpawn(protocol.EntityTypeItem); got.X != 10.5 || got.Z != 11.5 {
		t.Errorf("item at (%v, %v), want (10.5, 11.5)", got.X, got.Z)
	}
	c.Disconnect()
}

// TestPlaceTemplatePlacesCartsAndTNT checks that a minecart and a primed
// TNT of the template stand at their placed positions.
// vanilla: StructureTemplate.placeEntities
func TestPlaceTemplatePlacesCartsAndTNT(t *testing.T) {
	t.Parallel()
	cart := entityAt(doubles(0.5, 0, 0.5), ints(0, 0, 0), nbt.Compound{"id": nbt.String("minecraft:minecart")})
	tnt := entityAt(doubles(1.5, 0, 0.5), ints(1, 0, 0), nbt.Compound{"id": nbt.String("minecraft:tnt"), "fuse": nbt.Short(200)})
	_, c := startPlaceWorldWith(t, map[string]nbt.Compound{"carts": entityTemplate(cart, tnt)})
	c.ChatCommand("place template test:carts 10 70 10")
	expectPlaced(t, c, "commands.place.template.success", "test:carts", 10, 70, 10)
	if got := c.AwaitSpawn(protocol.EntityTypeMinecart); got.X != 10.5 || got.Y != 70 || got.Z != 10.5 {
		t.Errorf("minecart at (%v, %v, %v), want (10.5, 70, 10.5)", got.X, got.Y, got.Z)
	}
	if got := c.AwaitSpawn(protocol.EntityTypeTnt); got.X != 11.5 || got.Y != 70 || got.Z != 10.5 {
		t.Errorf("tnt at (%v, %v, %v), want (11.5, 70, 10.5)", got.X, got.Y, got.Z)
	}
	c.Disconnect()
}

// TestPlaceTemplateReshapesNeighbourStairsAndWalls checks that a placed
// block reshapes the stair and wall already standing beside it, and that
// strict leaves them as they were.
// vanilla: Level.setBlock, StructureTemplate.placeInWorld
func TestPlaceTemplateReshapesNeighbourStairsAndWalls(t *testing.T) {
	t.Parallel()
	stair := rowTemplate([]nbt.List{paletteOf(paletteEntry("minecraft:oak_stairs", map[string]string{"facing": "east"}))},
		blocksOf(templateBlock(0, 0, 0, 0, nil)), 1, 1, 1)
	wall := rowTemplate([]nbt.List{paletteOf(paletteEntry("minecraft:cobblestone_wall", nil))},
		blocksOf(templateBlock(0, 0, 0, 0, nil)), 1, 1, 1)
	srv, c := startPlaceWorldWith(t, map[string]nbt.Compound{"stair": stair, "wall": wall})
	north, _ := registry.FindState("minecraft:oak_stairs", map[string]string{"facing": "north"})
	cobble, _ := registry.FindState("minecraft:cobblestone_wall", nil)
	for _, z := range []int32{10, 30} {
		srv.SetBlock(10, 70, z, north)
		srv.SetBlock(20, 70, z, cobble)
	}
	// The stair lands behind the one facing north, turning it to an inner
	// corner; the wall lands beside the standing wall and joins it.
	c.ChatCommand("place template test:stair 10 70 11")
	expectPlaced(t, c, "commands.place.template.success", "test:stair", 10, 70, 11)
	c.ChatCommand("place template test:wall 21 70 10")
	expectPlaced(t, c, "commands.place.template.success", "test:wall", 21, 70, 10)
	wantBlock(t, srv, 10, 70, 10, "minecraft:oak_stairs", map[string]string{"shape": "inner_right"})
	wantBlock(t, srv, 20, 70, 10, "minecraft:cobblestone_wall", map[string]string{"east": "low"})
	wantBlock(t, srv, 21, 70, 10, "minecraft:cobblestone_wall", map[string]string{"west": "low"})

	c.ChatCommand("place template test:stair 10 70 31 none none 1 0 strict")
	expectPlaced(t, c, "commands.place.template.success", "test:stair", 10, 70, 31)
	c.ChatCommand("place template test:wall 21 70 30 none none 1 0 strict")
	expectPlaced(t, c, "commands.place.template.success", "test:wall", 21, 70, 30)
	wantBlock(t, srv, 10, 70, 30, "minecraft:oak_stairs", map[string]string{"shape": "straight"})
	wantBlock(t, srv, 20, 70, 30, "minecraft:cobblestone_wall", map[string]string{"east": "none"})
	c.Disconnect()
}

// TestPlaceTemplateStrictWaterlogs checks that a strict placement still
// waterlogs a block written over water.
// vanilla: StructureTemplate.placeInWorld, SimpleWaterloggedBlock.placeLiquid
func TestPlaceTemplateStrictWaterlogs(t *testing.T) {
	t.Parallel()
	slab := paletteEntry("minecraft:oak_slab", map[string]string{"type": "bottom"})
	wet := rowTemplate([]nbt.List{paletteOf(slab)}, blocksOf(templateBlock(0, 0, 0, 0, nil)), 1, 1, 1)
	srv, c := startPlaceWorldWith(t, map[string]nbt.Compound{"wet": wet})
	water, _ := registry.FindState("minecraft:water", map[string]string{"level": "0"})
	srv.SetBlock(30, 70, 30, water)
	c.ChatCommand("place template test:wet 30 70 30 none none 1 0 strict")
	expectPlaced(t, c, "commands.place.template.success", "test:wet", 30, 70, 30)
	wantBlock(t, srv, 30, 70, 30, "minecraft:oak_slab", map[string]string{"waterlogged": "true"})
	c.Disconnect()
}

// TestPlaceJigsawAndStructureCheckPosition checks that the position of
// jigsaw and structure is held to the loaded world.
// vanilla: BlockPosArgument.getLoadedBlockPos
func TestPlaceJigsawAndStructureCheckPosition(t *testing.T) {
	t.Parallel()
	_, c := startPlaceWorld(t)
	c.ChatCommand("place jigsaw minecraft:village/plains/town_centers minecraft:empty 5 100000 70 100000")
	servertest.AwaitCommandFailure(t, c, "argument.pos.unloaded")
	c.ChatCommand("place structure minecraft:village_plains 100000 70 100000")
	servertest.AwaitCommandFailure(t, c, "argument.pos.unloaded")
	c.ChatCommand("place structure minecraft:village_plains 0 100000 0")
	servertest.AwaitCommandFailure(t, c, "argument.pos.outofworld")
	c.Disconnect()
}

func TestPlaceTemplateUpgradesOldDataVersion(t *testing.T) {
	t.Parallel()
	old := rowTemplate([]nbt.List{paletteOf(
		paletteEntry("minecraft:chain", map[string]string{"axis": "x"}),
		paletteEntry("minecraft:chest", nil),
	)}, blocksOf(
		templateBlock(0, 0, 0, 0, nil),
		templateBlock(1, 1, 0, 0, nbt.Compound{"id": nbt.String("minecraft:chest"), "Items": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"Slot": nbt.Byte(0), "id": nbt.String("minecraft:chain"), "count": nbt.Int(1)}}}}),
	), 2, 1, 1)
	srv, c := startPlaceWorldWith(t, map[string]nbt.Compound{"old": old})
	c.ChatCommand("place template test:old 10 70 10")
	expectPlaced(t, c, "commands.place.template.success", "test:old", 10, 70, 10)
	wantBlock(t, srv, 10, 70, 10, "minecraft:iron_chain", map[string]string{"axis": "x"})
	tag, ok := srv.BlockEntityTagAt(11, 70, 10)
	if !ok {
		t.Fatal("upgraded chest has no block entity")
	}
	items, _ := nbt.Get[nbt.List](tag, "Items")
	if len(items.Items) != 1 {
		t.Fatalf("chest Items = %v", tag["Items"])
	}
	stack, _ := items.Items[0].(nbt.Compound)
	if stack["id"] != nbt.String("minecraft:iron_chain") {
		t.Errorf("chest item = %v", stack)
	}
	c.Disconnect()
}
