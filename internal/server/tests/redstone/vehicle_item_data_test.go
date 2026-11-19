package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: MinecartDispenseItemBehavior.execute, BoatDispenseItemBehavior.execute
func TestDispensedVehiclesLoadTheirItemsEntityData(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		item       string
		boat, data bool
	}{
		{"minecraft:hopper_minecart", false, true},
		{"minecraft:oak_chest_boat", true, true},
		// EntityType.updateCustomEntityTag rejects these without an operator.
		{"minecraft:command_block_minecart", false, false},
	} {
		t.Run(tc.item, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.Dispensing(t)
			state := registry.BlockRail.DefaultState()
			if tc.boat {
				state = registry.BlockWater.DefaultState()
			}
			srv.SetBlock(4, 64, 1, state)
			stack := servertest.ItemStack(t, tc.item, 1)
			stack.CustomName = &protocol.Text{Text: "Item"}
			typ, _ := registry.EntityTypeID(tc.item)
			data := nbt.Compound{
				"Invulnerable": nbt.Byte(1), "CustomName": nbt.String("Data"),
				"CustomNameVisible": nbt.Byte(1), "Enabled": nbt.Byte(0),
			}
			if tc.boat {
				// Dispensers load Pos without the hand placement collision gate.
				data["Pos"] = nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(6.5), nbt.Double(64), nbt.Double(0.5)}}
				srv.SetBlock(6, 64, 0, registry.BlockStone.DefaultState())
			}
			if !stack.SetEntityData(typ, data) || !srv.SetInventoryAt(4, 64, 0, 0, stack) {
				t.Fatal("could not fill the dispenser with the vehicle stack")
			}
			srv.Dispense(4, 64, 0, protocol.FaceSouth)
			var tag nbt.Compound
			if tc.boat {
				ids := srv.BoatIDs()
				if len(ids) != 1 {
					t.Fatalf("boats = %v, want one", ids)
				}
				tag, _ = srv.BoatSaveTag(ids[0])
				x, y, z, _, _, _ := srv.BoatState(ids[0])
				if x != 6.5 || y != 64 || z != 0.5 {
					t.Errorf("boat position = (%v, %v, %v), want (6.5, 64, 0.5) despite the block", x, y, z)
				}
			} else {
				carts := srv.MinecartsInDimension("minecraft:overworld")
				if len(carts) != 1 {
					t.Fatalf("carts = %v, want one", carts)
				}
				tag, _ = srv.MinecartSaveTag(carts[0].ID)
			}
			wantName := nbt.String("Item")
			if tc.data {
				wantName = "Data"
			}
			if tag["CustomName"] != wantName || nbt.BoolField(tag, "Invulnerable") != tc.data || nbt.BoolField(tag, "CustomNameVisible") != tc.data {
				t.Errorf("vehicle tag = %v, want name %s and flags %v", tag, wantName, tc.data)
			}
			if tc.item == "minecraft:hopper_minecart" && tag["Enabled"] != nbt.Byte(0) {
				t.Errorf("Enabled = %v, want 0b", tag["Enabled"])
			}
			if left := srv.InventoryAt(4, 64, 0)[0]; !left.Empty() {
				t.Errorf("dispenser slot = %v, want the vehicle spent", left)
			}
		})
	}
}
