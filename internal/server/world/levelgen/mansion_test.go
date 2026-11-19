package levelgen

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Each grid digest covers 256 complete layouts and the next random draw.
// The room digests cover all three collections, including stairs and secrets.
// Assembly digests also cover every ordered template and its world transform.
// Pins come from the server's classes through tools/java/MansionGolden.java.
func TestMansionGolden(t *testing.T) {
	t.Parallel()
	file, err := os.Open("testdata/mansion_golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	grids, collections, fallbacks, assemblies := 0, 0, 0, 0
	scan := bufio.NewScanner(file)
	for scan.Scan() {
		line := scan.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		switch f[0] {
		case "grid":
			var seed int64
			var samples, wantAbsent, wantStairs, wantSecrets int
			var want string
			if _, err := fmt.Sscanf(line, "grid %d %d %d %d %d %s", &seed, &samples, &wantAbsent, &wantStairs, &wantSecrets, &want); err != nil {
				t.Fatal(err)
			}
			var lines []string
			absent, stairs, secrets := 0, 0, 0
			for sample := range samples {
				rng := NewLegacyRandom(seed + int64(sample))
				g := NewMansionGrid(rng)
				hasThird := false
				for i, floor := range []*MansionFloor{&g.Base, &g.Third, &g.Rooms[0], &g.Rooms[1], &g.Rooms[2]} {
					for z := range 11 {
						for x := range 11 {
							v := floor[x][z]
							lines = append(lines, strconv.Itoa(v))
							if i == 1 && floor.IsHouse(x, z) {
								hasThird = true
							}
							if i >= 2 && v&MansionRoomStairs != 0 {
								stairs++
							}
							if i >= 2 && v&MansionRoomOrigin != 0 && v&MansionRoomDoor == 0 {
								secrets++
							}
						}
					}
				}
				if !hasThird {
					absent++
				}
				lines = append(lines, strconv.Itoa(int(rng.NextInt())))
			}
			// The Java helper terminates every cell and random draw with a newline.
			lines = append(lines, "")
			if got := digestLines(lines); got != want {
				t.Errorf("seed %d grids digest %s, vanilla %s", seed, got, want)
			}
			if absent != wantAbsent || stairs != wantStairs || secrets != wantSecrets {
				t.Errorf("seed %d absent/stairs/secrets %d/%d/%d, vanilla %d/%d/%d", seed, absent, stairs, secrets, wantAbsent, wantStairs, wantSecrets)
			}
			grids += samples
		case "fallback":
			var scenario int
			var want string
			if _, err := fmt.Sscanf(line, "fallback %d %s", &scenario, &want); err != nil {
				t.Fatal(err)
			}
			rng := NewLegacyRandom(12345)
			g := &mansionGridBuilder{MansionGrid: &MansionGrid{}, rng: rng}
			if scenario == 1 {
				g.Base[5][5], g.Base[6][5] = 2, 2
				g.Rooms[1][5][5] = MansionRoomOrigin | MansionRoomDoor | MansionRoom1x2 | 10
				g.Rooms[1][6][5] = MansionRoom1x2 | 10
			}
			g.setupThirdFloor()
			var lines []string
			for _, floor := range []*MansionFloor{&g.Base, &g.Third, &g.Rooms[0], &g.Rooms[1], &g.Rooms[2]} {
				for z := range 11 {
					for x := range 11 {
						lines = append(lines, strconv.Itoa(floor[x][z]))
					}
				}
			}
			lines = append(lines, strconv.Itoa(int(rng.NextInt())), "")
			if got := digestLines(lines); got != want {
				t.Errorf("fallback %d digest %s, vanilla %s", scenario, got, want)
			}
			fallbacks++
		case "pieces":
			var seed int64
			var rotation, samples, wantCount int
			var want string
			if _, err := fmt.Sscanf(line, "pieces %d %d %d %d %s", &seed, &rotation, &samples, &wantCount, &want); err != nil {
				t.Fatal(err)
			}
			var lines []string
			count := 0
			for sample := range samples {
				rng := NewLegacyRandom(seed + int64(sample))
				pieces := GenerateMansion(rng, [3]int{-137 + sample, 70 + sample%5, 211 - sample}, rotation)
				lines = append(lines, strconv.Itoa(len(pieces)))
				count += len(pieces)
				for _, piece := range pieces {
					lines = append(lines, fmt.Sprintf("%s %d %d %d %d %d", piece.Name, piece.Pos[0], piece.Pos[1], piece.Pos[2], piece.Rotation, piece.Mirror))
				}
				lines = append(lines, strconv.Itoa(int(rng.NextInt())))
			}
			lines = append(lines, "")
			if got := digestLines(lines); got != want || count != wantCount {
				t.Errorf("seed %d rotation %d: %d pieces, digest %s; vanilla %d, %s", seed, rotation, count, got, wantCount, want)
			}
			assemblies += samples
		case "room":
			var floor, kind int
			var stairs bool
			var want string
			if _, err := fmt.Sscanf(line, "room %d %d %t %s", &floor, &kind, &stairs, &want); err != nil {
				t.Fatal(err)
			}
			rng := NewLegacyRandom(12345)
			var lines []string
			for range 256 {
				lines = append(lines, MansionRoomTemplate(rng, floor, MansionRoomKind(kind), stairs), strconv.Itoa(int(rng.NextInt())))
			}
			lines = append(lines, "")
			if got := digestLines(lines); got != want {
				t.Errorf("floor %d kind %d stairs %t digest %s, vanilla %s", floor, kind, stairs, got, want)
			}
			collections++
		default:
			t.Fatalf("unknown golden line %q", line)
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	if assemblies != 5120 {
		t.Fatalf("checked %d assemblies, want 5120", assemblies)
	}
	if grids != 1280 || collections != 42 || fallbacks != 2 {
		t.Fatalf("checked %d layouts, %d collections and %d fallbacks, want 1280, 42 and 2", grids, collections, fallbacks)
	}
}

func BenchmarkMansionGenerate(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		GenerateMansion(NewLegacyRandom(12345), [3]int{-137, 70, 211}, 0)
	}
}
