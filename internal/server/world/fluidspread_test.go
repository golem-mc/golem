package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestFluidSpreadCellClassification(t *testing.T) {
	t.Parallel()
	for _, kind := range []fluidKind{fluidWater, fluidLava} {
		c := spreadContext{
			kind:        kind,
			flowingType: fluidState{kind: kind, amount: 8, falling: true},
			holdFlags:   currentFluidHoldFlags(),
		}
		for state := range uint16(registry.StateCount) {
			flags := c.cellFlags(state)
			fs := fluidHolding(state)
			enterable := (fs.kind != kind || !fs.isSource()) && canHoldAny(state) && canHoldSpecific(state, c.flowingType)
			if (flags&cellEnterable != 0) != enterable {
				t.Fatalf("kind %d state %d: enterable = %v, want %v", kind, state, flags&cellEnterable != 0, enterable)
			}
			shape := collisionOf(state)
			if (flags&cellEmptyShape != 0) != (len(shape.boxes) == 0) || (flags&cellFullShape != 0) != shape.fullCube {
				t.Fatalf("state %d: shape flags %d disagree with collision", state, flags)
			}
		}
	}
}

func TestFluidSpreadCellCache(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	c := p.newSpreadContext(7, 80, 7, fluidWater)
	stone := registry.BlockStone.DefaultState()
	for _, x := range []int32{1, 13, 14} {
		state, flags := c.cell(x, 7)
		if state != airState || flags != cellEnterable|cellEmptyShape {
			t.Fatalf("air at %d: state %d, flags %d", x, state, flags)
		}
		w.SetBlock(x, 80, 7, stone)
		state, flags = c.cell(x, 7)
		if x == 14 {
			if state != stone || flags != cellFullShape {
				t.Fatalf("uncached stone: state %d, flags %d", state, flags)
			}
		} else if state != airState || flags != cellEnterable|cellEmptyShape {
			t.Fatalf("cached air at %d: state %d, flags %d", x, state, flags)
		}
	}
}

// uncachedSlopeDistance keeps the individual fluid gates as an oracle for the
// per-cell classification and wall shortcuts.
func uncachedSlopeDistance(x, z int32, pass int, fromDX, fromDZ int32, b fluidBehaviour, c *spreadContext) int {
	lowest := 1000
	state := c.state(x, z)
	for _, d := range horizontal4 {
		if d[0] == fromDX && d[1] == fromDZ {
			continue
		}
		nx, nz := x+d[0], z+d[1]
		ns := c.state(nx, nz)
		axis, positive := faceOf(d[0], d[1])
		if !canMaybePassThrough(c.kind, state, axis, positive, ns, fluidHolding(ns)) || !canHoldSpecific(ns, c.flowingType) {
			continue
		}
		if c.isHole(nx, nz) {
			return pass
		}
		if pass < b.slopeFindDistance {
			if v := uncachedSlopeDistance(nx, nz, pass+1, -d[0], -d[1], b, c); v < lowest {
				lowest = v
			}
		}
	}
	return lowest
}

func TestFluidSlopeClassificationMatchesIndividualGates(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	states := []uint16{
		airState, airState, airState,
		registry.BlockStone.DefaultState(),
		registry.MustState("minecraft:oak_slab", map[string]string{"type": "top"}),
		registry.MustState("minecraft:oak_stairs", nil),
		registry.MustState("minecraft:oak_stairs", map[string]string{"waterlogged": "true"}),
		registry.MustState("minecraft:water", nil),
		registry.MustState("minecraft:lava", nil),
	}
	for layout := range 8 {
		for x := int32(1); x <= 13; x++ {
			for z := int32(1); z <= 13; z++ {
				w.SetBlock(x, 64, z, states[(int(x)*7+int(z)*11+layout)%len(states)])
				floor := registry.BlockStone.DefaultState()
				if (int(x)*3+int(z)+layout)%13 == 0 {
					floor = airState
				}
				w.SetBlock(x, 63, z, floor)
			}
		}
		for _, kind := range []fluidKind{fluidWater, fluidLava} {
			for di, d := range horizontal4 {
				c := p.newSpreadContext(7, 64, 7, kind)
				b := fluidBehaviours[kind]
				x, z := 7+d[0], 7+d[1]
				want := uncachedSlopeDistance(x, z, 1, -d[0], -d[1], b, &c)
				c.state(x, z)
				ti, _ := c.index(x, z)
				got := c.slopeDistance(ti, 1, di^2, b.slopeFindDistance)
				if got != want {
					t.Fatalf("layout %d kind %d direction %v: distance %d, want %d", layout, kind, d, got, want)
				}
			}
		}
	}
}

// TestLavaSpreadReachByDimension checks the fast-lava drop-off and slope
// distance: lava reaches 7 blocks where it runs fast, 3 elsewhere.
func TestLavaSpreadReachByDimension(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fast     bool
		reach    int32
		wantNone int32
	}{{"normal", false, 3, 4}, {"fast", true, 7, 8}} {
		t.Run(tc.name, func(t *testing.T) {
			w, p := newPhysicsWorld(t)
			p.fastLava = tc.fast
			var clock int64
			setBlock(t, w, p, 0, GroundLevel, 0, lavaByLevel[0])
			tickToQuiet(t, p, &clock, 800)
			if got := fluidHolding(w.BlockAt(tc.reach, GroundLevel, 0)); got.kind != fluidLava {
				t.Errorf("no lava at +%d", tc.reach)
			}
			assertAir(t, w, tc.wantNone, GroundLevel, 0, "lava's reach ends")
		})
	}
}
