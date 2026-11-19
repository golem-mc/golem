package rules

import "math"

// WaypointReallyFar is the distance past which the locator bar shows a
// transmitter as a bearing alone, in blocks.
// vanilla: WaypointTransmitter.REALLY_FAR_DISTANCE
const WaypointReallyFar = 332.0

// WaypointAngleStep is the least a bearing has to turn for the turn to be
// worth sending, in radians (half a degree).
// vanilla: WaypointTransmitter.EntityAzimuthConnection.update
const WaypointAngleStep = math.Pi / 360

// WaypointForm is how precisely a waypoint names its transmitter.
type WaypointForm int

// The forms, from the most to the least precise.
const (
	WaypointBlock WaypointForm = iota
	WaypointChunk
	WaypointAzimuth
)

// WaypointIgnoresReceiver reports whether a transmitter stays off a
// receiver's bar. A spectator receiver sees everyone; otherwise a spectator
// transmitter is ignored and the rest go by the shorter of the transmitter's
// transmit range and the receiver's receive range.
// vanilla: WaypointTransmitter.doesSourceIgnoreReceiver
func WaypointIgnoresReceiver(transmitterSpectator, receiverSpectator bool, dist, transmitRange, receiveRange float64) bool {
	if receiverSpectator {
		return false
	}
	if transmitterSpectator {
		return true
	}
	return dist >= math.Min(transmitRange, receiveRange)
}

// WaypointFormFor picks the form a new waypoint starts in.
// vanilla: LivingEntity.makeWaypointConnectionWith
func WaypointFormFor(dist float64, chunkVisible bool) WaypointForm {
	switch {
	case dist > WaypointReallyFar:
		return WaypointAzimuth
	case !chunkVisible:
		return WaypointChunk
	}
	return WaypointBlock
}

// WaypointBroken reports whether a held waypoint of the given form has to be
// re-made rather than updated: it drifted more than one step from what it
// last named, or the transmitter is now visible (chunk) or no longer really
// far (azimuth). drift is the Manhattan distance in blocks for the block form
// and the chessboard distance in chunks for the chunk form.
// vanilla: WaypointTransmitter.*Connection.isBroken
func WaypointBroken(form WaypointForm, drift int32, chunkVisible bool, dist float64) bool {
	switch form {
	case WaypointBlock:
		return drift > 1
	case WaypointChunk:
		return drift > 1 || chunkVisible
	}
	return chunkVisible || dist <= WaypointReallyFar
}

// WaypointBearing is the bearing an azimuth waypoint names: the vector from
// transmitter to receiver turned a quarter-turn clockwise, in radians.
// vanilla: EntityAzimuthConnection, Vec3.rotateClockwise90
func WaypointBearing(dx, dz float64) float32 {
	return float32(math.Atan2(dx, -dz))
}

// WaypointView is a transmitter as one receiver's connection sees it.
type WaypointView struct {
	Dist                   float64
	BlockX, BlockY, BlockZ int32
	ChunkX, ChunkZ         int32
	DX, DZ                 float64 // receiver minus transmitter
	// ChunkVisible reports whether a chunk is within the receiver's view.
	ChunkVisible func(chunkX, chunkZ int32) bool
}

// Bearing computes the angle needed by an azimuth connection.
func (v WaypointView) Bearing() float32 {
	return WaypointBearing(v.DX, v.DZ)
}

// WaypointConnection is what one receiver was last told of one transmitter:
// the form and the block, chunk or bearing it named.
// vanilla: WaypointTransmitter.Connection
type WaypointConnection struct {
	Form WaypointForm
	// X, Y and Z hold the block or, in the chunk form, the chunk (X, Z).
	X, Y, Z int32
	Angle   float32
}

// WaypointStep is what a held connection asks of the server this pass.
type WaypointStep int

// The steps.
const (
	WaypointHold    WaypointStep = iota // nothing to send
	WaypointUpdate                      // send an update of the connection as it now stands
	WaypointRetrack                     // make a fresh connection and send a track
)

// NewWaypointConnection makes the connection for a pairing at the form the
// distance and sight name.
// vanilla: LivingEntity.makeWaypointConnectionWith
func NewWaypointConnection(v WaypointView) WaypointConnection {
	c := WaypointConnection{Form: WaypointFormFor(v.Dist, v.ChunkVisible(v.ChunkX, v.ChunkZ))}
	switch c.Form {
	case WaypointAzimuth:
		c.Angle = v.Bearing()
	case WaypointChunk:
		c.X, c.Z = v.ChunkX, v.ChunkZ
	default:
		c.X, c.Y, c.Z = v.BlockX, v.BlockY, v.BlockZ
	}
	return c
}

// Step advances a held connection: it decides whether the connection is
// broken, needs an update or holds, and for an update records what is sent.
// v.ChunkVisible judges the chunk last named, which the chunk form needs.
// vanilla: WaypointTransmitter.*Connection.update, isBroken
func (c *WaypointConnection) Step(v WaypointView) WaypointStep {
	abs := func(a int32) int32 {
		if a < 0 {
			return -a
		}
		return a
	}
	switch c.Form {
	case WaypointBlock:
		drift := abs(v.BlockX-c.X) + abs(v.BlockY-c.Y) + abs(v.BlockZ-c.Z)
		if WaypointBroken(WaypointBlock, drift, false, v.Dist) {
			return WaypointRetrack
		}
		if drift > 0 {
			c.X, c.Y, c.Z = v.BlockX, v.BlockY, v.BlockZ
			return WaypointUpdate
		}
	case WaypointChunk:
		drift := max(abs(v.ChunkX-c.X), abs(v.ChunkZ-c.Z))
		if WaypointBroken(WaypointChunk, drift, v.ChunkVisible(c.X, c.Z), v.Dist) {
			return WaypointRetrack
		}
		if drift > 0 {
			c.X, c.Z = v.ChunkX, v.ChunkZ
			return WaypointUpdate
		}
	default:
		if WaypointBroken(WaypointAzimuth, 0, v.ChunkVisible(v.ChunkX, v.ChunkZ), v.Dist) {
			return WaypointRetrack
		}
		// The turn is measured unwrapped: a step across the far side of the
		// circle is a whole turn, not none.
		angle := v.Bearing()
		if math.Abs(float64(angle-c.Angle)) > WaypointAngleStep {
			c.Angle = angle
			return WaypointUpdate
		}
	}
	return WaypointHold
}
