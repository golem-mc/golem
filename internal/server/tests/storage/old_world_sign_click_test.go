package storage

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// oldSignChunk is chunk (0,0) as vanilla 1.21.1 (DataVersion 3955) saves it
// with a stone floor at y=3 and a command sign at (8,4,12).
func oldSignChunk(t *testing.T) []byte {
	t.Helper()
	data := make([]int64, 256)
	for i := 48; i < 64; i++ {
		data[i] = 0x2222222222222222 // stone, 4 bits per block, 16 to a long
	}
	data[(4*256+12*16+8)/16] = 1 << (4 * ((4*256 + 12*16 + 8) % 16))
	name := func(s string) nbt.Tag { return nbt.Compound{"Name": nbt.String(s)} }
	palette := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		name("minecraft:air"),
		nbt.Compound{"Name": nbt.String("minecraft:oak_wall_sign"), "Properties": nbt.Compound{"facing": nbt.String("south"), "waterlogged": nbt.String("false")}},
		name("minecraft:stone"),
	}}
	click := `{"text":"Click","clickEvent":{"action":"run_command","value":"/setblock 8 6 12 minecraft:gold_block"}}`
	empty := nbt.String(`""`)
	messages := func(first nbt.Tag) nbt.Tag {
		return nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{first, empty, empty, empty}}
	}
	sign := nbt.Compound{
		"id": nbt.String("minecraft:sign"), "x": nbt.Int(8), "y": nbt.Int(4), "z": nbt.Int(12),
		"is_waxed":   nbt.Byte(0),
		"front_text": nbt.Compound{"messages": messages(nbt.String(click)), "color": nbt.String("black"), "has_glowing_text": nbt.Byte(0)},
		"back_text":  nbt.Compound{"messages": messages(empty), "color": nbt.String("black"), "has_glowing_text": nbt.Byte(0)},
	}
	root := nbt.Compound{
		"DataVersion": nbt.Int(3955), "Status": nbt.String("minecraft:full"),
		"xPos": nbt.Int(0), "zPos": nbt.Int(0), "yPos": nbt.Int(0),
		"sections": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
			"Y":            nbt.Byte(0),
			"block_states": nbt.Compound{"palette": palette, "data": nbt.LongArray(data)},
		}}},
		"block_entities": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{sign}},
	}
	encoded, err := nbt.Encode("", root)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

// writeRegion0 stores chunk (0,0) alone in the region file at path.
func writeRegion0(t *testing.T, path string, chunk []byte) {
	t.Helper()
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	w.Write(chunk)
	w.Close()
	payload := append(binary.BigEndian.AppendUint32(nil, uint32(z.Len()+1)), 2)
	payload = append(payload, z.Bytes()...)
	sectors := (len(payload) + 4095) / 4096
	file := make([]byte, 8192+sectors*4096)
	binary.BigEndian.PutUint32(file, 2<<8|uint32(sectors))
	copy(file[8192:], payload)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, file, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestACommandSignFromAnOldWorldRunsItsClickEvent loads a 1.21.1 chunk whose
// sign has a run_command click event: the upgrade renames the event and sets
// allow_op_features, so a click runs the command instead of opening the editor.
func TestACommandSignFromAnOldWorldRunsItsClickEvent(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	level := t.TempDir()
	cfg.LevelName = level
	writeRegion0(t, filepath.Join(level, "dimensions", "minecraft", "overworld", "region", "r.0.0.mca"), oldSignChunk(t))

	srv, c := servertest.Join(t, cfg, "Clicker")
	release := srv.HoldChunk(0, 0)
	defer release()
	c.MoveTo(8.5, 4, 13.6, 0, 0)
	c.Settle()
	c.UseBlock(8, 4, 12)
	c.Settle()
	gold := registry.BlockStates["minecraft:gold_block"]
	servertest.UntilTicks(t, srv, 20, "the sign's command to run", func() bool { return srv.BlockAt(8, 6, 12) == gold })
}
