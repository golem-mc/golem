package world

import (
	"math"
	"slices"
	"sort"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The sculk spreader: charge cursors that walk sculk and veins, turning
// replaceable blocks to sculk and growing sensors and shriekers. A death in a
// catalyst's reach gives it cursors; world generation runs the same spreader
// with its own tuning, random and blocks (sculk_worldgen.go).
const (
	sculkMaxCursors    = 32
	sculkMaxCharge     = 1000
	sculkShriekerRate  = 11
	sculkMaxReach      = 24
	sculkMaxCursorDist = 1024
	catalystRadiusSq   = 64
	catalystPulseTicks = 8
	// sculkChargeEvent is LevelEvent.PARTICLES_SCULK_CHARGE.
	sculkChargeEvent = 3006
	// vanilla: SculkSpreader.ChargeCursor.MAX_WORLDGEN_SPREAD
	sculkGenReach = 12

	sculkCatalyst = "minecraft:sculk_catalyst"
)

// sculkTuning is what differs between SculkSpreader's two factories.
// vanilla: SculkSpreader.createLevelSpreader, createWorldGenSpreader
type sculkTuning struct {
	growthCost, noGrowthRadius, decayRate, additionalDecay int
	// replaceable is the tag of blocks a vein turns to sculk.
	replaceable registry.BlockTagID
	// worldGen limits the walk to sculkGenReach, grows shriekers that can
	// summon and plays no sounds.
	worldGen bool
}

var levelSculkTuning = sculkTuning{
	growthCost: 10, noGrowthRadius: 4, decayRate: 10, additionalDecay: 5,
	replaceable: registry.TagIDSculkReplaceable,
}

// chargeCursor is one cursor of a catalyst's spreader. facings is the vein
// faces of the cell it last stood on; hasFacings false is vanilla's null.
// vanilla: SculkSpreader.ChargeCursor
type chargeCursor struct {
	x, y, z                 int32
	charge                  int
	decayDelay, updateDelay int
	facings                 uint8
	hasFacings              bool
}

// spreadType is MultifaceSpreader.SpreadType.
type spreadType int

const (
	spreadSamePosition spreadType = iota
	spreadSamePlane
	spreadWrapAround
)

var (
	veinSpreadOrder = []spreadType{spreadSamePosition, spreadSamePlane, spreadWrapAround}
	veinSameSpace   = []spreadType{spreadSamePosition}
)

// SculkCatalystDeath hands a death to the nearest catalyst within eight blocks.
// reward, asked only when a catalyst takes it, is the body's experience. taken
// means one did, so the caller drops no orbs; spread means the reward was
// positive, when vanilla awards kill_mob_near_sculk_catalyst.
// vanilla: SculkCatalystBlockEntity.CatalystListener.handleGameEvent
func (p *Physics) SculkCatalystDeath(x, y, z float64, reward func() int) (taken, spread bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	bx, by, bz := floorCell(x), floorCell(y), floorCell(z)
	best, bestDist := blockKey{}, math.MaxInt32
	// Ties go to the first in x, y, z order, as the cell-by-cell search did.
	for _, k := range p.w.sculkCatalystsNear(bx, by, bz) {
		dx, dy, dz := k.x-bx, k.y-by, k.z-bz
		d := int(dx*dx + dy*dy + dz*dz)
		if d > catalystRadiusSq || d > bestDist || (d == bestDist && !keyBefore(k, best)) {
			continue
		}
		best, bestDist = k, d
	}
	if bestDist == math.MaxInt32 {
		return false, false
	}
	xp := reward()
	spread = xp > 0
	if xp > 0 {
		sx, sy, sz := floorCell(x), floorCell(y+0.5), floorCell(z)
		for xp > 0 {
			charge := min(xp, sculkMaxCharge)
			if len(p.catalysts[best]) < sculkMaxCursors {
				p.catalysts[best] = append(p.catalysts[best], chargeCursor{x: sx, y: sy, z: sz, charge: charge, decayDelay: 1})
			}
			xp -= charge
		}
		p.saveCursors(best)
	}
	p.blooms = append(p.blooms, best)
	return true, spread
}

// sculkSpreaderTicks blooms the catalysts that took a death and moves every
// catalyst's cursors one step.
// vanilla: SculkCatalystBlockEntity.serverTick, SculkCatalystBlock.tick
func (p *Physics) sculkSpreaderTicks(changes *[]BlockChange) {
	for _, key := range p.blooms {
		p.catalystBloom(key, changes)
	}
	p.blooms = p.blooms[:0]
	p.adoptCatalystCursors()
	if len(p.catalysts) == 0 {
		return
	}
	keys := make([]blockKey, 0, len(p.catalysts))
	for k := range p.catalysts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keyBefore(keys[i], keys[j]) })
	for _, k := range keys {
		if registry.BlockOf(p.w.BlockAt(k.x, k.y, k.z)) != registry.BlockSculkCatalyst {
			delete(p.catalysts, k)
			continue
		}
		// vanilla: SculkSpreader.ChargeCursor.shouldUpdate reads the
		// catalyst's chunk; loaded stands for ticking here.
		if !p.w.ChunkLoaded(ChunkPos{X: k.x >> 4, Z: k.z >> 4}) || !p.chunkTicksNow(k.x, k.z) {
			continue
		}
		p.updateCursors(k, changes)
		p.saveCursors(k)
		if len(p.catalysts[k]) == 0 {
			delete(p.catalysts, k)
		}
	}
}

// adoptCatalystCursors takes up the cursors of catalysts a chunk load brought
// in with them.
// vanilla: SculkCatalystBlockEntity.loadAdditional
func (p *Physics) adoptCatalystCursors() {
	for _, be := range p.w.SculkCatalystEntities() {
		if !be.hasCursors.Load() {
			continue
		}
		var key blockKey
		var saved []chargeCursor
		be.Use(func(b *BlockEntity) {
			key, saved = blockKey{b.X, b.Y, b.Z}, slices.Clone(b.cursors)
		})
		if _, ok := p.catalysts[key]; !ok && len(saved) > 0 {
			p.catalysts[key] = saved
		}
	}
}

// saveCursors writes the catalyst's cursors to its block entity, which the
// chunk save reads.
// vanilla: SculkSpreader.save
func (p *Physics) saveCursors(k blockKey) {
	cursors := p.catalysts[k]
	if p.w.BlockEntity(k.x, k.y, k.z) == nil && len(cursors) == 0 {
		return
	}
	be := p.w.EnsureBlockEntity(k.x, k.y, k.z, KindSculkCatalyst)
	if be == nil {
		return
	}
	be.Use(func(b *BlockEntity) {
		b.cursors = slices.Clone(cursors)
		b.hasCursors.Store(len(cursors) > 0)
	})
	p.w.MarkDirty(ChunkPos{X: k.x >> 4, Z: k.z >> 4})
}

// cursorsToNBT is ChargeCursor.CODEC over the list; facings is left out for
// a cursor that has none.
func cursorsToNBT(cursors []chargeCursor) nbt.List {
	list := nbt.List{Elem: nbt.TypeCompound}
	for _, c := range cursors {
		e := nbt.NewOrderedCompound(5)
		e.Set("pos", nbt.IntArray{c.x, c.y, c.z})
		e.Set("charge", nbt.Int(c.charge))
		e.Set("decay_delay", nbt.Int(c.decayDelay))
		e.Set("update_delay", nbt.Int(c.updateDelay))
		if c.hasFacings {
			faces := nbt.List{Elem: nbt.TypeString}
			for d, name := range faceProps {
				if c.facings&(1<<d) != 0 {
					faces.Items = append(faces.Items, nbt.String(name))
				}
			}
			if len(faces.Items) == 0 {
				faces.Elem = nbt.TypeEnd
			}
			e.Set("facings", faces)
		}
		list.Items = append(list.Items, e)
	}
	if len(list.Items) == 0 {
		list.Elem = nbt.TypeEnd
	}
	return list
}

// cursorsFromNBT reads at most 32 cursors; one with a bad position or a
// charge outside 0..1000 fails the whole list, as the codec does.
// vanilla: SculkSpreader.load
func cursorsFromNBT(entry nbt.Compound) []chargeCursor {
	list, ok := nbt.Get[nbt.List](entry, "cursors")
	if !ok || len(list.Items) > sculkMaxCursors {
		return nil
	}
	var out []chargeCursor
	for _, item := range list.Items {
		e, ok := nbt.CompoundView(item)
		if !ok {
			return nil
		}
		pos, ok := nbt.Get[nbt.IntArray](e, "pos")
		if !ok || len(pos) != 3 {
			return nil
		}
		c := chargeCursor{x: pos[0], y: pos[1], z: pos[2], decayDelay: 1}
		for _, f := range []struct {
			name     string
			dst      *int
			min, max int32
		}{
			{"charge", &c.charge, 0, sculkMaxCharge},
			{"decay_delay", &c.decayDelay, 0, 1},
			{"update_delay", &c.updateDelay, 0, math.MaxInt32},
		} {
			tag, present := e[f.name]
			if !present {
				continue
			}
			v, ok := nbt.IntValue(tag)
			if !ok || v < f.min || v > f.max {
				return nil
			}
			*f.dst = int(v)
		}
		if faces, ok := nbt.Get[nbt.List](e, "facings"); ok {
			c.hasFacings = true
			for _, f := range faces.Items {
				name, _ := f.(nbt.String)
				if d := slices.Index(faceProps[:], string(name)); d >= 0 {
					c.facings |= 1 << d
				}
			}
		}
		out = append(out, c)
	}
	return out
}

func keyBefore(a, b blockKey) bool {
	if a.x != b.x {
		return a.x < b.x
	}
	if a.y != b.y {
		return a.y < b.y
	}
	return a.z < b.z
}

// sculkCatalystsNear lists the catalysts in sections intersecting the death's
// eight-block search radius.
func (w *World) sculkCatalystsNear(bx, by, bz int32) []blockKey {
	return w.sculkCellsNear(bx, by, bz, 8, sculkCatalystCell, nil)
}

// catalystBloom lights the catalyst's pulse for eight ticks.
// vanilla: CatalystListener.bloom
func (p *Physics) catalystBloom(key blockKey, changes *[]BlockChange) {
	state := p.w.BlockAt(key.x, key.y, key.z)
	if registry.BlockOf(state) != registry.BlockSculkCatalyst {
		return
	}
	next, _ := registry.WithBool(state, registry.PropBloom, true)
	p.setClient(key.x, key.y, key.z, next, changes)
	p.wireWakeNeighbors(key.x, key.y, key.z, changes)
	p.shapeUpdateNeighbors(key.x, key.y, key.z, changes)
	p.scheduleBooked(p.sculkBooked, key.x, key.y, key.z, p.now+catalystPulseTicks, sculkTickEvent)
	p.effects = append(p.effects, Effect{
		SoundID: registry.SoundBlockSculkCatalystBloom, X: key.x, Y: key.y, Z: key.z,
		Volume: 2, Pitch: 0.6 + p.rng.Float32()*0.4,
		Particles: &ParticleBurst{
			ID: protocol.ParticleSculkSoul, X: float64(key.x) + 0.5, Y: float64(key.y) + 1.15, Z: float64(key.z) + 0.5,
			SpreadX: 0.2, SpreadZ: 0.2, Count: 2,
		},
	})
}

// catalystEndPulse is the booked tick that puts the pulse out.
// vanilla: SculkCatalystBlock.tick
func (p *Physics) catalystEndPulse(x, y, z int32, state uint16, changes *[]BlockChange) {
	if !registry.StateBool(state, registry.PropBloom) {
		return
	}
	next, _ := registry.WithBool(state, registry.PropBloom, false)
	p.setClient(x, y, z, next, changes)
	p.wireWakeNeighbors(x, y, z, changes)
	p.shapeUpdateNeighbors(x, y, z, changes)
}

// updateCursors steps a catalyst's cursors and merges the ones that now
// share a cell.
// vanilla: SculkSpreader.updateCursors
func (p *Physics) updateCursors(origin blockKey, changes *[]BlockChange) {
	cursors := p.catalysts[origin]
	sp := p.levelSpreader(origin, changes)
	processed := make([]chargeCursor, 0, len(cursors))
	merge := map[blockKey]int{}
	var order []blockKey
	charges := map[blockKey]int{}
	for i := range cursors {
		c := cursors[i]
		if chessboard(c.x-origin.x, c.y-origin.y, c.z-origin.z) > sculkMaxCursorDist {
			continue
		}
		sp.update(&c, true)
		if c.charge <= 0 {
			// vanilla: LevelEvent.PARTICLES_SCULK_CHARGE with data 0 pops it
			p.effects = append(p.effects, Effect{LevelEvent: sculkChargeEvent, X: c.x, Y: c.y, Z: c.z})
			continue
		}
		pos := blockKey{c.x, c.y, c.z}
		if _, seen := charges[pos]; !seen {
			order = append(order, pos)
		}
		charges[pos] += c.charge
		at, ok := merge[pos]
		if !ok {
			merge[pos] = len(processed)
			processed = append(processed, c)
			continue
		}
		if existing := &processed[at]; c.charge+existing.charge <= sculkMaxCharge {
			existing.charge += c.charge
			existing.updateDelay = min(existing.updateDelay, c.updateDelay)
			continue
		}
		processed = append(processed, c)
		if c.charge < processed[at].charge {
			merge[pos] = len(processed) - 1
		}
	}
	// Particles go out per cell with the faces of the cursor that ended there,
	// the charge summing every cursor that stood there.
	for _, pos := range order {
		held := processed[merge[pos]]
		if !held.hasFacings || charges[pos] <= 0 {
			continue
		}
		n := int32(math.Log1p(float64(charges[pos]))/float64(float32(2.3))) + 1
		p.effects = append(p.effects, Effect{
			LevelEvent: sculkChargeEvent, LevelEventData: n<<6 + int32(held.facings),
			X: pos.x, Y: pos.y, Z: pos.z,
		})
	}
	p.catalysts[origin] = processed
}

func chessboard(dx, dy, dz int32) int32 {
	return max(abs32(dx), abs32(dy), abs32(dz))
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// sculkWorld is the blocks and effects a spreader works on.
type sculkWorld interface {
	get(x, y, z int32) uint16
	// set writes a block, false when it did not change; wake also notifies
	// the neighbours, a flag-3 write.
	set(x, y, z int32, state uint16, wake bool) bool
	sound(x, y, z int32, sound registry.Sound)
	// placedSculk follows a sculk write over old.
	// vanilla: Block.pushEntitiesUp
	placedSculk(x, y, z int32, old uint16)
}

// sculkRandom is the random source of a spreader.
type sculkRandom interface {
	NextIntN(n int32) int32
	// Perm is a shuffled 0..n-1.
	Perm(n int) []int
}

// sculkSpreader is SculkSpreader for one origin.
type sculkSpreader struct {
	w      sculkWorld
	rng    sculkRandom
	t      *sculkTuning
	origin blockKey
}

type sculkBlockKind int

const (
	sculkOther sculkBlockKind = iota
	sculkIsSculk
	sculkIsVein
)

func sculkKindOf(state uint16) sculkBlockKind {
	switch registry.BlockOf(state) {
	case registry.BlockSculk:
		return sculkIsSculk
	case registry.BlockSculkVein:
		return sculkIsVein
	}
	return sculkOther
}

var propCanSummon, _ = registry.PropNamed("can_summon")

// nonCornerNeighbours are the 18 offsets ChargeCursor moves by, in the order
// BlockPos.betweenClosed walks them: x fastest, z slowest.
var nonCornerNeighbours = func() (out [][3]int32) {
	for dz := int32(-1); dz <= 1; dz++ {
		for dy := int32(-1); dy <= 1; dy++ {
			for dx := int32(-1); dx <= 1; dx++ {
				if (dx == 0 || dy == 0 || dz == 0) && (dx != 0 || dy != 0 || dz != 0) {
					out = append(out, [3]int32{dx, dy, dz})
				}
			}
		}
	}
	return out
}()

// levelSculk is the sculkWorld of a catalyst's spreader, writing through
// Physics.
type levelSculk struct {
	p       *Physics
	changes *[]BlockChange
	// mark is len(*changes) before the last write.
	mark int
}

func (l *levelSculk) get(x, y, z int32) uint16 { return l.p.w.BlockAt(x, y, z) }

func (l *levelSculk) set(x, y, z int32, state uint16, wake bool) bool {
	l.mark = len(*l.changes)
	if !l.p.setClientShaped(x, y, z, state, l.changes) {
		return false
	}
	if wake {
		l.p.wireWakeNeighbors(x, y, z, l.changes)
	}
	return true
}

func (l *levelSculk) sound(x, y, z int32, sound registry.Sound) {
	l.p.effects = append(l.p.effects, Effect{SoundID: sound, X: x, Y: y, Z: z, Volume: 1, Pitch: 1})
}

func (l *levelSculk) placedSculk(x, y, z int32, old uint16) {
	if len(*l.changes) > l.mark {
		(*l.changes)[l.mark].PushUp = CollisionTop(l.p.w.BlockAt(x, y, z)) - CollisionTop(old)
	}
}

// levelRand is the sculkRandom over Physics' dice.
type levelRand struct{ p *Physics }

func (r levelRand) NextIntN(n int32) int32 { return int32(r.p.rng.Intn(int(n))) }
func (r levelRand) Perm(n int) []int       { return r.p.rng.Perm(n) }

func (p *Physics) levelSpreader(origin blockKey, changes *[]BlockChange) *sculkSpreader {
	return &sculkSpreader{w: &levelSculk{p: p, changes: changes}, rng: levelRand{p}, t: &levelSculkTuning, origin: origin}
}

// update is one cursor's step: spread veins from its cell, spend charge by
// the block under it, then move to a neighbouring sculk cell.
// vanilla: SculkSpreader.ChargeCursor.update
func (s *sculkSpreader) update(c *chargeCursor, spreadVeins bool) {
	if c.charge <= 0 {
		return
	}
	if c.updateDelay > 0 {
		c.updateDelay--
		return
	}
	state := s.w.get(c.x, c.y, c.z)
	kind := sculkKindOf(state)
	if spreadVeins && s.attemptSpreadVein(c, kind, state) {
		if kind != sculkIsSculk {
			state = s.w.get(c.x, c.y, c.z)
			kind = sculkKindOf(state)
		}
		s.w.sound(c.x, c.y, c.z, registry.SoundBlockSculkSpread)
	}
	c.charge = s.attemptUseCharge(c, kind, spreadVeins)
	if c.charge <= 0 {
		s.onDischarged(kind, state, c.x, c.y, c.z)
		return
	}
	if nx, ny, nz, ok := s.validMovementPos(c.x, c.y, c.z); ok {
		s.onDischarged(kind, state, c.x, c.y, c.z)
		c.x, c.y, c.z = nx, ny, nz
		state = s.w.get(nx, ny, nz)
	} else if s.t.worldGen {
		s.onDischarged(kind, state, c.x, c.y, c.z)
		c.charge = 0
		return
	}
	if sculkKindOf(state) != sculkOther {
		c.facings, c.hasFacings = stateFaceMask(state), true
	}
	if kind == sculkOther {
		c.decayDelay = max(c.decayDelay-1, 0)
	} else {
		c.decayDelay = 1
	}
	c.updateDelay = 1
}

// stateFaceMask packs a multiface state's face flags by direction.
func stateFaceMask(state uint16) uint8 {
	var m uint8
	for d, p := range facePropIDs {
		if registry.StateBool(state, p) {
			m |= 1 << d
		}
	}
	return m
}

// veinState is a sculk vein on the faces of the mask.
func veinState(faces uint8, waterlogged bool) uint16 {
	state := registry.BlockSculkVein.DefaultState()
	for d, p := range facePropIDs {
		if faces&(1<<d) != 0 {
			state, _ = registry.WithBool(state, p, true)
		}
	}
	if waterlogged {
		state, _ = registry.WithBool(state, registry.PropWaterlogged, true)
	}
	return state
}

// attemptSpreadVein is SculkBehaviour.attemptSpreadVein: sculk and veins
// spread veins all round, any other block regrows the veins it last stood on.
func (s *sculkSpreader) attemptSpreadVein(c *chargeCursor, kind sculkBlockKind, state uint16) bool {
	if kind == sculkOther {
		if !c.hasFacings {
			return s.spreadAll(veinSameSpace, state, c.x, c.y, c.z) > 0
		}
		if c.facings != 0 {
			if !IsAirState(state) && !isWaterSource(state) {
				return false
			}
			return s.veinRegrow(c.x, c.y, c.z, c.facings, state)
		}
	}
	return s.spreadAll(veinSpreadOrder, state, c.x, c.y, c.z) > 0
}

// veinRegrow places a vein on every face of the cell that can hold one.
// vanilla: SculkVeinBlock.regrow
func (s *sculkSpreader) veinRegrow(x, y, z int32, faces uint8, existing uint16) bool {
	var attach uint8
	for d := range int32(dirCount) {
		if faces&(1<<d) != 0 && s.canAttach(x, y, z, d) {
			attach |= 1 << d
		}
	}
	if attach == 0 {
		return false
	}
	return s.w.set(x, y, z, veinState(attach, fluidHolding(existing).kind != fluidNone), true)
}

// canAttach is MultifaceBlock.canAttachTo: the neighbour toward d has a full
// face against the vein.
func (s *sculkSpreader) canAttach(x, y, z, d int32) bool {
	o := protocol.FaceOffset[d]
	return registry.FaceSturdy(s.w.get(x+o[0], y+o[1], z+o[2]), blocklogic.Opposite(int(d)), registry.SupportFull)
}

// spreadAll is MultifaceSpreader.spreadAll over the vein spreader config:
// from every face the source may spread from, toward every direction.
func (s *sculkSpreader) spreadAll(types []spreadType, state uint16, x, y, z int32) int {
	isVein := registry.BlockOf(state) == registry.BlockSculkVein
	n := 0
	for from := range int32(dirCount) {
		// vanilla: SculkVeinSpreaderConfig.isOtherBlockValidAsSource is
		// true for everything but a vein.
		if isVein && !registry.StateBool(state, facePropIDs[from]) {
			continue
		}
		for dir := range int32(dirCount) {
			if from/2 == dir/2 || isVein && registry.StateBool(state, facePropIDs[dir]) {
				continue
			}
			for _, t := range types {
				px, py, pz, face := spreadPos(t, x, y, z, dir, from)
				if !s.canSpreadInto(x, y, z, px, py, pz, face) {
					continue
				}
				if s.placeVein(px, py, pz, face) {
					n++
				}
				break
			}
		}
	}
	return n
}

func spreadPos(t spreadType, x, y, z, dir, from int32) (px, py, pz, face int32) {
	d, f := protocol.FaceOffset[dir], protocol.FaceOffset[from]
	switch t {
	case spreadSamePosition:
		return x, y, z, dir
	case spreadSamePlane:
		return x + d[0], y + d[1], z + d[2], from
	}
	return x + d[0] + f[0], y + d[1] + f[1], z + d[2] + f[2], int32(blocklogic.Opposite(int(dir)))
}

// canSpreadInto is SculkVeinSpreaderConfig.stateCanBeReplaced and
// MultifaceBlock.isValidStateForPlacement.
func (s *sculkSpreader) canSpreadInto(sx, sy, sz, px, py, pz, face int32) bool {
	return veinCanSpreadInto(s.w.get, sx, sy, sz, px, py, pz, face)
}

// veinCanSpreadInto is canSpreadInto over get, for the sculk_vein growth
// feature, whose spreader is the vein's too.
// vanilla: SculkVeinBlock.getSpreader
func veinCanSpreadInto(get func(x, y, z int32) uint16, sx, sy, sz, px, py, pz, face int32) bool {
	o := protocol.FaceOffset[face]
	switch registry.BlockOf(get(px+o[0], py+o[1], pz+o[2])) {
	case registry.BlockSculk, registry.BlockSculkCatalyst, registry.BlockMovingPiston:
		return false
	}
	if abs32(sx-px)+abs32(sy-py)+abs32(sz-pz) == 2 {
		b := protocol.FaceOffset[blocklogic.Opposite(int(face))]
		if registry.FaceSturdy(get(sx+b[0], sy+b[1], sz+b[2]), int(face), registry.SupportFull) {
			return false
		}
	}
	state := get(px, py, pz)
	if fluidHolding(state).kind != fluidNone && !isWaterSource(state) {
		return false
	}
	block := registry.BlockOf(state)
	if block.InTag(registry.TagIDFire) {
		return false
	}
	isVein := block == registry.BlockSculkVein
	if !block.IsReplaceable() && !IsAirState(state) && !isVein {
		return false
	}
	if isVein && registry.StateBool(state, facePropIDs[face]) {
		return false
	}
	return registry.FaceSturdy(get(px+o[0], py+o[1], pz+o[2]), blocklogic.Opposite(int(face)), registry.SupportFull)
}

// placeVein adds the face to the vein already in the cell, or starts one,
// waterlogged in a water source.
// vanilla: MultifaceBlock.getStateForPlacement
func (s *sculkSpreader) placeVein(x, y, z, face int32) bool {
	state := s.w.get(x, y, z)
	if registry.BlockOf(state) != registry.BlockSculkVein {
		state = veinState(0, isWaterSource(state))
	}
	state, _ = registry.WithBool(state, facePropIDs[face], true)
	return s.w.set(x, y, z, state, false)
}

// onDischarged takes the faces of a vein that now rest on sculk, and the
// vein itself when none is left. state is what the cursor read before its
// step.
// vanilla: SculkVeinBlock.onDischarged
func (s *sculkSpreader) onDischarged(kind sculkBlockKind, state uint16, x, y, z int32) {
	if kind != sculkIsVein || registry.BlockOf(state) != registry.BlockSculkVein {
		return
	}
	left := false
	for d := range int32(dirCount) {
		o := protocol.FaceOffset[d]
		if registry.StateBool(state, facePropIDs[d]) && registry.BlockOf(s.w.get(x+o[0], y+o[1], z+o[2])) == registry.BlockSculk {
			state, _ = registry.WithBool(state, facePropIDs[d], false)
		}
		left = left || registry.StateBool(state, facePropIDs[d])
	}
	switch {
	case left:
		s.w.set(x, y, z, state, true)
	case fluidHolding(s.w.get(x, y, z)).kind != fluidNone:
		s.w.set(x, y, z, registry.BlockWater.DefaultState(), true)
	default:
		s.w.set(x, y, z, airState, true)
	}
}

// attemptUseCharge is the charge the cursor keeps after the block under it
// has taken its share.
// vanilla: SculkBlock.attemptUseCharge, SculkVeinBlock.attemptUseCharge,
// SculkBehaviour.DEFAULT
func (s *sculkSpreader) attemptUseCharge(c *chargeCursor, kind sculkBlockKind, spreadVeins bool) int {
	switch kind {
	case sculkIsVein:
		if spreadVeins && s.attemptPlaceSculk(c.x, c.y, c.z) {
			return c.charge - 1
		}
		if s.rng.NextIntN(int32(s.t.decayRate)) == 0 {
			return c.charge / 2
		}
		return c.charge
	case sculkIsSculk:
		return s.sculkUseCharge(c)
	}
	if c.decayDelay > 0 {
		return c.charge
	}
	return 0
}

func (s *sculkSpreader) sculkUseCharge(c *chargeCursor) int {
	charge := c.charge
	if charge == 0 || s.rng.NextIntN(int32(s.t.decayRate)) != 0 {
		return charge
	}
	dx, dy, dz := c.x-s.origin.x, c.y-s.origin.y, c.z-s.origin.z
	distSq := dx*dx + dy*dy + dz*dz
	radius := s.t.noGrowthRadius
	close := int(distSq) < radius*radius
	if close || !s.canPlaceGrowth(c.x, c.y, c.z) {
		if s.rng.NextIntN(int32(s.t.additionalDecay)) != 0 {
			return charge
		}
		if close {
			return charge - 1
		}
		// float32 throughout, as vanilla's decay penalty is.
		outer := float32(math.Sqrt(float64(distSq))) - float32(radius)
		reach := float32((sculkMaxReach - radius) * (sculkMaxReach - radius))
		factor := min(1, outer*outer/reach)
		return charge - max(1, int(float32(charge)*factor*0.5))
	}
	if int(s.rng.NextIntN(int32(s.t.growthCost))) < charge {
		s.growth(c.x, c.y+1, c.z)
	}
	return max(0, charge-s.t.growthCost)
}

// canPlaceGrowth wants air or a water source above and at most two sensors or
// shriekers around.
// vanilla: SculkBlock.canPlaceGrowth
func (s *sculkSpreader) canPlaceGrowth(x, y, z int32) bool {
	above := s.w.get(x, y+1, z)
	if !IsAirState(above) && (!isWater(above) || !isWaterSource(above)) {
		return false
	}
	n := 0
	for dz := int32(-4); dz <= 4; dz++ {
		for dy := int32(0); dy <= 2; dy++ {
			for dx := int32(-4); dx <= 4; dx++ {
				if b := registry.BlockOf(s.w.get(x+dx, y+dy, z+dz)); b == registry.BlockSculkSensor || b == registry.BlockSculkShrieker {
					if n++; n > 2 {
						return false
					}
				}
			}
		}
	}
	return true
}

// growth places a sensor, or a shrieker one time in eleven.
// vanilla: SculkBlock.getRandomGrowthState
func (s *sculkSpreader) growth(x, y, z int32) {
	block := registry.BlockSculkSensor
	state := block.DefaultState()
	if s.rng.NextIntN(sculkShriekerRate) == 0 {
		block = registry.BlockSculkShrieker
		state, _ = registry.WithBool(block.DefaultState(), propCanSummon, s.t.worldGen)
	}
	if fluidHolding(s.w.get(x, y, z)).kind != fluidNone {
		state, _ = registry.WithBool(state, registry.PropWaterlogged, true)
	}
	if s.w.set(x, y, z, state, true) {
		// The charge cursor is below the growth. vanilla: SculkBlock.attemptUseCharge
		s.w.sound(x, y-1, z, registry.BlockSoundGroup(block).Place)
	}
}

// attemptPlaceSculk turns a replaceable block behind one of the vein's faces
// into sculk and lets the vein spread from the new block.
// vanilla: SculkVeinBlock.attemptPlaceSculk
func (s *sculkSpreader) attemptPlaceSculk(x, y, z int32) bool {
	state := s.w.get(x, y, z)
	for _, support := range s.rng.Perm(dirCount) {
		if !registry.StateBool(state, facePropIDs[support]) {
			continue
		}
		o := protocol.FaceOffset[support]
		sx, sy, sz := x+o[0], y+o[1], z+o[2]
		old := s.w.get(sx, sy, sz)
		if !registry.BlockOf(old).InTag(s.t.replaceable) || !s.w.set(sx, sy, sz, registry.BlockSculk.DefaultState(), true) {
			continue
		}
		s.w.placedSculk(sx, sy, sz, old)
		s.w.sound(sx, sy, sz, registry.SoundBlockSculkSpread)
		s.spreadAll(veinSpreadOrder, s.w.get(sx, sy, sz), sx, sy, sz)
		skip := blocklogic.Opposite(support)
		for d := range dirCount {
			if d == skip {
				continue
			}
			v := protocol.FaceOffset[d]
			if vein := s.w.get(sx+v[0], sy+v[1], sz+v[2]); registry.BlockOf(vein) == registry.BlockSculkVein {
				s.onDischarged(sculkIsVein, vein, sx+v[0], sy+v[1], sz+v[2])
			}
		}
		return true
	}
	return false
}

// validMovementPos picks a neighbouring sculk or vein cell in random order,
// preferring a vein with a face toward a replaceable block.
// vanilla: ChargeCursor.getValidMovementPos
func (s *sculkSpreader) validMovementPos(x, y, z int32) (int32, int32, int32, bool) {
	rx, ry, rz := x, y, z
	for _, i := range s.rng.Perm(len(nonCornerNeighbours)) {
		o := nonCornerNeighbours[i]
		nx, ny, nz := x+o[0], y+o[1], z+o[2]
		if dx, dz := nx-s.origin.x, nz-s.origin.z; s.t.worldGen && dx*dx+dz*dz > sculkGenReach*sculkGenReach {
			continue
		}
		state := s.w.get(nx, ny, nz)
		kind := sculkKindOf(state)
		if kind == sculkOther || !s.movementUnobstructed(x, y, z, o) {
			continue
		}
		rx, ry, rz = nx, ny, nz
		if kind != sculkIsVein {
			continue
		}
		for d := range dirCount {
			f := protocol.FaceOffset[d]
			if registry.StateBool(state, facePropIDs[d]) && registry.BlockOf(s.w.get(nx+f[0], ny+f[1], nz+f[2])).InTag(registry.TagIDSculkReplaceable) {
				return rx, ry, rz, true
			}
		}
	}
	return rx, ry, rz, rx != x || ry != y || rz != z
}

// movementUnobstructed lets a diagonal step through when one of the two
// cells it cuts across has no sturdy face toward it.
// vanilla: ChargeCursor.isMovementUnobstructed
func (s *sculkSpreader) movementUnobstructed(x, y, z int32, delta [3]int32) bool {
	if abs32(delta[0])+abs32(delta[1])+abs32(delta[2]) == 1 {
		return true
	}
	// Face indices run down, up, north, south, west, east.
	dx, dy, dz := axisDirIndex(4, delta[0]), axisDirIndex(0, delta[1]), axisDirIndex(2, delta[2])
	switch {
	case delta[0] == 0:
		return s.cellOpen(x, y, z, dy) || s.cellOpen(x, y, z, dz)
	case delta[1] == 0:
		return s.cellOpen(x, y, z, dx) || s.cellOpen(x, y, z, dz)
	}
	return s.cellOpen(x, y, z, dx) || s.cellOpen(x, y, z, dy)
}

// axisDirIndex is the face index along an axis (base is its negative face)
// in the sign's direction; a zero delta counts as positive.
func axisDirIndex(base int32, delta int32) int32 {
	if delta < 0 {
		return base
	}
	return base + 1
}

func (s *sculkSpreader) cellOpen(x, y, z, d int32) bool {
	o := protocol.FaceOffset[d]
	return !registry.FaceSturdy(s.w.get(x+o[0], y+o[1], z+o[2]), blocklogic.Opposite(int(d)), registry.SupportFull)
}
