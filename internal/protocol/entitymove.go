package protocol

// moveProperties is the properties varint of a move_entity_pos packet:
// onGround in bit 0, the interpolation step count above it. The server always
// sends step count 0, the plain three-delta form.
func moveProperties(onGround bool) int32 {
	if onGround {
		return 1
	}
	return 0
}

// PutMoveEntityPos writes move_entity_pos: three i16 deltas in 1/4096 blocks.
// vanilla: ClientboundMoveEntityPacket.Pos
func PutMoveEntityPos(b *Buffer, id int32, dx, dy, dz int16, onGround bool) {
	b.PutVarInt(id)
	b.PutVarInt(moveProperties(onGround))
	b.PutI16(dx)
	b.PutI16(dy)
	b.PutI16(dz)
}

// PutMoveEntityPosRot writes move_entity_pos_rot: the Pos layout, then the angles.
// vanilla: ClientboundMoveEntityPacket.PosRot
func PutMoveEntityPosRot(b *Buffer, id int32, dx, dy, dz int16, yaw, pitch uint8, onGround bool) {
	PutMoveEntityPos(b, id, dx, dy, dz, onGround)
	b.PutU8(yaw)
	b.PutU8(pitch)
}

// PutMoveEntityRot writes move_entity_rot; unlike the position forms it
// carries onGround as a plain bool before the angles.
// vanilla: ClientboundMoveEntityPacket.Rot
func PutMoveEntityRot(b *Buffer, id int32, yaw, pitch uint8, onGround bool) {
	b.PutVarInt(id)
	b.PutBool(onGround)
	b.PutU8(yaw)
	b.PutU8(pitch)
}

// PutEntityPositionSync writes entity_position_sync with a LINEAR position
// path (varint 0, then x, y, z) and no velocity.
// vanilla: ClientboundEntityPositionSyncPacket
func PutEntityPositionSync(b *Buffer, id int32, x, y, z float64, yaw, pitch float32, onGround bool) {
	b.PutVarInt(id)
	b.PutVarInt(0)
	b.PutF64(x)
	b.PutF64(y)
	b.PutF64(z)
	b.PutF32(yaw)
	b.PutF32(pitch)
	b.PutBool(onGround)
}
