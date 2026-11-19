package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The wither, black box: the T of soul sand under three skulls summons it
// invulnerable with a boss bar, it blasts when the 220 ticks end and then
// shoots skulls at a survival player.
// vanilla: WitherSkullBlock.checkSpawn, WitherBoss

const witherBuilder = "Summoner"

// witherScene joins a survival player into the flat world with the loop
// stopped, the soul sand T built at x 1 to 3 (skulls over it but the middle
// one) and the last skull in hand; the build is one click from done.
func witherScene(t *testing.T, difficulty uint8) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), witherBuilder)
	c.Settle()
	srv.StopTick()
	srv.SetSpawning(false)
	srv.SetDifficulty(difficulty)
	srv.SetGameRule("mob_griefing", false)
	sand := servertest.MustState(t, "minecraft:soul_sand", nil)
	skull := servertest.MustState(t, "minecraft:wither_skeleton_skull", nil)
	for _, cell := range [][3]int32{{2, 64, 3}, {1, 65, 3}, {2, 65, 3}, {3, 65, 3}} {
		srv.SetBlock(cell[0], cell[1], cell[2], sand)
	}
	srv.SetBlock(1, 66, 3, skull)
	srv.SetBlock(3, 66, 3, skull)
	srv.GiveSlot(witherBuilder, player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:wither_skeleton_skull"), Count: 1})
	c.Settle()
	return srv, c
}

// witherSummoned places the last skull and returns the wither's spawn.
func witherSummoned(t *testing.T, srv *server.Server, c *servertest.Client) servertest.SpawnedMob {
	t.Helper()
	typeID, _ := registry.EntityTypeID("minecraft:wither")
	c.PlaceBlock(2, 65, 3, protocol.FaceTop)
	return c.ExpectMobSpawn(typeID)
}

// TestTheThirdSkullOnTheSoulSandTSummonsAnInvulnerableWither: the seven
// blocks go, the wither stands at the stem's foot with Invul 220 and a
// purple darkening bar arrives with the first tick.
// vanilla: WitherSkullBlock.checkSpawn, WitherBoss.makeInvulnerable
func TestTheThirdSkullOnTheSoulSandTSummonsAnInvulnerableWither(t *testing.T) {
	t.Parallel()
	srv, c := witherScene(t, 2)
	wither := witherSummoned(t, srv, c)
	if wither.X != 2.5 || wither.Y != 64.55 || wither.Z != 3.5 {
		t.Errorf("the wither stands at (%v, %v, %v), want the stem's foot (2.5, 64.55, 3.5)", wither.X, wither.Y, wither.Z)
	}
	if wither.WitherInvul != 220 {
		t.Errorf("Invul = %d, want 220", wither.WitherInvul)
	}
	if wither.Health != 100 {
		t.Errorf("health = %v, want a third of 300", wither.Health)
	}
	for _, cell := range [][3]int32{{1, 66, 3}, {2, 66, 3}, {3, 66, 3}, {1, 65, 3}, {2, 65, 3}, {3, 65, 3}, {2, 64, 3}} {
		if got := srv.BlockAt(cell[0], cell[1], cell[2]); got != 0 {
			t.Errorf("pattern cell %v still holds state %d, want air", cell, got)
		}
	}
	srv.TickMobs(1)
	var added bool
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayBossEvent {
			return
		}
		r := protocol.NewReader(data)
		r.ReadUUID()
		if r.ReadVarInt() != protocol.BossEventAdd {
			return
		}
		added = true
		// The packet ends with colour, overlay and flags: purple, progress,
		// darken-screen.
		if tail := data[len(data)-3:]; tail[0] != 5 || tail[1] != 0 || tail[2] != 1 {
			t.Errorf("the bar ends colour %d overlay %d flags %#x, want purple 5, progress 0, darken 1", tail[0], tail[1], tail[2])
		}
	})
	if !added {
		t.Error("no Boss Event ADD arrived")
	}
}

// TestNoWitherRisesInPeaceful: the pattern is checked on any difficulty but
// peaceful.
func TestNoWitherRisesInPeaceful(t *testing.T) {
	t.Parallel()
	srv, c := witherScene(t, 0)
	c.PlaceBlock(2, 65, 3, protocol.FaceTop)
	c.Settle()
	if n := srv.MobCount(); n != 0 {
		t.Errorf("mob count = %d, want none", n)
	}
}

// TestTheWitherBlastsWhenItsInvulnerabilityEndsThenShootsSkulls: after 220
// ticks an Explosion packet and level event 1023 arrive; afterwards a wither
// skull spawns within 60 ticks, aimed at the survival player.
// vanilla: WitherBoss.customServerAiStep, performRangedAttack
func TestTheWitherBlastsWhenItsInvulnerabilityEndsThenShootsSkulls(t *testing.T) {
	t.Parallel()
	srv, c := witherScene(t, 2)
	witherSummoned(t, srv, c)
	// The blast kills what stands within its seven blocks: the builder, who
	// reached the T from three blocks away, steps back to twelve first.
	c.MoveTo(14.5, 64, 3.5, 0, 0)
	c.Settle()
	var blast, spawned bool
	var heard1023, heard1024 bool
	var blastTick, skullTick int
	collect := func(tick int) func(id int32, data []byte) {
		return func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayExplosion:
				if !blast {
					blast, blastTick = true, tick
				}
			case protocol.IDPlayWorldEvent:
				switch protocol.NewReader(data).ReadI32() {
				case protocol.WorldEventWitherSpawn:
					heard1023 = true
				case protocol.WorldEventWitherShoot:
					heard1024 = true
				}
			case protocol.IDPlaySpawnEntity:
				r := protocol.NewReader(data)
				r.ReadVarInt()
				r.ReadUUID()
				if r.ReadVarInt() == int32(protocol.EntityTypeWitherSkull) && !spawned {
					spawned, skullTick = true, tick
				}
			}
		}
	}
	for tick := range 219 {
		srv.TickMobs(1)
		c.SettleAndCollect(collect(tick))
	}
	if blast || heard1023 {
		t.Fatal("the blast came before the 220th tick")
	}
	srv.TickMobs(1)
	c.SettleAndCollect(collect(219))
	if !blast || !heard1023 {
		t.Fatalf("after 220 ticks: explosion %v, level event 1023 %v, want both", blast, heard1023)
	}
	for tick := range 60 {
		srv.TickMobs(1)
		servertest.DriveTargetTicks(srv, 1)
		c.SettleAndCollect(collect(220 + tick))
		if spawned {
			break
		}
	}
	if !spawned {
		t.Fatalf("no wither skull spawned within 60 ticks of the blast (blast at %d)", blastTick)
	}
	if !heard1024 {
		t.Error("the launch's level event 1024 was never heard")
	}
	_ = skullTick
}

// witherHunt joins a creative player far from the scene, with the loop held,
// and summons a vulnerable wither at (6.5, 64, 6.5) beside a victim of the
// kind 8 blocks east of it. It returns the wither's and the victim's IDs.
func witherHunt(t *testing.T, victim string) (srv *server.Server, c *servertest.Client, wither, other int32) {
	t.Helper()
	srv, c, _ = servertest.JoinAs(t, servertest.HostileTestConfig(), witherBuilder)
	c.Settle()
	srv.SetSpawning(false)
	srv.SetDifficulty(2)
	srv.SetGameRule("mob_griefing", false)
	srv.SetPlayerGameMode(witherBuilder, 1)
	servertest.HoldTicks(t, srv)
	srv.RunLine("summon minecraft:wither 6.5 64 6.5 {Invul:0}")
	wither = srv.MobIDsOfKind("minecraft:wither")[0]
	other = srv.SpawnMob(victim, 14.5, 64, 6.5)
	return srv, c, wither, other
}

// skullsWithin runs n ticks and reports whether a wither skull spawned.
func skullsWithin(t *testing.T, srv *server.Server, c *servertest.Client, n int) bool {
	t.Helper()
	var spawned bool
	for range n {
		servertest.RunTicks(t, srv, 1)
		c.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlaySpawnEntity {
				return
			}
			r := protocol.NewReader(data)
			r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == int32(protocol.EntityTypeWitherSkull) {
				spawned = true
			}
		})
	}
	return spawned
}

// TestAWitherShootsACowWithNoPlayerToHunt: the wither's target goal takes any
// living mob outside wither_friends; a zombie, undead, is left alone.
// vanilla: WitherBoss.registerGoals, LIVING_ENTITY_SELECTOR
func TestAWitherShootsACowWithNoPlayerToHunt(t *testing.T) {
	t.Parallel()
	srv, c, _, _ := witherHunt(t, "minecraft:cow")
	if !skullsWithin(t, srv, c, 100) {
		t.Error("no wither skull spawned at a cow within 100 ticks")
	}
	srv2, c2, _, _ := witherHunt(t, "minecraft:zombie")
	if skullsWithin(t, srv2, c2, 100) {
		t.Error("a wither skull spawned with only a zombie in reach")
	}
}

// witherNear joins a survival player teleported to (px, 64, 6.5) with the
// loop held, runs setup, and summons a vulnerable wither at (6.5, 64, 6.5).
// It returns the wither's and the player's IDs.
func witherNear(t *testing.T, px float64, setup func(srv *server.Server)) (srv *server.Server, wither, player int32) {
	t.Helper()
	srv, c, player := servertest.JoinAs(t, servertest.HostileTestConfig(), witherBuilder)
	c.Settle()
	srv.SetSpawning(false)
	srv.SetDifficulty(2)
	srv.SetGameRule("mob_griefing", false)
	srv.SetPlayerGameMode(witherBuilder, 0)
	srv.RunLine(fmt.Sprintf("tp %s %v 64 6.5", witherBuilder, px))
	c.Settle()
	servertest.HoldTicks(t, srv)
	setup(srv)
	srv.RunLine("summon minecraft:wither 6.5 64 6.5 {Invul:0}")
	return srv, srv.MobIDsOfKind("minecraft:wither")[0], player
}

// TestAWitherTargetsANearerCowBeforeAFarPlayer: the wither's one target goal
// ranks every living entity by distance, so a cow 6 blocks off goes ahead of
// a player 30 blocks off.
// vanilla: WitherBoss.registerGoals, NearestAttackableTargetGoal.findTarget
func TestAWitherTargetsANearerCowBeforeAFarPlayer(t *testing.T) {
	t.Parallel()
	var cow int32
	srv, wither, player := witherNear(t, 36.5, func(srv *server.Server) {
		cow = srv.SpawnMob("minecraft:cow", 12.5, 64, 6.5)
	})
	servertest.UntilTicks(t, srv, 20, "the wither to take a target", func() bool { return srv.MobTargetID(wither) != 0 })
	if got := srv.MobTargetID(wither); got != cow {
		t.Errorf("wither targets %d, want the cow %d (player %d)", got, cow, player)
	}
}

// TestAWitherTargetsAPlayerBehindAWall: the target goal does not need sight
// (mustSee false), so a player behind a wall in follow range is taken.
// vanilla: WitherBoss.registerGoals
func TestAWitherTargetsAPlayerBehindAWall(t *testing.T) {
	t.Parallel()
	srv, wither, player := witherNear(t, 20.5, func(srv *server.Server) {
		srv.RunLine("fill 14 64 0 14 80 13 minecraft:stone")
	})
	servertest.UntilTicks(t, srv, 20, "the wither to target the player behind the wall", func() bool { return srv.MobTargetID(wither) == player })
}
