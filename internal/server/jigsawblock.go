package server

import (
	"errors"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// kindJigsaw is the jigsaw block's entity id.
const kindJigsaw = "minecraft:jigsaw"

// handleSetJigsawBlock handles Set Jigsaw Block: a creative gamemaster sets
// the block entity's seven fields.
// vanilla: ServerGamePacketListenerImpl.handleSetJigsawBlock
func (c *client) handleSetJigsawBlock(r *protocol.Reader) error {
	x, y, z := r.ReadPosition()
	name := r.ReadString(32767)
	target := r.ReadString(32767)
	pool := r.ReadString(32767)
	finalState := r.ReadString(32767)
	joint := r.ReadString(32767)
	selection := r.ReadVarInt()
	placement := r.ReadVarInt()
	if err := r.Err(); err != nil {
		return c.decodeFailure("set_jigsaw_block", err)
	}
	// vanilla: FriendlyByteBuf.readIdentifier
	if !protocol.ValidIdentifier(name) || !protocol.ValidIdentifier(target) || !protocol.ValidIdentifier(pool) {
		return c.decodeFailure("set_jigsaw_block", errors.New("jigsaw block packet carries an invalid identifier"))
	}
	if !c.canUseGameMasterBlocks() {
		return nil
	}
	w := c.theWorld()
	tag, ok := w.BlockEntityTag(x, y, z)
	switch {
	case ok:
		if id, _ := nbt.Get[nbt.String](tag, "id"); id != kindJigsaw {
			return nil
		}
	case registry.BlockOf(w.BlockAt(x, y, z)) == registry.BlockJigsaw:
		tag = nbt.Compound{}
	default:
		return nil
	}
	// An unknown joint name is aligned. vanilla: JigsawBlock.JointType.byName
	if joint != "rollable" {
		joint = "aligned"
	}
	tag["name"] = nbt.String(name)
	tag["target"] = nbt.String(target)
	tag["pool"] = nbt.String(pool)
	tag["final_state"] = nbt.String(finalState)
	tag["joint"] = nbt.String(joint)
	tag["selection_priority"] = nbt.Int(selection)
	tag["placement_priority"] = nbt.Int(placement)
	w.SetBlockEntityTag(x, y, z, kindJigsaw, tag)
	c.srv.markChanged(w, x, y, z)
	c.srv.tellBlockEntity(w, x, y, z, kindJigsaw, false)
	return nil
}

// handleJigsawGenerate handles Jigsaw Generate: a gamemaster grows the
// assembly the jigsaw block's pool and target name, from the block it faces.
// vanilla: ServerGamePacketListenerImpl.handleJigsawGenerate, JigsawBlockEntity.generate
func (c *client) handleJigsawGenerate(r *protocol.Reader) error {
	x, y, z := r.ReadPosition()
	levels := r.ReadVarInt()
	keep := r.ReadBool()
	if err := r.Err(); err != nil {
		return c.decodeFailure("jigsaw_generate", err)
	}
	if !c.canUseGameMasterBlocks() {
		return nil
	}
	w := c.theWorld()
	tag, ok := w.BlockEntityTag(x, y, z)
	if !ok {
		return nil
	}
	if id, _ := nbt.Get[nbt.String](tag, "id"); id != kindJigsaw {
		return nil
	}
	dx, dy, dz := world.JigsawFrontStep(w.BlockAt(x, y, z))
	// An unset pool or target reads as minecraft:empty.
	// vanilla: JigsawBlockEntity.EMPTY_ID
	pool, _ := nbt.Get[nbt.String](tag, "pool")
	target, _ := nbt.Get[nbt.String](tag, "target")
	if target == "" {
		target = "minecraft:empty"
	}
	ws := worldScope{srv: c.srv, in: func() *dimension { return c.srv.dimensionOfWorld(w) }, quiet: true}
	ws.generateJigsaw(registry.Qualify(string(pool)), registry.Qualify(string(target)), int(levels), x+dx, y+dy, z+dz, keep)
	return nil
}
