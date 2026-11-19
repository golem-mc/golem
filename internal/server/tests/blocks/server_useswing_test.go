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

// A use inside the first half of a swing already in progress neither resets
// the attack cooldown nor swings again.
// vanilla: LivingEntity.SwingState.startIfAble, ServerPlayer.swingAndResetAttackStrength
func TestUseDuringASwingDoesNotSwingAgain(t *testing.T) {
	t.Parallel()
	srv, watcher := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	c := servertest.DialServer(t, srv)
	c.JoinAs("User")
	watcher.ExpectPlayerAdded("User")
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetBlock(3, 64, 0, registry.BlockOakDoor.DefaultState())
	servertest.RunTicks(t, srv, 10)

	c.UseBlock(3, 64, 0)
	c.SettleKeeping()
	watcher.ExpectWithinTicks(srv, 20, protocol.IDPlaySwingAnimation)
	servertest.RunTicks(t, srv, 1)
	c.UseBlock(3, 64, 0)
	c.SettleKeeping()
	if got := srv.AttackTicks("User"); got != 1 {
		t.Errorf("attack ticks after a use mid-swing = %d, want 1", got)
	}
	watcher.ExpectNoneWithinTicks(srv, 1, servertest.PacketIs(protocol.IDPlaySwingAnimation))

	servertest.RunTicks(t, srv, 5)
	c.UseBlock(3, 64, 0)
	c.SettleKeeping()
	if got := srv.AttackTicks("User"); got != 0 {
		t.Errorf("attack ticks after a use past half the swing = %d, want 0", got)
	}
	watcher.ExpectWithinTicks(srv, 20, protocol.IDPlaySwingAnimation)
}

// A punch inside the first half of a swing in progress resets the attack
// cooldown but relays no second swing.
// vanilla: ServerGamePacketListenerImpl.handlePunch, LivingEntity.swing
func TestPunchDuringASwingDoesNotBroadcastAgain(t *testing.T) {
	t.Parallel()
	srv, watcher := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	c := servertest.DialServer(t, srv)
	c.JoinAs("User")
	watcher.ExpectPlayerAdded("User")
	c.Settle()
	servertest.HoldTicks(t, srv)

	punch := func() {
		c.Send(protocol.IDPlayPunch, func(b *protocol.Buffer) {})
		c.SettleKeeping()
	}
	punch()
	watcher.ExpectWithinTicks(srv, 20, protocol.IDPlaySwingAnimation)
	servertest.RunTicks(t, srv, 1)
	punch()
	if got := srv.AttackTicks("User"); got != 0 {
		t.Errorf("attack ticks after a punch mid-swing = %d, want 0", got)
	}
	watcher.ExpectNoneWithinTicks(srv, 1, servertest.PacketIs(protocol.IDPlaySwingAnimation))
}

// vanilla: LivingEntity.SwingState.startIfAble, PiercingWeapon.attack
func TestUseRespectsThePrecedingSwingDuration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, start             string
		item                    registry.Item
		haste, conduit, fatigue int32 // effect levels, zero means absent
		duration                int
		setupTicks              int
	}{
		{name: "wooden spear punch", start: "punch", item: registry.ItemWoodenSpear, duration: 13, setupTicks: 1},
		{name: "netherite spear punch", start: "punch", item: registry.ItemNetheriteSpear, duration: 23, setupTicks: 1},
		{name: "wooden spear jab", start: "jab", item: registry.ItemWoodenSpear, duration: 13, setupTicks: 9},
		{name: "hasty spear jab", start: "jab", item: registry.ItemWoodenSpear, haste: 3, duration: 10, setupTicks: 9},
		{name: "hasty use", start: "use", haste: 2, duration: 4, setupTicks: 1},
		{name: "fatigued use", start: "use", fatigue: 3, duration: 12, setupTicks: 1},
		{name: "conduit over haste and fatigue", start: "use", haste: 1, conduit: 2, fatigue: 3, duration: 4, setupTicks: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, watcher := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
			c := servertest.DialServer(t, srv)
			id := c.JoinAs("User")
			watcher.ExpectPlayerAdded("User")
			c.Settle()
			servertest.HoldTicks(t, srv)
			srv.SetBlock(3, 64, 0, registry.BlockOakDoor.DefaultState())
			if tc.item != registry.ItemAir {
				srv.GiveSlot("User", player.HotbarSlot, protocol.ItemStack{ItemID: int32(tc.item), Count: 1})
			}
			for effect, level := range map[string]int32{"minecraft:haste": tc.haste, "minecraft:conduit_power": tc.conduit, "minecraft:mining_fatigue": tc.fatigue} {
				if level > 0 {
					srv.GiveEffect("User", effect, 600, level-1, false)
				}
			}
			// The first tick settles the held item; only jabs need attack charge.
			servertest.RunTicks(t, srv, tc.setupTicks)
			switch tc.start {
			case "punch":
				c.Send(protocol.IDPlayPunch, nil)
			case "jab":
				c.Stab()
			case "use":
				c.UseBlock(3, 64, 0)
			}
			c.SettleKeeping()
			r := protocol.NewReader(watcher.ExpectWithinTicks(srv, 20, protocol.IDPlaySwingAnimation))
			if got := r.ReadVarInt(); got != id {
				t.Fatalf("swing entity = %d, want %d", got, id)
			}
			r.ReadVarInt()
			kind, duration := r.ReadVarInt(), r.ReadVarInt()
			want := registry.DefaultSwingAnimation
			if tc.start != "use" {
				want = registry.AttackAnimationOf(int32(tc.item))
			}
			if kind != want.Type || duration != want.Duration {
				t.Fatalf("swing = (%d, %d), want %+v (unmodified on the wire)", kind, duration, want)
			}
			// The inclusive halfway boundary belongs to the preceding animation,
			// even though the next use would start a default six-tick swing.
			half := tc.duration / 2
			servertest.RunTicks(t, srv, half)
			c.UseBlock(3, 64, 0)
			c.SettleKeeping()
			if got := srv.AttackTicks("User"); got != int32(half) {
				t.Errorf("attack ticks at halfway = %d, want %d", got, half)
			}
			// Zero-tick barriers preserve the boundary for the following attempt.
			watcher.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlaySwingAnimation))
			c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlaySwingAnimation))
			servertest.RunTicks(t, srv, 1)
			c.UseBlock(3, 64, 0)
			c.SettleKeeping()
			if got := srv.AttackTicks("User"); got != 0 {
				t.Errorf("attack ticks after halfway = %d, want 0", got)
			}
			watcher.ExpectWithinTicks(srv, 20, protocol.IDPlaySwingAnimation)
		})
	}
}
