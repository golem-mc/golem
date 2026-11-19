package redstone

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: ShearsDispenseItemBehavior.tryShearEntity, CopperGolem.shear
func TestADispenserShearsACopperGolemsAntennaFlower(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)
	id := srv.SpawnMobTagged("minecraft:copper_golem", 4.5, 64, 1.5, nbt.Compound{
		"equipment": nbt.Compound{"saddle": nbt.Compound{"id": nbt.String("minecraft:poppy"), "count": nbt.Int(1)}},
		"NoAI":      nbt.Byte(1),
	})
	if id == 0 {
		t.Fatal("the copper golem was not summoned")
	}
	srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:shears", 1))
	if srv.MobEquipment(id, "antenna").Empty() {
		t.Fatal("the antenna slot did not load the flower")
	}
	w.DrainRecorded()

	shearSound := false
	dispense := func() {
		srv.Dispense(4, 64, 0, protocol.FaceSouth)
		w.SettleAndCollect(func(pid int32, data []byte) {
			w.Record(pid, data)
			if pid == protocol.IDPlayEntitySoundEffect && servertest.SoundIs(data, "minecraft:entity.copper_golem.shear") {
				shearSound = true
			}
		})
	}
	dispense()
	poppies := func() int { return countDropped(srv.ItemsInDimension(world.DimensionName), "minecraft:poppy") }
	if got := poppies(); got != 1 {
		t.Fatalf("poppies in the world = %d, want 1", got)
	}
	if !shearSound {
		t.Error("the shear sound was not heard")
	}
	if got := srv.InventoryAt(4, 64, 0)[0]; got.Count != 1 || got.Damage != 1 {
		t.Errorf("dispenser holds %+v, want shears with damage 1", got)
	}
	if !srv.MobEquipment(id, "antenna").Empty() {
		t.Error("the antenna slot still holds the flower")
	}

	w.Events = nil
	dispense()
	if !slices.Contains(w.Events, protocol.WorldEventDispenseFail) {
		t.Errorf("second dispense events %v, want the fail click", w.Events)
	}
	if got := poppies(); got != 1 {
		t.Errorf("poppies after the second dispense = %d, want still 1", got)
	}
}
