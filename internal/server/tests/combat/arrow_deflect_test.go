package combat

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"

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
