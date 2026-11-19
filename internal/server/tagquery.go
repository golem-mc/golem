package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/nbt"
)

// handleBlockEntityTagQuery answers F3+I on a block: a gamemaster gets the
// block entity's compound without id and position (empty if none); anyone
// else gets nothing. vanilla: ServerGamePacketListenerImpl.handleBlockEntityTagQuery
func (c *client) handleBlockEntityTagQuery(r *protocol.Reader) error {
	transaction := r.ReadVarInt()
	x, y, z := r.ReadPosition()
	if err := r.Err(); err != nil {
		return c.decodeFailure("block_entity_tag_query", err)
	}
	if c.PermLevel() < command.PermLevelGamemasters {
		return nil
	}
	var reply nbt.Tag
	if tag, ok := c.theWorld().BlockEntityTag(x, y, z); ok {
		out := make(nbt.Compound, len(tag))
		for k, v := range tag {
			switch k {
			case "id", "x", "y", "z":
			default:
				out[k] = v
			}
		}
		reply = out
	}
	return c.sendTagQuery(transaction, reply)
}

// handleEntityTagQuery answers F3+I on an entity: a gamemaster gets its
// saved compound, nothing if no such entity is in the player's dimension.
// vanilla: ServerGamePacketListenerImpl.handleEntityTagQuery
func (c *client) handleEntityTagQuery(r *protocol.Reader) error {
	transaction := r.ReadVarInt()
	id := r.ReadVarInt()
	if err := r.Err(); err != nil {
		return c.decodeFailure("entity_tag_query", err)
	}
	if c.PermLevel() < command.PermLevelGamemasters {
		return nil
	}
	var tag nbt.CompoundTag
	var ok bool
	switch e := c.tagQueryEntity(id).(type) {
	case *client:
		// The query sends saveWithoutId; only /data get adds SelectedItem.
		tag, ok = e.savedTag()
	case command.DataTarget:
		tag, ok = e.DataTag()
	}
	if !ok {
		return nil
	}
	return c.sendTagQuery(transaction, tag)
}

// tagQueryEntity is the entity with the ID in the player's dimension:
// players and mobs directly, other kinds via the filtered scan.
// vanilla: ServerGamePacketListenerImpl.handleEntityTagQuery
func (c *client) tagQueryEntity(id int32) command.Entity {
	dim := c.dimension()
	for _, p := range c.srv.playerClientsIn(dim.name) {
		if p.player != nil && p.EntityID() == id {
			return p
		}
	}
	s := c.srv
	s.mobsMu.Lock()
	for _, ent := range dim.mobs.book {
		if ent.mob.ID == id {
			snap := s.mobSnapshot(ent, dim.name, nil)
			s.mobsMu.Unlock()
			return snap
		}
	}
	s.mobsMu.Unlock()
	return c.cameraCommandByID(id)
}

// sendTagQuery sends the Tag Query Response; a nil tag goes as TAG_End.
func (c *client) sendTagQuery(transaction int32, tag nbt.Tag) error {
	var body []byte
	if tag == nil {
		body = []byte{0}
	} else {
		var err error
		if body, err = nbt.EncodeNetwork(tag); err != nil {
			return err
		}
	}
	var b protocol.Buffer
	b.PutVarInt(transaction)
	b.PutRaw(body)
	c.send(protocol.IDPlayTagQuery, b.Bytes())
	return nil
}
