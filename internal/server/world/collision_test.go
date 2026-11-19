package world

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestCollisionBoxes is the entity sweep's per-state read: the modelled
// families' own boxes, the generated table's height for everything else,
// and nothing where the table carries no shape at all.
func TestCollisionBoxes(t *testing.T) {
	t.Parallel()
	post := CollisionBox{MinX: 6.0 / 16, MinZ: 6.0 / 16, MaxX: 10.0 / 16, MaxY: 1.5, MaxZ: 10.0 / 16}
	for _, tc := range []struct {
		name  string
		props map[string]string
		want  []CollisionBox
	}{
		{"minecraft:stone", nil, []CollisionBox{{MaxX: 1, MaxY: 1, MaxZ: 1}}},
		{"minecraft:farmland", nil, []CollisionBox{{MaxX: 1, MaxY: 15.0 / 16, MaxZ: 1}}},
		{"minecraft:white_carpet", nil, []CollisionBox{{MaxX: 1, MaxY: 1.0 / 16, MaxZ: 1}}},
		{"minecraft:oak_fence", nil, []CollisionBox{post}},
		{"minecraft:oak_slab", map[string]string{"type": "bottom"},
			[]CollisionBox{{MaxX: 1, MaxY: 0.5, MaxZ: 1}}},
		{"minecraft:oak_slab", map[string]string{"type": "top"},
			[]CollisionBox{{MinY: 0.5, MaxX: 1, MaxY: 1, MaxZ: 1}}},
		{"minecraft:chest", nil,
			[]CollisionBox{{MinX: 1.0 / 16, MinZ: 1.0 / 16, MaxX: 15.0 / 16, MaxY: 14.0 / 16, MaxZ: 15.0 / 16}}},
		{"minecraft:lily_pad", nil, []CollisionBox{{MaxX: 1, MaxY: 1.5 / 16, MaxZ: 1}}},
		// An end portal frame is a 13/16 base; the eye adds the 8x8 middle
		// column that reaches the full 16/16.
		{"minecraft:end_portal_frame", map[string]string{"eye": "false", "facing": "north"},
			[]CollisionBox{{MaxX: 1, MaxY: 13.0 / 16, MaxZ: 1}}},
		{"minecraft:end_portal_frame", map[string]string{"eye": "true", "facing": "north"},
			[]CollisionBox{{MaxX: 1, MaxY: 13.0 / 16, MaxZ: 1},
				{MinX: 4.0 / 16, MinY: 13.0 / 16, MinZ: 4.0 / 16,
					MaxX: 12.0 / 16, MaxY: 1, MaxZ: 12.0 / 16}}},
		// Frogspawn models a 1.5-sixteenth plate for the flow, but the
		// generated table carries no collision for it, so the entity sweep
		// must read nothing.
		{"minecraft:frogspawn", nil, nil},
		{"minecraft:oak_fence_gate", map[string]string{"open": "true"}, nil},
		{"minecraft:torch", nil, nil},
	} {
		state := mustFind(t, tc.name, tc.props)
		got := CollisionBoxes(state)
		if len(got) != len(tc.want) {
			t.Errorf("CollisionBoxes(%s %v) = %v, want %v", tc.name, tc.props, got, tc.want)
			continue
		}
		wantTop := 0.0
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("box %d of CollisionBoxes(%s %v) = %v, want %v",
					i, tc.name, tc.props, got[i], tc.want[i])
			}
			wantTop = max(wantTop, tc.want[i].MaxY)
		}
		if top := CollisionTop(state); top != wantTop {
			t.Errorf("CollisionTop(%s %v) = %v, want the boxes' top %v", tc.name, tc.props, top, wantTop)
		}
	}
}

// TestCollisionFull checks a sample of states, picked from the vanilla
// dump, whose collision fullness doesn't match their look, both ways.
func TestCollisionFull(t *testing.T) {
	state := func(s string) uint16 {
		t.Helper()
		name, props := s, map[string]string{}
		if i := strings.IndexByte(s, '['); i >= 0 {
			name = s[:i]
			props = registry.ParseStateProps(s[i+1 : len(s)-1])
		}
		id, ok := registry.FindState("minecraft:"+name, props)
		if !ok {
			t.Fatalf("no state %s", s)
		}
		return id
	}
	for _, tc := range []struct {
		s    string
		want bool
	}{
		{"stone", true},
		{"glass", true},
		{"beacon", true},
		{"spawner", true},
		{"loom", true},
		{"brain_coral_block", true},
		{"copper_grate", true},
		{"chiseled_bookshelf", true},
		{"frosted_ice", true},
		{"chorus_flower", true},
		{"mangrove_roots", true},
		{"sculk_catalyst", true},
		{"vault", true},
		{"trial_spawner", true},
		{"test_instance_block", true},
		{"oak_slab[type=double]", true},
		{"piston[facing=up]", true},
		{"piston[facing=up,extended=true]", false},
		{"mud", false},
		{"soul_sand", false},
		{"moss_carpet", false},
		{"oak_stairs", false},
		{"oak_slab", false},
		{"oak_door[half=lower]", false},
		{"snow[layers=1]", false},
	} {
		if got := CollisionFull(state(tc.s)); got != tc.want {
			t.Errorf("CollisionFull(%s) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

// sweepBoxes is the sweep's read of boxes drawn in sixteenths, the form the
// vanilla block classes write theirs in.
func sweepBoxes(boxes ...[6]float64) []CollisionBox {
	out := make([]CollisionBox, len(boxes))
	for i, b := range boxes {
		out[i] = CollisionBox{b[0] / 16, b[1] / 16, b[2] / 16, b[3] / 16, b[4] / 16, b[5] / 16}
	}
	return out
}

// topOf is the highest top among a state's boxes.
func topOf(boxes []CollisionBox) float64 {
	var top float64
	for _, b := range boxes {
		top = max(top, b.MaxY)
	}
	return top
}

// TestCollidesAnew is the moved-wrongly read: only a shape the new box
// crosses and the old one did not counts, a face touched is not crossed, and
// scaffolding is never asked.
func TestCollidesAnew(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.Acquire(ChunkPos{X: 0, Z: 0})
	w.SetBlock(2, 100, 0, mustFind(t, "minecraft:stone", nil))
	w.SetBlock(0, 100, 0, mustFind(t, "minecraft:stone", nil))
	w.SetBlock(4, 100, 0, mustFind(t, "minecraft:scaffolding", map[string]string{"distance": "0", "bottom": "false", "waterlogged": "false"}))
	w.SetBlock(6, 100, 0, mustFind(t, "minecraft:cactus", map[string]string{"age": "0"}))
	w.SetBlock(8, 100, 0, mustFind(t, "minecraft:honey_block", nil))
	body := func(x, y, z float64) [6]float64 {
		return [6]float64{x - 0.3, y, z - 0.3, x + 0.3, y + 1.8, z + 0.3}
	}
	for _, tc := range []struct {
		name     string
		from, to [6]float64
		want     bool
	}{
		{"into a block", body(1.5, 100, 0.5), body(2.5, 100, 0.5), true},
		{"up against its face", body(1.5, 100, 0.5), body(1.7, 100, 0.5), false},
		{"onto its top", body(2.5, 102, 0.5), body(2.5, 101, 0.5), false},
		{"within one it was already in", body(0.5, 100, 0.5), body(0.6, 100, 0.5), false},
		// Shapes the table only rounds up to a whole cell are never refused.
		{"up against a cactus", body(5.5, 100, 0.5), body(6.0625-0.3, 100, 0.5), false},
		{"onto honey", body(7.5, 100, 0.5), body(8.5, 100+15.0/16, 0.5), false},
		{"into scaffolding", body(4.5, 101, 0.5), body(4.5, 100.5, 0.5), false},
	} {
		if got := w.CollidesAnew(tc.from, tc.to); got != tc.want {
			t.Errorf("%s: CollidesAnew = %v, want %v", tc.name, got, tc.want)
		}
	}
}
