package entities

import (
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
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

// TestAMinecartBendsRoundACurveOntoTheCrossLine: the northbound ride
// through a south-east curve comes out of it eastbound: the z run ends at
// the curve's own lane and the x run carries the speed the turn kept.
// vanilla: RailBlock's south_east, OldMinecartBehavior.moveAlongTrack
func TestAMinecartBendsRoundACurveOntoTheCrossLine(t *testing.T) {
	t.Parallel()
	srv, c, _ := cartPlayer(t, "survival", "Bender", 1)
	// A lit powered north-south rail whose south end a stone holds, a
	// northbound line into the curve, and the cross line east out of it.
	if !srv.SetBlock(2, 64, 7, servertest.MustState(t, "minecraft:stone", nil)) ||
		!srv.SetBlock(2, 64, 6, servertest.MustState(t, "minecraft:powered_rail",
			map[string]string{"shape": "north_south", "powered": "true"})) {
		t.Fatal("the launcher's chunks are not loaded")
	}
	for z := int32(4); z <= 5; z++ {
		if !srv.SetBlock(2, 64, z, servertest.MustState(t, "minecraft:rail",
			map[string]string{"shape": "north_south"})) {
			t.Fatalf("the rail at (2, 64, %d) did not stand", z)
		}
	}
	if !srv.SetBlock(2, 64, 3, servertest.MustState(t, "minecraft:rail",
		map[string]string{"shape": "south_east"})) {
		t.Fatal("the curve did not stand")
	}
	for x := int32(3); x <= 12; x++ {
		if !srv.SetBlock(x, 64, 3, servertest.MustState(t, "minecraft:rail",
			map[string]string{"shape": "east_west"})) {
			t.Fatalf("the rail at (%d, 64, 3) did not stand", x)
		}
	}
	c.MoveTo(2.5, 64, 6.9, 0, 60)
	c.Settle()
	srv.StopTick()

	c.PlaceBlock(2, 64, 6, protocol.FaceTop)
	spawned := awaitMinecartSpawn(c)
	cartID := spawned.EntityID

	srv.TickMinecarts(100)
	x, y, z, _, ok := srv.MinecartState(cartID)
	if !ok {
		t.Fatal("the cart is gone from the world")
	}
	if z > 3.51 {
		t.Fatalf("the northbound cart stands at z=%v, want it through the curve's lane", z)
	}
	if x <= 3.5 {
		t.Fatalf("the cart stands at x=%v after the curve, want the eastbound run", x)
	}
	if got, want := y, 64.0625; servertest.Abs64(got-want) > 0.001 {
		t.Errorf("the curve keeps the cart at y=%v, want its lane %v", got, want)
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

// TestABrokenMinecartDropsItsItemWithAnItemsToss: the cart's item leaves
// the wreck with the bare ItemEntity constructor's toss, up 0.2 and up to
// 0.1 to either side, not straight up.
// vanilla: Entity.spawnAtLocation, ItemEntity.<init>
func TestABrokenMinecartDropsItsItemWithAnItemsToss(t *testing.T) {
	t.Parallel()
	srv, c, connect := cartPlayer(t, "survival", "Breaker", 1)
	servertest.CartRails(t, srv, 3, 6)
	cartID := srv.SpawnMinecart(4.5, 64.0625, 1.5)
	watcher := connect().Join("Watcher")
	watcher.Settle()
	srv.GiveSlot("Breaker", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:diamond_sword"), Count: 1})
	servertest.AwaitFullSwing(t, srv, "Breaker")
	c.MoveTo(4.5, 64, 2.7, 0, 60)
	c.Settle()
	c.Attack(cartID)
	expectItemToss(t, servertest.AwaitBoatSpawn(watcher, protocol.EntityTypeItem))
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

// TestARiderCrossingAnEndPortalTakesTheirCartAlong: the end portal carries
// a rider's cart too, the same handlePortal path the nether portal runs.
// vanilla: Entity.handlePortal
func TestARiderCrossingAnEndPortalTakesTheirCartAlong(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, c := servertest.Join(t, cfg, "Ender")

	if !srv.SetBlock(10, servertest.PortalFloorY, 10, srv.EndPortalState()) {
		t.Fatal("placing the end portal block failed")
	}
	// An empty cart in the end portal crosses alone on its first tick, so
	// no tick runs until the rider is aboard.
	servertest.HoldTicks(t, srv)
	cartID := srv.SpawnMinecart(10.5, servertest.PortalFloorY, 10.5)
	if cartID == 0 {
		t.Fatal("SpawnMinecart came to nothing")
	}
	c.MoveTo(10.5, servertest.PortalFloorY, 11.5, 0, 0)
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
		t.Fatal("the end never arrived around the crossing")
	}
	if got := srv.PlayerDimension("Ender"); got != world.EndName {
		t.Fatalf("dimension after the crossing = %q, want the end", got)
	}
	if dim := srv.MinecartDimension(cartID); dim != world.EndName {
		t.Errorf("the cart is in %q, want the end with its rider", dim)
	}
	if rider, ok := srv.MinecartRider(cartID); !ok || rider != "Ender" {
		t.Errorf("the cart's rider = %q (%v), want Ender still aboard", rider, ok)
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

// TestADeadOrSpectatingRiderLosesTheSeatToo: the death and the spectator
// takes the other two ways a seat is given back, and both step... and the
// death leaves the body where the seat held it while the spectator switch
// keeps standing beside the cart.
// vanilla: ServerPlayer.die's startRiding clean-up, ServerPlayer.setGameMode
func TestADeadOrSpectatingRiderLosesTheSeatToo(t *testing.T) {
	t.Parallel()
	// An operator, so the own gamemode switch the test drives answers.
	cfg := servertest.BlockTestConfig()
	cfg.GameMode = "survival"
	srv, c := servertest.Join(t, cfg, "Casualty", servertest.OpsListing(t, "Casualty"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
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

	// The spectator switch: the seat is stepped out of, beside the cart.
	c.ChatCommand("gamemode spectator")
	if riders := servertest.AwaitPassengersFor(c, cartID); len(riders) != 0 {
		t.Errorf("passengers after the spectator switch = %v, want none", riders)
	}
	if _, ok := srv.MinecartRider(cartID); ok {
		t.Error("the cart still holds a rider after the spectator switch")
	}
	if pos := srv.PlayerPosition("Casualty"); servertest.Abs64(pos.Y-64) > 0.001 {
		t.Errorf("the stepped-out rider stands at y=%v, want the ground", pos.Y)
	}

	// Back to the seat, and the death: the seat is given back with the
	// body left where it sat.
	c.ChatCommand("gamemode survival")
	c.MoveTo(3.5, 64, 2.5, 0, 60)
	c.Settle()
	c.UseOn(cartID)
	if riders := servertest.AwaitPassengersFor(c, cartID); len(riders) != 1 {
		t.Fatalf("the cart holds %v before the death, want the rider back aboard", riders)
	}
	seated := srv.PlayerPosition("Casualty")
	c.ChatCommand("kill")
	if riders := servertest.AwaitPassengersFor(c, cartID); len(riders) != 0 {
		t.Errorf("passengers after the death = %v, want none", riders)
	}
	if _, ok := srv.MinecartRider(cartID); ok {
		t.Error("the cart still holds a rider after the death")
	}
	rests := srv.PlayerPosition("Casualty")
	if servertest.Abs64(rests.X-seated.X) > 0.001 || servertest.Abs64(rests.Z-seated.Z) > 0.001 {
		t.Errorf("the dead body rests at (%v, %v), want the seat's (%v, %v)",
			rests.X, rests.Z, seated.X, seated.Z)
	}
}

// TestARiderSwitchesBetweenACartAndABoat: vanilla's startRiding gives up
// the vehicle a rider already holds before it seats them on the new one,
// so a cart rider who boards a boat is left in the boat alone — and back
// the other way.
// vanilla: Entity.startRiding
func TestARiderSwitchesBetweenACartAndABoat(t *testing.T) {
	t.Parallel()
	srv, c, _ := cartPlayer(t, "creative", "Switcher", 1)
	servertest.CartRails(t, srv, 3, 6)
	cartID := srv.SpawnMinecart(3.5, 64.0625, 1.5)
	boatID := srv.SpawnBoat(3.5, 64, 2.5, "minecraft:oak_boat")
	if cartID == 0 || boatID == 0 {
		t.Fatal("the cart or the boat did not stand")
	}
	c.MoveTo(3.5, 64, 2.0, 0, 60)
	c.Settle()

	c.UseOn(cartID)
	if riders := servertest.AwaitPassengersFor(c, cartID); len(riders) != 1 {
		t.Fatalf("the cart holds %v, want the rider aboard", riders)
	}
	// Onto the boat: the cart gives the rider up.
	c.UseOn(boatID)
	if riders := servertest.AwaitPassengersFor(c, boatID); len(riders) != 1 || riders[0] != c.PlayerEntityID {
		t.Fatalf("the boat holds %v, want the clicker's own %d", riders, c.PlayerEntityID)
	}
	if _, ok := srv.MinecartRider(cartID); ok {
		t.Error("the cart still holds the rider after the switch to the boat")
	}
	if riders := srv.MinecartPassengerIDs(cartID); len(riders) != 0 {
		t.Errorf("the cart holds %v, want none", riders)
	}
	// Back onto the cart: the boat gives the rider up.
	c.UseOn(cartID)
	if riders := servertest.AwaitPassengersFor(c, cartID); len(riders) != 1 || riders[0] != c.PlayerEntityID {
		t.Fatalf("the cart holds %v, want the clicker's own %d", riders, c.PlayerEntityID)
	}
	if _, ok := srv.BoatRider(boatID); ok {
		t.Error("the boat still holds the rider after the switch back to the cart")
	}
	if riders := srv.BoatPassengerIDs(boatID); len(riders) != 0 {
		t.Errorf("the boat holds %v, want none", riders)
	}
}

// TestACartRiderLaysABoat: a body in a cart is a passenger, and vanilla's
// BoatItem.use does not ask about seats — the boat is laid and spent.
// vanilla: BoatItem.use
func TestACartRiderLaysABoat(t *testing.T) {
	t.Parallel()
	srv, c, _ := cartPlayer(t, "survival", "Packer", 1)
	srv.GiveSlot("Packer", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:oak_boat"), Count: 1})
	c.MoveToGround(8.5, 64, 8.5, true)
	c.Settle()
	cartID := srv.SpawnMinecart(8.5, 64.0625, 8.5)
	if cartID == 0 {
		t.Fatal("the cart did not stand")
	}
	c.UseOn(cartID)
	if riders := servertest.AwaitPassengersFor(c, cartID); len(riders) != 1 {
		t.Fatalf("the cart holds %v, want the rider aboard", riders)
	}
	// Look down at the open ground beside the cart and use the held boat.
	c.UseBoatItem(0, 60)
	c.Settle()
	if got := srv.BoatCount(); got != 1 {
		t.Errorf("boats standing = %d, want 1: a cart rider lays a boat", got)
	}
	if got := srv.InventorySlot("Packer", player.HotbarSlot).Count; got != 0 {
		t.Errorf("boats left = %d, want 0: the laid one was spent", got)
	}
}

// TestASneakingUseOfACartDoesNotRaceTheStep: a cart's step moves it on the
// tick goroutine under the cart lock, and a use's reach check reads its box.
// The two overlap for as long as the click takes, so the loop hammers one
// sneaking click — which reads the box and mounts nothing, drawing no reply
// the read would have to keep up with — while the cart ticks; the race
// detector is the assertion, the empty seat the plain one.
func TestASneakingUseOfACartDoesNotRaceTheStep(t *testing.T) {
	t.Parallel()
	srv, c, _ := cartPlayer(t, "creative", "Cranker", 0)
	c.MoveToGround(8.5, 64, 8.5, true)
	c.Settle()
	cartID := srv.SpawnMinecart(8.5, 64.0625, 8.5)
	if cartID == 0 {
		t.Fatal("the cart did not stand")
	}
	c.Settle()
	for range 50000 {
		c.Send(protocol.IDPlayInteractIn, func(b *protocol.Buffer) {
			b.PutVarInt(cartID)
			b.PutVarInt(protocol.HandMain)
			b.PutLpVec3(0.1, 0.9, 0.2)
			b.PutBool(true) // the sneak key, which mounts nothing
		})
	}
	c.Settle()
	if riders := srv.MinecartPassengerIDs(cartID); len(riders) != 0 {
		t.Errorf("the sneaking use boarded %v, want nobody", riders)
	}
}

// TestAnOutOfReachAttackOnACartDoesNotRaceTheStep: a cart's step moves it on
// the tick goroutine under the cart lock, and a blow reads its box to decide
// the reach before it does anything else. A swing from beyond the reach reads
// the box and returns without touching the cart, so the loop hammers one
// while the cart ticks; the race detector is the assertion, the cart still
// standing behind it.
func TestAnOutOfReachAttackOnACartDoesNotRaceTheStep(t *testing.T) {
	t.Parallel()
	srv, c, _ := cartPlayer(t, "creative", "Striker", 0)
	cartID := srv.SpawnMinecart(8.5, 64.0625, 8.5)
	if cartID == 0 {
		t.Fatal("the cart did not stand")
	}
	c.Settle()
	for range 50000 {
		c.Attack(cartID)
	}
	c.Settle()
	if _, _, _, _, ok := srv.MinecartState(cartID); !ok {
		t.Error("the out-of-reach blows broke the cart, want it standing")
	}
}

// TestASneakOfACartRiderDoesNotRaceTheStep: a cart's rider steps off on the
// connection goroutine, reading the cart's direction and where it stands, and
// the step keeps moving the cart on the tick goroutine under the lock. The
// loop boards and steps off again and again — one click and one sneak — so the
// read happens often enough for the race detector to catch it; the empty seat
// is the plain assertion.
func TestASneakOfACartRiderDoesNotRaceTheStep(t *testing.T) {
	t.Parallel()
	srv, c, _ := cartPlayer(t, "creative", "Boarder", 0)
	cartID := srv.SpawnMinecart(0.5, 64.0625, 0.5)
	if cartID == 0 {
		t.Fatal("the cart did not stand")
	}
	c.Settle()
	for range 25000 {
		c.UseOn(cartID)
		c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) {
			b.PutU8(protocol.InputSneak)
		})
	}
	c.Settle()
	if riders := srv.MinecartPassengerIDs(cartID); len(riders) != 0 {
		t.Errorf("the cart holds %v after the last sneak, want nobody", riders)
	}
}

// TestACartRidersPortalWaitIsTheCartsOwn: the cart crosses on the tick it
// enters a nether portal whatever game mode its rider is in, and the
// cooldown it takes is the player passenger's.
// vanilla: NetherPortalBlock.getPortalTransitionTime, Entity.getDimensionChangingDelay
func TestACartRidersPortalWaitIsTheCartsOwn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		cfg  func() *config.Config
	}{
		{"survival", servertest.SurvivalTestConfig},
		{"creative", servertest.BlockTestConfig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, tc.cfg(), "Crosser")

			servertest.PortalFrame(t, srv)
			srv.StopTick()
			cartID := srv.SpawnMinecart(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, float64(servertest.PortalInnerZ)+0.5)
			c.MoveTo(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, float64(servertest.PortalInnerZ)+1.5, 0, 0)
			c.Settle()
			c.UseOn(cartID)
			if riders := servertest.AwaitPassengersFor(c, cartID); len(riders) != 1 {
				t.Fatalf("the cart holds %v, want the rider aboard", riders)
			}
			srv.TickPortals()
			servertest.PollUntil(t, 20*time.Second, func() bool {
				return srv.PlayerDimension("Crosser") == world.NetherName
			})
			if dim := srv.MinecartDimension(cartID); dim != world.NetherName {
				t.Errorf("the cart is in %q, want the nether with its rider", dim)
			}
			if got, want := srv.PortalCooldown("Crosser"), int32(10); got != want {
				t.Errorf("cooldown after the crossing = %d, want %d", got, want)
			}
		})
	}
}

// TestARiderSwitchesBetweenCartsAndKeepsTheirSeatOnAFullOne: a cart rider
// who clicks a free cart moves over, while a rider of a boat or cart who
// clicks a held cart stays where they were.
// vanilla: Entity.startRiding
func TestARiderSwitchesBetweenCartsAndKeepsTheirSeatOnAFullOne(t *testing.T) {
	t.Parallel()
	srv, c, connect := cartPlayer(t, "creative", "Hopper", 1)
	servertest.CartRails(t, srv, 3, 8)
	first := srv.SpawnMinecart(3.5, 64.0625, 1.5)
	second := srv.SpawnMinecart(3.5, 64.0625, 4.5)
	boatID := srv.SpawnBoat(3.5, 64, 2.5, "minecraft:oak_boat")
	if first == 0 || second == 0 || boatID == 0 {
		t.Fatal("a cart or the boat did not stand")
	}
	c.MoveTo(3.5, 64, 2.0, 0, 60)
	c.Settle()

	c.UseOn(first)
	if riders := servertest.AwaitPassengersFor(c, first); len(riders) != 1 {
		t.Fatalf("the first cart holds %v, want the rider", riders)
	}
	c.UseOn(second)
	if riders := servertest.AwaitPassengersFor(c, second); len(riders) != 1 || riders[0] != c.PlayerEntityID {
		t.Fatalf("the second cart holds %v, want the clicker's own %d", riders, c.PlayerEntityID)
	}
	if riders := srv.MinecartPassengerIDs(first); len(riders) != 0 {
		t.Errorf("the first cart holds %v after the switch, want none", riders)
	}

	// A held cart refuses the boat rider, who stays in the boat.
	c.UseOn(boatID)
	if riders := servertest.AwaitPassengersFor(c, boatID); len(riders) != 1 {
		t.Fatalf("the boat holds %v, want the rider", riders)
	}
	d := connect().Join("Holder")
	d.MoveTo(3.5, 64, 5.5, 0, 60)
	d.Settle()
	d.UseOn(second)
	if riders := servertest.AwaitPassengersFor(d, second); len(riders) != 1 {
		t.Fatalf("the second cart holds %v, want the holder", riders)
	}
	c.UseOn(second)
	c.Settle()
	if riders := srv.BoatPassengerIDs(boatID); len(riders) != 1 || riders[0] != c.PlayerEntityID {
		t.Errorf("the boat holds %v after a click on a held cart, want the rider kept", riders)
	}
	if riders := srv.MinecartPassengerIDs(second); len(riders) != 1 || riders[0] == c.PlayerEntityID {
		t.Errorf("the second cart holds %v, want the holder alone", riders)
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

// vanilla: AbstractMinecart.canCollideWith, Entity.collide
func TestARollingCartStopsAtAPlayerOnTheTrack(t *testing.T) {
	t.Parallel()
	srv, c, _ := cartPlayer(t, "creative", "Stopper", 0)
	servertest.HoldTicks(t, srv)
	const playerZ = 10.5
	stone := servertest.MustState(t, "minecraft:stone", nil)
	power := servertest.MustState(t, "minecraft:redstone_block", nil)
	rail := servertest.MustState(t, "minecraft:powered_rail", map[string]string{"shape": "north_south", "powered": "true"})
	for z := int32(3); z <= 15; z++ {
		if !srv.SetBlock(2, 63, z, power) || !srv.SetBlock(2, 64, z, rail) {
			t.Fatal("the rail line is not loaded")
		}
	}
	srv.SetBlock(2, 64, 2, stone)
	c.MoveToGround(2.5, 64, playerZ, true)
	c.Settle()
	cart := srv.SpawnMinecart(2.5, 64.1, 3.5)
	lastZ := 3.5
	ticks := 0
	servertest.UntilTicks(t, srv, 60, "the cart settling just short of the player", func() bool {
		_, _, z, _, ok := srv.MinecartState(cart)
		if !ok || z >= playerZ {
			t.Fatalf("cart z=%v (present %v), want it never passing the player at %v", z, ok, playerZ)
		}
		settled := z > playerZ-0.81 && z < playerZ-0.79 && math.Abs(z-lastZ) < 1e-7
		lastZ = z
		ticks++
		return settled
	})
	for ; ticks <= 60; ticks++ {
		servertest.RunTicks(t, srv, 1)
		_, _, z, _, ok := srv.MinecartState(cart)
		if !ok || z >= playerZ-0.79 || math.Abs(z-lastZ) > 1e-7 {
			t.Fatalf("cart moved from z=%v to %v (present %v), want it held short of the player", lastZ, z, ok)
		}
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

func TestACartPassesThroughASpectator(t *testing.T) {
	t.Parallel()
	srv, c, _ := cartPlayer(t, "spectator", "Spectator", 0)
	servertest.HoldTicks(t, srv)
	c.MoveToGround(4.2, 64, 5.5, true)
	c.Settle()
	cart := srv.SpawnMinecart(3, 64.0625, 5.5)
	srv.SetMinecartMotion(cart, 0.4, 0, 0)
	servertest.RunTicks(t, srv, 1)
	if x, _, _, _, ok := srv.MinecartState(cart); !ok || math.Abs(x-3.4) > 1e-7 {
		t.Fatalf("cart x=%v (present %v), want an unobstructed move through the spectator", x, ok)
	}
}

// vanilla: Shapes.collide does not stop movement out of an existing overlap.
func TestACartCanMoveOutOfAnOverlappingBoat(t *testing.T) {
	t.Parallel()
	srv, _, _ := cartPlayer(t, "creative", "Watcher", 0)
	servertest.HoldTicks(t, srv)
	cart := srv.SpawnMinecart(3, 64.0625, 5.5)
	srv.SpawnBoat(3.1, 64, 5.5, "minecraft:oak_boat")
	srv.SetMinecartMotion(cart, 0.4, 0, 0)
	servertest.RunTicks(t, srv, 1)
	if x, _, _, _, ok := srv.MinecartState(cart); !ok || math.Abs(x-3.4) > 1e-7 {
		t.Fatalf("cart x=%v (present %v), want it to move out of the overlapping hull", x, ok)
	}
}
