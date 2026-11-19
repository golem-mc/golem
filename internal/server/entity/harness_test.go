package entity

import (
	"github.com/golem-mc/golem/registry"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/google/uuid"
)

// TestADispenserHarnessesAGrownHappyGhastOnly: a fired harness goes on a bare
// grown happy ghast, whatever its loot flag, and not on a ghastling or a
// harnessed body.
// vanilla: HappyGhast.canDispenserEquipIntoSlot, HappyGhast.canUseSlot
func TestADispenserHarnessesAGrownHappyGhastOnly(t *testing.T) {
	harness := protocol.ItemStack{ItemID: itemID(t, "minecraft:red_harness"), Count: 3}
	ghast := NewMob(TypeHappyGhast, 1, 0.5, 100, 0.5, 1)
	if !ghast.DispenserEquip(harness) || ghast.Body.Count != 1 {
		t.Fatalf("a dispensed harness gave the body %+v, want one harness", ghast.Body)
	}
	if ghast.DispenserEquip(harness) {
		t.Error("a second harness went on a harnessed body")
	}
	ghastling := NewMob(TypeHappyGhast, 2, 0.5, 100, 0.5, 2)
	ghastling.SetBaby(true)
	if ghastling.DispenserEquip(harness) {
		t.Error("a ghastling took a dispensed harness")
	}
}

// TestAHarnessedHappyGhastSeatsFourAndHoldsStillAfterOneLeaves: a grown
// harnessed body takes riders until four sit on it; one leaving holds it
// still and out of the steerer's hands for ten ticks, counted only past the
// sixty-tick grace.
// vanilla: HappyGhast.canAddPassenger, removePassenger, tick, getControllingPassenger
func TestAHarnessedHappyGhastSeatsFourAndHoldsStillAfterOneLeaves(t *testing.T) {
	ghast := NewMob(TypeHappyGhast, 1, 0.5, 100, 0.5, 1)
	if ghast.CanBeRidden() {
		t.Fatal("a bare happy ghast can be ridden")
	}
	ghast.EquipHarness(protocol.ItemStack{ItemID: itemID(t, "minecraft:white_harness"), Count: 1})
	ghast.Board(uuid.New())
	ghast.Horse.RiderID = 2
	ghast.Horse.ExtraRiderIDs = []int32{3, 4}
	if !ghast.CanBeRidden() || !ghast.Steered() {
		t.Fatalf("three riders: ridable %v, steered %v; want true, true", ghast.CanBeRidden(), ghast.Steered())
	}
	ghast.Horse.ExtraRiderIDs = append(ghast.Horse.ExtraRiderIDs, 5)
	if ghast.CanBeRidden() {
		t.Error("a fifth rider was seated")
	}

	ghast.Horse.ExtraRiderIDs = ghast.Horse.ExtraRiderIDs[:2]
	if sound := ghast.PlayerLeft(); sound != registry.SoundNone {
		t.Errorf("a rider leaving three behind made %q", sound)
	}
	if ghast.Steered() || !ghast.StaysStill() {
		t.Fatal("the ghast took steering straight after a rider left")
	}
	ghast.happyGhastStillStep(nil)
	if ghast.stillTimeout != happyGhastStillTicks {
		t.Errorf("still timeout %d inside the grace, want %d", ghast.stillTimeout, happyGhastStillTicks)
	}
	ghast.Age = happyGhastStillGrace + 1
	for range happyGhastStillTicks {
		ghast.happyGhastStillStep(nil)
	}
	if !ghast.Steered() {
		t.Error("the ghast still holds still after ten ticks")
	}
}
