package entity

import (
	"fmt"
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

func TestPushGridGatherBoxMatchesScan(t *testing.T) {
	const herd = 160
	rng := rand.New(rand.NewSource(632))
	mobs := make([]*Mob, herd)
	for i := range mobs {
		mobs[i] = NewMob(TypeCow, int32(i), rng.Float64()*40-20, 64+float64(i%3), rng.Float64()*40-20, int64(i))
	}
	g := takePushGrid(herd, nil)
	defer g.release()
	g.build(mobs)
	for range 200 {
		x, y, z := rng.Float64()*50-25, 63+rng.Float64()*3, rng.Float64()*50-25
		box := AABB{x, y, z, x + rng.Float64()*6, y + rng.Float64()*3, z + rng.Float64()*6}
		var want []int32
		for _, m := range mobs {
			if box.Crosses(m.mobBox()) {
				want = append(want, m.ID)
			}
		}
		var got []int32
		for _, m := range g.gatherBox(box) {
			got = append(got, m.ID)
		}
		slices.Sort(want)
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Fatalf("box %v: got %v, want %v", box, got, want)
		}
	}
}

func TestPushGridMatchesHerdAtCellEdges(t *testing.T) {
	const inflate, epsilon = .25, 1e-6
	for _, edge := range []float64{-pushCellSide, pushCellSide} {
		for _, offset := range []float64{-epsilon, 0, epsilon} {
			t.Run(fmt.Sprintf("%g/%g", edge, offset), func(t *testing.T) {
				query := NewMob(TypeBat, 0, 0, 64, 0, 0)
				reach := query.width() + inflate
				query.X, query.Z = edge-reach+offset, edge+reach+offset
				herd := []*Mob{query}
				for _, dx := range []float64{epsilon, 0, -epsilon} {
					for _, dz := range []float64{epsilon, 0, -epsilon} {
						id := int32(len(herd))
						herd = append(herd, NewMob(TypeBat, id, edge+dx, 64, edge+dz, int64(id)))
					}
				}
				grid := takePushGrid(len(herd), nil)
				defer grid.release()
				grid.build(herd)
				assertPushCandidates(t, grid, query, inflate, herd)
			})
		}
	}
}

func TestPushGridMatchesHerdWithOversizedTarget(t *testing.T) {
	for _, update := range []string{"add", "refresh", "moved"} {
		t.Run(update, func(t *testing.T) {
			query := NewMob(TypeBat, 0, -.25, 64, .25, 0)
			slime := NewMob(TypeSlime, 1, 8.25, 64, .25, 1)
			other := NewMob(TypeBat, 2, .25, 64, .25, 2)
			herd := []*Mob{query, slime, other}
			if update == "add" {
				slime.SetSize(40, true)
			}
			if update == "moved" {
				slime.X = .25
			}
			grid := takePushGrid(len(herd), nil)
			defer grid.release()
			grid.build(herd)
			switch update {
			case "refresh":
				slime.SetSize(40, true)
				grid.refresh(slime)
			case "moved":
				slime.SetSize(40, true)
				slime.X = 8.25
				grid.moved(slime)
			}
			assertPushCandidates(t, grid, query, .25, herd)
		})
	}
}

func TestPushGridScansOnlyReachableCells(t *testing.T) {
	herd := make([]*Mob, 0, 9)
	for x := range 3 {
		for z := range 3 {
			id := int32(len(herd))
			herd = append(herd, NewMob(TypeBat, id, (float64(x)+.5)*pushCellSide, 64, (float64(z)+.5)*pushCellSide, int64(id)))
		}
	}
	var scanned int
	grid := takePushGrid(len(herd), &scanned)
	defer grid.release()
	grid.build(herd)
	assertPushCandidates(t, grid, herd[4], .25, herd)
	if scanned != 1 {
		t.Errorf("scanned %d entries, want 1", scanned)
	}
}

func assertPushCandidates(t *testing.T, grid *pushGrid, query *Mob, inflate float64, herd []*Mob) {
	t.Helper()
	box := func(m *Mob, inflate float64) AABB {
		hw := m.width()/2 + inflate
		return NewAABB(m.X-hw, m.Y-inflate, m.Z-hw, m.X+hw, m.Y+(m.height()+inflate), m.Z+hw)
	}
	queryBox := box(query, inflate)
	var want []int32
	for _, m := range herd {
		if queryBox.Crosses(box(m, 0)) {
			want = append(want, m.ID)
		}
	}
	var got []int32
	for _, m := range grid.candidates(query, inflate) {
		got = append(got, m.ID)
	}
	if !slices.Equal(got, want) {
		t.Errorf("candidate IDs %v, want %v in herd order", got, want)
	}
}

func TestPushGridLazyByIDAndCaravan(t *testing.T) {
	cow := NewMob(TypeCow, 10, 0, 64, 0, 1)
	cow.LeashHolderID = 99
	dupCow := NewMob(TypeCow, 10, 4, 64, 0, 2)
	idleLlama := NewMob(TypeLlama, 20, 8, 64, 0, 3)
	leashedLlama := NewMob(TypeLlama, 21, 12, 64, 0, 4)
	leashedLlama.LeashHolderID = 99
	caravanLlama := NewMob(TypeLlama, 22, 16, 64, 0, 5)
	caravanLlama.joinCaravan(leashedLlama)
	herd := []*Mob{cow, dupCow, idleLlama, leashedLlama, caravanLlama}

	g := takePushGrid(len(herd), nil)
	defer g.release()
	g.build(herd)

	if g.byIDReady || len(g.byID) != 0 {
		t.Fatalf("byID populated eagerly: ready=%v len=%d", g.byIDReady, len(g.byID))
	}
	if !g.present[TypeCow] || !g.present[TypeLlama] || g.present[TypeZombie] {
		t.Fatalf("present flags wrong: cow=%v llama=%v zombie=%v", g.present[TypeCow], g.present[TypeLlama], g.present[TypeZombie])
	}
	if !slices.Equal(g.caravan, []*Mob{leashedLlama, caravanLlama}) {
		t.Fatalf("caravan = %v, want [%v %v]", g.caravan, leashedLlama, caravanLlama)
	}

	view := &tickView{others: herd, push: g}
	if got := view.mobByID(10); got != cow {
		t.Fatalf("mobByID(10) = %v, want first-filed cow %v", got, cow)
	}
	if !g.byIDReady {
		t.Fatal("byIDReady not set after first lookup")
	}
	if got := view.mobByID(22); got != caravanLlama {
		t.Fatalf("mobByID(22) = %v, want %v", got, caravanLlama)
	}
	if got := view.mobByID(999); got != nil {
		t.Fatalf("mobByID(999) = %v, want nil", got)
	}

	g.reset()
	if g.byIDReady || len(g.byID) != 0 || g.mobs != nil {
		t.Fatalf("reset did not clear lazy byID state: ready=%v len=%d mobs=%v", g.byIDReady, len(g.byID), g.mobs)
	}

	next := []*Mob{idleLlama}
	g.build(next)
	if g.byIDReady {
		t.Fatal("second build populated byID eagerly")
	}
	if got := g.lookupByID(10); got != nil {
		t.Fatalf("stale ID 10 found after reset: %v", got)
	}
	if got := g.lookupByID(20); got != idleLlama {
		t.Fatalf("lookupByID(20) = %v, want %v", got, idleLlama)
	}
}
