package terrain

import (
	"bufio"
	"crypto/sha256"
	binaryencoding "encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

// floatBitsDigest hashes fixed-width float-bit hex lines without a trailing newline.
type floatBitsDigest struct {
	hash.Hash
	count int
}

func (d *floatBitsDigest) add(value float32) {
	if d.count != 0 {
		d.Write([]byte{'\n'})
	}
	var bits [4]byte
	var text [8]byte
	binaryencoding.BigEndian.PutUint32(bits[:], math.Float32bits(value))
	hex.Encode(text[:], bits[:])
	d.Write(text[:])
	d.count++
}

// TestBeardifierGolden preserves StructureGolden's complete sampled ranges;
// the volume records also check clipping, strides and clearing an empty intersection.
func TestBeardifierGolden(t *testing.T) {
	t.Parallel()
	f, err := os.Open("testdata/beardifier_golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	atoi := func(s string) int {
		v, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("bad integer %q: %v", s, err)
		}
		return v
	}
	adjustments := map[string]levelgen.TerrainAdjustment{
		"none": levelgen.AdjustNone, "bury": levelgen.AdjustBury,
		"beard_thin": levelgen.AdjustBeardThin, "beard_box": levelgen.AdjustBeardBox,
		"encapsulate": levelgen.AdjustEncapsulate,
	}
	contexts := map[string]*Context{}
	volumes := map[string]int{}
	kernels := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		fl := strings.Fields(sc.Text())
		if len(fl) == 0 || fl[0] == "#" {
			continue
		}
		switch fl[0] {
		case "kernel":
			if len(fl) != 3 {
				t.Fatalf("bad kernel record: %v", fl)
			}
			d := floatBitsDigest{Hash: sha256.New()}
			for _, value := range beardKernel {
				d.add(value)
			}
			got := fmt.Sprintf("%d %x", d.count, d.Sum(nil))
			if want := strings.Join(fl[1:], " "); got != want {
				t.Fatalf("kernel %s, vanilla %s", got, want)
			}
			kernels++
		case "beard":
			if len(fl) < 10 || fl[2] != "range" {
				t.Fatalf("bad beard record: %v", fl)
			}
			if _, exists := contexts[fl[1]]; exists {
				t.Fatalf("duplicate beard case %s", fl[1])
			}
			var rigids []levelgen.BeardRigid
			var junctions []levelgen.BeardJunction
			for i := 10; i < len(fl); {
				switch fl[i] {
				case "rigid":
					if len(fl)-i < 9 {
						t.Fatalf("short rigid: %v", fl[i:])
					}
					a, ok := adjustments[fl[i+7]]
					if !ok {
						t.Fatalf("unknown adjustment %q", fl[i+7])
					}
					rigids = append(rigids, levelgen.BeardRigid{
						Box: levelgen.BoundingBox{MinX: atoi(fl[i+1]), MinY: atoi(fl[i+2]), MinZ: atoi(fl[i+3]),
							MaxX: atoi(fl[i+4]), MaxY: atoi(fl[i+5]), MaxZ: atoi(fl[i+6])},
						Adjustment: a, GroundLevelDelta: atoi(fl[i+8]),
					})
					i += 9
				case "junction":
					if len(fl)-i < 4 {
						t.Fatalf("short junction: %v", fl[i:])
					}
					junctions = append(junctions, levelgen.BeardJunction{X: atoi(fl[i+1]), GroundY: atoi(fl[i+2]), Z: atoi(fl[i+3])})
					i += 4
				default:
					t.Fatalf("unknown piece %q", fl[i])
				}
			}
			c := NewContext(NewBeardifier(rigids, junctions))
			contexts[fl[1]] = c
			d := floatBitsDigest{Hash: sha256.New()}
			for x := int32(atoi(fl[3])); x <= int32(atoi(fl[6])); x++ {
				for y := int32(atoi(fl[4])); y <= int32(atoi(fl[7])); y++ {
					for z := int32(atoi(fl[5])); z <= int32(atoi(fl[8])); z++ {
						d.add(BeardifierSampler().Sample(c, x, y, z))
					}
				}
			}
			if got := fmt.Sprintf("%x", d.Sum(nil)); got != fl[9] {
				t.Errorf("beard case %s: %s, vanilla %s", fl[1], got, fl[9])
			}
		case "beardvolume":
			if len(fl) < 11 || contexts[fl[1]] == nil {
				t.Fatalf("bad volume or missing beard case: %v", fl[:min(len(fl), 11)])
			}
			v := NewVolume(int32(atoi(fl[2])), int32(atoi(fl[3])), int32(atoi(fl[4])),
				int32(atoi(fl[5])), int32(atoi(fl[6])), int32(atoi(fl[7])))
			v.StepX, v.StepY, v.StepZ = int32(atoi(fl[8])), int32(atoi(fl[9])), int32(atoi(fl[10]))
			if len(fl) != 11+v.Size() {
				t.Fatalf("case %s: %d volume values, want %d", fl[1], len(fl)-11, v.Size())
			}
			values := make([]float32, v.Size())
			for i := range values {
				values[i] = float32(math.NaN())
			}
			BeardifierSampler().SampleVolume(contexts[fl[1]], values, v)
			for i, value := range values {
				if got := bitsHex(value); got != fl[11+i] {
					t.Fatalf("beard case %s volume %v index %d: %s, vanilla %s", fl[1], v, i, got, fl[11+i])
				}
			}
			volumes[fl[1]]++
		default:
			t.Fatalf("unknown beardifier record %q", fl[0])
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if kernels != 1 || len(contexts) != 10 {
		t.Fatalf("got %d kernels and %d beard cases, want 1 and 10", kernels, len(contexts))
	}
	for id := range contexts {
		if volumes[id] != 3 {
			t.Errorf("beard case %s: %d volumes, want 3", id, volumes[id])
		}
	}
}
