package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAZombieIgnoresABreezesWindChargeButNotItsArrow strikes a zombie with a
// NoAI breeze's wind charge, which a no_anger_from_wind_charge kind does not
// answer, and, as a control, with the breeze's arrow, which it does. The
// player is in creative, so the zombie hunts nobody else.
// vanilla: LivingEntity.resolveMobResponsibleForDamage
func TestAZombieIgnoresABreezesWindChargeButNotItsArrow(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.ItemCreativeConfig(), "Calm")
	breeze := srv.SpawnMob("minecraft:breeze", 0.5, 64, 6.5)
	if breeze == 0 || !srv.SetMobNoAI(breeze) {
		t.Fatal("could not stand up a NoAI breeze")
	}

	t.Run("wind charge", func(t *testing.T) {
		zombie := srv.SpawnMob("minecraft:zombie", 3.5, 64, 6.5)
		if zombie == 0 {
			t.Fatal("could not spawn the zombie")
		}
		if srv.ThrowThrown(entity.ThrownBreezeWindCharge, 1.5, 65, 6.5, -90, 0, breeze, 1) == 0 {
			t.Fatal("could not launch the wind charge")
		}
		servertest.RunTicks(t, srv, 20)
		if hp, _ := srv.MobHealth(zombie); hp >= 20 {
			t.Fatalf("the zombie's health = %v, want the wind charge's hurt taken", hp)
		}
		if got := srv.MobTargetID(zombie); got == breeze {
			t.Errorf("the zombie targets the breeze (%d) after its wind charge, want it ignored", got)
		}
	})
	t.Run("arrow", func(t *testing.T) {
		zombie := srv.SpawnMob("minecraft:zombie", 3.5, 64, 10.5)
		if zombie == 0 {
			t.Fatal("could not spawn the zombie")
		}
		if srv.ShootMobArrowFrom(breeze, 1.5, 65, 10.5, 1.5, 0, 0) == 0 {
			t.Fatal("could not shoot the arrow")
		}
		servertest.RunTicks(t, srv, 20)
		if got := srv.MobTargetID(zombie); got != breeze {
			t.Errorf("the zombie targets %d after the breeze's arrow, want the breeze (%d)", got, breeze)
		}
	})
}
