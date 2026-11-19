package server

import (
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestStructureBastionMobsFinalizeOnLoad(t *testing.T) {
	w, err := world.NewWorld(t.TempDir(), world.NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	s := &Server{world: w}
	g := world.NewNetherGenerator(0)
	counts := map[entity.MobType]int{}
	// The seed-0 bastion at (-27,-10), including its outer pieces.
	for x := int32(-30); x <= -25; x++ {
		for z := int32(-14); z <= -9; z++ {
			for _, tag := range g.GeneratedEntities(world.ChunkPos{X: x, Z: z}) {
				ent := mobOfTag(s, w, tag)
				if ent == nil {
					t.Fatal("unhandled bastion mob")
				}
				m := ent.mob
				counts[m.Type]++
				if m.Type == entity.TypePiglinBrute && registry.Item(m.MainHand.ItemID) != registry.ItemGoldenAxe {
					t.Errorf("brute holds %v", m.MainHand)
				}
				if m.Type == entity.TypePiglin && m.IsBaby() {
					t.Error("structure piglin became a baby")
				}
				saved := mobTag(ent)
				if _, ok := saved[world.StructureFinalizeKey]; ok {
					t.Error("finalization marker persisted")
				}
				// A saved mob keeps its finalized equipment and age on subsequent loads.
				again := mobOfTag(s, w, saved).mob
				if again.MainHand.ItemID != m.MainHand.ItemID || again.IsBaby() != m.IsBaby() {
					t.Error("reload changed finalized mob")
				}
			}
		}
	}
	if counts[entity.TypePiglinBrute] == 0 || counts[entity.TypePiglin] == 0 {
		t.Fatalf("missing fixture kinds: %v", counts)
	}
}

func TestStructureHutCatFinalizesOnLoad(t *testing.T) {
	w, err := world.NewWorld(t.TempDir(), world.NewOverworldGenerator(0))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	pos := world.ChunkPos{X: -336, Z: -171}
	w.Acquire(pos)
	defer w.Release(pos)
	w.Save()
	w.FlushSaves()
	tags, _, err := w.LoadChunkEntities(pos)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range tags {
		tag, ok := nbt.CompoundView(raw)
		if !ok {
			t.Fatalf("loaded entity is %T, want a compound", raw)
		}
		if tag["id"] != nbt.String("minecraft:cat") {
			continue
		}
		x, y, z, ok := entityPositionOfTag(tag)
		if !ok || !entity.CatInHut(w, int32(x), int32(y), int32(z)) {
			t.Fatal("generated cat is outside the hut")
		}
		ent := mobOfTag(&Server{world: w}, w, tag)
		if ent.mob.Variant != "minecraft:all_black" {
			t.Fatalf("hut cat variant = %q", ent.mob.Variant)
		}
		if _, ok := mobTag(ent)[world.StructureFinalizeKey]; ok {
			t.Fatal("finalization marker persisted")
		}
		return
	}
	t.Fatal("hut did not generate a cat")
}

func TestStructureHutCatFinalizesAtGeneration(t *testing.T) {
	g := world.NewOverworldGenerator(0)
	w, err := world.NewWorld(t.TempDir(), g)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	s := &Server{world: w}
	g.SetStructureMobFinalizer(s.structureMobFinalizer(world.DimensionName))
	pos := world.ChunkPos{X: -336, Z: -171}
	w.Acquire(pos)
	defer w.Release(pos)
	w.Save()
	w.FlushSaves()
	tags, _, err := w.LoadChunkEntities(pos)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range tags {
		tag, ok := nbt.CompoundView(raw)
		if !ok {
			t.Fatalf("loaded entity is %T, want a compound", raw)
		}
		if tag["id"] != nbt.String("minecraft:cat") {
			continue
		}
		if tag["variant"] != nbt.String("minecraft:all_black") {
			t.Fatalf("saved hut cat variant = %v", tag["variant"])
		}
		if _, ok := tag[world.StructureFinalizeKey]; ok {
			t.Fatal("finalized cat still carries the load-time marker")
		}
		ent := mobOfTag(s, w, tag)
		if ent.mob.Variant != "minecraft:all_black" {
			t.Fatalf("loaded hut cat variant = %q", ent.mob.Variant)
		}
		return
	}
	t.Fatal("hut did not generate a cat")
}

// TestStructureBastionMobsFinalizeAtGeneration: the nether generator finalizes
// a bastion chunk's mobs in placement order on that chunk's legacy region
// random, so the first piglin draws its hunt cooldown and then its four armour
// rolls (head to feet) first. vanilla: Piglin.finalizeSpawn, RandomState
func TestStructureBastionMobsFinalizeAtGeneration(t *testing.T) {
	w, err := world.NewWorld(t.TempDir(), world.NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	s := &Server{world: w}
	g := world.NewNetherGenerator(0)
	g.SetStructureMobFinalizer(s.structureMobFinalizer(world.NetherName))
	// Seed 0's bastion: 6 piglins and a brute, the first placed a piglin.
	pos := world.ChunkPos{X: -28, Z: -11}
	tags := g.GeneratedEntities(pos)
	if len(tags) == 0 || tags[0]["id"] != nbt.String("minecraft:piglin") {
		t.Fatalf("first placed mob is not a piglin: %d tags", len(tags))
	}
	brutes := 0
	for _, tag := range tags {
		if _, ok := tag[world.StructureFinalizeKey]; ok {
			t.Errorf("%v still carries the load-time marker", tag["id"])
		}
		if tag["id"] != nbt.String("minecraft:piglin_brute") {
			continue
		}
		brutes++
		home, _ := brainMemory(tag, "minecraft:home")["value"].(nbt.Compound)
		if home["dimension"] != nbt.String(world.NetherName) {
			t.Errorf("brute home %v, want %s", home, world.NetherName)
		}
	}
	if brutes == 0 {
		t.Fatal("no brute in the bastion chunk")
	}

	dice := levelgen.NewLegacyNamedRandom(0, "minecraft:worldgen_region_random").At(pos.X*16, 0, pos.Z*16)
	first := tags[0]
	wantRest := 600 + int64(dice.NextIntN(1801))
	if got, _ := brainMemory(first, "minecraft:hunted_recently")["ttl"].(nbt.Long); int64(got) != wantRest {
		t.Errorf("huntRest %d, want %d", got, wantRest)
	}
	equipment, _ := first["equipment"].(nbt.Compound)
	for _, slot := range []string{"head", "chest", "legs", "feet"} {
		want := dice.NextFloat() < 0.1
		if _, got := equipment[slot]; got != want {
			t.Errorf("%s worn %v, want %v", slot, got, want)
		}
	}
}

// brainMemory is the Brain memory name of a mob tag, or nil.
func brainMemory(tag nbt.Compound, name string) nbt.Compound {
	brain, _ := tag["Brain"].(nbt.Compound)
	memories, _ := brain["memories"].(nbt.Compound)
	m, _ := memories[name].(nbt.Compound)
	return m
}

func TestStructureNautilusKeyLoadsJockey(t *testing.T) {
	w, err := world.NewWorld(t.TempDir(), world.NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	tag := nbt.Compound{
		"id":                        nbt.String("minecraft:drowned"),
		"Pos":                       nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(0.5), nbt.Double(64), nbt.Double(0.5)}},
		entity.StructureNautilusKey: nbt.Byte(1),
	}
	ent := mobOfTag(&Server{world: w}, w, tag)
	if ent.mob.Jockey != entity.JockeyStructureNautilus {
		t.Fatalf("jockey = %v", ent.mob.Jockey)
	}
	if _, ok := mobTag(ent)[entity.StructureNautilusKey]; ok {
		t.Fatal("nautilus marker persisted")
	}
}

// TestStructureDrownedGeneratesOnNautilus: seed 7's cold ocean ruin saves a
// zombie nautilus carrying the drowned that rolled it, and the chunk's wake
// stands the drowned up riding it.
// vanilla: Drowned.finalizeSpawn, StructureTemplate.placeEntities
func TestStructureDrownedGeneratesOnNautilus(t *testing.T) {
	g := world.NewOverworldGenerator(7)
	w, err := world.NewWorld(t.TempDir(), g)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	s := &Server{world: w}
	g.SetStructureMobFinalizer(s.structureMobFinalizer(world.DimensionName))
	pos := world.ChunkPos{X: 2, Z: 6} // the ruin's drowned at 42.5 59 98.5
	w.Acquire(pos)
	defer w.Release(pos)
	w.Save()
	w.FlushSaves()
	tags, _, err := w.LoadChunkEntities(pos)
	if err != nil {
		t.Fatal(err)
	}
	var vehicle nbt.Compound
	for _, raw := range tags {
		tag, ok := nbt.CompoundView(raw)
		if !ok {
			t.Fatalf("loaded entity is %T, want a compound", raw)
		}
		if tag["id"] == nbt.String("minecraft:zombie_nautilus") {
			vehicle = tag
		}
	}
	if vehicle == nil || !nbt.BoolField(vehicle, "PersistenceRequired") {
		t.Fatalf("no persistent zombie nautilus saved in %d tags", len(tags))
	}
	passengers, _ := nbt.Get[nbt.List](vehicle, "Passengers")
	if len(passengers.Items) != 1 {
		t.Fatalf("nautilus passengers = %v", passengers)
	}
	rider, ok := nbt.CompoundView(passengers.Items[0])
	if !ok {
		t.Fatalf("loaded passenger is %T, want a compound", passengers.Items[0])
	}
	if rider["id"] != nbt.String("minecraft:drowned") {
		t.Fatalf("nautilus carries %v", rider["id"])
	}
	if _, ok := rider[entity.StructureNautilusKey]; ok {
		t.Error("drowned still carries the nautilus marker")
	}

	woken := (&mobHerd{w: w}).standUp(pos, tags, s, map[uuid.UUID]bool{}, true)
	for _, ent := range woken.mobs {
		if ent.mob.Type != entity.TypeDrowned || ent.mob.Vehicle == nil {
			continue
		}
		if v := ent.mob.Vehicle; v.Type != entity.TypeZombieNautilus || !v.Persistence || ent.mob.Jockey != entity.JockeyNone {
			t.Fatalf("drowned rides %v (persistent %v), jockey %v", v.Type, v.Persistence, ent.mob.Jockey)
		}
		return
	}
	t.Fatal("no drowned loaded riding its nautilus")
}
