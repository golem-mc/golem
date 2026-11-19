package combat

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
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

// TestAWindChargeLaunchForgivesTheFallBackToTheImpactHeight bursts a wind
// charge at the thrower's feet and reports the launch: only the part of the
// fall below the burst's height hurts, and without a burst the same fall
// takes its full damage.
// vanilla: ServerPlayer.onExplosionHit, LivingEntity.causeFallDamage
func TestAWindChargeLaunchForgivesTheFallBackToTheImpactHeight(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		burst  bool
		landY  float64
		health float32
	}{
		{"back to the impact height", true, 64, 20},
		{"four blocks below the impact height", true, 60, 19},
		{"no wind charge", false, 64, 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c, id := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")
			c.MoveTo(0.5, 64, 0.5, 0, 90)
			// A pit beside the burst for the landing below it.
			for y := int32(60); y < 64; y++ {
				if !srv.SetBlock(2, y, 0, 0) {
					t.Fatal("the pit was not dug")
				}
			}
			if tc.burst {
				srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:wind_charge"), Count: 1})
				servertest.HoldTicks(t, srv)
				servertest.UseHeld(c, 0, 90)
				if !c.ReadTicks(srv, 20, func(id int32, _ []byte) bool { return id == protocol.IDPlayExplosion }) {
					t.Fatal("the charge never burst")
				}
			}
			// The launch, well inside the burst's 40 ticks of grace.
			x := 0.5
			if tc.landY < 64 {
				x = 2.5
			}
			for _, y := range []float64{72, 70, 68, 66} {
				c.MoveToGround(x, y, 0.5, false)
			}
			if tc.landY < 64 {
				c.MoveToGround(x, 62, 0.5, false)
			}
			c.MoveToGround(x, tc.landY, 0.5, true)

			fall := registry.MustID("minecraft:damage_type", "minecraft:fall")
			health, falls := float32(20), 0
			c.SettleAndCollect(func(pid int32, data []byte) {
				switch pid {
				case protocol.IDPlaySetHealth:
					health = min(health, servertest.ExpectHealthReport(c, data).Health)
				case protocol.IDPlayDamageEvent:
					r := protocol.NewReader(data)
					if r.ReadVarInt() == id && r.ReadVarInt() == fall {
						falls++
					}
				}
			})
			if health != tc.health {
				t.Errorf("health fell to %v, want %v", health, tc.health)
			}
			if want := map[bool]int{true: 1, false: 0}[tc.health < 20]; falls != want {
				t.Errorf("%d fall damage events, want %d", falls, want)
			}
		})
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

// TestAWindChargeCarriesNoItemSlotAndBurstsAboveTheWorld checks the
// metadata of a charge thrown straight up: no item slot, which its client
// has no index for, and a burst once it flies past the build
// limit without meeting anything.
// vanilla: AbstractHurtingProjectile.defineSynchedData, AbstractWindCharge.tick
func TestAWindChargeCarriesNoItemSlotAndBurstsAboveTheWorld(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:wind_charge"), Count: 4})
	c.MoveTo(0.5, 64, 0.5, 0, -90)

	servertest.HoldTicks(t, srv)
	servertest.UseHeld(c, 0, -90)

	var chargeID int32
	c.ReadTicks(srv, 280, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySpawnEntity:
			if sp := servertest.ReadThrownSpawn(c, data); sp.EntityType == protocol.EntityTypeWindCharge {
				chargeID = sp.EntityID
			}
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			if r.ReadVarInt() != chargeID {
				return false
			}
			if index := r.ReadU8(); index != 0xff {
				t.Fatalf("wind charge metadata starts with index %d, want the terminator: no item slot", index)
			}
		}
		return false
	})
	if chargeID == 0 {
		t.Fatal("no wind charge spawned")
	}
	if left := srv.ThrownInDimension("minecraft:overworld"); len(left) != 0 {
		t.Errorf("%d charges still fly after 14 s straight up, want them burst past the build limit", len(left))
	}
}

// TestAWindChargeBurstLeavesExperienceOrbsUnhurt checks that the harmless
// blast pushes an orb but does not destroy it.
// vanilla: SimpleExplosionDamageCalculator
func TestAWindChargeBurstLeavesExperienceOrbsUnhurt(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:wind_charge"), Count: 4})
	// The wall and the orb sit past the 8 blocks an orb is drawn to a player
	// from.
	stone := registry.BlockStates["minecraft:stone"]
	for y := int32(64); y <= 66; y++ {
		srv.SetBlock(14, y, 0, stone)
	}
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	srv.SpawnOrb(13.0, 65.5, 0.5, 1)

	servertest.HoldTicks(t, srv)
	servertest.UseHeld(c, -90, 0)

	c.ReadTicks(srv, 60, func(id int32, _ []byte) bool {
		return id == protocol.IDPlayExplosion
	})
	c.SettleAndCollect(func(int32, []byte) {})
	if n := srv.OrbCount(); n != 1 {
		t.Errorf("%d orbs left after the burst, want the one it only pushed", n)
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

// TestAWindChargeBurstPutsOutCandlesAndRingsABell checks that the burst
// unlights candles and a candle cake with the extinguish sound and a
// block_change game event, pulses an observer watching a candle, and rings a
// bell beside it.
// vanilla: AbstractCandleBlock.onExplosionHit, BellBlock.onExplosionHit
func TestAWindChargeBurstPutsOutCandlesAndRingsABell(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:wind_charge"), Count: 4})
	lit := servertest.BlockStateWith(t, "minecraft:candle", "candles=1,lit=true,waterlogged=false")
	out := servertest.BlockStateWith(t, "minecraft:candle", "candles=1,lit=false,waterlogged=false")
	litCake := servertest.BlockStateWith(t, "minecraft:candle_cake", "lit=true")
	outCake := servertest.BlockStateWith(t, "minecraft:candle_cake", "lit=false")
	bell := servertest.BlockStateWith(t, "minecraft:bell", "attachment=floor,facing=north,powered=false")
	srv.SetBlock(1, 64, 0, lit)
	srv.SetBlock(1, 64, 1, lit)
	srv.SetBlock(1, 64, -1, lit)
	srv.SetBlock(1, 63, 2, servertest.BlockState(t, "minecraft:stone"))
	srv.SetBlock(1, 64, 2, litCake)
	srv.SetBlock(1, 63, 0, servertest.BlockStateWith(t, "minecraft:observer", "facing=up,powered=false"))
	srv.SetBlock(2, 64, 0, bell)
	c.MoveTo(0.5, 64, 0.5, 0, 90)

	var changes atomic.Int32
	srv.ObserveGameEvents(func(event string, x, y, z float64) {
		if event == "minecraft:block_change" && int32(x) == 1 && (int32(z) == 0 || int32(z) == 2) && int32(y) == 64 {
			changes.Add(1)
		}
	})
	servertest.HoldTicks(t, srv)
	servertest.UseHeld(c, 0, 90)

	fizz, _ := registry.SoundEventID("minecraft:block.candle.extinguish")
	sounds, rings, pulsed := 0, 0, false
	c.ReadTicks(srv, 60, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySoundEffect:
			if s := servertest.DecodePlaySoundEffect(t, data); s.RegistryID == fizz {
				sounds++
			}
		case protocol.IDPlayBlockAction:
			r := protocol.NewReader(data)
			if x, y, z := r.ReadPosition(); x == 2 && y == 64 && z == 0 {
				rings++
			}
		}
		if _, powered := servertest.ObserverPowerAt(srv, 1, 63, 0); powered {
			pulsed = true
		}
		return sounds > 0 && rings > 0 && pulsed && srv.BlockAt(1, 64, 2) == outCake
	})
	for _, z := range []int32{-1, 0, 1} {
		if got := srv.BlockAt(1, 64, z); got != out {
			t.Errorf("candle at z=%d is state %d, want it out (%d)", z, got, out)
		}
	}
	if got := srv.BlockAt(1, 64, 2); got != outCake {
		t.Errorf("candle cake is state %d, want it out (%d)", got, outCake)
	}
	if sounds == 0 {
		t.Error("no candle extinguish sound was sent")
	}
	if rings == 0 {
		t.Error("the bell's block event was not sent")
	}
	if !pulsed {
		t.Error("the observer watching the extinguished candle never pulsed")
	}
	if changes.Load() == 0 {
		t.Error("no block_change game event was posted for the extinguished candles")
	}
}

// TestABreezeWindBurstPullsALeverOnlyWithMobGriefing checks a breeze's burst
// leaves a lever alone with mob_griefing off.
// vanilla: ServerExplosion.canTriggerBlocks
func TestABreezeWindBurstPullsALeverOnlyWithMobGriefing(t *testing.T) {
	t.Parallel()
	srv, _, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Watcher")
	off := servertest.BlockStateWith(t, "minecraft:lever", "face=floor,facing=north,powered=false")
	on := servertest.BlockStateWith(t, "minecraft:lever", "face=floor,facing=north,powered=true")
	srv.SetBlock(1, 64, 0, off)
	srv.SetGameRule("mob_griefing", false)
	servertest.HoldTicks(t, srv)
	srv.RunBreezeWindBurst(1.5, 64.5, 0.5)
	if got := srv.BlockAt(1, 64, 0); got != off {
		t.Errorf("mob_griefing=false: lever is state %d, want %d", got, off)
	}
	srv.SetGameRule("mob_griefing", true)
	srv.RunBreezeWindBurst(1.5, 64.5, 0.5)
	if got := srv.BlockAt(1, 64, 0); got != on {
		t.Errorf("mob_griefing=true: lever is state %d, want %d", got, on)
	}
}
