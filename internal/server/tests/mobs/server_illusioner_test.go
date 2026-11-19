package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
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
