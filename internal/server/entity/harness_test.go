package entity

import (
	"github.com/golem-mc/golem/registry"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/google/uuid"
)

// TestAHarnessedHappyGhastIsTemptedByFoodAlone: once harnessed, the grown
// body's lure narrows to happy_ghast_food, as a ghastling's is.
// vanilla: HappyGhast.registerGoals
func TestAHarnessedHappyGhastIsTemptedByFoodAlone(t *testing.T) {
	harness := protocol.ItemStack{ItemID: itemID(t, "minecraft:white_harness"), Count: 1}
	snowball := protocol.ItemStack{ItemID: itemID(t, "minecraft:snowball"), Count: 1}
	ghast := NewMob(TypeHappyGhast, 1, 0.5, 100, 0.5, 1)
	if !ghast.tempts(harness) {
		t.Fatal("a bare happy ghast is not tempted by a harness")
	}
	if !ghast.EquipHarness(harness) {
		t.Fatal("a bare grown happy ghast refused a harness")
	}
	if ghast.tempts(harness) || !ghast.tempts(snowball) {
		t.Errorf("a harnessed happy ghast: tempted by harness %v, by snowball %v; want false, true",
			ghast.tempts(harness), ghast.tempts(snowball))
	}
}

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

func TestAHappyGhastHoldsStillUnderAPlayerOnTop(t *testing.T) {
	ghast := NewMob(TypeHappyGhast, 1, 0, 64, 0, 1)
	ghast.Age = happyGhastStillGrace + 1
	on := []Player{{X: 0, Y: 68, Z: 0}}
	for i := range 31 {
		ghast.happyGhastStillStep(on)
		if !ghast.StaysStill() || ghast.stillTimeout != 10 {
			t.Fatalf("step %d: timeout %d, want 10", i, ghast.stillTimeout)
		}
	}
	for name, p := range map[string]Player{
		"too high":  {X: 0, Y: 70, Z: 0},
		"too wide":  {X: 3.1, Y: 68, Z: 0},
		"spectator": {X: 0, Y: 68, Z: 0, Spectator: true},
		"on ghast":  {X: 0, Y: 68, Z: 0, OnHappyGhast: true},
	} {
		g := NewMob(TypeHappyGhast, 1, 0, 64, 0, 1)
		g.Age = happyGhastStillGrace + 1
		g.happyGhastStillStep([]Player{p})
		if g.stillTimeout != 0 {
			t.Errorf("%s: timeout %d, want 0", name, g.stillTimeout)
		}
	}
	for range 10 {
		ghast.happyGhastStillStep(nil)
	}
	if ghast.StaysStill() {
		t.Error("still held after the player left")
	}
}
