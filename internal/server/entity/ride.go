package entity

import (
	"github.com/google/uuid"
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// Mob-on-mob riding: a spider jockey's skeleton, a chicken jockey's baby
// zombie. Unlike a boat (server state that places its seat), the vehicle
// here is a mob in the collection, which places the passenger after the
// vehicle's walk. The server tells clients; the finalize rolls are below.

// The jockey odds the finalizes roll.
const (
	// spiderJockeyChance is the one in a hundred a spider's finalize gives
	// its back a skeleton.
	// vanilla: Spider.finalizeSpawn
	spiderJockeyChance = 100
	// chickenJockeyOdds is each of a baby zombie's two chances (a nearby
	// chicken, then a fresh one).
	// vanilla: Zombie.finalizeSpawn
	chickenJockeyOdds = 0.05
)

// JockeyKind is a finalizeSpawn jockey decision, resolved by the server once
// the body is in the world, since this package cannot mint entity IDs or
// walk a herd. JockeyNone also marks a mob loaded from a chunk.
type JockeyKind uint8

const (
	// JockeyNone: no roll landed, or the finalize never ran.
	JockeyNone JockeyKind = iota
	// JockeyRider: stand a rider of the mob's RiderKind on this body's back.
	JockeyRider
	// JockeyNearbyChicken: seat this body on a nearby unridden chicken.
	JockeyNearbyChicken
	// JockeyFreshChicken: stand a fresh chicken beneath this body.
	JockeyFreshChicken
	// JockeyZombieNautilus: stand a zombie nautilus beneath an adult drowned.
	JockeyZombieNautilus
	// JockeyStructureNautilus keeps a structure-spawned vehicle persistent.
	JockeyStructureNautilus
	// JockeyBuiltRider: stand the mob's Rider, which its own finalize built
	// and finalized on the level's dice, on this body's back.
	JockeyBuiltRider
	// JockeyCamelHusk: stand a camel husk beneath this husk, and a parched
	// behind it.
	JockeyCamelHusk
)

// Ride seats passenger on vehicle and places it at once, so no rider is
// shown standing where it arrived. The list order is Set Passengers' order.
// vanilla: Entity.startRiding
func Ride(vehicle, passenger *Mob) {
	if !passenger.CanRide() {
		return
	}
	passenger.Vehicle = vehicle
	passenger.StartRiding()
	vehicle.Passengers = append(vehicle.Passengers, passenger)
	x, y, z := riderSeat(vehicle, passenger)
	passenger.SetPos(x, y, z)
	passenger.VX, passenger.VY, passenger.VZ = 0, 0, 0
	passenger.FallDistance = 0
}

// StartRiding applies the mob's state when boarding any kind of vehicle.
// vanilla: Shulker.startRiding
func (m *Mob) StartRiding() {
	m.RidesVehicle = true
	if m.IsShulker() {
		m.SetMetadataShulkerFace(faceDown)
	}
}

// CanRide reports whether the body permits any vehicle. The warden overrides
// vanilla's canRide with false.
// vanilla: Warden.canRide
func (m *Mob) CanRide() bool { return m.Type != TypeWarden }

// Dismount takes passenger off vehicle and clears its seat, leaving it where
// it sat; false if the vehicle was not carrying it.
func Dismount(vehicle, passenger *Mob) (ok bool) {
	for i, p := range vehicle.Passengers {
		if p != passenger {
			continue
		}
		vehicle.Passengers = append(vehicle.Passengers[:i], vehicle.Passengers[i+1:]...)
		passenger.Vehicle = nil
		passenger.RidesVehicle = false
		return true
	}
	return false
}

// ConvertMobRides moves a SINGLE conversion's first mob passenger and mob
// vehicle to the replacement, releasing the other riders with the old body.
// vanilla: ConversionType.SINGLE.convert, Entity.remove
func ConvertMobRides(from, to *Mob) {
	if len(from.Passengers) > 0 && from.Horse.Rider == uuid.Nil {
		first := from.Passengers[0]
		Dismount(from, first)
		first.BoardingCooldown = 0
		Ride(to, first)
	}
	for len(from.Passengers) > 0 {
		Dismount(from, from.Passengers[0])
	}
	if vehicle := from.Vehicle; vehicle != nil {
		Dismount(vehicle, from)
		Ride(vehicle, to)
	}
}

// DismountMob is Dismount for a mob taken off a mob: it ends beside an
// animal where the ground allows, else on top of the vehicle's box.
// vanilla: Entity.getDismountLocationForPassenger
func DismountMob(w rules.BoatWorld, vehicle, passenger *Mob) (ok bool) {
	if !Dismount(vehicle, passenger) {
		return false
	}
	dims := rules.PlayerDimensions{Width: passenger.width(), Height: passenger.height()}
	if x, y, z, found := animalDismountSpot(w, vehicle, []rules.DismountPose{{Dims: dims}}); found {
		passenger.SetPos(x, y, z)
		return true
	}
	passenger.SetPos(vehicle.X, vehicle.Y+vehicle.height(), vehicle.Z)
	return true
}

// animalDismountSpot is rules.AnimalDismountSpot for an Animal kind facing
// its yaw; false for other kinds.
// vanilla: Animal.getDismountLocationForPassenger
func animalDismountSpot(w rules.BoatWorld, vehicle *Mob, poses []rules.DismountPose) (x, y, z float64, ok bool) {
	if !mobIsAnimal(vehicle.Type) {
		return 0, 0, 0, false
	}
	// Direction.fromYRot, in the HORIZONTALS' order: south, west, north, east.
	steps := [4][2]int32{{0, 1}, {-1, 0}, {0, -1}, {1, 0}}
	d := steps[int32(math.Floor(float64(vehicle.Yaw)/90+0.5))&3]
	return rules.AnimalDismountSpot(w, vehicle.X, vehicle.Y, vehicle.Z, d[0], d[1], poses)
}

// PlayerDismountSpot is where a dismounted player stands: beside a horse or
// other animal if the ground allows, else on top of the mob; a happy ghast
// always sets them on top.
// vanilla: AbstractHorse.getDismountLocationForPassenger, Animal.getDismountLocationForPassenger, Entity.getDismountLocationForPassenger, HappyGhast.getDismountLocationForPassenger
func PlayerDismountSpot(w rules.BoatWorld, m *Mob, riderYaw float32, riderWidth, riderHeight float64) player.Position {
	if m.Type == TypeHappyGhast {
		return player.Position{X: m.X, Y: m.Y + m.height(), Z: m.Z}
	}
	if IsEquine(m.Type) {
		return HorseDismountSpot(w, m, riderWidth, riderHeight)
	}
	if m.Type == TypeStrider {
		return StriderDismountSpot(w, m, riderYaw)
	}
	if x, y, z, ok := animalDismountSpot(w, m, rules.PlayerDismountPoses); ok {
		return player.Position{X: x, Y: y, Z: z}
	}
	return player.Position{X: m.X, Y: m.Y + m.height(), Z: m.Z}
}

// RootVehicle is the bottom mount of a ride chain (itself if riding
// nothing), whose chunk and save the whole stack belongs to.
// vanilla: Entity.getRootVehicle
func (m *Mob) RootVehicle() *Mob {
	if m.Vehicle != nil {
		return m.Vehicle.RootVehicle()
	}
	return m
}

// KnownSpeed is the position change of this mob's root vehicle over its last
// tick, in blocks per tick.
// vanilla: KineticWeapon.getMotion, Entity.getKnownSpeed
func (m *Mob) KnownSpeed() [3]float64 {
	return m.RootVehicle().motion.speed
}

// controllingPassenger is the first mob rider eligible to steer this vehicle.
// vanilla: Mob.getControllingPassenger
func (m *Mob) controllingPassenger() *Mob {
	if m.NoAI || m.Horse.Rider != uuid.Nil || len(m.Passengers) == 0 ||
		m.Passengers[0].inTag(registry.EntityTypeTagIDNonControllingRider) {
		return nil
	}
	return m.Passengers[0]
}

// vanilla: Entity.isVehicle
func (m *Mob) isVehicle() bool {
	return m.Horse.Rider != uuid.Nil || len(m.Passengers) > 0 || len(m.Horse.ExtraRiderIDs) > 0
}

func (m *Mob) hasPlayerPassenger(id int32) bool {
	if id != 0 && (m.Horse.RiderID == id || slices.Contains(m.Horse.ExtraRiderIDs, id)) {
		return true
	}
	for _, p := range m.Passengers {
		if p.hasPlayerPassenger(id) {
			return true
		}
	}
	return false
}

// PassengerIDs lists riders in Set Passengers order: the steering player,
// then mobs, then other players.
// vanilla: Entity.addPassenger puts a first player at the head of the list
func (m *Mob) PassengerIDs() []int32 {
	ids := make([]int32, 0, 1+len(m.Passengers)+len(m.Horse.ExtraRiderIDs))
	if m.Horse.Rider != uuid.Nil {
		ids = append(ids, m.Horse.RiderID)
	}
	for _, p := range m.Passengers {
		ids = append(ids, p.ID)
	}
	return append(ids, m.Horse.ExtraRiderIDs...)
}

// riderSeat is a passenger's feet on the vehicle: the mount's passenger
// point less the rider's vehicle attachment, turned by the mount's yaw. It
// is server bookkeeping (reach and body tests); the client draws riders from
// its own attachments.
// vanilla: Entity.positionRider, getPassengerAttachmentPoint,
// getVehicleAttachmentPoint
func riderSeat(vehicle, passenger *Mob) (x, y, z float64) {
	ax, ay, az := riderVehiclePoint(passenger)
	return seatAt(vehicle, 0, ax, ay, az)
}

// MinecartMobSeat is the feet of a mob riding a cart standing at (x, y, z):
// the cart's passenger point less the mob's vehicle attachment.
// vanilla: Entity.positionRider, EntityType.MINECART passengerAttachments
func MinecartMobSeat(m *Mob, x, y, z float64) (float64, float64, float64) {
	ax, ay, az := riderVehiclePoint(m)
	return x - ax, y + MinecartSeatY - ay, z - az
}

// playerVehicleY is the height of a player's vehicle attachment.
// vanilla: Avatar.DEFAULT_VEHICLE_ATTACHMENT
const playerVehicleY = 0.6

// CushionSeatY is the feet of a player sitting on a cushion standing at y:
// the default passenger point at the box's top less the player's vehicle
// attachment. vanilla: Entity.positionRider, EntityAttachment.PASSENGER
func CushionSeatY(y float64) float64 {
	return y + CushionHeight - playerVehicleY
}

// OffRemovedVehicle is where a body at (x, y, z) stands once the
// vehicle under them, its feet at vehicleY, is removed: their own column at
// the higher feet, moved to the nearest spot their box clears, if any.
// vanilla: LivingEntity.dismountVehicle
func OffRemovedVehicle(w World, x, y, z, vehicleY, width, height float64) (float64, float64, float64) {
	y = max(y, vehicleY)
	if width > 4 || height > 4 {
		return x, y, z
	}
	half := height / 2
	allowed := NewAABB(x-width/2, y, z-width/2, x+width/2, y+height, z+width/2)
	if c, ok := findFreePosition(w, allowed, [3]float64{x, y + half, z}, width, height, width); ok {
		return c[0], c[1] - half, c[2]
	}
	return x, y, z
}

// PlayerSeat is where the feet of the mob's passenger at index stand.
// vanilla: Entity.positionRider
func PlayerSeat(m *Mob, index int) (x, y, z float64) {
	return seatAt(m, index, 0, playerVehicleY, 0)
}

// seatAt is the vehicle's passenger point at index less the rider's own
// attachment (ax, ay, az), turned by the vehicle's yaw.
func seatAt(vehicle *Mob, index int, ax, ay, az float64) (x, y, z float64) {
	px, py, pz := vehiclePassengerPoint(vehicle, index)
	dx, dy, dz := px-ax, py-ay, pz-az
	rad := float64(vehicle.Yaw) * math.Pi / 180
	sin, cos := math.Sin(rad), math.Cos(rad)
	return vehicle.X + dx*cos - dz*sin, vehicle.Y + dy, vehicle.Z + dx*sin + dz*cos
}

// passengerPoints are the types' first passenger attachments in the type's
// frame at adult scale. Unlisted kinds use vanilla's default, the top of the
// body.
// vanilla: EntityTypes passengerAttachments
var passengerPoints = [registry.EntityTypeCount][3]float64{
	TypeZombieNautilus: {0, float64(float32(1.1375)), 0},
	TypeNautilus:       {0, float64(float32(1.1375)), 0},
	TypeCow:            {0, 1.36875, 0},
	TypeMooshroom:      {0, 1.36875, 0},
	TypePig:            {0, 0.86875, 0},
	TypeSheep:          {0, 1.2375, 0},
	TypeChicken:        {0, 0.7, -0.1},
	TypeTurtle:         {0, 0.55625, -0.25},
	TypeHorse:          {0, 1.44375, 0},
	TypeDonkey:         {0, 1.1125, 0},
	TypeMule:           {0, 1.2125, 0},
	TypeLlama:          {0, 1.37, -0.3},
	TypeTraderLlama:    {0, 1.37, -0.3},
	TypeSkeletonHorse:  {0, 1.31875, 0},
	TypeZombieHorse:    {0, 1.31875, 0},
	TypeZombie:         {0, 2.0125, 0},
	TypeSpider:         {0, 0.765, 0},
	TypeEnderman:       {0, 2.80625, 0},
	TypeHusk:           {0, 2.075, 0},
	TypeDrowned:        {0, 2.0125, 0},
	TypeZombieVillager: {0, 2.125, 0},
	TypePhantom:        {0, 0.3375, 0},
	TypeWolf:           {0, 0.81875, -0.0625},
	TypeFox:            {0, 0.6375, -0.25},
	TypeEndermite:      {0, 0.2375, 0},
}

// happyGhastSeats are the happy ghast's four passenger attachments: front,
// left, back, right.
// vanilla: EntityTypes.HAPPY_GHAST passengerAttachments
var happyGhastSeats = [...][3]float64{{0, 4.0, 1.7}, {-1.7, 4.0, 0}, {0, 4.0, -1.7}, {1.7, 4.0, 0}}

// vehiclePassengerPoint is the mount's seat for the passenger at index in
// its own frame, scaled for a baby; an index past the last seat takes it.
// vanilla: Entity.getPassengerAttachmentPoint, EntityAttachments.getClamped
func vehiclePassengerPoint(m *Mob, index int) (x, y, z float64) {
	// vanilla: Strider.BABY_DIMENSIONS
	if (m.Type == TypeNautilus || m.Type == TypePig) && m.IsBaby() {
		return 0, 0.5, 0
	}
	if m.IsStrider() && m.IsBaby() {
		return 0, 0.65625, 0
	}
	// vanilla: Fox.BABY_DIMENSIONS
	if m.Type == TypeFox && m.IsBaby() {
		return 0, 0.375, 0
	}
	if IsCamel(m.Type) {
		return m.camelSeat(index)
	}
	var p [3]float64
	if uint32(m.Type) < uint32(len(passengerPoints)) {
		p = passengerPoints[m.Type]
	}
	// Explicit attachments all have positive height; zero uses the body top.
	ok := p != [3]float64{}
	if m.Type == TypeHappyGhast {
		p, ok = happyGhastSeats[min(max(index, 0), len(happyGhastSeats)-1)], true
	}
	if !ok {
		return 0, m.height(), 0
	}
	f := 1.0
	if k := m.kind(); m.IsBaby() && k.babyHeight > 0 && k.height > 0 {
		f = k.babyHeight / k.height
	}
	return p[0] * f, p[1] * f, p[2] * f
}

// riderVehiclePoint is the rider's vehicle attachment in its own frame; a
// kind without one anchors at its feet, which the jockey riders here
// override.
// vanilla: Entity.getVehicleAttachmentPoint
func riderVehiclePoint(m *Mob) (x, y, z float64) {
	switch m.family() {
	case TypeSkeleton:
		return 0, 0.7, 0
	case TypeZombie:
		if m.IsBaby() {
			return 0, 0.1875, 0
		}
		return 0, 0.7, 0
	}
	return 0, 0, 0
}

// seatPassenger places p at the vehicle's current seat for it.
// vanilla: Entity.positionRider
func (m *Mob) seatPassenger(p *Mob) {
	p.SetPos(riderSeat(m, p))
	p.FallDistance = 0
}

// endSeats ends the rides of a dead or gone body, given and taken, and
// reports each vehicle whose list changed.
// vanilla: Entity.remove, whose stopRiding leaves the bodies where they were
func (m *Mob) endSeats() []MobEvent {
	var events []MobEvent
	if veh := m.Vehicle; veh != nil {
		Dismount(veh, m)
		events = append(events, mountEvent(m, veh, MobDismounted), MobEvent{Mob: veh, Kind: MobPassengersChanged})
	}
	if len(m.Passengers) == 0 {
		return events
	}
	for _, p := range m.Passengers {
		events = append(events, mountEvent(p, m, MobDismounted))
		p.Vehicle = nil
		p.RidesVehicle = false
	}
	m.Passengers = nil
	return append(events, MobEvent{Mob: m, Kind: MobPassengersChanged})
}

// rollJockey is the spider's 1-in-100 skeleton rider, and the zombie
// horse's zombie when the pack comes from the natural spawn pass. A cave
// spider's finalize overrides the spider's without calling it, so only the
// spider rolls.
// vanilla: Spider.finalizeSpawn, CaveSpider.finalizeSpawn, ZombieHorse.finalizeSpawn
func (m *Mob) rollJockey(group *SpawnGroup) {
	if m.Type == TypeZombieHorse {
		// vanilla: only EntitySpawnReason.NATURAL rolls, never a summon
		if group != nil && group.natural {
			m.Jockey, m.RiderKind = JockeyRider, TypeZombie
		}
		return
	}
	if m.Type == TypeSpider && m.Rand.Intn(spiderJockeyChance) == 0 {
		m.Jockey, m.RiderKind = JockeyRider, TypeSkeleton
	}
}

// rollChickenJockey is a baby zombie's 1 in 20 for a nearby chicken, then
// another 1 in 20 for a fresh one.
// vanilla: Zombie.finalizeSpawn
func (m *Mob) rollChickenJockey(dice Dice) {
	if !m.is(TypeZombie) || !m.IsBaby() {
		return
	}
	if float64(dice.NextFloat()) < chickenJockeyOdds {
		m.Jockey = JockeyNearbyChicken
	} else if float64(dice.NextFloat()) < chickenJockeyOdds {
		m.Jockey = JockeyFreshChicken
	}
}

// NearbyJockeyChicken is the first live, unridden, non-riding chicken in
// herd order whose box meets the baby's grown by 5 sideways and 3 up; nil if
// none.
// vanilla: Zombie.finalizeSpawn, EntitySelector.ENTITY_NOT_BEING_RIDDEN
func NearbyJockeyChicken(baby *Mob, herd []*Mob) *Mob {
	minX := baby.X - baby.width()/2 - 5
	maxX := baby.X + baby.width()/2 + 5
	minY := baby.Y - 3
	maxY := baby.Y + baby.height() + 3
	minZ := baby.Z - baby.width()/2 - 5
	maxZ := baby.Z + baby.width()/2 + 5
	for _, other := range herd {
		if other == baby || other.Type != TypeChicken || !other.Alive() ||
			len(other.Passengers) > 0 || other.RidesVehicle {
			continue
		}
		hw := other.width() / 2
		if other.X+hw <= minX || other.X-hw >= maxX ||
			other.Z+hw <= minZ || other.Z-hw >= maxZ ||
			other.Y+other.height() <= minY || other.Y >= maxY {
			continue
		}
		return other
	}
	return nil
}

// spiderEffectOdds is the pre-multiplier chance a spider pack gets its
// effect on hard.
// vanilla: Spider.finalizeSpawn
const spiderEffectOdds = 0.1

// finalizeSpiderEffect gives a spider the pack's permanent effect: the first
// of a pack rolls it (hard only) and the rest share it; a pack another kind
// claimed rolls nothing; a lone spider rolls for itself. CaveSpider skips
// this.
// vanilla: Spider.finalizeSpawn, SpiderEffectsGroupData.setRandomEffect
func (m *Mob) finalizeSpiderEffect(group *SpawnGroup, local rules.LocalDifficulty) {
	if m.Type != TypeSpider {
		return
	}
	if group == nil {
		group = &SpawnGroup{rng: GoDice{m.Rand}}
	}
	if group.claimed && !group.spider {
		return
	}
	if !group.claimed {
		group.claimed, group.spider = true, true
		if local.Hard && group.rng.NextFloat() < spiderEffectOdds*local.Special {
			// selection 0-1 speed, 2 strength, 3 regeneration, 4 invisibility
			effects := [...]int32{registry.MobEffectSpeed, registry.MobEffectSpeed, registry.MobEffectStrength, registry.MobEffectRegeneration, registry.MobEffectInvisibility}
			group.spiderEffect = effects[group.rng.NextIntN(int32(len(effects)))]
			group.hasEffect = true
		}
	}
	if group.hasEffect {
		m.AddEffect(effect.New(group.spiderEffect, effect.Infinite, 0, false, true, true))
	}
}

// mountEvent snapshots the vehicle's origin before the rider is repositioned.
func mountEvent(rider, vehicle *Mob, kind MobEventKind) MobEvent {
	return MobEvent{Mob: rider, Kind: kind, X: vehicle.X, Y: vehicle.Y, Z: vehicle.Z}
}
