package entity

import (
	"math/rand"
	"slices"
	"testing"
)

func TestPushGridDenseMatchesSparse(t *testing.T) {
	const herd = 160
	rng := rand.New(rand.NewSource(6536))
	dense := takePushGrid(herd, nil)
	defer dense.release()
	sparse := &pushGrid{cells: make(map[pushCellKey][]pushEntry), byID: make(map[int32]*Mob)}
	defer sparse.release()
	a, b := make([]*Mob, herd), make([]*Mob, herd)
	for i := range a {
		x, z := rng.Float64()*16-8, rng.Float64()*16-8
		y := 64 + float64(i%3)
		a[i], b[i] = NewMob(TypeCow, int32(i), x, y, z, int64(i)), NewMob(TypeCow, int32(i), x, y, z, int64(i))
		if i%17 == 0 {
			a[i], b[i] = NewMob(TypeSlime, int32(i), x, y, z, int64(i)), NewMob(TypeSlime, int32(i), x, y, z, int64(i))
			a[i].SetSize(16, true)
			b[i].SetSize(16, true)
		}
		sparse.add(b[i], i)
	}
	dense.build(a)
	if len(dense.dense) == 0 || len(dense.cells) != 0 {
		t.Fatal("compact herd did not use only dense cells")
	}
	check := func() {
		t.Helper()
		for i := range a {
			if a[i].pushSlot != b[i].pushSlot || a[i].pushCell != b[i].pushCell {
				t.Fatalf("mob %d filing differs", i)
			}
			ac, bc := dense.cell(a[i].pushCell), sparse.cell(b[i].pushCell)
			if len(ac) != len(bc) {
				t.Fatalf("mob %d cell lengths %d != %d", i, len(ac), len(bc))
			}
			for j := range ac {
				if ac[j].m.ID != bc[j].m.ID {
					t.Fatalf("mob %d cell slot %d order differs", i, j)
				}
			}
			for _, inflate := range []float64{0, .2, 8} {
				var an, bn int
				dense.gathered, sparse.gathered = &an, &bn
				ac, bc := dense.candidates(a[i], inflate), sparse.candidates(b[i], inflate)
				if len(ac) != len(bc) || an != bn {
					t.Fatalf("query %d inflate %g: lengths %d/%d, scanned %d/%d", i, inflate, len(ac), len(bc), an, bn)
				}
				for j := range ac {
					if ac[j].ID != bc[j].ID {
						t.Fatalf("query %d slot %d order differs", i, j)
					}
				}
			}
		}
	}
	check()
	// Equal indices retain the old map traversal's tie-breaking order too.
	for i := range a {
		a[i].pushIdx, b[i].pushIdx = i%3, i%3
	}
	check()
	for step := range 40 {
		i := rng.Intn(herd)
		// Exercise a full destination, empty interior cells, and the sparse overflow.
		x, z := a[0].X, a[0].Z
		if step%3 == 0 {
			x, z = 100, -100
		}
		if step%3 == 1 {
			x, z = rng.Float64()*16-8, rng.Float64()*16-8
		}
		a[i].X, a[i].Z, b[i].X, b[i].Z = x, z, x, z
		dense.moved(a[i])
		sparse.moved(b[i])
		a[i].Y += .25
		b[i].Y += .25
		dense.refresh(a[i])
		sparse.refresh(b[i])
		check()
	}
	// A mob added after the snapshot must not overwrite a filed entry.
	extra := NewMob(TypeCow, 999, 0, 64, 0, 1)
	before := slices.Clone(dense.cell(extra.pushCell))
	dense.refresh(extra)
	if !slices.Equal(before, dense.cell(extra.pushCell)) {
		t.Fatal("unfiled mob changed a cell")
	}
}

func TestPushGridStorageSelectionAndReuse(t *testing.T) {
	g := takePushGrid(2, nil)
	defer g.release()
	// Keep this same grid, including its storage, across differently shaped ticks.
	shapes := []struct {
		x, z  float64
		dense bool
	}{
		{4, 12, true}, // 2×4 = four heads per mob, exactly the limit.
		{4, 16, false},
		{29999999, -29999999, false},
		{0, 0, true},
	}
	for _, shape := range shapes {
		mobs := []*Mob{NewMob(TypeCow, 1, 0, 64, 0, 1), NewMob(TypeCow, 1, shape.x, 64, shape.z, 2)}
		g.build(mobs)
		if (len(g.dense) > 0) != shape.dense {
			t.Fatalf("(%g,%g): dense=%v, want %v", shape.x, shape.z, len(g.dense) > 0, shape.dense)
		}
		if g.byID[1] != mobs[0] {
			t.Fatal("duplicate ID did not keep the first mob")
		}
		if len(g.candidates(mobs[1], 0)) == 0 {
			t.Fatal("query did not find itself")
		}
		g.reset()
		for _, c := range g.dense[:cap(g.dense)] {
			for _, e := range c.spare[:cap(c.spare)] {
				if e.m != nil {
					t.Fatal("released overflow retains a mob")
				}
			}
		}
		for _, e := range g.entries[:cap(g.entries)] {
			if e.m != nil {
				t.Fatal("released entries retain a mob")
			}
		}
	}
	g.build(nil)
	if len(g.candidates(NewMob(TypeCow, 1, 0, 64, 0, 1), 0)) != 0 {
		t.Fatal("empty tick retained candidates")
	}
}

func TestDensePushGridTicksMatchWholeHerdScan(t *testing.T) {
	w := newTestWorld()
	floor(w, -12, -12, 12, 12, 63, defaultState(t, "minecraft:stone"))
	makeHerd := func() Mobs {
		var mobs Mobs
		for i := range 64 {
			x, z := -4.2+float64(i%8)*.15, -4.2+float64(i/8)*.15
			mobs = append(mobs, aimless(NewMob(TypeCow, int32(i), x, 64, z, int64(i+1))))
		}
		return mobs
	}
	collection, scan := makeHerd(), makeHerd()
	original := slices.Clone(collection)
	reference := slices.Clone(scan)
	for range 8 {
		collection.Tick(w, nil, nil)
		snapshot := slices.Clone(scan)
		for _, m := range scan {
			m.tick(w, nil, snapshot)
		}
		scan = slices.DeleteFunc(scan, func(m *Mob) bool { return m.Dead || m.gone })
		if len(collection) != len(scan) {
			t.Fatal("herd survival differs")
		}
		for i := range collection {
			if collection[i].ID != scan[i].ID {
				t.Fatal("surviving herd order differs")
			}
		}
		for i, got := range original {
			want := reference[i]
			if got.X != want.X || got.Y != want.Y || got.Z != want.Z ||
				got.VX != want.VX || got.VY != want.VY || got.VZ != want.VZ ||
				got.Health != want.Health || got.Dead != want.Dead {
				t.Fatalf("mob %d movement or cramming differs from whole-herd scan", i)
			}
		}
	}
	for i, got := range original {
		if got.Rand.Int63() != reference[i].Rand.Int63() {
			t.Fatalf("mob %d random stream differs", i)
		}
	}
}
