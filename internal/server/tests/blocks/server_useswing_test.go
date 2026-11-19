package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestPlacingABlockSwingsTheArmForOthers(t *testing.T) {
	t.Parallel()
	for _, door := range []bool{false, true} {
		name := "placement"
		if door {
			name = "door"
		}
		t.Run(name, func(t *testing.T) {
			srv, watcher := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
			c := servertest.DialServer(t, srv)
			id := c.JoinAs("Placer")
			watcher.ExpectPlayerAdded("Placer")
			c.Settle()
			servertest.HoldTicks(t, srv)
			if door {
				srv.SetBlock(3, 64, 0, registry.BlockOakDoor.DefaultState())
			} else {
				srv.GiveSlot("Placer", player.HotbarSlot, protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 2})
			}
			servertest.RunTicks(t, srv, 30)
			if got := srv.AttackTicks("Placer"); got <= 20 {
				t.Fatalf("attack ticks before use = %d", got)
			}
			if door {
				c.UseBlock(3, 64, 0)
			} else {
				c.PlaceBlock(3, 63, 0, protocol.FaceTop)
			}
			c.SettleKeeping()
			if got := srv.AttackTicks("Placer"); got != 0 {
				t.Errorf("attack ticks after use = %d, want 0", got)
			}
			r := protocol.NewReader(watcher.ExpectWithinTicks(srv, 20, protocol.IDPlaySwingAnimation))
			if got := r.ReadVarInt(); got != id {
				t.Errorf("swing entity = %d, want %d", got, id)
			}
			if got := r.ReadVarInt(); got != protocol.HandMain {
				t.Errorf("swing hand = %d", got)
			}
			c.ExpectNoneWithinTicks(srv, 5, servertest.PacketIs(protocol.IDPlaySwingAnimation))
		})
	}
}

// vanilla: Block.useWithoutItem, Item.useOn
func TestBlockUseSwingOutcomes(t *testing.T) {
	t.Parallel()
	srv, watcher := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	c := servertest.DialServer(t, srv)
	id := c.JoinAs("User")
	watcher.ExpectPlayerAdded("User")
	c.Settle()
	servertest.HoldTicks(t, srv)
	for _, tc := range []struct {
		name        string
		block       registry.Block
		item        registry.Item
		prop        registry.Prop
		value       string
		swing, self bool
	}{
		{name: "lever", block: registry.BlockLever, swing: true},
		{name: "axe", block: registry.BlockOakLog, item: registry.ItemIronAxe, swing: true},
		{name: "hoe", block: registry.BlockDirt, item: registry.ItemIronHoe, swing: true},
		{name: "steel", block: registry.BlockStone, item: registry.ItemFlintAndSteel, swing: true},
		{name: "incomplete bed", block: registry.BlockRedBed},
		{name: "brush", block: registry.BlockStone, item: registry.ItemBrush},
		{name: "unplaceable crystal", block: registry.BlockStone, item: registry.ItemEndCrystal},
		{name: "unplaceable stand", block: registry.BlockStone, item: registry.ItemArmorStand},
		{name: "empty bookshelf", block: registry.BlockChiseledBookshelf, prop: registry.PropFacing, value: "west"},
		{name: "pressed button", block: registry.BlockStoneButton, prop: registry.PropPowered, value: "true"},
		{name: "campfire", block: registry.BlockCampfire, item: registry.ItemBeef, swing: true, self: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := tc.block.DefaultState()
			if tc.value != "" {
				var ok bool
				state, ok = registry.WithProp(state, tc.prop, tc.value)
				if !ok {
					t.Fatal("invalid state")
				}
			}
			srv.SetBlock(3, 64, 0, state)
			srv.SetBlock(3, 65, 0, registry.BlockAir.DefaultState())
			face := int32(protocol.FaceTop)
			if tc.name == "unplaceable stand" {
				face = protocol.FaceBottom
			}
			if tc.name == "empty bookshelf" {
				face = protocol.FaceWest
			}
			stack := protocol.ItemStack{}
			if tc.item != registry.ItemAir {
				stack = protocol.ItemStack{ItemID: int32(tc.item), Count: 1}
			}
			srv.GiveSlot("User", player.HotbarSlot, stack)
			servertest.RunTicks(t, srv, 30)
			before := srv.AttackTicks("User")
			c.PlaceBlock(3, 64, 0, face)
			c.SettleKeeping()
			want := before
			if tc.swing {
				want = 0
			}
			if got := srv.AttackTicks("User"); got != want {
				t.Errorf("attack ticks = %d, want %d", got, want)
			}
			for _, viewer := range []struct {
				c     *servertest.Client
				swing bool
			}{{watcher, tc.swing}, {c, tc.self}} {
				if !viewer.swing {
					viewer.c.ExpectNoneWithinTicks(srv, 5, servertest.PacketIs(protocol.IDPlaySwingAnimation))
					continue
				}
				r := protocol.NewReader(viewer.c.ExpectWithinTicks(srv, 20, protocol.IDPlaySwingAnimation))
				if got := r.ReadVarInt(); got != id {
					t.Errorf("swing entity=%d, want %d", got, id)
				}
				if got := r.ReadVarInt(); got != protocol.HandMain {
					t.Errorf("swing hand=%d", got)
				}
			}
		})
	}
}
