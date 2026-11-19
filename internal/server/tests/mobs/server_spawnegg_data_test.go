package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// eggWithData is a spawn egg whose entity_data names typeName and carries data.
func eggWithData(t *testing.T, egg, typeName string, data nbt.Compound) protocol.ItemStack {
	t.Helper()
	typ, ok := registry.EntityTypeID(typeName)
	if !ok {
		t.Fatalf("no entity type %s", typeName)
	}
	tag, err := nbt.EncodeNetwork(data)
	if err != nil {
		t.Fatal(err)
	}
	var b protocol.Buffer
	b.PutVarInt(typ)
	b.PutRaw(tag)
	stack := servertest.ItemStack(t, egg, 2)
	stack.Raw = []protocol.RawComponent{{Type: protocol.DataComponentEntityData, Data: b.Bytes()}}
	return stack
}

func dataOf(t *testing.T, srv *server.Server, typeName string) nbt.Compound {
	t.Helper()
	found := srv.SelectedEntities(typeName)
	if len(found) != 1 {
		t.Fatalf("%d %s, want 1", len(found), typeName)
	}
	tag, ok := found[0].(command.DataTarget).DataTag()
	if !ok {
		t.Fatalf("%s has no data tag", typeName)
	}
	return tag.Entries()
}

// TestASpawnEggAppliesItsEntityData: the egg's entity_data is loaded into the
// mob, and its id picks the kind over the egg's own.
// vanilla: EntityType.updateCustomEntityTag, SpawnEggItem.getType
func TestASpawnEggAppliesItsEntityData(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, egg, typeName, want string
		data                      nbt.Compound
		check                     func(*testing.T, nbt.Compound)
	}{
		{"NoAI and Tags", "minecraft:zombie_spawn_egg", "minecraft:zombie", "minecraft:zombie",
			nbt.Compound{"id": nbt.String("minecraft:zombie"), "NoAI": nbt.Byte(1),
				"Tags": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("eggy")}}},
			func(t *testing.T, tag nbt.Compound) {
				if !nbt.BoolField(tag, "NoAI") {
					t.Errorf("NoAI missing: %v", tag)
				}
				if tags, _ := nbt.Get[nbt.List](tag, "Tags"); len(tags.Items) != 1 {
					t.Errorf("Tags = %v, want [eggy]", tags)
				}
			}},
		{"Health", "minecraft:pig_spawn_egg", "minecraft:pig", "minecraft:pig",
			nbt.Compound{"id": nbt.String("minecraft:pig"), "Health": nbt.Float(3)},
			func(t *testing.T, tag nbt.Compound) {
				if h, _ := nbt.Get[nbt.Float](tag, "Health"); h != 3 {
					t.Errorf("Health = %v, want 3", h)
				}
			}},
		{"cow from a pig egg", "minecraft:pig_spawn_egg", "minecraft:cow", "minecraft:cow",
			nbt.Compound{"id": nbt.String("minecraft:cow")}, func(*testing.T, nbt.Compound) {}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.EggPlayer(t, "survival", "Hatcher", tc.egg, 1)
			srv.GiveSlot("Hatcher", player.HotbarSlot, eggWithData(t, tc.egg, tc.typeName, tc.data))
			c.PlaceBlock(2, 63, 3, protocol.FaceTop)
			typ, _ := registry.EntityTypeID(tc.want)
			c.AwaitSpawn(typ)
			tc.check(t, dataOf(t, srv, tc.want))
			if tc.want == "minecraft:cow" && len(srv.SelectedEntities("minecraft:pig")) != 0 {
				t.Error("a pig hatched from the cow data")
			}
		})
	}
}

// TestADispensedSpawnEggAppliesItsEntityData: SpawnEggItemBehavior spawns
// through EntityType.spawn with the stack, so entity_data applies.
func TestADispensedSpawnEggAppliesItsEntityData(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Dispensing(t)
	srv.SetInventoryAt(4, 64, 0, 0, eggWithData(t, "minecraft:zombie_spawn_egg", "minecraft:zombie",
		nbt.Compound{"id": nbt.String("minecraft:zombie"), "NoAI": nbt.Byte(1)}))
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	if tag := dataOf(t, srv, "minecraft:zombie"); !nbt.BoolField(tag, "NoAI") {
		t.Errorf("NoAI missing: %v", tag)
	}
}

// TestASpawnEggsEntityDataOnASpawnerNeedsAnOp: an op's egg sets the spawner's
// spawn data, anyone else's only its type.
// vanilla: SpawnEggItem.useOn
func TestASpawnEggsEntityDataOnASpawnerNeedsAnOp(t *testing.T) {
	t.Parallel()
	for _, op := range []bool{false, true} {
		name := "non-op"
		if op {
			name = "op"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var setup []func(*server.Server)
			if op {
				setup = append(setup, servertest.OpsListing(t, "Keeper"))
			}
			cfg := servertest.MobTestConfig()
			cfg.GameMode = "survival"
			srv, c, _ := servertest.JoinAs(t, cfg, "Keeper", setup...)
			c.MoveToGround(0.5, 64, 0.5, true)
			c.Settle()
			srv.GiveSlot("Keeper", player.HotbarSlot, eggWithData(t, "minecraft:pig_spawn_egg", "minecraft:cow",
				nbt.Compound{"id": nbt.String("minecraft:cow"), "NoAI": nbt.Byte(1)}))
			srv.SetBlock(2, 64, 3, servertest.MustState(t, "minecraft:spawner", nil))
			c.PlaceBlock(2, 64, 3, protocol.FaceTop)
			_, _, _, _, data := c.AwaitBlockEntityData()
			spawn, _ := nbt.Get[nbt.Compound](data, "SpawnData")
			next, _ := nbt.Get[nbt.Compound](spawn, "entity")
			if id, _ := nbt.Get[nbt.String](next, "id"); id != "minecraft:cow" {
				t.Errorf("spawner entity = %v, want the cow", next)
			}
			if has := nbt.BoolField(next, "NoAI"); has != op {
				t.Errorf("NoAI on the spawner = %v, want %v", has, op)
			}
		})
	}
}
