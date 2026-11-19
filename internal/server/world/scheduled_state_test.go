package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestScheduledWritesPreserveUnchangedProperties(t *testing.T) {
	for _, tc := range []struct {
		name, block   string
		before, after map[string]string
		prepare       func(*World)
		visit         func(*Physics, uint16, *[]BlockChange)
	}{
		{"leaf", "minecraft:oak_leaves", map[string]string{"distance": "7", "persistent": "true", "waterlogged": "true"}, map[string]string{"distance": "1", "persistent": "true", "waterlogged": "true"},
			func(w *World) { w.SetBlock(9, GroundLevel, 8, registry.BlockOakLog.DefaultState()) },
			func(p *Physics, s uint16, changes *[]BlockChange) { p.leafTick(8, GroundLevel, 8, s, changes) }},
		{"scaffolding", "minecraft:scaffolding", map[string]string{"distance": "5", "bottom": "true", "waterlogged": "true"}, map[string]string{"distance": "0", "bottom": "false", "waterlogged": "true"}, nil,
			func(p *Physics, s uint16, changes *[]BlockChange) {
				var falls []FallingSpawn
				p.scaffoldingTick(8, GroundLevel, 8, s, changes, &falls)
			}},
		{"wet ghast", "minecraft:dried_ghast", map[string]string{"hydration": "1", "facing": "west", "waterlogged": "true"}, map[string]string{"hydration": "2", "facing": "west", "waterlogged": "true"}, nil,
			func(p *Physics, s uint16, changes *[]BlockChange) { p.driedGhastTick(8, GroundLevel, 8, s, changes) }},
		{"dry ghast", "minecraft:dried_ghast", map[string]string{"hydration": "2", "facing": "east"}, map[string]string{"hydration": "1", "facing": "east"}, nil,
			func(p *Physics, s uint16, changes *[]BlockChange) { p.driedGhastTick(8, GroundLevel, 8, s, changes) }},
		{"egg", "minecraft:turtle_egg", map[string]string{"hatch": "2", "eggs": "4"}, map[string]string{"hatch": "2", "eggs": "3"}, nil,
			func(p *Physics, _ uint16, _ *[]BlockChange) {
				if _, ok := p.TurtleEggTrampled(8, GroundLevel, 8, 1); !ok {
					t.Fatal("egg was not trampled")
				}
			}},
		{"rail", "minecraft:powered_rail", map[string]string{"waterlogged": "true", "powered": "true"}, map[string]string{"waterlogged": "true", "powered": "true", "shape": "east_west"},
			func(w *World) {
				w.SetBlock(9, GroundLevel, 8, registry.BlockPoweredRail.DefaultState())
				w.SetBlock(8, GroundLevel, 9, registry.BlockRedstoneBlock.DefaultState())
			},
			func(p *Physics, s uint16, changes *[]BlockChange) {
				r, _ := p.railAround(8, GroundLevel, 8, s)
				r.updateDir(true, false, changes)
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld(t, NewFlatGenerator())
			t.Cleanup(func() { _ = w.Close() })
			w.Acquire(ChunkPos{})
			p := NewPhysics(w)
			if tc.prepare != nil {
				tc.prepare(w)
			}
			state := registry.MustState(tc.block, tc.before)
			w.SetBlock(8, GroundLevel, 8, state)
			var changes []BlockChange
			tc.visit(p, state, &changes)
			if got, want := w.BlockAt(8, GroundLevel, 8), registry.MustState(tc.block, tc.after); got != want {
				t.Fatalf("got %v, want %v", registry.StateProps(got), registry.StateProps(want))
			}
		})
	}
}

func TestHeartAndResinKeepAxisNaturalAndFaces(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	w.SetBlock(7, GroundLevel, 8, registry.MustState("minecraft:pale_oak_log", map[string]string{"axis": "x"}))
	w.SetBlock(9, GroundLevel, 8, registry.MustState("minecraft:pale_oak_log", map[string]string{"axis": "x"}))
	state := registry.MustState("minecraft:creaking_heart", map[string]string{"axis": "x", "natural": "true", "creaking_heart_state": "uprooted"})
	rooted, changed := CreakingHeartRootedState(w, state, 8, GroundLevel, 8)
	if !changed || rooted != registry.MustState("minecraft:creaking_heart", map[string]string{"axis": "x", "natural": "true", "creaking_heart_state": "dormant"}) {
		t.Fatal("rooting lost the heart's properties")
	}
	w.SetClockTime(OverworldClock, 18000)
	awake, changed := CreakingHeartNextState(w, rooted, 8, GroundLevel, 8, false)
	if !changed || awake != registry.MustState("minecraft:creaking_heart", map[string]string{"axis": "x", "natural": "true", "creaking_heart_state": "awake"}) {
		t.Fatal("waking lost the heart's properties")
	}
	resin := registry.MustState("minecraft:resin_clump", map[string]string{"waterlogged": "true", "north": "true"})
	for _, cell := range []uint16{airState, waterByLevel[0], resin} {
		next, ok := resinFace(cell, registry.PropEast)
		if !ok || !registry.StateBool(next, registry.PropEast) || registry.StateBool(next, registry.PropWaterlogged) != (cell != airState) || registry.StateBool(next, registry.PropNorth) != (cell == resin) {
			t.Fatalf("unexpected resin from state %d: %v", cell, registry.StateProps(next))
		}
		if _, ok := resinFace(next, registry.PropEast); ok {
			t.Fatal("added an existing face")
		}
	}
}

func TestGrowthBuildsStatesWithoutItemForms(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	var changes []BlockChange
	p.placeDoublePlant(8, GroundLevel, 8, registry.BlockTallSeagrass, &changes)
	for dy, half := range []string{"lower", "upper"} {
		if got, want := w.BlockAt(8, GroundLevel+int32(dy), 8), registry.MustState("minecraft:tall_seagrass", map[string]string{"half": half}); got != want {
			t.Fatalf("seagrass %s = %d, want %d", half, got, want)
		}
	}
	w.SetBlock(8, GroundLevel, 8, waterByLevel[0])
	p.setDripleafCell(8, GroundLevel, 8, registry.BlockBigDripleafStem, "west", &changes)
	if got, want := w.BlockAt(8, GroundLevel, 8), registry.MustState("minecraft:big_dripleaf_stem", map[string]string{"facing": "west", "waterlogged": "true"}); got != want {
		t.Fatalf("dripleaf stem = %d, want %d", got, want)
	}
	w.SetBlock(8, GroundLevel, 8, registry.MustState("minecraft:pitcher_crop", map[string]string{"age": "2"}))
	w.SetBlock(8, GroundLevel-1, 8, registry.BlockFarmland.DefaultState())
	w.SetBlock(8, GroundLevel+1, 8, airState)
	if !p.growPitcherCrop(8, GroundLevel, 8, w.BlockAt(8, GroundLevel, 8), &changes) {
		t.Fatal("pitcher crop did not grow")
	}
	for dy, half := range []string{"lower", "upper"} {
		if got, want := w.BlockAt(8, GroundLevel+int32(dy), 8), registry.MustState("minecraft:pitcher_crop", map[string]string{"half": half, "age": "3"}); got != want {
			t.Fatalf("pitcher %s = %d, want %d", half, got, want)
		}
	}
}

// TestWaterloggedBlockTicksRunBesideFluidVisits: a waterlogged big dripleaf or
// detector rail still gets its own block tick.
// vanilla: BigDripleafBlock.tick, DetectorRailBlock.tick
func TestWaterloggedBlockTicksRunBesideFluidVisits(t *testing.T) {
	for _, tc := range []struct {
		name, block string
		before      map[string]string
		act         func(*Physics)
		ticks       int
		prop, want  string
	}{
		{"dripleaf", "minecraft:big_dripleaf",
			map[string]string{"facing": "west", "waterlogged": "true"},
			func(p *Physics) { p.DripleafProjectiled(8, GroundLevel, 8) },
			100, "tilt", "none"},
		{"detector rail", "minecraft:detector_rail",
			map[string]string{"shape": "north_south", "waterlogged": "true"},
			func(p *Physics) {
				p.SetRailCartSource(railCartsStub{crossing: true})
				var changes []BlockChange
				var effects []Effect
				p.EntityInside(8, GroundLevel, 8, &changes, &effects)
				p.SetRailCartSource(railCartsStub{})
			},
			20, "powered", "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld(t, NewFlatGenerator())
			w.Acquire(ChunkPos{})
			p := NewPhysics(w)
			w.SetBlock(8, GroundLevel, 8, registry.MustState(tc.block, tc.before))
			tc.act(p)
			for tick := int64(1); tick <= int64(tc.ticks); tick++ {
				p.Tick(tick)
			}
			if got := registry.StateProps(w.BlockAt(8, GroundLevel, 8))[tc.prop]; got != tc.want {
				t.Fatalf("%s = %s, want %s", tc.prop, got, tc.want)
			}
		})
	}
}
