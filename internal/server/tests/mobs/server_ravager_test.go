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
	"github.com/golem-mc/golem/internal/server/world"
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

// entityEventsOf collects the entity events of the entity the client sees in
// n ticks.
func entityEventsOf(c *servertest.Client, srv *server.Server, id int32, n int) map[uint8]int {
	seen := map[uint8]int{}
	pumpTicks(c, srv, n, servertest.HostileSkip, func(pid int32, data []byte) bool {
		if pid == protocol.IDPlayEntityEvent {
			r := protocol.NewReader(data)
			if r.ReadI32() == id {
				seen[r.ReadU8()]++
			}
		}
		return false
	})
	return seen
}

// TestARavagerHasItsVanillaBody: a hundred health.
// vanilla: Ravager.createAttributes
func TestARavagerHasItsVanillaBody(t *testing.T) {
	t.Parallel()
	srv, _, _ := ravagerWorld(t)
	id := srv.SpawnMob(ravagerKind, 40.5, 64, 40.5)
	if hp, _ := srv.MobHealth(id); hp != 100 {
		t.Errorf("ravager health = %v, want 100", hp)
	}
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

// TestARavagerSwingsAndStandsStillForTenTicks: a blow plays the attack event
// and holds the body still while the swing counts down.
// vanilla: Ravager.doHurtTarget, Ravager.isImmobile
func TestARavagerSwingsAndStandsStillForTenTicks(t *testing.T) {
	t.Parallel()
	srv, c, _ := ravagerWorld(t)
	id := srv.SpawnMob(ravagerKind, 2.5, 64, 0.5)
	var swung bool
	pumpTicks(c, srv, 400, servertest.HostileSkip, func(pid int32, data []byte) bool {
		if pid == protocol.IDPlayEntityEvent {
			r := protocol.NewReader(data)
			swung = r.ReadI32() == id && r.ReadU8() == protocol.EntityStatusRavagerAttack
		}
		return swung
	})
	if !swung {
		t.Fatal("the ravager never swung")
	}
	attack, _, _, _ := srv.MobRavagerTicks(id)
	if attack < 1 || attack > 10 {
		t.Fatalf("attack ticks = %d after the swing, want 1 to 10", attack)
	}
	x, _, z, _ := srv.MobPos(id)
	servertest.RunTicks(t, srv, attack-1)
	// Momentum from the approach carries it a few hundredths of a block.
	if nx, _, nz, _ := srv.MobPos(id); math.Hypot(nx-x, nz-z) > 0.1 {
		t.Errorf("the ravager moved (%v, %v) -> (%v, %v) mid-swing", x, z, nx, nz)
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
		servertest.RunTicks(t, srv, 10)
	}
	if stuns == 0 || throws == 0 {
		t.Errorf("stuns = %d, throws = %d over the blows, want both", stuns, throws)
	}
}

// TestAStunnedRavagerRoarsAfterFortyTicks: forty ticks of stun, a roar tick
// count of 20, and at 10 the roar hurts a player in reach for 6, spares an
// illager but throws it, and hurts and throws another mob.
// vanilla: Ravager.aiStep, Ravager.roar
func TestAStunnedRavagerRoarsAfterFortyTicks(t *testing.T) {
	t.Parallel()
	srv, c, _ := ravagerWorld(t)
	id := srv.SpawnMob(ravagerKind, 3.5, 64, 0.5)
	pig := srv.SpawnMob("minecraft:pig", 3.5, 64, 3.5)
	illager := srv.SpawnMob("minecraft:vindicator", 3.5, 64, -2.5)
	srv.SetMobNoAI(pig)
	srv.SetMobNoAI(illager)
	srv.SetMobRavagerTicks(id, 0, 40, 0)
	seen := entityEventsOf(c, srv, id, 45)
	if hp := srv.PlayerHealth(ravagerHero); hp != 20 {
		t.Fatalf("health = %v before the roar, want 20", hp)
	}
	if _, stun, roar, _ := srv.MobRavagerTicks(id); stun != 0 || roar != 20-5 {
		t.Errorf("after 45 ticks stun = %d, roar = %d, want 0 and 15", stun, roar)
	}
	for k, v := range entityEventsOf(c, srv, id, 6) {
		seen[k] += v
	}
	if seen[protocol.EntityStatusRavagerRoar] != 1 {
		t.Errorf("roar events = %d, want 1", seen[protocol.EntityStatusRavagerRoar])
	}
	if hp := srv.PlayerHealth(ravagerHero); hp != 14 {
		t.Errorf("health = %v after the roar, want 14", hp)
	}
	if hp, _ := srv.MobHealth(pig); hp != 4 {
		t.Errorf("pig health = %v, want 4 after 6 damage", hp)
	}
	if hp, _ := srv.MobHealth(illager); hp != 24 {
		t.Errorf("vindicator health = %v, want the illager spared", hp)
	}
	if _, _, pz, _ := srv.MobVelocity(pig); pz < 0.5 {
		t.Errorf("pig vz = %v, want it thrown away from the ravager along +z", pz)
	}
	if _, _, iz, _ := srv.MobVelocity(illager); iz > -0.5 {
		t.Errorf("vindicator vz = %v, want it thrown away from the ravager along -z", iz)
	}
}

// TestARavagersRoarIsAnEntityActionVibration: the roar posts entity_action at
// the ravager, which a sculk sensor in range picks up.
// vanilla: Ravager.roar
func TestARavagersRoarIsAnEntityActionVibration(t *testing.T) {
	t.Parallel()
	srv, _, _ := ravagerWorld(t)
	srv.SetBlock(8, 64, 4, servertest.MustState(t, "minecraft:sculk_sensor", nil))
	id := srv.SpawnMob(ravagerKind, 8.5, 64, 0.5)
	srv.SetMobNoAI(id)
	servertest.RunTicks(t, srv, 4)
	if _, props := servertest.StatePropsOf(t, srv, 8, 64, 4); props["sculk_sensor_phase"] != "inactive" {
		t.Fatalf("sensor phase = %q before the roar, want inactive", props["sculk_sensor_phase"])
	}
	srv.SetMobRavagerTicks(id, 0, 0, 11)
	servertest.RunTicks(t, srv, 12)
	if _, props := servertest.StatePropsOf(t, srv, 8, 64, 4); props["sculk_sensor_phase"] == "inactive" {
		t.Error("the sensor never heard the roar")
	}
}

// TestANoAIRavagerStillRunsItsCountdowns: a stun ends in a roar and the
// countdowns run down with the goals off.
// vanilla: Ravager.aiStep
func TestANoAIRavagerStillRunsItsCountdowns(t *testing.T) {
	t.Parallel()
	srv, _, _ := ravagerWorld(t)
	id := srv.SpawnMob(ravagerKind, 8.5, 64, 0.5)
	srv.SetMobNoAI(id)
	srv.SetMobRavagerTicks(id, 7, 5, 0)
	servertest.RunTicks(t, srv, 6)
	if attack, stun, roar, _ := srv.MobRavagerTicks(id); attack != 1 || stun != 0 || roar != 19 {
		t.Errorf("after 6 ticks attack = %d, stun = %d, roar = %d, want 1, 0 and 19", attack, stun, roar)
	}
}

// TestARoarSparesAPlayerOutOfReach: more than four blocks from the ravager's
// box, the roar does nothing to a player.
// vanilla: Ravager.roar
func TestARoarSparesAPlayerOutOfReach(t *testing.T) {
	t.Parallel()
	srv, _, _ := ravagerWorld(t)
	id := srv.SpawnMob(ravagerKind, 8.5, 64, 0.5)
	srv.SetMobRavagerTicks(id, 0, 0, 11)
	servertest.RunTicks(t, srv, 3)
	if _, _, roar, _ := srv.MobRavagerTicks(id); roar != 8 {
		t.Fatalf("roar ticks = %d after 3 ticks from 11, want 8 (the roar went off at 10)", roar)
	}
	if hp := srv.PlayerHealth(ravagerHero); hp != 20 {
		t.Errorf("health = %v, want a player 7 blocks off unhurt", hp)
	}
}

// TestARavagerKeepsItsCountdownsAcrossARestart: AttackTick, StunTick and
// RoarTick are read from the saved ravager and written back.
// vanilla: Ravager.addAdditionalSaveData, Ravager.readAdditionalSaveData
func TestARavagerKeepsItsCountdownsAcrossARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.LevelName = t.TempDir()
	id := servertest.FixtureUUID(7, 8, 9, 10)
	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{},
		nbt.Compound{
			"id":                  nbt.String(ravagerKind),
			"UUID":                servertest.UuidWords(id),
			"Pos":                 nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(8.5), nbt.Double(64), nbt.Double(8.5)}},
			"NoAI":                nbt.Byte(1),
			"PersistenceRequired": nbt.Byte(1),
			"AttackTick":          nbt.Int(80),
			"StunTick":            nbt.Int(90),
			"RoarTick":            nbt.Int(100),
		})
	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login("Keeper")
	c.Configure()
	c.JoinSeeingMobs("Keeper", id)
	servertest.HoldTicks(t, srv)
	var mobID int32
	for _, m := range srv.Mobs() {
		if m.Kind == ravagerKind {
			mobID = m.ID
		}
	}
	// The join's ticks counted all three down alike, NoAI or not.
	attack, stun, roar, _ := srv.MobRavagerTicks(mobID)
	if attack >= 80 || stun-attack != 10 || roar-stun != 10 {
		t.Errorf("loaded ticks = %d, %d, %d, want 80, 90, 100 less the same elapsed ticks", attack, stun, roar)
	}
	srv.FlushMobs()
	saved := servertest.SavedMobsByUUID(t, cfg.LevelName)[id]
	for key, want := range map[string]int32{"AttackTick": int32(attack), "StunTick": int32(stun), "RoarTick": int32(roar)} {
		if got, _ := nbt.IntField(saved, key); got != want {
			t.Errorf("saved %s = %d, want %d", key, got, want)
		}
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

// vanilla: Ravager.<init> setPathfindingMalus, Ravager.aiStep
func TestARavagerPathsThroughALeafWall(t *testing.T) {
	t.Parallel()
	srv, _, _ := ravagerWorld(t)
	srv.SetPlayerGameMode(ravagerHero, 0)
	leaves := registry.MustState("minecraft:oak_leaves", map[string]string{"persistent": "true"})
	for x := int32(-4); x <= 4; x++ {
		for y := int32(64); y < 67; y++ {
			if !srv.SetBlock(x, y, 5, leaves) {
				t.Fatal("the leaf wall was not placed")
			}
		}
	}
	id := srv.SpawnMobSeeded(ravagerKind, 0.5, 64, 10.5, 1)
	if !srv.SetMobTargetPlayer(id, ravagerHero) {
		t.Fatal("the ravager did not target the player")
	}
	servertest.UntilTicks(t, srv, 200, "the ravager through the middle of the leaf wall", func() bool {
		x, _, z, ok := srv.MobPos(id)
		if !ok || x < -3 || x > 4 {
			t.Fatalf("ravager went around the wall: (%v, %v), present=%v", x, z, ok)
		}
		return z < 4 && srv.BlockAt(0, 64, 5) == 0 && srv.BlockAt(0, 65, 5) == 0
	})
}

// TestADeadRavagerDropsASaddle: its loot table is one saddle, whoever killed it.
// vanilla: entities/ravager loot table
func TestADeadRavagerDropsASaddle(t *testing.T) {
	t.Parallel()
	srv, _, _ := ravagerWorld(t)
	id := srv.SpawnMob(ravagerKind, 20.5, 64, 20.5)
	srv.SetMobNoAI(id)
	srv.KillMob(id)
	servertest.UntilTicks(t, srv, 100, "the saddle", func() bool {
		return overworldHoldsItemNamed(srv, "minecraft:saddle")
	})
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

// TestARavagerRiddenByAZombieNeverAttacks: a non-raider mob rider takes the
// ravager's MOVE and LOOK, so its melee goal never starts; the zombie steers
// it alongside the player (Mob.getNavigation hands the rider the mount's),
// but no blow is the ravager's.
// vanilla: Mob.updateControlFlags, Ravager.updateControlFlags
func TestARavagerRiddenByAZombieNeverAttacks(t *testing.T) {
	t.Parallel()
	srv, c, playerID := ravagerWorld(t)
	id := riddenRavager(t, srv, "minecraft:zombie")
	if ravagerGores(c, srv, playerID, id, 100) {
		t.Error("the ravager hit the player under a zombie rider")
	}
	if x, _, z, _ := srv.MobPos(id); math.Hypot(x-0.5, z-0.5) > 4 {
		t.Errorf("the zombie never steered the ravager to the player: at (%v, %v)", x, z)
	}
}
