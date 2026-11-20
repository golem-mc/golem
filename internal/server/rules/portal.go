package rules

import (
	"iter"
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// Nether portals: an obsidian frame around a 2x3 to 21x21 interior on
// either axis, filled by a fire lit inside and collapsing when frame or
// portal is broken. These are the decisions (valid shape, lighting, the
// collapse, the arrival's exit, search and built portal); writing blocks,
// standing ticks, travel and broadcasts are internal/server/portal.go's.

// The vanilla numbers a portal shape runs on.
const (
	portalMaxWidth  = 21 // the largest interior a frame may have; the smallest is 2x3
	portalMinWidth  = 2
	portalMinHeight = 3
)

// Portal block states, resolved at startup; a missing one is a data error,
// not a player's doing. Exported because the server writes them (the fire,
// the portal blocks, the frame's obsidian) and travels by the end portal's
// state.
var (
	ObsidianState  = registry.MustState("minecraft:obsidian", nil)
	FireState      = registry.MustState("minecraft:fire", nil)
	NetherPortalX  = registry.MustState("minecraft:nether_portal", nil) // the default state is axis=x
	NetherPortalZ  = registry.MustState("minecraft:nether_portal", map[string]string{"axis": "z"})
	EndPortalState = registry.MustState("minecraft:end_portal", nil)
)

// IsPortalState reports whether a block state is a nether portal, of either
// axis.
func IsPortalState(state uint16) bool {
	return state == NetherPortalX || state == NetherPortalZ
}

// portalAxisOf reports how the nether portal block state's axis runs: the
// axis=x portal's plane spans X, so its interior steps along X.
func portalAxisOf(state uint16) (dx, dz int32, ok bool) {
	switch state {
	case NetherPortalX:
		return 1, 0, true
	case NetherPortalZ:
		return 0, 1, true
	}
	return 0, 0, false
}

// PortalShape finds the interior holding (x, y, z) along the (dx, dz) axis
// and reports whether a valid obsidian frame closes it: out along the axis,
// down to the floor and up to the ceiling; valid when every interior cell is
// frame-fillable, the border is obsidian, and the size is within bounds.
// vanilla: PortalShape
//
// allowAir lets plain air count as interior, for ignition, where the fire
// goes into an opening. Revalidation passes false: air in a frame is a
// punched hole, not a portal in the making.
func PortalShape(w BlockReader, dx, dz, x, y, z int32, allowAir bool) ([][3]int32, bool) {
	interior := func(state uint16) bool {
		if isAirState(state) {
			return allowAir
		}
		if IsPortalState(state) {
			return true
		}
		// Any state of either fire counts: the fire a frame is lit with
		// ages and leans as it stands.
		// vanilla: PortalShape.isEmpty — isAir, BlockTags.FIRE or nether_portal
		return IsFire(state)
	}
	if !interior(w.BlockAt(x, y, z)) {
		return nil, false
	}
	// Out along the axis, both ways, while the interior goes on.
	left, right := int32(0), int32(0)
	for left < portalMaxWidth-1 && interior(w.BlockAt(x-dx*(left+1), y, z-dz*(left+1))) {
		left++
	}
	for right < portalMaxWidth-1 && interior(w.BlockAt(x+dx*(right+1), y, z+dz*(right+1))) {
		right++
	}
	// Down to the floor and up to the ceiling, in the seed cell's column,
	// which every other column of the interior is then required to match.
	down, up := int32(0), int32(0)
	for down < portalMaxWidth-1 && interior(w.BlockAt(x, y-down-1, z)) {
		down++
	}
	for up < portalMaxWidth-1 && interior(w.BlockAt(x, y+up+1, z)) {
		up++
	}
	width, height := left+right+1, down+up+1
	if width < portalMinWidth || width > portalMaxWidth || height < portalMinHeight || height > portalMaxWidth {
		return nil, false
	}

	// The interior, every cell of it, with the frame that has to close it.
	cells := make([][3]int32, 0, width*height)
	for i := -left; i <= right; i++ {
		cx, cz := x+dx*i, z+dz*i
		for j := -down; j <= up; j++ {
			if !interior(w.BlockAt(cx, y+j, cz)) {
				return nil, false
			}
			cells = append(cells, [3]int32{cx, y + j, cz})
			// The floor beneath the bottom of every column, and the ceiling
			// over the top of every one.
			if j == -down && w.BlockAt(cx, y+j-1, cz) != ObsidianState {
				return nil, false
			}
			if j == up && w.BlockAt(cx, y+j+1, cz) != ObsidianState {
				return nil, false
			}
		}
	}
	// The frame closing each end of the interior, floor to ceiling.
	for j := -down; j <= up; j++ {
		if w.BlockAt(x-dx*(left+1), y+j, z-dz*(left+1)) != ObsidianState {
			return nil, false
		}
		if w.BlockAt(x+dx*(right+1), y+j, z+dz*(right+1)) != ObsidianState {
			return nil, false
		}
	}
	return cells, true
}

// LightPortal returns the portal blocks filling a valid frame around the
// fire at (x, y, z), both axes tried and at most one valid, and whether
// there was one. Only the overworld and the nether light portals.
// vanilla: BaseFireBlock.inPortalDimension, PortalShape.findEmptyPortalShape
func LightPortal(w BlockReader, dimension string, x, y, z int32) ([]world.BlockChange, bool) {
	if dimension != world.DimensionName && dimension != world.NetherName {
		return nil, false
	}
	for _, state := range [2]uint16{NetherPortalX, NetherPortalZ} {
		dx, dz, _ := portalAxisOf(state)
		interior, ok := PortalShape(w, dx, dz, x, y, z, true)
		if !ok {
			continue
		}
		lit := make([]world.BlockChange, len(interior))
		for i, cell := range interior {
			lit[i] = world.BlockChange{X: cell[0], Y: cell[1], Z: cell[2], State: state}
		}
		return lit, true
	}
	return nil, false
}

// portalSteps are the six neighbours of a portal block.
var portalSteps = [6][3]int32{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}

// CollapsedPortal returns the changes turning every portal block connected
// to (x, y, z) into air, leaving the frame.
func CollapsedPortal(w BlockReader, x, y, z int32) []world.BlockChange {
	visited := map[[3]int32]bool{}
	queue := [][3]int32{{x, y, z}}
	var cleared []world.BlockChange
	for len(queue) > 0 {
		cell := queue[0]
		queue = queue[1:]
		if visited[cell] {
			continue
		}
		visited[cell] = true
		if !IsPortalState(w.BlockAt(cell[0], cell[1], cell[2])) {
			continue
		}
		cleared = append(cleared, world.BlockChange{X: cell[0], Y: cell[1], Z: cell[2], State: airState})
		for _, step := range portalSteps {
			queue = append(queue, [3]int32{cell[0] + step[0], cell[1] + step[1], cell[2] + step[2]})
		}
	}
	return cleared
}

// CollapsingPortals returns the portal blocks next to a change at (x, y, z)
// that lost their frame, as a portal block's updateShape sees it: only
// neighbours along the axis or above and below count, never the changed cell.
// A portal or obsidian neighbour still holds; anything else re-checks the
// shape. So placing portal blocks one at a time (/setblock) leaves them
// standing. Each position stands for its whole portal, which the caller
// removes.
func CollapsingPortals(w BlockReader, x, y, z int32) [][3]int32 {
	changed := w.BlockAt(x, y, z)
	var collapsing [][3]int32
	for _, step := range portalSteps {
		px, py, pz := x+step[0], y+step[1], z+step[2]
		dx, dz, isPortal := portalAxisOf(w.BlockAt(px, py, pz))
		if !isPortal {
			continue
		}
		alongAxis := step[1] != 0 || (step[0] != 0 && dx != 0) || (step[2] != 0 && dz != 0)
		if !alongAxis || IsPortalState(changed) || changed == ObsidianState {
			continue
		}
		if _, ok := PortalShape(w, dx, dz, px, py, pz, false); !ok {
			collapsing = append(collapsing, [3]int32{px, py, pz})
		}
	}
	return collapsing
}

// The radii a crossing's arrival searches for a portal to reuse.
// vanilla: PortalForcer.findClosestPortalPosition
const (
	portalSearchOverworld = 128
	portalSearchNether    = 16
)

// NetherPortalExit is where a nether portal crossed at (x, y, z) leads: the
// overworld from the nether, the nether from anywhere else (the end at the
// overworld's scale), with the block the arrival's search centres on and its
// radius. The Y is not scaled.
// vanilla: NetherPortalBlock.getPortalDestination
func NetherPortalExit(inNether bool, x, y, z float64) (toNether bool, at world.SpawnPos, radius int32) {
	// Scale the exact coordinate, then floor (vanilla: BlockPos.containing);
	// scaling the block coordinate could be 8 blocks off.
	scale, radius := 1.0/8, int32(portalSearchNether)
	if inNether {
		scale, radius = 8, portalSearchOverworld
	}
	at = world.SpawnPos{X: int32(math.Floor(x * scale)), Y: int32(math.Floor(y)), Z: int32(math.Floor(z * scale))}
	return !inNether, at, radius
}

// PortalSearch looks for the portal block nearest At in the square of
// Radius around it (any Y), inside the destination border: least 3D squared
// distance, ties to the lower Y.
// vanilla: PortalForcer.findClosestPortalPosition, PoiManager.getInSquare
type PortalSearch struct {
	At     world.SpawnPos
	Radius int32
	// InBorder reports whether a block column is inside the world border.
	InBorder func(x, z int32) bool

	best  world.SpawnPos // the foot of the nearest block's column
	bestY int32          // the nearest block's own Y
	dist  int64
	found bool
}

// Chunks yields the chunks touching the square.
func (s *PortalSearch) Chunks() iter.Seq[world.ChunkPos] {
	return func(yield func(world.ChunkPos) bool) {
		minX, maxX := world.ChunkOf(float64(s.At.X-s.Radius)), world.ChunkOf(float64(s.At.X+s.Radius))
		minZ, maxZ := world.ChunkOf(float64(s.At.Z-s.Radius)), world.ChunkOf(float64(s.At.Z+s.Radius))
		for cx := minX; cx <= maxX; cx++ {
			for cz := minZ; cz <= maxZ; cz++ {
				if !yield(world.ChunkPos{X: cx, Z: cz}) {
					return
				}
			}
		}
	}
}

// ScanChunk weighs every portal block of the chunk at pos within minY..maxY.
// Keeping the chunk loaded during the scan is the caller's.
func (s *PortalSearch) ScanChunk(w BlockReader, pos world.ChunkPos, minY, maxY int32) {
	var sections world.SectionMask
	indexed, hasIndex := w.(interface {
		PortalSections(world.ChunkPos) world.SectionMask
	})
	if hasIndex {
		sections = indexed.PortalSections(pos)
		if sections == 0 {
			return
		}
	}
	baseX, baseZ := pos.X*16, pos.Z*16
	for lz := range int32(16) {
		z := baseZ + lz
		dz := int64(z - s.At.Z)
		if dz < -int64(s.Radius) || dz > int64(s.Radius) {
			continue
		}
		for lx := range int32(16) {
			x := baseX + lx
			dx := int64(x - s.At.X)
			if dx < -int64(s.Radius) || dx > int64(s.Radius) || (s.InBorder != nil && !s.InBorder(x, z)) {
				continue
			}
			foot := minY // the bottom of the run of portal blocks y is in
			for y := minY; y <= maxY; y++ {
				if hasIndex && !sections.Has(y) {
					// A section without portals breaks the run just like non-portal cells.
					y = min(y|15, maxY)
					foot = y + 1
					continue
				}
				if !IsPortalState(w.BlockAt(x, y, z)) {
					foot = y + 1
					continue
				}
				dy := int64(y - s.At.Y)
				d := dx*dx + dy*dy + dz*dz
				if !s.found || d < s.dist || (d == s.dist && y < s.bestY) {
					s.best, s.bestY, s.dist, s.found = world.SpawnPos{X: x, Y: foot, Z: z}, y, d, true
				}
			}
		}
	}
}

// Found returns the bottom of the nearest portal block's column, so the
// arrival stands in the portal as the departure did; false when no chunk
// scanned held one.
func (s *PortalSearch) Found() (world.SpawnPos, bool) {
	return s.best, s.found
}

// PortalSiteWorld is the destination a missing exit portal is built in.
type PortalSiteWorld interface {
	BlockAt(x, y, z int32) uint16
	// MotionBlockingHeight is the y of the column's highest motion-blocking
	// block, one below the floor for none.
	MotionBlockingHeight(x, z int32) int32
}

// PortalBorder is the destination's world border.
type PortalBorder interface {
	ContainsBlock(x, z int32) bool
	ClampBlock(x, z int32) (int32, int32)
}

// The square a missing exit portal's site is sought in, and how far past it
// the frame tests read: the frame's width -1..2 along the axis and one
// column either side across it.
const (
	PortalBuildRadius = 16
	PortalBuildMargin = 2
)

// BuildNetherPortal chooses where the missing exit portal near origin stands
// and returns its blocks, the 2x3 portal along axis (NetherPortalAxis's step)
// with its obsidian frame. The site is the nearest (3D, to origin) column of
// the square of radius 16 with a solid floor and replaceable, fluid-free room
// for the frame and the columns on both faces, else the nearest with room
// for the frame alone, else a platform forced at origin's Y clamped to
// 70..maxPlaceableY-9. corner is the portal's bottom cell at the low end of
// the axis; false only when that band is empty. minY and maxPlaceableY bound
// the walk: maxPlaceableY is the top of the logical height. The caller keeps
// the chunks within PortalBuildRadius+PortalBuildMargin loaded.
// vanilla: PortalForcer.createPortal
func BuildNetherPortal(w PortalSiteWorld, border PortalBorder, origin world.SpawnPos, radius int32, axis [2]int32, minY, maxPlaceableY int32) (corner world.SpawnPos, changes []world.BlockChange, ok bool) {
	dx, dz := axis[0], axis[1]
	cwx, cwz := -dz, dx // the axis' direction turned clockwise
	canReplace := func(x, y, z int32) bool { return canPortalReplace(w.BlockAt(x, y, z)) }
	// hostsFrame is vanilla's canHostFrame: a solid floor under the frame's
	// four columns, offset across the axis, and replaceable room in them.
	hostsFrame := func(x, y, z, offset int32) bool {
		for width := int32(-1); width < 3; width++ {
			fx, fz := x+dx*width+cwx*offset, z+dz*width+cwz*offset
			if !registry.IsSolid(w.BlockAt(fx, y-1, fz)) {
				return false
			}
			for height := range int32(4) {
				if !canReplace(fx, y+height, fz) {
					return false
				}
			}
		}
		return true
	}

	var full, partial world.SpawnPos
	fullDist, partialDist := int64(-1), int64(-1)
	for x, z := range spiralAround(origin.X, origin.Z, radius) {
		if !border.ContainsBlock(x, z) || !border.ContainsBlock(x+dx, z+dz) {
			continue
		}
		// getHeight is the first free y, one above the top block.
		for y := min(maxPlaceableY, w.MotionBlockingHeight(x, z)+1); y >= minY; y-- {
			if !canReplace(x, y, z) {
				continue
			}
			firstEmpty := y
			for y > minY && canReplace(x, y-1, z) {
				y--
			}
			// Room for the portal under the logical top, and a gap of one or two
			// is too low to stand the portal in.
			if gap := firstEmpty - y; y+4 > maxPlaceableY || gap > 0 && gap < 3 {
				continue
			}
			if !hostsFrame(x, y, z, 0) {
				continue
			}
			ex, ey, ez := int64(x-origin.X), int64(y-origin.Y), int64(z-origin.Z)
			d := ex*ex + ey*ey + ez*ez
			at := world.SpawnPos{X: x, Y: y, Z: z}
			if hostsFrame(x, y, z, -1) && hostsFrame(x, y, z, 1) && (fullDist == -1 || fullDist > d) {
				full, fullDist = at, d
			}
			if fullDist == -1 && (partialDist == -1 || partialDist > d) {
				partial, partialDist = at, d
			}
		}
	}
	if fullDist == -1 && partialDist != -1 {
		full, fullDist = partial, partialDist
	}

	changes = make([]world.BlockChange, 0, 30)
	if fullDist == -1 {
		maxStart, minStart := maxPlaceableY-9, max(minY+1, 70)
		if maxStart < minStart {
			return world.SpawnPos{}, nil, false
		}
		x, z := border.ClampBlock(origin.X-dx, origin.Z-dz)
		full = world.SpawnPos{X: x, Y: min(max(origin.Y, minStart), maxStart), Z: z}
		// An obsidian platform under the portal and both faces, and air to stand
		// in above it; the portal's own cells are written below.
		for _, box := range [2]int32{-1, 1} {
			for width := range int32(2) {
				for height := int32(-1); height < 3; height++ {
					state := airState
					if height < 0 {
						state = ObsidianState
					}
					changes = append(changes, world.BlockChange{
						X: full.X + width*dx + box*cwx, Y: full.Y + height, Z: full.Z + width*dz + box*cwz, State: state,
					})
				}
			}
		}
	}
	for width := int32(-1); width < 3; width++ {
		for height := int32(-1); height < 4; height++ {
			if width != -1 && width != 2 && height != -1 && height != 3 {
				continue
			}
			changes = append(changes, world.BlockChange{X: full.X + width*dx, Y: full.Y + height, Z: full.Z + width*dz, State: ObsidianState})
		}
	}
	portal := NetherPortalX
	if dz != 0 {
		portal = NetherPortalZ
	}
	for width := range int32(2) {
		for height := range int32(3) {
			changes = append(changes, world.BlockChange{X: full.X + width*dx, Y: full.Y + height, Z: full.Z + width*dz, State: portal})
		}
	}
	return full, changes, true
}

// canPortalReplace reports whether a built portal's frame may take a cell:
// replaceable and holding no fluid.
// vanilla: PortalForcer.canPortalReplaceBlock
func canPortalReplace(state uint16) bool {
	return IsReplaceableState(state) && !world.HoldsFluid(state)
}

// spiralAround yields the columns of the square of radius around (cx, cz)
// from the centre outward, turning east, south, west, north; the order
// settles ties between equally near sites.
// vanilla: BlockPos.spiralAround(center, radius, EAST, SOUTH)
func spiralAround(cx, cz, radius int32) iter.Seq2[int32, int32] {
	return func(yield func(x, z int32) bool) {
		dirs := [4][2]int32{{1, 0}, {0, 1}, {-1, 0}, {0, -1}}
		x, z := cx, cz+1 // the cursor starts one step south
		leg, legSize, legIndex := int32(-1), int32(0), int32(0)
		for {
			d := dirs[(leg+4)%4]
			x, z = x+d[0], z+d[1]
			if legIndex >= legSize {
				if leg >= 4*radius {
					return
				}
				leg++
				legIndex, legSize = 0, leg/2+1
			}
			legIndex++
			if !yield(x, z) {
				return
			}
		}
	}
}

// NearestPOI finds the block of the chunk nearest to the point whose
// point-of-interest type is in types, no farther than maxDistSqr, and returns
// its type and squared distance. The caller keeps the chunk loaded for the
// scan.
// vanilla: PoiManager.findClosestWithType
func NearestPOI(w BlockReader, pos world.ChunkPos, minY, maxY, x, y, z int32, types []string, maxDistSqr int64) (world.SpawnPos, string, int64, bool) {
	baseX, baseZ := pos.X*16, pos.Z*16
	var best world.SpawnPos
	var bestType string
	bestDist, found := maxDistSqr, false
	for by := minY; by <= maxY; by++ {
		dy := int64(by - y)
		for lz := range int32(16) {
			dz := int64(baseZ + lz - z)
			for lx := range int32(16) {
				dx := int64(baseX + lx - x)
				d := dx*dx + dy*dy + dz*dz
				if d > bestDist || found && d == bestDist {
					continue
				}
				typ := POITypeOf(w.BlockAt(baseX+lx, by, baseZ+lz))
				if typ != "" && slices.Contains(types, typ) {
					best, bestType, bestDist, found = world.SpawnPos{X: baseX + lx, Y: by, Z: baseZ + lz}, typ, d, true
				}
			}
		}
	}
	return best, bestType, bestDist, found
}

// PortalTransitionTime is 0: a non-player crosses on its first tick in
// a portal (vanilla: NetherPortalBlock.getPortalTransitionTime).
const PortalTransitionTime = 0

// PortalKind is which portal a body stands in (vanilla:
// PortalProcessor.isSamePortal).
type PortalKind uint8

const (
	PortalNone PortalKind = iota
	PortalNether
	PortalEnd
)

// EntityPortal is a non-player's portal state (vanilla: Entity.portalCooldown
// and PortalProcessor). Cooldown and Inside are in ticks.
type EntityPortal struct {
	Cooldown int
	Inside   int
	Kind     PortalKind
}

// PortalKindOfBlock reports which portal a block state is, if any.
func PortalKindOfBlock(state uint16) (PortalKind, bool) {
	switch {
	case IsPortalState(state):
		return PortalNether, true
	case state == EndPortalState:
		return PortalEnd, true
	}
	return PortalNone, false
}

// NetherPortalAxis returns the step along a nether portal block's axis.
func NetherPortalAxis(state uint16) [2]int32 {
	if dx, dz, ok := portalAxisOf(state); ok {
		return [2]int32{dx, dz}
	}
	return [2]int32{1, 0} // vanilla's Optional.orElse fallback
}

// Step advances the portal state a tick for the block the body is in and
// reports a portal that takes it now. cooldown is the body's dimension
// change delay: 300 plain, 2 for a projectile. A portal during the cooldown
// restarts it, so arrivals do not bounce back; outside one the count decays
// 4 a tick. A due result already set the cooldown, so a crossing with
// nowhere to arrive is still spent.
// vanilla: Entity.handlePortal, setAsInsidePortal,
// Projectile.getDimensionChangingDelay
func (p *EntityPortal) Step(state uint16, transitionTime, cooldown int) (PortalKind, bool) {
	return p.step(state, transitionTime, cooldown, true)
}

// StepRider is Step for a passenger, which may not use a portal: its count
// holds in one rather than grows, and it is never due.
// vanilla: Entity.canUsePortal, PortalProcessor.processPortalTeleportation
func (p *EntityPortal) StepRider(state uint16, cooldown int) {
	p.step(state, 0, cooldown, false)
}

func (p *EntityPortal) step(state uint16, transitionTime, cooldown int, allowed bool) (PortalKind, bool) {
	if p.Cooldown > 0 {
		p.Cooldown--
	}
	kind, standing := PortalKindOfBlock(state)
	if !standing {
		if p.Inside > 0 {
			if p.Inside = PortalDecay(p.Inside); p.Inside == 0 {
				p.Kind = PortalNone
			}
		}
		return PortalNone, false
	}
	if p.Cooldown > 0 {
		p.Cooldown = cooldown
		p.Inside, p.Kind = 0, PortalNone
		return PortalNone, false
	}
	if kind != p.Kind {
		p.Kind, p.Inside = kind, 0
	}
	if !allowed {
		return PortalNone, false
	}
	p.Inside++
	if p.Inside >= transitionTime {
		p.Inside, p.Kind = 0, PortalNone
		p.Cooldown = cooldown
		return kind, true
	}
	return PortalNone, false
}

// PortalDecay is a standing count after a tick out of the portal: 4 off,
// clamped at 0, so a brief exit keeps most of it.
// vanilla: PortalProcessor.decayTick
func PortalDecay(inside int) int {
	return max(0, inside-4)
}

// PlayerPortalWait is the ticks a player stands in a nether portal before
// it takes them: the game rule's wait for their mode, which delay reads,
// clamped at 0, the invulnerable creative and spectator taking creative's;
// none for a minecart's or boat's rider, the vehicle crossing and being no
// Player.
// vanilla: NetherPortalBlock.getPortalTransitionTime, Entity.canUsePortal
func PlayerPortalWait(gameMode int32, inVehicle bool, delay func(rule world.GameRuleIndex) int32) int32 {
	if inVehicle {
		return 0
	}
	rule := world.RulePlayersNetherPortalDefaultDelay
	if gameMode == protocol.GameModeCreative || gameMode == protocol.GameModeSpectator {
		rule = world.RulePlayersNetherPortalCreativeDelay
	}
	return max(0, delay(rule))
}

// PortalTurn returns the degrees to add to a crossing body's yaw. Nether:
// 90 when the two portals' axes differ, else 0 (vanilla: outputRotation).
// End: whatever makes the yaw 90 (west), in both directions.
func PortalTurn(kind PortalKind, sourceAxis, exitAxis [2]int32, yaw float32) float32 {
	if kind == PortalEnd {
		return 90 - yaw
	}
	if sourceAxis != exitAxis {
		return 90
	}
	return 0
}
