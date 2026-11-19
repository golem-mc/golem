package levelgen

import "testing"

func TestFindSpawnPositionRadialSteps(t *testing.T) {
	var samples [][2]int32
	x, z := FindSpawnPosition(func(qx, qz int32) [6]int64 {
		samples = append(samples, [2]int32{qx, qz})
		target := [6]int64{100, 0, 0, 0, 999999, 0}
		switch [2]int32{qx, qz} {
		case [2]int32{0, 128}: // first coarse candidate: (0,512)
			target[0] = 1
		case [2]int32{0, 136}: // first fine candidate around (0,512): (0,544)
			target[0] = 0
		}
		return target
	}, []ClimatePoint{{}})
	if x != 0 || z != 544 {
		t.Fatalf("spawn = (%d,%d), want (0,544)", x, z)
	}
	// Hand-evaluated Climate.SpawnFinder steps: angles 0,1,2,3,4,
	// radius 512, truncate sin/cos products to blocks, then floor to quarts.
	want := [][2]int32{{0, 0}, {0, 128}, {107, 69}, {116, -54}, {18, -127}, {-97, -84}}
	for i, point := range want {
		if samples[i] != point {
			t.Errorf("sample %d = %v, want %v", i, samples[i], point)
		}
	}
}

func TestFindSpawnPositionFitness(t *testing.T) {
	for axis := range 6 {
		if axis == 4 { // depth is ignored, covered by the radial-step test.
			continue
		}
		target := ClimatePoint{}
		target.Bounds[axis] = [2]int64{10, 20}
		// The first target's offset makes it worse than the second target.
		penalized := target
		penalized.Bounds[6] = [2]int64{100, 100}
		x, z := FindSpawnPosition(func(qx, qz int32) [6]int64 {
			var values [6]int64
			if qx == 0 && qz == 128 {
				values[axis] = 15
			}
			return values
		}, []ClimatePoint{penalized, target})
		if x != 0 || z != 512 {
			t.Errorf("axis %d: spawn = (%d,%d), want (0,512)", axis, x, z)
		}
	}
	// All candidates fit equally well: the origin's distance bias wins.
	if x, z := FindSpawnPosition(func(int32, int32) [6]int64 { return [6]int64{} }, []ClimatePoint{{}}); x != 0 || z != 0 {
		t.Errorf("uniform climate spawn = (%d,%d), want origin", x, z)
	}
	if x, z := FindSpawnPosition(nil, nil); x != 0 || z != 0 {
		t.Errorf("empty target spawn = (%d,%d), want origin", x, z)
	}
}

func TestOverworldSpawnTargets(t *testing.T) {
	want := [7][2]int64{{-10000, 10000}, {-10000, 10000}, {-1100, 10000}, {-10000, 10000}, {0, 0}, {-10000, -1600}, {0, 0}}
	points := OverworldSpawnTargets()
	if len(points) != 2 {
		t.Fatalf("got %d targets, want 2", len(points))
	}
	for i, point := range points {
		if i == 1 {
			want[5] = [2]int64{1600, 10000}
		}
		if point.Bounds != want {
			t.Errorf("target %d = %v, want %v", i, point.Bounds, want)
		}
	}
}
