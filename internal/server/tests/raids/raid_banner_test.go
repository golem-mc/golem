package raids

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
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

func TestLoadedPillagerClaimsCaptainBannerWithoutAI(t *testing.T) {
	t.Parallel()
	srv, c, _ := village(t, rules.DifficultyNormal)
	startRaid(t, c, srv, 0)
	run(c, srv, 330)
	captain, _ := captainOf(srv)
	var heir int32
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:pillager" && m.ID != captain {
			heir = m.ID
			break
		}
	}
	if heir == 0 {
		t.Fatal("no ordinary pillager in the first wave")
	}
	uuid, _ := srv.MobUUID(heir)
	srv.RunLine("data merge entity " + uuid + " {NoAI:1b}")
	srv.RunLine("item replace entity " + uuid + " weapon.mainhand with minecraft:air")
	for _, id := range raiderIDs(srv) {
		if id != heir {
			other, _ := srv.MobUUID(id)
			srv.RunLine("kill " + other)
		}
	}
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		srv.DiscardItem(it.ID)
	}
	x, y, z, _ := srv.MobPos(heir)
	// No tick may restore membership before the banner is offered.
	srv.SpawnOminousBannerItem(x, y, z)
	run(c, srv, 20)
	if head := srv.MobEquipment(heir, "head"); !head.Equal(srv.OminousBanner()) {
		t.Fatalf("head = %v, hand = %v; want the captain's banner on the head", head, srv.MobEquipment(heir, "mainhand"))
	}
	if !srv.MobEquipment(heir, "mainhand").Empty() || bannersOnTheGround(srv) != 0 {
		t.Fatal("captain pickup left a banner in the hand or on the ground")
	}
	if !srv.Raids()[0].LeaderWaveOne {
		t.Fatal("the banner wearer was not registered as wave captain")
	}
}

// vanilla: PathNavigation.getMaxPathLength, Raider.ObtainRaidLeaderBannerGoal
func TestAVindicatorReachesABannerThroughAPassageBeyondItsFollowRange(t *testing.T) {
	t.Parallel()
	srv, c, _ := village(t, rules.DifficultyNormal)
	startRaid(t, c, srv, 0)
	srv.SetPlayerGameMode("Hero", protocol.GameModeCreative)
	for _, m := range srv.Mobs() {
		srv.SetMobInvulnerable(m.ID)
	}
	stone := registry.BlockStone.DefaultState()
	// The banner is inside the follow-range box for every follow range the
	// spawn bonus can leave (at least 10.62), across a wall whose only
	// passage is x=12. Reaching the banner's neighbour takes 14 steps.
	for x := int32(-13); x <= 11; x++ {
		for y := int32(64); y <= 66; y++ {
			srv.SetBlock(x, y, 1, stone)
		}
	}
	id := srv.SpawnMobSeeded("minecraft:vindicator", 0.5, 64, 0.5, 2)
	servertest.UntilTicks(t, srv, 20, "the vindicator to join the raid", func() bool {
		in, _ := srv.MobRaid(id)
		return in
	})
	srv.MoveMob(id, 0.5, 0.5)
	if head := srv.MobEquipment(id, "head"); !head.Empty() {
		t.Fatalf("vindicator already wears %v", head)
	}
	srv.SpawnOminousBannerItem(11.5, 64, 2.5)
	crossed := false
	servertest.UntilTicks(t, srv, 160, "the vindicator to take the banner", func() bool {
		x, _, _, _ := srv.MobPos(id)
		crossed = crossed || x > 12
		return srv.MobEquipment(id, "head").Same(srv.OminousBanner())
	})
	if !crossed {
		t.Fatal("the vindicator took the banner without reaching the passage 12 blocks from its start")
	}
	if got := bannersOnTheGround(srv); got != 0 {
		t.Errorf("banners left on the ground = %d, want none", got)
	}
}

// TestARaiderLeavesABannerWhileTheCaptainLives: a banner on the ground is no
// use to a wave whose captain is alive, so nobody goes for it.
// vanilla: Raider.ObtainRaidLeaderBannerGoal.cannotPickUpBanner
func TestARaiderLeavesABannerWhileTheCaptainLives(t *testing.T) {
	t.Parallel()
	srv, ticks := firstWave(t)
	captain, _ := captainOf(srv)
	var x, y, z float64
	for _, id := range raiderIDs(srv) {
		if id != captain {
			x, y, z, _ = srv.MobPos(id)
			break
		}
	}
	srv.SpawnOminousBannerItem(x, y, z)
	ticks(100)
	if got := bannersOnTheGround(srv); got != 1 {
		t.Errorf("banners on the ground = %d, want the one lying untouched", got)
	}
	if got, ok := captainOf(srv); !ok || got != captain {
		t.Errorf("the captain is now %d (found %v), want %d", got, ok, captain)
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

// TestARaiderOutsideARaidLeavesABanner: a vindicator in no raid has no wave to
// lead.
// vanilla: Raider.ObtainRaidLeaderBannerGoal.cannotPickUpBanner
func TestARaiderOutsideARaidLeavesABanner(t *testing.T) {
	t.Parallel()
	srv, c, _ := village(t, rules.DifficultyNormal)
	pos := srv.PlayerPosition("Hero")
	id := srv.SpawnMob("minecraft:vindicator", pos.X+6, pos.Y, pos.Z)
	x, y, z, _ := srv.MobPos(id)
	srv.SpawnOminousBannerItem(x+1, y, z)
	run(c, srv, 100)
	if got := bannersOnTheGround(srv); got != 1 {
		t.Errorf("banners on the ground = %d, want the one lying untouched", got)
	}
}

// TestARaiderLeavesABannerOnceTheRaidIsLost: a raid that is over has no wave
// left to lead, so the banner stays where it fell.
// vanilla: Raider.ObtainRaidLeaderBannerGoal.cannotPickUpBanner (Raid.isOver)
func TestARaiderLeavesABannerOnceTheRaidIsLost(t *testing.T) {
	t.Parallel()
	srv, c, bell := village(t, rules.DifficultyNormal)
	startRaid(t, c, srv, 0)
	run(c, srv, 330)
	srv.SetBlock(bell[0], bell[1], bell[2], registry.MustState("minecraft:air", nil))
	run(c, srv, 450)
	if r := srv.Raids()[0]; r.Status != "loss" {
		t.Fatalf("raid status = %q, want loss", r.Status)
	}
	captain, ok := captainOf(srv)
	if !ok {
		t.Skip("the lost raid's raiders no longer wear the banner")
	}
	srv.KillMob(captain)
	run(c, srv, 120)
	if got := bannersOnTheGround(srv); got != 1 {
		t.Errorf("banners on the ground = %d, want the one lying untouched", got)
	}
}

// TestAPillagerThatTookTheBannerDropsNoOminousBottle: a pillager that picks up
// the dead captain's banner leads the wave and drops no bottle while in the raid.
// vanilla: Raider.pickUpItem, loot_table entities/pillager (raider is_captain)
func TestAPillagerThatTookTheBannerDropsNoOminousBottle(t *testing.T) {
	t.Parallel()
	srv, ticks := firstWave(t)
	old, _ := captainOf(srv)
	for _, m := range srv.Mobs() {
		if m.Kind != "minecraft:pillager" && m.Kind != "minecraft:villager" && m.ID != old {
			srv.KillMob(m.ID)
		}
	}
	srv.KillMob(old)
	ticks(2)
	bottles := func() int {
		n := 0
		for _, it := range srv.ItemsInDimension("minecraft:overworld") {
			if it.Item == "minecraft:ominous_bottle" {
				n += int(it.Count)
			}
		}
		return n
	}
	if got := bottles(); got != 0 {
		t.Fatalf("the original wave captain dropped %d ominous bottles, want 0", got)
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
		t.Fatal("no pillager took the banner in 400 ticks")
	}
	srv.KillMob(heir)
	ticks(2)
	if got := bottles(); got != 0 {
		t.Errorf("the pillager that took the banner dropped %d ominous bottles, want 0", got)
	}
}
