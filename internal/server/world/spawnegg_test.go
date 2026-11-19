package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
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

// TestSpawnYOffsetDropsOntoTheCellsShapes checks EntityType.getYOffset over
// the flat world's stone (top at GroundLevel).
func TestSpawnYOffsetDropsOntoTheCellsShapes(t *testing.T) {
	w, _ := newPhysicsWorld(t)
	w.SetBlock(1, GroundLevel, 0, mustFind(t, "minecraft:stone_slab", map[string]string{"type": "bottom"}))
	w.SetBlock(2, GroundLevel, 0, mustFind(t, "minecraft:oak_fence", nil))
	w.SetBlock(3, GroundLevel, 0, mustFind(t, "minecraft:chest",
		map[string]string{"facing": "north", "type": "single"}))
	cases := []struct {
		name    string
		x, y    int32
		movedUp bool
		want    float64
	}{
		{"air over stone, moved up", 0, GroundLevel, true, 0},
		{"air over stone, not moved up", 0, GroundLevel, false, 0},
		{"over a bottom slab, moved up", 1, GroundLevel + 1, true, -0.5},
		{"inside a bottom slab's cell", 1, GroundLevel, false, 0.5},
		{"a fence below, moved up onto its tall collision", 2, GroundLevel + 1, true, 0.5},
		{"a chest below, moved up onto its rim", 3, GroundLevel + 1, true, -0.125},
		{"high in the air", 0, GroundLevel + 5, true, -1},
	}
	for _, tc := range cases {
		if got := w.SpawnYOffset(tc.x, tc.y, 0, tc.movedUp, 0.45); got != tc.want {
			t.Errorf("%s: offset %v, want %v", tc.name, got, tc.want)
		}
	}
	if w.CollisionEmpty(0, GroundLevel-1, 0) || !w.CollisionEmpty(0, GroundLevel, 0) {
		t.Error("stone reads empty or air reads solid")
	}
}

// TestClipSourceFluidFindsTheFirstSourceOrBlock checks the SOURCE_ONLY clip.
func TestClipSourceFluidFindsTheFirstSourceOrBlock(t *testing.T) {
	w, _ := newPhysicsWorld(t)
	w.SetBlock(0, GroundLevel, 3, mustFind(t, "minecraft:water", map[string]string{"level": "0"}))
	w.SetBlock(0, GroundLevel, 2, mustFind(t, "minecraft:water", map[string]string{"level": "3"}))
	eyeY := float64(GroundLevel) + 1.62
	// Straight down onto the source from above: the fluid shape's top face.
	if x, y, z, face, liquid, hit := w.ClipSourceFluid(0.5, eyeY, 3.5, 0, -1, 0, 4.5); !hit || !liquid ||
		x != 0 || y != GroundLevel || z != 3 || face != protocol.FaceTop {
		t.Errorf("down onto the source: (%d %d %d) face=%d liquid=%v hit=%v", x, y, z, face, liquid, hit)
	}
	// Straight down onto the flowing water: no outline, so the stone.
	if _, y, _, face, liquid, hit := w.ClipSourceFluid(0.5, eyeY, 2.5, 0, -1, 0, 4.5); !hit || liquid ||
		y != GroundLevel-1 || face != protocol.FaceTop {
		t.Errorf("down through the flow: y=%d face=%d liquid=%v hit=%v, want the stone top", y, face, liquid, hit)
	}
	// Level over the source's surface (8/9 high) misses it; into the sky.
	if _, _, _, _, _, hit := w.ClipSourceFluid(0.5, float64(GroundLevel)+0.95, 0.5, 0, 0, 1, 4.5); hit {
		t.Error("a ray over the surface hit something")
	}
	// Beyond reach.
	if _, _, _, _, _, hit := w.ClipSourceFluid(0.5, eyeY+10, 3.5, 0, -1, 0, 4.5); hit {
		t.Error("a hit beyond reach")
	}
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
