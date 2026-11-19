package server

import (
	"math"
	"reflect"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/dragonfight"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// dragonFight is the End's non-command boss event and saved fight state. Its
// mutex precedes mobsMu; packets are sent after the simulation locks release.
// vanilla: EnderDragonFight
type dragonFight struct {
	mu                     sync.Mutex
	data                   nbt.Compound
	bar                    world.BossBar
	dragon                 uuid.UUID
	scan, killed           bool
	tick, unseen           int
	crystalTicks, crystals int
	spikeBoxes             [10]world.Box
	held                   []world.ChunkPos
	// loading is the arena chunks still on the generation pool; the fight
	// waits for them all. vanilla: EnderDragonFight.isArenaLoaded
	loading []arenaChunk
	ritual  dragonfight.Ritual
}

type arenaChunk struct {
	pos world.ChunkPos
	ch  <-chan *world.Chunk
}

func (s *Server) loadDragonFight() {
	w := s.dimensionByName(world.EndName).w
	data := w.ReadDragonFight()
	if data == nil {
		data = nbt.Compound{}
	}
	if _, ok := data["previously_killed"]; !ok {
		data["previously_killed"] = nbt.Byte(0)
	}
	if _, ok := data["respawn_time"]; !ok {
		data["respawn_time"] = nbt.Int(0)
	}
	f := &dragonFight{data: data, scan: true, bar: world.BossBar{Wire: uuid.New(), Name: protocol.TextTag(protocol.Text{Translate: "entity.minecraft.ender_dragon"}), Max: barScale, Value: barScale, Visible: true, PlayMusic: true, CreateFog: true}}
	if _, ok := data["needs_state_scanning"]; ok {
		f.scan = nbt.BoolField(data, "needs_state_scanning")
	}
	f.killed = nbt.BoolField(data, "dragon_killed")
	f.dragon, _ = nbt.UUIDField(data, "dragon_uuid")
	if gateways, ok := nbt.Get[nbt.List](data, "gateways"); !ok || len(gateways.Items) == 0 {
		order := make([]int, 20)
		for i := range order {
			order[i] = i
		}
		rng := levelgen.NewLegacyRandom(s.world.SeedValue())
		for i := len(order); i > 1; i-- {
			j := int(rng.NextIntN(int32(i)))
			order[i-1], order[j] = order[j], order[i-1]
		}
		list := nbt.List{Elem: nbt.TypeInt}
		for _, i := range order {
			list.Items = append(list.Items, nbt.Int(i))
		}
		data["gateways"] = list
	}
	f.ritual.Load(data)
	f.spikeBoxes = world.EndSpikeBoxes(s.world.SeedValue())
	s.dragonFight = f
}

func (s *Server) saveDragonFight() {
	f := s.dragonFight
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ritual.Save(f.data)
	f.data["needs_state_scanning"] = boolTag(f.scan)
	f.data["dragon_killed"] = boolTag(f.killed)
	if f.dragon != uuid.Nil {
		f.data["dragon_uuid"] = nbt.UUIDToIntArray(f.dragon)
	}
	s.dimensionByName(world.EndName).w.WriteDragonFight(f.data)
}

// tickDragonFight updates membership every second and reconciles the boss
// with its saved identity. No players means no fight work or arena ticket.
// vanilla: EnderDragonFight.tick, updatePlayers, scanState, findOrCreateDragon
func (s *Server) tickDragonFight() {
	f := s.dragonFight
	if f == nil {
		return
	}
	dim := s.dimensionByName(world.EndName)
	f.mu.Lock()
	var ritual dragonRitualEffect
	defer func() { f.mu.Unlock(); s.applyDragonRitual(ritual) }()
	f.tick++
	before, renamed := f.bar, false
	if f.tick%20 == 0 {
		f.bar.Players = nil
		s.mu.RLock()
		for _, c := range s.clients {
			if c.theWorld() != dim.w || !c.Alive() {
				continue
			}
			p := c.player.Position()
			if p.X*p.X+(p.Y-128)*(p.Y-128)+p.Z*p.Z <= 192*192 {
				f.bar.Players = append(f.bar.Players, c.player.UUID)
			}
		}
		s.mu.RUnlock()
	}
	if len(f.bar.Players) > 0 && len(f.held)+len(f.loading) == 0 {
		// The fight owns the arena while watched, so a dragon cannot be saved
		// and forgotten when it flies beyond a player's chunk view. Its 289
		// chunks generate on the pool: in the tick they would stall it.
		for x := int32(-8); x <= 8; x++ {
			for z := int32(-8); z <= 8; z++ {
				pos := world.ChunkPos{X: x, Z: z}
				f.loading = append(f.loading, arenaChunk{pos, dim.w.AcquireAsync(pos)})
			}
		}
	}
	f.loading = slices.DeleteFunc(f.loading, func(a arenaChunk) bool {
		select {
		case <-a.ch:
		default:
			return false
		}
		f.held = append(f.held, a.pos)
		if wake, ok := s.wakeChunkOnce(dim, a.pos); ok {
			s.registerWake(wake)
		}
		return true
	})
	if len(f.loading) > 0 {
		// Neither scan nor spawn into a partly loaded arena, nor release a
		// hold that has yet to arrive.
	} else if len(f.bar.Players) > 0 {
		scanning := f.scan
		if f.scan {
			active := false
			// An end portal's entity is carried, never a modelled kind.
			lo, hi := [3]int32{-128, dim.w.MinY(), -128}, [3]int32{143, dim.w.MaxY(), 143}
			if len(dim.w.CarriedOfKind("minecraft:end_portal", lo, hi)) > 0 {
				active = true
			}
			f.data["previously_killed"] = boolTag(active)
			f.killed = active
			if _, ok := f.data["exit_portal_location"]; !ok {
				for y := dim.w.MaxY() - 1; y >= dim.w.MinY(); y-- {
					if registry.BlockOf(dim.w.BlockAt(0, y, 0)) == registry.BlockBedrock && world.EndPodiumMatches(0, y-3, 0, dim.w.BlockAt) {
						f.data["exit_portal_location"] = nbt.IntArray{0, y - 3, 0}
						break
					}
				}
			}
			if _, ok := f.data["exit_portal_location"]; !ok && !active {
				s.placeDragonPodium(dim.w, f, false)
			}
			f.scan = false
		}
		if f.ritual.Stage != "" {
			ritual = s.stepDragonRespawn(dim.w, f)
		}
		if !f.killed {
			s.countDragonCrystals(dim.w, f)
		}
		s.lockTick(&s.mobsMu)
		// The fight's own dragon by its UUID, else any other in the herd.
		var dragon *mobEntity
		live := func(m *entity.Mob) bool { return m.Type == entity.TypeEnderDragon && !m.Dead && !m.Discarded() }
		if m := dim.mobs.byUUID[f.dragon]; m != nil && live(m) {
			dragon = dim.mobs.book[m]
		} else if dim.mobs.dragons.Load() != 0 {
			for _, m := range dim.mobs.herd {
				if live(m) {
					dragon = dim.mobs.book[m]
					break
				}
			}
		}
		if scanning && dragon != nil {
			f.killed = false
			if !nbt.BoolField(f.data, "previously_killed") {
				dragon.mob.Discard()
				dragon = nil
				f.dragon = uuid.Nil
			}
		}
		if dragon != nil && (dragon.uuid == f.dragon || f.dragon == uuid.Nil || f.unseen >= 1200) {
			f.dragon = dragon.uuid
			dragon.mob.SetDragonCrystalCount(f.crystals)
			f.unseen = 0
			f.bar.Value = int32(math.Round(dragon.mob.Health / dragon.mob.MaxHealth * barScale))
			// Only a dragon with a custom name pays for the comparison.
			if dragon.name != nil && !reflect.DeepEqual(dragon.name, f.bar.Name) {
				f.bar.Name = dragon.name
				renamed = true
			}
		} else {
			dragon = nil
			f.unseen++
		}
		s.unlockTick(&s.mobsMu)
		if !f.killed && dragon == nil && (f.dragon == uuid.Nil || f.unseen >= 1200) {
			id := s.spawnMobArriving(dim, entity.TypeEnderDragon, 0, 128, 0, 0, nil, false, "", false, func(ent *mobEntity) {
				ent.mob.SetDragonPhase(entity.DragonHolding, 0)
				ent.mob.Yaw = float32(ent.mob.Rand.Float64() * 360)
			})
			s.lockTick(&s.mobsMu)
			f.dragon = s.mobByEntityIDLocked(dim.w, id).uuid
			s.unlockTick(&s.mobsMu)
			f.unseen = 0
		}
	} else if len(f.held) > 0 {
		for _, pos := range f.held {
			dim.w.Release(pos)
		}
		f.held = nil
	}
	f.bar.Visible = !f.killed
	// Reconcile visibility and membership before deltas, so a joiner always
	// receives ADD before a progress or title update.
	for _, p := range before.Players {
		if before.Visible && (!f.bar.Visible || !slices.Contains(f.bar.Players, p)) {
			s.sendBossEvent([]uuid.UUID{p}, before, protocol.BossEventRemove)
		}
	}
	var stayed []uuid.UUID
	for _, p := range f.bar.Players {
		if !f.bar.Visible {
			continue
		}
		if !before.Visible || !slices.Contains(before.Players, p) {
			s.sendBossEvent([]uuid.UUID{p}, f.bar, protocol.BossEventAdd)
		} else {
			stayed = append(stayed, p)
		}
	}
	if before.Value != f.bar.Value {
		s.sendBossEvent(stayed, f.bar, protocol.BossEventProgress)
	}
	if renamed {
		s.sendBossEvent(stayed, f.bar, protocol.BossEventName)
	}
}

// tickDragonCrystals reads the crystal list before taking mobsMu. An End
// without a dragon costs one atomic read.
func (s *Server) tickDragonCrystals() {
	dim := s.dimensionByName(world.EndName)
	if dim.mobs.dragons.Load() == 0 {
		return
	}
	s.lockTick(&s.endCrystalsMu)
	var crystals []entity.EndCrystal
	for _, ent := range s.endCrystals {
		if ent.w == dim.w && !ent.crystal.Dead {
			crystals = append(crystals, *ent.crystal)
		}
	}
	s.unlockTick(&s.endCrystalsMu)
	s.lockTick(&s.mobsMu)
	for _, m := range dim.mobs.herd {
		if m.Type == entity.TypeEnderDragon && !m.Dead {
			m.DragonCrystals(crystals)
		}
	}
	s.unlockTick(&s.mobsMu)
}

// countDragonCrystals runs only while an observed fight's arena is loaded.
// vanilla: EnderDragonFight.tick, updateCrystalCount
func (s *Server) countDragonCrystals(w *world.World, f *dragonFight) {
	f.crystalTicks++
	if f.crystalTicks < 100 {
		return
	}
	s.refreshDragonCrystalCount(w, f)
}

func (s *Server) refreshDragonCrystalCount(w *world.World, f *dragonFight) {
	f.crystalTicks, f.crystals = 0, 0
	s.endCrystalsMu.Lock()
	defer s.endCrystalsMu.Unlock()
	for _, ent := range s.endCrystals {
		if ent.w != w || ent.crystal.Dead {
			continue
		}
		x0, y0, z0, x1, y1, z1 := ent.crystal.BlastBox()
		for _, b := range f.spikeBoxes {
			if x1 > b.MinX && x0 < b.MaxX && y1 > b.MinY && y0 < b.MaxY && z1 > b.MinZ && z0 < b.MaxZ {
				f.crystals++
			}
		}
	}
}
