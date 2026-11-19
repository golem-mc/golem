package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestMonsterLightByDimension checks the dimension type's monster light
// rules at a cell with block light 5 and no sky light: the overworld refuses
// any block light, the nether allows up to its fixed level of 7, and the End
// allows none from a block but answers a level of 15.
// vanilla: Monster.isDarkEnoughToSpawn
func TestMonsterLightByDimension(t *testing.T) {
	cases := []struct {
		dimension string
		light     uint8
		want      bool
	}{
		{"minecraft:overworld", 5, false},
		{world.NetherName, 5, true},
		{world.NetherName, 8, false},
		{world.EndName, 5, false},
		{world.EndName, 0, true},
	}
	for _, tc := range cases {
		w := newSpawnTestWorld()
		w.dimension = tc.dimension
		w.blockLights[[3]int32{0, 64, 0}] = tc.light
		rng := rand.New(rand.NewSource(1))
		for range 50 {
			if got := isDarkEnoughToSpawn(w, 0, 64, 0, GoDice{rng}); got != tc.want {
				t.Fatalf("%s at block light %d: dark enough %v, want %v", tc.dimension, tc.light, got, tc.want)
			}
		}
	}
}

// vanilla: SpawnPlacements, BaseSpawner.serverTick
func TestSpawnerRulesFollowEachTypesPlacement(t *testing.T) {
	for _, tc := range []struct {
		name   string
		kind   MobType
		light  uint8
		ground registry.Block
		want   bool
	}{
		{"blaze", TypeBlaze, 10, registry.BlockStone, true},
		{"zombie", TypeZombie, 10, registry.BlockStone, false},
		{"pillager bright", TypePillager, 9, registry.BlockStone, false},
		{"pillager dim", TypePillager, 8, registry.BlockStone, true},
		{"bee", TypeBee, 0, registry.BlockStone, true},
		{"shulker", TypeShulker, 10, registry.BlockStone, true},
		{"zombie nautilus unregistered", TypeZombieNautilus, 10, registry.BlockStone, true},
		{"skeleton horse dark", TypeSkeletonHorse, 8, registry.BlockStone, false},
		{"skeleton horse bright", TypeSkeletonHorse, 9, registry.BlockStone, true},
		{"axolotl clay", TypeAxolotl, 0, registry.BlockClay, true},
		{"axolotl stone", TypeAxolotl, 0, registry.BlockStone, false},
		{"cod dry", TypeCod, 0, registry.BlockStone, false},
		{"dolphin dry", TypeDolphin, 0, registry.BlockStone, false},
		{"tropical fish dry", TypeTropicalFish, 0, registry.BlockStone, false},
		{"nautilus dry", TypeNautilus, 0, registry.BlockStone, false},
		{"bat above surface", TypeBat, 0, registry.BlockStone, false},
		{"strider air above", TypeStrider, 0, registry.BlockStone, true},
		{"allay", TypeAllay, 0, registry.BlockStone, true},
		{"sniffer", TypeSniffer, 0, registry.BlockStone, true},
		{"trader", TypeWanderingTrader, 0, registry.BlockStone, true},
		{"vex registered monster", TypeVex, 10, registry.BlockStone, false},
		{"breeze", TypeBreeze, 10, registry.BlockStone, true},
		{"zoglin", TypeZoglin, 10, registry.BlockStone, true},
		{"endermite", TypeEndermite, 10, registry.BlockStone, true},
		{"piglin", TypePiglin, 10, registry.BlockStone, true},
		{"hoglin", TypeHoglin, 10, registry.BlockStone, true},
		{"zombified piglin", TypeZombifiedPiglin, 10, registry.BlockStone, true},
		{"piglin wart", TypePiglin, 10, registry.BlockNetherWartBlock, false},
		{"hoglin wart", TypeHoglin, 10, registry.BlockNetherWartBlock, false},
		{"zombified piglin wart", TypeZombifiedPiglin, 10, registry.BlockNetherWartBlock, false},
		{"cow stone", TypeCow, 10, registry.BlockStone, false},
		{"cow grass", TypeCow, 10, registry.BlockGrassBlock, true},
		{"cow dark", TypeCow, 8, registry.BlockGrassBlock, false},
		{"goat", TypeGoat, 10, registry.BlockStone, true},
		{"rabbit", TypeRabbit, 10, registry.BlockSand, true},
		{"camel", TypeCamel, 10, registry.BlockSand, true},
		{"mooshroom", TypeMooshroom, 10, registry.BlockMycelium, true},
		{"mooshroom grass", TypeMooshroom, 10, registry.BlockGrassBlock, false},
		{"fox", TypeFox, 10, registry.BlockSnowBlock, true},
		{"armadillo", TypeArmadillo, 10, registry.BlockRedSand, true},
		{"wolf", TypeWolf, 10, registry.BlockPodzol, true},
		{"frog", TypeFrog, 10, registry.BlockMud, true},
		{"parrot", TypeParrot, 10, registry.BlockJungleLeaves, true},
		{"polar bear", TypePolarBear, 10, registry.BlockGrassBlock, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newSpawnTestWorld()
			w.dimension = world.NetherName
			w.sky = 0
			w.set(0, 63, 0, tc.ground.DefaultState())
			w.blockLights[[3]int32{0, 64, 0}] = tc.light
			if got := SpawnerAllows(w, rand.New(rand.NewSource(1)), tc.kind, 0.5, 64, 0.5); got != tc.want {
				t.Errorf("SpawnerAllows = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSpawnerPlacementDice(t *testing.T) {
	for _, kind := range []MobType{TypeGhast, TypeGuardian, TypeElderGuardian, TypeOcelot} {
		w := newSpawnTestWorld()
		w.sky = 15
		w.blockLights[[3]int32{0, 64, 0}] = 10
		w.set(0, 63, 0, registry.BlockWater.DefaultState())
		rng, want := rand.New(rand.NewSource(1)), rand.New(rand.NewSource(1))
		for range 100 {
			expect := false
			if kind == TypeOcelot {
				expect = want.Intn(3) != 0
			} else {
				expect = want.Intn(20) == 0
			}
			if got := SpawnerAllows(w, rng, kind, 0.5, 64, 0.5); got != expect {
				t.Fatalf("%v: SpawnerAllows = %v, want roll passing %v", kind, got, expect)
			}
		}
		if rng.Int63() != want.Int63() {
			t.Fatal("placement consumed extra dice")
		}
		if kind == TypeGhast || kind == TypeOcelot {
			continue
		}
		w.sky = 0
		if !SpawnerAllows(w, rng, kind, 0.5, 64, 0.5) {
			t.Fatal("covered guardian refused water below an air spawn cell")
		}
		w.set(0, 63, 0, registry.BlockStone.DefaultState())
		if SpawnerAllows(w, rng, kind, 0.5, 64, 0.5) {
			t.Fatal("guardian allowed without water below")
		}
	}
}

func BenchmarkSpawnerMobFits(b *testing.B) {
	w := newSpawnTestWorld()
	w.sky = 15
	w.darken = 0
	w.set(0, 62, 0, registry.BlockGrassBlock.DefaultState())
	for _, kind := range []MobType{TypeOcelot, TypeCow, TypeBlaze} {
		b.Run(kind.Name(), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				SpawnerMobFits(w, kind, 0.5, 63, 0.5, nil)
			}
		})
	}
}

func BenchmarkSpawnerAllows(b *testing.B) {
	w := newSpawnTestWorld()
	w.dimension = world.NetherName
	w.sky = 0
	w.blockLights[[3]int32{0, 64, 0}] = 10
	rng := rand.New(rand.NewSource(1))
	for _, kind := range []MobType{TypeBlaze, TypeZombie, TypeCow} {
		b.Run(kind.Name(), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				SpawnerAllows(w, rng, kind, 0.5, 64, 0.5)
			}
		})
	}
}
