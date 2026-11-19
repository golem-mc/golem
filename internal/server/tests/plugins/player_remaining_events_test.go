package plugins_test

import (
	"slices"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

func joinPlugin(t *testing.T, cfg *config.Config, listen func(plugin.Server)) (*server.Server, *servertest.Client) {
	t.Helper()
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "remaining", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		listen(s)
		return nil
	}}}})
	c := servertest.DialServer(t, srv)
	c.Join("Tester")
	c.Settle()
	servertest.HoldTicks(t, srv)
	return srv, c
}

// vanilla: ServerGamePacketListenerImpl.handlePlayerAbilities
func TestPluginToggleFlightCancelResendsAbilities(t *testing.T) {
	var flights int
	var on, cancel bool
	srv, c := joinPlugin(t, servertest.BlockTestConfig(), func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerToggleFlightEvent) {
			flights++
			on = e.On
			e.SetCancelled(cancel)
		})
	})
	cancel = true
	c.Send(protocol.IDPlayPlayerAbilitiesIn, func(b *protocol.Buffer) { b.PutI8(protocol.AbilityFlying) })
	ids := fired(t, srv, c, "flight event", &flights, 1)
	if !on {
		t.Fatal("On = false, want true")
	}
	if !slices.Contains(ids, protocol.IDPlayAbilities) {
		t.Error("the cancelled flight was not answered with the abilities")
	}
	cancel = false
	c.Send(protocol.IDPlayPlayerAbilitiesIn, func(b *protocol.Buffer) { b.PutI8(protocol.AbilityFlying) })
	fired(t, srv, c, "allowed flight event", &flights, 2)
}

// vanilla: ItemStack.hurtAndBreak
func TestPluginItemDamageCancelAndChange(t *testing.T) {
	var events, change int
	var cancel bool
	srv, c := joinPlugin(t, servertest.SurvivalTestConfig(), func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerItemDamageEvent) {
			events++
			if e.Damage != 1 {
				t.Errorf("Damage = %d, want 1", e.Damage)
			}
			if change > 0 {
				e.Damage = change
			}
			e.SetCancelled(cancel)
		})
	})
	srv.GiveSlot("Tester", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:diamond_pickaxe"), Count: 1})
	c.MoveToGround(0.5, 64, 0.5, true)
	d := servertest.DigTarget
	stone := servertest.BlockState(t, "minecraft:stone")
	srv.SetBlock(d[0], d[1]-1, d[2], stone)
	srv.SetBlock(d[0], d[1], d[2], stone)
	c.Settle()

	dig := func() {
		c.StartDigging(d[0], d[1], d[2])
		c.Settle()
		servertest.RunTicks(t, srv, 1)
		c.StopDigging(d[0], d[1], d[2])
		c.Settle()
		servertest.RunTicks(t, srv, 12)
	}
	cancel = true
	dig()
	if events != 1 {
		t.Fatalf("damage events = %d, want 1", events)
	}
	if got := srv.InventorySlot("Tester", 36); got.Damage != 0 {
		t.Fatalf("cancelled wear left damage %d", got.Damage)
	}
	cancel, change = false, 4
	srv.SetBlock(d[0], d[1], d[2], stone)
	dig()
	if events != 2 {
		t.Fatalf("damage events = %d, want 2", events)
	}
	if got := srv.InventorySlot("Tester", 36); got.Damage != 4 {
		t.Fatalf("damage = %d, want the listener's 4 (unbreaking aside)", got.Damage)
	}
}

// vanilla: ArmorStand.interact
func TestPluginArmorStandManipulateCancel(t *testing.T) {
	var events int
	var cancel bool
	var slot plugin.EquipmentSlot
	srv, c := joinPlugin(t, servertest.SurvivalTestConfig(), func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerArmorStandManipulateEvent) {
			events++
			slot = e.Slot
			if e.Stand == nil || e.PlayerItem.Count != 1 {
				t.Errorf("event = %+v", e)
			}
			e.SetCancelled(cancel)
		})
	})
	id := srv.SpawnArmorStand(2.5, 64, 0.5)
	srv.GiveSlot("Tester", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:iron_helmet"), Count: 1})
	c.Settle()
	cancel = true
	c.InteractAt(id, protocol.HandMain, 0, 1.9, 0)
	fired(t, srv, c, "cancelled manipulate", &events, 1)
	if slot != plugin.SlotHead {
		t.Errorf("Slot = %v, want head", slot)
	}
	if got := srv.ArmorStandStack(id, 5); !got.Empty() {
		t.Fatal("cancelled swap put the helmet on the stand")
	}
	if got := srv.InventorySlot("Tester", 36); got.Empty() {
		t.Fatal("cancelled swap took the helmet from the hand")
	}
	cancel = false
	c.InteractAt(id, protocol.HandMain, 0, 1.9, 0)
	fired(t, srv, c, "allowed manipulate", &events, 2)
	servertest.RunTicks(t, srv, 1)
	if got := srv.ArmorStandStack(id, 5); got.Empty() {
		t.Fatal("allowed swap did not dress the stand")
	}
}

// vanilla: FishingRodItem.use
func TestPluginFishCastCancel(t *testing.T) {
	var casts int
	var cancel bool
	srv, c := joinPlugin(t, servertest.SurvivalTestConfig(), func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerFishEvent) {
			if e.State == plugin.FishCast {
				casts++
				e.SetCancelled(cancel)
			}
		})
	})
	srv.GiveSlot("Tester", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:fishing_rod"), Count: 1})
	c.Settle()
	cancel = true
	servertest.UseHeld(c, 0, 30)
	fired(t, srv, c, "cancelled cast", &casts, 1)
	servertest.RunTicks(t, srv, 1)
	if _, ok := srv.BobberOnGround("Tester"); ok {
		t.Fatal("a cancelled cast left a hook")
	}
	cancel = false
	servertest.UseHeld(c, 0, 30)
	fired(t, srv, c, "allowed cast", &casts, 2)
	servertest.RunTicks(t, srv, 1)
	if _, ok := srv.BobberOnGround("Tester"); !ok {
		t.Fatal("an allowed cast left no hook")
	}
}

// pump runs ticks paced in real time, so a packet a listener blocks on is
// read and finished.
func pump(t *testing.T, srv *server.Server, n int) {
	t.Helper()
	for range n {
		servertest.RunTicks(t, srv, 1)
		time.Sleep(3 * time.Millisecond)
	}
}

// hurt lands a blow from off the tick goroutine, which a listener waits on.
func hurt(t *testing.T, srv *server.Server) {
	t.Helper()
	done := make(chan struct{})
	go func() { srv.Hurt("Tester", 4); close(done) }()
	for {
		select {
		case <-done:
			return
		default:
			servertest.RunTicks(t, srv, 1)
			time.Sleep(3 * time.Millisecond)
		}
	}
}

// fishPool is a pool at the player's feet and the hook cast into it.
func fishPool(t *testing.T, srv *server.Server, c *servertest.Client) {
	t.Helper()
	for x := int32(1); x <= 3; x++ {
		for z := int32(2); z <= 4; z++ {
			for y := int32(61); y <= 63; y++ {
				srv.SetBlock(x, y, z, servertest.MustState(t, "minecraft:water", nil))
			}
		}
	}
	c.MoveTo(2.5, 64, 1.5, 0, 40)
	c.Settle()
}

// vanilla: FishingHook.catchingFish, FishingHook.retrieve
func TestPluginFishBiteCaughtAndFailed(t *testing.T) {
	counts := map[plugin.FishState]int{}
	var cancelBite, cancelCaught bool
	var xp = -1
	var sawCaught bool
	srv, c := joinPlugin(t, servertest.SurvivalTestConfig(), func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerFishEvent) {
			counts[e.State]++
			switch e.State {
			case plugin.FishBite:
				e.SetCancelled(cancelBite)
			case plugin.FishCaught:
				sawCaught = e.Caught != nil
				e.SetCancelled(cancelCaught)
				if xp >= 0 {
					e.Experience = xp
				}
			}
		})
	})
	srv.GiveSlot("Tester", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:fishing_rod"), Count: 1})
	fishPool(t, srv, c)
	servertest.UseHeld(c, 0, 40)
	pump(t, srv, 10)
	servertest.RunTicks(t, srv, 20)

	// A cancelled bite is drawn again the next tick.
	cancelBite = true
	srv.WindBobberBite("Tester")
	servertest.RunTicks(t, srv, 6)
	if counts[plugin.FishBite] < 2 {
		t.Fatalf("a cancelled bite fired %d times, want it retried", counts[plugin.FishBite])
	}
	cancelBite = false
	before := counts[plugin.FishBite]
	servertest.RunTicks(t, srv, 6)
	if counts[plugin.FishBite] > before+1 {
		t.Fatalf("bite events kept firing: %d -> %d", before, counts[plugin.FishBite])
	}

	// The nibble running out unanswered is a failed attempt.
	servertest.RunTicks(t, srv, 60)
	if counts[plugin.FishFailed] < 1 {
		t.Fatalf("no failed attempt after the nibble ran out: %v", counts)
	}

	loot := func() int {
		n := srv.ItemCount()
		for slot := range 46 {
			if got := srv.InventorySlot("Tester", slot); !got.Empty() && slot != 36 {
				n++
			}
		}
		return n
	}

	// A catch: cancelled it pays nothing, allowed it pays the listener's orb.
	servertest.UseHeld(c, 0, 40) // reel the spent hook
	pump(t, srv, 10)
	servertest.UseHeld(c, 0, 40)
	pump(t, srv, 10)
	srv.BiteBobber("Tester", 1)
	cancelCaught = true
	servertest.UseHeld(c, 0, 40)
	pump(t, srv, 10)
	servertest.RunTicks(t, srv, 2)
	if counts[plugin.FishCaught] != 1 || loot() != 0 {
		t.Fatalf("cancelled catch: %d events, %d items", counts[plugin.FishCaught], srv.ItemCount())
	}
	servertest.UseHeld(c, 0, 40)
	pump(t, srv, 10)
	cancelCaught, xp = false, 9
	srv.BiteBobber("Tester", 1)
	servertest.UseHeld(c, 0, 40)
	pump(t, srv, 10)
	servertest.RunTicks(t, srv, 2)
	if counts[plugin.FishCaught] != 2 || loot() == 0 || !sawCaught {
		t.Fatalf("allowed catch: %d events, %d items, caught set %v", counts[plugin.FishCaught], srv.ItemCount(), sawCaught)
	}
}
