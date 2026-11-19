package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

const (
	silverfishKind = "minecraft:silverfish"
	// spawnAnimStatus is the entity event of Mob.spawnAnim.
	spawnAnimStatus = 20
)

// silverfishTests joins a player in the mode; a stepped scene stops the
// loop, which the digs cannot: their acknowledgement goes out at a tick's end.
func silverfishTests(t *testing.T, mode string, stepped bool) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = mode
	srv, c := servertest.Join(t, cfg, "Miner")
	servertest.GroundThePlayer(c)
	if stepped {
		srv.StopTick()
	}
	srv.SetDifficulty(2)
	return srv, c
}

func silverfishes(srv *server.Server) []server.TestMob {
	var found []server.TestMob
	for _, m := range srv.Mobs() {
		if m.Kind == silverfishKind {
			found = append(found, m)
		}
	}
	return found
}

func blockNamed(t *testing.T, name string) uint16 {
	t.Helper()
	id, ok := registry.BlockStates[name]
	if !ok {
		t.Fatalf("no block %s", name)
	}
	return id
}

func stateName(srv *server.Server, x, y, z int32) string {
	name, _ := servertest.StateProps(srv.BlockAt(x, y, z))
	return name
}

func silverfishTypeID(t *testing.T) int32 {
	t.Helper()
	id, ok := registry.EntityTypeID(silverfishKind)
	if !ok {
		t.Fatal("no silverfish entity type")
	}
	return id
}

// TestBreakingInfestedStoneReleasesASilverfishAndDropsNothing breaks the
// block bare-handed: one silverfish stands in the cell with the poof, the
// stone is not dropped.
// vanilla: InfestedBlock.spawnAfterBreak
func TestBreakingInfestedStoneReleasesASilverfishAndDropsNothing(t *testing.T) {
	t.Parallel()
	srv, c := silverfishTests(t, "survival", false)
	srv.SetBlock(3, 64, 3, blockNamed(t, "minecraft:infested_stone"))

	c.BreakBlock(3, 64, 3)
	c.WaitBlockChanged()

	spawned := c.ExpectMobSpawn(silverfishTypeID(t))
	c.AwaitEntityEvent(spawned.EntityID, spawnAnimStatus)
	found := silverfishes(srv)
	if len(found) != 1 {
		t.Fatalf("%d silverfish after the break, want 1", len(found))
	}
	if spawned.X != 3.5 || spawned.Y != 64 || spawned.Z != 3.5 {
		t.Errorf("the silverfish appeared at %v,%v,%v, want 3.5,64,3.5", spawned.X, spawned.Y, spawned.Z)
	}
	if items := srv.ItemsInDimension("minecraft:overworld"); len(items) != 0 {
		t.Errorf("the break dropped %d items, want none", len(items))
	}
}

// TestSilkTouchKeepsTheSilverfishInAndDropsTheHost checks the enchantment's
// two effects: the host block drops and nothing comes out.
// vanilla: InfestedBlock.spawnAfterBreak, EnchantmentTags.PREVENTS_INFESTED_SPAWNS
func TestSilkTouchKeepsTheSilverfishInAndDropsTheHost(t *testing.T) {
	t.Parallel()
	srv, c := silverfishTests(t, "survival", false)
	srv.SetBlock(3, 64, 3, blockNamed(t, "minecraft:infested_stone"))
	srv.GiveSlot("Miner", player.HotbarSlot,
		servertest.EnchantedStack(t, "minecraft:diamond_pickaxe", "minecraft:silk_touch", 1))

	c.BreakBlock(3, 64, 3)
	c.WaitBlockChanged()
	c.Settle()
	servertest.UntilTicks(t, srv, 100, "the host stone to drop", func() bool {
		return len(srv.ItemsInDimension("minecraft:overworld")) > 0
	})

	if n := len(silverfishes(srv)); n != 0 {
		t.Errorf("%d silverfish out of a silk-touched block, want none", n)
	}
	items := srv.ItemsInDimension("minecraft:overworld")
	if len(items) != 1 || items[0].Item != "minecraft:stone" {
		t.Errorf("the silk-touched break dropped %+v, want one stone", items)
	}
}

// TestACreativeBreakReleasesNoSilverfish: a creative break never reaches
// playerDestroy.
// vanilla: ServerPlayerGameMode.destroyBlock
func TestACreativeBreakReleasesNoSilverfish(t *testing.T) {
	t.Parallel()
	srv, c := silverfishTests(t, "creative", false)
	srv.SetBlock(3, 64, 3, blockNamed(t, "minecraft:infested_stone"))

	c.BreakBlock(3, 64, 3)
	c.WaitBlockChanged()
	c.Settle()

	if n := len(silverfishes(srv)); n != 0 {
		t.Errorf("%d silverfish out of a creative break, want none", n)
	}
}

// TestNoSilverfishWhenBlockDropsAreOff reads block_drops.
// vanilla: InfestedBlock.spawnAfterBreak
func TestNoSilverfishWhenBlockDropsAreOff(t *testing.T) {
	t.Parallel()
	srv, c := silverfishTests(t, "survival", false)
	srv.SetGameRule("block_drops", false)
	srv.SetBlock(3, 64, 3, blockNamed(t, "minecraft:infested_stone"))

	c.BreakBlock(3, 64, 3)
	c.WaitBlockChanged()
	c.Settle()

	if n := len(silverfishes(srv)); n != 0 {
		t.Errorf("%d silverfish with block_drops off, want none", n)
	}
}

// TestASilverfishHasItsVanillaBody pins health and size.
// vanilla: Silverfish.createAttributes
func TestASilverfishHasItsVanillaBody(t *testing.T) {
	t.Parallel()
	srv, _ := silverfishTests(t, "creative", true)
	id := srv.SpawnMob(silverfishKind, 3.5, 64, 3.5)
	if h, ok := srv.MobHealth(id); !ok || h != 8 {
		t.Errorf("silverfish health = %v, want 8", h)
	}
}

// walledIn closes the cell in bedrock on its six sides, so a mob standing
// there keeps its cell for the whole test.
func walledIn(t *testing.T, srv *server.Server, x, y, z int32) {
	t.Helper()
	bedrock := blockNamed(t, "minecraft:bedrock")
	for _, d := range [][3]int32{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
		srv.SetBlock(x+d[0], y+d[1], z+d[2], bedrock)
	}
}

// infestedRow lays count infested stones along x from x0 at y 64, z 3.
func infestedRow(t *testing.T, srv *server.Server, x0, count int32) {
	t.Helper()
	for x := x0; x < x0+count; x++ {
		srv.SetBlock(x, 64, 3, blockNamed(t, "minecraft:infested_stone"))
	}
}

// infestedLeft counts the infested stones of the row.
func infestedLeft(srv *server.Server, x0, count int32) int32 {
	var n int32
	for x := x0; x < x0+count; x++ {
		if stateName(srv, x, 64, 3) == "minecraft:infested_stone" {
			n++
		}
	}
	return n
}

// TestAHurtSilverfishCallsTheInfestedBlocksOut: a magic hurt, twenty ticks
// later, breaks infested blocks around it, each break letting another
// silverfish out.
// vanilla: Silverfish.SilverfishWakeUpFriendsGoal
func TestAHurtSilverfishCallsTheInfestedBlocksOut(t *testing.T) {
	t.Parallel()
	srv, _ := silverfishTests(t, "creative", true)
	infestedRow(t, srv, 4, 6)
	id := srv.SpawnMob(silverfishKind, 3.5, 64, 3.5)

	srv.HurtMobByWorld(id, 1, "minecraft:magic")
	srv.TickMobs(8)
	if left := infestedLeft(srv, 4, 6); left != 6 {
		t.Fatalf("%d of 6 infested blocks left after 8 ticks, want all: the call waits twenty", left)
	}
	srv.TickMobs(16)

	left := infestedLeft(srv, 4, 6)
	if left == 6 {
		t.Fatal("no infested block broke after the call")
	}
	if got, want := int32(len(silverfishes(srv))), 1+(6-left); got != want {
		t.Errorf("%d silverfish after %d blocks broke, want %d", got, 6-left, want)
	}
	if items := srv.ItemsInDimension("minecraft:overworld"); len(items) != 0 {
		t.Errorf("the woken blocks dropped %d items, want none", len(items))
	}
}

// TestTheWakeUpCallReachesTenBlocksSidewaysAndFiveUpAndDown calls with one
// infested block in reach, so the call's coin flip cannot hide it: the cell
// at the edge breaks, the cell a block further stays.
// vanilla: SilverfishWakeUpFriendsGoal.tick
func TestTheWakeUpCallReachesTenBlocksSidewaysAndFiveUpAndDown(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		dx, dy, dz int32
		broke      bool
	}{
		{"ten east", 10, 0, 0, true},
		{"eleven east", 11, 0, 0, false},
		{"ten south", 0, 0, 10, true},
		{"eleven south", 0, 0, 11, false},
		{"five up", 0, 5, 0, true},
		{"six up", 0, 6, 0, false},
		{"five down", 0, -5, 0, true},
		{"six down", 0, -6, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := silverfishTests(t, "creative", true)
			x, y, z := 3+tc.dx, 64+tc.dy, 3+tc.dz
			srv.SetBlock(x, y, z, blockNamed(t, "minecraft:infested_stone"))
			walledIn(t, srv, 3, 64, 3)
			id := srv.SpawnMob(silverfishKind, 3.5, 64, 3.5)

			srv.HurtMobByWorld(id, 1, "minecraft:magic")
			srv.TickMobs(30)

			if broke := stateName(srv, x, y, z) != "minecraft:infested_stone"; broke != tc.broke {
				t.Errorf("block at %+d,%+d,%+d broke = %v, want %v", tc.dx, tc.dy, tc.dz, broke, tc.broke)
			}
		})
	}
}

// TestAFallHurtDoesNotCallTheFriends: only a hurt with a body behind it or
// of the always_triggers_silverfish types makes the call.
// vanilla: Silverfish.hurtServer
func TestAFallHurtDoesNotCallTheFriends(t *testing.T) {
	t.Parallel()
	srv, _ := silverfishTests(t, "creative", true)
	infestedRow(t, srv, 4, 6)
	id := srv.SpawnMob(silverfishKind, 3.5, 64, 3.5)

	srv.HurtMobByWorld(id, 1, "minecraft:fall")
	srv.TickMobs(40)

	if left := infestedLeft(srv, 4, 6); left != 6 {
		t.Errorf("%d of 6 infested blocks left after a fall hurt, want all", left)
	}
}

// TestAHitTheInvulnerabilityWindowAbsorbsStillCallsTheFriends: a fall hurt
// opens the window, the magic hurt inside it lands nothing, and the call is
// armed all the same.
// vanilla: Silverfish.hurtServer
func TestAHitTheInvulnerabilityWindowAbsorbsStillCallsTheFriends(t *testing.T) {
	t.Parallel()
	srv, _ := silverfishTests(t, "creative", true)
	infestedRow(t, srv, 4, 6)
	id := srv.SpawnMob(silverfishKind, 3.5, 64, 3.5)

	srv.HurtMobByWorld(id, 1, "minecraft:fall")
	srv.HurtMobByWorld(id, 1, "minecraft:magic")
	srv.TickMobs(40)

	if left := infestedLeft(srv, 4, 6); left == 6 {
		t.Error("no infested block broke after an absorbed magic hurt, want the call")
	}
}

// TestAnExplosionBreakingInfestedStoneReleasesASilverfish: a blast has no
// tool, so nothing keeps the silverfish in.
// vanilla: BlockBehaviour.onExplosionHit
func TestAnExplosionBreakingInfestedStoneReleasesASilverfish(t *testing.T) {
	t.Parallel()
	srv, _ := silverfishTests(t, "creative", true)
	srv.SetBlock(8, 64, 8, blockNamed(t, "minecraft:infested_stone"))

	srv.RunExplosion(8.5, 70, 8.5, 6)

	if got := stateName(srv, 8, 64, 8); got != "minecraft:air" {
		t.Fatalf("the blast left %s at the cell, want air", got)
	}
	if n := len(silverfishes(srv)); n != 1 {
		t.Errorf("%d silverfish after the blast, want 1", n)
	}
}

// TestAHurtSilverfishTurnsBlocksBackWithoutMobGriefing: with the rule off
// the call restores the host block and lets nothing out.
// vanilla: SilverfishWakeUpFriendsGoal.tick
func TestAHurtSilverfishTurnsBlocksBackWithoutMobGriefing(t *testing.T) {
	t.Parallel()
	srv, _ := silverfishTests(t, "creative", true)
	srv.SetGameRule(world.RuleMobGriefing.String(), false)
	infestedRow(t, srv, 4, 6)
	id := srv.SpawnMob(silverfishKind, 3.5, 64, 3.5)

	srv.HurtMobByWorld(id, 1, "minecraft:magic")
	srv.TickMobs(30)

	left := infestedLeft(srv, 4, 6)
	if left == 6 {
		t.Fatal("no infested block turned back")
	}
	stones := 0
	for x := int32(4); x < 10; x++ {
		if stateName(srv, x, 64, 3) == "minecraft:stone" {
			stones++
		}
	}
	if int32(stones) != 6-left {
		t.Errorf("%d stones for %d turned blocks", stones, 6-left)
	}
	if n := len(silverfishes(srv)); n != 1 {
		t.Errorf("%d silverfish after a griefing-off call, want the 1 that called", n)
	}
}

// TestASilverfishEntersAdjacentStone: closed into a cavity of stone, an
// idle silverfish eventually merges into a wall: the block turns infested,
// the poof is told and the mob is gone.
// vanilla: Silverfish.SilverfishMergeWithStoneGoal
func TestASilverfishEntersAdjacentStone(t *testing.T) {
	t.Parallel()
	srv, c := silverfishTests(t, "creative", true)
	stone := blockNamed(t, "minecraft:stone")
	for x := int32(6); x <= 8; x++ {
		for y := int32(63); y <= 65; y++ {
			for z := int32(6); z <= 8; z++ {
				srv.SetBlock(x, y, z, stone)
			}
		}
	}
	srv.SetBlock(7, 64, 7, 0)
	id := srv.SpawnMob(silverfishKind, 7.5, 64, 7.5)
	c.Settle()

	for range 40 {
		srv.TickMobs(50)
		if len(silverfishes(srv)) == 0 {
			break
		}
	}

	if n := len(silverfishes(srv)); n != 0 {
		t.Fatalf("%d silverfish after 2000 ticks closed in stone, want it merged", n)
	}
	infested := 0
	beside := map[[3]int32]bool{{6, 64, 7}: true, {8, 64, 7}: true, {7, 63, 7}: true, {7, 65, 7}: true, {7, 64, 6}: true, {7, 64, 8}: true}
	for x := int32(6); x <= 8; x++ {
		for y := int32(63); y <= 65; y++ {
			for z := int32(6); z <= 8; z++ {
				if stateName(srv, x, y, z) == "minecraft:infested_stone" {
					infested++
					if !beside[[3]int32{x, y, z}] {
						t.Errorf("the infested block %d,%d,%d is not beside the cavity", x, y, z)
					}
				}
			}
		}
	}
	if infested != 1 {
		t.Errorf("%d infested blocks, want 1", infested)
	}
	c.AwaitEntityEvent(id, spawnAnimStatus)
}

// TestASilverfishStaysOutOfStoneWithoutMobGriefing: the merge asks the rule.
// vanilla: SilverfishMergeWithStoneGoal.canUse
func TestASilverfishStaysOutOfStoneWithoutMobGriefing(t *testing.T) {
	t.Parallel()
	srv, _ := silverfishTests(t, "creative", true)
	srv.SetGameRule(world.RuleMobGriefing.String(), false)
	stone := blockNamed(t, "minecraft:stone")
	for x := int32(6); x <= 8; x++ {
		for y := int32(63); y <= 65; y++ {
			for z := int32(6); z <= 8; z++ {
				srv.SetBlock(x, y, z, stone)
			}
		}
	}
	srv.SetBlock(7, 64, 7, 0)
	srv.SpawnMob(silverfishKind, 7.5, 64, 7.5)

	srv.TickMobs(1000)

	if n := len(silverfishes(srv)); n != 1 {
		t.Errorf("%d silverfish after 1000 ticks with mob_griefing off, want 1", n)
	}
}
