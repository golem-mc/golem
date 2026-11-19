package rules

import "testing"

// The vectors are what a real java.util.Random prints for the same seeds,
// run on a JDK: the port has to step through the same states the JVM's own
// does, draw for draw, or the offers a table makes drift from vanilla's.
func TestJavaRandomMatchesTheJVM(t *testing.T) {
	vectors := []struct {
		seed    int64
		nextInt []int32
		int5    []int32
		int50   []int32
		floats  []float32
		doubles []float64
	}{
		{
			seed:    0,
			nextInt: []int32{-1155484576, -723955400, 1033096058},
			int5:    []int32{0, 3, 4, 2},
			int50:   []int32{10, 48, 29},
			floats:  []float32{0.73096776, 0.831441},
			doubles: []float64{0.730967787376657, 0.24053641567148587},
		},
		{
			seed:    1,
			nextInt: []int32{-1155869325, 431529176, 1761283695},
			int5:    []int32{0, 3, 2, 3},
			int50:   []int32{35, 38, 47},
			floats:  []float32{0.7308782, 0.100473166},
			doubles: []float64{0.7308781907032909, 0.41008081149220166},
		},
		{
			seed:    123456789,
			nextInt: []int32{-1442945365, -1016548095, 1962592967},
			int5:    []int32{0, 0, 3, 4},
			int50:   []int32{15, 0, 33},
			floats:  []float32{0.66403806, 0.76331645},
			doubles: []float64{0.664038103272266, 0.45695178590520646},
		},
		{
			seed:    -987654321,
			nextInt: []int32{-1314770571, -1648208228, -1702638463},
			int5:    []int32{2, 4, 1, 0},
			int50:   []int32{12, 34, 16},
			floats:  []float32{0.6938811, 0.6162466},
			doubles: []float64{0.6938811185396703, 0.6035736018242882},
		},
		{
			seed:    20260226,
			nextInt: []int32{1374869525, 1605725126, 1413705118},
			int5:    []int32{2, 3, 4, 3},
			int50:   []int32{12, 13, 9},
			floats:  []float32{0.32011175, 0.37386197},
			doubles: []float64{0.3201117571273746, 0.329153874515419},
		},
	}
	for _, v := range vectors {
		r := NewJavaRandom(v.seed)
		for i, want := range v.nextInt {
			if got := r.NextInt(); got != want {
				t.Errorf("seed %d NextInt #%d = %d, want %d", v.seed, i, got, want)
			}
		}
		r = NewJavaRandom(v.seed)
		for i, want := range v.int5 {
			if got := r.NextIntBound(5); got != want {
				t.Errorf("seed %d NextIntBound(5) #%d = %d, want %d", v.seed, i, got, want)
			}
		}
		r = NewJavaRandom(v.seed)
		for i, want := range v.int50 {
			if got := r.NextIntBound(50); got != want {
				t.Errorf("seed %d NextIntBound(50) #%d = %d, want %d", v.seed, i, got, want)
			}
		}
		r = NewJavaRandom(v.seed)
		for i, want := range v.floats {
			if got := r.NextFloat(); got != want {
				t.Errorf("seed %d NextFloat #%d = %v, want %v", v.seed, i, got, want)
			}
		}
		r = NewJavaRandom(v.seed)
		for i, want := range v.doubles {
			if got := r.NextDouble(); got != want {
				t.Errorf("seed %d NextDouble #%d = %v, want %v", v.seed, i, got, want)
			}
		}
	}
}

// TestJavaRandomNextIntBoundOne checks the one bound whose draw consumes
// randomness without ever seeming to: a power of two, and the smallest one
// there is, whose answers are always zero but whose state moves all the
// same - the vanilla bookshelf arithmetic nextInt(1)s its way through, and
// a port that returned without drawing would leave the offers off by
// everything that draw would have moved.
func TestJavaRandomNextIntBoundOne(t *testing.T) {
	r := NewJavaRandom(0)
	for i := range 2 {
		if got := r.NextIntBound(1); got != 0 {
			t.Fatalf("NextIntBound(1) #%d = %d, want 0", i, got)
		}
	}
	// The two draws happened: the third full draw is the one a fresh
	// source reaches two nextIntBound(1) calls in.
	fresh := NewJavaRandom(0)
	fresh.NextIntBound(1)
	fresh.NextIntBound(1)
	if got, want := fresh.NextInt(), int32(1033096058); got != want {
		t.Errorf("after two NextIntBound(1) draws NextInt = %d, want %d", got, want)
	}
}

// TestJavaRandomReSeeding checks that SetSeed puts the source back to the
// state a fresh one of the same seed stands in, which is what the offer
// recompute relies on when it re-seeds for each slot of a window.
func TestJavaRandomReSeeding(t *testing.T) {
	r := NewJavaRandom(12345)
	fresh := NewJavaRandom(99)
	r.NextInt()
	r.NextInt()
	r.SetSeed(99)
	for i := range 4 {
		if got, want := r.NextInt(), fresh.NextInt(); got != want {
			t.Fatalf("after re-seeding draw #%d = %d, want %d", i, got, want)
		}
	}
}

// Values from java.util.Random(42): nextInt(10) x4, nextInt(7) x3,
// nextFloat x3, nextBoolean x4, nextInt(2) x2.
func TestJavaRandomMatchesJDK(t *testing.T) {
	r := NewJavaRandom(42)
	for i, want := range []int32{0, 3, 8, 4} {
		if got := r.NextIntBound(10); got != want {
			t.Errorf("nextInt(10) #%d = %d, want %d", i, got, want)
		}
	}
	for i, want := range []int{5, 4, 1} {
		if got := r.Intn(7); got != want {
			t.Errorf("nextInt(7) #%d = %d, want %d", i, got, want)
		}
	}
	for i, want := range []float32{0.70771056, 0.6655489, 0.09132457} {
		if got := r.Float32(); got != want {
			t.Errorf("nextFloat #%d = %v, want %v", i, got, want)
		}
	}
	for i, want := range []bool{true, false, false, false} {
		if got := r.NextBoolean(); got != want {
			t.Errorf("nextBoolean #%d = %v, want %v", i, got, want)
		}
	}
	for i, want := range []int{0, 1} {
		if got := r.Intn(2); got != want {
			t.Errorf("nextInt(2) #%d = %d, want %d", i, got, want)
		}
	}
}
