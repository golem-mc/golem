package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

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
