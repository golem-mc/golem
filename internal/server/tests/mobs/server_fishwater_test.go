package mobs

import (
	"math"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// testFishSpawnInOcean runs the spawn pass over the seeded ocean: the ocean's
// water-ambient list is the cod alone, so every fish is a cod, in the sea's
// water inside the band the surface rule allows, between the 24 blocks the
// distance rule keeps clear and the 64 the category despawns at. The herd
// stops at the cap of twenty plus what the last pack of eight can carry over
// it, and further passes add none.
// vanilla: SpawnPlacements (COD), WaterAnimal.checkSurfaceWaterAnimalSpawnRules,
// MobCategory.WATER_AMBIENT, AbstractFish.getMaxSpawnClusterSize
func testFishSpawnInOcean(t *testing.T, srv *server.Server, herd waterHerd) {
	cod := 0
	// The player stands at the seed's world spawn.
	at := srv.PlayerPosition("Watcher")
	px, py, pz := at.X, at.Y, at.Z
	for _, m := range herd.capped {
		if m.Kind == "minecraft:salmon" {
			// The cold oceans', the river's and the frozen river's water-ambient lists include salmon.
			b := srv.OverworldBiomeAt(int32(math.Floor(m.X)), int32(math.Floor(m.Y)), int32(math.Floor(m.Z)))
			if !strings.Contains(b, "cold_ocean") && !strings.Contains(b, "frozen_ocean") && b != "minecraft:river" && b != "minecraft:frozen_river" {
				t.Errorf("a salmon spawned in %s, want only cod outside the cold oceans and rivers", b)
			}
			continue
		}
		if m.Kind != "minecraft:cod" {
			continue
		}
		cod++
		x, y, z := int32(math.Floor(m.X)), int32(math.Floor(m.Y)), int32(math.Floor(m.Z))
		if got := srv.BlockAt(x, y, z); !rules.IsWater(got) {
			t.Errorf("a cod stands at (%d, %d, %d) in block %d, want the sea's water", x, y, z, got)
		}
		if m.Y < 63-13 || m.Y > 62 {
			t.Errorf("a cod stands at y=%v, want the band from %d to the sea's surface at 62", m.Y, 63-13)
		}
		dx, dy, dz := float64(x)+0.5-px, float64(y)-py, float64(z)+0.5-pz
		distSq := dx*dx + dy*dy + dz*dz
		if distSq <= 576 {
			t.Errorf("a cod stands %.2f blocks from the player, inside the 24 the spawn rule keeps clear", math.Sqrt(distSq))
		}
		if distSq > 64*64 {
			t.Errorf("a cod stands %.2f blocks from the player, outside the 64 its category may be spawned at", math.Sqrt(distSq))
		}
	}
	// The local cap is checked before each chunk, so a chunk begun at nineteen
	// may add its pack of eight.
	if fish := fishCount(herd.capped); fish < 20 || fish > 20+8-1 {
		t.Fatalf("the ocean holds %d fish after the passes, want the cap's twenty and at most seven over", fish)
	}
	cod = fishCount(herd.capped)
	if still := fishCount(herd.after); still != cod {
		t.Errorf("the ocean holds %d fish after the cap was reached, want it held at %d", still, cod)
	}
}

// fishCount counts the cod and salmon of a herd.
func fishCount(mobs []server.TestMob) int {
	n := 0
	for _, m := range mobs {
		if m.Kind == "minecraft:cod" || m.Kind == "minecraft:salmon" {
			n++
		}
	}
	return n
}

// TestABucketKeepsTheFishsName: scooping a named cod puts its name on the
// bucket, and pouring the bucket lets out a cod with that name.
// vanilla: Bucketable.saveDefaultDataToBucketTag, Entity.applyImplicitComponents
func TestABucketKeepsTheFishsName(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	id := summonSquid(t, c, "minecraft:cod", fishType(t, "minecraft:cod"), pondCentreX, pondNorth)
	srv.GiveSlot("Striker", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:name_tag"), Count: 1, CustomName: &protocol.Text{Text: "Finny"},
	})
	c.UseOn(id)
	c.Settle()

	hotbarFish(t, srv, "minecraft:water_bucket")
	c.UseOn(id)
	c.Settle()
	bucket := srv.InventorySlot("Striker", player.HotbarSlot)
	if bucket.ItemID != servertest.MustItemID(t, "minecraft:cod_bucket") {
		t.Fatalf("the hand holds item %d, want a cod bucket", bucket.ItemID)
	}
	if bucket.CustomName == nil || bucket.CustomName.Text != "Finny" {
		t.Fatalf("the bucket's name = %+v, want the cod's Finny", bucket.CustomName)
	}

	c.MoveTo(playerX, 64, playerZ, 180, 60)
	c.Settle()
	servertest.UseHeld(c, 180, 60)
	c.Settle()
	srv.TickMobs(1)
	named := 0
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:cod" && m.Name == "Finny" {
			named++
		}
	}
	if named != 1 {
		t.Errorf("%d cod named Finny stand in the world after the pour, want one", named)
	}
}

// TestASwimmingFishSplashes: a cod that swims a few blocks plays the fish swim
// sound in the neutral category, once for each block of way scaled by 0.6.
// vanilla: Entity.applyMovementEmissionAndPlaySound, AbstractFish.getSwimSound
func TestASwimmingFishSplashes(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	id := summonSquid(t, c, "minecraft:cod", fishType(t, "minecraft:cod"), pondCentreX, -8.5)
	start, _ := fishAt(srv, id)
	last, travelled := start, 0.0
	splashes := 0
	for range 300 {
		srv.TickMobs(1)
		m, _ := fishAt(srv, id)
		travelled += math.Hypot(m.X-last.X, m.Z-last.Z)
		last = m
		for _, h := range collectSounds(c) {
			if h.name == "minecraft:entity.fish.swim" {
				splashes++
				if h.category != protocol.SoundCategoryNeutral {
					t.Errorf("the swim sound's category = %d, want neutral", h.category)
				}
			}
		}
	}
	if travelled < 3 {
		t.Fatalf("the cod swam %.2f blocks, want it wandering", travelled)
	}
	// The first splash comes with the first movement, then one per whole
	// 0.6-scaled block of way.
	if limit := int(math.Floor(travelled*0.6)) + 1; splashes == 0 || splashes > limit {
		t.Errorf("%d splashes over %.2f blocks swum, want some but no more than %d", splashes, travelled, limit)
	}
}

// TestASolitaryTropicalFishEndsItsPackOnTheLiveServer runs the server's spawn
// pass over the seeded ocean relabelled warm ocean: a pack's first tropical
// fish leads the rest and gives them its variant, so every school a pass
// makes shares its leader's variant. A fish that rolls a random pattern is
// solitary, the last of its pack, and leads nobody.
// vanilla: NaturalSpawner.spawnCategoryForPosition, TropicalFish.isMaxGroupSizeReached
func TestASolitaryTropicalFishEndsItsPackOnTheLiveServer(t *testing.T) {
	t.Parallel()
	srv, _ := waterSpawningTests(t)
	for cx := int32(-4); cx <= 4; cx++ {
		for cz := int32(-4); cz <= 4; cz++ {
			srv.SetOverworldBiome(cx*16, cz*16, "minecraft:warm_ocean")
		}
	}
	lone := 0
	for seed := int64(1); seed <= 60 && lone < 3; seed++ {
		srv.SpawnPass(seed)
		var fish []server.TestFish
		var ids []int32
		for _, m := range srv.Mobs() {
			if m.Kind != "minecraft:tropical_fish" && m.Kind != "minecraft:pufferfish" {
				continue
			}
			ids = append(ids, m.ID)
			if f, ok := srv.MobFishState(m.ID); ok && m.Kind == "minecraft:tropical_fish" {
				fish = append(fish, f)
			}
		}
		for _, leader := range fish {
			if leader.Follower {
				continue
			}
			same := 0
			for _, f := range fish {
				if f.TropicalVariant == leader.TropicalVariant {
					same++
				}
			}
			if same < leader.SchoolSize {
				t.Fatalf("pass %d: a tropical fish of variant %#x leads a school of %d, but only %d fish share its variant",
					seed, leader.TropicalVariant, leader.SchoolSize, same)
			}
			if same == 1 && leader.SchoolSize == 1 {
				lone++
			}
		}
		// Clear the fish and their corpses so the next pass has the cap's room.
		for _, id := range ids {
			srv.KillMob(id)
		}
		srv.TickMobs(21)
	}
	if lone < 3 {
		t.Fatalf("the passes made %d lone tropical fish, want at least 3 to hold the rule over", lone)
	}
}
