package advancements

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// TestStructureLocationAdvancements loads imported starts whose pieces reach
// across a chunk boundary. Gaps between pieces must not award the advancement.
// vanilla: LocationPredicate.matches, StructureManager.structureHasPieceAt
func TestStructureLocationAdvancements(t *testing.T) {
	cases := []struct{ structure, advancement, criterion string }{
		{"fortress", "minecraft:nether/find_fortress", "fortress"},
		{"bastion_remnant", "minecraft:nether/find_bastion", "bastion"},
		{"stronghold", "minecraft:story/follow_ender_eye", "in_stronghold"},
		{"end_city", "minecraft:end/find_end_city", "in_city"},
		{"ancient_city", "test:ancient_city", "in_city"},
		{"trial_chambers", "minecraft:adventure/minecraft_trials_edition", "minecraft_trials_edition"},
	}
	cfg, data := packConfig(t)
	writePackFile(t, filepath.Join(data, "test", "advancement", "ancient_city.json"), `{"criteria":{"in_city":{
		"trigger":"minecraft:location","conditions":{"player":[{"condition":"minecraft:entity_properties","entity":"this",
		"predicate":{"minecraft:location":{"structures":"minecraft:ancient_city"}}}]}}}}`)
	starts, refs := nbt.Compound{}, nbt.Compound{}
	for i, tc := range cases {
		id, z := "minecraft:"+tc.structure, int32(i*3)
		starts[id] = nbt.Compound{
			"id": nbt.String(id), "ChunkX": nbt.Int(-1), "ChunkZ": nbt.Int(0),
			"Children": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				nbt.Compound{"BB": nbt.IntArray{-4, 64, z, 2, 66, z + 1}},
				nbt.Compound{"BB": nbt.IntArray{10, 64, z, 12, 66, z + 1}},
			}},
		}
		refs[id] = nbt.LongArray{int64(uint32(0xffffffff))} // start chunk (-1, 0)
	}
	plantStructureChunk(t, cfg.LevelName, world.ChunkPos{X: -1}, nbt.Compound{"starts": starts})
	plantStructureChunk(t, cfg.LevelName, world.ChunkPos{}, nbt.Compound{"References": refs})
	srv, connect := servertest.Start(t, cfg)
	servertest.FreezeUntilHeld(t, srv)
	c, _ := join(t, connect, "Explorer")
	servertest.HoldTicks(t, srv)
	for i, tc := range cases {
		t.Run(tc.structure, func(t *testing.T) {
			obtained := func() bool { return srv.AdvancementObtained("Explorer", tc.advancement, tc.criterion) }
			c.StandAt(6.5, 64, float64(i*3)+0.5, 0)
			c.Settle()
			servertest.RunTicks(t, srv, 20)
			if obtained() {
				t.Fatal("standing in the gap between pieces obtained the criterion")
			}
			c.StandAt(2.99, 64, float64(i*3)+0.5, 0)
			c.Settle()
			servertest.RunTicks(t, srv, 20)
			if !obtained() {
				t.Fatal("entering the inclusive edge of a piece did not obtain the criterion")
			}
		})
	}
}

// plantStructureChunk writes a minimal full chunk with a floor at y=63 and
// imported structure records, before the server opens its region files.
func plantStructureChunk(t *testing.T, level string, pos world.ChunkPos, structures nbt.Compound) {
	t.Helper()
	root := nbt.Compound{
		"DataVersion": nbt.Int(4903), "Status": nbt.String("minecraft:full"),
		"xPos": nbt.Int(pos.X), "zPos": nbt.Int(pos.Z), "structures": structures,
		"sections": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
			"Y": nbt.Byte(3),
			"block_states": nbt.Compound{"palette": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				nbt.Compound{"Name": nbt.String("minecraft:stone")},
			}}},
		}}},
	}
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(servertest.MustEncodeNBT(t, root)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	payload := compressed.Bytes()
	sectors := (5 + len(payload) + 4095) / 4096
	raw := make([]byte, 8192+sectors*4096)
	index := int((pos.X&31)+(pos.Z&31)*32) * 4
	binary.BigEndian.PutUint32(raw[index:], uint32(2<<8|sectors))
	binary.BigEndian.PutUint32(raw[8192:], uint32(len(payload)+1))
	raw[8196] = 2
	copy(raw[8197:], payload)
	path := servertest.ChunkRegionPath(level, pos)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}
