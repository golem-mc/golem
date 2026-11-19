package mobs

import (
	"fmt"
	"math"
	"strings"
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

// TestAWitherSkullSkullMissingItsBaseBuildsNothing: without the stem the
// third skull is only a skull.
func TestAWitherSkullMissingItsBaseBuildsNothing(t *testing.T) {
	t.Parallel()
	srv, c := witherScene(t, 2)
	srv.SetBlock(2, 64, 3, 0)
	c.PlaceBlock(2, 65, 3, protocol.FaceTop)
	c.Settle()
	if n := srv.MobCount(); n != 0 {
		t.Errorf("mob count = %d, want none", n)
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

// TestAZombiesBlowDoesNotHurtAWither: a wither_friends attacker is refused, an
// iron golem's blow lands.
// vanilla: WitherBoss.hurtServer
func TestAZombiesBlowDoesNotHurtAWither(t *testing.T) {
	t.Parallel()
	srv, _, wither, zombie := witherHunt(t, "minecraft:zombie")
	golem := srv.SpawnMob("minecraft:iron_golem", 14.5, 64, 8.5)
	before, _ := srv.MobHealth(wither)
	srv.StrikeMobWith(zombie, wither, 5)
	if after, _ := srv.MobHealth(wither); after != before {
		t.Errorf("a zombie's blow took the wither from %v to %v", before, after)
	}
	srv.StrikeMobWith(golem, wither, 5)
	if after, _ := srv.MobHealth(wither); after >= before {
		t.Errorf("an iron golem's blow left the wither at %v from %v", after, before)
	}
}

// TestAWitherAimsAtASpiderByItsEyeHeight: the centre head aims at half the
// target's eye height, so a skull at a spider passes well under the 0.9 up a
// player-shaped aim point would be.
// vanilla: WitherBoss.performRangedAttack
func TestAWitherAimsAtASpiderByItsEyeHeight(t *testing.T) {
	t.Parallel()
	srv, c, wither, spider := witherHunt(t, "minecraft:spider")
	aimUp, found := 0.0, false
	for range 100 {
		servertest.RunTicks(t, srv, 1)
		c.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlaySpawnEntity || found {
				return
			}
			r := protocol.NewReader(data)
			r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() != int32(protocol.EntityTypeWitherSkull) {
				return
			}
			x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
			vx, vy, vz := r.ReadLpVec3()
			wx, _, wz, _ := srv.MobPos(wither)
			if math.Hypot(x-wx, z-wz) > 0.5 {
				return // a side head's skull, 1.3 out from the body
			}
			cx, cy, cz, _ := srv.MobPos(spider)
			// The skull's line, followed to the spider's horizontal spot.
			aimUp, found = y+vy/math.Hypot(vx, vz)*math.Hypot(cx-x, cz-z)-cy, true
		})
		if found {
			break
		}
	}
	if !found {
		t.Fatal("no centre-head skull spawned at a spider within 100 ticks")
	}
	if aimUp > 0.6 {
		t.Errorf("skull aimed %v above the spider's feet, want about half its eye height (under 0.6)", aimUp)
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

// witherSpawnHeard runs a summoning to its blast with a second player in the
// Nether and a third far away in the overworld, and returns each one's
// level event 1023 (nil when none arrived) as (x, y, z, global).
func witherSpawnHeard(t *testing.T, global bool) (near, nether, far *[4]int32) {
	t.Helper()
	srv, c := witherScene(t, 2)
	srv.SetGameRule("global_sound_events", global)
	witherSummoned(t, srv, c)
	c.MoveTo(14.5, 64, 3.5, 0, 0)
	c.Settle()
	hearNether := servertest.DialServer(t, srv).Join("NetherHearer")
	hearFar := servertest.DialServer(t, srv).Join("FarHearer")
	srv.RunLine("execute in minecraft:the_nether run tp NetherHearer 8 40 8")
	srv.RunLine("execute in minecraft:overworld run tp FarHearer 200 64 200")
	c.Settle()
	hearNether.Settle()
	hearFar.Settle()
	for range 220 {
		srv.TickMobs(1)
	}
	read := func(c *servertest.Client) *[4]int32 {
		var got *[4]int32
		c.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlayWorldEvent {
				return
			}
			r := protocol.NewReader(data)
			if r.ReadI32() != protocol.WorldEventWitherSpawn {
				return
			}
			x, y, z := r.ReadPosition()
			r.ReadI32()
			flag := int32(0)
			if r.ReadBool() {
				flag = 1
			}
			got = &[4]int32{x, y, z, flag}
		})
		return got
	}
	return read(c), read(hearNether), read(hearFar)
}

// TestTheWitherSpawnSoundReachesEveryDimension: with global_sound_events on
// everyone hears level event 1023 flagged global, a player in the Nether at
// their own block; with it off only players within 64 blocks do.
// vanilla: ServerLevel.globalLevelEvent
func TestTheWitherSpawnSoundReachesEveryDimension(t *testing.T) {
	t.Parallel()
	t.Run("on", func(t *testing.T) {
		t.Parallel()
		near, nether, far := witherSpawnHeard(t, true)
		if near == nil || near[3] != 1 {
			t.Errorf("player beside the wither heard %v, want a global event", near)
		}
		if nether == nil || nether[3] != 1 || nether[0] != 8 || nether[1] != 40 || nether[2] != 8 {
			t.Errorf("Nether player heard %v, want a global event at 8 40 8", nether)
		}
		if far == nil || far[3] != 1 {
			t.Errorf("distant player heard %v, want a global event", far)
		}
	})
	t.Run("off", func(t *testing.T) {
		t.Parallel()
		near, nether, far := witherSpawnHeard(t, false)
		if near == nil {
			t.Error("player within 64 blocks heard nothing")
		}
		if nether != nil || far != nil {
			t.Errorf("rule off: Nether player heard %v, distant player %v, want none", nether, far)
		}
	})
}

// witherSideHeadsHit runs 100 ticks with a wither 10 blocks from a survival
// player, after setup, and reports whether a side head named the player.
func witherSideHeadsHit(t *testing.T, setup func(srv *server.Server)) bool {
	t.Helper()
	srv, wither, player := witherNear(t, 16.5, setup)
	for range 100 {
		servertest.RunTicks(t, srv, 1)
		if h := srv.MobWitherHeads(wither); h[1] == player || h[2] == player {
			return true
		}
	}
	return false
}

// TestWitherSideHeadsIgnoreInvisiblePlayer: a side head picks its target
// through targeting conditions, which cap the range by the target's
// visibility, so an invisible unarmoured player is out of reach at 10 blocks.
// vanilla: WitherBoss.customServerAiStep, TargetingConditions.test
func TestWitherSideHeadsIgnoreInvisiblePlayer(t *testing.T) {
	t.Parallel()
	if witherSideHeadsHit(t, func(srv *server.Server) {
		srv.RunLine("effect give " + witherBuilder + " minecraft:invisibility 1000 0 true")
	}) {
		t.Error("a side head targeted the invisible player")
	}
	if !witherSideHeadsHit(t, func(*server.Server) {}) {
		t.Error("no side head targeted the visible player within 100 ticks")
	}
}

// TestAWitherSpawnBlastLeavesItsSourceUnhurtAndInPlace: the blast of a
// summoning excludes the wither itself, so it keeps full health and does not
// drift.
// vanilla: ServerExplosion.hurtEntities, WitherBoss.customServerAiStep
func TestAWitherSpawnBlastLeavesItsSourceUnhurtAndInPlace(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), witherBuilder)
	c.Settle()
	srv.SetSpawning(false)
	srv.SetDifficulty(2)
	srv.SetPlayerGameMode(witherBuilder, 1)
	servertest.HoldTicks(t, srv)
	srv.RunLine("summon minecraft:wither 6.5 64 6.5 {Invul:2}")
	servertest.RunTicks(t, srv, 10)
	query := func(path string) string {
		var out []string
		for _, text := range srv.RunLine("data get entity @e[type=wither,limit=1] " + path) {
			for _, arg := range text.With {
				out = append(out, arg.Plain())
			}
		}
		return strings.Join(out, " ")
	}
	health := query("Health")
	if !strings.Contains(health, "300.0f") {
		t.Errorf("Health after the blast = %q, want 300.0f", health)
	}
	pos := query("Pos")
	if !strings.Contains(pos, "6.5d") || strings.Count(pos, "6.5d") != 2 {
		t.Errorf("Pos after the blast = %q, want x and z still 6.5", pos)
	}
}
