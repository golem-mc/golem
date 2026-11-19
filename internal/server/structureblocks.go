package server

import (
	"errors"
	"log/slog"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// structureBlockPlaced creates a placed structure block's entity.
// vanilla: StructureBlock.setPlacedBy
func (c *client) structureBlockPlaced(w *world.World, b registry.Block, x, y, z int32) {
	if b != registry.BlockStructureBlock {
		return
	}
	if s, _, ok := w.StructureBlockAt(x, y, z); ok {
		s.Author = c.Profile().Name
		c.srv.storeStructureBlock(w, x, y, z, s)
	}
}

// storeStructureBlock writes a structure block's data, keeping the
// compound's other keys.
func (s *Server) storeStructureBlock(w *world.World, x, y, z int32, sb world.StructureBlock) {
	tag, _ := w.BlockEntityTag(x, y, z)
	w.SetBlockEntityTag(x, y, z, world.KindStructureBlock, world.MergeStructureBlock(tag, sb))
}

// structureFlag bits of the Program Structure Block packet.
// vanilla: ServerboundSetStructureBlockPacket
const (
	structureFlagIgnoreEntities = 0x01
	structureFlagShowAir        = 0x02
	structureFlagShowBox        = 0x04
	structureFlagStrict         = 0x08
)

// The packet's update types.
// vanilla: StructureBlockEntity.UpdateType
const (
	structureUpdate = iota
	structureSaveArea
	structureLoadArea
	structureScanArea
)

// handleSetStructureBlock handles Program Structure Block: a creative
// gamemaster sets the fields and runs the action.
// vanilla: ServerGamePacketListenerImpl.handleSetStructureBlock
func (c *client) handleSetStructureBlock(r *protocol.Reader) error {
	x, y, z := r.ReadPosition()
	action := r.ReadVarInt()
	mode := r.ReadVarInt()
	name := r.ReadString(32767)
	var off, size [3]int32
	for i := range off {
		off[i] = min(max(int32(r.ReadI8()), -world.MaxStructureAxis), world.MaxStructureAxis)
	}
	for i := range size {
		size[i] = min(max(int32(r.ReadI8()), 0), world.MaxStructureAxis)
	}
	mirror := r.ReadVarInt()
	rotation := r.ReadVarInt()
	data := r.ReadString(128)
	integrity := min(max(r.ReadF32(), 0), 1)
	seed := r.ReadVarLong()
	flags := r.ReadU8()
	if err := r.Err(); err != nil {
		return c.decodeFailure("set_structure_block", err)
	}
	// An ordinal outside a readEnum enum fails the decode; the rotation wraps.
	// vanilla: FriendlyByteBuf.readEnum, Rotation.BY_ID
	if action < 0 || action > structureScanArea || mode < 0 || mode > int32(world.StructureData) ||
		mirror < 0 || mirror > int32(world.MirrorFrontBack) {
		return c.decodeFailure("set_structure_block", errors.New("structure block packet carries an ordinal outside its enum"))
	}
	rotation = protocol.IDWrap(rotation, int32(world.RotateCounterclockwise90)+1)
	// vanilla: Player.canUseGameMasterBlocks
	if !c.canUseGameMasterBlocks() {
		return nil
	}
	w := c.theWorld()
	sb, _, ok := w.StructureBlockAt(x, y, z)
	if !ok {
		return nil
	}
	sb.Mode = world.StructureMode(mode)
	sb.Name = name
	sb.Pos, sb.Size = off, size
	sb.Mirror, sb.Rotation = world.Mirror(mirror), world.Rotation(rotation)
	sb.Metadata = data
	sb.IgnoreEntities = flags&structureFlagIgnoreEntities != 0
	sb.Strict = flags&structureFlagStrict != 0
	sb.ShowAir = flags&structureFlagShowAir != 0
	sb.ShowBox = flags&structureFlagShowBox != 0
	sb.Integrity, sb.Seed = integrity, seed
	id, named := world.ParseStructureName(name)
	if !named {
		sb.Name = ""
	} else {
		sb.Name = id
	}
	c.setStructureMode(w, x, y, z, sb.Mode)
	switch {
	case !named:
		_ = c.Message(protocol.Text{Translate: "structure_block.invalid_structure_name", With: []protocol.Text{{Text: name}}})
	case action == structureSaveArea:
		key := "structure_block.save_failure"
		if sb.Mode == world.StructureSave && sb.Save(w, c.srv.structures, [3]int32{x, y, z}, true, c.srv.entitiesToSave(w)) {
			key = "structure_block.save_success"
		}
		_ = c.Message(protocol.Text{Translate: key, With: []protocol.Text{{Text: id}}})
	case action == structureLoadArea:
		t, found := c.srv.structures.Get(id)
		switch {
		case sb.Mode != world.StructureLoad || !found:
			_ = c.Message(protocol.Text{Translate: "structure_block.load_not_found", With: []protocol.Text{{Text: id}}})
		case t.Size == sb.Size:
			c.placeStructure(w, &sb, t, x, y, z)
			_ = c.Message(protocol.Text{Translate: "structure_block.load_success", With: []protocol.Text{{Text: id}}})
		default:
			sb.LoadInfo(t)
			_ = c.Message(protocol.Text{Translate: "structure_block.load_prepare", With: []protocol.Text{{Text: id}}})
		}
	case action == structureScanArea:
		if sb.DetectSize(w, [3]int32{x, y, z}) {
			_ = c.Message(protocol.Text{Translate: "structure_block.size_success", With: []protocol.Text{{Text: id}}})
		} else {
			_ = c.Message(protocol.Text{Translate: "structure_block.size_failure"})
		}
	}
	c.srv.storeStructureBlock(w, x, y, z, sb)
	c.srv.tellBlockEntity(w, x, y, z, world.KindStructureBlock, false)
	return nil
}

// setStructureMode switches the block's mode property.
// vanilla: StructureBlockEntity.setMode
func (c *client) setStructureMode(w *world.World, x, y, z int32, mode world.StructureMode) {
	state, ok := registry.WithProp(registry.BlockStructureBlock.DefaultState(), registry.PropMode, mode.StateName())
	if ok && state != w.BlockAt(x, y, z) && w.SetBlock(x, y, z, state) {
		c.srv.markChanged(w, x, y, z)
	}
}

// placeStructure loads the template's size and author onto the block and
// places it at the block's offset. vanilla: StructureBlockEntity.placeStructure
func (ws worldScope) placeStructure(w *world.World, sb *world.StructureBlock, t *world.Template, x, y, z int32) {
	sb.LoadInfo(t)
	p := world.TemplatePlacement{
		Origin:    [3]int32{x + sb.Pos[0], y + sb.Pos[1], z + sb.Pos[2]},
		Rotation:  sb.Rotation,
		Mirror:    sb.Mirror,
		Integrity: min(max(sb.Integrity, 0), 1),
		Seed:      sb.Seed,

		IgnoreEntities: sb.IgnoreEntities,
	}
	if placed, ok := t.Place(p); ok {
		ws.PlaceTemplate(placed, sb.Strict)
		command.PlaceTemplateEntities(ws, placed.Entities, slog.Debug)
	}
}

// entitiesToSave reads w's entities for a template, excluding players, whom
// the selector list holds as clients, not snapshots.
// vanilla: StructureTemplate.fillEntityList
func (s *Server) entitiesToSave(w *world.World) world.EntityReader {
	return func(lo, hi [3]int32) []world.CapturedEntity {
		dim := s.dimensionOfWorld(w).name
		var out []world.CapturedEntity
		for _, e := range (worldScope{srv: s}).Entities(command.EntityQuery{}) {
			snap, ok := e.(*entitySnapshot)
			if !ok || snap.dim != dim {
				continue
			}
			b := snap.box
			if b[0] >= float64(hi[0]) || b[3] <= float64(lo[0]) || b[1] >= float64(hi[1]) ||
				b[4] <= float64(lo[1]) || b[2] >= float64(hi[2]) || b[5] <= float64(lo[2]) {
				continue
			}
			// Entity.save leaves a passenger's output empty, but
			// StructureTemplate.fillEntityList still keeps its entry.
			tag := nbt.Compound{}
			if _, passenger := snap.Vehicle(); !passenger {
				saved, got := snap.DataTag()
				if !got {
					continue
				}
				saved.Set("id", nbt.String(snap.EntityType()))
				tag = saved.Entries()
			}
			out = append(out, world.CapturedEntity{Pos: [3]float64{snap.pos.X, snap.pos.Y, snap.pos.Z}, NBT: tag})
		}
		return out
	}
}

// runStructureTrigger runs a structure block's power-rise action after the
// physics drain rather than inside the neighbour change: placing a template
// writes cells and broadcasts, which the redstone batch cannot.
// vanilla: StructureBlock.trigger
func (s *Server) runStructureTrigger(w *world.World, t world.StructureTrigger) {
	sb, _, ok := w.StructureBlockAt(t.X, t.Y, t.Z)
	if !ok {
		return
	}
	ws := worldScope{srv: s, in: func() *dimension { return s.dimensionOfWorld(w) }, quiet: true}
	id, named := world.ParseStructureName(sb.Name)
	switch sb.Mode {
	case world.StructureSave:
		sb.Save(w, s.structures, [3]int32{t.X, t.Y, t.Z}, false, s.entitiesToSave(w))
	case world.StructureLoad:
		if template, found := s.structures.Get(id); named && found {
			ws.placeStructure(w, &sb, template, t.X, t.Y, t.Z)
		}
	case world.StructureCorner:
		if named {
			s.structures.Remove(id)
		}
	}
	s.storeStructureBlock(w, t.X, t.Y, t.Z, sb)
}
