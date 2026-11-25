package server

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"slices"
	"strconv"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// enchantTarget is the body a pack enchantment's entity effect acts on, read
// under its lock before the effect runs.
type enchantTarget struct {
	uuid          uuid.UUID
	player        bool
	x, y, z       float64
	width, height float64
	vx, vy, vz    float64
	// entityID is the body's entity ID and causeID the one an attributed
	// explosion is blamed on (a projectile's owner); zero when unknown.
	entityID, causeID int32
	// name is a player's name, its scoreboard entry; other bodies use the UUID.
	name string
}

// applyPackEffects runs the data-pack-only entity effects of one action at the
// target: block writes, particles, summons and a function. The movement
// particles scale by is zero for a player, whose velocity the server does not
// keep.
// vanilla: ReplaceBlock.apply, ReplaceDisk.apply, SetBlockProperties.apply, SpawnParticlesEffect.apply, SummonEntityEffect.apply, RunFunction.apply
func (s *Server) applyPackEffects(w *world.World, e registry.EnchEntityEffect, level int32, t enchantTarget) {
	if r := e.Replace; r != nil {
		s.replaceBlocks(w, r, level, t)
	}
	if p := e.SetProperties; p != nil {
		s.setBlockProperties(w, p, t)
	}
	if p := e.Particles; p != nil {
		s.spawnEnchantParticles(w, p, t)
	}
	if sm := e.Summon; sm != nil {
		kind := sm.Entities[s.levelRng.Intn(len(sm.Entities))]
		entry := t.uuid.String()
		if t.player {
			entry = t.name
		}
		team, _ := s.scoreboard.PlayerTeam(entry)
		if !sm.JoinTeam || team == "" {
			s.runEnchantLine(w, fmt.Sprintf("summon %s %s %s %s", kind, fnum(t.x), fnum(t.y), fnum(t.z)))
		} else {
			// vanilla: SummonEntityEffect.apply adds the new entity to the body's team
			id := uuid.New()
			var w4 [4]int32
			for i := range w4 {
				w4[i] = int32(binary.BigEndian.Uint32(id[4*i:]))
			}
			s.runEnchantLine(w, fmt.Sprintf("summon %s %s %s %s {UUID:[I;%d,%d,%d,%d]}", kind, fnum(t.x), fnum(t.y), fnum(t.z), w4[0], w4[1], w4[2], w4[3]))
			s.scoreboard.JoinTeam(team, []string{id.String()})
		}
	}
	if x := e.Explode; x != nil {
		s.runEnchantExplode(w, x, level, t)
	}
	if e.RunFunction != "" {
		if fn, ok := s.funcs.Load().lib.Get(e.RunFunction); ok {
			s.runEnchantFunction(w, fn, t)
		}
	}
}

// runEnchantExplode runs an explode effect at the target.
// vanilla: ExplodeEffect.apply, SimpleExplosionDamageCalculator
func (s *Server) runEnchantExplode(w *world.World, x *registry.EnchExplode, level int32, t enchantTarget) {
	b := blast{w: w, x: t.x + x.Offset[0], y: t.y + x.Offset[1], z: t.z + x.Offset[2],
		radius: max(0, float64(rules.EnchLevelValueAt(x.Radius, level))), rng: s.levelRng,
		harmless: x.DamageType == "", fire: x.CreateFire, directID: t.entityID, burst: x.WindBurst() && x.Look == nil, look: x.Look}
	if x.KnockbackMultiplier != nil {
		b.knockbackScale = float64(rules.EnchLevelValueAt(x.KnockbackMultiplier, level))
	}
	if !b.harmless {
		b.damageType, _ = registry.DamageTypeByName(x.DamageType)
	}
	if x.AttributeToUser {
		b.causeID = t.causeID
		if b.causeID == 0 {
			b.causeID = t.entityID
		}
	}
	switch x.BlockInteraction {
	case "none":
		b.interaction = rules.ExplosionKeep
	case "block":
		b.interaction = rules.BlockExplosionInteraction(s.world)
	case "mob":
		b.interaction = rules.MobExplosionInteraction(s.world)
	case "tnt":
		b.interaction = rules.TNTExplosionInteraction(s.world)
	case "trigger":
		b.interaction = rules.ExplosionTrigger
	}
	if x.ImmuneBlocks != "" || x.ImmuneList != nil {
		b.blockResist = immuneResistanceOf(w, x)
	}
	s.runExplosion(b)
}

// immuneResistanceOf is the blast's resistance of the blocks of an immune
// set: 3600000 for a member, none for the rest.
// vanilla: SimpleExplosionDamageCalculator.getBlockExplosionResistance
func immuneResistanceOf(w *world.World, x *registry.EnchExplode) func(bx, by, bz int32) (float64, bool) {
	var tag registry.BlockTagID
	if x.ImmuneBlocks != "" {
		tag = registry.BlockTagOf(x.ImmuneBlocks)
	}
	return func(bx, by, bz int32) (float64, bool) {
		b := registry.BlockOf(w.BlockAt(bx, by, bz))
		if x.ImmuneBlocks != "" && b.InTag(tag) || slices.Contains(x.ImmuneList, b.Name()) {
			return 3600000, true
		}
		return -0.3, true
	}
}

func fnum(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// runEnchantLine runs a command at gamemaster level with its output suppressed.
// vanilla: RunFunction.apply (withPermission(GAMEMASTER).withSuppressedOutput)
func (s *Server) runEnchantLine(w *world.World, line string) {
	src := s.gameLoopSource()
	src.in = func() *dimension { return s.dimensionOfWorld(w) }
	quiet, ok := src.InContext(command.Context{Silent: true})
	if !ok {
		return
	}
	_, _ = runSourceCommand(quiet, line, command.Tokenize(line), func(protocol.Text) {})
}

// runEnchantFunction runs a function as the target, at its position and
// rotation but in w, the level of the hit, which may not be the target's.
// vanilla: RunFunction.apply, ServerFunctionManager.execute
func (s *Server) runEnchantFunction(w *world.World, fn *command.Function, t enchantTarget) {
	// Through the interface: a static call to Entities closes an initialization cycle.
	var src command.Source = s.gameLoopSource()
	id := t.uuid
	found := src.Entities(command.EntityQuery{UUID: &id})
	if len(found) == 0 {
		return
	}
	at, ok := src.InContext(command.Context{Silent: true, Dim: s.dimensionOfWorld(w).name, HasDim: true,
		Executor: found[0], Pos: found[0].Position(), HasPos: true})
	if !ok {
		return
	}
	command.RunFunction(at, fn)
}

// blockPredicateTest runs a block predicate at a cell; a nil one passes.
// vanilla: BlockPredicate.test
func (s *Server) blockPredicateTest(p *registry.EnchBlockPredicate, w *world.World, x, y, z int32) bool {
	return s.blockPredicateTestIn(p, w, x, y, z, s.fullBlockObstructed(w))
}

// fullBlockObstructed answers the unobstructed term from every body in the world.
func (s *Server) fullBlockObstructed(w *world.World) func(x, y, z int32) bool {
	return func(x, y, z int32) bool { return s.obstructed(w, x, y, z, registry.BlockStone.DefaultState()) }
}

// blockPredicateTestIn is blockPredicateTest with the unobstructed term's
// whole-block body test supplied, so a batch can answer it from one gather of
// bodies.
func (s *Server) blockPredicateTestIn(p *registry.EnchBlockPredicate, w *world.World, x, y, z int32, obstructed func(x, y, z int32) bool) bool {
	if p == nil {
		return true
	}
	r := cellReads{w: w, obstructed: obstructed}
	return compiledCellTest(p)(&r, x, y, z)
}

// cellTests caches the compiled form of each block predicate.
var cellTests sync.Map // *registry.EnchBlockPredicate -> cellTest

// compiledCellTest is the compiled form of a non-nil predicate, built once.
func compiledCellTest(p *registry.EnchBlockPredicate) cellTest {
	if t, ok := cellTests.Load(p); ok {
		return t.(cellTest)
	}
	t, _ := cellTests.LoadOrStore(p, compileCellTest(p))
	return t.(cellTest)
}

// cellTest runs a compiled block predicate at a cell.
type cellTest func(r *cellReads, x, y, z int32) bool

// cellReads is the world and the one block read a test keeps, so the terms of
// an all_of that read the same cell cost one BlockAt. It is valid only until
// the world is written.
type cellReads struct {
	w          *world.World
	obstructed func(x, y, z int32) bool
	pos        [3]int32
	state      uint16
	have       bool
}

func (r *cellReads) reset() *cellReads {
	r.have = false
	return r
}

func (r *cellReads) block(x, y, z int32) uint16 {
	if r.have && r.pos == [3]int32{x, y, z} {
		return r.state
	}
	r.pos, r.state, r.have = [3]int32{x, y, z}, r.w.BlockAt(x, y, z), true
	return r.state
}

// compileCellTest builds the test of a predicate; a nil one passes and an
// unknown kind passes. Only the cases that read the block fetch it.
// vanilla: BlockPredicate.test
func compileCellTest(p *registry.EnchBlockPredicate) cellTest {
	if p == nil {
		return func(*cellReads, int32, int32, int32) bool { return true }
	}
	off := p.Offset
	switch p.Kind {
	case "matching_blocks":
		return func(r *cellReads, x, y, z int32) bool {
			return slices.Contains(p.Blocks, registry.BlockOf(r.block(x+off[0], y+off[1], z+off[2])))
		}
	case "matching_block_tag":
		return func(r *cellReads, x, y, z int32) bool {
			return registry.BlockOf(r.block(x+off[0], y+off[1], z+off[2])).InTag(p.Tag)
		}
	case "matching_fluids":
		return func(r *cellReads, x, y, z int32) bool {
			return slices.Contains(p.Names, world.FluidName(r.block(x+off[0], y+off[1], z+off[2])))
		}
	case "matching_biomes":
		return func(r *cellReads, x, y, z int32) bool {
			return slices.Contains(p.Names, r.w.BiomeAt(x+off[0], y+off[1], z+off[2]))
		}
	case "solid":
		return func(r *cellReads, x, y, z int32) bool {
			return registry.IsSolid(r.block(x+off[0], y+off[1], z+off[2]))
		}
	case "replaceable":
		return func(r *cellReads, x, y, z int32) bool {
			return registry.BlockOf(r.block(x+off[0], y+off[1], z+off[2])).IsReplaceable()
		}
	case "has_sturdy_face":
		face := world.DirIndex(p.Face)
		return func(r *cellReads, x, y, z int32) bool {
			return registry.FaceSturdy(r.block(x+off[0], y+off[1], z+off[2]), face, registry.SupportFull)
		}
	case "would_survive":
		return func(r *cellReads, x, y, z int32) bool {
			return world.PlacementSurvives(r.w, p.State, x+off[0], y+off[1], z+off[2])
		}
	case "inside_world_bounds":
		return func(r *cellReads, x, y, z int32) bool {
			py := y + off[1]
			return py >= r.w.MinY() && py < r.w.MinY()+r.w.Height()
		}
	case "height_range":
		// The un-offset y, as vanilla's HeightRange tests the position.
		return func(r *cellReads, x, y, z int32) bool {
			return y >= anchorY(p.Bottom, r.w) && y <= anchorY(p.Top, r.w)
		}
	case "unobstructed":
		return func(r *cellReads, x, y, z int32) bool {
			return !r.obstructed(x+off[0], y+off[1], z+off[2])
		}
	case "volume_match":
		term := compileCellTest(p.Terms[0])
		return func(r *cellReads, x, y, z int32) bool {
			px, py, pz := x+off[0], y+off[1], z+off[2]
			for dx := p.Min[0]; dx <= p.Max[0]; dx++ {
				for dz := p.Min[2]; dz <= p.Max[2]; dz++ {
					for dy := p.Min[1]; dy <= p.Max[1]; dy++ {
						if !term(r, px+dx, py+dy, pz+dz) {
							return false
						}
					}
				}
			}
			return true
		}
	case "not":
		term := compileCellTest(p.Terms[0])
		return func(r *cellReads, x, y, z int32) bool { return !term(r, x, y, z) }
	case "all_of":
		terms := compileTerms(p.Terms)
		return func(r *cellReads, x, y, z int32) bool {
			for _, term := range terms {
				if !term(r, x, y, z) {
					return false
				}
			}
			return true
		}
	case "any_of":
		terms := compileTerms(p.Terms)
		return func(r *cellReads, x, y, z int32) bool {
			for _, term := range terms {
				if term(r, x, y, z) {
					return true
				}
			}
			return false
		}
	}
	return func(*cellReads, int32, int32, int32) bool { return true }
}

func compileTerms(ps []*registry.EnchBlockPredicate) []cellTest {
	out := make([]cellTest, len(ps))
	for i, p := range ps {
		out[i] = compileCellTest(p)
	}
	return out
}

// anchorY resolves a vertical anchor in the world's dimension.
// vanilla: VerticalAnchor.resolveY
func anchorY(a registry.EnchAnchor, w *world.World) int32 {
	switch a.Kind {
	case "above_bottom":
		return w.MinY() + a.Value
	case "below_top":
		return w.MinY() + w.Height() - 1 - a.Value
	}
	return a.Value
}

// writeEnchantBlock sets a block and, when it changed, fires the effect's game event.
func (s *Server) writeEnchantBlock(w *world.World, x, y, z int32, state uint16, event registry.GameEvent, t enchantTarget) {
	if w.BlockAt(x, y, z) == state {
		return
	}
	s.setBlockAndTell(w, x, y, z, state)
	if event != registry.GameEventNone {
		ctx := rules.GameEventContext{Affected: state}
		if t.player {
			ctx.Player = t.uuid
		} else {
			ctx.Mob = t.uuid
		}
		s.gameEvent(w, event, float64(x)+.5, float64(y)+.5, float64(z)+.5, ctx)
	}
}

// replaceProviders caches the compiled state provider of each replace effect.
var replaceProviders sync.Map // *registry.EnchReplace -> world.StateProviderPicker

// replaceState is the state a replace effect writes at a cell.
func (s *Server) replaceState(r *registry.EnchReplace, x, y, z int32) uint16 {
	if r.Provider == nil {
		return r.State
	}
	pick, ok := replaceProviders.Load(r)
	if !ok {
		compiled, err := world.NewStateProviderPicker(r.Provider, s.levelRng.Int63())
		if err != nil {
			slog.Warn("A replace effect's state provider is not supported", "error", err)
			return r.State
		}
		pick, _ = replaceProviders.LoadOrStore(r, compiled)
	}
	return pick.(world.StateProviderPicker)(x, y, z)
}

// replaceBlocks runs replace_block, or replace_disk when it has a radius.
// vanilla: ReplaceBlock.apply, ReplaceDisk.apply
func (s *Server) replaceBlocks(w *world.World, r *registry.EnchReplace, level int32, t enchantTarget) {
	cx, cy, cz := rules.BlockOf(t.x)+r.Offset[0], rules.BlockOf(t.y)+r.Offset[1], rules.BlockOf(t.z)+r.Offset[2]
	if r.Radius == nil {
		if s.blockPredicateTest(r.Predicate, w, cx, cy, cz) {
			s.writeEnchantBlock(w, cx, cy, cz, s.replaceState(r, cx, cy, cz), r.GameEvent, t)
		}
		return
	}
	for _, c := range s.diskCells(w, r, level, t, s.fullBlockObstructed(w)) {
		s.writeEnchantBlock(w, c[0], c[1], c[2], s.replaceState(r, c[0], c[1], c[2]), r.GameEvent, t)
	}
}

// diskCells is the cells a replace_disk writes: those whose centre is
// strictly within the radius of the target's x and z and whose predicate
// holds, with obstructed answering the unobstructed term.
// vanilla: ReplaceDisk.apply
func (s *Server) diskCells(w *world.World, r *registry.EnchReplace, level int32, t enchantTarget, obstructed func(x, y, z int32) bool) [][3]int32 {
	cx, cy, cz := rules.BlockOf(t.x)+r.Offset[0], rules.BlockOf(t.y)+r.Offset[1], rules.BlockOf(t.z)+r.Offset[2]
	dist := int32(rules.EnchLevelValueAt(r.Radius, level))
	height := int32(rules.EnchLevelValueAt(r.Height, level))
	top := min(height-1, 0)
	var cells [][3]int32
	test := func(*cellReads, int32, int32, int32) bool { return true }
	if r.Predicate != nil {
		test = compiledCellTest(r.Predicate)
	}
	// One reads value for the walk (an indirect call makes it escape), its
	// cached read dropped per cell.
	reads := &cellReads{w: w, obstructed: obstructed}
	for x := cx - dist; x <= cx+dist; x++ {
		for y := cy + top; y <= cy; y++ {
			for z := cz - dist; z <= cz+dist; z++ {
				dx, dz := float64(x)+.5-t.x, float64(z)+.5-t.z
				// vanilla: BlockPos.distToCenterSqr, the y term being zero
				if dx*dx+dz*dz < float64(dist)*float64(dist) && test(reads.reset(), x, y, z) {
					cells = append(cells, [3]int32{x, y, z})
				}
			}
		}
	}
	return cells
}

// setBlockProperties sets the properties the block has, leaving the others.
// vanilla: SetBlockProperties.apply, BlockItemStateProperties.apply
func (s *Server) setBlockProperties(w *world.World, p *registry.EnchSetProperties, t enchantTarget) {
	x, y, z := rules.BlockOf(t.x)+p.Offset[0], rules.BlockOf(t.y)+p.Offset[1], rules.BlockOf(t.z)+p.Offset[2]
	state := w.BlockAt(x, y, z)
	for _, pv := range p.Props {
		state, _ = registry.WithProp(state, pv.P, pv.V)
	}
	s.writeEnchantBlock(w, x, y, z, state, p.GameEvent, t)
}

func sampleRange(f registry.EnchFloatRange, rng *rand.Rand) float64 {
	if f.Max <= f.Min {
		return float64(f.Min)
	}
	return float64(f.Min) + rng.Float64()*float64(f.Max-f.Min)
}

// spawnEnchantParticles sends one particle whose count is zero, so the
// velocity fields carry its motion.
// vanilla: SpawnParticlesEffect.apply
func (s *Server) spawnEnchantParticles(w *world.World, p *registry.EnchParticles, t enchantTarget) {
	rng := s.levelRng
	coord := func(a registry.EnchParticleAxis, pos, center, span float64) float64 {
		if a.InBox {
			pos = center + (rng.Float64()-.5)*span*float64(a.Scale)
		}
		return pos + float64(a.Offset)
	}
	x := coord(p.Horizontal, t.x, t.x, t.width)
	y := coord(p.Vertical, t.y, t.y+t.height/2, t.height)
	z := coord(p.Horizontal, t.z, t.z, t.width)
	vx := t.vx*float64(p.Horizontal.Movement) + sampleRange(p.Horizontal.Base, rng)
	vy := t.vy*float64(p.Vertical.Movement) + sampleRange(p.Vertical.Base, rng)
	vz := t.vz*float64(p.Horizontal.Movement) + sampleRange(p.Horizontal.Base, rng)
	speed := sampleRange(p.Speed, rng)
	if math.IsNaN(speed) {
		speed = 0
	}
	s.sendParticlePayload(w, particleDataPayload(p.Particle, p.Options, false, x, y, z, float32(vx), float32(vy), float32(vz), float32(speed), 0), x, y, z)
}
