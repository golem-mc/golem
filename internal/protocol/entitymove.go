package protocol

// PutMoveEntityPos writes move_entity_pos with three i16 deltas in 1/4096 blocks.
// A positive stepTicks encodes a one-step VecDelta with that tick offset.
// vanilla: ClientboundMoveEntityPacket.Pos, VecDelta.write
func PutMoveEntityPos(b *Buffer, id int32, dx, dy, dz int16, stepTicks int32, onGround bool) {
	b.PutVarInt(id)
	var properties int32
	if onGround {
		properties = 1
	}
	if stepTicks > 0 {
		b.PutVarInt(properties | 1<<1)
		b.PutVarInt(stepTicks)
	} else {
		b.PutVarInt(properties)
	}
	b.PutI16(dx)
	b.PutI16(dy)
	b.PutI16(dz)
}

// PutMoveEntityPosRot writes move_entity_pos_rot: the Pos layout, then the angles.
// vanilla: ClientboundMoveEntityPacket.PosRot
func PutMoveEntityPosRot(b *Buffer, id int32, dx, dy, dz int16, yaw, pitch uint8, stepTicks int32, onGround bool) {
	PutMoveEntityPos(b, id, dx, dy, dz, stepTicks, onGround)
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

// PutEntityPositionSyncStepped writes entity_position_sync with a STEPPED
// path of one step that takes stepTicks ticks to reach the position.
// vanilla: ClientboundEntityPositionSyncPacket, PositionPath, PositionStep
func PutEntityPositionSyncStepped(b *Buffer, id int32, x, y, z float64, stepTicks int32, yaw, pitch float32, onGround bool) {
	b.PutVarInt(id)
	b.PutVarInt(1)
	b.PutVarInt(1)
	b.PutF64(x)
	b.PutF64(y)
	b.PutF64(z)
	b.PutVarInt(stepTicks)
	b.PutF32(yaw)
	b.PutF32(pitch)
	b.PutBool(onGround)
}
