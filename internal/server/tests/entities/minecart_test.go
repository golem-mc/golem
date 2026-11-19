package entities

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The minecart tests, black-box: the cart a rail item stands and the ride
// it then takes, driven through a real connection in the flat world the
// harness grows. A lit powered rail shoves a resting cart off along its
// line; the line's slopes lift it and its curves bend it; a rider boards
// by the click and dismounts by the sneak, carried where the cart holds
// them while they ride; a detector rail under the wheels signals while
// the cart crosses it; and a sword's blow breaks the cart and leaves the
// item it was.

// cartPlayer joins a player of the game mode given holding count
// minecarts, standing where the flat world's ground is. The connect it
// hands back raises the further clients the watching tests read.
func cartPlayer(t *testing.T, gameMode, name string, count int32) (*server.Server, *servertest.Client, func() *servertest.Client) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.GameMode = gameMode
	srv, connect := servertest.Start(t, cfg)
	c := connect().Join(name)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	if count > 0 {
		srv.GiveSlot(name, player.HotbarSlot,
			protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:minecart"), Count: count})
	}
	return srv, c, connect
}

// awaitMinecartSpawn reads until a cart's Spawn Entity arrives.
func awaitMinecartSpawn(c *servertest.Client) servertest.BoatSpawn {
	c.T.Helper()
	return servertest.AwaitBoatSpawn(c, servertest.CartMinecartType(c.T))
}

// TestALaidMinecartStandsOnTheRailAndIsSpent: the minecart item used on a
// rail stands the cart at the rail's centre a sixteenth over its floor,
// spends the item outside creative, and shows every watcher a minecart
// arrival.
// vanilla: MinecartItem.useOn
func TestALaidMinecartStandsOnTheRailAndIsSpent(t *testing.T) {
	t.Parallel()
	srv, c, _ := cartPlayer(t, "survival", "Layer", 2)
	servertest.CartRails(t, srv, 2, 5)
	c.MoveTo(2.5, 64, 2.9, 0, 60) // south of the line, looking north at it
	c.Settle()

	c.PlaceBlock(2, 64, 1, protocol.FaceTop)
	spawned := awaitMinecartSpawn(c)
	if spawned.EntityID == 0 {
		t.Fatal("minecart spawn ID = 0, want a real entity ID")
	}
	if servertest.Abs64(spawned.X-2.5) > 0.001 || servertest.Abs64(spawned.Z-1.5) > 0.001 {
		t.Errorf("cart spawned at (%v, %v), want the clicked rail's centre (2.5, 1.5)", spawned.X, spawned.Z)
	}
	if got, want := spawned.Y, 64.0625; servertest.Abs64(got-want) > 0.001 {
		t.Errorf("cart spawned at y=%v, want the rail's lane %v", got, want)
	}
	servertest.PollUntil(t, 10*time.Second, func() bool {
		return srv.InventorySlot("Layer", player.HotbarSlot).Count == 1
	})
	if x, _, _, _, ok := srv.MinecartState(spawned.EntityID); !ok || math.Abs(x-2.5) > 0.001 {
		t.Errorf("the laid cart stands at x=%v (found %v), want where the item laid it", x, ok)
	}
}

// TestAPoweredRailShovesARestingCartAlongItsLine is the issue's own: a
// rail line, a minecart laid on a lit powered rail, and the pushing block
// behind the rail — the cart rides east along the line, pinned to its
// lane, and the empty cart's running pace spends itself to a stand before
// the line's end.
// vanilla: OldMinecartBehavior.moveAlongTrack's power half
func TestAPoweredRailShovesARestingCartAlongItsLine(t *testing.T) {
	t.Parallel()
	srv, c, _ := cartPlayer(t, "survival", "Driver", 1)
	servertest.CartRails(t, srv, 2, 14)
	// The pusher: a stone ending the line behind the lit rail, which the
	// rest-launch shoves the cart away from.
	if !srv.SetBlock(1, 64, 1, servertest.MustState(t, "minecraft:stone", nil)) ||
		!srv.SetBlock(2, 64, 1, servertest.MustState(t, "minecraft:powered_rail",
			map[string]string{"shape": "east_west", "powered": "true"})) {
		t.Fatal("the launcher's chunks are not loaded")
	}
	c.MoveTo(2.5, 64, 2.9, 0, 60)
	c.Settle()
	srv.StopTick()

	c.PlaceBlock(2, 64, 1, protocol.FaceTop)
	spawned := awaitMinecartSpawn(c)
	cartID := spawned.EntityID

	// Two ticks in, the shove has landed and the first step east been
	// taken.
	srv.TickMinecarts(2)
	x, _, z, _, ok := srv.MinecartState(cartID)
	if !ok {
		t.Fatal("the cart is gone from the world")
	}
	if x <= 2.5 {
		t.Fatalf("two ticks in the cart stands at x=%v, want shoved east of where it was laid", x)
	}
	if servertest.Abs64(z-1.5) > 0.001 {
		t.Errorf("the cart rides at z=%v, want pinned to its lane's z=1.5", z)
	}

	// The ride runs east and spends itself to a stand on the line.
	srv.TickMinecarts(200)
	x2, y, _, _, ok := srv.MinecartState(cartID)
	if !ok {
		t.Fatalf("the cart is gone from the world mid-line (last x=%v)", x2)
	}
	if x2 <= x {
		t.Fatalf("the cart stands at x=%v, want further east than the first step's %v", x2, x)
	}
	if x2 > 14.5 {
		t.Fatalf("the cart ran on to x=%v, want the line's end holding it", x2)
	}
	if got, want := y, 64.0625; servertest.Abs64(got-want) > 0.001 {
		t.Errorf("the cart rides the flat line at y=%v, want its lane %v", got, want)
	}
	// The ride told its own: the packets a watcher holds carry the cart
	// east, one whole relative step about it at least.
	if !c.ReadTicksSkipping(srv, 100, servertest.BoatSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot:
			r := protocol.NewReader(data)
			if r.ReadVarInt() != cartID {
				return false
			}
			r.ReadVarInt() // properties
			dx := float64(r.ReadI16()) / 4096
			return dx > 0
		}
		return false
	}) {
		t.Error("no movement packet carried the cart east")
	}
}

// TestAMinecartClimbsASlopeOntoTheRaisedLine: a cart with a powered start
// reaches the slope and rides it up the whole cell — its height climbing
// the way the slope's lane pages it — onto the raised line beyond, where
// it carries on east at that line's own height.
// vanilla: OldMinecartBehavior.moveAlongTrack's ascending half
func TestAMinecartClimbsASlopeOntoTheRaisedLine(t *testing.T) {
	t.Parallel()
	srv, c, _ := cartPlayer(t, "survival", "Climber", 1)
	// The launcher, the flats, the slope, and the raised line with the
	// stone its rails stand on. Three lit rails: the slope takes the
	// head of steam one alone will not give.
	stone := servertest.MustState(t, "minecraft:stone", nil)
	if !srv.SetBlock(1, 64, 1, stone) {
		t.Fatal("the pusher did not stand")
	}
	for x := int32(2); x <= 4; x++ {
		if !srv.SetBlock(x, 64, 1, servertest.MustState(t, "minecraft:powered_rail",
			map[string]string{"shape": "east_west", "powered": "true"})) {
			t.Fatalf("the launch rail at (%d, 64, 1) did not stand", x)
		}
	}
	for x := int32(5); x <= 6; x++ {
		if !srv.SetBlock(x, 64, 1, servertest.MustState(t, "minecraft:rail",
			map[string]string{"shape": "east_west"})) {
			t.Fatalf("the rail at (%d, 64, 1) did not stand", x)
		}
	}
	if !srv.SetBlock(7, 64, 1, servertest.MustState(t, "minecraft:rail",
		map[string]string{"shape": "ascending_east"})) {
		t.Fatal("the slope did not stand")
	}
	for x := int32(8); x <= 12; x++ {
		if !srv.SetBlock(x, 64, 1, stone) ||
			!srv.SetBlock(x, 65, 1, servertest.MustState(t, "minecraft:rail",
				map[string]string{"shape": "east_west"})) {
			t.Fatalf("the raised line at (%d, 65, 1) did not stand", x)
		}
	}
	c.MoveTo(2.5, 64, 2.9, 0, 60)
	c.Settle()
	srv.StopTick()

	c.PlaceBlock(2, 64, 1, protocol.FaceTop)
	spawned := awaitMinecartSpawn(c)
	cartID := spawned.EntityID

	// Twenty-four ticks in, the ride is onto the raised line: the lane a
	// sixteenth over its rails, the x run still climbing east.
	srv.TickMinecarts(24)
	x, y, _, _, ok := srv.MinecartState(cartID)
	if !ok {
		t.Fatal("the cart is gone from the world")
	}
	if x < 8 || x > 12 {
		t.Fatalf("the cart stands at x=%v, want it up the slope onto the raised line", x)
	}
	if got, want := y, 65.0625; servertest.Abs64(got-want) > 0.001 {
		t.Errorf("the cart climbs to y=%v, want the raised line's lane %v", got, want)
	}
	// The ride carries on east along the raised line.
	srv.TickMinecarts(30)
	x2, _, _, _, ok := srv.MinecartState(cartID)
	if !ok {
		t.Fatal("the cart is gone from the world")
	}
	if x2 <= x {
		t.Errorf("the cart stands at x=%v, want the climb's run past %v east", x2, x)
	}
}

// TestAPlayerBoardsAMinecartAndSneakDismounts: a right-click on a cart
// takes the clicker aboard, the passengers packet naming them for every
// viewer at the seat the server keeps them, and a sneak of the Player
// Input packet puts them off again, standing beside the cart.
// vanilla: Minecart.interact, VehicleEntity's startRiding and dismount
func TestAPlayerBoardsAMinecartAndSneakDismounts(t *testing.T) {
	t.Parallel()
	srv, c, connect := cartPlayer(t, "creative", "Rider", 1)
	servertest.CartRails(t, srv, 3, 6)
	cartID := srv.SpawnMinecart(3.5, 64.0625, 1.5)
	if cartID == 0 {
		t.Fatal("SpawnMinecart came to nothing")
	}
	// A watcher reads the boarding off the wire.
	watcher := connect().Join("Watcher")
	watcher.Settle()

	// A sneaking click boards nothing.
	c.Send(protocol.IDPlayInteractIn, func(b *protocol.Buffer) {
		b.PutVarInt(cartID)
		b.PutVarInt(protocol.HandMain)
		b.PutLpVec3(0.5, 0.5, 0.5)
		b.PutBool(true) // sneaking
	})
	c.Settle()
	if riders := srv.MinecartPassengerIDs(cartID); len(riders) != 0 {
		t.Fatalf("the sneaking click boarded %v, want nobody", riders)
	}

	c.MoveTo(3.5, 64, 2.5, 0, 60)
	c.Settle()
	c.UseOn(cartID)
	if riders := servertest.AwaitPassengersFor(c, cartID); len(riders) != 1 || riders[0] != c.PlayerEntityID {
		t.Errorf("passengers = %v, want the clicker's own %d", riders, c.PlayerEntityID)
	}
	if rider, ok := srv.MinecartRider(cartID); !ok || rider != "Rider" {
		t.Errorf("the cart's rider = %q (%v), want Rider", rider, ok)
	}
	// The seat keeps the rider at the attachment: 0.1875 up the cart.
	if pos := srv.PlayerPosition("Rider"); servertest.Abs64(pos.X-3.5) > 0.001 ||
		servertest.Abs64(pos.Z-1.5) > 0.001 || servertest.Abs64(pos.Y-(64.0625+0.1875)) > 0.001 {
		t.Errorf("the rider sits at (%v, %v, %v), want the cart's seat", pos.X, pos.Y, pos.Z)
	}
	if riders := srv.MinecartPassengerIDs(cartID); len(riders) != 1 {
		t.Fatalf("the cart holds %v, want the rider aboard", riders)
	}

	// The sneak of the Player Input steps them off, beside the cart.
	c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) {
		b.PutU8(protocol.InputSneak)
	})
	if riders := servertest.AwaitPassengersFor(c, cartID); len(riders) != 0 {
		t.Errorf("passengers after the sneak = %v, want none", riders)
	}
	if _, ok := srv.MinecartRider(cartID); ok {
		t.Error("the cart still holds a rider after the sneak")
	}
	pos := srv.PlayerPosition("Rider")
	if servertest.Abs64(pos.Y-64) > 0.001 {
		t.Errorf("the dismounted rider stands at y=%v, want the ground's top 64", pos.Y)
	}
	if servertest.Abs64(pos.X-3.5) < 1e-9 && servertest.Abs64(pos.Z-1.5) < 1e-9 {
		t.Error("the dismounted rider stands where the cart was, want the step out beside it")
	}
}

// TestAMinecartCarriesItsRiderAlongTheLine: the server drives the cart
// every step of the ride, so the rider it carries moves with it — the seat
// the server holds them at following the cart down the line, away from
// where the boarding sat them.
// vanilla: Entity.positionRider
func TestAMinecartCarriesItsRiderAlongTheLine(t *testing.T) {
	t.Parallel()
	srv, c, _ := cartPlayer(t, "creative", "Rider", 1)
	stone := servertest.MustState(t, "minecraft:stone", nil)
	if !srv.SetBlock(1, 64, 1, stone) ||
		!srv.SetBlock(2, 64, 1, servertest.MustState(t, "minecraft:powered_rail",
			map[string]string{"shape": "east_west", "powered": "true"})) {
		t.Fatal("the launcher's chunks are not loaded")
	}
	for x := int32(3); x <= 9; x++ {
		if !srv.SetBlock(x, 64, 1, servertest.MustState(t, "minecraft:rail",
			map[string]string{"shape": "east_west"})) {
			t.Fatalf("the rail at (%d, 64, 1) did not stand", x)
		}
	}
	cartID := srv.SpawnMinecart(2.5, 64.0625, 1.5)
	if cartID == 0 {
		t.Fatal("SpawnMinecart came to nothing")
	}
	c.MoveTo(2.5, 64, 2.5, 0, 60)
	c.Settle()
	srv.StopTick()
	c.UseOn(cartID)
	if riders := servertest.AwaitPassengersFor(c, cartID); len(riders) != 1 {
		t.Fatalf("the cart holds %v, want the rider aboard", riders)
	}

	srv.TickMinecarts(2)
	x0, _, _, _, _ := srv.MinecartState(cartID)
	seated := srv.PlayerPosition("Rider")
	if servertest.Abs64(seated.X-x0) > 0.001 {
		t.Errorf("the rider stands at x=%v, want the cart's %v: the seat carries them", seated.X, x0)
	}
	// Ridden, the cart keeps to its slower pace: three-quarters of a step
	// a tick against the empty cart's whole, which the position at the
	// fixed tick count shows.
	srv.TickMinecarts(6)
	x1, _, _, _, _ := srv.MinecartState(cartID)
	carried := srv.PlayerPosition("Rider")
	if servertest.Abs64(carried.X-x1) > 0.001 {
		t.Errorf("the rider stands at x=%v after the ride, want the cart's %v", carried.X, x1)
	}
	if x1 <= x0 {
		t.Errorf("the ridden cart stands at x=%v, want east of %v", x1, x0)
	}
}

// TestADetectorRailSignalsWhileTheCartCrossesIt: the lit powered rail
// shoves the cart down the line and across a detector rail; the cart's
// crossing turns the detector on, its neighbor lamp lit — and once the
// cart has gone past, the twenty-tick look-back takes the press down
// again.
// vanilla: DetectorRailBlock.checkPressed
func TestADetectorRailSignalsWhileTheCartCrossesIt(t *testing.T) {
	t.Parallel()
	srv, c, _ := cartPlayer(t, "creative", "Watcher", 1)
	servertest.CartRails(t, srv, 3, 14)
	stone := servertest.MustState(t, "minecraft:stone", nil)
	if !srv.SetBlock(2, 64, 1, stone) ||
		!srv.SetBlock(3, 64, 1, servertest.MustState(t, "minecraft:powered_rail",
			map[string]string{"shape": "east_west", "powered": "true"})) {
		t.Fatal("the launcher's chunks are not loaded")
	}
	// The detector early in the line, where the ride is fast: the cart
	// crosses it and runs on, rather than stalling over it.
	if !srv.SetBlock(6, 64, 1, servertest.MustState(t, "minecraft:detector_rail",
		map[string]string{"shape": "east_west", "powered": "false"})) {
		t.Fatal("the detector did not stand")
	}
	if !srv.SetBlock(6, 64, 2, servertest.MustState(t, "minecraft:redstone_lamp", nil)) {
		t.Fatal("the lamp did not stand")
	}
	c.MoveTo(3.5, 64, 2.9, 0, 60)
	c.Settle()

	// The loop's own ticks carry the whole ride, so the touch passes and
	// the scheduled looks run as they would in the real game.
	c.PlaceBlock(3, 64, 1, protocol.FaceTop)
	spawned := awaitMinecartSpawn(c)
	cartID := spawned.EntityID

	pressed, _ := registry.FindState("minecraft:detector_rail",
		map[string]string{"shape": "east_west", "powered": "true"})
	lampOn, _ := registry.FindState("minecraft:redstone_lamp",
		map[string]string{"lit": "true"})
	// The press holds for twenty ticks, so waiting until the lamp has
	// followed it too leaves both standing when the assertions read them,
	// rather than catching the detector mid-tick before the lamp's wake.
	servertest.WaitForTicks(t, srv, 400, func() bool {
		return srv.BlockAt(6, 64, 1) == pressed && srv.BlockAt(6, 64, 2) == lampOn
	}, "the cart never pressed the detector and lit the lamp")
	servertest.ExpectBlockNamed(t, srv, 6, 64, 2, "minecraft:redstone_lamp",
		map[string]string{"lit": "true"})
	servertest.ExpectBlockNamed(t, srv, 6, 64, 1, "minecraft:detector_rail",
		map[string]string{"powered": "true"})

	// The cart runs on and the press comes down again: the twenty-tick
	// look-back finds no cart in the search box, the detector goes dark
	// and the lamp follows on its own turn-off's delay.
	lampOff, _ := registry.FindState("minecraft:redstone_lamp",
		map[string]string{"lit": "false"})
	servertest.WaitForTicks(t, srv, 400, func() bool {
		return srv.BlockAt(6, 64, 2) == lampOff
	}, "the detector's press never came down with the lamp")
	servertest.ExpectBlockNamed(t, srv, 6, 64, 1, "minecraft:detector_rail",
		map[string]string{"powered": "false"})
	servertest.ExpectBlockNamed(t, srv, 6, 64, 2, "minecraft:redstone_lamp",
		map[string]string{"lit": "false"})
	// The ride itself went on east, well past the detector.
	if x, _, _, _, ok := srv.MinecartState(cartID); !ok || x < 7.5 {
		t.Errorf("the cart stands at x=%v (found %v), want east past the detector", x, ok)
	}
}

// TestASwordBlowBreaksAMinecartAndDropsItSelf: a survival blow's
// accumulated damage past forty destroys the cart — one diamond sword's
// hit is far past it — leaving the minecart item standing where the wreck
// was.
// vanilla: VehicleEntity.hurtServer, VehicleEntity.destroy
func TestASwordBlowBreaksAMinecartAndDropsItSelf(t *testing.T) {
	t.Parallel()
	srv, c, connect := cartPlayer(t, "survival", "Breaker", 1)
	servertest.CartRails(t, srv, 3, 6)
	cartID := srv.SpawnMinecart(4.5, 64.0625, 1.5)
	if cartID == 0 {
		t.Fatal("SpawnMinecart came to nothing")
	}
	watcher := connect().Join("Watcher")
	watcher.Settle()

	srv.GiveSlot("Breaker", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:diamond_sword"), Count: 1})
	servertest.AwaitFullSwing(t, srv, "Breaker")
	c.MoveTo(4.5, 64, 2.7, 0, 60)
	c.Settle()
	c.Attack(cartID)

	// The wreck is removed from every view and its item left standing.
	removed := watcher.ReadTicksSkipping(srv, 100, servertest.BoatSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlayRemoveEntities {
			return false
		}
		r := protocol.NewReader(data)
		if count := r.ReadVarInt(); count != 1 {
			return false
		}
		return r.ReadVarInt() == cartID
	})
	if !removed {
		t.Error("the broken cart was never removed")
	}
	spawned := servertest.AwaitBoatSpawn(watcher, protocol.EntityTypeItem)
	if servertest.Abs64(spawned.X-4.5) > 0.001 || servertest.Abs64(spawned.Z-1.5) > 0.001 {
		t.Errorf("the drop stands at (%v, %v), want the wreck's column (4.5, 1.5)", spawned.X, spawned.Z)
	}
	if _, _, _, _, ok := srv.MinecartState(cartID); ok {
		t.Error("the wreck is still in the world")
	}
}

// expectItemToss checks a spawned item's velocity is a toss: 0.2 up and
// a sideways nudge within 0.1, which is never both exactly zero.
func expectItemToss(t *testing.T, got servertest.BoatSpawn) {
	t.Helper()
	if servertest.Abs64(got.Vy-0.2) > 0.01 {
		t.Errorf("drop vy = %v, want 0.2", got.Vy)
	}
	if servertest.Abs64(got.Vx) > 0.101 || servertest.Abs64(got.Vz) > 0.101 {
		t.Errorf("drop sideways velocity (%v, %v), want within 0.1", got.Vx, got.Vz)
	}
	if servertest.Abs64(got.Vx) < 1e-4 && servertest.Abs64(got.Vz) < 1e-4 {
		t.Errorf("drop sideways velocity (%v, %v), want a random nudge", got.Vx, got.Vz)
	}
}

// TestACreativeBlowDiscardsAMinecartWithNoDrop: a creative blow is
// instabuild's own answer — the cart goes at once and nothing is left
// where it stood.
// vanilla: VehicleEntity.hurtServer's discard
func TestACreativeBlowDiscardsAMinecartWithNoDrop(t *testing.T) {
	t.Parallel()
	srv, c, _ := cartPlayer(t, "creative", "Dropper", 1)
	servertest.CartRails(t, srv, 3, 6)
	cartID := srv.SpawnMinecart(4.5, 64.0625, 1.5)
	if cartID == 0 {
		t.Fatal("SpawnMinecart came to nothing")
	}
	c.MoveTo(4.5, 64, 2.7, 0, 60)
	c.Settle()
	servertest.AwaitFullSwing(t, srv, "Dropper")
	c.Attack(cartID)
	servertest.PollUntil(t, 10*time.Second, func() bool {
		_, _, _, _, ok := srv.MinecartState(cartID)
		return !ok
	})
	if got := srv.ItemCount(); got != 0 {
		t.Errorf("items standing = %d, want none: the creative blow discards with no drop", got)
	}
}

// TestARiderCrossingAPortalTakesTheirCartAlong: a rider whose cart stands
// in a lit portal crosses with it — vanilla's handlePortal carries the
// vehicle's own passengers — so the cart is re-homed in the dimension
// arrived at, still holding the rider.
// vanilla: Entity.handlePortal
func TestARiderCrossingAPortalTakesTheirCartAlong(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, c := servertest.Join(t, cfg, "Crosser")

	_, portalX, _, _ := servertest.PortalStates(t)
	servertest.BuildFrame(t, srv)
	servertest.Flint(t, c)
	c.PlaceBlock(servertest.PortalInnerX0, servertest.PortalFloorY-1, servertest.PortalInnerZ, protocol.FaceTop)
	c.Settle()
	if got := srv.BlockAt(servertest.PortalInnerX0, servertest.PortalFloorY, servertest.PortalInnerZ); got != portalX {
		t.Fatalf("the portal interior is %d, want the lit portal's %d", got, portalX)
	}

	// An empty cart in the portal crosses alone on its first tick, so no
	// tick runs until the rider is aboard.
	servertest.HoldTicks(t, srv)
	cartID := srv.SpawnMinecart(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, float64(servertest.PortalInnerZ)+0.5)
	if cartID == 0 {
		t.Fatal("SpawnMinecart came to nothing")
	}
	// Board from beside the portal: the seat puts the rider in the portal
	// block, and a creative player crosses on the first tick of standing.
	c.MoveTo(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, float64(servertest.PortalInnerZ)+1.5, 0, 0)
	c.Settle()
	c.UseOn(cartID)
	if riders := servertest.AwaitPassengersFor(c, cartID); len(riders) != 1 {
		t.Fatalf("the cart holds %v, want the rider aboard", riders)
	}
	servertest.ReleaseTicks(t, srv)
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn); !ok {
		t.Fatal("no respawn arrived for the riding crossing")
	}
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("the nether never arrived around the crossing")
	}
	if got := srv.PlayerDimension("Crosser"); got != world.NetherName {
		t.Fatalf("dimension after the crossing = %q, want the nether", got)
	}
	// The cart crossed with the rider and still holds them, in the
	// dimension arrived at.
	if dim := srv.MinecartDimension(cartID); dim != world.NetherName {
		t.Errorf("the cart is in %q, want the nether with its rider", dim)
	}
	if rider, ok := srv.MinecartRider(cartID); !ok || rider != "Crosser" {
		t.Errorf("the cart's rider = %q (%v), want Crosser still aboard", rider, ok)
	}
	if riders := srv.MinecartPassengerIDs(cartID); len(riders) != 1 {
		t.Errorf("passengers after the crossing = %v, want the rider", riders)
	}
}

// TestATeleportedRiderLeavesTheCartBehind: a command's teleport across
// dimensions drops the cart, since ServerPlayer.teleport removes the
// vehicle unless the transition is as a passenger — only the vehicle's own
// portal crossing carries its rider.
// vanilla: ServerPlayer.teleport
func TestATeleportedRiderLeavesTheCartBehind(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, c := servertest.Join(t, cfg, "Teleporter", servertest.OpsListing(t, "Teleporter"))

	servertest.CartRails(t, srv, 3, 6)
	cartID := srv.SpawnMinecart(3.5, 64.0625, 1.5)
	if cartID == 0 {
		t.Fatal("SpawnMinecart came to nothing")
	}
	c.MoveTo(3.5, 64, 2.5, 0, 60)
	c.Settle()
	c.UseOn(cartID)
	if riders := servertest.AwaitPassengersFor(c, cartID); len(riders) != 1 {
		t.Fatalf("the cart holds %v, want the rider aboard", riders)
	}

	// Once the rider is gone the overworld chunk would unload and take the
	// cart with it; the test reads the cart where it stayed.
	srv.RunLine("forceload add 0 0")
	c.ChatCommand("execute in minecraft:the_nether run tp @s 8 40 8")
	servertest.PollUntil(t, 20*time.Second, func() bool {
		return srv.PlayerDimension("Teleporter") == world.NetherName
	})
	// The cart stayed in the overworld, its seat given back.
	if dim := srv.MinecartDimension(cartID); dim != world.DimensionName {
		t.Errorf("the cart is in %q, want left behind in the overworld", dim)
	}
	if rider, ok := srv.MinecartRider(cartID); ok {
		t.Errorf("the cart's rider = %q, want none after the teleport", rider)
	}
	if riders := srv.MinecartPassengerIDs(cartID); len(riders) != 0 {
		t.Errorf("passengers after the teleport = %v, want none", riders)
	}
	// The nether's stream read through, so the server is not left holding
	// chunks nobody is reading.
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn); !ok {
		t.Fatal("no respawn arrived for the teleport")
	}
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("the nether never arrived around the teleport")
	}
}

// TestARiderHoldingForwardNudgesTheCart: a player at rest on a flat rail who
// holds forward creeps the cart along their heading, and on a dark powered
// rail the nudge overrides the halt. Without the key the cart stays put.
// vanilla: OldMinecartBehavior.moveAlongTrack, ServerPlayer.getLastClientMoveIntent
func TestARiderHoldingForwardNudgesTheCart(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		first string // the block under the cart at x=3
	}{
		{"rail", "minecraft:rail"},
		{"unpowered powered rail", "minecraft:powered_rail"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c, _ := cartPlayer(t, "creative", "Rider", 1)
			servertest.CartRails(t, srv, 4, 8)
			if !srv.SetBlock(3, 64, 1, servertest.MustState(t, tc.first,
				map[string]string{"shape": "east_west"})) {
				t.Fatal("the first rail did not stand")
			}
			cartID := srv.SpawnMinecart(3.5, 64.0625, 1.5)
			if cartID == 0 {
				t.Fatal("SpawnMinecart came to nothing")
			}
			c.MoveTo(3.5, 64, 2.5, 0, 60)
			c.Settle()
			srv.StopTick()
			c.UseOn(cartID)
			if riders := servertest.AwaitPassengersFor(c, cartID); len(riders) != 1 {
				t.Fatalf("the cart holds %v, want the rider aboard", riders)
			}
			// Face east, down the line.
			c.Send(protocol.IDPlayRotation, func(b *protocol.Buffer) {
				b.PutF32(-90)
				b.PutF32(0)
				b.PutU8(0)
			})
			c.Settle()
			srv.TickMinecarts(5)
			x0, _, _, _, _ := srv.MinecartState(cartID)
			if servertest.Abs64(x0-3.5) > 1e-9 {
				t.Fatalf("the cart stands at x=%v with no key held, want it still at 3.5", x0)
			}

			c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) {
				b.PutU8(protocol.InputForward)
			})
			c.Settle()
			srv.TickMinecarts(60)
			x1, _, z1, _, _ := srv.MinecartState(cartID)
			if x1 < 4 {
				t.Errorf("the cart stands at x=%v after 60 ticks of forward, want it rolled east past 4", x1)
			}
			if servertest.Abs64(z1-1.5) > 1e-9 {
				t.Errorf("the cart stands at z=%v, want it kept on the rail's 1.5", z1)
			}
			if yaw := srv.PlayerPosition("Rider").Yaw; yaw != -90 {
				t.Errorf("the rider faces yaw %v after the ride, want the -90 they turned to: the seat moves, not turns, them", yaw)
			}
		})
	}
}

// vanilla: AbstractBoat.canVehicleCollide, Entity.collide
func TestACartMoveStopsAtMobsAndVehicles(t *testing.T) {
	for _, onRail := range []bool{true, false} {
		for _, kind := range []string{"cow", "boat", "cart"} {
			t.Run(fmt.Sprintf("%s/rail=%v", kind, onRail), func(t *testing.T) {
				t.Parallel()
				srv, _, _ := cartPlayer(t, "creative", "Watcher", 0)
				servertest.HoldTicks(t, srv)
				if onRail {
					rail := servertest.MustState(t, "minecraft:rail", map[string]string{"shape": "east_west"})
					for x := int32(2); x <= 6; x++ {
						srv.SetBlock(x, 64, 5, rail)
					}
				}
				cart := srv.SpawnMinecart(3, 64.0625, 5.5)
				halfWidth := 0.49
				switch kind {
				case "cow":
					halfWidth = 0.45
					cow := srv.SpawnMob("minecraft:cow", 4.2, 64, 5.5)
					srv.SetMobNoAI(cow)
				case "boat":
					halfWidth = 0.6875
					srv.SpawnBoat(4.2, 64, 5.5, "minecraft:oak_boat")
				case "cart":
					srv.SpawnMinecart(4.2, 64.0625, 5.5)
				}
				srv.SetMinecartMotion(cart, 0.4, 0, 0)
				servertest.RunTicks(t, srv, 1)
				wantX := 4.2 - halfWidth - 0.49
				if x, _, _, _, ok := srv.MinecartState(cart); !ok || math.Abs(x-wantX) > 1e-6 {
					t.Fatalf("cart x=%v (present %v), want contact with the %s at x=%v", x, ok, kind, wantX)
				}
			})
		}
	}
}
