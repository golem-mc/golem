package server

import (
	"reflect"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// pluginListening reports whether any plugin listens for hook id; every
// event is built only behind it.
func (c *client) pluginListening(id int) bool {
	return c.srv.plugins != nil && c.srv.plugins.Has(id)
}

// pluginFire runs the event's listeners on the tick goroutine and returns
// once they have finished, with no lock of c held. onTick says the caller
// already is the tick goroutine. It reports false when the server is
// stopping and the listeners did not run.
func (c *client) pluginFire(e plugin.Event, onTick bool) bool {
	if onTick {
		c.srv.plugins.Fire(e)
		return true
	}
	return c.srv.plugins.Call(func() { c.srv.plugins.Fire(e) })
}

func (c *client) pluginBlockAt(x, y, z int32) plugin.Block {
	w := pluginWorld{srv: c.srv, dim: c.dimension(), host: c.srv.pluginAdapter}
	return plugin.Block{World: w, Pos: plugin.BlockPos{X: int(x), Y: int(y), Z: int(z)}}
}

func (c *client) pluginSelf() plugin.Player {
	return pluginPlayer{host: c.srv.pluginAdapter, c: c}
}

// pluginBlockDamage fires BlockDamageEvent for a dig's START; cancelled
// means the dig is refused (the caller resends the block). instant says
// whether the block breaks at once.
// vanilla: ServerPlayerGameMode.handleBlockBreakAction START_DESTROY_BLOCK
func (c *client) pluginBlockDamage(x, y, z int32, instant bool) (cancelled, breakNow bool) {
	if !c.pluginListening(pluginruntime.BlockDamage) {
		return false, instant
	}
	e := &plugin.BlockDamageEvent{Player: c.pluginSelf(), Block: c.pluginBlockAt(x, y, z),
		Item: pluginStack(c.heldItem(protocol.HandMain)), InstaBreak: instant}
	if !c.pluginFire(e, false) {
		return false, instant
	}
	return e.Cancelled(), e.InstaBreak
}

// pluginBlockDamageAbort fires BlockDamageAbortEvent for a dig's abort.
// vanilla: ServerPlayerGameMode.handleBlockBreakAction ABORT_DESTROY_BLOCK
func (c *client) pluginBlockDamageAbort(x, y, z int32) {
	if c.pluginListening(pluginruntime.BlockDamageAbort) {
		c.pluginFire(&plugin.BlockDamageAbortEvent{Player: c.pluginSelf(), Block: c.pluginBlockAt(x, y, z)}, false)
	}
}

// brokenLoot is what BlockBreakEvent's and BlockDropItemEvent's listeners
// decided about a break's drops; nil means vanilla's.
type brokenLoot struct {
	items      bool
	experience int
	// event runs BlockDropItemEvent over the stacks about to spawn, nil
	// without a listener.
	event func([]protocol.ItemStack) []protocol.ItemStack
}

// pluginBlockBreak fires BlockBreakEvent after the break's checks and
// before it changes anything. A cancel leaves the block; the caller
// resends it. Otherwise loot, when not nil, carries the listeners' choice.
// vanilla: ServerPlayerGameMode.destroyBlock
func (c *client) pluginBlockBreak(w *world.World, x, y, z int32, state uint16, held protocol.ItemStack, onTick bool) (cancelled bool, loot *brokenLoot) {
	breaks, drops := c.pluginListening(pluginruntime.BlockBreak), c.pluginListening(pluginruntime.BlockDropItem)
	if (!breaks && !drops) || state == airBlockState {
		return false, nil
	}
	creative := c.player.GameMode() == protocol.GameModeCreative
	block := registry.BlockOf(state)
	harvests := !creative && rules.HasCorrectToolForDrops(held, block)
	xp := 0
	if harvests && w.BoolRule(world.RuleBlockDrops) {
		xp = rules.BlockBreakExperience(block, rules.ToolOf(held), c.srv.levelRng)
	}
	loot = &brokenLoot{items: harvests, experience: xp}
	if breaks {
		e := &plugin.BlockBreakEvent{Player: c.pluginSelf(), Block: c.pluginBlockAt(x, y, z),
			State: plugin.BlockState(state), DropItems: harvests, Experience: xp}
		if !c.pluginFire(e, onTick) {
			return false, nil
		}
		if e.Cancelled() {
			return true, nil
		}
		loot.items = e.DropItems && harvests
		// A break that does not harvest drops no experience either.
		if harvests {
			loot.experience = max(e.Experience, 0)
		} else {
			loot.experience = 0
		}
	}
	if drops {
		loot.event = func(stacks []protocol.ItemStack) []protocol.ItemStack {
			e := &plugin.BlockDropItemEvent{Player: c.pluginSelf(), Block: c.pluginBlockAt(x, y, z), State: plugin.BlockState(state)}
			for _, stack := range stacks {
				e.Items = append(e.Items, pluginStack(stack))
			}
			if !c.pluginFire(e, onTick) || e.Cancelled() {
				return nil
			}
			out := make([]protocol.ItemStack, 0, len(e.Items))
			for _, item := range e.Items {
				if stack, ok := pluginProtocolStack(item); ok && !stack.Empty() {
					out = append(out, stack)
				}
			}
			return out
		}
	}
	return false, loot
}

// pluginBlockPlace fires BlockPlaceEvent for a placement about to be
// written; cancelled means nothing is placed or spent.
// vanilla: BlockItem.place
func (c *client) pluginBlockPlace(hand int32, x, y, z int32, placed uint16, against [3]int32, stack protocol.ItemStack, upper uint16) (cancelled bool) {
	if !c.pluginListening(pluginruntime.BlockPlace) {
		return false
	}
	w := c.theWorld()
	e := &plugin.BlockPlaceEvent{Player: c.pluginSelf(), Block: c.pluginBlockAt(x, y, z),
		Placed: plugin.BlockState(placed), Replaced: plugin.BlockState(w.BlockAt(x, y, z)),
		Against: c.pluginBlockAt(against[0], against[1], against[2]), Hand: plugin.Hand(hand),
		Item: pluginStack(stack), Blocks: placedPositions(placed, upper, x, y, z)}
	return c.pluginFire(e, false) && e.Cancelled()
}

// placedPositions lists every cell a placement of state sets: the block and
// its door or double-plant upper half or bed head.
func placedPositions(state uint16, upper uint16, x, y, z int32) []plugin.BlockPos {
	out := []plugin.BlockPos{{X: int(x), Y: int(y), Z: int(z)}}
	block := registry.BlockOf(state)
	switch {
	case block.InGroup(registry.GroupDoor) || upper != 0:
		out = append(out, plugin.BlockPos{X: int(x), Y: int(y) + 1, Z: int(z)})
	case block.InGroup(registry.GroupBed):
		if d, ok := facingDelta[registry.StateValue(state, registry.PropFacing)]; ok {
			out = append(out, plugin.BlockPos{X: int(x + d[0]), Y: int(y), Z: int(z + d[1])})
		}
	}
	return out
}

// pluginSignChange fires SignChangeEvent with the lines the player typed and
// returns the lines to write.
// vanilla: ServerGamePacketListenerImpl.updateSignText
func (c *client) pluginSignChange(x, y, z int32, front bool, lines [4]string) ([4]string, bool) {
	e := &plugin.SignChangeEvent{Player: c.pluginSelf(), Block: c.pluginBlockAt(x, y, z), Front: front}
	for i, line := range lines {
		e.Lines[i] = plugin.Plain(line)
	}
	if !c.pluginFire(e, false) {
		return lines, false
	}
	for i, line := range e.Lines {
		lines[i] = line.PlainString()
	}
	return lines, e.Cancelled()
}

// pluginOn reports whether any plugin listens for hook id, for code that
// runs on the tick goroutine.
func (s *Server) pluginOn(id int) bool { return s.plugins != nil && s.plugins.Has(id) }

// pluginBlock is the plugin handle of a cell of w.
func (s *Server) pluginBlock(w *world.World, x, y, z int32) plugin.Block {
	pw := pluginWorld{srv: s, dim: s.dimensionOfWorld(w), host: s.pluginAdapter}
	return plugin.Block{World: pw, Pos: plugin.BlockPos{X: int(x), Y: int(y), Z: int(z)}}
}

// pluginDispense fires BlockDispenseEvent on the tick goroutine, without the
// dispenser's lock, and applies the listeners' edits: another item
// dispenses by its own behaviour, and Velocity aims projectile launches (an
// item drop keeps its own motion). It reports whether the dispense was
// cancelled.
// vanilla: DispenserBlock.dispenseFrom
func (s *Server) pluginDispense(w *world.World, visit *dispenseVisit, d *world.Dispense, dropper bool) bool {
	if !s.pluginOn(pluginruntime.BlockDispense) {
		return false
	}
	ax, ay, az := dispenseAim(*d)
	original := pluginStack(visit.taken)
	e := &plugin.BlockDispenseEvent{Block: s.pluginBlock(w, d.X, d.Y, d.Z), Item: original, Velocity: plugin.Vec3{X: ax, Y: ay, Z: az}}
	if !dropper {
		if id, ok := s.dispenseEquipTarget(w, *visit, *d); ok {
			if ent, ok := s.pluginAdapter.entityByUUID(id, ""); ok {
				e.Target, _ = s.pluginAdapter.pluginEntityOf(ent).(plugin.LivingEntity)
			}
		}
	}
	s.plugins.Fire(e)
	if e.Cancelled() {
		return true
	}
	if !reflect.DeepEqual(e.Item, original) {
		if stack, ok := pluginProtocolStack(e.Item); ok && !stack.Empty() {
			stack.Count = 1
			visit.taken = stack
			if !dropper {
				visit.behaviour = inventory.DispenseBehaviourOf(stack)
			}
		}
	}
	if (e.Velocity != plugin.Vec3{X: ax, Y: ay, Z: az}) {
		d.Aim = &[3]float64{e.Velocity.X, e.Velocity.Y, e.Velocity.Z}
	}
	return false
}

// pluginIgnite fires BlockIgniteEvent for a player's flint and steel or fire
// charge and reports whether a listener cancelled it.
// vanilla: FlintAndSteelItem.useOn, FireChargeItem.useOn
func (c *client) pluginIgnite(x, y, z int32, fireCharge bool) bool {
	if !c.pluginListening(pluginruntime.BlockIgnite) {
		return false
	}
	cause := plugin.IgniteFlintAndSteel
	if fireCharge {
		cause = plugin.IgniteFireball
	}
	p := c.pluginSelf()
	e := &plugin.BlockIgniteEvent{Block: c.pluginBlockAt(x, y, z), Cause: cause, Player: p, Entity: p}
	return c.pluginFire(e, false) && e.Cancelled()
}

// pluginIgniteByLightning fires BlockIgniteEvent for a bolt's fire and reports
// whether a listener cancelled it. Runs on the tick goroutine.
// vanilla: LightningBolt.spawnFire
func (s *Server) pluginIgniteByLightning(w *world.World, x, y, z int32) bool {
	if !s.pluginOn(pluginruntime.BlockIgnite) {
		return false
	}
	e := &plugin.BlockIgniteEvent{Block: s.pluginBlock(w, x, y, z), Cause: plugin.IgniteLightning}
	s.firePluginEvent(e)
	return e.Cancelled()
}
