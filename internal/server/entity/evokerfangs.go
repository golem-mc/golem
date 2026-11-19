package entity

// EvokerFangs is the jaws an evoker's spell raises: they wait out a warmup,
// show themselves, bite everything in reach 8 ticks later and vanish.
// vanilla: EvokerFangs

// The fangs' numbers.
// vanilla: EvokerFangs
const (
	// FangsWidth and FangsHeight are the box.
	FangsWidth  = 0.5
	FangsHeight = 0.8
	// FangsTracking and FangsUpdateInterval are the type's clientTrackingRange
	// in chunks and updateInterval.
	FangsTracking       = 6
	FangsUpdateInterval = 2
	// FangsDamage is the bite's hurt.
	FangsDamage = 6.0
	// fangsLife is lifeTicks, counted down after the warmup.
	fangsLife = 22
	// fangsBiteAt is the warmup count the bite lands on.
	fangsBiteAt = -8
	// FangsReach is the box's inflation on each horizontal side.
	FangsReach = 0.2
)

// EvokerFangs is one fangs entity.
type EvokerFangs struct {
	ID      int32
	X, Y, Z float64
	// Yaw is the facing in degrees.
	Yaw     float32
	OwnerID int32
	Dead    bool

	warmup    int
	life      int
	sentSpike bool
}

// NewEvokerFangs stands fangs at the spot, facing the angle in radians, that
// bite after the warmup in ticks.
func NewEvokerFangs(id int32, x, y, z, radians float64, warmup int, ownerID int32) *EvokerFangs {
	return &EvokerFangs{ID: id, X: x, Y: y, Z: z, Yaw: float32(radians * 57.295776), OwnerID: ownerID,
		warmup: warmup, life: fangsLife}
}

// Warmup is the remaining delay saved by EvokerFangs.addAdditionalSaveData.
func (f *EvokerFangs) Warmup() int { return f.warmup }

// SetWarmup changes the saved delay without restarting the attack animation.
func (f *EvokerFangs) SetWarmup(ticks int) { f.warmup = ticks }

// Tick runs one tick. spike is the entity event that starts the client's
// animation, bite the tick the jaws close.
// vanilla: EvokerFangs.tick
func (f *EvokerFangs) Tick() (spike, bite bool) {
	f.warmup--
	if f.warmup >= 0 {
		return false, false
	}
	bite = f.warmup == fangsBiteAt
	if !f.sentSpike {
		f.sentSpike = true
		spike = true
	}
	f.life--
	if f.life < 0 {
		f.Dead = true
	}
	return spike, bite
}

// BiteBox is the box the bite looks for living bodies in.
func (f *EvokerFangs) BiteBox() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	half := FangsWidth/2 + FangsReach
	return f.X - half, f.Y, f.Z - half, f.X + half, f.Y + FangsHeight, f.Z + half
}

// FangsAllied is EvokerFangs.dealDamageTo's isAlliedTo(owner, victim): the
// illagers spare each other and an evoker spares the vexes whose summoner is
// itself or an illager. others is the herd the vex's summoner is looked up in.
// vanilla: AbstractIllager.considersEntityAsAlly, Evoker.considersEntityAsAlly
func FangsAllied(owner, victim *Mob, others []*Mob) bool {
	if owner == nil || victim == nil {
		return false
	}
	if owner == victim || IsIllagerType(victim.Type) {
		return true
	}
	if victim.Type != TypeVex {
		return false
	}
	root := mobByID(others, victim.vex.ownerID)
	return root != nil && (root == owner || IsIllagerType(root.Type))
}
