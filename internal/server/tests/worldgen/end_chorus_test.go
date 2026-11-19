package worldgen

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func TestEndChorusCrossChunkContinuation(t *testing.T) {
	testEndChorusContinuation(t, false)
}

func TestEndChorusContinuationAfterRestart(t *testing.T) {
	testEndChorusContinuation(t, true)
}

// Vanilla 26.3 seed 0: a tree rooted at (1000059,62,-1000033) crosses
// the southern chunk boundary. Its shape depends on earlier trees to the north.
func testEndChorusContinuation(t *testing.T, restart bool) {
	t.Helper()
	dir := t.TempDir()
	w := world.NewDimensionWorld(dir, false, world.NewEndGenerator(0))
	defer func() { w.Close() }()
	north := world.ChunkPos{X: 62503, Z: -62503}
	w.Acquire(north)
	w.Release(north)
	if restart {
		w.Close()
		w = world.NewDimensionWorld(dir, false, world.NewEndGenerator(0))
	}
	south := world.ChunkPos{X: 62503, Z: -62502}
	w.Acquire(south)
	defer w.Release(south)
	// Raw block states from the cached vanilla range 62500,-62504..62504,-62500.
	for line := range strings.SplitSeq(strings.TrimSpace(`
1000059 64 -1000032 minecraft:chorus_plant,down=false,east=false,north=true,south=false,up=true,west=false
1000059 65 -1000032 minecraft:chorus_plant,down=true,east=false,north=false,south=false,up=true,west=false
1000059 66 -1000032 minecraft:chorus_plant,down=true,east=false,north=false,south=false,up=true,west=false
1000059 67 -1000032 minecraft:chorus_plant,down=true,east=false,north=false,south=true,up=false,west=false
1000059 67 -1000031 minecraft:chorus_plant,down=false,east=false,north=true,south=false,up=true,west=false
1000059 68 -1000031 minecraft:chorus_plant,down=true,east=false,north=false,south=false,up=true,west=false
1000059 69 -1000031 minecraft:chorus_plant,down=true,east=false,north=false,south=false,up=true,west=false
1000059 70 -1000031 minecraft:chorus_plant,down=true,east=false,north=false,south=true,up=false,west=false
1000059 70 -1000030 minecraft:chorus_plant,down=false,east=false,north=true,south=false,up=true,west=false
1000059 71 -1000030 minecraft:chorus_plant,down=true,east=false,north=false,south=false,up=true,west=false
1000058 72 -1000030 minecraft:chorus_plant,down=false,east=true,north=false,south=false,up=true,west=false
1000059 72 -1000030 minecraft:chorus_plant,down=true,east=false,north=false,south=true,up=false,west=true
1000059 72 -1000029 minecraft:chorus_plant,down=false,east=false,north=true,south=false,up=true,west=false
1000058 73 -1000030 minecraft:chorus_flower,age=5
1000059 73 -1000029 minecraft:chorus_plant,down=true,east=false,north=false,south=false,up=true,west=false
1000059 74 -1000029 minecraft:chorus_plant,down=true,east=false,north=false,south=false,up=true,west=false
1000059 75 -1000029 minecraft:chorus_flower,age=5
`), "\n") {
		var x, y, z int32
		var want string
		if _, err := fmt.Sscan(line, &x, &y, &z, &want); err != nil {
			t.Fatal(err)
		}
		name, props, _ := registry.BlockStateName(w.BlockAt(x, y, z))
		got := name
		if props != "" {
			got += "," + props
		}
		if got != want {
			t.Errorf("(%d,%d,%d): %s, want %s", x, y, z, got, want)
		}
	}
}
