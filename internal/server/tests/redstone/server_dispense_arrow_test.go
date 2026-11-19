package redstone

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

func TestDispensedArrowDamageDoesNotScaleWithDifficulty(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)
	servertest.HoldTicks(t, srv)
	srv.RunLine("gamemode survival Testificate")
	srv.RunLine("gamerule natural_health_regeneration false")

	var normalLoss float64
	for _, difficulty := range []string{"normal", "hard"} {
		srv.RunLine("difficulty " + difficulty)
		w.MoveToGround(4.5, 64, 2.5, true)
		w.Settle()
		servertest.RunTicks(t, srv, 20)
		srv.SetHealth("Testificate", 20)
		if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:arrow", 1)) {
			t.Fatal("the dispenser has no block entity to fill")
		}
		// Both shots must have the same spread and therefore the same plain damage.
		srv.SetLevelSeed(21)
		srv.SetBlockAndTell(4, 64, -1, servertest.MustState(t, "minecraft:redstone_block", nil))
		servertest.UntilTicks(t, srv, 10, "the dispensed arrow hitting the player", func() bool {
			return srv.PlayerHealth("Testificate") < 20
		})
		loss := 20 - srv.PlayerHealth("Testificate")
		if difficulty == "normal" {
			normalLoss = loss
		} else if loss != normalLoss {
			t.Errorf("hard arrow damage = %g, want normal's plain damage %g", loss, normalLoss)
		}
		srv.SetBlockAndTell(4, 64, -1, servertest.MustState(t, "minecraft:air", nil))
	}
}

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

// TestADispensedTippedArrowGivesEveryEffect checks a Turtle Master arrow
// leaves both Slowness and Resistance in the body it hits.
func TestADispensedTippedArrowGivesEveryEffect(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)

	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.TippedArrow(t, "minecraft:turtle_master")) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	w.ExpectSpawned(protocol.EntityTypeArrow)

	arrows := srv.ArrowsInDimension(world.DimensionName)
	if len(arrows) != 1 {
		t.Fatalf("the dispense left %d arrows, want 1", len(arrows))
	}
	want := []string{"minecraft:slowness", "minecraft:resistance"}
	if !slices.Equal(arrows[0].PostHit, want) {
		t.Errorf("a hit leaves %v, want %v", arrows[0].PostHit, want)
	}
}
