package world

import (
	"math"
	"sync"
	"sync/atomic"
)

// The world border's limits.
// vanilla: WorldBorder (MAX_SIZE, MAX_CENTER_COORDINATE, absoluteMaxSize)
const (
	BorderMaxSize   = 59_999_968.0
	BorderMaxCenter = 29_999_984.0
	// BorderAbsoluteMax is the default absolute radius, max-world-size's
	// default and ceiling.
	BorderAbsoluteMax    = 29_999_984
	borderDefaultDamage  = 0.2
	borderDefaultBuffer  = 5.0
	borderDefaultWarnBlk = 5
	// The saved-data default in ticks; the class field's 15 is not what vanilla
	// sends.
	borderDefaultWarnTime = 300
	// borderEdgeEpsilon trims a box's far edge before its bounds test.
	// vanilla: WorldBorder.isWithinBounds(AABB)
	borderEdgeEpsilon = float64(float32(1.0e-5))
)

// BorderState is one read of a border: what its packets carry.
type BorderState struct {
	CenterX, CenterZ float64
	// Size is the current side length, Target the one a lerp heads for (Size
	// when not moving).
	Size, Target float64
	// LerpTicks is the ticks a lerp has left, zero for a border not moving.
	LerpTicks     int64
	WarningBlocks int32
	// WarningTime is in ticks.
	WarningTime int32
	// AbsoluteMax is the radius every edge is clamped to.
	AbsoluteMax int32
}

// Border is one dimension's world border: a square around a centre whose side
// lerps over ticks. Safe for concurrent use.
// vanilla: WorldBorder
type Border struct {
	mu                 sync.Mutex
	centerX, centerZ   float64
	damagePerBlock     float64
	safeZone           float64
	warningBlocks      int32
	warningTime        int32
	size, from, to     float64
	duration, progress int64 // progress counts down; zero for a static border
	absoluteMax        float64
	// published is edges() as of the last change to the centre or size, so
	// Contains reads it without mu.
	published atomic.Pointer[[4]float64]
}

// publish stores the current edges; the caller holds mu or owns b.
func (b *Border) publish() {
	minX, minZ, maxX, maxZ := b.edges()
	b.published.Store(&[4]float64{minX, minZ, maxX, maxZ})
}

// NewBorder returns a border at vanilla's defaults.
// vanilla: WorldBorder.Settings.DEFAULT
func NewBorder() *Border {
	b := &Border{
		damagePerBlock: borderDefaultDamage,
		safeZone:       borderDefaultBuffer,
		warningBlocks:  borderDefaultWarnBlk,
		warningTime:    borderDefaultWarnTime,
		size:           BorderMaxSize,
		to:             BorderMaxSize,
		absoluteMax:    BorderAbsoluteMax,
	}
	b.publish()
	return b
}

// State reads the border.
func (b *Border) State() BorderState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return BorderState{
		CenterX: b.centerX, CenterZ: b.centerZ,
		Size: b.size, Target: b.to, LerpTicks: b.progress,
		WarningBlocks: b.warningBlocks, WarningTime: b.warningTime,
		AbsoluteMax: int32(b.absoluteMax),
	}
}

// SetAbsoluteMax sets the radius every edge is clamped to.
// vanilla: WorldBorder.setAbsoluteMaxSize
func (b *Border) SetAbsoluteMax(n int32) {
	b.mu.Lock()
	b.absoluteMax = float64(n)
	b.publish()
	b.mu.Unlock()
}

// DamageSettings returns the damage per block and the safe zone past the
// border.
func (b *Border) DamageSettings() (perBlock, buffer float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.damagePerBlock, b.safeZone
}

// Size is the side length now.
func (b *Border) Size() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.size
}

// SetCenter moves the centre.
func (b *Border) SetCenter(x, z float64) {
	b.mu.Lock()
	b.centerX, b.centerZ = x, z
	b.publish()
	b.mu.Unlock()
}

// SetSize makes the side length d at once.
func (b *Border) SetSize(d float64) {
	b.mu.Lock()
	b.size, b.from, b.to, b.progress, b.duration = d, d, d, 0, 0
	b.publish()
	b.mu.Unlock()
}

// LerpSize moves the side from one length to another over ticks; equal ends do
// not move.
// vanilla: WorldBorder.lerpSizeBetween
func (b *Border) LerpSize(from, to float64, ticks int64) {
	if from == to || ticks <= 0 {
		b.SetSize(to)
		return
	}
	b.mu.Lock()
	b.size, b.from, b.to, b.duration, b.progress = from, from, to, ticks, ticks
	b.publish()
	b.mu.Unlock()
}

// SetDamagePerBlock sets the damage a player takes per block past the buffer.
func (b *Border) SetDamagePerBlock(d float64) {
	b.mu.Lock()
	b.damagePerBlock = d
	b.mu.Unlock()
}

// SetSafeZone sets the buffer past the edge a player may stand in unhurt.
func (b *Border) SetSafeZone(d float64) {
	b.mu.Lock()
	b.safeZone = d
	b.mu.Unlock()
}

// SetWarningBlocks sets the distance at which the client starts tinting.
func (b *Border) SetWarningBlocks(n int32) {
	b.mu.Lock()
	b.warningBlocks = n
	b.mu.Unlock()
}

// SetWarningTime sets the warning time, in ticks.
func (b *Border) SetWarningTime(n int32) {
	b.mu.Lock()
	b.warningTime = n
	b.mu.Unlock()
}

// Tick advances a lerp by one tick.
// vanilla: WorldBorder.tick, MovingBorderExtent.update
func (b *Border) Tick() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.progress <= 0 {
		return
	}
	defer b.publish()
	b.progress--
	if b.progress <= 0 {
		b.size, b.from, b.duration = b.to, b.to, 0
		return
	}
	frac := float64(b.duration-b.progress) / float64(b.duration)
	b.size = b.from + (b.to-b.from)*frac
}

// edges are the four edges, clamped to the absolute maximum.
func (b *Border) edges() (minX, minZ, maxX, maxZ float64) {
	clamp := func(v float64) float64 { return math.Max(-b.absoluteMax, math.Min(b.absoluteMax, v)) }
	half := b.size / 2
	return clamp(b.centerX - half), clamp(b.centerZ - half), clamp(b.centerX + half), clamp(b.centerZ + half)
}

// Contains reports whether the point is inside the border, far edges exclusive.
// vanilla: WorldBorder.isWithinBounds(double, double)
func (b *Border) Contains(x, z float64) bool {
	e := b.published.Load()
	return x >= e[0] && x < e[2] && z >= e[1] && z < e[3]
}

func (b *Border) contains(x, z float64) bool {
	minX, minZ, maxX, maxZ := b.edges()
	return x >= minX && x < maxX && z >= minZ && z < maxZ
}

// ContainsBlock reports whether the block column is inside the border.
// vanilla: WorldBorder.isWithinBounds(BlockPos)
func (b *Border) ContainsBlock(x, z int32) bool {
	return b.Contains(float64(x), float64(z))
}

// ClampBlock moves a block column into the border, the far edges exclusive.
// vanilla: WorldBorder.clampToBounds
func (b *Border) ClampBlock(x, z int32) (int32, int32) {
	b.mu.Lock()
	minX, minZ, maxX, maxZ := b.edges()
	b.mu.Unlock()
	// Flooring commutes with the clamp, so the block's own floor needs none.
	const inset = float64(float32(1e-5)) // vanilla's (double)1.0E-5f
	return int32(math.Max(math.Floor(minX), math.Min(float64(x), math.Floor(maxX-inset)))),
		int32(math.Max(math.Floor(minZ), math.Min(float64(z), math.Floor(maxZ-inset))))
}

// DistanceTo is the distance from the point to the nearest edge, negative
// outside.
// vanilla: WorldBorder.getDistanceToBorder
func (b *Border) DistanceTo(x, z float64) float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.distanceTo(x, z)
}

func (b *Border) distanceTo(x, z float64) float64 {
	minX, minZ, maxX, maxZ := b.edges()
	return math.Min(math.Min(x-minX, maxX-x), math.Min(z-minZ, maxZ-z))
}

// OutsideDamage is this tick's hurt for a player whose box spans the corners,
// standing at (x, z): zero inside the border or within the safe zone past it,
// else a whole number of at least one.
// vanilla: LivingEntity.baseTick
func (b *Border) OutsideDamage(minX, minZ, maxX, maxZ, x, z float64) float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.contains(minX, minZ) && b.contains(maxX-borderEdgeEpsilon, maxZ-borderEdgeEpsilon) {
		return 0
	}
	dist := b.distanceTo(x, z) + b.safeZone
	if dist >= 0 || b.damagePerBlock <= 0 {
		return 0
	}
	return math.Max(1, math.Floor(-dist*b.damagePerBlock))
}

// SetBorder gives the world the border its mobs' moves are held to. It
// belongs to construction time.
func (w *World) SetBorder(b *Border) { w.limit = b }

// InBorder reports whether the block column is inside the dimension's
// border; true when none was set.
// vanilla: WorldBorder.isWithinBounds(BlockPos)
func (w *World) InBorder(x, z int32) bool {
	return w.limit == nil || w.limit.ContainsBlock(x, z)
}

// InBorderPoint reports whether the point is inside the border, far edges exclusive.
// vanilla: WorldBorder.isWithinBounds(Vec3)
func (w *World) InBorderPoint(x, z float64) bool {
	return w.limit == nil || w.limit.Contains(x, z)
}

// ClampToBorder moves the point into the dimension's border, the far edges
// exclusive; unchanged when none was set.
// vanilla: WorldBorder.clampVec3ToBound
func (w *World) ClampToBorder(x, z float64) (float64, float64) {
	if w.limit == nil {
		return x, z
	}
	b := w.limit
	b.mu.Lock()
	minX, minZ, maxX, maxZ := b.edges()
	b.mu.Unlock()
	const inset = float64(float32(1e-5)) // vanilla's (double)1.0E-5f
	return math.Max(minX, math.Min(x, maxX-inset)), math.Max(minZ, math.Min(z, maxZ-inset))
}

// BorderEdges is the border's current edges; ok is false when none was set.
func (w *World) BorderEdges() (minX, minZ, maxX, maxZ float64, ok bool) {
	if w.limit == nil {
		return 0, 0, 0, 0, false
	}
	e := w.limit.published.Load()
	return e[0], e[1], e[2], e[3], true
}

// InBorderBox reports whether the entire horizontal box lies inside the border.
// vanilla: WorldBorder.isWithinBounds(AABB)
func (w *World) InBorderBox(minX, minZ, maxX, maxZ float64) bool {
	if w.limit == nil {
		return true
	}
	b := w.limit
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.contains(minX, minZ) && b.contains(maxX-borderEdgeEpsilon, maxZ-borderEdgeEpsilon)
}

// ContainsChunk tests both chunk corners against one published border extent.
// vanilla: WorldBorder.isWithinBounds(ChunkPos)
func (b *Border) ContainsChunk(pos ChunkPos) bool {
	e := b.published.Load()
	x, z := float64(pos.X*16), float64(pos.Z*16)
	xx, zz := float64(pos.X*16+15), float64(pos.Z*16+15)
	return x >= e[0] && x < e[2] && z >= e[1] && z < e[3] && xx >= e[0] && xx < e[2] && zz >= e[1] && zz < e[3]
}

// ChunkInBorder reports whether both chunk corners are inside the border.
func (w *World) ChunkInBorder(pos ChunkPos) bool {
	return w.limit == nil || w.limit.ContainsChunk(pos)
}
