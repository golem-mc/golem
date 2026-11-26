package storage

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server"
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

// chestItems reads a chest's saved stacks as "id xCount [enchantment levels]"
// by slot, the way a vanilla 26.3 server reports them under /data get block.
func chestItems(t *testing.T, srv *server.Server, x, y, z int32) map[int8]string {
	t.Helper()
	chest, ok := srv.BlockEntityTagAt(x, y, z)
	if !ok {
		t.Fatalf("no chest block entity at (%d,%d,%d)", x, y, z)
	}
	items, _ := nbt.Get[nbt.List](chest, "Items")
	got := map[int8]string{}
	for _, item := range items.Items {
		c, _ := nbt.CompoundView(item)
		slot, _ := nbt.Get[nbt.Byte](c, "Slot")
		id, _ := nbt.Get[nbt.String](c, "id")
		count, _ := nbt.IntValue(c["count"])
		text := fmt.Sprintf("%s x%d", id, count)
		if components, ok := nbt.CompoundView(c["components"]); ok {
			text += fmt.Sprintf(" %v", components)
		}
		got[int8(slot)] = text
	}
	return got
}

// A vanilla 26.3 server run over both fixtures (chunks forceloaded, read with
// /data get block) reports these chests, and the stone at (18,y,4):
//
//	(4,y,2):  [{Slot:0b,id:"minecraft:diamond",count:5},{Slot:3b,id:"minecraft:short_grass",count:12}]
//	(18,y,2): [{Slot:0b,id:"minecraft:diamond_sword",count:1,components:{"minecraft:enchantments":{"minecraft:sharpness":5}}}]
var (
	wantPlainChest = map[int8]string{0: "minecraft:diamond x5", 3: "minecraft:short_grass x12"}
	wantSwordChest = map[int8]string{0: "minecraft:diamond_sword x1 map[minecraft:enchantments:map[minecraft:sharpness:5]]"}
)

// TestAVanilla1_18_2WorldLoadsItsChunks loads a region file vanilla 1.18.2
// wrote: the sign keeps its text, colour and glow through the double-sided
// sign upgrade, grass is short grass, and both chests hold what vanilla 26.3
// reads from the file, the enchanted sword included. A restart reads the sign
// the server saved back the same.
func TestAVanilla1_18_2WorldLoadsItsChunks(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName, _ = oldWorld(t, "vanilla-1.18.2-r.0.0.mca")

	check := func(t *testing.T, run string) {
		srv, _ := servertest.Start(t, cfg)
		release0 := srv.HoldChunk(0, 0)
		defer release0()
		release1 := srv.HoldChunk(16, 0)
		defer release1()
		wantSign, _ := registry.FindState("minecraft:oak_sign", map[string]string{"rotation": "0"})
		for _, b := range []struct {
			x, y, z int32
			want    uint16
		}{
			{2, -60, 2, wantSign},
			{6, -60, 2, registry.BlockStates["minecraft:short_grass"]},
			{2, -61, 2, registry.BlockStates["minecraft:grass_block"]},
			{18, -60, 4, registry.BlockStates["minecraft:stone"]},
		} {
			if got := srv.BlockAt(b.x, b.y, b.z); got != b.want {
				t.Errorf("%s: block at (%d,%d,%d) = %d, want %d", run, b.x, b.y, b.z, got, b.want)
			}
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
		if got := chestItems(t, srv, 4, -60, 2); !maps.Equal(got, wantPlainChest) {
			t.Errorf("%s: chest items = %v, want %v", run, got, wantPlainChest)
		}
		if got := chestItems(t, srv, 18, -60, 2); !maps.Equal(got, wantSwordChest) {
			t.Errorf("%s: sword chest items = %v, want %v", run, got, wantSwordChest)
		}

		// An edit makes the server save the chunk it upgraded.
		srv.SetLoadedBlock(8, -60, 8, registry.BlockStates["minecraft:glass"])
		srv.SetLoadedBlock(24, -60, 8, registry.BlockStates["minecraft:glass"])
		srv.Shutdown()
	}
	check(t, "first load")
	check(t, "after a restart")
}

// TestAVanilla1_16_5WorldLoadsItsChunks loads a region file vanilla 1.16.5
// wrote, in the layout before 1.18: the sign keeps its text and colour,
// grass is short grass, both chests hold what vanilla 26.3 reads from the
// file, the ground is where it was and the biome is superflat's plains.
func TestAVanilla1_16_5WorldLoadsItsChunks(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName, _ = oldWorld(t, "vanilla-1.16.5-r.0.0.mca")

	srv, _ := servertest.Start(t, cfg)
	release0 := srv.HoldChunk(0, 0)
	defer release0()
	release1 := srv.HoldChunk(16, 0)
	defer release1()
	wantSign, _ := registry.FindState("minecraft:oak_sign", map[string]string{"rotation": "0"})
	for _, b := range []struct {
		x, y, z int32
		want    uint16
	}{
		{2, 4, 2, wantSign},
		{6, 4, 2, registry.BlockStates["minecraft:short_grass"]},
		{2, 3, 2, registry.BlockStates["minecraft:grass_block"]},
		{2, 0, 2, registry.BlockStates["minecraft:bedrock"]},
		{18, 4, 4, registry.BlockStates["minecraft:stone"]},
	} {
		if got := srv.BlockAt(b.x, b.y, b.z); got != b.want {
			t.Errorf("block at (%d,%d,%d) = %d, want %d", b.x, b.y, b.z, got, b.want)
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
	if got := chestItems(t, srv, 4, 4, 2); !maps.Equal(got, wantPlainChest) {
		t.Errorf("chest items = %v, want %v", got, wantPlainChest)
	}
	if got := chestItems(t, srv, 18, 4, 2); !maps.Equal(got, wantSwordChest) {
		t.Errorf("sword chest items = %v, want %v", got, wantSwordChest)
	}
	if b := srv.OverworldBiomeAt(2, 4, 2); b != "minecraft:plains" {
		t.Errorf("biome = %q, want minecraft:plains", b)
	}
}
