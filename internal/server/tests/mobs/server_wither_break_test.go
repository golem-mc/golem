package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// The wither's rampage, black box: a hit starts a 20-tick countdown to a pass
// over its box grown by one block that breaks what is not wither-immune; its
// death leaves a nether star and a rose where it killed.
// vanilla: WitherBoss.hurtServer, customServerAiStep, dropCustomDeathLoot

// witherBox boxes a free wither (Invul 0) in stone at x, z 7 to 9 and y 64 to
// 67 with bedrock at one corner, and returns its ID. The creative builder
// stands outside, within reach, and has its swing wound.
func witherBox(t *testing.T) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	srv, c := witherScene(t, 2)
	srv.SetGameRule("mob_griefing", true)
	srv.SetPlayerGameMode(witherBuilder, 1)
	stone := servertest.MustState(t, "minecraft:stone", nil)
	for x := int32(7); x <= 9; x++ {
		for z := int32(7); z <= 9; z++ {
			for y := int32(64); y <= 67; y++ {
				if x != 8 || z != 8 {
					srv.SetBlock(x, y, z, stone)
				}
			}
		}
	}
	srv.SetBlock(9, 65, 9, servertest.MustState(t, "minecraft:bedrock", nil))
	id := srv.SpawnMob("minecraft:wither", 8.5, 64, 8.5)
	c.MoveTo(11.5, 64, 8.5, 90, 0)
	c.Settle()
	for range 20 {
		srv.TickSurvival()
	}
	return srv, c, id
}

// TestAHitWitherBreaksTheStoneAroundItTwentyTicksLater: the hit arms the
// pass; after 19 ticks the box still stands, on the 20th the stone is air,
// bedrock stays and level event 1022 is sent.
func TestAHitWitherBreaksTheStoneAroundItTwentyTicksLater(t *testing.T) {
	t.Parallel()
	srv, c, id := witherBox(t)
	c.Attack(id)
	c.Settle()
	broke := false
	tick := func() {
		srv.PlaceMob(id, 8.5, 64, 8.5)
		srv.TickMobs(1)
		srv.PlaceMob(id, 8.5, 64, 8.5)
		c.SettleAndCollect(func(pid int32, data []byte) {
			if pid == protocol.IDPlayWorldEvent && protocol.NewReader(data).ReadI32() == protocol.WorldEventWitherBreak {
				broke = true
			}
		})
	}
	for range 19 {
		tick()
	}
	if broke || srv.BlockAt(7, 64, 7) == 0 {
		t.Fatalf("after 19 ticks: broke %v, corner air %v, want the box standing", broke, srv.BlockAt(7, 64, 7) == 0)
	}
	tick()
	if !broke {
		t.Error("level event 1022 was not sent on the 20th tick")
	}
	for _, cell := range [][3]int32{{7, 64, 7}, {9, 67, 7}, {8, 66, 9}, {7, 65, 8}} {
		if got := srv.BlockAt(cell[0], cell[1], cell[2]); got != 0 {
			t.Errorf("stone at %v is state %d, want air", cell, got)
		}
	}
	if got := srv.BlockAt(9, 65, 9); got != servertest.MustState(t, "minecraft:bedrock", nil) {
		t.Errorf("bedrock cell is state %d, want it kept", got)
	}
}

// TestAHitWitherLeavesTheBlocksWithoutMobGriefing holds the rule's gate.
func TestAHitWitherLeavesTheBlocksWithoutMobGriefing(t *testing.T) {
	t.Parallel()
	srv, c, id := witherBox(t)
	srv.SetGameRule("mob_griefing", false)
	c.Attack(id)
	c.Settle()
	for range 20 {
		srv.PlaceMob(id, 8.5, 64, 8.5)
		srv.TickMobs(1)
	}
	if srv.BlockAt(7, 64, 7) == 0 {
		t.Error("the wither broke stone with mob_griefing off")
	}
}

// TestAKilledWitherDropsALongLivedNetherStar: the star is an item entity
// whose age starts at minus the despawn time.
// vanilla: WitherBoss.dropCustomDeathLoot, ItemEntity.setExtendedLifetime
func TestAKilledWitherDropsALongLivedNetherStar(t *testing.T) {
	t.Parallel()
	srv, c := witherDeathScene(t)
	id := srv.SpawnMob("minecraft:wither", 8.5, 64, 8.5)
	srv.KillMob(id)
	srv.TickMobs(1)
	c.Settle()
	star := servertest.ExpectOneDroppedItem(t, srv, "minecraft:nether_star")
	if star.Age > entity.ItemExtendedAge+5 {
		t.Errorf("nether star age %d, want about %d", star.Age, entity.ItemExtendedAge)
	}
}

// TestACowKilledByAWitherSkullLeavesAWitherRose: the flat world's stone ground
// cannot hold a rose, so the cow stands on dirt.
// vanilla: LivingEntity.createWitherRose
func TestACowKilledByAWitherSkullLeavesAWitherRose(t *testing.T) {
	t.Parallel()
	srv, c := witherDeathScene(t)
	wither := srv.SpawnMob("minecraft:wither", 30.5, 80, 30.5)
	srv.SetBlock(8, 63, 8, servertest.MustState(t, "minecraft:dirt", nil))
	cow := srv.SpawnMob("minecraft:cow", 8.5, 64, 8.5)
	c.Settle()
	srv.HurtMobByMob(cow, wither, 1000, "minecraft:wither_skull")
	srv.TickMobs(1)
	c.Settle()
	if got, want := srv.BlockAt(8, 64, 8), servertest.MustState(t, "minecraft:wither_rose", nil); got != want {
		t.Errorf("the cow's cell is state %d, want a wither rose (%d)", got, want)
	}
}

// witherDeathScene holds a flat scene without a summoning structure.
func witherDeathScene(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), witherBuilder)
	servertest.HoldTicks(t, srv)
	srv.SetSpawning(false)
	srv.SetGameRule("mob_griefing", true)
	return srv, c
}

// TestACowWitheredAfterASkullLeavesARose keeps the mob kill credit through
// the effect's sourceless damage.
func TestACowWitheredAfterASkullLeavesARose(t *testing.T) {
	t.Parallel()
	srv, c := witherDeathScene(t)
	wither := srv.SpawnMob("minecraft:wither", 30.5, 80, 30.5)
	srv.SetBlock(8, 63, 8, servertest.MustState(t, "minecraft:dirt", nil))
	cow := srv.SpawnMob("minecraft:cow", 8.5, 64, 8.5)
	srv.HurtMobByMob(cow, wither, 0.5, "minecraft:wither_skull")
	// The skull's knockback has no direction, so its push is the cow's
	// dice; still the cow so it dies over the dirt.
	srv.PlaceMob(cow, 8.5, 64, 8.5)
	srv.GiveMobEffect(cow, "minecraft:wither", 20, 1)
	srv.SetMobHealth(cow, 0.5)
	srv.TickMobs(1)
	c.Settle()
	if got, want := srv.BlockAt(8, 64, 8), servertest.MustState(t, "minecraft:wither_rose", nil); got != want {
		t.Errorf("withered cow's cell = %d, want rose %d", got, want)
	}
}

// TestAPlayerCreditedCowKilledByASkullLeavesNoRose gives the recent player
// priority over the fatal skull's owner.
func TestAPlayerCreditedCowKilledByASkullLeavesNoRose(t *testing.T) {
	t.Parallel()
	srv, c := witherDeathScene(t)
	wither := srv.SpawnMob("minecraft:wither", 30.5, 80, 30.5)
	srv.SetBlock(8, 63, 8, servertest.MustState(t, "minecraft:dirt", nil))
	cow := srv.SpawnMob("minecraft:cow", 8.5, 64, 8.5)
	c.MoveTo(10.5, 64, 8.5, 90, 0)
	c.Attack(cow)
	c.Settle()
	if hp, _ := srv.MobHealth(cow); hp >= 10 {
		t.Fatal("player's hit did not land")
	}
	srv.HurtMobByMob(cow, wither, 1000, "minecraft:wither_skull")
	srv.TickMobs(1)
	c.Settle()
	if got := srv.BlockAt(8, 64, 8); got != 0 {
		t.Errorf("player-credited cow's cell = %d, want air", got)
	}
	for _, item := range srv.ItemsInDimension(world.DimensionName) {
		if item.Item == "minecraft:wither_rose" {
			t.Error("player-credited cow dropped a rose")
		}
	}
}

// TestAPlayerKilledByAWitherLeavesARose covers ServerPlayer's death path.
func TestAPlayerKilledByAWitherLeavesARose(t *testing.T) {
	t.Parallel()
	srv, c := witherDeathScene(t)
	wither := srv.SpawnMob("minecraft:wither", 30.5, 80, 30.5)
	srv.SetBlock(8, 63, 8, servertest.MustState(t, "minecraft:dirt", nil))
	c.MoveTo(8.5, 64, 8.5, 0, 0)
	c.Settle()
	if !srv.HurtByEntity(witherBuilder, 1000, wither) {
		t.Fatal("fatal wither hit did not land")
	}
	c.Settle()
	if got, want := srv.BlockAt(8, 64, 8), servertest.MustState(t, "minecraft:wither_rose", nil); got != want {
		t.Errorf("player's cell = %d, want rose %d", got, want)
	}
}
