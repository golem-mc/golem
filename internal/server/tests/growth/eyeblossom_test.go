package growth

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The eyeblossom, black-box: the vanilla EyeblossomBlock's random tick
// carries it by the hour the overworld's day timeline keeps — open through
// the eyeblossom-open stretch the night is, closed the rest of the way — and
// the flip it takes tells itself aloud and wakes its neighbours, the closed
// ones within the box around it booking their own visits.
// vanilla: EyeblossomBlock.randomTick, tick, tryChangingState

// eyeblossomTickSpeed is the random tick rule the eyeblossom test runs at,
// against the default three, so the pass counts below are tens of hits on
// the one cell the test watches.
const eyeblossomTickSpeed = 50

// eyeblossomState reads the one state the named eyeblossom block carries.
func eyeblossomState(t *testing.T, name string) uint16 {
	t.Helper()
	state, ok := registry.FindState(name, nil)
	if !ok {
		t.Fatalf("the registry has no %s", name)
	}
	return state
}

// TestAnEyeblossomOpensAtNightAndClosesByDay checks the random tick's two
// sides of the day: a closed eyeblossom on grass opens under a night clock
// and closes again under a day clock, each flip reaching the wire beside the
// block change.
func TestAnEyeblossomOpensAtNightAndClosesByDay(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.SaplingSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), eyeblossomTickSpeed)

	// Grass where the flat world's ground is stone: the flower family
	// stands on the soil the supports_vegetation tag names.
	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:closed_eyeblossom", nil)

	srv.SetDayTime(14000)
	srv.TickRandomTicks(20000)
	servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:open_eyeblossom", nil)
	servertest.AwaitBlockUpdate(t, c, 5, 64, 0, eyeblossomState(t, "minecraft:open_eyeblossom"))

	srv.SetDayTime(2000)
	srv.TickRandomTicks(20000)
	servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:closed_eyeblossom", nil)
}
