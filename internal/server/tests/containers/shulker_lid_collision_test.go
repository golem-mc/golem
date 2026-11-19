package containers_test

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestAnItemOnAnOpenShulkerBoxStaysLiftedUntilTheLidCloses checks that the
// box's collision grows with its lid: an item on top rides up half a block and
// stays there while the box is open, then falls back once it has closed.
// vanilla: ShulkerBoxBlockEntity.getBoundingBox
func TestAnItemOnAnOpenShulkerBoxStaysLiftedUntilTheLidCloses(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:shulker_box", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()

	srv.SpawnItemEntityMotion(4.5, 65.2, 0.5, "minecraft:stick", 1, 0, 0, 0)
	y := func() float64 {
		items := srv.ItemsInDimension(world.DimensionName)
		if len(items) != 1 {
			return -1
		}
		return items[0].Y
	}
	near := func(want float64) func() bool { return func() bool { return math.Abs(y()-want) < 1e-6 } }
	servertest.WaitForTicks(t, srv, 40, near(65+entity.ItemHalfSize))

	w.UseBlock(4, 64, 0)
	servertest.WaitForTicks(t, srv, 40, near(65.5+entity.ItemHalfSize))
	if got := y(); math.Abs(got-(65.5+entity.ItemHalfSize)) > 1e-6 {
		t.Fatalf("the item rests at y %v on the open box, want 65.5 plus the item's half size", got)
	}
	servertest.RunTicks(t, srv, 20)
	if got := y(); math.Abs(got-(65.5+entity.ItemHalfSize)) > 1e-6 {
		t.Errorf("the item fell to y %v while the box was open, want 65.5 plus the item's half size", got)
	}

	w.CloseWindow()
	servertest.WaitForTicks(t, srv, 60, near(65+entity.ItemHalfSize))
	if got := y(); math.Abs(got-(65+entity.ItemHalfSize)) > 1e-6 {
		t.Errorf("the item rests at y %v after the lid closed, want 65 plus the item's half size", got)
	}
}
