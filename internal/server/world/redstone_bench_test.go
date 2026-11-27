package world

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkWeightedPlateStrengthChange rechecks a pressed plate with an alternating load.
func BenchmarkWeightedPlateStrengthChange(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	touch := &touchStub{all: 1}
	p.SetToucher(touch)
	block := registry.BlockLightWeightedPressurePlate
	w.SetBlock(8, GroundLevel, 8, block.DefaultState())
	var changes []BlockChange
	var effects []Effect
	p.plateCheckPressed(8, GroundLevel, 8, block, 0, &changes, &effects)
	var clock int64
	b.ReportAllocs()
	for b.Loop() {
		touch.all = 4 - touch.all
		clock += weightedPlateTickDelay
		p.Tick(clock)
	}
}

// BenchmarkObserverClockGrid is one physics tick of 128 observer clocks: rows
// of redstone lamps under wire, observer facing east, observer facing west,
// wire, repeated 8 along x and 16 along z so neighbouring wires connect.
func BenchmarkObserverClockGrid(b *testing.B) { benchObserverClockGrid(b, false) }

// BenchmarkObserverClockGridTorches is the same grid with a standing torch on
// each observer, so every pulse's shape pass reaches support-watching blocks.
func BenchmarkObserverClockGridTorches(b *testing.B) { benchObserverClockGrid(b, true) }

func benchObserverClockGrid(b *testing.B, torches bool) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatalf("NewWorld: %v", err)
	}
	b.Cleanup(func() { _ = w.Close() })
	for cx := int32(-1); cx <= 2; cx++ {
		for cz := int32(-1); cz <= 2; cz++ {
			w.Acquire(ChunkPos{X: cx, Z: cz})
		}
	}
	p := NewPhysics(w)
	state := func(name string, props map[string]string) uint16 {
		id, ok := registry.FindState(name, props)
		if !ok {
			b.Fatalf("no state for %s %v", name, props)
		}
		return id
	}
	lamp := state("minecraft:redstone_lamp", nil)
	wire := state("minecraft:redstone_wire", nil)
	east := state("minecraft:observer", map[string]string{"facing": "east"})
	west := state("minecraft:observer", map[string]string{"facing": "west"})
	torch := state("minecraft:torch", nil)
	set := func(x, y, z int32, s uint16) {
		old := w.BlockAt(x, y, z)
		if !w.SetBlock(x, y, z, s) {
			b.Fatalf("SetBlock(%d, %d, %d) did not take", x, y, z)
		}
		p.NotifyChanged(x, y, z, old)
	}
	const y = GroundLevel
	for z := range int32(16) {
		for cell := range int32(8) {
			x := cell * 4
			for i := range int32(4) {
				set(x+i, y-1, z, lamp)
			}
			set(x, y, z, wire)
			set(x+3, y, z, wire)
			set(x+1, y, z, east)
			set(x+2, y, z, west)
			if torches {
				set(x+1, y+1, z, torch)
				set(x+2, y+1, z, torch)
			}
		}
	}
	var clock int64
	for range 40 {
		clock++
		p.Tick(clock)
	}
	b.ReportAllocs()
	for b.Loop() {
		clock++
		p.Tick(clock)
	}
}

// BenchmarkEntityInside is the movement sweep's visit of one cell a body
// stands in: a block that has no entity-inside reaction, and a pressure plate
// and a tripwire nothing presses.
func BenchmarkEntityInside(b *testing.B) {
	w, _ := NewWorld(b.TempDir(), NewFlatGenerator())
	b.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	for _, name := range []string{"air", "oak_pressure_plate", "tripwire"} {
		state := registry.BlockStates["minecraft:"+name]
		w.SetBlock(1, GroundLevel, 1, state)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			var changes []BlockChange
			var effects []Effect
			for b.Loop() {
				changes, effects = changes[:0], effects[:0]
				p.EntityInside(1, GroundLevel, 1, &changes, &effects)
			}
			if w.BlockAt(1, GroundLevel, 1) != state || len(changes) != 0 {
				b.Fatalf("%s changed: %v", name, changes)
			}
		})
	}
}

// BenchmarkComparatorInput reads an analog source directly and through a conductor.
func BenchmarkComparatorInput(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	for _, through := range []bool{false, true} {
		b.Run(fmt.Sprint("through=", through), func(b *testing.B) {
			input, _ := registry.FindState("minecraft:composter", map[string]string{"level": "4"})
			w.SetBlock(2, GroundLevel, 1, input)
			if through {
				w.SetBlock(2, GroundLevel, 1, registry.BlockStone.DefaultState())
				w.SetBlock(3, GroundLevel, 1, input)
			}
			b.ReportAllocs()
			for b.Loop() {
				p.comparatorInput(1, GroundLevel, 1, dirEast)
			}
		})
	}
}

// BenchmarkNeighborPower measures the boolean and numeric queries with a source below the receiver.
func BenchmarkNeighborPower(b *testing.B) {
	w := newTestWorld(b, NewFlatGenerator())
	b.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	weak, ok := registry.WithInt(registry.BlockDaylightDetector.DefaultState(), registry.PropPower, 7)
	if !ok {
		b.Fatal("no daylight detector state with power 7")
	}
	const x, y, z = 8, 80, 8
	for _, tc := range []struct {
		name  string
		state uint16
		power int
	}{
		{"air", registry.BlockAir.DefaultState(), 0},
		{"weak", weak, 7},
		{"full", registry.BlockRedstoneBlock.DefaultState(), 15},
	} {
		w.SetBlock(x, y-1, z, tc.state)
		b.Run(tc.name, func(b *testing.B) {
			b.Run("boolean", func(b *testing.B) {
				if got, want := p.hasNeighborSignal(x, y, z), tc.power > 0; got != want {
					b.Fatalf("hasNeighborSignal = %t, want %t", got, want)
				}
				b.ReportAllocs()
				for b.Loop() {
					p.hasNeighborSignal(x, y, z)
				}
			})
			b.Run("numeric", func(b *testing.B) {
				if got := p.bestNeighborSignal(x, y, z); got != tc.power {
					b.Fatalf("bestNeighborSignal = %d, want %d", got, tc.power)
				}
				b.ReportAllocs()
				for b.Loop() {
					p.bestNeighborSignal(x, y, z)
				}
			})
		})
	}
}

// BenchmarkComparatorTickUnchanged refreshes an idle comparator's stored output.
func BenchmarkComparatorTickUnchanged(b *testing.B) {
	for _, mode := range []string{"compare", "subtract"} {
		b.Run(mode, func(b *testing.B) {
			w, err := NewWorld(b.TempDir(), NewFlatGenerator())
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = w.Close() })
			w.Acquire(ChunkPos{})
			p := NewPhysics(w)
			state, _ := registry.WithProp(registry.BlockComparator.DefaultState(), registry.PropMode, mode)
			state, _ = registry.WithProp(state, registry.PropFacing, "east")
			w.SetBlock(8, GroundLevel, 8, state)
			w.EnsureBlockEntity(8, GroundLevel, 8, KindComparator)
			var changes []BlockChange
			b.ReportAllocs()
			for b.Loop() {
				changes = changes[:0]
				p.comparatorTick(8, GroundLevel, 8, state, dirEast, &changes)
			}
		})
	}
}

// BenchmarkDoubleChestComparatorWake drains a touch to a double chest read
// through a conductor, from either the near half or the far half.
func BenchmarkDoubleChestComparatorWake(b *testing.B) {
	w := newTestWorld(b, NewFlatGenerator())
	w.Acquire(ChunkPos{})
	w.Acquire(ChunkPos{X: 1})
	p := NewPhysics(w)
	const y = GroundLevel
	w.SetBlock(13, y, 0, registry.MustState("minecraft:chest", map[string]string{"facing": "north", "type": "left"}))
	w.SetBlock(14, y, 0, registry.MustState("minecraft:chest", map[string]string{"facing": "north", "type": "right"}))
	w.EnsureBlockEntity(13, y, 0, KindChest)
	w.EnsureBlockEntity(14, y, 0, KindChest)
	w.SetBlock(15, y, 0, registry.BlockStone.DefaultState())
	w.SetBlock(16, y, 0, registry.MustState("minecraft:comparator", map[string]string{"facing": "west"}))
	w.EnsureBlockEntity(16, y, 0, KindComparator)
	for x, name := range []string{"far", "near"} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			var tick int64
			for b.Loop() {
				w.Touch(13+int32(x), y, 0)
				tick++
				changes, _, _, effects := p.Tick(tick)
				p.RecycleTickResults(changes, effects)
			}
		})
	}
}

// BenchmarkVibrationOccluded traces the vibration rays through open air or wool.
func BenchmarkVibrationOccluded(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	for _, wool := range []bool{false, true} {
		b.Run(fmt.Sprint("wool=", wool), func(b *testing.B) {
			state := registry.BlockAir.DefaultState()
			if wool {
				state = registry.BlockWhiteWool.DefaultState()
			}
			w.SetBlock(4, GroundLevel, 1, state)
			v := Vibration{X: 1.5, Y: float64(GroundLevel) + 0.5, Z: 1.5}
			dest := blockKey{8, GroundLevel, 1}
			b.ReportAllocs()
			for b.Loop() {
				VibrationOccluded(w, v.X, v.Y, v.Z, float64(dest.x), float64(dest.y), float64(dest.z))
			}
		})
	}
}

// BenchmarkRedstoneHandlers alternates a power source beside a door and a
// redstone torch, and runs each block's neighbour reaction.
func BenchmarkRedstoneHandlers(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	const y = GroundLevel
	door := registry.BlockIronDoor
	lower, _ := registry.FindState(door.Name(), map[string]string{"half": "lower", "open": "false", "powered": "false"})
	upper, _ := registry.FindState(door.Name(), map[string]string{"half": "upper", "open": "false", "powered": "false"})
	w.SetBlock(2, y, 2, lower)
	w.SetBlock(2, y+1, 2, upper)
	torch := registry.BlockRedstoneTorch.DefaultState()
	w.SetBlock(6, y, 6, torch)
	source := registry.BlockRedstoneBlock.DefaultState()
	air := registry.BlockAir.DefaultState()
	b.Run("door", func(b *testing.B) {
		var changes []BlockChange
		b.ReportAllocs()
		on := false
		for b.Loop() {
			on = !on
			if on {
				w.SetBlock(3, y, 2, source)
			} else {
				w.SetBlock(3, y, 2, air)
			}
			changes = changes[:0]
			p.redstoneNeighborChanged(2, y, 2, w.BlockAt(2, y, 2), &changes, false)
		}
	})
	b.Run("torch", func(b *testing.B) {
		b.ReportAllocs()
		on := false
		for b.Loop() {
			on = !on
			if on {
				w.SetBlock(7, y, 6, source)
			} else {
				w.SetBlock(7, y, 6, air)
			}
			p.redstoneNeighborChanged(6, y, 6, w.BlockAt(6, y, 6), new([]BlockChange), false)
		}
	})
}

// BenchmarkPistonExtendRetract cycles a sticky piston pushing and pulling a
// stone block in steady state.
func BenchmarkPistonExtendRetract(b *testing.B) {
	w := newTestWorld(b, NewFlatGenerator())
	b.Cleanup(func() { w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	const y = GroundLevel
	piston := pistonStateOf(true, dirEast, false)
	stone := registry.BlockStone.DefaultState()
	power := registry.BlockRedstoneBlock.DefaultState()
	set := func(x, z int32, s uint16) {
		old := w.BlockAt(x, y, z)
		w.SetBlock(x, y, z, s)
		p.NotifyChanged(x, y, z, old)
	}
	set(4, 4, piston)
	set(5, 4, stone)
	var tick int64
	step := func(n int) {
		for range n {
			tick++
			changes, _, _, effects := p.Tick(tick)
			p.RecycleTickResults(changes, effects)
		}
	}
	step(4)
	// Warm up one extend/retract cycle so maps and slices reach steady state.
	set(4, 3, power)
	step(4)
	set(4, 3, airState)
	step(4)
	w.FlushLighting()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		set(4, 3, power)
		step(4)
		set(4, 3, airState)
		step(4)
	}
}

// BenchmarkWireLineToggle toggles a redstone block beside a 15-block wire line,
// exercising wireUpdatePower and wireWake across a full power-up and decay.
func BenchmarkWireLineToggle(b *testing.B) {
	w := newTestWorld(b, NewFlatGenerator())
	b.Cleanup(func() { w.Close() })
	w.Acquire(ChunkPos{})
	w.Acquire(ChunkPos{X: 1})
	p := NewPhysics(w)
	const y = GroundLevel
	wire := registry.BlockRedstoneWire.DefaultState()
	for x := int32(1); x <= 15; x++ {
		w.SetBlock(x, y, 4, wire)
	}
	source := registry.BlockRedstoneBlock.DefaultState()
	set := func(s uint16) {
		old := w.BlockAt(0, y, 4)
		w.SetBlock(0, y, 4, s)
		changes, _, _ := p.NotifyChanged(0, y, 4, old)
		p.RecycleTickResults(changes, nil)
	}
	set(source)
	set(airState)
	w.FlushLighting()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		set(source)
		set(airState)
	}
}
