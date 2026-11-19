package worldgen

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server"

	"github.com/golem-mc/golem/registry"
)

// The overworld's copper and iron veins, against the vanilla server's own
// fill (levelgen/testdata/overworld_golden.txt, overworldblocks lines): the
// raw ore blocks of each pinned chunk of seed 12345 are placed by the fill
// alone. The carvers replace raw ore blocks too (they are in
// overworld_carver_replaceables), so the served chunk holds at most as many as
// vanilla's fill, and exactly as many in a chunk no cave reaches.
func overworldRawOreBlocksMatchVanillasFill(t *testing.T, srv *server.Server) {
	f, err := os.Open("../../world/levelgen/testdata/overworld_golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	rawCopper := registry.BlockStates["minecraft:raw_copper_block"]
	rawIron := registry.BlockStates["minecraft:raw_iron_block"]

	checked, raw, kept := 0, 0, 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fl := strings.Fields(sc.Text())
		if len(fl) < 15 || fl[0] != "overworldblocks" || fl[1] != "12345" {
			continue
		}
		cx, cz := atoi(t, fl[2]), atoi(t, fl[3])
		// The golden's block codes 5 and 8 are the raw copper and raw iron.
		want := [2]int{}
		want[0], _ = strconv.Atoi(fl[4+5])
		want[1], _ = strconv.Atoi(fl[4+8])
		var got [2]int
		srv.ScanOverworldBlocksAt(cx*16, cz*16, cx*16+15, cz*16+15, -64, 319, func(_, _, _ int32, s uint16) {
			switch s {
			case rawCopper:
				got[0]++
			case rawIron:
				got[1]++
			}
		})
		if got[0] > want[0] || got[1] > want[1] {
			t.Errorf("chunk %d,%d: raw copper and raw iron blocks %v, more than vanilla's fill %v", cx, cz, got, want)
		}
		kept += got[0] + got[1]
		raw += want[0] + want[1]
		checked++
	}
	if kept == 0 || kept < raw/2 {
		t.Errorf("%d of vanilla's %d raw ore blocks stand, want at least half", kept, raw)
	}
	if checked == 0 || raw == 0 {
		t.Fatalf("checked %d chunks holding %d raw ore blocks; the golden file must pin some", checked, raw)
	}
}

func atoi(t *testing.T, s string) int32 {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return int32(n)
}
