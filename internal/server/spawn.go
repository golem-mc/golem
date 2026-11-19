package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/google/uuid"
)

// spawnEntity holds wire values; angles are already quantized so a tracked
// baseline can be sent without a second conversion.
type spawnEntity struct {
	id, typ             int32
	uuid                uuid.UUID
	x, y, z             float64
	vx, vy, vz          float64
	pitch, yaw, headYaw uint8
	data                int32
}

// spawnEntityPayload encodes Spawn Entity; metadata and pairing order are
// supplied by the caller.
// vanilla: ClientboundAddEntityPacket
func spawnEntityPayload(v spawnEntity) []byte {
	var b protocol.Buffer
	// Ordinary IDs, coordinates and packed motion fit in one allocation.
	b.Grow(64)
	b.PutVarInt(v.id)
	b.PutUUID(v.uuid)
	b.PutVarInt(v.typ)
	b.PutF64(v.x)
	b.PutF64(v.y)
	b.PutF64(v.z)
	b.PutLpVec3(v.vx, v.vy, v.vz)
	b.PutU8(v.pitch)
	b.PutU8(v.yaw)
	b.PutU8(v.headYaw)
	b.PutVarInt(v.data)
	return b.Bytes()
}
