package server

import (
	"github.com/golem-mc/golem/plugin"
	"log/slog"
	"slices"
	"strconv"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

const (
	itemPopSpeed   = entity.ItemPopSpeed
	itemPopScatter = entity.ItemPopScatter
)

// itemEntity is one stack of items lying in a world. The stack, position,
// physics and age are an entity.Item; the server holds the dimension, UUID
// and tracking.
type itemEntity struct {
	item *entity.Item
	w    *world.World
	uuid uuid.UUID
	// thrower is the entity that last tossed this stack, if known.
	thrower uuid.UUID

	bodyTrack[struct{}]

	// killed is the mark killItem leaves on the body. A pickup whose
	// claim window the kill lands in reads it in its settle: there the
	// kill's Dead write sits underneath the claim's own, and without the
	// mark the settle could not tell them apart and would raise the body
	// whose removal is already out.
	killed bool

	// skipStep holds back the item's next physics step: an entity added
	// while a tick iterates its list first steps on the following one.
	// vanilla: EntityTickList.ensureActiveIsNotIterated
	skipStep bool

	// portal is the item's portal state: the cooldown of its last
	// crossing and its standing in the portal it lies in.
	portal rules.EntityPortal

	// tags are the scoreboard tags, what the tag selector option matches
	// and /tag edits. Guarded by the item lock, like the rest of an
	// item's own fields.
	tags nbt.StringSet
	// custom is the custom name, guarded like tags.
	custom customName
	// spawnReason is plugin.SpawnReason+1 when recorded; 0 means SpawnDefault.
	spawnReason uint8
	look        entityLook

	// sentFire is the on-fire bit the watchers were last told.
	sentFire bool
}

// itemTracker tracks the items. An item is told by the bottom of its box,
// and its Teleport Entity carries no velocity: the motion packets do.
var itemTracker = trackedKind[*itemEntity, struct{}]{
	invalidatePiston: func(dim *dimension) { dim.pistons.items.built = false },

	rangeChunks:    entity.ItemTrackingRange,
	interval:       entity.ItemUpdateInterval,
	frozenSchedule: true,
	lock:           func(s *Server) *sync.Mutex { return &s.itemsMu },
	list:           func(s *Server) *[]*itemEntity { return &s.items },
	chunks:         func(s *Server) *entity.ChunkIndex[*itemEntity] { return &s.itemChunks },
	filing:         func(it *itemEntity) *entity.Filing { return &it.item.Filing },
	uuid:           func(it *itemEntity) uuid.UUID { return it.uuid },
	track:          func(it *itemEntity) *bodyTrack[struct{}] { return &it.bodyTrack },
	world:          func(it *itemEntity) *world.World { return it.w },
	id:             func(it *itemEntity) int32 { return it.item.ID },
	dead:           func(it *itemEntity) bool { return it.item.Dead },
	pose: func(it *itemEntity) bodyPose {
		return bodyPose{x: it.item.X, y: it.item.Y - entity.ItemHalfSize, z: it.item.Z, onGround: it.item.OnGround}
	},
	pairing: itemPairing,
}

// canPickUp reports whether this player can pick an item up now: alive and
// not a spectator, whose body never meets an item entity. Creative plays
// too, its inventory growing the same way a survival one does.
// vanilla: ItemEntity.playerTouch, EntitySelector.NO_SPECTATORS
func canPickUp(c *client) bool {
	if c.player.GameMode() == protocol.GameModeSpectator {
		return false
	}
	return !c.player.Vitals().Dead
}

// itemPickupCandidate returns the first client who may claim the item now —
// alive, not a spectator, allowed by the item's owner rule and within the
// pickup range — or nil for one nobody claims.
func itemPickupCandidate(it *itemEntity, clients []*client, reach map[*client]entity.AABB) *itemPickup {
	for _, c := range clients {
		if c.theWorld() != it.w || !canPickUp(c) || !it.item.AllowsPickup(c.player.UUID) || !reach[c].Crosses(entity.NewAABB(it.item.BlastBox())) {
			continue
		}
		return &itemPickup{collector: c, item: it.item.ID}
	}
	return nil
}

// pickupBox is the box within which this player touches items and orbs: on
// foot their pose's box grown 1.0 sideways and 0.5 vertically; riding, the
// box holding both theirs and the vehicle's, grown 1.0 sideways only. Takes
// each vehicle lock only for the read, so call it with none held.
// vanilla: Player.aiStep
func (c *client) pickupBox() entity.AABB {
	s := c.srv
	pos := c.player.Position()
	dims := c.dims(c.player.Appearance().Pose)
	hw := dims.Width / 2
	minX, minY, minZ := pos.X-hw, pos.Y, pos.Z-hw
	maxX, maxY, maxZ := pos.X+hw, pos.Y+dims.Height, pos.Z+hw
	var v [6]float64
	riding := true
	switch {
	case c.vehicle.Load() != nil:
		it := c.vehicle.Load()
		s.boatsMu.Lock()
		v[0], v[1], v[2], v[3], v[4], v[5] = it.boat.BlastBox()
		s.boatsMu.Unlock()
	case c.minecart.Load() != nil:
		it := c.minecart.Load()
		s.minecartsMu.Lock()
		v[0], v[1], v[2], v[3], v[4], v[5] = it.cart.BlastBox()
		s.minecartsMu.Unlock()
	case c.mount.Load() != nil:
		ent := c.mount.Load()
		s.mobsMu.Lock()
		v[0], v[1], v[2], v[3], v[4], v[5] = ent.mob.BlastBox()
		s.mobsMu.Unlock()
	case c.cushion.Load() != nil:
		v[0], v[1], v[2], v[3], v[4], v[5] = s.fixtureBox(c.cushion.Load())
	default:
		riding = false
	}
	if !riding {
		return entity.NewAABB(minX-1, minY-0.5, minZ-1, maxX+1, maxY+0.5, maxZ+1)
	}
	return entity.NewAABB(min(minX, v[0])-1, min(minY, v[1]), min(minZ, v[2])-1,
		max(maxX, v[3])+1, max(maxY, v[4]), max(maxZ, v[5])+1)
}

// motionThreshold is how far a tick may move an item's velocity off the one
// the clients were last told before the motion packet is worth sending:
// vanilla's ServerEntity 1.0E-7, squared.
const motionThreshold = 1e-7

// itemPickup is what one pickup has to tell its collector before it can be
// broadcast: the slot updates their inventory grew.
type itemPickup struct {
	collector *client
	item      int32
	count     int32
	left      int32
	consumed  bool
	slots     [][]byte
	visible   bool
}

// itemMetadataUpdate is what one stack change owes the clients watching it:
// the metadata packet that says the new stack, and the players who were shown
// the item. The packet is built under the item lock, from the stack as the
// change left it; the clients it goes to are resolved once the lock is down.
type itemMetadataUpdate struct {
	it       *itemEntity
	metadata []byte
	watchers []uuid.UUID
}

// cellSpot is a block cell in a world a tick's bodies gave an event at.
type cellSpot struct {
	w       *world.World
	x, y, z int32
}

// setItemStepDeferral holds arrivals back while an entity pass is running.
func (s *Server) setItemStepDeferral(deferSteps bool) {
	s.lockTick(&s.itemsMu)
	s.deferItemSteps = deferSteps
	s.unlockTick(&s.itemsMu)
}

// tickItems runs one game tick of everything lying in the worlds. The tick
// is three phases, for the locks — the item lock is taken beneath the
// player ones, never above them: positions read first with nothing held;
// physics and movements under the item lock alone; pickups last, each
// needing a player's inventory and an item's stack, taking and releasing
// each lock in turn.
func (s *Server) tickItems() {
	s.lockTick(&s.itemsMu)
	s.deferItemSteps = false
	none := len(s.items) == 0
	s.unlockTick(&s.itemsMu)
	if none {
		return
	}
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	// Where everyone is, read before the item lock: positions belong to
	// the player's lock.
	where := make(map[*client]player.Position, len(clients))
	radius := make(map[*client]int32, len(clients))
	reach := make(map[*client]entity.AABB, len(clients))
	for _, c := range clients {
		where[c] = c.player.Position()
		reach[c] = c.pickupBox()
		radius[c] = c.chunkRadius()
	}

	near := nearbyFor(clients, func(c *client) trackingWatcher {
		return trackingWatcher{c: c, x: where[c].X, z: where[c].Z, chunkRadius: radius[c]}
	})
	defer near.release()

	var (
		out        trackSends
		candidates []*itemPickup
		// despawns is per world: a Remove Entities packet carries the ids
		// a world's players know.
		despawns = make(map[*world.World][]int32)
		// announced is the items whose removal a packet is already on
		// its way for.
		announced = make(map[*itemEntity]bool)
		// merges is the stacks a merge grew and the items whose on-fire
		// bit turned this tick, with the metadata and the clients it goes
		// to.
		merges []itemMetadataUpdate
	)

	// itemPortalTravel is one item the portal pass caught in a portal,
	// the crossing of which has come due.
	type itemPortalTravel struct {
		it   *itemEntity
		kind rules.PortalKind
	}
	var travels []itemPortalTravel
	var gates []gatewayTouch
	// The despawns and merges listeners are asked about once the lock is
	// down; the step keeps those items alive meanwhile.
	asks := s.newItemAsks()

	// Read colliders before the item lock: a move under it may not take
	// the boat or mob locks.
	colliders := s.collidersByWorld(itemCollisions)
	gate := s.newEntityGate()
	var held []*itemEntity
	defer func() { itemTracker.dormantPass(s, held) }()
	s.lockTick(&s.itemsMu)
	// The portal pass runs ahead of the tick: a body the portal is
	// sending across does not step this dimension's tick at all.
	portalAlive := s.items[:0]
	for _, it := range s.items {
		if it.item.Dead {
			itemTracker.unfile(it)
			continue // a pickup consumed it last tick
		}
		if it.skipStep {
			portalAlive = append(portalAlive, it)
			continue
		}
		state, kind, due := gate.stepPortal(it.w, it.item.X, it.item.Y, it.item.Z, &it.portal, portalCooldownTicks)
		s.notePortalTouch(nil, it.uuid, it.w, it.item.X, it.item.Y, it.item.Z, state)
		if !due {
			if state == rules.EndGatewayState {
				gatewayTouchAt(&gates, it.w, it.item.X, it.item.Y, it.item.Z, gatewayTouch{
					mu: &s.itemsMu, portal: &it.portal, cooldown: portalCooldownTicks,
					move: func(to player.Position) { s.gatewayMoveItem(it, to) },
				})
			}
			portalAlive = append(portalAlive, it)
			continue
		}
		itemTracker.unfile(it)
		travels = append(travels, itemPortalTravel{it: it, kind: kind})
	}
	s.items = portalAlive

	// live and neighbours are the items alive as the tick began — a merge
	// this tick makes does not take its victim out of a later item's
	// neighbours; slot is each live item's index in its world's grid.
	live := make([]*itemEntity, 0, len(s.items))
	slot := make([]int, 0, len(s.items))
	neighbours := make(map[*world.World]*bodyNear[*entity.Item])
	for _, it := range s.items {
		if it.item.Dead {
			continue
		}
		n := neighbours[it.w]
		if n == nil {
			n = newBodyNear(entity.ItemMergeReach, func(i *entity.Item) (float64, float64) { return i.X, i.Z })
			neighbours[it.w] = n
		}
		live = append(live, it)
		slot = append(slot, n.add(it.item))
		asks.watch(it)
	}
	defer func() {
		for _, n := range neighbours {
			n.release()
		}
	}()

	alive := s.items[:0]
	// sounds is one packet a burning or extinguished item's tick produced,
	// written after the lock is down.
	type itemSound struct {
		w *world.World
		outbound
	}
	var sounds []itemSound
	// gameEvents are the splashes and bounces, posted once the lock is down.
	type itemGameEvent struct {
		w       *world.World
		event   rules.GameEvent
		x, y, z float64
		dampens bool
	}
	var gameEvents []itemGameEvent
	var bodyLandings []bodyLanding
	// spills are the contents of the destroyed containers, laid down once
	// the lock is down.
	var spills []itemSpill
	// bubbles are the column cells items rode the surface of this tick.
	var bubbles []bubbleSurface
	var cauldrons []cellSpot
	var ores []cellSpot
	// flakes are the points snowflakes may rise from, above powder snow.
	var flakes []snowflakeSpot
	for k, it := range live {
		if it.item.Dead {
			itemTracker.unfile(it)
			continue // a merge took it into a neighbour earlier in the pass
		}
		if it.skipStep {
			it.skipStep = false
			alive = append(alive, it)
			// A player's touch does not require the newborn item's own tick.
			// vanilla: LivingEntity.tick, Player.aiStep, ItemEntity.playerTouch
			if it.item.PickupDue() {
				if p := itemPickupCandidate(it, clients, reach); p != nil {
					candidates = append(candidates, p)
				}
			}
			continue
		}
		// An item whose chunk is not loaded (a joining player's own chunk
		// can still be generating) waits for it: stepping it would read air
		// for its ground and drop it through the terrain.
		// vanilla: ServerLevel.tick
		if pos := (world.ChunkPos{X: world.ChunkOf(it.item.X), Z: world.ChunkOf(it.item.Z)}); !it.w.ChunkLoaded(pos) || !gate.ticks(it.w, it.item.X, it.item.Z) {
			held = append(held, it)
			alive = append(alive, it)
			continue
		}
		// Send the pre-physics baseline: the client predicts this tick's
		// movement from it. vanilla: ChunkMap.tick, ServerEntity.sendChanges
		body := itemTracker.body(it)
		mt := body.schedule(it.item.TakeSync)
		var motion []byte
		if mt.gate && it.motionDue(it.item.VX, it.item.VY, it.item.VZ, true) {
			motion = entityVelocityPayload(it.item.ID, it.item.VX, it.item.VY, it.item.VZ)
		}
		for _, i := range body.watchers(near) {
			c := clients[i]
			if c.theWorld() != it.w {
				continue // a player of another dimension never sees it move
			}
			was, seen := body.settle(c, where[c].X, where[c].Z, radius[c], &out)
			if !seen || !mt.gate {
				continue
			}
			if motion != nil {
				out.moves.add(c, outbound{id: protocol.IDPlaySetEntityMotion, data: motion})
			}
			body.moveTold(was, c, mt, &out)
		}
		n := neighbours[it.w]
		asks.hearHurts(it)
		events := it.item.Step(colliders[it.w], n.query)
		it.item.HurtAsk = nil
		if fall := it.item.LandedFall; fall > 0 {
			it.item.LandedFall = 0
			bodyLandings = append(bodyLandings, bodyLanding{w: it.w, x: it.item.X, y: it.item.Y - entity.ItemHalfSize, z: it.item.Z,
				halfWidth: entity.ItemHalfSize, dampens: dampensVibrations(it.item.Stack)})
		}
		n.moved(slot[k])
		for _, ev := range events {
			switch ev.Kind {
			case entity.ItemBurned:
				sounds = append(sounds, itemSound{w: it.w, id: protocol.IDPlaySoundEffect,
					data: s.burnSound(protocol.SoundCategoryAmbient, it.item.X, it.item.Y-entity.ItemHalfSize, it.item.Z)})
			case entity.ItemExtinguished:
				sounds = append(sounds, itemSound{w: it.w, id: protocol.IDPlaySoundEffect,
					data: s.extinguishSound(protocol.SoundCategoryAmbient, it.item.X, it.item.Y-entity.ItemHalfSize, it.item.Z)})
			case entity.ItemSplashed:
				sounds = append(sounds, itemSound{w: it.w, id: protocol.IDPlaySoundEffect,
					data: soundEffectIDAt(registry.SoundEntityGenericSplash, protocol.SoundCategoryAmbient,
						it.item.X, it.item.Y-entity.ItemHalfSize, it.item.Z, ev.Volume, ev.Pitch)})
				gameEvents = append(gameEvents, itemGameEvent{it.w, rules.GameEventSplash, it.item.X, it.item.Y - entity.ItemHalfSize, it.item.Z, dampensVibrations(it.item.Stack)})
			case entity.ItemHurt:
				gameEvents = append(gameEvents, itemGameEvent{it.w, rules.GameEventEntityDamage, it.item.X, it.item.Y - entity.ItemHalfSize, it.item.Z, dampensVibrations(it.item.Stack)})
			case entity.ItemBounced:
				gameEvents = append(gameEvents, itemGameEvent{it.w, rules.GameEventBounce, it.item.X, it.item.Y - entity.ItemHalfSize, it.item.Z, dampensVibrations(it.item.Stack)})
			case entity.ItemDousedCauldron:
				cauldrons = append(cauldrons, cellSpot{it.w, ev.X, ev.Y, ev.Z})
			case entity.ItemSteppedOnRedstoneOre:
				ores = append(ores, cellSpot{it.w, ev.X, ev.Y, ev.Z})
			case entity.ItemBubbleSurface:
				bubbles = append(bubbles, bubbleSurface{it.w, ev.X, ev.Y, ev.Z})
			case entity.ItemSnowflake:
				flakes = append(flakes, snowflakeSpot{it.w, it.item.X, float64(ev.Y + 1), it.item.Z})
			case entity.ItemDestroyed:
				// The lava or the fire won: out of every view,
				// nothing laid down behind it.
				// Each further damaging cell repeats the event.
				if announced[it] {
					continue
				}
				announced[it] = true
				if asks.hasHurts(it) {
					continue // the listeners decide whether the lethal hurt stands
				}
				despawns[it.w] = append(despawns[it.w], it.item.ID)
				s.entityDestroyed(it.uuid)
				spills = append(spills, spillOf(it))
			}
		}
		if it.item.Dead {
			if asks.hasHurts(it) {
				alive = append(alive, it)
			} else {
				itemTracker.unfile(it)
			}
			continue
		}

		if fire := itemFireDelta(it); fire != nil {
			merges = append(merges, itemMetadataUpdate{it: it, metadata: fire, watchers: trackedWatchers(it.tracked)})
		}
		claimed := false
		if it.item.PickupDue() {
			if p := itemPickupCandidate(it, clients, reach); p != nil {
				// The first player in range claims it, as vanilla
				// gives an item to one collector.
				candidates = append(candidates, p)
				claimed = true
			}
		}
		// A claimed item stays in the list until the pickup phase has
		// had it.
		if !claimed && it.item.ShouldDespawn() {
			if asks.despawn(it) {
				alive = append(alive, it)
				continue
			}
			despawns[it.w] = append(despawns[it.w], it.item.ID)
			s.entityDestroyedBy(it.uuid, plugin.RemoveDespawn)
			itemTracker.unfile(it)
			continue
		}
		alive = append(alive, it)
	}
	// The merges the tick's scans made, heard here because neither half
	// of one carries an event of its own: an item a neighbour's scan took
	// into its stack is dead without one, and the survivor's grown stack
	// is a change the clients' view of the item ran underneath. The
	// victims ride the same removals; the metadata update is built here,
	// from the stack as the merge left it, for the clients the tracking
	// says are watching.
	for _, it := range live {
		if it.item.Dead && !announced[it] {
			despawns[it.w] = append(despawns[it.w], it.item.ID)
			s.entityDestroyedBy(it.uuid, plugin.RemoveMerge)
			announced[it] = true
		}
		if !it.item.Dead && it.item.TakeMerge() {
			update := itemMetadataUpdate{it: it, metadata: itemMetadataPayload(it)}
			for id := range it.tracked.all() {
				update.watchers = append(update.watchers, id)
			}
			merges = append(merges, update)
		}
	}
	s.items = alive
	s.unlockTick(&s.itemsMu)
	s.resolveItemAsks(asks, despawns, &merges, &spills)

	out.send()
	// The removals ahead of the fire's sounds: vanilla takes the burned
	// body out inside the hurt and plays the fizz after.
	for w, ids := range despawns {
		s.broadcastWorld(w, protocol.IDPlayRemoveEntities, removeEntitiesPayload(ids))
	}
	s.sendItemMetadata(merges)
	s.spillContents(spills)
	for _, sound := range sounds {
		s.broadcastWorld(sound.w, sound.id, sound.data)
	}
	for _, g := range gameEvents {
		s.gameEvent(g.w, g.event, g.x, g.y, g.z, rules.GameEventContext{Dampens: g.dampens})
	}
	s.runBodyLandings(bodyLandings)
	for _, c := range cauldrons {
		s.cauldronDoused(c.w, c.x, c.y, c.z)
	}
	for _, o := range ores {
		s.lightRedstoneOre(o.w, o.x, o.y, o.z)
	}
	applyEach(s, bubbles, (*Server).sendBubbleSurface)
	s.rollSnowflakes(flakes)

	// The crossings: each item the portal pass caught arrives as a new
	// body in the dimension its portal goes to; a crossing with nowhere
	// to arrive puts the item back in the list, the cooldown spent.
	s.touchGateways(gates)
	for _, tr := range travels {
		s.finishItemTravel(tr.it, tr.kind)
	}

	s.giveItemPickups(candidates)
}

// tickItemPickups runs one pass of the item pickups alone, the part of
// tickItems a frozen world still owes its players: vanilla's player keeps
// ticking and touching nearby entities while the item's own tick is held.
// vanilla: Player.aiStep, ItemEntity.playerTouch
func (s *Server) tickItemPickups() {
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	reach := make(map[*client]entity.AABB, len(clients))
	for _, c := range clients {
		reach[c] = c.pickupBox()
	}
	var candidates []*itemPickup
	s.lockTick(&s.itemsMu)
	for _, it := range s.items {
		if it.item.Dead || !it.item.PickupDue() {
			continue
		}
		if p := itemPickupCandidate(it, clients, reach); p != nil {
			candidates = append(candidates, p)
		}
	}
	s.unlockTick(&s.itemsMu)
	s.giveItemPickups(candidates)
}

// giveItemPickups hands each claimed item to its collector: the stack is
// claimed out from under the item lock — alive check, count taken, body
// marked dead — before the inventory is given any of it, and what did not
// fit is put back after. A give with the lock down would leave the body
// alive holding a stack already handed out, for a teleport across the
// dimensions to twin.
func (s *Server) giveItemPickups(candidates []*itemPickup) {
	for _, p := range candidates {
		it := s.itemByID(p.item)
		if it == nil {
			continue // gone: the tick raced a despawn or another pickup's tail
		}
		c := p.collector

		// rest is what a listener left on the ground by taking fewer.
		take, ok := c.pluginPickup(it)
		if !ok {
			continue
		}
		// vanilla: ItemEntity.playerTouch, whose shrink runs before
		// anything else can read the stack.
		s.itemsMu.Lock()
		rest := int32(0)
		var given protocol.ItemStack
		claimed := !it.item.Dead
		thrower := it.thrower
		if claimed {
			given = it.item.Stack
			if take > 0 && take < given.Count {
				rest, given.Count = given.Count-take, take
			}
			it.item.Stack.Count = 0
			it.item.Dead = true
		}
		s.itemsMu.Unlock()
		if !claimed {
			continue // gone between the find and the claim
		}
		if s.pickupSeam != nil {
			s.pickupSeam(p.item)
		}

		c.player.Inventory(func(inv *player.Inventory) {
			left, slots := inv.Add(given)
			p.count = given.Count - left
			for _, slot := range slots {
				p.slots = append(p.slots, playerWindowSlotOf(inv, slot))
				p.visible = p.visible || player.VisibleSlot(inv, slot)
			}
			p.left = left
		})
		p.consumed = p.left == 0 && rest == 0

		// A creative add always succeeds, room or not: whatever the
		// inventory cannot hold is voided, the take playing the whole
		// original count. vanilla: Inventory.add, Player.hasInfiniteMaterials
		if c.player.GameMode() == protocol.GameModeCreative {
			p.count = given.Count
			p.left = 0
			p.consumed = rest == 0
		}

		// What did not fit goes back where it lay: the count restored,
		// the body alive again unless the collector took the last of it,
		// or a kill took the body during the window — the kill's removal
		// is already out, and the body is not raised under it.
		s.itemsMu.Lock()
		it.item.Stack.Count = p.left + rest
		it.item.Dead = p.consumed || it.killed
		s.itemsMu.Unlock()
		if p.count == 0 {
			// The inventory could not hold any of it: the item stays
			// as it lies, as vanilla retries a pickup that did not fit.
			continue
		}
		// The whole stack's count, even when part of it did not fit.
		// vanilla: ItemEntity.playerTouch
		c.awardItemStat(registry.StatTypePickedUp, given.ItemID, given.Count)

		for _, slot := range p.slots {
			c.send(protocol.IDPlayContainerSlot, slot)
		}
		if p.visible {
			c.equipmentDue.Store(true)
		}
		s.itemsMu.Lock()
		watchers := trackedWatchers(it.tracked)
		s.itemsMu.Unlock()
		s.tellPickup(it.w, watchers, c, pickupItemPayload(p.item, c.player.EntityID, p.count))
		if p.consumed {
			s.entityDestroyedBy(it.uuid, plugin.RemovePickup)
			s.broadcastWorld(it.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{p.item}))
		}
		s.thrownItemPickedUpAdvancements(it.w, thrower, given, c, advancement.Entity{Type: playerEntityType, Loot: c.LootEntity})
	}
}

// itemByID finds the item entity a pickup phase is working on, nil for one
// gone. The item lock says the answer is not half-way through an append.
func (s *Server) itemByID(id int32) *itemEntity {
	s.itemsMu.Lock()
	defer s.itemsMu.Unlock()
	for _, it := range s.items {
		if it.item.ID == id && !it.item.Dead {
			return it
		}
	}
	return nil
}

// spawnItem lays a stack of items on the ground as an item entity, in the
// world given, at the position given and moving with the velocity given,
// and shows it to every player within its tracking range of that spot. The
// pickupDelay is the ticks before anyone may pick it up: the near-ten of a
// block's drop, the two-second wait of a player's throw. feetY is the Y
// vanilla passes to new ItemEntity, the bottom of the item's box.
func (s *Server) spawnItem(w *world.World, stack protocol.ItemStack, x, feetY, z, vx, vy, vz float64, pickupDelay int) *itemEntity {
	it := s.newItemEntity(w, stack, x, feetY, z, vx, vy, vz, pickupDelay)
	s.registerFreshItem(it)
	return it
}

// newItemEntity builds an item entity without putting it in the world, so a
// caller may set state the spawn metadata reads before registerItem shows
// it. feetY is the bottom of the item's box, as in spawnItem; the entity
// keeps the box's centre.
func (s *Server) newItemEntity(w *world.World, stack protocol.ItemStack, x, feetY, z, vx, vy, vz float64, pickupDelay int) *itemEntity {
	id := s.nextEntity()
	return &itemEntity{
		item:   entity.NewItem(id, stack, x, feetY+entity.ItemHalfSize, z, vx, vy, vz, pickupDelay, s.entitySeed(id)),
		uuid:   uuid.New(),
		w:      w,
		sentVX: vx, sentVY: vy, sentVZ: vz,
	}
}

// registerItem puts a built item entity into the world and shows it to
// every player within the item's tracking range — the same both-locks
// moment spawnItem keeps, the one door every arrival but a wake walks.
// The registration and each watcher's starting view are recorded under
// the player list's and the item's locks together, so a client joining in
// the same moment is either shown the item here or in their own join — a
// double spawn the client resolves by replacing the entity — and neither
// tick can miss a baseline.
func (s *Server) registerItem(it *itemEntity) {
	s.registerItems(it.w, []*itemEntity{it}, true)
}

// registerItems is registerItem for several items of world w in one hold of
// the item lock, so no item tick runs between them: a death's drops all
// exist before the first of them steps or merges, as they do when vanilla
// spawns them inside one main-thread task.
func (s *Server) registerItems(w *world.World, its []*itemEntity, show bool) {
	s.placeItems(w, its, show, true)
}

// placeItems is registerItems; add false only shows items already registered.
func (s *Server) placeItems(w *world.World, its []*itemEntity, show, add bool) {
	s.mu.RLock()
	// Who could see where they fell, read under the player list's lock alone.
	type watcher struct {
		c           *client
		x, z        float64
		chunkRadius int32
	}
	watchers := make([]watcher, 0, len(s.clients))
	for _, c := range s.clients {
		if !show || c.theWorld() != w {
			continue
		}
		pos := c.player.Position()
		watchers = append(watchers, watcher{c: c, x: pos.X, z: pos.Z, chunkRadius: c.chunkRadius()})
	}

	s.itemsMu.Lock()
	shown := make(map[*client][]outbound)
	var order []*client
	for _, it := range its {
		if add {
			it.skipStep = it.skipStep || s.deferItemSteps
			s.items = append(s.items, it)
			itemTracker.file(s, it)
			if s.itemCols != nil {
				s.itemCols.add(it)
			}
		}
		body := itemTracker.body(it)
		var pairing []outbound
		for _, wh := range watchers {
			if !inTrackingRange(wh.x, wh.z, it.item.X, it.item.Z, entity.ItemTrackingRange, int(s.entityBroadcastRange.Load()), wh.chunkRadius) {
				continue
			}
			body.mark(wh.c)
			if pairing == nil {
				// Built under the lock; the tick goroutine may have moved
				// it by the time the sends run.
				pairing = itemPairing(it)
			}
			if _, ok := shown[wh.c]; !ok {
				order = append(order, wh.c)
			}
			shown[wh.c] = append(shown[wh.c], pairing...)
		}
	}
	s.itemsMu.Unlock()
	s.mu.RUnlock()

	for _, c := range order {
		c.sendAllIn(w, shown[c])
	}
}

// itemPairing is what shows an item: the spawn, then the stack.
func itemPairing(it *itemEntity) []outbound {
	return spawnAndMetadata(spawnItemPayload(it), itemMetadataPayload(it))
}

// itemSpill is a destroyed item entity's stack and where its feet were.
type itemSpill struct {
	w       *world.World
	stack   protocol.ItemStack
	x, y, z float64
}

// spillOf records a destroyed item entity for spillContents.
func spillOf(it *itemEntity) itemSpill {
	return itemSpill{w: it.w, stack: it.item.Stack, x: it.item.X, y: it.item.Y - entity.ItemHalfSize, z: it.item.Z}
}

// spillContents lays each destroyed stack's contents down as item entities at
// its position, with the constructor's random toss and no pickup delay.
// vanilla: ItemUtils.onContainerDestroyed
func (s *Server) spillContents(spills []itemSpill) {
	for _, sp := range spills {
		_, isBlock := registry.Item(sp.stack.ItemID).Block()
		for _, inner := range rules.DestroyedContents(sp.stack, isBlock) {
			if inner.Empty() {
				continue
			}
			vx, vy, vz := entity.PopVelocity(s.popRng)
			s.spawnItem(sp.w, inner, sp.x, sp.y, sp.z, vx, vy, vz, 0)
		}
	}
}

// spawnBlockDrop lays one of a broken block's dropped stacks about the
// block's centre and pops it clear of the hole in a little arc (vanilla:
// Block.popResource). Nothing spawns while the block_drops rule is off.
func (s *Server) spawnBlockDrop(w *world.World, x, y, z int32, stack protocol.ItemStack) {
	if !w.BoolRule(world.RuleBlockDrops) {
		return
	}
	px, py, pz := entity.BlockDropSpot(s.popRng, x, y, z)
	vx, vy, vz := entity.PopVelocity(s.popRng)
	s.spawnItem(w, stack, px, py, pz, vx, vy, vz, entity.BreakPickupDelay)
}

// spawnHangingDrop lays a hanging body's broken drop at
// entity.HangingDropSpot, thrown with entity.PopVelocity's toss. centreY is
// the body's centre.
// vanilla: HangingEntity.spawnAtLocation, ItemEntity.<init>
func (s *Server) spawnHangingDrop(w *world.World, dir int32, centreX, centreY, centreZ float64, stack protocol.ItemStack) {
	vx, vy, vz := entity.PopVelocity(s.popRng)
	x, y, z := entity.HangingDropSpot(dir, centreX, centreY, centreZ)
	s.spawnItem(w, stack, x, y, z, vx, vy, vz, entity.BreakPickupDelay)
}

// spawnItemAtFeet lays one stack where a body stands, feet at (x, y, z),
// thrown with entity.PopVelocity's toss.
// vanilla: Entity.spawnAtLocation, ItemEntity.<init>
func (s *Server) spawnItemAtFeet(w *world.World, stack protocol.ItemStack, x, y, z float64) {
	s.registerFreshItem(s.newItemAtFeet(w, stack, x, y, z))
}

// newItemAtFeet builds spawnItemAtFeet's item without putting it in the
// world.
func (s *Server) newItemAtFeet(w *world.World, stack protocol.ItemStack, x, y, z float64) *itemEntity {
	vx, vy, vz := entity.PopVelocity(s.popRng)
	return s.newItemEntity(w, stack, x, y, z, vx, vy, vz, entity.BreakPickupDelay)
}

// spawnJukeboxRecord pops a jukebox's disc out above the block.
// vanilla: JukeboxBlockEntity.popOutTheItem
func (s *Server) spawnJukeboxRecord(w *world.World, x, y, z int32, stack protocol.ItemStack) {
	s.spawnPoppedItem(w, x, y, z, stack)
}

// spawnPoppedItem lays an item just above a block's centre, rolled by
// entity.PoppedItem.
func (s *Server) spawnPoppedItem(w *world.World, x, y, z int32, stack protocol.ItemStack) {
	px, py, pz, vx, vy, vz := entity.PoppedItem(s.popRng, x, y, z)
	s.spawnItem(w, stack, px, py, pz, vx, vy, vz, entity.BreakPickupDelay)
}

// spawnBlockDropFromFace lays one of a broken block's dropped stacks against
// the face that was clicked and throws it that way, rolled by
// entity.FaceDrop. Nothing spawns while the block_drops rule is off.
// vanilla: Block.popResourceFromFace
func (s *Server) spawnBlockDropFromFace(w *world.World, x, y, z int32, face int32, stack protocol.ItemStack) {
	if !w.BoolRule(world.RuleBlockDrops) {
		return
	}
	px, py, pz, vx, vy, vz := entity.FaceDrop(s.popRng, x, y, z, face)
	s.spawnItem(w, stack, px, py, pz, vx, vy, vz, entity.BreakPickupDelay)
}

// decoratedPotSherds is the dynamic drop of a shattered pot: the item on each
// face in the order left, back, front, right. A pot with no sherds recorded,
// or unreadable ones, is EMPTY and gives nothing; one placed from a plain
// item recorded four bricks.
// vanilla: DecoratedPotBlock.getDrops, DecoratedPotBlockEntity.loadAdditional
func decoratedPotSherds(sherds nbt.Compound) []protocol.ItemStack {
	sides, ok := registry.PotDecorationsOf(sherds)
	if !ok {
		return nil
	}
	// sides is indexed back, left, right, front.
	drops := make([]protocol.ItemStack, 0, 4)
	for _, i := range [4]int{1, 0, 3, 2} {
		if !sides[i].Empty() {
			drops = append(drops, sides[i])
		}
	}
	return drops
}

// spillContainer lays what a broken container held around the block it
// stood in, the pieces entity.Spill rolls. The pieces carry no pickup delay,
// so a player standing in the block may claim them at once.
// vanilla: Containers.dropContents
func (s *Server) spillContainer(w *world.World, x, y, z int32, stacks []protocol.ItemStack) {
	s.dropMu.Lock()
	defer s.dropMu.Unlock()
	for _, p := range entity.Spill(s.dropRng, x, y, z, stacks) {
		s.spawnItem(w, p.Stack, p.X, p.Y, p.Z, p.VX, p.VY, p.VZ, 0)
	}
}

// throwItem throws a stack out of the player's hands, at their own x and
// z three tenths of a block under the eyes, moving away in the look
// direction with vanilla's strength, rise and spread, out of the way of
// their own feet for the two seconds a throw cannot be picked up in.
// vanilla: LivingEntity.createItemStackToDrop
func (c *client) throwItem(stack protocol.ItemStack) {
	c.dropItem(stack, nil, false, entity.ThrownPickupDelay, uuid.Nil, false)
}

// throwFromHand throws a stack the player threw from their hand or cursor:
// the item records the player as its thrower and counts as dropped. restore,
// where non-nil, marks the drop key's throw: PlayerDropItemEvent fires before
// the item is shown, and a cancel calls restore to put the stack back where
// it left.
// vanilla: ServerPlayer.drop, LivingEntity.createItemStackToDrop
func (c *client) throwFromHand(stack protocol.ItemStack, restore func()) {
	gated := restore != nil && c.pluginListening(pluginruntime.PlayerDropItem)
	it := c.dropItem(stack, nil, false, entity.ThrownPickupDelay, c.player.UUID, gated)
	if gated {
		if !c.pluginDrop(it) {
			c.srv.killItem(it)
			restore()
			return
		}
		c.srv.placeItems(it.w, []*itemEntity{it}, true, false)
	}
	c.awardItemStat(registry.StatTypeDropped, stack.ItemID, stack.Count)
	c.awardStat(player.Custom(registry.CustomStatDrop), 1)
}

// dropItem builds the item Player.drop makes: thrown from three tenths of a
// block under the pose's eyes with the throw velocity and the given pickup
// delay. target, where non-nil, is the one player who may pick it up; fake marks
// the unpickable item a full /give throws for the client's pickup
// animation, which despawns on the next tick. thrower is uuid.Nil for none.
// hidden registers the item without showing it; the caller shows it or kills it.
// vanilla: LivingEntity.createItemStackToDrop, ItemEntity.makeFakeItem
func (c *client) dropItem(stack protocol.ItemStack, target *uuid.UUID, fake bool, pickupDelay int, thrower uuid.UUID, hidden bool) *itemEntity {
	pos := c.player.Position()
	vx, vy, vz := entity.ThrowVelocity(pos.Yaw, pos.Pitch, c.ownDice())
	it := c.srv.newItemEntity(c.theWorld(), stack,
		pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight-entity.ThrowSpawnBelow, pos.Z,
		vx, vy, vz,
		pickupDelay)
	it.item.Target = target
	it.thrower = thrower
	if fake {
		it.item.MakeFake()
	}
	c.srv.registerItems(it.w, []*itemEntity{it}, !hidden)
	c.srv.itemsSpawned(plugin.SpawnDefault, it)
	return it
}

// containerOf is the block entity's slots as the minecraft:container
// component a shulker box's loot table copies onto the item it drops: one
// entry per slot, running only as far as the last one with something in
// it. Nil or empty gives no component.
func containerOf(be *world.BlockEntity) []protocol.ItemStack {
	if be == nil {
		return nil
	}
	var slots []protocol.ItemStack
	be.Use(func(b *world.BlockEntity) {
		slots = make([]protocol.ItemStack, len(b.Slots))
		copy(slots, b.Slots)
	})
	for len(slots) > 0 && slots[len(slots)-1].Empty() {
		slots = slots[:len(slots)-1]
	}
	if len(slots) == 0 {
		return nil
	}
	return slots
}

// customNameOf is the block entity's custom name as the minecraft:custom_name
// component a container's loot table copies onto the item it drops. Nil for
// an unnamed container or no block entity at all.
func customNameOf(be *world.BlockEntity) *protocol.Text {
	if be == nil {
		return nil
	}
	var name *protocol.Text
	be.Use(func(b *world.BlockEntity) { name = b.CustomName })
	return name
}

// rollBlockLoot rolls a block's loot against ctx, lays the stacks where the
// block broke, and spawns the experience the block leaves (vanilla:
// Block.dropResources, spawnAfterBreak).
func (s *Server) rollBlockLoot(w *world.World, x, y, z int32, state uint16, ctx *rules.LootContext) {
	s.rollBlockLootLater(w, x, y, z, state, ctx, nil)()
}

// rollBlockLootLater rolls the loot now and returns the call that spawns it,
// for a break that must write its cell first.
func (s *Server) rollBlockLootLater(w *world.World, x, y, z int32, state uint16, ctx *rules.LootContext, loot *brokenLoot) func() {
	block := registry.BlockOf(state)
	stacks := rules.RollLoot(rules.BlockLootTable(block), ctx, s.lootRng)
	if loot != nil {
		// A BlockBreakEvent listener already took the experience's roll.
		return func() {
			if loot.items {
				if loot.event != nil {
					stacks = loot.event(stacks)
				}
				for _, stack := range stacks {
					s.spawnBlockDrop(w, x, y, z, stack)
				}
			}
			if loot.experience > 0 {
				s.spawnOrbs(w, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, loot.experience)
			}
		}
	}
	return func() { s.spawnBlockLoot(w, x, y, z, block, ctx.Tool, stacks) }
}

// spawnBlockLoot lays rolled stacks and the block's experience.
func (s *Server) spawnBlockLoot(w *world.World, x, y, z int32, block registry.Block, tool rules.LootTool, stacks []protocol.ItemStack) {
	for _, stack := range stacks {
		s.spawnBlockDrop(w, x, y, z, stack)
	}
	// vanilla: Block.popExperience
	if !w.BoolRule(world.RuleBlockDrops) {
		return
	}
	if amount := rules.BlockBreakExperience(block, tool, s.levelRng); amount > 0 {
		s.spawnOrbs(w, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, amount)
	}
}

// dropBrokenBlock lays the items a broken block leaves behind. Only a hand
// whose materials are not infinite drops anything — a creative break keeps
// nothing — and a block that requires the correct
// tool leaves the wrong tool nothing. What a block gives up is its loot
// table's answer, rolled by the rules package's engine against the
// breaking tool and the state that broke; the experience rides the same
// gate, as vanilla's dropResources reaches spawnAfterBreak only through
// playerDestroy.
// vanilla: Player.preventsBlockDrops
//
// The loot is rolled against the standing block and held, the main-hand
// stack before the break wore it (vanilla's destroyedWith); the returned
// call, nil for no drops, spawns it once the cell is written.
func (c *client) dropBrokenBlock(x, y, z int32, state uint16, held protocol.ItemStack, loot *brokenLoot) func() {
	if state == airBlockState || c.player.GameMode() == protocol.GameModeCreative {
		return nil
	}
	if !rules.HasCorrectToolForDrops(held, registry.BlockOf(state)) {
		return nil
	}
	w := c.theWorld()
	// The block entity the break took out rides the roll: the box drops
	// with what it held and the name it carried, read before it leaves the
	// world. A skull keeps a carried compound rather than a modelled
	// entity, so its profile and name are read from that.
	be := w.BlockEntity(x, y, z)
	ctx := c.srv.brokenBlockLootContext(w, x, y, z, state, customNameOf(be))
	ctx.Container = containerOf(be)
	ctx.Tool = rules.ToolOf(held)
	// The break is credited to the breaker, vanilla's THIS_ENTITY.
	ctx.This = c.LootEntity()
	roll := c.srv.rollBlockLootLater(w, x, y, z, state, &ctx, loot)
	if !rules.IsInfestedBlock(registry.BlockOf(state)) || !rules.InfestationSpawns(ctx.Tool) {
		return roll
	}
	return func() {
		roll()
		c.srv.spawnInfestation(w, x, y, z)
	}
}

// dropCreativeShulkerBox drops a broken non-empty shulker box with its
// contents and name in creative, where nothing else drops. The item appears
// at the block's centre with an ordinary item entity's random toss, not a
// block drop's jittered spot.
// vanilla: ShulkerBoxBlock.playerWillDestroy
func (c *client) dropCreativeShulkerBox(x, y, z int32, state uint16) {
	if !registry.BlockOf(state).IsShulkerBox() {
		return
	}
	w := c.theWorld()
	be := w.BlockEntity(x, y, z)
	ctx := c.srv.brokenBlockLootContext(w, x, y, z, state, customNameOf(be))
	ctx.Container = containerOf(be)
	if len(ctx.Container) == 0 {
		return
	}
	for _, stack := range rules.RollLoot(rules.BlockLootTable(registry.BlockOf(state)), &ctx, c.srv.lootRng) {
		c.srv.spawnItemAtFeet(w, stack, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5)
	}
}

// dropCreativeHive drops a broken beehive or bee nest in creative when it
// holds bees or honey: the item carries the bees and the honey level, and
// appears at the block's corner. An empty dry hive drops nothing.
// vanilla: BeehiveBlock.playerWillDestroy
func (c *client) dropCreativeHive(x, y, z int32, state uint16) {
	w := c.theWorld()
	if !registry.BlockOf(state).InTag(registry.TagIDBeehives) || !w.BoolRule(world.RuleBlockDrops) {
		return
	}
	level, _ := registry.StateInt(state, registry.PropHoneyLevel)
	if len(hiveBees(w, x, y, z).Items) == 0 && level == 0 {
		return
	}
	itemID, ok := registry.CloneItem(state)
	if !ok {
		return
	}
	stack := protocol.ItemStack{ItemID: itemID, Count: 1}
	if components, ok := w.ItemComponentsAt(x, y, z); ok && components.Bees != nil {
		stack.SetBees(components.Bees)
	}
	stack.SetBlockStateProperties(map[string]string{"honey_level": strconv.Itoa(level)})
	c.srv.spawnItemAtFeet(w, stack, float64(x), float64(y), float64(z))
}

// breakLootContext builds the context a broken block rolls its loot against:
// the state that stood there, the block entity's slots, name and raw
// components the caller read before it left the world, and the partner a
// broken half's table reads through a location check. A shattered decorated
// pot's sherds are its faces' own, a dynamic drop the loot table takes instead
// of the pot.
// vanilla: Block.dropResources
func (s *Server) breakLootContext(w *world.World, x, y, z int32, state uint16, customName *protocol.Text, components world.ItemComponents) rules.LootContext {
	var scratch protocol.ItemStack
	if components.Profile != nil {
		scratch.SetProfile(*components.Profile)
	}
	if components.NoteBlockSound != "" {
		scratch.SetNoteBlockSound(components.NoteBlockSound)
	}
	if len(components.BannerPatterns) > 0 {
		scratch.SetBannerPatterns(components.BannerPatterns)
	}
	if len(components.Bees) > 0 {
		scratch.SetBees(components.Bees)
	}
	if customName == nil {
		customName = components.CustomName
	}
	ctx := rules.LootContext{
		BlockState:    state,
		HasBlock:      true,
		Container:     components.Container,
		CustomName:    customName,
		Components:    scratch.Raw,
		Lock:          components.Lock,
		ContainerLoot: components.ContainerLoot,
		// A broken half's table reads its still-standing partner.
		BlockAt: func(dx, dy, dz int32) uint16 { return w.BlockAt(x+dx, y+dy, z+dz) },
		Level:   s.lootLevelAtBlock(w, x, y, z),
	}
	if registry.BlockOf(state) == registry.BlockDecoratedPot {
		var sherds nbt.Compound
		if be := w.BlockEntity(x, y, z); be != nil {
			be.Use(func(b *world.BlockEntity) {
				if b.Sherds != nil {
					sherds = b.Sherds.Entries()
				}
			})
		}
		ctx.Dynamic = map[string][]protocol.ItemStack{"minecraft:sherds": decoratedPotSherds(sherds)}
		// A whole pot keeps its sides through copy_components.
		if sides := world.PotDecorationsPatch(sherds); sides != nil {
			var scratch protocol.ItemStack
			scratch.SetPotDecorations(*sides)
			ctx.Components = append(slices.Clone(ctx.Components), scratch.Raw...)
		}
	}
	return ctx
}

// brokenBlockLootContext builds the context a broken block's loot rolls
// against, reading the carried compound a skull, banner or hive keeps its
// components and custom name in, and holding the block entity for the
// table's block_entity, before the break takes it out of the world. The caller reads and
// removes the modelled block entity itself and sets Container on the result;
// the name falls back to the carried one when the caller has none.
// vanilla: Block.dropResources
func (s *Server) brokenBlockLootContext(w *world.World, x, y, z int32, state uint16, customName *protocol.Text) rules.LootContext {
	components, tag := brokenBlockEntity(w, x, y, z)
	ctx := s.breakLootContext(w, x, y, z, state, customName, components)
	ctx.BlockEntity = tag
	ctx.Extra = components.Extra
	return ctx
}

// dropHeld throws the held stack out of the player's hand: the whole of it
// for the drop-stack action, a single item for the drop one. The slot is
// told back to the client, which decrements its own stack only when the
// server confirms. What is left in the slot keeps the wear the thrown part
// took with it as its own — the wear being of the item, not the stack it
// sat in — and the other players see the hand lighten through the
// equipment broadcast.
func (c *client) dropHeld(wholeStack bool) error {
	var slot int
	var left, taken, held protocol.ItemStack
	c.player.Inventory(func(inv *player.Inventory) {
		slot = player.HotbarSlot + int(inv.Selected)
		held = inv.Slots[slot]
		if held.Empty() {
			return
		}
		taken = held
		if !wholeStack {
			taken.Count = 1
		}
		left = held
		left.Count = held.Count - taken.Count
		inv.Set(slot, left)
	})
	if taken.Empty() {
		// The client sends the action with an empty hand too, and the
		// cooldown resets all the same. vanilla: ServerPlayer.drop
		c.attackStrengthTicks.Store(0)
		return nil
	}
	var payload []byte
	c.player.Inventory(func(inv *player.Inventory) {
		payload = inv.WindowSlot(playerWindowID, int32(slot), left)
	})
	if err := c.writePacket(protocol.IDPlayContainerSlot, "set container slot", payload); err != nil {
		return err
	}
	c.broadcastEquipment()
	// A use of the stack that just emptied ends; the dropper's client swings
	// its own arm, so only the others are told.
	// vanilla: ServerPlayer.drop, LivingEntity.drop
	if app := c.player.Appearance(); left.Empty() && app.Eating && app.EatingHand != protocol.HandOff {
		c.stopUsingItem()
	}
	c.srv.broadcastExcept(c, protocol.IDPlaySwingAnimation, swingArm(c.player, protocol.HandMain))
	c.throwFromHand(taken, func() {
		c.player.Inventory(func(inv *player.Inventory) { inv.Set(slot, held) })
		c.resyncHand(protocol.HandMain)
	})
	c.attackStrengthTicks.Store(0)
	return nil
}

// consumeHeld takes one item out of the hand the player placed from, the
// cost of building in survival. The client is told the slot it left, and
// the other players see the hand lighten through the equipment broadcast.
func (c *client) consumeHeld(hand int32) error {
	var slot int
	var stack protocol.ItemStack
	var payload []byte
	c.player.Inventory(func(inv *player.Inventory) {
		if hand == protocol.HandOff {
			slot = player.OffHandSlot
		} else {
			slot = player.HotbarSlot + int(inv.Selected)
		}
		stack = inv.Slots[slot]
		if !stack.Empty() {
			stack.Count--
		}
		inv.Set(slot, stack)
		payload = inv.WindowSlot(playerWindowID, int32(slot), stack)
	})
	if err := c.writePacket(protocol.IDPlayContainerSlot, "set container slot", payload); err != nil {
		return err
	}
	c.broadcastEquipment()
	return nil
}

// deathDrops is what a death would drop from the inventory: every stack
// without the vanishing curse, in slot order, then the body and saddle
// slots. A spectator drops nothing. vanilla: Player.destroyVanishingCursedItems
func (c *client) deathDrops() []protocol.ItemStack {
	if c.player.GameMode() == protocol.GameModeSpectator {
		return nil
	}
	var drops []protocol.ItemStack
	c.player.Inventory(func(inv *player.Inventory) {
		keep := func(stack protocol.ItemStack) {
			if !stack.Empty() && !rules.PreventsEquipmentDrop(stack) {
				drops = append(drops, stack)
			}
		}
		for slot, stack := range inv.Slots {
			if slot >= player.ArmorSlot {
				keep(stack)
			}
		}
		keep(inv.Body)
		keep(inv.Saddle)
	})
	return drops
}

// dropInventory empties everything the player is carrying, scatters drops
// (deathDrops, as a plugin left them) as item entities around them, and
// returns a Set Container Slot packet for every slot the death emptied, for
// the caller to send the dying player — the vanilla sync reaching the dying
// client the same way. A spectator drops nothing, its inventory kept for the
// respawn. The 2x2 grid is the window's, not the inventory's, and stays until
// the respawn removes the body. vanilla: Inventory.dropAll
func (c *client) dropInventory(drops []protocol.ItemStack) [][]byte {
	if c.player.GameMode() == protocol.GameModeSpectator {
		return nil
	}
	var emptied [][]byte
	c.player.Inventory(func(inv *player.Inventory) {
		var grid [player.ArmorSlot]protocol.ItemStack
		copy(grid[:], inv.Slots[:])
		for slot, stack := range inv.Slots {
			if !stack.Empty() && slot >= player.ArmorSlot {
				emptied = append(emptied, inv.WindowSlot(playerWindowID, int32(slot), protocol.ItemStack{}))
			}
		}
		inv.Clear()
		copy(inv.Slots[:], grid[:])
	})
	pos := c.player.Position()
	// Every stack leaves the body's eyes, three tenths down, scattered on
	// a random heading. vanilla: LivingEntity.createItemStackToDrop
	dropY := pos.Y + c.dims(c.player.Appearance().Pose).EyeHeight - entity.ThrowSpawnBelow
	w := c.theWorld()
	its := make([]*itemEntity, 0, len(drops))
	for _, stack := range drops {
		vx, vy, vz := entity.DropVelocity(c.srv.popRng)
		its = append(its, c.srv.newItemEntity(w, stack, pos.X, dropY, pos.Z, vx, vy, vz, entity.ThrownPickupDelay))
	}
	c.srv.registerItems(w, its, true)
	c.srv.itemsSpawned(plugin.SpawnDefault, its...)
	return emptied
}

// spawnItemPayload builds the Spawn Entity packet that puts an item entity
// in the world for a client: entity ID and UUID, the minecraft:item type.
// No type-specific data; the stack follows in the metadata packet.
func spawnItemPayload(it *itemEntity) []byte {
	return spawnEntityPayload(spawnEntity{
		id: it.item.ID, uuid: it.uuid, typ: protocol.EntityTypeItem,
		x: it.item.X, y: it.item.Y - entity.ItemHalfSize, z: it.item.Z,
		vx: it.item.VX, vy: it.item.VY, vz: it.item.VZ,
	})
}

// itemMetadataPayload builds the Set Entity Metadata packet of an item
// entity's non-default values: the stack always, the flags, name and no-gravity
// when set.
// vanilla: SynchedEntityData.getNonDefaultValues
func itemMetadataPayload(it *itemEntity) []byte {
	return itemMetadataTable(it.item.ID, itemMetaSpawn, itemMetaOf(it), itemMeta{})
}

// itemFields is every synched value of an item entity, defaults included.
func itemFields(it *itemEntity) metaFields { return itemMetaOf(it).fields() }

// itemMeta is an item entity's synched values.
type itemMeta struct {
	fire, noGravity bool
	custom          customName
	look            entityLook
	stack           *protocol.ItemStack
}

func itemMetaOf(it *itemEntity) itemMeta {
	return itemMeta{fire: it.item.FireFlag(), noGravity: it.item.NoGravity, custom: it.custom, look: it.look, stack: &it.item.Stack}
}

// itemMetaWalk is which walk of itemMetadataTable runs.
type itemMetaWalk uint8

const (
	// itemMetaSpawn compares with the defaults and always writes the stack.
	itemMetaSpawn itemMetaWalk = iota
	// itemMetaTick compares with what the watchers were last told; the stack
	// is never part of it, a stack change sends the spawn walk.
	itemMetaTick
)

// itemMetadataTable builds the Set Entity Metadata packet of one walk of the
// table of an item entity's metadata fields, each written where now and was
// differ, or nil where none does.
// vanilla: SynchedEntityData.getNonDefaultValues, packDirty
func itemMetadataTable(id int32, walk itemMetaWalk, now, was itemMeta) []byte {
	var b protocol.Buffer
	b.PutVarInt(id)
	head := b.Len()
	if flags := now.look.flags(now.fire); flags != was.look.flags(was.fire) {
		b.PutMetadata(protocol.MetadataIndexEntityFlags, protocol.MetadataByte)
		b.PutU8(flags)
	}
	now.look.putSilent(&b, was.look.silent)
	now.custom.putMetadata(&b, was.custom)
	if now.noGravity != was.noGravity {
		b.PutMetadata(protocol.MetadataIndexNoGravity, protocol.MetadataBoolean)
		b.PutBool(now.noGravity)
	}
	if walk == itemMetaSpawn {
		b.PutMetadata(protocol.MetadataIndexItemSlot, protocol.MetadataSlot)
		b.PutSlot(*now.stack)
	}
	if b.Len() == head {
		return nil
	}
	b.EndMetadata()
	return b.Bytes()
}

// fields is an item entity's synched values for the /data diff, in index
// order.
func (m itemMeta) fields() metaFields {
	var f metaFields
	f.add(protocol.MetadataIndexEntityFlags, protocol.MetadataByte, func(b *protocol.Buffer) { b.PutU8(m.look.flags(m.fire)) })
	m.look.addSilentField(&f)
	m.custom.addFields(&f)
	f.add(protocol.MetadataIndexNoGravity, protocol.MetadataBoolean, func(b *protocol.Buffer) { b.PutBool(m.noGravity) })
	f.add(protocol.MetadataIndexItemSlot, protocol.MetadataSlot, func(b *protocol.Buffer) { b.PutSlot(*m.stack) })
	return f
}

// itemFireDelta builds the Set Entity Metadata packet of an item whose
// on-fire bit turned since its watchers were last told, and records the
// turn; nil if it has not. Called under the item lock.
func itemFireDelta(it *itemEntity) []byte {
	if it.item.FireFlag() == it.sentFire {
		return nil
	}
	now := itemMetaOf(it)
	was := now
	was.fire = it.sentFire
	it.sentFire = now.fire
	return itemMetadataTable(it.item.ID, itemMetaTick, now, was)
}

// sendItemMetadata sends each stack change's packet to the watchers collected
// for it, the clients resolved here, once the item lock is down.
func (s *Server) sendItemMetadata(updates []itemMetadataUpdate) {
	for _, update := range updates {
		for _, id := range update.watchers {
			s.mu.RLock()
			c := s.clients[id]
			s.mu.RUnlock()
			if c == nil || c.theWorld() != update.it.w {
				continue
			}
			c.send(protocol.IDPlayEntityMetadata, update.metadata)
		}
	}
}

// pickupItemPayload builds the Pickup Item packet that plays an item
// entity flying to the player who picked it up. The count is the number of
// items that left the ground.
func pickupItemPayload(collected, collector, count int32) []byte {
	var b protocol.Buffer
	b.PutVarInt(collected)
	b.PutVarInt(collector)
	b.PutVarInt(count)
	return b.Bytes()
}

// removeEntitiesPayload builds the Remove Entities packet taking the
// entities with the given IDs back out of the world.
func removeEntitiesPayload(ids []int32) []byte {
	var b protocol.Buffer
	b.PutVarInt(int32(len(ids)))
	for _, id := range ids {
		b.PutVarInt(id)
	}
	return b.Bytes()
}

// burnSound builds the Sound Effect packet vanilla's Entity.lavaHurt plays
// when the lava's bite lands — for an item, an orb, a player or a mob: the
// generic burn, on the sound source the caller's own sounds go out on, at
// four tenths of a volume and the high pitch the fizz makes.
func (s *Server) burnSound(category int32, x, y, z float64) []byte {
	return soundEffectIDAt(registry.SoundEntityGenericBurn, category,
		x, y, z, 0.4, s.burnPitch())
}

// burnPitch is the high pitch Entity.lavaHurt's fizz plays at.
func (s *Server) burnPitch() float32 { return 2.0 + s.soundRng.Float32()*0.4 }

// extinguishSound builds the Sound Effect packet vanilla plays when a burn
// one of the three was carrying goes out — the water it fell into or the
// rain falling on it: the generic extinguish, at seven tenths of a volume
// and the pitch the two dice of the vanilla call swing around.
func (s *Server) extinguishSound(category int32, x, y, z float64) []byte {
	return soundEffectIDAt(registry.SoundEntityGenericExtinguishFire, category,
		x, y, z, extinguishVolume, s.extinguishPitch())
}

// extinguishVolume is the generic extinguish sound's volume.
// vanilla: Entity.playEntityOnFireExtinguishedSound
const extinguishVolume = 0.7

// extinguishPitch is the pitch the two dice of the extinguish call swing
// around 1.6.
func (s *Server) extinguishPitch() float32 {
	return 1.6 + (s.soundRng.Float32()-s.soundRng.Float32())*0.4
}

// candleExtinguishSound is the fizz a put-out candle plays from the centre
// of its block.
func candleExtinguishSound(x, y, z float64) []byte {
	return soundEffectIDAt(registry.SoundBlockCandleExtinguish, protocol.SoundCategoryBlock,
		x+0.5, y+0.5, z+0.5, 1.0, 1.0)
}

// killItem discards an item entity as vanilla's Entity.kill removes one:
// the body out of the world with nothing laid down behind it.
func (s *Server) killItem(it *itemEntity) {
	s.itemsMu.Lock()
	it.item.Dead = true
	it.killed = true
	s.itemsMu.Unlock()
	s.entityDestroyed(it.uuid)
	s.broadcastWorld(it.w, protocol.IDPlayRemoveEntities,
		removeEntitiesPayload([]int32{it.item.ID}))
}

// teleportItem moves an item entity's feet to the position given.
// vanilla: Entity.teleportTo
func (s *Server) teleportItem(it *itemEntity, to player.Position, by string) {
	itemTracker.teleport(s, it, func() {
		// The simulation runs the box's middle.
		it.item.SetPos(to.X, to.Y+entity.ItemHalfSize, to.Z)
		it.item.VX, it.item.VY, it.item.VZ = 0, 0, 0
		if s.itemCols != nil {
			s.itemCols.add(it)
		}
	})
	logTeleported("item", to, by)
}

// entityTeleportPayload builds the entity_position_sync packet that tells a
// client where an entity it was shown has ended up.
func entityTeleportPayload(id int32, to player.Position, onGround bool) []byte {
	var b protocol.Buffer
	b.Grow(teleportEntityLen)
	protocol.PutEntityPositionSync(&b, id, to.X, to.Y, to.Z, to.Yaw, to.Pitch, onGround)
	return b.Bytes()
}

// itemTwinAt builds the item entity that continues this one's stack in
// another dimension, its feet at pos: a new entity — new ID, the same UUID
// — raised from the old one's entity tag, the same save and load a chunk's
// entity file gives an item. Nil is an unreadable stack. The caller holds
// the item lock or has taken the item out of the list.
// vanilla: Entity.teleportCrossDimension
func (s *Server) itemTwinAt(it *itemEntity, to *dimension, pos player.Position) *itemEntity {
	twin := itemOfTag(s, to.w, itemTag(it).Entries())
	if twin == nil {
		return nil
	}
	twin.item.SetPos(pos.X, pos.Y+entity.ItemHalfSize, pos.Z)
	return twin
}

// finishItemTravel sends one item the portal pass caught across its portal
// (crossPortal); an unreadable stack stays.
func (s *Server) finishItemTravel(it *itemEntity, kind rules.PortalKind) {
	pos := player.Position{X: it.item.X, Y: it.item.Y - entity.ItemHalfSize, Z: it.item.Z}
	s.crossPortal(it.w, it.item.ID, pos, kind, func(to *dimension, spot world.SpawnPos, _ [2]int32) func() {
		twin := s.itemTwinAt(it, to, standingAt(spot))
		if twin == nil {
			slog.Error("could not carry an item's stack across a portal", "item", it.item.Stack.ItemID)
			return nil
		}
		return func() {
			s.registerItem(twin)
			slog.Debug("item travelled through a portal",
				"to", to.name, "x", twin.item.X, "y", twin.item.Y, "z", twin.item.Z)
		}
	}, func() {
		s.itemsMu.Lock()
		s.items = append(s.items, it)
		itemTracker.file(s, it)
		if s.itemCols != nil {
			s.itemCols.add(it)
		}
		s.itemsMu.Unlock()
	})
}

// dampensVibrations reports whether an item entity holding stack is silent to
// vibration listeners. vanilla: ItemEntity.dampensVibrations
func dampensVibrations(stack protocol.ItemStack) bool {
	return registry.Item(stack.ItemID).InTag(registry.ItemTagIDDampensVibrations)
}
