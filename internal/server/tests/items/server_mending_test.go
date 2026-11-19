package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
)

// The mending tests: the repair pass of an orb's pickup, read from the wire
// the experience tests read. Vanilla's ExperienceOrb.repairPlayerItems takes
// a random damaged item carrying a repair_with_xp enchantment from the hands
// and the armour a player holds, repairs it two a point the orb was worth,
// and hands what the repair did not spend to the next item and then to the
// bar — the numbers these tests pin. Most stand one candidate up, so the
// random pick has nothing to decide between; the two-candidate test is
// shaped so whichever the pick takes first, its answer is the same.

// TestAnOrbMendsADamagedHeldItem checks the pass on its own numbers: a damaged
// mending pickaxe held at a hundred picks up an orb worth ten and comes away
// at eighty, the two a point the repair rate is, with nothing of the worth
// left for the bar to take.
func TestAnOrbMendsADamagedHeldItem(t *testing.T) {
	t.Parallel()
	cfg, _ := servertest.ExperienceSetup(t)

	srv, c, _ := servertest.JoinAs(t, cfg, "Mender")

	pick := servertest.EnchantedStack(t, "minecraft:diamond_pickaxe", "minecraft:mending", 1)
	pick.Damage = 100
	srv.GiveSlot("Mender", player.HotbarSlot, pick)

	// The orb stands where the player does, so the pickup's sweep takes it
	// the first tick it is in the world.
	w := servertest.NewOrbWatcher(t, c)
	srv.SpawnOrb(0.5, 64, 0.5, 10)
	w.ReadUntilTicks(srv, 240, func() bool {
		_, told := w.Slots[player.HotbarSlot]
		return len(w.Takes) > 0 && told
	})

	if len(w.Takes) != 1 {
		t.Fatalf("the orb was taken %d times, want the one pickup of it", len(w.Takes))
	}
	if got := srv.InventorySlot("Mender", player.HotbarSlot); got.Damage != 80 {
		t.Errorf("the pickaxe's damage after the pickup = %d, want the eighty the ten points repair at two apiece", got.Damage)
	}
	// The mend is drawn from the correction the held slot is sent, not only
	// read off the server's own inventory.
	if got := w.Slots[player.HotbarSlot]; got != 80 {
		t.Errorf("the slot correction carried damage %d, want the eighty the mend leaves", got)
	}
	if len(w.Exps) > 0 {
		report := w.LastExperience()
		t.Errorf("the bar took %d points (level %d, progress %g) of a pickup the pickaxe mended whole",
			report.Total, report.Level, report.Progress)
	}
}
