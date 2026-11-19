package entities

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: MinecartItem.useOn, VehicleEntity.destroy
func TestAHandPlacedMinecartKeepsItsItemNameThroughBreaking(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.GameMode = "survival"
	srv, c := servertest.Join(t, cfg, "Carter", servertest.OpsListing(t, "Carter"))
	servertest.HoldTicks(t, srv)
	servertest.CartRails(t, srv, 3, 6)
	c.MoveToGrounded(4.5, 64, 2.9, 180, 60, true)
	c.Settle()
	srv.GiveSlot("Carter", player.HotbarSlot, protocol.ItemStack{
		ItemID: int32(registry.ItemMinecart), Count: 1, CustomName: &protocol.Text{Text: "Cart"},
	})
	c.PlaceBlock(4, 64, 1, protocol.FaceTop)
	spawn := awaitMinecartSpawn(c)
	servertest.RunTicks(t, srv, 1)
	if got := awaitEntityDataCompound(t, srv, c, "@e[type=minecart,limit=1]"); !strings.Contains(got, `CustomName: "Cart"`) {
		t.Errorf("placed cart data = %s, want CustomName Cart", got)
	}
	if got := srv.InventorySlot("Carter", player.HotbarSlot); !got.Empty() {
		t.Errorf("held stack = %v, want the named cart spent", got)
	}

	srv.GiveSlot("Carter", player.HotbarSlot, servertest.ItemStack(t, "minecraft:diamond_sword", 1))
	servertest.AwaitFullSwing(t, srv, "Carter")
	c.Attack(spawn.EntityID)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 {
		t.Fatalf("drops = %v, want one named cart", items)
	}
	if drop := items[0].Stack; registry.Item(drop.ItemID) != registry.ItemMinecart || drop.CustomName == nil || drop.CustomName.Plain() != "Cart" {
		t.Errorf("drop = %v, want minecart named Cart", drop)
	}
}

// vanilla: AbstractMinecart.createMinecart, EntityType.createDefaultStackConfig
func TestAHandPlacedMinecartLoadsMatchingEntityData(t *testing.T) {
	t.Parallel()
	for _, matching := range []bool{true, false} {
		t.Run(map[bool]string{true: "matching", false: "other_type"}[matching], func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.GameMode = "survival"
			srv, c := servertest.Join(t, cfg, "Carter")
			servertest.HoldTicks(t, srv)
			servertest.CartRails(t, srv, 3, 6)
			c.MoveToGrounded(4.5, 64, 2.9, 180, 60, true)
			c.Settle()
			typ := int32(protocol.EntityTypeHopperMinecart)
			if !matching {
				typ = protocol.EntityTypeMinecart
			}
			stack := servertest.ItemStack(t, "minecraft:hopper_minecart", 1)
			stack.CustomName = &protocol.Text{Text: "Item"}
			stack.Raw = []protocol.RawComponent{entityDataComponent(t, typ, nbt.Compound{
				"Enabled": nbt.Byte(0), "Invulnerable": nbt.Byte(1),
				"CustomName": nbt.String("Data"), "CustomNameVisible": nbt.Byte(1),
			})}
			srv.GiveSlot("Carter", player.HotbarSlot, stack)
			c.PlaceBlock(4, 64, 1, protocol.FaceTop)
			spawn := servertest.AwaitBoatSpawn(c, protocol.EntityTypeHopperMinecart)
			tag, _ := srv.MinecartSaveTag(spawn.EntityID)
			wantName, wantEnabled := nbt.String("Item"), nbt.Byte(1)
			if matching {
				wantName, wantEnabled = "Data", 0
			}
			if tag["CustomName"] != wantName || tag["Enabled"] != wantEnabled || nbt.BoolField(tag, "Invulnerable") != matching || nbt.BoolField(tag, "CustomNameVisible") != matching {
				t.Errorf("cart tag = %v, want name %s, Enabled %d, flags %v", tag, wantName, wantEnabled, matching)
			}
		})
	}
}

// vanilla: BoatItem.getBoat, EntityType.createDefaultStackConfig
func TestAHandPlacedBoatLoadsMatchingEntityData(t *testing.T) {
	t.Parallel()
	for _, matching := range []bool{true, false} {
		t.Run(map[bool]string{true: "matching", false: "other_type"}[matching], func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.GameMode = "survival"
			srv, c := servertest.Join(t, cfg, "Sailor", servertest.OpsListing(t, "Sailor"))
			servertest.HoldTicks(t, srv)
			c.MoveToGrounded(0.5, 64, 0.5, 0, 0, true)
			for x := int32(2); x <= 3; x++ {
				for z := int32(0); z <= 1; z++ {
					srv.SetBlock(x, 63, z, registry.BlockWater.DefaultState())
				}
			}
			c.Settle()
			typ := int32(protocol.EntityTypeOakBoat)
			if !matching {
				typ = protocol.EntityTypeMinecart
			}
			stack := servertest.ItemStack(t, "minecraft:oak_boat", 1)
			stack.Raw = []protocol.RawComponent{entityDataComponent(t, typ, nbt.Compound{
				"Invulnerable": nbt.Byte(1),
				"Rotation":     nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{nbt.Float(90), nbt.Float(0)}},
			})}
			srv.GiveSlot("Sailor", player.HotbarSlot, stack)
			aim := boatAim(0.5, 0.5, 64+rules.PlayerEyeHeight(protocol.PoseStanding), 2.9, 63.0+8.0/9, 1)
			c.UseBoatItem(aim.yaw, aim.pitch)
			spawn := servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat)
			_, _, _, yaw, _, _ := srv.BoatState(spawn.EntityID)
			if yaw != float64(aim.yaw) {
				t.Errorf("boat yaw = %v, want placer yaw %v", yaw, aim.yaw)
			}
			want := "0b"
			if matching {
				want = "1b"
			}
			if got := awaitEntityDataCompound(t, srv, c, "@e[type=oak_boat,limit=1]", "Invulnerable"); got != want {
				t.Errorf("boat Invulnerable = %s, want %s", got, want)
			}
		})
	}
}

// vanilla: BoatItem.use, TypedEntityData.loadInto
func TestAHandPlacedBoatChecksCollisionAtItsEntityDataPosition(t *testing.T) {
	t.Parallel()
	for _, obstacle := range []string{"final_block", "final_boat", "original_block", "original_boat"} {
		t.Run(obstacle, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.GameMode = "survival"
			srv, c := servertest.Join(t, cfg, "Sailor")
			servertest.HoldTicks(t, srv)
			c.MoveToGrounded(0.5, 64, 0.5, 0, 0, true)
			for x := int32(2); x <= 3; x++ {
				for z := int32(0); z <= 1; z++ {
					srv.SetBlock(x, 63, z, registry.BlockWater.DefaultState())
				}
			}
			wantBoats := 0
			rejected := strings.HasPrefix(obstacle, "final_")
			switch obstacle {
			case "final_block":
				srv.SetBlock(6, 64, 0, registry.BlockStone.DefaultState())
			case "final_boat":
				srv.SpawnBoat(6.5, 64, 0.5, "minecraft:oak_boat")
				wantBoats++
			case "original_block":
				srv.SetBlock(3, 63, 1, registry.BlockStone.DefaultState())
			case "original_boat":
				srv.SpawnBoat(2.9, 63+8.0/9, 1, "minecraft:oak_boat")
				wantBoats++
			}
			stack := servertest.ItemStack(t, "minecraft:oak_boat", 1)
			if !stack.SetEntityData(protocol.EntityTypeOakBoat, nbt.Compound{
				"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(6.5), nbt.Double(64), nbt.Double(0.5)}},
			}) {
				t.Fatal("could not encode entity data")
			}
			srv.GiveSlot("Sailor", player.HotbarSlot, stack)
			c.Settle()
			aim := boatAim(0.5, 0.5, 64+rules.PlayerEyeHeight(protocol.PoseStanding), 2.9, 63+8.0/9, 1)
			c.UseBoatItem(aim.yaw, aim.pitch)
			c.Settle()
			wantCount := int32(1)
			if !rejected {
				wantBoats++
				wantCount = 0
			}
			if got := srv.BoatCount(); got != wantBoats {
				t.Errorf("boats = %d, want %d", got, wantBoats)
			}
			if got := srv.InventorySlot("Sailor", player.HotbarSlot); got.Count != wantCount {
				t.Errorf("held count = %d, want %d", got.Count, wantCount)
			}
			if !rejected {
				found := false
				for _, id := range srv.BoatIDs() {
					x, y, z, _, _, _ := srv.BoatState(id)
					found = found || x == 6.5 && y == 64 && z == 0.5
				}
				if !found {
					t.Error("no boat at the entity data position (6.5, 64, 0.5)")
				}
			}
		})
	}
}

// vanilla: EntityType.updateCustomEntityTag, EntityTypes.OP_ONLY_CUSTOM_DATA
func TestHandPlacedRestrictedCartsOnlyLoadEntityDataForOperators(t *testing.T) {
	t.Parallel()
	const kind = "minecraft:command_block_minecart"
	for _, name := range []string{"Operator", "Layer"} {
		t.Run(kind+"/"+name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.GameMode = "survival"
			srv, c := servertest.Join(t, cfg, name, servertest.OpsListing(t, "Operator"))
			servertest.HoldTicks(t, srv)
			servertest.CartRails(t, srv, 3, 6)
			c.MoveToGrounded(4.5, 64, 2.9, 180, 60, true)
			c.Settle()
			stack := servertest.ItemStack(t, kind, 1)
			stack.CustomName = &protocol.Text{Text: "Item"}
			typ, _ := registry.EntityTypeID(kind)
			if !stack.SetEntityData(typ, nbt.Compound{"Invulnerable": nbt.Byte(1)}) {
				t.Fatal("could not encode entity data")
			}
			srv.GiveSlot(name, player.HotbarSlot, stack)
			c.PlaceBlock(4, 64, 1, protocol.FaceTop)
			spawn := servertest.AwaitBoatSpawn(c, typ)
			tag, _ := srv.MinecartSaveTag(spawn.EntityID)
			if got := nbt.BoolField(tag, "Invulnerable"); got != (name == "Operator") {
				t.Errorf("Invulnerable = %v for %s", got, name)
			}
			if tag["CustomName"] != nbt.String("Item") {
				t.Errorf("CustomName = %v, want Item regardless of operator status", tag["CustomName"])
			}
		})
	}
}
