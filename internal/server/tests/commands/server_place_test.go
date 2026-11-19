package commands

import (
	"bytes"
	"compress/gzip"
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
