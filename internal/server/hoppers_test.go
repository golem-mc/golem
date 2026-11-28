package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func TestHopperColumnSeesItemRegisteredDuringPass(t *testing.T) {
	s := &Server{}
	w := new(world.World)
	s.indexItemColumns(w)
	if s.itemsOverColumn(w, 0, 0) {
		t.Fatal("empty hopper column reported an item")
	}
	if scanned := len(s.itemCols.scanned); scanned != 0 {
		t.Errorf("empty query scanned %d chunks, want 0", scanned)
	}

	stack := protocol.ItemStack{ItemID: int32(registry.ItemDirt), Count: 1}
	s.spawnItem(w, stack, 0.5, 65, 0.5, 0, 0, 0, 0)
	if !s.itemsOverColumn(w, 0, 0) {
		t.Fatal("hopper column missed the drop registered during the pass")
	}
}

func TestHopperSeesMidTickVehiclesAndItems(t *testing.T) {
	dirt := protocol.ItemStack{ItemID: int32(registry.ItemDirt), Count: 1}
	hopperState := registry.MustState("minecraft:hopper", map[string]string{"facing": "down", "enabled": "true"})

	cases := []struct {
		name  string
		spawn func(t *testing.T, s *Server, w *world.World)
	}{
		{
			name: "chest_minecart",
			spawn: func(t *testing.T, s *Server, w *world.World) {
				if got := s.cartColsEmptyWorld.Load(); got != w {
					t.Fatalf("cartColsEmptyWorld = %p, want %p", got, w)
				}
				if got := s.entityContainerAt(w, 0.5, 65.5, 0.5, nil); got != nil {
					t.Fatalf("entityContainerAt before spawn = %v, want nil", got)
				}
				// A non-container cart must not drop the empty fast path.
				s.spawnMinecart(w, "minecraft:minecart", 0.5, 65, 0.5)
				if got := s.cartColsEmptyWorld.Load(); got != w {
					t.Fatalf("cartColsEmptyWorld after plain cart = %p, want %p", got, w)
				}
				cart := s.spawnMinecart(w, "minecraft:chest_minecart", 0.5, 65, 0.5)
				s.minecartsMu.Lock()
				cart.cargo.slots[0] = dirt
				s.minecartsMu.Unlock()
			},
		},
		{
			name: "chest_boat",
			spawn: func(t *testing.T, s *Server, w *world.World) {
				if got := s.cartColsEmptyWorld.Load(); got != w {
					t.Fatalf("cartColsEmptyWorld = %p, want %p", got, w)
				}
				if got := s.entityContainerAt(w, 0.5, 65.5, 0.5, nil); got != nil {
					t.Fatalf("entityContainerAt before spawn = %v, want nil", got)
				}
				// A non-chest boat must not drop the empty fast path.
				s.spawnBoat(w, "minecraft:oak_boat", 0.5, 65, 0.5, 0, customName{})
				if got := s.cartColsEmptyWorld.Load(); got != w {
					t.Fatalf("cartColsEmptyWorld after plain boat = %p, want %p", got, w)
				}
				boat := s.spawnBoat(w, "minecraft:oak_chest_boat", 0.5, 65, 0.5, 0, customName{})
				s.boatsMu.Lock()
				boat.cargo[0] = dirt
				s.boatsMu.Unlock()
			},
		},
		{
			name: "dropped_item",
			spawn: func(t *testing.T, s *Server, w *world.World) {
				if got := s.itemCount.Load(); got != 0 {
					t.Fatalf("itemCount = %d, want 0", got)
				}
				if s.itemsOverColumn(w, 0, 0) {
					t.Fatal("itemsOverColumn before spawn = true, want false")
				}
				s.spawnItem(w, dirt, 0.5, 65, 0.5, 0, 0, 0, 0)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.LevelName = t.TempDir()
			cfg.LevelType = TestFlatLevelType
			s, err := newServer(cfg, 1)
			if err != nil {
				t.Fatal(err)
			}
			s.SetSpawning(false)
			t.Cleanup(s.Shutdown)

			w := s.world
			pos := world.ChunkPos{X: 0, Z: 0}
			w.Acquire(pos)
			t.Cleanup(func() { w.Release(pos) })
			t.Cleanup(w.HoldEntityTicking(pos))

			w.SetBlock(0, 64, 0, hopperState)
			be := w.EnsureBlockEntity(0, 64, 0, world.KindHopper)
			w.TickTime()

			s.blockEntityTickSeam = func(cur *world.BlockEntity) {
				if cur == be {
					tc.spawn(t, s, w)
				}
			}
			s.tickBlockEntities()
			s.blockEntityTickSeam = nil

			if got := be.Slots[0]; got.ItemID != dirt.ItemID || got.Count != 1 {
				t.Fatalf("hopper slot 0 after same-tick spawn = %+v, want %+v", got, dirt)
			}
		})
	}
}
