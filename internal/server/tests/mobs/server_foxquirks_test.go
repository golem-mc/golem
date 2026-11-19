package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAFoxKitFromASpawnEggTrustsItsSpawner: a fox egg used on a grown fox
// makes a kit that trusts the player.
// vanilla: Fox.onOffspringSpawnedFromEgg
func TestAFoxKitFromASpawnEggTrustsItsSpawner(t *testing.T) {
	t.Parallel()
	srv, c := servertest.EggPlayer(t, "survival", "Breeder", "minecraft:fox_spawn_egg", 2)
	srv.StopTick()
	srv.SpawnMob(foxType, 2.5, 64, 1.5)
	parent := c.ExpectMobSpawn(protocol.EntityTypeFox)
	if parent.Baby {
		t.Fatal("the planted fox is a baby")
	}
	c.UseOn(parent.EntityID)
	kit := c.AwaitSpawn(protocol.EntityTypeFox)
	if !srv.FoxTrusts(kit.EntityID, "Breeder") {
		t.Error("the kit does not trust the player whose egg made it")
	}
}

// TestAFoxSpitsItsOldMouthfulWhenItTakesAMeal: swapping a stick for bread
// lays the stick a block ahead of the fox and a block up, out of reach for
// forty ticks, and the spit is heard at the fox.
// vanilla: Fox.pickUpItem, Fox.spitOutItem
func TestAFoxSpitsItsOldMouthfulWhenItTakesAMeal(t *testing.T) {
	t.Parallel()
	srv, c := foxSceneClient(t, false)
	id := srv.SpawnMob(foxType, 6.5, 64, 0.5)
	srv.TrustFox(id, "Fox")
	srv.SetFoxMouth(id, "minecraft:stick")
	servertest.RunTicks(t, srv, 5)
	srv.SpawnItemEntityDelay(6.5, 64, 0.5, "minecraft:bread", 1, 0)
	var stick *server.TestItem
	for i := 0; i < 40 && stick == nil; i++ {
		servertest.RunTicks(t, srv, 1)
		for _, it := range srv.ItemsInDimension("minecraft:overworld") {
			if it.Item == "minecraft:stick" {
				stick = &it
			}
		}
	}
	if stick == nil {
		t.Fatal("the fox never spat the stick")
	}
	fx, fy, fz, _ := srv.MobPos(id)
	if d := math.Hypot(stick.X-fx, stick.Z-fz); d < 0.5 || d > 1.6 {
		t.Errorf("the stick lies %v blocks from the fox, want about one", d)
	}
	if stick.Y < fy+0.5 {
		t.Errorf("the stick is at y=%v, want about a block above the fox at %v", stick.Y, fy)
	}
	if stick.PickupDelay < 36 || stick.PickupDelay > 40 {
		t.Errorf("the stick's pickup delay = %d, want about 40", stick.PickupDelay)
	}
	sx, _, sz, _ := c.AwaitSoundNamed("minecraft:entity.fox.spit")
	if math.Abs(float64(sx)/8-fx) > 0.6 || math.Abs(float64(sz)/8-fz) > 0.6 {
		t.Errorf("the spit played at (%v, %v), want at the fox (%v, %v)", float64(sx)/8, float64(sz)/8, fx, fz)
	}
}

// TestAFaceplantedFoxThrowsUpSnowParticles: the block under a faceplanted
// fox bursts as break particles.
// vanilla: Fox.tick
func TestAFaceplantedFoxThrowsUpSnowParticles(t *testing.T) {
	t.Parallel()
	srv, c := foxSceneClient(t, false)
	id := srv.SpawnMob(foxType, 6.5, 64, 0.5)
	srv.TrustFox(id, "Fox")
	srv.FaceplantFox(id)
	servertest.RunTicks(t, srv, 30)
	var heard bool
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid == protocol.IDPlayWorldEvent && protocol.NewReader(data).ReadI32() == protocol.WorldEventBlockBreak {
			heard = true
		}
	})
	if !heard {
		t.Error("no block break burst around the faceplanted fox")
	}
}
