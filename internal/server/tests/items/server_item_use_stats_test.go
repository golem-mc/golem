package items

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func expectUsed(t *testing.T, srv *server.Server, item string, want int32) {
	t.Helper()
	if got := itemStat(srv, registry.StatUsed, item); got != want {
		t.Errorf("used %s = %d, want %d", item, got, want)
	}
}

// underFeet is the cell the actor stands on.
func underFeet(srv *server.Server) (x, y, z int32) {
	pos := srv.PlayerPosition("Actor")
	return int32(math.Floor(pos.X)), int32(math.Floor(pos.Y)) - 1, int32(math.Floor(pos.Z))
}

func giveStack(t *testing.T, srv *server.Server, slot int, item string, n int32) {
	t.Helper()
	srv.GiveSlot("Actor", slot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, item), Count: n})
}

// aimedAtWater is a scene whose actor holds item and uses it at a pool of
// water a block ahead, looking down at it.
func aimedAtWater(t *testing.T, item string) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, actor, _ := servertest.ToolScene(t, item)
	// Away from the watcher, whose body a boat's eye check would hit.
	actor.MoveTo(8.5, 64, 0.5, 0, 45)
	actor.Settle()
	x, y, z := underFeet(srv)
	// A pool wide enough for a boat's hull to float in.
	for dx := int32(-2); dx <= 2; dx++ {
		for dz := int32(0); dz <= 4; dz++ {
			srv.SetBlock(x+dx, y, z+dz, registry.MustState("minecraft:water", nil))
		}
	}
	actor.Settle()
	// Aimed ahead and down so the hull floats clear of the actor's own box.
	servertest.UseHeld(actor, 0, 45)
	actor.Settle()
	return srv, actor
}

// TestScoopingWithABucketCountsAsUsed: vanilla: BucketItem.use
func TestScoopingWithABucketCountsAsUsed(t *testing.T) {
	t.Parallel()
	srv, _ := aimedAtWater(t, "minecraft:bucket")
	expectUsed(t, srv, "minecraft:bucket", 1)
}

// TestAShotBowCountsAsUsed: vanilla: BowItem.releaseUsing
func TestAShotBowCountsAsUsed(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:bow")
	giveStack(t, srv, player.HotbarSlot+1, "minecraft:arrow", 5)
	actor.Settle()

	servertest.DrawBow(actor, -90, 0)
	actor.Settle()
	for i := 0; srv.BowTicks("Actor") < 15; i++ {
		if i == 40 {
			t.Fatalf("40 ticks drew the bow %d ticks, want 15", srv.BowTicks("Actor"))
		}
		srv.TickSurvival()
	}
	servertest.ReleaseBow(actor)
	actor.Settle()

	expectUsed(t, srv, "minecraft:bow", 1)
}

// TestAnEatenMealCountsAsUsed: vanilla: Consumable.onConsume
func TestAnEatenMealCountsAsUsed(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:bread")
	srv.SetFood("Actor", 10)
	actor.UseItemRightClicks(protocol.HandMain)
	actor.Settle()
	for range 40 {
		srv.TickSurvival()
	}
	actor.Settle()
	expectUsed(t, srv, "minecraft:bread", 1)
}

// mineStoneWith has a survival actor break stone with the stack and returns
// the server once the cell is air.
func mineStoneWith(t *testing.T, stack protocol.ItemStack) *server.Server {
	t.Helper()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:stone")
	srv.GiveSlot("Actor", player.HotbarSlot, stack)
	srv.SetBlock(3, 64, 0, registry.MustState("minecraft:stone", nil))
	actor.Settle()

	actor.BreakBlock(3, 64, 0)
	actor.Settle()
	for range 200 {
		if srv.BlockAt(3, 64, 0) == registry.MustState("minecraft:air", nil) {
			break
		}
		srv.RunTick()
	}
	actor.Settle()
	if srv.BlockAt(3, 64, 0) != registry.MustState("minecraft:air", nil) {
		t.Fatal("the stone was never broken")
	}
	return srv
}

// TestMiningABlockCountsTheToolAsUsed: vanilla: ItemStack.mineBlock
func TestMiningABlockCountsTheToolAsUsed(t *testing.T) {
	t.Parallel()
	pick := servertest.EnchantedStack(t, "minecraft:netherite_pickaxe", "minecraft:efficiency", 5)
	expectUsed(t, mineStoneWith(t, pick), "minecraft:netherite_pickaxe", 1)
}

// useKnowledgeBook has the actor use a knowledge book naming recipes and
// reports how many Recipe Book Adds it was sent. vanilla: KnowledgeBookItem.use
func useKnowledgeBook(t *testing.T, recipes ...string) (*server.Server, int) {
	t.Helper()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:knowledge_book")
	book := srv.InventorySlot("Actor", player.HotbarSlot)
	book.Recipes = recipes
	srv.GiveSlot("Actor", player.HotbarSlot, book)
	actor.Settle()
	actor.WatchBook = true
	servertest.UseHeld(actor, 0, 0)
	adds := 0
	actor.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlayRecipeBookAdd {
			adds++
		}
	})
	if got := srv.InventorySlot("Actor", player.HotbarSlot); !got.Empty() {
		t.Errorf("the knowledge book was not spent: %+v", got)
	}
	return srv, adds
}
