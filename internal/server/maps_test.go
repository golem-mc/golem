package server

import (
	"maps"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func TestMapFrameMarksMatchListScan(t *testing.T) {
	t.Parallel()
	s := &Server{
		frameCells:  make(map[frameCell][]*frameEntity),
		frameChunks: entity.NewRegionalChunkIndex[*frameEntity](),
		maps:        world.NewMaps(t.TempDir()),
	}
	w, other := new(world.World), new(world.World)
	s.entityBroadcastRange.Store(10)
	stack := protocol.ItemStack{ItemID: int32(registry.ItemFilledMap), Count: 1}
	stack.SetMapID(0)
	register := func(dim *world.World, x, z, dir int32, stack protocol.ItemStack) *frameEntity {
		f := newFrame(s.nextEntity(), uuid.New(), dim, x, world.GroundLevel+1, z, dir, false)
		f.setItem(stack)
		s.framesMu.Lock()
		s.registerFrameLocked(f)
		s.framesMu.Unlock()
		return f
	}
	east := register(w, 16, 0, protocol.FaceSouth, stack)
	west := register(w, -1, 0, protocol.FaceNorth, stack)
	register(w, 16, 0, protocol.FaceEast, stack)
	register(w, -1, -2, protocol.FaceWest, stack)
	register(w, 15, 15, protocol.FaceSouth, stack) // in the square, outside tracking range
	register(other, 0, 0, protocol.FaceSouth, stack)
	register(w, 0, 0, protocol.FaceSouth, protocol.ItemStack{ItemID: int32(registry.ItemFilledMap), Count: 1})
	mapIDOnStone := protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}
	mapIDOnStone.SetMapID(0)
	register(w, 0, 0, protocol.FaceSouth, mapIDOnStone)
	secondMap := protocol.ItemStack{ItemID: int32(registry.ItemFilledMap), Count: 1}
	secondMap.SetMapID(1)
	register(w, 0, -1, protocol.FaceSouth, secondMap)

	compare := func(step string, x, z float64, radius int32) {
		t.Helper()
		want := make(map[int32][]frameMark)
		pct := int(s.entityBroadcastRange.Load())
		for _, f := range s.frames {
			if id, ok := filledMapID(f.item); ok && f.w == w && frameSees(f, x, z, radius, pct) {
				want[id] = append(want[id], frameMark{x: f.x, z: f.z, dir: f.dir})
			}
		}
		if got := s.mapFrameMarks(w, x, z, radius); !maps.EqualFunc(got, want, slices.Equal[[]frameMark]) {
			t.Errorf("%s: frame marks = %v, want %v", step, got, want)
		}
	}
	compare("small list", 0.5, 0.5, 4)
	for i := range 20 {
		register(w, int32(4096+i), 0, protocol.FaceSouth, stack)
	}
	compare("indexed", 0.5, 0.5, 4)

	s.teleportFrame(east, player.Position{X: 15, Y: float64(world.GroundLevel + 1)}, "test")
	s.teleportFrame(west, player.Position{X: -17, Y: float64(world.GroundLevel + 1)}, "test")
	compare("after rehangs", 0.5, 0.5, 4)
	compare("west of the border", -16.25, -0.25, 4)
	s.entityBroadcastRange.Store(100)
	compare("limited by view distance", 0.5, 0.5, 1)

	s.framesMu.Lock()
	s.unlistFrameLocked(east)
	s.framesMu.Unlock()
	compare("after removal", 0.5, 0.5, 1)
}
