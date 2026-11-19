package world

import "testing"

// A honey block moving east carries a body on its top face along, and not one
// whose centre is off the block.
func TestHoneyRideCarriesABodyOnTop(t *testing.T) {
	pp := &PistonPush{
		X: 3, Y: 64, Z: 0, Dir: dirEast, Extending: true, Sticky: true, Top: 1,
		Boxes: []CollisionBox{{0, 0, 0, 1, 1, 1}},
	}
	on := [6]float64{2.7, 65, 0.2, 3.3, 66.8, 0.8}
	if dx, dz, ok := pp.Carries(on, 3, 0.5); !ok || dx != 0.5 || dz != 0 {
		t.Errorf("Carries on top = %v, %v, %v, want 0.5, 0, true", dx, dz, ok)
	}
	if _, _, ok := pp.Carries(on, 3, 1.5); ok {
		t.Error("a body whose centre is past the block's edge was carried")
	}
	pp.Dir = dirUp
	if _, _, ok := pp.Carries(on, 3, 0.5); ok {
		t.Error("a vertical ride carried a body by stickiness")
	}
}

// Every box Shift hits or Carries carries is one Reaches lets through.
func TestPistonPushReaches(t *testing.T) {
	for dir := range dirCount {
		for _, extending := range []bool{true, false} {
			for _, sticky := range []bool{true, false} {
				pp := &PistonPush{
					X: 3, Y: 64, Z: 0, Dir: dir, Progress: 0.5, Extending: extending, Source: true, Sticky: sticky, Top: 0.5,
					Boxes: []CollisionBox{{0, 0, 0, 1, 0.5, 1}, {0.25, 0.5, 0.25, 0.75, 1, 0.75}},
				}
				var reached, moved int
				for x := 1.0; x <= 5; x += 0.25 {
					for y := 62.0; y <= 67; y += 0.25 {
						for z := -2.0; z <= 2; z += 0.25 {
							e := [6]float64{x, y, z, x + 0.5, y + 0.75, z + 0.5}
							_, _, _, hit := pp.Shift(e)
							_, _, carried := pp.Carries(e, x+0.25, z+0.25)
							in := pp.Reaches(e)
							if in && !pp.ReachesFrom(e[1]) {
								t.Fatalf("dir %d extending %v sticky %v: box %v reached but ReachesFrom(%v) false", dir, extending, sticky, e, e[1])
							}
							if in {
								reached++
							}
							if hit || carried {
								moved++
								if !in {
									t.Fatalf("dir %d extending %v sticky %v: box %v moved (hit %v, carried %v) but not reached", dir, extending, sticky, e, hit, carried)
								}
							}
						}
					}
				}
				if moved == 0 || reached == 0 {
					t.Errorf("dir %d extending %v sticky %v: moved %d, reached %d, want both above zero", dir, extending, sticky, moved, reached)
				}
			}
		}
	}
}
