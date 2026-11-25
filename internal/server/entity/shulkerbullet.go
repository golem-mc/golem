package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/world"
)

// The shulker bullet's numbers.
// vanilla: ShulkerBullet
const (
	shulkerBulletSpeed   = 0.15
	shulkerBulletGravity = 0.04
	// ShulkerBulletDamage is the hurt of a bullet that lands.
	ShulkerBulletDamage = 4.0
	// shulkerBulletHalf is half the 0.3125 of EntityTypes.SHULKER_BULLET's
	// sized, also the side of the other 0.3125 kinds' boxes; the flight
	// keeps the quarter block of the thrown bodies.
	shulkerBulletHalf = 0.15625
	// ShulkerBulletLevitation is the Levitation a bullet's hit gives, in ticks.
	ShulkerBulletLevitation = 200
	// shulkerBulletNear is the distance to the target's cell, squared, below
	// which the bullet stops creeping and flies straight.
	shulkerBulletNear = 2.0
)

// bulletAxis is the axis each face id runs along: 0 x, 1 y, 2 z.
var bulletAxis = [6]int{1, 1, 2, 2, 0, 0}

// bulletFlight is a shulker bullet's extra state: it creeps toward the
// target one axis at a time, changing direction every few steps.
// vanilla: ShulkerBullet
type bulletFlight struct {
	targetID int32
	// dir is currentMoveDirection as a face id, -1 for none.
	dir        int8
	steps      int
	dx, dy, dz float64
}

// NewShulkerBullet is the bullet an owner at (x, y, z) (the centre of its
// box) fires at target; avoidAxis is the axis of the shulker's attach face
// (0 x, 1 y, 2 z), which the first leg must not run along.
// vanilla: ShulkerBullet.<init>(Level, LivingEntity, Entity, Direction.Axis)
func NewShulkerBullet(id int32, w World, x, y, z float64, ownerID int32, target ArrowTarget, avoidAxis int, seed int64) *Thrown {
	t := NewThrown(id, ThrownShulkerBullet, x, y, z, 0, 0, 0, ownerID, seed)
	t.Bullet = &bulletFlight{targetID: target.EntityID, dir: int8(faceUp)}
	t.selectBulletDirection(w, avoidAxis, &target)
	return t
}

// BulletTargetID is the entity the bullet homes on.
func (t *Thrown) BulletTargetID() int32 {
	if t.Bullet == nil {
		return 0
	}
	return t.Bullet.targetID
}

// BulletSave is the creep state a save keeps: the steps left in the leg, the
// direction as a face id (-1 for none) and the delta to the target.
// vanilla: ShulkerBullet.addAdditionalSaveData
func (t *Thrown) BulletSave() (steps int, dir int8, dx, dy, dz float64) {
	b := t.Bullet
	return b.steps, b.dir, b.dx, b.dy, b.dz
}

// RestoreBullet gives a bullet built from a save its creep state and the
// entity it homes on (0 for none yet).
// vanilla: ShulkerBullet.readAdditionalSaveData
func (t *Thrown) RestoreBullet(targetID int32, steps int, dir int8, dx, dy, dz float64) {
	if dir < -1 || dir > 5 {
		dir = -1
	}
	t.Bullet = &bulletFlight{targetID: targetID, dir: dir, steps: steps, dx: dx, dy: dy, dz: dz}
}

// SetBulletTarget points the bullet at the entity ID given.
func (t *Thrown) SetBulletTarget(id int32) {
	if t.Bullet != nil {
		t.Bullet.targetID = id
	}
}

// steerBullet pulls the velocity toward the creep direction while the target
// is a live body, and lets the bullet fall otherwise. It returns the target
// body if it is in the world.
// vanilla: ShulkerBullet.tick
func (t *Thrown) steerBullet(targets []ArrowTarget) *ArrowTarget {
	b := t.Bullet
	var found *ArrowTarget
	for i := range targets {
		if targets[i].EntityID == b.targetID {
			found = &targets[i]
			break
		}
	}
	if found != nil && !found.Unhittable {
		b.dx = math.Max(-1, math.Min(1, b.dx*1.025))
		b.dy = math.Max(-1, math.Min(1, b.dy*1.025))
		b.dz = math.Max(-1, math.Min(1, b.dz*1.025))
		t.VX += (b.dx - t.VX) * 0.2
		t.VY += (b.dy - t.VY) * 0.2
		t.VZ += (b.dz - t.VZ) * 0.2
	} else if !t.NoGravity {
		t.VY -= shulkerBulletGravity
	}
	return found
}

// bulletMoved is the part of the tick after the move: count the leg down and
// pick the next direction when it ends, the creep reaches a block, or the
// bullet is level with the target on the axis it runs along.
// vanilla: ShulkerBullet.tick
func (t *Thrown) bulletMoved(w World, target *ArrowTarget) {
	b := t.Bullet
	if target == nil {
		return
	}
	if b.steps > 0 {
		b.steps--
		if b.steps == 0 {
			axis := -1
			if b.dir >= 0 {
				axis = bulletAxis[b.dir]
			}
			t.selectBulletDirection(w, axis, target)
		}
	}
	if b.dir < 0 {
		return
	}
	cur := [3]int32{int32(math.Floor(t.X)), int32(math.Floor(t.Y)), int32(math.Floor(t.Z))}
	axis := bulletAxis[b.dir]
	step := faceStep[b.dir]
	ahead := [3]int32{cur[0] + step[0], cur[1] + step[1], cur[2] + step[2]}
	if chunkLoaded(w, ahead[0], ahead[2]) && canStandOn(w.BlockAt(ahead[0], ahead[1], ahead[2]), ahead[1], t.Y) {
		t.selectBulletDirection(w, axis, target)
		return
	}
	tp := [3]int32{int32(math.Floor((target.MinX + target.MaxX) / 2)), int32(math.Floor(target.MinY)),
		int32(math.Floor((target.MinZ + target.MaxZ) / 2))}
	if cur[axis] == tp[axis] {
		t.selectBulletDirection(w, axis, target)
	}
}

// selectBulletDirection picks the face to creep along next, at random among
// those that close on the target and are free, and aims the velocity at the
// cell beyond.
// vanilla: ShulkerBullet.selectNextMoveDirection
func (t *Thrown) selectBulletDirection(w World, avoidAxis int, target *ArrowTarget) {
	b := t.Bullet
	cur := [3]int32{int32(math.Floor(t.X)), int32(math.Floor(t.Y)), int32(math.Floor(t.Z))}
	yOff := 0.5
	var tp [3]int32
	if target == nil {
		tp = [3]int32{cur[0], cur[1] - 1, cur[2]}
	} else {
		yOff = (target.MaxY - target.MinY) * 0.5
		tp = [3]int32{int32(math.Floor((target.MinX + target.MaxX) / 2)), int32(math.Floor(target.MinY + yOff)),
			int32(math.Floor((target.MinZ + target.MaxZ) / 2))}
	}
	goal := [3]float64{float64(tp[0]) + 0.5, float64(tp[1]) + yOff, float64(tp[2]) + 0.5}
	sel := int8(-1)
	// closerToCenterThan: the squared distance to the centre of the cell.
	dx, dy, dz := t.X-(float64(tp[0])+0.5), t.Y-(float64(tp[1])+0.5), t.Z-(float64(tp[2])+0.5)
	if dx*dx+dy*dy+dz*dz >= shulkerBulletNear*shulkerBulletNear {
		empty := func(face uint8) bool {
			s := faceStep[face]
			return world.IsAirState(w.BlockAt(cur[0]+s[0], cur[1]+s[1], cur[2]+s[2]))
		}
		var options []uint8
		// Per axis: toward the target if the cell is free.
		for axis, faces := range [3][2]uint8{{faceWest, faceEast}, {faceDown, faceUp}, {faceNorth, faceSouth}} {
			if avoidAxis == axis {
				continue
			}
			switch {
			case cur[axis] < tp[axis] && empty(faces[1]):
				options = append(options, faces[1])
			case cur[axis] > tp[axis] && empty(faces[0]):
				options = append(options, faces[0])
			}
		}
		pick := uint8(t.Rand.Intn(6))
		if len(options) == 0 {
			for attempts := 5; !empty(pick) && attempts > 0; attempts-- {
				pick = uint8(t.Rand.Intn(6))
			}
		} else {
			pick = options[t.Rand.Intn(len(options))]
		}
		sel = int8(pick)
		goal = [3]float64{t.X + float64(faceStep[pick][0]), t.Y + float64(faceStep[pick][1]), t.Z + float64(faceStep[pick][2])}
	}
	b.dir = sel
	xa, ya, za := goal[0]-t.X, goal[1]-t.Y, goal[2]-t.Z
	if d := math.Sqrt(xa*xa + ya*ya + za*za); d == 0 {
		b.dx, b.dy, b.dz = 0, 0, 0
	} else {
		b.dx, b.dy, b.dz = xa/d*shulkerBulletSpeed, ya/d*shulkerBulletSpeed, za/d*shulkerBulletSpeed
	}
	b.steps = 10 + t.Rand.Intn(5)*10
}

// chunkLoaded reports whether the chunk at the block column is loaded, true
// for a world that cannot say.
func chunkLoaded(w World, x, z int32) bool {
	l, ok := w.(interface{ ChunkLoadedAt(x, z int32) bool })
	return !ok || l.ChunkLoadedAt(x, z)
}
