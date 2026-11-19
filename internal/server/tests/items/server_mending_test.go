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

// TestAnOrbWorthMoreThanTheDamageBanksTheRemainder checks the other half of
// the rate: a wear too small to spend the orb's worth ends at nothing worn,
// and the points the repair did not use reach the bar — an orb of ten
// repairing five wears spends the two those five are worth and banks eight.
func TestAnOrbWorthMoreThanTheDamageBanksTheRemainder(t *testing.T) {
	t.Parallel()
	cfg, _ := servertest.ExperienceSetup(t)

	srv, c, _ := servertest.JoinAs(t, cfg, "Banker")

	pick := servertest.EnchantedStack(t, "minecraft:diamond_pickaxe", "minecraft:mending", 1)
	pick.Damage = 5
	srv.GiveSlot("Banker", player.HotbarSlot, pick)

	w := servertest.NewOrbWatcher(t, c)
	srv.SpawnOrb(0.5, 64, 0.5, 10)
	w.ReadUntilTicks(srv, 240, func() bool {
		return len(w.Takes) > 0 && len(w.Exps) > 0
	})

	if got := srv.InventorySlot("Banker", player.HotbarSlot); got.Damage != 0 {
		t.Errorf("the pickaxe's damage after the pickup = %d, want the full mend of it", got.Damage)
	}
	// Eight points from an empty bar cross into the second level: one level,
	// one of the nine it needs, the walk the xp tests pin elsewhere.
	if report := w.LastExperience(); report.Total != 8 || report.Level != 1 {
		t.Errorf("the bar read total=%d level=%d, want the eight points the mend left, one level in",
			report.Total, report.Level)
	}
}

// TestAnOrbMendsDamagedWornArmour checks the sweep the repair runs its pick
// over: the worn armour counts beside the held hands, so a damaged mending
// helmet gets the point's twos with nothing held worth mending.
func TestAnOrbMendsDamagedWornArmour(t *testing.T) {
	t.Parallel()
	cfg, _ := servertest.ExperienceSetup(t)

	srv, c, _ := servertest.JoinAs(t, cfg, "Helm")

	helmet := servertest.EnchantedStack(t, "minecraft:diamond_helmet", "minecraft:mending", 1)
	helmet.Damage = 200
	srv.GiveSlot("Helm", player.ArmorSlot, helmet)

	w := servertest.NewOrbWatcher(t, c)
	srv.SpawnOrb(0.5, 64, 0.5, 10)
	w.ReadUntilTicks(srv, 240, func() bool { return len(w.Takes) > 0 })

	if got := srv.InventorySlot("Helm", player.ArmorSlot); got.Damage != 180 {
		t.Errorf("the helmet's damage after the pickup = %d, want the hundred and eighty the mend leaves", got.Damage)
	}
	if got := srv.InventorySlot("Helm", player.HotbarSlot); !got.Empty() {
		t.Errorf("the held slot holds %v, want it empty", got)
	}
	if len(w.Exps) > 0 {
		report := w.LastExperience()
		t.Errorf("the bar took %d points (level %d, progress %g) of a pickup the helmet mended whole",
			report.Total, report.Level, report.Progress)
	}
}

// TestAnOrbsRemainderMendsTheNextDamagedItem checks the carry: an orb worth
// more than one item's wear spends the largest part of itself on one and the
// remainder on another, both candidates damaged, so whichever the random
// pick takes first the answer is the same: both mended, six points banked.
func TestAnOrbsRemainderMendsTheNextDamagedItem(t *testing.T) {
	t.Parallel()
	cfg, _ := servertest.ExperienceSetup(t)

	srv, c, _ := servertest.JoinAs(t, cfg, "Holder")

	pick := servertest.EnchantedStack(t, "minecraft:diamond_pickaxe", "minecraft:mending", 1)
	pick.Damage = 5
	srv.GiveSlot("Holder", player.HotbarSlot, pick)
	helmet := servertest.EnchantedStack(t, "minecraft:diamond_helmet", "minecraft:mending", 1)
	helmet.Damage = 5
	srv.GiveSlot("Holder", player.ArmorSlot, helmet)

	w := servertest.NewOrbWatcher(t, c)
	srv.SpawnOrb(0.5, 64, 0.5, 10)
	w.ReadUntilTicks(srv, 240, func() bool {
		return len(w.Takes) > 0 && len(w.Exps) > 0
	})

	if got := srv.InventorySlot("Holder", player.HotbarSlot); got.Damage != 0 {
		t.Errorf("the pickaxe's damage after the pickup = %d, want the full mend of it", got.Damage)
	}
	if got := srv.InventorySlot("Holder", player.ArmorSlot); got.Damage != 0 {
		t.Errorf("the helmet's damage after the pickup = %d, want the full mend of it", got.Damage)
	}
	// The first wear spends two points and the second the two of the eight
	// left, so six is what reaches the bar.
	if report := w.LastExperience(); report.Total != 6 {
		t.Errorf("the bar read total=%d, want the six the two mends bank", report.Total)
	}
}

// TestAnOrbLeavesANonSelectedHotbarSlotUnmended checks the slot the repair
// sweep reads: a mending item in a hotbar slot the player is not holding is
// not read, so it comes away worn and the orb's whole worth reaches the bar.
func TestAnOrbLeavesANonSelectedHotbarSlotUnmended(t *testing.T) {
	t.Parallel()
	cfg, _ := servertest.ExperienceSetup(t)

	srv, c, _ := servertest.JoinAs(t, cfg, "Idler")

	pick := servertest.EnchantedStack(t, "minecraft:diamond_pickaxe", "minecraft:mending", 1)
	pick.Damage = 100
	srv.GiveSlot("Idler", player.HotbarSlot+1, pick)

	w := servertest.NewOrbWatcher(t, c)
	srv.SpawnOrb(0.5, 64, 0.5, 10)
	w.ReadUntilTicks(srv, 240, func() bool { return len(w.Exps) > 0 })

	if got := srv.InventorySlot("Idler", player.HotbarSlot+1); got.Damage != 100 {
		t.Errorf("the unheld pickaxe's damage = %d, want the hundred it started with", got.Damage)
	}
	if report := w.LastExperience(); report.Total != 10 {
		t.Errorf("the bar read total=%d, want the whole ten the pickaxe could not take", report.Total)
	}
}
