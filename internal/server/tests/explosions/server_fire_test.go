package explosions

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The fire, black-box: the block the flint and steel lights, the burns the
// physics engine runs, and what reaches the wire. The timing tests stop the
// main loop and drive the engine by hand against a pinned seed — the fire's
// dice are seeded, so the same world and the same seed play out the same —
// and the live-loop tests run with the world's seed pinned so the loop's own
// dice are fixed too.

// fireSeed is the seed the fire tests pin the engine's dice with.
const fireSeed = 20260226

// fireConfig is the block tests' configuration with the world's seed pinned,
// which is what pins the fire dice a live-loop test runs on.
func fireConfig() *config.Config {
	cfg := servertest.BlockTestConfig()
	cfg.LevelSeed = "-12345"
	return cfg
}

// driveFire stops the main loop and runs the given number of engine ticks by
// hand, the dice pinned to fireSeed first. Every test that calls it arranges
// its blocks before it and reads the world after it.
func driveFire(t *testing.T, srv *server.Server, ticks int) {
	t.Helper()
	srv.StopTick()
	servertest.SeedPhysics(t, srv, fireSeed)
	srv.TickPhysics(ticks)
}

// setBlockCmd lays a block with /setblock, the way an operator would, and
// waits for the update that says it landed.
func setBlockCmd(t *testing.T, c *servertest.Client, x, y, z int32, block string) uint16 {
	t.Helper()
	c.ChatCommand("setblock " + strconv.FormatInt(int64(x), 10) + " " +
		strconv.FormatInt(int64(y), 10) + " " + strconv.FormatInt(int64(z), 10) + " " + block)
	state, ok := registry.FindState("minecraft:"+block, nil)
	if !ok {
		t.Fatalf("the registry has no minecraft:%s", block)
	}
	// The feedback first: the server tells the block update before it
	// wakes the block's physics, and only the feedback comes after the
	// wake, so a test that drives the physics next finds the block known.
	servertest.AwaitCommandFeedback(t, c, "commands.setblock.success",
		strconv.FormatInt(int64(x), 10), strconv.FormatInt(int64(y), 10), strconv.FormatInt(int64(z), 10))
	servertest.AwaitBlockUpdateSoon(t, c, x, y, z, state)
	return state
}

// fireAge reads the AGE property of a fire state, -1 for anything else.
func fireAge(state uint16) int {
	name, props, ok := registry.BlockStateName(state)
	if !ok || name != "minecraft:fire" {
		return -1
	}
	age, err := strconv.Atoi(registry.ParseStateProps(props)["age"])
	if err != nil {
		return -1
	}
	return age
}

// giveFlint puts a flint and steel in the player's hand.
func giveFlint(t *testing.T, c *servertest.Client) {
	t.Helper()
	flintID, ok := registry.ItemID("minecraft:flint_and_steel")
	if !ok {
		t.Fatal("the item registry has no flint and steel")
	}
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: flintID, Count: 1})
}

// TestFireBurnsPlanksAwayOnTheWire checks the burn at the black box: planks
// beside a fire are eaten by it on the hand-driven engine, and the Block
// Update that tells the client what the planks became arrives.
func TestFireBurnsPlanksAwayOnTheWire(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Arsonist", servertest.OpsListing(t, "Arsonist"))

	planks := setBlockCmd(t, c, 6, 64, 3, "oak_planks")
	setBlockCmd(t, c, 5, 64, 3, "fire")

	driveFire(t, srv, 1600)

	gone := srv.BlockAt(6, 64, 3)
	if gone == planks {
		t.Fatal("the planks stood 1600 ticks of fire beside them and were not burnt away")
	}
	// The wire heard about it: some Block Update for the cell says
	// something other than the planks — what the burn's dice decided it
	// became, the flame that eats it or the air left behind. The
	// updates the run made for the cell are stepped over until one does.
	awaitBlockOtherThan(t, srv, c, 6, 64, 3, planks)
}

// awaitBlockOtherThan reads packets until a Block Update for the position
// carrying anything but the state given arrives or the timeout runs out,
// stepping over the traffic the world makes around it. A burnt block's cell
// can be told about more than once on its way — the flame that eats it and
// the air that follows it — and the aging of a fire is not told at all, so
// the check is for the burn having reached the wire, not for a particular
// final state on it.
func awaitBlockOtherThan(t *testing.T, srv *server.Server, c *servertest.Client, x, y, z int32, not uint16) {
	t.Helper()
	if c.ReadTicks(srv, 200, func(packetID int32, data []byte) bool {
		if packetID != protocol.IDPlayBlockUpdate {
			return false
		}
		r := protocol.NewReader(data)
		gx, gy, gz := r.ReadPosition()
		return gx == x && gy == y && gz == z && uint16(r.ReadVarInt()) != not
	}) {
		return
	}
	t.Fatalf("no block update for (%d, %d, %d) arrived carrying anything but %d",
		x, y, z, not)
}

// TestFireOnNetherrackNeverBurnsOutOnTheServer checks the infiniburn rule
// through the server's own engine: fire on netherrack is still there, at the
// top of its age, after every visit that would have ended a fire anywhere
// else — through rain, too.
func TestFireOnNetherrackNeverBurnsOutOnTheServer(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Eternal", servertest.OpsListing(t, "Eternal"))

	setBlockCmd(t, c, 6, 63, 3, "netherrack")
	setBlockCmd(t, c, 6, 64, 3, "fire")
	c.ChatCommand("weather rain 4000")

	driveFire(t, srv, 2400)

	state := srv.BlockAt(6, 64, 3)
	if fireAge(state) != 15 {
		t.Fatalf("fire on netherrack after the run = %d, want fire aged to 15", fireAge(state))
	}
}

// TestRainPutsFireOutInTheOpen checks the douse at the black box: fire in
// the open under a raining sky is gone within the visits the hand-driven
// engine runs, and the air it left went out on the wire.
func TestRainPutsFireOutInTheOpen(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Doused", servertest.OpsListing(t, "Doused"))

	fire := setBlockCmd(t, c, 6, 64, 3, "fire")
	c.ChatCommand("weather rain 4000")

	driveFire(t, srv, 1000)

	if got := srv.BlockAt(6, 64, 3); got != 0 {
		t.Fatalf("fire in the open rain = %d, want air", got)
	}
	awaitBlockOtherThan(t, srv, c, 6, 64, 3, fire)
}

// TestFlintAndSteelLightsSoulFire checks the soul fire's lighting: the steel
// struck above soul sand lights the soul fire, the flame that never ages and
// never spreads.
func TestFlintAndSteelLightsSoulFire(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Necromancer", servertest.OpsListing(t, "Necromancer"))

	setBlockCmd(t, c, 3, 63, 3, "soul_sand")
	giveFlint(t, c)
	c.PlaceBlock(3, 63, 3, protocol.FaceTop)
	c.Settle()

	soul, ok := registry.FindState("minecraft:soul_fire", nil)
	if !ok {
		t.Fatal("the block registry has no soul fire")
	}
	if got := srv.BlockAt(3, 64, 3); got != soul {
		t.Errorf("the steel above soul sand lit %d, want the soul fire %d", got, soul)
	}
}

// TestFireSetDownInsideAFrameLightsThePortal checks the onPlace half of the
// portals: a fire that lands inside a complete obsidian frame — here by
// /setblock, the way a spread or a burnt-in flame lands — fills the frame
// with portal blocks, the way the vanilla placement's own portal check does.
func TestFireSetDownInsideAFrameLightsThePortal(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Kindler", servertest.OpsListing(t, "Kindler"))

	_, portalX, _, _ := servertest.PortalStates(t)
	servertest.BuildFrame(t, srv)

	// The fire goes into the interior's corner, where no flint and steel
	// check has run ahead of it.
	x, y, z := int32(servertest.PortalInnerX0), int32(servertest.PortalFloorY), int32(servertest.PortalInnerZ)
	xs, ys, zs := strconv.Itoa(int(x)), strconv.Itoa(int(y)), strconv.Itoa(int(z))
	c.ChatCommand("setblock " + xs + " " + ys + " " + zs + " fire")
	servertest.AwaitCommandFeedback(t, c, "commands.setblock.success", xs, ys, zs)
	// The flush reads the fire's cell once the placement's check has
	// filled the frame, so the cell arrives as portal.
	servertest.AwaitBlockUpdateSoon(t, c, x, y, z, portalX)
	c.Settle()
	for _, cell := range servertest.FrameInterior() {
		if got := srv.BlockAt(cell[0], cell[1], cell[2]); got != portalX {
			t.Errorf("interior block at (%d, %d, %d) = %d, want the portal's %d",
				cell[0], cell[1], cell[2], got, portalX)
		}
	}
}

// TestFireLitBesidePlanksSpreadsOnTheLoop is the live-loop shape of the
// burn: planks on three sides of a fire the steel lit are burnt away by the
// main loop's own ticks, with nothing driven by hand.
func TestFireLitBesidePlanksSpreadsOnTheLoop(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, fireConfig(), "Watchman", servertest.OpsListing(t, "Watchman"))

	planks := setBlockCmd(t, c, -3, 64, 3, "oak_planks")
	setBlockCmd(t, c, -1, 64, 3, "oak_planks")
	setBlockCmd(t, c, -2, 65, 3, "oak_planks")
	giveFlint(t, c)
	// The steel struck on the stone in the planks' middle.
	c.PlaceBlock(-2, 63, 3, protocol.FaceTop)
	c.Settle()

	if got := srv.BlockAt(-2, 64, 3); fireAge(got) < 0 {
		t.Fatalf("the steel lit %d, want fire", got)
	}

	// The planks go: the loop's own dice burn them away in the end, and
	// the test waits out the ticks that take, watching all three — the
	// burn's dice pick a mouth, not a block.
	servertest.HoldTicks(t, srv)
	servertest.UntilTicks(t, srv, 900, "the planks stood the whole wait and were not burnt away", func() bool {
		return srv.BlockAt(-3, 64, 3) != planks || srv.BlockAt(-1, 64, 3) != planks ||
			srv.BlockAt(-2, 65, 3) != planks
	})
}
