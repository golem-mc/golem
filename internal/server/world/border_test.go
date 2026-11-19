package world

import (
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
