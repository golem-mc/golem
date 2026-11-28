package entity

import (
	"math"
	"reflect"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The allay: a flying creature that takes one item of a kind from a player,
// fetches matching items off the ground, brings them to that player (or to a
// note block it heard), dances to a jukebox and duplicates on an amethyst
// shard. Not modelled: the vibration listener's travel delay
// (a note block is heard at once within 16 blocks).
// vanilla: Allay, AllayAi

// TypeAllay is minecraft:allay.
const TypeAllay = registry.EntityTypeAllay

// The allay's numbers.
const (
	// allayDuplicationCooldown is DUPLICATION_COOLDOWN_TICKS.
	allayDuplicationCooldown = 6000
	// allayJukeboxRadius is JUKEBOX_PLAY's notification radius.
	allayJukeboxRadius = 10.0
	// AllayNoteRadius is VibrationUser.getListenerRadius.
	AllayNoteRadius = 16.0
	// allayNoteMemory is TIME_TO_FORGET_NOTEBLOCK.
	allayNoteMemory = 600
	// allayThrowCooldown is GoAndGiveItemsToTarget's ITEM_PICKUP_COOLDOWN_AFTER_THROWING.
	allayThrowCooldown = 60
	// allayItemRange is GoToWantedItem's maxDistToWalk, allayLikedRange
	// getLikedPlayer's closerThan, allayGiveRange GoAndGiveItemsToTarget's
	// CLOSE_ENOUGH_DISTANCE_TO_TARGET.
	allayItemRange  = 32.0
	allayLikedRange = 64.0
	allayGiveRange  = 3.0
	// allayStayClose and allayTooFar are StayCloseToTarget's closeEnough and tooFar.
	allayStayClose = 4
	allayTooFar    = 16.0
	// allayWanderRange is RandomStroll.fly's MAX_XZ_DIST.
	allayWanderRange = 10.0
	// allayReplan is how often a moving errand target is re-planned.
	allayReplan = 10
	// allayThrowHorizontal, allayThrowVertical and allayThrowDrop are the
	// delivery throw's velocity scales and its drop below the eyes.
	// vanilla: GoAndGiveItemsToTarget.throwItem
	allayThrowHorizontal = float64(float32(0.2))
	allayThrowVertical   = float64(float32(0.3))
	allayThrowDrop       = 0.2
)

// allayThrowPitches is Allay.THROW_SOUND_PITCHES.
var allayThrowPitches = [...]float32{0.5625, 0.625, 0.75, 0.9375, 1, 1, 1.125, 1.25, 1.5, 1.875, 2, 2.25, 2.5, 3, 3.75, 4}

func init() {
	// vanilla: EntityTypes.ALLAY (0.35 by 0.6, eyes 0.36, tracking range 8,
	// update interval 2), Allay.createAttributes, getSoundVolume
	kinds[TypeAllay] = &kind{
		name: "Allay", width: float64(float32(0.35)), height: float64(float32(0.6)), eyeHeight: float64(float32(0.36)),
		maxHealth: 20, speed: float64(float32(0.1)), attackDamage: 2, volume: 0.4, flying: true, flyPath: &allayFlight,
		sounds: MobSounds{Ambient: registry.SoundEntityAllayAmbientWithoutItem, Hurt: registry.SoundEntityAllayHurt, Death: registry.SoundEntityAllayDeath},
	}
	mobTracking[TypeAllay] = tracking{rangeChunks: 8, updateInterval: 2}
}

// allayState is what an allay keeps beyond the common mob.
type allayState struct {
	dancing bool
	// jukebox is the jukebox the dance is to, when jukeboxSet.
	jukebox    [3]int32
	jukeboxSet bool
	// cooldown is DuplicationCooldown, the ticks until it may duplicate again.
	cooldown int64
	// liked is LIKED_PLAYER; likedID is that player's entity ID, which a blow
	// from them is checked against.
	liked   uuid.UUID
	likedID int32
	// note is LIKED_NOTEBLOCK_POSITION, nil for none; noteTicks is
	// LIKED_NOTEBLOCK_COOLDOWN_TICKS.
	note      *GlobalPos
	noteTicks int
	// pickupWait is ITEM_PICKUP_COOLDOWN_TICKS; zero remains present until the next tick.
	pickupWait    int
	pickupWaitSet bool
	// replan counts the ticks to the errand's next path plan.
	replan int
	// listener is the saved vibration data, kept untouched through a save.
	listener nbt.Tag
}

// AllayDancing is Allay.DATA_DANCING.
func (m *Mob) AllayDancing() bool { return m.allay.dancing }

// AllayCanDuplicate is Allay.DATA_CAN_DUPLICATE.
func (m *Mob) AllayCanDuplicate() bool { return m.allay.cooldown == 0 }

// AllayHolding reports Allay.hasItemInHand.
func (m *Mob) AllayHolding() bool { return !m.MainHand.Empty() }

// AllayLikesPlayer reports whether the player is the allay's LIKED_PLAYER.
// vanilla: Allay.isLikedPlayer
func (m *Mob) AllayLikesPlayer(id uuid.UUID) bool {
	return m.Type == TypeAllay && id != uuid.Nil && m.allay.liked == id
}

func (m *Mob) allayLikes(causeID int32) bool {
	return m.allay.likedID != 0 && m.allay.likedID == causeID
}

// allayGoals are AllayAi's core and idle activities, the errands folded into
// one goal that picks the first that applies, as the brain's ordered idle list
// does.
func allayGoals(m *Mob) []Goal {
	return []Goal{newFloatGoal(m), &panicGoal{m: m, pace: 2.5, brain: true},
		&allayErrand{m: m},
		&lookAtPlayer{m: m, priority: 2, reach: 6},
		// vanilla: RandomStroll.fly has no noActionTime gate.
		&randomStroll{m: m, priority: 3, interval: 30, pace: 1, ignoreIdle: true, pick: func(t *tickView) (Waypoint, bool) {
			return m.airWanderPos(t, allayWanderRange)
		}}}
}

// allayTarget is where the allay's errand leads and how.
type allayTarget struct {
	x, y, z float64
	pace    float64
	// reach is how close the walk ends; item marks a fetch, give a delivery.
	reach       int32
	fetch, give bool
}

// allayErrand is GoToWantedItem, GoAndGiveItemsToTarget and StayCloseToTarget
// in the idle activity's order.
// vanilla: AllayAi.initIdleActivity
type allayErrand struct {
	m *Mob
	t allayTarget
}

func (g *allayErrand) Priority() int    { return 1 }
func (g *allayErrand) Flags() GoalFlags { return GoalMove | GoalLook }

func (g *allayErrand) CanStart(t *tickView) bool {
	var ok bool
	g.t, ok = g.m.allayPlan(t)
	return ok
}

func (g *allayErrand) Start(t *tickView) {
	g.m.allay.replan = 0
	g.Tick(t)
}

func (g *allayErrand) ShouldContinue(t *tickView) bool {
	var ok bool
	g.t, ok = g.m.allayPlan(t)
	return ok
}

func (g *allayErrand) Tick(t *tickView) {
	m, p := g.m, g.t
	m.HeadYaw = float32(yawToward(p.x-m.X, p.z-m.Z))
	if p.give {
		m.allayGive(t, p)
	}
	if m.allay.replan--; m.allay.replan > 0 {
		return
	}
	m.allay.replan = allayReplan
	m.seekTo(t.w, p.x, p.y, p.z, p.pace, p.reach)
}

func (g *allayErrand) Stop(*tickView)          { g.m.stopSeeking() }
func (g *allayErrand) RequiresEveryTick() bool { return true }

// allayPlan is the first applicable errand.
// vanilla: AllayAi.initIdleActivity
func (m *Mob) allayPlan(t *tickView) (allayTarget, bool) {
	b := &m.barter
	if m.LootPickupEnabled() && b.hasWanted &&
		Dist2(m.X, m.Y, m.Z, b.wanted.X, b.wanted.Y, b.wanted.Z) < allayItemRange*allayItemRange {
		return allayTarget{x: b.wanted.X, y: b.wanted.Y, z: b.wanted.Z, pace: 1.75, fetch: true}, true
	}
	x, y, z, ok := m.allayDeposit(t)
	if !ok {
		return allayTarget{}, false
	}
	if !b.Inventory[0].Empty() {
		return allayTarget{x: x, y: y, z: z, pace: 2.25, reach: navReach, give: true}, true
	}
	if !b.hasWanted && Dist2(m.X, m.Y, m.Z, x, y, z) >= allayTooFar*allayTooFar {
		return allayTarget{x: x, y: y, z: z, pace: 2.25, reach: allayStayClose}, true
	}
	return allayTarget{}, false
}

// allayDeposit is getItemDepositPosition: the liked note block's top while it
// is still a note block and was heard lately, else the liked player's eyes.
// A note block that no longer qualifies is forgotten.
// vanilla: AllayAi.getItemDepositPosition, shouldDepositItemsAtLikedNoteblock
func (m *Mob) allayDeposit(t *tickView) (x, y, z float64, ok bool) {
	a := &m.allay
	if n := a.note; n != nil {
		if a.noteTicks > 0 && m.memHere(n) && m.allayNoteInRange(n) && registry.BlockOf(t.w.BlockAt(n.X, n.Y, n.Z)) == registry.BlockNoteBlock {
			return float64(n.X) + 0.5, float64(n.Y + 1), float64(n.Z) + 0.5, true
		}
		a.note = nil
	}
	if p := m.allayLikedPlayer(t); p != nil {
		return p.X, p.Y + p.eyeHeight(), p.Z, true
	}
	return 0, 0, 0, false
}

// allayNoteInRange is GlobalPos.isCloseEnough's block-coordinate distance check.
func (m *Mob) allayNoteInRange(n *GlobalPos) bool {
	// vanilla: GlobalPos.isCloseEnough, Vec3i.distChessboard
	return max(math.Abs(float64(n.X)-math.Floor(m.X)),
		math.Abs(float64(n.Y)-math.Floor(m.Y)),
		math.Abs(float64(n.Z)-math.Floor(m.Z))) <= 1024
}

// allayLikedPlayer is the liked player within 64 blocks, nil for none; the
// server still checks its game mode.
// vanilla: AllayAi.getLikedPlayer
func (m *Mob) allayLikedPlayer(t *tickView) *Player {
	if m.allay.liked == uuid.Nil {
		return nil
	}
	for i := range t.players {
		p := &t.players[i]
		if p.UUID == m.allay.liked && !p.Spectator && !p.Dead &&
			Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z) < allayLikedRange*allayLikedRange {
			m.allay.likedID = p.EntityID // re-derived after a load or a rejoin
			return p
		}
	}
	return nil
}

// allayGive throws one carried item toward the target once within three
// blocks, and stops picking up for 60 ticks.
// vanilla: GoAndGiveItemsToTarget.tick, BehaviorUtils.throwItem, AllayAi.onItemThrown
// The sound's one-in-seven gate counts the mob's own ticks, not the level's.
func (m *Mob) allayGive(t *tickView, p allayTarget) {
	_, ey, _ := m.EyePosition()
	if Dist2(m.X, ey, m.Z, p.x, p.y, p.z) >= allayGiveRange*allayGiveRange {
		return
	}
	inv := &m.barter.Inventory
	if inv[0].Empty() {
		return
	}
	one := inv[0]
	one.Count = 1
	if inv[0].Count--; inv[0].Count <= 0 {
		inv[0] = protocol.ItemStack{}
	}
	ev := m.throwEvent(one, p.x, p.y+1, p.z, allayThrowHorizontal, allayThrowVertical, allayThrowDrop)
	if liked := m.allayLikedPlayer(t); liked != nil {
		ev.Liked = liked.UUID
		ev.DropOn = [3]int32{int32(math.Floor(p.x)), int32(math.Floor(p.y)) - 1, int32(math.Floor(p.z))}
	}
	t.events = append(t.events, ev)
	m.allay.pickupWait = allayThrowCooldown
	m.allay.pickupWaitSet = true
	if m.Age%7 == 0 && m.Rand.Float64() < 0.9 {
		pitch := allayThrowPitches[m.Rand.Intn(len(allayThrowPitches))]
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobMoaned, SoundID: registry.SoundEntityAllayItemThrown, Volume: 1, Pitch: pitch})
	}
}

// allayCooldownStep counts down DuplicationCooldown each tick, including with
// NoAI.
// vanilla: Allay.aiStep, Allay.updateDuplicationCooldown
func (m *Mob) allayCooldownStep() {
	if m.allay.cooldown > 0 {
		m.SetMetadataAllayCooldown(m.allay.cooldown - 1)
	}
}

// allayStep is the AI-gated part of the allay's tick: brain cooldowns,
// regeneration and the dance's end.
// vanilla: Allay.aiStep, tick, CountDownCooldownTicks
func (m *Mob) allayStep(t *tickView) {
	if !m.Alive() {
		return
	}
	a := &m.allay
	if a.pickupWaitSet {
		// vanilla: CountDownCooldownTicks.canStillUse, tick, stop
		if a.pickupWait > 0 {
			a.pickupWait--
		} else {
			a.pickupWaitSet = false
		}
	}
	if a.noteTicks > 0 {
		a.noteTicks--
	}
	if m.Age%10 == 0 {
		m.SetMetadataHealth(math.Min(m.Health+1, m.MaxHealth))
	}
	if a.dancing && m.Age%20 == 0 && m.allayDanceOver(t.w) {
		m.SetMetadataAllayDancing(false)
		a.jukeboxSet = false
	}
	if a.dancing && m.panicking() {
		m.SetMetadataAllayDancing(false)
	}
}

// allayDanceOver is shouldStopDancing.
func (m *Mob) allayDanceOver(w World) bool {
	a := &m.allay
	if !a.jukeboxSet {
		return true
	}
	j := a.jukebox
	return Dist2(m.X, m.Y, m.Z, float64(j[0])+0.5, float64(j[1])+0.5, float64(j[2])+0.5) >= allayJukeboxRadius*allayJukeboxRadius ||
		registry.BlockOf(w.BlockAt(j[0], j[1], j[2])) != registry.BlockJukebox
}

// SetJukeboxPlaying is Allay.setJukeboxPlaying for a jukebox within ten blocks
// starting or stopping its song.
func (m *Mob) SetJukeboxPlaying(x, y, z int32, playing bool) {
	a := &m.allay
	if m.NoAI || !m.Alive() || m.Type != TypeAllay {
		return
	}
	if playing {
		if !a.dancing && !m.panicking() {
			a.jukebox, a.jukeboxSet = [3]int32{x, y, z}, true
			m.SetMetadataAllayDancing(true)
		}
	} else if !a.jukeboxSet || a.jukebox == [3]int32{x, y, z} {
		a.jukeboxSet = false
		m.SetMetadataAllayDancing(false)
	}
}

// AllayHearsJukebox reports whether a jukebox at the cell is within the
// listener's radius.
func (m *Mob) AllayHearsJukebox(x, y, z int32) bool {
	_, ey, _ := m.EyePosition()
	return Dist2(m.X, ey, m.Z, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5) <= allayJukeboxRadius*allayJukeboxRadius
}

// AllayHearsNote is VibrationUser.onReceiveVibration for a note block played
// within sixteen blocks: it likes that block while it has no other.
// vanilla: AllayAi.hearNoteblock, VibrationUser.canReceiveVibration, VibrationSystem.Listener.handleGameEvent
func (m *Mob) AllayHearsNote(w World, dim string, x, y, z int32) {
	a := &m.allay
	if m.NoAI || !m.Alive() {
		return
	}
	_, ey, _ := m.EyePosition()
	if Dist2(m.X, ey, m.Z, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5) > AllayNoteRadius*AllayNoteRadius ||
		world.VibrationOccluded(w, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, m.X, ey, m.Z) {
		return
	}
	if heard := (GlobalPos{dim, x, y, z}); a.note == nil {
		a.note, a.noteTicks = &heard, allayNoteMemory
	} else if *a.note == heard && m.allayNoteInRange(a.note) {
		a.noteTicks = allayNoteMemory
	}
}

// AllayUse is what a use on an allay did.
type AllayUse struct {
	// Duplicate, Give and Take say which branch ran; none ran if all are false.
	Duplicate, Give, Take bool
	// Sound is the branch's sound.
	Sound registry.Sound
	// DuplicateCooldown is the new allay's DuplicationCooldown.
	DuplicateCooldown int64
	// Given is the held item a Take hands back; Thrown are the throws of the
	// inventory it empties.
	Given  protocol.ItemStack
	Thrown []MobEvent
}

// UseAllay is Allay.mobInteract: a shard duplicates a dancing allay, an item
// goes into an empty-handed allay, and an empty main hand takes it back.
// vanilla: Allay.mobInteract
func (m *Mob) UseAllay(held protocol.ItemStack, hand int32, player uuid.UUID, playerID int32) AllayUse {
	a := &m.allay
	switch {
	case a.dancing && a.cooldown == 0 && !held.Empty() && registry.Item(held.ItemID).InTag(registry.ItemTagIDDuplicatesAllays):
		m.SetMetadataAllayCooldown(allayDuplicationCooldown)
		return AllayUse{Duplicate: true, Sound: registry.SoundBlockAmethystBlockChime, DuplicateCooldown: allayDuplicationCooldown}
	case m.MainHand.Empty() && !held.Empty():
		held.Count = 1
		m.SetEquipment(protocol.EquipMainHand, held)
		m.DropChances[0] = guaranteedDropChance
		a.liked, a.likedID = player, playerID
		return AllayUse{Give: true, Sound: registry.SoundEntityAllayItemGiven}
	case !m.MainHand.Empty() && hand == protocol.HandMain && held.Empty():
		use := AllayUse{Take: true, Sound: registry.SoundEntityAllayItemTaken, Given: m.MainHand}
		m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{})
		for _, stack := range m.inventoryDrops() {
			use.Thrown = append(use.Thrown, m.throwEvent(stack, m.X, m.Y, m.Z, piglinThrowSpeed, piglinThrowSpeed, piglinThrowDrop))
		}
		a.liked, a.likedID = uuid.Nil, 0
		return use
	}
	return AllayUse{}
}

// allayFields saves and loads DuplicationCooldown, the brain's memories
// and the listener data, which is kept as it was read.
// vanilla: Allay.addAdditionalSaveData, readAdditionalSaveData
func (m *Mob) allayFields(c *mobCodec) {
	if c.load {
		n, _ := nbt.IntField(c.tag, "DuplicationCooldown")
		m.SetMetadataAllayCooldown(int64(max(n, 0)))
		m.allay.listener = nil
		if listener, ok := nbt.Get[nbt.Compound](c.tag, "listener"); ok {
			candidate, current, travel := decodeWardenListener(listener)
			m.allay.listener = vibrationListenerTag(candidate, current, travel)
		}
		m.loadInventory(c.tag)
		memories := brainMemories(c.tag)
		liked, _ := nbt.Get[nbt.Compound](memories, "minecraft:liked_player")
		m.allay.liked, _ = nbt.UUIDField(liked, "value")
		m.allay.likedID = 0
		m.allay.note = brainGlobalPos(memories, allayNoteKey)
		m.allay.noteTicks = brainTicks(memories, allayNoteTicksKey)
		pickup, _ := nbt.Get[nbt.Compound](memories, allayPickupKey)
		value, present := nbt.IntValue(pickup["value"])
		m.allay.pickupWait, m.allay.pickupWaitSet = int(value), present
		return
	}
	memories := m.newMemoryMap()
	if m.allay.liked != uuid.Nil {
		memories.Set("minecraft:liked_player", memoryTag(nbt.UUIDToIntArray(m.allay.liked)))
	}
	putBrainGlobalPos(memories, allayNoteKey, m.allay.note)
	if m.allay.noteTicks > 0 {
		memories.Set(allayNoteTicksKey, memoryTag(nbt.Int(m.allay.noteTicks)))
	}
	if m.allay.pickupWaitSet {
		memories.Set(allayPickupKey, memoryTag(nbt.Int(m.allay.pickupWait)))
	}
	c.set("Brain", brainTag(memories))
	m.putInventory(c.output())
	if m.allay.listener != nil {
		c.set("listener", m.allay.listener)
	} else {
		// vanilla: VibrationSystem.Data.CODEC, VibrationSelector.CODEC
		c.set("listener", vibrationListenerTag(wardenVibration{}, wardenVibration{}, 0))
	}
	c.set("DuplicationCooldown", nbt.Long(m.allay.cooldown))
}

// The allay's saved brain memories besides LIKED_PLAYER.
// vanilla: MemoryModuleType.LIKED_NOTEBLOCK_POSITION, LIKED_NOTEBLOCK_COOLDOWN_TICKS, ITEM_PICKUP_COOLDOWN_TICKS
const (
	allayNoteKey      = "minecraft:liked_noteblock"
	allayNoteTicksKey = "minecraft:liked_noteblock_cooldown_ticks"
	allayPickupKey    = "minecraft:item_pickup_cooldown_ticks"
)

// takeAllay is Allay.wantsToPickUp and InventoryCarrier.pickUpItem: with a
// held item, the items of its kind go into the one inventory slot.
// vanilla: Allay.wantsToPickUp, allayConsidersItemEqual
func (s *stagedSlots) takeAllay(m *Mob, stack protocol.ItemStack) (slot string, count int32, ok bool) {
	if s.main.Empty() || !allayConsidersEqual(s.main, stack) {
		return "", 0, false
	}
	n := m.inventoryRoom(stack)
	return "inventory", n, n > 0
}

// allayConsidersEqual is the same item with equal potion contents; other
// components may differ.
// vanilla: Allay.allayConsidersItemEqual
func allayConsidersEqual(a, b protocol.ItemStack) bool {
	if a.ItemID != b.ItemID {
		return false
	}
	pa, oka := a.PotionContentsOf()
	pb, okb := b.PotionContentsOf()
	return oka == okb && reflect.DeepEqual(pa, pb)
}
