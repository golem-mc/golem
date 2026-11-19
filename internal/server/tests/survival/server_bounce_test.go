package survival

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// bounceStep is one packet a client reports; status is the flags-only one.
type bounceStep struct {
	y        float64
	onGround bool
	status   bool
}

// bounceApproach is the n falling packets that end in a step of last blocks
// down onto y, walking gravity and drag backwards.
func bounceApproach(y, last float64, n int) []bounceStep {
	steps := make([]bounceStep, n)
	for i := n - 1; i >= 0; i-- {
		y += last
		last = last/0.98 - 0.08
		steps[i] = bounceStep{y: y}
	}
	return steps
}

// The block top is y=64; the packets are the issue's, shifted up by 124.
var (
	bounceLandingByPosition = append(bounceApproach(64.890, 0.83, 5), []bounceStep{
		{64.890, false, false}, {64.000, true, false}, {65.091, false, false}}...)
	// The held downward movement (-0.05) does not beat gravity, so the block
	// does not bounce the body and the rise is a jump.
	bounceLandingSlow = []bounceStep{
		{64.20, false, false}, {64.12, false, false}, {64.05, false, false},
		{64.000, true, false}, {64.200, false, false}}
	bounceLandingByFlags = append(bounceApproach(64.830, 0.6957, 5), []bounceStep{
		{64.830, false, false}, {64.070, false, false}, {0, true, true}, {64.407, false, false}}...)
)

// jumpsAndExhaustion replays steps one tick each on a floor whose top cell
// is block and returns the jump statistic and the exhaustion the replay
// added.
func jumpsAndExhaustion(t *testing.T, block string, steps []bounceStep, sneak bool) (jumps int32, exhaustion float64) {
	t.Helper()
	return jumpsAndExhaustionPerTick(t, block, steps, sneak, 1)
}

// jumpsAndExhaustionPerTick is jumpsAndExhaustion with perTick packets
// handled in each server tick.
func jumpsAndExhaustionPerTick(t *testing.T, block string, steps []bounceStep, sneak bool, perTick int) (jumps int32, exhaustion float64) {
	t.Helper()
	// Every assertion concerns the one column in chunk (0,0).
	cfg := servertest.SurvivalConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Bouncer")
	servertest.HoldTicks(t, srv)
	srv.SetBlock(0, 62, 0, servertest.BlockState(t, "minecraft:stone"))
	srv.SetBlock(0, 63, 0, servertest.BlockState(t, block))
	start := steps[0].y
	c.SendPosition(0.5, start, 0.5, true)
	c.SendPosition(0.5, start, 0.5, true)
	if sneak {
		c.SendSneak(true)
	}
	c.Settle()
	servertest.RunTicks(t, srv, 2)

	before := srv.PlayerExhaustion("Bouncer")
	for i, s := range steps {
		if s.status {
			c.SendMovementFlags(s.onGround)
		} else {
			c.SendPosition(0.5, s.y, 0.5, s.onGround)
		}
		c.Settle()
		if (i+1)%perTick == 0 || i == len(steps)-1 {
			servertest.RunTicks(t, srv, 1)
		}
	}
	return jumpsOf(t, c), srv.PlayerExhaustion("Bouncer") - before
}

// bounceFullSequence is a real client's three bounces on a block with top 64,
// one packet per client tick; the landings are entries 15, 29 and 42.
var bounceFullSequence = func() []bounceStep {
	ys := []float64{65.091, 65.503, 65.829, 66.069, 66.226, 66.302, 66.298, 66.216, 66.056, 65.822, 65.514, 65.133, 64.682, 64.162, 64.000,
		64.517, 64.945, 65.285, 65.541, 65.713, 65.804, 65.814, 65.745, 65.600, 65.379, 65.084, 64.717, 64.278, 64.000,
		64.457, 64.826, 65.109, 65.308, 65.425, 65.461, 65.418, 65.297, 65.101, 64.830, 64.485, 64.070, 64.000}
	steps := make([]bounceStep, len(ys))
	for i, y := range ys {
		steps[i] = bounceStep{y: y, onGround: y == 64.000}
	}
	return steps
}()

// One packet per server tick the bounce clears the ground flag before each
// rise, so no jump counts; a landing and its rise handled in one tick still
// read as a jump: the player's own tick runs only between ticks.
// vanilla: ServerGamePacketListenerImpl.handlePlayerPositionChange,
// Entity.restituteMovementAfterCollisions
func TestBounceFullSequence(t *testing.T) {
	t.Parallel()
	const jump = 0.05 // vanilla: Player.jumpFromGround, causeFoodExhaustion
	cases := []struct {
		name      string
		block     string
		perTick   int
		wantJumps int32
	}{
		{"slime, one packet per tick", "minecraft:slime_block", 1, 0},
		{"bed, one packet per tick", "minecraft:white_bed", 1, 0},
		// Pairs (14,15) and (28,29) hold a landing and its rise; (40,41) has no rise.
		{"slime, landing and rise in one tick", "minecraft:slime_block", 2, 2},
		{"bed, landing and rise in one tick", "minecraft:white_bed", 2, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			jumps, exhaustion := jumpsAndExhaustionPerTick(t, tc.block, bounceFullSequence, false, tc.perTick)
			if jumps != tc.wantJumps {
				t.Errorf("jumps = %d, want %d", jumps, tc.wantJumps)
			}
			if want := jump * float64(tc.wantJumps); math.Abs(exhaustion-want) > 1e-6 {
				t.Errorf("exhaustion added = %v, want %v", exhaustion, want)
			}
		})
	}
}

// jumpsOf reads minecraft:jump from the Award Statistics a Request Stats
// answers.
func jumpsOf(t *testing.T, c *servertest.Client) int32 {
	t.Helper()
	typeID, keyID, ok := registry.StatID(registry.StatCustom, "minecraft:jump")
	if !ok {
		t.Fatal("no custom statistic minecraft:jump")
	}
	c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandRequestStats)
	})
	r := protocol.NewReader(c.AwaitPlay(protocol.IDPlayAwardStats))
	var jumps int32
	for range r.ReadVarInt() {
		if r.ReadVarInt() == typeID && r.ReadVarInt() == keyID {
			jumps = r.ReadVarInt()
		} else {
			r.ReadVarInt()
		}
	}
	if err := r.Err(); err != nil {
		t.Fatalf("award statistics: %v", err)
	}
	return jumps
}
