package entity

import (
	"math"
	"math/rand"
)

// throwSpeed is the 0.3 blocks per tick a player throws an item along the
// look.
const throwSpeed = 0.3

// ThrowSpawnBelow is how far below the thrower's eyes a thrown item's
// feet start.
// vanilla: LivingEntity.createItemStackToDrop
const ThrowSpawnBelow = 0.3

// lookVector is the look direction from yaw and pitch: yaw 0 south (+Z), 90
// west (-X); negative pitch is up.
func lookVector(yaw, pitch float32) (x, y, z float64) {
	radYaw, radPitch := float64(yaw)*math.Pi/180, float64(pitch)*math.Pi/180
	return -math.Sin(radYaw) * math.Cos(radPitch),
		-math.Sin(radPitch),
		math.Cos(radYaw) * math.Cos(radPitch)
}

// LookVector is lookVector exported, for a bow's shot.
func LookVector(yaw, pitch float32) (x, y, z float64) {
	return lookVector(yaw, pitch)
}

// ThrowVelocity is a thrown item's velocity: the look at the throw speed,
// raised 0.1, with LivingEntity's drop spread (a triangle 0.1 on the rise
// and up to 0.02 in a random direction) rolled on r. The item spawns at the
// thrower's x and z, ThrowSpawnBelow under the eyes.
// vanilla: LivingEntity.createItemStackToDrop
func ThrowVelocity(yaw, pitch float32, r *rand.Rand) (vx, vy, vz float64) {
	lx, ly, lz := lookVector(yaw, pitch)
	dir := r.Float64() * 2 * math.Pi
	spread := 0.02 * r.Float64()
	return lx*throwSpeed + math.Cos(dir)*spread,
		ly*throwSpeed + 0.1 + (r.Float64()-r.Float64())*0.1,
		lz*throwSpeed + math.Sin(dir)*spread
}

// DropVelocity is createItemStackToDrop's random scatter: a random heading
// at up to 0.5 a tick rising 0.2 (a hand throw rises 0.1 ± 0.1). Speed is
// rolled before heading, matching the nextFloat order.
// vanilla: LivingEntity.createItemStackToDrop
func DropVelocity(r *rand.Rand) (vx, vy, vz float64) {
	speed := r.Float64() * 0.5
	heading := r.Float64() * 2 * math.Pi
	return -math.Sin(heading) * speed, 0.2, math.Cos(heading) * speed
}

// CrossbowShotVector is a crossbow shot's aim: the look turned angle degrees
// about the shooter's up vector (the look pitched a quarter turn up).
// vanilla: CrossbowItem.shootProjectile
func CrossbowShotVector(yaw, pitch, angle float32) (x, y, z float64) {
	vx, vy, vz := lookVector(yaw, pitch)
	if angle == 0 {
		return vx, vy, vz
	}
	ux, uy, uz := lookVector(yaw, pitch-90)
	rad := float64(angle) * math.Pi / 180
	cos, sin := math.Cos(rad), math.Sin(rad)
	// The up vector is perpendicular to the look, so Rodrigues' formula
	// loses its axis-parallel term.
	return vx*cos + (uy*vz-uz*vy)*sin,
		vy*cos + (uz*vx-ux*vz)*sin,
		vz*cos + (ux*vy-uy*vx)*sin
}
