package worldgen

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// packTemplate is a 3x1x3 slab of gold blocks as a structure file.
func packTemplate(t *testing.T) []byte {
	t.Helper()
	ints := func(v ...int32) nbt.List {
		l := nbt.List{Elem: nbt.TypeInt}
		for _, n := range v {
			l.Items = append(l.Items, nbt.Int(n))
		}
		return l
	}
	var blocks []nbt.Tag
	for x := range int32(3) {
		for z := range int32(3) {
			blocks = append(blocks, nbt.Compound{"pos": ints(x, 0, z), "state": nbt.Int(0)})
		}
	}
	raw, err := nbt.Encode("", nbt.Compound{
		"size":        ints(3, 1, 3),
		"palette":     nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"Name": nbt.String("minecraft:gold_block")}}},
		"blocks":      nbt.List{Elem: nbt.TypeCompound, Items: blocks},
		"DataVersion": nbt.Int(4440),
	})
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
	return zipped.Bytes()
}

// TestPackJigsawStructureGenerates starts an overworld with a data pack that
// defines a jigsaw structure with a one-template start pool and a
// random_spread set of spacing 4 and separation 2, then checks /locate finds
// it, its template stands in the located chunk and /place structure builds it.
// The pack tables are process-wide, so the test is not parallel.
func TestPackJigsawStructureGenerates(t *testing.T) {
	t.Cleanup(func() { world.ApplyPackWorldgen(world.PackWorldgen{}) })
	servertest.LimitGeneration(t)
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	cfg.LevelName = t.TempDir()
	cfg.GenerateStructures = true
	cfg.LevelType = "normal"
	cfg.LevelSeed = "12345"
	data := filepath.Join(cfg.LevelName, "datapacks", "structures")
	for file, body := range map[string][]byte{
		"pack.mcmeta": []byte(`{"pack":{"description":"structures"}}`),
		"data/test/worldgen/structure/slab.json": []byte(`{
			"type": "minecraft:jigsaw", "biomes": "#minecraft:is_overworld", "step": "surface_structures",
			"spawn_overrides": {}, "terrain_adaptation": "none", "start_pool": "test:start", "size": 1,
			"start_height": {"absolute": 100}, "max_distance_from_center": 80}`),
		"data/test/worldgen/structure_set/slab.json": []byte(`{
			"structures": [{"structure": "test:slab", "weight": 1}],
			"placement": {"type": "minecraft:random_spread", "spacing": 4, "separation": 2, "salt": 1234}}`),
		"data/test/worldgen/template_pool/start.json": []byte(`{
			"fallback": "minecraft:empty",
			"elements": [{"weight": 1, "element": {"element_type": "minecraft:single_pool_element",
				"location": "test:tiny", "processors": {"processors": []}, "projection": "rigid"}}]}`),
		"data/test/structure/tiny.nbt": packTemplate(t),
	} {
		path := filepath.Join(data, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv, c := servertest.Join(t, cfg, "Builder", servertest.OpsListing(t, "Builder"))
	servertest.AwaitChunksSettled(t, srv)
	srv.StopTick()

	// The chunk the player stands in is loaded; (1, 1) is not a start, so
	// the slab appears only when placed.
	if got := goldAround(srv, 32, 32); got != 0 {
		t.Fatalf("%d gold blocks around (32, 32) before /place structure, want 0", got)
	}
	c.ChatCommand("forceload add 16 16 48 48")
	srv.StartTick()
	servertest.UntilTicks(t, srv, 200, "the chunks around (32, 32) to load", func() bool {
		return srv.ChunkLoadedInDimension("minecraft:overworld", 16, 16) && srv.ChunkLoadedInDimension("minecraft:overworld", 48, 48)
	})
	c.ChatCommand("place structure test:slab 40 70 40")
	servertest.AwaitCommandFeedbackText(t, c, "commands.place.structure.success",
		protocol.Text{Text: "test:slab"}, num(40), num(70), num(40))
	if got := goldAround(srv, 32, 32); got != 9 {
		t.Errorf("%d gold blocks around (32, 32) after /place structure, want 9", got)
	}
	srv.StopTick()

	c.ChatCommand("locate structure test:slab")
	tp := regexp.MustCompile(`/tp @s (-?\d+) ~ (-?\d+)`)
	var x, z int32
	if !c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		m := tp.FindSubmatch(data)
		if id != protocol.IDPlaySystemChat || m == nil {
			return false
		}
		px, _ := strconv.Atoi(string(m[1]))
		pz, _ := strconv.Atoi(string(m[2]))
		x, z = int32(px), int32(pz)
		return true
	}) {
		t.Fatal("/locate structure test:slab sent no coordinates")
	}

	// Generating the located chunk places the slab at its start.
	c.ChatCommand("tp @s " + strconv.Itoa(int(x)) + " 120 " + strconv.Itoa(int(z)))
	srv.StartTick()
	servertest.UntilTicks(t, srv, 300, "the located structure's slab to generate", func() bool {
		return goldAround(srv, x, z) == 9
	})
}

// goldAround counts the gold blocks at y 99 within 4 blocks of (x, z).
func goldAround(srv *server.Server, x, z int32) int {
	n := 0
	for dx := int32(-4); dx <= 4; dx++ {
		for dz := int32(-4); dz <= 4; dz++ {
			if name, _ := servertest.StateProps(srv.BlockAt(x+dx, 99, z+dz)); name == "minecraft:gold_block" {
				n++
			}
		}
	}
	return n
}

func num(v int64) protocol.Text { return protocol.Text{Number: &v} }
