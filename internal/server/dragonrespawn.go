package server

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

type dragonDeathEffect struct {
	w       *world.World
	id      uuid.UUID
	x, y, z float64
	time    int
	silent  bool
}

// discardMob removes the body without tickDeath's animation, drops or
// experience, answering where it stood; false when it is not in a herd.
// vanilla: Entity.discard
func (s *Server) discardMob(ent *mobEntity) (x, y, z float64, ok bool) {
	s.mobsMu.Lock()
	h := s.herdOf(ent.w)
	if h.book[ent.mob] != ent {
		s.mobsMu.Unlock()
		return 0, 0, 0, false
	}
	m := ent.mob
	m.Kill()
	h.remove(m)
	h.forget(m)
	x, y, z = m.X, m.Y+float64(float32(0.1)), m.Z
	s.mobsMu.Unlock()
	s.mobLeftItsVehicle(ent)
	s.scoreboard.EntityRemoved(ent.uuid.String())
	s.dropEntityData(ent.uuid.String())
	s.broadcastWorld(ent.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{m.ID}))
	return x, y, z, true
}

// killDragon removes the body before completing the fight. The fight lock
// must never be taken under mobsMu.
// vanilla: EnderDragon.kill
func (s *Server) killDragon(ent *mobEntity) {
	x, y, z, ok := s.discardMob(ent)
	if !ok {
		return
	}
	s.gameEvent(ent.w, rules.GameEventEntityDie, x, y, z, rules.GameEventContext{})
	f := s.dragonFight
	f.mu.Lock()
	defer f.mu.Unlock()
	s.setDragonKilled(ent.w, f, ent.uuid)
}

func (t *mobDimTick) dragonDeath(fx *mobEffects, ev entity.MobEvent) {
	if ent := t.h.book[ev.Mob]; ent != nil {
		fx.dragonDeaths = append(fx.dragonDeaths, dragonDeathEffect{t.w, ent.uuid, ev.X, ev.Y, ev.Z, int(ev.Status), ev.Mob.Silent})
		if ev.Status == 200 {
			fx.gameEvent(t.w, rules.GameEventEntityDie, ev.X, ev.Y+float64(float32(0.1)), ev.Z, rules.GameEventContext{})
		}
	}
}

// applyDragonDeath runs after mobsMu; the fight mutex precedes that lock.
// vanilla: EnderDragon.tickDeath, EnderDragonFight.setDragonKilled
func (s *Server) applyDragonDeath(d dragonDeathEffect) {
	f := s.dragonFight
	f.mu.Lock()
	defer f.mu.Unlock()
	linked := d.w == s.dimensionByName(world.EndName).w && d.id == f.dragon
	xp := 500
	if linked && !nbt.BoolField(f.data, "previously_killed") {
		xp = 12000
	}
	if d.time >= 180 && d.time <= 200 {
		s.sendParticles(d.w, protocol.ParticleExplosionEmitter, d.x, d.y+2, d.z, 4, 2, 4, 0, 1)
	}
	if d.time == 1 && !d.silent {
		s.tellGlobalLevelEvent(d.w, 1028, int32(math.Floor(d.x)), int32(math.Floor(d.y)), int32(math.Floor(d.z)), 0)
	}
	if d.w.BoolRule(world.RuleMobDrops) {
		if d.time > 150 && d.time%5 == 0 {
			s.spawnOrbs(d.w, d.x, d.y, d.z, xp*8/100)
		}
		if d.time == 200 {
			s.spawnOrbs(d.w, d.x, d.y+float64(float32(0.1)), d.z, xp/5)
		}
	}
	if d.time == 200 {
		s.setDragonKilled(d.w, f, d.id)
	}
}

// setDragonKilled completes only the fight's linked dragon; f.mu is held.
// vanilla: EnderDragonFight.setDragonKilled
func (s *Server) setDragonKilled(w *world.World, f *dragonFight, id uuid.UUID) {
	if w != s.dimensionByName(world.EndName).w || id != f.dragon {
		return
	}
	f.killed, f.bar.Value = true, 0
	if f.bar.Visible {
		f.bar.Visible = false
		s.sendBossEvent(f.bar.Players, f.bar, protocol.BossEventRemove)
	}
	s.placeDragonPodium(w, f, true)
	if gateways, ok := nbt.Get[nbt.List](f.data, "gateways"); ok && len(gateways.Items) > 0 {
		n := float64(gateways.Items[len(gateways.Items)-1].(nbt.Int))
		gateways.Items = gateways.Items[:len(gateways.Items)-1]
		f.data["gateways"] = gateways
		angle := 2 * (-math.Pi + 0.15707963267948966*n)
		x, z := int32(math.Floor(96*math.Cos(angle))), int32(math.Floor(96*math.Sin(angle)))
		var changes []world.BlockChange
		for _, c := range world.EndGatewayPortalCells(x, 75, z) {
			changes = append(changes, world.BlockChange{X: c.X, Y: c.Y, Z: c.Z, State: c.State})
		}
		s.writeBatch(w, changes)
		w.SetBlockEntityTag(x, 75, z, "minecraft:end_gateway", nbt.Compound{})
		s.broadcastWorld(w, protocol.IDPlayWorldEvent, worldEvent(3000, x, 75, z, 0))
	}
	if !nbt.BoolField(f.data, "previously_killed") {
		p := dragonPodium(f)
		s.setBlockAndTell(w, p[0], w.MotionBlockingHeight(p[0], p[2])+1, p[2], registry.BlockDragonEgg.DefaultState())
	}
	f.data["previously_killed"] = nbt.Byte(1)
}

func dragonPodium(f *dragonFight) [3]int32 {
	if p, ok := nbt.Get[nbt.IntArray](f.data, "exit_portal_location"); ok && len(p) == 3 {
		return [3]int32{p[0], p[1], p[2]}
	}
	return [3]int32{0, 64, 0}
}

func (s *Server) placeDragonPodium(w *world.World, f *dragonFight, active bool) {
	// vanilla: EnderDragonFight.spawnExitPortal
	if _, ok := f.data["exit_portal_location"]; !ok {
		y := w.NoLeavesHeight(0, 0)
		for y > 63 && registry.BlockOf(w.BlockAt(0, y, 0)) == registry.BlockBedrock {
			y--
		}
		f.data["exit_portal_location"] = nbt.IntArray{0, max(w.MinY()+1, y), 0}
	}
	p := dragonPodium(f)
	var changes []world.BlockChange
	world.PlaceEndPodium(p[0], p[1], p[2], active, func(x, y, z int32, state uint16) {
		changes = append(changes, world.BlockChange{X: x, Y: y, Z: z, State: state})
	})
	for _, c := range changes {
		old := w.BlockAt(c.X, c.Y, c.Z)
		if active && old != c.State && (registry.BlockOf(c.State) == registry.BlockAir || registry.BlockOf(c.State) == registry.BlockEndPortal || registry.BlockOf(c.State) == registry.BlockEndStone) {
			s.destroyBlockContents(w, c.X, c.Y, c.Z, old)
		}
		if registry.BlockOf(w.BlockAt(c.X, c.Y, c.Z)) == registry.BlockEndPortal && registry.BlockOf(c.State) != registry.BlockEndPortal {
			w.RemoveBlockEntity(c.X, c.Y, c.Z)
		}
	}
	s.writeBatch(w, changes)
	if active {
		for _, c := range changes {
			if registry.BlockOf(c.State) == registry.BlockEndPortal {
				w.SetBlockEntityTag(c.X, c.Y, c.Z, "minecraft:end_portal", nbt.Compound{})
			}
		}
	}
}

// tryRespawnDragon tests the four unit boxes above the podium rim.
// vanilla: EndCrystalItem.useOn, EnderDragonFight.tryRespawn
func (s *Server) tryRespawnDragon(w *world.World) {
	f := s.dragonFight
	if f == nil || w != s.dimensionByName(world.EndName).w {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.killed || f.ritual.Stage != "" {
		return
	}
	p := dragonPodium(f)
	var ids []uuid.UUID
	s.endCrystalsMu.Lock()
	for _, dir := range [][2]int32{{0, -1}, {1, 0}, {0, 1}, {-1, 0}} {
		x, y, z := p[0]+dir[0]*3, p[1]+1, p[2]+dir[1]*3
		found := false
		for _, ent := range s.endCrystals {
			if ent.w == w && !ent.crystal.Dead && ent.crystal.Overlaps(entity.NewAABB(float64(x), float64(y), float64(z), float64(x+1), float64(y+1), float64(z+1))) {
				ids = append(ids, ent.uuid)
				found = true
			}
		}
		if !found {
			s.endCrystalsMu.Unlock()
			return
		}
	}
	s.endCrystalsMu.Unlock()
	f.ritual.Start(ids)
	s.placeDragonPodium(w, f, false)
}

type dragonRitualEffect struct {
	w        *world.World
	spike    int
	finish   bool
	crystals []*endCrystalEntity
	players  []uuid.UUID
}

// stepDragonRespawn changes saved state under the fight lock; explosions run
// after it releases because a blast calls back into onCrystalDestroyed.
// vanilla: DragonRespawnStage
func (s *Server) stepDragonRespawn(w *world.World, f *dragonFight) dragonRitualEffect {
	fx := dragonRitualEffect{w: w, spike: -1}
	s.lockTick(&s.endCrystalsMu)
	for _, ent := range s.endCrystals {
		if ent.w == w && !ent.crystal.Dead && slices.Contains(f.ritual.Crystals, ent.uuid) {
			fx.crystals = append(fx.crystals, ent)
		}
	}
	s.unlockTick(&s.endCrystalsMu)
	if len(fx.crystals) == 0 {
		s.abortDragonRespawn(w, f)
		return dragonRitualEffect{}
	}
	step := f.ritual.Step(s.world.SeedValue())
	if step.Beam != nil {
		s.setDragonCrystalBeams(fx.crystals, step.Beam)
	}
	if step.Event {
		s.broadcastWorld(w, protocol.IDPlayWorldEvent, worldEvent(3001, 0, 128, 0, 0))
	}
	fx.spike, fx.finish = step.Spike, step.Finish
	if step.Finish {
		f.killed, f.dragon = false, uuid.Nil
		fx.players = slices.Clone(f.bar.Players)
	}
	return fx
}

func (s *Server) setDragonCrystalBeams(crystals []*endCrystalEntity, beam *[3]int32) {
	var sends []mobSend
	lockedDo(&s.endCrystalsMu, func() {
		for _, ent := range crystals {
			if ent.crystal.Dead {
				continue
			}
			ent.crystal.Beam = beam
			sends = append(sends, mobSend{w: ent.w, id: protocol.IDPlayEntityMetadata, data: endCrystalMetadata(ent, true)})
		}
	})
	for _, send := range sends {
		s.broadcastWorld(send.w, send.id, send.data)
	}
}

func (s *Server) resetDragonSpikeCrystals(w *world.World) {
	boxes := s.dragonFight.spikeBoxes
	var crystals []*endCrystalEntity
	lockedDo(&s.endCrystalsMu, func() {
		for _, ent := range s.endCrystals {
			if ent.w != w || ent.crystal.Dead {
				continue
			}
			for _, box := range boxes {
				if ent.crystal.Overlaps(entity.NewAABB(box.MinX, box.MinY, box.MinZ, box.MaxX, box.MaxY, box.MaxZ)) {
					ent.crystal.Invulnerable = false
					crystals = append(crystals, ent)
					break
				}
			}
		}
	})
	s.setDragonCrystalBeams(crystals, nil)
}

func (s *Server) abortDragonRespawn(w *world.World, f *dragonFight) {
	f.ritual.Abort()
	s.resetDragonSpikeCrystals(w)
	s.placeDragonPodium(w, f, true)
}

func (s *Server) applyDragonRitual(fx dragonRitualEffect) {
	if fx.w == nil {
		return
	}
	if fx.spike >= 0 {
		changes, p := world.EndSpikeRebuild(s.world.SeedValue(), fx.spike)
		var clear []world.BlockChange
		for x := p[0] - 10; x <= p[0]+10; x++ {
			for y := p[1] - 11; y <= p[1]+9; y++ {
				for z := p[2] - 10; z <= p[2]+10; z++ {
					clear = append(clear, world.BlockChange{X: x, Y: y, Z: z, State: registry.BlockAir.DefaultState()})
				}
			}
		}
		s.writeBatch(fx.w, clear)
		s.runExplosion(blast{w: fx.w, x: float64(p[0]) + 0.5, y: float64(p[1] - 1), z: float64(p[2]) + 0.5, radius: 5, rng: s.lootRng, interaction: rules.BlockExplosionInteraction(s.world), damageType: rules.DamageTypeExplosion})
		s.writeBatch(fx.w, changes)
		crystal := s.newEndCrystalEntity(fx.w, float64(p[0])+0.5, float64(p[1]), float64(p[2])+0.5)
		crystal.crystal.Invulnerable = true
		crystal.crystal.Beam = &[3]int32{0, 128, 0}
		s.registerEndCrystal(crystal)
	}
	if fx.finish {
		s.resetDragonSpikeCrystals(fx.w)
		// Remove all ritual crystals before the blasts so they cannot set each other off.
		s.lockTick(&s.endCrystalsMu)
		for _, ent := range fx.crystals {
			s.removeEndCrystalLocked(ent)
		}
		s.unlockTick(&s.endCrystalsMu)
		for _, ent := range fx.crystals {
			s.broadcastWorld(fx.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{ent.crystal.ID}))
			s.runExplosion(blast{w: fx.w, x: ent.crystal.X, y: ent.crystal.Y, z: ent.crystal.Z, radius: 6, rng: s.lootRng, interaction: rules.ExplosionKeep, damageType: rules.DamageTypeExplosion})
		}
		for _, id := range fx.players {
			s.mu.RLock()
			c := s.clients[id]
			s.mu.RUnlock()
			if c != nil {
				c.summonedEntityAdvancements(entity.TypeEnderDragon.Name())
			}
		}
	}
}

// dragonEvent routes a dragon's shot, flame or death event.
func (t *mobDimTick) dragonEvent(fx *mobEffects, ev entity.MobEvent) {
	if ev.Kind == entity.MobDragonDeath {
		t.dragonDeath(fx, ev)
		return
	}
	fx.dragonShots = append(fx.dragonShots, ghastShotOf(t.w, ev, t.h.book[ev.Mob]))
}
