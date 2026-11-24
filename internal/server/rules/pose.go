package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
)

// Player poses and their shapes: updatePlayerPose and the POSES dimensions
// table over the caller's inputs (keys, sleep, fit). The dimensions set the
// eye height and box suffocation, drowning and reach run on; the caller's
// fit test decides whether a pose's box stands free.
// vanilla: Player.updatePlayerPose, Avatar.POSES

// Pose is one of the poses the metadata names, the wire values the
// protocol.Pose* constants carry.
type Pose = int32

// PlayerDimensions is a pose's shape in blocks: width, height and eye
// height, applied when the pose changes and read by suffocation, drowning
// and reach.
// vanilla: EntityDimensions
type PlayerDimensions struct {
	Width     float64
	Height    float64
	EyeHeight float64
}

// Scaled is the shape grown by s in width, height and eye height.
// The products are single precision, as the float fields are.
// vanilla: EntityDimensions.scale
func (d PlayerDimensions) Scaled(s float64) PlayerDimensions {
	f := float32(s)
	return PlayerDimensions{
		Width:     float64(float32(d.Width) * f),
		Height:    float64(float32(d.Height) * f),
		EyeHeight: float64(float32(d.EyeHeight) * f),
	}
}

// The poses' shapes.
// vanilla: Avatar.POSES
var playerPoseDimensions = map[Pose]PlayerDimensions{
	protocol.PoseStanding:   {Width: 0.6, Height: 1.8, EyeHeight: 1.62},
	protocol.PoseSleeping:   {Width: 0.2, Height: 0.2, EyeHeight: 0.2},
	protocol.PoseFallFlying: {Width: 0.6, Height: 0.6, EyeHeight: 0.4},
	protocol.PoseSwimming:   {Width: 0.6, Height: 0.6, EyeHeight: 0.4},
	protocol.PoseSpinAttack: {Width: 0.6, Height: 0.6, EyeHeight: 0.4},
	protocol.PoseSneaking:   {Width: 0.6, Height: 1.5, EyeHeight: 1.27},
}

// standingPlayerDimensions is the shape a pose the table has no entry for
// falls back to, vanilla's POSES.getOrDefault(pose, STANDING_DIMENSIONS).
var standingPlayerDimensions = playerPoseDimensions[protocol.PoseStanding]

// DismountPose is one body shape a dismount tries: the first whose box stands
// free where the body steps off is the one that fits. A mob's shape is its
// own dimensions rather than a player pose's.
type DismountPose struct {
	Dims PlayerDimensions
}

// PlayerDismountPoses are the shapes a rider's dismount tries in order,
// vanilla's Player.getDismountPoses: standing, crouching, swimming.
// vanilla: Player.getDismountPoses
var PlayerDismountPoses = []DismountPose{
	{Dims: PlayerDimensionsOf(protocol.PoseStanding)},
	{Dims: PlayerDimensionsOf(protocol.PoseSneaking)},
	{Dims: PlayerDimensionsOf(protocol.PoseSwimming)},
}

// PlayerDimensionsOf reports the shape of a body in the given pose. A pose
// the table does not name reads as the standing one.
// vanilla: Entity.getDimensions
func PlayerDimensionsOf(pose Pose) PlayerDimensions {
	if dims, ok := playerPoseDimensions[pose]; ok {
		return dims
	}
	return standingPlayerDimensions
}

// PlayerEyeHeight reports how far above the feet a body's eyes sit in the
// given pose, the height every rule that looks out of the body's eyes reads.
func PlayerEyeHeight(pose Pose) float64 {
	return PlayerDimensionsOf(pose).EyeHeight
}

// DesiredPose is the pose the body's state asks for, in priority: sleeping;
// swimming (the server flag, set only while sprinting in water); fall
// flying; sneaking when not flying; standing.
// vanilla: Player.getDesiredPose
func DesiredPose(sleeping, swimming, fallFlying, spinAttack, sneaking, flying bool) Pose {
	switch {
	case sleeping:
		return protocol.PoseSleeping
	case swimming:
		return protocol.PoseSwimming
	case fallFlying:
		return protocol.PoseFallFlying
	case spinAttack:
		return protocol.PoseSpinAttack
	case sneaking && !flying:
		return protocol.PoseSneaking
	default:
		return protocol.PoseStanding
	}
}

// PlayerPose decides the pose each tick. fit reports whether a box of a
// pose's shape stands free at the body, shaved by 1e-6, of blocks and bodies
// (canPlayerFitWithinBlocksAndEntitiesWhen).
//
// If even the swimming box does not fit, the pose is left as it is (decided
// false); otherwise spectators and passengers (free) get the desired pose.
// Other bodies get the desired pose if it fits, else crouching, else swimming.
//
// A desired box at least as wide and tall as the swimming box contains it
// (same feet and centre), so it is tried first: when it fits the swimming
// box fits too, and a body standing free costs one fit instead of vanilla's
// two. The result is vanilla's for every pose.
// vanilla: Player.updatePlayerPose
func PlayerPose(desired Pose, free bool, fit func(Pose) bool) (pose Pose, decided bool) {
	d, sw := PlayerDimensionsOf(desired), PlayerDimensionsOf(protocol.PoseSwimming)
	covers := d.Width >= sw.Width && d.Height >= sw.Height
	if !free && covers && fit(desired) {
		return desired, true
	}
	if !fit(protocol.PoseSwimming) {
		return desired, false // the pose is left alone, vanilla's early return
	}
	if free || !covers && fit(desired) {
		return desired, true
	}
	// The crouch: the pose the protocol's block names sneaking, whose
	// dimensions are the crouching body's.
	if fit(protocol.PoseSneaking) {
		return protocol.PoseSneaking, true
	}
	return protocol.PoseSwimming, true
}
