package server

import (
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"math"
)

func (w pluginWorld) LoadChunk(pos plugin.ChunkPos, generate bool, done func(plugin.Chunk)) {
	if done == nil {
		panic("plugin: nil chunk callback")
	}
	w.host.Scheduler().RunAsync(func() {
		var chunk *world.Chunk
		p := world.ChunkPos{X: int32(pos.X), Z: int32(pos.Z)}
		if int(p.X) == pos.X && int(p.Z) == pos.Z {
			if generate {
				chunk, _ = w.dim.w.Acquire(p)
			} else {
				chunk = w.dim.w.AcquireSaved(p)
			}
		}
		if chunk != nil {
			defer w.dim.w.Release(p)
		}
		finished := make(chan struct{})
		w.host.Scheduler().RunTask(func() {
			defer close(finished)
			if chunk == nil {
				done(nil)
			} else {
				if woken, fresh := w.srv.wakeChunkOnce(w.dim, p); fresh {
					w.srv.registerWake(woken)
				}
				done(pluginChunk{world: w, chunk: chunk})
			}
		})
		// Keep the hold through the callback, including panic unwinding. A
		// disabled plugin's cancelled tick task will never close finished.
		select {
		case <-finished:
		case <-w.host.host.Done():
		}
	})
}

func ticketPos(pos plugin.ChunkPos) (world.ChunkPos, bool) {
	p := world.ChunkPos{X: int32(pos.X), Z: int32(pos.Z)}
	return p, int(p.X) == pos.X && int(p.Z) == pos.Z
}

// pluginTicket identifies a ticket one plugin holds.
type pluginTicket struct {
	dim    *dimension
	pos    world.ChunkPos
	radius int
}

// AddTicket holds the ticket for the plugin and wakes the saved entities of
// the chunks it loads.
// vanilla: ServerChunkCache.addTicketWithRadius, PersistentEntitySectionManager.addEntityWithoutEvent
func (w pluginWorld) AddTicket(pos plugin.ChunkPos, radius int) {
	p, ok := ticketPos(pos)
	if !ok || radius < 0 || radius > world.MaxTicketRadius {
		return
	}
	h := w.host
	h.ticketOnce.Do(func() { h.host.Own(h.releaseTickets) })
	h.ticketMu.Lock()
	if h.ticketClosed {
		h.ticketMu.Unlock()
		return
	}
	if h.held == nil {
		h.held = map[pluginTicket]int{}
	}
	h.held[pluginTicket{w.dim, p, radius}]++
	loaded := w.dim.w.AddTicket(p, radius)
	h.ticketMu.Unlock()
	for _, c := range loaded {
		if woken, fresh := w.srv.wakeChunkOnce(w.dim, c); fresh {
			w.srv.registerWake(woken)
		}
	}
}

// RemoveTicket releases a ticket this plugin holds; another plugin's
// identical ticket is not touched.
func (w pluginWorld) RemoveTicket(pos plugin.ChunkPos, radius int) {
	p, ok := ticketPos(pos)
	if !ok {
		return
	}
	h := w.host
	key := pluginTicket{w.dim, p, radius}
	h.ticketMu.Lock()
	defer h.ticketMu.Unlock()
	if h.held[key] == 0 {
		return
	}
	if h.held[key]--; h.held[key] == 0 {
		delete(h.held, key)
	}
	w.dim.w.RemoveTicket(p, radius)
}

// releaseTickets drops every ticket the plugin still holds.
func (h *pluginHost) releaseTickets() {
	h.ticketMu.Lock()
	defer h.ticketMu.Unlock()
	h.ticketClosed = true
	for key, n := range h.held {
		for range n {
			key.dim.w.RemoveTicket(key.pos, key.radius)
		}
	}
	h.held = nil
}

// Explode runs a blast as a block explosion does: the caller's interaction
// applies as given, with no game rule. A live source entity is the blast's
// direct source and its indirect source credits the kill; a stale one counts
// as nil.
// vanilla: Level.explode, ServerExplosion
func (w pluginWorld) Explode(pos plugin.Vec3, power float32, fire bool, blocks plugin.ExplosionBlocks, source plugin.Entity) {
	if power < 0 || math.IsNaN(float64(power)) || math.IsInf(float64(power), 0) || blocks > plugin.ExplosionTriggerBlocks {
		return
	}
	interaction := [...]rules.ExplosionInteraction{
		plugin.ExplosionKeep:             rules.ExplosionKeep,
		plugin.ExplosionDestroy:          rules.ExplosionDestroy,
		plugin.ExplosionDestroyWithDecay: rules.ExplosionDecay,
		plugin.ExplosionTriggerBlocks:    rules.ExplosionTrigger,
	}[blocks]
	b := blast{
		w: w.dim.w, x: pos.X, y: pos.Y, z: pos.Z, radius: float64(power),
		damageType: rules.DamageTypeExplosion, rng: w.srv.lootRng,
		interaction: interaction, fire: fire,
	}
	if source != nil {
		if src, ok := w.host.entityOfHandle(source); ok {
			w.attributeBlast(&b, src)
		}
	}
	w.srv.runExplosion(b)
}

// attributeBlast makes src the blast's direct source and the entity behind it
// the indirect one: a primed TNT's or projectile's owner, a living source
// itself, otherwise nobody.
// vanilla: Explosion.getIndirectSourceEntity, DamageSources.explosion
func (w pluginWorld) attributeBlast(b *blast, src command.Entity) {
	b.directID = src.EntityID()
	b.directType = src.EntityType()
	_, b.directLiving = src.(command.AttributeTarget)
	indirect := src
	if !b.directLiving {
		indirect = nil
		base := pluginEntity{host: w.host, id: src.UUID()}
		for _, key := range [...]string{"owner", "Owner"} {
			if id, ok := base.uuidField(key); ok {
				indirect, _ = w.host.entityByUUID(uuidOf(id), "")
				break
			}
		}
	}
	if indirect == nil {
		return
	}
	killer := w.srv.entityKiller(w.dim.w, indirect.EntityID())
	if killer == nil {
		return
	}
	b.causeID, b.causeUUID, b.causeKiller = indirect.EntityID(), indirect.UUID(), killer
	b.causeName = indirect.EntityType()
	b.damageType = rules.DamageTypePlayerExplosion
}

// StrikeLightning spawns a bolt and returns its handle, nil when a plugin
// cancelled the strike. vanilla: LightningBolt.setVisualOnly
func (w pluginWorld) StrikeLightning(pos plugin.Vec3, effectOnly bool) plugin.Entity {
	it := w.srv.spawnBolt(w.dim.w, pos.X, pos.Y, pos.Z, effectOnly, plugin.LightningPlugin)
	if it == nil {
		return nil
	}
	if e, ok := w.host.entityByUUID(it.uuid, ""); ok {
		return w.host.pluginEntityOf(e)
	}
	return nil
}

// SetBiome writes b into the 4x4x4 biome cell holding p and resends the
// chunk's biomes to the players tracking it.
// vanilla: ChunkAccess.fillBiomesFromNoise, ChunkMap.resendBiomesForChunks
func (w pluginWorld) SetBiome(p plugin.BlockPos, b registry.Biome) bool {
	if !pluginPositionFits(p) || b < 0 || b >= registry.BiomeCount ||
		p.Y < w.MinY() || p.Y >= w.MinY()+w.Height() || !rules.InWorldBoundsHorizontal(int32(p.X), int32(p.Z)) {
		return false
	}
	box := world.BiomeBox{MinX: int32(p.X) &^ 3, MinY: int32(p.Y) &^ 3, MinZ: int32(p.Z) &^ 3}
	box.MaxX, box.MaxY, box.MaxZ = box.MinX, box.MinY, box.MinZ
	_, chunks, ok := w.dim.w.FillBiomes(box, int32(b), nil)
	if ok {
		w.srv.resendBiomes(w.dim.w, chunks)
	}
	return ok
}

// Regenerate replaces the loaded chunk's blocks and biomes with what the
// world's generator makes of it, and sends the difference to its viewers.
// Block entities and entities of the old terrain are not regenerated.
func (w pluginWorld) Regenerate(pos plugin.ChunkPos) bool {
	p, ok := ticketPos(pos)
	if !ok {
		return false
	}
	lw, s := w.dim.w, w.srv
	changes, biomes, ok := lw.Regenerate(p)
	if !ok {
		return false
	}
	if biomes {
		s.resendBiomes(lw, []world.ChunkPos{p})
	}
	applied := lw.SetBlocks(changes)
	for _, a := range applied {
		s.replaceBlockEntity(lw, a.X, a.Y, a.Z, a.State, lw.BlockAt(a.X, a.Y, a.Z))
		s.markChanged(lw, a.X, a.Y, a.Z)
	}
	if len(applied) > 0 {
		s.relightAreaAsync(lw, p.X<<4, p.Z<<4, p.X<<4|15, p.Z<<4|15)
	}
	return true
}
