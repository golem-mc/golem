package server

import (
	"maps"
	"math"
	"math/rand"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// spawnerEventDelay is the block event a spawner broadcasts on each new
// delay, which resets the client's own countdown and spin.
// vanilla: BaseSpawner.EVENT_SPAWN
const spawnerEventDelay = 1

// worldEventSpawnerSpawn is the smoke and flames at a spawner that stood a
// mob up. vanilla: LevelEvent.PARTICLES_MOBBLOCK_SPAWN
const worldEventSpawnerSpawn = 2004

// entityStatusSpawnAnim is the poof a spawned mob shows.
// vanilla: Mob.spawnAnim
const entityStatusSpawnAnim = 20

// spawnerHost is entity.SpawnerHost for one dimension during one block
// entity pass. The players are read once, not per spawner.
type spawnerHost struct {
	s       *Server
	dim     *dimension
	players []entity.SpawnPlayer
	read    bool
}

// tickSpawner runs one spawner block's server tick.
func (s *Server) tickSpawner(host *spawnerHost, be *world.BlockEntity) {
	entity.TickSpawner(host, be)
}

func (h *spawnerHost) World() entity.SpawnWorld { return h.dim.w }

func (h *spawnerHost) Rand() *rand.Rand { return h.s.levelRng }

func (h *spawnerHost) SpawnersWork() bool { return h.s.world.BoolRule(world.RuleSpawnerBlocks) }

func (h *spawnerHost) Players() []entity.SpawnPlayer {
	if !h.read {
		h.s.mu.RLock()
		clients := slices.Collect(maps.Values(h.s.clients))
		h.s.mu.RUnlock()
		clients = slices.DeleteFunc(clients, func(c *client) bool { return c.player.Vitals().Dead })
		h.players, h.read = spawnPlayersOf(clients, h.dim.w), true
	}
	return h.players
}

func (h *spawnerHost) Bodies() []entity.SpawnBody { return h.s.spawnBodies(h.dim, h.Players()) }

func (h *spawnerHost) MobBodies(kind entity.MobType) []entity.SpawnBody {
	var bodies []entity.SpawnBody
	add := func(m *entity.Mob) {
		if m.Type != kind {
			return
		}
		hw := m.BbWidth() / 2
		bodies = append(bodies, entity.SpawnBody{
			MinX: m.X - hw, MinY: m.Y, MinZ: m.Z - hw,
			MaxX: m.X + hw, MaxY: m.Y + m.BbHeight(), MaxZ: m.Z + hw,
		})
	}
	h.s.mobsMu.Lock()
	defer h.s.mobsMu.Unlock()
	for _, m := range h.dim.mobs.herd {
		add(m)
	}
	for _, corpse := range h.dim.mobs.dying {
		add(corpse.ent.mob)
	}
	return bodies
}

func (h *spawnerHost) StandUp(kind entity.MobType, x, y, z float64, yaw float32, tag nbt.Compound, plain bool, equip func(*entity.Mob)) (int32, bool) {
	s := h.s
	face := func(ent *mobEntity) {
		ent.spawnReason = spawnReasonCode(plugin.SpawnSpawner)
		ent.mob.Yaw, ent.mob.HeadYaw, ent.mob.Pitch = yaw, yaw, 0
		if equip != nil {
			equip(ent.mob)
		}
	}
	var id int32
	if plain {
		id = s.spawnMobArriving(h.dim, kind, x, y, z, 0, nil, false, "", true, face)
	} else {
		var err error
		if id, err = s.spawnSummonedMobFacing(h.dim, kind, x, y, z, tag, face); err != nil {
			return 0, false
		}
	}
	return id, s.mobByEntityID(h.dim.w, id) != nil
}

// Loot rolls in the spawner's level with no origin, as trialHost.Loot does.
// vanilla: Mob.equip(EquipmentTable)
func (h *spawnerHost) Loot(table string) []protocol.ItemStack {
	stacks, _ := h.s.rollLootWith(&rules.LootContext{Level: h.s.lootLevelWithoutOrigin(h.dim.w)}, table, 0)
	return stacks
}

// SpawnDataRolled sends the spawner's update tag.
// vanilla: SpawnerBlockEntity.setNextSpawnData
func (h *spawnerHost) SpawnDataRolled(x, y, z int32) {
	be := h.dim.w.BlockEntity(x, y, z)
	if be == nil {
		return
	}
	var tag nbt.Compound
	be.Use(func(b *world.BlockEntity) { tag = b.UpdateTag() })
	h.s.broadcastBlockChunkExcept(h.dim.w, nil, x, z, protocol.IDPlayBlockEntityData, blockEntityData(
		x, y, z, world.BlockEntityTypeID(world.KindSpawner.Name()), tag))

}

// DelayRolled is the block event a new delay broadcasts.
// vanilla: SpawnerBlockEntity.broadcastEvent
func (h *spawnerHost) DelayRolled(x, y, z int32) {
	var b protocol.Buffer
	b.PutPosition(x, y, z)
	b.PutU8(spawnerEventDelay)
	b.PutU8(0)
	b.PutVarInt(registry.BlockIDs["minecraft:spawner"])
	h.s.broadcastBlockEvent(h.dim.w, x, y, z, b.Bytes())
}

func (h *spawnerHost) Spawned(x, y, z int32, mobID int32) {
	h.s.broadcastLevelEvent(h.dim.w, worldEventSpawnerSpawn, x, y, z, 0)
	h.s.broadcastWorld(h.dim.w, protocol.IDPlayEntityEvent, entityEventPayload(mobID, entityStatusSpawnAnim))
	h.s.postEntityPlace(h.dim.w, mobID)
}

// cartSpawnerHost is the host of a spawner cart: the block spawner's, except
// that a new delay is entity event 1 and the spawn data goes out with no
// update.
// vanilla: MinecartSpawner.broadcastEvent
type cartSpawnerHost struct {
	*spawnerHost
	cart *minecartEntity
}

func (h cartSpawnerHost) SpawnDataRolled(x, y, z int32) {}

func (h cartSpawnerHost) DelayRolled(x, y, z int32) {
	h.s.broadcastWorld(h.dim.w, protocol.IDPlayEntityEvent, entityEventPayload(h.cart.cart.ID, spawnerEventDelay))
}

// tickSpawnerCart runs a spawner cart's spawner at the block its cart is in,
// on the dimension's shared host.
// vanilla: MinecartSpawner.tick
func (s *Server) tickSpawnerCart(it *minecartEntity, base *spawnerHost) {
	s.lockTick(&s.minecartsMu)
	x, y, z := int32(math.Floor(it.cart.X)), int32(math.Floor(it.cart.Y)), int32(math.Floor(it.cart.Z))
	s.unlockTick(&s.minecartsMu)
	it.spawner.Use(func(b *world.BlockEntity) { b.X, b.Y, b.Z = x, y, z })
	entity.TickSpawner(cartSpawnerHost{base, it}, it.spawner)
}

// postEntityPlace posts entity_place at the block a spawner's new mob stands
// in, sourced by the mob.
// vanilla: BaseSpawner.serverTick, TrialSpawner.spawnMob
func (s *Server) postEntityPlace(w *world.World, mobID int32) {
	ent := s.mobByEntityID(w, mobID)
	if ent == nil {
		return
	}
	s.gameEvent(w, rules.GameEventEntityPlace, math.Floor(ent.mob.X)+0.5, math.Floor(ent.mob.Y)+0.5, math.Floor(ent.mob.Z)+0.5,
		rules.GameEventContext{Mob: ent.uuid})
}
