package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Schooling fish swim on their own move control,
// flee players, flop on dry ground and, as schooling fish, follow a leader.
// vanilla: AbstractFish, AbstractSchoolingFish, Cod, Salmon, TropicalFish

const (
	TypeCod          = registry.EntityTypeCod
	TypeSalmon       = registry.EntityTypeSalmon
	TypeTropicalFish = registry.EntityTypeTropicalFish
)

// The kinds are registered here, not in mob.go's table, so the fish stay in
// one file.
func init() {
	// vanilla: EntityTypes.COD and AbstractFish.createAttributes; the 0.7
	// speed is Mob.createMobAttributes'.
	kinds[TypeCod] = &kind{
		name: "Cod", width: 0.5, height: 0.3, eyeHeight: 0.195,
		maxHealth: 3, speed: 0.7, water: true,
		sounds: MobSounds{
			Ambient: registry.SoundEntityCodAmbient,
			Hurt:    registry.SoundEntityCodHurt,
			Death:   registry.SoundEntityCodDeath,
		},
		ambientInterval: 120, // WaterAnimal.getAmbientSoundInterval
	}
	// vanilla: EntityTypes.SALMON
	kinds[TypeSalmon] = &kind{
		name: "Salmon", width: 0.7, height: 0.4, eyeHeight: 0.26,
		maxHealth: 3, speed: 0.7, water: true,
		sounds: MobSounds{
			Ambient: registry.SoundEntitySalmonAmbient,
			Hurt:    registry.SoundEntitySalmonHurt,
			Death:   registry.SoundEntitySalmonDeath,
		},
		ambientInterval: 120,
	}

	// vanilla: EntityTypes.COD and SALMON clientTrackingRange(4)
	mobTracking[TypeCod] = tracking{rangeChunks: 4, updateInterval: 3}
	mobTracking[TypeSalmon] = tracking{rangeChunks: 4, updateInterval: 3}
}

// Salmon sizes, the ids of Salmon.Variant.
const (
	SalmonSmall  = 0
	SalmonMedium = 1
	SalmonLarge  = 2
)

// salmonScale is Salmon.Variant's bounding box scale, by size id.
var salmonScale = [3]float64{0.5, 1.0, 1.5}

// The fish's numbers: the flop and the swim goal's cadence, the flee's pace
// and reach, and the school's.
const (
	fishFlopUp       = 0.4  // AbstractFish.aiStep
	fishFlopSide     = 0.05 // AbstractFish.aiStep
	fishSwimEvery    = 40   // FishSwimGoal's interval
	fishSwimSpreadXZ = 10   // BehaviorUtils.getRandomSwimmablePos
	fishSwimSpreadY  = 7
	fishPanicPace    = 1.25 // PanicGoal(this, 1.25)
	fishPanicSpread  = 5    // PanicGoal.findRandomPosition
	fishPanicSpreadY = 4
	fishAvoidRange   = 8.0 // AvoidEntityGoal(.. 8.0f, 1.6, 1.4)
	fishAvoidWalk    = 1.6
	fishAvoidSprint  = 1.4
	fishAvoidAwayXZ  = 16 // DefaultRandomPos.getPosAway(mob, 16, 7, ..)
	fishAvoidAwayY   = 7
	fishRecalc       = 10  // FollowFlockLeaderGoal's path interval
	fishFlockScan    = 8.0 // the box the school looks for a leader in
	fishLeaderRange2 = 121 // AbstractSchoolingFish.inRangeOfLeader
)

// fishState is what a fish keeps beyond the common mob: where it came from
// and its school.
type fishState struct {
	speed      float64 // the move control's eased pace
	leader     *Mob
	schoolSize int
	nextFlock  int // FollowFlockLeaderGoal.nextStartTick
	// moveDist and nextStep are Entity.moveDist and nextStep, the swim
	// sound's distance mark.
	moveDist, nextStep float32
}

// IsFish reports whether the mob is a cod, salmon, tropical fish or pufferfish.
func (m *Mob) IsFish() bool { return FishKind(m.Type) }

// FishKind reports whether the type is a cod, salmon, tropical fish or pufferfish.
func FishKind(t MobType) bool {
	return t == TypeCod || t == TypeSalmon || t == TypeTropicalFish || t == TypePufferfish
}

// schoolingFishKinds are the AbstractSchoolingFish types: every fish but the pufferfish.
var schoolingFishKinds = []MobType{TypeCod, TypeSalmon, TypeTropicalFish}

// maxSchoolSize is AbstractSchoolingFish.getMaxSchoolSize: the salmon's five,
// otherwise AbstractFish.getMaxSpawnClusterSize' eight.
func (m *Mob) maxSchoolSize() int {
	if m.Type == TypeSalmon {
		return 5
	}
	return 8
}

// initFish is the constructor's share for a fish.
func (m *Mob) initFish() {
	m.fish.schoolSize = 1
	m.fish.nextFlock = reducedTickDelay(200 + m.Rand.Intn(200)%20)
	if m.Type == TypeSalmon {
		m.SalmonSize = SalmonMedium
	}
}

// finalizeFish is AbstractFish's and Salmon's finalizeSpawn: the salmon's size
// roll, then the school the spawn pack forms around its first member.
// vanilla: Salmon.finalizeSpawn, AbstractSchoolingFish.finalizeSpawn
func (m *Mob) finalizeFish(group *SpawnGroup) {
	if m.Type == TypeSalmon {
		// WeightedList small 30, medium 50, large 15
		switch r := m.Rand.Intn(95); {
		case r < 30:
			m.SalmonSize = SalmonSmall
		case r < 80:
			m.SalmonSize = SalmonMedium
		default:
			m.SalmonSize = SalmonLarge
		}
	}
	if group != nil {
		if group.school == nil {
			group.school = m
		} else {
			m.startFollowing(group.school)
		}
	}
	if m.Type == TypeTropicalFish {
		m.finalizeTropicalFish(group)
	}
}

func (m *Mob) isFollower() bool {
	return m.fish.leader != nil && m.fish.leader.Alive()
}

func (m *Mob) hasFollowers() bool { return m.fish.schoolSize > 1 }

func (m *Mob) canBeFollowed() bool {
	return m.hasFollowers() && m.fish.schoolSize < m.maxSchoolSize()
}

func (m *Mob) startFollowing(leader *Mob) {
	m.fish.leader = leader
	leader.fish.schoolSize++
}

func (m *Mob) stopFollowing() {
	if m.fish.leader != nil {
		m.fish.leader.fish.schoolSize--
		m.fish.leader = nil
	}
}

// schoolmates are the live fish of the mob's own type inside the box eight
// blocks out on every side of its own, itself included.
func (m *Mob) schoolmates(t *tickView) []*Mob {
	box := m.mobBox()
	var out []*Mob
	for _, o := range t.others {
		if o.Type != m.Type || !o.Alive() {
			continue
		}
		b := o.mobBox()
		if b.maxX < box.minX-fishFlockScan || b.minX > box.maxX+fishFlockScan ||
			b.maxY < box.minY-fishFlockScan || b.minY > box.maxY+fishFlockScan ||
			b.maxZ < box.minZ-fishFlockScan || b.minZ > box.maxZ+fishFlockScan {
			continue
		}
		out = append(out, o)
	}
	return out
}

// fishTick is AbstractSchoolingFish.tick: a leader with no schoolmate left in
// reach forgets its followers, one tick in two hundred.
func (m *Mob) fishTick(t *tickView) {
	if m.hasFollowers() && m.Rand.Intn(200) == 1 && len(m.schoolmates(t)) <= 1 {
		m.fish.schoolSize = 1
	}
}

// fishMoveStep is the fish's aiStep and travel: flop on dry ground, follow
// the path, steer with FishMoveControl, and move by
// AbstractFish.travelInWater.
// vanilla: AbstractFish.aiStep, AbstractFish.FishMoveControl.tick,
// AbstractFish.travelInWater
func (m *Mob) fishMoveStep(t *tickView) []MobEvent {
	var events []MobEvent
	m.fishTick(t)
	if m.Type == TypePufferfish {
		events = m.pufferStep(t, events)
	}
	if !m.InWater() {
		events = m.fishFlop(events)
		return m.step(t.w, events)
	}

	n := &m.navOf().nav
	m.swimNavTick(t.w)
	if m.waterFluid.eyesIn {
		m.VY += 0.005
	}
	if len(n.path) == 0 {
		m.fish.speed = 0
	} else {
		target := n.speedFactor * m.kind().speed
		m.fish.speed += (target - m.fish.speed) * 0.125
		dx, dy, dz := n.flyX-m.X, n.flyY-m.Y, n.flyZ-m.Z
		if dy != 0 {
			dd := math.Sqrt(dx*dx + dy*dy + dz*dz)
			m.VY += m.fish.speed * (dy / dd) * 0.1
		}
		if dx != 0 || dz != 0 {
			m.Yaw = turnToward(m.Yaw, float32(yawToward(dx, dz)), 90)
		}
	}

	// moveRelative(0.01, (0, 0, speed)): the input is normalized past one
	// and dropped under 1e-7 squared.
	if sq := m.fish.speed * m.fish.speed; sq >= 1e-7 {
		push := m.fish.speed * 0.01
		if sq > 1 {
			push = 0.01
		}
		sin, cos := math.Sincos(float64(m.Yaw) * math.Pi / 180)
		m.VX += -push * sin
		m.VZ += push * cos
	}
	px, pz := m.X, m.Z
	m.move(t.w, m.VX, m.VY, m.VZ)
	events = m.swimSound(t.w, math.Hypot(m.X-px, m.Z-pz), events)
	m.VX, m.VY, m.VZ = m.VX*0.9, m.VY*0.9-0.005, m.VZ*0.9
	return events
}

// swimSound is Entity.applyMovementEmissionAndPlaySound in water: horizontal
// movement adds to the step distance, and each whole block past the last
// mark plays a splash, quieter for a slower body.
// vanilla: Entity.applyMovementEmissionAndPlaySound, Entity.waterSwimSound,
// Entity.playSwimSound
func (m *Mob) swimSound(w World, moved float64, events []MobEvent) []MobEvent {
	m.fish.moveDist += float32(moved * 0.6)
	if m.fish.moveDist <= m.fish.nextStep {
		return events
	}
	// A body resting on the bed makes the step sound, which a fish has none of.
	if m.OnGround {
		m.fish.nextStep = float32(int32(m.fish.moveDist) + 1)
		return events
	}
	if rules.IsWater(w.BlockAt(int32(math.Floor(m.X)), int32(math.Floor(m.Y-0.2)), int32(math.Floor(m.Z)))) {
		m.fish.nextStep = float32(int32(m.fish.moveDist) + 1)
		volume := float32(math.Min(1, math.Sqrt(m.VX*m.VX*0.2+m.VY*m.VY+m.VZ*m.VZ*0.2)*0.35))
		pitch := 1 + (m.Rand.Float32()-m.Rand.Float32())*0.4
		events = append(events, MobEvent{Mob: m, Kind: MobSwam, Volume: volume, Pitch: pitch})
	}
	return events
}

// seekWater orders a swim to a cell's centre at a pace, the path the
// navigation then walks.
func (m *Mob) seekWater(w World, cell Waypoint, pace float64) {
	m.swimTo(w, float64(cell.X)+0.5, float64(cell.Y), float64(cell.Z)+0.5, pace)
}

// fishOrdered reports whether the navigation still has a way to walk.
func (m *Mob) fishOrdered() bool { return len(m.navOf().nav.path) > 0 }

// swimmable is BlockBehaviour.isPathfindable(WATER) at a cell.
func swimmable(w World, c Waypoint) bool { return waterPathfindable(w.BlockAt(c.X, c.Y, c.Z)) }

// notSolidRender is WaterBoundPathNavigation.isStableDestination.
func notSolidRender(w World, c Waypoint) bool {
	return !registry.IsSolidRender(w.BlockAt(c.X, c.Y, c.Z))
}

// fishRandomPos is DefaultRandomPos.getPos for a water body: the first of ten
// offsets that is no solid block, all weighing nothing.
func fishRandomPos(w World, m *Mob, xz, y int) (Waypoint, bool) {
	for range defaultRandomTries {
		if c := randomDirection(m, xz, y); notSolidRender(w, c) {
			return c, true
		}
	}
	return Waypoint{}, false
}

// fishSwimmablePos is BehaviorUtils.getRandomSwimmablePos: up to ten draws
// until one is water. The last draw is returned even if not water; the
// navigation then swims to its nearest node.
func fishSwimmablePos(w World, m *Mob, xz, y int) (Waypoint, bool) {
	c, ok := fishRandomPos(w, m, xz, y)
	for i := 0; ok && !swimmable(w, c) && i < 10; i++ {
		c, ok = fishRandomPos(w, m, xz, y)
	}
	return c, ok
}

// fishPosAway is DefaultRandomPos.getPosAway: the first of ten positions
// within a quarter turn of away-from-the-threat that is not solid.
func fishPosAway(w World, m *Mob, xz, y int, fromX, fromZ float64) (Waypoint, bool) {
	centre := math.Atan2(m.Z-fromZ, m.X-fromX) - math.Pi/2
	for range defaultRandomTries {
		angle := centre + float64(2*m.Rand.Float32()-1)*(math.Pi/2)
		dist := math.Sqrt(m.Rand.Float64()) * float64(xz) * math.Sqrt2
		dx, dz := -dist*math.Sin(angle), dist*math.Cos(angle)
		if math.Abs(dx) > float64(xz) || math.Abs(dz) > float64(xz) {
			continue
		}
		dy := m.Rand.Intn(2*y+1) - y
		c := cellAt(dx+m.X, float64(dy)+m.Y, dz+m.Z)
		if notSolidRender(w, c) {
			return c, true
		}
	}
	return Waypoint{}, false
}

// fishGoals is AbstractFish.registerGoals and, for the schooling fish, the
// flock goal AbstractSchoolingFish adds.
func fishGoals(m *Mob) []Goal {
	return []Goal{
		&fishPanicGoal{m: m},
		&fishAvoidGoal{m: m},
		&fishSwimGoal{m: m},
		&flockGoal{m: m, next: m.fish.nextFlock},
	}
}

// fishPanicGoal is PanicGoal(1.25) over a swimming body.
type fishPanicGoal struct {
	m      *Mob
	target Waypoint
}

func (g *fishPanicGoal) Priority() int    { return 0 }
func (g *fishPanicGoal) Flags() GoalFlags { return GoalMove }

func (g *fishPanicGoal) CanStart(t *tickView) bool {
	if !g.m.lastDamageSource().InTag(panicCauses) {
		return false
	}
	c, ok := fishSwimmablePos(t.w, g.m, fishPanicSpread, fishPanicSpreadY)
	g.target = c
	return ok
}

func (g *fishPanicGoal) Start(t *tickView)             { g.m.seekWater(t.w, g.target, fishPanicPace) }
func (g *fishPanicGoal) ShouldContinue(*tickView) bool { return g.m.fishOrdered() }
func (g *fishPanicGoal) Tick(*tickView)                {}
func (g *fishPanicGoal) Stop(*tickView)                { g.m.stopSeeking() }
func (g *fishPanicGoal) RequiresEveryTick() bool       { return false }

// fishAvoidGoal is AvoidEntityGoal(Player, 8, 1.6, 1.4) with the players that
// are neither creative nor spectating.
type fishAvoidGoal struct {
	m    *Mob
	path []Waypoint
	end  Waypoint
	from int32 // entity ID of the fled player; the snapshots are reused between ticks
}

func (g *fishAvoidGoal) Priority() int    { return 2 }
func (g *fishAvoidGoal) Flags() GoalFlags { return GoalMove }

func (g *fishAvoidGoal) CanStart(t *tickView) bool {
	if !playersThreaten(t) {
		return false
	}
	var nearest *Player
	best := math.Inf(1)
	for i := range t.players {
		p := &t.players[i]
		if p.Creative || math.Abs(p.Y-g.m.Y) > 3+g.m.height() {
			continue
		}
		d := Dist2(g.m.X, g.m.Y, g.m.Z, p.X, p.Y, p.Z)
		if d < best && g.m.noticesPlayer(t, p, fishAvoidRange, true) {
			best, nearest = d, p
		}
	}
	if nearest == nil {
		return false
	}
	c, ok := fishPosAway(t.w, g.m, fishAvoidAwayXZ, fishAvoidAwayY, nearest.X, nearest.Z)
	if !ok {
		return false
	}
	// the threat must not be nearer the point than it is to the body
	px, py, pz := float64(c.X)+0.5, float64(c.Y), float64(c.Z)+0.5
	if Dist2(nearest.X, nearest.Y, nearest.Z, px, py, pz) < best {
		return false
	}
	// vanilla: AvoidEntityGoal.canUse plans the path with a reach of zero
	path, end, ok := g.m.createSwimPath(t.w, px, py, pz, 0)
	if !ok {
		return false
	}
	g.path, g.end, g.from = path, end, nearest.EntityID
	return true
}

func (g *fishAvoidGoal) Start(*tickView) { g.m.moveTo(g.path, g.end, fishAvoidWalk) }

func (g *fishAvoidGoal) ShouldContinue(*tickView) bool { return g.m.fishOrdered() }

// Tick keeps vanilla's pairing of the sprint pace with the near range.
func (g *fishAvoidGoal) Tick(t *tickView) {
	pace := fishAvoidWalk
	for i := range t.players {
		if p := &t.players[i]; p.EntityID == g.from {
			if Dist2(g.m.X, g.m.Y, g.m.Z, p.X, p.Y, p.Z) < 49 {
				pace = fishAvoidSprint
			}
			break
		}
	}
	g.m.navOf().nav.speedFactor = pace
}

func (g *fishAvoidGoal) Stop(*tickView)          { g.m.stopSeeking() }
func (g *fishAvoidGoal) RequiresEveryTick() bool { return false }

// fishSwimGoal is FishSwimGoal, a RandomSwimmingGoal(1.0, 40) a follower does
// not run.
type fishSwimGoal struct {
	m      *Mob
	target Waypoint
}

func (g *fishSwimGoal) Priority() int    { return 4 }
func (g *fishSwimGoal) Flags() GoalFlags { return GoalMove }

func (g *fishSwimGoal) CanStart(t *tickView) bool {
	if g.m.isFollower() || g.m.NoActionTicks >= strollIdleLimit {
		return false
	}
	if g.m.Rand.Intn(reducedTickDelay(fishSwimEvery)) != 0 {
		return false
	}
	c, ok := fishSwimmablePos(t.w, g.m, fishSwimSpreadXZ, fishSwimSpreadY)
	g.target = c
	return ok
}

func (g *fishSwimGoal) Start(t *tickView)             { g.m.seekWater(t.w, g.target, 1.0) }
func (g *fishSwimGoal) ShouldContinue(*tickView) bool { return g.m.fishOrdered() }
func (g *fishSwimGoal) Tick(*tickView)                {}
func (g *fishSwimGoal) Stop(*tickView)                { g.m.stopSeeking() }
func (g *fishSwimGoal) RequiresEveryTick() bool       { return false }

// flockGoal is FollowFlockLeaderGoal.
type flockGoal struct {
	m      *Mob
	next   int
	recalc int
}

func (g *flockGoal) Priority() int    { return 5 }
func (g *flockGoal) Flags() GoalFlags { return 0 }

func (g *flockGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.hasFollowers() {
		return false
	}
	if m.isFollower() {
		return true
	}
	if g.next > 0 {
		g.next--
		return false
	}
	g.next = reducedTickDelay(200 + m.Rand.Intn(200)%20)
	var near []*Mob
	for _, f := range m.schoolmates(t) {
		if f.canBeFollowed() || !f.isFollower() {
			near = append(near, f)
		}
	}
	leader := m
	for _, f := range near {
		if f.canBeFollowed() {
			leader = f
			break
		}
	}
	// addFollowers: limit first, then drop the leader itself
	room := max(leader.maxSchoolSize()-leader.fish.schoolSize, 0)
	taken := 0
	for _, f := range near {
		if f.isFollower() {
			continue
		}
		if taken == room {
			break
		}
		taken++
		if f != leader {
			f.startFollowing(leader)
		}
	}
	return m.isFollower()
}

func (g *flockGoal) ShouldContinue(*tickView) bool {
	l := g.m.fish.leader
	return g.m.isFollower() &&
		Dist2(g.m.X, g.m.Y, g.m.Z, l.X, l.Y, l.Z) <= fishLeaderRange2
}

func (g *flockGoal) Start(*tickView) { g.recalc = 0 }

func (g *flockGoal) Tick(t *tickView) {
	g.recalc--
	if g.recalc > 0 {
		return
	}
	g.recalc = reducedTickDelay(fishRecalc)
	if l := g.m.fish.leader; g.m.isFollower() {
		g.m.seekWater(t.w, cellAt(l.X, l.Y, l.Z), 1.0)
	}
}

func (g *flockGoal) Stop(*tickView)          { g.m.stopFollowing() }
func (g *flockGoal) RequiresEveryTick() bool { return false }

// MobBucketItem is the filled bucket a bucketable mob is carried in.
// vanilla: Cod.getBucketItemStack, Salmon.getBucketItemStack
func MobBucketItem(t MobType) string {
	switch t {
	case TypeSulfurCube:
		return "minecraft:sulfur_cube_bucket"
	case TypeTadpole:
		return "minecraft:tadpole_bucket"
	case TypeAxolotl:
		return "minecraft:axolotl_bucket"
	case TypeCod:
		return "minecraft:cod_bucket"
	case TypeSalmon:
		return "minecraft:salmon_bucket"
	case TypeTropicalFish:
		return "minecraft:tropical_fish_bucket"
	case TypePufferfish:
		return "minecraft:pufferfish_bucket"
	}
	return ""
}

// BucketMobKind is the mob a filled bucket item lets out.
func BucketMobKind(item registry.Item) (MobType, bool) {
	switch item {
	case registry.ItemSulfurCubeBucket:
		return TypeSulfurCube, true
	case registry.ItemTadpoleBucket:
		return TypeTadpole, true
	case registry.ItemAxolotlBucket:
		return TypeAxolotl, true
	case registry.ItemCodBucket:
		return TypeCod, true
	case registry.ItemSalmonBucket:
		return TypeSalmon, true
	case registry.ItemTropicalFishBucket:
		return TypeTropicalFish, true
	case registry.ItemPufferfishBucket:
		return TypePufferfish, true
	}
	return NoMobType, false
}

// BucketTag saves the bucketable mob's flags, health and kind-specific data.
// vanilla: Bucketable.saveDefaultDataToBucketTag
func (m *Mob) BucketTag() *nbt.OrderedCompound {
	tag := nbt.NewOrderedCompound(10)
	for _, flag := range []struct {
		name string
		set  bool
	}{
		{"NoAI", m.NoAI}, {"Silent", m.Silent}, {"NoGravity", m.NoGravity}, {"Glowing", m.Glowing},
		{"Invulnerable", m.Invulnerable}, {"PersistenceRequired", m.Persistence},
	} {
		if flag.set {
			tag.Set(flag.name, nbt.Byte(1))
		}
	}
	tag.Set("Health", nbt.Float(m.Health))
	if m.Type == TypeSulfurCube {
		tag.Set("age", nbt.Int(m.BabyAge))
		tag.Set("age_locked", boolTag(m.AgeLocked))
	}
	if m.Type == TypeTadpole {
		m.tadpoleFields(tag, false)
	}
	if m.Type == TypeAxolotl {
		m.saveAxolotlBucket(tag)
	}
	if m.Type == TypePufferfish {
		// vanilla: Pufferfish.saveToBucketTag
		tag.Set("PuffState", nbt.Int(m.puffer.state))
	}
	return tag
}

// LoadBucketTag restores the mob's bucket data and marks it as from a bucket.
func (m *Mob) LoadBucketTag(data nbt.CompoundTag) {
	tag, _ := nbt.CompoundView(data)
	flag := func(name string) (bool, bool) {
		b, ok := nbt.Get[nbt.Byte](tag, name)
		return b != 0, ok
	}
	if v, ok := flag("NoAI"); ok {
		m.SetMetadataNoAI(v)
	}
	if v, ok := flag("Silent"); ok {
		m.Silent = v
	}
	if v, ok := flag("NoGravity"); ok {
		m.SetMetadataNoGravity(v)
	}
	if v, ok := flag("Glowing"); ok {
		m.SetMetadataGlowing(v)
	}
	if v, ok := flag("Invulnerable"); ok {
		m.Invulnerable = v
	}
	if v, ok := flag("PersistenceRequired"); ok && v {
		m.Persistence = true
	}
	if m.Type == TypeSulfurCube {
		age, _ := nbt.IntField(tag, "age")
		m.SetMetadataBabyAge(int(age))
		m.SetMetadataAgeLocked(nbt.BoolField(tag, "age_locked"))
		if m.IsBaby() {
			m.SetSize(1, true)
		}
	}
	if h, ok := nbt.Get[nbt.Float](tag, "Health"); ok {
		m.SetMetadataHealth(float64(h))
	}
	m.SetMetadataFromBucket(true)
	if m.Type == TypeTadpole {
		m.tadpoleFields(tag, true)
	}
	if m.Type == TypeAxolotl {
		m.loadAxolotlBucket(tag)
	}
	if m.Type == TypePufferfish {
		// vanilla: Pufferfish.loadFromBucketTag
		puff, _ := nbt.IntField(tag, "PuffState")
		m.SetPuffState(int(puff))
	}
}

// Discarded reports whether the mob is set to leave the world on its next
// tick without dying.
func (m *Mob) Discarded() bool { return m.discarded }

// FishFlopSound is the sound a fish on dry ground flops with.
// vanilla: Cod.getFlopSound, Salmon.getFlopSound
func FishFlopSound(t MobType) registry.Sound {
	if int(t) >= len(fishFlopSounds) {
		return registry.SoundNone
	}
	return fishFlopSounds[t]
}

var fishFlopSounds = [registry.EntityTypeCount]registry.Sound{
	TypeCod:          registry.SoundEntityCodFlop,
	TypeSalmon:       registry.SoundEntitySalmonFlop,
	TypeTropicalFish: registry.SoundEntityTropicalFishFlop,
	TypePufferfish:   registry.SoundEntityPufferFishFlop,
	TypeTadpole:      registry.SoundEntityTadpoleFlop,
}

// SalmonSizeName is Salmon.Variant's serialized name for a size id.
func SalmonSizeName(size int) string { return [...]string{"small", "medium", "large"}[size] }

// SalmonSizeByName is the size id of a serialized name; an unknown name is the
// default medium.
func SalmonSizeByName(name string) int {
	switch name {
	case "small":
		return SalmonSmall
	case "large":
		return SalmonLarge
	}
	return SalmonMedium
}

// Following reports whether the fish follows a live schooling leader.
func (m *Mob) Following() bool { return m.isFollower() }

// SchoolSize is the leader's count of the school, itself included.
func (m *Mob) SchoolSize() int { return m.fish.schoolSize }

// vanilla: AbstractFish.aiStep
func (m *Mob) fishFlop(events []MobEvent) []MobEvent {
	if m.OnGround && m.justLanded {
		m.VX += float64(m.Rand.Float32()*2-1) * fishFlopSide
		m.VY += fishFlopUp
		m.VZ += float64(m.Rand.Float32()*2-1) * fishFlopSide
		m.OnGround = false
		events = append(events, MobEvent{Mob: m, Kind: MobFlopped})
	}
	return events
}
