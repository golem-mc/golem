package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

const blazePlayer = "Singed"

// blazeWorld stands a player in a flat world with the loop stopped and a
// blaze ten blocks east of them, and returns the blaze's entity ID with its
// spawn packet read.
func blazeWorld(t *testing.T) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), blazePlayer)
	c.Settle()
	srv.StopTick()
	srv.SetSpawning(false)
	srv.SetDifficulty(2)
	pos := srv.PlayerPosition(blazePlayer)
	id := srv.SpawnMob("minecraft:blaze", pos.X+10, pos.Y, pos.Z)
	srv.TickMobs(1)
	typeID, _ := registry.EntityTypeID("minecraft:blaze")
	if got := c.ExpectMobSpawn(typeID); got.EntityID != id {
		t.Fatalf("the spawn packet names entity %d, want %d", got.EntityID, id)
	}
	return srv, c, id
}

// blazeFlight is what a run of ticks showed of one blaze.
type blazeFlight struct {
	fireballs []int // the tick of each small fireball's arrival
	chargedAt int   // the first tick the charged flag was told, -1 for none
	clearedAt int   // the tick it was told cleared after that, -1 for none
	hurtAt    int
	meta      servertest.SpawnedMob
}

// run ticks the world up to limit ticks, stopping once the player is hurt
// unless keepGoing.
func (f *blazeFlight) run(t *testing.T, srv *server.Server, c *servertest.Client, id int32, limit int, keepGoing bool) {
	t.Helper()
	smallFireball := protocol.EntityTypeSmallFireball
	typeID, _ := registry.EntityTypeID("minecraft:blaze")
	f.meta = servertest.SpawnedMob{EntityID: id, TypeID: typeID}
	f.chargedAt, f.clearedAt, f.hurtAt = -1, -1, -1
	for tick := range limit {
		srv.TickMobs(1)
		servertest.DriveTargetTicks(srv, 1)
		c.SettleAndCollect(func(pid int32, data []byte) {
			switch pid {
			case protocol.IDPlaySpawnEntity:
				r := protocol.NewReader(data)
				r.ReadVarInt()
				r.ReadUUID()
				if r.ReadVarInt() == int32(smallFireball) {
					f.fireballs = append(f.fireballs, tick)
				}
			case protocol.IDPlayEntityMetadata:
				if protocol.NewReader(data).ReadVarInt() != id {
					return
				}
				c.ReadMobMetadataInto(&f.meta, data)
				if f.meta.BlazeCharged && f.chargedAt < 0 {
					f.chargedAt = tick
				}
				if !f.meta.BlazeCharged && f.chargedAt >= 0 && f.clearedAt < 0 {
					f.clearedAt = tick
				}
			}
		})
		if srv.PlayerHealth(blazePlayer) < 20 && !keepGoing {
			f.hurtAt = tick
			return
		}
	}
}

// TestABlazeShootsABurstOfThreeFireballs: a blaze that sees a player in range
// charges (the flag is told), then lets go of three small fireballs six
// ticks apart and clears the flag; the next burst follows its hundred-tick
// pause.
// vanilla: Blaze.BlazeAttackGoal.tick
func TestABlazeShootsABurstOfThreeFireballs(t *testing.T) {
	t.Parallel()
	srv, c, id := blazeWorld(t)
	srv.SetGameRule("mob_griefing", false)
	var f blazeFlight
	f.run(t, srv, c, id, 260, true)
	if len(f.fireballs) < 3 {
		t.Fatalf("%d fireballs in 260 ticks, want a burst of three", len(f.fireballs))
	}
	if f.fireballs[1]-f.fireballs[0] != 6 || f.fireballs[2]-f.fireballs[1] != 6 {
		t.Errorf("fireballs at %v, want gaps of 6", f.fireballs[:3])
	}
	if f.chargedAt < 0 || f.fireballs[0]-f.chargedAt < 55 || f.fireballs[0]-f.chargedAt > 65 {
		t.Errorf("charged at %d, first fireball at %d, want about 60 ticks between", f.chargedAt, f.fireballs[0])
	}
	// Six ticks to the burst's end, the 100-tick pause, then a 60-tick charge.
	if len(f.fireballs) < 4 || f.fireballs[3]-f.fireballs[2] != 166 {
		t.Errorf("fireballs at %v, want the next burst 166 ticks after the third fireball", f.fireballs)
	}
}

// TestABlazesFireballHurtsAndIgnitesThePlayer: the burst's fireballs reach
// the player standing still ten blocks off, hurt them and set them alight.
// vanilla: SmallFireball.onHitEntity
func TestABlazesFireballHurtsAndIgnitesThePlayer(t *testing.T) {
	t.Parallel()
	srv, c, id := blazeWorld(t)
	srv.SetGameRule("mob_griefing", false)
	var f blazeFlight
	f.run(t, srv, c, id, 1500, false)
	if f.hurtAt < 0 {
		t.Fatalf("the player was never hurt in 1500 ticks; %d fireballs", len(f.fireballs))
	}
	if srv.PlayerFire(blazePlayer) <= 0 {
		t.Error("the player is not burning")
	}
}

// TestABlazeTakesDamageStandingInWater: rain or water hurts a blaze one point a tick.
// vanilla: Blaze.isSensitiveToWater
func TestABlazeTakesDamageStandingInWater(t *testing.T) {
	t.Parallel()
	srv, c, id := blazeWorld(t)
	_ = c
	pos := srv.PlayerPosition(blazePlayer)
	water := servertest.MustState(t, "minecraft:water", nil)
	srv.SetBlock(int32(pos.X)+10, int32(pos.Y), int32(pos.Z), water)
	before, _ := srv.MobHealth(id)
	srv.TickMobs(10)
	after, _ := srv.MobHealth(id)
	if after >= before {
		t.Errorf("the blaze health went %v to %v in water, want a loss", before, after)
	}
}

// TestANoAIBlazeStillTakesWaterDamage: the water bite runs whatever the AI
// does.
// vanilla: LivingEntity.aiStep
func TestANoAIBlazeStillTakesWaterDamage(t *testing.T) {
	t.Parallel()
	srv, _, id := summonInPool(t, "blaze")
	before, _ := srv.MobHealth(id)
	srv.TickMobs(3)
	after, _ := srv.MobHealth(id)
	if after >= before {
		t.Errorf("the NoAI blaze health went %v to %v in water, want a loss", before, after)
	}
}
