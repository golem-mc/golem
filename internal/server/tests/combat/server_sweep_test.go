package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
)

// The sweep a full sword's blow raises off the ground it stands on: the
// hurt every other living body within a block of the stricken target
// takes, the shove along the swing's facing, and Sweeping Edge's share of
// the blow raising the hurt. The sweep's own sound and particle are the
// attack feedback tests' ground (TestAStandingSwordSweeps); this file is
// about the bodies the blow reaches. Each test drives a real connection,
// stops the tick loop before planting the bodies so nothing drifts
// between the planting and the blow, and reads what the world was told.

// sweepFixture is a fencer and a watcher of one world, the way the attack
// feedback tests' fixture stands them: the fencer on the ground at (2.5,
// 64, 5.5), twice so its last reported move is none and nothing sweeps by
// speed, with both entity IDs for the damage events' causes and the
// connection left over for a third body a test wants beside them.
type sweepFixture struct {
	srv      *server.Server
	connect  func() *servertest.Client
	fencer   *servertest.Client
	watcher  *servertest.Client
	fencerID int32
	whoID    int32
}

// newSweepFixture grows the fixture.
func newSweepFixture(t *testing.T) sweepFixture {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.CombatTestConfig(), servertest.OpsListing(t, "Fencer"))
	f := sweepFixture{srv: srv, connect: connect}
	f.watcher = connect()
	f.watcher.Login("Watcher")
	f.watcher.Configure()
	f.whoID = f.watcher.EnterWorldAs("Watcher")
	f.fencer = connect()
	f.fencer.Login("Fencer")
	f.fencer.Configure()
	f.fencerID = f.fencer.EnterWorldAs("Fencer")
	f.fencer.MoveToGround(2.5, 64, 5.5, true)
	f.fencer.MoveToGround(2.5, 64, 5.5, true)
	f.fencer.Settle()
	f.watcher.Settle()
	return f
}

// ironSword gives the fencer an iron sword and waits the blade's whole
// recovery out, so the blow to come lands at full strength.
func (f sweepFixture) ironSword(t *testing.T) {
	t.Helper()
	f.srv.GiveSlot("Fencer", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:iron_sword"), Count: 1,
	})
	servertest.AwaitFullSwing(t, f.srv, "Fencer")
}

// wantShove holds one body's shove to the block, tolerating the packed
// vector's own rounding.
func wantShove(t *testing.T, shove []float64, want [3]float64, what string) {
	t.Helper()
	if shove == nil {
		t.Fatalf("%s was shoved nowhere", what)
	}
	for axis, got := range shove {
		if math.Abs(got-want[axis]) > 0.01 {
			t.Errorf("%s's shove was (%v, %v, %v), want (%v, %v, %v)",
				what, shove[0], shove[1], shove[2], want[0], want[1], want[2])
		}
	}
}

// TestASweepHurtsTheBodiesBesideItsVictim plants the stricken cow two
// blocks east of the fencer with two bystander cows beside and behind it
// and a far one out of the sweep's reach, and holds that the full sword's
// blow hurts all three near bodies — the cow it was swung at with the
// blade's whole six, the bystanders each with the sweep's plain one — and
// none of the far one. The bystander beside takes vanilla's two folded
// shoves: the hurt's four tenths away from the fencer halved, and the
// sweep's four tenths along the facing.
func TestASweepHurtsTheBodiesBesideItsVictim(t *testing.T) {
	t.Parallel()
	f := newSweepFixture(t)
	f.watcher.MoveToGround(40.5, 64, 40.5, true)
	f.watcher.Settle()
	f.ironSword(t)

	f.srv.StopTick()
	stricken := f.srv.SpawnMob("minecraft:cow", 4.5, 64, 5.5)
	beside := f.srv.SpawnMob("minecraft:cow", 5.45, 64, 5.5)
	behind := f.srv.SpawnMob("minecraft:cow", 4.5, 64, 6.45)
	far := f.srv.SpawnMob("minecraft:cow", 9.5, 64, 5.5)
	f.fencer.Settle()
	f.watcher.Settle()

	f.fencer.Attack(stricken)
	f.fencer.Settle()
	heard := f.watcher.HearBodies()
	for _, id := range []int32{stricken, beside, behind} {
		if got := heard.DamageEvents[id]; got != f.fencerID+1 {
			t.Errorf("the blow on %d named %d as the cause, want the fencer's %d", id, got, f.fencerID+1)
		}
		if !heard.HurtEvents[id] {
			t.Errorf("body %d took no hurt status", id)
		}
	}
	if got := heard.Healths[stricken]; math.Abs(float64(got)-4) > 0.01 {
		t.Errorf("the stricken cow was left at %v, want the blade's whole six off a ten", got)
	}
	for _, id := range []int32{beside, behind} {
		if got := heard.Healths[id]; math.Abs(float64(got)-9) > 0.01 {
			t.Errorf("a swept cow was left at %v, want the sweep's plain one off a ten", got)
		}
	}
	// The fencer faces south (yaw 0), so the facing shove is all +Z; the
	// beside cow's away line is east, the fold leaving (0.2, 0.4, 0.4).
	wantShove(t, heard.Motions[beside], [3]float64{0.2, 0.4, 0.4}, "the cow beside")
	if _, ok := heard.Motions[behind]; !ok {
		t.Error("the cow behind the stricken one was shoved nowhere")
	}
	for name, seen := range map[string]bool{
		"a damage event": heard.DamageEvents[far] != 0,
		"a hurt status":  heard.HurtEvents[far],
		"a shove":        heard.Motions[far] != nil,
		"a health":       heard.Healths[far] != 0,
	} {
		if seen {
			t.Errorf("the far cow out of the sweep's reach was sent %s", name)
		}
	}
}

// TestSweepingEdgeRaisesTheSweepHurt plants a bystander cow beside the
// stricken one and swings a Sweeping Edge III blade at full strength, and
// holds that the sweep carries the edge's three quarters of the blow over
// the plain one: a blade whose own blow is six sweeps with one plus
// three quarters of six, five and a half.
func TestSweepingEdgeRaisesTheSweepHurt(t *testing.T) {
	t.Parallel()
	f := newSweepFixture(t)
	f.watcher.MoveToGround(40.5, 64, 40.5, true)
	f.watcher.Settle()
	f.srv.GiveSlot("Fencer", player.HotbarSlot,
		servertest.EnchantedStack(t, "minecraft:iron_sword", "minecraft:sweeping_edge", 3))
	servertest.AwaitFullSwing(t, f.srv, "Fencer")

	f.srv.StopTick()
	stricken := f.srv.SpawnMob("minecraft:cow", 4.5, 64, 5.5)
	beside := f.srv.SpawnMob("minecraft:cow", 5.45, 64, 5.5)
	f.fencer.Settle()
	f.watcher.Settle()

	f.fencer.Attack(stricken)
	f.fencer.Settle()
	heard := f.watcher.HearBodies()
	if got := heard.Healths[stricken]; math.Abs(float64(got)-4) > 0.01 {
		t.Errorf("the stricken cow was left at %v, want the blade's whole six off a ten", got)
	}
	if got := heard.Healths[beside]; math.Abs(float64(got)-4.5) > 0.01 {
		t.Errorf("the swept cow was left at %v, want one plus three quarters of the blow, five and a half, off a ten", got)
	}
}

// TestASweepHurtsAPlayerBesideItsVictim stands a player of the world beside
// the stricken cow — the fixture's own watcher, whose reading of the blow
// the other tests take — and holds that the sweep reaches a living player
// the same way it reaches a mob: the plain hurt off the player's own
// twenty of health, and the same folded shove along the facing.
func TestASweepHurtsAPlayerBesideItsVictim(t *testing.T) {
	t.Parallel()
	f := newSweepFixture(t)
	f.watcher.MoveToGround(5.45, 64, 5.5, true)
	f.watcher.Settle()
	f.ironSword(t)

	f.srv.StopTick()
	stricken := f.srv.SpawnMob("minecraft:cow", 4.5, 64, 5.5)
	f.fencer.Settle()
	f.watcher.Settle()

	f.fencer.Attack(stricken)
	f.fencer.Settle()
	var health float32
	var shove []float64
	f.watcher.SettleAndCollect(func(id int32, data []byte) {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySetHealth:
			health = r.ReadF32()
		case protocol.IDPlaySetEntityMotion:
			if body := r.ReadVarInt(); body == f.whoID {
				x, y, z := r.ReadLpVec3()
				shove = []float64{x, y, z}
			}
		}
	})
	if math.Abs(float64(health)-19) > 0.01 {
		t.Errorf("the swept player was left at %v of twenty, want the sweep's plain one off it", health)
	}
	wantShove(t, shove, [3]float64{0.2, 0.4, 0.4}, "the swept player")
}

// TestASweepSkipsACreativeBystander stands a creative player beside the
// stricken cow with a survival cow behind it, and holds that the sweep
// leaves the creative body whole while still hurting the survival one: the
// creative bystander gets no damage event, no shove and no health lost.
func TestASweepSkipsACreativeBystander(t *testing.T) {
	t.Parallel()
	f := newSweepFixture(t)
	f.watcher.MoveToGround(5.45, 64, 5.5, true)
	f.watcher.Settle()
	f.ironSword(t)
	f.fencer.ChatCommand("gamemode creative Watcher")
	servertest.UntilTicks(t, f.srv, 1200, "condition", func() bool {
		return f.srv.PlayerGameMode("Watcher") == protocol.GameModeCreative
	})
	f.fencer.Settle()
	f.watcher.Settle()

	f.srv.StopTick()
	stricken := f.srv.SpawnMob("minecraft:cow", 4.5, 64, 5.5)
	behind := f.srv.SpawnMob("minecraft:cow", 4.5, 64, 6.45)
	f.fencer.Settle()
	f.watcher.Settle()

	f.fencer.Attack(stricken)
	f.fencer.Settle()
	heard := f.watcher.HearBodies()
	if got := heard.Healths[stricken]; math.Abs(float64(got)-4) > 0.01 {
		t.Errorf("the stricken cow was left at %v, want the blade's whole six off a ten", got)
	}
	if got := heard.Healths[behind]; math.Abs(float64(got)-9) > 0.01 {
		t.Errorf("the survival cow swept behind the stricken one was left at %v, want the sweep's plain one off a ten", got)
	}
	if got := f.srv.PlayerHealth("Watcher"); math.Abs(got-20) > 0.01 {
		t.Errorf("the creative bystander was left at %v of twenty, want no hurt at all", got)
	}
	if got := heard.DamageEvents[f.whoID]; got != 0 {
		t.Errorf("the creative bystander took a damage event naming %d", got)
	}
	if heard.Motions[f.whoID] != nil {
		t.Errorf("the creative bystander was shoved %v", heard.Motions[f.whoID])
	}
}

// TestASweepShakesTheArmorStandsBesideItsVictim plants two armor stands
// beside the stricken cow and a third out of the sweep's reach, and holds
// that the sweep punches the near two like a hand would — the shake event
// on each — and leaves the far one alone.
func TestASweepShakesTheArmorStandsBesideItsVictim(t *testing.T) {
	t.Parallel()
	f := newSweepFixture(t)
	f.watcher.MoveToGround(40.5, 64, 40.5, true)
	f.watcher.Settle()
	f.ironSword(t)

	f.srv.StopTick()
	stricken := f.srv.SpawnMob("minecraft:cow", 4.5, 64, 5.5)
	beside := f.srv.SpawnArmorStand(5.45, 64, 5.5)
	behind := f.srv.SpawnArmorStand(4.5, 64, 6.45)
	far := f.srv.SpawnArmorStand(9.5, 64, 5.5)
	f.fencer.Settle()
	f.watcher.Settle()

	f.fencer.Attack(stricken)
	f.fencer.Settle()
	shaken := map[int32]bool{}
	f.watcher.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayEntityEvent {
			return
		}
		r := protocol.NewReader(data)
		if body, status := r.ReadI32(), r.ReadU8(); status == protocol.EntityStatusArmorStandShake {
			shaken[body] = true
		}
	})
	for name, id := range map[string]int32{"beside": beside, "behind": behind} {
		if !shaken[id] {
			t.Errorf("the stand %s the stricken cow was not shaken by the sweep", name)
		}
	}
	if shaken[far] {
		t.Error("the stand out of the sweep's reach was shaken")
	}
}
