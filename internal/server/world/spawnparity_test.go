package world_test

import (
	"crypto/sha256"
	"fmt"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// Hide the concrete type while retaining optional structure and conductor methods.
type fallbackSpawnWorld struct{ *world.World }

func spawnParityRun(sw entity.SpawnWorld, seed int64, players []entity.SpawnPlayer, capped bool) string {
	rng := rand.New(rand.NewSource(seed))
	var chunks []entity.ChunkPos
	for x := int32(-2); x <= 3; x++ {
		for z := int32(-1); z <= 1; z++ {
			chunks = append(chunks, entity.ChunkPos{X: x, Z: z})
		}
	}
	var counts [entity.SpawnCategoryCount]entity.SpawnCounts
	passes := []entity.SpawnPass{entity.MonsterPass(&counts[0]), entity.CreaturePass(&counts[1]), entity.WaterCreaturePass(&counts[2]), entity.UndergroundWaterPass(&counts[3]), entity.AmbientPass(&counts[4]), entity.WaterAmbientPass(&counts[5]), entity.AxolotlPass(&counts[6])}
	if capped {
		for i := range counts {
			counts[i].PerPlayer = map[int32]int{1: 70, 2: 0}
		}
	}
	var potential entity.SpawnPotential
	potential.Add(0, 30, 0, 0.7)
	var result string
	entity.SpawnNaturally(sw, rng, chunks, players, nil, &potential, passes, 289, func(r entity.SpawnRequest) {
		m := entity.NewMob(r.Type, 1, r.X, r.Y, r.Z, seed)
		m.FinalizeSpawn(entity.GoDice{Rand: rng}, sw.Biome(int32(r.X), int32(r.Y), int32(r.Z)), r.Group, rules.LocalDifficulty{})
		result += fmt.Sprintf("%s %g %g %g %g %t %v\n", r.Type.Name(), r.X, r.Y, r.Z, r.Yaw, m.IsBaby(), r.Group.Natural())
	})
	return fmt.Sprintf("%snext %d counts %v potential %v\n", result, rng.Int63(), counts, potential)
}

func TestNaturalSpawnPlayerParity(t *testing.T) {
	cases := []struct {
		name    string
		players []entity.SpawnPlayer
		capped  bool
	}{
		{name: "high-observer", players: []entity.SpawnPlayer{{EntityID: 1, X: 8, Y: 200, Z: 8}, {EntityID: 2, X: 210, Y: 64, Z: 8}}},
		{name: "low", players: []entity.SpawnPlayer{{EntityID: 1, X: 8, Y: 64, Z: 8}}},
		{name: "spectator-dead", players: []entity.SpawnPlayer{{EntityID: 1, X: 8, Y: 30, Z: 8, Spectator: true}, {EntityID: 2, X: 70, Y: 30, Z: 8, Dead: true}}},
		{name: "none"},
		{name: "overlap", players: []entity.SpawnPlayer{{EntityID: 1, X: 8, Y: 30, Z: 8}, {EntityID: 2, X: 8, Y: 30, Z: 8}}},
		{name: "capped", players: []entity.SpawnPlayer{{EntityID: 1, X: 8, Y: 30, Z: 8}, {EntityID: 2, X: 80, Y: 30, Z: 8}}, capped: true},
		{name: "far", players: []entity.SpawnPlayer{{EntityID: 1, X: 8, Y: 200, Z: 8}}},
	}
	wants := map[string]string{
		"false/high-observer":  "46890:3a9207e07c58e55c27fe222c6739434d77f20071afd4c1224da539cb42e642df",
		"false/low":            "48519:d39b49a089583ab89807f6145f2821dc7a79c3143617a9bbe83a0ab736361598",
		"false/spectator-dead": "47553:52dad8ca7777d0b43612b61965fc7c5eb9699dba6aa9f23374c9cf486632ce4f",
		"false/none":           "38653:be491a8fd57f09777f2c1adb1147f157079627523618d47f5e9be9b1d47c0fdd",
		"false/overlap":        "44024:dd8809283d38b722947905793772df1ac0cc9ee601e446e715867cc52de1319c",
		"false/capped":         "60304:1ed1845479f05eb86d3a34cbddfd610b9e45e27b4fc8f2b50345a1613920d23e",
		"false/far":            "46890:3a9207e07c58e55c27fe222c6739434d77f20071afd4c1224da539cb42e642df",
		"true/high-observer":   "38674:64d0173ccae4b6d30f57c501f8f1352f9ca749c336552ebc742724b80d47a479",
		"true/low":             "74218:bc0119ce2f47bd904d5e6f26365a3616857581209b5b6fcc278d27927eefa8a5",
		"true/spectator-dead":  "75768:3540119cb9510a59f451b18bbe3d2e3d6f1445a6215bb02097d9f2fcf8ba306f",
		"true/none":            "38653:be491a8fd57f09777f2c1adb1147f157079627523618d47f5e9be9b1d47c0fdd",
		"true/overlap":         "82652:c3d86951c3666a2cb47cd4203bc453196d85c1144d00d8d45234e745c69c2d10",
		"true/capped":          "98557:0d746c740d7c379792726b4435e41a10cda47fb998bf334fe7519b6267700f9f",
		"true/far":             "38674:64d0173ccae4b6d30f57c501f8f1352f9ca749c336552ebc742724b80d47a479",
	}
	for _, water := range []bool{false, true} {
		w := world.SpawnParityFixture(t, water)
		for _, tc := range cases {
			name := fmt.Sprintf("%t/%s", water, tc.name)
			t.Run(name, func(t *testing.T) {
				hash := sha256.New()
				encoded := 0
				for seed := int64(1); seed <= 300; seed++ {
					fast := spawnParityRun(w, seed, tc.players, tc.capped)
					slow := spawnParityRun(fallbackSpawnWorld{w}, seed, tc.players, tc.capped)
					if fast != slow {
						t.Fatalf("seed %d: fast\n%s\nfallback\n%s", seed, fast, slow)
					}
					fmt.Fprint(hash, fast)
					encoded += len(fast)
				}
				got := fmt.Sprintf("%d:%x", encoded, hash.Sum(nil))
				if got != wants[name] {
					t.Errorf("%s digest = %s, want %s", name, got, wants[name])
				}
			})
		}
	}
}
