package redstone

// The sculk sensor's later half: more event sources, wool in the way, the
// particle that carries a vibration, and the frequency kept in the block
// entity. vanilla: VibrationSystem, SculkSensorBlockEntity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// comparatorSensorScene is sensorScene with a comparator reading the sensor.
func comparatorSensorScene(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.AttachActor(t)
	standAtScene(c)
	servertest.LayComparatorScene(t, c, srv, 5, 64, 4)
	srv.SetBlock(4, 64, 4, servertest.MustState(t, "minecraft:sculk_sensor", nil))
	servertest.HoldTicks(t, srv)
	return srv, c
}

func expectComparator(t *testing.T, srv *server.Server, want string) {
	t.Helper()
	if got := servertest.WirePowerAt(srv, 6, 64, 4); got != want {
		t.Fatalf("the comparator gives %q, want %s", got, want)
	}
}

// TestAnExplosionActivatesASculkSensor: the blast's frequency is 15.
// vanilla: ServerExplosion.explode
func TestAnExplosionActivatesASculkSensor(t *testing.T) {
	t.Parallel()
	srv, _ := comparatorSensorScene(t)
	srv.RunExplosion(2.5, 64.5, 4.5, 0.5)
	servertest.RunTicks(t, srv, 4)
	expectSensor(t, srv, 4, 64, 4, "active", "12")
	expectComparator(t, srv, "15")
}

// TestADoorSwingingActivatesASculkSensor: opening a door is block_open,
// frequency 10.
// vanilla: DoorBlock.setOpen
func TestADoorSwingingActivatesASculkSensor(t *testing.T) {
	t.Parallel()
	srv, c := comparatorSensorScene(t)
	srv.SetBlock(2, 64, 4, servertest.MustState(t, "minecraft:oak_door", map[string]string{"half": "lower"}))
	srv.SetBlock(2, 65, 4, servertest.MustState(t, "minecraft:oak_door", map[string]string{"half": "upper"}))
	c.MoveToGround(0.5, 64, 4.5, true)
	c.Settle()
	c.UseBlock(2, 64, 4)
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	expectSensor(t, srv, 4, 64, 4, "active", "12")
	expectComparator(t, srv, "10")
}

// TestAChestOpeningAndClosingActivatesASculkSensor: container_open is
// frequency 10 and container_close 9.
// vanilla: ContainerOpenersCounter.incrementOpeners, decrementOpeners
func TestAChestOpeningAndClosingActivatesASculkSensor(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	c := w.Client
	servertest.LayComparatorScene(t, c, srv, 5, 64, 4)
	srv.SetBlock(4, 64, 4, servertest.MustState(t, "minecraft:sculk_sensor", nil))
	servertest.HoldTicks(t, srv)
	srv.SetBlock(2, 64, 4, servertest.MustState(t, "minecraft:chest", nil))
	c.MoveToGround(0.5, 64, 4.5, true)
	c.Settle()
	w.UseBlock(2, 64, 4)
	w.Settle()
	servertest.RunTicks(t, srv, 4)
	expectSensor(t, srv, 4, 64, 4, "active", "12")
	expectComparator(t, srv, "10")

	servertest.RunTicks(t, srv, 40)
	expectSensor(t, srv, 4, 64, 4, "inactive", "0")
	w.CloseWindow()
	w.Settle()
	servertest.RunTicks(t, srv, 4)
	expectComparator(t, srv, "9")
}

// TestWoolBlocksTheWayOfAVibration: a wool block between the source and the
// sensor stops the vibration; without it the vibration arrives.
// vanilla: VibrationSystem.Listener.isOccluded
func TestWoolBlocksTheWayOfAVibration(t *testing.T) {
	t.Parallel()
	srv, c := sensorScene(t)
	srv.SetBlock(3, 64, 4, servertest.MustState(t, "minecraft:white_wool", nil))
	placeStoneAt(c, 2)
	servertest.RunTicks(t, srv, 6)
	expectSensor(t, srv, 4, 64, 4, "inactive", "0")

	srv.SetBlock(3, 64, 4, servertest.MustState(t, "minecraft:air", nil))
	c.BreakBlock(2, 64, 4)
	c.Settle()
	servertest.RunTicks(t, srv, 6)
	expectSensor(t, srv, 4, 64, 4, "active", "12")
}

// TestAVibrationParticleFliesToTheSensor reads the particle a sensor sends as
// it takes a vibration: from the source to the sensor's cell, arriving in the
// travel time.
// vanilla: VibrationParticleOption.STREAM_CODEC, BlockPositionSource
func TestAVibrationParticleFliesToTheSensor(t *testing.T) {
	t.Parallel()
	srv, c := sensorScene(t)
	placeStoneAt(c, 2)
	vibration, _, _ := registry.ParticleID("minecraft:vibration")
	servertest.RunTicks(t, srv, 1)
	found := false
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayLevelParticles {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != vibration {
			return
		}
		found = true
		if source := r.ReadVarInt(); source != 0 {
			t.Errorf("the position source type is %d, want 0 (block)", source)
		}
		if bx, by, bz := r.ReadPosition(); bx != 4 || by != 64 || bz != 4 {
			t.Errorf("the particle flies to (%d, %d, %d), want (4, 64, 4)", bx, by, bz)
		}
		if arrival := r.ReadVarInt(); arrival != 2 {
			t.Errorf("the particle arrives in %d ticks, want 2", arrival)
		}
		tail := servertest.ReadParticleTail(t, r)
		x, y, z, count := tail.X, tail.Y, tail.Z, tail.Count
		if count != 1 || x != 2.5 || y != 64.5 || z != 4.5 {
			t.Errorf("the particle starts at (%g, %g, %g) x%d, want (2.5, 64.5, 4.5) x1", x, y, z, count)
		}
	})
	if !found {
		t.Fatal("no vibration particle reached the player")
	}
}

// TestASculkSensorKeepsItsLastFrequencyInItsBlockEntity: the frequency is
// saved, and a frequency loaded with the sensor is what the comparator reads
// while it is active.
// vanilla: SculkSensorBlockEntity.saveAdditional, loadAdditional
func TestASculkSensorKeepsItsLastFrequencyInItsBlockEntity(t *testing.T) {
	t.Parallel()
	srv, c := comparatorSensorScene(t)
	placeStoneAt(c, 2)
	servertest.RunTicks(t, srv, 4)
	tag, ok := srv.BlockEntityTagAt(4, 64, 4)
	if !ok {
		t.Fatal("the sensor has no block entity")
	}
	if got, _ := nbt.Get[nbt.Int](tag, "last_vibration_frequency"); got != 13 {
		t.Errorf("last_vibration_frequency is %d, want 13", got)
	}

	servertest.RunTicks(t, srv, 40)
	srv.SetBlockEntityTagAt(4, 64, 4, "minecraft:sculk_sensor", nbt.Compound{"last_vibration_frequency": nbt.Int(7)})
	srv.RunLine("setblock 4 64 4 minecraft:sculk_sensor[sculk_sensor_phase=active]")
	servertest.RunTicks(t, srv, 2)
	expectSensor(t, srv, 4, 64, 4, "active", "0")
	expectComparator(t, srv, "7")
}

func sensorFrequency(t *testing.T, srv *server.Server) int32 {
	t.Helper()
	tag, ok := srv.BlockEntityTagAt(4, 64, 4)
	if !ok {
		t.Fatal("the sensor has no block entity")
	}
	got, _ := nbt.Get[nbt.Int](tag, "last_vibration_frequency")
	return int32(got)
}

// TestAThrownSnowballShootsAndLandsAsVibrations: the throw is
// projectile_shoot (3), the landing on a block projectile_land (2).
// vanilla: Projectile.tick, Projectile.hitTargetOrDeflectSelf
func TestAThrownSnowballShootsAndLandsAsVibrations(t *testing.T) {
	t.Parallel()
	srv, c := sensorScene(t)
	id, _ := registry.ItemID("minecraft:snowball")
	c.SetCreativeSlot(36, protocol.ItemStack{ItemID: id, Count: 1})
	c.MoveToGround(0.5, 64, 4.5, true)
	c.Settle()
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	servertest.RunTicks(t, srv, 8)
	if got := sensorFrequency(t, srv); got != 3 {
		t.Errorf("the throw gave frequency %d, want 3", got)
	}

	servertest.RunTicks(t, srv, 40)
	srv.SetBlock(2, 64, 4, servertest.MustState(t, "minecraft:stone", nil))
	srv.ThrowThrown(entity.ThrownSnowball, 2.5, 66, 4.5, 0, 90, 0, 1)
	servertest.RunTicks(t, srv, 8)
	if got := sensorFrequency(t, srv); got != 2 {
		t.Errorf("the landing gave frequency %d, want 2", got)
	}
}

// vanilla: Projectile.onHit
func TestAnArrowLandingIsAVibration(t *testing.T) {
	t.Parallel()
	srv, _ := sensorScene(t)
	servertest.RunTicks(t, srv, 40)
	srv.SetBlock(2, 64, 4, servertest.MustState(t, "minecraft:stone", nil))
	// HasBeenShot keeps the shoot vibration out of the landing's.
	srv.RunLine(`summon minecraft:arrow 2.5 66 4.5 {Motion:[0d,-1d,0d],HasBeenShot:1b}`)
	servertest.RunTicks(t, srv, 8)
	if got := sensorFrequency(t, srv); got != 2 {
		t.Errorf("the landing gave frequency %d, want 2", got)
	}
}

// vanilla: Projectile.onHit
func TestASnowballBreakingOnAnArmorStandIsALandingVibration(t *testing.T) {
	t.Parallel()
	srv, _ := sensorScene(t)
	servertest.RunTicks(t, srv, 40)
	srv.SpawnArmorStand(2.5, 64, 4.5)
	srv.ThrowThrown(entity.ThrownSnowball, 2.5, 66, 4.5, 0, 90, 0, 1)
	servertest.RunTicks(t, srv, 8)
	if got := sensorFrequency(t, srv); got != 2 {
		t.Errorf("the landing gave frequency %d, want 2", got)
	}
}

// vanilla: Projectile.onHit
func TestProjectileHitsPostLandings(t *testing.T) {
	for _, kind := range []string{"arrow", "trident", "firework", "bobber", "splash_potion", "lingering_potion", "experience_bottle"} {
		for _, body := range []bool{false, true} {
			name := kind + "/block"
			if body {
				name = kind + "/body"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				srv, c := sensorScene(t)
				if body {
					if kind == "firework" {
						srv.RunLine("summon cow 2.5 64 4.5 {NoAI:1b}")
					} else {
						srv.SpawnArmorStand(2.5, 64, 4.5)
					}
				} else {
					srv.SetBlock(2, 64, 4, servertest.MustState(t, "minecraft:stone", nil))
				}
				events := observeEvents(srv)
				switch kind {
				case "arrow":
					srv.ShootArrow(2.5, 67, 4.5, 0, -1, 0)
				case "trident":
					srv.ShootTrident(2.5, 67, 4.5, 0, -1, 0)
				case "firework":
					srv.SpawnFireworkAtAngle(2.5, 67, 4.5, 0, -1, 0, 100)
				case "splash_potion", "lingering_potion", "experience_bottle":
					thrown := map[string]entity.ThrownKind{
						"splash_potion":     entity.ThrownSplashPotion,
						"lingering_potion":  entity.ThrownLingeringPotion,
						"experience_bottle": entity.ThrownExperienceBottle,
					}[kind]
					srv.ThrowThrown(thrown, 2.5, 67, 4.5, 0, 90, 0, 1)
				case "bobber":
					c.SetCreativeSlot(36, protocol.ItemStack{ItemID: int32(registry.ItemFishingRod), Count: 1})
					c.MoveTo(2.5, 65, 3.5, 0, 90)
					c.Settle()
					servertest.UseHeld(c, 0, 90)
					c.Settle()
				}
				servertest.RunTicks(t, srv, 8)
				got := events.of("minecraft:projectile_land")
				if len(got) == 0 {
					t.Fatal("the hit posted no projectile_land")
				}
				if body {
					if len(got) != 1 || math.Abs(got[0][0]-2.5) > 0.1 || got[0][2] < 4 || got[0][2] > 5 || got[0][1] <= 64 || got[0][1] >= 67 {
						t.Errorf("body landings = %v, want one at the hit point", got)
					}
				} else if got[0] != [3]float64{2.5, 64.5, 4.5} {
					t.Errorf("block landing = %v, want the block centre", got[0])
				}
			})
		}
	}
}

// vanilla: Projectile.hitTargetOrDeflectSelf
func TestProjectileDeflectionsPostNoLanding(t *testing.T) {
	for _, arrow := range []bool{false, true} {
		t.Run(map[bool]string{false: "snowball", true: "arrow"}[arrow], func(t *testing.T) {
			t.Parallel()
			srv, _ := sensorScene(t)
			id := srv.SpawnMob("minecraft:breeze", 2.5, 64, 4.5)
			srv.SetMobNoAI(id)
			events := observeEvents(srv)
			if arrow {
				srv.ShootArrow(2.5, 67, 4.5, 0, -1, 0)
			} else {
				srv.ThrowThrown(entity.ThrownSnowball, 2.5, 67, 4.5, 0, 90, 0, 1)
			}
			servertest.RunTicks(t, srv, 4)
			if got := events.of("minecraft:projectile_land"); len(got) != 0 {
				t.Errorf("deflection landings = %v, want none", got)
			}
		})
	}
}

// vanilla: AbstractArrow.stepMoveAndHit, Projectile.onHit
func TestAPiercingArrowPostsALandingForEachBody(t *testing.T) {
	t.Parallel()
	srv, _ := sensorScene(t)
	for _, x := range []float64{2.5, 4.5} {
		id := srv.SpawnMob("minecraft:cow", x, 64, 4.5)
		srv.SetMobNoAI(id)
	}
	events := observeEvents(srv)
	srv.ShootPiercingArrow(0.5, 65, 4.5, 6, 0, 0, 1, true)
	servertest.RunTicks(t, srv, 1)
	if got := events.of("minecraft:projectile_land"); len(got) != 2 {
		t.Errorf("piercing landings = %v, want one per body", got)
	}
}

// TestAPlayersHurtAndDeathAreVibrations: entity_damage is 7, entity_die 15.
// vanilla: LivingEntity.hurtServer, LivingEntity.die
func TestAPlayersHurtAndDeathAreVibrations(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.SurvivalTestConfig(), "Lander")
	srv.SetBlock(4, 64, 4, servertest.MustState(t, "minecraft:sculk_sensor", nil))
	servertest.HoldTicks(t, srv)

	srv.RunLine("damage Lander 1")
	servertest.RunTicks(t, srv, 8)
	if got := sensorFrequency(t, srv); got != 7 {
		t.Errorf("the hurt gave frequency %d, want 7", got)
	}
	servertest.RunTicks(t, srv, 40)
	srv.RunLine("kill Lander")
	servertest.RunTicks(t, srv, 8)
	if got := sensorFrequency(t, srv); got != 15 {
		t.Errorf("the death gave frequency %d, want 15", got)
	}
}

// TestALethalHitIsAVibration: a killing blow still posts entity_damage (7)
// before entity_die (15); the sensor keeps the higher frequency of the two.
// vanilla: LivingEntity.actuallyHurt, LivingEntity.die
func TestALethalHitIsAVibration(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.SurvivalTestConfig(), "Lander")
	srv.SetBlock(4, 64, 4, servertest.MustState(t, "minecraft:sculk_sensor", nil))
	servertest.HoldTicks(t, srv)

	srv.RunLine("damage Lander 1000")
	servertest.RunTicks(t, srv, 8)
	if got := sensorFrequency(t, srv); got != 15 {
		t.Errorf("the lethal hit gave frequency %d, want 15", got)
	}
}

// TestAMobsDeathIsAVibration: a mob's death is entity_die (15) for the
// sensors in range.
// vanilla: LivingEntity.die
func TestAMobsDeathIsAVibration(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Lander", servertest.OpsListing(t, "Lander"))
	srv.SetBlock(4, 64, 4, servertest.MustState(t, "minecraft:sculk_sensor", nil))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("summon minecraft:cow 1.5 64 1.5 {NoAI:1b,Silent:1b}")
	id := c.ExpectSummonedMob(protocol.EntityTypeCow)
	if !srv.KillMob(id) {
		t.Fatal("no live mob to kill")
	}
	servertest.RunTicks(t, srv, 8)
	if got := sensorFrequency(t, srv); got != 15 {
		t.Errorf("the death gave frequency %d, want 15", got)
	}
}

// TestWalkingActivatesASculkSensor: a step event is posted each time the walk
// distance (0.6 per block) passes the next whole number, so the second block
// walked gives one, frequency 1 and power 12 at two blocks.
// vanilla: Entity.applyMovementEmissionAndPlaySound
func TestWalkingActivatesASculkSensor(t *testing.T) {
	t.Parallel()
	srv, c := sensorScene(t)
	c.MoveToGround(1.5, 64, 4.5, true)
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	expectSensor(t, srv, 4, 64, 4, "inactive", "0")
	c.MoveToGround(2.5, 64, 4.5, true)
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	expectSensor(t, srv, 4, 64, 4, "active", "12")
}

// TestAJumpInTheAirMakesNoStep: the walk distance grows in the air but a step
// needs the ground, so the move through the air is silent.
// vanilla: Entity.vibrationAndSoundEffectsFromBlock
func TestAJumpInTheAirMakesNoStep(t *testing.T) {
	t.Parallel()
	srv, c := sensorScene(t)
	c.MoveToGround(1.5, 64, 4.5, false)
	c.MoveToGround(2.5, 64, 4.5, false)
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	expectSensor(t, srv, 4, 64, 4, "inactive", "0")
}

// TestLandingAfterAFlightMakesNoStep: a flying body emits nothing and adds no
// walk distance, so touching down after a long flight is silent.
// vanilla: Player.getMovementEmission
func TestLandingAfterAFlightMakesNoStep(t *testing.T) {
	t.Parallel()
	srv, c := sensorScene(t)
	c.ChatCommand("gamemode creative")
	c.Settle()
	c.Send(protocol.IDPlayPlayerAbilitiesIn, func(b *protocol.Buffer) {
		b.PutI8(protocol.AbilityFlying)
	})
	c.MoveToGround(1.5, 64, 4.5, false)
	c.MoveToGround(2.5, 64, 4.5, false)
	c.MoveToGround(3.5, 64, 4.5, false)
	c.Send(protocol.IDPlayPlayerAbilitiesIn, func(b *protocol.Buffer) { b.PutI8(0) })
	c.Settle()
	c.MoveToGround(3.6, 64, 4.5, true)
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	expectSensor(t, srv, 4, 64, 4, "inactive", "0")
}

// TestStandingUpAfterSneakingMakesNoStep: a body sneaking on the ground adds
// no walk distance, so one block sneaked and one walked is 0.6, not the 1.2
// that makes a step.
// vanilla: Player.getMovementEmission
func TestStandingUpAfterSneakingMakesNoStep(t *testing.T) {
	t.Parallel()
	srv, c := sensorScene(t)
	c.SendSneak(true)
	c.Settle()
	c.MoveToGround(1.5, 64, 4.5, true)
	c.Settle()
	c.SendSneak(false)
	c.Settle()
	c.MoveToGround(2.5, 64, 4.5, true)
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	expectSensor(t, srv, 4, 64, 4, "inactive", "0")
}

// TestClimbingALadderMakesAStep: a body on a climbable adds walk distance
// without the ground, so two blocks climbed past ladders make a step.
// vanilla: Entity.vibrationAndSoundEffectsFromBlock, Entity.isStateClimbable
func TestClimbingALadderMakesAStep(t *testing.T) {
	t.Parallel()
	srv, c := sensorScene(t)
	ladder := servertest.MustState(t, "minecraft:ladder", nil)
	srv.SetBlock(1, 63, 4, ladder)
	srv.SetBlock(2, 63, 4, ladder)
	c.Settle()
	c.MoveToGround(1.5, 64, 4.5, false)
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	expectSensor(t, srv, 4, 64, 4, "inactive", "0")
	c.MoveToGround(2.5, 64, 4.5, false)
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	expectSensor(t, srv, 4, 64, 4, "active", "12")
}

// TestLandingAfterASwimMakesNoStep: a body in water posts a swim (frequency
// 1) and moves the next step on as the distance passes it, so touching down
// after a long swim is silent.
// vanilla: Entity.applyMovementEmissionAndPlaySound
func TestLandingAfterASwimMakesNoStep(t *testing.T) {
	t.Parallel()
	srv, c := sensorScene(t)
	water := servertest.MustState(t, "minecraft:water", nil)
	srv.SetBlock(1, 64, 4, water)
	srv.SetBlock(2, 64, 4, water)
	srv.SetBlock(3, 64, 4, water)
	c.Settle()
	c.MoveToGround(1.5, 64, 4.5, false)
	c.MoveToGround(2.5, 64, 4.5, false)
	c.MoveToGround(3.5, 64, 4.5, false)
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	expectSensor(t, srv, 4, 64, 4, "active", "14")
	if got := sensorFrequency(t, srv); got != 1 {
		t.Errorf("the swim gave frequency %d, want 1", got)
	}

	servertest.RunTicks(t, srv, 40)
	c.MoveToGround(3.6, 64, 4.5, true)
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	expectSensor(t, srv, 4, 64, 4, "inactive", "0")
}

// TestAGlidingPlayerIsHeardEveryTenTicks: an open elytra posts elytra_glide
// (frequency 4) on glide ticks 10, 20, ... so a sensor in range goes active.
// vanilla: LivingEntity.updateFallFlying
func TestAGlidingPlayerIsHeardEveryTenTicks(t *testing.T) {
	t.Parallel()
	srv, c := sensorScene(t)
	// Worn from the start: equipping it would post an equip event of its own.
	srv.GiveSlot("Actor", 6, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:elytra"), Count: 1})
	c.MoveToGround(4.5, 66, 4.5, false)
	c.Settle()
	servertest.StartFallFlying(c)
	c.Settle()
	servertest.RunTicks(t, srv, 12)
	expectSensor(t, srv, 4, 64, 4, "active", "12")
	if got := sensorFrequency(t, srv); got != 4 {
		t.Errorf("the glide gave frequency %d, want 4", got)
	}
}

// TestALeverFlipActivatesASculkSensor: switching a lever on is
// block_activate (frequency 10), the flip back block_deactivate (9).
// vanilla: LeverBlock.pull
func TestALeverFlipActivatesASculkSensor(t *testing.T) {
	t.Parallel()
	srv, c := comparatorSensorScene(t)
	srv.SetBlock(2, 64, 4, servertest.MustState(t, "minecraft:lever", map[string]string{"face": "floor"}))
	c.MoveToGround(0.5, 64, 4.5, true)
	c.Settle()
	c.UseBlock(2, 64, 4)
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	expectSensor(t, srv, 4, 64, 4, "active", "12")
	expectComparator(t, srv, "10")

	servertest.RunTicks(t, srv, 40)
	c.UseBlock(2, 64, 4)
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	expectComparator(t, srv, "9")
}

// TestANoteBlockPlayedByAPlayerActivatesASculkSensor: note_block_play is
// frequency 10.
// vanilla: NoteBlock.playNote
func TestANoteBlockPlayedByAPlayerActivatesASculkSensor(t *testing.T) {
	t.Parallel()
	srv, c := comparatorSensorScene(t)
	srv.SetBlock(2, 64, 4, servertest.MustState(t, "minecraft:note_block", nil))
	c.MoveToGround(0.5, 64, 4.5, true)
	c.Settle()
	c.UseBlock(2, 64, 4)
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	expectSensor(t, srv, 4, 64, 4, "active", "12")
	expectComparator(t, srv, "10")
}

// TestAButtonPressActivatesASculkSensor: pressing is block_activate
// (frequency 10).
// vanilla: ButtonBlock.press
func TestAButtonPressActivatesASculkSensor(t *testing.T) {
	t.Parallel()
	srv, c := comparatorSensorScene(t)
	srv.SetBlock(2, 64, 4, servertest.MustState(t, "minecraft:stone_button", map[string]string{"face": "floor"}))
	c.MoveToGround(0.5, 64, 4.5, true)
	c.Settle()
	c.UseBlock(2, 64, 4)
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	expectSensor(t, srv, 4, 64, 4, "active", "12")
	expectComparator(t, srv, "10")
}

// TestAnEmptyDispenserActivatesASculkSensor: a dispenser with nothing to
// dispense posts block_activate (frequency 10).
// vanilla: DispenserBlock.dispenseFrom
func TestAnEmptyDispenserActivatesASculkSensor(t *testing.T) {
	t.Parallel()
	srv, c := comparatorSensorScene(t)
	servertest.Setblock(t, c, 2, 64, 4, "minecraft:dispenser[facing=up]")
	servertest.Setblock(t, c, 1, 64, 4, "minecraft:redstone_block")
	servertest.RunTicks(t, srv, 8)
	expectSensor(t, srv, 4, 64, 4, "active", "12")
	expectComparator(t, srv, "10")
}
