package server

import (
	"math"
	"math/rand"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The fishing rod and its hook: Use Item casts, or retrieves a hook already
// out. The hook's life is entity.Bobber. Hooks are session-bound: nothing
// saves one, and one goes when its angler does.
// vanilla: FishingRodItem.use, FishingHook

// rodPitchSpread is the spread of the throw's and the retrieval's sound
// pitch. vanilla: FishingRodItem.use
const rodPitchSpread = 0.4

// bobberOwnerRangeSquared is the squared 32 blocks a hook is held to its
// angler by. vanilla: FishingHook.shouldStopFishing
const bobberOwnerRangeSquared = 1024.0

// The rod's wear for what a retrieve found; the grounded two overrides the
// rest, and a reel of nothing wears none.
// vanilla: FishingHook.retrieve
const (
	fishingWearBody  = 5
	fishingWearItem  = 3
	fishingWearCatch = 1
	fishingWearBeach = 2
)

// bobberEntity is one hook in a world: the entity.Bobber the simulation
// runs, and the angler whose use decides its life.
type bobberEntity struct {
	hook  *entity.Bobber
	w     *world.World
	uuid  uuid.UUID
	owner uuid.UUID
	bodyTrack[bobberTold]
	shotMark

	// tags are the scoreboard tags. Guarded by the bobber lock.
	tags nbt.StringSet
	// custom is the custom name, guarded like tags.
	custom customName
}

// bobberTold is the catch and the bite one watcher was last told of.
type bobberTold struct {
	hooked int32
	biting bool
	name   customName
}

// bobberTracker tracks the hooks. A hook's Teleport Entity carries no
// velocity: the motion packets do.
var bobberTracker = trackedKind[*bobberEntity, bobberTold]{
	rangeChunks: entity.BobberTracking,
	interval:    entity.BobberInterval,
	lock:        func(s *Server) *sync.Mutex { return &s.bobbersMu },
	list:        func(s *Server) *[]*bobberEntity { return &s.bobbers },
	chunks:      func(s *Server) *entity.ChunkIndex[*bobberEntity] { return &s.bobberChunks },
	filing:      func(it *bobberEntity) *entity.Filing { return &it.hook.Filing },
	uuid:        func(it *bobberEntity) uuid.UUID { return it.uuid },
	track:       func(it *bobberEntity) *bodyTrack[bobberTold] { return &it.bodyTrack },
	world:       func(it *bobberEntity) *world.World { return it.w },
	id:          func(it *bobberEntity) int32 { return it.hook.ID },
	dead:        func(it *bobberEntity) bool { return it.hook.Dead },
	pose: func(it *bobberEntity) bodyPose {
		h := it.hook
		return bodyPose{x: h.X, y: h.Y, z: h.Z, yaw: h.Yaw, pitch: h.Pitch, onGround: h.OnGround}
	},
	pairing: bobberPairing,
	own:     func(it *bobberEntity) bobberTold { return bobberTold{it.hook.HookedID, it.hook.Biting, it.custom} },
}

// newBobberEntity is a hook not yet in the world. Its schedule counts its
// first tick as one, so its movement ticks fall on the fifth, the tenth, …
// of its life rather than on the first.
func newBobberEntity(hook *entity.Bobber, w *world.World, owner uuid.UUID) *bobberEntity {
	it := &bobberEntity{hook: hook, w: w, uuid: uuid.New(), owner: owner}
	it.tickCount = 1
	return it
}

// handleUseFishingRod casts the rod in hand, or retrieves the angler's hook
// already out. It reports whether the item was the rod.
// vanilla: FishingRodItem.use
func (c *client) handleUseFishingRod(hand int32) bool {
	if c.player.Vitals().Dead {
		return false
	}
	if c.player.GameMode() == protocol.GameModeSpectator {
		return false
	}
	held := c.heldItem(hand)
	if registry.Item(held.ItemID) != registry.ItemFishingRod {
		return false
	}
	if it := c.hookOf(); it != nil {
		c.retrieveBobber(hand, it)
	} else {
		c.castBobber(hand)
	}
	return true
}

// hookOf is the angler's live hook, nil while their rod is at rest.
func (c *client) hookOf() *bobberEntity {
	c.srv.bobbersMu.Lock()
	defer c.srv.bobbersMu.Unlock()
	for _, it := range c.srv.bobbers {
		if it.owner == c.player.UUID && !it.hook.Dead {
			return it
		}
	}
	return nil
}

// takeBobber takes the hook out of the world's list and returns the
// clients that were shown it, for the removal the taker sends. The
// watchers are resolved once the lock is down: bobbersMu is a leaf lock,
// and the player list is read outside it.
// vanilla: Entity.discard
func (s *Server) takeBobber(it *bobberEntity) []*client {
	s.bobbersMu.Lock()
	alive := s.bobbers[:0]
	for _, other := range s.bobbers {
		if other != it {
			alive = append(alive, other)
		} else {
			bobberTracker.unfile(other)
		}
	}
	s.bobbers = alive
	it.hook.Dead = true
	s.entityDestroyed(it.uuid)
	shown := trackedWatchers(it.tracked)
	s.bobbersMu.Unlock()
	var watchers []*client
	for _, id := range shown {
		if c := s.clientByUUID(id); c != nil && c.theWorld() == it.w {
			watchers = append(watchers, c)
		}
	}
	return watchers
}

// killBobber takes a hook out of the world, its removal sent to the world
// it stood in.
// vanilla: Entity.kill
func (s *Server) killBobber(it *bobberEntity) {
	s.takeBobber(it)
	s.broadcastWorld(it.w, protocol.IDPlayRemoveEntities,
		removeEntitiesPayload([]int32{it.hook.ID}))
}

// teleportBobber moves a hook to the position given, its motion stopped;
// its next movement tick tells the watchers.
// vanilla: Entity.teleportTo
func (s *Server) teleportBobber(it *bobberEntity, to player.Position, by string) {
	bobberTracker.teleport(s, it, func() {
		it.hook.SetPos(to.X, to.Y, to.Z)
		it.hook.VX, it.hook.VY, it.hook.VZ = 0, 0, 0
	})
	logTeleported("bobber", to, by)
}

// bobberTwinAt builds the hook that continues this one in another
// dimension at pos: a copy with a new ID, let go of the body it was caught
// in and watched by no one yet. The caller holds the bobbers' lock or has
// taken the hook out of the list.
// vanilla: Entity.teleportCrossDimension
func (s *Server) bobberTwinAt(it *bobberEntity, to *dimension, pos player.Position) *bobberEntity {
	body := *it.hook
	body.ID = s.nextEntity()
	body.SetPos(pos.X, pos.Y, pos.Z)
	body.HookedID = 0
	twin := *it
	twin.hook, twin.w = &body, to.w
	twin.tracked = nil
	twin.tags = nbt.NewStringSet(it.tags.Values())
	return &twin
}

// castBobber puts the angler's hook into their world, carrying the luck and
// the lure of the rod's enchantments. The throw's sound plays before the
// hook is shown; a cast costs the rod no wear.
// vanilla: FishingRodItem.use, FishingHook(Player, Level, int, int)
func (c *client) castBobber(hand int32) {
	s := c.srv
	pos := c.player.Position()
	eye := pos.Y + c.dims(c.player.Appearance().Pose).EyeHeight
	held := c.heldItem(hand)
	id := s.nextEntity()
	hook := entity.NewBobber(id, pos.X, eye, pos.Z,
		float64(pos.Yaw), float64(pos.Pitch), c.player.EntityID,
		s.entitySeed(id), rules.FishingLuck(held, c.itemEnchContext()), rules.FishingLureTicks(held, c.itemEnchContext()))
	it := newBobberEntity(hook, c.theWorld(), c.player.UUID)
	// The throw's sound, at the angler's feet, half a block loud.
	s.broadcastSoundID(c.theWorld(),
		registry.SoundEntityFishingBobberThrow, protocol.SoundCategoryNeutral,
		pos.X, pos.Y, pos.Z, 0.5, rodPitch(s.soundRng))

	// A cancelled cast leaves the sound, the use stat and the vibration.
	if c.pluginFish(plugin.FishCast, it, nil, nil) {
		s.registerBobber(it)
	}
	// vanilla: FishingRodItem.use
	c.awardItemStat(registry.StatTypeUsed, held.ItemID, 1)
	c.useVibration(held, rules.GameEventItemInteractStart)
}

// rodPitch is the roll the throw's and the retrieval's sounds make their
// pitch with.
// vanilla: FishingRodItem.use
func rodPitch(r *rand.Rand) float32 {
	return float32(rodPitchSpread / (r.Float64()*rodPitchSpread + 0.8))
}

// retrieveBobber takes the angler's hook back and pays out what it holds.
// The hook comes out of the world first, so the tick has stopped stepping
// it before the retrieve reads it. Out of range or across a world, only the
// hook comes back.
// vanilla: FishingRodItem.use, FishingHook.retrieve
func (c *client) retrieveBobber(hand int32, it *bobberEntity) {
	s := c.srv
	pos := c.player.Position()
	watchers := s.takeBobber(it)
	hook := it.hook
	dx := pos.X - hook.X
	dy := pos.Y - hook.Y
	dz := pos.Z - hook.Z
	// The shouldStopFishing of a retrieve: the rod is in hand or the use
	// is not this one, so only the range and the world can refuse.
	near := c.theWorld() == it.w && dx*dx+dy*dy+dz*dz <= bobberOwnerRangeSquared

	var wear int32
	if near && hook.HookedID != 0 {
		wear = fishingWearBody
		var hooked plugin.Entity
		if c.pluginListening(pluginruntime.PlayerFish) {
			hooked = s.hookedPluginEntity(it)
		}
		if !c.pluginFish(plugin.FishCaughtEntity, it, hooked, nil) {
			// A cancelled pull leaves the body alone and the rod unworn.
			wear = 0
		} else if pulled, wasItem := s.pullHookedBody(it, pos); pulled {
			// vanilla: FishingHook.retrieve fires with no items for any hooked entity
			c.fishingRodHookedAdvancements(nil)
			if wasItem {
				wear = fishingWearItem
			}
		} else {
			// The body it held has gone from the world between ticks; the
			// reel the plain hook takes is what is left.
			wear = 0
		}
	} else if near && hook.Nibble > 0 {
		// The catch: the loot roll, each stack tossed a tenth of the way
		// toward the angler, lifted a quarter root of the distance, and an
		// orb a stack.
		wear = fishingWearCatch
		lift := 0.08 * math.Sqrt(math.Sqrt(dx*dx+dy*dy+dz*dz))
		luck := float64(hook.Luck) + c.luckAttribute()
		biome := it.w.BiomeAt(int32(math.Floor(hook.X)), int32(math.Floor(hook.Y)), int32(math.Floor(hook.Z)))
		loot := rules.FishingLoot(hook.Rand, luck, hook.OpenWater, biome, s.lootLevelAt(it.w, hook.X, hook.Y, hook.Z))
		c.fishingRodHookedAdvancements(loot)
		for _, stack := range loot {
			item := s.newItemEntity(it.w, stack, hook.X, hook.Y, hook.Z,
				dx*0.1, dy*0.1+lift, dz*0.1, 0)
			xp := hook.Rand.Intn(6) + 1
			if !c.pluginFish(plugin.FishCaught, it, pluginItemEntity{pluginEntity{host: s.pluginAdapter, id: item.uuid}}, &xp) {
				wear = 0
				break
			}
			s.registerFreshItem(item)
			if registry.Item(stack.ItemID).InTag(registry.ItemTagIDFishes) {
				c.awardStat(player.Custom(registry.CustomStatFishCaught), 1)
			}
			vx, vy, vz := s.orbPopVector()
			s.spawnOrb(c.theWorld(), pos.X, pos.Y+0.5, pos.Z+0.5, vx, vy, vz, xp)
		}
	} else if near && !hook.OnGround {
		c.pluginFish(plugin.FishReelIn, it, nil, nil)
	}
	if near && hook.OnGround {
		wear = fishingWearBeach
		if !c.pluginFish(plugin.FishInGround, it, nil, nil) {
			wear = 0
		}
	}

	for _, watcher := range watchers {
		watcher.send(protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{hook.ID}))
	}
	// The retrieval's sound, at the angler's feet, however the reel went.
	s.broadcastSoundID(c.theWorld(),
		registry.SoundEntityFishingBobberRetrieve, protocol.SoundCategoryNeutral,
		pos.X, pos.Y, pos.Z, 1.0, rodPitch(s.soundRng))

	if near && wear > 0 {
		c.wearHeld(hand, wear)
	}
	// A rod the reel's wear broke is an empty stack and posts none.
	// vanilla: FishingRodItem.use
	if held := c.heldItem(hand); registry.Item(held.ItemID) == registry.ItemFishingRod {
		c.useVibration(held, rules.GameEventItemInteractFinish)
	}
}

// hookedPluginEntity is the plugin handle of the body a hook holds, nil for a
// kind plugins have no handle for.
func (s *Server) hookedPluginEntity(it *bobberEntity) plugin.Entity {
	id := it.hook.HookedID
	if body := s.relatedByID(it.w, id); body != nil {
		return s.pluginAdapter.pluginEntityOf(body)
	}
	s.itemsMu.Lock()
	defer s.itemsMu.Unlock()
	for _, lying := range s.items {
		if lying.item.ID == id {
			return pluginItemEntity{pluginEntity{host: s.pluginAdapter, id: lying.uuid}}
		}
	}
	return nil
}

// luckAttribute is the player's minecraft:luck attribute value, vanilla's
// Player.getLuck, added as a float to the rod's Luck of the Sea in the
// catch's roll.
func (c *client) luckAttribute() float64 {
	var luck float64
	c.player.Attributes(func(a *player.Attributes) {
		luck = a.Value(registry.AttributeLuck)
	})
	return luck
}

// pullHookedBody pulls the body the hook is caught in toward the angler,
// vanilla's pullEntity, and tells the wire of it: the hooked player reads
// their own pull off the entity event, and a mob's velocity goes out at
// once because the pull lands outside its tick. Reports whether the body
// was found in the world, and whether it was an item — the one kind the
// reel wears three rather than five.
// vanilla: FishingHook.retrieve, FishingHook.pullEntity, Level.broadcastEntityEvent
func (s *Server) pullHookedBody(it *bobberEntity, angler player.Position) (pulled, wasItem bool) {
	hook := it.hook
	dx := (angler.X - hook.X) * 0.1
	dy := (angler.Y - hook.Y) * 0.1
	dz := (angler.Z - hook.Z) * 0.1

	s.mobsMu.Lock()
	var mob *entity.Mob
	var watchers []uuid.UUID
	for _, dim := range s.allDims() {
		for _, m := range dim.mobs.herd {
			if m.ID == hook.HookedID {
				m.VX += dx
				m.VY += dy
				m.VZ += dz
				mob = m
				if ent := dim.mobs.book[m]; ent != nil {
					watchers = trackedWatchers(ent.tracked)
				}
			}
		}
	}
	vx, vy, vz := 0.0, 0.0, 0.0
	if mob != nil {
		vx, vy, vz = mob.VX, mob.VY, mob.VZ
	}
	s.mobsMu.Unlock()
	if mob != nil {
		s.tellWatchers(it.w, watchers, []outbound{{id: protocol.IDPlaySetEntityMotion,
			data: entityVelocityPayload(mob.ID, vx, vy, vz)}})
		pulled = true
	}

	s.itemsMu.Lock()
	for _, lying := range s.items {
		if lying.item.ID == hook.HookedID && !lying.item.Dead {
			lying.item.VX += dx
			lying.item.VY += dy
			lying.item.VZ += dz
			// The item's own tick carries the shove to the watchers.
			pulled, wasItem = true, true
		}
	}
	s.itemsMu.Unlock()

	// The rest of the bodies a hook may take: each moved the same way, its
	// own tick carrying the velocity to the watchers. A hanging painting or
	// frame keeps no velocity here, so the pull is only the unhook.
	s.tntMu.Lock()
	for _, primed := range s.tnts {
		if primed.tnt.ID == hook.HookedID && !primed.tnt.Dead {
			primed.tnt.Push(dx, dy, dz)
			pulled = true
		}
	}
	s.tntMu.Unlock()

	s.boatsMu.Lock()
	for _, boat := range s.boats {
		if boat.boat.ID == hook.HookedID && !boat.boat.Dead {
			boat.boat.VX += dx
			boat.boat.VY += dy
			boat.boat.VZ += dz
			pulled = true
		}
	}
	s.boatsMu.Unlock()

	s.fallingMu.Lock()
	for _, fall := range s.fallings {
		if fall.fb.ID == hook.HookedID && !fall.fb.Dead {
			fall.fb.Push(dx, dy, dz)
			pulled = true
		}
	}
	s.fallingMu.Unlock()

	s.framesMu.Lock()
	for _, frame := range s.frames {
		if frame.id == hook.HookedID {
			pulled = true
		}
	}
	s.framesMu.Unlock()

	s.paintingsMu.Lock()
	for _, painting := range s.paintings {
		if painting.id == hook.HookedID {
			pulled = true
		}
	}
	s.paintingsMu.Unlock()

	s.armorStandsMu.Lock()
	for _, stand := range s.armorStands {
		if stand.stand.ID == hook.HookedID && !stand.stand.Dead {
			stand.stand.VX += dx
			stand.stand.VY += dy
			stand.stand.VZ += dz
			pulled = true
		}
	}
	s.armorStandsMu.Unlock()

	s.mu.RLock()
	found := false
	for _, other := range s.clients {
		if other.player != nil && other.player.EntityID == hook.HookedID {
			found = true
		}
	}
	s.mu.RUnlock()
	if found {
		// The player's pull is their client's own, off the event below.
		pulled = true
	}
	if pulled {
		s.broadcastWorld(it.w, protocol.IDPlayEntityEvent,
			entityEventPayload(hook.ID, protocol.EntityStatusFishingPull))
	}
	return pulled, wasItem
}

// registerBobber puts a built hook into the world and shows it to every
// player in range.
func (s *Server) registerBobber(it *bobberEntity) { bobberTracker.register(s, it) }

// bobberPairing is what shows a hook: the spawn alone while the wire's
// defaults still describe it, with the metadata of a catch or a bite.
func bobberPairing(it *bobberEntity) []outbound {
	spawn := bobberSpawnPayload(it)
	meta := bobberMetadataPayload(it)
	if meta == nil {
		return bundle([]outbound{{id: protocol.IDPlaySpawnEntity, data: spawn}})
	}
	return spawnAndMetadata(spawn, meta)
}

// bobberSpawnPayload builds a hook's Spawn Entity; the data field names the
// angler. vanilla: FishingHook.getAddEntityPacket
func bobberSpawnPayload(it *bobberEntity) []byte {
	h := it.hook
	return spawnEntityPayload(spawnEntity{
		id: h.ID, uuid: it.uuid, typ: protocol.EntityTypeFishingBobber,
		x: h.X, y: h.Y, z: h.Z,
		vx: h.VX, vy: h.VY, vz: h.VZ,
		pitch: encodeAngle(h.Pitch),
		yaw:   encodeAngle(h.Yaw),
		data:  h.OwnerID,
	})
}

// bobberMetadataPayload builds the Set Entity Metadata packet a hook is drawn
// with: the fields that are not the client's defaults, or nil where none is.
func bobberMetadataPayload(it *bobberEntity) []byte {
	return bobberMetadataTable(it, bobberTold{})
}

// bobberMetadataDeltaPayload builds the metadata update of the fields that
// turned since what one watcher was last told.
func bobberMetadataDeltaPayload(it *bobberEntity, was bobberTold) []byte {
	return bobberMetadataTable(it, was)
}

// bobberMetadataTable is the table of a hook's own two fields, each declared
// once with its index, its serializer and its value, written where it
// differs from was, or nil where none does.
// vanilla: SynchedEntityData.getNonDefaultValues, packDirty
func bobberMetadataTable(it *bobberEntity, was bobberTold) []byte {
	h := it.hook
	var b protocol.Buffer
	b.PutVarInt(h.ID)
	head := b.Len()
	if h.HookedID != was.hooked {
		b.PutMetadata(protocol.MetadataIndexHookedEntity, protocol.MetadataVarInt)
		b.PutVarInt(bobberHookedWire(h.HookedID))
	}
	// "Is catchable" on the wire: the nibble window the client pops its
	// bobber down in.
	if h.Biting != was.biting {
		b.PutMetadata(protocol.MetadataIndexBite, protocol.MetadataBoolean)
		b.PutBool(h.Biting)
	}
	it.custom.putMetadata(&b, was.name)
	if b.Len() == head {
		return nil
	}
	b.EndMetadata()
	return b.Bytes()
}

// bobberHookedWire is the hooked-entity field's value: the body's entity ID
// plus one, 0 for none. vanilla: FishingHook.setHookedEntity
func bobberHookedWire(hookedID int32) int32 {
	if hookedID == 0 {
		return 0
	}
	return hookedID + 1
}

// bobberBiteSound is the splash of one hook's bite.
type bobberBiteSound struct {
	w       *world.World
	x, y, z float64
	pitch   float32
}

// tickBobbers runs one game tick of every hook. The anglers and the bodies
// a hook may take are read before the bobber and mob locks; the sends run
// after them.
func (s *Server) tickBobbers() {
	// No hook out: the item boxes and owner reads below would be work for
	// nothing.
	s.lockTick(&s.bobbersMu)
	none := len(s.bobbers) == 0
	s.unlockTick(&s.bobbersMu)
	if none {
		return
	}

	// The players of every world, read before any lock.
	type angler struct {
		c           *client
		w           *world.World
		pos         player.Position
		chunkRadius int32
		rod         bool
		canInteract bool
		id          int32
		uuid        uuid.UUID
	}
	s.mu.RLock()
	byUUID := make(map[uuid.UUID]*angler, len(s.clients))
	watchers := make(map[*world.World][]*angler)
	for _, c := range s.clients {
		if c.player == nil {
			continue
		}
		a := &angler{
			c: c, w: c.theWorld(), pos: c.player.Position(), chunkRadius: c.chunkRadius(),
			canInteract: !c.player.Vitals().Dead && c.player.GameMode() != protocol.GameModeSpectator,
			id:          c.player.EntityID, uuid: c.player.UUID,
		}
		a.rod = isFishingRod(c.heldItem(protocol.HandMain)) || isFishingRod(c.heldItem(protocol.HandOff))
		byUUID[c.player.UUID] = a
		watchers[a.w] = append(watchers[a.w], a)
	}
	s.mu.RUnlock()
	near := nearbyWorlds(watchers, func(a *angler) trackingWatcher {
		return trackingWatcher{c: a.c, x: a.pos.X, z: a.pos.Z, chunkRadius: a.chunkRadius}
	})
	defer releaseNearbyWorlds(near)

	// The bodies other than mobs and players, read before the hook pass.
	extraTargets := s.bobberExtraTargets()
	_, _, boatRoots := s.boatSnapshots()
	s.lockTick(&s.itemsMu)
	for _, lying := range s.items {
		if lying.item.Dead {
			continue
		}
		minX, minY, minZ, maxX, maxY, maxZ := lying.item.BlastBox()
		extraTargets[lying.w] = append(extraTargets[lying.w], entity.ArrowTarget{
			EntityID: lying.item.ID,
			Type:     protocol.EntityTypeItem,
			MinX:     minX, MinY: minY, MinZ: minZ,
			MaxX: maxX, MaxY: maxY, MaxZ: maxZ,
		})
	}
	s.unlockTick(&s.itemsMu)

	// gone holds the removals, for a watcher the range has left or a hook
	// that died, sent after everything else.
	var (
		out, gone      trackSends
		metas          sendList
		bites          []bobberBiteSound
		targetHits     []targetBlockHit
		projectileHits []projectileBlockHit
		particles      []bobberParticle
		fishing        []bobberFishing
		splashes       []projectileSplash
		landings       []projectileLanding
		bodyLandings   []bodyLanding
		shots          []projectileShot
	)

	listening := s.plugins != nil && s.plugins.Has(pluginruntime.PlayerFish)
	type bobberWorldTargets struct {
		targets []entity.ArrowTarget
		index   map[int32]int // a mob's or player's entry, by entity ID
	}
	worldTargets := make(map[*world.World]*bobberWorldTargets)
	s.lockTick(&s.bobbersMu)
	s.lockTick(&s.mobsMu)
	alive := s.bobbers[:0]
	for _, it := range s.bobbers {
		if it.hook.Dead {
			bobberTracker.unfile(it)
			continue
		}

		// A hook's step moves no body, so one list per world serves the tick.
		wt := worldTargets[it.w]
		if wt == nil {
			herd := s.dimensionOfWorld(it.w).mobs.herd
			wt = &bobberWorldTargets{index: make(map[int32]int, len(herd)+len(watchers[it.w]))}
			for _, m := range herd {
				wt.index[m.ID] = len(wt.targets)
				wt.targets = entity.MobProjectileTargets(wt.targets, m, mobEntityType(m.Type), boatRoots[m.ID])
			}
			for _, a := range watchers[it.w] {
				// A dead or spectating player is no body to hold: vanilla's
				// canInteractWithLevel, which unhooks the hook.
				target := a.c.projectileTarget(a.pos, !a.canInteract)
				target.RootVehicle = boatRoots[a.id]
				wt.index[a.id] = len(wt.targets)
				wt.targets = append(wt.targets, target)
			}
			wt.targets = append(wt.targets, extraTargets[it.w]...)
			worldTargets[it.w] = wt
		}
		targets := wt.targets
		var ownerBody *entity.ArrowTarget
		if i, ok := wt.index[it.hook.OwnerID]; ok {
			ownerBody = &targets[i]
		}

		// The angler still fishing, or the hook is out for good.
		// vanilla: FishingHook.shouldStopFishing
		var owner *entity.BobberOwner
		if a := byUUID[it.owner]; a != nil {
			dx := a.pos.X - it.hook.X
			dy := a.pos.Y - it.hook.Y
			dz := a.pos.Z - it.hook.Z
			if ownerBody == nil {
				ownerBody = &entity.ArrowTarget{}
			}
			owner = &entity.BobberOwner{
				Body:    *ownerBody,
				Fishing: a.canInteract && a.rod && a.w == it.w && dx*dx+dy*dy+dz*dz <= bobberOwnerRangeSquared,
			}
		}
		it.queue(&shots, it.w, it.hook.X, it.hook.Y, it.hook.Z, it.owner)
		nibbling := it.hook.Nibble > 0
		events := it.hook.Step(it.w, owner, targets)
		if it.hook.LandedFall > 0 {
			it.hook.LandedFall = 0
			bodyLandings = append(bodyLandings, bodyLanding{w: it.w, x: it.hook.X, y: it.hook.Y, z: it.hook.Z, halfWidth: entity.BobberHalfSize})
		}
		if nibbling && it.hook.Nibble <= 0 && !it.hook.Dead && listening {
			fishing = append(fishing, bobberFishing{it: it, state: plugin.FishFailed})
		}

		for _, ev := range events {
			switch ev.Kind {
			case entity.BobberBite:
				bite := bobberBiteSound{
					w: it.w,
					x: ev.X, y: ev.Y, z: ev.Z,
					pitch: float32(1.0 + (it.hook.Rand.Float64()-it.hook.Rand.Float64())*0.4),
				}
				if listening {
					fishing = append(fishing, bobberFishing{it: it, state: plugin.FishBite, sound: bite})
				} else {
					bites = append(bites, bite)
				}
			case entity.BobberHitBlock:
				landings = append(landings, blockProjectileLanding(it.w, ev.BlockHit, it.owner))
				if isTargetBlock(ev.BlockHit.State) {
					targetHits = append(targetHits, targetBlockHit{w: it.w, block: ev.BlockHit, ownerID: it.hook.OwnerID})
				}
				if isProjectileHitBlock(ev.BlockHit.State) {
					projectileHits = append(projectileHits, projectileBlockHit{w: it.w, block: ev.BlockHit,
						entityType: protocol.EntityTypeFishingBobber, ownerID: it.hook.OwnerID, ownerUUID: it.owner})
				}
			case entity.BobberHitTarget:
				landings = append(landings, projectileLanding{w: it.w, x: ev.X, y: ev.Y, z: ev.Z, ownerUUID: it.owner})
			case entity.BobberSplashed:
				splashes = append(splashes, projectileSplash{w: it.w, x: ev.X, y: ev.Y, z: ev.Z,
					volume: ev.Volume, pitch: ev.Pitch, owner: it.owner})
			case entity.BobberParticles:
				particles = append(particles, bobberParticle{
					w: it.w, particle: ev.Particle,
					x: ev.X, y: ev.Y, z: ev.Z,
					dx: ev.SpreadX, dy: ev.SpreadY, dz: ev.SpreadZ,
					speed: ev.Speed, count: ev.Count,
				})
			}
		}

		// A hook that died this tick has no movement tick. The velocity goes
		// out ahead of each watcher's move.
		body := bobberTracker.body(it)
		mt := body.schedule(nil)
		mt.gate = mt.gate && !it.hook.Dead
		var motion []byte
		if mt.gate && it.motionDue(it.hook.VX, it.hook.VY, it.hook.VZ, false) {
			motion = entityVelocityPayload(it.hook.ID, it.hook.VX, it.hook.VY, it.hook.VZ)
		}

		// A watcher shown the hook this tick is told nothing more of it.
		now := bobberTold{it.hook.HookedID, it.hook.Biting, it.custom}
		for _, i := range body.watchers(near[it.w]) {
			a := watchers[it.w][i]
			_, told := it.tracked.get(a.c)
			to := &out
			if told {
				to = &gone
			}
			was, seen := body.settle(a.c, a.pos.X, a.pos.Z, a.chunkRadius, to)
			if !seen || !told {
				continue
			}
			// The catch and the bite go out the tick they turned in.
			if was.own != now {
				metas.add(a.c, outbound{id: protocol.IDPlayEntityMetadata, data: bobberMetadataDeltaPayload(it, was.own)})
				was.own = now
			}
			if !mt.gate {
				continue
			}
			if motion != nil {
				out.moves.add(a.c, outbound{id: protocol.IDPlaySetEntityMotion, data: motion})
			}
			body.moveTold(was, a.c, mt, &out)
		}

		if it.hook.Dead {
			removal := outbound{id: protocol.IDPlayRemoveEntities, data: removeEntitiesPayload([]int32{it.hook.ID})}
			for _, i := range body.watchers(near[it.w]) {
				a := watchers[it.w][i]
				if _, told := it.tracked.get(a.c); told {
					gone.shows = append(gone.shows, removal)
					gone.showTos = append(gone.showTos, showTo{a.c, it.w})
				}
			}
			bobberTracker.unfile(it)
			continue
		}
		alive = append(alive, it)
	}
	s.bobbers = alive
	s.unlockTick(&s.mobsMu)
	s.unlockTick(&s.bobbersMu)
	s.hitTargetBlocks(targetHits)
	s.hitProjectileBlocks(projectileHits)
	applyEach(s, shots, (*Server).projectileShootEvent)
	applyEach(s, landings, (*Server).projectileLandEvent)
	s.runBodyLandings(bodyLandings)

	sendShows(out.shows, out.showTos)
	metas.send()
	out.moves.send()
	sendShows(gone.shows, gone.showTos)
	applyEach(s, splashes, (*Server).splashProjectile)
	if len(fishing) > 0 {
		owners := make(map[uuid.UUID]*client, len(byUUID))
		for id, a := range byUUID {
			owners[id] = a.c
		}
		bites = append(bites, s.pluginFishing(fishing, owners)...)
	}
	for _, bite := range bites {
		s.broadcastSoundID(bite.w,
			registry.SoundEntityFishingBobberSplash, protocol.SoundCategoryNeutral,
			bite.x, bite.y, bite.z, 0.25, bite.pitch)
	}
	for _, p := range particles {
		s.sendParticles(p.w, p.particle, p.x, p.y, p.z, p.dx, p.dy, p.dz, p.speed, p.count)
	}
}

// bobberFishing is a stage of fishing a hook's tick reached, for the
// PlayerFishEvent fired after the locks are released.
type bobberFishing struct {
	it    *bobberEntity
	state plugin.FishState
	sound bobberBiteSound // the splash a bite makes unless a listener cancels it
}

// pluginFishing fires PlayerFishEvent for the stages a tick reached and
// returns the bite sounds still to play. A cancelled bite is undone: the
// nibble closes and the next tick draws the bite again.
// vanilla: FishingHook.catchingFish
func (s *Server) pluginFishing(stages []bobberFishing, owners map[uuid.UUID]*client) (sounds []bobberBiteSound) {
	for _, st := range stages {
		allowed := true
		if c := owners[st.it.owner]; c != nil {
			allowed = c.pluginFish(st.state, st.it, nil, nil)
		}
		if st.state != plugin.FishBite {
			continue
		}
		if !allowed {
			s.lockTick(&s.bobbersMu)
			st.it.hook.Nibble, st.it.hook.Biting, st.it.hook.TimeUntilHooked = 0, false, 1
			s.unlockTick(&s.bobbersMu)
			continue
		}
		sounds = append(sounds, st.sound)
	}
	return sounds
}

// bobberParticle is one particle emission a hook's tick left, carried out
// of the locks for the send after them.
type bobberParticle struct {
	w                 *world.World
	particle          int32
	x, y, z           float64
	dx, dy, dz, speed float32
	count             int32
}

// bobberExtraTargets gathers the boxes of the bodies other than mobs,
// players and items a hook may take, each read under its kind's own lock.
// A marker stand has no box and is left out.
// vanilla: Entity.canBeHitByProjectile
func (s *Server) bobberExtraTargets() map[*world.World][]entity.ArrowTarget {
	targets := make(map[*world.World][]entity.ArrowTarget)
	add := func(w *world.World, id, typ int32, minX, minY, minZ, maxX, maxY, maxZ float64) {
		targets[w] = append(targets[w], entity.ArrowTarget{
			EntityID: id, Type: typ,
			MinX: minX, MinY: minY, MinZ: minZ,
			MaxX: maxX, MaxY: maxY, MaxZ: maxZ,
		})
	}
	s.tntMu.Lock()
	for _, primed := range s.tnts {
		if primed.tnt.Dead {
			continue
		}
		minX, minY, minZ, maxX, maxY, maxZ := primed.tnt.BlastBox()
		add(primed.w, primed.tnt.ID, protocol.EntityTypeTnt, minX, minY, minZ, maxX, maxY, maxZ)
	}
	s.tntMu.Unlock()
	s.boatsMu.Lock()
	for _, boat := range s.boats {
		if boat.boat.Dead {
			continue
		}
		minX, minY, minZ, maxX, maxY, maxZ := boat.boat.BlastBox()
		add(boat.w, boat.boat.ID, boat.entityTypeID(), minX, minY, minZ, maxX, maxY, maxZ)
		targets[boat.w][len(targets[boat.w])-1].RootVehicle = boat.boat.ID
	}
	s.boatsMu.Unlock()
	s.fallingMu.Lock()
	for _, fall := range s.fallings {
		if fall.fb.Dead {
			continue
		}
		minX, minY, minZ, maxX, maxY, maxZ := fall.fb.BlastBox()
		add(fall.w, fall.fb.ID, protocol.EntityTypeFallingBlock, minX, minY, minZ, maxX, maxY, maxZ)
	}
	s.fallingMu.Unlock()
	s.framesMu.Lock()
	for _, frame := range s.frames {
		add(frame.w, frame.id, frameEntityType(frame),
			frame.minX, frame.minY, frame.minZ, frame.maxX, frame.maxY, frame.maxZ)
	}
	s.framesMu.Unlock()
	s.paintingsMu.Lock()
	for _, painting := range s.paintings {
		add(painting.w, painting.id, protocol.EntityTypePainting,
			painting.minX, painting.minY, painting.minZ, painting.maxX, painting.maxY, painting.maxZ)
	}
	s.paintingsMu.Unlock()
	s.armorStandsMu.Lock()
	for _, stand := range s.armorStands {
		if stand.stand.Dead || stand.stand.Marker {
			continue
		}
		minX, minY, minZ, maxX, maxY, maxZ := stand.stand.BlastBox()
		add(stand.w, stand.stand.ID, protocol.EntityTypeArmorStand, minX, minY, minZ, maxX, maxY, maxZ)
	}
	s.armorStandsMu.Unlock()
	return targets
}

// isFishingRod reports whether the stack is the rod.
func isFishingRod(stack protocol.ItemStack) bool {
	return registry.Item(stack.ItemID) == registry.ItemFishingRod
}

// enchPieces is every equipment slot of the player, for effects that walk the
// killer's equipment.
// vanilla: EnchantmentHelper.runIterationOnEquipment
func (c *client) enchPieces() []rules.EnchPiece {
	var pieces []rules.EnchPiece
	c.player.Inventory(func(inv *player.Inventory) {
		for i, stack := range inv.Equipment() {
			pieces = append(pieces, rules.EnchPiece{Stack: stack, Slot: player.EquipmentSlots[i]})
		}
	})
	return pieces
}
