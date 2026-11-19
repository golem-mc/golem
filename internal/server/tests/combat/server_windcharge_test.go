package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestAWindChargeBurstsAtTheFeetPushingTheThrowerUnhurt checks the throw
// into the ground under the thrower: the charge flies as a wind_charge
// body, bursts without breaking the ground, hurts nobody and pushes the
// thrower upward through the Explosion packet, whose particle and sound are
// the burst's own.
// vanilla: WindChargeItem.use, WindCharge.explode, SimpleExplosionDamageCalculator
func TestAWindChargeBurstsAtTheFeetPushingTheThrowerUnhurt(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:wind_charge"), Count: 4})
	c.MoveTo(0.5, 64, 0.5, 0, 90)
	ground := srv.BlockAt(0, 63, 0)

	servertest.HoldTicks(t, srv)
	servertest.UseHeld(c, 0, 90)

	var spawned, sounded bool
	var burst *servertest.WindBurst
	hurt, updates := false, 0
	c.ReadTicks(srv, 60, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySpawnEntity:
			if servertest.ReadThrownSpawn(c, data).EntityType == protocol.EntityTypeWindCharge {
				spawned = true
			}
		case protocol.IDPlaySoundEffect:
			sounded = true
		case protocol.IDPlayExplosion:
			b := servertest.ReadWindBurst(c, data)
			burst = &b
		case protocol.IDPlaySetHealth:
			if servertest.ExpectHealthReport(c, data).Health < 20 {
				hurt = true
			}
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks, protocol.IDPlayChunkData:
			updates++
		}
		return burst != nil
	})
	if !spawned || !sounded {
		t.Fatalf("wind charge spawned = %v, throw sound = %v, want both", spawned, sounded)
	}
	if burst == nil {
		t.Fatal("the charge never burst")
	}
	if burst.Particle != protocol.ParticleGustEmitterSmall || burst.Sound != protocol.SoundEventWindChargeBurst || burst.Alternatives != 0 {
		t.Errorf("burst particle %d, sound %d, block particles %d; want the gust, the burst sound and none",
			burst.Particle, burst.Sound, burst.Alternatives)
	}
	if !burst.Pushed || burst.KY < 0.3 {
		t.Errorf("thrower pushed = %v with ky %.3f, want a real upward push", burst.Pushed, burst.KY)
	}
	// The unscaled push at this range tops out well under the 1.22 times it.
	if burst.KY > 1.22 {
		t.Errorf("ky %.3f is past the 1.22 multiplier's ceiling", burst.KY)
	}
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySetHealth && servertest.ExpectHealthReport(c, data).Health < 20 {
			hurt = true
		}
	})
	if hurt {
		t.Error("the thrower was hurt by their own wind charge")
	}
	if got := srv.BlockAt(0, 63, 0); got != ground {
		t.Errorf("the ground under the burst became %d, want it unbroken (%d)", got, ground)
	}
	if left := srv.InventorySlot("Thrower", 36); left.Count != 3 {
		t.Errorf("the hotbar holds %d wind charges, want the three one throw left", left.Count)
	}
}

// TestAWindChargeBurstPullsALeverAndRespectsTheCooldown checks that the
// burst toggles a lever beside it, and that a second use inside the ten
// ticks of the item's cooldown throws nothing.
// vanilla: LeverBlock.onExplosionHit, UseCooldown
func TestAWindChargeBurstPullsALeverAndRespectsTheCooldown(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:wind_charge"), Count: 4})
	off := servertest.BlockStateWith(t, "minecraft:lever", "face=floor,facing=north,powered=false")
	on := servertest.BlockStateWith(t, "minecraft:lever", "face=floor,facing=north,powered=true")
	srv.SetBlock(1, 64, 0, off)
	c.MoveTo(0.5, 64, 0.5, 0, 90)

	servertest.HoldTicks(t, srv)
	servertest.UseHeld(c, 0, 90)
	servertest.UseHeld(c, 0, 90)

	spawns := 0
	c.ReadTicks(srv, 60, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySpawnEntity && servertest.ReadThrownSpawn(c, data).EntityType == protocol.EntityTypeWindCharge {
			spawns++
		}
		return false
	})
	if spawns != 1 {
		t.Errorf("%d wind charges flew for two uses inside the cooldown, want one", spawns)
	}
	if got := srv.BlockAt(1, 64, 0); got != on {
		t.Errorf("the lever beside the burst is state %d, want it pulled (%d)", got, on)
	}
	if left := srv.InventorySlot("Thrower", 36); left.Count != 3 {
		t.Errorf("the hotbar holds %d wind charges, want three", left.Count)
	}
}

// TestAWindChargeFliesOnAfterItsThrowerLogsOut checks that a charge in
// flight survives its thrower leaving.
// vanilla: EntityReference.getEntity, AbstractHurtingProjectile.tick
func TestAWindChargeFliesOnAfterItsThrowerLogsOut(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BowTestConfig())
	c := connect()
	c.JoinAs("Thrower")
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:wind_charge"), Count: 4})
	c.MoveTo(0.5, 64, 0.5, 0, -90)
	// A second player keeps the chunks loaded once the thrower is gone.
	w := connect()
	w.JoinAs("Watcher")
	w.MoveTo(40.5, 64, 0.5, 0, 0)

	servertest.HoldTicks(t, srv)
	servertest.UseHeld(c, 0, -90)
	// Held ticks run back to back, so the throw has to be handled before they
	// are counted against its budget.
	c.Settle()
	servertest.WaitFor(t, srv, 100, func() bool { return len(srv.ThrownInDimension("minecraft:overworld")) > 0 })
	c.Disconnect()
	servertest.RunTicks(t, srv, 10)
	if left := srv.ThrownInDimension("minecraft:overworld"); len(left) != 1 {
		t.Errorf("%d charges fly after the thrower left, want 1", len(left))
	}
}
