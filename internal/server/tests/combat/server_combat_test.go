package combat

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

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
	c.Send(protocol.IDPlayPunch, func(b *protocol.Buffer) {})
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

// TestMobsShoveEachOtherApart stands two cows inside each other's boxes
// and holds that they part: each pair of overlapping bodies pushes both its
// sides, and the pair ends a body's width clear of where they began.
func TestMobsShoveEachOtherApart(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, combatTestConfig(), "Crowd")
	c.MoveToGround(2.5, 64, 2.5, true)
	c.Settle()

	first := srv.SpawnMob("minecraft:cow", 40.5, 64, 40.5)
	second := srv.SpawnMob("minecraft:cow", 40.6, 64, 40.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.ExpectMobSpawn(protocol.EntityTypeCow)

	parted := 0.0
	servertest.WaitForTicks(t, srv, 60, func() bool {
		x1, _, _, ok1 := srv.MobPos(first)
		x2, _, _, ok2 := srv.MobPos(second)
		if ok1 && ok2 {
			parted = x2 - x1
		}
		return !ok1 || !ok2 || parted >= 0.3
	})
	if parted < 0.3 {
		c.T.Errorf("two overlapping cows stood %v apart after sixty ticks, want at least 0.3 of the shove", parted)
	}
}

// TestAZombieClimbsNoCeiling walks a zombie around a two-block wall with a
// ceiling over it and holds the climb to the floor-delta test vanilla's
// move control makes: the body jumps for a waypoint whose floor stands
// more than a step height above it. The wall makes the hunt's path round
// it through the lane beside it — and a body pulled back against the
// wall's face keeps steering at the lane waypoint it already had, a walk
// whose straight line clips the wall on the way. The old jump read that
// block ahead of the body and fired whatever the waypoint's floor said,
// bonking the body's head on the ceiling; the floor-delta test holds the
// walk down, the drift carrying it along the wall's face.
func TestAZombieClimbsNoCeiling(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, combatTestConfig(), "Doorway")
	c.MoveToGround(8.5, 64, 8.5, true)
	c.Settle()

	// The doorway: a two-block wall at (5,64,5) and (6,64,5) with a
	// ceiling a block over each, a zombie west of the wall, its player
	// away south-east — far enough that no melee lands inside the walk
	// the test watches.
	stone := servertest.BlockState(t, "minecraft:stone")
	srv.SetBlock(5, 64, 5, stone)
	srv.SetBlock(6, 64, 5, stone)
	srv.SetBlock(5, 66, 5, stone)
	srv.SetBlock(6, 66, 5, stone)

	zombie := srv.SpawnMob("minecraft:zombie", 3.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)

	// The walk around the wall: the hunt's path rounds it south-east, and
	// the poll waits only for the corner it rounds — the climb the old
	// code made is not what this half is for.
	servertest.UntilTicks(c.T, srv, 200, "the zombie rounding the wall", func() bool {
		x, _, z, ok := srv.MobPos(zombie)
		return ok && x >= 4.6 && z >= 6.0
	})
	if x, _, z, ok := srv.MobPos(zombie); !ok || x < 4.6 || z < 6.0 {
		c.T.Fatalf("the zombie never rounded the wall: x=%v z=%v", x, z)
	}

	// Now the drift that made the old code bonk: set flush against the
	// wall's west face, its legs still steering at the lane waypoint the
	// hunt had already planned, a straight line that clips the wall. The
	// jump the old probe fired here put the body into the ceiling; the
	// floor-delta test keeps it at its feet. The body's half width is 0.3,
	// so x=4.69 keeps it out of the stone: a centre inside the wall's cell
	// would jump by vanilla's own-cell test (MoveControl.tick).
	srv.MoveMob(zombie, 4.69, 5.2)
	maxY := 0.0
	for range 40 {
		servertest.RunTicks(c.T, srv, 1)
		_, y, _, ok := srv.MobPos(zombie)
		if !ok {
			break
		}
		if y > maxY {
			maxY = y
		}
	}
	if maxY > 64.01 {
		c.T.Errorf("the zombie rose to %v walking past the wall under its ceiling, want it kept at its feet' 64", maxY)
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

// TestAFreshPlayerDoesNotStrikeACritical plants its player in the world and
// swings at a cow before a single movement packet has come from the client —
// the join's own placement being all the server knows, feet on the ground
// with no fall banked — and holds that the blow is the weapon's whole force
// and not the half again a critical would carry. Vanilla's critical asks a
// fall to have been banked, fallDistance over zero, and not merely a body
// whose ground bit the server has not been told yet; a fresh join has no
// fall to speak of, so its first standing blow lands plain. The second half
// stands the other way: a player who has reported a step of falling — the
// rise of a jump and the drop after it, the distance the server banks off
// the movement packets — lands the critical, the half again and all.
func TestAFreshPlayerDoesNotStrikeACritical(t *testing.T) {
	t.Parallel()
	srv, c, attacker := servertest.JoinAs(t, combatTestConfig(), "Fencer")

	// The cow is planted by the player's own spawn, near enough to be
	// tracked and struck, the three and a half blocks between them well
	// inside a blow's reach.
	pos := srv.PlayerPosition("Fencer")
	iron := servertest.MustItemID(t, "minecraft:iron_sword")
	ironFull := 1.0 + float64(registry.AttackDamage(iron))
	srv.GiveSlot("Fencer", player.HotbarSlot, protocol.ItemStack{ItemID: iron, Count: 1})
	awaitFullSwing(t, srv, "Fencer")
	mobID := srv.SpawnMob("minecraft:cow", pos.X+2.5, 64, pos.Z+2.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	if got := c.Strike(mobID, attacker); math.Abs(float64(got)-(10-ironFull)) > 0.01 {
		c.T.Errorf("the fresh player's first blow left %v, want %v — the whole force, no critical's half again",
			got, 10-ironFull)
	}

	// The control: a fall banked, the critical lands. The iron blade's
	// half again is six of the cow's ten, a blow it survives, so the
	// reads stay in order.
	srv.GiveSlot("Fencer", player.HotbarSlot, protocol.ItemStack{ItemID: iron, Count: 1})
	awaitFullSwing(t, srv, "Fencer")
	c.MoveToGround(pos.X+2.5, 65, pos.Z+2.5, false)   // the jump's rise
	c.MoveToGround(pos.X+2.5, 64.4, pos.Z+2.5, false) // the drop it banks
	if got := c.StrikeFresh(srv, pos.X+7.5, pos.Z+2.5, attacker); math.Abs(float64(got)-(10-1.5*ironFull)) > 0.01 {
		c.T.Errorf("the falling player's blow left %v, want %v — the critical's half again",
			got, 10-1.5*ironFull)
	}
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

// TestAnInteractBeyondReachIsIgnored right-clicks a cow with an empty
// bucket first from eighteen blocks away and then from three, and holds the
// far click refused and the near one taken: the far one milked nothing, no
// slot of the hand changed, while the near one filled the bucket with milk
// the way an in-reach use does. Vanilla's handleInteract verifies the body's
// box against the interaction range before the use is read at all.
func TestAnInteractBeyondReachIsIgnored(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, combatTestConfig(), "Milker")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	far := srv.SpawnMob("minecraft:cow", 20.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.GiveSlot("Milker", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:bucket"), Count: 1,
	})

	// The far click, eighteen blocks from the cow: nothing answers it.
	servertest.HoldTicks(t, srv)
	c.UseOn(far)
	c.ReadTicks(srv, 10, func(gotID int32, data []byte) bool {
		switch gotID {
		case protocol.IDPlayContainerSlot:
			c.T.Fatalf("a use past the reach filled a slot (% X)", data)
		}
		return false
	})

	// The near click, three blocks from the cow: the bucket comes back a
	// bucket of milk, the slot the hand holds changed under it.
	near := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(near)
	r := protocol.NewReader(c.AwaitPlay(protocol.IDPlayContainerSlot))
	r.ReadVarInt() // window ID
	r.ReadVarInt() // state ID
	if got := r.ReadI16(); got != int16(player.HotbarSlot) {
		c.T.Errorf("the milk named slot %d, want the held hotbar slot %d", got, player.HotbarSlot)
	}
	milk, _ := registry.ItemID("minecraft:milk_bucket")
	if stack, found := r.ReadSlot(); !found || stack.ItemID != milk {
		c.T.Errorf("the use left %+v in the hand, want a bucket of milk", stack)
	}
}
