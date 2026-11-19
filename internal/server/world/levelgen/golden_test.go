package levelgen

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The golden values in testdata/golden.txt were recorded from the vanilla
// random, decoration and biome-zoom classes; every double is compared by its
// bits.

// goldenRandom builds the stream a golden id names.
func goldenRandom(t *testing.T, id string) RandomSource {
	t.Helper()
	parts := strings.Split(id, "/")
	kind, seedText, _ := strings.Cut(parts[0], ":")
	seed := mustInt(t, seedText)
	var r RandomSource
	if kind == "xoro" {
		r = NewXoroshiroRandom(seed)
	} else {
		r = NewLegacyRandom(seed)
	}
	for _, step := range parts[1:] {
		switch {
		case strings.HasPrefix(step, "c"):
			r.ConsumeCount(int(mustInt(t, step[1:])))
		case step == "fork":
			r = r.Fork()
		case strings.HasPrefix(step, "at"):
			c := strings.Split(step[2:], ",")
			r = r.ForkPositional().At(int32(mustInt(t, c[0])), int32(mustInt(t, c[1])), int32(mustInt(t, c[2])))
		case strings.HasPrefix(step, "hash"):
			r = r.ForkPositional().FromHashOf(step[4:])
		case strings.HasPrefix(step, "fseed"):
			r = r.ForkPositional().FromSeed(mustInt(t, step[5:]))
		default:
			t.Fatalf("unknown step %q", step)
		}
	}
	return r
}

func mustInt(t *testing.T, s string) int64 {
	t.Helper()
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func bitsOf(v float64) string { return fmt.Sprintf("%016x", math.Float64bits(v)) }

func TestGolden(t *testing.T) {
	t.Parallel()
	f, err := os.Open("testdata/golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	rands := map[string]RandomSource{}
	checked := map[string]int{}

	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		if strings.HasPrefix(text, "#") {
			continue
		}
		fl := strings.Fields(text)
		kind := fl[0]
		checked[kind]++
		got, want := "", ""
		switch kind {
		case "upgrade":
			s := UpgradeSeedTo128bit(mustInt(t, fl[1]))
			got, want = fmt.Sprint(s.Lo, s.Hi), fl[2]+" "+fl[3]
		case "md5":
			name := fl[1]
			if name == "-" {
				name = ""
			}
			s := SeedFromHashOf(name)
			got, want = fmt.Sprint(s.Lo, s.Hi), fl[2]+" "+fl[3]
		case "getseed":
			got = fmt.Sprint(PositionSeed(int32(mustInt(t, fl[1])), int32(mustInt(t, fl[2])), int32(mustInt(t, fl[3]))))
			want = fl[4]
		case "rand":
			r := rands[fl[1]]
			if r == nil {
				r = goldenRandom(t, fl[1])
				rands[fl[1]] = r
			}
			got, want = randOp(t, r, fl[2]), fl[3]
			if fl[2] == "G" && withinOneULP(t, got, want) {
				got = want
			}
		case "overworldgradient":
			factory := NewXoroshiroNamedRandom(mustInt(t, fl[1]), "minecraft:"+fl[2])
			r := factory.At(int32(mustInt(t, fl[3])), int32(mustInt(t, fl[4])), int32(mustInt(t, fl[5])))
			got, want = fmt.Sprintf("%08x", math.Float32bits(r.NextFloat())), fl[6]
		case "decorate":
			seed := mustInt(t, fl[1])
			r := NewWorldgenRandom(0)
			deco := r.SetDecorationSeed(seed, int32(mustInt(t, fl[2])), int32(mustInt(t, fl[3])))
			r.SetFeatureSeed(deco, int32(mustInt(t, fl[4])), int32(mustInt(t, fl[5])))
			got = fmt.Sprintf("%d %d %d %08x %d %d %s", deco, r.NextIntN(16), r.NextIntN(5), math.Float32bits(r.NextFloat()), r.NextIntN(3), r.NextLong(), bitsOf(r.NextDouble()))
			want = strings.Join(fl[6:], " ")
		case "endfeature":
			got, want = fmt.Sprint(EndFeatureSlots[fl[3]]), "{"+fl[1]+" "+fl[2]+"}"
		case "biomezoom":
			qx, qy, qz := ZoomedQuart(BiomeZoomSeed(mustInt(t, fl[1])), int32(mustInt(t, fl[2])), int32(mustInt(t, fl[3])), int32(mustInt(t, fl[4])))
			got, want = fmt.Sprint(qx, qy, qz), strings.Join(fl[5:], " ")
		default:
			t.Fatalf("line %d: unknown kind %q", line, kind)
		}
		if got != want {
			t.Errorf("line %d: %s\n got  %s\n want %s", line, text, got, want)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"upgrade", "md5", "getseed", "rand", "overworldgradient", "decorate", "biomezoom", "endfeature"} {
		if checked[k] == 0 {
			t.Errorf("no %s lines checked", k)
		}
	}
}

// withinOneULP reports whether two hex-printed doubles are at most one unit
// in the last place apart, the slack nextGaussian's Math.log leaves.
func withinOneULP(t *testing.T, a, b string) bool {
	t.Helper()
	x, err := strconv.ParseUint(a, 16, 64)
	if err != nil {
		t.Fatal(err)
	}
	y, err := strconv.ParseUint(b, 16, 64)
	if err != nil {
		t.Fatal(err)
	}
	return x == y || x == y+1 || y == x+1
}

func randOp(t *testing.T, r RandomSource, op string) string {
	t.Helper()
	switch {
	case op == "L":
		return fmt.Sprint(r.NextLong())
	case op == "I":
		return fmt.Sprint(r.NextInt())
	case strings.HasPrefix(op, "N"):
		return fmt.Sprint(r.NextIntN(int32(mustInt(t, op[1:]))))
	case op == "F":
		return fmt.Sprintf("%08x", math.Float32bits(r.NextFloat()))
	case op == "D":
		return bitsOf(r.NextDouble())
	case op == "B":
		return fmt.Sprint(r.NextBoolean())
	case op == "G":
		return bitsOf(r.NextGaussian())
	case strings.HasPrefix(op, "C"):
		r.ConsumeCount(int(mustInt(t, op[1:])))
		return "-"
	}
	t.Fatalf("unknown op %q", op)
	return ""
}
