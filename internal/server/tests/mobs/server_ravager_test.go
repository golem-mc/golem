package mobs

import (
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

const (
	ravagerKind = "minecraft:ravager"
	ravagerHero = "Slayer"
)

// ravagerWorld joins a player on Normal difficulty, standing on the ground at
// the origin.
func ravagerWorld(t *testing.T) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	srv, c := servertest.Dial(t, servertest.HostileTestConfig())
	srv.SetDifficulty(rules.DifficultyNormal)
	id := c.JoinAs(ravagerHero)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	return srv, c, id
}

// TestARavagersBlowHurtsTwelveAndShovesAlongItsFacing: a blow of 12 on Normal,
// the default knockback and the attack_knockback attribute's 1.5 (half of it
// on top).
// vanilla: Ravager.createAttributes, LivingEntity.getKnockback
func TestARavagersBlowHurtsTwelveAndShovesAlongItsFacing(t *testing.T) {
	t.Parallel()
	srv, c, playerID := ravagerWorld(t)
	id := srv.SpawnMob(ravagerKind, 3.5, 64, 0.5)
	srv.SetMobNoAI(id)
	srv.SetPlayerVelocity(ravagerHero, 0, 0, 0)
	if !srv.StrikePlayerWith(id, ravagerHero, 12) {
		t.Fatal("the strike found no ravager or player")
	}
	if hp := srv.PlayerHealth(ravagerHero); hp != 8 {
		t.Errorf("health = %v, want 8 after the blow of 12", hp)
	}
	var got [][3]float64
	c.ReadTicks(srv, 1, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlaySetEntityMotion {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() == playerID {
			vx, vy, vz := r.ReadLpVec3()
			got = append(got, [3]float64{vx, vy, vz})
		}
		return false
	})
	if len(got) == 0 {
		t.Fatal("the blow shoved nobody")
	}
	vx, vy, vz := rules.KnockbackExtra(0.5-3.5, 0, 0, 0.75, 1, true, rand.New(rand.NewSource(1)))
	vx, vy, vz = vx*0.546, (vy-0.08)*0.98, vz*0.546
	if math.Abs(got[0][0]-vx) > 0.01 || math.Abs(got[0][1]-vy) > 0.01 || math.Abs(got[0][2]-vz) > 0.01 {
		t.Errorf("shove = %v, want (%v, %v, %v)", got[0], vx, vy, vz)
	}
}

// TestAShieldedRavagerBlowStunsOrThrows: a blow a shield took stuns the
// ravager for 40 ticks or throws the defender four blocks a tick, about half
// the times each; neither is the shield's plain 0.5 shove, and the fully
// blocked blow deals no default knockback.
// vanilla: Ravager.blockedByItem, LivingEntity.hurtServer (fullyBlocked)
func TestAShieldedRavagerBlowStunsOrThrows(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, ravagerHero))
	srv.SetDifficulty(rules.DifficultyNormal)
	c := connect().Join(ravagerHero)
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.GiveSlot(ravagerHero, player.OffHandSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:shield"), Count: 1})
	c.UseItemRightClicks(protocol.HandOff)
	c.Settle()
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob(ravagerKind, 0.5, 64, 3.5)
	srv.SetMobNoAI(id)
	servertest.RunTicks(t, srv, 10)
	var stuns, throws int
	for range 60 {
		if stuns > 0 && throws > 0 {
			break
		}
		srv.SetMobRavagerTicks(id, 0, 0, 0)
		srv.SetPlayerVelocity(ravagerHero, 0, 0, 0)
		if !srv.StrikePlayerWith(id, ravagerHero, 12) {
			t.Fatal("the strike found no ravager or player")
		}
		if hp := srv.PlayerHealth(ravagerHero); hp != 20 {
			t.Fatalf("health = %v, want the shield to have blocked the blow", hp)
		}
		vx, _, vz := srv.PlayerVelocity(ravagerHero)
		_, stun, _, _ := srv.MobRavagerTicks(id)
		// The ravager stands 3 blocks along +z of the holder: the throw is
		// 4*3/9 toward -z and the stun's nudge 0.05, alone.
		switch {
		case stun == 40 && math.Abs(vx) < 1e-6 && math.Abs(vz+0.05) < 1e-6:
			stuns++
		case stun == 0 && math.Abs(vx) < 1e-6 && math.Abs(vz+4.0/3) < 1e-6:
			throws++
		default:
			t.Fatalf("stun = %d with velocity (%v, %v), want 40 ticks and (0, -0.05) or no stun and (0, -1.3333)", stun, vx, vz)
		}
	}
	if stuns == 0 || throws == 0 {
		t.Errorf("stuns = %d, throws = %d over the blows, want both", stuns, throws)
	}
}

// TestARavagerBreaksTheLeavesItMeets: a ravager that meets a leaf wall breaks
// the leaves while mob_griefing is on, and leaves them while it is off.
// vanilla: Ravager.aiStep
func TestARavagerBreaksTheLeavesItMeets(t *testing.T) {
	t.Parallel()
	for _, griefing := range []bool{true, false} {
		srv, _, _ := ravagerWorld(t)
		srv.SetGameRule("mob_griefing", griefing)
		leaves := registry.MustState("minecraft:oak_leaves", map[string]string{"persistent": "true"})
		wall := 0
		for x := int32(-40); x <= 40; x++ {
			for y := int32(64); y <= 68; y++ {
				if srv.SetBlock(x, y, 6, leaves) {
					wall++
				}
			}
		}
		if wall == 0 {
			t.Fatal("the leaf wall was not placed")
		}
		srv.SetBlock(4, 64, 9, servertest.MustState(t, "minecraft:sculk_sensor", nil))
		id := srv.SpawnMob(ravagerKind, 0.5, 64, 8.2)
		servertest.RunTicks(t, srv, 20)
		// Thrown at the wall, the body meets the leaves.
		srv.PushMob(id, 0, 0, -0.6)
		servertest.RunTicks(t, srv, 6)
		broken := false
		for x := int32(-8); x <= 8; x++ {
			if srv.BlockAt(x, 64, 6) != leaves {
				broken = true
			}
		}
		if broken != griefing {
			t.Errorf("mob_griefing %v: leaves broken = %v", griefing, broken)
		}
		// Each broken leaf is a block_destroy vibration.
		_, props := servertest.StatePropsOf(t, srv, 4, 64, 9)
		if heard := props["sculk_sensor_phase"] != "inactive"; heard != griefing {
			t.Errorf("mob_griefing %v: sensor phase %q", griefing, props["sculk_sensor_phase"])
		}
	}
}

// riddenRavager spawns a ravager six blocks from the player with a rider of
// the kind seated on it.
func riddenRavager(t *testing.T, srv *server.Server, rider string) int32 {
	t.Helper()
	id := srv.SpawnMob(ravagerKind, 6.5, 64, 0.5)
	if !srv.LinkMobsForTest(id, srv.SpawnMob(rider, 6.5, 64, 0.5)) {
		t.Fatal("the rider was not seated")
	}
	return id
}

// ravagerGores pumps up to n ticks and reports whether the player took a
// blow whose source is the ravager.
func ravagerGores(c *servertest.Client, srv *server.Server, playerID, id int32, n int) bool {
	var gored bool
	pumpTicks(c, srv, n, servertest.HostileSkip, func(pid int32, data []byte) bool {
		if pid == protocol.IDPlayDamageEvent {
			r := protocol.NewReader(data)
			victim, _, cause := r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt()
			gored = victim == playerID && cause == id+1
		}
		return gored
	})
	return gored
}

// TestARavagerRiddenByAPillagerStillAttacks: a raider rider leaves the
// ravager its own MOVE, JUMP and LOOK, so its melee goal closes in and gores
// the player. The pillager shares the ravager's navigation and stops it
// while the player is in its 8-block crossbow range, so the ravager only
// creeps forward on its own path recalculations: the blow lands near tick
// 350, hence the 400-tick window.
// vanilla: Ravager.updateControlFlags, Mob.getNavigation,
// RangedCrossbowAttackGoal.tick
func TestARavagerRiddenByAPillagerStillAttacks(t *testing.T) {
	t.Parallel()
	srv, c, playerID := ravagerWorld(t)
	id := riddenRavager(t, srv, "minecraft:pillager")
	if !ravagerGores(c, srv, playerID, id, 400) {
		t.Error("the ridden ravager never hit the player in 400 ticks")
	}
}

// TestARavagersRoarThrowsAfterTheHurtKnockback: the roar's hurt knockback
// runs first, on the old motion, and the four-block throw is added after it:
// X = -4 / d - 0.4 for a mob d blocks west, not half the throw.
// vanilla: Ravager.roar, Ravager.strongKnockback
func TestARavagersRoarThrowsAfterTheHurtKnockback(t *testing.T) {
	t.Parallel()
	srv, _, _ := ravagerWorld(t)
	noAI := nbt.Compound{"NoAI": nbt.Byte(1)}
	id := srv.SpawnMobTagged(ravagerKind, 0.5, 64, 8.5, nbt.Compound{"NoAI": nbt.Byte(1), "StunTick": nbt.Int(5)})
	cow := srv.SpawnMobTagged("minecraft:cow", -3.5, 64, 8.5, noAI)
	witch := srv.SpawnMobTagged("minecraft:witch", -1.5, 64, 8.5, noAI)
	if id == 0 || cow == 0 || witch == 0 {
		t.Fatal("a mob was refused")
	}
	servertest.RunTicks(t, srv, 30)
	for _, c := range []struct {
		id   int32
		want float64
	}{{cow, -1.4}, {witch, -2.4}} {
		vx, vy, _, ok := srv.MobVelocity(c.id)
		if !ok {
			t.Fatalf("mob %d is gone", c.id)
		}
		if math.Abs(vx-c.want) > 1e-6 || math.Abs(vy-0.2) > 1e-6 {
			t.Errorf("mob %d motion = (%v, %v), want (%v, 0.2)", c.id, vx, vy, c.want)
		}
	}
}
