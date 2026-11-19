package server

import (
	"maps"
	"math"
	"math/rand"
	"slices"
	"strconv"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// trialHost is entity.TrialHost for one dimension during one block entity
// pass; players and mobs are read once, not per spawner.
type trialHost struct {
	s       *Server
	dim     *dimension
	spawn   *spawnerHost
	now     int64
	clients []*client
	read    bool
	mobs    map[uuid.UUID]*entity.Mob
}

func (h *trialHost) GameTime() int64 { return h.now }

func (h *trialHost) Rand() *rand.Rand { return h.s.levelRng }

// vanilla: TrialSpawner.canSpawnInLevel
func (h *trialHost) CanSpawn() bool {
	w := h.s.world
	return w.BoolRule(world.RuleSpawnerBlocks) && w.Difficulty() != rules.DifficultyPeaceful &&
		w.BoolRule(world.RuleSpawnMobs)
}

func (h *trialHost) World() entity.SpawnWorld { return h.dim.w }

func (h *trialHost) Bodies() []entity.SpawnBody { return h.spawn.Bodies() }

func (h *trialHost) players() []*client {
	if !h.read {
		h.s.mu.RLock()
		all := slices.Collect(maps.Values(h.s.clients))
		h.s.mu.RUnlock()
		h.clients = slices.DeleteFunc(all, func(c *client) bool { return c.theWorld() != h.dim.w })
		h.read = true
	}
	return h.clients
}

func (h *trialHost) Players() []entity.TrialPlayer {
	var out []entity.TrialPlayer
	for _, c := range h.players() {
		pos := c.player.Position()
		dims := c.dims(c.player.Appearance().Pose)
		_, trial := c.player.Effect(registry.MobEffectTrialOmen)
		_, bad := c.player.Effect(registry.MobEffectBadOmen)
		out = append(out, entity.TrialPlayer{
			UUID: c.player.UUID, Mode: c.player.GameMode(),
			X: pos.X, Y: pos.Y, Z: pos.Z, Eye: dims.EyeHeight, Height: dims.Height,
			Alive: c.Alive(), TrialOmen: trial, BadOmen: bad,
		})
	}
	return out
}

// LineClear is the line of sight from a point to the spawner's centre,
// outline shapes standing in for visual shapes.
// vanilla: PlayerDetector.inLineOfSight
func (h *trialHost) LineClear(x, y, z int32, px, py, pz float64) bool {
	cx, cy, cz := float64(x)+0.5, float64(y)+0.5, float64(z)+0.5
	hit, found := h.dim.w.ClipBlocks(px, py, pz, cx-px, cy-py, cz-pz, nil)
	return !found || (hit.X == x && hit.Y == y && hit.Z == z)
}

func (h *trialHost) Mob(id uuid.UUID) (entity.TrialMob, bool) {
	if h.mobs == nil {
		h.mobs = make(map[uuid.UUID]*entity.Mob)
		h.s.mobsMu.Lock()
		herd := h.dim.mobs
		for _, m := range herd.herd {
			if ent := herd.book[m]; ent != nil {
				h.mobs[ent.uuid] = m
			}
		}
		h.s.mobsMu.Unlock()
	}
	m := h.mobs[id]
	if m == nil {
		return entity.TrialMob{}, false
	}
	_, minY, _, _, maxY, _ := m.BlastBox()
	return entity.TrialMob{X: m.X, Y: m.Y, Z: m.Z, Height: maxY - minY, Alive: m.Alive()}, true
}

func (h *trialHost) StandUp(kind entity.MobType, x, y, z float64, yaw float32, tag nbt.Compound, plain bool, equip func(*entity.Mob)) (uuid.UUID, bool) {
	face := func(ent *mobEntity) {
		ent.mob.Yaw, ent.mob.HeadYaw, ent.mob.Pitch = yaw, yaw, 0
		// vanilla: TrialSpawner.spawnMob, Mob.setPersistenceRequired
		ent.mob.Persistence = true
		if equip != nil {
			equip(ent.mob)
		}
	}
	var id int32
	if plain {
		id = h.s.spawnMobArriving(h.dim, kind, x, y, z, 0, nil, false, "", true, face)
	} else {
		var err error
		if id, err = h.s.spawnSummonedMobFacing(h.dim, kind, x, y, z, tag, face); err != nil {
			return uuid.Nil, false
		}
	}
	ent := h.s.mobByEntityID(h.dim.w, id)
	if ent == nil {
		return uuid.Nil, false
	}
	if h.mobs != nil {
		h.mobs[ent.uuid] = ent.mob
	}
	h.s.postEntityPlace(h.dim.w, id)
	return ent.uuid, true
}

func (h *trialHost) LevelEvent(event, x, y, z, data int32) {
	h.s.broadcastLevelEvent(h.dim.w, event, x, y, z, data)
}

// Loot rolls in the spawner's level with no origin.
// vanilla: TrialSpawner.ejectReward (LootContextParamSets.EMPTY)
func (h *trialHost) Loot(table string) []protocol.ItemStack {
	stacks, _ := h.s.rollLootWith(&rules.LootContext{Level: h.s.lootLevelWithoutOrigin(h.dim.w)}, table, 0)
	return stacks
}

// DropItem takes the item's bottom y, as ItemEntity does; the item
// simulation uses the box middle.
func (h *trialHost) DropItem(stack protocol.ItemStack, x, y, z, vx, vy, vz float64) {
	// vanilla: DefaultDispenseItemBehavior.spawnItem (no pickup delay)
	h.s.spawnItem(h.dim.w, stack, x, y, z, vx, vy, vz, 0)
}

func (h *trialHost) Sound(sound registry.Sound, x, y, z int32) {
	h.s.broadcastSoundID(h.dim.w, sound, protocol.SoundCategoryBlock,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1, 1)
}

// BadOmenToTrialOmen swaps bad omen for trial omen on an ominous trigger,
// 18000 ticks per bad omen level.
// vanilla: TrialSpawnerStateData.transformBadOmenIntoTrialOmen
func (h *trialHost) BadOmenToTrialOmen(id uuid.UUID) {
	for _, c := range h.players() {
		if c.player.UUID != id {
			continue
		}
		const badOmen = registry.MobEffectBadOmen
		bad, ok := c.player.Effect(badOmen)
		if !ok {
			return
		}
		c.RemoveEffect(badOmen)
		c.AddEffect(effect.New(registry.MobEffectTrialOmen,
			entity.TrialOmenPerBadOmenLevel*(bad.Amplifier+1), 0, false, true, true))
		return
	}
}

// Discard drops the mob's preserved equipment and removes it.
// vanilla: Mob.dropPreservedEquipment, Entity.remove(DISCARDED)
func (h *trialHost) Discard(id uuid.UUID) (x, y, z float64, ok bool) {
	h.Mob(id)
	m := h.mobs[id]
	if m == nil {
		return 0, 0, 0, false
	}
	h.s.mobsMu.Lock()
	drops := m.TakePreservedEquipment()
	m.Discard()
	x, y, z = m.X, m.Y, m.Z
	h.s.mobsMu.Unlock()
	for _, stack := range drops {
		h.s.spawnItemAtFeet(h.dim.w, stack, x, y, z)
	}
	return x, y, z, true
}

// OminousItems rolls the table seeded by the spawner's 30x20x30 cell, so
// every spawner in a cell drops the same; the roll is cached since an
// ominous spawner asks every tick.
// vanilla: TrialSpawnerStateData.lowResolutionPosition
func (h *trialHost) OminousItems(table string, x, y, z int32) []protocol.ItemStack {
	cell := entity.PackBlockPos(int32(math.Floor(float64(float32(x)/30))),
		int32(math.Floor(float64(float32(y)/20))), int32(math.Floor(float64(float32(z)/30))))
	key := ominousLootKey{table, cell}
	if stacks, ok := h.s.ominousLoot[key]; ok {
		return stacks
	}
	stacks, _ := h.s.rollLootWith(&rules.LootContext{Level: h.s.lootLevelWithoutOrigin(h.dim.w)}, table, h.s.world.SeedValue()+cell)
	h.s.ominousRolls++
	if h.s.ominousLoot == nil {
		h.s.ominousLoot = map[ominousLootKey][]protocol.ItemStack{}
	}
	h.s.ominousLoot[key] = stacks
	return stacks
}

// ominousLootKey names an OminousItems roll: same table and cell seed,
// same stacks.
type ominousLootKey struct {
	table string
	cell  int64
}

// ClipUp is the cell the clip lands in.
// vanilla: TrialSpawnerState.calculatePositionAbove
func (h *trialHost) ClipUp(x, y, z, dy float64) (int32, int32, int32) {
	if hit, found := h.dim.w.ClipBlocks(x, y, z, 0, dy, 0, nil); found {
		return hit.X, hit.Y, hit.Z
	}
	return int32(math.Floor(x)), int32(math.Floor(y + dy)), int32(math.Floor(z))
}

func (h *trialHost) CollisionEmpty(x, y, z int32) bool { return h.dim.w.CollisionEmpty(x, y, z) }

func (h *trialHost) SpawnItemSpawner(stack protocol.ItemStack, x, y, z float64, afterTicks int) {
	h.s.spawnOminousItemSpawner(h.dim.w, stack, x, y, z, afterTicks)
}

func (h *trialHost) SoundAt(sound registry.Sound, x, y, z float64, volume, pitch float32) {
	h.s.broadcastSoundID(h.dim.w, sound, protocol.SoundCategoryBlock, x, y, z, volume, pitch)
}

// tickTrialSpawners ticks the trial spawners of a dimension.
func (s *Server) tickTrialSpawners(dim *dimension, spawn *spawnerHost, now int64, ticks func(world.ChunkPos) bool) {
	spawners := dim.w.TrialSpawners()
	if len(spawners) == 0 {
		return
	}
	host := &trialHost{s: s, dim: dim, spawn: spawn, now: now}
	canSpawn := host.CanSpawn()
	for _, at := range spawners {
		if ticks != nil && !ticks(blockChunk(at.X, at.Z)) {
			continue
		}
		if entity.TrialIdle(at.Tag, at.State, at.Ominous, at.X, at.Y, at.Z, now, canSpawn) {
			continue
		}
		t := entity.ParseTrialSpawner(at.Tag)
		t.Ominous = at.Ominous
		next := entity.TickTrialSpawner(host, t, at.X, at.Y, at.Z, at.State)
		if t.Dirty || t.Updated {
			dim.w.ReplaceCarried(at.X, at.Y, at.Z, t.Store(at.Tag))
		}
		if next != at.State || t.Ominous != at.Ominous {
			s.setTrialSpawnerState(dim.w, at, next, t.Ominous)
		} else if t.Updated {
			s.tellTrialSpawner(dim.w, at.X, at.Y, at.Z)
		}
	}
}

// setTrialSpawnerState sets the block's state; the flush sends clients the
// block and its update tag. vanilla: TrialSpawnerBlockEntity.setState
func (s *Server) setTrialSpawnerState(w *world.World, at world.TrialSpawnerAt, next string, ominous bool) {
	state := w.BlockAt(at.X, at.Y, at.Z)
	if registry.BlockOf(state) != registry.BlockTrialSpawner {
		return
	}
	state, ok := registry.WithProps(state,
		registry.PropValue{P: registry.PropTrialSpawnerState, V: next},
		registry.PropValue{P: registry.PropOminous, V: strconv.FormatBool(ominous)})
	if !ok {
		return
	}
	s.setBlockAndTell(w, at.X, at.Y, at.Z, state)
}

// tellTrialSpawner sends a trial spawner's update tag.
func (s *Server) tellTrialSpawner(w *world.World, x, y, z int32) {
	tag, ok := w.BlockEntityTag(x, y, z)
	if !ok {
		return
	}
	data := world.CarriedUpdateTag("minecraft:trial_spawner", tag, world.TrialSpawnerActive(w.BlockAt(x, y, z)))
	s.broadcastBlockChunkExcept(w, nil, x, z, protocol.IDPlayBlockEntityData,
		blockEntityData(x, y, z, protocol.BlockEntityTrialSpawner, data))
}
