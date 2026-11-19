package entity

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/google/uuid"
)

// The numbers a dropped item's spawn runs on.
const (
	// ItemPopSpeed is the jump a broken block gives its drop, and
	// ItemPopScatter the sideways nudge either way.
	// vanilla: Block.popResource
	ItemPopSpeed   = 0.2
	ItemPopScatter = 0.1
	// itemPopJitter is how far either way of the block's centre
	// Block.popResource stands a drop, on each axis.
	itemPopJitter = 0.25

	// lecternDropNudge is how far toward the lectern's facing its book lands.
	// vanilla: LecternBlockEntity.preRemoveSideEffects
	lecternDropNudge = 0.25

	// hangingDropNudge is how far out from its centre a broken hanging
	// body's drop is laid, toward the room in the direction the body faced.
	// vanilla: HangingEntity.spawnAtLocation
	hangingDropNudge = 0.15
)

// PopVelocity rolls the bare ItemEntity constructor's toss for a drop
// spawned with no velocity of its own, the X die before the Z.
// vanilla: ItemEntity.<init>
func PopVelocity(r *rand.Rand) (vx, vy, vz float64) {
	return (r.Float64() - 0.5) * ItemPopScatter * 2, ItemPopSpeed, (r.Float64() - 0.5) * ItemPopScatter * 2
}

// LecternBookDropSpot is where a broken lectern's book is laid: a quarter
// block out from the block's centre toward its facing, resting on the top.
// py is the item's feet.
// vanilla: LecternBlockEntity.preRemoveSideEffects
func LecternBookDropSpot(facing string, x, y, z int32) (px, py, pz float64) {
	px, py, pz = float64(x)+0.5, float64(y+1), float64(z)+0.5
	switch facing {
	case "north":
		pz -= lecternDropNudge
	case "south":
		pz += lecternDropNudge
	case "west":
		px -= lecternDropNudge
	case "east":
		px += lecternDropNudge
	}
	return px, py, pz
}

// BlockDropSpot rolls where about the block's centre one of a broken block's
// drops stands, the X, Y and Z dice in that order. py is the item's feet.
// vanilla: Block.popResource
func BlockDropSpot(r *rand.Rand, x, y, z int32) (px, py, pz float64) {
	jitter := func() float64 { return (r.Float64()*2 - 1) * itemPopJitter }
	return float64(x) + 0.5 + jitter(), float64(y) + 0.5 - ItemHalfSize + jitter(), float64(z) + 0.5 + jitter()
}

// HangingDropSpot is where a hanging body's broken drop is laid: a little out
// from the centre of its box toward the room along dir. centreY is the body's
// centre; the returned Y is the item's feet.
// vanilla: HangingEntity.spawnAtLocation
func HangingDropSpot(dir int32, centreX, centreY, centreZ float64) (x, y, z float64) {
	step := protocol.FaceOffset[dir]
	return centreX + float64(step[0])*hangingDropNudge,
		centreY,
		centreZ + float64(step[2])*hangingDropNudge
}

// PoppedItem rolls an item laid just above a block's centre, jittered across
// it, with the five-argument ItemEntity constructor's hop. py is the feet the
// constructor takes.
// vanilla: JukeboxBlockEntity.popOutTheItem
func PoppedItem(r *rand.Rand, x, y, z int32) (px, py, pz, vx, vy, vz float64) {
	px = float64(x) + 0.5 + (float64(r.Float32())-0.5)*0.7
	py = float64(y) + 1.01
	pz = float64(z) + 0.5 + (float64(r.Float32())-0.5)*0.7
	vx = r.Float64()*0.2 - 0.1
	vy = 0.2
	vz = r.Float64()*0.2 - 0.1
	return px, py, pz, vx, vy, vz
}

// FaceDrop rolls a broken block's drop laid against face and thrown that
// way (py is the item's feet): on the face's axis, offset half a block plus half an item and pushed
// 0.1; on the other axes popResource's jitter and scatter, the lift from 0
// to 0.1. Only the free axes draw dice.
// vanilla: Block.popResourceFromFace
func FaceDrop(r *rand.Rand, x, y, z, face int32) (px, py, pz, vx, vy, vz float64) {
	step := protocol.FaceOffset[face]
	axis := func(step int32) float64 {
		if step == 0 {
			return (r.Float64()*2 - 1) * itemPopJitter
		}
		return float64(step) * (0.5 + ItemHalfSize)
	}
	velocity := func(step int32, lift bool) float64 {
		if step == 0 {
			if lift {
				return r.Float64() * ItemPopScatter
			}
			return (r.Float64()*2 - 1) * ItemPopScatter
		}
		v := float64(step) * ItemPopScatter
		if lift {
			v += ItemPopScatter
		}
		return v
	}
	px = float64(x) + 0.5 + axis(step[0])
	py = float64(y) + 0.5 - ItemHalfSize + axis(step[1])
	pz = float64(z) + 0.5 + axis(step[2])
	vx = velocity(step[0], false)
	vy = velocity(step[1], true)
	vz = velocity(step[2], false)
	return px, py, pz, vx, vy, vz
}

// SpillPiece is one item entity a broken container's spill lays down.
type SpillPiece struct {
	Stack      protocol.ItemStack
	X, Y, Z    float64
	VX, VY, VZ float64
}

// Spill rolls a broken container's contents around its block: each stack
// takes one spot (an item's width in from the edges) and splits there into
// pieces of 10-30, tossed on the spill's triangle arc, with no pickup
// delay. Y is the item's feet.
// vanilla: Containers.dropContents, dropItemStack
func Spill(dice *rules.JavaRandom, x, y, z int32, stacks []protocol.ItemStack) []SpillPiece {
	const (
		// The item entity's width and the room it leaves in a whole block.
		itemSize    = 0.25
		centerRange = 1 - itemSize
		halfSize    = itemSize / 2
		// The sideways and forward humps share one spread.
		scatter = 0.11485000171139836
		upMean  = 0.2
	)
	var pieces []SpillPiece
	for _, stack := range stacks {
		if stack.Empty() {
			continue
		}
		xo := float64(x) + dice.NextDouble()*centerRange + halfSize
		yo := float64(y) + dice.NextDouble()*centerRange
		zo := float64(z) + dice.NextDouble()*centerRange + halfSize
		for !stack.Empty() {
			take := int(dice.NextIntBound(21)) + 10
			piece := stack
			if piece.Count > int32(take) {
				piece.Count = int32(take)
			}
			stack.Count -= piece.Count
			if stack.Count <= 0 {
				stack = protocol.ItemStack{}
			}
			pieces = append(pieces, SpillPiece{
				Stack: piece, X: xo, Y: yo, Z: zo,
				VX: dice.Triangle(0, scatter), VY: dice.Triangle(upMean, scatter), VZ: dice.Triangle(0, scatter),
			})
		}
	}
	return pieces
}

// AllowsPickup reports whether the player may claim the item: one with no
// target is anyone's, one with a target belongs to that player alone.
// vanilla: ItemEntity.playerTouch
func (it *Item) AllowsPickup(id uuid.UUID) bool {
	return it.Target == nil || *it.Target == id
}

// OrbPopVector rolls the push a freshly constructed orb leaves with: half its
// velocity range either way on the flat and the whole of it upward.
// vanilla: ExperienceOrb.<init>
func OrbPopVector(r *rand.Rand) (vx, vy, vz float64) {
	return (r.Float64() - 0.5) * 0.4, r.Float64() * 0.4, (r.Float64() - 0.5) * 0.4
}

// DirectedOrbPop rolls an orb's push and offset for an award with a rough
// direction: OrbPopVector's push, flipped if against the direction, and the
// spawn moved half an orb along it. A zero direction changes nothing.
// vanilla: ExperienceOrb.<init>(Level, Vec3, Vec3, int)
func DirectedOrbPop(r *rand.Rand, dx, dy, dz float64) (vx, vy, vz, ox, oy, oz float64) {
	vx, vy, vz = OrbPopVector(r)
	length := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if length == 0 {
		return vx, vy, vz, 0, 0, 0
	}
	if dx*vx+dy*vy+dz*vz < 0 {
		vx, vy, vz = -vx, -vy, -vz
	}
	// The orb's bounding-box size is its 0.5 height, the largest edge.
	k := orbHeight * 0.5 / length
	return vx, vy, vz, dx * k, dy * k, dz * k
}

// TakesAward reports whether an award of value at the point merges into the
// orb: alive, worth the same, one of the forty ids shift names, and within
// the award's box.
// vanilla: ExperienceOrb.tryMergeToExisting
func (o *Orb) TakesAward(x, y, z float64, value int, shift int32) bool {
	if o.Dead || o.Value != value || (o.ID-shift)%40 != 0 {
		return false
	}
	return o.WithinAwardMergeOf(x, y, z)
}
