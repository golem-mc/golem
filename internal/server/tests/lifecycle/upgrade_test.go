package lifecycle

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestForceUpgradeRewritesOldChunks plants a 1.21.4 chunk (DataVersion 4189)
// with a chain, cached heightmaps and section light, starts the server with
// --forceUpgrade --eraseCache, and finds the stored chunk at the current
// DataVersion under the new block name with no Heightmaps and no BlockLight.
// vanilla: WorldUpgrader.work
func TestForceUpgradeRewritesOldChunks(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, _ := servertest.Start(t, cfg)
	srv.SetLoadedBlock(5, 64, 5, registry.BlockStates["minecraft:torch"])
	srv.Shutdown()

	pos := world.ChunkPos{}
	path := servertest.ChunkRegionPath(cfg.LevelName, pos)
	servertest.EditChunkRecord(t, path, pos, func(record []byte) []byte {
		zr, err := zlib.NewReader(bytes.NewReader(record[5:]))
		if err != nil {
			t.Fatalf("decompress chunk: %v", err)
		}
		raw, err := io.ReadAll(zr)
		if err != nil {
			t.Fatalf("read chunk: %v", err)
		}
		_, tag, err := nbt.Decode(raw)
		if err != nil {
			t.Fatalf("decode chunk: %v", err)
		}
		root := tag.(nbt.Compound)
		root["DataVersion"] = nbt.Int(4189)
		root["Heightmaps"] = nbt.Compound{"WORLD_SURFACE": nbt.LongArray{1, 2, 3}}
		root["isLightOn"] = nbt.Byte(1)
		sections, _ := nbt.Get[nbt.List](root, "sections")
		for i, item := range sections.Items {
			section := item.(nbt.Compound)
			section["BlockLight"] = nbt.ByteArray(make([]byte, 2048))
			section["SkyLight"] = nbt.ByteArray(make([]byte, 2048))
			if y, _ := nbt.Get[nbt.Byte](section, "Y"); y == 4 {
				section["block_states"] = nbt.Compound{
					"palette": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
						nbt.Compound{"Name": nbt.String("minecraft:chain"), "Properties": nbt.Compound{
							"axis": nbt.String("y"), "waterlogged": nbt.String("false"),
						}},
					}},
				}
			}
			sections.Items[i] = section
		}
		out, err := nbt.Encode("", root)
		if err != nil {
			t.Fatalf("encode chunk: %v", err)
		}
		var buf bytes.Buffer
		zw := zlib.NewWriter(&buf)
		_, _ = zw.Write(out)
		_ = zw.Close()
		newRecord := make([]byte, 5+buf.Len())
		binary.BigEndian.PutUint32(newRecord, uint32(1+buf.Len()))
		newRecord[4] = record[4]
		copy(newRecord[5:], buf.Bytes())
		return newRecord
	})

	for _, recreate := range []bool{false, true} {
		cfg.ForceUpgrade, cfg.EraseCache, cfg.RecreateRegionFiles = true, true, recreate
		again, _ := servertest.Start(t, cfg)
		again.Shutdown()
	}

	cfg.ForceUpgrade, cfg.EraseCache, cfg.RecreateRegionFiles = false, false, false
	root := servertest.ReadBlockChunkNBT(t, path, pos)
	if version, _ := nbt.Get[nbt.Int](root, "DataVersion"); int32(version) != world.CurrentDataVersion {
		t.Errorf("DataVersion = %d, want %d", version, world.CurrentDataVersion)
	}
	if hm, _ := nbt.Get[nbt.Compound](root, "Heightmaps"); len(hm) != 0 {
		t.Errorf("Heightmaps = %v, want none", hm)
	}
	if _, ok := root["isLightOn"]; ok {
		t.Error("isLightOn was kept")
	}
	sections, _ := nbt.Get[nbt.List](root, "sections")
	chains := 0
	for _, item := range sections.Items {
		section := item.(nbt.Compound)
		for _, key := range []string{"BlockLight", "SkyLight"} {
			if _, ok := section[key]; ok {
				t.Errorf("a section kept its %s", key)
			}
		}
		bs, _ := nbt.Get[nbt.Compound](section, "block_states")
		palette, _ := nbt.Get[nbt.List](bs, "palette")
		for _, e := range palette.Items {
			// The writer keeps a property-less entry as a bare string.
			entry, ok := e.(nbt.Compound)
			if !ok {
				continue
			}
			name, _ := nbt.Get[nbt.String](entry, "Name")
			if id, _ := nbt.Get[nbt.String](entry, "id"); name == "minecraft:iron_chain" || id == "minecraft:iron_chain" {
				chains++
			}
		}
	}
	if chains != 1 {
		t.Errorf("%d sections hold an iron_chain palette entry, want 1", chains)
	}
}
