package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestADispensedArrowCanBePickedUp checks the shot is collectable and keeps
// no weapon.
func TestADispensedArrowCanBePickedUp(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)

	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:arrow", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	w.ExpectSpawned(protocol.EntityTypeArrow)

	arrows := srv.ArrowsInDimension(world.DimensionName)
	if len(arrows) != 1 {
		t.Fatalf("the dispense left %d arrows, want 1", len(arrows))
	}
	if arrows[0].Pickup != "allowed" {
		t.Errorf("the arrow's pickup is %q, want allowed", arrows[0].Pickup)
	}
	if !arrows[0].Weapon.Empty() {
		t.Errorf("the arrow has weapon %+v, want none", arrows[0].Weapon)
	}
}
