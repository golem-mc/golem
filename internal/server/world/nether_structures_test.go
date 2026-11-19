package world

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The chests and the spawner below are the ones a vanilla server
// generates for seed 0; their loot seeds come from the same random as the
// fortress's block placement.
func TestNetherFortressBlockEntitiesMatchVanilla(t *testing.T) {
	t.Parallel()
	cases := []struct {
		cx, cz  int32
		pos     posKey
		kind    BlockEntityKind
		lootSed int64
	}{
		{19, 1, posKey{315, 75, 27}, KindChest, 1675375627424343666},
		{19, 3, posKey{318, 75, 52}, KindChest, 3707919909708540670},
		{19, 4, posKey{313, 75, 74}, KindChest, 4335276323064466317},
		{20, 4, posKey{320, 75, 64}, KindChest, 1069160516443012791},
		{18, 1, posKey{298, 76, 30}, KindSpawner, 0},
	}
	for _, c := range cases {
		be := netherChunk(0, c.cx, c.cz).entities[entityKey(c.pos[0], c.pos[1], c.pos[2])]
		if be == nil || be.Kind != c.kind {
			t.Fatalf("chunk (%d, %d): no %s at %v", c.cx, c.cz, c.kind, c.pos)
		}
		if c.kind == KindChest && (be.LootTable != fortressLootTable || be.LootTableSeed != c.lootSed) {
			t.Errorf("chest at %v: loot %q seed %d, want %q seed %d", c.pos, be.LootTable, be.LootTableSeed, fortressLootTable, c.lootSed)
		}
		if c.kind == KindSpawner {
			if e, _ := nbt.Get[nbt.Compound](be.SpawnData.Entries(), "entity"); e["id"] != nbt.String("minecraft:blaze") {
				t.Errorf("spawner at %v spawns %v, want a blaze", c.pos, be.SpawnData)
			}
		}
	}
}

// The counts are those of a vanilla 26.3 world: worldgen:parity finds the
// blocks of these chunks identical.
func TestNetherFortressBricksMatchVanilla(t *testing.T) {
	t.Parallel()
	bricks, _ := registry.FindState("minecraft:nether_bricks", nil)
	cases := []struct {
		seed   int64
		cx, cz int32
		want   int
	}{{0, 19, 1, 1820}, {0, 19, 3, 2306}, {0, 20, 4, 803}, {0, 18, 1, 2853}}
	for _, c := range cases {
		ch := netherChunk(c.seed, c.cx, c.cz)
		n := 0
		for y := range int32(128) {
			for z := range int32(16) {
				for x := range int32(16) {
					if ch.BlockAt(x, y, z) == bricks {
						n++
					}
				}
			}
		}
		if n != c.want {
			t.Errorf("seed %d chunk (%d, %d): %d nether bricks, want %d", c.seed, c.cx, c.cz, n, c.want)
		}
	}
}

// TestFortressSaveMatchesVanilla checks the fortress start a generator saves,
// with the throne's spawner and the turns' chests saved as placed once the
// generated range reached them, and the references every chunk of the range
// holds, against vanilla's saved chunks.
// vanilla: StructureStart.createTag, ChunkGenerator.createReferences
func TestFortressSaveMatchesVanilla(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/fortress_starts.txt")
	if err != nil {
		t.Fatal(err)
	}
	type run struct {
		seed           int64
		x0, z0, x1, z1 int32
		starts         [][4]string
		refs           map[ChunkPos]int64
	}
	runs := map[string]*run{}
	var order []string
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		key := strings.Join(f[1:6], " ")
		r := runs[key]
		if r == nil {
			r = &run{refs: map[ChunkPos]int64{}}
			if _, err := fmt.Sscan(key, &r.seed, &r.x0, &r.z0, &r.x1, &r.z1); err != nil {
				t.Fatal(err)
			}
			runs[key] = r
			order = append(order, key)
		}
		if f[0] == "start" {
			r.starts = append(r.starts, [4]string(f[6:10]))
			continue
		}
		var pos ChunkPos
		var packed int64
		if _, err := fmt.Sscan(strings.Join(f[6:9], " "), &pos.X, &pos.Z, &packed); err != nil {
			t.Fatal(err)
		}
		r.refs[pos] = packed
	}
	for _, key := range order {
		r := runs[key]
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			fixture := cachedNetherFixture(r.seed, r.x0, r.z0, r.x1, r.z1)
			chunks := cachedNetherRange(r.seed, r.x0, r.z0, r.x1, r.z1)
			for _, st := range r.starts {
				var cx, cz int32
				var pieces int
				if _, err := fmt.Sscan(strings.Join(st[:3], " "), &cx, &cz, &pieces); err != nil {
					t.Fatal(err)
				}
				encoded, ok := fixture.fortresses[ChunkPos{X: cx, Z: cz}]
				if !ok {
					t.Fatalf("no fortress starts in chunk %d,%d", cx, cz)
				}
				_, tag, err := nbt.Decode(encoded)
				if err != nil {
					t.Fatal(err)
				}
				children, _ := nbt.Get[nbt.List](tag.(nbt.Compound), "Children")
				if got := fmt.Sprintf("%x", sha256.Sum256(encoded)); len(children.Items) != pieces || got != st[3] {
					t.Errorf("start %d,%d: %d pieces, digest %s; vanilla saved %d pieces, digest %s", cx, cz, len(children.Items), got, pieces, st[3])
				}
			}
			for pos, c := range chunks {
				root, err := encodeChunkNBT(c)
				if err != nil {
					t.Fatal(err)
				}
				structures, _ := nbt.Get[nbt.Compound](root, "structures")
				refs, _ := nbt.Get[nbt.Compound](structures, "References")
				got, _ := nbt.Get[nbt.LongArray](refs, fortressID)
				want, ok := r.refs[pos]
				if ok != (len(got) == 1) || ok && got[0] != want || len(got) > 1 {
					t.Errorf("chunk %v references %v, vanilla saved %d (%v)", pos, got, want, ok)
				}
			}
		})
	}
}
