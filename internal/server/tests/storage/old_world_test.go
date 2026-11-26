package storage

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// oldWorld copies a region file a vanilla server of an older version wrote
// into a fresh world's root, where that version kept the overworld's
// regions, and returns the world's directory and the copied file's path once
// the server has moved it.
//
// The fixtures hold chunks (0,0) and (1,0) of a superflat world with these
// commands run, Y being -60 for 1.18.2 and 4 for 1.16.5:
//
//	setblock 2 Y 2 oak_sign[rotation=0]{Text1:'{"text":"Hello"}',Text2:'{"text":"golem","color":"red"}',Color:"blue",GlowingText:1b}
//	setblock 4 Y 2 chest[facing=north]{Items:[{Slot:0b,id:"minecraft:diamond",Count:5b},{Slot:3b,id:"minecraft:grass",Count:12b}]}
//	setblock 6 Y 2 grass
//	setblock 18 Y 2 chest[facing=north]{Items:[{Slot:0b,id:"minecraft:diamond_sword",Count:1b,tag:{Enchantments:[{id:"minecraft:sharpness",lvl:5s}]}}]}
//	setblock 18 Y 4 stone
func oldWorld(t *testing.T, fixture string) (level, region string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	level = t.TempDir()
	if err := os.MkdirAll(filepath.Join(level, "region"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(level, "region", "r.0.0.mca"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return level, filepath.Join(level, "dimensions", "minecraft", "overworld", "region", "r.0.0.mca")
}

// signFront reads a sign's saved front text: its lines as stored, its dye
// colour and its glow.
func signFront(t *testing.T, tag nbt.Compound) (messages []string, color string, glowing bool) {
	t.Helper()
	front, ok := nbt.Get[nbt.Compound](tag, "front_text")
	if !ok {
		t.Fatalf("sign tag has no front_text: %v", tag)
	}
	lines, _ := nbt.Get[nbt.List](front, "messages")
	for _, line := range lines.Items {
		messages = append(messages, fmt.Sprint(nbt.ListElement(line)))
	}
	c, _ := nbt.Get[nbt.String](front, "color")
	g, _ := nbt.Get[nbt.Byte](front, "has_glowing_text")
	return messages, string(c), g != 0
}

// TestAVanilla1_18_2WorldLoadsItsChunks loads a region file vanilla 1.18.2
// wrote: the sign keeps its text, colour and glow through the double-sided
// sign upgrade, grass is short grass, and the chest of plain items holds them.
// The chunk whose chest holds an enchanted sword, a stack the server cannot
// componentize, stays as stored. A restart reads the sign the server saved
// back the same.
func TestAVanilla1_18_2WorldLoadsItsChunks(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	var region string
	cfg.LevelName, region = oldWorld(t, "vanilla-1.18.2-r.0.0.mca")

	check := func(t *testing.T, run string) {
		srv, _ := servertest.Start(t, cfg)
		release := srv.HoldChunk(0, 0)
		defer release()
		wantSign, _ := registry.FindState("minecraft:oak_sign", map[string]string{"rotation": "0"})
		for _, b := range []struct {
			x    int32
			want uint16
		}{
			{2, wantSign},
			{6, registry.BlockStates["minecraft:short_grass"]},
		} {
			if got := srv.BlockAt(b.x, -60, 2); got != b.want {
				t.Errorf("%s: block at (%d,-60,2) = %d, want %d", run, b.x, got, b.want)
			}
		}
		if got := srv.BlockAt(2, -61, 2); got != registry.BlockStates["minecraft:grass_block"] {
			t.Errorf("%s: ground at (2,-61,2) = %d, want the saved grass block", run, got)
		}

		sign, ok := srv.BlockEntityTagAt(2, -60, 2)
		if !ok {
			t.Fatalf("%s: no sign block entity", run)
		}
		messages, color, glowing := signFront(t, sign)
		// The lines are text components in memory; the file keeps them JSON.
		want := []string{"Hello", "map[color:red text:golem]", "", ""}
		if !slices.Equal(messages, want) {
			t.Errorf("%s: front messages = %q, want %q", run, messages, want)
		}
		if color != "blue" || !glowing {
			t.Errorf("%s: front colour %q glowing %v, want blue and glowing", run, color, glowing)
		}

		chest, ok := srv.BlockEntityTagAt(4, -60, 2)
		if !ok {
			t.Fatalf("%s: no chest block entity", run)
		}
		items, _ := nbt.Get[nbt.List](chest, "Items")
		got := map[int8]string{}
		for _, item := range items.Items {
			c, _ := nbt.CompoundView(item)
			slot, _ := nbt.Get[nbt.Byte](c, "Slot")
			id, _ := nbt.Get[nbt.String](c, "id")
			count, _ := nbt.IntValue(c["count"])
			got[int8(slot)] = fmt.Sprintf("%s x%d", id, count)
		}
		wantItems := map[int8]string{0: "minecraft:diamond x5", 3: "minecraft:short_grass x12"}
		if !maps.Equal(got, wantItems) {
			t.Errorf("%s: chest items = %v, want %v", run, got, wantItems)
		}

		// An edit makes the server save the chunk it upgraded.
		srv.SetLoadedBlock(8, -60, 8, registry.BlockStates["minecraft:glass"])
		srv.Shutdown()
	}
	check(t, "first load")
	check(t, "after a restart")

	// Chunk (1,0), index 1 of the region, keeps its header entry and sectors.
	fixture, err := os.ReadFile(filepath.Join("testdata", "vanilla-1.18.2-r.0.0.mca"))
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(region)
	if err != nil {
		t.Fatal(err)
	}
	entry := binary.BigEndian.Uint32(fixture[4:8])
	from, to := int(entry>>8)*4096, int(entry>>8+entry&0xFF)*4096
	if !bytes.Equal(after[4:8], fixture[4:8]) || len(after) < to || !bytes.Equal(after[from:to], fixture[from:to]) {
		t.Error("the chunk the server cannot upgrade changed on disk")
	}
}

// TestAVanilla1_16_5WorldLoadsItsChunks loads a region file vanilla 1.16.5
// wrote, in the layout before 1.18: the sign keeps its text and colour,
// grass is short grass, the chest of plain items holds them, the ground is
// where it was and the biome is superflat's plains. Chunk (1,0), whose chest
// holds an enchanted sword, stays as stored.
func TestAVanilla1_16_5WorldLoadsItsChunks(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	var region string
	cfg.LevelName, region = oldWorld(t, "vanilla-1.16.5-r.0.0.mca")

	srv, _ := servertest.Start(t, cfg)
	release := srv.HoldChunk(0, 0)
	wantSign, _ := registry.FindState("minecraft:oak_sign", map[string]string{"rotation": "0"})
	for _, b := range []struct {
		x, y int32
		want uint16
	}{
		{2, 4, wantSign},
		{6, 4, registry.BlockStates["minecraft:short_grass"]},
		{2, 3, registry.BlockStates["minecraft:grass_block"]},
		{2, 0, registry.BlockStates["minecraft:bedrock"]},
	} {
		if got := srv.BlockAt(b.x, b.y, 2); got != b.want {
			t.Errorf("block at (%d,%d,2) = %d, want %d", b.x, b.y, got, b.want)
		}
	}
	sign, ok := srv.BlockEntityTagAt(2, 4, 2)
	if !ok {
		t.Fatal("no sign block entity")
	}
	messages, color, glowing := signFront(t, sign)
	want := []string{"Hello", "map[color:red text:golem]", "", ""}
	if !slices.Equal(messages, want) {
		t.Errorf("front messages = %q, want %q", messages, want)
	}
	// 1.16.5 has no glowing text: the fixture's GlowingText tag was never stored.
	if color != "blue" || glowing {
		t.Errorf("front colour %q glowing %v, want blue and not glowing", color, glowing)
	}
	chest, ok := srv.BlockEntityTagAt(4, 4, 2)
	if !ok {
		t.Fatal("no chest block entity")
	}
	items, _ := nbt.Get[nbt.List](chest, "Items")
	got := map[int8]string{}
	for _, item := range items.Items {
		c, _ := nbt.CompoundView(item)
		slot, _ := nbt.Get[nbt.Byte](c, "Slot")
		id, _ := nbt.Get[nbt.String](c, "id")
		count, _ := nbt.IntValue(c["count"])
		got[int8(slot)] = fmt.Sprintf("%s x%d", id, count)
	}
	wantItems := map[int8]string{0: "minecraft:diamond x5", 3: "minecraft:short_grass x12"}
	if !maps.Equal(got, wantItems) {
		t.Errorf("chest items = %v, want %v", got, wantItems)
	}
	if b := srv.OverworldBiomeAt(2, 4, 2); b != "minecraft:plains" {
		t.Errorf("biome = %q, want minecraft:plains", b)
	}

	srv.SetLoadedBlock(8, 4, 8, registry.BlockStates["minecraft:glass"])
	release()
	srv.Shutdown()

	fixture, err := os.ReadFile(filepath.Join("testdata", "vanilla-1.16.5-r.0.0.mca"))
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(region)
	if err != nil {
		t.Fatal(err)
	}
	entry := binary.BigEndian.Uint32(fixture[4:8])
	from, to := int(entry>>8)*4096, int(entry>>8+entry&0xFF)*4096
	if !bytes.Equal(after[4:8], fixture[4:8]) || len(after) < to || !bytes.Equal(after[from:to], fixture[from:to]) {
		t.Error("the chunk the server cannot upgrade changed on disk")
	}
}
