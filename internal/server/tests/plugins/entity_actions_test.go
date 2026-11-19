package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// vanilla: Player.tryToStartFallFlying, stopFallFlying
func TestPluginEntityToggleGlideEvent(t *testing.T) {
	var seen []bool
	cancel := true
	srv, c := entityPlugin(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityToggleGlideEvent) {
			seen = append(seen, e.On)
			e.SetCancelled(cancel)
		})
	})
	srv.GiveSlot("Victim", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:elytra"), Count: 1})
	servertest.UseHeld(c, -90, 0)
	c.AwaitPlay(protocol.IDPlayContainerSlot)
	c.AwaitPlay(protocol.IDPlayContainerSlot)
	c.MoveToGrounded(0.5, 66, 0.5, -90, 10, false)
	servertest.StartFallFlying(c)
	c.Settle()
	if len(seen) == 0 || !seen[0] || srv.PlayerFallFlying("Victim") {
		t.Fatalf("cancelled start: events %v, gliding %v", seen, srv.PlayerFallFlying("Victim"))
	}
	cancel = false
	servertest.StartFallFlying(c)
	c.Settle()
	if !srv.PlayerFallFlying("Victim") {
		t.Error("an uncancelled start did not glide")
	}
}

// vanilla: DyeItem.interactLivingEntity
func TestPluginSheepDyeWoolEvent(t *testing.T) {
	var got *plugin.SheepDyeWoolEvent
	mode := "cancel"
	srv, c := entityPlugin(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.SheepDyeWoolEvent) {
			got = e
			switch mode {
			case "cancel":
				e.SetCancelled(true)
			case "recolour":
				e.Color = plugin.Red
			}
		})
	})
	pos := srv.PlayerPosition("Victim")
	sheep := srv.SpawnMob("minecraft:sheep", pos.X+1.5, pos.Y, pos.Z)
	srv.SetMobNoAI(sheep)
	srv.SetMobColour(sheep, "white")
	srv.GiveSlot("Victim", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:blue_dye"), Count: 2})
	servertest.HoldTicks(t, srv)
	c.UseOn(sheep)
	c.Settle()
	if got == nil || got.Color != plugin.Blue || got.Sheep == nil || got.Player.UUID() != protocol.OfflineUUID("Victim") {
		t.Fatalf("event = %+v", got)
	}
	if colour := srv.MobColour(sheep); colour != "white" {
		t.Errorf("cancelled dye left %q", colour)
	}
	mode = "recolour"
	c.UseOn(sheep)
	c.Settle()
	if colour := srv.MobColour(sheep); colour != "red" {
		t.Errorf("recoloured dye gave %q, want red", colour)
	}
}

// vanilla: ThrownExperienceBottle.onHit
func TestPluginExpBottleEvent(t *testing.T) {
	var got *plugin.ExpBottleEvent
	srv, c := entityPlugin(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.ExpBottleEvent) {
			got = e
			e.Experience = 0
		})
	})
	pos := srv.PlayerPosition("Victim")
	srv.ThrowThrown(entity.ThrownExperienceBottle, pos.X+3, pos.Y+0.5, pos.Z, 0, 90, 0, 1)
	c.ReadTicks(srv, 40, func(int32, []byte) bool { return got != nil })
	if got == nil || got.Bottle == nil {
		t.Fatalf("event = %+v", got)
	}
	if n := srv.OrbCount(); n != 0 {
		t.Errorf("orbs = %d after the listener zeroed the experience", n)
	}
}

// vanilla: Entity.startRiding, ServerGamePacketListenerImpl.handlePlayerInput
func TestPluginMountAndVehicleEvents(t *testing.T) {
	var mounts, dismounts int
	var vehicles []plugin.VehicleAction
	cancelEnter, cancelExit := true, true
	srv, c := entityPlugin(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityMountEvent) { mounts++ })
		plugin.Listen(s, func(e *plugin.EntityDismountEvent) { dismounts++ })
		plugin.Listen(s, func(e *plugin.VehicleEvent) {
			vehicles = append(vehicles, e.Action)
			if e.Vehicle == nil || e.Entity.UUID() != protocol.OfflineUUID("Victim") {
				t.Errorf("vehicle event = %+v", e)
			}
			e.SetCancelled(e.Action == plugin.VehicleEnter && cancelEnter || e.Action == plugin.VehicleExit && cancelExit)
		})
	})
	pos := srv.PlayerPosition("Victim")
	boat := srv.SpawnBoat(pos.X+1.5, pos.Y, pos.Z, "minecraft:oak_boat")
	servertest.HoldTicks(t, srv)
	c.UseOn(boat)
	c.Settle()
	if _, ok := srv.BoatRider(boat); ok || mounts != 1 {
		t.Fatalf("cancelled enter: mounted %v, mount events %d", ok, mounts)
	}
	cancelEnter = false
	c.UseOn(boat)
	c.Settle()
	if _, ok := srv.BoatRider(boat); !ok {
		t.Fatal("uncancelled enter did not board")
	}
	c.SendInput(protocol.InputSneak)
	c.Settle()
	if _, ok := srv.BoatRider(boat); !ok || dismounts != 1 {
		t.Fatalf("cancelled exit: seated %v, dismount events %d", ok, dismounts)
	}
	cancelExit = false
	c.SendInput(0)
	c.SendInput(protocol.InputSneak)
	c.Settle()
	if _, ok := srv.BoatRider(boat); ok {
		t.Error("uncancelled exit left the rider seated")
	}
	want := []plugin.VehicleAction{plugin.VehicleEnter, plugin.VehicleEnter, plugin.VehicleExit, plugin.VehicleExit}
	if len(vehicles) != len(want) {
		t.Fatalf("vehicle actions = %v, want %v", vehicles, want)
	}
}

// vanilla: Animal.mobInteract, Animal.setInLove
func TestPluginEntityEnterLoveModeEvent(t *testing.T) {
	var got *plugin.EntityEnterLoveModeEvent
	mode := "cancel"
	srv, c := entityPlugin(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityEnterLoveModeEvent) {
			got = e
			switch mode {
			case "cancel":
				e.SetCancelled(true)
			case "shorten":
				e.Ticks = 100
			}
		})
	})
	pos := srv.PlayerPosition("Victim")
	cow := srv.SpawnMob("minecraft:cow", pos.X+1.5, pos.Y, pos.Z)
	srv.SetMobNoAI(cow)
	srv.GiveSlot("Victim", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:wheat"), Count: 4})
	servertest.HoldTicks(t, srv)
	c.UseOn(cow)
	c.Settle()
	if got == nil || got.Ticks != 600 || got.Player.UUID() != protocol.OfflineUUID("Victim") {
		t.Fatalf("event = %+v", got)
	}
	if n := got.Entity.LoveTicks(); n != 0 {
		t.Errorf("cancelled love left %d ticks", n)
	}
	mode = "shorten"
	c.UseOn(cow)
	c.Settle()
	if n := got.Entity.LoveTicks(); n != 100 {
		t.Errorf("love ticks = %d, want the listener's 100", n)
	}
}

// vanilla: LivingEntity.baseTick, decreaseAirSupply, increaseAirSupply
func TestPluginEntityAirChangeEvent(t *testing.T) {
	var got *plugin.EntityAirChangeEvent
	srv, _ := entityPlugin(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityAirChangeEvent) {
			got = e
			e.SetCancelled(true)
		})
	})
	srv.SetPlayerAir("Victim", 100)
	servertest.RunTicks(t, srv, 3)
	if got == nil || got.Air <= 100 {
		t.Fatalf("event = %+v, want a refill above 100", got)
	}
	if air := srv.PlayerAir("Victim"); air != 100 {
		t.Errorf("air = %d after the cancelled refill, want 100", air)
	}
}

// vanilla: BowItem.releaseUsing
func TestPluginEntityShootBowEvent(t *testing.T) {
	var got *plugin.EntityShootBowEvent
	srv, c := entityPlugin(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityShootBowEvent) {
			got = e
			e.SetCancelled(true)
		})
	})
	servertest.GiveBowAndArrows(srv, "Victim")
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	servertest.DrawBow(c, -90, 0)
	servertest.HoldDraw(t, srv, "Victim", 24)
	servertest.ReleaseBow(c)
	c.Settle()
	if got == nil || got.Force < 0.99 || !got.Consume || got.Bow.Count != 1 {
		t.Fatalf("event = %+v", got)
	}
	if n := srv.InventorySlot("Victim", 37).Count; n != 5 {
		t.Errorf("arrows after a cancelled shot = %d, want 5", n)
	}
}

// vanilla: Wolf.mobInteract, TamableAnimal.tame
func TestPluginEntityTameEvent(t *testing.T) {
	var got *plugin.EntityTameEvent
	srv, c := entityPlugin(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityTameEvent) {
			got = e
			e.SetCancelled(true)
		})
	})
	pos := srv.PlayerPosition("Victim")
	wolf := srv.SpawnMob("minecraft:wolf", pos.X+1.5, pos.Y, pos.Z)
	srv.SetMobNoAI(wolf)
	srv.GiveSlot("Victim", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:bone"), Count: 64})
	servertest.HoldTicks(t, srv)
	for range 60 {
		if got != nil {
			break
		}
		c.UseOn(wolf)
		c.Settle()
	}
	if got == nil || got.Owner.UUID() != protocol.OfflineUUID("Victim") {
		t.Fatalf("event = %+v", got)
	}
	if got.Entity.Tamed() {
		t.Error("a cancelled taming tamed the wolf")
	}
}

// vanilla: FoodData.eat
func TestPluginFoodLevelChangeOnEating(t *testing.T) {
	var got *plugin.FoodLevelChangeEvent
	srv, c := entityPlugin(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.FoodLevelChangeEvent) {
			if !e.Item.Empty() {
				got = e
				e.SetCancelled(true)
			}
		})
	})
	srv.SetFood("Victim", 10)
	srv.GiveSlot("Victim", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:apple"), Count: 1})
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle() // the use is read before the ticks that finish the meal
	servertest.RunTicks(t, srv, 60)
	c.AwaitEntityEvent(c.PlayerEntityID, protocol.EntityStatusUseFinished)
	if got == nil || got.Level != 14 {
		t.Fatalf("event = %+v, want the apple's 4 points on 10", got)
	}
	if food := srv.PlayerFood("Victim"); food != 10 {
		t.Errorf("food = %d after a cancelled meal, want 10", food)
	}
}

// vanilla: AbstractCubeMob.remove
func TestPluginSlimeSplitEvent(t *testing.T) {
	var got *plugin.SlimeSplitEvent
	srv, _ := entityPlugin(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.SlimeSplitEvent) {
			got = e
			e.SetCancelled(true)
		})
	})
	pos := srv.PlayerPosition("Victim")
	slime := srv.SpawnMob("minecraft:slime", pos.X+3, pos.Y, pos.Z)
	srv.SetMobSize(slime, 4)
	before := srv.MobCount()
	srv.KillMob(slime)
	srv.TickMobs(40)
	if got == nil || got.Count < 2 || got.Slime == nil {
		t.Fatalf("event = %+v", got)
	}
	if after := srv.MobCount(); after != before-1 {
		t.Errorf("mobs %d -> %d, want the dead cube gone and no splits", before, after)
	}
}
