package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// The components a placed container takes from the item it was placed from:
// the minecraft:container slots ride into the block entity's own slots, and
// the minecraft:custom_name names its window. vanilla:
// BlockEntity.applyComponentsFromItemStack.

// TestAPlacedShulkerBoxKeepsItsContentsAndName places a shulker box carrying
// diamonds and a custom name, opens it, and checks the window shows both:
// the contents in the box's own slots and the name as its title.
func TestAPlacedShulkerBoxKeepsItsContentsAndName(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	diamonds := servertest.ItemStack(t, "minecraft:diamond", 64)
	name := protocol.Text{Text: "Loot"}
	box := servertest.ItemStack(t, "minecraft:shulker_box", 1)
	box.Container = []protocol.ItemStack{diamonds}
	box.CustomName = &name
	w.SetCreativeSlot(36, box)

	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()

	// The block entity the placement stood up carries the stack's slots.
	held := srv.InventoryAt(4, 64, 0)
	if len(held) != 27 {
		t.Fatalf("the placed box holds %d slots, want 27", len(held))
	}
	if !held[0].Equal(diamonds) {
		t.Errorf("the placed box's slot 0 = %+v, want the 64 diamonds it was placed with", held[0])
	}

	w.UseBlock(4, 64, 0)
	slots, title := w.ExpectOpenTitle(protocol.MenuShulkerBox, protocol.BlockEntityShulkerBox, 4, 64, 0)
	if len(slots) != 27+36 {
		t.Fatalf("the shulker window holds %d slots, want 27+36", len(slots))
	}
	if !slots[0].Equal(diamonds) {
		t.Errorf("the shulker window's slot 0 = %+v, want the 64 diamonds the box was placed with", slots[0])
	}
	if title.Text != "Loot" {
		t.Errorf("the shulker window's title = %+v, want Loot", title)
	}
}

// TestAPlacedContainerKeepsItsCustomName checks the custom name reaches every
// container's window, not only the shulker box's: a container placed from a
// named item opens under that name.
func TestAPlacedContainerKeepsItsCustomName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		item   string
		menu   int32
		beType int32
		slots  int
	}{
		{"chest", "minecraft:chest", protocol.MenuGeneric9x3, protocol.BlockEntityChest, 27 + 36},
		{"barrel", "minecraft:barrel", protocol.MenuGeneric9x3, protocol.BlockEntityBarrel, 27 + 36},
		{"furnace", "minecraft:furnace", protocol.MenuFurnace, protocol.BlockEntityFurnace, 3 + 36},
		{"hopper", "minecraft:hopper", protocol.MenuHopper, protocol.BlockEntityHopper, 5 + 36},
		{"dispenser", "minecraft:dispenser", protocol.MenuGeneric3x3, protocol.BlockEntityDispenser, 9 + 36},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

			name := protocol.Text{Text: "Keep"}
			named := servertest.ItemStack(t, tc.item, 1)
			named.CustomName = &name
			w.SetCreativeSlot(36, named)

			w.PlaceBlock(4, 63, 0, protocol.FaceTop)
			w.ExpectBlockAck()

			w.UseBlock(4, 64, 0)
			slots, title := w.ExpectOpenTitle(tc.menu, tc.beType, 4, 64, 0)
			if len(slots) != tc.slots {
				t.Errorf("the %s window holds %d slots, want %d", tc.name, len(slots), tc.slots)
			}
			if title.Text != "Keep" {
				t.Errorf("the %s window's title = %+v, want the item's custom name Keep", tc.name, title)
			}
		})
	}
}

// TestBreakingANamedContainerDropsItNamed places a named chest and breaks it
// in survival: the dropped item carries the name back through the loot
// table's copy_components, the round trip whose other half is containerPlaced.
func TestBreakingANamedContainerDropsItNamed(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Packrat")

	const x, y, z = int32(2), int32(64), int32(2)
	named := servertest.ItemStack(t, "minecraft:chest", 1)
	name := protocol.Text{Text: "Keep"}
	named.CustomName = &name
	srv.GiveSlot("Packrat", player.HotbarSlot, named)
	c.MoveStep(0.5, 64, 0.5, true)
	c.PlaceBlock(x, y-1, z, protocol.FaceTop)
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("acknowledged sequence = %d, want 1", seq)
	}
	c.SettleAndCollect(func(int32, []byte) {})
	if tag, ok := srv.BlockEntityTagAt(x, y, z); !ok {
		t.Fatal("the placed chest has no block entity")
	} else if _, has := tag["CustomName"]; !has {
		t.Fatal("the placed chest saved no CustomName to drop")
	}

	c.BreakBlock(x, y, z)
	_, drop := c.WaitBreakSettled()
	if drop.Stack.CustomName == nil || drop.Stack.CustomName.Text != "Keep" {
		t.Errorf("the drop = %+v, want a chest named Keep", drop.Stack)
	}
}

// TestABrokenBannerDropsItsPatterns places a patterned, named banner and
// breaks it: the drop keeps both through copy_components.
// vanilla: BannerBlockEntity.collectImplicitComponents
func TestABrokenBannerDropsItsPatterns(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Herald")

	const x, y, z = int32(2), int32(64), int32(2)
	banner := servertest.ItemStack(t, "minecraft:white_banner", 1)
	layer := protocol.BannerPatternLayer{Pattern: 0, Dye: 14}
	if !banner.SetBannerPatterns([]protocol.BannerPatternLayer{layer}) {
		t.Fatal("SetBannerPatterns refused the layer")
	}
	name := protocol.Text{Text: "Flag"}
	banner.CustomName = &name
	srv.GiveSlot("Herald", player.HotbarSlot, banner)
	c.MoveStep(0.5, 64, 0.5, true)
	c.PlaceBlock(x, y-1, z, protocol.FaceTop)
	c.ExpectBlockAck()
	c.SettleAndCollect(func(int32, []byte) {})
	if _, ok := srv.BlockEntityTagAt(x, y, z); !ok {
		t.Fatal("the placed banner has no block entity")
	}

	c.BreakBlock(x, y, z)
	_, drop := c.WaitBreakSettled()
	if layers, ok := drop.Stack.BannerPatternsOf(); !ok || len(layers) != 1 || layers[0] != layer {
		t.Errorf("the dropped banner's patterns = %v (ok %v), want [%v]", layers, ok, layer)
	}
	if drop.Stack.CustomName == nil || drop.Stack.CustomName.Text != "Flag" {
		t.Errorf("the drop = %+v, want a banner named Flag", drop.Stack)
	}
}
