package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// readOwnMotion reads the first Set Entity Motion for entity within n ticks.
func readOwnMotion(t *testing.T, srv *server.Server, c *servertest.Client, entity int32, n int) (vx, vy, vz float64) {
	t.Helper()
	got := c.ReadTicks(srv, n, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySetEntityMotion {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != entity {
			return false
		}
		vx, vy, vz = r.ReadLpVec3()
		return true
	})
	if !got {
		t.Fatal("the harmed player was told of no shove")
	}
	return vx, vy, vz
}

// TestASplashHarmingPotionShovesTheVictimAwayFromTheShatter splashes a
// Harming beside a player on +x of the shatter: the whole harm shoves them
// along +x. vanilla: LivingEntity.dealDefaultKnockback,
// AbstractThrownPotion.calculateHorizontalHurtKnockbackDirection
func TestASplashHarmingPotionShovesTheVictimAwayFromTheShatter(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, connect := servertest.Start(t, cfg)
	a := connect()
	a.JoinAs("Thrower")
	b := connect()
	bID := b.JoinAs("Victim")
	b.MoveTo(1.0, 64, 0.5, 270, 0)
	a.Settle()
	b.Settle()

	srv.GiveSlot("Thrower", 36, potionStack(t, "minecraft:splash_potion", registry.MustPotion("minecraft:harming"), 1))
	a.MoveTo(0.5, 64, 0.5, -90, 90)
	servertest.UseHeld(a, -90, 90)

	if vx, _, _ := readOwnMotion(t, srv, b, bID, 40); vx <= 0.2 {
		t.Errorf("the splash shoved the victim by vx %.4f, want > 0.2 after travel away from the shatter", vx)
	}
}

// TestDrinkingAHarmingPotionHopsTheDrinker drinks a harming potion at full
// health: the harm's source stands on the drinker, so the 0.4 shove goes in
// a random direction. vanilla: PotionContents.applyToLivingEntity,
// LivingEntity.knockback
func TestDrinkingAHarmingPotionHopsTheDrinker(t *testing.T) {
	t.Parallel()
	srv, c, id := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Drinker")
	c.MoveToGround(0.5, 64, 0.5, true) // airborne, the shove would not lift
	c.Settle()
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 1)
	c.Settle()

	srv.SetFood("Drinker", 6)
	srv.GiveSlot("Drinker", player.HotbarSlot, brewedPotion(t, 26)) // harming
	c.Settle()

	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	for range 40 {
		srv.TickSurvival()
	}

	vx, vy, vz := readOwnMotion(t, srv, c, id, 0)
	if math.Abs(vy-0.3608) > 1e-3 || math.Abs(math.Hypot(vx, vz)-0.4) > 1e-3 {
		t.Errorf("the drink shoved the drinker by (%.4f, %.4f, %.4f), want vy 0.3608 and a horizontal 0.4", vx, vy, vz)
	}
}
