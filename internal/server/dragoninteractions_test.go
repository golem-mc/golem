package server

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func dragonInteractionServer(t *testing.T) *Server {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { clear(s.clients); s.Shutdown() })
	return s
}

func TestDragonSpikeCrystalCensus(t *testing.T) {
	s := dragonInteractionServer(t)
	w := s.dimensionByName(world.EndName).w
	f := s.dragonFight
	b := f.spikeBoxes[0]
	// Top boxes span the build height; neither a low crystal inside a spike's
	// columns nor one intersecting its boundary is excluded by a height guess.
	for _, p := range [][3]float64{{(b.MinX + b.MaxX) / 2, 10, (b.MinZ + b.MaxZ) / 2}, {b.MaxX + 0.5, 200, (b.MinZ + b.MaxZ) / 2}, {0, 128, 0}} {
		s.endCrystals = append(s.endCrystals, s.newEndCrystalEntity(w, p[0], p[1], p[2]))
	}
	s.endCrystals = append(s.endCrystals, s.newEndCrystalEntity(s.world, (b.MinX+b.MaxX)/2, 100, (b.MinZ+b.MaxZ)/2))
	for range 99 {
		s.countDragonCrystals(w, f)
	}
	if f.crystals != 0 {
		t.Fatal("census ran early")
	}
	s.countDragonCrystals(w, f)
	if f.crystals != 2 || f.crystalTicks != 0 {
		t.Fatalf("census=%d timer=%d", f.crystals, f.crystalTicks)
	}
	s.endCrystals[0].crystal.Dead = true
	for range 99 {
		s.countDragonCrystals(w, f)
	}
	if f.crystals != 2 {
		t.Fatal("census changed before next scan")
	}
	s.countDragonCrystals(w, f)
	if f.crystals != 1 {
		t.Fatalf("dead crystal counted: %d", f.crystals)
	}
}

func TestDragonExplosionParts(t *testing.T) {
	typ := int32(entity.TypeEnderDragon)
	s := dragonInteractionServer(t)
	dim := s.dimensionByName(world.EndName)
	m := entity.NewMob(entity.TypeEnderDragon, 10, 0, 128, 0, 1)
	dim.mobs.herd = append(dim.mobs.herd, m)
	t.Cleanup(func() { dim.mobs.herd = nil })
	m.Tick(dim.w, nil) // positions the parts
	for _, part := range []int{entity.DragonHead, entity.DragonBody, 6} {
		m.Health, m.InvulnTicks = 200, 0
		p := entity.DragonPartTarget(m, typ, part)
		x, y, z := (p.MinX+p.MaxX)/2, p.MinY, (p.MinZ+p.MaxZ)/2
		b := blast{w: dim.w, x: x, y: y, z: z, radius: 0.1, damageType: rules.DamageTypeExplosion}
		in := blastInputs{blocks: b.w.SnapshotBlocks(), at: rules.Blast{X: x, Y: y, Z: z, Radius: 0.1}}
		s.blastMobs(b, &in, &blastEffects{})
		in.blocks.Release()
		damage := rules.ExplosionDamage(0, 1, 0.1)
		if part != entity.DragonHead {
			damage = damage/4 + math.Min(damage, 1)
		}
		if m.Health != 200-damage {
			t.Fatalf("part=%d health=%v want=%v", part, m.Health, 200-damage)
		}
	}
	for _, cx := range []int32{-1, 0} {
		p := world.ChunkPos{X: cx, Z: -1}
		dim.w.Acquire(p)
		t.Cleanup(func() { dim.w.Release(p) })
	}
	b := blast{w: dim.w, x: 0, y: 127, z: -8.5, radius: 1.1, damageType: rules.DamageTypeExplosion}
	in := blastInputs{blocks: b.w.SnapshotBlocks(), at: rules.Blast{X: b.x, Y: b.y, Z: b.z, Radius: b.radius}}
	m.Health, m.InvulnTicks = 200, 0
	s.blastMobs(b, &in, &blastEffects{})
	exposed := m.Health
	in.blocks.Release()
	for x := int32(-1); x <= 0; x++ {
		for y := int32(127); y <= 128; y++ {
			dim.w.SetBlock(x, y, -8, registry.BlockStone.DefaultState())
		}
	}
	m.Health, m.InvulnTicks = 200, 0
	in.blocks = b.w.SnapshotBlocks()
	defer in.blocks.Release()
	s.blastMobs(b, &in, &blastEffects{})
	if m.Health != 199 || exposed >= m.Health {
		t.Fatalf("head exposure open=%v occluded=%v", exposed, m.Health)
	}

}

func TestDragonArrowTargetsAndDamage(t *testing.T) {
	typ := int32(entity.TypeEnderDragon)
	s := dragonInteractionServer(t)
	dim := s.dimensionByName(world.EndName)
	m := entity.NewMob(entity.TypeEnderDragon, 10, 0, 128, 0, 1)
	dim.mobs.herd = append(dim.mobs.herd, m)
	t.Cleanup(func() { dim.mobs.herd = nil })
	for _, part := range []int{entity.DragonHead, entity.DragonBody} {
		m.Health, m.InvulnTicks = 200, 0
		it := &arrowEntity{w: dim.w, arrow: entity.NewArrow(30, 0, 128, 0, 0, 0, 1, false, 2, entity.PickupAllowed, 40, 1)}
		in := newArrowTickInputs()
		in.watchers[dim.w] = []arrowWatcher{{c: &client{player: &player.Player{EntityID: 40}}}}
		var bodies arrowBodies
		targets, _ := arrowTargets(it, dim, &in, &arrowHangingView{}, &bodies)
		if len(targets) != 10 || bodies.mobs[m.ID+1+int32(part)] != m {
			t.Fatal("dragon parts not in arrow targets")
		}
		ev := entity.ArrowEvent{Target: entity.DragonPartTarget(m, typ, part), Damage: 8}
		s.arrowHurtMob(it, dim, m, ev, true, false, nil)
		want := 197.0
		if part == entity.DragonHead {
			want = 192
		}
		if m.Health != want || !it.arrow.Dead {
			t.Fatalf("part=%d health=%v arrow dead=%v", part, m.Health, it.arrow.Dead)
		}
	}
}

func TestDragonCrystalPlayerAttribution(t *testing.T) {
	s := dragonInteractionServer(t)
	s.world.SetDifficulty(rules.DifficultyNormal)
	dim := s.dimensionByName(world.EndName)
	m := entity.NewMob(entity.TypeEnderDragon, 10, 0, 128, 0, 1)
	dim.mobs.herd = append(dim.mobs.herd, m)
	dim.mobs.dragons.Store(1)
	t.Cleanup(func() { dim.mobs.herd = nil; dim.mobs.dragons.Store(0) })
	for _, p := range []struct {
		id   int32
		x    float64
		mode int32
	}{{40, 100, protocol.GameModeSurvival}, {41, 200, protocol.GameModeCreative}, {42, 1, protocol.GameModeSpectator}} {
		pl := &player.Player{UUID: uuid.New(), EntityID: p.id}
		pl.SetPosition(player.Position{X: p.x, Y: 128})
		pl.UpdateVitals(func(v *player.Vitals) { v.GameMode = p.mode })
		c := &client{srv: s, player: pl}
		c.dim.Store(dim)
		s.assignPlayerSlot(c)
		s.clients[pl.UUID] = c
	}
	for _, source := range []int32{0, 41, -1} {
		if source == -1 {
			dim.w.SetDifficulty(rules.DifficultyPeaceful)
		}
		m.Health, m.InvulnTicks = 200, 0
		crystal := s.newEndCrystalEntity(dim.w, 0, 128, 0)
		for range 100 {
			m.DragonCrystals([]entity.EndCrystal{*crystal.crystal})
		}
		s.destroyEndCrystal(crystal, source)
		want := int32(40)
		if source != 0 {
			want = source
		}
		if source == -1 {
			want = 0
		}
		if m.Health != 190 || m.AttackerID() != want || m.LastHurtByPlayer() != (want != 0) {
			t.Fatalf("source=%d health=%v attacker=%d player=%v", source, m.Health, m.AttackerID(), m.LastHurtByPlayer())
		}
	}
}

func TestDragonContactDamageAndPush(t *testing.T) {
	s := dragonInteractionServer(t)
	dim := s.dimensionByName(world.EndName)
	dragon := entity.NewMob(entity.TypeEnderDragon, 10, 0, 128, 0, 1)
	cow := entity.NewMob(entity.TypeCow, 20, 8, 128, 0, 1)
	dim.mobs.herd = append(dim.mobs.herd, dragon, cow)
	t.Cleanup(func() { dim.mobs.herd = nil })
	for _, damage := range []float64{5, 10, 0} {
		cow.Health, cow.InvulnTicks = 20, 0
		cow.VX, cow.VY, cow.VZ = 0, 0, 0
		cow.Invulnerable = damage == 0
		s.landingMobBlow(mobStrike{w: dim.w, mob: dragon, victimID: cow.ID, damage: damage, dragonContact: true, roar: true, contactPush: [3]float64{1, 0.2, 0}})
		if cow.Health != 20-damage || cow.VX == 0 || cow.VY == 0 {
			t.Fatalf("damage=%v health=%v velocity=%v,%v", damage, cow.Health, cow.VX, cow.VY)
		}
	}
}

func TestDragonWallRemoval(t *testing.T) {
	s := dragonInteractionServer(t)
	w := s.world
	p := world.ChunkPos{}
	w.Acquire(p)
	t.Cleanup(func() { w.Release(p) })
	wet, ok := registry.WithBool(registry.BlockOakStairs.DefaultState(), registry.PropWaterlogged, true)
	if !ok {
		t.Fatal("stairs lack waterlogged property")
	}
	for _, state := range []uint16{registry.BlockStone.DefaultState(), wet} {
		w.SetBlock(0, 128, 0, state)
		b := mobBlockBreak{w: w, x: 0, y: 128, z: 0, remove: true}
		if !s.mobBrokeBlock(b) {
			t.Fatal("removal reported no change")
		}
		if got := w.BlockAt(0, 128, 0); got != world.RemovalLeaves(state) || len(s.items) != 0 {
			t.Fatalf("removed state=%d left=%d drops=%d", state, got, len(s.items))
		}
		if s.mobBrokeBlock(b) {
			t.Fatal("repeated removal reported a change")
		}
	}
}

func TestDragonContactPlayerSnapshot(t *testing.T) {
	s := dragonInteractionServer(t)
	p := &player.Player{UUID: uuid.New(), EntityID: 40}
	p.UpdateVitals(func(v *player.Vitals) { v.GameMode = protocol.GameModeCreative; v.AgeTicks = 30 })
	c := &client{srv: s, player: p}
	c.dim.Store(s.allDims()[0])
	s.assignPlayerSlot(c)
	s.clients[p.UUID] = c
	in := s.resetMobTickInputs()
	s.readMobTickInputs(in)
	got := in.byEntityID[p.EntityID]
	if !got.Creative || got.Age != 30 {
		t.Fatalf("creative contact snapshot=%+v", got)
	}
}
