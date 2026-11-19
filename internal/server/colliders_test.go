package server

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
)

func TestMobCollisionWorldExcludesRideChain(t *testing.T) {
	t.Parallel()
	vehicle := entity.NewMob(entity.TypeShulker, 1, 0, 64, 0, 1)
	rider := entity.NewMob(entity.TypeShulker, 2, 2, 64, 0, 2)
	sibling := entity.NewMob(entity.TypeShulker, 3, 4, 64, 0, 3)
	other := entity.NewMob(entity.TypeShulker, 4, 6, 64, 0, 4)
	rider.Vehicle, sibling.Vehicle = vehicle, vehicle
	vehicle.Passengers = []*entity.Mob{rider, sibling}
	var w mobCollisionWorld
	w.reset(nil, nil, entity.Mobs{vehicle, rider, sibling, other})
	query := entity.NewAABB(-10, 60, -10, 10, 70, 10)
	if got := len(w.BodyBoxes(query)); got != 4 {
		t.Fatalf("unfiltered snapshot has %d boxes, want 4", got)
	}
	for _, m := range []*entity.Mob{vehicle, rider, sibling} {
		boxes := w.ForMob(m).(entity.BodyWorld).BodyBoxes(query)
		if len(boxes) != 1 || boxes[0] != entity.NewAABB(other.BlastBox()) {
			t.Errorf("mob %d collides with %v, want only the unrelated shell", m.ID, boxes)
		}
	}
	if got := len(w.ForMob(other).(entity.BodyWorld).BodyBoxes(query)); got != 3 {
		t.Errorf("unrelated shulker collides with %d boxes, want the other three", got)
	}
}

func TestMobCollisionWorldCurrentRideLinks(t *testing.T) {
	t.Parallel()
	root := entity.NewMob(entity.TypeCow, 1, 0, 64, 0, 1)
	shell := entity.NewMob(entity.TypeShulker, 2, 0, 64, 0, 2)
	nested := entity.NewMob(entity.TypeShulker, 3, 0, 64, 0, 3)
	mover := entity.NewMob(entity.TypeZombie, 4, 0, 64, 0, 4)
	distant := entity.NewMob(entity.TypeShulker, 5, 200, 64, 200, 5)
	var w mobCollisionWorld
	w.reset(nil, nil, entity.Mobs{root, shell, nested, distant})
	query := entity.NewAABB(-2, 63, -2, 2, 67, 2)
	check := func(want int) {
		t.Helper()
		view := w.ForMob(mover).(entity.Bodies)
		if boxes := view.BodyBoxes(query); len(boxes) != want {
			t.Fatalf("current ride links yield %d boxes, want %d", len(boxes), want)
		}
		if len(w.riders) != 0 || slices.ContainsFunc(w.riders[:cap(w.riders)], func(m *entity.Mob) bool { return m != nil }) {
			t.Fatal("traversal retains riders after ForMob")
		}
	}
	// An ordinary mover absent from the solid-body lookup excludes nothing.
	check(2)
	if len(w.skip) != 0 {
		t.Fatal("ordinary unmounted mover has exclusions")
	}
	// Mount after snapshot construction, through a noncollidable root and a
	// nested solid rider; siblings belong to the same ride as the mover.
	root.Passengers = []*entity.Mob{shell, mover}
	shell.Vehicle, mover.Vehicle = root, root
	shell.Passengers, nested.Vehicle = []*entity.Mob{nested}, shell
	check(0)
	if len(w.skip) != 2 {
		t.Fatalf("ride excludes %d boxes, want 2", len(w.skip))
	}
	root.Passengers, mover.Vehicle = []*entity.Mob{shell}, nil
	check(2)
	shell.Passengers, nested.Vehicle = nil, nil
	root.Passengers, shell.Vehicle = []*entity.Mob{mover}, nil
	mover.Vehicle = root
	check(2)
}

func TestSolidCollisionBoatOnlyAndReuse(t *testing.T) {
	t.Parallel()
	near := entity.NewAABB(-4, 64, -4, 0, 65, 0)
	far := entity.NewAABB(100, 64, 100, 104, 65, 104)
	hulls := make([]entity.AABB, 2, 8)
	hulls[0], hulls[1] = near, far
	var w mobCollisionWorld
	view := w.reset(nil, hulls, nil).(*mobCollisionWorld)
	if view.Grid == nil || view.Grid.Cell != 4 {
		t.Fatal("boat-only snapshot has no 4-block index")
	}
	query := entity.NewAABB(-5, 63, -5, 1, 66, 1)
	if got := view.BodyBoxes(query); !slices.Equal(got, []entity.AABB{near}) {
		t.Fatalf("boat query = %v, want only near hull", got)
	}
	touching := entity.NewAABB(0, 64, -4, 4, 65, 0)
	if got := view.BodyBoxes(touching); len(got) != 0 {
		t.Fatalf("touching hull counted as crossing: %v", got)
	}
	// Copy hull inputs: another snapshot must not append mobs into their storage.
	shell := entity.NewMob(entity.TypeShulker, 1, -2, 64, -2, 1)
	w.reset(nil, hulls[:1], entity.Mobs{shell})
	if hulls[1] != far {
		t.Fatal("snapshot overwrote hull input storage")
	}
	shell.X = 1000
	if w.Boxes[1] == entity.NewAABB(shell.BlastBox()) {
		t.Fatal("snapshot geometry changed with the mob")
	}
	w.ForMob(shell)
	w.reset(nil, []entity.AABB{far}, nil)
	if len(w.mobs) != 0 || len(w.BodyBoxes(query)) != 0 {
		t.Fatal("reset retained stale mob or grid entries")
	}
	if got := w.reset(nil, nil, nil); got != nil && got != w.World {
		t.Fatal("empty snapshot did not return the underlying world")
	}
}

func TestSolidCollisionIndexedMatchesLinear(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(6542, 1))
	var snapshot mobCollisionWorld
	for pass := range 4 {
		herd := make(entity.Mobs, 80)
		for i := range herd {
			kind := entity.TypeShulker
			if i%3 == 0 {
				kind = entity.TypeCow
			}
			herd[i] = entity.NewMob(kind, int32(i+1), rng.Float64()*32-16, 64+rng.Float64()*8, rng.Float64()*32-16, int64(i))
		}
		hulls := []entity.AABB{
			entity.NewAABB(-8, 64, -8, 8, 65, 8),
			entity.NewAABB(-4, 66, -4, 4, 67, 4),
			entity.NewAABB(1000, 64, 1000, 1002, 65, 1002),
		}
		snapshot.reset(nil, hulls, herd)
		// Capture the old linear snapshot independently, including hull-first order.
		boxes := slices.Clone(hulls)
		var owners []*entity.Mob
		for _, m := range herd {
			if m.CollidesWithAny() {
				boxes = append(boxes, entity.NewAABB(m.BlastBox()))
				owners = append(owners, m)
			}
		}
		// Build a current forest after the geometry snapshot, including nested
		// riders, siblings and both collidable and noncollidable roots.
		for i := 1; i < len(herd); i++ {
			if i%4 == 0 {
				continue
			}
			parent := herd[rng.IntN(i)]
			herd[i].Vehicle = parent
			parent.Passengers = append(parent.Passengers, herd[i])
		}
		for _, m := range herd {
			m.X += 0.25
		}
		movers := append(slices.Clone(herd), entity.NewMob(entity.TypeZombie, 100, 0, 64, 0, 1))
		queries := []entity.AABB{
			entity.NewAABB(-32, 60, -32, 32, 80, 32),
			entity.NewAABB(-4, 64, -4, 4, 68, 4),
			entity.NewAABB(100, 64, 100, 104, 68, 104),
			entity.NewAABB(-8, 65, -8, 8, 66, 8),
		}
		for range 20 {
			x, y, z := rng.Float64()*40-20, rng.Float64()*12+62, rng.Float64()*40-20
			queries = append(queries, entity.NewAABB(x, y, z, x+rng.Float64()*10, y+2, z+rng.Float64()*10))
		}
		for _, mover := range movers {
			view := snapshot.ForMob(mover).(entity.BodyWorld)
			for _, query := range queries {
				var want []entity.AABB
				for i, box := range boxes {
					if i >= len(hulls) && owners[i-len(hulls)].RootVehicle() == mover.RootVehicle() {
						continue
					}
					if query.Crosses(box) {
						want = append(want, box)
					}
				}
				if got := view.BodyBoxes(query); !slices.Equal(got, want) {
					t.Fatalf("pass %d mover %d query %v: indexed %v, linear %v", pass, mover.ID, query, got, want)
				}
			}
		}
	}
}

func TestSolidCollisionPassOwnership(t *testing.T) {
	t.Parallel()
	// Different dimensions and passes keep independent mutable query scratch.
	dims := [2]dimension{}
	for i := range dims {
		for pass := range collisionPassCount {
			t.Run(string(rune('a'+int(pass)+i*int(collisionPassCount))), func(t *testing.T) {
				t.Parallel()
				var herd entity.Mobs
				for j := range 20 {
					herd = append(herd, entity.NewMob(entity.TypeShulker, int32(j+1), float64(j*4), 64, 0, int64(j)))
				}
				snapshot := &dims[i].colliders[pass]
				query := entity.NewAABB(-10, 60, -10, 100, 70, 10)
				for range 10 {
					snapshot.reset(nil, nil, herd)
					for _, m := range herd {
						if got := snapshot.ForMob(m).(entity.BodyWorld).BodyBoxes(query); len(got) != len(herd)-1 {
							t.Fatalf("got %d boxes, want %d", len(got), len(herd)-1)
						}
					}
				}
			})
		}
	}
}
