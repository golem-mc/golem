package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// Tripwire hooks and string, the vanilla TripWireHookBlock and TripWireBlock
// played end to end: the hooks attach through the string between them, an
// entity on the string powers both hooks and the lamps they hang on, leaving
// unpowers after the re-check, and cutting the string with shears detaches
// the line without a pulse while any other break fires it.

// tripZ is the row every tripwire test lays its line on, well off the spawn
// column the other tests build around.
const tripZ = 6

// buildTripwire lays a tripwire line whose hooks stand at x0 and x1, facing
// each other over the string between, with a redstone lamp as the block each
// hook hangs on. The lamps and the string are set with /setblock and the
// hooks are then placed by hand against the lamps' faces, because attaching
// is TripWireHookBlock.setPlacedBy's own work, which /setblock never runs.
// vanilla: TripWireHookBlock.setPlacedBy, TripWireBlock.getStateForPlacement
func buildTripwire(t *testing.T, srv *server.Server, c *servertest.Client, x0, x1 int32) {
	t.Helper()
	buildTripwireAt(t, srv, c, x0, x1, 64)
}

// buildTripwireAt is buildTripwire with the line at height y.
func buildTripwireAt(t *testing.T, srv *server.Server, c *servertest.Client, x0, x1, y int32) {
	t.Helper()
	if x1-x0 < 2 || x1-x0 > 42 {
		t.Fatalf("hook span %d has no room for string", x1-x0)
	}
	servertest.Setblock(t, c, x0-1, y, tripZ, "redstone_lamp")
	servertest.Setblock(t, c, x1+1, y, tripZ, "redstone_lamp")
	for x := x0 + 1; x < x1; x++ {
		servertest.Setblock(t, c, int32(x), y, tripZ, "tripwire")
	}
	// Each hook goes on the lamp behind it, clicked from outside the line:
	// the hook lands facing into the line and the hand never stood on a
	// string that would press it.
	// vanilla: TripWireBlock.getStateForPlacement
	servertest.PlaceFrom(c, "minecraft:tripwire_hook", x0-2, tripZ, 0, x0-1, y, tripZ, protocol.FaceEast)
	servertest.PlaceFrom(c, "minecraft:tripwire_hook", x1+2, tripZ, 0, x1+1, y, tripZ, protocol.FaceWest)
	servertest.ExpectBlockNamed(t, srv, x0, y, tripZ, "minecraft:tripwire_hook", map[string]string{"facing": "east"})
	servertest.ExpectBlockNamed(t, srv, x1, y, tripZ, "minecraft:tripwire_hook", map[string]string{"facing": "west"})
}

// tripwireServer joins two clients: the actor, an operator laying the line,
// and a watcher whose connection collects the sounds instead of the actor's
// own settle draining them — a hook's placement scan and a shears cut both
// play while the actor's packets are still being read for other things.
func tripwireServer(t *testing.T) (*server.Server, *servertest.Client, *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Actor"))
	actor := connect().Join("Actor")
	watcher := connect().Join("Watcher")
	servertest.HoldTicks(t, srv)
	return srv, actor, watcher
}

// expectPropsSettle polls the block at the position until it holds the named
// block with the properties wanted, failing when the ten seconds pass
// without: the trips a re-check settles rather than the write's own tick.
func expectPropsSettle(t *testing.T, srv *server.Server, x, y, z int32, name string, want map[string]string) {
	t.Helper()
	servertest.UntilTicks(t, srv, 200, "condition", func() bool {
		gotName, props := servertest.StateProps(srv.BlockAt(x, y, z))
		if gotName != name {
			return false
		}
		for k, v := range want {
			if props[k] != v {
				return false
			}
		}
		return true
	})
	servertest.ExpectBlockNamed(t, srv, x, y, z, name, want)
}

// TestTripwireHooksAttachThroughTheString lays the line at the vanilla
// maximum span: the hooks forty-one apart over forty strings, a partner each
// hook's walk still reaches. Everything attaches, and nothing powers the
// lamps: attaching alone is silent redstone.
// vanilla: TripWireHookBlock.calculateState, WIRE_DIST_MAX
func TestTripwireHooksAttachThroughTheString(t *testing.T) {
	t.Parallel()
	srv, c, watcher := tripwireServer(t)
	const x0, x1 = 30, 71
	// The hooks' sounds carry sixteen blocks; the watcher stands beside the
	// first, off the line.
	watcher.MoveToGround(float64(x0)+0.5, 64, tripZ+3.5, true)
	buildTripwire(t, srv, c, x0, x1)

	servertest.ExpectBlockNamed(t, srv, x0, 64, tripZ, "minecraft:tripwire_hook", map[string]string{
		"attached": "true", "powered": "false",
	})
	servertest.ExpectBlockNamed(t, srv, x1, 64, tripZ, "minecraft:tripwire_hook", map[string]string{
		"attached": "true", "powered": "false",
	})
	for _, x := range []int32{x0 + 1, x0 + 20, x1 - 1} {
		servertest.ExpectBlockNamed(t, srv, x, 64, tripZ, "minecraft:tripwire", map[string]string{
			"attached": "true", "powered": "false", "disarmed": "false",
		})
	}
	// The strings at the ends connect to their hook on one side and their
	// neighbour on the other; nothing runs north or south.
	// vanilla: TripWireBlock.shouldConnectTo
	servertest.ExpectBlockNamed(t, srv, x0+1, 64, tripZ, "minecraft:tripwire", map[string]string{
		"west": "true", "east": "true", "north": "false", "south": "false",
	})
	servertest.ExpectBlockNamed(t, srv, x1-1, 64, tripZ, "minecraft:tripwire", map[string]string{
		"west": "true", "east": "true",
	})
	// Attaching is what the hooks' own clicks say it is, and the lamps stay
	// dark: the block behind a hook carries power only while the hook is
	// powered.
	// vanilla: TripWireHookBlock.emitState, RedstoneLampBlock
	servertest.ExpectSoundEvent(t, watcher, "minecraft:block.tripwire.attach")
	servertest.ExpectBlockNamed(t, srv, x0-1, 64, tripZ, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
	servertest.ExpectBlockNamed(t, srv, x1+1, 64, tripZ, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
}

// TestStandingOnTheStringPowersBothHooksAndTheirLamps walks the player onto
// the middle string of an attached line: the string's own POWERED flips, both
// hooks power, and each hook strongly answers 15 into the lamp it hangs on.
// vanilla: TripWireBlock.entityInside, TripWireHookBlock.getDirectSignal
func TestStandingOnTheStringPowersBothHooksAndTheirLamps(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	const x0, x1 = 40, 52
	buildTripwire(t, srv, c, x0, x1)

	c.MoveToGround(float64(x0+6)+0.5, 64, tripZ+0.5, true)
	expectPropsSettle(t, srv, x0+6, 64, tripZ, "minecraft:tripwire", map[string]string{"powered": "true"})
	expectPropsSettle(t, srv, x0, 64, tripZ, "minecraft:tripwire_hook", map[string]string{"powered": "true"})
	expectPropsSettle(t, srv, x1, 64, tripZ, "minecraft:tripwire_hook", map[string]string{"powered": "true"})
	expectPropsSettle(t, srv, x0-1, 64, tripZ, "minecraft:redstone_lamp", map[string]string{"lit": "true"})
	expectPropsSettle(t, srv, x1+1, 64, tripZ, "minecraft:redstone_lamp", map[string]string{"lit": "true"})
	servertest.ExpectSoundEvent(t, c, "minecraft:block.tripwire.click_on")
}

// TestCuttingTheStringWithShearsDisarmsIt cuts the middle string with
// shears: the cut sets DISARMED on the string before the break, so the hooks
// only detach - the string copying itself powered to the removal's wake is
// armed off - and the lamps never light.
// vanilla: TripWireBlock.playerWillDestroy, affectNeighborsAfterRemoval
func TestCuttingTheStringWithShearsDisarmsIt(t *testing.T) {
	t.Parallel()
	srv, c, watcher := tripwireServer(t)
	const x0, x1 = 40, 52
	// The hooks' sounds carry sixteen blocks; the watcher stands between
	// them, off the line.
	watcher.MoveToGround(float64(x0+6)+0.5, 64, tripZ+3.5, true)
	buildTripwire(t, srv, c, x0, x1)

	shears, ok := registry.ItemID("minecraft:shears")
	if !ok {
		t.Fatal("no item minecraft:shears")
	}
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: shears, Count: 1})
	// One row off the line, within reach of the string and standing on
	// none of it: a hand holding shears in the string would press it.
	c.MoveToGround(float64(x0+5)+0.5, 64, tripZ+1.5, true)
	c.BreakBlock(x0+6, 64, tripZ)
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, x0, 64, tripZ, "minecraft:tripwire_hook", map[string]string{
		"attached": "false", "powered": "false",
	})
	servertest.ExpectBlockNamed(t, srv, x1, 64, tripZ, "minecraft:tripwire_hook", map[string]string{
		"attached": "false", "powered": "false",
	})
	for _, x := range []int32{x0 + 2, x0 + 8} {
		servertest.ExpectBlockNamed(t, srv, x, 64, tripZ, "minecraft:tripwire", map[string]string{
			"attached": "false", "powered": "false",
		})
	}
	servertest.ExpectBlockNamed(t, srv, x0-1, 64, tripZ, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
	servertest.ExpectBlockNamed(t, srv, x1+1, 64, tripZ, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
	servertest.ExpectSoundEvent(t, watcher, "minecraft:block.tripwire.detach")

	// Nothing arrives late either: the ten ticks a trigger's pulse would
	// last come and go with the line still dark.
	servertest.AwaitTicks(t, srv, 30)
	servertest.ExpectBlockNamed(t, srv, x0, 64, tripZ, "minecraft:tripwire_hook", map[string]string{"powered": "false"})
	servertest.ExpectBlockNamed(t, srv, x0-1, 64, tripZ, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
}

// TestBreakingTheStringFiresTheHooks breaks the middle string bare: the
// removal's wake pretends the string powered - so both hooks pulse, the
// lamps light - and the re-check releases it.
// vanilla: TripWireBlock.affectNeighborsAfterRemoval, checkPressed
func TestBreakingTheStringFiresTheHooks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	const x0, x1 = 40, 52
	buildTripwire(t, srv, c, x0, x1)
	servertest.ExpectBlockNamed(t, srv, x0, 64, tripZ, "minecraft:tripwire_hook", map[string]string{
		"attached": "true", "powered": "false",
	})

	// The break: a plain removal, shears nowhere near it.
	servertest.Setblock(t, c, x0+6, 64, tripZ, "air")
	expectPropsSettle(t, srv, x0, 64, tripZ, "minecraft:tripwire_hook", map[string]string{
		"attached": "true", "powered": "true",
	})
	expectPropsSettle(t, srv, x1, 64, tripZ, "minecraft:tripwire_hook", map[string]string{
		"attached": "true", "powered": "true",
	})
	expectPropsSettle(t, srv, x0-1, 64, tripZ, "minecraft:redstone_lamp", map[string]string{"lit": "true"})
	expectPropsSettle(t, srv, x1+1, 64, tripZ, "minecraft:redstone_lamp", map[string]string{"lit": "true"})
	servertest.ExpectSoundEvent(t, c, "minecraft:block.tripwire.click_on")

	// The pulse lasts the ten ticks the hook's re-check waits out, and the
	// line it crossed falls slack: the hooks unattach with it.
	expectPropsSettle(t, srv, x0, 64, tripZ, "minecraft:tripwire_hook", map[string]string{
		"attached": "false", "powered": "false",
	})
	expectPropsSettle(t, srv, x1, 64, tripZ, "minecraft:tripwire_hook", map[string]string{
		"attached": "false", "powered": "false",
	})
	expectPropsSettle(t, srv, x0+3, 64, tripZ, "minecraft:tripwire", map[string]string{
		"attached": "false", "powered": "false",
	})
	expectPropsSettle(t, srv, x0-1, 64, tripZ, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
	expectPropsSettle(t, srv, x1+1, 64, tripZ, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
	servertest.ExpectSoundEvent(t, c, "minecraft:block.tripwire.click_off")
}

// TestAnOverlongTripwireDoesNotAttach lays the line one string past the
// vanilla span: the hooks never attach, the string never powers them, and
// the lamps stay dark however long anyone stands on it.
// vanilla: TripWireHookBlock.calculateState, WIRE_DIST_MAX
func TestAnOverlongTripwireDoesNotAttach(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	const x0, x1 = 30, 72 // 41 strings between: the span is one too wide
	buildTripwire(t, srv, c, x0, x1)

	for _, x := range []int32{x0 + 1, x0 + 20, x1 - 1} {
		servertest.ExpectBlockNamed(t, srv, x, 64, tripZ, "minecraft:tripwire", map[string]string{
			"attached": "false", "powered": "false",
		})
	}
	c.MoveToGround(float64(x0+20)+0.5, 64, tripZ+0.5, true)
	servertest.AwaitTicks(t, srv, 25)
	servertest.ExpectBlockNamed(t, srv, x0, 64, tripZ, "minecraft:tripwire_hook", map[string]string{"powered": "false"})
	servertest.ExpectBlockNamed(t, srv, x1, 64, tripZ, "minecraft:tripwire_hook", map[string]string{"powered": "false"})
	servertest.ExpectBlockNamed(t, srv, x0-1, 64, tripZ, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
	servertest.ExpectBlockNamed(t, srv, x1+1, 64, tripZ, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
}

// TestBreakingAHookDetachesThePartner removes a hook already attached: the
// partner hook lets go quietly - a removed hook never fires - while the
// string keeps its attached, because the destroyed hook's scan meets its
// own now-air cell and returns before the wire writes and the sounds,
// leaving the slack for a later scan to relax.
// vanilla: TripWireHookBlock.affectNeighborsAfterRemoval, calculateState
func TestBreakingAHookDetachesThePartner(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	const x0, x1 = 40, 52
	buildTripwire(t, srv, c, x0, x1)
	servertest.ExpectBlockNamed(t, srv, x0, 64, tripZ, "minecraft:tripwire_hook", map[string]string{
		"attached": "true", "powered": "false",
	})

	servertest.Setblock(t, c, x1, 64, tripZ, "air")
	expectPropsSettle(t, srv, x0, 64, tripZ, "minecraft:tripwire_hook", map[string]string{
		"attached": "false", "powered": "false",
	})
	for _, x := range []int32{x0 + 1, x0 + 6, x1 - 1} {
		servertest.ExpectBlockNamed(t, srv, x, 64, tripZ, "minecraft:tripwire", map[string]string{
			"attached": "true", "powered": "false",
		})
	}
	servertest.ExpectBlockNamed(t, srv, x0-1, 64, tripZ, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
	servertest.ExpectBlockNamed(t, srv, x1+1, 64, tripZ, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
}

// TestAPistonDestroyingTheStringFiresTheHooks drives a piston through the
// middle of an attached line. The destroyed cell's removal runs with
// movedByPiston false, so both hooks pulse and their re-check then relaxes
// the line the head has broken.
// vanilla: PistonBaseBlock.moveBlocks, TripWireBlock.affectNeighborsAfterRemoval
func TestAPistonDestroyingTheStringFiresTheHooks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	const x0, x1 = 40, 52
	buildTripwire(t, srv, c, x0, x1)

	// The piston faces north into the string at x0+6, the redstone block
	// behind it the power that runs the extend.
	servertest.Setblock(t, c, x0+6, 64, tripZ+1, "piston[facing=north]")
	servertest.Setblock(t, c, x0+6, 64, tripZ+2, "redstone_block")

	expectPropsSettle(t, srv, x0, 64, tripZ, "minecraft:tripwire_hook", map[string]string{
		"attached": "true", "powered": "true",
	})
	expectPropsSettle(t, srv, x1, 64, tripZ, "minecraft:tripwire_hook", map[string]string{
		"attached": "true", "powered": "true",
	})
	servertest.ExpectSoundEvent(t, c, "minecraft:block.tripwire.click_on")

	// The head lands where the string stood, so the pulse's re-check finds
	// the line broken and detaches the hooks.
	expectPropsSettle(t, srv, x0, 64, tripZ, "minecraft:tripwire_hook", map[string]string{
		"attached": "false", "powered": "false",
	})
	expectPropsSettle(t, srv, x1, 64, tripZ, "minecraft:tripwire_hook", map[string]string{
		"attached": "false", "powered": "false",
	})
	servertest.ExpectSoundEvent(t, c, "minecraft:block.tripwire.click_off")
}
