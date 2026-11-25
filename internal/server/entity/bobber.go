package entity

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The fishing bobber, from the FishingHook bytecode. A plain value like
// the thrown projectiles, with bodies passed in and events returned. The
// catch (loot, pull, rod wear) is the angler's retrieve; the bobber only
// runs the water's countdowns and says when a fish bites.

// EntityType FISHING_BOBBER: sized(0.25F, 0.25F), clientTrackingRange(4),
// updateInterval(5); the position is the box's bottom centre.
const (
	BobberHalfSize = 0.125
	BobberHeight   = 0.25
	BobberTracking = 4
	BobberInterval = 5
)

// FishingHook.tick and catchingFish numbers: a flat 0.03 gravity while
// flying dry (getDefaultGravity is 0, so this is all of it), the 0.92 end-of-
// tick drag, the damping on entering water, the bobbing's drag and spring,
// the bite sink, the out-of-water clock capped at 10, a grounded hook's
// life, the bite countdowns, and the approach a splash only shortens while
// a fish is near.
const (
	bobberGravity         = 0.03
	bobberDrag            = 0.92
	bobberWaterEntryDragX = 0.3
	bobberWaterEntryDragY = 0.2
	bobberWaterEntryDragZ = 0.3
	bobberBobDrift        = 0.9
	bobberBobSpring       = 0.2
	bobberBitingSink      = 0.1
	maxOutOfWaterTime     = 10
	bobberLifeLimit       = 1200
	// Bite countdowns, Mth.nextInt's inclusive bounds: the lure wait 100-600
	// minus the lure's ticks, the approach 20-80, the nibble window 20-40.
	bobberLuredMin    = 100
	bobberLuredRange  = 501
	bobberHookedMin   = 20
	bobberHookedRange = 61
	bobberNibbleMin   = 20
	bobberNibbleRange = 21
	// bobberSpringNudge pushes a near-zero spring off the surface by 0.1 either
	// way, since signum(0) is 0.
	bobberSpringNudge = 0.1
)

// BobberState is FishHookState.
type BobberState uint8

const (
	// BobberFlying is the cast flight, ending in water, a body or the ground.
	BobberFlying BobberState = iota
	// BobberHookedInEntity is the hook caught in a body, riding it until it
	// leaves or is reeled in.
	BobberHookedInEntity
	// BobberBobbing is floating, running the countdowns until a bite.
	BobberBobbing
)

// BobberEventKind is something a hook's tick produced for the server.
type BobberEventKind uint8

const (
	// BobberBite: a fish took the hook; the splash plays, the nibble window
	// opens and the bite shows on the wire. Reeling in during the window
	// catches it.
	BobberBite BobberEventKind = iota
	// BobberParticles: one particle emission, a fish's wake or the bite.
	BobberParticles
	// BobberHitBlock: the flight met a block face with no body nearer;
	// BlockHit names it.
	BobberHitBlock
	// BobberSplashed: the hook first touched water at X, Y, Z; the server
	// plays entity.generic.splash at Volume and Pitch and posts a splash
	// game event.
	// vanilla: Entity.doWaterSplashEffect
	BobberSplashed
	// BobberHitTarget reports the point where the hook caught a body.
	BobberHitTarget
)

// BobberEvent is one event from a hook's tick: a hit, bite, splash or particles.
type BobberEvent struct {
	Kind                      BobberEventKind
	X, Y, Z                   float64
	Particle                  int32
	SpreadX, SpreadY, SpreadZ float32
	Speed                     float32
	Count                     int32
	BlockHit                  BlockHit
	// Volume and Pitch are a BobberSplashed's sound.
	Volume, Pitch float32
}

// BobberOwner is the angler as the hook's tick sees them: the body the
// flight may not hit until clear, and whether still fishing (rod in hand,
// alive, close enough; shouldStopFishing), decided beforehand because
// those facts are under locks the tick does not take.
type BobberOwner struct {
	Body    ArrowTarget
	Fishing bool
}

// Bobber is one hook in a world. OwnerID is the angler, whom the flight
// cannot hit until clear. Dead marks a discarded hook: reeled in, the angler
// gone, or its ground life spent.
type Bobber struct {
	ID int32
	Place
	VX, VY, VZ float64
	Yaw, Pitch float32

	State BobberState
	// HookedID is the entity ID of the hooked body, 0 when free; the wire
	// metadata sends it plus one.
	HookedID int32
	// Nibble is the bite window's remaining ticks, else 0; moving through water
	// restarts the countdowns.
	Nibble int
	// Biting is DATA_BITING, during which the client dips its bobber.
	Biting bool
	// TimeUntilLured and TimeUntilHooked are the wait before a fish approaches,
	// and the approach.
	TimeUntilLured  int
	TimeUntilHooked int
	// Luck is the rod's Luck of the Sea, for the retrieve; LureTicks is the
	// rod's Lure in ticks, taken off each rolled wait.
	Luck      int
	LureTicks int

	TickCount int
	OwnerID   int32
	Dead      bool
	OnGround  bool
	// OpenWater is the treasure condition: open sky and a clear stretch of
	// source water. It stays true until an approach re-checks it.
	OpenWater bool

	// fishAngle is the approaching fish's heading, turned by a triangle roll
	// each tick so its wake circles the hook.
	fishAngle float32

	// Rand drives the hook's rolls, seeded by the caller so a replayed cast
	// catches the same.
	Rand *rand.Rand

	// The fluids in the hook's box, read each tick for the current's push.
	water, lava fluidTracker
	// wasInWater is Entity.wasTouchingWater.
	wasInWater bool

	life              int
	outOfWaterTime    int
	leftOwner         bool
	horizontalBlocked bool
	// fallDistance is the fall banked while descending in air, cleared on the
	// ground; the hook's NBT fall_distance.
	fallDistance float64
	// LandedFall is the fall a landing this tick ended, 0 when none.
	LandedFall float64
}

// CapFall is Entity.checkFallDistanceAccumulation.
func (b *Bobber) CapFall() { CapFallAccumulation(b.VY, &b.fallDistance) }

// FallDistance is the fall banked since the hook was last on the ground.
func (b *Bobber) FallDistance() float64 { return b.fallDistance }

// SetFallDistance sets the banked fall.
func (b *Bobber) SetFallDistance(d float64) { b.fallDistance = d }

// NewBobber casts a hook: from the angler's eye, a third of a block along
// the look, thrown along it with the rod's luck and lure. The pitch tangent
// is clamped at 5 so a straight-up or straight-down cast still flies.
// vanilla: FishingHook(Player, Level, int, int)
func NewBobber(id int32, x, eyeY, z, yaw, pitch float64, ownerID int32, seed int64, luck, lureTicks int) *Bobber {
	// The look's horizontal and the pitch tangent's rise, clamped to 5.
	radYaw := yaw * math.Pi / 180
	rayX, rayZ := -math.Sin(radYaw), math.Cos(radYaw)
	rayY := -math.Tan(pitch * math.Pi / 180)
	if rayY > 5 {
		rayY = 5
	}
	if rayY < -5 {
		rayY = -5
	}
	// Each axis: 0.6 of the line plus a triangle roll around 0.5 with
	// deviation 0.0172275 × 0.6, two draws per axis from the hook's source.
	r := NewDice(seed)
	dist := math.Sqrt(rayX*rayX + rayY*rayY + rayZ*rayZ)
	triangle := 0.0103365 * (r.Float64() - r.Float64())
	vx := rayX * (0.6/dist + 0.5 + triangle)
	triangle = 0.0103365 * (r.Float64() - r.Float64())
	vy := rayY * (0.6/dist + 0.5 + triangle)
	triangle = 0.0103365 * (r.Float64() - r.Float64())
	vz := rayZ * (0.6/dist + 0.5 + triangle)
	// The constructor's yRot/xRot from its own velocity.
	bodyYaw, bodyPitch := ArrowAngles(vx, vy, vz)
	return &Bobber{
		ID: id,
		X:  x + rayX*0.3,
		Y:  eyeY,
		Z:  z + rayZ*0.3,
		VX: vx, VY: vy, VZ: vz,
		Yaw:       bodyYaw,
		Pitch:     bodyPitch,
		State:     BobberFlying,
		OwnerID:   ownerID,
		OpenWater: true,
		Rand:      r,
		Luck:      luck,
		LureTicks: lureTicks,
	}
}

// Step advances the hook one tick (FishingHook.tick) against the targets,
// with its angler as owner: a grounded hook counts its life; the state
// machine runs (hooked rides its body, bobbing drifts and counts down,
// flying sweeps for bodies); then dry-flight gravity, the move, rotation
// easing and drag.
func (b *Bobber) Step(w World, owner *BobberOwner, targets []ArrowTarget) []BobberEvent {
	if b.Dead {
		return nil
	}
	// vanilla: FishingHook.getPlayerOwner, FishingHook.shouldStopFishing —
	// a hook whose angler is gone, dead, too far or rodless is removed
	// silently.
	if owner == nil || !owner.Fishing {
		b.Dead = true
		return nil
	}

	// vanilla: Entity.checkBelowWorld
	if b.Y < float64(w.MinY())-64 {
		b.Dead = true
		return nil
	}

	b.TickCount++

	// The owner exclusion settles before the state machine, where
	// Projectile.tick's checkLeftOwner runs.
	if !b.leftOwner {
		box := b.bobberBox()
		b.leftOwner = !ownerGroupTouching(&box, &owner.Body, owner.Body.RootVehicle, targets)
	}

	// The push a flowing fluid gives the hook, vanilla's baseTick, whose
	// splash reads the velocity from before the push.
	vx, vy, vz := b.VX, b.VY, b.VZ
	b.bobberFluidPush(w)
	var splash []BobberEvent
	if touching := b.water.height > 0; touching != b.wasInWater {
		b.wasInWater = touching
		if touching && b.TickCount > 1 {
			volume, pitch := splashSound(vx, vy, vz, b.Rand)
			splash = []BobberEvent{{Kind: BobberSplashed, X: b.X, Y: b.Y, Z: b.Z, Volume: volume, Pitch: pitch}}
		}
	}

	// vanilla: FishingHook.tick
	if b.OnGround {
		b.life++
		if b.life >= bobberLifeLimit {
			b.Dead = true
			return splash
		}
	} else {
		b.life = 0
	}

	// The water the hook itself stands in, FluidState at its own cell.
	bx, by, bz := int32(math.Floor(b.X)), int32(math.Floor(b.Y)), int32(math.Floor(b.Z))
	cell := fluidCellOf(w.BlockAt(bx, by, bz))
	liquidHeight := 0.0
	if cell.kind == fluidWater {
		liquidHeight = fluidHeightAt(w, bx, by, bz, cell)
	}
	inWater := liquidHeight > 0
	// vanilla: Entity.baseTick — touching water clears the carried fall
	// before this tick's move banks any.
	if inWater {
		b.fallDistance = 0
	}

	events := splash
	switch b.State {
	case BobberHookedInEntity:
		if body := bobberTargetByID(b.HookedID, targets); body != nil {
			// vanilla: FishingHook.tick
			b.SetPos((body.MinX+body.MaxX)/2, body.MinY+0.8*(body.MaxY-body.MinY), (body.MinZ+body.MaxZ)/2)
			return splash
		}
		// The body it held is gone from the world; the hook flies on.
		b.HookedID = 0
		b.State = BobberFlying
		return splash
	case BobberFlying:
		if b.HookedID != 0 {
			b.VX, b.VY, b.VZ = 0, 0, 0
			b.State = BobberHookedInEntity
			return splash
		}
		if inWater {
			b.VX *= bobberWaterEntryDragX
			b.VY *= bobberWaterEntryDragY
			b.VZ *= bobberWaterEntryDragZ
			b.State = BobberBobbing
			return splash
		}
		if hit, ok := b.sweepFlight(w, owner.Body.RootVehicle, targets); ok {
			events = append(events, hit)
		}
	default:
		// Bobbing: a spring around the surface with a random fifth of nextFloat,
		// sideways drift, and the extra sink of a bite.
		force := b.Y + b.VY - float64(by) - liquidHeight
		if math.Abs(force) < 0.01 {
			force += signum(force) * bobberSpringNudge
		}
		b.VX *= bobberBobDrift
		b.VZ *= bobberBobDrift
		b.VY -= force * b.Rand.Float64() * bobberBobSpring
		// Open water is re-checked only while a fish is near; it must be one
		// stretch of source water and sky for a treasure catch.
		if b.Nibble > 0 || b.TimeUntilHooked > 0 {
			b.OpenWater = b.OpenWater && b.outOfWaterTime < maxOutOfWaterTime &&
				b.openWaterAt(w, bx, by, bz)
		} else {
			b.OpenWater = true
		}
		if inWater {
			b.outOfWaterTime = max(0, b.outOfWaterTime-1)
			if b.Biting {
				b.VY -= bobberBitingSink * b.Rand.Float64() * b.Rand.Float64()
			}
			events = append(events, b.catchingFish(w, bx, by, bz)...)
		} else {
			b.outOfWaterTime = min(maxOutOfWaterTime, b.outOfWaterTime+1)
		}
	}

	// Dry-flight gravity, skipped while hooked; in water the cell already
	// set the vertical.
	if cell.kind != fluidWater && !b.OnGround && b.HookedID == 0 {
		b.VY -= bobberGravity
	}

	// The move, against blocks only; a hooked hook takes its body's place and
	// never gets here.
	b.move(w)

	// vanilla: Projectile.updateRotation
	b.Yaw = lerpRotation(b.Yaw, arrowTargetYaw(b.VX, b.VZ))
	b.Pitch = lerpRotation(b.Pitch, arrowTargetPitch(b.VY, b.VX, b.VZ))

	// A flying hook that landed stops (vanilla's onGround/horizontalCollision
	// clause).
	if b.State == BobberFlying && (b.OnGround || b.horizontalBlocked) {
		b.VX, b.VY, b.VZ = 0, 0, 0
	}
	b.VX *= bobberDrag
	b.VY *= bobberDrag
	b.VZ *= bobberDrag
	return events
}

// sweepFlight is the FLYING tick's checkCollision: block clip first, then
// the flight swept for bodies with a thrown body's margin, skipping the
// angler and its vehicle group until clear and never meeting a spectator.
// The hook does not stop on a hit: it names the body and rides it from the
// next tick (onHitEntity). It reports the block hit when no body was nearer.
// vanilla: FishingHook.checkCollision, ProjectileUtil.getHitResultOnMoveVector
func (b *Bobber) sweepFlight(w World, ownerVehicle int32, targets []ArrowTarget) (BobberEvent, bool) {
	if b.VX == 0 && b.VY == 0 && b.VZ == 0 {
		return BobberEvent{}, false
	}
	clip := clipProjectileFlight(w, b.X, b.Y, b.Z, b.VX, b.VY, b.VZ)
	blockHit := clip.ok
	endX, endY, endZ := clip.end(b.X, b.Y, b.Z, b.VX, b.VY, b.VZ)
	// The nearest crossing wins, not the list's first (getEntityHitResult).
	margin := thrownHitMargin(b.TickCount)
	dx, dy, dz := endX-b.X, endY-b.Y, endZ-b.Z
	var nearest *ArrowTarget
	nearestT := math.Inf(1)
	for i := range targets {
		target := &targets[i]
		if target.Unhittable {
			continue // vanilla: Entity.canBeHitByProjectile
		}
		if ownerExcluded(b.OwnerID, ownerVehicle, b.leftOwner, target) {
			continue // the angler's group, until the flight has left it
		}
		enter, _, ok := world.SegmentAABB(b.X, b.Y, b.Z, dx, dy, dz,
			target.MinX-margin, target.MinY-margin, target.MinZ-margin,
			target.MaxX+margin, target.MaxY+margin, target.MaxZ+margin)
		if !ok || enter <= 0 || enter >= nearestT {
			continue
		}
		nearest, nearestT = target, enter
	}
	if nearest != nil {
		b.HookedID = nearest.EntityID
		return BobberEvent{Kind: BobberHitTarget, X: b.X + dx*nearestT, Y: b.Y + dy*nearestT, Z: b.Z + dz*nearestT}, true
	}
	if !blockHit {
		return BobberEvent{}, false
	}
	if clip.border {
		reverseDeflection(&b.VX, &b.VY, &b.VZ, &b.Yaw, b.Rand, 0.2)
		return BobberEvent{}, false
	}
	return BobberEvent{Kind: BobberHitBlock, BlockHit: BlockHit{X: clip.bx, Y: clip.by, Z: clip.bz, Face: clip.face,
		HitX: endX, HitY: endY, HitZ: endZ, State: clip.state}}, true
}

// catchingFish runs the water's countdowns: lure wait, approach, bite. Rain
// at the surface speeds a fish and a roof slows it, each a roll drawn
// whether or not it applies. The approach and the lure emit wake
// particles; the bite opens the nibble window.
// vanilla: FishingHook.catchingFish
func (b *Bobber) catchingFish(w World, bx, by, bz int32) []BobberEvent {
	fishingSpeed := 1
	if b.Rand.Float64() < 0.25 && w.RainingAt(bx, by+1, bz) {
		fishingSpeed++
	}
	if b.Rand.Float64() < 0.5 && !canSeeSkyAt(w, bx, by+1, bz) {
		fishingSpeed--
	}
	var events []BobberEvent
	switch {
	case b.Nibble > 0:
		b.Nibble--
		if b.Nibble <= 0 {
			b.TimeUntilLured, b.TimeUntilHooked = 0, 0
			b.Biting = false
		}
	case b.TimeUntilHooked > 0:
		b.TimeUntilHooked -= fishingSpeed
		if b.TimeUntilHooked > 0 {
			events = append(events, b.approachWake(w, by)...)
		} else {
			b.Nibble = bobberNibbleMin + b.Rand.Intn(bobberNibbleRange)
			b.Biting = true
			// Setting DATA_BITING runs onSyncedDataUpdated on the server too, and
			// its dip overwrites the vertical velocity.
			b.VY = -0.4 * (0.6 + b.Rand.Float64()*0.4)
			events = append(events, BobberEvent{Kind: BobberBite, X: b.X, Y: b.Y, Z: b.Z})
			y := b.Y + 0.5
			width := float32(BobberHalfSize * 2)
			count := int32(1 + BobberHalfSize*2*20)
			events = append(events,
				BobberEvent{Kind: BobberParticles, Particle: protocol.ParticleBubble,
					X: b.X, Y: y, Z: b.Z, SpreadX: width, SpreadZ: width, Speed: 0.2, Count: count},
				BobberEvent{Kind: BobberParticles, Particle: protocol.ParticleFishing,
					X: b.X, Y: y, Z: b.Z, SpreadX: width, SpreadZ: width, Speed: 0.2, Count: count})
		}
	case b.TimeUntilLured > 0:
		b.TimeUntilLured -= fishingSpeed
		if ev, ok := b.lureTease(w, by); ok {
			events = append(events, ev)
		}
		if b.TimeUntilLured <= 0 {
			b.fishAngle = float32(b.Rand.Float64() * 360)
			b.TimeUntilHooked = bobberHookedMin + b.Rand.Intn(bobberHookedRange)
		}
	default:
		b.TimeUntilLured = bobberLuredMin + b.Rand.Intn(bobberLuredRange) - b.LureTicks
	}
	return events
}

// approachWake is a circling fish's wake: the heading turned by a triangle
// each tick, a bubble where it surfaces and two fishing ripples, all only
// over water.
// vanilla: FishingHook.catchingFish
func (b *Bobber) approachWake(w World, by int32) []BobberEvent {
	b.fishAngle += float32(9.188 * (b.Rand.Float64() - b.Rand.Float64()))
	angle := float64(b.fishAngle) * math.Pi / 180
	sin, cos := math.Sincos(angle)
	fishX := b.X + sin*float64(b.TimeUntilHooked)*0.1
	fishY := float64(by) + 1.0
	fishZ := b.Z + cos*float64(b.TimeUntilHooked)*0.1
	if !bobberWaterAt(w, fishX, fishY-1, fishZ) {
		return nil
	}
	var events []BobberEvent
	if b.Rand.Float64() < 0.15 {
		events = append(events, BobberEvent{Kind: BobberParticles, Particle: protocol.ParticleBubble,
			X: fishX, Y: fishY - 0.1, Z: fishZ,
			SpreadX: float32(sin), SpreadY: 0.1, SpreadZ: float32(cos), Count: 1})
	}
	px, pz := sin*0.04, cos*0.04
	return append(events,
		BobberEvent{Kind: BobberParticles, Particle: protocol.ParticleFishing,
			X: fishX, Y: fishY, Z: fishZ, SpreadX: float32(pz), SpreadY: 0.01, SpreadZ: float32(-px), Speed: 1.0},
		BobberEvent{Kind: BobberParticles, Particle: protocol.ParticleFishing,
			X: fishX, Y: fishY, Z: fishZ, SpreadX: float32(-pz), SpreadY: 0.01, SpreadZ: float32(px), Speed: 1.0})
}

// lureTease is a fish's splash during the wait, likelier as the wait runs
// out, plus the heading and approach rolled when the wait ends.
// vanilla: FishingHook.catchingFish
func (b *Bobber) lureTease(w World, by int32) (BobberEvent, bool) {
	tease := 0.15
	switch t := b.TimeUntilLured; {
	case t < 20:
		tease += float64(20-t) * 0.05
	case t < 40:
		tease += float64(40-t) * 0.02
	case t < 60:
		tease += float64(60-t) * 0.01
	}
	if b.Rand.Float64() >= tease {
		return BobberEvent{}, false
	}
	angle := b.Rand.Float64() * 360 * math.Pi / 180
	dist := 25 + b.Rand.Float64()*35
	fishX := b.X + math.Sin(angle)*dist*0.1
	fishY := float64(by) + 1.0
	fishZ := b.Z + math.Cos(angle)*dist*0.1
	if !bobberWaterAt(w, fishX, fishY-1, fishZ) {
		return BobberEvent{}, false
	}
	return BobberEvent{Kind: BobberParticles, Particle: protocol.ParticleSplash,
		X: fishX, Y: fishY, Z: fishZ, SpreadX: 0.1, SpreadZ: 0.1,
		Count: int32(2 + b.Rand.Intn(2))}, true
}

// bobberWaterAt reports whether the block at a point is water, for the wake
// and splash particles.
func bobberWaterAt(w World, x, y, z float64) bool {
	return registry.BlockOf(w.BlockAt(
		int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z)))) == registry.BlockWater
}

// openWaterAt is the treasure condition's tower: the four layers from one
// below the hook's cell to two above must each be a five-by-five of one
// kind, source water then only air and lily pads. The lowest layer must be
// water, so a hook over air fails; a solid cell, or water again above air,
// breaks it.
// vanilla: FishingHook.calculateOpenWater, FishingHook.getOpenWaterTypeForBlock
func (b *Bobber) openWaterAt(w World, bx, by, bz int32) bool {
	aboveWater := false
	for i := range int32(4) {
		layer := openWaterLayer(w, bx-2, by-1+i, bz-2, bx+2, bz+2)
		switch layer {
		case openWaterInvalid:
			return false
		case openWaterInside:
			if aboveWater {
				return false
			}
			aboveWater = false
		case openWaterAbove:
			if i == 0 {
				return false // the lowest layer must be the water itself
			}
			aboveWater = true
		}
	}
	return true
}

// The block a cell of an open-water layer reads as.
type openWaterKind uint8

const (
	openWaterAbove   openWaterKind = iota // air or a lily pad
	openWaterInside                       // a water source with no shape of its own
	openWaterInvalid                      // anything else
)

// openWaterLayer classifies one five-by-five layer; any disagreement makes
// it invalid.
func openWaterLayer(w World, x0, y, z0, x1, z1 int32) openWaterKind {
	kind := openWaterKind(255)
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			state := w.BlockAt(x, y, z)
			block := registry.BlockOf(state)
			cell := fluidCellOf(state)
			var each openWaterKind
			switch {
			case world.IsAirState(state) || block == registry.BlockLilyPad:
				each = openWaterAbove
			case cell.kind == fluidWater && cell.level == 0 && len(blockBoxes(state)) == 0:
				each = openWaterInside
			default:
				each = openWaterInvalid
			}
			if kind == 255 {
				kind = each
			} else if kind != each {
				return openWaterInvalid
			}
		}
	}
	if kind == 255 {
		return openWaterInvalid
	}
	return kind
}

// bobberTargetByID finds the hooked body, nil when it is gone, in another
// world, or no longer interactable (canInteractWithLevel: dead or
// spectator).
func bobberTargetByID(id int32, targets []ArrowTarget) *ArrowTarget {
	for i := range targets {
		if targets[i].EntityID == id && !targets[i].Unhittable {
			return &targets[i]
		}
	}
	return nil
}

// bobberFluidPush adds the current's push to the hook (baseTick;
// isPushedByFluid is true for it).
func (b *Bobber) bobberFluidPush(w World) {
	box := b.bobberCollisionBox()
	updateFluid(w, box, b.X, b.Y+BobberHeight*0.8, b.Z, true, &b.water, &b.lava)
	if b.water.height > 0 {
		if dx, dy, dz, any := b.water.currentImpulse(waterCurrent, b.VX, b.VZ); any {
			b.VX += dx
			b.VY += dy
			b.VZ += dz
		}
	}
	if b.lava.height > 0 {
		scale := lavaCurrent
		if w.FastLava() {
			scale = fastLavaCurrent
		}
		if dx, dy, dz, any := b.lava.currentImpulse(scale, b.VX, b.VZ); any {
			b.VX += dx
			b.VY += dy
			b.VZ += dz
		}
	}
}

// bobberCollisionBox is the hook's own box, the shape move carries.
func (b *Bobber) bobberCollisionBox() AABB {
	return AABB{
		minX: b.X - BobberHalfSize, minY: b.Y, minZ: b.Z - BobberHalfSize,
		maxX: b.X + BobberHalfSize, maxY: b.Y + BobberHeight, maxZ: b.Z + BobberHalfSize,
	}
}

// BlastBox is the hook's hit box.
func (b *Bobber) BlastBox() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	return b.bobberCollisionBox().Corners()
}

// bobberBox is the hook's box grown along the flight and by a block each
// way, for Projectile.isOutsideOwnerCollisionRange.
func (b *Bobber) bobberBox() ArrowTarget {
	return ArrowTarget{
		MinX: math.Min(b.X, b.X+b.VX) - BobberHalfSize - 1,
		MinY: math.Min(b.Y, b.Y+b.VY) - 1,
		MinZ: math.Min(b.Z, b.Z+b.VZ) - BobberHalfSize - 1,
		MaxX: math.Max(b.X, b.X+b.VX) + BobberHalfSize + 1,
		MaxY: math.Max(b.Y, b.Y+b.VY) + BobberHeight + 1,
		MaxZ: math.Max(b.Z, b.Z+b.VZ) + BobberHalfSize + 1,
	}
}

// move is Entity.move with collision: the vertical first (a stopped fall is
// the ground its life and catch need), then the larger horizontal axis and
// the other.
func (b *Bobber) move(w World) {
	oldY := b.Y
	box := b.bobberCollisionBox()
	box, _, yBlocked := clipMove(w, fallCtx(b.fallDistance), box, axisY, b.VY)
	b.OnGround = b.VY < 0 && yBlocked
	box, _, bx, bz := collideHorizontal(w, fallCtx(b.fallDistance), box, b.VX, b.VZ)
	b.horizontalBlocked = bx || bz
	x, z := box.centerXZ()
	b.SetPos(x, box.minY, z)
	b.LandedFall = bankFall(&b.fallDistance, oldY, b.Y, b.OnGround)
}

// signum is Math.signum, 0 for 0.
func signum(f float64) float64 {
	switch {
	case f > 0:
		return 1
	case f < 0:
		return -1
	}
	return 0
}
