package world

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// The observer and the farm dice: a random tick's growth is a setBlock
// whose flags hold no 16, whose shape pass is the observer's whole way of
// hearing it. Each test seeds the engine's dice and runs the passes beside
// the physics ticks that drain their bookings, in the loop's own order:
// the scheduled visits of a game tick first, the random pass of the same
// tick after.
// vanilla: Level.setBlock, ObserverBlock.updateShape

// observerDicePulse drives random tick passes over chunk {0, 0}, the dice
// reseeded, until changed reports the block the circuit's observer watches
// — its facing west, (13, 63, 9) — has moved, then drains the eight engine
// ticks after. It reports how many of those ticks the observer stood
// powered and whether the lamp behind its output wire was seen lit.
func observerDicePulse(t *testing.T, w *World, p *Physics, clock *int64, speed int, seed int64, changed func() bool) (pulseTicks int, lampOn bool) {
	t.Helper()
	p.rng = rand.New(rand.NewSource(seed))
	for range 8000 {
		*clock++
		p.Tick(*clock)
		p.RandomTick(*clock, []ChunkPos{{0, 0}}, speed, 1)
		if !changed() {
			continue
		}
		for range 8 {
			*clock++
			p.Tick(*clock)
			if observerProps(t, w, 14, 63, 9)["powered"] == "true" {
				pulseTicks++
			}
			if _, lamp := rsNameAt(t, w, 16, 63, 9); lamp["lit"] == "true" {
				lampOn = true
			}
		}
		return pulseTicks, lampOn
	}
	t.Fatal("the watched block never moved under the seeded dice")
	return 0, false
}

// TestAnAgingCanePulsesTheObserverWatchingIt checks the flag-260 write: a
// cane whose age the random pass bumps pulses the observer watching its
// cell for the two game ticks vanilla's pulse lasts, no client ever told
// of the age itself.
// vanilla: SugarCaneBlock.randomTick
func TestAnAgingCanePulsesTheObserverWatchingIt(t *testing.T) {
	w, p, clock := observerCircuit(t)
	setBlock(t, w, p, 13, 62, 9, registry.BlockSand.DefaultState())
	setBlock(t, w, p, 12, 62, 9, registry.BlockWater.DefaultState())
	setBlock(t, w, p, 13, 63, 9, rsState(t, "minecraft:sugar_cane", map[string]string{"age": "14"}))
	for range 12 {
		*clock++
		p.Tick(*clock)
	}
	if props := observerProps(t, w, 14, 63, 9); props["powered"] == "true" {
		t.Fatalf("setup: the observer was pulsing before the dice: %v", props)
	}

	pulseTicks, lampOn := observerDicePulse(t, w, p, clock, 100, 11, func() bool {
		_, props := rsNameAt(t, w, 13, 63, 9)
		return props["age"] == "15"
	})

	if pulseTicks != 2 {
		t.Errorf("the cane's age bump pulsed the observer for %d ticks, want the two-tick pulse", pulseTicks)
	}
	if !lampOn {
		t.Error("the lamp behind the observer never lit on the age bump")
	}
}

// TestADyingFarmlandPulsesTheObserverWatchingIt checks the flags-three
// write the death makes: a farmland the pass dries out to dirt pulses the
// observer watching the square.
// vanilla: FarmlandBlock.randomTick, turnToDirt
func TestADyingFarmlandPulsesTheObserverWatchingIt(t *testing.T) {
	w, p, clock := observerCircuit(t)
	setBlock(t, w, p, 13, 63, 9, rsState(t, "minecraft:farmland", map[string]string{"moisture": "0"}))
	for range 12 {
		*clock++
		p.Tick(*clock)
	}
	if props := observerProps(t, w, 14, 63, 9); props["powered"] == "true" {
		t.Fatalf("setup: the observer was pulsing before the dice: %v", props)
	}

	// Nothing above the square maintains it, so the first landed die
	// takes it to dirt.
	pulseTicks, lampOn := observerDicePulse(t, w, p, clock, 100, 11, func() bool {
		name, _ := rsNameAt(t, w, 13, 63, 9)
		return name == "minecraft:dirt"
	})

	if pulseTicks != 2 {
		t.Errorf("the farmland's death pulsed the observer for %d ticks, want the two-tick pulse", pulseTicks)
	}
	if !lampOn {
		t.Error("the lamp behind the observer never lit on the farmland's death")
	}
}

// TestCropGrowthSpeedStartsAtTheVanillaBase pins getGrowthSpeed's base of
// one over the three-by-three of ground under the crop: a dry field scores
// four, a wet one ten, and a single same crop diagonal to it halves whatever
// the ground is worth. The diagonal is the one crop the vanilla rule lets
// halve on its own; a lone neighbour on an axis needs a second on the other
// to count.
// vanilla: CropBlock.getGrowthSpeed
func TestCropGrowthSpeedStartsAtTheVanillaBase(t *testing.T) {
	w, p := newPhysicsWorld(t)
	dry, ok := registry.FindState("minecraft:farmland", map[string]string{"moisture": "0"})
	if !ok {
		t.Fatal("the registry has no dry farmland")
	}
	wet, ok := registry.FindState("minecraft:farmland", map[string]string{"moisture": "7"})
	if !ok {
		t.Fatal("the registry has no wet farmland")
	}
	wheat, ok := registry.FindState("minecraft:wheat", map[string]string{"age": "0"})
	if !ok {
		t.Fatal("the registry has no wheat")
	}
	const x, y, z = int32(0), GroundLevel, int32(0)

	fill := func(state uint16) {
		for dz := int32(-1); dz <= 1; dz++ {
			for dx := int32(-1); dx <= 1; dx++ {
				if !w.SetBlock(x+dx, y-1, z+dz, state) {
					t.Fatalf("setup: farmland at (%d, %d, %d) did not take", x+dx, y-1, z+dz)
				}
			}
		}
	}

	fill(dry)
	if got := p.cropGrowthSpeed(x, y, z, registry.BlockWheat); got != 4 {
		t.Errorf("dry 3x3 growth speed = %g, want 4", got)
	}

	fill(wet)
	if got := p.cropGrowthSpeed(x, y, z, registry.BlockWheat); got != 10 {
		t.Errorf("wet 3x3 growth speed = %g, want 10", got)
	}
	fill(dry)

	if !w.SetBlock(x+1, y, z+1, wheat) {
		t.Fatal("setup: the neighbouring wheat did not take")
	}
	if got := p.cropGrowthSpeed(x, y, z, registry.BlockWheat); got != 2 {
		t.Errorf("dry 3x3 with a neighbouring wheat = %g, want 2", got)
	}
}

// TestStemsSurviveOnFarmlandInTheDarkButWheatDoesNot checks that only the
// crop rule asks for light: a stem is a plain farmland-supported plant.
// vanilla: StemBlock.mayPlaceOn, CropBlock.canSurvive
func TestStemsSurviveOnFarmlandInTheDarkButWheatDoesNot(t *testing.T) {
	w, p := newPhysicsWorld(t)
	// Deep in the stone, where no light reaches.
	setBlock(t, w, p, 4, 10, 4, rsState(t, "minecraft:farmland", map[string]string{"moisture": "7"}))
	if light := rawLightAt(w, 4, 11, 4); light != 0 {
		t.Fatalf("setup: raw light over the farmland = %d, want 0", light)
	}
	for _, c := range []struct {
		name string
		want bool
	}{
		{"minecraft:melon_stem", true},
		{"minecraft:pumpkin_stem", true},
		{"minecraft:wheat", false},
	} {
		state := rsState(t, c.name, map[string]string{"age": "0"})
		if got := w.Survives(4, 11, 4, state); got != c.want {
			t.Errorf("%s on farmland at light 0: Survives = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestCaneAndCactusLeftUnsupportedPlayTheBreakEvent checks the level event
// 2001 destroyBlock sends when a scheduled survival check breaks them.
func TestCaneAndCactusLeftUnsupportedPlayTheBreakEvent(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64
	for i, name := range []string{"minecraft:sugar_cane", "minecraft:cactus"} {
		x, y, z := int32(4+i*3), int32(GroundLevel+4), int32(4)
		state := registry.BlockStates[name]
		setBlock(t, w, p, x, y-1, z, registry.BlockStates["minecraft:sand"])
		w.SetBlock(x, y, z, state)
		old := w.BlockAt(x, y-1, z)
		w.SetBlock(x, y-1, z, 0)
		_, effects, _ := p.NotifyChanged(x, y-1, z, old)
		for range 8 {
			clock++
			_, _, _, e := p.Tick(clock)
			effects = append(effects, e...)
		}
		events := 0
		for _, e := range effects {
			if e.LevelEvent == 2001 && e.LevelEventData == int32(state) {
				events++
			}
		}
		if events != 1 {
			t.Errorf("%s sent %d break events, want 1", name, events)
		}
	}
}

// Rails and repeaters leave through neighborChanged, which drops them without
// the level event 2001.
func TestRailAndRepeaterPoppedByNeighborChangedPlayNoBreakEvent(t *testing.T) {
	w, p := newPhysicsWorld(t)
	for i, name := range []string{"minecraft:rail", "minecraft:repeater", "minecraft:comparator"} {
		x, y, z := int32(4+i*3), int32(GroundLevel+4), int32(4)
		state := registry.BlockStates[name]
		setBlock(t, w, p, x, y-1, z, registry.BlockStates["minecraft:stone"])
		w.SetBlock(x, y, z, state)
		old := w.BlockAt(x, y-1, z)
		w.SetBlock(x, y-1, z, 0)
		_, effects, _ := p.NotifyChanged(x, y-1, z, old)
		for clock := int64(1); clock <= 8; clock++ {
			_, _, _, e := p.Tick(clock)
			effects = append(effects, e...)
		}
		drops := 0
		for _, e := range effects {
			if e.LevelEvent == 2001 {
				t.Errorf("%s sent a break event", name)
			}
			if e.BreakDrop == state {
				drops++
			}
		}
		if drops != 1 {
			t.Errorf("%s dropped %d times, want 1", name, drops)
		}
	}
}

// growthTrials throws random visits at the crop at (x, y, z) until the cell
// changes, reporting whether it did within the bound.
func growthTrials(w *World, p *Physics, x, y, z int32, trials int) bool {
	start := w.BlockAt(x, y, z)
	for range trials {
		var changes []BlockChange
		var decays []Decay
		p.randomVisit(x, y, z, w.BlockAt(x, y, z), &changes, &decays)
		if w.BlockAt(x, y, z) != start {
			return true
		}
	}
	return false
}

// TestTorchflowerCropGrowsOnRandomTicks pins the ticking index, the age
// step and the flower at the end.
// vanilla: TorchflowerCropBlock.randomTick, getStateForAge
func TestTorchflowerCropGrowsOnRandomTicks(t *testing.T) {
	w, p := newPhysicsWorld(t)
	p.rng = rand.New(rand.NewSource(1))
	const x, y, z = int32(0), GroundLevel, int32(0)
	setBlock(t, w, p, x, y-1, z, rsState(t, "minecraft:farmland", map[string]string{"moisture": "7"}))
	young := rsState(t, "minecraft:torchflower_crop", map[string]string{"age": "0"})
	if !randomlyTicking(young) {
		t.Fatal("torchflower crop is not in the random-tick index")
	}
	setBlock(t, w, p, x, y, z, young)
	if !growthTrials(w, p, x, y, z, 2000) {
		t.Fatal("age 0 never grew")
	}
	if got, want := w.BlockAt(x, y, z), rsState(t, "minecraft:torchflower_crop", map[string]string{"age": "1"}); got != want {
		t.Fatalf("after one step state = %d, want age 1 (%d)", got, want)
	}
	if !growthTrials(w, p, x, y, z, 2000) {
		t.Fatal("age 1 never grew")
	}
	if got, want := w.BlockAt(x, y, z), rsState(t, "minecraft:torchflower", nil); got != want {
		t.Fatalf("after the last step state = %d, want the torchflower (%d)", got, want)
	}
}

// TestPitcherCropGrowsOnRandomTicksIntoTwoBlocks pins the lower-half-only
// index, the growth and the upper half joining at age 3.
// vanilla: PitcherCropBlock.isRandomlyTicking, randomTick, grow
func TestPitcherCropGrowsOnRandomTicksIntoTwoBlocks(t *testing.T) {
	w, p := newPhysicsWorld(t)
	p.rng = rand.New(rand.NewSource(1))
	const x, y, z = int32(0), GroundLevel, int32(0)
	setBlock(t, w, p, x, y-1, z, rsState(t, "minecraft:farmland", map[string]string{"moisture": "7"}))
	lower := func(age string) uint16 {
		return rsState(t, "minecraft:pitcher_crop", map[string]string{"half": "lower", "age": age})
	}
	if !randomlyTicking(lower("0")) || randomlyTicking(lower("4")) ||
		randomlyTicking(rsState(t, "minecraft:pitcher_crop", map[string]string{"half": "upper", "age": "3"})) {
		t.Fatal("pitcher crop ticking index wrong")
	}
	setBlock(t, w, p, x, y, z, lower("0"))
	for _, age := range []string{"1", "2", "3"} {
		if !growthTrials(w, p, x, y, z, 5000) {
			t.Fatalf("never grew to age %s", age)
		}
		if got := w.BlockAt(x, y, z); got != lower(age) {
			t.Fatalf("state = %d, want lower age %s (%d)", got, age, lower(age))
		}
	}
	upper := rsState(t, "minecraft:pitcher_crop", map[string]string{"half": "upper", "age": "3"})
	if got := w.BlockAt(x, y+1, z); got != upper {
		t.Fatalf("upper half = %d, want %d", got, upper)
	}
}

// TestPitcherCropDoesNotGrowIntoABlock pins the room gate.
// vanilla: PitcherCropBlock.canGrow
func TestPitcherCropDoesNotGrowIntoABlock(t *testing.T) {
	w, p := newPhysicsWorld(t)
	p.rng = rand.New(rand.NewSource(1))
	const x, y, z = int32(0), GroundLevel, int32(0)
	setBlock(t, w, p, x, y-1, z, rsState(t, "minecraft:farmland", map[string]string{"moisture": "7"}))
	setBlock(t, w, p, x, y, z, rsState(t, "minecraft:pitcher_crop", map[string]string{"half": "lower", "age": "2"}))
	setBlock(t, w, p, x, y+1, z, rsState(t, "minecraft:stone", nil))
	if growthTrials(w, p, x, y, z, 3000) {
		t.Fatal("grew into a block")
	}
}

// lightTunnel seals a stone corridor on the ground row, a torch at the far
// end, and returns the crop cell at its near end whose light is 14 - dist.
func lightTunnel(t *testing.T, w *World, p *Physics, dist int32) (x, y, z int32) {
	t.Helper()
	y = GroundLevel
	stone := rsState(t, "minecraft:stone", nil)
	for cx := int32(-1); cx <= dist+1; cx++ {
		for cz := int32(-1); cz <= 1; cz++ {
			setBlock(t, w, p, cx, y+1, cz, stone)
			if cz != 0 || cx == -1 || cx == dist+1 {
				setBlock(t, w, p, cx, y, cz, stone)
			}
		}
	}
	setBlock(t, w, p, 0, y-1, 0, rsState(t, "minecraft:farmland", map[string]string{"moisture": "7"}))
	setBlock(t, w, p, dist, y, 0, torchState)
	w.Relight(0, 0)
	return 0, y, 0
}

// TestTorchflowerCropGrowsOnlyFromLightNine pins the light gate at 9.
// vanilla: CropBlock.randomTick
func TestTorchflowerCropGrowsOnlyFromLightNine(t *testing.T) {
	for _, tc := range []struct {
		dist int32
		grow bool
	}{{5, true}, {6, false}} {
		t.Run("", func(t *testing.T) {
			w, p := newPhysicsWorld(t)
			p.rng = rand.New(rand.NewSource(1))
			x, y, z := lightTunnel(t, w, p, tc.dist)
			if got, want := int(rawLightAt(w, x, y, z)), int(14-tc.dist); got != want {
				t.Fatalf("setup: light = %d, want %d", got, want)
			}
			setBlock(t, w, p, x, y, z, rsState(t, "minecraft:torchflower_crop", map[string]string{"age": "0"}))
			if got := growthTrials(w, p, x, y, z, 3000); got != tc.grow {
				t.Errorf("light %d: grew = %v, want %v", 14-tc.dist, got, tc.grow)
			}
		})
	}
}

// TestTorchflowerCropSkipsAThirdOfItsVisits mirrors the dice of one visit:
// the one-in-three skip first, then the growth roll.
// vanilla: TorchflowerCropBlock.randomTick
func TestTorchflowerCropSkipsAThirdOfItsVisits(t *testing.T) {
	w, p := newPhysicsWorld(t)
	const x, y, z = int32(0), GroundLevel, int32(0)
	setBlock(t, w, p, x, y-1, z, rsState(t, "minecraft:farmland", map[string]string{"moisture": "7"}))
	young := rsState(t, "minecraft:torchflower_crop", map[string]string{"age": "0"})
	setBlock(t, w, p, x, y, z, young)
	bound := int(float32(25.0)/p.cropGrowthSpeed(x, y, z, registry.BlockTorchflowerCrop)) + 1
	skipped := 0
	for seed := range int64(300) {
		r := rand.New(rand.NewSource(seed))
		skip := r.Intn(3) == 0
		want := !skip && r.Intn(bound) == 0
		if skip {
			skipped++
		}
		p.rng = rand.New(rand.NewSource(seed))
		w.SetBlock(x, y, z, young)
		if got := growthTrials(w, p, x, y, z, 1); got != want {
			t.Fatalf("seed %d: grew = %v, want %v", seed, got, want)
		}
	}
	if skipped == 0 {
		t.Fatal("no seed exercised the skip")
	}
}

// TestPitcherCropGrowsOnlyFromLightEight pins the light gate at 8.
// vanilla: PitcherCropBlock.canGrow
func TestPitcherCropGrowsOnlyFromLightEight(t *testing.T) {
	for _, tc := range []struct {
		dist int32
		grow bool
	}{{6, true}, {7, false}} {
		t.Run("", func(t *testing.T) {
			w, p := newPhysicsWorld(t)
			p.rng = rand.New(rand.NewSource(1))
			x, y, z := lightTunnel(t, w, p, tc.dist)
			if got, want := int(rawLightAt(w, x, y, z)), int(14-tc.dist); got != want {
				t.Fatalf("setup: light = %d, want %d", got, want)
			}
			setBlock(t, w, p, x, y, z, rsState(t, "minecraft:pitcher_crop", map[string]string{"half": "lower", "age": "0"}))
			if got := growthTrials(w, p, x, y, z, 3000); got != tc.grow {
				t.Errorf("light %d: grew = %v, want %v", 14-tc.dist, got, tc.grow)
			}
		})
	}
}

// TestAGrowingPitcherCropPulsesTheObserverWatchingIt pins the shaped write
// of the lower half, shared by random ticks and bone meal.
// vanilla: Level.setBlock, ObserverBlock.updateShape
func TestAGrowingPitcherCropPulsesTheObserverWatchingIt(t *testing.T) {
	w, p := newPhysicsWorld(t)
	const x, y, z = int32(13), GroundLevel, int32(9)
	setBlock(t, w, p, x, y-1, z, rsState(t, "minecraft:farmland", map[string]string{"moisture": "7"}))
	setBlock(t, w, p, x, y, z, rsState(t, "minecraft:pitcher_crop", map[string]string{"half": "lower", "age": "0"}))
	setBlock(t, w, p, x+1, y, z, rsState(t, "minecraft:observer", map[string]string{"facing": "west", "powered": "false"}))
	var clock int64
	for range 12 {
		clock++
		p.Tick(clock)
	}
	var changes []BlockChange
	if !p.growPitcherCrop(x, y, z, w.BlockAt(x, y, z), &changes) {
		t.Fatal("the crop did not grow")
	}
	pulsed := false
	for range 8 {
		clock++
		p.Tick(clock)
		if observerProps(t, w, x+1, y, z)["powered"] == "true" {
			pulsed = true
		}
	}
	if !pulsed {
		t.Error("the observer beside the growing crop never pulsed")
	}
}

// newOneChunkPhysics loads only chunk (0,0); fixtures sit at x=z=8, clear of
// the chunk edge that neighbour updates and spread attempts reach across.
func newOneChunkPhysics(t *testing.T) (*World, *Physics) {
	t.Helper()
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { w.Close() })
	w.Acquire(ChunkPos{})
	return w, NewPhysics(w)
}

// TestSpreadingDirtUnderSealingBlock pins the bottom-face seal test.
// vanilla: SpreadingSnowyBlock.canStayAlive, LightEngine.getLightDampeningInto
func TestSpreadingDirtUnderSealingBlock(t *testing.T) {
	cases := []struct {
		name  string
		block string
		props map[string]string
		dies  bool
	}{
		{"bottom slab", "minecraft:stone_slab", map[string]string{"type": "bottom"}, true},
		{"bottom stairs", "minecraft:oak_stairs", map[string]string{"half": "bottom", "shape": "straight"}, true},
		{"dirt path", "minecraft:dirt_path", nil, true},
		{"top slab", "minecraft:stone_slab", map[string]string{"type": "top"}, false},
		{"glass", "minecraft:glass", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, p := newOneChunkPhysics(t)
			const x, y, z = int32(8), GroundLevel, int32(8)
			grass := registry.BlockGrassBlock.DefaultState()
			setBlock(t, w, p, x, y, z, grass)
			setBlock(t, w, p, x, y+1, z, rsState(t, c.block, c.props))
			var changes []BlockChange
			var decays []Decay
			p.randomVisit(x, y, z, w.BlockAt(x, y, z), &changes, &decays)
			if died := w.BlockAt(x, y, z) == dirtState; died != c.dies {
				t.Errorf("died = %v, want %v", died, c.dies)
			}
		})
	}
}

// vanilla: NyliumBlock.randomTick
func TestNyliumRevertsWhenCovered(t *testing.T) {
	for _, c := range []struct {
		name  string
		above uint16
		dies  bool
	}{
		{"stone", registry.BlockStone.DefaultState(), true},
		{"air", 0, false},
		{"glass", registry.BlockGlass.DefaultState(), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, p := newOneChunkPhysics(t)
			const x, y, z = int32(8), GroundLevel, int32(8)
			nylium := registry.BlockCrimsonNylium.DefaultState()
			setBlock(t, w, p, x, y, z, nylium)
			if c.above != 0 {
				setBlock(t, w, p, x, y+1, z, c.above)
			}
			var changes []BlockChange
			var decays []Decay
			p.randomVisit(x, y, z, w.BlockAt(x, y, z), &changes, &decays)
			if died := w.BlockAt(x, y, z) == registry.BlockNetherrack.DefaultState(); died != c.dies {
				t.Errorf("reverted = %v, want %v", died, c.dies)
			}
		})
	}
}
