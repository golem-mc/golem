package combat

import (
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// The refusals an arrow can meet, and what each one is to leave behind:
// the creative player a refused hurt deflects, the mob inside the window
// an earlier blow opened, the spectator a projectile passes straight
// through. The arrow is watched from both ends of every shot — the one
// who fired it and the one it was fired at — through the wire, the way
// the shots of the skeleton tests are.

// deflectionWatch is what one client has seen of one arrow: whether a
// motion packet flew it back the way it came, whether its metadata said
// it lay in the ground, whether it went out of the world, and whether a
// blow landed on the body it was aimed at.
type deflectionWatch struct {
	c       *servertest.Client
	arrowID int32
	bodyID  int32

	bounced bool
	landed  bool
	removed bool
	hurt    bool
}

// record reads one packet into the watch, stepping over the traffic that
// is not the arrow's or the body's.
func (w *deflectionWatch) record(id int32, data []byte) {
	switch id {
	case protocol.IDPlaySetEntityMotion:
		if r := protocol.NewReader(data); r.ReadVarInt() == w.arrowID {
			if vx, _, _ := r.ReadLpVec3(); vx > 0.1 {
				w.bounced = true
			}
		}
	case protocol.IDPlayEntityMetadata:
		if r := protocol.NewReader(data); r.ReadVarInt() == w.arrowID {
			w.landed = w.landed || servertest.ReadArrowMetadata(w.c, data).InGround
		}
	case protocol.IDPlayRemoveEntities:
		if slices.Contains(servertest.RemovedIDs(w.c.T, data), w.arrowID) {
			w.removed = true
		}
	case protocol.IDPlayDamageEvent:
		if protocol.NewReader(data).ReadVarInt() == w.bodyID {
			w.hurt = true
		}
	case protocol.IDPlaySetHealth:
		if w.c.PlayerEntityID == w.bodyID && protocol.NewReader(data).ReadF32() < 20 {
			w.hurt = true
		}
	}
}

// deflectionSlice is the ticks one client's traffic is read for before the
// next client's, so each client's keep-alives are answered while another's
// traffic is being read.
const deflectionSlice = 6

// watchDeflection runs up to ticks ticks in slices, recording every packet
// into every watch on the client it arrived at, until done says the
// watching is over.
func watchDeflection(t *testing.T, srv *server.Server, ticks int, done func() bool, watches ...*deflectionWatch) {
	t.Helper()
	var clients []*servertest.Client
	for _, w := range watches {
		if !slices.Contains(clients, w.c) {
			clients = append(clients, w.c)
		}
	}
	for range (ticks + deflectionSlice - 1) / deflectionSlice {
		if done != nil && done() {
			return
		}
		for _, c := range clients {
			c.ReadTicks(srv, deflectionSlice, func(id int32, data []byte) bool {
				for _, w := range watches {
					if w.c == c {
						w.record(id, data)
					}
				}
				return false
			})
		}
	}
}

// TestAnArrowBouncesOffACreativePlayer shoots a creative player: the hurt
// is refused, and the arrow deflects back the way it came — a motion
// packet reversing its flight, and both clients keeping the shaft alive
// all the way to the ground. A refused hit may leave nothing pending: the
// victim switching to survival long afterwards must not take the blow
// then.
func TestAnArrowBouncesOffACreativePlayer(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "Shooter"))
	shooter := connect()
	shooter.JoinAs("Shooter")
	shooter.MoveToGround(5.5, 64, 0.5, true)

	victim := connect()
	victimID := victim.JoinAs("Victim")
	victim.MoveToGround(0.5, 64, 0.5, true)
	shooter.Settle()
	victim.Settle()
	servertest.HoldTicks(t, srv)

	shooter.ChatCommand("gamemode creative Victim")
	shooter.Settle()
	servertest.UntilTicks(t, srv, 1200, "condition", func() bool {
		return srv.PlayerGameMode("Victim") == protocol.GameModeCreative
	})
	shooter.Settle()
	victim.Settle()

	// The arrow starts short of the shooter, flies level at the victim's
	// chest and meets them three blocks in.
	arrowID := srv.ShootArrow(4.5, 65, 0.5, -2, 0, 0)
	shot := &deflectionWatch{c: shooter, arrowID: arrowID, bodyID: victimID}
	struck := &deflectionWatch{c: victim, arrowID: arrowID, bodyID: victimID}
	watchDeflection(t, srv, servertest.SkeletonTicks, func() bool {
		return shot.bounced && shot.landed && struck.bounced && struck.landed
	}, shot, struck)

	if !shot.bounced || !struck.bounced {
		t.Error("the arrow refused at the creative player never flew back the way it came for both to see")
	}
	if !shot.landed || !struck.landed {
		t.Fatal("the arrow refused at the creative player never came clear of them to land")
	}
	if shot.removed || struck.removed {
		t.Error("the arrow refused at the creative player was taken out of the world")
	}
	if shot.hurt || struck.hurt {
		t.Fatal("a blow landed on the creative player the arrow was refused at")
	}

	// The victim turns hurtable long after the refusal. The blow may not
	// arrive then.
	shooter.ChatCommand("gamemode survival Victim")
	shooter.Settle()
	servertest.UntilTicks(t, srv, 1200, "condition", func() bool {
		return srv.PlayerGameMode("Victim") == protocol.GameModeSurvival
	})
	lateShot := &deflectionWatch{c: shooter, arrowID: arrowID, bodyID: victimID}
	lateStruck := &deflectionWatch{c: victim, arrowID: arrowID, bodyID: victimID}
	watchDeflection(t, srv, 60, nil, lateShot, lateStruck)
	if lateShot.hurt || lateStruck.hurt {
		t.Error("the refused arrow's blow landed once the victim turned hurtable")
	}
	if lateShot.removed || lateStruck.removed {
		t.Error("the refused arrow was taken out of the world once the victim turned hurtable")
	}
	if !srv.ArrowAlive(arrowID) {
		t.Error("the deflected arrow is not in the world any more")
	}
}

// TestAnArrowPassesThroughASpectator shoots a spectator, whom no projectile
// can meet at all: the flight pays their body no mind — no deflection,
// no blow — and lands beyond them with the shaft still in the world.
func TestAnArrowPassesThroughASpectator(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "Shooter"))
	shooter := connect()
	shooter.JoinAs("Shooter")
	shooter.MoveToGround(5.5, 64, 0.5, true)

	victim := connect()
	victimID := victim.JoinAs("Victim")
	victim.MoveToGround(0.5, 64, 0.5, true)
	shooter.Settle()
	victim.Settle()

	shooter.ChatCommand("gamemode spectator Victim")
	shooter.Settle()
	servertest.UntilTicks(t, srv, 1200, "condition", func() bool {
		return srv.PlayerGameMode("Victim") == protocol.GameModeSpectator
	})
	shooter.Settle()
	victim.Settle()

	arrowID := srv.ShootArrow(4.5, 65, 0.5, -2, 0, 0)
	shot := &deflectionWatch{c: shooter, arrowID: arrowID, bodyID: victimID}
	struck := &deflectionWatch{c: victim, arrowID: arrowID, bodyID: victimID}
	watchDeflection(t, srv, servertest.SkeletonTicks, func() bool {
		return shot.landed && struck.landed
	}, shot, struck)

	if shot.bounced || struck.bounced {
		t.Error("the arrow was deflected off a spectator no projectile can meet")
	}
	if !shot.landed || !struck.landed {
		t.Fatal("the arrow never flew through the spectator to land beyond them")
	}
	if shot.removed || struck.removed {
		t.Error("the arrow flown through the spectator was taken out of the world")
	}
	if shot.hurt || struck.hurt {
		t.Error("a blow landed on a spectator no projectile can meet")
	}
	if !srv.ArrowAlive(arrowID) {
		t.Error("the arrow flown through the spectator is not in the world any more")
	}
}

// TestASecondArrowRefusedByAMobsHurtWindowBouncesOff shoots a mob with two
// arrows of one tick: the first lands and opens the half-second window no
// next blow may pass, the second is refused — and rather than lying about
// the body until the window closes, it deflects off and lands beside it,
// still in the world.
func TestASecondArrowRefusedByAMobsHurtWindowBouncesOff(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), "Watcher")
	c.MoveToGround(5.5, 64, 0.5, true)
	c.Settle()

	cowID := srv.SpawnMob("minecraft:cow", 0.5, 64, 0.5)
	c.Settle()
	srv.MoveMob(cowID, 0.5, 0.5)
	landed := srv.ShootArrow(4.5, 64.5, 0.5, -2, 0, 0)
	refused := srv.ShootArrow(4.6, 64.5, 0.5, -2, 0, 0)

	hit := &deflectionWatch{c: c, arrowID: landed, bodyID: cowID}
	off := &deflectionWatch{c: c, arrowID: refused, bodyID: cowID}
	watchDeflection(t, srv, servertest.SkeletonTicks, func() bool {
		return hit.removed && off.bounced && off.landed
	}, hit, off)

	if !hit.removed {
		t.Fatal("the first arrow never hurt the mob, so the window this test is about never opened")
	}
	if !off.bounced {
		t.Error("the arrow the mob's hurt window refused never flew back the way it came")
	}
	if !off.landed {
		t.Error("the arrow the mob's hurt window refused never came clear of the body to land")
	}
	if off.removed {
		t.Error("the arrow the mob's hurt window refused was taken into the body once the window closed")
	}
	if srv.ArrowAlive(landed) {
		t.Error("the arrow that hurt the mob is still in the world")
	}
	if !srv.ArrowAlive(refused) {
		t.Error("the arrow the mob's hurt window refused is not in the world any more")
	}
}

// TestATeleportInsideARefusedHitRacesTheDeflection fires a /tp of the arrow
// from inside the window a refused hurt leaves before the tick's
// deflection — a command from another connection landing in the window,
// which nothing outside a hook holds open in production. The teleport runs
// on the shooter connection's own goroutine while the tick goes on to
// deflect the same shaft, so under -race it is the arrow lock the
// deflection holds that keeps the two writes clean. Whichever of the two
// wins, the deflection over a teleported-out velocity steps the shaft
// nothing, so it ends where the teleport put it either way.
func TestATeleportInsideARefusedHitRacesTheDeflection(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "Shooter"))
	shooter := connect()
	shooter.JoinAs("Shooter")
	shooter.MoveToGround(5.5, 64, 0.5, true)

	victim := connect()
	victim.JoinAs("Victim")
	victim.MoveToGround(0.5, 64, 0.5, true)
	shooter.Settle()
	victim.Settle()

	shooter.ChatCommand("gamemode creative Victim")
	servertest.UntilTicks(t, srv, 1200, "condition", func() bool {
		return srv.PlayerGameMode("Victim") == protocol.GameModeCreative
	})
	shooter.Settle()
	victim.Settle()

	var refused atomic.Bool
	srv.RefusalBetween(func() {
		refused.Store(true)
		shooter.ChatCommand("tp @e[type=minecraft:arrow] 12.5 70 5.5")
		// The window stays open through the teleport's own write — the
		// straddle the race is about — so the next tick's own lock-taking
		// cannot quietly order what happened inside it.
		servertest.PollUntil(t, servertest.ExpectTimeout, func() bool {
			for _, a := range srv.ArrowsInDimension(world.DimensionName) {
				if a.X == 12.5 {
					return true
				}
			}
			return false
		})
	})
	arrowID := srv.ShootArrow(4.5, 65, 0.5, -2, 0, 0)

	servertest.UntilTicks(t, srv, 1200, "condition", func() bool { return refused.Load() })

	// The teleport went through and the tracker tells the watchers: the
	// shaft is told standing over the spot the command in the window sent it
	// to, not in the body it was refused at. It falls on the way, its height
	// being whatever the next movement tick finds.
	data := victim.ExpectWithinTicks(srv, 60, protocol.IDPlayTeleportEntity)
	r := protocol.NewReader(data)
	if got := r.ReadVarInt(); got != arrowID {
		t.Fatalf("teleport entity = %d, want the refused shaft's %d", got, arrowID)
	}
	r.ReadVarInt() // position path
	if x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64(); x != 12.5 || z != 5.5 || y > 70 {
		t.Fatalf("the refused shaft was teleported to (%v, %v, %v), want (12.5, at most 70, 5.5)", x, y, z)
	}
	if !srv.ArrowAlive(arrowID) {
		t.Error("the refused shaft is gone from the world")
	}
}
