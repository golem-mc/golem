package raids

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
)

// captainOf is the raider wearing the captain's banner.
func captainOf(srv *server.Server) (int32, bool) {
	banner := srv.OminousBanner()
	for _, id := range raiderIDs(srv) {
		if head := srv.MobEquipment(id, "head"); head.Same(banner) {
			return id, true
		}
	}
	return 0, false
}

// bannersOnTheGround counts the captain's banners lying as items.
func bannersOnTheGround(srv *server.Server) int {
	banner, n := srv.OminousBanner(), 0
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Stack.Same(banner) {
			n++
		}
	}
	return n
}

// firstWave starts a raid and runs it to its first wave.
func firstWave(t *testing.T) (*server.Server, func(n int)) {
	t.Helper()
	srv, c, _ := village(t, rules.DifficultyNormal)
	startRaid(t, c, srv, 0)
	run(c, srv, 330)
	return srv, func(n int) { run(c, srv, n) }
}

// TestADeadCaptainDropsTheBanner: the banner on the captain's head is a
// guaranteed drop, so it lies on the ground once he is dead, with the pickup
// delay of a death drop.
// vanilla: Raid.setLeader (drop chance 2.0), Mob.dropCustomDeathLoot
func TestADeadCaptainDropsTheBanner(t *testing.T) {
	t.Parallel()
	srv, ticks := firstWave(t)
	id, ok := captainOf(srv)
	if !ok {
		t.Fatal("no raider wears the captain's banner")
	}
	if got := bannersOnTheGround(srv); got != 0 {
		t.Fatalf("%d banners lie about before the captain dies", got)
	}
	srv.KillMob(id)
	ticks(2)
	if got := bannersOnTheGround(srv); got != 1 {
		t.Errorf("banners on the ground after the captain died = %d, want 1", got)
	}
}

// TestARaiderTakesTheDeadCaptainsBanner: with the wave's captain dead the
// raiders walk to the banner he dropped; one wears it, leads the wave, and
// would drop it in turn.
// vanilla: Raider.ObtainRaidLeaderBannerGoal, Raider.pickUpItem
func TestARaiderTakesTheDeadCaptainsBanner(t *testing.T) {
	t.Parallel()
	srv, ticks := firstWave(t)
	old, _ := captainOf(srv)
	srv.KillMob(old)
	ticks(2)
	if r := srv.Raids()[0]; r.LeaderWaveOne {
		t.Fatal("the raid still knows a captain of the wave")
	}
	var heir int32
	for range 40 {
		ticks(10)
		if id, ok := captainOf(srv); ok && id != old {
			heir = id
			break
		}
	}
	if heir == 0 {
		t.Fatal("no raider took the banner in 400 ticks")
	}
	if got := bannersOnTheGround(srv); got != 0 {
		t.Errorf("%d banners still lie about", got)
	}
	if r := srv.Raids()[0]; !r.LeaderWaveOne {
		t.Errorf("the raid has no captain for the wave after a raider took the banner")
	}
	if _, chances := srv.MobGear(heir); chances[5] < 1 {
		t.Errorf("the new captain's head drop chance = %v, want a sure drop", chances[5])
	}
	srv.KillMob(heir)
	ticks(2)
	if got := bannersOnTheGround(srv); got != 1 {
		t.Errorf("banners on the ground after the new captain died = %d, want 1", got)
	}
}

// TestARaiderLeavesAStackOfTwoBanners: a stack of two ominous banners is not
// the captain's banner, so with the captain dead nobody takes it.
// vanilla: Raider.ObtainRaidLeaderBannerGoal, ItemStack.matches
func TestARaiderLeavesAStackOfTwoBanners(t *testing.T) {
	t.Parallel()
	srv, ticks := firstWave(t)
	old, _ := captainOf(srv)
	srv.KillMob(old)
	ticks(2)
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Stack.Same(srv.OminousBanner()) {
			srv.DiscardItem(it.ID)
		}
	}
	var x, y, z float64
	for _, id := range raiderIDs(srv) {
		if id != old {
			x, y, z, _ = srv.MobPos(id)
			break
		}
	}
	srv.SpawnOminousBannerStack(2, x, y, z)
	ticks(200)
	if got := bannersOnTheGround(srv); got != 1 {
		t.Errorf("banner stacks on the ground = %d, want the one lying untouched", got)
	}
	if _, ok := captainOf(srv); ok {
		t.Error("a raider wears a banner taken from a stack of two")
	}
}
