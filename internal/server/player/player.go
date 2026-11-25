package player

import (
	"math"
	"sync"
	"sync/atomic"

	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// Property is a signed piece of profile data, such as a player's skin.
type Property struct {
	Name      string
	Value     string
	Signature string // empty if the property is unsigned
}

// Profile identifies a player: the account they logged in with, plus
// what the session server told us about them.
type Profile struct {
	UUID       uuid.UUID
	Name       string
	Properties []Property
}

// Position is where a player is and which way they are facing.
type Position struct {
	X, Y, Z    float64
	Yaw, Pitch float32
	OnGround   bool
}

// Appearance is what others see of a player besides position.
//
// Swimming is derived server-side from sprinting and the block at the eyes
// (vanilla sets it only while sprinting in water); without it a player in
// water draws standing for everyone else.
//
// FallFlying is the server's answer to the client's fall-flying command, the
// shared flag the wings draw from, and feeds the pose.
//
// SpinAttack is the riptide spin flag, which also feeds the pose.
//
// Pose is a protocol.Pose* value derived like Swimming (vanilla:
// Player.updatePlayerPose); it sets the eye height suffocation and drowning
// use and the reach a swing stands from.
//
// Eating is the use in progress (meal, bow draw, raised shield), vanilla's
// hand-state flag; EatingHand (protocol.Hand*, zero for main) sets its
// off-hand bit.
type Appearance struct {
	Sneaking   bool
	Sprinting  bool
	Swimming   bool
	FallFlying bool
	SpinAttack bool
	Pose       int32
	Eating     bool
	EatingHand int32

	// SkinParts is the enabled skin-layer bit mask; others must be told or
	// they draw the player bare.
	SkinParts uint8

	// LeftHanded is a main hand of left; the zero value is vanilla's
	// default of right.
	LeftHanded bool

	// Entity fields the player carries without simulating them: synched to
	// watchers and saved. Name is the custom name as network NBT, "" for none.
	// vanilla: Entity.DATA_CUSTOM_NAME, DATA_SILENT, DATA_NO_GRAVITY, shared flag 6
	Glowing, Silent, NoGravity, NameVisible bool
	Name                                    string
}

// DeathLocation is a dimension's wire name and a block position; Set is false
// for a player who has not died.
type DeathLocation struct {
	Set       bool
	Dimension string
	X, Y, Z   int32
}

// Vitals is a player's survival state; GameMode is a protocol.GameMode*
// value. Several connections change it at once (a hit arrives on the
// attacker's while the victim's reports a landing), so it is reached only
// through Vitals, SetVitals and UpdateVitals under the player's lock. The
// tick counters are the server's clock, not what a file holds.
type Vitals struct {
	GameMode   int32
	Health     float64
	Food       int32
	Saturation float64
	Exhaustion float64
	Dead       bool

	// PreviousGameMode is the mode before the last switch, valid only when
	// HasPreviousGameMode; the F3+F4 switcher and the player file use it.
	// vanilla: ServerPlayerGameMode.previousGameModeForPlayer
	PreviousGameMode    int32
	HasPreviousGameMode bool

	// LastDeath is where the player last died, which the recovery compass
	// points to. vanilla: Player.lastDeathLocation
	LastDeath DeathLocation

	// Absorption is the extra hearts the absorption effect gave, spent
	// before health (vanilla: LivingEntity.absorptionAmount).
	Absorption float64

	// Experience is the banked orbs as Set Experience sends them and a file
	// keeps them (XpLevel, XpP, XpTotal); progress is a float32 because
	// vanilla's level crossings are calculated at that width.
	ExperienceLevel    int
	ExperienceProgress float32
	ExperienceTotal    int

	// LastLevelUpTick is the AgeTicks the last level-up sound was sent
	// on, which spaces out the sounds of quick levels.
	LastLevelUpTick int

	// Score is the death screen's score: every experience point gained,
	// kept through respawns; int32 wraps as vanilla's int does.
	// vanilla: Player.DATA_SCORE_ID
	Score int32

	// InvulnerableTicks counts down the half-second hurt immunity
	// (vanilla's HurtInvulnerableTicks).
	InvulnerableTicks int

	// DeathTime counts the ticks a dead body has lain, so a save reads it with
	// the health it belongs to. vanilla: LivingEntity.deathTime
	DeathTime int32

	// HurtTime is the saved hurt animation countdown; loading it grants no immunity.
	// vanilla: LivingEntity.hurtTime
	HurtTime int

	// LastHurt is the damage that opened the immunity window, which a
	// stronger hit inside it is measured against.
	// vanilla: LivingEntity.lastHurt
	LastHurt float64

	// LastHurtBy is the entity ID that last hurt this body (0 for none),
	// LastHurtByAge the AgeTicks it landed; LastHurtMob and LastHurtMobAge
	// the same for the mob this body last struck.
	// vanilla: LivingEntity.lastHurtByMob, lastHurtMob and their timestamps
	LastHurtBy, LastHurtMob       int32
	LastHurtByAge, LastHurtMobAge int
	// LastHurtByPlayer is the player credited with the last hurt (a tame
	// wolf's owner), uuid.Nil for none; LastHurtByPlayerAge the AgeTicks it
	// was set at, which the credit outlasts for 100 ticks.
	// vanilla: LivingEntity.lastHurtByPlayer, lastHurtByPlayerMemoryTime
	LastHurtByPlayer    uuid.UUID
	LastHurtByPlayerAge int
	// LastHurtNoWolfRetaliation says the last landed hurt's damage type is in
	// #no_wolf_retaliation. vanilla: LivingEntity.lastDamageSource
	LastHurtNoWolfRetaliation bool

	// FoodTicks is the counter the 80-tick steps of regeneration and
	// starvation act on, started over whenever none of them applies.
	FoodTicks int

	// AgeTicks is the player's age in ticks, the clock the peaceful
	// rhythms and the level-up sound's 100 ticks run on.
	// vanilla: Entity.tickCount
	AgeTicks int

	// FallDistance is in blocks, read for the critical hit and reset on
	// landing.
	// vanilla: Entity.fallDistance
	FallDistance float64

	// FallStart is where the fall FallDistance counts began, nil when not
	// falling; LevitationStart is where the body stood when Levitation took
	// hold, nil without it; EnteredNether is where it stood in the overworld
	// when it last crossed into the nether, nil once it has left the nether;
	// LavaRideStart is where it stood when its vehicle entered lava, nil
	// once the vehicle is out. The distance triggers measure from them.
	// vanilla: ServerPlayer.startingToFallPosition, levitationStartPos, enteredNetherPosition,
	// enteredLavaOnVehiclePosition
	FallStart, LevitationStart, EnteredNether, LavaRideStart *[3]float64

	// ExplosionImpact is where an explosion last hit the body and
	// ExplosionCause the entity type that caused it ("" for none); the next
	// fall's start reads and clears them.
	// vanilla: ServerPlayer.currentExplosionImpactPos, currentExplosionCause
	ExplosionImpact *[3]float64
	ExplosionCause  string

	// ImpulseImpact is the height a wind charge or mace smash launched the
	// body from, nil for none: a fall counts only below it. ImpulseGraceTicks
	// is how many more ticks a landing cannot clear it.
	// vanilla: LivingEntity.currentImpulseImpactPos, currentImpulseContextResetGraceTime
	ImpulseImpact     *[3]float64
	ImpulseGraceTicks int

	// SpawnExtraParticlesOnFall makes the next landing raise block dust.
	// vanilla: ServerPlayer.spawnExtraParticlesOnFall
	SpawnExtraParticlesOnFall bool

	// FireTicks is the burn left, in ticks. Below zero it is the douse
	// cooldown before fire can take hold again.
	// vanilla: Entity.remainingFireTicks
	FireTicks int

	// AirSupply is the breath left in ticks (300 fresh), the client's
	// bubble countdown.
	// vanilla: Entity.airSupply
	AirSupply int

	// TicksFrozen rises one a tick in powder snow and falls two a tick
	// outside; the client reads it (metadata index 7) for the frost overlay.
	// vanilla: Entity.ticksFrozen
	TicksFrozen int

	// ArrowCount is the arrows stuck in the body (metadata index 12);
	// RemoveArrowTime counts down to the next falling out.
	// vanilla: LivingEntity.getArrowCount, removeArrowTime
	ArrowCount      int
	RemoveArrowTime int

	// StingerCount is the bee stingers stuck in the body (metadata index
	// 13); RemoveStingerTime counts down to the next one falling out.
	// vanilla: LivingEntity.getStingerCount, removeStingerTime
	StingerCount      int
	RemoveStingerTime int
}

// Sleep is a player's bed state. Ticks is vanilla's Player.sleepCounter,
// at 100 of which the night can pass for this sleeper; it is read only
// while Lying.
//
// It is reached through Sleep, SetSleep and UpdateSleep under the player's
// lock: a sleep begins on its connection and is woken from the tick loop or
// another connection.
type Sleep struct {
	Lying   bool
	X, Y, Z int32
	Ticks   int
}

// Player is a profile that has made it into the world.
//
// Position, appearance, inventory, vitals, sleep and attributes are read by
// every viewer's connection and written by the player's own, so they are
// reachable only through locking methods. Profile and EntityID are fixed
// after login and read without the lock.
type Player struct {
	Profile

	EntityID int32

	// latency is the smoothed keep-alive round trip in milliseconds, which the
	// tab list shows as the ping bar.
	latency atomic.Int32

	mu            sync.RWMutex
	position      Position
	velocity      Velocity
	knownMovement Velocity
	movedThisTick bool
	appearance    Appearance
	inventory     Inventory
	mapHoldings   mapHoldings
	vitals        Vitals
	sleep         Sleep
	effects       effect.Set
	attributes    Attributes
	// seenCredits skips the end credits on a later exit from the end.
	seenCredits bool

	// shoulders is the entities perched on the player.
	shoulders Shoulders
	tags      nbt.StringSet
	// locatorIcon is how the locator bar draws this player's waypoint.
	locatorIcon LocatorIcon
}

// Attributes runs fn over the player's attributes under the player's lock.
func (p *Player) Attributes(fn func(a *Attributes)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fn(&p.attributes)
}

// AttributesWithEquipment is Attributes with the equipment held and worn as
// it stands; the copy is why the plain Attributes leaves it out.
func (p *Player) AttributesWithEquipment(fn func(a *Attributes, now Equipment)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fn(&p.attributes, p.inventory.Equipment())
}

// ResetAttributes gives a respawned body its map: bases kept, modifiers
// dropped to be put on again, and the client to be introduced anew.
// vanilla: ServerPlayer.restoreFrom, AttributeMap.assignBaseValues
func (p *Player) ResetAttributes() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.attributes = p.attributes.withBasesOnly()
}

// Inventory runs fn over the inventory under its lock. It is not copied out
// because it is large and every use changes it or writes it straight out.
func (p *Player) Inventory(fn func(*Inventory)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Callers may mutate Slots directly, including through read/modify callbacks.
	p.mapHoldings.valid = false
	fn(&p.inventory)
}

// Position returns where the player is, as last reported by their client.
func (p *Player) Position() Position {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.position
}

// SetPosition records where the player is.
func (p *Player) SetPosition(pos Position) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.position = pos
}

// Velocity is the server-side delta movement in blocks per tick, as
// knockback and launches set it; the client drives its own motion and never
// reports one.
// vanilla: Entity.deltaMovement
type Velocity struct{ X, Y, Z float64 }

// Velocity returns the server-side delta movement.
func (p *Player) Velocity() Velocity {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.velocity
}

// KnownMovement returns the last movement reported by the client.
func (p *Player) KnownMovement() Velocity {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.knownMovement
}

// SetKnownMovement records a movement in the current client tick.
// vanilla: ServerPlayer.lastKnownClientMovement
func (p *Player) SetKnownMovement(v Velocity) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.knownMovement, p.movedThisTick = v, true
}

// EndMovementTick clears the known movement after a client tick without a move.
// vanilla: ServerGamePacketListenerImpl.handleClientTickEnd
func (p *Player) EndMovementTick() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.movedThisTick {
		p.knownMovement = Velocity{}
	}
	p.movedThisTick = false
}

// SetVelocity replaces the server-side delta movement.
func (p *Player) SetVelocity(v Velocity) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.velocity = v
}

// AddVelocity adds to the server-side delta movement.
// vanilla: Entity.push
func (p *Player) AddVelocity(x, y, z float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.velocity.X += x
	p.velocity.Y += y
	p.velocity.Z += z
}

// knockbackLiftCap is the most a grounded knockback lifts the body to.
// vanilla: LivingEntity.knockback
const knockbackLiftCap = 0.4

// KnockStep folds a knockback into velocity v: half the old horizontal plus the
// shove; on the ground half the old lift plus y, capped at 0.4; in the air the
// vertical stays.
// vanilla: LivingEntity.knockback
func KnockStep(v Velocity, x, y, z float64, onGround bool) Velocity {
	v.X = v.X/2 + x
	v.Z = v.Z/2 + z
	if onGround {
		v.Y = math.Min(knockbackLiftCap, v.Y/2+y)
	}
	return v
}

// Knock folds a knockback into the delta movement and returns it.
// vanilla: LivingEntity.knockback
func (p *Player) Knock(x, y, z float64, onGround bool) Velocity {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.velocity = KnockStep(p.velocity, x, y, z, onGround)
	return p.velocity
}

// Per-tick decay of the server-side delta movement.
// vanilla: LivingEntity.travel, LivingEntity.aiStep
const (
	airDrag               = 0.91
	groundDrag            = 0.6 * 0.91 // default block friction times the air drag
	gravityPerTick        = 0.08
	verticalDrag          = 0.98
	minMotion             = 0.003
	minHorizontalMotionSq = 9.0e-6
)

// StepVelocity ages the delta movement a tick: the small-motion cut
// (horizontal as one below a squared length of 9.0E-6, vertical below
// 0.003), then drag and gravity. Ground contact cancels downward motion
// before gravity; the client drives the body and its other collisions.
// vanilla: LivingEntity.aiStep, LivingEntity.travel
func (p *Player) StepVelocity(onGround bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v := &p.velocity
	if !onGround && *v == (Velocity{}) {
		return
	}
	if v.X*v.X+v.Z*v.Z < minHorizontalMotionSq {
		v.X, v.Z = 0, 0
	}
	if math.Abs(v.Y) < minMotion {
		v.Y = 0
	}
	drag := airDrag
	if onGround {
		drag = groundDrag
		if v.Y < 0 {
			v.Y = 0
		}
	}
	v.Y = (v.Y - gravityPerTick) * verticalDrag
	v.X *= drag
	v.Z *= drag
}

// UpdatePosition runs fn on the position under the lock and returns the
// result, so a partial change cannot undo another goroutine's move.
func (p *Player) UpdatePosition(fn func(*Position)) Position {
	p.mu.Lock()
	defer p.mu.Unlock()
	fn(&p.position)
	return p.position
}

// Appearance returns what the player currently looks like to everyone else.
func (p *Player) Appearance() Appearance {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.appearance
}

// SetAppearance records what the player looks like to everyone else.
func (p *Player) SetAppearance(a Appearance) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.appearance = a
}

// Sleep returns the bed the player lies in, as it stands.
func (p *Player) Sleep() Sleep {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.sleep
}

// SetSleep records the bed the player lies in - or, with Lying unset, that
// they have stood back up and the counter they were left with.
func (p *Player) SetSleep(s Sleep) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sleep = s
}

// UpdateSleep runs fn on the sleep under the lock and returns the result; the
// connection and the tick loop both change it.
func (p *Player) UpdateSleep(fn func(*Sleep)) Sleep {
	p.mu.Lock()
	defer p.mu.Unlock()
	fn(&p.sleep)
	return p.sleep
}

// Latency returns the smoothed keep-alive round trip in milliseconds.
func (p *Player) Latency() int32 { return p.latency.Load() }

// SetLatency stores the smoothed keep-alive round trip in milliseconds.
func (p *Player) SetLatency(ms int32) { p.latency.Store(ms) }

// GameMode reports which of the four game modes the player is in, for the
// packets that name a mode.
func (p *Player) GameMode() int32 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.vitals.GameMode
}

// Vitals returns the player's survival state as it stands.
func (p *Player) Vitals() Vitals {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.vitals
}

// SetVitals overwrites the survival state whole, which is what restoring a
// player file and respawning do.
func (p *Player) SetVitals(v Vitals) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.vitals = v
}

// UpdateVitals runs fn on the vitals under the lock and returns the result.
// Read-modify-write must be one step: damage comes from other connections,
// regeneration from the tick loop, exhaustion from the player's own.
func (p *Player) UpdateVitals(fn func(*Vitals)) Vitals {
	p.mu.Lock()
	defer p.mu.Unlock()
	fn(&p.vitals)
	return p.vitals
}

// UpdateBody runs fn on the vitals, effects and attributes under one lock:
// an effect's tick changes the vitals, an effect caps the absorption it
// grants, and max_health caps the health.
func (p *Player) UpdateBody(fn func(*Vitals, *effect.Set, *Attributes)) Vitals {
	p.mu.Lock()
	defer p.mu.Unlock()
	fn(&p.vitals, &p.effects, &p.attributes)
	return p.vitals
}

// Effects returns the active effects in effect ID order.
func (p *Player) Effects() []effect.Instance {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.effects.All()
}

// Effect returns the active instance of one effect.
func (p *Player) Effect(id int32) (effect.Instance, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.effects.Get(id)
}

// TagCap is the most scoreboard tags a body takes.
// vanilla: Entity.addTag
const TagCap = 1024

// Tags returns a copy of the scoreboard tags in Java HashSet order, so a read
// cannot see a concurrent change.
// vanilla: Entity.entityTags
func (p *Player) Tags() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.tags.Values()
}

// SetTags replaces the tags whole, which a saved player file does. A tag
// held twice is one tag, the set vanilla reads a Tags list into.
func (p *Player) SetTags(tags []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tags.Reset(tags)
}

// SeenCredits reports whether the end credits were shown, skipping them on
// the next exit from the end.
// vanilla: ServerPlayer.seenCredits
func (p *Player) SeenCredits() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.seenCredits
}

// SetSeenCredits sets the end-credits flag.
// vanilla: ServerPlayer.showEndCredits, ServerPlayer.readAdditionalSaveData
func (p *Player) SetSeenCredits(seen bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seenCredits = seen
}

// AddTag adds a scoreboard tag; false if held already or at TagCap.
// vanilla: Entity.addTag
func (p *Player) AddTag(tag string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return AddTag(&p.tags, tag)
}

// RemoveTag removes a scoreboard tag; false if not held.
// vanilla: Entity.removeTag
func (p *Player) RemoveTag(tag string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tags.Remove(tag)
}

// AddTag inserts a scoreboard tag unless the set is at TagCap.
// vanilla: Entity.addTag
func AddTag(tags *nbt.StringSet, tag string) bool {
	if tags.Len() >= TagCap {
		return false
	}
	return tags.Add(tag)
}
