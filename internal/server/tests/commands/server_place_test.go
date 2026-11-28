package commands

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
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
func startPlaceWorldWith(t *testing.T, templates map[string]nbt.Compound, setup ...func(*server.Server)) (*server.Server, *servertest.Client) {
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
	srv, c := servertest.Join(t, cfg, "Testificate", append([]func(*server.Server){servertest.OpsListing(t, "Testificate")}, setup...)...)
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

// TestPlaceTemplateRunsOnPlaceBeforeLaterBlocks pins that /place template runs
// each block's onPlace hook as it lands (before later blocks in the template
// exist), matching StructureTemplate.placeInWorld. A compare-mode comparator
// placed before a powered dust schedules its refresh before a subtract-mode
// comparator placed after the dust, so the subtract comparator never pulses on
// tick 2.
// vanilla: StructureTemplate.placeInWorld
func TestPlaceTemplateRunsOnPlaceBeforeLaterBlocks(t *testing.T) {
	t.Parallel()
	palette := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		paletteEntry("minecraft:stone", nil),
		paletteEntry("minecraft:redstone_block", nil),
		paletteEntry("minecraft:comparator", map[string]string{"facing": "west", "mode": "compare", "powered": "true"}),
		paletteEntry("minecraft:redstone_wire", map[string]string{"east": "side", "north": "none", "power": "15", "south": "none", "west": "side"}),
		paletteEntry("minecraft:comparator", map[string]string{"facing": "north", "mode": "subtract", "powered": "false"}),
	}}
	// Floor at y=0, circuit at y=1. The extra stone at (3,1,2) precedes the
	// circuit blocks so a deferred wake pass would hit the dust at (3,1,1)
	// after both comparators exist; running onPlace during placement schedules
	// the compare comparator at (1,1,1) when the dust lands, before (4,1,1)
	// exists.
	blocks := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		templateBlock(0, 1, 0, 1, nil),
		templateBlock(0, 3, 0, 1, nil),
		templateBlock(0, 4, 0, 1, nil),
		templateBlock(0, 3, 1, 2, nil),
		templateBlock(1, 0, 1, 1, nil),
		templateBlock(1, 4, 1, 0, nil),
		templateBlock(2, 1, 1, 1, nil),
		templateBlock(0, 2, 1, 1, nil),
		templateBlock(3, 3, 1, 1, nil),
		templateBlock(4, 4, 1, 1, nil),
	}}
	tpl := nbt.Compound{
		"size": ints(5, 2, 3), "palette": palette, "blocks": blocks, "DataVersion": nbt.Int(4554),
	}
	srv, c := startPlaceWorldWith(t, map[string]nbt.Compound{"comp": tpl})
	servertest.HoldTicks(t, srv)
	c.ChatCommand("place template test:comp 10 70 10")
	servertest.RunTicks(t, srv, 1)
	expectPlaced(t, c, "commands.place.template.success", "test:comp", 10, 70, 10)

	for range 6 {
		servertest.RunTicks(t, srv, 1)
		wantBlock(t, srv, 14, 71, 11, "minecraft:comparator", map[string]string{"powered": "false"})
	}
	wantBlock(t, srv, 13, 71, 11, "minecraft:redstone_wire", map[string]string{"power": "15"})
	c.Disconnect()
}

// dustOnContainerTemplate is a 1x2x1 structure of a block-entity block with
// redstone dust above it, the two blocks in the given order.
func dustOnContainerTemplate(container string, props map[string]string, dustFirst bool) nbt.Compound {
	palette := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		paletteEntry("minecraft:"+container, props),
		paletteEntry("minecraft:redstone_wire", map[string]string{"east": "none", "north": "none", "south": "none", "west": "none", "power": "0"}),
	}}
	box := templateBlock(0, 0, 0, 0, nbt.Compound{"Items": nbt.List{Elem: nbt.TypeCompound}})
	dust := templateBlock(1, 0, 1, 0, nil)
	blocks := []nbt.Tag{box, dust}
	if dustFirst {
		blocks = []nbt.Tag{dust, box}
	}
	return nbt.Compound{
		"size": ints(1, 2, 1), "palette": palette, "DataVersion": nbt.Int(4554),
		"blocks": nbt.List{Elem: nbt.TypeCompound, Items: blocks},
	}
}

// TestPlaceTemplateClearsDustOnBlockEntity checks that /place template drops
// redstone dust standing on a dispenser or hopper, whichever block the
// template lists first, and that a hopper then reads as unpowered, on the
// placing tick and on the next one.
// vanilla: StructureTemplate.placeInWorld
func TestPlaceTemplateClearsDustOnBlockEntity(t *testing.T) {
	t.Parallel()
	down := map[string]string{"facing": "down"}
	hopper := map[string]string{"facing": "down", "enabled": "false"}
	srv, c := startPlaceWorldWith(t, map[string]nbt.Compound{
		"dispenser_dust_first": dustOnContainerTemplate("dispenser", down, true),
		"dispenser_box_first":  dustOnContainerTemplate("dispenser", down, false),
		"hopper_dust_first":    dustOnContainerTemplate("hopper", hopper, true),
		"hopper_box_first":     dustOnContainerTemplate("hopper", hopper, false),
	}, func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
	check := func(when string) {
		for i, name := range []string{"dispenser_dust_first", "dispenser_box_first", "hopper_dust_first", "hopper_box_first"} {
			x := int32(10 + 4*i)
			wantBlock(t, srv, x, 71, 10, "minecraft:air", nil)
			if i >= 2 {
				wantBlock(t, srv, x, 70, 10, "minecraft:hopper", map[string]string{"enabled": "true"})
			}
			if t.Failed() {
				t.Fatalf("%s: %s", when, name)
			}
		}
	}
	for i, name := range []string{"dispenser_dust_first", "dispenser_box_first", "hopper_dust_first", "hopper_box_first"} {
		x := int64(10 + 4*i)
		c.ChatCommand(fmt.Sprintf("place template test:%s %d 70 10", name, x))
		expectPlaced(t, c, "commands.place.template.success", "test:"+name, x, 70, 10)
	}
	check("placing tick")
	c.ChatCommand("tick step 1")
	servertest.RunTicks(t, srv, 1)
	check("after one tick")
}

// readChunkBlockTicks reads the saved block_ticks list of chunk (cx, cz) from
// its region file after /save-all flush.
func readChunkBlockTicks(t *testing.T, level string, cx, cz int32) []nbt.Compound {
	t.Helper()
	path := filepath.Join(level, "dimensions", "minecraft", "overworld", "region", fmt.Sprintf("r.%d.%d.mca", cx>>5, cz>>5))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read region %s: %v", path, err)
	}
	index := (int(cx&31) + int(cz&31)*32) * 4
	offset := binary.BigEndian.Uint32(raw[index:]) >> 8
	if offset == 0 {
		t.Fatalf("region %s holds no chunk (%d,%d)", path, cx, cz)
	}
	start := int(offset) * 4096
	size := int(binary.BigEndian.Uint32(raw[start : start+4]))
	if raw[start+4] != 2 {
		t.Fatalf("chunk (%d,%d) compression = %d, want zlib (2)", cx, cz, raw[start+4])
	}
	zr, err := zlib.NewReader(bytes.NewReader(raw[start+5 : start+4+size]))
	if err != nil {
		t.Fatalf("decompress chunk (%d,%d): %v", cx, cz, err)
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read chunk (%d,%d): %v", cx, cz, err)
	}
	_, tag, err := nbt.Decode(data)
	if err != nil {
		t.Fatalf("decode chunk (%d,%d): %v", cx, cz, err)
	}
	root, ok := tag.(nbt.Compound)
	if !ok {
		t.Fatalf("chunk (%d,%d) root is %s, want compound", cx, cz, tag.Type())
	}
	list, ok := nbt.Get[nbt.List](root, "block_ticks")
	if !ok {
		return nil
	}
	out := make([]nbt.Compound, 0, len(list.Items))
	for _, item := range list.Items {
		if entry, ok := item.(nbt.Compound); ok {
			out = append(out, entry)
		}
	}
	return out
}

func hasBlockTick(ticks []nbt.Compound, id string, x, y, z, delay int32) bool {
	for _, tk := range ticks {
		gotID, _ := nbt.Get[nbt.String](tk, "i")
		gx, _ := nbt.IntValue(tk["x"])
		gy, _ := nbt.IntValue(tk["y"])
		gz, _ := nbt.IntValue(tk["z"])
		gt, _ := nbt.IntValue(tk["t"])
		if string(gotID) == id && gx == x && gy == y && gz == z && gt == delay {
			return true
		}
	}
	return false
}

// TestPlaceTemplateObserverFacingAirPulses checks that non-strict /place
// template pulses every placed observer via updateFromNeighbourShapes — even
// one facing air — leaving an observer block tick pending at t=2, powering the
// lamp behind it at step 2, unpowering at step 4, and turning the lamp off at
// step 8. Strict placement leaves the observer quiet.
// vanilla: StructureTemplate.placeInWorld, Block.updateFromNeighbourShapes, ObserverBlock.updateShape
func TestPlaceTemplateObserverFacingAirPulses(t *testing.T) {
	t.Parallel()
	obsTpl := func(powered string) nbt.Compound {
		palette := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			paletteEntry("minecraft:redstone_lamp", map[string]string{"lit": "false"}),
			paletteEntry("minecraft:observer", map[string]string{"facing": "east", "powered": powered}),
			paletteEntry("minecraft:sand", nil),
		}}
		blocks := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			templateBlock(0, 0, 0, 0, nil),
			templateBlock(1, 1, 0, 0, nil),
			templateBlock(2, 4, 2, 0, nil),
		}}
		return nbt.Compound{
			"size": ints(5, 3, 1), "palette": palette, "blocks": blocks, "DataVersion": nbt.Int(4554),
		}
	}

	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	cfg.LevelName = t.TempDir()
	dir := filepath.Join(cfg.LevelName, "generated", "test", "structure")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, tpl := range map[string]nbt.Compound{
		"obs":         obsTpl("false"),
		"obs_powered": obsTpl("true"),
	} {
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

	srv, c := servertest.Join(t, cfg, "Testificate",
		servertest.OpsListing(t, "Testificate"),
		func(s *server.Server) { servertest.FreezeUntilHeld(t, s) },
	)
	// Non-strict place at (10, 70, 10) (chunk 0,0) and strict place at (10, 70, 14).
	// Also test a powered=true observer at (10, 70, 12), which onPlace resets to
	// powered=false before updateFromNeighbourShapes runs.
	c.ChatCommand("place template test:obs 10 70 10")
	expectPlaced(t, c, "commands.place.template.success", "test:obs", 10, 70, 10)
	c.ChatCommand("place template test:obs_powered 10 70 12")
	expectPlaced(t, c, "commands.place.template.success", "test:obs_powered", 10, 70, 12)
	c.ChatCommand("place template test:obs 10 70 14 none none 1.0 0 strict")
	expectPlaced(t, c, "commands.place.template.success", "test:obs", 10, 70, 14)

	c.ChatCommand("save-all flush")
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.save.success")

	ticks := readChunkBlockTicks(t, cfg.LevelName, 0, 0)
	if !hasBlockTick(ticks, "minecraft:observer", 11, 70, 10, 2) {
		t.Fatalf("missing pending observer tick at (11,70,10) t=2; got %v", ticks)
	}
	if !hasBlockTick(ticks, "minecraft:sand", 14, 72, 10, 2) {
		t.Fatalf("missing pending sand tick at (14,72,10) t=2; got %v", ticks)
	}
	if !hasBlockTick(ticks, "minecraft:observer", 11, 70, 12, 2) {
		t.Fatalf("missing pending observer tick for powered=true template at (11,70,12) t=2; got %v", ticks)
	}
	if hasBlockTick(ticks, "minecraft:observer", 11, 70, 14, 2) {
		t.Fatalf("strict /place template scheduled unexpected observer tick at (11,70,14); got %v", ticks)
	}

	step := func() {
		t.Helper()
		c.ChatCommand("tick step 1")
		servertest.RunTicks(t, srv, 1)
	}

	// Step 1: nothing has fired yet.
	step()
	wantBlock(t, srv, 11, 70, 10, "minecraft:observer", map[string]string{"powered": "false"})
	wantBlock(t, srv, 10, 70, 10, "minecraft:redstone_lamp", map[string]string{"lit": "false"})

	// Step 2: observer powers and lights the lamp; sand begins falling.
	step()
	wantBlock(t, srv, 11, 70, 10, "minecraft:observer", map[string]string{"powered": "true"})
	wantBlock(t, srv, 10, 70, 10, "minecraft:redstone_lamp", map[string]string{"lit": "true"})
	wantBlock(t, srv, 14, 72, 10, "minecraft:air", nil)
	wantBlock(t, srv, 11, 70, 12, "minecraft:observer", map[string]string{"powered": "true"})
	wantBlock(t, srv, 10, 70, 12, "minecraft:redstone_lamp", map[string]string{"lit": "true"})
	// Strict placement stays quiet.
	wantBlock(t, srv, 11, 70, 14, "minecraft:observer", map[string]string{"powered": "false"})
	wantBlock(t, srv, 10, 70, 14, "minecraft:redstone_lamp", map[string]string{"lit": "false"})

	// Step 3: observer still powered, lamp still lit.
	step()
	wantBlock(t, srv, 11, 70, 10, "minecraft:observer", map[string]string{"powered": "true"})
	wantBlock(t, srv, 10, 70, 10, "minecraft:redstone_lamp", map[string]string{"lit": "true"})

	// Step 4: observer unpowers; lamp stays lit for 4 more ticks.
	step()
	wantBlock(t, srv, 11, 70, 10, "minecraft:observer", map[string]string{"powered": "false"})
	wantBlock(t, srv, 10, 70, 10, "minecraft:redstone_lamp", map[string]string{"lit": "true"})

	// Steps 5-7: lamp still lit.
	for range 3 {
		step()
		wantBlock(t, srv, 10, 70, 10, "minecraft:redstone_lamp", map[string]string{"lit": "true"})
	}

	// Step 8: lamp turns off.
	step()
	wantBlock(t, srv, 11, 70, 10, "minecraft:observer", map[string]string{"powered": "false"})
	wantBlock(t, srv, 10, 70, 10, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
	c.Disconnect()
}
