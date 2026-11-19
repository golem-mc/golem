package server

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// A first-generation strider that rolled a jockey goes into the herd with its
// rider seated, and saves the rider nested under Passengers.
// vanilla: Strider.spawnJockey, Entity.addSaveData
func TestAFirstGenerationStriderSavesItsRiderNested(t *testing.T) {
	w, err := world.NewWorld(t.TempDir(), world.NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	s := &Server{world: w}
	biome, _ := registry.BiomeByName("minecraft:nether_wastes")
	found := map[entity.MobType]bool{}
	for seed := int64(1); seed <= 200 && len(found) < 2; seed++ {
		m := entity.NewMob(entity.TypeStrider, s.nextEntity(), 0.5, 32, 0.5, seed)
		m.FinalizeSpawn(entity.GoDice{Rand: rand.New(rand.NewSource(seed))}, biome, nil, rules.LocalDifficulty{})
		if m.Jockey != entity.JockeyBuiltRider {
			continue
		}
		rider := m.Rider
		herd := generatedHerd(s, w, []*entity.Mob{m})
		if len(herd) != 2 || herd[0].mob != m || herd[1].mob != rider {
			t.Fatalf("seed %d: herd of %d, want the strider then its rider", seed, len(herd))
		}
		if rider.ID == 0 || rider.ID == m.ID || rider.Vehicle != m || m.Rider != nil || m.Jockey != entity.JockeyNone {
			t.Fatalf("seed %d: rider id %d (strider %d), seated %v, jockey left %v", seed, rider.ID, m.ID, rider.Vehicle == m, m.Jockey)
		}
		book := map[*entity.Mob]*mobEntity{m: herd[0], rider: herd[1]}
		list, ok := mobTagIn(herd[0], book).Entries()["Passengers"].(nbt.List)
		if !ok || len(list.Items) != 1 {
			t.Fatalf("seed %d: strider saves passengers %v", seed, list)
		}
		if id, _ := nbt.Get[nbt.String](list.Items[0].(nbt.CompoundTag).Entries(), "id"); string(id) != rider.Type.Name() {
			t.Errorf("seed %d: nested passenger %q, want %q", seed, id, rider.Type.Name())
		}
		found[rider.Type] = true
	}
	if !found[entity.TypeZombifiedPiglin] || !found[entity.TypeStrider] {
		t.Errorf("riders found over 200 striders: %v", found)
	}
}
