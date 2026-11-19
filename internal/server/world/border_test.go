package world

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestBorderLerpReachesItsTarget(t *testing.T) {
	t.Parallel()
	b := NewBorder()
	b.LerpSize(100, 200, 10)
	for range 5 {
		b.Tick()
	}
	if got := b.Size(); got != 150 {
		t.Errorf("size halfway = %v, want 150", got)
	}
	if got := b.State().LerpTicks; got != 5 {
		t.Errorf("lerp ticks left = %d, want 5", got)
	}
	for range 5 {
		b.Tick()
	}
	if st := b.State(); st.Size != 200 || st.LerpTicks != 0 {
		t.Errorf("state after the lerp = %+v, want size 200 and no ticks left", st)
	}
}

func TestBorderBoundsAreHalfOpen(t *testing.T) {
	t.Parallel()
	b := NewBorder()
	b.SetSize(10)
	for _, c := range []struct {
		x    float64
		want bool
	}{{-5, true}, {4.99, true}, {5, false}, {-5.01, false}} {
		if got := b.Contains(c.x, 0); got != c.want {
			t.Errorf("Contains(%v, 0) = %v, want %v", c.x, got, c.want)
		}
	}
	b.SetCenter(100, 0)
	if !b.ContainsBlock(100, 0) || b.ContainsBlock(0, 0) {
		t.Error("the bounds did not follow the centre")
	}
}

func TestBorderOutsideDamage(t *testing.T) {
	t.Parallel()
	b := NewBorder()
	b.SetSize(10)
	box := func(x float64) float64 { return b.OutsideDamage(x-0.3, -0.3, x+0.3, 0.3, x, 0) }
	for _, c := range []struct {
		x    float64
		want float64
	}{{4, 0}, {5.5, 0}, {9.9, 0}, {12.5, 1}, {40, 6}} {
		if got := box(c.x); got != c.want {
			t.Errorf("damage at x=%v = %v, want %v", c.x, got, c.want)
		}
	}
	b.SetDamagePerBlock(0)
	if got := box(40); got != 0 {
		t.Errorf("damage with no damage per block = %v, want 0", got)
	}
}

func borderData(cx float64) nbt.Compound {
	return nbt.Compound{
		"center_x": nbt.Double(cx), "center_z": nbt.Double(3), "size": nbt.Double(64),
		"lerp_time": nbt.Long(0), "lerp_target": nbt.Double(0), "safe_zone": nbt.Double(5),
		"damage_per_block": nbt.Double(0.2), "warning_blocks": nbt.Int(2), "warning_time": nbt.Int(9),
	}
}

// TestBorderIsReadPerDimension checks that the nether reads its own
// world_border.dat, that a saved lerp resumes and that a centre out of range
// reads as the default.
func TestBorderIsReadPerDimension(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	nether := filepath.Join(dir, "dimensions", "minecraft", "the_nether")
	if err := WriteSavedData(nether, borderFile, borderData(-8)); err != nil {
		t.Fatal(err)
	}
	want := BorderState{CenterX: -8, CenterZ: 3, Size: 64, Target: 64, WarningBlocks: 2, WarningTime: 9, AbsoluteMax: BorderAbsoluteMax}
	if got := NewDimensionWorld(dir, true, NewNetherGenerator(1)).LoadBorder().State(); got != want {
		t.Errorf("nether border = %+v, want %+v", got, want)
	}
	if got, want := NewDimensionWorld(dir, false, NewEndGenerator(1)).LoadBorder().State(), NewBorder().State(); got != want {
		t.Errorf("end border = %+v, want the default %+v", got, want)
	}

	moving := borderData(0)
	moving["lerp_time"], moving["lerp_target"] = nbt.Long(40), nbt.Double(10)
	if err := WriteSavedData(nether, borderFile, moving); err != nil {
		t.Fatal(err)
	}
	if st := NewDimensionWorld(dir, true, NewNetherGenerator(1)).LoadBorder().State(); st.Size != 64 || st.Target != 10 || st.LerpTicks != 40 {
		t.Errorf("lerping border = %+v, want size 64 heading for 10 over 40 ticks", st)
	}

	if err := WriteSavedData(nether, borderFile, borderData(3e7)); err != nil {
		t.Fatal(err)
	}
	if got, want := NewDimensionWorld(dir, true, NewNetherGenerator(1)).LoadBorder().State(), NewBorder().State(); got != want {
		t.Errorf("out-of-range border = %+v, want the default", got)
	}
}

func TestBorderBoxBounds(t *testing.T) {
	t.Parallel()
	b := NewBorder()
	b.SetCenter(0.25, 0.25)
	b.SetSize(10)
	w := &World{}
	w.SetBorder(b)
	for _, c := range []struct {
		name                   string
		minX, minZ, maxX, maxZ float64
		want                   bool
	}{
		{"inside", -4.75, -4.75, 5.25, 5.25, true},
		{"cross min x", -4.76, 0, 1, 1, false},
		{"cross max x", 0, 0, 5.26, 1, false},
		{"cross min z", 0, -4.76, 1, 1, false},
		{"cross max z", 0, 0, 1, 5.26, false},
		{"epsilon", 0, 0, 5.250001, 1, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := w.InBorderBox(c.minX, c.minZ, c.maxX, c.maxZ); got != c.want {
				t.Fatalf("inside = %v, want %v", got, c.want)
			}
		})
	}
}

func TestSpawnChunkBorderMatchesCorners(t *testing.T) {
	b := NewBorder()
	w := newTestWorld(t, NewFlatGenerator())
	if !w.ChunkInBorder(ChunkPos{}) {
		t.Fatal("nil border")
	}
	w.SetBorder(b)
	check := func() {
		t.Helper()
		for x := int32(-10); x <= 10; x++ {
			for z := int32(-10); z <= 10; z++ {
				pos := ChunkPos{X: x, Z: z}
				want := w.InBorder(x*16, z*16) && w.InBorder(x*16+15, z*16+15)
				if w.ChunkInBorder(pos) != want || b.ContainsChunk(pos) != want {
					t.Fatalf("border corners differ at %v", pos)
				}
			}
		}
	}
	check()
	b.SetSize(32)
	check()
	b.SetCenter(16, -16)
	check()
	b.SetSize(31.999999)
	check()
	b.LerpSize(32, 128, 10)
	for range 10 {
		check()
		b.Tick()
	}
	check()
}
