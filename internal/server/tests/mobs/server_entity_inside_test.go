package mobs

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// Block contacts of a mob standing in a crop or an open eyeblossom, black box.
// vanilla: CropBlock.entityInside, EyeblossomBlock.entityInside

// ravagerInWheat stands a ravager over a row of mature wheat on a held loop,
// mob_griefing as given, and runs the contact tick. It returns the cells of
// the row still planted and the item names on the ground.
func ravagerInWheat(t *testing.T, griefing bool) (planted []int32, items []string) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Farmer", servertest.OpsListing(t, "Farmer"))
	servertest.HoldTicks(t, srv)
	c.Settle()
	srv.SetGameRule("mob_griefing", griefing)
	farmland := servertest.MustState(t, "minecraft:farmland", map[string]string{"moisture": "7"})
	crop := servertest.MustState(t, "minecraft:wheat", map[string]string{"age": "7"})
	for x := int32(2); x <= 7; x++ {
		srv.SetBlock(x, 63, 0, farmland)
		srv.SetBlock(x, 64, 0, crop)
	}
	id := srv.SpawnMob("minecraft:ravager", 4.5, 64, 0.5)
	if !srv.SetMobNoAI(id) {
		t.Fatal("the ravager could not be held in the wheat")
	}
	servertest.RunTicks(t, srv, 1)
	for x := int32(2); x <= 7; x++ {
		if srv.BlockAt(x, 64, 0) == crop {
			planted = append(planted, x)
		}
	}
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		items = append(items, it.Item)
	}
	return planted, items
}

// TestARavagerTramplesWheatItStandsIn: the cells under its box turn to air
// and drop wheat; the wheat beyond its reach stays.
func TestARavagerTramplesWheatItStandsIn(t *testing.T) {
	t.Parallel()
	planted, items := ravagerInWheat(t, true)
	// The ravager's 1.95-wide box at x 4.5 covers cells 3 to 5.
	if want := []int32{2, 6, 7}; !slices.Equal(planted, want) {
		t.Errorf("wheat stands at %v, want %v", planted, want)
	}
	if !slices.Contains(items, "minecraft:wheat") {
		t.Errorf("drops = %v, want wheat among them", items)
	}
}

// beeInEyeblossom stands a bee inside an open eyeblossom on a held loop at
// the difficulty and returns its poison after the contact tick and after the
// tick that follows.
func beeInEyeblossom(t *testing.T, difficulty uint8) (first, second int32, ok bool) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Botanist", servertest.OpsListing(t, "Botanist"))
	servertest.HoldTicks(t, srv)
	c.Settle()
	srv.SetDifficulty(difficulty)
	srv.SetBlock(4, 64, 0, servertest.BlockState(t, "minecraft:open_eyeblossom"))
	bee := srv.SpawnMob("minecraft:bee", 4.5, 64, 0.5)
	if !srv.SetMobNoAI(bee) {
		t.Fatal("the bee could not be held in the flower")
	}
	servertest.RunTicks(t, srv, 1)
	first, _, ok = srv.MobEffectOf(bee, "minecraft:poison")
	servertest.RunTicks(t, srv, 1)
	second, _, _ = srv.MobEffectOf(bee, "minecraft:poison")
	return first, second, ok
}

// TestAnOpenEyeblossomPoisonsABee: poison for 25 ticks, ticking down and not
// refreshed while it lasts.
func TestAnOpenEyeblossomPoisonsABee(t *testing.T) {
	t.Parallel()
	first, second, ok := beeInEyeblossom(t, rules.DifficultyNormal)
	if !ok || first != 25 || second != 24 {
		t.Errorf("poison = %d then %d, %v; want 25 then 24", first, second, ok)
	}
}
