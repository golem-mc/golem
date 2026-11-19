package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The camel husk: a monster camel that cannot breed, and the camel husk a
// natural husk may ride.
// vanilla: CamelHusk, Husk.finalizeSpawn

// camelHuskSeed makes a natural husk's roll for a camel husk land.
const camelHuskSeed = 4

func mobByID(srv *server.Server, id int32) server.TestMob {
	for _, m := range srv.Mobs() {
		if m.ID == id {
			return m
		}
	}
	return server.TestMob{}
}

// TestCamelHuskSpawnsAndDoesNotBreed: a summoned camel husk arrives with a
// camel's 32 health and feeding it a rabbit foot never sends it into love.
// vanilla: CamelHusk.canFallInLove, isFood
func TestCamelHuskSpawnsAndDoesNotBreed(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:rabbit_foot", 8, func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	typeID, _ := registry.EntityTypeID("minecraft:camel_husk")
	id := srv.SpawnMob("minecraft:camel_husk", 1.5, 64, 2.5)
	got := c.ExpectMobSpawn(typeID)
	if got.EntityID != id || got.Health != 32 {
		t.Fatalf("spawn = %+v, want entity %d with 32 health", got, id)
	}
	c.UseOn(id)
	c.Settle()
	if mobByID(srv, id).InLove != 0 {
		t.Fatal("a camel husk fed a rabbit foot fell in love")
	}
	if !srv.MobPersistent(id) {
		t.Fatal("interacting did not make the camel husk persistent")
	}
}

// TestNaturalHuskRollsCamelHuskJockey: a natural husk whose roll lands holds
// an iron spear on a camel husk, a parched behind it; a summoned one never
// rolls.
// vanilla: Husk.finalizeSpawn
func TestNaturalHuskRollsCamelHuskJockey(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.MobTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMobSeeded("minecraft:husk", 5.5, 64, 0.5, camelHuskSeed, true)
	if hand := srv.MobHand(id); registry.Item(hand.ItemID) != registry.ItemIronSpear {
		t.Fatalf("hand = %+v", hand)
	}
	vehicle := srv.MobVehicleID(id)
	if vehicle == 0 || mobByID(srv, vehicle).Kind != "minecraft:camel_husk" {
		t.Fatalf("husk rides %d (%s), want a camel husk", vehicle, mobByID(srv, vehicle).Kind)
	}
	riders := srv.MobPassengerIDs(vehicle)
	if len(riders) != 2 || riders[0] != id || mobByID(srv, riders[1]).Kind != "minecraft:parched" {
		t.Fatalf("riders = %v, want the husk then a parched", riders)
	}
	if other := srv.SpawnMobSeeded("minecraft:husk", 7.5, 64, 0.5, camelHuskSeed); srv.MobVehicleID(other) != 0 {
		t.Fatal("a summoned husk rolled a camel husk")
	}
}

// TestHuskUnderLowCeilingRidesNoCamelHusk: a natural husk in a one-block
// gap draws no roll for a camel husk.
// vanilla: Husk.finalizeSpawn
func TestHuskUnderLowCeilingRidesNoCamelHusk(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.MobTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	srv.SetBlock(9, 65, 0, registry.BlockStates["minecraft:stone"])
	id := srv.SpawnMobSeeded("minecraft:husk", 9.5, 64, 0.5, camelHuskSeed, true)
	if srv.MobVehicleID(id) != 0 {
		t.Fatal("a husk under a ceiling rode a camel husk")
	}
	if hand := srv.MobHand(id); registry.Item(hand.ItemID) == registry.ItemIronSpear {
		t.Fatalf("hand = %+v, want no spear", hand)
	}
}

// TestCamelHuskJockeyChargesAtFourTimesTheSpeed: a husk charging with its
// spear from a camel husk walks the mount at the camel husk's charge speed
// modifier, 4, against 1 for the same jockey with an empty hand.
// vanilla: SpearUseGoal.tick, CamelHusk.chargeSpeedModifier
func TestCamelHuskJockeyChargesAtFourTimesTheSpeed(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.HostileTestConfig(), "Target")
	servertest.HoldTicks(t, srv)
	spear := srv.SpawnMobSeeded("minecraft:husk", 25.5, 64, 0.5, camelHuskSeed, true)
	plain := srv.SpawnMobSeeded("minecraft:husk", -24.5, 64, 0.5, camelHuskSeed, true)
	srv.SetMobHand(plain, protocol.ItemStack{})
	servertest.UntilTicks(t, srv, 200, "both jockeys walk toward the player", func() bool {
		return srv.MobTargetID(spear) != 0 && srv.MobTargetID(plain) != 0 &&
			srv.MobNavSpeedFactor(spear) != 0 && srv.MobNavSpeedFactor(plain) != 0
	})
	servertest.RunTicks(t, srv, 5)
	charged, walked := srv.MobNavSpeedFactor(spear), srv.MobNavSpeedFactor(plain)
	if charged != 4*walked {
		t.Fatalf("charging mount's walk speed factor = %v, the plain one's = %v: want 4x", charged, walked)
	}
}

// TestCamelHuskJockeyWalksTowardItsTargetFasterWhenCharging: a camel husk
// carrying a husk walks along its rider's path, the charging one covering more
// ground than the plain one, and a mount is never pushed by its own rider.
// The pull is speed*speed below speed 1 (setSpeed also sets zza), so a factor of
// 4 covers well over 4x the distance of a factor of 1.
// vanilla: Entity.push, Entity.isPassengerOfSameVehicle, Mob.setSpeed
func TestCamelHuskJockeyWalksTowardItsTargetFasterWhenCharging(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.HostileTestConfig(), "Target")
	servertest.HoldTicks(t, srv)
	spear := srv.SpawnMobSeeded("minecraft:husk", 25.5, 64, 0.5, camelHuskSeed, true)
	plain := srv.SpawnMobSeeded("minecraft:husk", -24.5, 64, 0.5, camelHuskSeed, true)
	srv.SetMobHand(plain, protocol.ItemStack{})
	spearMount, plainMount := srv.MobVehicleID(spear), srv.MobVehicleID(plain)
	if spearMount == 0 || plainMount == 0 {
		t.Fatalf("mounts = %d, %d: want both jockeys seated", spearMount, plainMount)
	}
	spearX0, _, _, _ := srv.MobPos(spearMount)
	plainX0, _, _, _ := srv.MobPos(plainMount)
	servertest.UntilTicks(t, srv, 200, "both mounts walk", func() bool {
		x, _, _, _ := srv.MobPos(spearMount)
		return spearX0-x > 1
	})
	servertest.RunTicks(t, srv, 40)
	spearX, _, _, _ := srv.MobPos(spearMount)
	plainX, _, _, _ := srv.MobPos(plainMount)
	charged, walked := spearX0-spearX, plainX-plainX0
	if walked <= 0 {
		t.Fatalf("plain mount moved %v along x from the west: want toward the player (+x)", walked)
	}
	if charged < 4*walked {
		t.Fatalf("charging mount covered %v, plain one %v: want at least 4x", charged, walked)
	}
}

// vanilla: Entity.getControlledVehicle, Mob.getNavigation, SpearUseGoal.tick
func TestCamelHuskFrontRiderChargesInsideTheArchersStoppingDistance(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.HostileTestConfig(), "Target")
	servertest.HoldTicks(t, srv)
	husk := srv.SpawnMobSeeded("minecraft:husk", 9.5, 64, 0.5, camelHuskSeed, true)
	mount := srv.MobVehicleID(husk)
	riders := srv.MobPassengerIDs(mount)
	if len(riders) != 2 {
		t.Fatalf("riders = %v, want the natural two-rider jockey", riders)
	}
	servertest.UntilTicks(t, srv, 100, "both riders target the player", func() bool {
		return srv.MobTargetID(husk) != 0 && srv.MobTargetID(riders[1]) != 0
	})
	servertest.UntilTicks(t, srv, 60, "the front husk drives the camel through bow stopping range", func() bool {
		x, _, _, _ := srv.MobPos(mount)
		return x < 5.5
	})
}

// The husk's kinetic-hit event distinguishes its contact from the rear archer's arrows.
// vanilla: SpearUseGoal.tick, KineticWeapon.damageEntities
func TestCamelHuskSpearRiderAimsDownAndDamagesASurvivalPlayer(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Target")
	servertest.HoldTicks(t, srv)
	// A side approach lets the head finish turning before contact without
	// relying on collision impulses to keep the occupied mount in reach.
	husk := srv.SpawnMobSeeded("minecraft:husk", 9.5, 64, 0.5, camelHuskSeed, true)
	riders := srv.MobPassengerIDs(srv.MobVehicleID(husk))
	if len(riders) != 2 {
		t.Fatalf("riders = %v, want the natural two-rider jockey", riders)
	}
	var hurt, stabbed bool
	c.ReadTicks(srv, 160, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySetHealth:
			hurt = hurt || protocol.NewReader(data).ReadF32() < 20
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			stabbed = stabbed || r.ReadI32() == husk && r.ReadU8() == protocol.EntityStatusKineticHit
		}
		return hurt && stabbed
	})
	if !hurt || !stabbed {
		t.Fatalf("hurt = %v, husk spear contact = %v, want both within 160 ticks", hurt, stabbed)
	}
	m := mobByID(srv, husk)
	p := srv.PlayerPosition("Target")
	wantYaw := math.Atan2(p.Z-m.Z, p.X-m.X)*180/math.Pi - 90
	for _, yaw := range []float32{m.Yaw, m.HeadYaw} {
		if diff := math.Abs(math.Remainder(float64(yaw)-wantYaw, 360)); diff > 30 {
			t.Errorf("attacker yaw %v is %v degrees from the player", yaw, diff)
		}
	}
	if m.Pitch <= 0 {
		t.Fatalf("mounted attacker pitch = %v, want aimed down at the player", m.Pitch)
	}
}
