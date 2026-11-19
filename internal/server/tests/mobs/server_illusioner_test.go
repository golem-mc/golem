package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

const illusionerKind = "minecraft:illusioner"

// TestAnIllusionerHoldsABowAndShootsTheNearbyPlayer: 32 health, a bow in the
// main hand, and an arrow within 200 ticks of a survival player ten blocks
// away.
// vanilla: Illusioner.finalizeSpawn, Illusioner.performRangedAttack
func TestAnIllusionerHoldsABowAndShootsTheNearbyPlayer(t *testing.T) {
	t.Parallel()
	srv, _ := evokerWorld(t)
	id := srv.SpawnMob(illusionerKind, 10.5, 64, 0.5)
	if hp, _ := srv.MobHealth(id); hp != 32 {
		t.Errorf("illusioner health = %v, want 32", hp)
	}
	bow, _ := registry.ItemID("minecraft:bow")
	if got := srv.MobHand(id); got.ItemID != bow {
		t.Errorf("illusioner main hand = item %d, want the bow %d", got.ItemID, bow)
	}
	servertest.UntilTicks(t, srv, 200, "an arrow from the illusioner", func() bool { return srv.ArrowCount() > 0 })
}

// TestAnIllusionerTurnsInvisibleWithTheMirrorSpell: the first spell it has a
// target for is the mirror, which gives it Invisibility for 1200 ticks.
// vanilla: Illusioner.IllusionerMirrorSpellGoal
func TestAnIllusionerTurnsInvisibleWithTheMirrorSpell(t *testing.T) {
	t.Parallel()
	srv, _ := evokerWorld(t)
	id := srv.SpawnMob(illusionerKind, 10.5, 64, 0.5)
	var spell uint8
	servertest.UntilTicks(t, srv, 200, "the illusioner's invisibility", func() bool {
		if s := srv.MobSpell(id); s != 0 {
			spell = s
		}
		_, _, ok := srv.MobEffectOf(id, "minecraft:invisibility")
		return ok
	})
	if spell != entity.SpellDisappear {
		t.Errorf("spellcasting byte seen = %d, want %d", spell, entity.SpellDisappear)
	}
	if d, _, _ := srv.MobEffectOf(id, "minecraft:invisibility"); d < 1100 || d > 1200 {
		t.Errorf("invisibility duration = %d, want about 1200", d)
	}
}

// TestAnIllusionerBlindsItsTargetOnHard: on hard the effective difficulty
// passes normal, so the spell lands 400 ticks of Blindness on a target it has
// not blinded before.
// vanilla: Illusioner.IllusionerBlindnessSpellGoal
func TestAnIllusionerBlindsItsTargetOnHard(t *testing.T) {
	t.Parallel()
	srv, _ := evokerWorld(t)
	srv.SpawnMob(illusionerKind, 10.5, 64, 0.5)
	servertest.UntilTicks(t, srv, 400, "blindness on the player", func() bool {
		_, _, ok := srv.PlayerEffect("Target", "minecraft:blindness")
		return ok
	})
}

// TestAnIllusionerDrawingItsBowSendsItsHandState summons an illusioner with a
// bow in one hand ten blocks from a survival player: once its spells are cast
// it draws, and the client is told the use and, for the off hand, its bit.
// vanilla: RangedBowAttackGoal.tick, ProjectileUtil.getWeaponHoldingHand,
// LivingEntity.startUsingItem
func TestAnIllusionerDrawingItsBowSendsItsHandState(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		slot string
		want uint8
	}{
		{"mainhand", protocol.EntityFlagHandActive},
		{"offhand", protocol.EntityFlagHandActive | protocol.EntityFlagActiveHandOff},
	} {
		t.Run(tc.slot, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Target"))
			srv.SetDifficulty(rules.DifficultyHard)
			srv.SetDayTime(18000)
			servertest.FreezeUntilHeld(t, srv)
			c := connect()
			c.JoinAs("Target")
			c.MoveToGround(0.5, 64, 0.5, true)
			c.Settle()

			pos := srv.PlayerPosition("Target")
			c.ChatCommand(fmt.Sprintf(`summon minecraft:illusioner %.2f %.2f %.2f {equipment:{%s:{id:"minecraft:bow",count:1}}}`,
				pos.X+10, pos.Y, pos.Z, tc.slot))
			illusioner := c.ExpectMobSpawn(protocol.EntityTypeIllusioner)
			servertest.HoldTicks(t, srv)

			// The mirror and blindness spells (about 40 ticks each) come
			// before the bow goal's first draw.
			e, ok := c.MetaEntryWithinTicks(srv, 300, illusioner.EntityID, protocol.MetadataIndexHandState)
			if !ok {
				t.Fatal("the illusioner drew no bow within 300 ticks")
			}
			if e.ByteVal != tc.want {
				t.Errorf("hand state = %#02x, want %#02x", e.ByteVal, tc.want)
			}
		})
	}
}
