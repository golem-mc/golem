package server

import (
	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
)

// physicsHooks answers Physics' questions from the plugin listeners.
type physicsHooks struct {
	s   *Server
	dim *dimension
}

// physicsHookIDs maps a decision kind to its listener bit.
var physicsHookIDs = [...]int{
	world.HookBurn:      pluginruntime.BlockBurn,
	world.HookIgnite:    pluginruntime.BlockIgnite,
	world.HookSpread:    pluginruntime.BlockSpread,
	world.HookForm:      pluginruntime.BlockForm,
	world.HookFade:      pluginruntime.BlockFade,
	world.HookGrow:      pluginruntime.BlockGrow,
	world.HookFromTo:    pluginruntime.BlockFromTo,
	world.HookPhysics:   pluginruntime.BlockPhysics,
	world.HookRedstone:  pluginruntime.BlockRedstone,
	world.HookPiston:    pluginruntime.BlockPiston,
	world.HookLeaves:    pluginruntime.LeavesDecay,
	world.HookSponge:    pluginruntime.SpongeAbsorb,
	world.HookNote:      pluginruntime.NotePlay,
	world.HookCauldron:  pluginruntime.CauldronLevelChange,
	world.HookFertilize: pluginruntime.BlockFertilize,
	world.HookStructure: pluginruntime.StructureGrow,
	world.HookMoisture:  pluginruntime.MoistureChange,
}

func (h physicsHooks) Listening(kind world.HookKind) bool {
	return h.s.plugins != nil && h.s.plugins.Has(physicsHookIDs[kind])
}

func (h physicsHooks) block(x, y, z int32) plugin.Block {
	w := pluginWorld{srv: h.s, dim: h.dim, host: h.s.pluginAdapter}
	return plugin.Block{World: w, Pos: plugin.BlockPos{X: int(x), Y: int(y), Z: int(z)}}
}

func (h physicsHooks) blockAt(p [3]int32) plugin.Block { return h.block(p[0], p[1], p[2]) }

// Decide fires the event on the tick goroutine, whichever goroutine physics
// runs on, and copies the listeners' edits back.
func (h physicsHooks) Decide(d *world.HookDecision) {
	if h.s.plugins.OnTick() {
		h.fire(d)
		return
	}
	h.s.plugins.Call(func() { h.fire(d) })
}

func (h physicsHooks) fire(d *world.HookDecision) {
	pl := h.s.plugins
	at := h.block(d.X, d.Y, d.Z)
	switch d.Kind {
	case world.HookBurn:
		src := h.blockAt(d.From)
		e := &plugin.BlockBurnEvent{Block: at, Fire: &src}
		pl.Fire(e)
		d.Cancel = e.Cancelled()
	case world.HookIgnite:
		var cause plugin.IgniteCause
		var src *plugin.Block
		switch d.Cause {
		case world.HookCauseLava:
			cause = plugin.IgniteLava
			b := h.blockAt(d.From)
			src = &b
		case world.HookCauseSpread:
			cause = plugin.IgniteSpread
			b := h.blockAt(d.From)
			src = &b
		default:
			cause = plugin.IgniteSpread
		}
		e := &plugin.BlockIgniteEvent{Block: at, Cause: cause, Source: src}
		pl.Fire(e)
		d.Cancel = e.Cancelled()
	case world.HookSpread:
		e := &plugin.BlockSpreadEvent{Block: at, Source: h.blockAt(d.From), NewState: plugin.BlockState(d.New)}
		pl.Fire(e)
		d.Cancel, d.New = e.Cancelled(), uint16(e.NewState)
	case world.HookForm:
		e := &plugin.BlockFormEvent{Block: at, NewState: plugin.BlockState(d.New)}
		pl.Fire(e)
		d.Cancel, d.New = e.Cancelled(), uint16(e.NewState)
	case world.HookFade:
		e := &plugin.BlockFadeEvent{Block: at, NewState: plugin.BlockState(d.New)}
		pl.Fire(e)
		d.Cancel, d.New = e.Cancelled(), uint16(e.NewState)
	case world.HookGrow:
		e := &plugin.BlockGrowEvent{Block: at, NewState: plugin.BlockState(d.New)}
		pl.Fire(e)
		d.Cancel, d.New = e.Cancelled(), uint16(e.NewState)
	case world.HookFromTo:
		e := &plugin.BlockFromToEvent{From: h.blockAt(d.From), To: at, Face: faceBetween(d.From, d.X, d.Y, d.Z)}
		pl.Fire(e)
		d.Cancel = e.Cancelled()
	case world.HookPhysics:
		e := &plugin.BlockPhysicsEvent{Block: at, Source: h.blockAt(d.From), State: plugin.BlockState(d.Old)}
		pl.Fire(e)
		d.Cancel = e.Cancelled()
	case world.HookRedstone:
		e := &plugin.BlockRedstoneEvent{Block: at, Old: d.OldPower, New: d.Power}
		pl.Fire(e)
		d.Power = e.New
	case world.HookPiston:
		e := &plugin.BlockPistonEvent{Piston: at, Direction: plugin.BlockFace(d.Dir), Extending: d.Extending}
		for _, b := range d.Blocks {
			e.Blocks = append(e.Blocks, h.blockAt(b))
		}
		pl.Fire(e)
		d.Cancel = e.Cancelled()
	case world.HookLeaves:
		e := &plugin.LeavesDecayEvent{Block: at}
		pl.Fire(e)
		d.Cancel = e.Cancelled()
	case world.HookSponge:
		e := &plugin.SpongeAbsorbEvent{Block: at}
		for _, b := range d.Blocks {
			e.Blocks = append(e.Blocks, plugin.BlockPos{X: int(b[0]), Y: int(b[1]), Z: int(b[2])})
		}
		pl.Fire(e)
		d.Cancel = e.Cancelled()
		d.Blocks = d.Blocks[:0]
		for _, b := range e.Blocks {
			d.Blocks = append(d.Blocks, [3]int32{int32(b.X), int32(b.Y), int32(b.Z)})
		}
	case world.HookCauldron:
		e := &plugin.CauldronLevelChangeEvent{Block: at, NewState: plugin.BlockState(d.New)}
		pl.Fire(e)
		d.Cancel, d.New = e.Cancelled(), uint16(e.NewState)
	case world.HookFertilize:
		e := &plugin.BlockFertilizeEvent{Block: at, Blocks: h.changes(d.Cells)}
		h.setPlayer(&e.Player, d.Actor)
		pl.Fire(e)
		d.Cancel, d.Cells = e.Cancelled(), h.cells(e.Blocks)
	case world.HookStructure:
		e := &plugin.StructureGrowEvent{World: at.World, Pos: at.Pos, Blocks: h.changes(d.Cells)}
		h.setPlayer(&e.Player, d.Actor)
		pl.Fire(e)
		d.Cancel, d.Cells = e.Cancelled(), h.cells(e.Blocks)
	case world.HookMoisture:
		e := &plugin.MoistureChangeEvent{Block: at, NewState: plugin.BlockState(d.New)}
		pl.Fire(e)
		d.Cancel, d.New = e.Cancelled(), uint16(e.NewState)
	case world.HookNote:
		e := &plugin.NotePlayEvent{Block: at, Instrument: d.Instrument, Note: d.Note}
		pl.Fire(e)
		d.Cancel, d.Instrument, d.Note = e.Cancelled(), e.Instrument, e.Note
	}
}

// faceBetween is the face of from that looks at x, y, z; Down when they are
// not neighbours.
func faceBetween(from [3]int32, x, y, z int32) plugin.BlockFace {
	dx, dy, dz := x-from[0], y-from[1], z-from[2]
	for f := plugin.Down; f <= plugin.East; f++ {
		if v := f.Direction(); v.X == int(dx) && v.Y == int(dy) && v.Z == int(dz) {
			return f
		}
	}
	return plugin.Down
}

func (h physicsHooks) changes(cells []world.BlockChange) []plugin.BlockChange {
	out := make([]plugin.BlockChange, len(cells))
	for i, c := range cells {
		out[i] = plugin.BlockChange{Pos: plugin.BlockPos{X: int(c.X), Y: int(c.Y), Z: int(c.Z)}, State: plugin.BlockState(c.State)}
	}
	return out
}

func (h physicsHooks) cells(changes []plugin.BlockChange) []world.BlockChange {
	out := make([]world.BlockChange, len(changes))
	for i, c := range changes {
		out[i] = world.BlockChange{X: int32(c.Pos.X), Y: int32(c.Pos.Y), Z: int32(c.Pos.Z), State: uint16(c.State)}
	}
	return out
}

// setPlayer fills a listener's Player with the player behind id, nil for none.
func (h physicsHooks) setPlayer(dst *plugin.Player, id uuid.UUID) {
	if c := h.s.clientByUUID(id); c != nil {
		*dst = pluginPlayer{host: h.s.pluginAdapter, c: c}
	}
}
