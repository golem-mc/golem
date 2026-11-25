package server

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkTNTFuseBlast measures converting an expired fuse into a blast,
// including the game-rule gate, without running the blast itself.
func BenchmarkTNTFuseBlast(b *testing.B) {
	for _, enabled := range []bool{true, false} {
		name := "enabled"
		if !enabled {
			name = "disabled"
		}
		b.Run(name, func(b *testing.B) {
			site := newBlastSite(b, [6]int32{0, 0, 64, 64, 0, 0})
			site.w.SetBoolRule(world.RuleTNTExplodes, enabled)
			it := site.s.newTNTEntity(site.w, 0.5, 64, 0.5, 0, 0, uuid.Nil, false)
			ev := entity.TNTEvent{Kind: entity.TntExploded, X: 0.5, Y: 64, Z: 0.5, Radius: entity.TNTExplosionPower}
			blasts := make([]blast, 0, 1)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				blasts = site.s.tntBlast(it, ev, blasts[:0])
			}
		})
	}
}

// BenchmarkTNTStoneInputs measures a radius-four blast's block ray cast in
// intact stone, including the per-blast reader setup used by the server.
func BenchmarkTNTStoneInputs(b *testing.B) {
	site := newBlastSite(b, [6]int32{0, 16, 56, 64, 0, 16})
	blast := blast{w: site.w, x: 8.5, y: 60.5, z: 8.5, radius: 4, rng: site.rng}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		site.s.blastInputs(blast).blocks.Release()
	}
}

// Blast benchmarks. Each iteration runs the same runExplosion the tick loop
// runs a primed TNT's fuse out into, against the flat stone ground the
// black-box tests build on, with the chunks around the crater held the way a
// walking player holds them. The rebuild between iterations (the crater
// refilled, the drops cleared) runs under the timer stopped, so an
// iteration measures one blast in intact ground, not a blast plus the
// crater the last one left and the drops piling up around it.

// benchBlastSite is a benchmark's blast range: the server, the held chunks
// and the snapshot of the ground a blast chews, which the rebuild puts back
// cell by cell.
type benchBlastSite struct {
	s *Server
	w *world.World
	// rng is the blast's dice, re-seeded per blast: a primed TNT rolls its
	// own source, which it does not make anew for the blast.
	rng *rand.Rand

	ground []world.BlockChange // the snapshot, one entry per cell
}

// newBlastSite builds a server on the flat stone ground the black-box tests
// build on and snapshots the box given, holding every chunk the box, a
// blast's rays and the blast's relight reach touch.
func newBlastSite(b *testing.B, box [6]int32) *benchBlastSite {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = b.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		b.Fatalf("create server: %v", err)
	}
	w := s.world

	// A blast in the middle of the box reaches about five cells out and
	// its relight fifteen, so the held chunk block covers the box padded
	// by the wider of the two.
	const pad = 16
	holds := []world.ChunkPos{}
	for cx := world.ChunkOf(float64(box[0] - pad)); cx <= world.ChunkOf(float64(box[1]+pad)); cx++ {
		for cz := world.ChunkOf(float64(box[4] - pad)); cz <= world.ChunkOf(float64(box[5]+pad)); cz++ {
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

	site := &benchBlastSite{s: s, w: w, rng: rand.New(rand.NewSource(1))}
	for y := box[2]; y <= box[3]; y++ {
		for x := box[0]; x <= box[1]; x++ {
			for z := box[4]; z <= box[5]; z++ {
				site.ground = append(site.ground, world.BlockChange{
					X: x, Y: y, Z: z, State: w.BlockAt(x, y, z),
				})
			}
		}
	}
	// The held chunks' light is still being computed on the pool while the
	// snapshot reads; drain it, or the first iteration's window waits on
	// leftover chunk-set-up work instead of on its own blast.
	site.w.FlushLighting()
	return site
}

// rebuild refills the ground a blast chewed and clears the drops it
// left behind, so the next blast runs against the world the last one started
// from.
func (site *benchBlastSite) rebuild() {
	for _, ch := range site.ground {
		if site.w.BlockAt(ch.X, ch.Y, ch.Z) != ch.State {
			site.w.SetBlock(ch.X, ch.Y, ch.Z, ch.State)
		}
	}
	site.s.itemsMu.Lock()
	for _, it := range site.s.items {
		it.item.Dead = true
	}
	site.s.items = nil
	site.s.itemsMu.Unlock()
}

// runBlast runs one primed TNT's blast: the fuse out event's own centre
// (the body's feet plus the height share vanilla's explode adds —
// PrimedTnt's getY(0.0625)), radius 4, the tnt decay rules read the same
// way the tick loop reads them. The light work the blast schedules on the
// worker pool is waited on inside the window: a tick that fires a blast
// pays for it before the light is right again, and leaving it out would
// have the previous iteration's backlog bleed into the next one's time.
func (site *benchBlastSite) runBlast(x, z float64) {
	site.rng.Seed(1)
	site.s.runExplosion(blast{
		w: site.w,
		x: x, y: world.GroundLevel + entity.TNTHeight/16, z: z,
		radius:     entity.TNTExplosionPower,
		directType: "minecraft:tnt",
		damageType: rules.DamageTypeExplosion,
		rng:        site.rng,
		// A primed TNT's blast is a TNT's: the tnt decay rule's.
		interaction: rules.TNTExplosionInteraction(site.s.world),
	})
	site.w.FlushLighting()
}

// BenchmarkTNTExplosion is the cost of the blast one primed TNT leaves the
// tick loop to run, alone and as a chain of twenty — one blast into each
// adjacent spot, the way a lit TNT wall's short fuses land in the craters
// of their neighbours — with the ground rebuilt between iterations.
func BenchmarkTNTExplosion(b *testing.B) {
	b.Run("one", func(b *testing.B) {
		site := newBlastSite(b, [6]int32{0, 16, 56, 72, 0, 16})
		b.ResetTimer()
		b.ReportAllocs()
		for range b.N {
			b.StopTimer()
			site.rebuild()
			b.StartTimer()
			site.runBlast(8.5, 8.5)
		}
	})

	b.Run("chain20", func(b *testing.B) {
		site := newBlastSite(b, [6]int32{0, 16, 56, 72, 0, 32})
		b.ResetTimer()
		b.ReportAllocs()
		for range b.N {
			b.StopTimer()
			site.rebuild()
			b.StartTimer()
			for i := range 20 {
				site.runBlast(8.5, 8.5+float64(i))
			}
		}
	})
}

// BenchmarkTNTExplosionTeamed is BenchmarkTNTExplosion's single blast set
// off through tntBlast, with sixteen items on a team about the crater and a
// hundred more members on the scoreboard: the TNT on no team, on the
// items' team with friendly fire enabled, and with it disabled.
func BenchmarkTNTExplosionTeamed(b *testing.B) {
	for _, mode := range []string{"unteamed", "friendlyFire", "friendlyFireOff"} {
		b.Run(mode, func(b *testing.B) {
			site := newBlastSite(b, [6]int32{0, 16, 56, 72, 0, 16})
			board := site.s.scoreboard
			board.AddTeam("t", nbt.String("t"))
			board.UpdateTeam("t", func(t *scoreboard.Team) { t.FriendlyFire = mode != "friendlyFireOff" })
			members := make([]string, 100)
			for i := range members {
				members[i] = fmt.Sprint("player", i)
			}
			board.JoinTeam("t", members)
			tnt := site.s.newTNTEntity(site.w, 8.5, world.GroundLevel, 8.5, 0, 0, uuid.Nil, false)
			if mode != "unteamed" {
				board.JoinTeam("t", []string{tnt.uuid.String()})
			}
			ev := entity.TNTEvent{Kind: entity.TntExploded, X: 8.5, Y: world.GroundLevel + entity.TNTHeight/16, Z: 8.5,
				Radius: entity.TNTExplosionPower}
			stack := protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}
			var victims []string
			b.ResetTimer()
			b.ReportAllocs()
			for range b.N {
				b.StopTimer()
				board.LeaveTeam(victims)
				site.rebuild()
				victims = victims[:0]
				for i := range 16 {
					angle := float64(i) * math.Pi / 8
					it := site.s.spawnItem(site.w, stack, 8.5+3*math.Cos(angle), world.GroundLevel, 8.5+3*math.Sin(angle), 0, 0, 0, 10)
					victims = append(victims, it.uuid.String())
				}
				board.JoinTeam("t", victims)
				site.rng.Seed(1)
				b.StartTimer()
				blasts := site.s.tntBlast(tnt, ev, nil)
				blasts[0].rng = site.rng
				site.s.runExplosion(blasts[0])
				site.w.FlushLighting()
			}
		})
	}
}

// BenchmarkBlastInvulnerableHanging measures the hanging-body pass with an
// invulnerable painting, knot and cushion behind an intact obsidian wall.
func BenchmarkBlastInvulnerableHanging(b *testing.B) {
	site := newBlastSite(b, [6]int32{0, 7, 63, 67, 0, 6})
	for x := int32(0); x <= 7; x++ {
		for y := int32(64); y <= 67; y++ {
			site.w.SetBlock(x, y, 3, registry.BlockObsidian.DefaultState())
		}
	}
	site.s.paintings = []*paintingEntity{{w: site.w, invulnerable: true, minX: 1, minY: 64, minZ: 4.9375, maxX: 2, maxY: 65, maxZ: 5}}
	site.s.knots = []*knotEntity{{w: site.w, invulnerable: true, x: 5, y: 64, z: 4}}
	site.s.fixtures = []*fixtureEntity{{w: site.w, kind: entity.FixtureCushion, x: 3.5, y: 64, z: 4.5, tag: nbt.Compound{"Invulnerable": nbt.Byte(1)}}}
	explosion := blast{w: site.w, x: 3.5, y: 65.5, z: 1.5, radius: 4, interaction: rules.TNTExplosionInteraction(site.w)}
	in := blastInputs{blocks: site.w.SnapshotBlocks(), at: rules.Blast{X: explosion.x, Y: explosion.y, Z: explosion.z, Radius: explosion.radius}}
	defer in.blocks.Release()
	site.w.FlushLighting()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var fx blastEffects
		site.s.blastHanging(explosion, &in, &fx)
	}
}

// BenchmarkTNTStoneExplosion includes the removal, drop and lighting work
// of one radius-four TNT blast surrounded by intact stone.
func BenchmarkTNTStoneExplosion(b *testing.B) {
	site := newBlastSite(b, [6]int32{0, 16, 56, 64, 0, 16})
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		site.rebuild()
		site.rng.Seed(1)
		b.StartTimer()
		site.s.runExplosion(blast{
			w: site.w, x: 8.5, y: 60.5, z: 8.5, radius: entity.TNTExplosionPower,
			directType: "minecraft:tnt", damageType: rules.DamageTypeExplosion,
			rng: site.rng, interaction: rules.TNTExplosionInteraction(site.w),
		})
		site.w.FlushLighting()
	}
}

// BenchmarkTNTHiveExplosion includes releasing an occupant and preparing the hive's loot.
func BenchmarkTNTHiveExplosion(b *testing.B) {
	site := newBlastSite(b, [6]int32{3, 13, 59, 68, 3, 13})
	const x, y, z = 9, world.GroundLevel, 8
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		site.rebuild()
		for _, ent := range site.s.herdOf(site.w).book {
			site.s.discardSpawned(ent)
		}
		site.w.SetBlock(x, y, z, registry.BlockBeehive.DefaultState())
		site.w.SetBlockEntityTag(x, y, z, "minecraft:beehive", nbt.Compound{
			"bees": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
				"entity_data":       nbt.Compound{"id": nbt.String("minecraft:bee")},
				"min_ticks_in_hive": nbt.Int(600),
			}}},
		})
		b.StartTimer()
		site.runBlast(8.5, 8.5)
		b.StopTimer()
		if site.s.MobCount() != 1 {
			b.Fatal("the blast did not release one bee")
		}
		b.StartTimer()
	}
}

// BenchmarkTNTBlastCrowd is one blast among many primed TNT spread over a
// plane, as in a chain through a large cube.
func BenchmarkTNTBlastCrowd(b *testing.B) {
	site := newBlastSite(b, [6]int32{0, 16, 56, 72, 0, 16})
	for i := range 20000 {
		site.s.tnts = append(site.s.tnts, site.s.newTNTEntity(site.w, float64(i%40)*0.5+0.5, 64+float64(i/1600)*0.5, float64(i%1600/40)*0.5+0.5, 80, 0, uuid.Nil, false))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		site.rebuild()
		site.s.tntBlasts.build(site.s.tnts)
		b.StartTimer()
		site.runBlast(8.5, 8.5)
		site.s.tntBlasts.drop()
	}
}
