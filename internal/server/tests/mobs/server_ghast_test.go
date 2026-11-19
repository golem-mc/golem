package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

const ghastPlayer = "Ghastly"

// ghastWorld stands a player in a flat world with the loop stopped and a
// ghast dist blocks east of them, and returns the ghast's entity ID with its
// spawn packet read.
func ghastWorld(t *testing.T, dist float64) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	return ghastWorldAt(t, dist, 0)
}

// ghastWorldAt is ghastWorld with the ghast rise blocks above the player.
func ghastWorldAt(t *testing.T, dist, rise float64) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), ghastPlayer)
	c.Settle()
	srv.StopTick()
	srv.SetSpawning(false)
	srv.SetDifficulty(2)
	srv.SetGameRule("mob_griefing", false)
	pos := srv.PlayerPosition(ghastPlayer)
	id := srv.SpawnMob("minecraft:ghast", pos.X+dist, pos.Y+rise, pos.Z)
	srv.TickMobs(1)
	typeID, _ := registry.EntityTypeID("minecraft:ghast")
	if got := c.ExpectMobSpawn(typeID); got.EntityID != id {
		t.Fatalf("the spawn packet names entity %d, want %d", got.EntityID, id)
	}
	return srv, c, id
}

// ghastFlight is what a run of ticks showed of one ghast.
type ghastFlight struct {
	fireballs  []int32 // the entity ID of each large fireball, in order
	firedAt    []int   // the tick of each
	chargingAt int     // the first tick the charging flag was told, -1 for none
	clearedAt  int     // the tick it was told cleared after that, -1 for none
	meta       servertest.SpawnedMob
}

// run ticks the world until stop answers true or limit ticks pass.
func (f *ghastFlight) run(t *testing.T, srv *server.Server, c *servertest.Client, id int32, limit int, stop func(tick int) bool) {
	t.Helper()
	typeID, _ := registry.EntityTypeID("minecraft:ghast")
	f.meta = servertest.SpawnedMob{EntityID: id, TypeID: typeID}
	f.chargingAt, f.clearedAt = -1, -1
	for tick := range limit {
		srv.TickMobs(1)
		servertest.DriveTargetTicks(srv, 1)
		c.SettleAndCollect(func(pid int32, data []byte) {
			switch pid {
			case protocol.IDPlaySpawnEntity:
				r := protocol.NewReader(data)
				eid := r.ReadVarInt()
				r.ReadUUID()
				if r.ReadVarInt() == int32(protocol.EntityTypeFireball) {
					f.fireballs = append(f.fireballs, eid)
					f.firedAt = append(f.firedAt, tick)
				}
			case protocol.IDPlayEntityMetadata:
				if protocol.NewReader(data).ReadVarInt() != id {
					return
				}
				c.ReadMobMetadataInto(&f.meta, data)
				if f.meta.GhastCharging && f.chargingAt < 0 {
					f.chargingAt = tick
				}
				if !f.meta.GhastCharging && f.chargingAt >= 0 && f.clearedAt < 0 {
					f.clearedAt = tick
				}
			}
		})
		if stop != nil && stop(tick) {
			return
		}
	}
}

// TestAGhastChargesThenShootsALargeFireballEverySixtyTicks: a ghast that sees
// a player charges (the flag is told from the eleventh tick), lets go of a
// large fireball on the twentieth and clears the flag; the next follows after
// the forty-tick pause.
// vanilla: Ghast.GhastShootFireballGoal.tick
func TestAGhastChargesThenShootsALargeFireballEverySixtyTicks(t *testing.T) {
	t.Parallel()
	srv, c, id := ghastWorld(t, 12)
	var f ghastFlight
	f.run(t, srv, c, id, 200, func(int) bool { return len(f.fireballs) >= 2 })
	if len(f.fireballs) < 2 {
		t.Fatalf("%d large fireballs in 200 ticks, want two", len(f.fireballs))
	}
	if gap := f.firedAt[1] - f.firedAt[0]; gap != 60 {
		t.Errorf("fireballs at ticks %v, want 60 apart", f.firedAt)
	}
	if f.chargingAt < 0 || f.firedAt[0]-f.chargingAt < 8 || f.firedAt[0]-f.chargingAt > 10 {
		t.Errorf("charging told at %d, first fireball at %d, want the flag 9 ticks ahead", f.chargingAt, f.firedAt[0])
	}
	if f.clearedAt != f.firedAt[0] {
		t.Errorf("charging cleared at %d, want the tick of the launch %d", f.clearedAt, f.firedAt[0])
	}
}

// TestAGhastFireballExplodesOnThePlayer: the fireball reaches the player
// standing still and the blast hurts them.
// vanilla: LargeFireball.onHit
func TestAGhastFireballExplodesOnThePlayer(t *testing.T) {
	t.Parallel()
	srv, c, id := ghastWorld(t, 12)
	var f ghastFlight
	f.run(t, srv, c, id, 400, func(int) bool { return srv.PlayerHealth(ghastPlayer) < 20 })
	if srv.PlayerHealth(ghastPlayer) >= 20 {
		t.Fatalf("the player was never hurt in 400 ticks; %d fireballs", len(f.fireballs))
	}
}

// TestAPunchedGhastFireballKillsTheGhast: a player who turns the fireball
// back at the ghast kills it, whatever its ten health.
// vanilla: Ghast.hurtServer
func TestAPunchedGhastFireballKillsTheGhast(t *testing.T) {
	t.Parallel()
	srv := punchedFireballKill(t)
	// The player's hand is empty: the blast kill still names the
	// player as attacker and the fireball as direct attacker.
	for range 40 {
		srv.TickEntities(1)
		srv.TickMobs(1)
	}
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item == "minecraft:music_disc_tears" {
			return
		}
	}
	t.Error("the ghast killed by a bare-handed player's turned-back fireball dropped no music_disc_tears")
}

// TestAPunchedGhastFireballObtainsReturnToSender: the kill by a fireball the
// player turned back is a player_killed_entity whose killing blow came from a
// fireball.
// vanilla: Ghast.hurtServer, DamageSourcePredicate
func TestAPunchedGhastFireballObtainsReturnToSender(t *testing.T) {
	t.Parallel()
	srv := punchedFireballKill(t)
	srv.TickMobs(2)
	if !srv.AdvancementObtained(ghastPlayer, "minecraft:nether/return_to_sender", "killed_ghast") {
		t.Error("killing a ghast with its turned-back fireball did not obtain return_to_sender")
	}
}

// TestAGhastKilledByHandDoesNotObtainReturnToSender: the same ghast killed
// without a fireball is a kill but not the fireball's.
func TestAGhastKilledByHandDoesNotObtainReturnToSender(t *testing.T) {
	t.Parallel()
	srv, c, id := ghastWorld(t, 2)
	srv.SetMobNoAI(id)
	srv.SetMobHealth(id, 1)
	pos := srv.PlayerPosition(ghastPlayer)
	c.MoveTo(pos.X, pos.Y, pos.Z, -90, 0)
	c.Attack(id)
	c.Settle()
	for range 5 {
		srv.TickMobs(1)
	}
	if health, ok := srv.MobHealth(id); ok && health > 0 {
		t.Fatalf("the ghast survived the hit with %v health", health)
	}
	if srv.AdvancementObtained(ghastPlayer, "minecraft:nether/return_to_sender", "killed_ghast") {
		t.Error("a ghast killed by hand obtained return_to_sender")
	}
}

// punchedFireballKill has the player turn a ghast's fireball back at it and
// returns once the ghast is dead.
func punchedFireballKill(t *testing.T) *server.Server {
	t.Helper()
	srv, c, id := ghastWorld(t, 6.5)
	pos := srv.PlayerPosition(ghastPlayer)
	var f ghastFlight
	f.run(t, srv, c, id, 200, func(int) bool { return len(f.fireballs) > 0 })
	if len(f.fireballs) == 0 {
		t.Fatal("the ghast never shot")
	}
	// The ghast drifts while it charges: turn to face where it is now.
	gx, gy, gz, _ := srv.MobPos(id)
	pos = srv.PlayerPosition(ghastPlayer)
	dx, dy, dz := gx-pos.X, gy+2-(pos.Y+1.62), gz-pos.Z
	yaw := math.Atan2(-dx, dz) * 180 / math.Pi
	pitch := -math.Atan2(dy, math.Hypot(dx, dz)) * 180 / math.Pi
	c.MoveTo(pos.X, pos.Y, pos.Z, float32(yaw), float32(pitch))
	c.Settle()
	c.Attack(f.fireballs[0])
	c.Settle()
	for range 40 {
		srv.TickMobs(1)
		servertest.DriveTargetTicks(srv, 1)
		if health, ok := srv.MobHealth(id); !ok || health <= 0 {
			return srv
		}
	}
	health, _ := srv.MobHealth(id)
	t.Fatalf("the ghast has %v health 40 ticks after its fireball was turned back", health)
	return srv
}

// TestAGhastFloatsInTheAirWithoutFallingAndIgnoresAPlayerFarAbove: a ghast
// with nobody within four blocks of its height drifts to points near it,
// keeps its height and shoots nothing.
// vanilla: Ghast.RandomFloatAroundGoal, Ghast.GhastMoveControl, Ghast.registerGoals
func TestAGhastFloatsInTheAirWithoutFallingAndIgnoresAPlayerFarAbove(t *testing.T) {
	t.Parallel()
	srv, c, id := ghastWorldAt(t, 20, 30)
	x0, y0, z0, _ := srv.MobPos(id)
	var f ghastFlight
	f.run(t, srv, c, id, 300, nil)
	x, y, z, ok := srv.MobPos(id)
	if !ok {
		t.Fatal("the ghast is gone")
	}
	if y < y0-20 {
		t.Errorf("the ghast fell from %v to %v", y0, y)
	}
	if x == x0 && y == y0 && z == z0 {
		t.Error("the ghast never moved")
	}
	if len(f.fireballs) != 0 {
		t.Errorf("the ghast shot %d fireballs at a player 30 blocks below", len(f.fireballs))
	}
}

// TestAGhastWithNowhereToGoDipsTowardTheGroundInsteadOfClimbing: a ghast whose
// home is out of reach has every draw refused, and the fresh point it falls
// back on over the flat ground is mirrored below the ghast, so over a long
// flight it never climbs above where it started.
// vanilla: Ghast.RandomFloatAroundGoal.getSuitableFlyToPosition
func TestAGhastWithNowhereToGoDipsTowardTheGroundInsteadOfClimbing(t *testing.T) {
	t.Parallel()
	srv, c, id := ghastWorldAt(t, 40, 10)
	_, start, _, _ := srv.MobPos(id)
	if !srv.RestrictMob(id, 5000, int32(start), 5000, 1) {
		t.Fatal("the ghast was not found to restrict")
	}
	var f ghastFlight
	peak, lowest := start, start
	f.run(t, srv, c, id, 400, func(int) bool {
		if _, y, _, ok := srv.MobPos(id); ok {
			peak, lowest = max(peak, y), min(lowest, y)
		}
		return false
	})
	if peak > start+0.5 {
		t.Errorf("the ghast climbed to y=%.1f from %.1f, want it kept at or below its start", peak, start)
	}
	if lowest > start-1 {
		t.Errorf("the ghast never dipped below y=%.1f (lowest %.1f), want it to drift down toward the ground", start, lowest)
	}
}
