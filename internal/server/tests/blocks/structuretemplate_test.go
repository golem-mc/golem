package blocks

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: StructureTemplate.createEntityIgnoreException, EntityType.create
func TestTemplateLoadIgnoresNestedPassengers(t *testing.T) {
	t.Parallel()
	for _, route := range []string{"structure block", "place command"} {
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.LevelName = t.TempDir()
			dir := filepath.Join(cfg.LevelName, "generated", "test", "structure")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			template := nbt.Compound{
				"size":        tplInts(4, 2, 1),
				"palette":     nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"Name": nbt.String("minecraft:air")}}},
				"blocks":      nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{tplBlock(0, 0, nil)}},
				"DataVersion": nbt.Int(4440),
			}
			var entries []nbt.Tag
			for i, kind := range []string{"pig", "oak_boat", "minecart", "chicken"} {
				tag := nbt.Compound{"id": nbt.String("minecraft:" + kind), "NoAI": nbt.Byte(1), "NoGravity": nbt.Byte(1)}
				if kind != "chicken" {
					tag["Passengers"] = nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
						nbt.Compound{"id": nbt.String("minecraft:chicken")},
					}}
				}
				entries = append(entries, nbt.Compound{
					"pos":      nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(float64(i) + 0.5), nbt.Double(1), nbt.Double(0.5)}},
					"blockPos": tplInts(int32(i), 1, 0),
					"nbt":      tag,
				})
			}
			template["entities"] = nbt.List{Elem: nbt.TypeCompound, Items: entries}
			raw, err := nbt.Encode("", template)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "riders.nbt"), raw, 0o644); err != nil {
				t.Fatal(err)
			}
			srv, c := servertest.Join(t, cfg, "Actor", servertest.OpsListing(t, "Actor"))
			servertest.HoldTicks(t, srv)
			for _, x := range []int32{30, 50} {
				servertest.Setblock(t, c, x, 64, 10, "minecraft:structure_block[mode=load]")
				if route == "structure block" {
					c.ProgramStructureBlock(servertest.StructureBlockPacket{
						X: x, Y: 64, Z: 10, Action: servertest.StructureActionLoad, Mode: servertest.StructureModeLoad,
						Name: "test:riders", Offset: [3]int8{0, 1, 0}, Size: [3]int8{4, 2, 1}, Integrity: 1,
					})
				} else {
					c.ChatCommand(fmt.Sprintf("place template test:riders %d 65 10", x))
				}
				counts := make(map[int32]int)
				c.SettleAndCollect(func(id int32, data []byte) {
					switch id {
					case protocol.IDPlaySpawnEntity:
						r := protocol.NewReader(data)
						r.ReadVarInt()
						r.ReadUUID()
						counts[r.ReadVarInt()]++
					case protocol.IDPlaySetPassengers:
						_, riders := servertest.ReadPassengers(c, data)
						if len(riders) != 0 {
							t.Errorf("template loaded passengers %v", riders)
						}
					}
				})
				for _, typ := range []int32{protocol.EntityTypePig, protocol.EntityTypeOakBoat, protocol.EntityTypeMinecart, protocol.EntityTypeChicken} {
					if counts[typ] != 1 {
						t.Errorf("load at x=%d spawned %d of type %d, want one top-level entity", x, counts[typ], typ)
					}
				}
			}
		})
	}
}

func tplInts(v ...int32) nbt.List {
	l := nbt.List{Elem: nbt.TypeInt}
	for _, n := range v {
		l.Items = append(l.Items, nbt.Int(n))
	}
	return l
}

func tplBlock(state, x int32, tag nbt.Compound) nbt.Tag {
	b := nbt.Compound{"pos": tplInts(x, 0, 0), "state": nbt.Int(state)}
	if tag != nil {
		b["nbt"] = tag
	}
	return b
}

// markerTemplate is a 4x1x1 template of stone, a DATA structure block, a
// jigsaw and a test block, with an armor stand above the stone.
func markerTemplate() nbt.Compound {
	entry := func(name string) nbt.Tag { return nbt.Compound{"Name": nbt.String(name)} }
	dataBlock := nbt.Compound{"Name": nbt.String("minecraft:structure_block"), "Properties": nbt.Compound{"mode": nbt.String("data")}}
	stand := nbt.Compound{
		"pos":      nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(0.25), nbt.Double(0), nbt.Double(0.75)}},
		"blockPos": tplInts(0, 0, 0),
		"nbt":      nbt.Compound{"id": nbt.String("minecraft:armor_stand")},
	}
	return nbt.Compound{
		"size":    tplInts(4, 1, 1),
		"palette": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{entry("minecraft:stone"), dataBlock, entry("minecraft:jigsaw"), entry("minecraft:test_block")}},
		"blocks": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			tplBlock(0, 0, nil),
			tplBlock(1, 1, nbt.Compound{"id": nbt.String("minecraft:structure_block"), "mode": nbt.String("DATA"), "metadata": nbt.String("Chest")}),
			tplBlock(2, 2, nbt.Compound{"id": nbt.String("minecraft:jigsaw"), "name": nbt.String("test:a"), "target": nbt.String("test:b"), "pool": nbt.String("test:pool")}),
			tplBlock(3, 3, nbt.Compound{"id": nbt.String("minecraft:test_block"), "message": nbt.String("hello")}),
		}},
		"entities":    nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{stand}},
		"DataVersion": nbt.Int(4440),
	}
}

func markerScene(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	dir := filepath.Join(cfg.LevelName, "generated", "test", "structure")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := nbt.Encode("", markerTemplate())
	if err != nil {
		t.Fatal(err)
	}
	var zipped bytes.Buffer
	zw := gzip.NewWriter(&zipped)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	if err := os.WriteFile(filepath.Join(dir, "markers.nbt"), zipped.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, c := servertest.Join(t, cfg, "Actor", servertest.OpsListing(t, "Actor"))
	c.Settle()
	return srv, c
}

func loadMarkers(t *testing.T, c *servertest.Client, x int32, ignoreEntities bool) {
	t.Helper()
	servertest.Setblock(t, c, x, 64, 10, "minecraft:structure_block[mode=load]")
	c.ProgramStructureBlock(servertest.StructureBlockPacket{
		X: x, Y: 64, Z: 10, Action: servertest.StructureActionLoad, Mode: servertest.StructureModeLoad,
		Name: "test:markers", Offset: [3]int8{0, 1, 0}, Size: [3]int8{4, 1, 1}, Integrity: 1,
		IgnoreEntities: ignoreEntities,
	})
	servertest.AwaitCommandFeedback(t, c, "structure_block.load_success", "test:markers")
}

// TestAStructureBlockLoadPlacesTheTemplateEntities: a load block with
// "ignore entities" off stands up the template's entities at their offset
// from the placed origin, and one with it on places none.
// vanilla: StructureTemplate.placeInWorld, StructureTemplate.placeEntities
func TestAStructureBlockLoadPlacesTheTemplateEntities(t *testing.T) {
	t.Parallel()
	_, c := markerScene(t)
	loadMarkers(t, c, 20, true)
	loadMarkers(t, c, 30, false)
	got := c.AwaitSpawn(protocol.EntityTypeArmorStand)
	if got.X != 30.25 || got.Y != 65 || got.Z != 10.75 {
		t.Errorf("armor stand at (%v, %v, %v), want (30.25, 65, 10.75) from the second load only", got.X, got.Y, got.Z)
	}
}

// TestALoadKeepsDataMarkerJigsawAndTestBlocks: a template's DATA structure
// block, jigsaw and test block are placed as they are with their block
// entity data; only world generation turns them into anything else.
// vanilla: StructureTemplate.placeInWorld
func TestALoadKeepsDataMarkerJigsawAndTestBlocks(t *testing.T) {
	t.Parallel()
	srv, c := markerScene(t)
	loadMarkers(t, c, 30, true)
	servertest.ExpectBlockNamed(t, srv, 31, 65, 10, "minecraft:structure_block", map[string]string{"mode": "data"})
	servertest.ExpectBlockNamed(t, srv, 32, 65, 10, "minecraft:jigsaw", nil)
	servertest.ExpectBlockNamed(t, srv, 33, 65, 10, "minecraft:test_block", nil)
	for _, want := range []struct {
		x         int32
		key, text string
	}{{31, "metadata", "Chest"}, {32, "pool", "test:pool"}, {33, "message", "hello"}} {
		tag, ok := srv.BlockEntityTagAt(want.x, 65, 10)
		if got, _ := nbt.Get[nbt.String](tag, want.key); !ok || string(got) != want.text {
			t.Errorf("block entity at x=%d: %s = %q (tag %v), want %q", want.x, want.key, got, tag, want.text)
		}
	}
}

// TestAPoweredLoadPlacesTheTemplateEntitiesTurned: a lever-powered load block
// stands the template's armor stand up at its offset position, turned by the
// block's rotation or mirror about the origin block's corner.
// vanilla: StructureBlock.trigger, StructureTemplate.transform(Vec3, ...)
func TestAPoweredLoadPlacesTheTemplateEntitiesTurned(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name             string
		mirror, rotation int32
		dx, dz           float64
	}{
		{"plain", 0, 0, 0.25, 0.75},
		{"clockwise 90", 0, 1, 0.25, 0.25},
		{"mirror front-back", 2, 0, 0.75, 0.75},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := markerScene(t)
			servertest.Setblock(t, c, 30, 64, 10, "minecraft:structure_block[mode=load]")
			c.ProgramStructureBlock(servertest.StructureBlockPacket{
				X: 30, Y: 64, Z: 10, Action: servertest.StructureActionUpdate, Mode: servertest.StructureModeLoad,
				Name: "test:markers", Offset: [3]int8{0, 2, 0}, Size: [3]int8{4, 1, 1}, Integrity: 1,
				Mirror: tc.mirror, Rotation: tc.rotation,
			})
			c.Settle()
			servertest.Setblock(t, c, 30, 65, 10, "minecraft:lever[face=floor,facing=north]")
			c.MoveToGround(28.5, 64, 8.5, true)
			c.Settle()
			c.UseBlock(30, 65, 10)
			servertest.RunTicks(t, srv, 4)
			got := c.AwaitSpawn(protocol.EntityTypeArmorStand)
			if got.X != 30+tc.dx || got.Y != 66 || got.Z != 10+tc.dz {
				t.Errorf("armor stand at (%v, %v, %v), want (%v, 66, %v)", got.X, got.Y, got.Z, 30+tc.dx, 10+tc.dz)
			}
		})
	}
}

// saveWithEntities saves the 3x3x3 box at (11,64,10) from a save block at
// (10,64,10) as test:stands.
func saveWithEntities(t *testing.T, c *servertest.Client, ignoreEntities bool) {
	t.Helper()
	c.ProgramStructureBlock(servertest.StructureBlockPacket{
		X: 10, Y: 64, Z: 10, Action: servertest.StructureActionSave, Mode: servertest.StructureModeSave,
		Name: "test:stands", Offset: [3]int8{1, 0, 0}, Size: [3]int8{3, 3, 3}, Integrity: 1,
		IgnoreEntities: ignoreEntities,
	})
	servertest.AwaitCommandFeedback(t, c, "structure_block.save_success", "test:stands")
}

// TestAStructureBlockSaveCapturesTheEntitiesInTheBox: a save block with
// "ignore entities" off stores the box's entities (not the player standing
// in it, nor an entity outside) relative to the box origin with its UUID, one
// with it on stores none, and a load block places the saved entity again.
// vanilla: StructureTemplate.fillEntityList
func TestAStructureBlockSaveCapturesTheEntitiesInTheBox(t *testing.T) {
	t.Parallel()
	_, c, dir := structureScene(t)
	c.MoveToGround(12.5, 64, 11.5, true)
	c.ChatCommand("summon minecraft:armor_stand 12.5 65 11.5 {NoGravity:1b}")
	c.AwaitSpawn(protocol.EntityTypeArmorStand)
	c.ChatCommand("summon minecraft:armor_stand 20.5 65 11.5 {NoGravity:1b}")
	c.AwaitSpawn(protocol.EntityTypeArmorStand)

	servertest.Setblock(t, c, 10, 64, 10, "minecraft:structure_block[mode=save]")
	saveWithEntities(t, c, true)
	read := func() *world.Template {
		data, err := os.ReadFile(filepath.Join(dir, "generated", "test", "structure", "stands.nbt"))
		if err != nil {
			t.Fatal(err)
		}
		tpl, err := world.ParseTemplate(data)
		if err != nil {
			t.Fatal(err)
		}
		return tpl
	}
	if n := len(read().Entities); n != 0 {
		t.Fatalf("ignore entities saved %d entities, want 0", n)
	}

	saveWithEntities(t, c, false)
	tpl := read()
	if len(tpl.Entities) != 1 {
		t.Fatalf("saved %d entities, want only the armor stand in the box", len(tpl.Entities))
	}
	e := tpl.Entities[0]
	if id, _ := nbt.Get[nbt.String](e.NBT, "id"); id != "minecraft:armor_stand" {
		t.Errorf("entity id = %q", id)
	}
	if e.Pos != [3]float64{1.5, 1, 1.5} || e.BlockPos != [3]int32{1, 1, 1} {
		t.Errorf("entity pos %v blockPos %v, want (1.5,1,1.5) and (1,1,1) from the box origin", e.Pos, e.BlockPos)
	}
	if _, has := e.NBT["UUID"]; !has {
		t.Error("saved entity lost its UUID, vanilla keeps it in the template")
	}

	servertest.Setblock(t, c, 30, 64, 10, "minecraft:structure_block[mode=load]")
	c.ProgramStructureBlock(servertest.StructureBlockPacket{
		X: 30, Y: 64, Z: 10, Action: servertest.StructureActionLoad, Mode: servertest.StructureModeLoad,
		Name: "test:stands", Offset: [3]int8{1, 0, 0}, Size: [3]int8{3, 3, 3}, Integrity: 1,
	})
	servertest.AwaitCommandFeedback(t, c, "structure_block.load_success", "test:stands")
	got := c.AwaitSpawn(protocol.EntityTypeArmorStand)
	if got.X != 32.5 || got.Y != 65 || got.Z != 11.5 {
		t.Errorf("loaded stand at (%v, %v, %v), want (32.5, 65, 11.5)", got.X, got.Y, got.Z)
	}
}

// TestAStructureBlockSaveNestsRidersUnderTheirMount: a mount saved from the
// box carries its rider under Passengers, with an empty passenger-root entry.
// vanilla: StructureTemplate.fillEntityList, Entity.save, Entity.saveWithoutId
func TestAStructureBlockSaveNestsRidersUnderTheirMount(t *testing.T) {
	t.Parallel()
	srv, c, dir := structureScene(t)
	c.MoveToGround(12.5, 64, 11.5, true)
	servertest.HoldTicks(t, srv)
	c.ChatCommand("summon minecraft:pig 12.5 64 11.5")
	pig := c.AwaitSpawn(protocol.EntityTypePig)
	c.ChatCommand("summon minecraft:chicken 12.5 64 11.5")
	chicken := c.AwaitSpawn(protocol.EntityTypeChicken)
	c.ChatCommand("ride @e[type=chicken,limit=1] mount @e[type=pig,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")

	servertest.Setblock(t, c, 10, 64, 10, "minecraft:structure_block[mode=save]")
	saveWithEntities(t, c, false)
	data, err := os.ReadFile(filepath.Join(dir, "generated", "test", "structure", "stands.nbt"))
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := world.ParseTemplate(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(tpl.Entities) != 2 {
		t.Fatalf("saved %d entries, want the pig and an empty passenger root", len(tpl.Entities))
	}
	found := false
	empty := 0
	for _, e := range tpl.Entities {
		if len(e.NBT) == 0 {
			empty++
			continue
		}
		if id, _ := nbt.Get[nbt.String](e.NBT, "id"); id != "minecraft:pig" {
			t.Errorf("unexpected top-level entity id %q; mounted passengers must have empty root NBT", id)
			continue
		}
		if id, ok := nbt.UUIDField(e.NBT, "UUID"); !ok || id != pig.Uuid {
			t.Errorf("saved pig UUID = %v, want %v", id, pig.Uuid)
		}
		riders, _ := nbt.Get[nbt.List](e.NBT, "Passengers")
		if len(riders.Items) != 1 {
			continue
		}
		rider, ok := riders.Items[0].(nbt.CompoundTag)
		if !ok {
			t.Fatalf("saved passenger is %T, want a compound", riders.Items[0])
		}
		if id, _ := nbt.Get[nbt.String](rider.Entries(), "id"); id != "minecraft:chicken" {
			t.Errorf("saved passenger id = %q, want minecraft:chicken", id)
		}
		if id, ok := nbt.UUIDField(rider.Entries(), "UUID"); !ok || id != chicken.Uuid {
			t.Errorf("saved passenger UUID = %v, want %v", id, chicken.Uuid)
		}
		found = true
	}
	if !found {
		t.Errorf("pig has no chicken under Passengers in %d saved entities", len(tpl.Entities))
	}
	if empty != 1 {
		t.Errorf("saved %d empty passenger roots, want 1", empty)
	}
}
