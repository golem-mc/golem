package fluids

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The fluid tests: what the world's water and lava do to the things dropped
// into them. An item the vanilla way floats up out of still water instead of
// lying on the bottom of it, burns up in a pool of lava unless the stack it
// carries is one of the unburnable ones, and an orb whose eyes are under
// floats for the same reason. Each drives a real connection — the drop key
// for the items, an operator's summon for the orb — and reads what the
// server tells the client about where the body went and when it went away.

// fluidPool is the one-deep pool the tests float and burn things in: the
// fluid set into the flat world's surface block at the position, held there
// by the level ground about it.
func fluidPool(t *testing.T, srv *server.Server, x, z int32, block string) {
	t.Helper()
	state, ok := registry.FindState(block, nil)
	if !ok {
		t.Fatalf("the block registry has no %s", block)
	}
	if !srv.SetBlock(x, 63, z, state) {
		t.Fatal("the pool's chunk is not loaded")
	}
}

// fluidItem is one dropped item as the tests watch it travel: where it was
// spawned, where it lies now, whether it has touched the bottom of the pool
// it fell into and risen again since, and whether the server has taken it
// away or played the fire's sounds about it.
type fluidItem struct {
	entityID     int32
	spawnY       float64
	y            float64
	lowestY      float64
	landed       bool
	roseAgain    bool
	removed      bool
	burned       bool
	extinguished bool
}

// fluidWatcher reads a connection for one item's life: the movement packets
// that carry it upward or down, the removal that ends it, the sounds the
// fire plays. Heights accumulate from the spawn's own, the relative moves
// measured in the 1/4096 of a block they travel in and the absolute ones
// taken as they come.
type fluidWatcher struct {
	t    *testing.T
	c    *servertest.Client
	id   int32
	item fluidItem
}

// newFluidWatcher begins watching for the next item spawned, or for the
// entity ID given — zero meaning whichever item turns up first.
func newFluidWatcher(t *testing.T, c *servertest.Client, id int32) *fluidWatcher {
	return &fluidWatcher{t: t, c: c, id: id}
}

// watch reads the connection through the loop's next n ticks, filing
// everything the watched item did, and answers the keep-alives on the way.
func (w *fluidWatcher) watch(srv *server.Server, ticks uint64) {
	w.t.Helper()
	w.c.PumpTicks(srv, ticks, func(gotID int32, data []byte) bool {
		w.read(gotID, data)
		return false
	})
}

// sawHeight files where the item was last seen: low enough to have reached
// the pool's floor, and then any height above it is the float coming back.
func (w *fluidWatcher) sawHeight() {
	if w.item.y < w.item.lowestY {
		w.item.lowestY = w.item.y
	}
	if w.item.y < 63.3 {
		w.item.landed = true
	}
	if w.item.landed && w.item.y > 63.5 {
		w.item.roseAgain = true
	}
}

// read files one packet against the watched item.
func (w *fluidWatcher) read(gotID int32, data []byte) {
	w.t.Helper()
	switch gotID {
	case protocol.IDPlaySpawnEntity:
		r := protocol.NewReader(data)
		id := r.ReadVarInt()
		r.ReadUUID()
		kind := r.ReadVarInt()
		_, y, _ := r.ReadF64(), r.ReadF64(), r.ReadF64()
		if kind != protocol.EntityTypeItem || (w.id != 0 && id != w.id) {
			return
		}
		w.id = id
		w.item.entityID = id
		w.item.spawnY = y
		w.item.y = y
		w.item.lowestY = y
	case protocol.IDPlayEntityPosition:
		r := protocol.NewReader(data)
		if id := r.ReadVarInt(); id != w.id {
			return
		}
		r.ReadVarInt() // properties
		r.ReadI16()
		w.item.y += float64(r.ReadI16()) / 4096
		r.ReadI16()
		w.sawHeight()
	case protocol.IDPlayTeleportEntity:
		r := protocol.NewReader(data)
		if id := r.ReadVarInt(); id != w.id {
			return
		}
		r.ReadVarInt() // position path
		r.ReadF64()
		w.item.y = r.ReadF64()
		w.sawHeight()
	case protocol.IDPlayRemoveEntities:
		r := protocol.NewReader(data)
		for range r.ReadVarInt() {
			if r.ReadVarInt() == w.id {
				w.item.removed = true
			}
		}
	case protocol.IDPlaySoundEffect:
		r := protocol.NewReader(data)
		sound := r.ReadVarInt()
		var name string
		if sound == 0 {
			name = r.ReadString(int32(64)) // an inline definition
		} else {
			name, _ = servertest.SoundEventName(sound - 1)
		}
		if name == "minecraft:entity.generic.burn" {
			w.item.burned = true
		}
		if name == "minecraft:entity.generic.extinguish_fire" {
			w.item.extinguished = true
		}
	}
}

// dropIntoThePool hands the player a stack, stands them over the pool
// looking straight down, and presses the drop key, which is how a stack
// comes to be falling into the fluid below them. The thrower then steps two
// blocks aside: a creative player picks drops up now, and standing over the
// pool would take the item back once its pickup delay runs out.
func dropIntoThePool(t *testing.T, c *servertest.Client, item string) {
	t.Helper()
	id, ok := registry.ItemID(item)
	if !ok {
		t.Fatalf("the item registry has no %s", item)
	}
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: id, Count: 1})
	c.MoveTo(0.5, 64, 0.5, 0, 90)
	c.Settle()
	c.DropKey(false)
	c.MoveTo(2.5, 64, 0.5, 0, 0)
}

// TestAnItemDroppedIntoWaterFloatsUpToTheSurface drops a stone into a pool
// of still water and watches it: the vanilla item does not lie on the
// bottom of the pool it fell into, it floats — the drift carrying it back
// up until it bobs about the surface, which is what its highest seen
// height says.
func TestAnItemDroppedIntoWaterFloatsUpToTheSurface(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Dropper")

	fluidPool(t, srv, 0, 0, "minecraft:water")

	w := newFluidWatcher(t, c, 0)
	dropIntoThePool(t, c, "minecraft:stone")
	w.watch(srv, 160)
	if w.item.entityID == 0 {
		t.Fatal("the drop never arrived")
	}
	if !w.item.landed {
		t.Errorf("the item was never seen down at the pool's floor (lowest %v), want the drop to have reached the water", w.item.lowestY)
	}
	if !w.item.roseAgain {
		t.Errorf("the item touched the pool's floor and never rose again (lowest %v), want the water's drift to float it up", w.item.lowestY)
	}
	if w.item.removed {
		t.Error("the item floating in water was taken away, want it swimming still")
	}
}

// TestAnItemDroppedIntoLavaBurnsUp drops a stone into a pool of lava and
// watches it burn: the lava takes an item's five points of hurt two ticks
// at a bite of four, and the body goes — the removal the client is told of,
// the burn sound the hurt played before it.
func TestAnItemDroppedIntoLavaBurnsUp(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Dropper")

	fluidPool(t, srv, 0, 0, "minecraft:lava")

	w := newFluidWatcher(t, c, 0)
	dropIntoThePool(t, c, "minecraft:stone")
	w.watch(srv, 160)
	if w.item.entityID == 0 {
		t.Fatal("the drop never arrived")
	}
	if !w.item.removed {
		t.Error("the item in lava was never taken away, want it burned up")
	}
	if !w.item.burned {
		t.Error("the item burning in lava played no burn, want the fizz vanilla plays for it")
	}
}

// TestANetheriteIngotDroppedIntoLavaSurvivesAndFloats drops the one thing
// vanilla makes unburnable into the same pool: the ingot keeps every point
// of its health, is never taken away, and floats up through the lava the
// way the drift lifts anything.
func TestANetheriteIngotDroppedIntoLavaSurvivesAndFloats(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Dropper")

	fluidPool(t, srv, 0, 0, "minecraft:lava")

	w := newFluidWatcher(t, c, 0)
	dropIntoThePool(t, c, "minecraft:netherite_ingot")
	w.watch(srv, 160)
	if w.item.entityID == 0 {
		t.Fatal("the drop never arrived")
	}
	if w.item.removed {
		t.Error("the netherite ingot in lava was taken away, want it immune to the burn")
	}
	if w.item.burned {
		t.Error("the netherite ingot in lava played a burn, want it never on fire to fizz")
	}
	if !w.item.roseAgain {
		t.Errorf("the ingot touched the lava's floor and never rose again (lowest %v), want the lava's drift to float it up", w.item.lowestY)
	}
}

// TestSummoningAnItemIntoStillWaterFloatsItUp summons an item entity the
// way an operator does, into the bottom of a pool of still water, and
// watches it: the vanilla item does not lie where it was summoned, it
// floats — the drift carrying it up until it bobs about the surface, which
// is what its rising again after the floor says.
func TestSummoningAnItemIntoStillWaterFloatsItUp(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BlockTestConfig(), "Caller", servertest.OpsListing(t, "Caller"))

	fluidPool(t, srv, 0, 0, "minecraft:water")

	w := newFluidWatcher(t, c, 0)
	c.ChatCommand(`summon minecraft:item 0.5 63.0 0.5 {Item:{id:"minecraft:stone",count:1},PickupDelay:32767s}`)
	w.watch(srv, 160)
	if w.item.entityID == 0 {
		t.Fatal("the summon left no item behind")
	}
	if !w.item.roseAgain {
		t.Errorf("the summoned item never rose above the pool's floor (lowest %v), want the water's drift to float it up", w.item.lowestY)
	}
}

// orbTravel is one summoned orb as the tests watch it travel: where it was
// spawned and the lowest and highest it has been seen since.
type orbTravel struct {
	id      int32
	spawnY  float64
	y       float64
	lowest  float64
	highest float64
	seen    bool
}

// TestAnOrbSummonedIntoShallowWaterSinksBeforeItFloats summons an orb into
// the last sliver of a one-deep pool, where its feet are in the water and
// its eyes are above it: the vanilla drift is the eye test's to give, so
// the orb falls to the pool's floor first — sinking below where it was
// summoned — and only then, its eyes under, floats back up.
func TestAnOrbSummonedIntoShallowWaterSinksBeforeItFloats(t *testing.T) {
	t.Parallel()
	cfg, setup := servertest.ExperienceSetup(t, "Caller")
	srv, c, _ := servertest.JoinAs(t, cfg, "Caller", setup)

	fluidPool(t, srv, 0, 0, "minecraft:water")

	// The summoner stands clear of the pool, out of the eight blocks the
	// orb's follow reaches, so the water is the only thing moving it.
	c.MoveTo(10.5, 64, 0.5, 0, 0)
	c.Settle()

	c.ChatCommand("summon minecraft:experience_orb 0.5 63.7 0.5")

	// The orb's travels: the fall into the pool it was summoned over the
	// top of, and the float back up its underwater eyes give it once it
	// has landed.
	var orb orbTravel
	servertest.HoldTicks(t, srv)
	c.ReadTicks(srv, 300, func(gotID int32, data []byte) bool {
		switch gotID {
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			id := r.ReadVarInt()
			r.ReadUUID()
			if kind := r.ReadVarInt(); kind != protocol.EntityTypeExperienceOrb {
				return false
			}
			r.ReadF64()
			y := r.ReadF64()
			orb = orbTravel{id: id, spawnY: y, y: y, lowest: y, highest: y, seen: true}
		case protocol.IDPlayEntityPosition:
			r := protocol.NewReader(data)
			if id := r.ReadVarInt(); id != orb.id {
				return false
			}
			r.ReadVarInt() // properties
			r.ReadI16()
			orb.y += float64(r.ReadI16()) / 4096
			r.ReadI16()
			if orb.y < orb.lowest {
				orb.lowest = orb.y
			}
			if orb.y > orb.highest {
				orb.highest = orb.y
			}
		case protocol.IDPlayTeleportEntity:
			r := protocol.NewReader(data)
			if id := r.ReadVarInt(); id != orb.id {
				return false
			}
			r.ReadVarInt() // position path
			r.ReadF64()
			y := r.ReadF64()
			orb.y = y
			if y < orb.lowest {
				orb.lowest = y
			}
			if y > orb.highest {
				orb.highest = y
			}
		}
		return false
	})
	if !orb.seen {
		t.Fatal("the summon never stood the orb up")
	}
	if orb.lowest >= 63.6 {
		t.Errorf("the orb's lowest seen height = %v (summoned at %v), want the dip below where it was summoned, the fall its dry eyes left it", orb.lowest, orb.spawnY)
	}
	if orb.highest <= 63.65 {
		t.Errorf("the orb's highest seen height = %v, want the float its underwater eyes give it", orb.highest)
	}
}

// The deep-water tests run below y=0, where a vanilla superflat world's
// ground lies and every block top is negative (#82, #95). The main loop is
// stopped and the entities ticked by hand, so the water stays as built.

// stoneRoom builds a pocket in the flat world's stone over x and z 0..2 from
// x0, z0: stone under floorY, water from floorY up to the given depth, air
// above it to the stone roof at floorY+depth+3, walled in stone all round.
func stoneRoom(t *testing.T, srv *server.Server, x0, z0, floorY, depth int32) {
	t.Helper()
	stone := servertest.BlockState(t, "minecraft:stone")
	water := servertest.BlockState(t, "minecraft:water")
	roof := floorY + depth + 3
	for x := x0 - 1; x <= x0+3; x++ {
		for z := z0 - 1; z <= z0+3; z++ {
			for y := floorY - 1; y <= roof; y++ {
				state := uint16(0)
				switch {
				case x < x0 || x > x0+2 || z < z0 || z > z0+2 || y < floorY || y == roof:
					state = stone
				case y < floorY+depth:
					state = water
				}
				srv.SetBlock(x, y, z, state) // false where the block was already so
			}
		}
	}
	if srv.BlockAt(x0, roof, z0) != stone {
		t.Fatal("the room's chunk is not loaded")
	}
}

// itemFeet is the feet height of the item with the given entity ID.
func itemFeet(t *testing.T, srv *server.Server, id int32) (x, y, z float64) {
	t.Helper()
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.ID == id {
			return it.X, it.Y - entity.ItemHalfSize, it.Z
		}
	}
	t.Fatalf("item %d is gone", id)
	return 0, 0, 0
}

// The surface of the deep pool is y=-60; vanilla's float holds a body's
// feet between about 0.2 and 0.8 below it once the first rise has settled.
const (
	deepPoolSurface = -60.0
	bobLow          = deepPoolSurface - 0.9
	bobHigh         = deepPoolSurface - 0.1
)

// TestAnItemDroppedIntoADeepPoolFloatsAtTheSurface drops an item into a
// three-deep pool: it sinks to the floor, floats back up and bobs at the
// surface rather than resting on the bottom.
func TestAnItemDroppedIntoADeepPoolFloatsAtTheSurface(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Dropper")
	srv.StopTick()
	stoneRoom(t, srv, 0, 0, -63, 3)

	id := srv.SpawnItemEntity(1.5, -58.5+entity.ItemHalfSize, 1.5, "minecraft:stone", 1)
	lowest := 0.0
	for range 100 {
		srv.TickEntities(1)
		_, y, _ := itemFeet(t, srv, id)
		lowest = min(lowest, y)
	}
	if lowest > -62.9 {
		t.Errorf("the item's lowest feet = %v, want it to have sunk to the floor at -63 first", lowest)
	}
	srv.TickEntities(300)
	for range 200 {
		srv.TickEntities(1)
		if _, y, _ := itemFeet(t, srv, id); y < bobLow || y > bobHigh {
			t.Fatalf("the item's feet at %v, want them bobbing within [%v, %v] under the surface", y, bobLow, bobHigh)
		}
	}
}

// TestAnOrbDroppedIntoADeepPoolFloatsAtTheSurface is the same drop for an
// experience orb, whose eyes under water float it the same way.
func TestAnOrbDroppedIntoADeepPoolFloatsAtTheSurface(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Dropper")
	srv.StopTick()
	stoneRoom(t, srv, 0, 0, -63, 3)

	id := srv.SpawnOrb(1.5, -58.5, 1.5, 1)
	srv.TickEntities(400)
	for range 200 {
		srv.TickEntities(1)
		_, y, _, ok := srv.OrbPos(id)
		if !ok {
			t.Fatal("the orb is gone")
		}
		if y < bobLow || y > bobHigh {
			t.Fatalf("the orb's feet at %v, want them bobbing within [%v, %v] under the surface", y, bobLow, bobHigh)
		}
	}
}

// TestAnItemInFlowingWaterDriftsWithTheCurrent lays an item in a channel
// whose water runs from a source at x=0 down to level 7 at x=7: the current
// carries it along +x.
func TestAnItemInFlowingWaterDriftsWithTheCurrent(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Dropper")
	srv.StopTick()

	stone := servertest.BlockState(t, "minecraft:stone")
	for x := int32(-1); x <= 8; x++ {
		for z := int32(4); z <= 6; z++ {
			for y := int32(-62); y <= -58; y++ {
				state := uint16(0)
				switch {
				case x < 0 || x > 7 || z != 5 || y == -62 || y == -58:
					state = stone
				case y == -61:
					state = servertest.BlockStateWith(t, "minecraft:water", "level="+strconv.Itoa(int(x)))
				}
				srv.SetBlock(x, y, z, state)
			}
		}
	}
	if srv.BlockAt(0, -61, 5) != servertest.BlockState(t, "minecraft:water") {
		t.Fatal("the channel's chunk is not loaded")
	}

	id := srv.SpawnItemEntity(1.5, -61+entity.ItemHalfSize, 5.5, "minecraft:stone", 1)
	srv.TickEntities(40)
	x, _, z := itemFeet(t, srv, id)
	if x < 3.5 {
		t.Errorf("the item drifted to x=%v in 40 ticks, want the current to carry it at least two blocks from 1.5", x)
	}
	if z != 5.5 {
		t.Errorf("the item drifted to z=%v, want it kept on the channel's line at 5.5", z)
	}
}

// TestACowLandsOnGroundBelowYZero drops a cow onto a stone floor at y=-61,
// with the main loop running: it lands on the floor's top at -60 rather than
// being lifted toward y=0.
func TestACowLandsOnGroundBelowYZero(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Farmer")
	stoneRoom(t, srv, 10, 10, -60, 0)

	id := srv.SpawnMob("minecraft:cow", 11.5, -58.5, 11.5)
	servertest.AwaitTicks(t, srv, 40)
	if _, y, _, ok := srv.MobPos(id); !ok || y != -60 {
		t.Errorf("the cow stands at y=%v (found %v), want it on the floor at -60", y, ok)
	}
}

// lavaFireSeed is the seed the lava fire test pins the dice with.
const lavaFireSeed = int64(20261016)

// lavaFireTickSpeed is the random tick rule the test runs at: fifty dice a
// section a pass, against the default three, so the pass count below is
// tens of hits on the one lava cell the test watches rather than single
// ones.
const lavaFireTickSpeed = 50

// TestLavaLightsAFireBesideFlammableGround pins the dice on a one-deep lava
// pool with a plank in the ground beside it and drives the random ticks: a
// die that lands on the lava rolls vanilla's fire-lighting dice, and in the
// end a fire stands over the planks.
func TestLavaLightsAFireBesideFlammableGround(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Kindler")

	srv.StopTick()
	servertest.SeedPhysics(t, srv, lavaFireSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), lavaFireTickSpeed)

	fluidPool(t, srv, 0, 0, "minecraft:lava")
	planks, ok := registry.FindState("minecraft:oak_planks", nil)
	if !ok {
		t.Fatal("the block registry has no oak planks")
	}
	if !srv.SetBlock(1, 63, 0, planks) {
		t.Fatal("the planks were not placed")
	}

	srv.TickRandomTicks(20000)

	got := srv.BlockAt(1, 64, 0)
	if name, _, ok := registry.BlockStateName(got); !ok || name != "minecraft:fire" {
		t.Errorf("the cell over the planks = %d (%s), want a fire the lava lit", got, name)
	}
}
