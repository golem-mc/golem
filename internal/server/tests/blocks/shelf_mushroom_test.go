package blocks

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestShelfMushroomHangsOnAWall clicks the north face of stone with a shelf
// mushroom (it faces away from the stone), clicks the top of a block with no
// wall beside the cell (nothing is placed), then takes the stone away, which
// pops the mushroom.
// vanilla: ShelfMushroomBlock.getStateForPlacement, canSurvive, updateShape
func TestShelfMushroomHangsOnAWall(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 6, "stone")
	servertest.PlaceFrom(c, "minecraft:shelf_mushroom", 2, 3, 0, 2, 64, 6, protocol.FaceNorth)
	servertest.ExpectBlockNamed(t, srv, 2, 64, 5, "minecraft:shelf_mushroom", map[string]string{"facing": "north", "age": "0"})

	servertest.Setblock(t, c, 8, 64, 6, "stone")
	servertest.PlaceFrom(c, "minecraft:shelf_mushroom", 8, 3, 0, 8, 64, 6, protocol.FaceTop)
	if got := srv.BlockAt(8, 65, 6); got != 0 {
		name, props, _ := registry.BlockStateName(got)
		t.Errorf("a top click with no wall beside placed %s[%s], want nothing", name, props)
	}

	if id, _ := registry.ItemID("minecraft:shelf_mushroom"); servertest.RemoveAndAwaitDrop(t, c, 2, 64, 6) != id {
		t.Error("the popped shelf mushroom did not drop itself")
	}
	if got := srv.BlockAt(2, 64, 5); got != 0 {
		t.Errorf("the shelf mushroom stands as state %d after its wall went, want air", got)
	}
}

// TestBoneMealGrowsAShelfMushroomOnce: the first bone meal ages the mushroom
// to 1 and is spent; the second is refused and kept.
// vanilla: ShelfMushroomBlock.isValidBonemealTarget, performBonemeal
func TestBoneMealGrowsAShelfMushroomOnce(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, survivalPatchConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))
	srv.StopTick()

	servertest.SetFarmBlock(t, c, srv, 5, 64, 1, "minecraft:stone", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:shelf_mushroom", map[string]string{"facing": "north", "age": "0"})
	boneMeal, _ := registry.ItemID("minecraft:bone_meal")
	srv.GiveSlot("Gardener", player.HotbarSlot, protocol.ItemStack{ItemID: boneMeal, Count: 3})
	c.MoveToGround(4.5, 64, -1.5, true)
	c.Settle()

	for i, want := range []int32{2, 2} {
		c.UseBlock(5, 64, 0)
		c.Settle()
		srv.FlushBlockAcks()
		servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:shelf_mushroom", map[string]string{"facing": "north", "age": "1"})
		if got := srv.InventorySlot("Gardener", player.HotbarSlot); got.ItemID != boneMeal || got.Count != want {
			t.Errorf("held slot after bone meal %d = %+v, want %d bone meals", i+1, got, want)
		}
	}
}

// TestShelfMushroomLandingHalvesTheFallAndBounces drops a player ten blocks
// onto a shelf mushroom: the fall counts half (5 - 3 = 2 damage) and the
// bounce sound reaches the lander, whose client does not play it itself.
// vanilla: Blocks.SHELF_MUSHROOM fallDistanceReduction, ShelfMushroomBlock.bounceOn
func TestShelfMushroomLandingHalvesTheFallAndBounces(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, survivalPatchConfig(), "Faller")
	srv.StopTick()

	srv.SetLoadedBlock(0, 63, 1, servertest.BlockState(t, "minecraft:stone"))
	srv.SetLoadedBlock(0, 63, 0, servertest.MustState(t, "minecraft:shelf_mushroom", map[string]string{"facing": "north", "age": "0"}))
	top := 63 + 11.0/16 // the cap's collision top
	c.MoveToGround(0.5, top+10, 0.5, false)
	c.Settle()
	c.MoveToGround(0.5, top, 0.5, true)

	bounce, _ := registry.SoundEventID("minecraft:block.shelf_mushroom.bounce")
	var heard bool
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySoundEffect {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt()-1 != bounce {
			return
		}
		heard = true
		if category := r.ReadVarInt(); category != protocol.SoundCategoryBlock {
			t.Errorf("bounce sound category = %d, want blocks", category)
		}
		if x, y, z := r.ReadI32(), r.ReadI32(), r.ReadI32(); x != 4 || y != 63*8+4 || z != 4 {
			t.Errorf("bounce sound at fixed (%d, %d, %d), want the mushroom's centre", x, y, z)
		}
	})
	if !heard {
		t.Error("the landing on the shelf mushroom played no bounce sound")
	}
	if got := srv.PlayerHealth("Faller"); math.Abs(got-18) > 1e-3 {
		t.Errorf("health after the ten-block fall onto the shelf mushroom = %v, want 18", got)
	}
}
