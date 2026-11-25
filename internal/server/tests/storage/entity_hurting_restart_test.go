package storage

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

func motionOf(c nbt.Compound) [3]float64 {
	var out [3]float64
	if list, ok := nbt.Get[nbt.List](c, "Motion"); ok {
		for i, item := range list.Items {
			if d, ok := item.(nbt.Double); ok && i < 3 {
				out[i] = float64(d)
			}
		}
	}
	return out
}

func thrownByKind(srv *server.Server) map[string]string {
	out := map[string]string{}
	for _, it := range srv.ThrownInDimension("minecraft:overworld") {
		out[it.Kind] = it.UUID
	}
	return out
}

// TestHurtingProjectilesSurviveARestart saves a wind charge, both fireballs
// and a charged wither skull, and finds them again with their blast power
// and charge in the file the reloaded server writes.
// vanilla: AbstractHurtingProjectile.addAdditionalSaveData, LargeFireball.addAdditionalSaveData, WitherSkull.addAdditionalSaveData
func TestHurtingProjectilesSurviveARestart(t *testing.T) {
	t.Parallel()
	kinds := []string{"minecraft:wind_charge", "minecraft:breeze_wind_charge", "minecraft:small_fireball", "minecraft:fireball", "minecraft:wither_skull"}
	var breezeCharge string
	again, level := restartAround(t, func(srv *server.Server) {
		srv.ThrowThrown(entity.ThrownWindCharge, 3.5, 100, 3.5, 0, -90, 0, 1)
		srv.ThrowThrown(entity.ThrownBreezeWindCharge, 7.5, 100, 3.5, 0, -90, 0, 1)
		breezeCharge = thrownByKind(srv)["minecraft:breeze_wind_charge"]
		srv.LaunchFireball(entity.ThrownSmallFireball, 4.5, 100, 3.5, 1, 0, 0, 0, 1)
		srv.LaunchFireball(entity.ThrownLargeFireball, 5.5, 100, 3.5, 1, 0, 0, 0, 3)
		srv.TuneFireballAcceleration(uuid.MustParse(thrownByKind(srv)["minecraft:fireball"]), 0.02)
		srv.LaunchFireball(entity.ThrownWitherSkull, 6.5, 100, 3.5, 1, 0, 0, 0, 1)
		skull := thrownByKind(srv)["minecraft:wither_skull"]
		srv.MakeSkullDangerous(uuid.MustParse(skull))
		if got := thrownByKind(srv); len(got) != len(kinds) {
			t.Fatalf("the first server held %v, want %d kinds", got, len(kinds))
		}
	}, nil)
	servertest.UntilTicks(t, again, 40, "the projectiles woke", func() bool {
		return len(thrownByKind(again)) == len(kinds)
	})
	if got := thrownByKind(again)["minecraft:breeze_wind_charge"]; got != breezeCharge {
		t.Errorf("the reloaded breeze wind charge has UUID %q, want %q", got, breezeCharge)
	}
	again.FlushMobs()
	again.Shutdown()
	pos := world.ChunkPos{X: 0, Z: 0}
	servertest.EntityFileIs(t, level, pos, "minecraft:fireball", func(c nbt.Compound) {
		if power, _ := nbt.Get[nbt.Byte](c, "ExplosionPower"); power != 3 {
			t.Errorf("the reloaded fireball's ExplosionPower = %d, want 3", power)
		}
		if _, ok := nbt.Get[nbt.Compound](c, "Item"); !ok {
			t.Errorf("the reloaded fireball saved no Item")
		}
		if p, _ := nbt.Get[nbt.Double](c, "acceleration_power"); p != 0.02 {
			t.Errorf("the reloaded fireball's acceleration_power = %v, want 0.02", p)
		}
		if vx := motionOf(c)[0]; vx <= 0 {
			t.Errorf("the reloaded fireball flies at vx = %v, want it to resume its heading", vx)
		}
	})
	servertest.EntityFileIs(t, level, pos, "minecraft:wither_skull", func(c nbt.Compound) {
		if d, _ := nbt.Get[nbt.Byte](c, "dangerous"); d != 1 {
			t.Errorf("the reloaded skull's dangerous = %d, want 1", d)
		}
		if _, ok := c["Item"]; ok {
			t.Errorf("the reloaded skull saved an Item")
		}
	})
	servertest.EntityFileIs(t, level, pos, "minecraft:wind_charge", func(c nbt.Compound) {
		if _, ok := c["Item"]; ok {
			t.Errorf("the reloaded wind charge saved an Item")
		}
		if p, ok := nbt.Get[nbt.Double](c, "acceleration_power"); !ok || p != 0 {
			t.Errorf("the reloaded wind charge's acceleration_power = %v (%v), want 0", p, ok)
		}
		if vy := motionOf(c)[1]; vy < 0.5 {
			t.Errorf("the reloaded wind charge climbs at vy = %v, want it to resume its launch", vy)
		}
	})
	servertest.EntityFileIs(t, level, pos, "minecraft:breeze_wind_charge", func(c nbt.Compound) {
		if vy := motionOf(c)[1]; vy < 0.5 {
			t.Errorf("the reloaded breeze wind charge climbs at vy = %v, want it to resume its launch", vy)
		}
	})
	servertest.EntityFileIs(t, level, pos, "minecraft:small_fireball", func(nbt.Compound) {})
}
