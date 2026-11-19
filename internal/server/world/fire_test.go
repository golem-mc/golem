package world

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// The fire tests. Every one seeds the engine's dice first — the fire's
// spread, burn-out and dousing are all dice, and a seeded run of the same
// world plays out the same however often it runs — and drives the engine
// tick by tick, the way the main loop does. The fire visits the engine every
// thirty to thirty-nine ticks, so the windows the tests drive are counted in
// visits, not in single ticks.

// newFireWorld is a physics world with the dice pinned to the seed given,
// ready for a fire to be lit in.
func newFireWorld(t *testing.T, seed int64) (*World, *Physics) {
	t.Helper()
	w, p := newPhysicsWorld(t)
	p.rng = rand.New(rand.NewSource(seed))
	p.w.SetDifficulty(DifficultyHard)
	return w, p
}

// setRain drives the rain level past the 0.2 isRaining tests, the fade
// vanilla runs over twenty-one ticks; the physics tests tick no weather
// themselves, so the level stands where it is left.
func setRain(w *World) {
	w.SetWeather(true, false, 0, 0, 0)
	for range 21 {
		w.TickWeather()
	}
}

// fireState resolves a state of the plain fire, failing the test when the
// registry does not have it.
func fireState(t *testing.T, overrides map[string]string) uint16 {
	t.Helper()
	state, ok := registry.FindState("minecraft:fire", overrides)
	if !ok {
		t.Fatalf("the registry has no fire with %v", overrides)
	}
	return state
}

// blockLabel names a block state for a failure message.
func blockLabel(state uint16) string {
	name, props, ok := registry.BlockStateName(state)
	if !ok {
		return "unknown"
	}
	if props == "" {
		return name
	}
	return name + " " + props
}

// firesIn counts the fire states in the box of the given reach around a
// position, the sweep a spread is seen in.
func firesIn(w *World, x, y, z, reach int32) int {
	n := 0
	for dx := -reach; dx <= reach; dx++ {
		for dy := -reach; dy <= reach; dy++ {
			for dz := -reach; dz <= reach; dz++ {
				name, _, ok := registry.BlockStateName(w.BlockAt(x+dx, y+dy, z+dz))
				if ok && name == "minecraft:fire" {
					n++
				}
			}
		}
	}
	return n
}

// TestFireBurnsPlanksAwayAndSpreads checks the heart of the fire: lit among
// oak planks, it consumes them — the block going to air or to the flame that
// eats it — and, before its own age runs out, sets fire down in an empty
// cell that had none before.
func TestFireBurnsPlanksAwayAndSpreads(t *testing.T) {
	w, p := newFireWorld(t, 42)
	planks := registry.BlockStates["minecraft:oak_planks"]

	// Planks on three sides of where the fire will stand, and one more on
	// top of the first: more than one mouth for the fire to eat with, so
	// the burn-out does not hinge on a single die.
	for _, c := range [][3]int32{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}} {
		setBlock(t, w, p, c[0], GroundLevel, c[2], planks)
	}
	setBlock(t, w, p, 1, GroundLevel+1, 0, planks)
	setBlock(t, w, p, 0, GroundLevel, 0, blocklogic.Fire)

	spread := false
	var clock int64
	for clock = 1; clock <= 1600; clock++ {
		p.Tick(clock)
		if !spread && firesIn(w, 0, GroundLevel, 0, 2) > 1 {
			spread = true
		}
		if w.BlockAt(1, GroundLevel, 0) != planks {
			break // the planks are gone, eaten or burnt through
		}
	}
	if w.BlockAt(1, GroundLevel, 0) == planks {
		t.Fatalf("the planks stood %d ticks of fire and were not burnt away", clock)
	}
	if !spread {
		t.Error("the fire never spread past its first block")
	}
}

// TestFireOnNetherrackNeverBurnsOut checks the infiniburn rule: fire on
// netherrack ages to the top of its scale and stays there, through rain and
// past every visit that would end any other fire.
func TestFireOnNetherrackNeverBurnsOut(t *testing.T) {
	w, p := newFireWorld(t, 7)
	netherrack := registry.BlockStates["minecraft:netherrack"]

	setBlock(t, w, p, 0, GroundLevel-1, 0, netherrack)
	setBlock(t, w, p, 0, GroundLevel, 0, blocklogic.Fire)

	// Rain and all: the netherrack's fire is not rained out. Forty-odd
	// visits is what the age needs to climb to the top.
	setRain(w)
	var clock int64
	for clock = 1; clock <= 2400; clock++ {
		p.Tick(clock)
	}
	state := w.BlockAt(0, GroundLevel, 0)
	if name, _, _ := registry.BlockStateName(state); name != "minecraft:fire" {
		t.Fatalf("fire on netherrack after %d ticks = %s, want fire", clock, blockLabel(state))
	}
	if age := fireAgeOf(state); age != fireMaxAge {
		t.Errorf("fire on netherrack aged to %d, want the top of %d", age, fireMaxAge)
	}
}

// TestFireOnStoneWithNothingToEatBurnsOut checks the other end: a fire on
// plain stone with nothing flammable in reach lingers only while it is young
// and is gone soon after.
func TestFireOnStoneWithNothingToEatBurnsOut(t *testing.T) {
	w, p := newFireWorld(t, 11)

	setBlock(t, w, p, 0, GroundLevel, 0, blocklogic.Fire)

	// The young fire lingers: its first visit leaves it standing.
	p.Tick(1)
	if got := w.BlockAt(0, GroundLevel, 0); got == airState {
		t.Error("a young fire on stone went out on its first visit")
	}
	// And past its youth it goes, within the dozen visits that follow.
	var clock int64
	for clock = 2; clock <= 400; clock++ {
		p.Tick(clock)
		if w.BlockAt(0, GroundLevel, 0) == airState {
			break
		}
	}
	if got := w.BlockAt(0, GroundLevel, 0); got != airState {
		t.Errorf("fire with nothing to eat after %d ticks = %s, want air", clock, blockLabel(got))
	}
}

// TestRainPutsOutFireInTheOpen checks the douse: a fire the sky reaches goes
// out within a few visits of the rain, while one under a roof the rain does
// not reach stands on.
func TestRainPutsOutFireInTheOpen(t *testing.T) {
	w, p := newFireWorld(t, 5)
	setRain(w)

	setBlock(t, w, p, 0, GroundLevel, 0, blocklogic.Fire)

	var clock int64
	for clock = 1; clock <= 1000; clock++ {
		p.Tick(clock)
		if w.BlockAt(0, GroundLevel, 0) == airState {
			break
		}
	}
	if got := w.BlockAt(0, GroundLevel, 0); got != airState {
		t.Errorf("fire in the open rain after %d ticks = %s, want air", clock, blockLabel(got))
	}
}

// TestRainSparesFireUnderARoof checks the canSeeSky half of the douse: the
// same rain, with a stone roof over the fire, leaves it burning — and the
// fire past its youth under a dry roof still goes the way of any other.
func TestRainSparesFireUnderARoof(t *testing.T) {
	w, p := newFireWorld(t, 5)
	setRain(w)

	// A roof of stone two above, which dims the sky light under it to
	// nothing; the fire sits on the floor beneath it.
	setBlock(t, w, p, 0, GroundLevel+2, 0, stoneState)
	setBlock(t, w, p, 0, GroundLevel, 0, blocklogic.Fire)

	var clock int64
	for clock = 1; clock <= 6; clock++ {
		p.Tick(clock)
	}
	if name, _, _ := registry.BlockStateName(w.BlockAt(0, GroundLevel, 0)); name != "minecraft:fire" {
		t.Errorf("fire under a roof in the rain = %s, want fire standing",
			blockLabel(w.BlockAt(0, GroundLevel, 0)))
	}
}

// TestFirePrimesATNTBlock checks the one block fire does not burn: the
// burn-out takes a TNT block and the engine hands the server a prime, the
// block gone from its cell.
func TestFirePrimesATNTBlock(t *testing.T) {
	w, p := newFireWorld(t, 3)
	tnt := registry.BlockStates["minecraft:tnt"]

	setBlock(t, w, p, 1, GroundLevel, 0, tnt)
	setBlock(t, w, p, 0, GroundLevel, 0, blocklogic.Fire)

	var clock int64
	primed := false
	for clock = 1; clock <= 400; clock++ {
		_, _, primes, _ := p.Tick(clock)
		if len(primes) == 1 {
			if primes[0].X != 1 || primes[0].Y != GroundLevel || primes[0].Z != 0 {
				t.Fatalf("prime handed over at (%d, %d, %d), want (1, %d, 0)",
					primes[0].X, primes[0].Y, primes[0].Z, GroundLevel)
			}
			primed = true
			break
		}
		if len(primes) > 1 {
			t.Fatalf("the tick handed over %d primes, want at most one", len(primes))
		}
	}
	if !primed {
		t.Fatal("400 ticks of fire beside a TNT block primed nothing")
	}
	if got := w.BlockAt(1, GroundLevel, 0); got == tnt {
		t.Error("the primed TNT block is still standing in its cell")
	}
}

// TestFireAboveSoulSandBurnsAsSoulFire checks the soul fire: the flame a
// burning block is consumed into stands as the soul fire where a soul-fire
// base sits below, and the soul fire is a block the generated tables can
// name. The soul fire never ages, so once it stands it stands.
func TestFireAboveSoulSandBurnsAsSoulFire(t *testing.T) {
	w, p := newFireWorld(t, 9)
	soulSand := registry.BlockStates["minecraft:soul_sand"]
	planks := registry.BlockStates["minecraft:oak_planks"]

	// Planks standing on soul sand on three sides of where the fire will
	// stand: each one the burn-out consumes is a coin toss for the flame
	// that takes its place, and the fire has three coins to win.
	for _, c := range [][2]int32{{1, 0}, {-1, 0}, {0, 1}} {
		setBlock(t, w, p, c[0], GroundLevel-1, c[1], soulSand)
		setBlock(t, w, p, c[0], GroundLevel, c[1], planks)
	}
	setBlock(t, w, p, 0, GroundLevel, 0, blocklogic.Fire)

	found := false
	var clock int64
	for clock = 1; clock <= 1600; clock++ {
		p.Tick(clock)
		for _, c := range [][2]int32{{1, 0}, {-1, 0}, {0, 1}} {
			if w.BlockAt(c[0], GroundLevel, c[1]) == blocklogic.SoulFire {
				found = true
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Errorf("no soul fire appeared above the soul sand in %d ticks", clock)
		return
	}
	// The soul fire stands: it has no age to run out, and the visits the
	// rest of the run drives take nothing from it.
	for range int64(400) {
		p.Tick(clock)
		clock++
	}
	for _, c := range [][2]int32{{1, 0}, {-1, 0}, {0, 1}} {
		if w.BlockAt(c[0], GroundLevel, c[1]) == blocklogic.SoulFire {
			return
		}
	}
	t.Error("the soul fire did not stand")
}

// vanilla: SoulFireBlock.updateShape, Level.destroyBlock
func TestSoulFireLosingItsBasePlaysNoBreakEvent(t *testing.T) {
	w, p := newPhysicsWorld(t)
	const x, y, z = 4, GroundLevel + 4, 4
	for _, base := range []registry.Block{registry.BlockSoulSand, registry.BlockSoulSoil} {
		for _, replacement := range []registry.Block{registry.BlockStone, registry.BlockAir} {
			w.SetBlock(x, y-1, z, base.DefaultState())
			w.SetBlock(x, y, z, registry.BlockSoulFire.DefaultState())
			w.SetBlock(x, y-1, z, replacement.DefaultState())
			_, effects, _ := p.NotifyChanged(x, y-1, z, base.DefaultState())
			if got := w.BlockAt(x, y, z); got != airState {
				t.Errorf("%s replaced by %s: soul fire remains as %s", base.Name(), replacement.Name(), blockLabel(got))
			}
			for _, e := range effects {
				if e.LevelEvent == 2001 {
					t.Errorf("%s replaced by %s: soul fire sent a break event: %+v", base.Name(), replacement.Name(), e)
				}
			}
		}
	}
}

// TestAFirePlacedOnNothingGoesOutAtOnce checks the placement half: fire set
// in mid-air, with nothing sturdy below and no fuel beside, is taken back
// out in the neighbour update that follows the change, before any tick.
func TestAFirePlacedOnNothingGoesOutAtOnce(t *testing.T) {
	w, p := newFireWorld(t, 1)

	changes := setBlock(t, w, p, 5, GroundLevel+3, 5, blocklogic.Fire)
	if got := w.BlockAt(5, GroundLevel+3, 5); got != airState {
		t.Fatalf("fire set in mid-air = %s, want air at once", blockLabel(got))
	}
	if len(changes) == 0 || changes[0].State != airState {
		t.Errorf("the placement's neighbour update reported %v, want the fire taken out", changes)
	}
}

// TestAFireWhoseFloorBecameABottomSlabGoesOut is the survival twin's floor
// check: the flat ground's full top face holds a fire up, but a bottom slab's
// half-height top — not fuel either — is no floor, and the neighbour update
// takes the flame out at once.
func TestAFireWhoseFloorBecameABottomSlabGoesOut(t *testing.T) {
	w, p := newFireWorld(t, 1)
	slab := registry.BlockStates["minecraft:stone_slab"]

	setBlock(t, w, p, 5, GroundLevel, 5, blocklogic.Fire)
	if got := w.BlockAt(5, GroundLevel, 5); got != blocklogic.Fire {
		t.Fatalf("fire on the stone ground = %s, want it standing", blockLabel(got))
	}

	changes := setBlock(t, w, p, 5, GroundLevel-1, 5, slab)
	if got := w.BlockAt(5, GroundLevel, 5); got != airState {
		t.Fatalf("fire whose floor became a bottom slab = %s, want air at once", blockLabel(got))
	}
	if findChange(t, changes, 5, GroundLevel, 5) != airState {
		t.Errorf("the removal reported %v, want the fire taken out", changes)
	}
}

// TestAFireBesidePlanksStandsWithoutAFloor checks the other half of survival:
// fire leaning on a planks wall, with nothing under it, stands — placed with
// the lean the vanilla placement gives it when nothing below is sturdy — and
// goes when the wall it leans on goes, there being nothing else to hold it.
func TestAFireBesidePlanksStandsWithoutAFloor(t *testing.T) {
	w, p := newFireWorld(t, 1)
	planks := registry.BlockStates["minecraft:oak_planks"]

	leaning := fireState(t, map[string]string{"age": "0", "west": "true"})
	setBlock(t, w, p, -1, GroundLevel+2, 0, planks)
	changes := setBlock(t, w, p, 0, GroundLevel+2, 0, leaning)
	if got := w.BlockAt(0, GroundLevel+2, 0); got != leaning {
		t.Fatalf("fire beside planks = %s, want it standing as it was set",
			blockLabel(got))
	}
	if len(changes) != 0 {
		t.Errorf("a standing placement changed %v, want nothing", changes)
	}

	// And the wall goes: the fire's rebuild finds no sturdy face below and
	// no fuel beside, and the neighbour update takes it out at once.
	changes = setBlock(t, w, p, -1, GroundLevel+2, 0, airState)
	if got := w.BlockAt(0, GroundLevel+2, 0); got != airState {
		t.Fatalf("fire whose wall went = %s, want air at once", blockLabel(got))
	}
	if len(changes) == 0 || changes[0].State != airState {
		t.Errorf("the removal reported %v, want the fire taken out", changes)
	}
}

// TestAFireRebuildsItsLeanWhenItsFuelGoes checks the neighbour update's
// rebuild: a fire leaning on planks it stands beside on a stone floor keeps
// standing when the planks go, at the age it had, and the lean it showed
// goes with them.
func TestAFireRebuildsItsLeanWhenItsFuelGoes(t *testing.T) {
	w, p := newFireWorld(t, 1)
	planks := registry.BlockStates["minecraft:oak_planks"]

	leaning := fireState(t, map[string]string{"age": "4", "west": "true"})
	setBlock(t, w, p, -1, GroundLevel, 0, planks)
	setBlock(t, w, p, 0, GroundLevel, 0, leaning)

	changes := setBlock(t, w, p, -1, GroundLevel, 0, airState)
	if got := w.BlockAt(0, GroundLevel, 0); got != fireState(t, map[string]string{"age": "4"}) {
		t.Fatalf("fire whose fuel went = %s, want the plain fire at the age it had",
			blockLabel(got))
	}
	if len(changes) == 0 || changes[0].State != fireState(t, map[string]string{"age": "4"}) {
		t.Errorf("the rebuild reported %v, want the lean taken off", changes)
	}
}

// lavaGroundSeed pins lavaTick's dice so the ground branch's first try lands
// on (x+1, z): the branch die zero, then the two offsets. The re-seed right
// before the call keeps the setup's own dice from shifting it.
// vanilla: LavaFluid.randomTick
const lavaGroundSeed = int64(17)

// TestTheLavaGroundBranchBuildsTheStateForTheFlaggedCell checks the ground
// branch's state cell: a plank over soul sand beside lava builds the soul
// fire from the plank's own cell and writes it one above, where its own
// onPlace finds no soul base below and removes it — no fire stands, rather
// than the plain fire a state built for the fire's cell would leave.
// vanilla: LavaFluid.randomTick
func TestTheLavaGroundBranchBuildsTheStateForTheFlaggedCell(t *testing.T) {
	w, p := newFireWorld(t, lavaGroundSeed)
	soulSand := registry.BlockStates["minecraft:soul_sand"]
	planks := registry.BlockStates["minecraft:oak_planks"]

	setBlock(t, w, p, 1, GroundLevel-1, 0, soulSand)
	setBlock(t, w, p, 1, GroundLevel, 0, planks)

	p.rng = rand.New(rand.NewSource(lavaGroundSeed))
	p.lavaTick(0, GroundLevel, 0, &[]BlockChange{})

	if got := w.BlockAt(1, GroundLevel+1, 0); got != airState {
		t.Errorf("the cell over the flagged planks = %s, want air (the soul fire's onPlace removes it)",
			blockLabel(got))
	}
}

// TestTheLavaGroundBranchKeepsTheFlaggedCellsFaceFlags checks the same cell:
// a flagged plank floating over air, beside a hay block, makes the leaning
// state the vanilla getState carries — east set — and not the default fire a
// state built for the fire's own cell would write.
// vanilla: LavaFluid.randomTick
func TestTheLavaGroundBranchKeepsTheFlaggedCellsFaceFlags(t *testing.T) {
	w, p := newFireWorld(t, lavaGroundSeed)
	planks := registry.BlockStates["minecraft:oak_planks"]
	hay := registry.BlockStates["minecraft:hay_block"]

	// The plank floats: air below is neither sturdy nor fuel, so the state
	// leans where it is flammable — hay east, which does not itself catch
	// from lava.
	setBlock(t, w, p, 1, GroundLevel-1, 0, airState)
	setBlock(t, w, p, 1, GroundLevel, 0, planks)
	setBlock(t, w, p, 2, GroundLevel, 0, hay)

	p.rng = rand.New(rand.NewSource(lavaGroundSeed))
	p.lavaTick(0, GroundLevel, 0, &[]BlockChange{})

	want := fireState(t, map[string]string{"age": "0", "east": "true"})
	if got := w.BlockAt(1, GroundLevel+1, 0); got != want {
		t.Errorf("the cell over the flagged planks = %s, want the east-leaning fire %s",
			blockLabel(got), blockLabel(want))
	}
}

// TestFireSpreadsIntoCaveAir checks that a pocket of cave air takes a spread
// like plain air does.
func TestFireSpreadsIntoCaveAir(t *testing.T) {
	w, p := newFireWorld(t, 42)
	planks := registry.BlockStates["minecraft:oak_planks"]
	caveAir, ok := registry.FindState("minecraft:cave_air", nil)
	if !ok {
		t.Fatal("the registry has no cave air")
	}
	for x := int32(-3); x <= 3; x++ {
		for y := int32(GroundLevel); y <= GroundLevel+5; y++ {
			for z := int32(-3); z <= 3; z++ {
				setBlock(t, w, p, x, y, z, caveAir)
			}
		}
	}
	for _, c := range [][3]int32{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}} {
		setBlock(t, w, p, c[0], GroundLevel, c[2], planks)
	}
	setBlock(t, w, p, 1, GroundLevel+1, 0, planks)
	setBlock(t, w, p, 0, GroundLevel, 0, blocklogic.Fire)

	for clock := int64(1); clock <= 1600; clock++ {
		p.Tick(clock)
		if firesIn(w, 0, GroundLevel, 0, 2) > 1 {
			return
		}
	}
	t.Error("the fire never spread into the cave air")
}

// burnoutTrials lights a fire beside wool in a flat world of the biome,
// visits it once per trial, and reports how often the wool burned away and
// how many fires stood around it after the visits.
func burnoutTrials(t *testing.T, biome string, trials int) (burned, spread int) {
	t.Helper()
	w := newTestWorld(t, newFlatGenerator(biome, []flatLayer{{state: stoneState, height: GroundLevel - worldMinY}}))
	t.Cleanup(func() { w.Close() })
	w.Acquire(ChunkPos{X: 0, Z: 0})
	p := NewPhysics(w)
	p.rng = rand.New(rand.NewSource(5))
	p.w.SetDifficulty(DifficultyHard)
	wool, ok := registry.FindState("minecraft:white_wool", nil)
	if !ok {
		t.Fatal("no white wool")
	}
	var changes []BlockChange
	var primes []TNTPrime
	for range trials {
		w.SetBlock(1, GroundLevel, 0, wool)
		w.SetBlock(0, GroundLevel, 0, blocklogic.Fire)
		p.fireTick(0, GroundLevel, 0, blocklogic.Fire, &changes, &primes)
		if w.BlockAt(1, GroundLevel, 0) != wool {
			burned++
		}
		// The wool's own cell is the burn-out's, the fire's cell is the
		// fire's; every other fire is a spread.
		spread += firesIn(w, 0, GroundLevel, 0, 3)
		for _, c := range [][3]int32{{0, GroundLevel, 0}, {1, GroundLevel, 0}} {
			if name, _, _ := registry.BlockStateName(w.BlockAt(c[0], c[1], c[2])); name == "minecraft:fire" {
				spread--
			}
		}
		for dx := int32(-3); dx <= 3; dx++ {
			for dy := int32(0); dy <= 4; dy++ {
				for dz := int32(-3); dz <= 3; dz++ {
					if dy > 0 || dx != 0 || dz != 0 {
						w.SetBlock(dx, GroundLevel+dy, dz, airState)
					}
				}
			}
		}
	}
	return burned, spread
}

// TestIncreasedFireBurnoutHastensBurnAndHalvesSpread checks the biome
// attribute on a seeded run: the same fire in a jungle burns its neighbour
// out at 250 instead of 300 and spreads at half the odds.
func TestIncreasedFireBurnoutHastensBurnAndHalvesSpread(t *testing.T) {
	t.Parallel()
	const trials = 6000
	// Wool burns at 60 in 300 (0.2) normally and 60 in 250 (0.24) in a jungle.
	burnedPlains, spreadPlains := burnoutTrials(t, "minecraft:plains", trials)
	burnedJungle, spreadJungle := burnoutTrials(t, "minecraft:jungle", trials)
	if burnedJungle <= burnedPlains+100 {
		t.Errorf("wool burned %d times in a jungle, %d on plains; want clearly more", burnedJungle, burnedPlains)
	}
	if spreadJungle >= spreadPlains*3/4 {
		t.Errorf("fires spread %d in a jungle, %d on plains; want about half", spreadJungle, spreadPlains)
	}
}
