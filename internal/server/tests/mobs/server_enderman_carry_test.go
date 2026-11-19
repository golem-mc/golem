package mobs

import (
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/console"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// The enderman's carried block over the wire, in the world and in its save.

// carriedWatch records the carried-block metadata an enderman's packets say.
type carriedWatch struct {
	id    int32
	state int32
	seen  bool
}

func (w *carriedWatch) consume(c *servertest.Client) func(int32, []byte) bool {
	return func(packetID int32, data []byte) bool {
		if packetID != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != w.id {
			return false
		}
		seen := servertest.VariantSeen{Meta: map[uint8][]int32{}}
		c.ReadVariantMetadata(r, &seen)
		if v, ok := seen.Meta[protocol.MetadataIndexEndermanCarried]; ok {
			w.state, w.seen = v[0], true
			return true
		}
		return false
	}
}

func TestAnEndermanTakesADirtBlockFromTheWorld(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), "Watcher")
	srv.SetPaused(true)

	dirt := servertest.BlockState(t, "minecraft:dirt")
	const from, to = 5, 11
	for x := int32(from); x <= to; x++ {
		for z := int32(-3); z <= 3; z++ {
			srv.SetBlock(x, 64, z, dirt)
		}
	}
	srv.SetBlock(8, 64, 0, 0)
	id := srv.SpawnMob("minecraft:enderman", 8.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeEnderman)

	watch := &carriedWatch{id: id}
	pumpTicks(c, srv, 2000, servertest.HostileSkip, watch.consume(c))
	if !watch.seen || watch.state != int32(dirt) {
		t.Fatalf("carried-block metadata = %d (seen %v), want the dirt state %d", watch.state, watch.seen, dirt)
	}
	taken := 0
	for x := int32(from); x <= to; x++ {
		for z := int32(-3); z <= 3; z++ {
			if srv.BlockAt(x, 64, z) == 0 {
				taken++
			}
		}
	}
	// The cell the enderman stood in was already empty, and may have been
	// walked out of since; exactly one more cell is now air.
	if taken != 2 {
		t.Errorf("%d empty cells in the dirt field, want the enderman's own cell and the one block it took", taken)
	}
}

func TestTwoEndermenNeverDuplicateOrLoseABlockInOneTick(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), "Watcher")
	srv.SetPaused(true)

	dirt := servertest.BlockState(t, "minecraft:dirt")
	srv.SetBlock(8, 64, 0, dirt)
	a := srv.SpawnMob("minecraft:enderman", 6.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeEnderman)
	b := srv.SpawnMob("minecraft:enderman", 6.5, 64, 2.5)
	c.ExpectMobSpawn(protocol.EntityTypeEnderman)

	ca, cb := srv.EndermenMoveBlockTogether(a, b, 8, 64, 0, true, 0)
	if (ca == dirt) == (cb == dirt) || ca+cb != dirt {
		t.Errorf("after both took one cell they carry %d and %d, want exactly one carrying the dirt %d", ca, cb, dirt)
	}
	if srv.BlockAt(8, 64, 0) != 0 {
		t.Error("the taken cell is not air")
	}

	ca, cb = srv.EndermenMoveBlockTogether(a, b, 8, 64, 0, false, dirt)
	if (ca == dirt) == (cb == dirt) || ca+cb != dirt {
		t.Errorf("after both set down into one cell they carry %d and %d, want exactly one still carrying the dirt %d", ca, cb, dirt)
	}
	if srv.BlockAt(8, 64, 0) != dirt {
		t.Error("the cell does not hold the placed block")
	}
}

func TestACarriedBlockLoadsTellsAndIsDroppedOnDeath(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"))
	srv.SetPaused(true)

	c.ChatCommand(`summon minecraft:enderman 6.5 64 0.5 {NoAI:1b,carriedBlockState:{id:"minecraft:grass_block",properties:{snowy:"false"}}}`)
	m := c.ExpectMobSpawn(protocol.EntityTypeEnderman)
	grass := servertest.BlockState(t, "minecraft:grass_block")
	if !m.HasCarried || m.Carried != int32(grass) {
		t.Fatalf("spawn carried-block metadata = %d (seen %v), want the grass block state %d", m.Carried, m.HasCarried, grass)
	}

	if !srv.KillMob(m.EntityID) {
		t.Fatal("the enderman did not answer /kill")
	}
	srv.TickMobs(4)
	var dropped []string
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		dropped = append(dropped, it.Item)
	}
	// A silk touch roll: the block itself, not the dirt a plain break gives.
	has := func(name string) bool {
		return slices.Contains(dropped, name)
	}
	if !has("minecraft:grass_block") || has("minecraft:dirt") {
		t.Errorf("the dead enderman dropped %v, want the grass block it carried and no dirt", dropped)
	}
}

func TestACarriedBlockAndAngerAreSaved(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	srv, _ := servertest.Start(t, cfg)
	script := strings.Join([]string{
		`summon minecraft:enderman 100.5 64 0.5 {NoAI:1b,carriedBlockState:{id:"minecraft:dirt"},anger_end_time:5000L}`,
		`summon minecraft:enderman 100.5 64 8.5 {NoAI:1b}`,
	}, "\n") + "\n"
	console.New(strings.NewReader(script), io.Discard, io.Discard, srv).Serve()
	srv.Shutdown()

	var carrying, calm nbt.Compound
	for _, compound := range servertest.ReadChunkEntities(t, cfg.LevelName, world.ChunkPos{X: world.ChunkOf(100.5), Z: world.ChunkOf(0.5)}) {
		if _, ok := compound["carriedBlockState"]; ok {
			carrying = compound
		} else {
			calm = compound
		}
	}
	if carrying["carriedBlockState"] != nbt.String("minecraft:dirt") {
		t.Errorf("saved carriedBlockState = %v, want dirt", carrying["carriedBlockState"])
	}
	if end, _ := carrying["anger_end_time"].(nbt.Long); end <= 0 {
		t.Errorf("a carrying, angry enderman saved anger_end_time %v, want a time in the future", carrying["anger_end_time"])
	}
	if end, ok := calm["anger_end_time"].(nbt.Long); !ok || end != -1 {
		t.Errorf("a calm enderman saved anger_end_time %v, want -1", calm["anger_end_time"])
	}
	if _, ok := calm["carriedBlockState"]; ok {
		t.Error("an enderman carrying nothing saved a carriedBlockState")
	}
}
