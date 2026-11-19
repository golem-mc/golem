package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func mustFind(t *testing.T, name string, props map[string]string) uint16 {
	t.Helper()
	s, ok := registry.FindState(name, props)
	if !ok {
		t.Fatalf("no state %s %v", name, props)
	}
	return s
}

// TestTheSpawnerBlockEntityKeepsItsFields: a spawner a vanilla file carries
// is read into the fields the egg's setEntityId writes, written back the way
// vanilla saves it — the numbers as shorts, the next spawn as
// SpawnData.entity, the potentials kept whole in the file and left out of
// the packet — while the trial spawner beside it stays carried through,
// nothing here reading its config.
func TestTheSpawnerBlockEntityKeepsItsFields(t *testing.T) {
	t.Parallel()
	spawner := nbt.Compound{
		"id": nbt.String("minecraft:mob_spawner"),
		"x":  nbt.Int(4), "y": nbt.Int(worldMinY), "z": nbt.Int(7),
		"Delay":               nbt.Short(17),
		"MinSpawnDelay":       nbt.Short(100),
		"MaxSpawnDelay":       nbt.Short(399),
		"SpawnCount":          nbt.Short(5),
		"MaxNearbyEntities":   nbt.Short(7),
		"RequiredPlayerRange": nbt.Short(15),
		"SpawnRange":          nbt.Short(3),
		"SpawnData": nbt.Compound{
			"entity": nbt.Compound{"id": nbt.String("minecraft:pig")},
		},
		"SpawnPotentials": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{
				"weight": nbt.Int(2),
				"data":   nbt.Compound{"entity": nbt.Compound{"id": nbt.String("minecraft:cow")}},
			},
		}},
	}
	be, ok := decodeBlockEntity(spawner)
	if !ok || be == nil || be.Kind != KindSpawner {
		t.Fatalf("decodeBlockEntity = %v/%v, want the spawner", be, ok)
	}
	if be.Delay != 17 || be.MinSpawnDelay != 100 || be.MaxSpawnDelay != 399 ||
		be.SpawnCount != 5 || be.MaxNearbyEntities != 7 ||
		be.RequiredPlayerRange != 15 || be.SpawnRange != 3 {
		t.Errorf("spawner fields = %d %d %d %d %d %d %d, want what the file said",
			be.Delay, be.MinSpawnDelay, be.MaxSpawnDelay, be.SpawnCount,
			be.MaxNearbyEntities, be.RequiredPlayerRange, be.SpawnRange)
	}
	if got := be.SpawnEntityID(); got != "minecraft:pig" {
		t.Errorf("next spawn = %q, want minecraft:pig", got)
	}
	if _, has := be.UpdateTag()["SpawnPotentials"]; has {
		t.Error("packet carries SpawnPotentials, want it dropped")
	}
	saved := be.EncodeNBT()
	if potentials, ok := saved["SpawnPotentials"].(nbt.List); !ok || len(potentials.Items) != 1 {
		t.Errorf("saved potentials = %#v, want the one entry back", saved["SpawnPotentials"])
	}
	if got, ok := nbt.Get[nbt.Short](saved, "Delay"); !ok || got != 17 {
		t.Errorf("saved Delay = %d/%v, want 17", got, ok)
	}

	// A spawner the file says nothing about keeps BaseSpawner's own
	// defaults, and the egg's setEntityId lands in SpawnData.entity.
	fresh := newBlockEntity(KindSpawner, 0, 64, 0)
	fresh.SetSpawnEntityID("minecraft:cow")
	packet := fresh.UpdateTag()
	spawn, ok := nbt.Get[nbt.Compound](packet, "SpawnData")
	entity, ok2 := nbt.Get[nbt.Compound](spawn, "entity")
	cowID, ok3 := nbt.Get[nbt.String](entity, "id")
	if !ok || !ok2 || !ok3 || cowID != "minecraft:cow" {
		t.Errorf("packet SpawnData = %#v, want the cow", packet["SpawnData"])
	}
	for name, want := range map[string]nbt.Short{
		"Delay": 20, "MinSpawnDelay": 200, "MaxSpawnDelay": 800, "SpawnCount": 4,
		"MaxNearbyEntities": 6, "RequiredPlayerRange": 16, "SpawnRange": 4,
	} {
		if got, has := packet[name]; !has || got != want {
			t.Errorf("fresh packet %s = %v/%v, want %d", name, got, has, want)
		}
	}
	if _, has := packet["SpawnPotentials"]; has {
		t.Error("fresh packet carries SpawnPotentials, want it dropped")
	}

	// The trial spawner is not one of the kinds this server models.
	if _, ok := decodeBlockEntity(nbt.Compound{
		"id": nbt.String("minecraft:trial_spawner"), "x": nbt.Int(0), "y": nbt.Int(0), "z": nbt.Int(0),
	}); ok {
		t.Error("decodeBlockEntity took the trial spawner, want it carried through")
	}
}
