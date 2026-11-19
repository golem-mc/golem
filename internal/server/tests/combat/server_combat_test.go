package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The combat tests: the ways a fight went wrong that a player report
// brought — the right-click that struck, the sword that swung at full force
// however recently it had swung, the bodies that stand inside each other
// unmoved, the zombie that ran twice its legs' pace, and the climb that
// jumped into the ceiling. Each drives a real connection the way the mob
// and hostile tests do, against the flat world the harness grows, where
// every mob is one the test planted.

// combatTestConfig is the configuration the combat tests run in: survival,
// the mode a fight makes sense in.
func combatTestConfig() *config.Config {
	cfg := config.Default()
	cfg.OnlineMode = false
	return cfg
}

// swordDamage is what one whole swing of the diamond sword carries: the
// fist's one plus the blade's own.
func swordDamage() float64 {
	sword, ok := registry.ItemID("minecraft:diamond_sword")
	if !ok {
		panic("the item registry has no diamond sword")
	}
	return 1.0 + float64(registry.AttackDamage(sword))
}

// TestARightClickNeverStrikes right-clicks a cow as a client
// does — the Interact packet's record of an entity, a hand, the point on
// the body the cursor landed on, and the sneak key, with no kind of its own
// left in it — first with the main hand and then with the off hand, and
// holds that neither click drew blood: no damage event, no flinch, no shove
// answering either click. The strike is the Attack packet's alone, and the
// sword's own blow, swung recovered, takes the cow from its untouched ten
// down by the blade's whole force — the seven a struck-before cow could not
// have lost.
func TestARightClickNeverStrikes(t *testing.T) {
	t.Parallel()
	srv, c, attacker := servertest.JoinAs(t, combatTestConfig(), "Clicker")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	servertest.HoldTicks(t, srv)
	mobID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	for _, hand := range []int32{protocol.HandMain, protocol.HandOff} {
		c.Send(protocol.IDPlayInteractIn, func(b *protocol.Buffer) {
			b.PutVarInt(mobID)
			b.PutVarInt(hand)
			b.PutLpVec3(0.2, 1.1, -0.1)
			b.PutBool(false)
		})
	}
	c.Settle()

	// Nothing a landed blow would have told the dimension arrives behind
	// the clicks.
	c.ReadTicks(srv, 10, func(gotID int32, data []byte) bool {
		switch gotID {
		case protocol.IDPlayDamageEvent, protocol.IDPlayHurtAnimation,
			protocol.IDPlayEntityEvent:
			c.T.Fatalf("a right-click was answered with packet 0x%02X (% X)", gotID, data)
		case protocol.IDPlaySetEntityMotion:
			// A shove lifts the body; a cow settling to the floor only falls.
			r := protocol.NewReader(data)
			r.ReadVarInt()
			if _, vy, _ := r.ReadLpVec3(); vy > 0 {
				c.T.Fatalf("a right-click shoved the cow (% X)", data)
			}
		}
		return false
	})

	srv.GiveSlot("Clicker", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:diamond_sword"), Count: 1,
	})
	awaitFullSwing(t, srv, "Clicker")
	if got := c.Strike(mobID, attacker); math.Abs(float64(got)-(10-swordDamage())) > 0.01 {
		c.T.Errorf("the cow's health after the clicks and one whole blow = %v, want %v",
			got, 10-swordDamage())
	}
}

// TestASwingRecoversBeforeItsNextBlow walks the attack-strength cooldown
// the way a player's hand lives it: a sword swung the moment it arrives
// lands well under half its force, the same sword given its recovery lands
// whole, a swing of the arm spends the recovery again, and so does the
// blade leaving the hand for another.
func TestASwingRecoversBeforeItsNextBlow(t *testing.T) {
	t.Parallel()
	srv, c, attacker := servertest.JoinAs(t, combatTestConfig(), "Duelist")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	// Each hurried blow is struck with the live loop stopped, so no tick
	// of recovery slips in before it however slow the machine is: the one
	// tick that sees the new blade in the hand is run by hand. The loop
	// runs again for the recoveries, which wait on the ticker itself.
	srv.StopTick()
	srv.GiveSlot("Duelist", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:diamond_sword"), Count: 1,
	})
	srv.TickSurvival()
	full := swordDamage()

	// The first blow, swung before the recovery the sword's arrival spent:
	// the strength scale holds it under half the blade's force.
	afterFirst := c.StrikeFresh(srv, 5.5, 5.5, attacker)
	if got, floor := afterFirst, float32(10-full*0.5); got < floor {
		c.T.Errorf("health after the hurried swing = %v, want at least %v (the blow's %v of the whole %v)",
			got, floor, 10-got, full)
	}
	if afterFirst >= 10 {
		c.T.Errorf("the hurried swing landed nothing at all: health %v", afterFirst)
	}

	// The second, after the cooldown has run: the blade's whole force, to
	// the point.
	awaitRecovery(t, srv)
	afterSecond := c.StrikeFresh(srv, 6.5, 4.5, attacker)
	if got, want := afterSecond, float32(10-full); math.Abs(float64(got-want)) > 0.01 {
		c.T.Errorf("health after the recovered swing = %v, want %v (the whole %v off a full ten)", got, want, full)
	}

	// The third, right after a swing of the arm: the swing spent the
	// recovery, and the blow that follows it lands short again.
	awaitRecovery(t, srv)
	srv.StopTick()
	c.Swing()
	afterThird := c.StrikeFresh(srv, 7.5, 5.5, attacker)
	if got, floor := afterThird, float32(10-full*0.5); got < floor {
		c.T.Errorf("health after the swing's blow = %v, want at least %v (the blow held under half)", got, floor)
	}

	// The fourth, right after the blade left the hand for another: the
	// change spends the recovery the same way. The iron blade's whole
	// force is the fist's one plus what the registry says it adds, and a
	// hurried one well under half of that.
	iron := servertest.MustItemID(t, "minecraft:iron_sword")
	ironFull := 1.0 + float64(registry.AttackDamage(iron))
	srv.GiveSlot("Duelist", player.HotbarSlot, protocol.ItemStack{ItemID: iron, Count: 1})
	srv.TickSurvival()
	afterFourth := c.StrikeFresh(srv, 8.5, 4.5, attacker)
	if got, floor := afterFourth, float32(10-ironFull*0.5); got < floor {
		c.T.Errorf("health after the swapped blade's hurried blow = %v, want at least %v", got, floor)
	}

	// And the fifth, recovered: the iron blade's whole force. The target
	// stands within the attack's reach of the attacker: vanilla refuses a
	// blow past the reach, and the row of targets outruns any one
	// standing.
	awaitRecovery(t, srv)
	afterFifth := c.StrikeFresh(srv, 8.0, 5.5, attacker)
	if got, want := afterFifth, float32(10-ironFull); math.Abs(float64(got-want)) > 0.01 {
		c.T.Errorf("health after the recovered iron blow = %v, want %v", got, want)
	}
}

// awaitRecovery runs the live loop until the duelist's swing has wound past a
// sword's whole recovery: 1.6 attacks a second is a period of twelve and a
// half ticks, and fifteen leaves a margin. Waiting on the ticker rather
// than the clock keeps a slow machine's late ticks from landing the blow
// early.
func awaitRecovery(t *testing.T, srv *server.Server) {
	t.Helper()
	srv.StartTick()
	servertest.UntilTicks(t, srv, 200, "condition", func() bool { return srv.AttackTicks("Duelist") >= 15 })
}

// TestAMobIsShovedOffAPlayer spawns a cow overlapping a standing player's
// box and holds that the pair parts: vanilla's push runs between every two
// overlapping bodies each tick, the cow taking its shares of the shove and
// drifting clear of the player who spawned inside it.
func TestAMobIsShovedOffAPlayer(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, combatTestConfig(), "Crowd")
	c.MoveToGround(20.5, 64, 20.5, true)
	c.Settle()

	// The cow's feet a tenth off the player's, so the pair's positions
	// differ by more than the deadband the push reads.
	const spawnX, spawnZ = 20.6, 20.5
	mobID := srv.SpawnMob("minecraft:cow", spawnX, 64, spawnZ)
	c.ExpectMobSpawn(protocol.EntityTypeCow)

	// Sixty ticks of the loop, however long a slow machine takes for them.
	// The measure runs from the spawn x, not from a first sample: the
	// shove can have moved the cow before the loop's pace allows one.
	moved := 0.0
	servertest.WaitForTicks(t, srv, 60, func() bool {
		x, _, z, ok := srv.MobPos(mobID)
		if ok {
			moved = math.Hypot(x-spawnX, z-spawnZ)
		}
		return !ok || moved >= 0.3
	})
	if moved < 0.3 {
		c.T.Errorf("the cow overlapped by the player drifted %v in sixty ticks, want at least 0.3 of the shove", moved)
	}
}

// swingRecovered is the AttackTicks a whole swing's recovery has wound past:
// a sword's twelve and a half ticks, so the count at thirteen reads the
// swing wound up.
const swingRecovered = 13

// awaitFullSwing waits until the named player's attack has recovered past
// the slowest weapon here, a sword's twelve and a half ticks, so the next
// blow lands at full strength - the wait a fresh join or a held item
// change starts from the bottom of, timed on the server's own ticks
// rather than the wall clock.
func awaitFullSwing(t *testing.T, srv *server.Server, name string) {
	t.Helper()
	// Two whole ticks first: the tick that sees a blade just given and
	// spends the recovery for it may not have run yet, and a count read
	// before it would let a hurried blow through on a slow machine.
	since := srv.TicksRun()
	servertest.UntilTicks(t, srv, 200, "condition", func() bool {
		return srv.TicksRun() >= since+2 && srv.AttackTicks(name) >= swingRecovered
	})
}

// TestAnAttackBeyondReachStrikesNothing stands its striker's eye six blocks
// and a hundredth from the body it swings at, and holds the swing refused:
// no damage event, nothing — vanilla's handleAttack verifying the target's
// box against the reach the interaction range carries before any blow is
// spent. A body stood with its box's near face at the reach's own six — the
// distance the check takes, the boundary being inclusive — takes the blow.
// The distances are measured eye to box: the striker's feet a block and three
// fifths under its eyes and the target's body the box a standing player
// occupies.
func TestAnAttackBeyondReachStrikesNothing(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, combatTestConfig())

	striker := connect()
	striker.JoinAs("Striker")
	striker.MoveToGround(2.5, 64, 5.5, true)
	striker.Settle()

	target := connect()
	target.JoinAs("Target")
	targetID := striker.ExpectPlayerAdded("Target")

	// A hundredth past the reach: the target's box near face 6.01 from the
	// striker's eye, the swing refused outright.
	target.MoveToGround(8.81, 64, 5.5, true)
	target.Settle()
	servertest.HoldTicks(t, srv)
	striker.Attack(targetID)
	striker.ReadTicks(srv, 10, func(gotID int32, data []byte) bool {
		switch gotID {
		case protocol.IDPlayDamageEvent, protocol.IDPlayHurtAnimation:
			striker.T.Fatalf("a swing past the reach was answered with packet 0x%02X (% X)", gotID, data)
		}
		return false
	})

	// At the reach itself the blow lands: the box's near face six from the
	// eye, the distance the inclusive boundary takes.
	target.MoveToGround(8.8, 64, 5.5, true)
	target.Settle()
	striker.Attack(targetID)
	r := protocol.NewReader(striker.ExpectPlay(protocol.IDPlayDamageEvent))
	if got := r.ReadVarInt(); got != targetID {
		striker.T.Errorf("the in-reach blow landed on entity %d, want %d", got, targetID)
	}
}
