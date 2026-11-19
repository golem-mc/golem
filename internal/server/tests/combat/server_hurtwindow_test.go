package combat

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The invulnerability window a hurt opens in a mob, vanilla's
// invulnerableTime: a blow inside it that tops the last hurt lands only
// its excess above it — the health alone, told by the tick's own metadata
// pass, none of a whole hurt's damage event, flinch, tilt, shove or hurt
// sound — while the attacker's own feedback stands behind a blow that
// lands at all. Outside the window a whole blow's packets all arrive. The
// attack-strength cooldown rides both blows the vanilla way: it scales a
// hurried blow's damage, not its knockback.
// vanilla: LivingEntity.hurtServer, Player.attack

// hurrySword hands the player a sword and runs the one stopped tick that
// sees it come to the hand, leaving the swing spent to nought: the next
// blow reads the strength scale at the half tick of a fresh swing.
func hurrySword(t *testing.T, srv *server.Server, name string) protocol.ItemStack {
	t.Helper()
	sword := protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:diamond_sword"), Count: 1,
	}
	srv.GiveSlot(name, player.HotbarSlot, sword)
	srv.TickSurvival()
	return sword
}

// hurriedBlow is the damage a blow of the given stack carries where its
// swing needs the given recovery, with no critical in it.
func hurriedBlow(held protocol.ItemStack, strength float64) float64 {
	return rules.AttackDamage(servertest.FistDamageTestBase+float64(registry.AttackDamage(held.ItemID)), false, strength)
}

// TestABlowInsideTheWindowLandsOnlyItsExcess checks the half-second after
// a landed hurt: the fist's whole blow opens the window with its one, a
// hurried sword blow inside it tops that and lands only its excess — its
// health alone reaching anyone, none of a whole hurt's packets telling of
// it — and the health that does arrive is the amount above the last hurt.
func TestABlowInsideTheWindowLandsOnlyItsExcess(t *testing.T) {
	t.Parallel()
	srv, c, attackerID := servertest.JoinAs(t, servertest.CombatTestConfig(), "Sash")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	// The whole fist's blow the window opens with.
	servertest.AwaitFullSwing(t, srv, "Sash")
	mobID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)

	srv.StopTick()
	if got := c.Strike(mobID, attackerID); math.Abs(float64(got)-9) > 0.01 {
		c.T.Fatalf("the fist's whole left blow %v health, want the nine", got)
	}

	// The hurried sword inside the window: a blow of strength the
	// half-tick's scale gives, larger than the fist's one.
	sword := hurrySword(t, srv, "Sash")
	strength := rules.AttackStrengthScale(0, rules.AttackSpeed(sword))
	swordHit := hurriedBlow(sword, strength)
	c.Attack(mobID)
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayDamageEvent, protocol.IDPlayHurtAnimation,
			protocol.IDPlayEntityEvent, protocol.IDPlaySetEntityMotion,
			protocol.IDPlayEntityMetadata:
			c.T.Fatalf("a blow inside the window was told of like a whole one, packet 0x%02X (% X)", id, data)
		}
	})

	// The health the excess left goes out on the tick's own metadata
	// pass, alone.
	srv.TickMobs(1)
	want := float32(9 - (swordHit - 1))
	if got := servertest.ExpectMobHealth(c, mobID); math.Abs(float64(got-want)) > 0.01 {
		c.T.Errorf("the sword's %v over the last hurt's one left %v health, want the excess's %v",
			swordHit, got, want)
	}
}

// TestAHurriedBlowOutsideTheWindowShovesWhole checks the cooldown's
// outside: a hurried blow on a body past the window lands whole, its
// damage scaled by the swing's recovery but its shove not — a hurried
// blow lifts a grounded mob to the same four tenths a recovered one
// would.
// vanilla: Player.attack
func TestAHurriedBlowOutsideTheWindowShovesWhole(t *testing.T) {
	t.Parallel()
	srv, c, attackerID := servertest.JoinAs(t, servertest.CombatTestConfig(), "Hasty")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	mobID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)

	srv.StopTick()
	sword := hurrySword(t, srv, "Hasty")
	strength := rules.AttackStrengthScale(0, rules.AttackSpeed(sword))
	swordHit := hurriedBlow(sword, strength)

	c.Attack(mobID)
	c.ExpectMobDamageEvent(mobID, "minecraft:player_attack", attackerID+1)
	hurtAnim := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlayHurtAnimation))
	if got := hurtAnim.ReadVarInt(); got != mobID {
		c.T.Fatalf("hurt animation for entity %d, want %d", got, mobID)
	}
	hurtAnim.ReadF32()
	motion := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlaySetEntityMotion))
	if got := motion.ReadVarInt(); got != mobID {
		c.T.Fatalf("shove for entity %d, want %d", got, mobID)
	}
	vx, vy, vz := motion.ReadLpVec3()
	if err := motion.Err(); err != nil {
		c.T.Fatalf("set entity velocity: %v", err)
	}
	if vx <= 0 || vz != 0 {
		c.T.Errorf("knockback = (%v %v %v), wanted east and no south (the cow stands east)", vx, vy, vz)
	}
	if math.Abs(vy-0.4) > 0.1 {
		c.T.Errorf("the hurried blow lifted the cow to %v, want the whole blow's vanilla 0.4 — "+
			"the cooldown scales a hurried blow's damage, not its knockback", vy)
	}
	if got := servertest.ExpectMobHealth(c, mobID); math.Abs(float64(got)-(10-swordHit)) > 0.01 {
		c.T.Errorf("the hurried blow left %v health, want the scaled %v", got, 10-swordHit)
	}
}

// TestAReducedArrowTellsOnlyItsHealth checks the telling of an arrow's blow
// that lands inside a mob's window: the excess alone reaches the health,
// and none of a whole hurt's damage event, flinch, tilt, shove or hurt
// sound rides it — the same gate a melee blow is behind.
func TestAReducedArrowTellsOnlyItsHealth(t *testing.T) {
	t.Parallel()
	srv, c, attackerID := servertest.JoinAs(t, servertest.CombatTestConfig(), "Archer")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	// The fist's whole blow opens the window with its one.
	servertest.AwaitFullSwing(t, srv, "Archer")
	mobID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.StopTick()
	if got := c.Strike(mobID, attackerID); math.Abs(float64(got)-9) > 0.01 {
		c.T.Fatalf("the fist's whole left blow %v health, want the nine", got)
	}

	// An arrow of four lands inside the window: only its excess over the
	// one, told by the health alone.
	srv.ShootArrow(8.5, 64.5, 5.5, -2, 0, 0)
	srv.TickEntities(4)

	var health float32
	seen := false
	c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayEntityEvent:
			if protocol.NewReader(data).ReadI32() == mobID {
				c.T.Fatalf("a reduced arrow blow was told of like a whole one, packet 0x%02X", id)
			}
		case protocol.IDPlayDamageEvent, protocol.IDPlayHurtAnimation, protocol.IDPlaySetEntityMotion:
			if protocol.NewReader(data).ReadVarInt() == mobID {
				c.T.Fatalf("a reduced arrow blow was told of like a whole one, packet 0x%02X", id)
			}
		case protocol.IDPlayEntityMetadata:
			if protocol.NewReader(data).ReadVarInt() != mobID {
				return false
			}
			var m servertest.SpawnedMob
			m.EntityID = mobID
			c.ReadMobMetadataInto(&m, data)
			if !seen {
				health, seen = m.Health, true
			}
		}
		return false
	})
	if !seen {
		t.Fatal("the reduced arrow's health never reached the watching client")
	}
	if math.Abs(float64(health)-6) > 0.01 {
		t.Errorf("the reduced arrow left the cow at %v, want the excess's six", health)
	}
}

// TestAReducedFallIsToldOnlyByItsHealth checks a landing that lands inside
// a mob's window: the excess alone reaches the health and the damage event
// and hurt sound of a whole fall stay back — the gate the fall's own hurt
// passes through — while the fall-damage sound and the block-fall sound
// still play ungated.
// vanilla: LivingEntity.causeFallDamage, hurtServer
func TestAReducedFallIsToldOnlyByItsHealth(t *testing.T) {
	t.Parallel()
	srv, c, attackerID := servertest.JoinAs(t, servertest.CombatTestConfig(), "Landing")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	servertest.AwaitFullSwing(t, srv, "Landing")

	// The loop stopped before the drop, so the fall is the hand-driven
	// ticks' alone to run.
	srv.StopTick()
	cowID := srv.SpawnMob("minecraft:cow", 2.5, 74, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)

	// Fall until the cow is within the player's reach, then strike it in
	// mid-air to open the window with the fist's one.
	for {
		srv.TickMobs(1)
		if _, y, _, ok := srv.MobPos(cowID); !ok || y <= 66.5 {
			break
		}
	}
	if got := c.Strike(cowID, attackerID); math.Abs(float64(got)-9) > 0.01 {
		t.Fatalf("the mid-air fist left %v health, want the nine", got)
	}
	c.Settle() // the melee's own hurt sound, before the fall's telling

	damages, hurtAnims, hurtSounds, fallSounds := 0, 0, 0, 0
	health := float32(-1)
	for range 8 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayDamageEvent:
				if protocol.NewReader(data).ReadVarInt() == cowID {
					damages++
				}
			case protocol.IDPlayHurtAnimation:
				if protocol.NewReader(data).ReadVarInt() == cowID {
					hurtAnims++
				}
			case protocol.IDPlaySoundEffect:
				switch servertest.SoundOf(data) {
				case "minecraft:entity.generic.big_fall", "minecraft:entity.generic.small_fall":
					fallSounds++
				case "minecraft:entity.cow.hurt":
					hurtSounds++
				}
			case protocol.IDPlayEntityMetadata:
				if protocol.NewReader(data).ReadVarInt() != cowID {
					return
				}
				if m := servertest.ReadZombieMetadata(c, data); m.HasHealth {
					health = m.Health
				}
			}
		})
	}
	if damages != 0 {
		t.Errorf("the reduced fall told %d damage events, want none", damages)
	}
	if hurtAnims != 0 {
		t.Errorf("the reduced fall sent %d hurt animations, want none", hurtAnims)
	}
	if hurtSounds != 0 {
		t.Errorf("the reduced fall played %d hurt sounds, want none", hurtSounds)
	}
	if fallSounds != 1 {
		t.Errorf("the reduced fall played %d fall sounds, want the one ungated big_fall", fallSounds)
	}
	// The melee's one and the fall's excess over it: the fall's seven less
	// the one already behind the window, off the cow's ten.
	if math.Abs(float64(health)-3) > 0.01 {
		t.Errorf("the reduced fall left %v health, want the %v", health, 3.0)
	}
}

// mobMetadataHealth reads the health out of a mob metadata packet, past the
// air field a drowning mob carries beside it, and reports whether the packet
// was about the entity named.
func mobMetadataHealth(c *servertest.Client, data []byte, entityID int32) (float32, bool) {
	c.T.Helper()
	r := protocol.NewReader(data)
	if r.ReadVarInt() != entityID {
		return 0, false
	}
	health := float32(-1)
	has := false
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		switch fieldType := r.ReadVarInt(); fieldType {
		case protocol.MetadataByte, protocol.MetadataBoolean:
			r.ReadU8()
		case protocol.MetadataVarInt, protocol.MetadataPose, protocol.MetadataHumanoidArm:
			r.ReadVarInt()
		case protocol.MetadataFloat:
			if index == protocol.MetadataIndexLivingHealth {
				health, has = r.ReadF32(), true
			} else {
				r.ReadF32()
			}
		default:
			c.T.Fatalf("mob metadata field %d of type %d", index, fieldType)
		}
	}
	return health, has
}

// TestAReducedDrownIsToldOnlyByItsBubble checks a drown bite that lands
// inside a mob's window: the excess alone reaches the health and neither the
// damage event nor the hurt sound of a whole bite goes out, while the bubble
// burst still does — the one telling not behind the full-hurt gate, as it is
// not for a player.
func TestAReducedDrownIsToldOnlyByItsBubble(t *testing.T) {
	t.Parallel()
	srv, c, attackerID := servertest.JoinAs(t, servertest.MobTestConfig(), "Diver", servertest.OpsListing(t, "Diver"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	servertest.AwaitFullSwing(t, srv, "Diver")

	srv.StopTick()
	// The stopped loop streams no chunks, so the pool's own chunk is held
	// for the mob's tick to run.
	release := srv.HoldChunk(5, 5)
	t.Cleanup(release)
	water := servertest.BlockState(t, "minecraft:water")
	srv.SetBlock(5, 64, 5, water)
	srv.SetBlock(5, 65, 5, water)

	c.ChatCommand("summon minecraft:cow 5.5 64 5.5 {NoAI:1b}")
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow).EntityID
	c.Settle()

	// The fist's whole blow opens the cow's window with its one.
	if got := c.Strike(cow, attackerID); math.Abs(float64(got)-9) > 0.01 {
		t.Fatalf("the fist's whole left the cow at %v health, want the nine", got)
	}
	c.Settle()

	// The breath reaches its floor the next tick, inside the window.
	srv.SetMobAir(cow, rules.AirDrownFloor+1)
	srv.TickMobs(1)

	damages, bubbles, hurtSounds := 0, 0, 0
	health := float32(-1)
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayDamageEvent:
			if protocol.NewReader(data).ReadVarInt() == cow {
				damages++
			}
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			if r.ReadI32() == cow && r.ReadU8() == protocol.EntityEventDrown {
				bubbles++
			}
		case protocol.IDPlaySoundEffect:
			if servertest.SoundOf(data) == "minecraft:entity.cow.hurt" {
				hurtSounds++
			}
		case protocol.IDPlayEntityMetadata:
			if m, ok := mobMetadataHealth(c, data, cow); ok {
				health = m
			}
		}
	})
	if damages != 0 {
		t.Errorf("the reduced drown told %d damage events, want none", damages)
	}
	if bubbles != 1 {
		t.Errorf("the reduced drown sent %d bubble bursts, want the one", bubbles)
	}
	if hurtSounds != 0 {
		t.Errorf("the reduced drown played %d hurt sounds, want none", hurtSounds)
	}
	// The fist's one and the drown's excess over it: the two less the one
	// already behind the window, off the cow's ten.
	if math.Abs(float64(health)-8) > 0.01 {
		t.Errorf("the reduced drown left %v health, want the excess's eight", health)
	}
}

// TestAReducedAnvilsBlowIsToldOnlyByItsHealth checks a gravity block's blow
// that lands inside a mob's window: the excess alone reaches the health and
// none of the damage event, the flinch or the shove of a whole blow goes
// out. The fall and the landing run by hand, and the stopped mob pass keeps
// the window open until it lands.
func TestAReducedAnvilsBlowIsToldOnlyByItsHealth(t *testing.T) {
	t.Parallel()
	srv, c, attackerID := servertest.JoinAs(t, servertest.MobTestConfig(), "Bruiser", servertest.OpsListing(t, "Bruiser"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	servertest.AwaitFullSwing(t, srv, "Bruiser")

	c.ChatCommand("summon minecraft:cow 5.5 64 5.5 {NoAI:1b}")
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow).EntityID
	c.Settle()

	srv.StopTick()
	// The stopped loop streams no chunks, so the anvil's chunk is held for
	// the physics and the falling pass to find the block in memory.
	release := srv.HoldChunk(5, 5)
	t.Cleanup(release)

	// The fist's whole blow opens the cow's window with its one.
	if got := c.Strike(cow, attackerID); math.Abs(float64(got)-9) > 0.01 {
		t.Fatalf("the fist's whole left the cow at %v health, want the nine", got)
	}
	c.Settle()
	// The fist's knockback plays on in the tick after it; flush that motion
	// so only the anvil's own traffic is counted below.
	srv.TickMobs(1)
	c.Settle()

	// An anvil hangs three blocks over the cow's cell. The physics pass
	// stands the falling body up and the falling pass runs it down.
	c.ChatCommand("setblock 5 67 5 minecraft:anvil")
	c.Settle()
	for range 40 {
		if srv.FallingCount() > 0 {
			break
		}
		srv.TickPhysics(1)
	}
	if srv.FallingCount() != 1 {
		t.Fatal("the anvil never became a falling body")
	}
	for range 40 {
		if srv.FallingCount() == 0 {
			break
		}
		srv.TickFallings(1)
	}
	if srv.FallingCount() != 0 {
		t.Fatal("the anvil never landed")
	}

	srv.TickMobs(1)
	damages, hurtAnims, motions := 0, 0, 0
	health := float32(-1)
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayDamageEvent:
			if protocol.NewReader(data).ReadVarInt() == cow {
				damages++
			}
		case protocol.IDPlayHurtAnimation:
			if protocol.NewReader(data).ReadVarInt() == cow {
				hurtAnims++
			}
		case protocol.IDPlaySetEntityMotion:
			// A shove lifts the body; a cow settling to the floor only falls.
			r := protocol.NewReader(data)
			if r.ReadVarInt() == cow {
				if _, vy, _ := r.ReadLpVec3(); vy > 0 {
					motions++
				}
			}
		case protocol.IDPlayEntityMetadata:
			if protocol.NewReader(data).ReadVarInt() != cow {
				return
			}
			if m := servertest.ReadZombieMetadata(c, data); m.HasHealth {
				health = m.Health
			}
		}
	})
	if damages != 0 {
		t.Errorf("the reduced anvil told %d damage events, want none", damages)
	}
	if hurtAnims != 0 {
		t.Errorf("the reduced anvil sent %d hurt animations, want none", hurtAnims)
	}
	if motions != 0 {
		t.Errorf("the reduced anvil sent %d shoves, want none", motions)
	}
	// The fist's one and the anvil's excess over it: the anvil's six for
	// this fall less the one already behind the window, off the cow's ten.
	if math.Abs(float64(health)-4) > 0.01 {
		t.Errorf("the reduced anvil left %v health, want the excess's four", health)
	}
}

// TestHasteAndMiningFatigueSetTheSwingsPace checks the attack cooldown reads
// the attack_speed attribute with the effects on it: a sword swung just after
// it arrives recovers further under Haste and less under Mining Fatigue than
// the bare 1.6.
// vanilla: Player.getCurrentItemAttackStrengthDelay, MobEffects.HASTE, MINING_FATIGUE
func TestHasteAndMiningFatigueSetTheSwingsPace(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, effect string
		amp          int32
		speed        float64
	}{
		{"Quick", "minecraft:haste", 49, (4 + float64(registry.AttackSpeed(servertest.MustItemID(t, "minecraft:diamond_sword")))) * (1 + float64(float32(0.1))*50)},
		{"Tired", "minecraft:mining_fatigue", 2, (4 + float64(registry.AttackSpeed(servertest.MustItemID(t, "minecraft:diamond_sword")))) * (1 - float64(float32(0.1))*3)},
	} {
		t.Run(tc.effect, func(t *testing.T) {
			t.Parallel()
			srv, c, attackerID := servertest.JoinAs(t, servertest.CombatTestConfig(), tc.name)
			c.MoveToGround(2.5, 64, 5.5, true)
			c.Settle()
			mobID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
			c.ExpectMobSpawn(protocol.EntityTypeCow)

			srv.StopTick()
			srv.GiveEffect(tc.name, tc.effect, 6000, tc.amp, true)
			c.AwaitMobEffect()
			sword := hurrySword(t, srv, tc.name)
			srv.TickSurvival()
			ticks := srv.AttackTicks(tc.name)
			strength := rules.AttackStrengthScale(int(ticks), tc.speed)
			want := 10 - float32(hurriedBlow(sword, strength))
			if got := c.Strike(mobID, attackerID); math.Abs(float64(got-want)) > 0.01 {
				t.Errorf("health after the %s-paced blow = %v, want %v (strength %v after %d ticks)",
					tc.effect, got, want, strength, ticks)
			}
		})
	}
}

// helmetPlayer joins a survival player in an iron helmet.
func helmetPlayer(t *testing.T, name string) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), name)
	srv.GiveSlot(name, player.ArmorSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:iron_helmet"), Count: 1})
	c.Settle()
	srv.StopTick()
	return srv, c
}

// TestAHelmetBlowRefusedInsideTheWindowStillWearsTheHelmet checks that the
// helmet wears before the window answers: a falling anvil's nine beats a
// last hurt of seven and a half only unsoftened.
// vanilla: LivingEntity.hurtServer
func TestAHelmetBlowRefusedInsideTheWindowStillWearsTheHelmet(t *testing.T) {
	t.Parallel()
	srv, _ := helmetPlayer(t, "Capped")
	srv.OpenHurtWindow("Capped", 7.5)
	if srv.HurtAs("Capped", 9, rules.DamageTypeFallingAnvil) {
		t.Fatal("a blow softened to 6.75 landed inside a window of 7.5")
	}
	if got := srv.InventorySlot("Capped", player.ArmorSlot); got.Damage == 0 {
		t.Error("the refused blow wore no helmet")
	}
}

// TestAHelmetSoftenedBlowIsWhatTheWindowKeeps checks that the window
// remembers the softened amount: after a falling anvil's ten the last hurt
// is 7.5, so a mob's eight tops it.
// vanilla: LivingEntity.hurtServer
func TestAHelmetSoftenedBlowIsWhatTheWindowKeeps(t *testing.T) {
	t.Parallel()
	srv, _ := helmetPlayer(t, "Capped")
	if !srv.HurtAs("Capped", 10, rules.DamageTypeFallingAnvil) {
		t.Fatal("the anvil's blow did not land")
	}
	if !srv.Hurt("Capped", 8) {
		t.Error("a blow of 8 was refused inside a window whose last hurt is 7.5")
	}
}

// TestARefusedBlowInsideTheWindowWakesASleeper checks that a hurt the window
// turns away still stands the sleeper up.
// vanilla: LivingEntity.hurtServer
func TestARefusedBlowInsideTheWindowWakesASleeper(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Actor", servertest.NoTimeSync)
	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)
	srv.OpenHurtWindow("Actor", 5)
	if srv.Hurt("Actor", 1) {
		t.Fatal("a blow of 1 landed inside a window of 5")
	}
	if srv.Sleeping("Actor") {
		t.Error("the refused blow left the player asleep")
	}
}
