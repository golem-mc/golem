package server

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// blockEntityEvents asks the plugin listeners about what a ticking block
// entity is about to do. Every method runs inside the entity's Tick with its
// lock held, so it releases the lock for the listeners and re-reads what it
// was asked about afterwards.
type blockEntityEvents struct {
	s   *Server
	dim *dimension
}

// blockEntityEvents is nil unless a plugin listens for one of the block entity
// events, so a tick without listeners pays one nil check.
func (s *Server) blockEntityEvents(w *world.World) *blockEntityEvents {
	if s.plugins == nil {
		return nil
	}
	if !s.plugins.Has(pluginruntime.BlockCook) && !s.plugins.Has(pluginruntime.FurnaceBurn) && !s.plugins.Has(pluginruntime.Brew) {
		return nil
	}
	return &blockEntityEvents{s: s, dim: s.dimensionOfWorld(w)}
}

func (h *blockEntityEvents) block(be *world.BlockEntity) plugin.Block {
	w := pluginWorld{srv: h.s, dim: h.dim, host: h.s.pluginAdapter}
	return plugin.Block{World: w, Pos: plugin.BlockPos{X: int(be.X), Y: int(be.Y), Z: int(be.Z)}}
}

// fire runs e's listeners with be unlocked and reports whether be is still in
// the world afterwards.
func (h *blockEntityEvents) fire(be *world.BlockEntity, e plugin.Event) bool {
	be.Unlocked(func() {
		h.s.firePluginEvent(e)
	})
	return !be.Removed()
}

func sameStack(a, b protocol.ItemStack) bool { return a.ItemID == b.ItemID && a.Count == b.Count }

// burn is the burn time a furnace gets from its fuel: 0 when a listener
// cancels, so the fuel is not spent.
// vanilla: AbstractFurnaceBlockEntity.serverTick, getBurnDuration
func (h *blockEntityEvents) burn(be *world.BlockEntity, fuel protocol.ItemStack, burn int32) int32 {
	if !h.s.plugins.Has(pluginruntime.FurnaceBurn) {
		return burn
	}
	e := &plugin.FurnaceBurnEvent{Block: h.block(be), Fuel: pluginStack(fuel), BurnTime: int(burn)}
	if !h.fire(be, e) || e.Cancelled() || !sameStack(be.Slots[1], fuel) {
		return 0
	}
	return int32(max(e.BurnTime, 0))
}

// cook is the stack a finished smelt puts in the output slot; ok is false when
// a listener cancelled it or the input changed while they ran.
// vanilla: AbstractFurnaceBlockEntity.burn
func (h *blockEntityEvents) cook(be *world.BlockEntity, source, result protocol.ItemStack) (protocol.ItemStack, bool) {
	if !h.s.plugins.Has(pluginruntime.BlockCook) {
		return result, true
	}
	e := &plugin.BlockCookEvent{Block: h.block(be), Source: pluginStack(source), Result: pluginStack(result)}
	if !h.fire(be, e) || e.Cancelled() || !sameStack(be.Slots[0], source) {
		return result, false
	}
	out, _ := pluginProtocolStack(e.Result)
	return out, true
}

// brew is the three bottle slots a finished brew pours; ok is false when a
// listener cancelled it or the ingredient changed while they ran.
// vanilla: BrewingStandBlockEntity.doBrew
func (h *blockEntityEvents) brew(be *world.BlockEntity, ingredient protocol.ItemStack, mixed [3]protocol.ItemStack) ([3]protocol.ItemStack, bool) {
	if !h.s.plugins.Has(pluginruntime.Brew) {
		return mixed, true
	}
	e := &plugin.BrewEvent{Block: h.block(be)}
	for i, m := range mixed {
		e.Results[i] = pluginStack(m)
	}
	if !h.fire(be, e) || e.Cancelled() || !sameStack(be.Slots[3], ingredient) {
		return mixed, false
	}
	for i := range mixed {
		mixed[i], _ = pluginProtocolStack(e.Results[i])
	}
	return mixed, true
}

// firePluginEvent runs e's listeners on the tick goroutine, from whichever
// goroutine the caller is on, holding none of the server's locks.
func (s *Server) firePluginEvent(e plugin.Event) {
	if s.plugins.OnTick() {
		s.plugins.Fire(e)
		return
	}
	s.plugins.Call(func() { s.plugins.Fire(e) })
}

// blockExplodeEvent asks the listeners about a block explosion once its
// blocks are known: a cancel keeps every block, the list may be trimmed and the
// yield changed.
// vanilla: ServerExplosion.explode (a bed or respawn anchor out of its dimension)
func (s *Server) blockExplodeEvent(b blast, in *blastInputs) blast {
	if s.plugins == nil || !s.plugins.Has(pluginruntime.BlockExplode) {
		return b
	}
	at := s.blastBlockAt(b)
	yield := blastYield(b)
	e := &plugin.BlockExplodeEvent{Block: at(b.source.x, b.source.y, b.source.z), State: plugin.BlockState(b.source.state), Yield: yield}
	e.Blocks = blastBlocksOf(in, at)
	s.firePluginEvent(e)
	return keepBlastBlocks(b, in, e.Cancelled(), e.Blocks, e.Yield, yield)
}

// blastEntityType is the exploder's type, -1 for a blast without one.
func blastEntityType(b blast) registry.EntityType {
	if t, ok := registry.EntityTypeByName(b.directType); ok {
		return t
	}
	return -1
}

func (s *Server) blastLocation(b blast) plugin.Location {
	return plugin.Location{World: pluginWorld{srv: s, dim: s.dimensionOfWorld(b.w), host: s.pluginAdapter}, Pos: plugin.Vec3{X: b.x, Y: b.y, Z: b.z}}
}

func (s *Server) blastBlockAt(b blast) func(x, y, z int32) plugin.Block {
	w := pluginWorld{srv: s, dim: s.dimensionOfWorld(b.w), host: s.pluginAdapter}
	return func(x, y, z int32) plugin.Block {
		return plugin.Block{World: w, Pos: plugin.BlockPos{X: int(x), Y: int(y), Z: int(z)}}
	}
}

// blastYield is the share of destroyed blocks that drop before a listener
// changes it.
func blastYield(b blast) float32 {
	if b.interaction == rules.ExplosionDecay && b.radius > 0 {
		return float32(1 / b.radius)
	}
	return 1
}

func blastBlocksOf(in *blastInputs, at func(x, y, z int32) plugin.Block) []plugin.Block {
	blocks := make([]plugin.Block, 0, len(in.destroyed))
	for _, c := range in.destroyed {
		blocks = append(blocks, at(c[0], c[1], c[2]))
	}
	return blocks
}

// keepBlastBlocks applies a listener's verdict to the blast: a cancel keeps
// every block, otherwise the trimmed list and a changed yield stand.
func keepBlastBlocks(b blast, in *blastInputs, cancelled bool, blocks []plugin.Block, yield, was float32) blast {
	if cancelled {
		in.destroyed = nil
		in.cancelled = true
		return b
	}
	kept := make(rules.BlastBlocks, 0, len(blocks))
	for _, blk := range blocks {
		kept = append(kept, [3]int32{int32(blk.Pos.X), int32(blk.Pos.Y), int32(blk.Pos.Z)})
	}
	in.destroyed = kept
	if yield != was {
		b.yield = &yield
	}
	return b
}

// explosionPrime asks ExplosionPrimeEvent for an entity's blast before it
// runs: the radius and fire may change, and a cancel stops the blast. The
// exploder itself is already gone, as a primed TNT's is.
// vanilla: PrimedTnt.explode, Creeper.explodeCreeper
func (s *Server) explosionPrime(b blast) (blast, bool) {
	if b.directUUID == (uuid.UUID{}) || s.plugins == nil || !s.plugins.Has(pluginruntime.ExplosionPrime) {
		return b, true
	}
	e := &plugin.ExplosionPrimeEvent{Entity: pluginEntity{host: s.pluginAdapter, id: b.directUUID}, Type: blastEntityType(b), Location: s.blastLocation(b), Radius: float32(b.radius), Fire: b.fire}
	s.firePluginEvent(e)
	if e.Cancelled() {
		return b, false
	}
	if e.Radius != float32(b.radius) {
		b.radius = float64(e.Radius)
	}
	b.fire = e.Fire
	return b, true
}

// entityExplodeEvent asks EntityExplodeEvent about an entity's blast once its
// blocks are known, as blockExplodeEvent does for a block's.
// vanilla: ServerExplosion.explode
func (s *Server) entityExplodeEvent(b blast, in *blastInputs) blast {
	if s.plugins == nil || !s.plugins.Has(pluginruntime.EntityExplode) {
		return b
	}
	at := s.blastBlockAt(b)
	yield := blastYield(b)
	e := &plugin.EntityExplodeEvent{Entity: pluginEntity{host: s.pluginAdapter, id: b.directUUID}, Type: blastEntityType(b), Location: s.blastLocation(b), Pos: plugin.Vec3{X: b.x, Y: b.y, Z: b.z}, Yield: yield}
	e.Blocks = blastBlocksOf(in, at)
	s.firePluginEvent(e)
	return keepBlastBlocks(b, in, e.Cancelled(), e.Blocks, e.Yield, yield)
}

// yieldLootRadius is the loot context radius that makes a destroyed block's
// drop chance the yield: 1/radius under decay.
func yieldLootRadius(yield float32) float64 {
	switch {
	case yield >= 1:
		return 0
	case yield <= 0:
		return math.MaxFloat64
	}
	return 1 / float64(yield)
}

// pluginTNTPrime fires TNTPrimeEvent for a TNT block about to become a primed
// entity and reports a cancel. Most callers have written air over the block by
// now, so a cancel puts the block back rather than losing it; it is the plain
// block, as the caller's state is gone.
// vanilla: TntBlock.prime
func (s *Server) pluginTNTPrime(w *world.World, t world.TNTPrime, igniter uuid.UUID) bool {
	pw := pluginWorld{srv: s, dim: s.dimensionOfWorld(w), host: s.pluginAdapter}
	e := &plugin.TNTPrimeEvent{Block: plugin.Block{World: pw, Pos: plugin.BlockPos{X: int(t.X), Y: int(t.Y), Z: int(t.Z)}}}
	if igniter != uuid.Nil {
		if c := s.clientByUUID(igniter); c != nil {
			e.Entity = c.pluginSelf()
		}
	}
	s.firePluginEvent(e)
	if !e.Cancelled() {
		return false
	}
	if w.BlockAt(t.X, t.Y, t.Z) == airBlockState {
		s.setBlockAndTell(w, t.X, t.Y, t.Z, registry.BlockTnt.DefaultState())
	}
	return true
}

// pluginCauldronChange fires CauldronLevelChangeEvent for a player's use of a
// cauldron and returns the state to write; false when a listener cancelled.
// vanilla: LayeredCauldronBlock.lowerFillLevel, CauldronInteractions
func (c *client) pluginCauldronChange(w *world.World, x, y, z int32, state uint16) (uint16, bool) {
	if !c.pluginListening(pluginruntime.CauldronLevelChange) {
		return state, true
	}
	e := &plugin.CauldronLevelChangeEvent{Block: c.pluginBlockAt(x, y, z), Entity: c.pluginSelf(), NewState: plugin.BlockState(state)}
	c.srv.firePluginEvent(e)
	if e.Cancelled() {
		return state, false
	}
	return uint16(e.NewState), true
}
