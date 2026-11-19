package server

import (
	"maps"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

func spawnableChunkCountMap(players []entity.SpawnPlayer) int {
	seen := make(map[entity.ChunkPos]struct{})
	for i := range players {
		px, pz := world.ChunkOf(players[i].X), world.ChunkOf(players[i].Z)
		for dx := -spawnReachChunks; dx <= spawnReachChunks; dx++ {
			for dz := -spawnReachChunks; dz <= spawnReachChunks; dz++ {
				seen[entity.ChunkPos{X: px + int32(dx), Z: pz + int32(dz)}] = struct{}{}
			}
		}
	}
	return len(seen)
}

func TestSpawnableChunkCountMatchesTheMap(t *testing.T) {
	cases := [][]entity.SpawnPlayer{
		nil,
		{{X: -0.1, Z: -16.1}},
		{{}, {}},
		{{}, {X: 17 * 16}},
		{{}, {X: 16 * 16}},
		{{}, {Z: 17 * 16}},
		{{}, {X: -16, Z: -16}, {X: 16, Z: 16, Spectator: true}},
		{{X: -29999984, Z: -29999984}, {X: 29999984, Z: 29999984}},
	}
	rng := rand.New(rand.NewSource(1))
	for range 2000 {
		players := make([]entity.SpawnPlayer, rng.Intn(12))
		spread := float64(1 + rng.Intn(600))
		for i := range players {
			players[i].X, players[i].Z = (rng.Float64()-0.5)*spread, (rng.Float64()-0.5)*spread
			players[i].Spectator = rng.Intn(2) == 0
		}
		cases = append(cases, players)
	}
	for _, players := range cases {
		if got, want := spawnableChunkCount(players), spawnableChunkCountMap(players); got != want {
			t.Fatalf("players %+v: %d, want %d", players, got, want)
		}
	}
}

func TestSpawnCountsPlayerRange(t *testing.T) {
	players := []entity.SpawnPlayer{
		{EntityID: 1, X: 8, Z: 8, Y: 1000},
		{EntityID: 2, X: 136, Z: 8}, // Exactly 128 blocks from the chunk centre.
		{EntityID: 3, X: 135.999, Z: 8},
		{EntityID: 4, X: 8, Z: 8, Spectator: true},
		{EntityID: 5, X: 8, Z: 8, Dead: true},
		{EntityID: 6, X: -8, Z: -8},
	}
	herd := entity.Mobs{
		{Type: entity.TypeZombie, X: 0, Z: 0},
		{Type: entity.TypeZombie, X: -0.1, Z: -0.1},
		{Type: entity.TypeZombie, X: 10000},
		{Type: entity.TypeZombie, Persistence: true},
		{Type: entity.TypeCow},
	}
	dim := &dimension{w: &world.World{}, mobs: &mobHerd{herd: herd}}
	s := &Server{}
	all, _ := s.spawnCountsAll(dim, players)
	got := all[entity.CategoryMonster]
	want := map[int32]int{1: 2, 3: 1, 5: 2, 6: 2}
	if got.Global != 3 || !maps.Equal(got.PerPlayer, want) {
		t.Fatalf("spawn counts = %+v, want global 3, per player %v", got, want)
	}
}

func TestSpawnCountsAllMatchesPerCategory(t *testing.T) {
	players := []entity.SpawnPlayer{
		{EntityID: 1, X: 8, Z: 8},
		{EntityID: 2, X: 200, Z: -40},
		{EntityID: 3, X: 8, Z: 8, Spectator: true},
		{EntityID: 4, X: -150, Z: 90},
	}
	types := []entity.MobType{
		entity.TypeZombie, entity.TypeCreeper, entity.TypeCow, entity.TypeSheep,
		entity.TypeAxolotl, entity.TypeGlowSquid, entity.TypeSquid, entity.TypeDolphin,
		entity.TypeCod, entity.TypeTropicalFish, entity.TypeBat, entity.TypeVillager,
		entity.TypeIronGolem,
	}
	rng := rand.New(rand.NewSource(4599))
	var herd entity.Mobs
	for i := range 600 {
		herd = append(herd, &entity.Mob{
			Type:        types[i%len(types)],
			X:           (rng.Float64() - 0.5) * 600,
			Z:           (rng.Float64() - 0.5) * 600,
			Persistence: rng.Intn(5) == 0,
		})
	}
	dim := &dimension{w: &world.World{}, mobs: &mobHerd{herd: herd}}
	s := &Server{}
	got, _ := s.spawnCountsAll(dim, players)
	kinds := map[entity.SpawnCategory]func(entity.MobType) bool{
		entity.CategoryMonster:          entity.MonsterKind,
		entity.CategoryCreature:         entity.CreatureKind,
		entity.CategoryAxolotls:         entity.AxolotlKind,
		entity.CategoryUndergroundWater: entity.UndergroundWaterKind,
		entity.CategoryWaterCreature:    entity.WaterCreatureKind,
		entity.CategoryWaterAmbient:     entity.WaterAmbientKind,
		entity.CategoryAmbient:          entity.AmbientKind,
	}
	for category, kind := range kinds {
		// The per-category walk spawnCountsAll replaces.
		want := &entity.SpawnCounts{PerPlayer: map[int32]int{}}
		for _, m := range herd {
			if !kind(m.Type) || !m.CountsForSpawnCap() {
				continue
			}
			want.Global++
			at := entity.ChunkPos{X: world.ChunkOf(m.X), Z: world.ChunkOf(m.Z)}
			for i := range players {
				if p := &players[i]; p.CloseForSpawning(at) {
					want.PerPlayer[p.EntityID]++
				}
			}
		}
		if want.Global == 0 {
			t.Fatalf("category %v: herd has no counted mob", category)
		}
		if g := got[category]; g.Global != want.Global || !maps.Equal(g.PerPlayer, want.PerPlayer) {
			t.Errorf("category %v: got %+v, want %+v", category, g, want)
		}
	}
}
