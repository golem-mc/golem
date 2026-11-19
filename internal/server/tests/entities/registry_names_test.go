package entities

import (
	"fmt"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: VillagerData.CODEC, Registry.holderByNameCodec
func TestSummonedVillagersQualifyRegistryNames(t *testing.T) {
	for _, tc := range []struct {
		kind, profession, biome string
		level                   int
		index                   uint8
		data                    []int32
	}{
		{"villager", "farmer", "desert", 1, protocol.MetadataIndexVillagerVillagerData, []int32{0, 5, 1}},
		{"zombie_villager", "cleric", "swamp", 2, protocol.MetadataIndexVillagerData, []int32{5, 4, 2}},
	} {
		for _, namespace := range []string{"", "minecraft:"} {
			t.Run(tc.kind+"/"+namespace, func(t *testing.T) {
				t.Parallel()
				srv, c := servertest.Join(t, servertest.MobTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))
				servertest.HoldTicks(t, srv)
				c.ChatCommand(fmt.Sprintf(`summon %s 1.5 64 0.5 {NoAI:1b,VillagerData:{profession:%q,level:%d,type:%q}}`, tc.kind, namespace+tc.profession, tc.level, namespace+tc.biome))
				seen := c.WatchVariant(srv, "minecraft:"+tc.kind)
				if got := seen.Meta[tc.index]; !slices.Equal(got, tc.data) {
					t.Errorf("spawn's villager metadata = %v, want %v", got, tc.data)
				}
				for key, want := range map[string]string{
					"profession": `"minecraft:` + tc.profession + `"`,
					"type":       `"minecraft:` + tc.biome + `"`,
					"level":      fmt.Sprint(tc.level),
				} {
					got := awaitEntityDataCompound(t, srv, c, fmt.Sprintf("@e[type=%s,limit=1,sort=nearest]", tc.kind), "VillagerData."+key)
					if got != want {
						t.Errorf("/data get VillagerData.%s = %s, want %s", key, got, want)
					}
				}
			})
		}
	}
}

// vanilla: Painting.readAdditionalSaveData, VariantUtils.readVariant
func TestSummonedPaintingsQualifyRegistryNames(t *testing.T) {
	for _, name := range []string{"kebab", "minecraft:kebab"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))
			servertest.HoldTicks(t, srv)
			servertest.BuildPaintingWall(srv, 4, 64, 64, 5, 5)
			c.ChatCommand(fmt.Sprintf(`summon painting 4 64 4 {variant:%q,facing:2b}`, name))
			seen := c.ExpectPaintingSpawnWithinTicks(srv, 1, protocol.FaceNorth, 4, 64, 4)
			variants, _ := registry.Get("minecraft:painting_variant")
			if want := variants.ID("minecraft:kebab"); seen.Variant != want {
				t.Errorf("spawn's painting variant = %d, want kebab (%d)", seen.Variant, want)
			}
			if got := awaitEntityDataCompound(t, srv, c, "@e[type=painting,limit=1,sort=nearest]", "variant"); got != `"minecraft:kebab"` {
				t.Errorf("/data get variant = %s, want minecraft:kebab", got)
			}
		})
	}
}
