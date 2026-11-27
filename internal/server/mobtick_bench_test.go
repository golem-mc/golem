package server

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkTickMobs runs the server's whole mob tick, the herd's step and
// the effects applied after the mob lock, over two hundred mobs of the
// natural generation's mix on the flat stone ground of held chunks, no
// players in the world. The monsters are persistent, so the absence of
// players spares them the despawn.
func BenchmarkTickMobs(b *testing.B) {
	mix := []mobMix{
		{entity.TypeCow, 45}, {entity.TypePig, 45}, {entity.TypeSheep, 45}, {entity.TypeChicken, 35},
		{entity.TypeZombie, 8}, {entity.TypeSkeleton, 8}, {entity.TypeCreeper, 7}, {entity.TypeSpider, 7},
	}
	benchTickMobs(b, mix, 96, 180)
}

// BenchmarkTickMobsCrowd is BenchmarkTickMobs over three thousand mobs, a
// third of them zombies, in the middle 110 blocks.
func BenchmarkTickMobsCrowd(b *testing.B) {
	mix := []mobMix{
		{entity.TypeCow, 600}, {entity.TypePig, 600}, {entity.TypeSheep, 600}, {entity.TypeChicken, 200},
		{entity.TypeZombie, 1000},
	}
	benchTickMobs(b, mix, 110, 2800)
}

// BenchmarkTickMobsAmongVehicles is BenchmarkTickMobs with a hundred
// minecarts and a hundred boats parked among the mobs, which their cramming
// counts.
func BenchmarkTickMobsAmongVehicles(b *testing.B) {
	mix := []mobMix{
		{entity.TypeCow, 45}, {entity.TypePig, 45}, {entity.TypeSheep, 45}, {entity.TypeChicken, 35},
		{entity.TypeZombie, 8}, {entity.TypeSkeleton, 8}, {entity.TypeCreeper, 7}, {entity.TypeSpider, 7},
	}
	benchTickMobs(b, mix, 96, 180, func(s *Server, rng *rand.Rand) {
		y := float64(world.GroundLevel)
		for range 100 {
			s.spawnMinecart(s.world, entity.MinecartPlain, (rng.Float64()-0.5)*96, y, (rng.Float64()-0.5)*96)
			s.spawnBoat(s.world, "minecraft:oak_boat", (rng.Float64()-0.5)*96, y, (rng.Float64()-0.5)*96, 0, customName{})
		}
	})
}

// BenchmarkTickMobsTicketed is BenchmarkTickMobs with a plugin ticket held
// over the herd and four players in it, so each mob checks its chunk against
// the tickets, the players and the forced chunks.
func BenchmarkTickMobsTicketed(b *testing.B) {
	mix := []mobMix{
		{entity.TypeCow, 45}, {entity.TypePig, 45}, {entity.TypeSheep, 45}, {entity.TypeChicken, 35},
		{entity.TypeZombie, 8}, {entity.TypeSkeleton, 8}, {entity.TypeCreeper, 7}, {entity.TypeSpider, 7},
	}
	benchTickMobs(b, mix, 96, 180, func(s *Server, rng *rand.Rand) {
		dim := s.allDims()[0]
		dim.w.AddTicket(world.ChunkPos{X: -4, Z: -4}, 2)
		for range 4 {
			p := &player.Player{UUID: uuid.New(), EntityID: s.nextEntity()}
			p.SetPosition(player.Position{X: (rng.Float64() - 0.5) * 96, Y: float64(world.GroundLevel), Z: (rng.Float64() - 0.5) * 96})
			c := &client{srv: s, player: p}
			c.dim.Store(dim)
			s.mu.Lock()
			s.assignPlayerSlot(c)
			s.clients[p.UUID] = c
			s.mu.Unlock()
		}
		// The players have no connection to shut down with the server.
		b.Cleanup(func() {
			s.mu.Lock()
			clear(s.clients)
			s.mu.Unlock()
		})
	})
}

// BenchmarkTickMobsCalves is a thousand cows in 96 blocks, every second one
// a calf, and a spectator so the herd keeps ticking: each calf looks for a
// parent.
func BenchmarkTickMobsCalves(b *testing.B) {
	benchTickMobs(b, []mobMix{{entity.TypeCow, 1000}}, 96, 900, func(s *Server, rng *rand.Rand) {
		dim := s.allDims()[0]
		s.mobsMu.Lock()
		for i, m := range dim.mobs.herd {
			if i%2 == 0 {
				m.SetBaby(true)
				m.BabyAge = -1 << 30
			}
		}
		s.mobsMu.Unlock()
		benchMobSpectator(b)(s, rng)
	})
}

// BenchmarkTickMobsCowsInHerd is BenchmarkTickMobsShulkersInHerd without the
// shulkers: the herd and the spectator alone.
func BenchmarkTickMobsCowsInHerd(b *testing.B) {
	benchTickMobs(b, herdMix(0), 96, 2000, benchMobSpectator(b))
}

func BenchmarkTickMobsCalfFollow(b *testing.B) {
	benchTickMobs(b, []mobMix{{entity.TypeCow, 400}, {entity.TypePig, 100}}, 96, 500,
		benchMobSpectator(b), func(s *Server, _ *rand.Rand) {
			s.mobsMu.Lock()
			defer s.mobsMu.Unlock()
			for _, m := range s.allDims()[0].mobs.herd {
				if m.Type == entity.TypeCow && m.ID%2 == 0 {
					m.SetBaby(true)
				}
			}
		})
}

// BenchmarkTickMobsShulkersInHerd is BenchmarkTickMobs with 2000 cows and 50
// shulkers added and a spectator, so the herd ticks and each shulker's
// collision and push queries run beside a large herd.
func BenchmarkTickMobsShulkersInHerd(b *testing.B) {
	benchTickMobs(b, herdMix(50), 96, 2000, benchMobSpectator(b))
}

// herdMix is the BenchmarkTickMobs mix plus 2000 cows and shulkers shulkers.
func herdMix(shulkers int) []mobMix {
	return []mobMix{
		{entity.TypeCow, 45 + 2000}, {entity.TypePig, 45}, {entity.TypeSheep, 45}, {entity.TypeChicken, 35},
		{entity.TypeZombie, 8}, {entity.TypeSkeleton, 8}, {entity.TypeCreeper, 7}, {entity.TypeSpider, 7},
		{entity.TypeShulker, shulkers},
	}
}

// BenchmarkTickMobsFrogs ticks a thousand frogs with no tongue targets.
func BenchmarkTickMobsFrogs(b *testing.B) {
	benchTickMobs(b, []mobMix{{entity.TypeFrog, 1000}}, 96, 1000, benchMobSpectator(b))
}

// BenchmarkTickMobsDenseAvoiders packs mobs into a collection area with no threats.
func BenchmarkTickMobsDenseAvoiders(b *testing.B) {
	for _, herd := range []struct {
		name string
		kind entity.MobType
	}{
		{"skeletons", entity.TypeSkeleton},
		{"wolves", entity.TypeWolf},
	} {
		b.Run(herd.name, func(b *testing.B) {
			benchTickMobs(b, []mobMix{{herd.kind, 400}}, 20, 400, benchMobSpectator(b))
		})
	}
}

// BenchmarkTickMobsWolfPack ticks a thousand wolves with a spectator and no prey.
func BenchmarkTickMobsWolfPack(b *testing.B) {
	benchTickMobs(b, []mobMix{{entity.TypeWolf, 1000}}, 96, 1000, benchMobSpectator(b))
}

func benchMobSpectator(b *testing.B) func(*Server, *rand.Rand) {
	return func(s *Server, _ *rand.Rand) {
		p := &player.Player{UUID: uuid.New(), EntityID: s.nextEntity()}
		p.SetPosition(player.Position{Y: float64(world.GroundLevel)})
		p.UpdateVitals(func(v *player.Vitals) { v.GameMode = protocol.GameModeSpectator })
		c := &client{srv: s, player: p}
		c.dim.Store(s.allDims()[0])
		s.mu.Lock()
		s.assignPlayerSlot(c)
		s.clients[p.UUID] = c
		s.mu.Unlock()
		// The spectator has no connection to shut down with the server.
		b.Cleanup(func() {
			s.mu.Lock()
			delete(s.clients, p.UUID)
			s.mu.Unlock()
		})
	}
}

type mobMix struct {
	kind entity.MobType
	n    int
}

// BenchmarkTickFrozenMobTracking tracks two hundred frozen pigs from a nearby watcher.
func BenchmarkTickFrozenMobTracking(b *testing.B) {
	s := benchMobHerd(b, []mobMix{{entity.TypePig, 200}}, 32, 200, benchMobSpectator(b))
	s.tickRate.SetFrozen(true)
	s.tickRate.Tick()
	watchers := s.trackingWatchers()
	b.ReportAllocs()
	for b.Loop() {
		s.tickMobTracking(watchers)
	}
}

// benchTickMobs runs the mob tick over mix spread across span blocks; each
// setup runs after the spawns.
func benchTickMobs(b *testing.B, mix []mobMix, span float64, minHerd int, setups ...func(*Server, *rand.Rand)) {
	s := benchMobHerd(b, mix, span, minHerd, setups...)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickMobs()
	}
	b.StopTimer()
	s.mobsMu.Lock()
	b.ReportMetric(float64(len(s.allDims()[0].mobs.herd)), "mobs")
	s.mobsMu.Unlock()
}

func benchMobHerd(b *testing.B, mix []mobMix, span float64, minHerd int, setups ...func(*Server, *rand.Rand)) *Server {
	b.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = b.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		b.Fatal(err)
	}
	dim := s.allDims()[0]
	w := dim.w
	// 8x8 chunks around the origin, the mobs kept to the middle 96 blocks
	// so a random walk does not carry one into an unheld chunk.
	var holds []world.ChunkPos
	for cx := int32(-4); cx < 4; cx++ {
		for cz := int32(-4); cz < 4; cz++ {
			holds = append(holds, world.ChunkPos{X: cx, Z: cz})
		}
	}
	for _, pos := range holds {
		w.Acquire(pos)
	}
	b.Cleanup(func() {
		for _, pos := range holds {
			w.Release(pos)
		}
		s.Shutdown()
	})
	w.FlushLighting()

	rng := rand.New(rand.NewSource(1544))
	y := float64(world.GroundLevel)
	for _, kind := range mix {
		for range kind.n {
			x := (rng.Float64() - 0.5) * span
			z := (rng.Float64() - 0.5) * span
			id := s.spawnMobInDim(dim, kind.kind, x, y, z, int64(rng.Uint32()), nil)
			s.mobsMu.Lock()
			if ent := s.mobByEntityIDLocked(w, id); ent != nil && ent.mob.Hostile() {
				ent.mob.Persistence = true
			}
			s.mobsMu.Unlock()
		}
	}
	for _, setup := range setups {
		setup(s, rng)
	}
	// The first ticks settle the spawns onto the ground and wake the held
	// chunks' entity records.
	for range 20 {
		s.tickMobs()
	}

	s.mobsMu.Lock()
	herd := len(dim.mobs.herd)
	s.mobsMu.Unlock()
	if herd < minHerd {
		b.Fatalf("%d mobs in the herd after the settling ticks, want at least %d", herd, minHerd)
	}
	return s
}

// BenchmarkTickMobsNoMobs runs the mob tick with no mob in the world: one
// player's view of loaded chunks (21x21) and 2000 item entities lying in it.
func BenchmarkTickMobsNoMobs(b *testing.B) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = b.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		b.Fatal(err)
	}
	s.SetSpawning(false)
	w := s.allDims()[0].w
	var holds []world.ChunkPos
	for cx := int32(-10); cx <= 10; cx++ {
		for cz := int32(-10); cz <= 10; cz++ {
			holds = append(holds, world.ChunkPos{X: cx, Z: cz})
		}
	}
	for _, pos := range holds {
		w.Acquire(pos)
	}
	b.Cleanup(func() {
		for _, pos := range holds {
			w.Release(pos)
		}
		s.Shutdown()
	})
	w.FlushLighting()
	stone, _ := registry.ItemID("minecraft:stone")
	for i := range 2000 {
		s.spawnItem(w, protocol.ItemStack{ItemID: stone, Count: 1},
			float64(i%50-25)+0.5, float64(world.GroundLevel), float64(i/50-20)+0.5, 0, 0, 0, 32767)
	}
	s.tickMobs() // the first tick wakes every chunk
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickMobs()
	}
}

// BenchmarkTickMobsOnSculk runs the mob tick over two hundred pigs without
// AI, each standing on a sculk sensor of its own, so every tick schedules a
// step at each sensor. Pigs, as zombies would burn in the daylight and thin
// the herd as b.N grows.
func BenchmarkTickMobsOnSculk(b *testing.B) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = b.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		b.Fatal(err)
	}
	s.SetSpawning(false)
	dim := s.allDims()[0]
	w := dim.w
	var holds []world.ChunkPos
	for cx := int32(-2); cx < 2; cx++ {
		for cz := int32(-2); cz < 2; cz++ {
			holds = append(holds, world.ChunkPos{X: cx, Z: cz})
		}
	}
	for _, pos := range holds {
		w.Acquire(pos)
	}
	b.Cleanup(func() {
		for _, pos := range holds {
			w.Release(pos)
		}
		s.Shutdown()
	})
	w.FlushLighting()

	sensor := registry.BlockSculkSensor.DefaultState()
	var ids []int32
	// A 20x10 grid, two blocks apart so no pig touches a neighbour's sensor.
	for i := range 200 {
		x, z := int32(i%20)*2-20, int32(i/20)*2-10
		w.SetBlock(x, world.GroundLevel, z, sensor)
		ids = append(ids, s.spawnMobInDim(dim, entity.TypePig, float64(x)+0.5, world.GroundLevel+0.5, float64(z)+0.5, int64(i), nil))
	}
	for _, id := range ids {
		s.SetMobNoAI(id)
	}
	for range 20 {
		s.tickMobs()
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickMobs()
	}
	b.StopTimer()
	s.mobsMu.Lock()
	b.ReportMetric(float64(len(dim.mobs.herd)), "mobs")
	s.mobsMu.Unlock()
}

// BenchmarkTickMobsWatched runs the mob tick over two thousand cows that
// every one of the tracker bench's watching players is told about.
func BenchmarkTickMobsWatched(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	dim := s.allDims()[0]
	ground := float64(world.GroundLevel)
	for i := range 2000 {
		s.spawnMobInDim(dim, entity.TypeCow, float64(i%45)-22, ground, float64(i/45)-22, int64(i), nil)
	}
	for range 5 {
		s.tickMobs()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickMobs()
	}
}

// BenchmarkTickMobsFoxAmongItems times the mob tick over 500 cows and 2000
// dropped items with no pickup delay, spread over 94 blocks, beside one pig, one
// fox or ten dolphins: the items a mob cannot reach should cost nothing.
func BenchmarkTickMobsFoxAmongItems(b *testing.B) {
	for _, extra := range []mobMix{{entity.TypePig, 1}, {entity.TypeFox, 1}, {entity.TypeDolphin, 10}} {
		b.Run(extra.kind.Name(), func(b *testing.B) {
			mix := []mobMix{{entity.TypeCow, 500}, extra}
			s := benchMobHerd(b, mix, 94, 500, benchMobSpectator(b), func(s *Server, rng *rand.Rand) {
				for range 2000 {
					s.SpawnItemEntityDelay((rng.Float64()-0.5)*94, float64(world.GroundLevel), (rng.Float64()-0.5)*94, "dirt", 1, 0)
				}
			})
			b.ReportAllocs()
			for b.Loop() {
				s.tickMobs()
			}
		})
	}
}
