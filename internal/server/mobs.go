package server

import (
	"errors"
	"github.com/golem-mc/golem/internal/server/advancement"
	"iter"
	"log/slog"
	"maps"
	"math"
	"math/rand"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Server-side mob state: dimension, UUID and entity ID, per-client tracking
// and the chunk a mob saves with; the mob itself is entity.Mob. One lock
// covers the collection, sends happen outside it, and the herd holds only
// live mobs: a corpse stays twenty ticks in the dying list.

// corpseTicks is how long a dead mob's body stays, vanilla's twenty-tick
// death animation.
const corpseTicks = 20

// mobEntity is one mob in a world: entity.Mob plus its dimension, its
// persistent UUID and its tracking.
type mobEntity struct {
	// spawnReason is plugin.SpawnReason+1 as a caller names it, 0 for none.
	spawnReason uint8
	// equipSeen is the equipment the tracking pass last read; equipGen counts
	// its changes and is never 0 once the pass has read the mob. equipRead is
	// the Mob.EquipmentRevision last read. Guarded by the mob lock.
	equipSeen [7]protocol.ItemStack
	equipGen  uint32
	equipRead uint64
	// metaSeen is immutable; immediate broadcasts patch watcher baselines by copy.
	metaSeen *mobMetadataState
	metaRead uint64
	mob      *entity.Mob
	w        *world.World
	uuid     uuid.UUID
	// uuidText is the immutable scoreboard member name, cached on herd entry.
	uuidText string

	bodyTrack[mobShown]
	// locatorIcon is the icon its locator bar waypoint is drawn with. Guarded
	// by the mob lock.
	locatorIcon player.LocatorIcon
	// raider is the mob's place in a raid, set for the raider kinds once they
	// have been seen by the raid sweep. Guarded by the mob lock.
	raider *raiderRef
	// lastBolt is the bolt that last flipped a mooshroom, so one bolt flips it
	// once; unsaved. Guarded by the mob lock.
	// vanilla: MushroomCow.lastLightningBoltUUID
	lastBolt uuid.UUID
	// teleported marks a teleport not yet told: the next movement tick sends
	// one absolute sync, so many teleports in a tick cost one packet. Guarded
	// by the mob lock.
	teleported bool
	// loveCause is the player whose feeding caused love, credited with
	// bred_animals; zero for none. Guarded by the mob lock.
	// vanilla: Animal.loveCause
	loveCause uuid.UUID
	// equipSounds are the equip sounds of pieces a mount screen fitted, played
	// once the click's locks are released. Guarded by the mob lock.
	equipSounds []protocol.SoundHolder
	// resendOffers sends the open merchant window the mob's rewritten offers;
	// nil when nobody trades. Guarded by the mob lock.
	resendOffers func()

	// vehicle is the boat the mob sits in, nil when walking; the boat's tick
	// carries the body. Guarded by the mob lock.
	vehicle *boatEntity
	// cart is the minecart the mob sits in, nil for none; the cart's tick
	// carries the body. Guarded by the mob lock.
	cart *minecartEntity

	// rider is the player on the mob, nil for none. Guarded by the mob lock.
	rider *client
	// extra are the non-steering players seated after rider. Guarded by the
	// mob lock.
	extra []*client
	// dying marks a corpse still in the world, whose riders stay seated until
	// it is removed. Guarded by the mob lock.
	dying bool
	// converting keeps player seats until the deferred SINGLE seat transfer.
	// Guarded by the mob lock.
	converting bool

	// firstX, firstY, firstZ is a ridden mob's start-of-tick position, which
	// rider move reports are measured from. Guarded by the mob lock.
	firstX, firstY, firstZ float64

	// portal is the mob's portal state, shared with every other body of
	// vanilla's Entity, and the part that walks a herd across dimensions.
	portal rules.EntityPortal

	// pendingPassengers are the UUIDs a saved compound listed riding this
	// body, linked once the whole chunk's mobs are awake.
	pendingPassengers []uuid.UUID

	// carriedPassengers are nested rider compounds of unmodelled kinds,
	// written back under Passengers so they are not lost.
	carriedPassengers []nbt.Tag

	// saved holds the tags of the loaded compound this server does not
	// model (Brain, attributes, LoveCause, ...), written back unchanged.
	saved nbt.Compound
	// name is the CustomName tag as the file keeps it, nil for none.
	name        nbt.Tag
	nameVisible bool
	// tags are the scoreboard tags (tag selector, /tag). Guarded by the mob
	// lock.
	tags nbt.StringSet
	// convertTeam is the team a conversion's successor joins as it is added,
	// the old body's; "" for none.
	// vanilla: ConversionType.convertCommon
	convertTeam string

	// leash names the holder leashOf as the save does, while that is still
	// the mob's holder; with leashOf 0 and no holder it is a lead read from
	// a save still waiting for its holder, leashWait the ticks it has
	// waited. Change them through tieLeash and readLeash. Guarded by the
	// mob lock.
	leash     leashRef
	leashOf   int32
	leashWait int
}

// mobShown is a mob's own state as one watcher was last told it: the head
// angle as a byte, the velocity, which drives the client's splash and step
// particles, the metadata, and each equipment slot in wire-slot order.
type mobShown struct {
	// Keep the every-tick revision checks beside the position baseline.
	meta *mobMetadataState
	// wornGen is the mobEntity.equipGen that worn was last compared at.
	// 0 means the tracking pass has not compared worn: a spawn or updateMobSlot wrote it.
	wornGen    uint32
	head       uint8
	vx, vy, vz float64
	worn       [7]protocol.ItemStack
}

// mobShownOf is a mob's own state as a watcher is told it now.
func mobShownOf(ent *mobEntity) mobShown {
	m := ent.mob
	return mobShown{head: encodeAngle(m.HeadYaw), vx: m.VX, vy: m.VY, vz: m.VZ,
		meta: ent.metadataState(), worn: mobEquipmentNow(m)}
}

// mobStackRange is the largest tracking range in chunks among m and its
// indirect passengers.
func mobStackRange(m *entity.Mob) int {
	r := entity.MobTrackingRange(m.Type)
	for _, p := range m.Passengers {
		r = max(r, mobStackRange(p))
	}
	return r
}

// mobTracker tracks the mobs, kept in each dimension's herd. A mob's
// Teleport Entity carries no velocity: the motion packets tell it.
var mobTracker = trackedKind[*mobEntity, mobShown]{
	rangeOf: func(ent *mobEntity) int { return entity.MobTrackingRange(ent.mob.Type) },
	passengerRange: func(ent *mobEntity) int {
		r := 0
		if ent.rider != nil || len(ent.extra) > 0 {
			r = entity.PlayerTrackingRange
		}
		for _, p := range ent.mob.Passengers {
			r = max(r, mobStackRange(p))
		}
		return r
	},
	intervalOf: func(ent *mobEntity) int { return ent.mob.UpdateInterval() },
	lock:       func(s *Server) *sync.Mutex { return &s.mobsMu },
	all: func(s *Server) iter.Seq[*mobEntity] {
		return func(yield func(*mobEntity) bool) {
			for _, dim := range s.allDims() {
				for _, ent := range dim.mobs.book {
					if !yield(ent) {
						return
					}
				}
			}
		}
	},
	track: func(ent *mobEntity) *bodyTrack[mobShown] { return &ent.bodyTrack },
	world: func(ent *mobEntity) *world.World { return ent.w },
	id:    func(ent *mobEntity) int32 { return ent.mob.ID },
	// A dying mob is shown until its death's event takes it out of the book.
	dead: func(*mobEntity) bool { return false },
	pose: func(ent *mobEntity) bodyPose {
		m := ent.mob
		return bodyPose{x: m.X, y: m.Y, z: m.Z, yaw: m.Yaw, pitch: m.Pitch, onGround: m.OnGround}
	},
	readBody: func(ent *mobEntity) (*bodyTrack[mobShown], *world.World, int32, bodyPose) {
		m := ent.mob
		return &ent.bodyTrack, ent.w, m.ID, bodyPose{x: m.X, y: m.Y, z: m.Z, yaw: m.Yaw, pitch: m.Pitch, onGround: m.OnGround}
	},
	pairing: mobPairing,
	own:     mobShownOf,
}

// dyingMob is a dead mob's body, kept for its twenty-tick death animation.
type dyingMob struct {
	id      int32
	remains int
	// ent is the body itself, which a cube splits when it is removed.
	ent *mobEntity
}

// cubeSplit is one smaller cube a removed cube leaves, spawned outside the
// mob lock.
type cubeSplit struct {
	w     *world.World
	from  *mobEntity
	split entity.Split
}

// mobHerd is one dimension's mob state. woken marks chunks whose entities
// have been read in; carried holds entities this server cannot simulate as
// their read compounds, written back on every save so a vanilla world loses
// nothing.
type mobHerd struct {
	w    *world.World
	herd entity.Mobs
	// events is the previous tick's event buffer, reused to avoid a fresh slice per tick.
	events []entity.MobEvent
	book   map[*entity.Mob]*mobEntity
	// moveMarks is each mob's transform at the last EntityMoveEvent pass; nil while nothing listens.
	moveMarks map[*entity.Mob]markState
	moveGen   uint32
	// lootExtra builds each remembered mob's Mob.LootExtra; nil leaves it unset.
	lootExtra func(*mobHerd, *mobEntity) func(*rules.LootEntity)
	// records uses stable, one-based slots independent of herd compaction;
	// freeRecords holds vacant slots. Both are guarded by mobsMu.
	records     []*mobEntity
	freeRecords []int
	byUUID      map[uuid.UUID]*entity.Mob
	dying       []dyingMob
	woken       map[world.ChunkPos]struct{}
	carried     map[world.ChunkPos][]nbt.Tag
	// listed is the chunks whose entity file has to be written when they
	// leave even empty, or the next load would generate the animals again.
	listed map[world.ChunkPos]bool
	// wokenForWrite is chunks a write woke, whose items, arrows, orbs, TNT and
	// falling blocks are still tags until the tick's wake finds them loaded.
	wokenForWrite map[world.ChunkPos]bool
	// wakeSet is the world's ChunkSetVersion at the last wake pass that left
	// every loaded chunk woken; wakeSetOK is false until then.
	wakeSet   uint64
	wakeSetOK bool
	// writeTeams are the Team keys of the mobs a write woke, joined with the
	// next tick's wake once mobsMu is released.
	writeTeams []tagTeam
	// wardens counts the wardens in herd, readable without mobsMu so game
	// events skip the listener pass when there is none.
	wardens atomic.Int32
	// dragons counts the ender dragons in herd the same way, so a tick with
	// none skips the crystal and fight passes over the herd.
	dragons atomic.Int32
	// leashWaits is the mobs that joined carrying a saved lead, which the
	// mob tick re-ties to their holder or lets go after the grace.
	leashWaits []*mobEntity
	// version counts changes to herd's membership, guarded by mobsMu.
	version uint64
	// chunks files the live mobs (book) by feet chunk, guarded by mobsMu.
	chunks        entity.ChunkIndex[*entity.Mob]
	pistonGrid    bodyPistonGrid[*entity.Mob]
	pistonVersion uint64
}

// count keeps the per-kind counters in step with n more (or fewer) of m.
func (h *mobHerd) count(m *entity.Mob, n int32) {
	switch m.Type {
	case entity.TypeWarden:
		h.wardens.Add(n)
	case entity.TypeEnderDragon:
		h.dragons.Add(n)
	}
}

// record finds a filed body by slot. The caller holds mobsMu.
func (h *mobHerd) record(m *entity.Mob) *mobEntity {
	if slot := m.RecordSlot(); slot > 0 && slot <= len(h.records) {
		if ent := h.records[slot-1]; ent != nil && ent.mob == m {
			return ent
		}
	}
	return nil
}

// remember indexes a registered body by slot, pointer and UUID. The caller holds mobsMu.
func (h *mobHerd) remember(ent *mobEntity) {
	slot := ent.mob.RecordSlot()
	if h.record(ent.mob) == nil {
		if n := len(h.freeRecords); n > 0 {
			slot = h.freeRecords[n-1]
			h.freeRecords = h.freeRecords[:n-1]
		} else {
			slot = len(h.records) + 1
			h.records = append(h.records, nil)
		}
	}
	h.records[slot-1] = ent
	ent.mob.SetRecordSlot(slot)
	h.book[ent.mob] = ent
	h.chunks.Add(ent.mob, &ent.mob.Filing, entity.ChunkAt(ent.mob.X, ent.mob.Z))
	if h.lootExtra != nil {
		ent.mob.LootExtra = h.lootExtra(h, ent)
	}
	h.byUUID[ent.uuid] = ent.mob
	if ent.waitingLeash() {
		h.leashWaits = append(h.leashWaits, ent)
	}
}

// forget drops the indexes when a body leaves. The caller holds mobsMu.
func (h *mobHerd) forget(m *entity.Mob) {
	if ent := h.book[m]; ent != nil {
		slot := m.RecordSlot()
		h.records[slot-1] = nil
		h.freeRecords = append(h.freeRecords, slot)
		m.SetRecordSlot(0)
		delete(h.byUUID, ent.uuid)
		delete(h.book, m)
		m.Unfile()
	}
}

// add puts mobs in the herd. The caller holds mobsMu.
func (h *mobHerd) add(mobs ...*entity.Mob) {
	h.herd = append(h.herd, mobs...)
	h.version++
	for _, m := range mobs {
		h.count(m, 1)
	}
}

// remove takes a mob out of the herd. The caller holds mobsMu.
func (h *mobHerd) remove(mob *entity.Mob) {
	n := len(h.herd)
	h.herd = slices.DeleteFunc(h.herd, func(m *entity.Mob) bool { return m == mob })
	h.version++
	h.count(mob, int32(len(h.herd)-n))
}

// setHerd replaces the herd. The caller holds mobsMu.
func (h *mobHerd) setHerd(herd entity.Mobs) {
	h.herd = herd
	h.version++
	h.wardens.Store(0)
	h.dragons.Store(0)
	for _, m := range herd {
		h.count(m, 1)
	}
}

// newMobHerd builds the empty mob state of one dimension.
func newMobHerd(w *world.World) *mobHerd {
	return &mobHerd{
		w:       w,
		book:    make(map[*entity.Mob]*mobEntity),
		byUUID:  make(map[uuid.UUID]*entity.Mob),
		woken:   make(map[world.ChunkPos]struct{}),
		carried: make(map[world.ChunkPos][]nbt.Tag),
		listed:  make(map[world.ChunkPos]bool),

		wokenForWrite: make(map[world.ChunkPos]bool),
	}
}

// mobPortalTravel is a mob whose portal crossing came due, with the portal
// kind and the mob riders crossing with it, still seated.
type mobPortalTravel struct {
	ent    *mobEntity
	twin   *mobEntity
	kind   rules.PortalKind
	riders []mobPortalTravel
}

// twins raises the riders' continuations on vehicle, the mob's twin, each
// turned as far as the mob was, and appends them and theirs to stack.
// vanilla: Entity.teleportCrossDimension, calculatePassengerTransition
func (tr *mobPortalTravel) twins(s *Server, to *dimension, vehicle *mobEntity, stack []*mobEntity) []*mobEntity {
	tr.twin = vehicle
	turn := vehicle.mob.Yaw - tr.ent.mob.Yaw
	for i := range tr.riders {
		r := &tr.riders[i]
		twin := s.portalTwinOfMob(r.ent, to, player.Position{X: vehicle.mob.X, Y: vehicle.mob.Y, Z: vehicle.mob.Z}, turn)
		if twin == nil {
			continue
		}
		entity.Ride(vehicle.mob, twin.mob)
		stack = r.twins(s, to, twin, append(stack, twin))
	}
	return stack
}

// carryPlayers moves each player to their continuation's seat after the
// whole mob stack has been registered. No entity lock spans enterDimension.
// vanilla: Entity.teleportCrossDimension
func (tr mobPortalTravel) carryPlayers(s *Server, to *dimension) {
	if tr.twin == nil {
		return
	}
	s.mobsMu.Lock()
	players := slices.Clone(tr.ent.playersAboard())
	s.mobsMu.Unlock()
	for i, c := range players {
		if c.mount.Load() != tr.ent {
			continue
		}
		c.dismountMob(false)
		s.mobsMu.Lock()
		seat := horseSeat(tr.twin.mob, i)
		s.mobsMu.Unlock()
		seat.Pitch = c.player.Position().Pitch
		if err := c.enterDimension(to, command.Move{Pos: seat}, respawnNone, true); err != nil {
			c.log.Warn("mounted portal travel failed", "error", err)
			c.conn.Close()
			continue
		}
		s.seatPlayerOnMob(c, tr.twin, true)
		c.playPortalTravel()
	}
	for _, r := range tr.riders {
		r.carryPlayers(s, to)
	}
}

// putBack returns the mob and its riders to the herd. The caller holds
// mobsMu.
func (tr mobPortalTravel) putBack(h *mobHerd) {
	h.add(tr.ent.mob)
	tr.ent.uuidText = tr.ent.uuid.String()
	h.remember(tr.ent)
	for _, r := range tr.riders {
		r.putBack(h)
	}
}

// mobSend is a packet collected under the mob lock and written outside it:
// to one client, the whole world, or (a sound) those within earshot.
type mobSend struct {
	c    *client
	w    *world.World
	id   int32
	data []byte
	// near sends to players within radius blocks of (x, y, z); otherwise the
	// whole dimension hears. vanilla: PlayerList.broadcast
	near            bool
	x, y, z, radius float64
}

// mobEgg is an egg a chicken laid, spawned after the mob lock is released;
// its gift table has already rolled.
type mobEgg struct {
	w      *world.World
	x, y   float64
	z      float64
	stacks []protocol.ItemStack
	silent bool
	hen    uuid.UUID
	// pitch is the hen's own roll, 1.0 +/- 0.2, taken under the mob lock.
	pitch float32
}

// mobDrops is a dead mob's rolled drops, spawned after the mob lock is
// released.
type mobDrops struct {
	w      *world.World
	stacks []protocol.ItemStack
	// lasting are stacks dropped as items with a second lifetime.
	lasting []protocol.ItemStack
	// by is the living mob that dropped them by itself rather than dying
	// (the zero UUID for a death), whom EntityDropItemEvent names.
	by   uuid.UUID
	x, y float64
	z    float64
}

// mobXpDrop is a dead mob's rolled experience, cut into orbs after the mob
// lock is released.
type mobXpDrop struct {
	w       *world.World
	amount  int
	x, y, z float64
}

// mobStrike is a monster's blow on a player. The damage pipeline runs
// outside the mob lock, so the killer named in a death message is the one
// read under it.
type mobStrike struct {
	w             *world.World
	mob           *entity.Mob
	uuid          uuid.UUID
	victimID      int32
	damage        float64
	x, y, z       float64
	killer        *rules.Killer
	touch         bool
	puff          int // a puffed pufferfish's state: poison, no scaling or swing
	dragonContact bool
	contactPush   [3]float64
	pitch         float32
	// beam is a guardian beam's magic damage, dealt ahead of the blow; else 0.
	beam float64
	// hoglin is a hoglin or zoglin blow's tell and throw, nil otherwise.
	hoglin *entity.HoglinBlow
	// roar marks a ravager's roar, which swings nothing and adds no knockback
	// or after-effects to the hurt.
	roar  bool
	sonic *entity.SonicBoom
	// stab is a kinetic weapon's contact, nil for a swing.
	stab *entity.SpearStab
	// ram marks a ram or charge: the shove after the blow is ramPush along ramYaw.
	ram     bool
	ramYaw  float64
	ramPush float64
	// bunny marks a killer bunny's blow, whose attack sound plays at bunnyPitch
	// when it lands.
	bunny      bool
	bunnyPitch float32
	// ignites and igniteRoll, read under the mob lock, say whether the blow can
	// set the target alight and the draw deciding it.
	ignites    bool
	igniteRoll float32
	// owner is a tame wolf's owner, read under the mob lock, whose blow on a
	// mob credits them; uuid.Nil otherwise.
	owner uuid.UUID
}

// mobShot is an arrow or trident a monster loosed; shooter is who its first ticks
// cannot hit. It is spawned outside the mob lock.
type mobShot struct {
	w           *world.World
	shooter     int32
	shooterUUID uuid.UUID
	x, y, z     float64
	vx, vy, vz  float64
	damage      float64
	effect      entity.ArrowEffect
	weapon      protocol.ItemStack
	// fireTicks is the burn the arrow is shot with.
	fireTicks int32
	crossbow  bool
	trident   bool
}

// mobPotionThrow is a witch's splash potion, spawned outside the mob lock
// and owned by the witch.
type mobPotionThrow struct {
	w           *world.World
	thrower     int32
	throwerUUID uuid.UUID
	potion      string
	x, y, z     float64
	vx, vy, vz  float64
}

// hasEffect reports whether the player has the effect, for the mob
// snapshot.
func hasEffect(c *client, id int32) bool {
	_, ok := c.player.Effect(id)
	return ok
}

// The effects a witch's choice of potion reads off its target.
const (
	effectSlowness         = registry.MobEffectSlowness
	effectPoison           = registry.MobEffectPoison
	effectWeakness         = registry.MobEffectWeakness
	effectHeroOfTheVillage = registry.MobEffectHeroOfTheVillage
)

// invisibilityEffect is read for a player's invisibility in the mob
// snapshot.
const invisibilityEffect = registry.MobEffectInvisibility

// effectOozing and effectWeaving fire on a removed body's death, leaving
// slimes and cobwebs. vanilla: MobEffects.OOZING, MobEffects.WEAVING
const (
	effectOozing      = registry.MobEffectOozing
	effectWeaving     = registry.MobEffectWeaving
	effectWindCharged = registry.MobEffectWindCharged
)

var (
	// cobwebState is the web a dying body's weaving lays.
	cobwebState = registry.MustState("minecraft:cobweb", nil)
)

// mobEffectTrigger is a removed body whose oozing or weaving fires outside
// the mob lock, with dice, effects and place read under it. body is nil for
// a player's respawn; box is the hull already grown for the slime search.
// vanilla: OozingMobEffect.onMobRemoved, WeavingMobEffect.onMobRemoved
type mobEffectTrigger struct {
	w      *world.World
	body   *entity.Mob
	player bool
	box    entity.AABB
	x      float64
	y      float64
	z      float64
	dice   *rand.Rand
	// effects are the body's at removal; each one with a trigger fires.
	effects []effect.Instance
	// killed is a KILLED removal (a corpse's end, a creeper's or sulfur cube's
	// explosion, a player's respawn). height is the hull height.
	killed bool
	height float64
}

// sendMobSends writes packets a mob pass collected under its lock. A
// single-client packet is checked against the world, since a player who
// changed dimension meanwhile must not get the old world's mob packets.
func (s *Server) sendMobSends(sends []mobSend) {
	for _, send := range sends {
		if send.c == nil {
			if send.near {
				s.broadcastNear(send.w, send.x, send.y, send.z, send.radius, send.id, send.data)
				continue
			}
			s.broadcastWorld(send.w, send.id, send.data)
			continue
		}
		send.c.sendIn(send.w, send.id, send.data)
	}
}

// tickMobTracking updates mob visibility in a frozen world: mobs do not
// tick, but players still see mobs enter and leave range and teleports.
// vanilla: ChunkMap.tick
func (s *Server) tickMobTracking(watchers []trackingWatcher) {
	near := nearbyTracking(watchers)
	defer near.release()
	var sends []mobSend
	s.lockTick(&s.mobsMu)
	for _, dim := range s.allDims() {
		for _, m := range dim.mobs.herd {
			ent := dim.mobs.book[m]
			if ent == nil {
				continue
			}
			body := mobTracker.body(ent)
			motion := mobMovement{seated: ent.seated()}
			for _, i := range body.watchers(near) {
				wt := watchers[i]
				if wt.c.theWorld() != dim.w {
					continue // crossed to another dimension since the snapshot
				}
				_, told := ent.tracked.get(wt.c)
				if was, seen := mobSettle(&body, wt.c, wt.x, wt.z, wt.chunkRadius, &sends); seen && told && ent.teleported {
					motion.move(&body, was, wt.c, moveTick{teleported: true}, &sends)
				}
			}
			ent.teleported = false
		}
	}
	s.unlockTick(&s.mobsMu)
	s.sendMobSends(sends)
}

// mobConversion is a body a conversion replaces, with its new kind.
type mobConversion struct {
	w    *world.World
	old  *mobEntity
	into entity.MobType

	infectionSilent                    bool
	infectionX, infectionY, infectionZ int32
}

// reinforcementCall is one zombie's call for help, rolled in its tick.
type reinforcementCall struct {
	w      *world.World
	caller *entity.Mob
	target int32
	seed   int64
}

// spawningMonsters reports whether w's spawn_mobs and spawn_monsters both
// allow monsters. vanilla: ServerLevel.isSpawningMonsters
func spawningMonsters(w *world.World) bool {
	return w.BoolRule(world.RuleSpawnMobs) && w.BoolRule(world.RuleSpawnMonsters)
}

// finishReinforcement places the called zombie hunting the caller's
// target; only a call that found a place costs the caller its 0.05.
// vanilla: Zombie.hurtServer
func (s *Server) finishReinforcement(call reinforcementCall) {
	if !spawningMonsters(call.w) {
		return
	}
	dim := s.dimensionOfWorld(call.w)
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	players := spawnPlayersOf(clients, call.w)
	bodies := s.spawnBodies(dim, players)
	s.mobsMu.Lock()
	kind, x, y, z := call.caller.Type, rules.BlockOf(call.caller.X), rules.BlockOf(call.caller.Y), rules.BlockOf(call.caller.Z)
	s.mobsMu.Unlock()
	sx, sy, sz, ok := entity.ReinforcementSite(call.w, rand.New(rand.NewSource(call.seed)), kind, x, y, z, players, bodies)
	if !ok {
		return
	}
	s.spawnMobArriving(dim, kind, float64(sx), float64(sy), float64(sz), 0, nil, false, "", true, func(ent *mobEntity) {
		ent.mob.AsReinforcement(call.target)
	})
	s.mobsMu.Lock()
	call.caller.ChargeReinforcement()
	s.mobsMu.Unlock()
}

// mobBirth is a breeding come due: the mother (the child spawns at her
// place, vanilla's spawnChildFromBreeding snapTo), the other parent and the
// mating's experience. The goal already set cooldowns and ended love.
type mobBirth struct {
	w       *world.World
	mother  *entity.Mob
	partner *entity.Mob
	x, y, z float64
	amount  int
	// cause is the player credited with bred_animals, zero for none.
	cause uuid.UUID
	// motherID and partnerID are the parents' identities, for EntityBreedEvent.
	motherID, partnerID uuid.UUID
	// parentCauses are the parents' own love causes, whom a fox kit trusts.
	// vanilla: Fox.FoxBreedGoal.breed
	parentCauses [2]uuid.UUID
}

// finishBreeding spawns a mating's results in finalizeSpawnChildFromBreeding
// order: the mother's hearts, one experience orb (if mob_drops allows), then
// the baby inheriting what getBreedOffspring takes. A villager's hearts are
// its child's.
func (s *Server) finishBreeding(birth mobBirth) {
	if !s.askBreed(&birth) {
		return
	}
	c := s.clientByUUID(birth.cause)
	if birth.cause == (uuid.UUID{}) {
		c = nil
	}
	// Frog, turtle and sniffer breeding credit no child entity.
	childless := birth.mother.Type == entity.TypeTurtle || birth.mother.Type == entity.TypeFrog || birth.mother.Type == entity.TypeSniffer
	if c != nil && childless {
		c.bredAnimalsAdvancements(birth.mother.Type.Name(), birth.partner.Type.Name(), "", s.breedingParents(birth))
	}
	if c != nil {
		c.awardStat(player.Custom(registry.CustomStatAnimalsBred), 1)
	}
	villager := birth.mother.Type == entity.TypeVillager
	if birth.mother.Type != entity.TypeTurtle && !villager {
		s.broadcastWorld(birth.w, protocol.IDPlayEntityEvent,
			entityEventPayload(birth.mother.ID, protocol.EntityStatusLoveHearts))
	}
	if birth.amount > 0 && birth.w.BoolRule(world.RuleMobDrops) {
		// The orb is added whole (vanilla's addFreshEntity, not a death's award),
		// so it does not merge, with the constructor's fresh-orb pop.
		vx, vy, vz := s.orbPopVector()
		s.spawnOrb(birth.w, birth.x, birth.y, birth.z, vx, vy, vz, birth.amount)
	}
	if birth.mother.Type == entity.TypeTurtle || birth.mother.Type == entity.TypeFrog {
		// vanilla: Turtle.TurtleBreedGoal.breed, Frog.spawnChildFromBreeding
		return
	}
	if birth.mother.Type == entity.TypeSniffer {
		s.laySnifferEgg(birth)
		return
	}
	biome := birth.w.Biome(rules.BlockOf(birth.x), rules.BlockOf(birth.y), rules.BlockOf(birth.z))
	born := s.standMobArriving(s.dimensionOfWorld(birth.w), birth.mother.OffspringType(birth.partner),
		birth.x, birth.y, birth.z, 0, nil, true, "", false, func(ent *mobEntity) {
			ent.spawnReason = spawnReasonCode(plugin.SpawnBreeding)
			// Read the parents under the mob lock: their dice share the goal's stream.
			s.mobsMu.Lock()
			birth.mother.BreedInto(ent.mob, birth.partner)
			if birth.mother.Type == entity.TypeRabbit {
				birth.mother.BreedRabbit(ent.mob, birth.partner, biome)
			}
			if birth.mother.Type == entity.TypeCat {
				birth.mother.BreedCat(ent.mob, birth.partner)
			}
			if villager {
				birth.mother.BreedVillager(ent.mob, birth.partner, biome)
			}
			if birth.mother.Type == entity.TypeFox {
				ent.mob.FoxTrust(birth.parentCauses[0])
				if birth.parentCauses[1] != birth.parentCauses[0] {
					ent.mob.FoxTrust(birth.parentCauses[1])
				}
			}
			s.mobsMu.Unlock()
		})
	child := born.mob.ID
	if c != nil {
		ents := s.breedingParents(birth)
		ents["child"] = s.mobLoot(born)
		c.bredAnimalsAdvancements(birth.mother.Type.Name(), birth.partner.Type.Name(), born.mob.Type.Name(), ents)
	}
	if villager {
		// vanilla: VillagerMakeLove.breed sends the hearts on the child
		s.broadcastWorld(birth.w, protocol.IDPlayEntityEvent, entityEventPayload(child, protocol.EntityStatusVillagerHearts))
	}
}

// breedingParents are the loot entities of a mating's parents.
func (s *Server) breedingParents(birth mobBirth) map[string]func() rules.LootEntity {
	return map[string]func() rules.LootEntity{
		"parent":  s.mobLootOf(birth.w, birth.mother),
		"partner": s.mobLootOf(birth.w, birth.partner),
	}
}

// finishConversion spawns the converted body in the old one's place,
// unfinalized (vanilla: Mob.convertTo), and plays the world event (1040
// zombie drowned, 1041 husk, 1048 skeleton frozen).
func (s *Server) finishConversion(cv mobConversion) {
	old := cv.old.mob
	id := s.nextEntity()
	m := entity.NewMob(cv.into, id, old.X, old.Y, old.Z, s.entitySeed(id))
	m.ConvertedFrom(old)
	if old.Type == entity.TypeZombieVillager && cv.into == entity.TypeVillager {
		s.zombieVillagerCured(cv, m)
		return
	}
	if old.Type == entity.TypeVillager && cv.into == entity.TypeZombieVillager {
		s.villagerInfected(cv, m)
		return
	}
	if old.Type == entity.TypeHoglin {
		s.hoglinConverted(cv, m)
		return
	}
	if entity.Family(old.Type) == entity.TypeZombie {
		pos := world.ChunkPos{X: world.ChunkOf(old.X), Z: world.ChunkOf(old.Z)}
		cv.w.Acquire(pos)
		m.RollConversionAttributes(rules.SpecialMultiplier(s.effectiveDifficultyAt(cv.w, old.X, old.Z)))
		cv.w.Release(pos)
	}
	if old.Type == entity.TypePiglin || old.Type == entity.TypePiglinBrute {
		// vanilla: AbstractPiglin.finishConversion
		m.AddEffect(effect.Instance{ID: registry.MobEffectNausea, Duration: 200})
	}
	if old.Type == entity.TypeTadpole {
		ent := convertedEntity(cv.old, m, cv.w, false)
		m.FinishTadpoleGrowth(cv.w, old, cv.w.Biome(rules.BlockOf(old.X), rules.BlockOf(old.Y), rules.BlockOf(old.Z)))
		s.addRiddenConversion(cv.old, ent)
		if !old.Silent {
			s.broadcastSoundID(cv.w, registry.SoundEntityTadpoleGrowUp, protocol.SoundCategoryNeutral, old.X, old.Y, old.Z, 0.15, 1)
		}
		return
	}
	s.addRiddenConversion(cv.old, convertedEntity(cv.old, m, cv.w, true))
	event := int32(protocol.WorldEventZombieDrowned)
	switch old.Type {
	case entity.TypePiglin, entity.TypePiglinBrute:
		// vanilla: Piglin.playConvertedSound, PiglinBrute.playConvertedSound,
		// AbstractPiglin.customServerAiStep
		if !old.Silent && cv.w.Difficulty() != rules.DifficultyPeaceful {
			sound := registry.SoundEntityPiglinConvertedToZombified
			if old.Type == entity.TypePiglinBrute {
				sound = registry.SoundEntityPiglinBruteConvertedToZombified
			}
			s.broadcastSoundID(cv.w, sound,
				mobSoundCategory(old.Type), old.X, old.Y, old.Z, old.SoundVolume(), old.VoicePitch())
		}
		return
	case entity.TypeHusk:
		event = protocol.WorldEventHuskDrowned
	case entity.TypeSkeleton:
		// vanilla: Skeleton.doFreezeConversion plays it unless silent
		if old.Silent {
			return
		}
		event = protocol.WorldEventSkeletonToStray
	}
	s.broadcastLevelEvent(cv.w, event,
		int32(math.Floor(old.X)), int32(math.Floor(old.Y)), int32(math.Floor(old.Z)), 0)

}

// spawnSplitCube spawns one cube of a split at full health, unfinalized,
// the convertTo of a TRIGGERED spawn.
func (s *Server) spawnSplitCube(sp cubeSplit) {
	id := s.nextEntity()
	m := entity.NewMob(sp.from.mob.Type, id, sp.split.X, sp.split.Y, sp.split.Z, s.entitySeed(id))
	m.SetSize(sp.split.Size, true)
	m.Yaw, m.HeadYaw = sp.split.Yaw, sp.split.Yaw
	s.addMob(convertedEntity(sp.from, m, sp.w, false))
}

// fireMobEffectTrigger spawns a removed body's oozing slimes (capped by the
// cramming rule against slimes already near) and weaving cobwebs. A
// player's respawn has no body: the web gate passes on the player alone and
// the respawn's own reset clears the effects.
// vanilla: OozingMobEffect.onMobRemoved, WeavingMobEffect.onMobRemoved
func (s *Server) fireMobEffectTrigger(tr mobEffectTrigger) {
	for _, in := range tr.effects {
		switch in.ID {
		case effectOozing:
			requested := rules.OozingSlimeCount()
			count := requested
			// The near-slime search runs only for a rule of 1 or more; below that the
			// count is uncapped. vanilla: OozingMobEffect.numberOfSlimesToSpawn
			if maxCramming := int(tr.w.IntRule(world.RuleMaxCramming)); maxCramming >= 1 {
				count = rules.OozingSlimesToSpawn(maxCramming, s.nearbySlimeCount(tr.w, tr.body, tr.box, maxCramming), requested)
			}
			for i := 0; i < count; i++ {
				s.spawnMobArriving(s.dimensionOfWorld(tr.w), entity.TypeSlime,
					tr.x, tr.y+0.5, tr.z, 0, nil, false, "", false, func(ent *mobEntity) {
						// vanilla: OozingMobEffect.spawnSlimeOffspring —
						// the size and the one yaw the level's dice pick
						ent.mob.SetSize(2, true)
						yaw := s.envRng.Float32() * 360
						ent.mob.Yaw, ent.mob.HeadYaw = yaw, yaw
					})
			}
		case effectWindCharged:
			if !tr.killed {
				continue
			}
			// vanilla: WindChargedMobEffect.onMobRemoved
			radius := 3 + float64(tr.dice.Float32())*2
			var causeID int32
			var silent bool
			if tr.body != nil {
				causeID, silent = tr.body.ID, tr.body.Silent
			}
			s.runExplosion(blast{
				w: tr.w, x: tr.x, y: tr.y + tr.height/2, z: tr.z,
				radius: radius, causeID: causeID, directID: causeID,
				interaction: rules.ExplosionTrigger,
				damageType:  rules.DamageTypePlayerExplosion,
				rng:         tr.dice,
				harmless:    true,
				silent:      silent,
				gust:        true,
				blockResist: windChargeResistanceOf(tr.w),
			})
		case effectWeaving:
			// vanilla: WeavingMobEffect.onMobRemoved; a player's own webs ignore
			// mob_griefing.
			if tr.player || tr.w.BoolRule(world.RuleMobGriefing) {
				s.weaveCobwebs(tr)
			}
		}
	}
	if tr.body != nil {
		tr.body.ClearEffects()
	}
}

// nearbySlimeCount counts slimes whose hull crosses box, excluding body,
// stopping at max. vanilla: OozingMobEffect.NearbySlimes.closeTo
func (s *Server) nearbySlimeCount(w *world.World, body *entity.Mob, box entity.AABB, max int) int {
	if max < 1 {
		return 0
	}
	h := s.herdOf(w)
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	count := 0
	for _, mob := range h.herd {
		if mob == body || mob.Type != entity.TypeSlime {
			continue
		}
		minX, minY, minZ, maxX, maxY, maxZ := mob.BlastBox()
		if !entity.NewAABB(minX, minY, minZ, maxX, maxY, maxZ).Crosses(box) {
			continue
		}
		count++
		if count == max {
			break
		}
	}
	return count
}

// weaveCobwebs lays the weaving's webs in random cells of the cube around
// the body, up to the count or fifteen draws, only where a web can replace
// the block over a sturdy top face, each with its own sound.
// vanilla: WeavingMobEffect.spawnCobwebsRandomlyAround
func (s *Server) weaveCobwebs(tr mobEffectTrigger) {
	count := rules.WeavingMaxCobwebs(tr.dice)
	bx, by, bz := int32(math.Floor(tr.x)), int32(math.Floor(tr.y)), int32(math.Floor(tr.z))
	picked := make(map[[3]int32]struct{}, count)
	for draw := 0; draw < 15 && len(picked) < count; draw++ {
		pos := [3]int32{bx + int32(tr.dice.Intn(3)) - 1, by + int32(tr.dice.Intn(3)) - 1, bz + int32(tr.dice.Intn(3)) - 1}
		if _, ok := picked[pos]; ok {
			continue
		}
		if !registry.BlockOf(tr.w.BlockAt(pos[0], pos[1], pos[2])).IsReplaceable() {
			continue
		}
		// Face 1 is up: the web needs a sturdy top face below.
		if !registry.FaceSturdy(tr.w.BlockAt(pos[0], pos[1]-1, pos[2]), 1, registry.SupportFull) {
			continue
		}
		picked[pos] = struct{}{}
	}
	for pos := range picked {
		s.setBlockAndTell(tr.w, pos[0], pos[1], pos[2], cobwebState)
		s.broadcastLevelEvent(tr.w, protocol.WorldEventCobwebWeave, pos[0], pos[1], pos[2], 0)
	}
}

// convertedMobTags are saved keys a full conversion carries and a split
// does not. Door breaking is carried ungated in vanilla; no splitting kind
// breaks doors.
// vanilla: ConversionType.convertCommon
// vanilla: ConversionParams.single(this, true, true) sets the keep-flags
var convertedMobTags = []string{"CanBreakDoors"}

// convertedEntity wraps a converted body with what convertCommon carries
// over (name, portal cooldown, invulnerability, unmodelled tags, NoAI,
// persistence) under a new UUID. full (a drowned, a husk's zombie) also
// keeps loot pickup and convertedMobTags; a split's children keep neither.
func convertedEntity(from *mobEntity, m *entity.Mob, w *world.World, full bool) *mobEntity {
	m.SetMetadataNoAI(from.mob.NoAI)
	m.Persistence = from.mob.Persistence
	m.SetMetadataNoGravity(from.mob.NoGravity)
	m.Silent = from.mob.Silent
	m.SetMetadataLeftHanded(from.mob.LeftHanded)
	m.Invulnerable = from.mob.Invulnerable
	if full {
		m.CanPickUpLoot = from.mob.CanPickUpLoot
	}
	// vanilla: ConversionType.convertCommon's active_effects, from the old
	// body's live set.
	m.InheritEffects(from.mob)
	ent := &mobEntity{
		mob: m, w: w, uuid: uuid.New(),
		name: from.name, nameVisible: from.nameVisible,
		portal:            rules.EntityPortal{Cooldown: from.portal.Cooldown},
		tags:              nbt.NewStringSet(from.tags.Values()),
		convertTeam:       from.mob.Team,
		carriedPassengers: slices.Clone(from.carriedPassengers),
		// The lead's name goes with a holder the caller copies over.
		leash: from.leash, leashOf: from.leashOf,
	}
	carry := func(key string) {
		if tag, ok := from.saved[key]; ok {
			if ent.saved == nil {
				ent.saved = nbt.Compound{}
			}
			ent.saved[key] = tag
		}
	}
	if full {
		for _, key := range convertedMobTags {
			carry(key)
		}
	}
	return ent
}

// mobTrample is one mob's landing in a tick: where, its size and the fall
// distance.
type mobTrample struct {
	w       *world.World
	x, y, z float64
	width   float64
	height  float64
	fall    float64
	kind    entity.MobType
	uuid    uuid.UUID // the mob, the source of the farmland's block_change
}

// mobTrampledFarmland is the mob half of FarmlandBlock.fallOn: only a body
// mob_griefing allows and heavy enough by its size can trample; the engine
// rolls against the fall.
func (s *Server) mobTrampledFarmland(t mobTrample) {
	if !t.w.BoolRule(world.RuleMobGriefing) {
		return
	}
	if t.width*t.width*t.height <= 0.512 {
		return
	}
	x := int32(math.Floor(t.x))
	z := int32(math.Floor(t.z))
	ground := int32(math.Floor(t.y - 0.2))
	if !s.mobMayInteract(t.w, t.uuid, x, ground, z, t.fall) {
		return
	}
	changes := s.physicsFor(t.w).FarmlandFallenOn(x, ground, z, t.fall)
	s.tellBlockChanges(t.w, changes)
	if len(changes) > 0 {
		s.gameEvent(t.w, rules.GameEventBlockChange, float64(x)+0.5, float64(ground)+0.5, float64(z)+0.5,
			rules.GameEventContext{Mob: t.uuid, Affected: t.w.BlockAt(x, ground, z)})
	}
}

// mobTrampled offers a landing or step to the farmland and turtle egg
// underfoot.
func (s *Server) mobTrampled(t mobTrample) {
	s.mobTrampledFarmland(t)
	s.mobTrampledTurtleEgg(t)
}

// mobGraze is the cell a sheep bit: underfoot, or its feet cell for an
// edible_for_sheep plant.
type mobGraze struct {
	w *world.World
	// mob is the body that caused it, nil when none did.
	mob     *entity.Mob
	x, y, z int32
}

// mobSculkStep is a mob standing on the sculk sensor at x, y, z.
type mobSculkStep struct {
	w       *world.World
	id      uuid.UUID
	x, y, z int32
	// at is the mob's position.
	at [3]float64
}

// mobSteppedOnSculk schedules a mob's step on a sculk sensor.
func (s *Server) mobSteppedOnSculk(st mobSculkStep) {
	s.stepOnSculkSensor(st.w, st.x, st.y, st.z, st.at[0], st.at[1], st.at[2], uuid.Nil, st.id)
}

// mobBlockMove is an enderman's pick-up (took) or set-down of a block.
type mobBlockMove struct {
	mob     *entity.Mob
	w       *world.World
	x, y, z int32
	state   uint16
	took    bool
}

// mobMovedBlock applies an enderman's block goal to the world, with the
// block game event. If another enderman took the cell first this tick, the
// carried state is undone so a block is neither duplicated nor lost.
// vanilla: EnderMan.EndermanTakeBlockGoal.tick, EndermanLeaveBlockGoal.tick
func (s *Server) mobMovedBlock(move mobBlockMove) {
	event, state := rules.GameEventBlockPlace, move.state
	if move.took {
		if move.w.BlockAt(move.x, move.y, move.z) != move.state {
			s.setMobCarried(move.mob, 0)
			return
		}
		event, state = rules.GameEventBlockDestroy, 0
	} else if !move.w.IsAirAt(move.x, move.y, move.z) {
		s.setMobCarried(move.mob, move.state)
		return
	}
	if !s.mobMayChange(move.w, move.mob, move.x, move.y, move.z, state) {
		// A refused take leaves the block, a refused set-down keeps the carry.
		if move.took {
			s.setMobCarried(move.mob, 0)
		} else {
			s.setMobCarried(move.mob, move.state)
		}
		return
	}
	s.setBlockAndTell(move.w, move.x, move.y, move.z, state)
	s.gameEvent(move.w, event, float64(move.x)+0.5, float64(move.y)+0.5, float64(move.z)+0.5,
		rules.GameEventContext{Affected: move.state})
}

// setMobCarried sets the block an enderman carries outside the tick's locks.
func (s *Server) setMobCarried(m *entity.Mob, state uint16) {
	s.mobsMu.Lock()
	m.SetCarriedBlock(state)
	s.mobsMu.Unlock()
}

// mobGrazedGround is the world half of EatBlockGoal.tick. mob_griefing
// gates only the block (the fleece already regrew); world event 2001 names
// the eaten state. A plant goes through destroyBlock with flags 3 so a
// neighbouring fluid flows in; grass goes to dirt with flags 2.
func (s *Server) mobGrazedGround(g mobGraze) {
	if !g.w.BoolRule(world.RuleMobGriefing) {
		return
	}
	state := g.w.BlockAt(g.x, g.y, g.z)
	block := registry.BlockOf(state)
	plant := block.InTag(registry.TagIDEdibleForSheep)
	if !plant && block != registry.BlockGrassBlock {
		// The cell changed since the grazing: nothing to eat.
		return
	}
	to := registry.BlockDirt.DefaultState()
	if plant {
		to = 0
	}
	if !s.mobMayChange(g.w, g.mob, g.x, g.y, g.z, to) {
		return
	}
	if plant {
		if !g.w.SetBlock(g.x, g.y, g.z, 0) {
			return
		}
		s.broadcastLevelEvent(g.w,
			protocol.WorldEventBlockBreak, g.x, g.y, g.z, int32(state))

		s.tellBlockChanges(g.w, []world.BlockChange{{X: g.x, Y: g.y, Z: g.z}})
		s.blockChanged(g.w, g.x, g.y, g.z, state)
		return
	}
	dirt := registry.BlockDirt.DefaultState()
	if !s.writeBlock(g.w, g.x, g.y, g.z, dirt, nil, writeNeighborShapes) {
		return
	}
	s.broadcastLevelEvent(g.w,
		protocol.WorldEventBlockBreak, g.x, g.y, g.z, int32(state))
}

// mobRaidedCrop is the world half of a rabbit's raid: an age-0 carrot
// becomes air with no drops or event, an older one loses a stage with the
// break event. mob_griefing is reread as it can change since the goal.
// vanilla: Rabbit.RaidGardenGoal.tick
func (s *Server) mobRaidedCrop(g mobGraze) {
	if !g.w.BoolRule(world.RuleMobGriefing) {
		return
	}
	state := g.w.BlockAt(g.x, g.y, g.z)
	if registry.BlockOf(state) != registry.BlockCarrots {
		return
	}
	age, ok := registry.StateInt(state, registry.PropAge)
	if !ok {
		return
	}
	if age == 0 {
		// The block is already air when vanilla's destroyBlock runs, so no drops
		// or break event.
		s.writeBlock(g.w, g.x, g.y, g.z, airBlockState, nil, writeNeighborShapes)
		return
	}
	next, found := registry.WithInt(state, registry.PropAge, age-1)
	if !found || !g.w.SetBlock(g.x, g.y, g.z, next) {
		return
	}
	s.broadcastLevelEvent(g.w, protocol.WorldEventBlockBreak, g.x, g.y, g.z, int32(state))
	s.tellBlockChanges(g.w, []world.BlockChange{{X: g.x, Y: g.y, Z: g.z, State: next}})
	s.blockChanged(g.w, g.x, g.y, g.z, state)
}

// cauldronDoused lowers a layered cauldron a level for a burning body and
// posts the block_change. vanilla: LayeredCauldronBlock.lowerFillLevel
func (s *Server) cauldronDoused(w *world.World, x, y, z int32) {
	changes := s.physicsFor(w).CauldronDoused(x, y, z)
	s.tellBlockChanges(w, changes)
	if len(changes) > 0 {
		s.gameEvent(w, rules.GameEventBlockChange, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, rules.GameEventContext{Affected: w.BlockAt(x, y, z)})
	}
}

// mobDousedCauldron lowers the cauldron a burning mob doused itself in.
func (s *Server) mobDousedCauldron(g mobGraze) {
	s.cauldronDoused(g.w, g.x, g.y, g.z)
}

// mobSwingArm builds the Entity Animation for a mob's main-hand swing.
func mobSwingArm(entityID int32) []byte {
	return swingAnimationPayload(entityID, protocol.HandMain)
}

// teamGated reports whether the blow is a cube touch, a pufferfish sting or a
// dragon contact, which vanilla checks Entity.doTeamsAllowDamage for.
func (m mobStrike) teamGated() bool {
	return m.touch || m.puff > 0 || m.dragonContact
}

// landingBlow runs a monster's blow through the player damage pipeline,
// difficulty-scaled (vanilla: Player.hurt), with the monster named in the
// death message.
func (s *Server) landingBlow(strike mobStrike, clients []*client) {
	var victim *client
	for _, c := range clients {
		if c.player.EntityID == strike.victimID {
			victim = c
			break
		}
	}
	if strike.dragonContact && victim != nil {
		if strike.contactPush != [3]float64{} {
			// Server-side only; a landed hurt syncs it. vanilla: Entity.push
			victim.player.AddVelocity(strike.contactPush[0], strike.contactPush[1], strike.contactPush[2])
		}
		if strike.damage == 0 {
			return
		}
	}
	// vanilla: Entity.doTeamsAllowDamage
	if victim != nil && strike.teamGated() && !s.scoreboard.CanHarm(victim.player.Name, strike.uuid.String()) {
		return
	}
	if strike.sonic != nil {
		s.sonicBoom(strike, victim)
		return
	}
	if strike.stab != nil {
		s.landingStab(strike, victim)
		return
	}
	// The swing plays whether or not the blow lands; a cube's touch, a beam
	// and a ram swing nothing.
	if !strike.touch && strike.beam == 0 && !strike.ram && !strike.roar && strike.mob.Type != entity.TypeIronGolem && strike.mob.Type != entity.TypeFrog {
		s.broadcastWorld(strike.w, protocol.IDPlaySwingAnimation,
			mobSwingArm(strike.mob.ID))
	}
	if strike.hoglin != nil {
		s.hoglinCharge(strike)
	}
	if victim == nil {
		// No such player: the blow may be on another mob. The swing already went
		// out.
		s.landingMobBlow(strike)
		return
	}
	if strike.puff > 0 {
		s.pufferStingPlayer(strike, victim)
		return
	}
	if strike.beam > 0 {
		s.guardianBeam(strike, victim)
	}
	dealt, extra := strike.mob.BlowEnchantments(protocol.EntityTypePlayer, strike.damage, func(ctx *rules.EnchContext) {
		s.bindBlowLoot(strike.w, ctx, attackBody{mob: strike.mob}, attackBody{player: victim}, false)
	})
	if strike.roar {
		extra = 0
	}
	scaled := rules.MobAttackAfterDifficulty(dealt, strike.w.Difficulty())
	if scaled <= 0 {
		return
	}
	damageType := rules.DamageTypeMobAttack
	if strike.ram && !entity.IsNautilus(strike.mob.Type) {
		damageType = rules.DamageTypeMobAttackNoAggro
	}
	if strike.mob.Type == entity.TypeBee {
		damageType = rules.DamageTypeSting
	}
	cause := &damageCause{
		entityID: strike.mob.ID,
		x:        strike.x, y: strike.y, z: strike.z,
		killer: strike.killer, weapon: strike.mob.MainHand,
		shove: shovesOnBlock(strike.mob.Type),
	}
	cause.disableSeconds = livingDisableSeconds(strike.mob.Type, strike.mob.MainHand)
	landedHit, softened := victim.hurt(scaled, damageType, cause)
	if cause.blocked {
		// vanilla: Hoglin.blockedByItem, Zoglin.blockedByItem
		s.hoglinThrowPlayer(strike, victim)
		s.ravagerShielded(strike, victim)
	}
	if strike.ram {
		// The ram's shove applies whether or not the hurt landed; a blocking
		// shield halves it. vanilla: RamTarget.tick
		push := strike.ramPush
		if !cause.shieldAsked {
			if blocked, _ := victim.shieldBlocks(rules.DamageTypeMobAttack, cause, scaled); blocked > 0 {
				cause.blocked = true
			}
		}
		if cause.blocked && !entity.IsNautilus(strike.mob.Type) {
			push *= 0.5
		}
		if landedHit && !softened {
			victim.knockbackFromPointExtraBy(strike.w, strike.uuid, strike.x, strike.y, strike.z, strike.ramYaw, push, syncVelocityAfterTravel)
		} else {
			victim.knockbackFromPointExtraOnlyBy(strike.w, strike.uuid, strike.ramYaw, push, syncVelocityAfterTravel)
		}
	}
	if landedHit {
		if strike.dragonContact {
			victim.velocityDirty = true
		}
		// A hit that landed only through the invulnerability window skips default
		// knockback but keeps the weapon's extra.
		if !strike.ram {
			switch {
			case !softened:
				victim.knockbackFromPointExtraBy(strike.w, strike.uuid, strike.x, strike.y, strike.z, float64(strike.mob.Yaw), extra, syncVelocityAfterTravel)
			case extra > 0:
				victim.knockbackFromPointExtraOnlyBy(strike.w, strike.uuid, float64(strike.mob.Yaw), extra, syncVelocityAfterTravel)
			}
		}
		if strike.roar {
			return
		}
		s.killerBunnyBite(strike)
		s.beeStung(strike, victim)
		s.hoglinThrowPlayer(strike, victim)
		if strike.mob.Type == entity.TypeIronGolem {
			var resistance float64
			victim.player.Attributes(func(a *player.Attributes) {
				resistance = a.Value(registry.AttributeKnockbackResistance)
			})
			victim.addVelocity(strike.w, 0, rules.IronGolemLift(resistance), 0)
		}
		if strike.touch {
			// vanilla: AbstractCubeMob.dealDamage
			s.broadcastVoiceID(strike.w, strike.mob.Type, strike.mob.Silent, strike.x, strike.y, strike.z,
				registry.SoundEntitySlimeAttack, 1.0, strike.pitch)
		}
		effective := s.effectiveDifficultyAt(strike.w, strike.x, strike.z)
		if name, ticks, ok := strike.mob.BlowEffect(strike.w.Difficulty(), effective); ok {
			victim.AddEffect(blowEffect(registry.MustMobEffect(name), ticks, 0))
		}
		if secs := entity.BlowIgniteSeconds(effective, strike.igniteRoll); strike.ignites && secs > 0 {
			var by plugin.Entity
			if ent := s.mobEntityOf(strike.mob); ent != nil {
				by = pendingMob{id: ent.uuid}
			}
			victim.igniteForTicksBy(secs*20, by)
		}
		s.postAttack(strike.w, attackBody{mob: strike.mob}, attackBody{player: victim}, strike.mob.MainHand, &rules.EnchContext{
			DamageType: rules.DamageTypeMobAttack, Direct: true,
			Victim: protocol.EntityTypePlayer, Attacker: strike.mob.TypeID(), DirectAttacker: strike.mob.TypeID(),
		}, syncVelocityAfterTravel)
	}
}

// beeStung ends Bee.doHurtTarget for a sting that hurt: the bee has stung
// and calms, the victim gains a stinger, the sting sounds.
func (s *Server) beeStung(strike mobStrike, victim *client) {
	if strike.mob.Type != entity.TypeBee {
		return
	}
	s.mobsMu.Lock()
	strike.mob.Sting()
	s.mobsMu.Unlock()
	victim.player.UpdateVitals(func(v *player.Vitals) { v.StingerCount++ })
	s.broadcastVoiceID(strike.w, strike.mob.Type, strike.mob.Silent, strike.x, strike.y, strike.z,
		registry.SoundEntityBeeSting, 1.0, 1.0)
}

// killerBunnyBite plays the attack sound of a bite that landed.
// vanilla: Mob.doHurtTarget, Rabbit.playAttackSound
func (s *Server) killerBunnyBite(strike mobStrike) {
	if !strike.bunny || strike.mob.Silent {
		return
	}
	s.broadcastSoundID(strike.w, registry.SoundEntityRabbitAttack, protocol.SoundCategoryHostile,
		strike.x, strike.y, strike.z, 1, strike.bunnyPitch)
}

// landingMobBlow runs a monster's blow on another mob found under the mob
// lock: raw damage (mob_attack has no difficulty scaling), told like an
// arrow's mob hurt.
func (s *Server) landingMobBlow(strike mobStrike) {
	if strike.beam > 0 {
		s.guardianBeamOnMob(strike)
	}
	// Resolved before the mob lock, which the clients' lock may not nest in.
	var ownerID int32
	if strike.owner != uuid.Nil {
		if c := s.clientByUUID(strike.owner); c != nil && c.theWorld() == strike.w {
			ownerID = c.player.EntityID
		}
	}
	s.mobsMu.Lock()
	h := s.herdOf(strike.w)
	var victim *entity.Mob
	for _, m := range h.herd {
		if m.ID == strike.victimID {
			victim = m
			break
		}
	}
	if victim == nil || victim.Dead {
		s.mobsMu.Unlock()
		return
	}
	oldVX, oldVY, oldVZ, oldImpulse := victim.VX, victim.VY, victim.VZ, victim.Impulse
	var ent *mobEntity
	refused := false
	if strike.teamGated() {
		ent = h.book[victim]
		refused = ent != nil && !s.scoreboard.CanHarm(ent.uuid.String(), strike.uuid.String())
	}
	if strike.dragonContact {
		if strike.contactPush != [3]float64{} {
			victim.Push(strike.contactPush[0], strike.contactPush[1], strike.contactPush[2])
		}
		if strike.damage == 0 || refused {
			kb := s.holdMobKnockback(h.book[victim], strike.uuid, oldVX, oldVY, oldVZ, oldImpulse)
			s.mobsMu.Unlock()
			if kb != nil {
				s.applyMobKnockback(kb)
			}
			return
		}
	}
	if refused {
		s.mobsMu.Unlock()
		return
	}
	dealt, extra := strike.mob.BlowEnchantments(victim.TypeID(), strike.damage, func(ctx *rules.EnchContext) {
		s.bindBlowLoot(strike.w, ctx, attackBody{mob: strike.mob}, attackBody{mob: victim}, true)
	})
	// The spikes are read before the blow lands. vanilla: Guardian.hurtServer
	thorns := strike.beam == 0 && victim.ThornsOut()
	damageType := rules.DamageTypeMobAttack
	var landed bool
	if strike.ram && !entity.IsNautilus(strike.mob.Type) {
		damageType = rules.DamageTypeMobAttackNoAggro
		landed = victim.HurtByRam(dealt, strike.mob)
	} else {
		landed = victim.HurtByMobOwned(dealt, strike.mob, ownerID)
	}
	if strike.ram {
		// The shove follows the hurt whether or not it landed.
		// vanilla: RamTarget.tick
		victim.KnockbackAlong(strike.ramYaw, strike.ramPush)
	}
	if landed {
		// vanilla: Mob.doHurtTarget
		if !strike.ram && !strike.roar && extra > 0 {
			victim.KnockbackAlong(float64(strike.mob.Yaw), extra)
		}
		hoglinThrowMob(strike, victim)
		if strike.mob.Type == entity.TypeIronGolem {
			victim.Push(0, rules.IronGolemLift(victim.KnockbackResistance()), 0)
		}
	}
	var health []byte
	var effectTell splashMobTell
	if landed {
		if ent := h.book[victim]; ent != nil {
			// Recorded in each watcher's view so the metadata pass does not resend it.
			health = ent.syncHealth()
			// A venomous blow applies its effect to a mob as to a player.
			// vanilla: CaveSpider.doHurtTarget, Husk.doHurtTarget
			effective := s.effectiveDifficultyAt(strike.w, strike.x, strike.z)
			if secs := entity.BlowIgniteSeconds(effective, strike.mob.Rand.Float32()); strike.mob.IgnitesBlow() && secs > 0 {
				var by plugin.Entity
				if attacker := h.book[strike.mob]; attacker != nil {
					by = pendingMob{id: attacker.uuid}
				}
				victim.IgniteForTicksBy(int(secs)*20, entity.CombustSource{By: by})
			}
			name, ticks, ok := strike.mob.BlowEffect(strike.w.Difficulty(), effective)
			if strike.puff > 0 {
				name, ticks, ok = "minecraft:poison", int32(60*strike.puff), true
			}
			if ok {
				if ch, ok := victim.AddEffect(blowEffect(registry.MustMobEffect(name), ticks, 0)); ok {
					effectTell = mobEffectTellOf(ent, []effect.Change{ch})
				}
			}
		}
	}
	kind := victim.Type
	sound, volume := mobSound(victim, true), victim.SoundVolume()
	mx, my, mz := victim.X, victim.Y, victim.Z
	vx, vy, vz := victim.VX, victim.VY, victim.VZ
	yaw := mobHurtYaw(entity.Player{X: strike.x, Y: strike.y, Z: strike.z}, victim)
	kb := s.holdMobKnockback(h.book[victim], strike.uuid, oldVX, oldVY, oldVZ, oldImpulse)
	s.mobsMu.Unlock()
	if kb != nil {
		vx, vy, vz = s.applyMobKnockback(kb)
	}
	if thorns {
		s.guardianThornsOnMob(strike.w, strike.mob, victim)
	}
	if !landed {
		if strike.ram {
			s.broadcastWorld(strike.w, protocol.IDPlaySetEntityMotion,
				entityVelocityPayload(strike.victimID, vx, vy, vz))
		}
		return
	}
	s.postAttack(strike.w, attackBody{mob: strike.mob}, attackBody{mob: victim}, strike.mob.MainHand, &rules.EnchContext{
		DamageType: damageType, Direct: true, Victim: victim.TypeID(), Attacker: strike.mob.TypeID(), DirectAttacker: strike.mob.TypeID(),
	}, syncVelocityAfterTravel)

	s.killerBunnyBite(strike)
	if strike.puff > 0 {
		s.broadcastVoiceID(strike.w, strike.mob.Type, strike.mob.Silent, strike.x, strike.y, strike.z,
			registry.SoundEntityPufferFishSting, 1, 1)
	}
	s.broadcastWorld(strike.w, protocol.IDPlayDamageEvent,
		damageEventPayload(strike.victimID, damageType,
			causeEntityID(&damageCause{entityID: strike.mob.ID}), causeEntityID(&damageCause{entityID: strike.mob.ID})))
	s.broadcastWorld(strike.w, protocol.IDPlayHurtAnimation,
		hurtAnimationPayload(strike.victimID, yaw))
	s.broadcastWorld(strike.w, protocol.IDPlaySetEntityMotion,
		entityVelocityPayload(strike.victimID, vx, vy, vz))
	s.broadcastWorld(strike.w, protocol.IDPlayEntityMetadata, health)
	s.broadcastSoundHolder(strike.w, sound, mobSoundCategory(kind), mx, my, mz, volume, 1.0)

	s.tellSplashMob(strike.w, effectTell)
}

// effectiveDifficultyAt is ServerLevel.getCurrentDifficultyAt: difficulty
// scaled by the level's clock and moon and the chunk's inhabited time
// (zero if unloaded). vanilla: DifficultyInstance.calculateDifficulty
func (s *Server) effectiveDifficultyAt(w *world.World, x, z float64) float32 {
	_, clock := w.Time()
	return rules.EffectiveDifficulty(w.Difficulty(), clock,
		w.InhabitedTime(rules.BlockOf(x), rules.BlockOf(z)),
		rules.MoonBrightness(w.MoonPhase()))
}

// wake returns a chunk's mobs, once per chunk per load: read from its entity
// file, or for freshly generated terrain asked of the generator (the end's
// crystals) and the overworld's grass animals. Other modelled kinds wake into
// their own lists; unknown kinds are carried as compounds. An entity whose
// UUID is in known is not woken twice. A wake for a write generates no
// animals and carries the four kinds as tags.
func (h *mobHerd) wake(pos world.ChunkPos, s *Server, known map[uuid.UUID]bool, forWrite bool) wakeResult {
	tags, _, err := h.w.LoadChunkEntities(pos)
	if err != nil {
		slog.Warn("could not read the entities of a chunk, waking it without them",
			"x", pos.X, "z", pos.Z, "error", err)
		return wakeResult{}
	}
	if len(tags) == 0 && h.w.FreshlyGenerated(pos) {
		// Starting entities only grow on first generation; a chunk read from disk
		// with an empty entity entry stays empty. A wake for a write carries them as
		// tags so the file holds them. vanilla: ChunkStatusTasks.generateSpawn
		var generatedTags []nbt.Tag
		for _, c := range h.w.TakeGeneratedEntities(pos) {
			generatedTags = append(generatedTags, c)
		}
		woken := h.standUp(pos, generatedTags, s, known, forWrite, false)
		if !forWrite {
			woken.mobs = append(woken.mobs, generateNaturalMobs(s, h.w, pos)...)
		}
		if len(generatedTags) > 0 || len(woken.mobs) > 0 {
			// Save the terrain too, so a reload cannot regenerate its entities.
			h.w.MarkDirty(pos)
		}
		h.listed[pos] = len(generatedTags) > 0 || !forWrite
		return woken
	}
	h.listed[pos] = len(tags) > 0
	return h.standUp(pos, tags, s, known, forWrite, true)
}

// standUp turns saved entity tags into entities, skipping known UUIDs;
// mobsOnly carries the four kinds as tags, like every unsimulated kind.
// fromDisk marks loaded bodies of kinds that persist Paper.Origin so
// registration does not invent one for vanilla saves that had none.
func (h *mobHerd) standUp(pos world.ChunkPos, tags []nbt.Tag, s *Server, known map[uuid.UUID]bool, mobsOnly, fromDisk bool) wakeResult {
	var woken wakeResult
	for _, tag := range tags {
		compoundTag, ok := tag.(nbt.CompoundTag)
		if !ok {
			continue
		}
		compound := compoundTag.Entries()
		identity, hasUUID := entityUUIDOfTag(compound)
		if hasUUID && known[identity] {
			slog.Warn("not loading an entity whose UUID is already in the world",
				"uuid", identity, "x", pos.X, "z", pos.Z)
			continue
		}
		id, _ := nbt.Get[nbt.String](compound, "id")
		// A wake for a write leaves every kind but the mobs to the tick's wake,
		// which joins their Team when it stands them up.
		if mobsOnly {
			id = ""
		} else {
			woken.collectTeams(compound, known)
		}
		switch string(id) {
		case itemEntityID:
			if ent := itemOfTag(s, h.w, compound); ent != nil {
				woken.items = append(woken.items, ent)
				known[ent.uuid] = true
				continue
			}
		case arrowEntityID, spectralArrowEntityID, tridentEntityID:
			// A thrown trident loads the arrow the AbstractArrow it is.
			if ent := arrowOfTag(s, h.w, compound); ent != nil {
				woken.arrows = append(woken.arrows, ent)
				known[ent.uuid] = true
				continue
			}
		case orbEntityID:
			if ent := orbOfTag(s, h.w, compound); ent != nil {
				woken.orbs = append(woken.orbs, ent)
				known[ent.uuid] = true
				continue
			}
		case tntEntityID:
			if ent := tntOfTag(s, h.w, compound); ent != nil {
				woken.tnts = append(woken.tnts, ent)
				known[ent.uuid] = true
				continue
			}
		case fallingEntityID:
			if ent := fallingOfTag(s, h.w, compound); ent != nil {
				woken.fallings = append(woken.fallings, ent)
				known[ent.uuid] = true
				continue
			}
		case paintingEntityID:
			if ent := paintingOfTag(s, h.w, compound); ent != nil {
				woken.paintings = append(woken.paintings, ent)
				known[ent.uuid] = true
				continue
			}
		case frameEntityID, glowFrameEntityID:
			if ent := frameOfTag(s, h.w, compound); ent != nil {
				woken.frames = append(woken.frames, ent)
				known[ent.uuid] = true
				continue
			}
		case entity.MinecartPlain, entity.MinecartChest, entity.MinecartHopper, entity.MinecartTNT, entity.MinecartFurnace, entity.MinecartCommand, entity.MinecartSpawner:
			if ent := minecartOfTag(s, h.w, compound); ent != nil {
				woken.minecarts = append(woken.minecarts, ent)
				known[ent.uuid] = true
				ent.carriedPassengers = h.standVehicleRiders(s, compound, known, vehicleSeat{cart: ent}, &woken)
				continue
			}
		case armorStandEntityID:
			if ent := armorStandOfTag(s, h.w, compound); ent != nil {
				woken.armorStands = append(woken.armorStands, ent)
				known[ent.uuid] = true
				continue
			}
		case mannequinEntityID:
			if ent := mannequinOfTag(s, h.w, compound); ent != nil {
				woken.mannequins = append(woken.mannequins, ent)
				known[ent.uuid] = true
				continue
			}
		case endCrystalEntityID:
			if ent := endCrystalOfTag(s, h.w, compound); ent != nil {
				woken.endCrystals = append(woken.endCrystals, ent)
				known[ent.uuid] = true
				continue
			}
		case entity.FixtureBlockDisplay, entity.FixtureItemDisplay, entity.FixtureTextDisplay,
			entity.FixtureInteraction, entity.FixtureMarker, entity.FixtureOminousItemSpawner, entity.FixtureCushion:
			if ent := fixtureOfTag(s, h.w, compound); ent != nil {
				woken.fixtures = append(woken.fixtures, ent)
				known[ent.uuid] = true
				continue
			}
		case "minecraft:snowball", "minecraft:egg", "minecraft:ender_pearl",
			"minecraft:splash_potion", "minecraft:lingering_potion", "minecraft:experience_bottle",
			"minecraft:wind_charge", "minecraft:small_fireball", "minecraft:fireball", "minecraft:dragon_fireball",
			"minecraft:wither_skull", "minecraft:shulker_bullet":
			if ent := s.thrownOfTag(h.w, compound); ent != nil {
				woken.thrown = append(woken.thrown, ent)
				known[ent.uuid] = true
				continue
			}
		case cloudEntityID:
			if ent := s.cloudOfTag(h.w, compound); ent != nil {
				woken.clouds = append(woken.clouds, ent)
				known[ent.uuid] = true
				continue
			}
		case fireworkEntityID:
			if ent := s.fireworkOfTag(h.w, compound); ent != nil {
				woken.fireworks = append(woken.fireworks, ent)
				known[ent.uuid] = true
				continue
			}
		case eyeEntityID:
			if ent := s.eyeOfTag(h.w, compound); ent != nil {
				woken.eyes = append(woken.eyes, ent)
				known[ent.uuid] = true
				continue
			}
		default:
			// Each boat kind's id is its own name, so no constant names them.
			if string(id) != "" && entity.BoatKind(string(id)) != "" {
				if ent := boatOfTag(s, h.w, compound); ent != nil {
					woken.boats = append(woken.boats, ent)
					known[ent.uuid] = true
					ent.carriedPassengers = h.standVehicleRiders(s, compound, known, vehicleSeat{boat: ent}, &woken)
					continue
				}
			}
		}
		if ent := mobOfTag(s, h.w, compound); ent != nil {
			if mobsOnly {
				woken.collectTeams(compound, known)
			}
			ent.pendingPassengers = passengersOfTag(compound)
			woken.mobs = append(woken.mobs, ent)
			known[ent.uuid] = true
			woken.mobs = append(woken.mobs, standPassengers(h, s, ent, compound, known)...)
			continue
		}
		if !mobsOnly {
			withoutTeam(compoundTag)
		}
		h.carried[pos] = append(h.carried[pos], compoundTag)
	}
	linkMobRides(woken.mobs)
	if fromDisk {
		markOriginsLoaded(woken.mobs)
		markOriginsLoaded(woken.items)
		markOriginsLoaded(woken.tnts)
		markOriginsLoaded(woken.fallings)
		markOriginsLoaded(woken.boats)
		markOriginsLoaded(woken.minecarts)
		markOriginsLoaded(woken.armorStands)
	}
	return woken
}

// passengersOfTag reads the UUIDs of a Passengers list: nested compounds,
// or plain UUID arrays another save may write.
// vanilla: Entity.addSaveData
func passengersOfTag(compound nbt.Compound) []uuid.UUID {
	list, ok := nbt.Get[nbt.List](compound, "Passengers")
	if !ok {
		return nil
	}
	var ids []uuid.UUID
	for _, item := range list.Items {
		switch v := item.(type) {
		case nbt.CompoundTag:
			if id, ok := entityUUIDOfTag(v.Entries()); ok {
				ids = append(ids, id)
			}
		case nbt.IntArray:
			if id, ok := nbt.UUIDFromIntArray(v); ok && id != uuid.Nil {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// standPassengers spawns the nested riders of a Passengers list,
// recursively, and returns them. One already live is left to the file's
// top-level copy; an unsimulated kind stays on the vehicle to write back.
// vanilla: Entity.addSaveData
func standPassengers(h *mobHerd, s *Server, parent *mobEntity, compound nbt.Compound, known map[uuid.UUID]bool) []*mobEntity {
	list, ok := nbt.Get[nbt.List](compound, "Passengers")
	if !ok {
		return nil
	}
	var mobs []*mobEntity
	for _, item := range list.Items {
		saved, ok := item.(nbt.CompoundTag)
		if !ok {
			continue
		}
		c := saved.Entries()
		if identity, ok := entityUUIDOfTag(c); ok && known[identity] {
			continue
		}
		ent := mobOfTag(s, h.w, c)
		if ent == nil {
			parent.carriedPassengers = append(parent.carriedPassengers, withoutTeam(saved))
			continue
		}
		ent.pendingPassengers = passengersOfTag(c)
		known[ent.uuid] = true
		mobs = append(mobs, ent)
		mobs = append(mobs, standPassengers(h, s, ent, c, known)...)
	}
	return mobs
}

// standVehicleRiders stands up the mobs a boat's or cart's Passengers list
// nests, with their own riders, adding each to woken with seat naming the
// vehicle, and returns the rider compounds of unsimulated kinds to carry.
// vanilla: EntityType.loadPassengersRecursive
func (h *mobHerd) standVehicleRiders(s *Server, compound nbt.Compound, known map[uuid.UUID]bool, seat vehicleSeat, woken *wakeResult) []nbt.Tag {
	list, ok := nbt.Get[nbt.List](compound, "Passengers")
	if !ok {
		return nil
	}
	var carried []nbt.Tag
	for _, item := range list.Items {
		saved, ok := item.(nbt.CompoundTag)
		if !ok {
			continue
		}
		c := saved.Entries()
		if identity, ok := entityUUIDOfTag(c); ok && known[identity] {
			continue
		}
		rider := mobOfTag(s, h.w, c)
		if rider == nil {
			carried = append(carried, withoutTeam(saved))
			continue
		}
		rider.pendingPassengers = passengersOfTag(c)
		known[rider.uuid] = true
		woken.mobs = append(woken.mobs, rider)
		woken.mobs = append(woken.mobs, standPassengers(h, s, rider, c, known)...)
		seat.rider = rider
		woken.seats = append(woken.seats, seat)
	}
	return carried
}

// linkMobRides seats saved riders by UUID once the whole chunk is awake.
func linkMobRides(mobs []*mobEntity) {
	if len(mobs) == 0 {
		return
	}
	byUUID := make(map[uuid.UUID]*mobEntity, len(mobs))
	for _, e := range mobs {
		byUUID[e.uuid] = e
	}
	for _, veh := range mobs {
		for _, pu := range veh.pendingPassengers {
			p := byUUID[pu]
			if p == nil || p == veh || p.mob.Vehicle != nil {
				continue
			}
			entity.Ride(veh.mob, p.mob)
		}
		veh.pendingPassengers = nil
	}
}

// boxMobs queues the chunk's whole entity list; steppedOff names the vehicle
// each mob departing with a boat or cart sat in. The world retains failed writes.
func (h *mobHerd) boxMobs(pos world.ChunkPos, mobs []*entity.Mob, extra []nbt.Tag, steppedOff map[*mobEntity]uuid.UUID) {
	entities := h.chunkEntities(pos, mobs, extra, steppedOff)
	h.w.QueueChunkEntitiesStamped(pos, h.w.StampChunkEntities(pos), entities)
	h.listed[pos] = len(entities) > 0
}

// chunkEntities is a chunk's entity list as it is written: the carried
// entities, the live mobs' tags and the caller's extras. A mob seated in a
// boat or cart among the extras, or stepped off one (steppedOff), is nested
// under its Passengers; one whose vehicle is not in this write stays
// top-level. The extras are not changed, as a failed write retries them.
// vanilla: Entity.save, Entity.saveWithoutId
func (h *mobHerd) chunkEntities(pos world.ChunkPos, mobs []*entity.Mob, extra []nbt.Tag, steppedOff map[*mobEntity]uuid.UUID) []nbt.Tag {
	entities := make([]nbt.Tag, 0, len(h.carried[pos])+len(mobs)+len(extra))
	entities = append(entities, h.carried[pos]...)
	var vehicles map[uuid.UUID][]nbt.Tag
	for _, t := range extra {
		if c, ok := t.(nbt.CompoundTag); ok {
			if id, ok := entityUUIDOfTag(c); ok {
				if vehicles == nil {
					vehicles = make(map[uuid.UUID][]nbt.Tag)
				}
				vehicles[id] = nil
			}
		}
	}
	for _, m := range mobs {
		if m.Vehicle != nil {
			// A rider is written nested under its mount, never also top-level,
			// whatever chunk it is in. vanilla: Entity.save, Entity.addSaveData
			continue
		}
		ent, ok := h.book[m]
		if !ok {
			continue
		}
		tag := mobTagIn(ent, h.book)
		if vehicle, ok := seatedVehicle(ent, steppedOff); ok {
			if riders, ok := vehicles[vehicle]; ok {
				vehicles[vehicle] = append(riders, tag)
				continue
			}
		}
		entities = append(entities, tag)
	}
	for _, t := range extra {
		if c, ok := t.(nbt.CompoundTag); ok {
			if id, ok := entityUUIDOfTag(c); ok && len(vehicles[id]) > 0 {
				c = nbt.Snapshot(c).(nbt.CompoundTag)
				list, _ := nbt.Get[nbt.List](c.Entries(), "Passengers")
				c.Set("Passengers", nbt.List{Elem: nbt.TypeCompound, Items: slices.Concat(list.Items, vehicles[id])})
				t = c
			}
		}
		entities = append(entities, t)
	}
	return entities
}

// seatedVehicle is the UUID of the boat or cart the mob sits in, or that
// steppedOff says it left with its chunk. The vehicle's uuid never changes,
// so no vehicle lock is taken; the caller holds the mob lock.
func seatedVehicle(ent *mobEntity, steppedOff map[*mobEntity]uuid.UUID) (uuid.UUID, bool) {
	if id, ok := steppedOff[ent]; ok {
		return id, true
	}
	switch {
	case ent.cart != nil:
		return ent.cart.uuid, true
	case ent.vehicle != nil:
		return ent.vehicle.uuid, true
	}
	return uuid.Nil, false
}

// mobWrite is one chunk's entity file, tags already read from the live mobs.
type mobWrite struct {
	h        *mobHerd
	pos      world.ChunkPos
	entities []nbt.Tag
	stamp    uint64
}

// wakeForWrite wakes an unwoken chunk about to be written, since the file
// is written whole: its saved mobs join the herd and its other entities are
// carried until the tick's wake. A mob can be in such a chunk by walking in
// or being added after the tick boxed it. An unreadable file wakes empty
// and the world refuses to overwrite it. A freshly generated chunk grows its
// animals here, as this file is what later wakes read.
// vanilla: PersistentEntitySectionManager.storeChunkSections loads a FRESH
// chunk before storing it.
func (h *mobHerd) wakeForWrite(pos world.ChunkPos, s *Server, live map[uuid.UUID]bool) []*entity.Mob {
	h.woken[pos] = struct{}{}
	h.wokenForWrite[pos] = true
	woken := h.wake(pos, s, live, true)
	h.writeTeams = append(h.writeTeams, woken.teams...)
	if h.w.FreshlyGenerated(pos) {
		woken.mobs = generateNaturalMobs(s, h.w, pos)
		if len(woken.mobs) > 0 {
			h.w.MarkDirty(pos)
		}
	}
	mobs := make([]*entity.Mob, 0, len(woken.mobs))
	for _, ent := range woken.mobs {
		ent.uuidText = ent.uuid.String()
		h.remember(ent)
		mobs = append(mobs, ent.mob)
	}
	return mobs
}

// liveWith is known plus the herd's mobs, the refusal set of a wake for a
// write.
func (h *mobHerd) liveWith(known map[uuid.UUID]bool) map[uuid.UUID]bool {
	live := maps.Clone(known)
	for _, ent := range h.book {
		live[ent.uuid] = true
	}
	return live
}

// herdChunk is the chunk a body saves with: its root vehicle's, since a
// passenger saves with its base mount.
// vanilla: PersistentEntitySectionManager.getPassengersAndSelf
func herdChunk(m *entity.Mob) world.ChunkPos {
	root := m.RootVehicle()
	return world.ChunkPos{X: world.ChunkOf(root.X), Z: world.ChunkOf(root.Z)}
}

// saveMobs writes every live entity to its chunk's entity file, leaving the
// mobs live.
func (s *Server) saveMobs() {
	s.writeMobs(s.snapshotMobs())
	// Explicit saves also include snapshots already removed from the live herd.
	for _, dim := range s.allDims() {
		dim.w.DrainSaves()
	}
}

// snapshotMobs reads every live entity's tags, for writeMobs to write. The
// four kinds' tags are read before the mob lock, each under its own lock in
// turn; an entity crossing a chunk border in between is written where it was
// read, and the next save fixes it. No file is written under the mob lock: the
// tick takes it every turn.
func (s *Server) snapshotMobs() []mobWrite {
	kindTags := s.liveEntityTags()
	known := s.liveEntityUUIDs()
	var writes []mobWrite
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		h := dim.mobs
		byChunk := make(map[world.ChunkPos][]*entity.Mob, len(h.herd))
		// Write every woken chunk, empty ones too, so a mob that left is no longer
		// listed.
		for pos := range h.woken {
			if h.listed[pos] && h.w.ChunkLoaded(pos) {
				byChunk[pos] = nil
			}
		}
		for _, m := range h.herd {
			pos := herdChunk(m)
			byChunk[pos] = append(byChunk[pos], m)
		}
		// Loaded chunks holding the four kinds are written too; one no tick has
		// woken is woken first so its file is read before being overwritten.
		tags := kindTags[dim.w]
		for pos := range tags {
			if _, ok := byChunk[pos]; ok {
				continue
			}
			if h.w.ChunkLoaded(pos) {
				byChunk[pos] = nil
			}
		}
		// Wake an unwoken chunk before writing it; its saved mobs stay live.
		var live map[uuid.UUID]bool
		for pos := range byChunk {
			if _, ok := h.woken[pos]; ok {
				continue
			}
			if live == nil {
				live = h.liveWith(known)
			}
			mobs := h.wakeForWrite(pos, s, live)
			h.add(mobs...)
			byChunk[pos] = append(byChunk[pos], mobs...)
		}
		for pos, mobs := range byChunk {
			writes = append(writes, mobWrite{h, pos, h.chunkEntities(pos, mobs, tags[pos], nil), h.w.StampChunkEntities(pos)})
			h.listed[pos] = len(writes[len(writes)-1].entities) > 0
		}
	}
	return writes
}

// writeMobs writes the entity files snapshotMobs read. A failed write leaves
// the chunk listed, so the next save rewrites a file that may still name mobs
// the chunk no longer has.
func (s *Server) writeMobs(writes []mobWrite) {
	for _, mw := range writes {
		err := mw.h.w.SaveChunkEntitiesStamped(mw.pos, mw.stamp, mw.entities)
		if err == nil {
			continue
		}
		slog.Error("could not save the mobs of a chunk",
			"x", mw.pos.X, "z", mw.pos.Z, "error", err)
		// Only while nothing newer was read and the chunk is still woken: a
		// later write has set the flag from its own entities, and an unloaded
		// chunk's flag is set again by its next wake.
		s.mobsMu.Lock()
		if _, woken := mw.h.woken[mw.pos]; woken && mw.h.w.ChunkEntitiesStampCurrent(mw.pos, mw.stamp) {
			mw.h.listed[mw.pos] = true
		}
		s.mobsMu.Unlock()
	}
}

// addMob adds live mobs of one world to its herd and shows them to players
// in tracking range, under both locks like spawnItem, so a client joining at
// the same moment sees each either here or in its join. A riding stack is
// added at one go, so no save finds a rider without its mount.
func (s *Server) addMob(ents ...*mobEntity) {
	s.addMobCore(false, ents...)
}

// addMobCore is addMob; with unique, a UUID already in the herd is refused
// (vanilla's tryAddFreshEntityWithPassengers) and it returns false.
func (s *Server) addMobCore(unique bool, ents ...*mobEntity) bool {
	h := s.herdOf(ents[0].w)
	for _, ent := range ents {
		if ent.name == nil && ent.mob.KillerBunny() {
			// vanilla: Rabbit.setVariant names the killer bunny unless it has a name
			ent.name = protocol.TextTag(protocol.Text{Translate: "entity.minecraft.killer_bunny"})
		}
		// Before the spawn, as vanilla joins it ahead of addFreshEntity; the board
		// broadcasts, so no lock is held.
		if ent.convertTeam != "" {
			s.scoreboard.JoinTeam(ent.convertTeam, []string{ent.uuid.String()})
			ent.convertTeam = ""
		}
	}

	s.mu.RLock()
	// Watchers are read holding only the player list, as the tick's sweep does,
	// so player locks are never taken under the mob lock.
	watchers := s.trackingWatchersLocked()

	s.mobsMu.Lock()
	if unique {
		for _, live := range h.book {
			for _, ent := range ents {
				if live.uuid == ent.uuid {
					s.mobsMu.Unlock()
					s.mu.RUnlock()
					return false
				}
			}
		}
	}
	// On the stack for a stack of up to four.
	var shownBuf [4][]*client
	var packetsBuf [4][]outbound
	shown, packets := shownBuf[:0], packetsBuf[:0]
	for _, ent := range ents {
		ent.mob.Gate = mobHurtGate{s}
		ent.mob.EffectGate = mobEffectGate{s}
		ent.mob.Barter = mobBarterGate{s}
		ent.mob.Career = s.askCareer
		ent.mob.Convert = s.askTransform
		ent.mob.Combust = s.askCombust
		h.add(ent.mob)
		ent.uuidText = ent.uuid.String()
		h.remember(ent)
		shown = append(shown, mobTracker.showLocked(ent, watchers))
		// Built under the locks from the mob as it is; the tick may move it
		// before the sends run.
		packets = append(packets, mobPairing(ent))
	}
	s.mobsMu.Unlock()
	s.mu.RUnlock()

	for i, ent := range ents {
		sendPacketsToAll(ent.w, shown[i], packets[i])
	}
	return true
}

// mobPairing is what shows a mob: spawn, metadata (effect particles
// included), equipment, attributes, mob passengers and, last, a lead.
// Effect packets go only to its riders; a boat is the one rideable body it
// does not name.
// vanilla: ServerEntity.sendPairingData
func mobPairing(ent *mobEntity) []outbound {
	return bundle(mobPairingPackets(ent))
}

// mobPairingPackets is mobPairing without the bundle delimiters.
func mobPairingPackets(ent *mobEntity) []outbound {
	packets := pairing(
		outbound{id: protocol.IDPlaySpawnEntity, data: spawnMobPayload(ent)},
		outbound{id: protocol.IDPlayEntityMetadata, data: mobMetadataPayload(ent)},
		outbound{id: protocol.IDPlayEntityEquipment, data: mobEquipmentPayload(ent)},
		outbound{id: protocol.IDPlayUpdateAttributes, data: mobAttributesPayload(ent)},
	)
	if !ent.mob.SaddleSlot().Empty() {
		packets = append(packets, outbound{id: protocol.IDPlayEntityEquipment, data: saddlePayload(ent.mob)})
	}
	if ids := ent.mob.PassengerIDs(); len(ids) > 0 {
		// The passenger link goes last before a lead: a viewer that has not met the
		// riders gets their spawns after it.
		packets = append(packets, outbound{id: protocol.IDPlaySetPassengers,
			data: setPassengersPayload(ent.mob.ID, ids)})
	}
	if v := ent.mob.Vehicle; v != nil {
		// A rider's pairing also carries the mount's list after the rider's spawn,
		// so a viewer that met the mount first learns the link.
		// vanilla: ServerEntity.sendPairingData
		packets = append(packets, outbound{id: protocol.IDPlaySetPassengers,
			data: setPassengersPayload(v.ID, v.PassengerIDs())})
	}
	if ent.mob.LeashHolderID != 0 {
		// The lead is told last. vanilla: ServerEntity.sendPairingData
		packets = append(packets, outbound{id: protocol.IDPlaySetEntityLink,
			data: leashLinkPayload(ent.mob.ID, ent.mob.LeashHolderID)})
	}
	return packets
}

// spawnGhastling spawns a dried ghast's baby happy ghast at the cell's
// bottom centre, facing as the block did, then plays the entity sound.
// vanilla: DriedGhastBlock.spawnGhastling
func (s *Server) spawnGhastling(dim *dimension, g world.GhastlingSpawn) {
	x, y, z := float64(g.X)+0.5, float64(g.Y), float64(g.Z)+0.5
	id := s.spawnMobArriving(dim, entity.TypeHappyGhast, x, y, z, 0, nil, true, "", false, func(ent *mobEntity) {
		yaw := commandBlockFacingYaw(g.Facing)
		ent.mob.Yaw, ent.mob.HeadYaw, ent.mob.Pitch = yaw, yaw, 0
	})
	s.broadcastEntitySoundID(dim.w, nil, id, registry.SoundEntityGhastlingSpawn, protocol.SoundCategoryBlock, x, y, z, 1.0, 1.0)
}

// hatchTurtles spawns an egg block's hatchlings along the cell's south-west
// edge, each homed on the cell. vanilla: TurtleEggBlock.randomTick
func (s *Server) hatchTurtles(dim *dimension, h world.TurtleHatch) {
	for i := range h.Eggs {
		x := float64(h.X) + 0.3 + float64(i)*0.2
		z := float64(h.Z) + 0.3
		s.spawnMobArriving(dim, entity.TypeTurtle, x, float64(h.Y), z, 0, nil, true, "", false, func(ent *mobEntity) {
			ent.spawnReason = spawnReasonCode(plugin.SpawnEgg)
			ent.mob.Home = entity.Waypoint{X: h.X, Y: h.Y, Z: h.Z}
		})
	}
}

// spawnStandsOnGround reports whether a body at (x, y, z) stands on a
// block.
func spawnStandsOnGround(w *world.World, x, y, z float64) bool {
	state := w.BlockAt(int32(math.Floor(x)), int32(math.Floor(y))-1, int32(math.Floor(z)))
	return registry.CollisionHeight(state) > 0
}

// spawnMobInDim spawns a live mob and shows it; every arrival but a chunk's
// wake goes through it (summon, natural spawning, tests). A zero seed mints
// a new UUID and seeds the random source from it, like a loaded mob. The
// mob is finalized against its biome (the chunk woken first), where a sheep
// gets its colour and the spawn group decides babies; group is nil for a
// lone spawn.
func (s *Server) spawnMobInDim(dim *dimension, kind entity.MobType, x, y, z float64, seed int64, group *entity.SpawnGroup) int32 {
	return s.spawnMobArriving(dim, kind, x, y, z, seed, group, false, "", true, nil)
}

// spawnMobArriving spawns the mob and returns its entity ID.
func (s *Server) spawnMobArriving(dim *dimension, kind entity.MobType, x, y, z float64, seed int64, group *entity.SpawnGroup, baby bool, variant string, finalize bool, prep func(*mobEntity)) int32 {
	return s.standMobArriving(dim, kind, x, y, z, seed, group, baby, variant, finalize, prep).mob.ID
}

// standMobArriving spawns an arrival. baby makes an egg's chick, born a
// baby and never finalized. finalize false skips finalizeSpawn (a pattern
// golem, created plain). variant is a chick's egg variant, "" for none.
// prep runs before anyone sees the mob. A jockey the finalize rolled spawns
// after the mob, through this same function.
// vanilla: ThrownEgg.onHit, CarvedPumpkinBlock.trySpawnGolem
func (s *Server) standMobArriving(dim *dimension, kind entity.MobType, x, y, z float64, seed int64, group *entity.SpawnGroup, baby bool, variant string, finalize bool, prep func(*mobEntity)) *mobEntity {
	id := s.nextMobEntity(kind)
	identity := uuid.New()
	if seed == 0 {
		seed = s.entitySeed(id)
	}
	m := entity.NewMob(kind, id, x, y, z, seed)
	m.SetParrotSoundRandom(s.envRng)
	// Grounded only over a block: a blow before the first tick lifts a grounded
	// spawn, and an airborne one cannot jump a fence before its first fall.
	m.OnGround = spawnStandsOnGround(dim.w, x, y, z)
	ent := &mobEntity{
		mob:  m,
		w:    dim.w,
		uuid: identity,
	}
	// An unvisited chunk may hold saved mobs on disk; waking it keeps the new
	// mob from overwriting them.
	if woken, ok := s.wakeChunkOnce(dim, world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}); ok {
		s.registerWake(woken)
	}
	// Finalizing reads the biome, which loads the chunk, as a vanilla summon
	// does. An egg's chick and a pattern golem skip it.
	if baby {
		m.SetBaby(true)
		if variant != "" {
			m.Variant = variant
		}
	} else if finalize {
		s.finalizeMobSpawn(dim.w, ent, group, false)
	}
	if prep != nil {
		prep(ent)
	}
	if ent.spawnReason == 0 && group != nil {
		ent.spawnReason = spawnReasonCode(plugin.SpawnNatural)
	}
	ent.mob.DressSpawnLeader(ominousBanner())
	// Snapshot before publication: commands and ticks can move or silence the mob.
	spawnSound := finalize && !baby && m.Type == entity.TypeCopperGolem && !m.Silent
	spawnX, spawnY, spawnZ := m.X, m.Y, m.Z
	s.addMob(ent)
	if !s.spawnAllowed(ent) {
		return ent
	}
	if spawnSound {
		// Send the body before its voice. vanilla: CopperGolem.finalizeSpawn
		s.broadcastSoundID(dim.w, registry.SoundEntityCopperGolemSpawn, protocol.SoundCategoryNeutral, spawnX, spawnY, spawnZ, 1, 1)
	}
	if m.Jockey != entity.JockeyNone {
		// A jockey's other half spawns after the arrival, so the passenger link
		// names bodies the views already have.
		s.resolveJockey(dim, ent)
	}
	return ent
}

// finalizeMobSpawn shares the world-dependent spawn rolls for arrivals and generated tags.
func (s *Server) finalizeMobSpawn(w *world.World, ent *mobEntity, group *entity.SpawnGroup, structure bool) {
	m := ent.mob
	if entity.IsRaiderType(m.Type) {
		// vanilla: Raider.finalizeSpawn
		ent.raider = &raiderRef{s: s, ent: ent, canJoin: m.Type != entity.TypeWitch || structure || !group.Natural()}
	}
	x, y, z := m.X, m.Y, m.Z
	pos := world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}
	w.Acquire(pos)
	defer w.Release(pos)
	effective := s.effectiveDifficultyAt(w, x, z)
	local := rules.NewLocalDifficulty(w.Difficulty(), effective)
	local.Halloween = rules.IsHalloween(s.spawnTime())
	local.Moon = rules.MoonBrightness(w.MoonPhase())
	biome := w.Biome(rules.BlockOf(x), rules.BlockOf(y), rules.BlockOf(z))
	if m.Type == entity.TypeStrider {
		m.MonsterJockeysDisabled = !spawningMonsters(w)
	}
	if m.Type == entity.TypeHusk {
		minX, minY, minZ, maxX, maxY, maxZ := entity.CamelHuskSpawnBox(rules.BlockOf(x), rules.BlockOf(y), rules.BlockOf(z))
		m.CamelHuskFits = !w.BoxCollides(minX, minY, minZ, maxX, maxY, maxZ)
	}
	bx, by, bz := rules.BlockOf(x), rules.BlockOf(y), rules.BlockOf(z)
	ctx := rules.SpawnContext{
		Biome:          biome,
		MoonBrightness: local.Moon,
		InStructure:    entity.StructurePredicate(w, bx, by, bz),
	}
	if structure {
		m.FinalizeStructureSpawnAt(m.SpawnDice(), ctx, local)
	} else {
		m.FinalizeSpawnAt(m.SpawnDice(), ctx, group, local)
	}
	m.InitBruteHome(w.DimensionName())
	m.SetSpawnSize(rules.SpecialMultiplier(effective))
}

// structureMobFinalizer finalizes a structure mob's tag in dim as the
// generator places it, at the local difficulty of a chunk never inhabited. It
// reads only the level's difficulty and clock, so it acquires no chunk.
// vanilla: WorldGenRegion.getCurrentDifficultyAt
func (s *Server) structureMobFinalizer(dim string) world.StructureMobFinalizer {
	return func(tag nbt.Compound, biome registry.Biome, blackCat bool, rng levelgen.RandomSource) nbt.Compound {
		_, clock := s.world.Time()
		moon := rules.MoonBrightness(s.world.MoonPhase())
		d := s.world.Difficulty()
		local := rules.NewLocalDifficulty(d, rules.EffectiveDifficulty(d, clock, 0, moon))
		local.Halloween = rules.IsHalloween(s.spawnTime())
		local.Moon = moon
		return entity.FinalizeStructureTag(tag, dim, biome, blackCat, local, rng)
	}
}

// errDuplicateUUID refuses a summon tag whose UUID a live mob already has;
// errNotSimulated refuses a kind this server has no mob for.
var (
	errDuplicateUUID = errors.New("duplicate uuid")
	errNotSimulated  = errors.New("mob kind not simulated")
)

// spawnSummonedMobInDim spawns a summon with a tag: the mob is read from
// the tag by the chunk loader's reader, so it carries every saved field,
// and finalizeSpawn is skipped. The command's position overrides the tag's.
// vanilla: SummonCommand.createEntity
func (s *Server) spawnSummonedMobInDim(dim *dimension, kind entity.MobType, x, y, z float64, tag nbt.Compound) (int32, error) {
	return s.spawnSummonedMobFacing(dim, kind, x, y, z, tag, nil)
}

// spawnSummonedMobFacing is spawnSummonedMobInDim with prep run before
// anyone sees the mob.
func (s *Server) spawnSummonedMobFacing(dim *dimension, kind entity.MobType, x, y, z float64, tag nbt.Compound, prep func(*mobEntity)) (int32, error) {
	compound := maps.Clone(tag)
	compound["id"] = nbt.String(kind.Name())
	if woken, ok := s.wakeChunkOnce(dim, world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}); ok {
		s.registerWake(woken)
	}
	// Load the mob's chunk, as a summon forces it.
	pos := world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}
	dim.w.Acquire(pos)
	ent := mobOfTag(s, dim.w, compound)
	dim.w.Release(pos)
	if ent == nil {
		return 0, errNotSimulated
	}
	ent.mob.SetPos(x, y, z)
	// An explicit ground flag survives loading, even when NoAI prevents travel.
	// vanilla: Entity.load, LivingEntity.aiStep
	if _, ok := nbt.ByteField(compound, "OnGround"); !ok {
		ent.mob.OnGround = !ent.mob.NoAI && spawnStandsOnGround(dim.w, x, y, z)
	}
	if prep != nil {
		prep(ent)
	}
	giveRidersUUIDs(compound)
	var riders wakeResult
	mobs := s.standWithRiders(s.herdOf(dim.w), ent, compound, &riders)
	for _, r := range mobs[1:] {
		if r.spawnReason == 0 {
			r.spawnReason = ent.spawnReason
		}
	}
	if !s.addMobCore(true, mobs...) {
		return 0, errDuplicateUUID
	}
	if !s.spawnAllowed(ent) {
		for _, r := range mobs[1:] {
			s.discardSpawned(r)
		}
		return ent.mob.ID, nil
	}
	// A summoned vex may name an evoker already live, and a tagged summon
	// may bring back an evoker a live vex was saved to follow.
	s.relinkVexOwners()
	for _, t := range riders.teams {
		s.joinTagTeam(t)
	}
	return ent.mob.ID, nil
}

// giveRidersUUIDs gives every nested rider compound that lacks a UUID a
// random one, so the seating can name it. It writes into a copy of each
// Passengers list: a spawner's clone shares them with its SpawnData.
func giveRidersUUIDs(compound nbt.CompoundTag) {
	list, ok := nbt.Get[nbt.List](compound.Entries(), "Passengers")
	if !ok {
		return
	}
	list, _ = nbt.Copy(list).(nbt.List)
	compound.Set("Passengers", list)
	for _, item := range list.Items {
		c, ok := item.(nbt.CompoundTag)
		if !ok {
			continue
		}
		if _, has := entityUUIDOfTag(c); !has {
			c.Set("UUID", nbt.UUIDToIntArray(uuid.New()))
		}
		giveRidersUUIDs(c)
	}
}

// mobByEntityID finds a live mob by ID in the actor's world only.
func (s *Server) mobByEntityID(w *world.World, id int32) *mobEntity {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	return s.mobByEntityIDLocked(w, id)
}

// mobByEntityIDLocked is mobByEntityID for a caller already holding mobsMu.
func (s *Server) mobByEntityIDLocked(w *world.World, id int32) *mobEntity {
	h := s.herdOf(w)
	for _, m := range h.herd {
		if m.ID == id || (m.Type == entity.TypeEnderDragon && id > m.ID && id <= m.ID+8) {
			return h.book[m]
		}
	}
	return nil
}

// mobNameByID is the display name of the mob with this ID in w, false for
// none. Read under mobsMu, which guards name tag names.
func (s *Server) mobNameByID(w *world.World, id int32) (string, bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	ent := s.mobByEntityIDLocked(w, id)
	if ent == nil {
		return "", false
	}
	return mobName(ent), true
}

// mobKillerByID is the death-message killer for the mob with this ID in w,
// false for none. Read under mobsMu, which guards name tag names.
func (s *Server) mobKillerByID(w *world.World, id int32) (*rules.Killer, bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	ent := s.mobByEntityIDLocked(w, id)
	if ent == nil {
		return nil, false
	}
	return s.mobKiller(ent), true
}

// entityKiller is the death-message killer for an entity ID: a player's
// name and held item, a mob's display name, or nil.
func (s *Server) entityKiller(w *world.World, id int32) *rules.Killer {
	if id == 0 {
		return nil
	}
	if c := s.entityClient(id); c != nil {
		return &rules.Killer{Name: c.DisplayName(), Weapon: c.heldItem(protocol.HandMain)}
	}
	if k, ok := s.mobKillerByID(w, id); ok {
		return k
	}
	return nil
}

// killMob kills like /kill (vanilla: LivingEntity.kill): generic-kill
// damage at max, which bypasses invulnerability, with the invulnerability
// window cleared first so even an unhurtable body dies. With no source
// entity there is no shove and no player credit; the death is otherwise
// ordinary (corpse, drops, sound). Dragons override this with direct removal.
func (s *Server) killMob(ent *mobEntity) {
	if ent.mob.Type == entity.TypeEnderDragon {
		s.killDragon(ent)
		return
	}
	// A command's death effects need current kill credit even while frozen.
	// Do not read tick inputs here: that also advances player movement state.
	in := mobTickInputs{clientByEntityID: make(map[int32]*client)}
	s.mu.RLock()
	for _, c := range s.clients {
		in.clientByEntityID[c.player.EntityID] = c
	}
	s.mu.RUnlock()
	centers := s.lootRaidCenters()
	fx := newMobEffects(s)
	s.mobsMu.Lock()
	h := s.herdOf(ent.w)
	if h.book[ent.mob] != ent || !ent.mob.Alive() {
		s.mobsMu.Unlock()
		return
	}
	events := ent.mob.KillAndReport(ent.w)
	if ent.mob.Dead {
		// vanilla: LivingEntity.hurtServer broadcasts damage before death feedback.
		fx.sends = append(fx.sends, mobSend{w: ent.w, id: protocol.IDPlayDamageEvent,
			data: damageEventPayload(ent.mob.ID, rules.DamageTypeGenericKill, 0, 0)})
		h.remove(ent.mob)
	}
	t := mobDimTick{s: s, dim: s.dimensionOfWorld(ent.w), h: h, w: ent.w, raidCenters: centers[ent.w]}
	t.mountGameEvents(&fx, events)
	for _, ev := range events {
		t.event(&fx, &in, ev)
	}
	s.mobsMu.Unlock()
	fx.apply(s, &in)
}

// teleportMob moves the mob, leaving the telling to the movement tick.
// vanilla: Entity.teleportTo, ServerEntity.sendChanges
func (s *Server) teleportMob(ent *mobEntity, to player.Position, yaw, pitch float32, by string) {
	s.mobsMu.Lock()
	m := ent.mob
	m.SetPos(to.X, to.Y, to.Z)
	// Stop the vertical fall; horizontal motion is kept. The ground flag is
	// left to the next movement tick, as vanilla's teleportSetPosition does.
	m.VY = 0
	m.Yaw, m.Pitch = yaw, clampBodyPitch(pitch)
	// vanilla: Entity.teleportSetPosition
	m.HeadYaw = yaw
	// vanilla: AbstractVillager.teleport stops trading
	m.Trading = false
	ent.teleported = true
	s.herdOf(ent.w).pistonGrid.built = false
	s.mobsMu.Unlock()
	logTeleported("mob", to, by, "kind", m.Type)
}

// rotateMob turns a mob in place, head with body, and tells watchers with
// Entity Rotation plus Head Rotation. The record is marked told so a
// tracked client is not later sent a stale turn.
func (s *Server) rotateMob(ent *mobEntity, yaw, pitch float32) {
	var watchers []uuid.UUID
	var rot, head protocol.Buffer
	s.mobsMu.Lock()
	m := ent.mob
	m.Yaw, m.Pitch = yaw, clampBodyPitch(pitch)
	m.HeadYaw = yaw
	encYaw, encPitch, encHead := encodeAngle(m.Yaw), encodeAngle(m.Pitch), encodeAngle(m.HeadYaw)
	for id, was := range ent.tracked.all() {
		was.yaw, was.pitch, was.own.head = encYaw, encPitch, encHead
		watchers = append(watchers, id)
	}
	protocol.PutMoveEntityRot(&rot, m.ID, encYaw, encPitch, m.OnGround)
	head.PutVarInt(m.ID)
	head.PutU8(encHead)
	s.mobsMu.Unlock()
	for _, id := range watchers {
		s.mu.RLock()
		c := s.clients[id]
		s.mu.RUnlock()
		if c == nil || c.theWorld() != ent.w {
			continue
		}
		c.send(protocol.IDPlayEntityRotation, rot.Bytes())
		c.send(protocol.IDPlayHeadRotation, head.Bytes())
	}
}

// mobTwinAt builds the mob's continuation in another dimension at pos: a
// new body and entity ID with the same UUID, raised from the old one's tag
// so it keeps health, fleece, egg timer, fire, fall and the rest. The
// caller holds the mob lock or has taken the mob out of the herd.
// vanilla: Entity.teleportCrossDimension
func (s *Server) mobTwinAt(ent *mobEntity, to *dimension, pos player.Position) *mobEntity {
	twin := mobOfTag(s, to.w, mobTag(ent))
	if twin == nil {
		return nil
	}
	twin.mob.SetPos(pos.X, pos.Y, pos.Z)
	return twin
}

// portalTwinOfMob is mobTwinAt at a portal's arrival, the yaw turned by
// the angle between the portals.
func (s *Server) portalTwinOfMob(ent *mobEntity, to *dimension, at player.Position, turn float32) *mobEntity {
	twin := s.mobTwinAt(ent, to, at)
	if twin == nil {
		return nil
	}
	m := twin.mob
	if turn != 0 {
		m.Yaw += turn
		m.HeadYaw = m.Yaw
	}
	// The lead crosses as an unresolved lead, re-tied if a holder in the new
	// dimension claims it and dropped after the grace period. The old body
	// lets go, and a knot holding nothing else comes down.
	// vanilla: Entity.teleportCrossDimension, Leashable.restoreLeashFromSave
	if holder := ent.mob.RemoveLeash(); holder != 0 {
		m.CarryLeashAcross(holder)
		// The twin read the holder's name off the tag; it saves it while
		// that holder may still claim the lead.
		twin.leashOf = holder
		if knot := s.knotByEntityID(ent.w, holder); knot != nil {
			s.discardEmptiedKnot(knot)
		}
	}
	return twin
}

// finishMobTravel moves a mob and its riders through its portal: the
// arrival is found or built and its chunk woken so the new bodies save with
// it, and the riders' twins are seated on the mob's again. With nowhere to
// arrive, the stack returns to the herd.
// vanilla: Entity.teleportCrossDimension
func (s *Server) finishMobTravel(tr mobPortalTravel) {
	ent := tr.ent
	pos := player.Position{X: ent.mob.X, Y: ent.mob.Y, Z: ent.mob.Z}
	s.crossPortal(ent.w, ent.mob.ID, pos, tr.kind, func(to *dimension, spot world.SpawnPos, exitAxis [2]int32) func() {
		turn := portalTurnAt(ent.w, tr.kind, player.Position{X: ent.mob.X, Y: ent.mob.Y, Z: ent.mob.Z, Yaw: ent.mob.Yaw}, exitAxis)
		at := standingAt(spot)
		if tr.kind == rules.PortalNether {
			at = netherArrivalPosition(ent.w, to.w, pos, spot, ent.mob.BbWidth(), ent.mob.BbHeight(), true)
		}
		twin := s.portalTwinOfMob(ent, to, at, turn)
		if twin == nil {
			slog.Error("could not carry a mob across a portal", "kind", ent.mob.Type)
			return nil
		}
		stack := tr.twins(s, to, twin, []*mobEntity{twin})
		return func() {
			s.addMob(stack...)
			tr.carryPlayers(s, to)
			if len(stack) > 1 {
				s.broadcastWorld(ent.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload(riderIDs(tr.riders, nil)))
			}
			slog.Debug("mob travelled through a portal", "kind", ent.mob.Type, "to", to.name, "x", twin.mob.X, "y", twin.mob.Y, "z", twin.mob.Z)
		}
	}, func() {
		s.mobsMu.Lock()
		defer s.mobsMu.Unlock()
		tr.putBack(s.herdOf(ent.w))
	})
}

// riderIDs appends the entity IDs of riders and theirs to ids.
func riderIDs(riders []mobPortalTravel, ids []int32) []int32 {
	for _, r := range riders {
		ids = riderIDs(r.riders, append(ids, r.ent.mob.ID))
	}
	return ids
}

// seated reports whether the mob rides a vehicle of any kind.
func (ent *mobEntity) seated() bool {
	return ent.vehicle != nil || ent.cart != nil || ent.mob.Vehicle != nil
}

// mobSettle is trackedBody.settle with the pairing or removal queued on
// sends, in order with the mob's other packets.
func mobSettle(v *trackedBody[*mobEntity, mobShown], c *client, x, z float64, radius int32, sends *[]mobSend) (*toldBody[mobShown], bool) {
	was, seen, packets := v.settlePackets(c, x, z, radius)
	for _, p := range packets {
		*sends = append(*sends, mobSend{c: c, w: v.w, id: p.id, data: p.data})
	}
	return was, seen
}

// mobMovement prepares head and seat inputs once for a mob's watcher pass.
type mobMovement struct {
	seated   bool
	head     uint8
	headRead bool
}

// move tells one watcher where the mob went: the velocity first once it
// drifted from the one this watcher was last told, then the tracker's move,
// then the head, which turns apart from the body. A passenger is told only
// its turns, as a move of its own would pull it off the seat the client
// draws it in; a steered horse's position is left to its rider's own client.
// was is the watcher's record, which settle has made.
// vanilla: ServerEntity.sendChanges
func (motion *mobMovement) move(v *trackedBody[*mobEntity, mobShown], was *toldBody[mobShown], c *client, mt moveTick, sends *[]mobSend) {
	ent, m := v.b, v.b.mob
	mt.rotationOnly = motion.seated
	if !motion.seated {
		dx, dy, dz := m.VX-was.own.vx, m.VY-was.own.vy, m.VZ-was.own.vz
		if diff := dx*dx + dy*dy + dz*dz; diff > motionThreshold || diff > 0 && m.VX == 0 && m.VY == 0 && m.VZ == 0 {
			*sends = append(*sends, mobSend{c: c, w: v.w, id: protocol.IDPlaySetEntityMotion,
				data: entityVelocityPayload(m.ID, m.VX, m.VY, m.VZ)})
			was.own.vx, was.own.vy, was.own.vz = m.VX, m.VY, m.VZ
		}
	}
	// The rider still needs motion updates when server travel clears an old
	// velocity. Position updates would fight the client's own steering.
	if ent.rider == c && m.Steered() {
		return
	}
	if p, ok := v.movePacket(was, mt); ok {
		*sends = append(*sends, mobSend{c: c, w: v.w, id: p.id, data: p.data})
	}
	if !motion.headRead {
		motion.head, motion.headRead = encodeAngle(m.HeadYaw), true
	}
	if motion.head != was.own.head {
		was.own.head = motion.head
		var head protocol.Buffer
		head.PutVarInt(m.ID)
		head.PutU8(was.own.head)
		*sends = append(*sends, mobSend{c: c, w: v.w, id: protocol.IDPlayHeadRotation, data: head.Bytes()})
	}
}

// mobEntityType is a mob kind's minecraft:entity_type ID, as Spawn Entity
// carries it.
func mobEntityType(kind entity.MobType) int32 {
	return int32(kind)
}

// mobSound selects a resolved hurt or death holder. The mob lock is held.
func mobSound(m *entity.Mob, hurt bool) protocol.SoundHolder {
	// vanilla: LivingEntity.hurt plays the death sound instead of the hurt one once isDeadOrDying
	if hurt && (m.TotemSaved() || m.DragonDying() || m.Health <= 0) {
		return protocol.SoundHolder{ID: -1}
	}
	sounds := m.Sounds()
	sound := sounds.Death
	if hurt {
		sound = sounds.Hurt
	}
	if sound != registry.SoundNone {
		return registeredSoundHolder(sound)
	}
	fallback := mobDamageSounds[m.Type]
	if hurt {
		return fallback.hurt
	}
	return fallback.death
}

// registeredSoundHolder maps SoundNone to the ID -1 sentinel voice senders suppress.
func registeredSoundHolder(sound registry.Sound) protocol.SoundHolder {
	return sound.Holder()
}

// These defaults fill only gaps in Mob.Sounds; state and variant overrides win.
// The inline definitions preserve the existing missing sound-set behavior.
const (
	giantDamageSound     = "minecraft:entity." + "giant."
	mooshroomDamageSound = "minecraft:entity.mooshroom."
)

var mobDamageSounds = [registry.EntityTypeCount]struct{ hurt, death protocol.SoundHolder }{
	entity.TypeChicken:   {registeredSoundHolder(registry.SoundEntityChickenHurt), registeredSoundHolder(registry.SoundEntityChickenDeath)},
	entity.TypeCow:       {registeredSoundHolder(registry.SoundEntityCowHurt), registeredSoundHolder(registry.SoundEntityCowDeath)},
	entity.TypePig:       {registeredSoundHolder(registry.SoundEntityPigHurt), registeredSoundHolder(registry.SoundEntityPigDeath)},
	entity.TypeSheep:     {registeredSoundHolder(registry.SoundEntitySheepHurt), registeredSoundHolder(registry.SoundEntitySheepDeath)},
	entity.TypeTurtle:    {registeredSoundHolder(registry.SoundEntityTurtleHurt), registeredSoundHolder(registry.SoundEntityTurtleDeath)},
	entity.TypeGiant:     {protocol.SoundHolder{Name: giantDamageSound + "hurt"}, protocol.SoundHolder{Name: giantDamageSound + "death"}},
	entity.TypeMooshroom: {protocol.SoundHolder{Name: mooshroomDamageSound + "hurt"}, protocol.SoundHolder{Name: mooshroomDamageSound + "death"}},
}

// mobSoundCategory is a kind's sound category: hostile for monsters,
// neutral for animals.
func mobSoundCategory(kind entity.MobType) int32 {
	if entity.Enemy(kind) || kind == entity.TypeEnderDragon { // vanilla: EnderDragon.getSoundSource
		return protocol.SoundCategoryHostile
	}
	return protocol.SoundCategoryNeutral
}

// mobVoiceSend queues a resolved voice at the mob's position.
func mobVoiceSend(w *world.World, m *entity.Mob, sound protocol.SoundHolder, volume, pitch float32) (mobSend, bool) {
	return mobVoiceSendAt(w, m, m.X, m.Y, m.Z, sound, volume, pitch)
}

func mobVoiceSendAt(w *world.World, m *entity.Mob, x, y, z float64, sound protocol.SoundHolder, volume, pitch float32) (mobSend, bool) {
	if sound.ID < 0 && sound.Name == "" || m.Silent {
		return mobSend{}, false
	}
	send := mobVoicePacket(w, x, y, z, soundEffectHolderAt(sound, mobVoiceCategory(m), x, y, z, volume, pitch), volume)
	send.radius = soundHolderRange(sound, volume)
	return send, true
}

func mobVoiceIDSend(w *world.World, m *entity.Mob, sound registry.Sound, volume, pitch float32) (mobSend, bool) {
	if sound == registry.SoundNone || m.Silent {
		return mobSend{}, false
	}
	return mobVoicePacket(w, m.X, m.Y, m.Z, soundEffectIDAt(sound, mobVoiceCategory(m), m.X, m.Y, m.Z, volume, pitch), volume), true
}

func mobVoiceCategory(m *entity.Mob) int32 {
	category := mobSoundCategory(m.Type)
	if m.KillerBunny() {
		// vanilla: Rabbit.getSoundSource
		category = protocol.SoundCategoryHostile
	}
	return category
}

func mobVoicePacket(w *world.World, x, y, z float64, data []byte, volume float32) mobSend {
	return mobSend{
		w: w, id: protocol.IDPlaySoundEffect,
		data: data,
		near: true, x: x, y: y, z: z, radius: soundRange(volume),
	}
}

// mobHurtTold is a mob's causeless hurt: the damage event and the hurt
// sound. The hurt animation is for players only.
func mobHurtTold(w *world.World, m *entity.Mob, damageType registry.DamageType) []mobSend {
	return mobHurtToldBy(w, m, damageType, 0, 0, nil)
}

// mobHurtToldBy is mobHurtTold with the Damage Event's cause and direct IDs
// (entity ID + 1, zero for none) and optional source position.
func mobHurtToldBy(w *world.World, m *entity.Mob, damageType registry.DamageType, cause, direct int32, at *[3]float64) []mobSend {
	sends := []mobSend{{w: w, id: protocol.IDPlayDamageEvent,
		data: damageEventPayloadAt(m.ID, damageType, cause, direct, at)}}
	if send, ok := mobVoiceSend(w, m, mobSound(m, true), m.SoundVolume(), m.VoicePitch()); ok {
		sends = append(sends, send)
	}
	return sends
}

// phantomBiteEvent is the level event of a phantom's bite.
const phantomBiteEvent = 1039

// batTakeoffEvent is the level event of a hanging bat waking.
const batTakeoffEvent = 1025

// inkCloud is a hurt squid's ink: the squirt sound and the ink particles.
// vanilla: Squid.spawnInk
type inkCloud struct {
	sound         registry.Sound
	particle      int32
	x, y, z       float64
	volume, pitch float32
	silent        bool
}

// mobInk is an ink cloud with its world and kind, for hurts (blast, fall,
// fire) whose telling carries no ink of its own.
type mobInk struct {
	w     *world.World
	kind  entity.MobType
	cloud *inkCloud
}

// takeInk reads and clears the Squirt flag a landed hurt set on a squid;
// nil for any other body.
func takeInk(m *entity.Mob) *inkCloud {
	if !m.Squirt {
		return nil
	}
	m.Squirt = false
	cloud := &inkCloud{
		sound:    m.Sounds().Squirt,
		particle: protocol.ParticleSquidInk,
		x:        m.X,
		y:        m.Y + 0.5,
		z:        m.Z,
		volume:   m.SoundVolume(),
		pitch:    m.VoicePitch(),
		silent:   m.Silent,
	}
	if m.Type == entity.TypeGlowSquid {
		cloud.particle = protocol.ParticleGlowSquidInk
	}
	return cloud
}

// sendInk plays the squirt sound and thirty ink particles. Vanilla sends
// thirty packets, each around the body's tilt; one shared-form packet here
// scatters them over the box the directions span, at their drawn speed.
func (s *Server) sendInk(w *world.World, kind entity.MobType, cloud *inkCloud) {
	if cloud == nil {
		return
	}
	s.broadcastVoiceID(w, kind, cloud.silent, cloud.x, cloud.y, cloud.z, cloud.sound, cloud.volume, cloud.pitch)
	s.sendParticles(w, cloud.particle, cloud.x, cloud.y, cloud.z, 0.3, 0.3, 0.3, 0.1, 30)
}

// sendMobInk tells the ink of a hurt the mob tick read and cleared.
func (s *Server) sendMobInk(ink mobInk) {
	s.sendInk(ink.w, ink.kind, ink.cloud)
}

// blockFallSound is the fall sound of the block 0.2 below the feet, with
// its sound type's volume and pitch; false over air.
// vanilla: LivingEntity.playBlockFallSound
func blockFallSound(w *world.World, x, y, z float64) (registry.Sound, float32, float32, bool) {
	state := w.BlockAt(rules.BlockOf(x), rules.BlockOf(y-0.2), rules.BlockOf(z))
	if world.IsAirState(state) {
		return registry.SoundNone, 0, 0, false
	}
	return rules.BlockFallSound(registry.BlockOf(state))
}

// mobDisplayName is a kind's en_us type name, as in death messages.
func mobDisplayName(kind entity.MobType) string {
	if name, ok := entity.MobName(kind); ok {
		return name
	}
	return "Mob"
}

// mobName is the mob's custom name where it has one, else its type name.
func mobName(ent *mobEntity) string {
	if _, plain, ok := mobCustomName(ent); ok {
		return plain
	}
	return mobTypeEnglishName(ent.mob)
}

// mobKiller is a mob's display name and held item for a death message.
// vanilla: Entity.getDisplayName, DamageSource.getLocalizedDeathMessage
func (s *Server) mobKiller(ent *mobEntity) *rules.Killer {
	return &rules.Killer{Name: s.mobKillerName(ent), Weapon: ent.mob.MainHand}
}

// mobKillerName formats a mob's name for its team, with entity hover and insertion.
func (s *Server) mobKillerName(ent *mobEntity) protocol.Text {
	name := mobBaseName(ent)
	return entityDisplayName(s.scoreboard, ent.mob.Type.Name(), ent.uuid, &name)
}

// mobBaseName is the custom name or translated type name.
func mobBaseName(ent *mobEntity) protocol.Text {
	if text, _, ok := mobCustomName(ent); ok {
		return text
	}
	return mobTypeName(ent.mob)
}

// mobTypeName is the mob's type name, including a villager's profession.
// vanilla: Entity.getTypeName, Villager.getTypeName
func mobTypeName(m *entity.Mob) protocol.Text {
	if m.Type == entity.TypeVillager {
		return protocol.Text{Translate: "entity.minecraft.villager." + strings.TrimPrefix(m.Villager.Profession, "minecraft:")}
	}
	return protocol.Text{Translate: "entity.minecraft." + strings.TrimPrefix(m.Type.Name(), "minecraft:")}
}

// mobTypeEnglishName is the mob's en_us type name, matched by a selector's name
// option.
func mobTypeEnglishName(m *entity.Mob) string {
	if m.Type == entity.TypeVillager {
		return mobTypeName(m).Plain()
	}
	if name, ok := entity.MobName(m.Type); ok {
		return name
	}
	return mobTypeName(m).Translate
}

// cubeSound is a cube mob's hop or landing sound, small for the smallest
// slime (Slime.getJumpSound, getSquishSound; the magma cube's hop has no
// small one).
func cubeSound(m *entity.Mob, jump bool) registry.Sound {
	sounds := cubeMovementSounds[m.Type]
	if jump {
		if m.Size <= 1 {
			return sounds.smallJump
		}
		return sounds.jump
	}
	if m.Size <= 1 {
		return sounds.smallSquish
	}
	// vanilla: SulfurCube.getSquishSound
	if m.SulfurCubeFilled() {
		return registry.SoundEntitySulfurCubeBounce
	}
	return sounds.squish
}

var cubeMovementSounds = [registry.EntityTypeCount]struct{ jump, smallJump, squish, smallSquish registry.Sound }{
	entity.TypeSlime:      {registry.SoundEntitySlimeJump, registry.SoundEntitySlimeJumpSmall, registry.SoundEntitySlimeSquish, registry.SoundEntitySlimeSquishSmall},
	entity.TypeMagmaCube:  {registry.SoundEntityMagmaCubeJump, registry.SoundEntityMagmaCubeJump, registry.SoundEntityMagmaCubeSquish, registry.SoundEntityMagmaCubeSquishSmall},
	entity.TypeSulfurCube: {registry.SoundEntitySulfurCubeJump, registry.SoundEntitySmallSulfurCubeJump, registry.SoundEntitySulfurCubeSquish, registry.SoundEntitySmallSulfurCubeSquish},
}

// spawnMobPayload builds a mob's Spawn Entity packet, with its own
// velocity.
func spawnMobPayload(ent *mobEntity) []byte {
	m := ent.mob
	var data int32
	if m.Type == entity.TypeWarden && m.WardenPose() == protocol.PoseEmerging {
		data = 1
	}
	return spawnEntityPayload(spawnEntity{
		id: m.ID, uuid: ent.uuid, typ: mobEntityType(m.Type), data: data,
		x: m.X, y: m.Y, z: m.Z,
		vx: m.VX, vy: m.VY, vz: m.VZ,
		pitch:   encodeAngle(m.Pitch),
		yaw:     encodeAngle(m.Yaw),
		headYaw: encodeAngle(m.HeadYaw),
	})
}

// dyingMetadataPayload is a just-dead body's metadata: dying pose, zero
// health.
func dyingMetadataPayload(id int32) []byte {
	var b protocol.Buffer
	b.PutVarInt(id)
	b.PutMetadata(protocol.MetadataIndexPose, protocol.MetadataPose)
	b.PutVarInt(protocol.PoseDying)
	b.PutMetadata(protocol.MetadataIndexLivingHealth, protocol.MetadataFloat)
	b.PutF32(0)
	b.PutU8(protocol.MetadataEnd)
	return b.Bytes()
}

// mobHealthPayload is a metadata update of only the health.
func mobHealthPayload(ent *mobEntity) []byte {
	var b protocol.Buffer
	b.PutVarInt(ent.mob.ID)
	b.PutMetadata(protocol.MetadataIndexLivingHealth, protocol.MetadataFloat)
	b.PutF32(float32(ent.mob.Health))
	b.EndMetadata()
	return b.Bytes()
}

// sheepColourByte is the sheep's metadata byte: dye ID in the low nibble,
// sheared in bit 0x10.
func sheepColourByte(m *entity.Mob) uint8 {
	var colour uint8
	for dye, name := range registry.DyeNames {
		if m.Colour == name {
			colour = uint8(dye)
			break
		}
	}
	if m.Sheared {
		colour |= 0x10
	}
	return colour
}

// sheepColourTell builds the colour update and advances the tracking
// baselines so the next mob tick does not resend it. The caller holds the
// mob lock.
func sheepColourTell(ent *mobEntity) []byte {
	m := ent.mob
	colour := sheepColourByte(m)
	var b protocol.Buffer
	b.PutVarInt(m.ID)
	b.PutMetadata(protocol.MetadataIndexSheepColour, protocol.MetadataByte)
	b.PutU8(colour)
	b.EndMetadata()
	ent.updateShownMetadata(func(state *mobMetadataState) { state.sheepColour = colour })
	return b.Bytes()
}

// mooshroomVariantID is the mooshroom's variant VarInt: brown 1, red 0.
// vanilla: MushroomCow.Variant.RED and BROWN
func mooshroomVariantID(m *entity.Mob) int32 {
	if m.Mushroom == "brown" {
		return 1
	}
	return 0
}

// mobEquipmentPayload builds Set Equipment for every non-empty slot, nil
// when all are empty.
func mobEquipmentPayload(ent *mobEntity) []byte {
	m := ent.mob
	slots := mobEquipmentNow(m)
	var used []int
	for slot, stack := range slots {
		if !stack.Empty() {
			used = append(used, slot)
		}
	}
	if len(used) == 0 {
		return nil
	}
	return equipmentEntriesPayload(m.ID, &slots, used)
}

// tickMobCompasses ticks a mob's compasses; the tick's equipment diff tells
// watchers of a cleared target. vanilla: EntityEquipment.tick
func tickMobCompasses(m *entity.Mob, w *world.World, dim string) {
	bottom, top := w.MinY(), w.MinY()+w.Height()
	if cleared, ok := rules.ClearLostLodestone(w, m.MainHand, dim, bottom, top); ok {
		m.SetEquipment(protocol.EquipMainHand, cleared)
	}
	if cleared, ok := rules.ClearLostLodestone(w, m.OffHand, dim, bottom, top); ok {
		m.SetEquipment(protocol.EquipOffHand, cleared)
	}
	for i := range m.Worn {
		if cleared, ok := rules.ClearLostLodestone(w, m.Worn[i], dim, bottom, top); ok {
			m.SetEquipment(protocol.EquipFeet+i, cleared)
		}
	}
}

// mobEquipmentNow is a mob's equipment in wire-slot order: main hand, off
// hand, feet, legs, chest, head, body.
func mobEquipmentNow(m *entity.Mob) [7]protocol.ItemStack {
	var slots [7]protocol.ItemStack
	slots[0], slots[1] = m.MainHand, m.OffHand
	copy(slots[2:], m.Worn[:])
	slots[6] = m.Body
	return slots
}

// mobEquipmentSame compares live slots in mobEquipmentNow's wire-slot order,
// without copying the whole equipment snapshot.
func mobEquipmentSame(seen *[7]protocol.ItemStack, m *entity.Mob) bool {
	for i := range seen {
		live := m.EquipmentSlot(i)
		if seen[i].ItemID == 0 && live.ItemID == 0 && seen[i].Count == live.Count {
			continue // two bare empty slots; components only ride on an item
		}
		if !seen[i].Equal(live) {
			return false
		}
	}
	return true
}

// equipmentEntriesPayload writes Set Equipment for the used slots, the
// continuation bit on all but the last.
func equipmentEntriesPayload(id int32, slots *[7]protocol.ItemStack, used []int) []byte {
	var b protocol.Buffer
	b.PutVarInt(id)
	for i, slot := range used {
		if i < len(used)-1 {
			b.PutU8(uint8(slot) | 0x80)
		} else {
			b.PutU8(uint8(slot))
		}
		b.PutSlot(slots[slot])
	}
	return b.Bytes()
}

// setMobSlot sets the mob's stack in a mobEquipmentNow slot and sends Set
// Equipment to watchers, updating their records. An emptied slot is still
// sent, since the spawn payload omits empty slots.
func (s *Server) setMobSlot(ent *mobEntity, wire int, stack protocol.ItemStack) {
	s.updateMobSlot(ent, wire, func(protocol.ItemStack) (protocol.ItemStack, bool) {
		return stack, true
	})
}

// updateMobSlot runs update on a mobEquipmentNow slot's stack under one
// hold of the mob lock and, if asked, stores the result and tells watchers.
func (s *Server) updateMobSlot(ent *mobEntity, wire int, update func(protocol.ItemStack) (protocol.ItemStack, bool)) {
	s.mobsMu.Lock()
	stack, write := update(ent.mob.EquipmentSlot(wire))
	if !write {
		s.mobsMu.Unlock()
		return
	}
	before := mobEquipmentNow(ent.mob)
	ent.mob.SetEquipment(wire, stack)
	equipped := mobEquipmentNow(ent.mob)
	used := equipmentChanged(&before, &equipped)
	var payload []byte
	if len(used) > 0 {
		payload = equipmentEntriesPayload(ent.mob.ID, &equipped, used)
	}
	var watchers []uuid.UUID
	for id, tracked := range ent.tracked.all() {
		tracked.own.worn = equipped
		tracked.own.wornGen = 0
		watchers = append(watchers, id)
	}
	s.mobsMu.Unlock()
	if payload == nil {
		return
	}
	// Look up watcher clients after releasing the mob lock: the player list
	// lock stays outside it, as in the tick.
	for _, id := range watchers {
		s.mu.RLock()
		c := s.clients[id]
		s.mu.RUnlock()
		if c == nil || c.theWorld() != ent.w {
			continue
		}
		c.send(protocol.IDPlayEntityEquipment, payload)
	}
}

// equipmentChanged returns the slots whose whole stack changed, wear
// included.
func equipmentChanged(was, now *[7]protocol.ItemStack) []int {
	if equipmentSame(was, now) {
		return nil
	}
	var used []int
	for slot := range now {
		if !now[slot].Equal(was[slot]) {
			used = append(used, slot)
		}
	}
	return used
}

// equipmentSame reports whether every slot is Equal, without copying the
// stacks.
func equipmentSame(a, b *[7]protocol.ItemStack) bool {
	for i := range a {
		if a[i].ItemID == 0 && b[i].ItemID == 0 && a[i].Count == b[i].Count {
			continue // two bare empty slots; components only ride on an item
		}
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}

// mobAttributesPayload builds Update Attributes for a mob's syncable
// attributes: movement speed (the client's leg animation) and anything a
// command or saved file changed. A mob the map is not installed on gets a
// throwaway pairing, dropped after the send.
// vanilla: ServerEntity.sendPairingData
func mobAttributesPayload(ent *mobEntity) []byte {
	m := ent.mob
	a := m.Attributes()
	if a == nil {
		a = m.BuildAttributes(savedAttributes(ent.saved))
	} else {
		m.SyncSpeedModifiers(a)
	}
	return attributesPayload(m.ID, a.Syncable())
}

// savedAttributes is the attributes list a mob's saved compound carries,
// empty where the save wrote none.
func savedAttributes(saved nbt.Compound) nbt.List {
	if list, ok := nbt.Get[nbt.List](saved, "attributes"); ok {
		return list
	}
	return nbt.List{}
}

// lastHurtByMemory is how many ticks a player's last attacker is
// remembered. vanilla: LivingEntity.tick
const lastHurtByMemory = 100

// herdOf is the herd of w, which is always one of the server's three
// dimensions.
func (s *Server) herdOf(w *world.World) *mobHerd {
	return s.dimensionOfWorld(w).mobs
}

// attackMob is a player's hit on a mob: damage from the players' attack
// rule through the mob's TakeDamage (invulnerability window, health,
// knockback). A blow that only landed its excess through the window is
// told by health alone, without damage event, flinch, tilt, shove or hurt
// sound, but the attacker's feedback (sound, wear, exhaustion) still
// applies. The swing is spent.
// vanilla: Player.attack, onAttack
func (c *client) attackMob(target int32) {
	ent := c.srv.mobByEntityID(c.theWorld(), target)
	if ent == nil {
		return
	}
	// Out of reach spends nothing.
	minX, minY, minZ, maxX, maxY, maxZ := c.srv.mobAttackBox(ent, target)
	if !c.withinAttackRange(minX, minY, minZ, maxX, maxY, maxZ) {
		return
	}
	held := c.attackWeapon()
	blow, ok := c.prepareAttack(held, mobEntityType(ent.mob.Type), true, func() *rules.LootEntity {
		return c.srv.mobAttackLoot(ent)
	})
	if !ok {
		return
	}
	attacker := entity.Player{
		X: c.player.Position().X, Y: c.player.Position().Y, Z: c.player.Position().Z,
		Yaw:      float64(c.player.Position().Yaw),
		EntityID: c.player.EntityID,
		Huntable: true,
		Creative: c.player.GameMode() == protocol.GameModeCreative,
	}
	attacker = c.sized(attacker)

	c.srv.mobsMu.Lock()
	healthBefore := ent.mob.Health
	// The spikes are read before the blow lands. vanilla: Guardian.hurtServer
	var pricks *rules.Killer
	if ent.mob.ThornsOut() {
		pricks = c.srv.mobKiller(ent)
	}
	extra := rules.AttackKnockback(rules.EnchantedKnockback(held, &blow.context, c.attackKnockbackBase()), blow.feedback.Knockback)
	oldVX, oldVY, oldVZ, oldImpulse := ent.mob.VX, ent.mob.VY, ent.mob.VZ, ent.mob.Impulse
	hurt, ink, laid, softened := landMobHurt(ent, blow.damage, attacker, extra, held, blow.context, nil, int(target-ent.mob.ID-1))
	kb := c.srv.holdMobKnockback(ent, c.player.UUID, oldVX, oldVY, oldVZ, oldImpulse)
	var thorns mobThorns
	if laid {
		thorns = mobRetaliations(ent, &blow.context, held)
	}
	// Damage indicator origin, read while the tick cannot move the mob.
	mx, my, mz := ent.mob.X, ent.mob.Y, ent.mob.Z
	onGround := ent.mob.OnGround
	kind := ent.mob.Type
	_, lowY, _, _, highY, _ := ent.mob.BlastBox()
	dealt := healthBefore - ent.mob.Health
	resisted, absorbed := ent.mob.BlowResisted, ent.mob.BlowAbsorbed
	c.srv.mobsMu.Unlock()
	if kb != nil {
		vx, vy, vz := c.srv.applyMobKnockback(kb)
		if hurt != nil {
			hurt.vx, hurt.vy, hurt.vz = vx, vy, vz
		}
	}
	if pricks != nil {
		c.srv.guardianThorns(c, ent.mob.ID, mx, my, mz, pricks)
	}
	if !laid {
		c.attackSound(soundAttackNoDamage)
		return
	}
	c.awardDamageStat(registry.CustomStatDamageDealtResisted, resisted)
	c.awardDamageStat(registry.CustomStatDamageDealtAbsorbed, absorbed)
	c.noteHurtMob(ent.mob.ID)
	self := c.LootEntity
	c.hurtEntityAdvancements(advancement.Entity{Type: kind.Name(), Loot: c.srv.mobLoot(ent), Blow: &advancement.Blow{
		Dealt: blow.damage, Taken: dealt, Type: blow.context.DamageType, Attacker: self, Direct: self}})

	w := c.theWorld()
	if !softened {
		c.srv.broadcastMobHurt(w, hurt)
	} else {
		c.srv.sendInk(w, kind, ink)
	}
	if blow.feedback.Sweep {
		c.sweepHurt(w, held, blow.baseDamage, ent.mob.ID, minX, minY, minZ, maxX, maxY, maxZ, blow.strength)
	}
	c.landedBlowFeedback(blow.feedback, ent.mob.ID)
	if blow.smash {
		c.landSmash(w, blow.fall, smashVictim{id: ent.mob.ID, x: mx, y: my, z: mz, onGround: onGround,
			halfWidth: (maxX - minX) / 2, damage: blow.damage})
	}
	c.thornsFromMob(w, thorns, syncVelocityNow)
	c.resetSmashFall(blow.smash)
	c.awardWeaponHit(held)
	c.wearAttackWeapon(registry.StackDamagePerAttack(held))
	c.damageDealt(w, mx, (lowY+highY)/2, mz, dealt)
	c.exhaust(rules.AttackExhaustion)
	c.endSprintAfterKnockback(extra)
}

// mobHurtYaw is the attacker's bearing from the mob, for Hurt Animation.
func mobHurtYaw(attacker entity.Player, m *entity.Mob) float32 {
	return float32(math.Atan2(-(attacker.X-m.X), attacker.Z-m.Z) * 180 / math.Pi)
}

// eggVariantOfItem is the chick variant an egg item hatches, "" for a
// non-egg.
func eggVariantOfItem(item registry.Item) string {
	switch item {
	case registry.ItemEgg:
		return "minecraft:temperate"
	case registry.ItemBrownEgg:
		return "minecraft:warm"
	case registry.ItemBlueEgg:
		return "minecraft:cold"
	}
	return ""
}

// spawnEgg drops a laid egg as an item entity at the chicken, with the
// pop sound pitched 1.0 +/- 0.2 within earshot.
// vanilla: Chicken.aiStep
func (s *Server) spawnEgg(egg mobEgg) {
	for _, stack := range egg.stacks {
		it := s.newItemAtFeet(egg.w, stack, egg.x, egg.y, egg.z)
		s.registerFreshItem(it)
		s.askMobDrop(egg.hen, it)
	}
	s.broadcastVoiceID(egg.w, entity.TypeChicken, egg.silent, egg.x, egg.y, egg.z,
		registry.SoundEntityChickenEgg, 1.0, egg.pitch)
	// A silent hen still posts it.
	s.gameEvent(egg.w, rules.GameEventEntityPlace, egg.x, egg.y, egg.z, rules.GameEventContext{Mob: egg.hen})
}

// turtleEgg is one egg block a turtle finished laying.
type turtleEgg struct {
	w       *world.World
	x, y, z int32
	eggs    int
}

// layTurtleEgg sets the egg block where the turtle dug.
// vanilla: Turtle.TurtleLayEggGoal.tick
func (s *Server) layTurtleEgg(egg turtleEgg) {
	state, ok := registry.WithInt(registry.BlockTurtleEgg.DefaultState(), registry.PropEggs, egg.eggs)
	if !ok {
		return
	}
	s.setBlockAndTell(egg.w, egg.x, egg.y, egg.z, state)
}

// scatterDrops drops a dead mob's loot at its feet, each with a fresh
// item velocity. vanilla: Entity.spawnAtLocation, no offset
func (s *Server) scatterDrops(drops mobDrops) {
	for _, stack := range drops.stacks {
		it := s.newItemAtFeet(drops.w, stack, drops.x, drops.y, drops.z)
		s.registerFreshItem(it)
		s.askMobDrop(drops.by, it)
	}
	for _, stack := range drops.lasting {
		vx, vy, vz := entity.PopVelocity(s.popRng)
		it := s.newItemEntity(drops.w, stack, drops.x, drops.y, drops.z, vx, vy, vz, entity.BreakPickupDelay)
		it.item.Age = entity.ItemExtendedAge
		s.registerFreshItem(it)
	}
}

// spawnMobXp cuts a dead mob's experience into orbs at its place.
func (s *Server) spawnMobXp(drop mobXpDrop) {
	s.spawnOrbs(drop.w, drop.x, drop.y, drop.z, drop.amount)
}

// handleInteract handles serverbound Interact (right-clicking an entity).
// Attacks arrive as the separate Attack packet in this version, so this is
// only ever a use.
func (c *client) handleInteract(r *protocol.Reader) error {
	c.useSwing = swingNone
	target := r.ReadVarInt()
	hand := r.ReadVarInt()
	// The click's offset on the body picks an armor stand's slot.
	ox, oy, oz := r.ReadLpVec3()
	sneaking := r.ReadBool() // the sneak key, which the boat the target is reads
	if err := r.Err(); err != nil {
		return c.decodeFailure("interact", err)
	}
	hand = wireHand(hand)
	defer c.swingForUse(hand)
	// Vanilla ignores Interact while the client is still loading; the packet
	// is decoded first.
	if !c.clientLoaded() {
		return nil
	}
	if c.player.Vitals().Dead {
		return nil
	}
	if c.targetOutsideBorder(target) {
		return nil
	}
	if !c.pluginInteractEntity(target, hand, float64(ox), float64(oy), float64(oz)) {
		return nil
	}
	// Boats take the click before mobs, then frames.
	// vanilla: ServerGamePacketListenerImpl.handleInteract, ItemFrame.interact
	if it := c.srv.boatByEntityID(c.theWorld(), target); it != nil {
		return c.useOnBoat(it, hand, sneaking)
	}
	// Minecarts come next, with their own reach; spectators reach them too,
	// for the contents.
	// vanilla: Minecart.interact, MinecartChest.interact, Player.interactOn
	if it := c.srv.minecartByEntityID(c.theWorld(), target); it != nil {
		return c.useOnMinecart(it, hand, sneaking)
	}
	if c.player.GameMode() == protocol.GameModeSpectator {
		return nil
	}
	if ent := c.srv.fixtureByEntityID(c.theWorld(), target, entity.FixtureCushion); ent != nil {
		return c.useOnCushion(ent, sneaking)
	}
	if c.srv.frameByEntityID(c.theWorld(), target) != nil {
		return c.useItemFrame(target, hand)
	}
	// Then leash knots: a use moves the knot's held mobs.
	if knot := c.srv.knotByEntityID(c.theWorld(), target); knot != nil {
		return c.useKnot(knot, hand, sneaking)
	}
	if c.useInteraction(target, hand) {
		return nil
	}
	// Then armor stands, where the offset picks the swapped slot.
	if handled, err := c.useOnArmorStand(target, hand, ox, oy, oz); handled {
		return err
	}
	if c.nameBody(target, hand) {
		return nil
	}
	return c.useOnMob(target, hand, sneaking)
}

// nameBody names an armor stand or a mannequin with the held name tag,
// reporting whether the target was one and took it.
// vanilla: NameTagItem.interactLivingEntity
func (c *client) nameBody(target int32, hand int32) bool {
	held := c.heldItem(hand)
	if registry.Item(held.ItemID) != registry.ItemNameTag || held.CustomName == nil {
		return false
	}
	if !c.nameArmorStand(target, held) && !c.nameMannequin(target, held) {
		return false
	}
	c.useSwing = swingPredicted
	if c.player.GameMode() != protocol.GameModeCreative {
		_ = c.consumeHeld(hand)
	}
	return true
}

// useOnMob gives the mob its click, then the held item if the mob passed.
func (c *client) useOnMob(target int32, hand int32, sneaking bool) error {
	ent := c.srv.mobByEntityID(c.theWorld(), target)
	if ent == nil {
		return nil
	}
	// Vanilla's handleInteract checks the box against entity interaction range
	// plus buffer before reading the use.
	pos := c.player.Position()
	minX, minY, minZ, maxX, maxY, maxZ := c.srv.mobAttackBox(ent, target)
	if !rules.WithinEntityInteractionRange(c.entityRange(),
		pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z,
		minX, minY, minZ, maxX, maxY, maxZ) {
		return nil
	}
	if c.spawnEggOnMob(ent, hand) {
		return nil
	}
	held := c.heldItem(hand)
	if consumed, err := c.mobInteract(ent, hand, held, sneaking); consumed || err != nil {
		return err
	}
	// The equippable component gets its turn before the item's own interaction.
	// vanilla: Player.interactOn, ItemStack.interactLivingEntity
	if c.equipOnInteract(ent, hand, held) {
		return nil
	}
	if ent.mob.Type == entity.TypeSheep {
		if _, ok := rules.DyeItemColour(held); ok {
			return c.dyeSheep(ent, hand)
		}
	}
	return nil
}

// mobInteract reports whether the mob consumed the click, before the held item's turn.
func (c *client) mobInteract(ent *mobEntity, hand int32, held protocol.ItemStack, sneaking bool) (consumed bool, err error) {
	// Handled uses default to SUCCESS; CONSUME and SUCCESS_SERVER branches override it.
	c.useSwing = swingPredicted
	defer func() {
		if !consumed {
			c.useSwing = swingNone
		}
	}()
	item := registry.Item(held.ItemID)
	// Read lead ownership and shearability (sheared, age, dead) under the mob
	// lock, so the click cannot race the tick's drop and untie a gone lead.
	s := c.srv
	s.mobsMu.Lock()
	dead := ent.mob.Dead
	if entity.IsNautilus(ent.mob.Type) {
		ent.mob.Persistence = true
	}
	baby := ent.mob.IsBaby()
	filledCube := ent.mob.SulfurCubeFilled()
	primedCube := ent.mob.SulfurPrimed()
	leashedToPlayer := ent.mob.LeashHolderID == c.player.EntityID
	shearable := ent.mob.Shearable()
	leashable := entity.CanBeLeashed(ent.mob)
	s.mobsMu.Unlock()
	// A dead body not yet removed ignores the click. vanilla: Mob.interact
	if dead {
		c.useSwing = swingNone
		return true, nil
	}
	// A name tag skips the untie only when used: an unnamed tag falls through
	// to the leash branches (vanilla: checkAndHandleImportantInteractions).
	tagUsed := item == registry.ItemNameTag && held.CustomName != nil
	switch {
	// Vanilla handles name tags and spawn eggs before the kind's mobInteract
	// (checkAndHandleImportantInteractions); no item is both a name tag and an
	// igniter, so their order here is free. Lead cases come next, before milk
	// and fleece: a click by the holder unties (a used name tag excepted), and
	// a lead ties anything tieable.
	// vanilla: Mob.interact, Entity.interact
	case ent.mob.Type == entity.TypeCreeper && registry.Item(held.ItemID).InTag(registry.ItemTagIDCreeperIgniters):
		return true, c.igniteCreeper(ent, held, hand)
	// vanilla: Entity.interact moves the player's leads onto a sneak-clicked mob
	case sneaking && !tagUsed && !baby && leashable && c.tieLedOntoMob(ent, hand):
		return true, nil
	case item == registry.ItemShears && !tagUsed && c.shearLeashesOfMob(ent, hand):
		return true, nil
	case item == registry.ItemShears && !sneaking && c.shearEquipment(ent, hand, held):
		return true, nil
	case leashedToPlayer && !tagUsed:
		if handled, err := c.bindLeadToMob(ent, held, hand); handled {
			return true, err
		}
		return false, nil
	case item == registry.ItemLead && (ent.mob.Type != entity.TypeSulfurCube || filledCube) && ent.mob.Type != entity.TypeVillager && ent.mob.Type != entity.TypeWanderingTrader:
		// vanilla: AbstractVillager.canBeLeashed is false
		if handled, err := c.bindLeadToMob(ent, held, hand); handled {
			return true, err
		}
		return false, nil
	// vanilla: SulfurCube.mobInteract passes on a primed adult, then lights
	// an explosive one with its igniters before shears and swallowing.
	case ent.mob.Type == entity.TypeSulfurCube && !baby && primedCube && !tagUsed:
		return false, nil
	case ent.mob.Type == entity.TypeSulfurCube && !baby && (item == registry.ItemFlintAndSteel || item == registry.ItemFireCharge):
		return c.igniteSulfurCube(ent, hand, held)
	case ent.mob.Type == entity.TypeDolphin && !held.Empty() && item.InTag(registry.ItemTagIDFishes):
		return true, c.feedDolphin(ent, hand)
	case ent.mob.Type == entity.TypeIronGolem && item == registry.ItemIronIngot:
		return c.repairIronGolem(ent, hand)
	case ent.mob.Type == entity.TypeArmadillo && !baby && item == registry.ItemBrush:
		return true, c.brushArmadillo(ent, hand, held)
	case ent.mob.Type == entity.TypeCopperGolem && held.Empty():
		return true, c.takeFromCopperGolem(ent)
	case ent.mob.Type == entity.TypeCopperGolem && ent.mob.CopperAccepts(item):
		return true, c.useOnCopperGolem(ent, hand, held)
	case ent.mob.Type == entity.TypeSheep && !shearable && item == registry.ItemShears:
		c.useSwing = swingNone
		return true, nil
	case shearable && item == registry.ItemShears:
		return true, c.shearMob(ent, hand)
	case ent.mob.Type == entity.TypeSulfurCube && !baby && item.InTag(registry.ItemTagIDSulfurCubeSwallowable):
		return c.feedSulfurCube(ent, hand, held)
	case ent.mob.Type == entity.TypeSulfurCube && !baby && item == registry.ItemBucket:
		return true, c.bucketMob(ent, hand)
	case ent.mob.Type != entity.TypeSulfurCube && entity.MobBucketItem(ent.mob.Type) != "" && item == registry.ItemWaterBucket:
		return true, c.bucketMob(ent, hand)
	case ent.mob.Type == entity.TypeGoat && !baby && item == registry.ItemBucket:
		return true, c.milkGoat(ent, hand)
	case (ent.mob.Type == entity.TypeCow || ent.mob.Type == entity.TypeMooshroom) && !baby && item == registry.ItemBucket:
		// AbstractCow.mobInteract, the branch behind the mooshroom's own.
		return true, c.milkCow(ent, hand)
	case ent.mob.Type == entity.TypeMooshroom && !baby && item == registry.ItemBowl:
		return true, c.stewFromMooshroom(ent, hand)
	case tagUsed:
		return true, c.nameMob(ent, hand)
	case ent.mob.Type == entity.TypeZombieVillager && item == registry.ItemGoldenApple:
		return true, c.cureZombieVillager(ent, hand)
	case ent.mob.Type == entity.TypePiglin && item == registry.ItemGoldIngot:
		return c.offerGoldToPiglin(ent, hand, held)
	case item == registry.ItemGoldenDandelion && ent.mob.CanLockAge():
		return true, c.lockAge(ent, hand)
	case ent.mob.Type == entity.TypeVillager || ent.mob.Type == entity.TypeWanderingTrader:
		return c.useOnVillager(ent, hand)
	case entity.IsEquine(ent.mob.Type):
		return c.useOnHorse(ent, hand, held, sneaking)
	case entity.IsNautilus(ent.mob.Type):
		return c.useOnNautilus(ent, hand, held, sneaking)
	case ent.mob.IsStrider() || ent.mob.Type == entity.TypePig:
		return c.useOnStrider(ent, hand, held, sneaking)
	case ent.mob.Type == entity.TypeWolf:
		return c.useOnWolf(ent, hand, held, sneaking)
	case ent.mob.Type == entity.TypeCat:
		return c.useOnCat(ent, hand, held)
	case ent.mob.Type == entity.TypeOcelot:
		return c.useOnOcelot(ent, hand, held)
	case ent.mob.Type == entity.TypeParrot:
		return c.useOnParrot(ent, hand, held)
	case ent.mob.Type == entity.TypeAllay:
		return c.useOnAllay(ent, hand, held)
	case ent.mob.Type == entity.TypeHappyGhast:
		return c.useOnHappyGhast(ent, hand, held, sneaking)
	case ent.mob.Type == entity.TypeMooshroom && !baby && !ent.mob.FedBy(held):
		return c.flowerToMooshroom(ent, hand, item)
	case ent.mob.Type == entity.TypeBee && beeFlowerEffect(held):
		return true, c.feedBeeFlower(ent, hand, held)
	case ent.mob.Type == entity.TypePanda && c.srv.pandaScared(ent):
		return false, nil
	case ent.mob.Type == entity.TypePanda && c.srv.pandaStandUp(ent):
		c.useSwing = swingServer
		return true, nil
	case ent.mob.FedBy(held):
		return c.feedMob(ent, hand)
	}
	return false, nil
}

// igniteCreeper lights a creeper's fuse, plays the item's use sound to all
// but the clicker, and wears the flint and steel or spends the fire charge
// outside creative. vanilla: Creeper.ignite
func (c *client) igniteCreeper(ent *mobEntity, held protocol.ItemStack, hand int32) error {
	s := c.srv
	s.mobsMu.Lock()
	if ent.mob.Dead {
		s.mobsMu.Unlock()
		return nil
	}
	x, y, z := ent.mob.X, ent.mob.Y, ent.mob.Z
	ent.mob.Ignite()
	// vanilla: the use sound's pitch, getRandom().nextFloat() * 0.4 + 0.8
	pitch := ent.mob.Rand.Float32()*0.4 + 0.8
	s.mobsMu.Unlock()

	sound := registry.SoundItemFlintandsteelUse
	if registry.Item(held.ItemID) == registry.ItemFireCharge {
		sound = registry.SoundItemFirechargeUse
	}
	s.broadcastSoundIDExcept(ent.w, c,
		sound, mobSoundCategory(ent.mob.Type), x, y, z, 1.0, pitch)

	if c.player.GameMode() == protocol.GameModeCreative {
		return nil
	}
	if registry.StackMaxDamage(held) > 0 {
		// vanilla: hurtAndBreak(1) on an igniter that can wear
		c.wearHeld(hand, 1)
		return nil
	}
	// vanilla: shrink(1) on one that cannot
	return c.consumeHeld(hand)
}

// shearMob shears a sheep, bogged, snow golem or mooshroom: the kind's shearing table
// is rolled before the shear, drops are spawned, the sound plays in the
// player's voice, and a mooshroom's cow spawns last.
func (c *client) shearMob(ent *mobEntity, hand int32) error {
	s := c.srv
	take, done := s.shearOffAsking(ent, c.heldItem(hand), c.pluginShear(ent))
	if !done {
		return nil
	}
	c.useSwing = swingPredicted
	if ent.mob.Type == entity.TypeSheep {
		c.useSwing = swingServer
	}
	s.tellShear(ent.w, take, protocol.SoundCategoryPlayer)
	s.gameEvent(ent.w, rules.GameEventShear, take.x, take.y, take.z, c.gameEventContext(0))
	if ent.mob.Type == entity.TypeSulfurCube && len(take.drops) > 0 {
		s.mobsMu.Lock()
		after := s.bodyInteraction(ent)
		s.mobsMu.Unlock()
		c.shearedEquipmentAdvancements(after, take.drops[0])
	}
	if ent.mob.Type == entity.TypeMooshroom {
		s.convertShearedMooshroom(ent)
	}
	c.wearHeld(hand, 1)
	return nil
}

// dyeSheep recolours a sheep with the held dye; where the dye would not
// take, nothing is spent or sent. The sound plays to all but the clicker
// (who plays its own), the colour byte goes out ahead of the mob tick with
// baselines advanced, and one dye is spent outside creative.
// vanilla: DyeItem.interactLivingEntity, Player.interactOn
func (c *client) dyeSheep(ent *mobEntity, hand int32) error {
	s := c.srv
	colour, _ := rules.DyeItemColour(c.heldItem(hand))
	colour, ok := c.sheepDyeAllowed(ent, colour)
	if !ok {
		return nil
	}
	s.mobsMu.Lock()
	m := ent.mob
	recoloured := m.Dye(colour)
	x, y, z := m.X, m.Y, m.Z
	var metadata []byte
	if recoloured {
		metadata = sheepColourTell(ent)
	}
	s.mobsMu.Unlock()
	if !recoloured {
		return nil
	}
	c.useSwing = swingPredicted
	s.broadcastEntitySoundID(ent.w, c, m.ID, registry.SoundItemDyeUse, protocol.SoundCategoryPlayer, x, y, z, 1.0, 1.0)
	s.broadcastWorld(ent.w, protocol.IDPlayEntityMetadata, metadata)
	if c.player.GameMode() == protocol.GameModeCreative {
		return nil
	}
	return c.consumeHeld(hand)
}

// convertShearedMooshroom replaces a sheared mooshroom with a cow as a
// SINGLE conversion (keepEquipment and preserveCanPickUpLoot false): name,
// position and leash carry over, loot pickup and gear do not, and the cow
// takes the boat seat. The alive check under the mob lock stops a body that
// died or despawned since the shear from leaving a cow.
// vanilla: MushroomCow.shear, ConversionParams.single(this, false, false)
func (s *Server) convertShearedMooshroom(ent *mobEntity) {
	if !s.transformAllowed(ent, entity.TypeCow, plugin.TransformSheared) {
		return
	}
	s.mobsMu.Lock()
	h := s.herdOf(ent.w)
	if h.book[ent.mob] != ent || ent.mob.Dead {
		s.mobsMu.Unlock()
		return
	}
	h.remove(ent.mob)
	h.forget(ent.mob)
	x, y, z := ent.mob.X, ent.mob.Y, ent.mob.Z
	holder := ent.mob.LeashHolderID
	s.mobsMu.Unlock()
	// Read off the board before the removal takes the old body off its team:
	// the tick's copy may be a tick old.
	team, _ := s.scoreboard.PlayerTeam(ent.uuid.String())
	s.scoreboard.EntityRemoved(ent.uuid.String())

	s.broadcastWorld(ent.w, protocol.IDPlayRemoveEntities,
		removeEntitiesPayload([]int32{ent.mob.ID}))
	id := s.nextEntity()
	m := entity.NewMob(entity.TypeCow, id, x, y, z, s.entitySeed(id))
	m.ConvertedFrom(ent.mob)
	// keepEquipment false only skips the copy: the old body is removed without
	// loot, so its gear is gone, not dropped.
	// vanilla: ConversionType.single's keepEquipment
	for slot := range 6 {
		m.SetEquipment(slot, protocol.ItemStack{})
	}
	m.ResetDropChances()
	m.LeashHolderID = holder
	cow := convertedEntity(ent, m, ent.w, false)
	cow.convertTeam = team
	s.addRiddenConversion(ent, cow)
}

// shearTake is a shearing's outcome, gathered under the mob lock for the
// telling after. A sheep's wool drops at its x and z, 1.0 above its feet
// (spawnAtLocation), with ItemEntity's toss; a mooshroom's mushrooms drop
// at its x and z, unscattered, pickup delay 0, with an EXPLOSION at its
// middle. vanilla: Sheep.shear, MushroomCow.shear
type shearTake struct {
	drops     []protocol.ItemStack
	sound     registry.Sound
	x, y, z   float64
	dropY     float64
	entityID  int32
	metadata  []byte
	mooshroom bool
	sheep     bool
	copper    bool
}

// shearOff, under the mob lock, rolls the kind's shearing table before
// marking sheared (vanilla: Sheep.shear's order, as the pools check an
// unshorn sheep) and returns the drops, sound and colour byte; done is
// false when there was nothing to shear.
func (s *Server) shearOff(ent *mobEntity, tool protocol.ItemStack) (take shearTake, done bool) {
	return s.shearOffAsking(ent, tool, nil)
}

// shearOffAsking is shearOff with a PlayerShearEntityEvent hook: ask sees the
// rolled drops, may replace them, and refuses the shear by returning false.
// It runs with the mob lock released and before the mob is marked sheared.
func (s *Server) shearOffAsking(ent *mobEntity, tool protocol.ItemStack, ask func(drops []protocol.ItemStack) ([]protocol.ItemStack, bool)) (take shearTake, done bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	m := ent.mob
	if !m.Shearable() {
		return shearTake{}, false
	}
	if m.Type == entity.TypeCopperGolem {
		// The antenna's item is the one drop, 1.5 above the feet; the golem is
		// not marked sheared. vanilla: CopperGolem.shear
		return shearTake{
			drops: []protocol.ItemStack{m.CopperShear()},
			x:     m.X, y: m.Y, z: m.Z,
			entityID: m.ID,
			sound:    registry.SoundEntityCopperGolemShear,
			dropY:    m.Y + 1.5,
			copper:   true,
		}, true
	}
	take = shearTake{
		drops: m.RollShear(tool, s.lootLevelAt(ent.w, m.X, m.Y, m.Z)),
		x:     m.X, y: m.Y, z: m.Z,
		entityID: m.ID,
		sound:    registry.SoundEntitySheepShear,
		dropY:    m.Y + 0.5,
	}
	if m.Type == entity.TypeSulfurCube {
		take.sound = registry.SoundEntitySulfurCubeEject
		take.dropY = m.Y + m.BbHeight()
	}
	if ask != nil {
		s.mobsMu.Unlock()
		drops, ok := ask(take.drops)
		s.mobsMu.Lock()
		if !ok || !m.Shearable() {
			return shearTake{}, false
		}
		take.drops = drops
	}
	// The mark is also the mooshroom's once-guard: nothing else stops a second
	// click between the shear and the conversion.
	if m.Type != entity.TypeSulfurCube {
		m.SetMetadataSheared(true)
	}
	// The bogged's mushrooms drop from its box top
	// (Bogged.spawnShearedMushrooms), the mooshroom's at getY(1.0).
	if m.Type == entity.TypeBogged {
		take.dropY, take.sound = m.Y+m.BbHeight(), registry.SoundEntityBoggedShear
	}
	if m.Type == entity.TypeSnowGolem {
		// vanilla: SnowGolem.shear drops at 1.7 above the feet; dropY is the box centre
		take.dropY, take.sound = m.Y+1.7, registry.SoundEntitySnowGolemShear
	}
	if m.Type == entity.TypeMooshroom {
		take.dropY, take.sound = m.Y+m.BbHeight(), registry.SoundEntityMooshroomShear
		take.mooshroom = true
	}
	// Sheep.shear drops wool at x, z, 1.0 above the feet; spawnItem takes the
	// box centre. The bogged's flag goes with the next mob tick, the sheep's
	// byte now, with the baselines it was told against.
	if m.Type == entity.TypeSheep {
		take.dropY, take.sheep = m.Y+1.0, true
		take.metadata = sheepColourTell(ent)
	}
	return take, true
}

// tellShear plays the shear sound from the sheared body in the source's
// category, drops the items, then sends the sheep's new colour.
// vanilla: Sheep.shear speaks before it lays the wool, and PlayerList.broadcast
// carries the sound only as far as its earshot.
func (s *Server) tellShear(w *world.World, take shearTake, category int32) {
	s.broadcastEntitySoundID(w, nil, take.entityID, take.sound, category, take.x, take.y, take.z, 1.0, 1.0)
	r := s.popRng
	switch {
	case take.copper:
		for _, stack := range take.drops {
			s.spawnItem(w, stack, take.x, take.dropY, take.z,
				(r.Float64()-0.5)*0.2, 0.2, (r.Float64()-0.5)*0.2, entity.BreakPickupDelay)
		}
	case take.mooshroom:
		// vanilla: MushroomCow.shear's sendParticles, then its ItemEntity
		// loop — the four-argument constructor's toss, no pickup delay.
		s.sendParticles(w, protocol.ParticleExplosion,
			take.x, (take.y+take.dropY)/2, take.z, 0, 0, 0, 0, 1)
		for _, stack := range take.drops {
			s.spawnItem(w, stack, take.x, take.dropY, take.z,
				(r.Float64()-0.5)*0.2, 0.2, (r.Float64()-0.5)*0.2, 0)
		}
	case take.sheep:
		// vanilla: Sheep.shear's spawnAtLocation(stack, 1.0) at the body's x and
		// z; ItemEntity.<init>'s toss plus Sheep.shear's own jitter.
		for _, stack := range take.drops {
			vx, vy, vz := entity.PopVelocity(r)
			s.spawnItem(w, stack, take.x, take.dropY, take.z,
				vx+(r.Float64()-r.Float64())*0.1,
				vy+r.Float64()*0.05,
				vz+(r.Float64()-r.Float64())*0.1,
				entity.BreakPickupDelay)
		}
	default:
		// vanilla: Bogged.spawnShearedMushrooms — spawnAtLocation at the
		// body's own x and z with ItemEntity.<init>'s toss.
		for _, stack := range take.drops {
			vx, vy, vz := entity.PopVelocity(r)
			s.spawnItem(w, stack, take.x, take.dropY, take.z, vx, vy, vz, entity.BreakPickupDelay)
		}
	}
	if take.metadata != nil {
		s.broadcastWorld(w, protocol.IDPlayEntityMetadata, take.metadata)
	}
}

// mobsIn returns the mobs crossing the box; a shear dispenser's sweep box
// is the target block's own (vanilla's getEntitiesOfClass).
func (s *Server) mobsIn(w *world.World, minX, minY, minZ, maxX, maxY, maxZ float64) []*mobEntity {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	var in []*mobEntity
	for m, ent := range s.herdOf(w).book {
		hw := m.BbWidth() / 2
		if m.X-hw < maxX && m.X+hw > minX &&
			m.Z-hw < maxZ && m.Z+hw > minZ &&
			m.Y < maxY && m.Y+m.BbHeight() > minY {
			in = append(in, ent)
		}
	}
	return in
}

// milkCow fills the held bucket from a grown cow and plays the milking
// sound to all but the milker. vanilla: AbstractCow.mobInteract
func (c *client) milkCow(ent *mobEntity, hand int32) error {
	return c.milkAnimal(ent, hand, registry.SoundEntityCowMilk)
}

// milkAnimal fills the held bucket from a grown animal, playing sound from
// the milker; a calf gives nothing.
func (c *client) milkAnimal(ent *mobEntity, hand int32, sound registry.Sound) error {
	// Read under the mob lock: the tick ages the animal.
	c.srv.mobsMu.Lock()
	baby := ent.mob.IsBaby()
	c.srv.mobsMu.Unlock()
	if baby {
		return nil
	}
	// The sound travels in the PLAYERS category from the milker.
	// vanilla: Player.playSound
	pos := c.player.Position()
	c.srv.broadcastSoundIDExcept(ent.w, c, sound, protocol.SoundCategoryPlayer, pos.X, pos.Y, pos.Z, 1.0, 1.0)
	c.filledResult(hand, protocol.ItemStack{ItemID: int32(registry.ItemMilkBucket), Count: 1}, true)
	return nil
}

// flowerToMooshroom stores a flower's effects or shows smoke when already full.
// vanilla: MushroomCow.mobInteract
func (c *client) flowerToMooshroom(ent *mobEntity, hand int32, item registry.Item) (bool, error) {
	s := c.srv
	s.mobsMu.Lock()
	m := ent.mob
	handled, consumed := m.FeedMooshroomFlower(item)
	x, y, z, silent := m.X, m.Y, m.Z, m.Silent
	particle, count := int32(protocol.ParticleSmoke), 2
	var data protocol.Buffer
	if consumed {
		particle = protocol.ParticleEffect
		count = 4
		// vanilla: SpellParticleOption.streamCodec (color, power)
		data.PutI32(-1)
		data.PutF32(1)
	}
	type puff struct {
		x, y, z float64
		vy      float32
	}
	var puffs []puff
	if handled {
		for range count {
			puffs = append(puffs, puff{x + m.Rand.Float64()/2, y + m.BbHeight()/2, z + m.Rand.Float64()/2, float32(m.Rand.Float64() / 5)})
		}
	}
	s.mobsMu.Unlock()
	if !handled {
		return false, nil
	}
	if consumed {
		if err := c.consumeHeldOutsideCreative(hand); err != nil {
			return true, err
		}
	}
	for _, p := range puffs {
		// Count zero gives the single particle the supplied velocity.
		s.sendParticlePayload(ent.w, particleDataPayload(particle, data.Bytes(), false, p.x, p.y, p.z, 0, p.vy, 0, 1, 0), p.x, p.y, p.z)
	}
	if consumed && !silent {
		s.broadcastSoundID(ent.w, registry.SoundEntityMooshroomEat, protocol.SoundCategoryNeutral, x, y, z, 2, 1)
	}
	return true, nil
}

func beeFlowerEffect(held protocol.ItemStack) bool {
	_, ok := registry.Item(held.ItemID).BeeInteractionEffect()
	return ok && registry.Item(held.ItemID).InTag(registry.ItemTagIDBeeFood)
}

// feedBeeFlower uses up the flower and gives the bee its effect.
// vanilla: Bee.mobInteract
func (c *client) feedBeeFlower(ent *mobEntity, hand int32, held protocol.ItemStack) error {
	s := c.srv
	be, _ := registry.Item(held.ItemID).BeeInteractionEffect()
	if err := c.consumeHeldOutsideCreative(hand); err != nil {
		return err
	}
	s.mobsMu.Lock()
	ch, added := ent.mob.AddEffect(effect.New(be.Effect, be.Duration, 0, false, true, true))
	var tell splashMobTell
	var ok bool
	if added {
		tell, ok = mobEffectTell(s.dimensionOfWorld(ent.w), ent.mob, []effect.Change{ch})
	}
	s.mobsMu.Unlock()
	if ok {
		s.tellSplashMob(ent.w, tell)
	}
	return nil
}

// stewFromMooshroom fills a bowl and clears any stored flower effects.
// vanilla: MushroomCow.mobInteract, Entity.playSound
func (c *client) stewFromMooshroom(ent *mobEntity, hand int32) error {
	// Read under the mob lock: the tick ages and moves it.
	c.srv.mobsMu.Lock()
	baby, silent := ent.mob.IsBaby(), ent.mob.Silent
	x, y, z := ent.mob.X, ent.mob.Y, ent.mob.Z
	effects := ent.mob.StewEffects
	if !baby {
		ent.mob.StewEffects = nil
	}
	c.srv.mobsMu.Unlock()
	if baby {
		return nil
	}
	stack := protocol.ItemStack{ItemID: int32(registry.ItemMushroomStew), Count: 1}
	sound := registry.SoundEntityMooshroomMilk
	if effects != nil {
		stack.ItemID = int32(registry.ItemSuspiciousStew)
		stack.SetSuspiciousStewEffects(effects)
		sound = registry.SoundEntityMooshroomSuspiciousMilk
	}
	c.filledResult(hand, stack, false)
	// The mooshroom plays from its body in its own category, unlike
	// AbstractCow's milking, which plays from the milker.
	if !silent {
		c.srv.broadcastSoundID(ent.w,
			sound, protocol.SoundCategoryNeutral,
			x, y, z, 1.0, 1.0)

	}
	return nil
}

// feedMob feeds an animal its food in Animal.mobInteract order. A branch
// that acts spends one item (not in creative); one that cannot (breed
// cooldown, adult already in love, age-locked baby) spends and shows
// nothing; a corpse is not fed. Love shows the hearts (entity status 18),
// then the kind's feeding sounds and a server-side swing
// (InteractionResult.SUCCESS_SERVER); a baby grows with only the eat sound
// (SUCCESS). It reports whether the food was taken.
// vanilla: Mob.interact, Animal.mobInteract, setInLove
func (c *client) feedMob(ent *mobEntity, hand int32) (bool, error) {
	s := c.srv
	held := c.heldItem(hand)
	s.mobsMu.Lock()
	m := ent.mob
	if !m.Alive() {
		s.mobsMu.Unlock()
		return false, nil
	}
	loved, grew := m.Feed()
	after := s.bodyInteraction(ent)
	x, y, z := m.X, m.Y, m.Z
	kind := m.Type
	ticks := m.InLove
	s.mobsMu.Unlock()
	// The listener runs with no lock held, so a cancel or a changed length is
	// written back after the feed.
	if loved {
		if kept := c.loveAllowed(ent, ticks); kept != ticks {
			s.mobsMu.Lock()
			m.InLove = kept
			s.mobsMu.Unlock()
			loved = kept > 0
			grew = grew || !loved // the food is spent either way
		}
	}

	if !loved && !grew {
		if kind == entity.TypePanda {
			return c.pandaSnack(ent, hand)
		}
		return false, nil
	}
	// vanilla: Axolotl.usePlayerItem, Animal.usePlayerItem
	if entity.IsNautilus(kind) {
		if err := c.consumeNautilusFood(hand, c.heldItem(hand)); err != nil {
			return true, err
		}
	} else if kind == entity.TypeAxolotl {
		c.filledResult(hand, protocol.ItemStack{ItemID: int32(registry.ItemWaterBucket), Count: 1}, true)
	} else if c.player.GameMode() != protocol.GameModeCreative {
		if err := c.consumeHeld(hand); err != nil {
			return true, err
		}
	}

	remaining := c.heldItem(hand)
	s.mobsMu.Lock()
	sounds := m.FeedSounds(remaining)
	s.mobsMu.Unlock()

	// vanilla: setInLove's event 18 goes out ahead of playEatingSound.
	if loved {
		s.mobsMu.Lock()
		ent.loveCause = c.player.UUID
		s.mobsMu.Unlock()
		s.broadcastWorld(ent.w, protocol.IDPlayEntityEvent,
			entityEventPayload(m.ID, protocol.EntityStatusLoveHearts))
	}
	for _, sound := range sounds {
		s.broadcastSoundID(ent.w,
			sound.Event, mobSoundCategory(kind), x, y, z, sound.Volume, sound.Pitch)
	}
	c.interactedWithEntityAdvancements(after, held)
	if !loved {
		c.useSwing = swingPredicted
		return true, nil
	}
	// The server swings the arm, SUCCESS_SERVER's answer to the click.
	c.useSwing = swingServer
	return true, nil
}

// spawnTime is the date the spawn rolls read.
func (s *Server) spawnTime() time.Time {
	if s.spawnNow != nil {
		return s.spawnNow()
	}
	return time.Now()
}

// nameIsJohnny reports whether a custom name's text reads exactly "Johnny",
// whatever its styling (vanilla: Vindicator.setCustomName, getString).
func nameIsJohnny(name nbt.Tag) bool {
	return name != nil && protocol.PlainTextOf(name) == "Johnny"
}

// nextMobEntity reserves a contiguous range for the dragon's client-created parts.
func (s *Server) nextMobEntity(kind entity.MobType) int32 {
	if kind == entity.TypeEnderDragon {
		return s.nextEntityID.Add(9) - 8
	}
	return s.nextEntity()
}

func (s *Server) mobAttackBox(ent *mobEntity, target int32) (float64, float64, float64, float64, float64, float64) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	if ent.mob.Type == entity.TypeEnderDragon && target > ent.mob.ID && target <= ent.mob.ID+8 {
		b := ent.mob.DragonPartBox(int(target - ent.mob.ID - 1))
		return b.MinX, b.MinY, b.MinZ, b.MaxX, b.MaxY, b.MaxZ
	}
	return ent.mob.BlastBox()
}

// hatchTadpole spawns the persistent body from the block's hatch roll.
// vanilla: FrogspawnBlock.spawnTadpoles
func (s *Server) hatchTadpole(dim *dimension, h world.TadpoleHatch) {
	s.spawnMobArriving(dim, entity.TypeTadpole, h.X, h.Y, h.Z, 0, nil, false, "", false, func(ent *mobEntity) {
		ent.mob.Persistence = true
		ent.mob.Yaw, ent.mob.HeadYaw = h.Yaw, h.Yaw
	})
}

// shearEquipment takes the first shearable piece off a mob: one shears wear,
// the piece drops, its shearing sound plays.
// vanilla: Entity.interact, Mob.attemptToShearEquipment, Mob.shearItem
func (c *client) shearEquipment(ent *mobEntity, hand int32, shears protocol.ItemStack) bool {
	s := c.srv
	s.mobsMu.Lock()
	after := s.bodyInteraction(ent)
	taken, ok := ent.mob.ShearEquipment(c.player.UUID, c.player.Vitals().GameMode == protocol.GameModeCreative)
	x, y, z, silent := ent.mob.X, ent.mob.Y, ent.mob.Z, ent.mob.Silent
	s.mobsMu.Unlock()
	if !ok {
		return false
	}
	c.wearHeld(hand, 1)
	s.spawnItemAtFeet(ent.w, taken.Stack, x, y+taken.DropHeight, z)
	if !silent {
		s.broadcastSoundHolder(ent.w, taken.Sound, protocol.SoundCategoryNeutral, x, y, z, 1, 1)
	}
	s.gameEvent(ent.w, rules.GameEventShear, x, y, z, c.gameEventContext(0))
	c.shearedEquipmentAdvancements(after, taken.Stack)
	c.interactedWithEntityAdvancements(after, shears)
	return true
}

// mobAttackLoot is the loot snapshot of a mob under attack, for the loot
// conditions of a data-pack effect; the caller holds no mob lock.
func (s *Server) mobAttackLoot(ent *mobEntity) *rules.LootEntity {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	return s.mobLootLocked(ent)
}

// mobLootLocked is mobAttackLoot for a caller that holds mobsMu; the saved
// compound is built when a predicate asks, under the same lock.
func (s *Server) mobLootLocked(ent *mobEntity) *rules.LootEntity {
	e := mobLootEntity(ent.mob)
	m := ent.mob
	e.Place = &rules.LootPlace{X: m.X, Y: m.Y, Z: m.Z, OnGround: m.OnGround,
		OnPos: [3]int32{rules.BlockOf(m.X), int32(math.Floor(m.Y - 1e-5)), rules.BlockOf(m.Z)}}
	book := s.herdOf(ent.w).book
	e.NBT = func() nbt.Compound { return mobTagAs(ent, book, "").Entries() }
	return &e
}

// victimLoot is a player victim's loot snapshot for a data-pack effect.
func victimLoot(v *client) func() *rules.LootEntity {
	return func() *rules.LootEntity {
		e := v.LootEntity()
		return &e
	}
}
