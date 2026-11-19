package entities

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestFishCaughtCountsTheFishNotTheJunk: a catch adds one to fish_caught per
// stack in the minecraft:fishes tag it lands, and nothing for junk or
// treasure. The loot roll is seeded, so which one each catch lands is known.
// vanilla: FishingHook.retrieve
func TestFishCaughtCountsTheFishNotTheJunk(t *testing.T) {
	t.Parallel()
	roll := func(seed int64) (fish int32) {
		for _, stack := range rules.FishingLoot(rand.New(rand.NewSource(seed)), 0, false, "minecraft:plains", nil) {
			if registry.Item(stack.ItemID).InTag(registry.ItemTagIDFishes) {
				fish++
			}
		}
		return fish
	}
	var fishSeed, junkSeed int64 = -1, -1
	for seed := int64(0); fishSeed < 0 || junkSeed < 0; seed++ {
		if roll(seed) > 0 {
			fishSeed = seed
		} else {
			junkSeed = seed
		}
	}

	srv, c, _ := givenRod(t, "Fisher")
	for x := int32(1); x <= 3; x++ {
		for z := int32(2); z <= 4; z++ {
			srv.SetBlock(x, 63, z, servertest.MustState(t, "minecraft:water", nil))
		}
	}
	c.MoveTo(2.5, 64, 1.5, 0, 40)
	stat := func() int32 { return srv.StatValue("Fisher", registry.StatCustom, "minecraft:fish_caught") }

	for _, tc := range []struct {
		name string
		seed int64
		want int32
	}{{"junk", junkSeed, 0}, {"fish", fishSeed, roll(fishSeed)}} {
		servertest.UseHeld(c, 0, 40)
		c.Settle()
		if !srv.BiteBobber("Fisher", tc.seed) {
			t.Fatalf("%s: no hook was out", tc.name)
		}
		before := stat()
		servertest.UseHeld(c, 0, 40)
		c.Settle()
		if got := stat() - before; got != tc.want {
			t.Errorf("%s: fish_caught rose by %d, want %d", tc.name, got, tc.want)
		}
	}
}
