package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// Bees breed on flowers, follow one held out, and fly round what is in the
// way. vanilla: Bee.registerGoals, FlyingPathNavigation

// beeWithFlowers is beeWorld with a stack of dandelions in the player's hand.
func beeWithFlowers(t *testing.T) (*servertest.Client, *server.Server) {
	t.Helper()
	c, _, srv := beeWorld(t, rules.DifficultyNormal)
	srv.GiveSlot("Slayer", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:dandelion"), Count: 16})
	return c, srv
}

func TestTwoFlowerFedBeesBreedABabyBee(t *testing.T) {
	t.Parallel()
	c, srv := beeWithFlowers(t)
	a := srv.SpawnMob(bee, 4.5, 64, 2.5)
	b := srv.SpawnMob(bee, 5.5, 64, 2.5)
	servertest.RunTicks(t, srv, 1)
	c.UseOn(a)
	c.UseOn(b)
	c.Settle()
	for range 40 {
		servertest.RunTicks(t, srv, 10)
		if len(srv.MobIDsOfKind(bee)) == 3 {
			break
		}
	}
	ids := srv.MobIDsOfKind(bee)
	if len(ids) != 3 {
		t.Fatalf("%d bees after feeding two flowers, want a baby among three", len(ids))
	}
	babies := 0
	for _, id := range ids {
		if srv.MobIsBaby(id) {
			babies++
		}
	}
	if babies != 1 {
		t.Fatalf("%d baby bees, want one", babies)
	}
}

func TestAFlowerFedBeeFallsInLove(t *testing.T) {
	t.Parallel()
	c, srv := beeWithFlowers(t)
	id := srv.SpawnMob(bee, 4.5, 64, 2.5)
	c.UseOn(id)
	c.AwaitEntityEvent(id, loveHearts)
	if m, _ := mobWithID(srv, id); m.InLove != 600 {
		t.Fatalf("a fed bee loves for %d ticks, want the 600 of Animal.setInLove", m.InLove)
	}
	if got := srv.InventorySlot("Slayer", player.HotbarSlot).Count; got != 15 {
		t.Fatalf("%d flowers left of 16, want one spent", got)
	}
}

// The wither rose and the open eyeblossom give a bee an effect, not love,
// and are used up. vanilla: Bee.mobInteract, FlowerBlock.getBeeInteractionEffect
func TestTheBeeEffectFlowersGiveABeeAnEffect(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		flower, effect string
		duration       int32
	}{
		{"minecraft:wither_rose", "minecraft:wither", 40},
		{"minecraft:open_eyeblossom", "minecraft:poison", 25},
	} {
		t.Run(tc.flower, func(t *testing.T) {
			t.Parallel()
			c, srv := beeWithFlowers(t)
			srv.GiveSlot("Slayer", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, tc.flower), Count: 4})
			id := srv.SpawnMob(bee, 4.5, 64, 2.5)
			c.UseOn(id)
			c.Settle()
			if m, _ := mobWithID(srv, id); m.InLove != 0 {
				t.Fatalf("a bee fed %s loves for %d ticks, want none", tc.flower, m.InLove)
			}
			if got := srv.InventorySlot("Slayer", player.HotbarSlot).Count; got != 3 {
				t.Fatalf("%d of %s left of 4, want one spent", got, tc.flower)
			}
			if d, _, ok := srv.MobEffectOf(id, tc.effect); !ok || d > tc.duration || d < tc.duration-5 {
				t.Fatalf("%s on the bee: %d ticks, %v; want about %d", tc.effect, d, ok, tc.duration)
			}
		})
	}
}

// beeFarthestFrom runs 30 samples of 10 ticks after 100 of approach and
// reports the farthest the bee stood from (x, z) in them.
func beeFarthestFrom(t *testing.T, srv *server.Server, id int32, x, z float64) float64 {
	t.Helper()
	servertest.RunTicks(t, srv, 100)
	far := 0.0
	for range 30 {
		servertest.RunTicks(t, srv, 10)
		bx, _, bz, _ := srv.MobPos(id)
		far = math.Max(far, math.Hypot(bx-x, bz-z))
	}
	return far
}

func TestATemptedBeeKeepsBesideTheFlowerHolder(t *testing.T) {
	t.Parallel()
	_, srv := beeWithFlowers(t)
	id := srv.SpawnMob(bee, 9.5, 65, 0.5)
	// TemptGoal stops the walk 2.5 blocks from the player (2.5, 0.5); a
	// wandering bee leaves that circle within the 300 ticks.
	if far := beeFarthestFrom(t, srv, id, 2.5, 0.5); far > 4.5 {
		t.Fatalf("the tempted bee strays %.1f blocks from the flower holder, want it kept within 4.5", far)
	}
}

func TestABabyBeeFollowsAnAdultBee(t *testing.T) {
	t.Parallel()
	_, _, srv := beeWorld(t, rules.DifficultyNormal)
	adult := srv.SpawnMob(bee, 12.5, 65, 0.5)
	srv.SetMobNoAI(adult)
	baby := srv.SpawnMob(bee, 4.5, 65, 0.5)
	srv.SetMobBaby(baby, true)
	// FollowParentGoal walks the baby to within three blocks of the adult
	// and keeps it there; a wandering one leaves.
	if far := beeFarthestFrom(t, srv, baby, 12.5, 0.5); far > 4 {
		t.Fatalf("the baby bee strays %.1f blocks from the adult, want it kept within 4", far)
	}
}

// wallAcross raises a stone wall at x = 5 over z -8..8 and y 64..71, leaving
// the cells of z 6..8 open.
func wallAcross(t *testing.T, srv *server.Server) {
	t.Helper()
	stone := registry.MustState("minecraft:stone", nil)
	for y := int32(64); y <= 71; y++ {
		for z := int32(-8); z <= 5; z++ {
			srv.SetBlock(5, y, z, stone)
		}
	}
}

func TestATemptedBeeFliesRoundAWallToTheFlower(t *testing.T) {
	t.Parallel()
	_, srv := beeWithFlowers(t)
	wallAcross(t, srv)
	id := srv.SpawnMob(bee, 9.5, 65, 0.5)
	for range 60 {
		servertest.RunTicks(t, srv, 10)
		x, _, z, _ := srv.MobPos(id)
		if x < 4 && math.Abs(z) < 4 {
			return
		}
	}
	x, y, z, _ := srv.MobPos(id)
	t.Fatalf("the bee stands at %.1f %.1f %.1f, never round the wall to the flower", x, y, z)
}

func TestABeeGrowingACropSparklesOverIt(t *testing.T) {
	t.Parallel()
	c, _, srv := beeWorld(t, rules.DifficultyNormal)
	putHive(srv, 5, 64, 2, hiveState(t, "minecraft:bee_nest", "[facing=north,honey_level=0]"))
	farmland := registry.MustState("minecraft:farmland", map[string]string{"moisture": "7"})
	carrots := registry.MustState("minecraft:carrots", map[string]string{"age": "0"})
	for x := int32(8); x <= 24; x++ {
		for z := int32(-8); z <= 8; z++ {
			srv.SetBlock(x, 63, z, farmland)
			srv.SetBlock(x, 64, z, carrots)
		}
	}
	c.ChatCommand("summon minecraft:bee 16.5 65.5 0.5 {HasNectar:1b,hive_pos:[I;5,64,2],CannotEnterHiveTicks:24000}")
	c.Settle()
	seen := c.ReadTicks(srv, 1500, func(id int32, data []byte) bool {
		return id == protocol.IDPlayWorldEvent && protocol.NewReader(data).ReadI32() == protocol.WorldEventBeeGrowth
	})
	if !seen {
		t.Fatal("no growth particles in 1500 ticks of a nectar bee over carrots")
	}
	grown := 0
	for x := int32(8); x <= 24; x++ {
		for z := int32(-8); z <= 8; z++ {
			if srv.BlockAt(x, 64, z) != carrots {
				grown++
			}
		}
	}
	if grown == 0 {
		t.Fatal("the particles played but no carrot grew")
	}
}
